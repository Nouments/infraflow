package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"infraflow/internal/domain"
	"infraflow/internal/infrastructure/safefs"
	"infraflow/internal/ports"
	"infraflow/pkg/protocol"
)

const maxGeneratedArtifactBytes = 16 << 20

type PlanGenerationResult struct {
	Plan               domain.Plan            `json:"plan"`
	Tasks              []TaskGenerationResult `json:"tasks"`
	GenerationStatus   string                 `json:"generation_status"`
	ExecutionStatus    string                 `json:"execution_status"`
	VerificationStatus string                 `json:"verification_status"`
	AuditStatus        string                 `json:"audit_status"`
	// Published is true when the complete input was published to the catalog
	// that Agents synchronize.
	Published bool `json:"published"`
	// Unpublished lists devices the native generator refuses; they are left
	// out of the published catalog, exactly as `generate-all` would refuse them.
	Unpublished []UnpublishedDevice `json:"unpublished,omitempty"`
}

type UnpublishedDevice struct {
	Site   string `json:"site"`
	Device string `json:"device"`
	Reason string `json:"reason"`
}

type TaskGenerationResult struct {
	TaskID             string                 `json:"task_id"`
	Site               string                 `json:"site"`
	Device             string                 `json:"device"`
	Method             string                 `json:"method,omitempty"`
	Status             string                 `json:"status"`
	Capability         domain.CapabilityState `json:"capability"`
	Reason             string                 `json:"reason,omitempty"`
	ExecutionStatus    string                 `json:"execution_status"`
	VerificationStatus string                 `json:"verification_status"`
	Artifacts          []GeneratedArtifact    `json:"artifacts,omitempty"`
}

type GeneratedArtifact struct {
	Site            string `json:"site"`
	Device          string `json:"device"`
	TaskID          string `json:"task_id"`
	Type            string `json:"type"`
	Path            string `json:"path"`
	Format          string `json:"format"`
	InputHash       string `json:"input_hash"`
	SHA256          string `json:"sha256"`
	TemplateVersion string `json:"template_version"`
	Status          string `json:"status"`
}

type nativeManifestVersion struct {
	TemplateVersion string `json:"template_version"`
	InputHash       string `json:"input_hash"`
	Status          string `json:"status"`
	Devices         []struct {
		Name string `json:"name"`
		Path string `json:"path"`
	} `json:"devices"`
}

// PlanAndGenerate links a validated plan to native configuration artifacts.
// It records render-only evidence; it never runs a device command.
func (service *Service) PlanAndGenerate(input []byte) (PlanGenerationResult, error) {
	return service.PlanAndGenerateContext(context.Background(), input)
}

