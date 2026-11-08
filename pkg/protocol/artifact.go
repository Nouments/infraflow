package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Artifact struct {
	Type       string `json:"type"`
	Path       string `json:"path"`
	InputHash  string `json:"input_hash"`
	OutputHash string `json:"output_hash"`
}

type SiteArtifact struct {
	Type                    string   `json:"type"`
	Path                    string   `json:"path"`
	SizeBytes               int64    `json:"size_bytes"`
	SHA256                  string   `json:"sha256"`
	SiteID                  string   `json:"site_id,omitempty"`
	DeviceID                string   `json:"device_id,omitempty"`
	Vendor                  string   `json:"vendor,omitempty"`
	Family                  string   `json:"family,omitempty"`
	Model                   string   `json:"model,omitempty"`
	OSVersion               string   `json:"os_version,omitempty"`
	Format                  string   `json:"artifact_format,omitempty"`
	TemplateID              string   `json:"template_id,omitempty"`
	TemplateVersion         string   `json:"template_version,omitempty"`
	InputHash               string   `json:"input_hash,omitempty"`
	GenerationStatus        string   `json:"generation_status,omitempty"`
	CompatibilityStatus     string   `json:"compatibility_status,omitempty"`
	DocumentationReferences []string `json:"documentation_references"`
}

type GenerationCapability struct {
	Feature  string `json:"feature"`
	Status   string `json:"status"`
	Evidence string `json:"evidence,omitempty"`
}

type SiteManifest struct {
	SchemaVersion    int                    `json:"schema_version"`
	SiteID           string                 `json:"site_id"`
	SiteName         string                 `json:"site_name"`
	Status           string                 `json:"status"`
	GeneratorVersion string                 `json:"generator_version"`
	TemplateVersion  string                 `json:"template_version"`
	InputHash        string                 `json:"input_hash"`
	Artifacts        []SiteArtifact         `json:"artifacts"`
	Capabilities     []GenerationCapability `json:"capabilities"`
	Errors           []string               `json:"errors,omitempty"`
}

type SiteGenerationSummary struct {
	SiteID               string   `json:"site_id"`
	SiteName             string   `json:"site_name"`
	Status               string   `json:"status"`
	ArtifactCount        int      `json:"artifact_count"`
	SiteManifestPath     string   `json:"site_manifest_path"`
	SiteManifestSHA256   string   `json:"site_manifest_sha256,omitempty"`
	GenerationReportPath string   `json:"generation_report_path"`
	Errors               []string `json:"errors,omitempty"`
}

type GenerationReport struct {
	SchemaVersion int                     `json:"schema_version"`
	Status        string                  `json:"status"`
	InputHash     string                  `json:"input_hash"`
	Sites         []SiteGenerationSummary `json:"sites"`
}

type AgentReport struct {
	ReportID   string           `json:"report_id"`
	AgentID    string           `json:"agent_id"`
	ReportedAt time.Time        `json:"reported_at"`
	Artifacts  []ArtifactResult `json:"artifacts"`
}

