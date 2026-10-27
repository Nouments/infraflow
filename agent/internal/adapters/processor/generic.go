package processor

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"infraflow/pkg/protocol"
)

type Generic struct{}

type inventoryDocument struct {
	Site    string `json:"site"`
	Devices []struct {
		Name string `json:"name"`
	} `json:"devices"`
}

type topologyDocument struct {
	Site  string `json:"site"`
	Nodes []struct {
		Name string `json:"name"`
	} `json:"nodes"`
	Edges []struct {
		A struct {
			Device string `json:"device"`
		} `json:"a"`
		B struct {
			Device string `json:"device"`
		} `json:"b"`
	} `json:"edges"`
}

func (Generic) Process(artifact protocol.Artifact, data io.Reader) protocol.ArtifactResult {
	result := protocol.ArtifactResult{Path: artifact.Path, OutputHash: artifact.OutputHash, Status: protocol.StatusFailed}
	site, _, _ := strings.Cut(artifact.Path, "/")
	switch artifact.Type {
	case "inventory":
		var inventory inventoryDocument
		if err := json.NewDecoder(data).Decode(&inventory); err != nil || inventory.Site != site {
			result.Message = "inventory artifact is invalid"
			return result
		}
		seenDevices := make(map[string]struct{}, len(inventory.Devices))
		for _, device := range inventory.Devices {
			if strings.TrimSpace(device.Name) == "" {
				result.Message = "inventory contains a device without a name"
				return result
			}
			if _, exists := seenDevices[device.Name]; exists {
				result.Message = "inventory contains duplicate device names"
				return result
			}
			seenDevices[device.Name] = struct{}{}
		}
		result.Status = protocol.StatusCompleted
		result.Message = "inventory imported into local agent state"
	case "topology":
		var topology topologyDocument
		if err := json.NewDecoder(data).Decode(&topology); err != nil || topology.Site != site {
			result.Message = "topology artifact is invalid"
			return result
		}
		devices := make(map[string]struct{}, len(topology.Nodes))
		for _, node := range topology.Nodes {
			if strings.TrimSpace(node.Name) == "" {
				result.Message = "topology contains a node without a name"
				return result
			}
			devices[node.Name] = struct{}{}
		}
		for _, edge := range topology.Edges {
			if _, exists := devices[edge.A.Device]; !exists {
				result.Message = "topology edge references an unknown device"
				return result
			}
			if _, exists := devices[edge.B.Device]; !exists {
				result.Message = "topology edge references an unknown device"
				return result
			}
		}
		result.Status = protocol.StatusCompleted
		result.Message = "topology imported into local agent state"
	case "site_manifest":
		var manifest protocol.SiteManifest
		if err := decodeStrictJSON(data, &manifest); err != nil ||
			manifest.SchemaVersion != 1 || manifest.Status != "succeeded" ||
			manifest.SiteName != site || !protocol.ValidSiteName(manifest.SiteID) || len(manifest.SiteID) > 128 ||
			strings.TrimSpace(manifest.GeneratorVersion) == "" ||
			strings.TrimSpace(manifest.TemplateVersion) == "" || !protocol.IsSHA256(manifest.InputHash) {
			result.Message = "site manifest is invalid"
			return result
		}
		seenArtifacts := make(map[string]struct{}, len(manifest.Artifacts))
		for _, declared := range manifest.Artifacts {
			if !protocol.ValidArtifactPath(site, protocol.Artifact{Type: declared.Type, Path: declared.Path}) ||
				!protocol.IsSHA256(declared.SHA256) || declared.SizeBytes < 0 {
				result.Message = "site manifest contains invalid artifact metadata"
				return result
			}
			if _, exists := seenArtifacts[declared.Path]; exists {
				result.Message = "site manifest contains duplicate artifact paths"
				return result
			}
			seenArtifacts[declared.Path] = struct{}{}
		}
		result.Status = protocol.StatusCompleted
		result.Message = "site manifest validated"
	case "generation_report":
		var report protocol.GenerationReport
		if err := decodeStrictJSON(data, &report); err != nil || report.SchemaVersion != 1 ||
			report.Status != "succeeded" || !protocol.IsSHA256(report.InputHash) || len(report.Sites) != 1 ||
			report.Sites[0].SiteName != site || report.Sites[0].Status != "succeeded" {
			result.Message = "site generation report is invalid"
			return result
		}
		result.Status = protocol.StatusCompleted
		result.Message = "site generation report validated"
	case "ansible_inventory", "ansible_inspection_playbook", "ansible_playbook", "ansible_vendor_inventory", "ansible_requirements", "ansible_vendor_manifest",
		"terraform_versions", "terraform_providers", "terraform_variables", "terraform_locals",
		"terraform_main", "terraform_outputs", "terraform_tfvars_example",
		"bootstrap_dhcp", "bootstrap_dns", "bootstrap_tftp", "bootstrap_pxe",
		"bootstrap_ipxe_script", "bootstrap_ipxe_menu":
		result.Status = protocol.StatusCompleted
		result.Message = "verified artifact staged; tool execution or bootstrap service startup remains explicit"
	default:
		result.Status = protocol.StatusBlocked
		result.Message = "agent has no executor for this artifact type"
	}
	return result
}

func decodeStrictJSON(source io.Reader, destination any) error {
	decoder := json.NewDecoder(source)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return err
	}
	return nil
}