func (service *Service) PlanAndGenerateContext(ctx context.Context, input []byte) (PlanGenerationResult, error) {
	infrastructure, err := service.Validate(input)
	if err != nil {
		return PlanGenerationResult{}, err
	}
	if service.planBuilder == nil {
		return PlanGenerationResult{}, fmt.Errorf("plan builder is not configured")
	}
	result := PlanGenerationResult{
		Plan:               service.planBuilder.Build(infrastructure),
		GenerationStatus:   "NOT_REQUIRED",
		ExecutionStatus:    "NOT_REQUESTED",
		VerificationStatus: "NOT_PERFORMED",
		AuditStatus:        "NOT_CONFIGURED",
	}
	if len(result.Plan.Tasks) == 0 {
		return result, nil
	}

	taskIndexes := make(map[string]int, len(result.Plan.Tasks))
	for index, task := range result.Plan.Tasks {
		taskIndexes[task.Site+"\x00"+task.Target] = index
		result.Tasks = append(result.Tasks, TaskGenerationResult{
			TaskID: task.ID, Site: task.Site, Device: task.Target, Method: task.Method,
			Status: "PLANNED", Capability: domain.CapabilityUnknown,
			ExecutionStatus: result.ExecutionStatus, VerificationStatus: result.VerificationStatus,
		})
	}

	filtered := domain.Infrastructure{}
	for _, site := range infrastructure.Sites {
		filteredSite := domain.Site{Name: site.Name}
		for _, device := range site.Devices {
			taskIndex, exists := taskIndexes[site.Name+"\x00"+device.Name]
			if !exists {
				continue
			}
			profile, method, reason := service.interfaceGeneratorSupport(site, device)
			if reason != "" {
				result.Tasks[taskIndex].Status = "UNSUPPORTED"
				result.Tasks[taskIndex].Reason = reason
				result.Plan.Tasks[taskIndex].Status = "UNSUPPORTED"
				result.Plan.Tasks[taskIndex].Reason = reason
				continue
			}
			result.Tasks[taskIndex].Capability = profile.Status
			device.Provisioning.Method = domain.ProvisioningMethod(method)
			filteredSite.Devices = append(filteredSite.Devices, device)
		}
		if len(filteredSite.Devices) > 0 {
			filtered.Sites = append(filtered.Sites, filteredSite)
		}
	}

	supportedCount := 0
	for _, task := range result.Tasks {
		if task.Status == "PLANNED" {
			supportedCount++
		}
	}
	if supportedCount == 0 {
		result.GenerationStatus = "UNSUPPORTED"
		return service.recordPlanGeneration(ctx, result)
	}
	generator, ok := service.generator.(ports.NativeArtifactGenerator)
	if !ok {
		return service.markGenerationUnavailable(ctx, result, "native configuration generator is not configured")
	}
	if strings.TrimSpace(service.artifactDirectory) == "" {
		return service.markGenerationUnavailable(ctx, result, "artifact directory is not configured")
	}
	// The artifact directory is the published catalog that Agents synchronize.
	// Writing native files into it alone would leave site-manifest.json with
	// stale hashes and make the whole catalog unavailable (D15). So the complete
	// validated input is published through the staged, locked GenerateAll path
	// (the same as `generate-all`), and only a generator without it renders
	// into a throwaway directory that is never published.
	readRoot := service.artifactDirectory
	var artifacts []protocol.Artifact
	if publisher, publishes := service.generator.(ports.AllArtifactGenerator); publishes {
		var published domain.Infrastructure
		published, result.Unpublished = service.publishableInfrastructure(infrastructure)
		artifacts, err = publisher.GenerateAll(published, service.artifactDirectory)
		result.Published = err == nil
	} else {
		staging, stagingErr := os.MkdirTemp("", "infraflow-plan-generate-")
		if stagingErr != nil {
			return service.markGenerationUnavailable(ctx, result, "staging directory is unavailable")
		}
		defer os.RemoveAll(staging)
		readRoot = staging
		artifacts, err = generator.GenerateNative(filtered, staging)
	}
	if err != nil {
		for index := range result.Tasks {
			if result.Tasks[index].Status == "PLANNED" {
				result.Tasks[index].Status = "FAILED"
				result.Tasks[index].Reason = "native configuration generation failed"
				result.Plan.Tasks[index].Status = "FAILED"
				result.Plan.Tasks[index].Reason = result.Tasks[index].Reason
			}
		}
		result.GenerationStatus = "FAILED"
		result, auditErr := service.recordPlanGeneration(ctx, result)
		return result, errors.Join(fmt.Errorf("generate and publish artifacts: %w", err), auditErr)
	}

	manifests := make(map[string]nativeManifestVersion)
	for _, artifact := range artifacts {
		if artifact.Type != "native_manifest" {
			continue
		}
		site, _, _ := strings.Cut(artifact.Path, "/")
		data, err := readGeneratedArtifact(readRoot, artifact)
		if err != nil {
			return service.failPlanGeneration(ctx, result, err)
		}
		var manifest nativeManifestVersion
		if err := json.Unmarshal(data, &manifest); err != nil {
			return service.failPlanGeneration(ctx, result, fmt.Errorf("decode generated native manifest %q: %w", artifact.Path, err))
		}
		if strings.TrimSpace(manifest.TemplateVersion) == "" || manifest.Status != "rendered_unverified" || !protocol.IsSHA256(manifest.InputHash) || manifest.InputHash != artifact.InputHash {
			return service.failPlanGeneration(ctx, result, fmt.Errorf("generated native manifest %q has invalid provenance", artifact.Path))
		}
		manifests[site] = manifest
	}

	artifactsBySite := make(map[string][]protocol.Artifact)
	for _, artifact := range artifacts {
		if artifact.Type != "native_device_config" {
			continue
		}
		if _, err := readGeneratedArtifact(readRoot, artifact); err != nil {
			return service.failPlanGeneration(ctx, result, err)
		}
		site, _, _ := strings.Cut(artifact.Path, "/")
		artifactsBySite[site] = append(artifactsBySite[site], artifact)
	}

	for index := range result.Tasks {
		task := &result.Tasks[index]
		if task.Status != "PLANNED" {
			continue
		}
		manifest, exists := manifests[task.Site]
		if !exists {
			return service.failPlanGeneration(ctx, result, fmt.Errorf("generated native manifest does not match task %q", task.TaskID))
		}
		version := manifest.TemplateVersion
		devicePath := ""
		for _, device := range manifest.Devices {
			if device.Name == task.Device {
				devicePath = device.Path
				break
			}
		}
		if devicePath == "" {
			return service.failPlanGeneration(ctx, result, fmt.Errorf("generated native manifest does not include device %q", task.Device))
		}
		for _, artifact := range artifactsBySite[task.Site] {
			if artifact.InputHash != manifest.InputHash {
				return service.failPlanGeneration(ctx, result, fmt.Errorf("generated artifact %q has a mismatched input hash", artifact.Path))
			}
			if artifact.Path != devicePath {
				continue
			}
			format := generatedArtifactFormat(artifact.Path)
			task.Artifacts = append(task.Artifacts, GeneratedArtifact{
				Site: task.Site, Device: task.Device, TaskID: task.TaskID,
				Type: artifact.Type, Path: artifact.Path, Format: format,
				InputHash: artifact.InputHash, SHA256: artifact.OutputHash,
				TemplateVersion: version, Status: "GENERATED",
			})
			result.Plan.Tasks[index].Artifacts = append(result.Plan.Tasks[index].Artifacts, artifact.Path)
		}
		if len(task.Artifacts) == 0 || version == "" {
			return service.failPlanGeneration(ctx, result, fmt.Errorf("generator returned no traceable native configuration for task %q", task.TaskID))
		}
		task.Status = "GENERATED"
		result.Plan.Tasks[index].Status = "GENERATED"
	}

	allGenerated := true
	for _, task := range result.Tasks {
		if task.Status != "GENERATED" {
			allGenerated = false
		}
	}
	if allGenerated {
		result.GenerationStatus = "GENERATED"
	} else {
		result.GenerationStatus = "PARTIAL"
	}
	if allGenerated {
		if err := result.Plan.Lifecycle.Transition(domain.StateGenerated); err != nil {
			return service.failPlanGeneration(ctx, result, fmt.Errorf("record generated plan lifecycle: %w", err))
		}
	}
	return service.recordPlanGeneration(ctx, result)
}

