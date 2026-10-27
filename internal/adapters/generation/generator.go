package generator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"infraflow/internal/adapters/config"
	"infraflow/internal/domain"
	"infraflow/internal/infrastructure/safefs"
	"infraflow/pkg/protocol"
)

const Version = "0.1.0"
const TemplateVersion = "1"
const maxParallelSiteGenerations = 4

type Artifact = protocol.Artifact

type Manifest struct {
	GeneratorVersion string     `json:"generator_version"`
	TemplateVersion  string     `json:"template_version"`
	InputHash        string     `json:"input_hash"`
	SiteID           string     `json:"site_id,omitempty"`
	Status           string     `json:"status,omitempty"`
	Artifacts        []Artifact `json:"artifacts"`
}

type GlobalManifest struct {
	SchemaVersion    int                              `json:"schema_version"`
	GeneratorVersion string                           `json:"generator_version"`
	TemplateVersion  string                           `json:"template_version"`
	InputHash        string                           `json:"input_hash"`
	Status           string                           `json:"status"`
	GenerationReport string                           `json:"generation_report"`
	Sites            []protocol.SiteGenerationSummary `json:"sites"`
}

type inventory struct {
	Site    string          `json:"site"`
	Devices []domain.Device `json:"devices"`
}

type topology struct {
	Site  string         `json:"site"`
	Nodes []topologyNode `json:"nodes"`
	Edges []topologyEdge `json:"edges"`
}

type topologyNode struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Role         string `json:"role,omitempty"`
	Vendor       string `json:"vendor,omitempty"`
	Model        string `json:"model,omitempty"`
	ManagementIP string `json:"management_ip,omitempty"`
}

type topologyEdge struct {
	ID      string       `json:"id,omitempty"`
	A       topologyPort `json:"a"`
	B       topologyPort `json:"b"`
	Network string       `json:"network"`
}

type topologyPort struct {
	Device    string `json:"device"`
	Interface string `json:"interface"`
}

type pendingFile struct {
	path string
	data []byte
}

type siteGenerationResult struct {
	site        domain.Site
	stagingRoot string
	artifacts   []Artifact
	err         error
}

type ansibleInventory struct {
	All ansibleInventoryAll `json:"all"`
}

type ansibleInventoryAll struct {
	Children ansibleInventoryChildren `json:"children"`
}

type ansibleInventoryChildren struct {
	Network ansibleGroup `json:"network"`
}

type ansibleGroup struct {
	Hosts map[string]ansibleHost `json:"hosts"`
}

type ansibleHost struct {
	AnsibleHost        string `json:"ansible_host,omitempty"`
	InfraflowVendor    string `json:"infraflow_vendor,omitempty"`
	InfraflowFamily    string `json:"infraflow_family,omitempty"`
	InfraflowModel     string `json:"infraflow_model,omitempty"`
	InfraflowOSVersion string `json:"infraflow_os_version,omitempty"`
	InfraflowRole      string `json:"infraflow_role,omitempty"`
}

type ansiblePlay struct {
	Name        string            `json:"name"`
	Hosts       string            `json:"hosts"`
	GatherFacts bool              `json:"gather_facts"`
	Tasks       []ansiblePlayTask `json:"tasks"`
}

type ansiblePlayTask struct {
	Name  string           `json:"name"`
	Debug ansibleDebugTask `json:"ansible.builtin.debug"`
}

type ansibleDebugTask struct {
	Msg string `json:"msg"`
}

type terraformPendingFile struct {
	path string
	data []byte
}

type terraformExpression string

// GenerateTerraform emits a data-only Terraform configuration. It represents
// the validated desired state and topology without selecting a provider or
// applying any infrastructure changes.
func GenerateTerraform(infrastructure domain.Infrastructure, outputDirectory string) ([]Artifact, error) {
	if problems := config.Validate(infrastructure); len(problems) > 0 {
		return nil, problems
	}
	if strings.TrimSpace(outputDirectory) == "" {
		return nil, fmt.Errorf("output directory is required")
	}

	canonical := canonicalize(infrastructure)
	canonicalInput, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("encode normalized input: %w", err)
	}
	inputHash := protocol.SHA256(canonicalInput)
	files := make([]terraformPendingFile, 0, len(canonical.Sites)*8)
	artifacts := make([]Artifact, 0, len(canonical.Sites)*8)
	for _, site := range canonical.Sites {
		localBytes, err := marshalTerraform(terraformLocals(site, inputHash))
		if err != nil {
			return nil, err
		}
		contents := map[string][]byte{
			"versions.tf":  []byte("terraform {\n  required_version = \">= 1.4.0\"\n}\n"),
			"providers.tf": []byte("# This generated configuration intentionally has no external provider.\n"),
			"variables.tf": []byte(terraformVariables),
			"locals.tf":    localBytes,
			"main.tf":      []byte(terraformMain),
			"outputs.tf":   []byte(terraformOutputs),
			"terraform.tfvars.example": []byte("# Copy this file to terraform.tfvars only when a local override is needed.\n" +
				"environment = \"lab\"\n"),
		}

		fileNames := []string{"versions.tf", "providers.tf", "variables.tf", "locals.tf", "main.tf", "outputs.tf", "terraform.tfvars.example"}
		for _, fileName := range fileNames {
			data := contents[fileName]
			relativePath := filepath.ToSlash(filepath.Join(site.Name, "terraform", fileName))
			artifact := Artifact{Type: terraformArtifactType(fileName), Path: relativePath, InputHash: inputHash, OutputHash: protocol.SHA256(data)}
			artifacts = append(artifacts, artifact)
			files = append(files, terraformPendingFile{path: relativePath, data: data})
		}
	}

	root, err := filepath.Abs(outputDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve output directory: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	for _, file := range files {
		if err := safefs.AtomicWrite(root, file.path, file.data, 0o644); err != nil {
			return nil, fmt.Errorf("write Terraform artifact %q: %w", file.path, err)
		}
	}
	return artifacts, nil
}

