package generator

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"infraflow/internal/domain"
	"infraflow/pkg/protocol"
)

type vendorProfile struct {
	group      string
	collection string
	family     string
}

type vendorTemplateManifest struct {
	TemplateVersion string   `json:"template_version"`
	InputHash       string   `json:"input_hash"`
	ExecutionMode   string   `json:"execution_mode"`
	Devices         []string `json:"devices"`
	Status          string   `json:"status"`
	Evidence        string   `json:"evidence"`
}

func hasVendorNetworkIntent(site domain.Site) bool {
	for _, device := range site.Devices {
		if device.Network != nil {
			return true
		}
	}
	return false
}

func vendorAnsibleFiles(site domain.Site, inputHash string) ([]pendingFile, []Artifact, error) {
	profiles := make(map[string]vendorProfile)
	hosts := make(map[string]any)
	deviceNames := make([]string, 0)
	for _, device := range site.Devices {
		if device.Network == nil {
			continue
		}
		profile, err := vendorProfileFor(device)
		if err != nil {
			return nil, nil, err
		}
		if device.Management.IPv4 == "" {
			return nil, nil, fmt.Errorf("site %s device %s: vendor Ansible generation requires management.ipv4", site.Name, device.Name)
		}
		profiles[device.Name] = profile
		hosts[device.Name] = map[string]any{
			"ansible_host":     device.Management.IPv4,
			"infraflow_vendor": device.Vendor,
			"infraflow_family": device.Family,
			"infraflow_model":  device.Model,
			"infraflow_role":   device.Role,
		}
		deviceNames = append(deviceNames, device.Name)
	}
	if len(deviceNames) == 0 {
		return nil, nil, nil
	}
	sort.Strings(deviceNames)

	inventory := map[string]any{
		"all": map[string]any{
			"children": map[string]any{
				"network": map[string]any{"hosts": hosts},
			},
		},
	}
	inventoryBytes, err := marshal(inventory)
	if err != nil {
		return nil, nil, err
	}

	playbook := make([]any, 0, len(deviceNames))
	for _, deviceName := range deviceNames {
		device := findDevice(site.Devices, deviceName)
		playbook = append(playbook, map[string]any{
			"name":         "InfraFlow vendor configuration for " + deviceName,
			"hosts":        deviceName,
			"gather_facts": false,
			"serial":       1,
			"tasks": []any{
				map[string]any{
					"name": "Require explicit safety opt-in",
					"ansible.builtin.assert": map[string]any{
						"that":     []string{"ansible_check_mode or lookup('env', 'INFRAFLOW_APPLY') == 'true'"},
						"fail_msg": "Refusing device changes. Review the generated plan and set INFRAFLOW_APPLY=true only when authorized.",
					},
				},
				vendorSafetyTask(device, profiles[deviceName]),
			},
		})
	}
	playbookBytes, err := marshal(playbook)
	if err != nil {
		return nil, nil, err
	}

	manifest := vendorTemplateManifest{
		TemplateVersion: "1",
		InputHash:       inputHash,
		ExecutionMode:   "manual-only; check mode by default; INFRAFLOW_APPLY=true is required for mutation",
		Devices:         deviceNames,
		Status:          "UNVERIFIED",
		Evidence:        "Rendered from validated desired-state input; no device execution or lab verification occurred",
	}
	manifestBytes, err := marshal(manifest)
	if err != nil {
		return nil, nil, err
	}

	requirementsBytes := vendorRequirements(profiles)
	outputs := []struct {
		name     string
		typeName string
		data     []byte
	}{
		{"vendor-inventory.yml", "ansible_vendor_inventory", inventoryBytes},
		{"vendor-playbook.yml", "ansible_playbook", playbookBytes},
		{"requirements.yml", "ansible_requirements", requirementsBytes},
		{"vendor-template.json", "ansible_vendor_manifest", manifestBytes},
	}
	files := make([]pendingFile, 0, len(outputs))
	artifacts := make([]Artifact, 0, len(outputs))
	for _, output := range outputs {
		relativePath := filepath.ToSlash(filepath.Join(site.Name, "ansible", output.name))
		files = append(files, pendingFile{path: relativePath, data: output.data})
		artifacts = append(artifacts, Artifact{
			Type:       output.typeName,
			Path:       relativePath,
			InputHash:  inputHash,
			OutputHash: protocol.SHA256(output.data),
		})
	}
	return files, artifacts, nil
}

