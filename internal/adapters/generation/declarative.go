package generator

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"infraflow/internal/adapters/catalog"
	"infraflow/internal/adapters/config"
	"infraflow/internal/domain"
	"infraflow/pkg/protocol"

	"gopkg.in/yaml.v3"
)

//go:embed templates/vendors
var vendorTemplateFiles embed.FS

type deviceTemplateMetadata struct {
	ID                 string   `yaml:"id"`
	Version            string   `yaml:"version"`
	Manufacturer       string   `yaml:"manufacturer"`
	Model              string   `yaml:"model"`
	OSFamily           string   `yaml:"os_family"`
	OSVersions         []string `yaml:"os_versions"`
	ProvisioningMethod string   `yaml:"provisioning_method,omitempty"`
	TemplateFile       string   `yaml:"template_file,omitempty"`
	Features           []string `yaml:"features"`
	Variables          []string `yaml:"variables"`
	RequiredVariables  []string `yaml:"required_variables,omitempty"`
	Status             string   `yaml:"status"`
	Source             string   `yaml:"source"`
	SourceReferences   []string `yaml:"source_references,omitempty"`
}

type templateCandidate struct {
	Metadata deviceTemplateMetadata
	Path     string
	Content  []byte
	Hash     string
	Parsed   *template.Template
}

type TemplateCriterion struct {
	Name          string `json:"name"`
	Score         int    `json:"score"`
	Maximum       int    `json:"maximum"`
	Justification string `json:"justification"`
}

type TemplateEvaluation struct {
	TemplateID        string              `json:"template_id"`
	Score             int                 `json:"score,omitempty"`
	Admissible        bool                `json:"admissible"`
	Selected          bool                `json:"selected"`
	Criteria          []TemplateCriterion `json:"criteria,omitempty"`
	Incompatibilities []string            `json:"incompatibilities,omitempty"`
}

type DeviceGenerationReport struct {
	Device                 string                       `json:"device"`
	Manufacturer           string                       `json:"manufacturer"`
	Model                  string                       `json:"model"`
	OS                     string                       `json:"os"`
	OSVersion              string                       `json:"os_version,omitempty"`
	CatalogID              string                       `json:"catalog_id"`
	CatalogStatus          string                       `json:"catalog_status"`
	TemplateID             string                       `json:"template_id,omitempty"`
	TemplateVersion        string                       `json:"template_version,omitempty"`
	TemplateHash           string                       `json:"template_hash,omitempty"`
	SelectionReason        string                       `json:"selection_reason,omitempty"`
	Candidates             []TemplateEvaluation         `json:"candidates"`
	Variables              map[string]any               `json:"variables,omitempty"`
	ConfigPath             string                       `json:"config_path,omitempty"`
	ConfigHash             string                       `json:"config_hash,omitempty"`
	Status                 string                       `json:"status"`
	Limits                 []string                     `json:"limits,omitempty"`
	ConfigSyntaxValidation ConfigSyntaxValidationResult `json:"config_syntax_validation"`
}

type DeclarativeGenerationReport struct {
	APIVersion               string                       `json:"api_version"`
	Deployment               string                       `json:"deployment"`
	Site                     string                       `json:"site"`
	Validation               string                       `json:"validation"`
	CompatibilityScope       string                       `json:"compatibility_scope"`
	CommandSyntaxValidation  string                       `json:"command_syntax_validation"`
	ConfigSyntaxValidation   ConfigSyntaxValidationResult `json:"config_syntax_validation"`
	PhysicalDeviceValidation string                       `json:"physical_device_validation"`
	Status                   string                       `json:"status"`
	Devices                  []DeviceGenerationReport     `json:"devices"`
	Incompatibilities        []string                     `json:"incompatibilities,omitempty"`
}

type renderedInterface struct {
	Name    string
	Address string
	Netmask string
	Enabled bool
}

type deviceConfigTemplateData struct {
	TemplateID      string
	TemplateVersion string
	Interfaces      []renderedInterface
}

type declarativeArtifact struct {
	Path string
	Type string
	Data []byte
}