func (service *Service) failPlanGeneration(ctx context.Context, result PlanGenerationResult, cause error) (PlanGenerationResult, error) {
	result.GenerationStatus = "FAILED"
	for index := range result.Tasks {
		if result.Tasks[index].Status == "PLANNED" {
			result.Tasks[index].Status = "FAILED"
			result.Tasks[index].Reason = "generated artifact provenance validation failed"
			result.Plan.Tasks[index].Status = "FAILED"
			result.Plan.Tasks[index].Reason = result.Tasks[index].Reason
		}
	}
	result, auditErr := service.recordPlanGeneration(ctx, result)
	return result, errors.Join(cause, auditErr)
}

func (service *Service) markGenerationUnavailable(ctx context.Context, result PlanGenerationResult, reason string) (PlanGenerationResult, error) {
	for index := range result.Tasks {
		if result.Tasks[index].Status != "PLANNED" {
			continue
		}
		result.Tasks[index].Status = "UNVERIFIED"
		result.Tasks[index].Reason = reason
		result.Plan.Tasks[index].Status = "UNVERIFIED"
		result.Plan.Tasks[index].Reason = reason
	}
	result.GenerationStatus = "UNAVAILABLE"
	result, auditErr := service.recordPlanGeneration(ctx, result)
	return result, errors.Join(fmt.Errorf("%s", reason), auditErr)
}