const terraformVariables = `variable "environment" {
  type        = string
  description = "Logical environment label for this generated declaration."
  default     = "generated"

  validation {
    condition     = trimspace(var.environment) != ""
    error_message = "environment must not be empty."
  }
}
`

const terraformMain = `# This configuration is intentionally data-only.
# It does not declare provider resources and does not provision devices.
locals {
  site_names = sort(keys(local.infrastructure.sites))
}
`

const terraformOutputs = `output "infraflow_input_hash" {
  description = "SHA-256 of the normalized InfraFlow input."
  value       = local.infrastructure.input_hash
}

output "infraflow_site_names" {
  description = "Sites present in the normalized InfraFlow input."
  value       = local.site_names
}

output "infraflow_declaration" {
  description = "The normalized desired state represented as Terraform data."
  value       = local.infrastructure
}
`

func terraformArtifactType(fileName string) string {
	switch fileName {
	case "versions.tf":
		return "terraform_versions"
	case "providers.tf":
		return "terraform_providers"
	case "variables.tf":
		return "terraform_variables"
	case "locals.tf":
		return "terraform_locals"
	case "main.tf":
		return "terraform_main"
	case "outputs.tf":
		return "terraform_outputs"
	case "terraform.tfvars.example":
		return "terraform_tfvars_example"
	default:
		return ""
	}
}

func terraformLocals(site domain.Site, inputHash string) map[string]any {
	devices := make([]any, 0, len(site.Devices))
	for _, device := range site.Devices {
		devices = append(devices, map[string]any{
			"id": device.ID, "name": device.Name, "role": device.Role, "vendor": device.Vendor,
			"family": device.Family, "model": device.Model,
			"identity":     map[string]any{"serial": device.Identity.Serial, "macs": append([]string(nil), device.Identity.MACs...)},
			"management":   map[string]any{"ipv4": device.Management.IPv4},
			"provisioning": map[string]any{"method": device.Provisioning.Method},
		})
	}
	links := make([]any, 0, len(site.Links))
	for _, link := range site.Links {
		links = append(links, map[string]any{
			"id": link.ID, "a": link.A, "b": link.B, "network": link.Network,
		})
	}
	services := make(map[string]any, len(site.Services))
	for name, enabled := range site.Services {
		services[name] = enabled
	}
	sites := map[string]any{
		site.Name: map[string]any{
			"id": site.ID, "name": site.Name, "mode": site.Mode,
			"bootstrap": map[string]any{"network": site.Bootstrap.Network, "gateway": site.Bootstrap.Gateway},
			"services":  services, "devices": devices, "links": links,
		},
	}
	return map[string]any{
		"input_hash": inputHash, "environment": terraformExpression("var.environment"), "sites": sites,
	}
}

func marshalTerraform(locals map[string]any) ([]byte, error) {
	value, err := hclValue(locals, 2)
	if err != nil {
		return nil, fmt.Errorf("encode Terraform locals: %w", err)
	}
	return []byte("locals {\n  infrastructure = " + value + "\n}\n"), nil
}

func hclValue(value any, indent int) (string, error) {
	indentText := strings.Repeat(" ", indent)
	switch typed := value.(type) {
	case terraformExpression:
		return string(typed), nil
	case string:
		encoded, err := json.Marshal(typed)
		return string(encoded), err
	case bool:
		if typed {
			return "true", nil
		}
		return "false", nil
	case nil:
		return "null", nil
	case []string:
		items := make([]any, len(typed))
		for index := range typed {
			items[index] = typed[index]
		}
		return hclValue(items, indent)
	case []any:
		if len(typed) == 0 {
			return "[]", nil
		}
		var builder strings.Builder
		builder.WriteString("[\n")
		for index, item := range typed {
			encoded, err := hclValue(item, indent+2)
			if err != nil {
				return "", err
			}
			builder.WriteString(strings.Repeat(" ", indent+2))
			builder.WriteString(encoded)
			if index+1 < len(typed) {
				builder.WriteString(",")
			}
			builder.WriteString("\n")
		}
		builder.WriteString(indentText)
		builder.WriteString("]")
		return builder.String(), nil
	case map[string]any:
		if len(typed) == 0 {
			return "{}", nil
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		encodedKeys := make(map[string][]byte, len(keys))
		keyWidths := make(map[string]int, len(keys))
		for _, key := range keys {
			keyJSON, err := json.Marshal(key)
			if err != nil {
				return "", err
			}
			encodedKeys[key] = keyJSON
		}
		for start := 0; start < len(keys); {
			if isHCLCollection(typed[keys[start]]) {
				keyWidths[keys[start]] = len(encodedKeys[keys[start]])
				start++
				continue
			}
			end := start
			maxKeyWidth := 0
			for end < len(keys) && !isHCLCollection(typed[keys[end]]) {
				if width := len(encodedKeys[keys[end]]); width > maxKeyWidth {
					maxKeyWidth = width
				}
				end++
			}
			for index := start; index < end; index++ {
				keyWidths[keys[index]] = maxKeyWidth
			}
			start = end
		}
		var builder strings.Builder
		builder.WriteString("{\n")
		for _, key := range keys {
			encoded, err := hclValue(typed[key], indent+2)
			if err != nil {
				return "", err
			}
			builder.WriteString(strings.Repeat(" ", indent+2))
			builder.Write(encodedKeys[key])
			builder.WriteString(strings.Repeat(" ", keyWidths[key]-len(encodedKeys[key])))
			builder.WriteString(" = ")
			builder.WriteString(encoded)
			builder.WriteString("\n")
		}
		builder.WriteString(indentText)
		builder.WriteString("}")
		return builder.String(), nil
	default:
		return "", fmt.Errorf("unsupported Terraform value %T", value)
	}
}

func isHCLCollection(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		return len(typed) > 0
	case []any:
		return len(typed) > 0
	case []string:
		return len(typed) > 0
	default:
		return false
	}
}

