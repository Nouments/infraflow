package generator

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"infraflow/internal/adapters/catalog"
	"infraflow/internal/adapters/config"
	"infraflow/internal/domain"
	"infraflow/internal/infrastructure/safefs"
	"infraflow/pkg/protocol"

	"gopkg.in/yaml.v3"
)

//go:embed templates/vendors
var vendorTemplateFiles embed.FS

type deviceTemplateMetadata struct {
	ID           string   `yaml:"id"`
	Version      string   `yaml:"version"`
	Manufacturer string   `yaml:"manufacturer"`
	Model        string   `yaml:"model"`
	OSFamily     string   `yaml:"os_family"`
	OSVersions   []string `yaml:"os_versions"`
	Features     []string `yaml:"features"`
	Variables    []string `yaml:"variables"`
	Status       string   `yaml:"status"`
	Source       string   `yaml:"source"`
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
	Device          string               `json:"device"`
	Manufacturer    string               `json:"manufacturer"`
	Model           string               `json:"model"`
	OS              string               `json:"os"`
	OSVersion       string               `json:"os_version,omitempty"`
	CatalogID       string               `json:"catalog_id"`
	CatalogStatus   string               `json:"catalog_status"`
	TemplateID      string               `json:"template_id,omitempty"`
	TemplateVersion string               `json:"template_version,omitempty"`
	TemplateHash    string               `json:"template_hash,omitempty"`
	SelectionReason string               `json:"selection_reason,omitempty"`
	Candidates      []TemplateEvaluation `json:"candidates"`
	Variables       map[string]any       `json:"variables,omitempty"`
	ConfigPath      string               `json:"config_path,omitempty"`
	ConfigHash      string               `json:"config_hash,omitempty"`
	Status          string               `json:"status"`
	Limits          []string             `json:"limits,omitempty"`
}

type DeclarativeGenerationReport struct {
	APIVersion               string                   `json:"api_version"`
	Deployment               string                   `json:"deployment"`
	Site                     string                   `json:"site"`
	Validation               string                   `json:"validation"`
	CompatibilityScope       string                   `json:"compatibility_scope"`
	CommandSyntaxValidation  string                   `json:"command_syntax_validation"`
	PhysicalDeviceValidation string                   `json:"physical_device_validation"`
	Status                   string                   `json:"status"`
	Devices                  []DeviceGenerationReport `json:"devices"`
	Incompatibilities        []string                 `json:"incompatibilities,omitempty"`
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
	infrastructure, deployment, err := config.ParseNetworkDeployment(input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(outputDirectory) == "" {
		return nil, fmt.Errorf("output directory is required")
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
		APIVersion:               config.NetworkDeploymentAPIVersion,
		Deployment:               deployment.Metadata.Name,
		Site:                     deployment.Spec.Site,
		Validation:               "passed",
		CompatibilityScope:       "declared manufacturer/model/OS matched local template metadata; physical hardware and OS compatibility are not verified",
		CommandSyntaxValidation:  "not_performed",
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
			Status: "blocked",
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

	artifacts := make([]Artifact, 0, len(pending))
	for _, item := range pending {
		if err := safefs.AtomicWrite(outputDirectory, item.Path, item.Data, 0o644); err != nil {
			return nil, fmt.Errorf("write generated artifact %q: %w", item.Path, err)
		}
		artifacts = append(artifacts, Artifact{
			Type: item.Type, Path: item.Path, InputHash: protocol.SHA256(input),
			OutputHash: protocol.SHA256(item.Data),
		})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
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
		var metadata deviceTemplateMetadata
		decoder := yaml.NewDecoder(strings.NewReader(string(metadataBytes)))
		decoder.KnownFields(true)
		if err := decoder.Decode(&metadata); err != nil {
			return nil, fmt.Errorf("decode template metadata %q: %w", metadataPath, err)
		}
		if metadata.ID == "" || metadata.Version == "" || metadata.Manufacturer == "" ||
			metadata.Model == "" || metadata.OSFamily == "" || metadata.Status == "" || metadata.Source == "" {
			return nil, fmt.Errorf("template metadata %q is incomplete", metadataPath)
		}
		if _, exists := seenIDs[metadata.ID]; exists {
			return nil, fmt.Errorf("duplicate local template id %q", metadata.ID)
		}
		seenIDs[metadata.ID] = struct{}{}
		templatePath := strings.TrimSuffix(metadataPath, "metadata.yaml") + "interfaces.cfg.tmpl"
		content, err := vendorTemplateFiles.ReadFile(templatePath)
		if err != nil {
			return nil, fmt.Errorf("template %q references missing file %q: %w", metadata.ID, templatePath, err)
		}
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