// interfaceGeneratorSupport decides whether a planned interface task can be
// generated. Vendor/method support comes from the native generator itself
// (ports.NativeSupportChecker) so the web path and generate-all agree; the
// plan model adds one restriction of its own: it describes interface
// configuration only, so devices with static routes or NAT intent are
// reported as unsupported here rather than generated partially.
func (service *Service) interfaceGeneratorSupport(site domain.Site, device domain.Device) (domain.VendorProfile, string, string) {
	profile, known := domain.VendorProfileFor(device)
	method := strings.TrimSpace(string(device.Provisioning.Method))
	if method == "" && known {
		method = profile.DefaultMethod
	}
	if device.Network != nil && (len(device.Network.Routes) > 0 || len(device.Network.NAT.Source) > 0 || len(device.Network.NAT.Destination) > 0) {
		return profile, method, "the current plan models interface configuration only; static routes and NAT intent are unsupported"
	}
	if checker, ok := service.generator.(ports.NativeSupportChecker); ok {
		if err := checker.CheckNativeDevice(site, device); err != nil {
			return profile, method, "native generator cannot render this device: " + err.Error()
		}
		if !known {
			profile = domain.VendorProfile{Vendor: device.Vendor, Family: device.Family, Model: device.Model, Status: domain.CapabilityUnverified}
		}
		return profile, method, ""
	}
	if !known {
		return domain.VendorProfile{}, "", fmt.Sprintf("no native configuration profile for %s/%s", device.Vendor, device.Family)
	}
	if _, ok := profile.Methods[method]; !ok {
		return profile, method, fmt.Sprintf("method %q is not supported by the %s/%s profile", method, device.Vendor, device.Family)
	}
	if (profile.Family == "routeros" || profile.Family == "fortios") && (device.Network == nil || !hasIPv4Interface(device.Network.Interfaces)) {
		return profile, method, "native configuration rendering requires at least one explicit IPv4 interface address"
	}
	return profile, method, ""
}

// publishableInfrastructure keeps every validated device the native generator
// can render, including those whose intent the web plan does not model
// (routes, NAT checks happen in the generator), so publishing from the web
// never drops a device that `generate-all` would publish.
func (service *Service) publishableInfrastructure(infrastructure domain.Infrastructure) (domain.Infrastructure, []UnpublishedDevice) {
	checker, ok := service.generator.(ports.NativeSupportChecker)
	if !ok {
		return infrastructure, nil
	}
	published := infrastructure
	published.Sites = make([]domain.Site, 0, len(infrastructure.Sites))
	var refused []UnpublishedDevice
	for _, site := range infrastructure.Sites {
		kept := site
		kept.Devices = make([]domain.Device, 0, len(site.Devices))
		for _, device := range site.Devices {
			if err := checker.CheckNativeDevice(site, device); err != nil {
				refused = append(refused, UnpublishedDevice{Site: site.Name, Device: device.Name, Reason: err.Error()})
				continue
			}
			kept.Devices = append(kept.Devices, device)
		}
		published.Sites = append(published.Sites, kept)
	}
	return published, refused
}

func hasIPv4Interface(interfaces []domain.NetworkInterface) bool {
	for _, networkInterface := range interfaces {
		if strings.TrimSpace(networkInterface.IPv4Address) != "" {
			return true
		}
	}
	return false
}

func readGeneratedArtifact(root string, artifact protocol.Artifact) ([]byte, error) {
	site, _, found := strings.Cut(artifact.Path, "/")
	if !found || !protocol.ValidArtifactPath(site, artifact) || !protocol.IsSHA256(artifact.InputHash) || !protocol.IsSHA256(artifact.OutputHash) {
		return nil, fmt.Errorf("generator returned invalid artifact metadata for %q", artifact.Path)
	}
	file, err := safefs.OpenReadOnly(root, artifact.Path)
	if err != nil {
		return nil, fmt.Errorf("open generated artifact %q: %w", artifact.Path, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxGeneratedArtifactBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read generated artifact %q: %w", artifact.Path, err)
	}
	if len(data) > maxGeneratedArtifactBytes {
		return nil, fmt.Errorf("generated artifact %q exceeds %d bytes", artifact.Path, maxGeneratedArtifactBytes)
	}
	if protocol.SHA256(data) != artifact.OutputHash {
		return nil, fmt.Errorf("generated artifact %q failed its output hash", artifact.Path)
	}
	return data, nil
}

func generatedArtifactFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yml", ".yaml":
		return "yaml"
	case ".json":
		return "json"
	default:
		return strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}
}

func (service *Service) recordPlanGeneration(ctx context.Context, result PlanGenerationResult) (PlanGenerationResult, error) {
	if service.events == nil {
		return result, nil
	}
	if err := service.recordEvent(ctx, "plan.generation.result", "", "", "", map[string]any{
		"plan_id": result.Plan.ID, "generation_status": result.GenerationStatus,
		"execution_status": result.ExecutionStatus, "verification_status": result.VerificationStatus,
		"tasks": result.Tasks,
	}); err != nil {
		result.AuditStatus = "FAILED"
		return result, fmt.Errorf("record plan generation result: %w", err)
	}
	result.AuditStatus = "RECORDED"
	return result, nil
}
