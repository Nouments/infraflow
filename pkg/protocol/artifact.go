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
	case "ansible_inventory":
		return len(parts) == 3 && parts[1] == "ansible" && parts[2] == "inventory.yml"
	case "ansible_playbook":
		return len(parts) == 3 && parts[1] == "ansible" && (parts[2] == "site.yml" || parts[2] == "vendor-playbook.yml")
	case "ansible_vendor_inventory":
		return len(parts) == 3 && parts[1] == "ansible" && parts[2] == "vendor-inventory.yml"
	case "ansible_requirements":
		return len(parts) == 3 && parts[1] == "ansible" && parts[2] == "requirements.yml"
	case "ansible_vendor_manifest":
		return len(parts) == 3 && parts[1] == "ansible" && parts[2] == "vendor-template.json"
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