func vendorProfileFor(device domain.Device) (vendorProfile, error) {
	vendor := strings.ToLower(strings.TrimSpace(device.Vendor))
	family := strings.ToLower(strings.TrimSpace(device.Family))
	switch {
	case vendor == "cisco" && (family == "iosxe" || family == "ios-xe" || family == "ios_xe"):
		return vendorProfile{group: "cisco_iosxe", collection: "cisco.ios", family: "iosxe"}, nil
	case vendor == "mikrotik" && family == "routeros":
		return vendorProfile{group: "mikrotik_routeros", collection: "community.routeros", family: "routeros"}, nil
	case (vendor == "fortinet" || vendor == "fortigate") && (family == "fortios" || family == "fortigate"):
		if strings.TrimSpace(device.Network.VDOM) == "" {
			return vendorProfile{}, fmt.Errorf("device %s: FortiGate network intent requires network.vdom", device.Name)
		}
		return vendorProfile{group: "fortinet_fortios", collection: "fortinet.fortios", family: "fortios"}, nil
	default:
		return vendorProfile{}, fmt.Errorf("device %s: unsupported vendor profile %s/%s", device.Name, device.Vendor, device.Family)
	}
}

func findDevice(devices []domain.Device, name string) domain.Device {
	for _, device := range devices {
		if device.Name == name {
			return device
		}
	}
	return domain.Device{Name: name}
}

func vendorSafetyTask(device domain.Device, profile vendorProfile) map[string]any {
	var module string
	var parameters map[string]any
	switch profile.group {
	case "cisco_iosxe":
		module = "cisco.ios.ios_interfaces"
		parameters = map[string]any{
			"config": []any{map[string]any{"name": "management", "enabled": true}},
			"state":  "merged",
		}
	case "mikrotik_routeros":
		module = "community.routeros.api_modify"
		parameters = map[string]any{
			"hostname": device.Management.IPv4,
			"path":     "ip address",
			"data":     []any{},
		}
	case "fortinet_fortios":
		module = "fortinet.fortios.fortios_system_interface"
		parameters = map[string]any{
			"vdom":         device.Network.VDOM,
			"state":        "present",
			"access_token": "{{ lookup('env', 'INFRAFLOW_FORTIOS_ACCESS_TOKEN') }}",
		}
	default:
		module = "ansible.builtin.debug"
		parameters = map[string]any{"msg": "Template is intentionally inert until a verified profile implementation is available"}
	}
	return map[string]any{"name": "Render " + profile.family + " placeholder task", module: parameters}
}

func vendorRequirements(profiles map[string]vendorProfile) []byte {
	versions := map[string]string{
		"cisco.ios":          "11.5.1",
		"community.routeros": "3.22.0",
		"fortinet.fortios":   "2.6.0",
	}
	collections := make(map[string]bool)
	for _, profile := range profiles {
		collections[profile.collection] = true
	}
	var builder strings.Builder
	builder.WriteString("# Generated by InfraFlow; no collection is installed by this renderer.\ncollections:\n")
	for _, collection := range []string{"cisco.ios", "community.routeros", "fortinet.fortios"} {
		if !collections[collection] {
			continue
		}
		builder.WriteString("  - name: " + collection + "\n")
		builder.WriteString("    version: \"")
		builder.WriteString(versions[collection])
		builder.WriteString("\"\n")
	}
	return []byte(builder.String())
}