// GenerateAnsible emits a generic, read-only Ansible inventory and inspection
// playbook. It does not select vendor modules or execute Ansible.
func GenerateAnsible(infrastructure domain.Infrastructure, outputDirectory string) ([]Artifact, error) {
	if problems := config.Validate(infrastructure); len(problems) > 0 {
		return nil, problems
	}
	if strings.TrimSpace(outputDirectory) == "" {
		return nil, fmt.Errorf("output directory is required")
	}
	canonical := canonicalize(infrastructure)
	canonicalInput, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("encode normalized input: %w", err)
	}
	inputHash := protocol.SHA256(canonicalInput)
	files := make([]pendingFile, 0, len(canonical.Sites)*6)
	artifacts := make([]Artifact, 0, len(canonical.Sites)*6)
	for _, site := range canonical.Sites {
		hosts := make(map[string]ansibleHost, len(site.Devices))
		for _, device := range site.Devices {
			host := ansibleHost{
				AnsibleHost: device.Management.IPv4, InfraflowVendor: device.Vendor,
				InfraflowFamily: device.Family, InfraflowModel: device.Model,
				InfraflowOSVersion: device.OSVersion, InfraflowRole: device.Role,
			}
			hosts[device.Name] = host
		}
		inventoryBytes, err := marshal(ansibleInventory{All: ansibleInventoryAll{Children: ansibleInventoryChildren{Network: ansibleGroup{Hosts: hosts}}}})
		if err != nil {
			return nil, err
		}
		playbookBytes, err := marshal([]ansiblePlay{{
			Name: "Inspect declared InfraFlow devices", Hosts: "network", GatherFacts: false,
			Tasks: []ansiblePlayTask{{
				Name:  "Display declared device metadata",
				Debug: ansibleDebugTask{Msg: "device={{ inventory_hostname }} vendor={{ hostvars[inventory_hostname].infraflow_vendor | default('unknown') }} model={{ hostvars[inventory_hostname].infraflow_model | default('unknown') }} os_version={{ hostvars[inventory_hostname].infraflow_os_version | default('unknown') }}"},
			}},
		}})
		if err != nil {
			return nil, err
		}
		inventoryPath := filepath.ToSlash(filepath.Join(site.Name, "ansible", "inventory.yml"))
		playbookPath := filepath.ToSlash(filepath.Join(site.Name, "ansible", "site.yml"))
		inventoryArtifact := Artifact{Type: "ansible_inventory", Path: inventoryPath, InputHash: inputHash, OutputHash: protocol.SHA256(inventoryBytes)}
		playbookArtifact := Artifact{Type: "ansible_inspection_playbook", Path: playbookPath, InputHash: inputHash, OutputHash: protocol.SHA256(playbookBytes)}
		artifacts = append(artifacts, inventoryArtifact, playbookArtifact)
		files = append(files, pendingFile{path: inventoryPath, data: inventoryBytes}, pendingFile{path: playbookPath, data: playbookBytes})
	}
	root, err := filepath.Abs(outputDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve output directory: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	for _, file := range files {
		if err := safefs.AtomicWrite(root, file.path, file.data, 0o644); err != nil {
			return nil, fmt.Errorf("write Ansible artifact %q: %w", file.path, err)
		}
	}
	return artifacts, nil
}