type ArtifactResult struct {
	Path       string `json:"path"`
	OutputHash string `json:"output_hash"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
}

const (
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusBlocked   = "blocked"
	// MaxArtifactBytes bounds catalog hashing, artifact verification, and streaming.
	MaxArtifactBytes = 32 << 20
)

var siteNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

func ValidSiteName(value string) bool {
	return siteNamePattern.MatchString(value)
}

func ValidArtifactPath(site string, artifact Artifact) bool {
	value := artifact.Path
	if !ValidSiteName(site) || value == "" || path.IsAbs(value) || path.Clean(value) != value || strings.Contains(value, "\\") {
		return false
	}
	parts := strings.Split(value, "/")
	if len(parts) < 2 || parts[0] != site {
		return false
	}
	switch artifact.Type {
	case "inventory":
		return len(parts) == 2 && parts[1] == "inventory.json"
	case "topology":
		return len(parts) == 2 && parts[1] == "topology.json"
	case "generation_report":
		return len(parts) == 2 && parts[1] == "generation-report.json"
	case "site_manifest":
		return len(parts) == 2 && parts[1] == "site-manifest.json"
	case "native_device_config":
		if len(parts) != 3 || parts[1] != "native" || !strings.HasPrefix(parts[2], "device-") || !strings.HasSuffix(parts[2], ".cfg") {
			return false
		}
		index := strings.TrimSuffix(strings.TrimPrefix(parts[2], "device-"), ".cfg")
		return len(index) <= 63 && ValidSiteName(index)
	case "native_manifest":
		return len(parts) == 3 && parts[1] == "native" && parts[2] == "native-manifest.json"
	case "terraform_versions":
		return len(parts) == 3 && parts[1] == "terraform" && parts[2] == "versions.tf"
	case "terraform_providers":
		return len(parts) == 3 && parts[1] == "terraform" && parts[2] == "providers.tf"
	case "terraform_variables":
		return len(parts) == 3 && parts[1] == "terraform" && parts[2] == "variables.tf"
	case "terraform_locals":
		return len(parts) == 3 && parts[1] == "terraform" && parts[2] == "locals.tf"
	case "terraform_main":
		return len(parts) == 3 && parts[1] == "terraform" && parts[2] == "main.tf"
	case "terraform_outputs":
		return len(parts) == 3 && parts[1] == "terraform" && parts[2] == "outputs.tf"
	case "terraform_tfvars_example":
		return len(parts) == 3 && parts[1] == "terraform" && parts[2] == "terraform.tfvars.example"
	case "bootstrap_dhcp":
		return len(parts) == 4 && parts[1] == "bootstrap" && parts[2] == "dhcp" && parts[3] == "config.json"
	case "bootstrap_dns":
		return len(parts) == 4 && parts[1] == "bootstrap" && parts[2] == "dns" && parts[3] == "config.json"
	case "bootstrap_tftp":
		return len(parts) == 4 && parts[1] == "bootstrap" && parts[2] == "tftp" && parts[3] == "config.json"
	case "bootstrap_pxe":
		return len(parts) == 4 && parts[1] == "bootstrap" && parts[2] == "pxe" && parts[3] == "metadata.json"
	case "bootstrap_ipxe_script":
		return len(parts) == 5 && parts[1] == "bootstrap" && parts[2] == "pxe" && parts[3] == "ipxe" && parts[4] == "bootstrap.ipxe"
	case "bootstrap_ipxe_menu":
		return len(parts) == 5 && parts[1] == "bootstrap" && parts[2] == "pxe" && parts[3] == "ipxe" && parts[4] == "menu.ipxe"
	default:
		return false
	}
}

func IsSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

// ValidSiteArtifactMetadata validates optional per-device provenance fields.
// Older site manifests remain valid when all such fields are absent.
func ValidSiteArtifactMetadata(siteID, manifestInputHash string, artifact SiteArtifact) bool {
	hasMetadata := artifact.SiteID != "" || artifact.DeviceID != "" || artifact.Vendor != "" ||
		artifact.Family != "" || artifact.Model != "" || artifact.OSVersion != "" ||
		artifact.Format != "" || artifact.TemplateID != "" || artifact.TemplateVersion != "" ||
		artifact.InputHash != "" || artifact.GenerationStatus != "" ||
		artifact.CompatibilityStatus != "" || artifact.DocumentationReferences != nil
	if !hasMetadata {
		return true
	}

	return artifact.SiteID == siteID &&
		ValidSiteName(artifact.DeviceID) && len(artifact.DeviceID) <= 128 &&
		artifact.Type == "native_device_config" && artifact.Format == "text/plain" &&
		artifact.Vendor != "" && artifact.Family != "" &&
		artifact.Format != "" && artifact.TemplateID != "" &&
		artifact.TemplateVersion != "" &&
		IsSHA256(artifact.InputHash) && artifact.InputHash == manifestInputHash &&
		artifact.GenerationStatus == "rendered_unverified" &&
		artifact.CompatibilityStatus == "unknown" &&
		artifact.DocumentationReferences != nil
}

func SHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func ComputeReportID(report AgentReport) string {
	artifacts := append([]ArtifactResult(nil), report.Artifacts...)
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].Path != artifacts[j].Path {
			return artifacts[i].Path < artifacts[j].Path
		}
		return artifacts[i].OutputHash < artifacts[j].OutputHash
	})
	payload, _ := json.Marshal(struct {
		AgentID   string           `json:"agent_id"`
		Artifacts []ArtifactResult `json:"artifacts"`
	}{AgentID: report.AgentID, Artifacts: artifacts})
	return SHA256(payload)
}