// GenerateDeclarativeConfig resolves local, versioned templates against the
// equipment catalog and writes only configurations supported by a registered
// template. It never executes the generated configuration on a device.
func GenerateDeclarativeConfig(input []byte, outputDirectory string) ([]Artifact, error) {
	if strings.TrimSpace(outputDirectory) == "" {
		return nil, fmt.Errorf("output directory is required")
	}
	pending, err := prepareDeclarativeArtifacts(input)
	if err != nil {
		return nil, err
	}
	return withOutputGenerationLock(outputDirectory, func(root string) ([]Artifact, error) {
		return publishDeclarativeArtifacts(root, input, pending)
	})
}

func prepareDeclarativeArtifacts(input []byte) ([]declarativeArtifact, error) {
	infrastructure, deployment, err := config.ParseNetworkDeployment(input)
	if err != nil {
		return nil, err
	}
	equipmentCatalog, err := catalog.Load()
	if err != nil {
		return nil, err
	}
	templates, err := loadDeviceTemplates()
	if err != nil {
		return nil, err
	}
	if err := validateCatalogTemplateReferences(equipmentCatalog, templates); err != nil {
		return nil, err
	}

	report := DeclarativeGenerationReport{
		APIVersion:              config.NetworkDeploymentAPIVersion,
		Deployment:              deployment.Metadata.Name,
		Site:                    deployment.Spec.Site,
		Validation:              "passed",
		CompatibilityScope:      "declared manufacturer/model/OS matched local template metadata; physical hardware and OS compatibility are not verified",
		CommandSyntaxValidation: "not_performed",
		ConfigSyntaxValidation: ConfigSyntaxValidationResult{
			Name:    configSyntaxValidationName,
			Status:  configSyntaxValidationUnavailable,
			Message: "No offline syntax parser is registered for generated vendor configurations; see per-device details.",
		},
		PhysicalDeviceValidation: "not_performed",
		Status:                   "rendered_unverified",
	}
	var pending []declarativeArtifact
	for index, declared := range deployment.Spec.Devices {
		device := infrastructure.Sites[0].Devices[index]
		record, ok := findEquipment(equipmentCatalog, device.Vendor, device.Model)
		if !ok {
			return nil, fmt.Errorf("spec.devices[%d]: no hardware catalog record for %s/%s; generation blocked", index, device.Vendor, device.Model)
		}
		evaluation, selected, err := selectDeviceTemplate(device, declared, record, templates)
		deviceReport := DeviceGenerationReport{
			Device: declared.Name, Manufacturer: declared.Vendor, Model: declared.Model,
			OS: declared.OS, OSVersion: declared.OSVersion,
			CatalogID: record.ID, CatalogStatus: record.ValidationStatus, Candidates: evaluation,
			Status:                 "blocked",
			ConfigSyntaxValidation: configSyntaxValidationFor(declared.Vendor, declared.OS, false),
		}
		if err != nil {
			deviceReport.Limits = []string{err.Error()}
			report.Devices = append(report.Devices, deviceReport)
			report.Incompatibilities = append(report.Incompatibilities, fmt.Sprintf("spec.devices[%d]: %v", index, err))
			return nil, fmt.Errorf("spec.devices[%d]: %w", index, err)
		}
		deviceReport.TemplateID = selected.Metadata.ID
		deviceReport.TemplateVersion = selected.Metadata.Version
		deviceReport.TemplateHash = selected.Hash
		deviceReport.SelectionReason = "highest deterministic score among admissible templates matching the declared manufacturer, model, OS family, and requested features; physical-device compatibility is unverified"
		deviceReport.Status = "rendered_unverified"
		deviceReport.ConfigSyntaxValidation = configSyntaxValidationFor(declared.Vendor, declared.OS, true)
		deviceReport.Limits = []string{
			"catalog record is partial; manufacturer-specific hardware interfaces and OS versions are not verified",
			"rendering does not prove command acceptance or device state",
		}

		templateData, variables, err := buildDeviceTemplateData(device, selected)
		if err != nil {
			return nil, fmt.Errorf("spec.devices[%d]: %w", index, err)
		}
		var configBuffer strings.Builder
		if err := selected.Parsed.Execute(&configBuffer, templateData); err != nil {
			return nil, fmt.Errorf("execute template %q: %w", selected.Metadata.ID, err)
		}
		configContents := []byte(configBuffer.String())
		if len(strings.TrimSpace(string(configContents))) == 0 {
			return nil, fmt.Errorf("template %q produced an empty configuration", selected.Metadata.ID)
		}
		configPath := filepath.ToSlash(filepath.Join(deployment.Spec.Site, declared.Name, "config.cfg"))
		deviceReport.Variables = variables
		deviceReport.ConfigPath = configPath
		deviceReport.ConfigHash = protocol.SHA256(configContents)
		deviceReport.Candidates = markSelected(evaluation, selected.Metadata.ID)
		report.Devices = append(report.Devices, deviceReport)
		pending = append(pending, declarativeArtifact{Path: configPath, Type: "device_configuration", Data: configContents})
	}
	reportBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode declarative generation report: %w", err)
	}
	reportBytes = append(reportBytes, '\n')
	pending = append(pending, declarativeArtifact{
		Path: filepath.ToSlash(filepath.Join(deployment.Spec.Site, "generation-report.json")),
		Type: "generation_report",
		Data: reportBytes,
	})

	return pending, nil
}