func Generate(infrastructure domain.Infrastructure, outputDirectory string) ([]Artifact, error) {
	if problems := config.Validate(infrastructure); len(problems) > 0 {
		return nil, problems
	}
	if strings.TrimSpace(outputDirectory) == "" {
		return nil, fmt.Errorf("output directory is required")
	}

	canonical := canonicalize(infrastructure)
	canonicalInput, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("encode normalized input: %w", err)
	}
	inputHash := protocol.SHA256(canonicalInput)
	files := make([]pendingFile, 0, len(canonical.Sites)*3)
	allArtifacts := make([]Artifact, 0, len(canonical.Sites)*2)

	for _, site := range canonical.Sites {
		inventoryBytes, err := marshal(inventory{Site: site.Name, Devices: site.Devices})
		if err != nil {
			return nil, err
		}
		topologyBytes, err := marshal(buildTopology(site))
		if err != nil {
			return nil, err
		}

		inventoryPath := filepath.ToSlash(filepath.Join(site.Name, "inventory.json"))
		topologyPath := filepath.ToSlash(filepath.Join(site.Name, "topology.json"))
		siteArtifacts := []Artifact{
			{Type: "inventory", Path: inventoryPath, InputHash: inputHash, OutputHash: protocol.SHA256(inventoryBytes)},
			{Type: "topology", Path: topologyPath, InputHash: inputHash, OutputHash: protocol.SHA256(topologyBytes)},
		}
		manifestBytes, err := marshal(Manifest{
			GeneratorVersion: Version,
			TemplateVersion:  TemplateVersion,
			InputHash:        inputHash,
			Artifacts:        siteArtifacts,
		})
		if err != nil {
			return nil, err
		}

		files = append(files,
			pendingFile{path: inventoryPath, data: inventoryBytes},
			pendingFile{path: topologyPath, data: topologyBytes},
			pendingFile{path: filepath.ToSlash(filepath.Join(site.Name, "manifest.json")), data: manifestBytes},
		)
		allArtifacts = append(allArtifacts, siteArtifacts...)
	}

	root, err := filepath.Abs(outputDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve output directory: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	for _, site := range canonical.Sites {
		sitePath := filepath.Join(root, site.Name)
		if info, err := os.Lstat(sitePath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("refusing to write through symlink %q", sitePath)
		} else if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect output path %q: %w", sitePath, err)
		}
		if err := os.MkdirAll(sitePath, 0o755); err != nil {
			return nil, fmt.Errorf("create site output directory: %w", err)
		}
	}
	for _, file := range files {
		if err := safefs.AtomicWrite(root, file.path, file.data, 0o644); err != nil {
			return nil, fmt.Errorf("write artifact %q: %w", file.path, err)
		}
	}
	return allArtifacts, nil
}

func GenerateAll(infrastructure domain.Infrastructure, outputDirectory string) ([]Artifact, error) {
	return GenerateAllContext(context.Background(), infrastructure, outputDirectory)
}

