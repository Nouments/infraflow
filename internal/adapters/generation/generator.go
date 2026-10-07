package generator

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"infraflow/internal/adapters/config"
	"infraflow/internal/domain"
	"infraflow/internal/infrastructure/safefs"
	"infraflow/pkg/protocol"
)

const Version = "0.1.0"
const TemplateVersion = "1"

type Artifact = protocol.Artifact

type Manifest struct {
	GeneratorVersion string     `json:"generator_version"`
	TemplateVersion  string     `json:"template_version"`
	InputHash        string     `json:"input_hash"`
	Artifacts        []Artifact `json:"artifacts"`
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
	AnsibleHost     string `json:"ansible_host,omitempty"`
	InfraflowVendor string `json:"infraflow_vendor,omitempty"`
	InfraflowFamily string `json:"infraflow_family,omitempty"`
	InfraflowModel  string `json:"infraflow_model,omitempty"`
	InfraflowRole   string `json:"infraflow_role,omitempty"`
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
		var builder strings.Builder
		builder.WriteString("{\n")
		for _, key := range keys {
			encoded, err := hclValue(typed[key], indent+2)
			if err != nil {
				return "", err
			}
			keyJSON, err := json.Marshal(key)
			if err != nil {
				return "", err
			}
			builder.WriteString(strings.Repeat(" ", indent+2))
			builder.Write(keyJSON)
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
				InfraflowFamily: device.Family, InfraflowModel: device.Model, InfraflowRole: device.Role,
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
				Debug: ansibleDebugTask{Msg: "device={{ inventory_hostname }} vendor={{ hostvars[inventory_hostname].infraflow_vendor | default('unknown') }} model={{ hostvars[inventory_hostname].infraflow_model | default('unknown') }}"},
			}},
		}})
		if err != nil {
			return nil, err
		}
		inventoryPath := filepath.ToSlash(filepath.Join(site.Name, "ansible", "inventory.yml"))
		playbookPath := filepath.ToSlash(filepath.Join(site.Name, "ansible", "site.yml"))
		inventoryArtifact := Artifact{Type: "ansible_inventory", Path: inventoryPath, InputHash: inputHash, OutputHash: protocol.SHA256(inventoryBytes)}
		playbookArtifact := Artifact{Type: "ansible_playbook", Path: playbookPath, InputHash: inputHash, OutputHash: protocol.SHA256(playbookBytes)}
		artifacts = append(artifacts, inventoryArtifact, playbookArtifact)
		files = append(files, pendingFile{path: inventoryPath, data: inventoryBytes}, pendingFile{path: playbookPath, data: playbookBytes})
		if hasVendorNetworkIntent(site) {
			vendorFiles, vendorArtifacts, err := vendorAnsibleFiles(site, inputHash)
			if err != nil {
				return nil, err
			}
			files = append(files, vendorFiles...)
			artifacts = append(artifacts, vendorArtifacts...)
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
	if problems := config.Validate(infrastructure); len(problems) > 0 {
		return nil, problems
	}
	if strings.TrimSpace(outputDirectory) == "" {
		return nil, fmt.Errorf("output directory is required")
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
	staging, err := os.MkdirTemp("", "infraflow-generate-all-")
	if err != nil {
		return nil, fmt.Errorf("create generation staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	var artifacts []Artifact
	for _, generate := range []func(domain.Infrastructure, string) ([]Artifact, error){
		Generate,
		GenerateAnsible,
		GenerateTerraform,
		GenerateBootstrap,
	} {
		generated, err := generate(infrastructure, staging)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, generated...)
	}
	pending := make([]pendingFile, 0, len(artifacts))
	for _, artifact := range artifacts {
		file, err := safefs.OpenReadOnly(staging, artifact.Path)
		if err != nil {
			return nil, fmt.Errorf("open staged artifact %q: %w", artifact.Path, err)
		}
		data, readErr := io.ReadAll(file)
		closeErr := file.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read staged artifact %q: %w", artifact.Path, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close staged artifact %q: %w", artifact.Path, closeErr)
		}
		if protocol.SHA256(data) != artifact.OutputHash {
			return nil, fmt.Errorf("staged artifact %q failed its output hash", artifact.Path)
		}
		pending = append(pending, pendingFile{path: artifact.Path, data: data})
	}
	if err := removeSiteManifests(root, infrastructure); err != nil {
		return nil, err
	}
	for _, file := range pending {
		if err := safefs.AtomicWrite(root, file.path, file.data, 0o644); err != nil {
			return nil, fmt.Errorf("publish artifact %q: %w", file.path, err)
		}
	}
	if err := writeCombinedManifests(outputDirectory, artifacts); err != nil {
		return nil, err
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
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