func publishDeclarativeArtifacts(outputDirectory string, input []byte, pending []declarativeArtifact) ([]Artifact, error) {
	root, err := filepath.Abs(outputDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve output directory: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect output directory: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, fmt.Errorf("output directory must be a real directory")
	}

	targets := make(map[string]declarativeArtifact, len(pending))
	var site, reportPath string
	for _, item := range pending {
		if !safePublicationPath(item.Path) {
			return nil, fmt.Errorf("unsafe generated artifact path %q", item.Path)
		}
		if len(item.Data) > protocol.MaxArtifactBytes {
			return nil, fmt.Errorf("generated artifact %q exceeds size limit", item.Path)
		}
		if _, exists := targets[item.Path]; exists {
			return nil, fmt.Errorf("duplicate generated artifact path %q", item.Path)
		}
		targets[item.Path] = item
		if item.Type == "generation_report" {
			if reportPath != "" || !strings.HasSuffix(item.Path, "/generation-report.json") {
				return nil, fmt.Errorf("declarative generation must publish exactly one site report")
			}
			reportPath = item.Path
			site = strings.TrimSuffix(item.Path, "/generation-report.json")
		}
	}
	if reportPath == "" || !protocol.ValidSiteName(site) {
		return nil, fmt.Errorf("declarative generation report path is missing or invalid")
	}

	managed := make(map[string]struct{}, len(pending))
	previousPaths, err := inspectDeclarativeGeneration(root, reportPath, site)
	if err != nil {
		return nil, fmt.Errorf("inspect previous declarative generation: %w", err)
	}
	for artifactPath := range previousPaths {
		managed[artifactPath] = struct{}{}
	}
	for artifactPath := range targets {
		exists, _, err := inspectPublicationTarget(root, artifactPath)
		if err != nil {
			return nil, fmt.Errorf("validate generated artifact target %q: %w", artifactPath, err)
		}
		if exists {
			if _, owned := managed[artifactPath]; !owned {
				return nil, fmt.Errorf("refusing to replace unmanaged file %q", artifactPath)
			}
		}
	}

	var stale []string
	files := make([]publicationFile, 0, len(pending))
	for artifactPath := range previousPaths {
		if _, remains := targets[artifactPath]; !remains && artifactPath != reportPath {
			stale = append(stale, artifactPath)
		}
	}
	for _, item := range pending {
		phase := publicationPhaseArtifacts
		if item.Path == reportPath {
			phase = publicationPhaseGlobalReport
		}
		files = append(files, publicationFile{
			pendingFile: pendingFile{path: item.Path, data: item.Data},
			phase:       phase,
		})
	}
	sort.Strings(stale)
	if err := publishSiteFiles(context.Background(), root, files, stale, managed, publicationHooks{}); err != nil {
		return nil, fmt.Errorf("publish declarative artifacts: %w", err)
	}

	artifacts := make([]Artifact, 0, len(pending))
	for _, item := range pending {
		artifacts = append(artifacts, Artifact{
			Type: item.Type, Path: item.Path, InputHash: protocol.SHA256(input),
			OutputHash: protocol.SHA256(item.Data),
		})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func inspectDeclarativeGeneration(root, reportPath, site string) (map[string]struct{}, error) {
	managed := make(map[string]struct{})
	exists, _, err := inspectPublicationTarget(root, reportPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return managed, nil
	}
	reportBytes, _, err := readManagedFile(root, reportPath)
	if err != nil {
		return nil, fmt.Errorf("read previous report: %w", err)
	}
	var report DeclarativeGenerationReport
	if err := decodeStrictJSON(reportBytes, &report); err != nil {
		return nil, fmt.Errorf("decode previous report: %w", err)
	}
	if report.APIVersion != config.NetworkDeploymentAPIVersion || report.Site != site ||
		report.Deployment == "" || report.Validation != "passed" || report.Status != "rendered_unverified" ||
		len(report.Devices) == 0 {
		return nil, fmt.Errorf("previous report does not identify a complete declarative generation")
	}
	seen := make(map[string]struct{}, len(report.Devices))
	for _, device := range report.Devices {
		if !protocol.ValidSiteName(device.Device) || device.ConfigPath != filepath.ToSlash(filepath.Join(site, device.Device, "config.cfg")) ||
			!protocol.IsSHA256(device.ConfigHash) || device.Status != "rendered_unverified" ||
			device.TemplateID == "" || device.TemplateVersion == "" || !protocol.IsSHA256(device.TemplateHash) {
			return nil, fmt.Errorf("previous report contains invalid device provenance")
		}
		if _, duplicate := seen[device.ConfigPath]; duplicate {
			return nil, fmt.Errorf("previous report contains duplicate device output %q", device.ConfigPath)
		}
		seen[device.ConfigPath] = struct{}{}
		data, _, err := readManagedFile(root, device.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("read previous configuration %q: %w", device.ConfigPath, err)
		}
		if protocol.SHA256(data) != device.ConfigHash {
			return nil, fmt.Errorf("previous configuration %q does not match its report hash", device.ConfigPath)
		}
		managed[device.ConfigPath] = struct{}{}
	}
	managed[reportPath] = struct{}{}
	return managed, nil
}

func loadDeviceTemplates() ([]templateCandidate, error) {
	var paths []string
	err := fs.WalkDir(vendorTemplateFiles, "templates/vendors", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == "metadata.yaml" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk local device templates: %w", err)
	}
	sort.Strings(paths)
	candidates := make([]templateCandidate, 0, len(paths))
	seenIDs := make(map[string]struct{}, len(paths))
	for _, metadataPath := range paths {
		metadataBytes, err := vendorTemplateFiles.ReadFile(metadataPath)
		if err != nil {
			return nil, fmt.Errorf("read template metadata %q: %w", metadataPath, err)
		}
		metadata, err := decodeDeviceTemplateMetadata(metadataPath, metadataBytes)
		if err != nil {
			return nil, err
		}
		normalizedID := strings.ToLower(strings.TrimSpace(metadata.ID))
		if _, exists := seenIDs[normalizedID]; exists {
			return nil, fmt.Errorf("duplicate local template id %q", metadata.ID)
		}
		seenIDs[normalizedID] = struct{}{}
		templateFile := metadata.TemplateFile
		if templateFile == "" {
			templateFile = "interfaces.cfg.tmpl"
		}
		templatePath := strings.TrimSuffix(metadataPath, "metadata.yaml") + templateFile
		content, err := vendorTemplateFiles.ReadFile(templatePath)
		if err != nil {
			return nil, fmt.Errorf("template %q references missing file %q: %w", metadata.ID, templatePath, err)
		}
		content = []byte(strings.ReplaceAll(string(content), "\r\n", "\n"))
		parsed, err := template.New(metadata.ID).Option("missingkey=error").Parse(string(content))
		if err != nil {
			return nil, fmt.Errorf("parse template %q: %w", metadata.ID, err)
		}
		hash := sha256.Sum256(content)
		candidates = append(candidates, templateCandidate{
			Metadata: metadata, Path: templatePath, Content: content,
			Hash: hex.EncodeToString(hash[:]), Parsed: parsed,
		})
	}
	return candidates, nil
}

func decodeDeviceTemplateMetadata(metadataPath string, data []byte) (deviceTemplateMetadata, error) {
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	var metadata deviceTemplateMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return deviceTemplateMetadata{}, fmt.Errorf("decode template metadata %q: %w", metadataPath, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return deviceTemplateMetadata{}, fmt.Errorf("template metadata %q must contain exactly one YAML document", metadataPath)
		}
		return deviceTemplateMetadata{}, fmt.Errorf("decode trailing template metadata %q: %w", metadataPath, err)
	}
	if strings.TrimSpace(metadata.ID) == "" || strings.TrimSpace(metadata.Version) == "" ||
		strings.TrimSpace(metadata.Manufacturer) == "" || strings.TrimSpace(metadata.Model) == "" ||
		strings.TrimSpace(metadata.OSFamily) == "" || strings.TrimSpace(metadata.Source) == "" {
		return deviceTemplateMetadata{}, fmt.Errorf("template metadata %q is incomplete", metadataPath)
	}
	switch metadata.Status {
	case "verified", "unknown", "unverified", "render-only":
	default:
		return deviceTemplateMetadata{}, fmt.Errorf("template metadata %q has unsupported status %q", metadataPath, metadata.Status)
	}
	return metadata, nil
}

func validateCatalogTemplateReferences(equipmentCatalog *catalog.Catalog, templates []templateCandidate) error {
	available := make(map[string]struct{}, len(templates))
	for _, item := range templates {
		available[item.Metadata.ID] = struct{}{}
	}
	for _, record := range equipmentCatalog.Search("", "") {
		for _, id := range record.TemplateIDs {
			if _, exists := available[id]; !exists {
				return fmt.Errorf("catalog record %q references missing template %q", record.ID, id)
			}
		}
	}
	return nil
}

func findEquipment(equipmentCatalog *catalog.Catalog, manufacturer, model string) (catalog.Record, bool) {
	matches := equipmentCatalog.Search(manufacturer, model)
	if len(matches) == 0 {
		return catalog.Record{}, false
	}
	return matches[0], true
}

func selectDeviceTemplate(device domain.Device, declared config.NetworkDeploymentDevice, record catalog.Record, templates []templateCandidate) ([]TemplateEvaluation, templateCandidate, error) {
	requestedFeatures := []string{"interfaces.ipv4_static", "interfaces.admin_state"}
	var evaluations []TemplateEvaluation
	var eligible []templateCandidate
	for _, candidate := range templates {
		result := TemplateEvaluation{TemplateID: candidate.Metadata.ID}
		if !containsFold(record.TemplateIDs, candidate.Metadata.ID) {
			result.Incompatibilities = append(result.Incompatibilities, "template is not listed for this catalog equipment")
		}
		if !strings.EqualFold(device.Vendor, candidate.Metadata.Manufacturer) ||
			!strings.EqualFold(device.Model, candidate.Metadata.Model) {
			result.Incompatibilities = append(result.Incompatibilities, "manufacturer/model do not exactly match template metadata")
		}
		if !strings.EqualFold(declared.OS, candidate.Metadata.OSFamily) {
			result.Incompatibilities = append(result.Incompatibilities, fmt.Sprintf("OS family %q does not match required %q", declared.OS, candidate.Metadata.OSFamily))
		}
		if declared.OSVersion != "" && !containsFold(candidate.Metadata.OSVersions, declared.OSVersion) {
			result.Incompatibilities = append(result.Incompatibilities, fmt.Sprintf("OS version %q is not explicitly supported by template metadata", declared.OSVersion))
		}
		if !containsOS(record.OperatingSystems, declared.OS) {
			result.Incompatibilities = append(result.Incompatibilities, fmt.Sprintf("catalog has no OS family record for %q", declared.OS))
		}
		missingFeatures := missingStrings(requestedFeatures, candidate.Metadata.Features)
		if len(missingFeatures) > 0 {
			result.Incompatibilities = append(result.Incompatibilities, "template does not support requested features: "+strings.Join(missingFeatures, ", "))
		}
		missingVariables := missingStrings(candidate.Metadata.Variables, []string{"interfaces.name", "interfaces.ipv4", "interfaces.enabled"})
		if len(missingVariables) > 0 {
			result.Incompatibilities = append(result.Incompatibilities, "required variables are unavailable: "+strings.Join(missingVariables, ", "))
		}
		if candidate.Metadata.Status != "render-only" {
			result.Incompatibilities = append(result.Incompatibilities, fmt.Sprintf("template status %q is not approved for rendering", candidate.Metadata.Status))
		}

		if len(result.Incompatibilities) == 0 {
			result.Admissible = true
			osScore, osReason := 20, "exact OS family match; catalog OS compatibility is unknown and no OS version was supplied"
			versionScore := 0
			if declared.OSVersion != "" {
				versionScore = 5
				osReason = fmt.Sprintf("exact OS family and version match for %s", declared.OSVersion)
			}
			result.Criteria = []TemplateCriterion{
				{Name: "manufacturer_model", Score: 30, Maximum: 30, Justification: "catalog record and template metadata exactly match the declared manufacturer/model"},
				{Name: "os_family_version", Score: osScore + versionScore, Maximum: 25, Justification: osReason + "; supported OS-version metadata is absent"},
				{Name: "requested_features", Score: 25, Maximum: 25, Justification: "all requested features are implemented by the selected template"},
				{Name: "variables", Score: 10, Maximum: 10, Justification: "all required template variables are present in validated YAML"},
				{Name: "template_validity", Score: 8, Maximum: 10, Justification: "embedded template parses and its metadata is complete; status is render-only, not hardware-verified"},
			}
			for _, criterion := range result.Criteria {
				result.Score += criterion.Score
			}
			eligible = append(eligible, candidate)
		}
		evaluations = append(evaluations, result)
	}
	if len(eligible) == 0 {
		if len(evaluations) == 0 {
			return evaluations, templateCandidate{}, fmt.Errorf("no local device templates are registered")
		}
		var explanations []string
		for _, evaluation := range evaluations {
			if len(evaluation.Incompatibilities) > 0 {
				explanations = append(explanations, evaluation.TemplateID+": "+strings.Join(evaluation.Incompatibilities, "; "))
			}
		}
		return evaluations, templateCandidate{}, fmt.Errorf("no compatible template; generation blocked (%s)", strings.Join(explanations, " | "))
	}
	sort.Slice(eligible, func(i, j int) bool {
		left, right := scoreFor(evaluations, eligible[i].Metadata.ID), scoreFor(evaluations, eligible[j].Metadata.ID)
		if left != right {
			return left > right
		}
		return eligible[i].Metadata.ID < eligible[j].Metadata.ID
	})
	return evaluations, eligible[0], nil
}

func buildDeviceTemplateData(device domain.Device, selected templateCandidate) (deviceConfigTemplateData, map[string]any, error) {
	result := deviceConfigTemplateData{TemplateID: selected.Metadata.ID, TemplateVersion: selected.Metadata.Version}
	variableInterfaces := make([]map[string]any, 0, len(device.Network.Interfaces))
	for _, iface := range device.Network.Interfaces {
		address, network, err := net.ParseCIDR(iface.IPv4Address)
		if err != nil || address.To4() == nil {
			return deviceConfigTemplateData{}, nil, fmt.Errorf("interface %q has invalid IPv4 prefix", iface.Name)
		}
		ones, _ := network.Mask.Size()
		result.Interfaces = append(result.Interfaces, renderedInterface{
			Name: iface.Name, Address: address.To4().String(),
			Netmask: net.IP(network.Mask).String(), Enabled: iface.Enabled != nil && *iface.Enabled,
		})
		variableInterfaces = append(variableInterfaces, map[string]any{
			"name": iface.Name, "ipv4": address.To4().String() + "/" + fmt.Sprint(ones), "enabled": *iface.Enabled,
		})
	}
	sort.Slice(result.Interfaces, func(i, j int) bool { return result.Interfaces[i].Name < result.Interfaces[j].Name })
	return result, map[string]any{"interfaces": variableInterfaces}, nil
}

func scoreFor(evaluations []TemplateEvaluation, id string) int {
	for _, evaluation := range evaluations {
		if evaluation.TemplateID == id {
			return evaluation.Score
		}
	}
	return -1
}

func markSelected(evaluations []TemplateEvaluation, id string) []TemplateEvaluation {
	for index := range evaluations {
		evaluations[index].Selected = evaluations[index].TemplateID == id
	}
	return evaluations
}

func containsOS(operatingSystems []catalog.OS, name string) bool {
	for _, item := range operatingSystems {
		if strings.EqualFold(item.Name, name) {
			return true
		}
	}
	return false
}

func containsFold(values []string, value string) bool {
	for _, candidate := range values {
		if strings.EqualFold(candidate, value) {
			return true
		}
	}
	return false
}

func missingStrings(required, available []string) []string {
	var missing []string
	for _, item := range required {
		if !containsFold(available, item) {
			missing = append(missing, item)
		}
	}
	return missing
}