func GenerateAllContext(ctx context.Context, infrastructure domain.Infrastructure, outputDirectory string) ([]Artifact, error) {
	if ctx == nil {
		return nil, fmt.Errorf("generation context is required")
	}
	if len(infrastructure.Sites) == 0 {
		return nil, fmt.Errorf("at least one site is required")
	}
	if strings.TrimSpace(outputDirectory) == "" {
		return nil, fmt.Errorf("output directory is required")
	}
	if err := validateSiteIdentities(infrastructure.Sites); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(outputDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve output directory: %w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, fmt.Errorf("output directory must be a real directory")
	}
	if err := removeGlobalManifest(root); err != nil {
		return nil, err
	}
	if err := removeGeneratedReport(root, "generation-report.json"); err != nil {
		return nil, err
	}
	for _, site := range infrastructure.Sites {
		if err := removeCatalogManifest(root, site.Name); err != nil {
			return nil, err
		}
	}
	staging, err := os.MkdirTemp("", "infraflow-generate-all-")
	if err != nil {
		return nil, fmt.Errorf("create generation staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	canonicalInput, err := json.Marshal(canonicalize(infrastructure))
	if err != nil {
		return nil, fmt.Errorf("encode normalized input: %w", err)
	}
	globalInputHash := protocol.SHA256(canonicalInput)
	results := runSitesInParallel(ctx, infrastructure.Sites, maxParallelSiteGenerations, func(site domain.Site) siteGenerationResult {
		siteRoot, err := os.MkdirTemp(staging, site.Name+"-")
		if err != nil {
			return siteGenerationResult{site: site, err: fmt.Errorf("create staging directory: %w", err)}
		}
		siteInfrastructure := domain.Infrastructure{
			CapabilityRegistry: infrastructure.CapabilityRegistry,
			TemplateRegistry:   infrastructure.TemplateRegistry,
			Sites:              []domain.Site{site},
		}
		if problems := config.Validate(siteInfrastructure); len(problems) > 0 {
			return siteGenerationResult{site: site, stagingRoot: siteRoot, err: problems}
		}
		var artifacts []Artifact
		for _, generate := range []struct {
			name string
			run  func(domain.Infrastructure, string) ([]Artifact, error)
		}{
			{"inventory and topology", Generate},
			{"generic Ansible", GenerateAnsible},
			{"vendor Ansible", GenerateVendorAnsible},
			{"Terraform data", GenerateTerraform},
			{"bootstrap data", GenerateBootstrap},
		} {
			if err := ctx.Err(); err != nil {
				return siteGenerationResult{site: site, stagingRoot: siteRoot, err: err}
			}
			generated, err := generate.run(siteInfrastructure, siteRoot)
			if err != nil {
				return siteGenerationResult{site: site, stagingRoot: siteRoot, err: fmt.Errorf("%s: %w", generate.name, err)}
			}
			artifacts = append(artifacts, generated...)
		}
		return siteGenerationResult{site: site, stagingRoot: siteRoot, artifacts: artifacts}
	})
	return publishSiteGenerationResults(ctx, root, results, globalInputHash)
}

func runSitesInParallel(ctx context.Context, sites []domain.Site, workerLimit int, work func(domain.Site) siteGenerationResult) []siteGenerationResult {
	if workerLimit < 1 {
		workerLimit = 1
	}
	if workerLimit > len(sites) {
		workerLimit = len(sites)
	}
	results := make([]siteGenerationResult, len(sites))
	jobs := make(chan int)
	var workers sync.WaitGroup
	workers.Add(workerLimit)
	for worker := 0; worker < workerLimit; worker++ {
		go func() {
			defer workers.Done()
			for index := range jobs {
				if err := ctx.Err(); err != nil {
					results[index] = siteGenerationResult{site: sites[index], err: err}
					continue
				}
				results[index] = work(sites[index])
			}
		}()
	}
	for index := range sites {
		if err := ctx.Err(); err != nil {
			results[index] = siteGenerationResult{site: sites[index], err: err}
			continue
		}
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return results
}

func validateSiteIdentities(sites []domain.Site) error {
	seenNames := make(map[string]struct{}, len(sites))
	seenIDs := make(map[string]struct{}, len(sites))
	for _, site := range sites {
		if !protocol.ValidSiteName(site.Name) {
			return fmt.Errorf("invalid site name %q", site.Name)
		}
		if _, exists := seenNames[site.Name]; exists {
			return fmt.Errorf("duplicate site name %q", site.Name)
		}
		seenNames[site.Name] = struct{}{}
		siteID := stableSiteID(site)
		if !protocol.ValidSiteName(siteID) || len(siteID) > 128 {
			return fmt.Errorf("invalid site id %q for site %q", siteID, site.Name)
		}
		if _, exists := seenIDs[siteID]; exists {
			return fmt.Errorf("duplicate site id %q", siteID)
		}
		seenIDs[siteID] = struct{}{}
	}
	return nil
}

func stableSiteID(site domain.Site) string {
	if strings.TrimSpace(site.ID) != "" {
		return strings.TrimSpace(site.ID)
	}
	return site.Name
}

func publishSiteGenerationResults(ctx context.Context, root string, results []siteGenerationResult, globalInputHash string) ([]Artifact, error) {
	summaries := make([]protocol.SiteGenerationSummary, 0, len(results))
	allArtifacts := make([]Artifact, 0)
	var operationErrors []error
	for _, result := range results {
		summary := protocol.SiteGenerationSummary{
			SiteID: stableSiteID(result.site), SiteName: result.site.Name,
			Status:               "succeeded",
			SiteManifestPath:     filepath.ToSlash(filepath.Join(result.site.Name, "site-manifest.json")),
			GenerationReportPath: filepath.ToSlash(filepath.Join(result.site.Name, "generation-report.json")),
		}
		var baseArtifacts []Artifact
		var artifactFiles []pendingFile
		if result.err == nil {
			baseArtifacts, artifactFiles, result.err = readAndValidateStagedArtifacts(result.stagingRoot, result.artifacts)
		}
		if result.err != nil {
			summary.Status = "failed"
			summary.Errors = []string{result.err.Error()}
			operationErrors = append(operationErrors, fmt.Errorf("site %q: %w", result.site.Name, result.err))
		} else {
			summary.ArtifactCount = len(baseArtifacts) + 2
		}
		if ctxErr := ctx.Err(); ctxErr != nil && summary.Status == "succeeded" {
			summary.Status = "failed"
			summary.Errors = []string{ctxErr.Error()}
			operationErrors = append(operationErrors, fmt.Errorf("site %q: %w", result.site.Name, ctxErr))
		}

		inputBytes, err := json.Marshal(canonicalize(domain.Infrastructure{Sites: []domain.Site{result.site}}))
		if err != nil {
			return allArtifacts, fmt.Errorf("encode input for site %q: %w", result.site.Name, err)
		}
		siteInputHash := protocol.SHA256(inputBytes)
		siteReportBytes, reportArtifact, err := marshalSiteGenerationReport(summary, siteInputHash)
		if err != nil {
			return allArtifacts, fmt.Errorf("encode generation report for site %q: %w", result.site.Name, err)
		}
		details := []protocol.SiteArtifact(nil)
		if summary.Status == "succeeded" {
			details = artifactDetails(baseArtifacts, artifactFiles)
			details = append(details, protocol.SiteArtifact{
				Type: reportArtifact.Type, Path: reportArtifact.Path,
				SizeBytes: int64(len(siteReportBytes)), SHA256: reportArtifact.OutputHash,
			})
		}
		siteManifest := protocol.SiteManifest{
			SchemaVersion: 1, SiteID: summary.SiteID, SiteName: result.site.Name,
			Status: summary.Status, GeneratorVersion: Version, TemplateVersion: TemplateVersion,
			InputHash: siteInputHash, Artifacts: details,
			Capabilities: generationCapabilities(result.site, baseArtifacts),
			Errors:       append([]string(nil), summary.Errors...),
		}
		siteManifestBytes, err := marshal(siteManifest)
		if err != nil {
			return allArtifacts, fmt.Errorf("encode site manifest for %q: %w", result.site.Name, err)
		}
		summary.SiteManifestSHA256 = protocol.SHA256(siteManifestBytes)

		if summary.Status == "succeeded" {
			publishFiles := append([]pendingFile(nil), artifactFiles...)
			publishFiles = append(publishFiles,
				pendingFile{path: reportArtifact.Path, data: siteReportBytes},
				pendingFile{path: summary.SiteManifestPath, data: siteManifestBytes},
			)
			if err := publishSiteFiles(root, publishFiles); err != nil {
				summary.Status = "failed"
				summary.Errors = []string{err.Error()}
				operationErrors = append(operationErrors, fmt.Errorf("site %q: %w", result.site.Name, err))
			} else {
				catalogArtifacts := append([]Artifact(nil), baseArtifacts...)
				catalogArtifacts = append(catalogArtifacts, reportArtifact, Artifact{
					Type: "site_manifest", Path: summary.SiteManifestPath,
					InputHash: siteInputHash, OutputHash: summary.SiteManifestSHA256,
				})
				sort.Slice(catalogArtifacts, func(i, j int) bool { return catalogArtifacts[i].Path < catalogArtifacts[j].Path })
				catalogBytes, err := marshal(Manifest{
					GeneratorVersion: Version, TemplateVersion: TemplateVersion,
					InputHash: siteInputHash, SiteID: summary.SiteID,
					Status: "succeeded", Artifacts: catalogArtifacts,
				})
				if err == nil {
					err = safefs.AtomicWrite(
						root, filepath.ToSlash(filepath.Join(result.site.Name, "manifest.json")), catalogBytes, 0o644,
					)
				}
				if err != nil {
					summary.Status = "failed"
					summary.Errors = []string{fmt.Errorf("publish site catalog manifest: %w", err).Error()}
					operationErrors = append(operationErrors, fmt.Errorf("site %q: %w", result.site.Name, err))
					_ = removeCatalogManifest(root, result.site.Name)
				} else {
					allArtifacts = append(allArtifacts, catalogArtifacts...)
				}
			}
		}
		if summary.Status == "failed" {
			summary.ArtifactCount = 0
			if len(summary.Errors) == 0 {
				summary.Errors = []string{"site generation failed"}
			}
			failureReportBytes, failureReportArtifact, err := marshalSiteGenerationReport(summary, siteInputHash)
			if err != nil {
				return allArtifacts, fmt.Errorf("encode failure report for site %q: %w", result.site.Name, err)
			}
			failureManifestBytes, err := marshal(protocol.SiteManifest{
				SchemaVersion: 1, SiteID: summary.SiteID, SiteName: result.site.Name,
				Status: "failed", GeneratorVersion: Version, TemplateVersion: TemplateVersion,
				InputHash: siteInputHash, Capabilities: generationCapabilities(result.site, nil),
				Errors: append([]string(nil), summary.Errors...),
			})
			if err != nil {
				return allArtifacts, fmt.Errorf("encode failure manifest for site %q: %w", result.site.Name, err)
			}
			summary.SiteManifestSHA256 = protocol.SHA256(failureManifestBytes)
			if err := publishSiteFiles(root, []pendingFile{
				{path: failureReportArtifact.Path, data: failureReportBytes},
				{path: summary.SiteManifestPath, data: failureManifestBytes},
			}); err != nil {
				summary.SiteManifestSHA256 = ""
				summary.Errors = append(summary.Errors, err.Error())
				operationErrors = append(operationErrors, fmt.Errorf("publish failure report for site %q: %w", result.site.Name, err))
			}
		}
		summaries = append(summaries, summary)
	}

	sort.Slice(summaries, func(i, j int) bool { return summaries[i].SiteName < summaries[j].SiteName })
	sort.Slice(allArtifacts, func(i, j int) bool { return allArtifacts[i].Path < allArtifacts[j].Path })
	globalStatus := "succeeded"
	if len(operationErrors) > 0 {
		globalStatus = "partial_failure"
	}
	globalReport := protocol.GenerationReport{
		SchemaVersion: 1, Status: globalStatus, InputHash: globalInputHash, Sites: summaries,
	}
	reportBytes, err := marshal(globalReport)
	if err != nil {
		return allArtifacts, fmt.Errorf("encode global generation report: %w", err)
	}
	if err := safefs.AtomicWrite(root, "generation-report.json", reportBytes, 0o644); err != nil {
		operationErrors = append(operationErrors, fmt.Errorf("publish global generation report: %w", err))
		globalStatus = "partial_failure"
	}
	globalBytes, err := marshal(GlobalManifest{
		SchemaVersion: 1, GeneratorVersion: Version, TemplateVersion: TemplateVersion,
		InputHash: globalInputHash, Status: globalStatus,
		GenerationReport: "generation-report.json", Sites: summaries,
	})
	if err != nil {
		return allArtifacts, errors.Join(append(operationErrors, fmt.Errorf("encode global manifest: %w", err))...)
	}
	if err := safefs.AtomicWrite(root, "manifest.json", globalBytes, 0o644); err != nil {
		operationErrors = append(operationErrors, fmt.Errorf("publish global manifest: %w", err))
	}
	if len(operationErrors) > 0 {
		return allArtifacts, errors.Join(operationErrors...)
	}
	return allArtifacts, nil
}

func marshalSiteGenerationReport(summary protocol.SiteGenerationSummary, inputHash string) ([]byte, Artifact, error) {
	report := protocol.GenerationReport{
		SchemaVersion: 1, Status: summary.Status, InputHash: inputHash,
		Sites: []protocol.SiteGenerationSummary{summary},
	}
	data, err := marshal(report)
	if err != nil {
		return nil, Artifact{}, err
	}
	return data, Artifact{
		Type: "generation_report", Path: summary.GenerationReportPath,
		InputHash: inputHash, OutputHash: protocol.SHA256(data),
	}, nil
}

func readAndValidateStagedArtifacts(root string, artifacts []Artifact) ([]Artifact, []pendingFile, error) {
	artifacts = append([]Artifact(nil), artifacts...)
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	files := make([]pendingFile, 0, len(artifacts))
	seen := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		site, _, found := strings.Cut(artifact.Path, "/")
		if !found || !protocol.ValidArtifactPath(site, artifact) ||
			!protocol.IsSHA256(artifact.InputHash) || !protocol.IsSHA256(artifact.OutputHash) {
			return nil, nil, fmt.Errorf("invalid generated artifact metadata for %q", artifact.Path)
		}
		if _, exists := seen[artifact.Path]; exists {
			return nil, nil, fmt.Errorf("duplicate generated artifact %q", artifact.Path)
		}
		seen[artifact.Path] = struct{}{}
		file, err := safefs.OpenReadOnly(root, artifact.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("open staged artifact %q: %w", artifact.Path, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, protocol.MaxArtifactBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, nil, fmt.Errorf("read staged artifact %q: %w", artifact.Path, readErr)
		}
		if closeErr != nil {
			return nil, nil, fmt.Errorf("close staged artifact %q: %w", artifact.Path, closeErr)
		}
		if len(data) > protocol.MaxArtifactBytes || protocol.SHA256(data) != artifact.OutputHash {
			return nil, nil, fmt.Errorf("staged artifact %q failed its size or SHA-256 check", artifact.Path)
		}
		files = append(files, pendingFile{path: artifact.Path, data: data})
	}
	return artifacts, files, nil
}

func artifactDetails(artifacts []Artifact, files []pendingFile) []protocol.SiteArtifact {
	dataByPath := make(map[string][]byte, len(files))
	for _, file := range files {
		dataByPath[file.path] = file.data
	}
	details := make([]protocol.SiteArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		data := dataByPath[artifact.Path]
		details = append(details, protocol.SiteArtifact{
			Type: artifact.Type, Path: artifact.Path,
			SizeBytes: int64(len(data)), SHA256: artifact.OutputHash,
		})
	}
	return details
}

func generationCapabilities(site domain.Site, artifacts []Artifact) []protocol.GenerationCapability {
	types := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		types[artifact.Type] = struct{}{}
	}
	capabilities := []protocol.GenerationCapability{
		{Feature: "site_artifact_generation", Status: "generated"},
		{Feature: "bootstrap_service_execution", Status: "not_configured", Evidence: "generation emits static files only; no service is started"},
		{Feature: "ztp", Status: "unknown", Evidence: "no device-specific ZTP mechanism was verified"},
		{Feature: "autoinstall", Status: "unknown", Evidence: "no device-specific autoinstall mechanism was verified"},
	}
	if _, exists := types["ansible_inspection_playbook"]; exists {
		capabilities = append(capabilities, protocol.GenerationCapability{
			Feature: "ansible_inspection", Status: "generated",
			Evidence: "inspection playbook only; no Ansible process was executed",
		})
	}
	if _, exists := types["ansible_playbook"]; exists {
		capabilities = append(capabilities, protocol.GenerationCapability{
			Feature: "vendor_ansible_rendering", Status: "unverified",
			Evidence: "rendered from an unverified profile; no equipment execution was performed",
		})
	} else {
		capabilities = append(capabilities, protocol.GenerationCapability{Feature: "vendor_ansible_rendering", Status: "not_configured"})
	}
	for _, device := range site.Devices {
		profile, exists := domain.VendorProfileFor(device)
		capability := protocol.GenerationCapability{
			Feature: "device_compatibility:" + device.Name,
			Status:  string(domain.CapabilityUnknown), Evidence: "no matching device profile is recorded",
		}
		if exists {
			capability.Status = string(profile.Status)
			capability.Evidence = profile.Evidence
		}
		capabilities = append(capabilities, capability)
	}
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i].Feature < capabilities[j].Feature })
	return capabilities
}

func publishSiteFiles(root string, files []pendingFile) error {
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if filepath.IsAbs(file.path) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(file.path))) != file.path {
			return fmt.Errorf("refusing to publish unsafe artifact path %q", file.path)
		}
		if _, exists := seen[file.path]; exists {
			return fmt.Errorf("refusing to publish duplicate artifact path %q", file.path)
		}
		seen[file.path] = struct{}{}
		target := filepath.Join(root, filepath.FromSlash(file.path))
		relative, err := filepath.Rel(root, target)
		if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || relative == ".." {
			return fmt.Errorf("refusing to publish artifact outside output root")
		}
		parent := filepath.Dir(target)
		for current := parent; current != root && current != filepath.Dir(current); current = filepath.Dir(current) {
			info, err := os.Lstat(current)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("inspect artifact directory %q: %w", current, err)
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("artifact directory %q must be a real directory", current)
			}
		}
		info, err := os.Lstat(target)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect artifact destination %q: %w", file.path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("artifact destination %q must be a regular file", file.path)
		}
	}
	for _, file := range files {
		if err := safefs.AtomicWrite(root, file.path, file.data, 0o644); err != nil {
			return fmt.Errorf("publish artifact %q: %w", file.path, err)
		}
	}
	return nil
}

func removeCatalogManifest(root, site string) error {
	sitePath := filepath.Join(root, site)
	siteInfo, err := os.Lstat(sitePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect site output %q: %w", site, err)
	}
	if siteInfo.Mode()&os.ModeSymlink != 0 || !siteInfo.IsDir() {
		return fmt.Errorf("site output %q must be a real directory", site)
	}
	manifestPath := filepath.Join(sitePath, "manifest.json")
	info, err := os.Lstat(manifestPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect catalog manifest for %q: %w", site, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("catalog manifest for %q must be a regular file", site)
	}
	if err := os.Remove(manifestPath); err != nil {
		return fmt.Errorf("remove stale catalog manifest for %q: %w", site, err)
	}
	return nil
}

func removeGeneratedReport(root, name string) error {
	path := filepath.Join(root, name)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect generated report %q: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("generated report %q must be a regular file", name)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale generated report %q: %w", name, err)
	}
	return nil
}

func removeGlobalManifest(root string) error {
	path := filepath.Join(root, "manifest.json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect global manifest: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("global manifest must be a regular file")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale global manifest: %w", err)
	}
	return nil
}

func removeSiteManifests(root string, infrastructure domain.Infrastructure) error {
	for _, site := range infrastructure.Sites {
		siteDirectory := filepath.Join(root, site.Name)
		info, err := os.Lstat(siteDirectory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect site directory %q: %w", site.Name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("site output %q must be a real directory", site.Name)
		}
		manifest := filepath.Join(siteDirectory, "manifest.json")
		manifestInfo, err := os.Lstat(manifest)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect site manifest %q: %w", site.Name, err)
		}
		if !manifestInfo.Mode().IsRegular() {
			return fmt.Errorf("site manifest %q must be a regular file", site.Name)
		}
		if err := os.Remove(manifest); err != nil {
			return fmt.Errorf("remove stale site manifest %q: %w", site.Name, err)
		}
	}
	return nil
}

func writeCombinedManifests(outputDirectory string, artifacts []Artifact) error {
	bySite := make(map[string][]Artifact)
	inputHashes := make(map[string]string)
	seenPaths := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		site, _, found := strings.Cut(artifact.Path, "/")
		if !found || !protocol.ValidSiteName(site) || !protocol.ValidArtifactPath(site, artifact) || !protocol.IsSHA256(artifact.InputHash) || !protocol.IsSHA256(artifact.OutputHash) {
			return fmt.Errorf("cannot publish invalid generated artifact %q", artifact.Path)
		}
		if _, exists := seenPaths[artifact.Path]; exists {
			return fmt.Errorf("cannot publish duplicate generated artifact %q", artifact.Path)
		}
		seenPaths[artifact.Path] = struct{}{}
		if inputHash, exists := inputHashes[site]; exists && inputHash != artifact.InputHash {
			return fmt.Errorf("generated artifacts for site %q have inconsistent input hashes", site)
		}
		inputHashes[site] = artifact.InputHash
		bySite[site] = append(bySite[site], artifact)
	}
	root, err := filepath.Abs(outputDirectory)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}
	sites := make([]string, 0, len(bySite))
	for site := range bySite {
		sites = append(sites, site)
	}
	sort.Strings(sites)
	for _, site := range sites {
		siteArtifacts := bySite[site]
		sort.Slice(siteArtifacts, func(i, j int) bool { return siteArtifacts[i].Path < siteArtifacts[j].Path })
		manifest, err := marshal(Manifest{
			GeneratorVersion: Version,
			TemplateVersion:  TemplateVersion,
			InputHash:        inputHashes[site],
			Artifacts:        siteArtifacts,
		})
		if err != nil {
			return fmt.Errorf("encode combined manifest for %q: %w", site, err)
		}
		manifestPath := filepath.ToSlash(filepath.Join(site, "manifest.json"))
		if err := safefs.AtomicWrite(root, manifestPath, manifest, 0o644); err != nil {
			return fmt.Errorf("publish combined manifest for %q: %w", site, err)
		}
	}
	return nil
}

func canonicalize(infrastructure domain.Infrastructure) domain.Infrastructure {
	result := domain.Infrastructure{Sites: append([]domain.Site(nil), infrastructure.Sites...)}
	sort.Slice(result.Sites, func(i, j int) bool { return result.Sites[i].Name < result.Sites[j].Name })
	for siteIndex := range result.Sites {
		site := &result.Sites[siteIndex]
		site.Devices = append([]domain.Device(nil), site.Devices...)
		sort.Slice(site.Devices, func(i, j int) bool { return site.Devices[i].Name < site.Devices[j].Name })
		for deviceIndex := range site.Devices {
			device := &site.Devices[deviceIndex]
			device.Identity.MACs = append([]string(nil), device.Identity.MACs...)
			sort.Strings(device.Identity.MACs)
		}
		site.Links = append([]domain.Link(nil), site.Links...)
		sort.Slice(site.Links, func(i, j int) bool {
			if site.Links[i].ID != site.Links[j].ID {
				return site.Links[i].ID < site.Links[j].ID
			}
			if site.Links[i].A != site.Links[j].A {
				return site.Links[i].A < site.Links[j].A
			}
			if site.Links[i].B != site.Links[j].B {
				return site.Links[i].B < site.Links[j].B
			}
			return site.Links[i].Network < site.Links[j].Network
		})
	}
	return result
}

func buildTopology(site domain.Site) topology {
	result := topology{Site: site.Name}
	for _, device := range site.Devices {
		id := device.ID
		if id == "" {
			id = device.Name
		}
		result.Nodes = append(result.Nodes, topologyNode{
			ID: id, Name: device.Name, Role: device.Role, Vendor: device.Vendor,
			Model: device.Model, ManagementIP: device.Management.IPv4,
		})
	}
	for _, link := range site.Links {
		result.Edges = append(result.Edges, topologyEdge{
			ID: link.ID, A: parsePort(link.A), B: parsePort(link.B), Network: link.Network,
		})
	}
	return result
}

func parsePort(value string) topologyPort {
	device, iface, _ := strings.Cut(value, ":")
	return topologyPort{Device: device, Interface: iface}
}

func marshal(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode artifact: %w", err)
	}
	return append(data, '\n'), nil
}
