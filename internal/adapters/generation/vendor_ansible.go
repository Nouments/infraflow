package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"infraflow/internal/adapters/config"
	"infraflow/internal/domain"
	"infraflow/internal/infrastructure/safefs"
	"infraflow/pkg/protocol"
)

type vendorProfile struct {
	group      string
	collection string
	family     string
	method     string
}

type vendorTemplateManifest struct {
	TemplateVersion string            `json:"template_version"`
	InputHash       string            `json:"input_hash"`
	ExecutionMode   string            `json:"execution_mode"`
	Devices         []string          `json:"devices"`
	Methods         map[string]string `json:"methods"`
	Status          string            `json:"status"`
	Evidence        string            `json:"evidence"`
}

// GenerateVendorAnsible writes only the existing vendor configuration bundle.
func GenerateVendorAnsible(infrastructure domain.Infrastructure, outputDirectory string) ([]Artifact, error) {
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
	var files []pendingFile
	var artifacts []Artifact
	for _, site := range canonical.Sites {
		siteFiles, siteArtifacts, err := vendorAnsibleFiles(site, inputHash)
		if err != nil {
			return nil, err
		}
		files = append(files, siteFiles...)
		artifacts = append(artifacts, siteArtifacts...)
	}
	if len(files) == 0 {
		return artifacts, nil
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
			return nil, fmt.Errorf("write vendor Ansible artifact %q: %w", file.path, err)
		}
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func vendorAnsibleFiles(site domain.Site, inputHash string) ([]pendingFile, []Artifact, error) {
	profiles := make(map[string]vendorProfile)
	hosts := make(map[string]any)
	methods := make(map[string]string)
	deviceNames := make([]string, 0)
	for _, device := range site.Devices {
		if device.Network == nil {
			continue
		}
		profile, err := vendorProfileFor(device)
		if err != nil {
			return nil, nil, err
		}
		if len(device.Network.Routes) > 0 && profile.group == "cisco_iosxe" {
			return nil, nil, fmt.Errorf("device %s: Cisco Ansible renderer does not support static routes", device.Name)
		}
		if len(device.Network.NAT.Source)+len(device.Network.NAT.Destination) > 0 {
			return nil, nil, fmt.Errorf("device %s: vendor Ansible renderer does not support NAT intent", device.Name)
		}
		for _, iface := range device.Network.Interfaces {
			if profile.group == "mikrotik_routeros" && iface.IPv4Mode != "static" {
				return nil, nil, fmt.Errorf("device %s: MikroTik Ansible renderer supports only explicit static interface addresses", device.Name)
			}
		}
		profiles[device.Name] = profile
		methods[device.Name] = profile.method
		device.Provisioning.Method = profile.method
		host := map[string]any{
			"ansible_host":     device.Management.IPv4,
			"infraflow_method": device.Provisioning.Method,
			"infraflow_vendor": device.Vendor,
			"infraflow_family": device.Family,
			"infraflow_model":  device.Model,
			"infraflow_role":   device.Role,
		}
		switch profile.group {
		case "cisco_iosxe":
			host["ansible_connection"] = "ansible.netcommon.network_cli"
			host["ansible_network_os"] = "cisco.ios.ios"
		case "fortinet_fortios":
			host["ansible_connection"] = "ansible.netcommon.httpapi"
			host["ansible_network_os"] = "fortinet.fortios.fortios"
			host["ansible_httpapi_use_ssl"] = true
			host["ansible_httpapi_validate_certs"] = true
		}
		hosts[device.Name] = host
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
			"tasks":        buildVendorTasks(device, profiles[deviceName]),
		})
	}
	playbookBytes, err := marshal(playbook)
	if err != nil {
		return nil, nil, err
	}

	manifest := vendorTemplateManifest{
		TemplateVersion: "1",
		InputHash:       inputHash,
		ExecutionMode:   "manual-only; run with --check for preview; INFRAFLOW_APPLY=true is required for mutation",
		Devices:         deviceNames,
		Methods:         methods,
		Status:          "UNVERIFIED",
		Evidence:        "Rendered only for supported interface and static-route intent; no device execution or lab verification occurred",
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
	capability, supported := domain.VendorProfileFor(device)
	if !supported {
		return vendorProfile{}, fmt.Errorf("device %s: unsupported vendor profile %s/%s", device.Name, device.Vendor, device.Family)
	}
	method := strings.TrimSpace(device.Provisioning.Method)
	if method == "" {
		method = capability.DefaultMethod
	}
	if _, exists := capability.Methods[method]; !exists {
		return vendorProfile{}, fmt.Errorf("device %s: unsupported Ansible method %q for %s/%s", device.Name, method, device.Vendor, device.Family)
	}
	switch {
	case vendor == "cisco" && (family == "iosxe" || family == "ios-xe" || family == "ios_xe"):
		return vendorProfile{group: "cisco_iosxe", collection: "cisco.ios", family: "iosxe", method: method}, nil
	case vendor == "mikrotik" && family == "routeros":
		return vendorProfile{group: "mikrotik_routeros", collection: "community.routeros", family: "routeros", method: method}, nil
	case (vendor == "fortinet" || vendor == "fortigate") && (family == "fortios" || family == "fortigate"):
		if strings.TrimSpace(device.Network.VDOM) == "" {
			return vendorProfile{}, fmt.Errorf("device %s: FortiGate network intent requires network.vdom", device.Name)
		}
		return vendorProfile{group: "fortinet_fortios", collection: "fortinet.fortios", family: "fortios", method: method}, nil
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

func buildVendorTasks(device domain.Device, profile vendorProfile) []any {
	if device.Network == nil {
		return []any{
			map[string]any{
				"name": "Require explicit safety opt-in",
				"ansible.builtin.assert": map[string]any{
					"that":     []string{"ansible_check_mode or lookup('env', 'INFRAFLOW_APPLY') == 'true'"},
					"fail_msg": "Refusing device changes. Review the generated plan and set INFRAFLOW_APPLY=true only when authorized.",
				},
			},
			map[string]any{"name": "No network intent for this device", "ansible.builtin.debug": map[string]any{"msg": "No vendor task generated because network intent is empty"}},
		}
	}

	baseTasks := []any{
		map[string]any{
			"name": "Require explicit safety opt-in",
			"ansible.builtin.assert": map[string]any{
				"that":     []string{"ansible_check_mode or lookup('env', 'INFRAFLOW_APPLY') == 'true'"},
				"fail_msg": "Refusing device changes. Review the generated plan and set INFRAFLOW_APPLY=true only when authorized.",
			},
		},
	}

	switch profile.group {
	case "cisco_iosxe":
		baseTasks = append(baseTasks, renderCiscoAdminState(device.Network.Interfaces)...)
		baseTasks = append(baseTasks, renderCiscoTasks(device)...)
	case "mikrotik_routeros":
		baseTasks = append(baseTasks, renderMikroTikTasks(device)...)
	case "fortinet_fortios":
		baseTasks = append(baseTasks, renderFortinetTasks(device)...)
	default:
		baseTasks = append(baseTasks, map[string]any{"name": "Render vendor placeholder task", "ansible.builtin.debug": map[string]any{"msg": "Template is intentionally inert until a verified profile implementation is available"}})
	}
	return baseTasks
}

func renderCiscoAdminState(interfaces []domain.NetworkInterface) []any {
	config := make([]any, 0, len(interfaces))
	for _, iface := range interfaces {
		if iface.Enabled == nil {
			continue
		}
		state := "disabled"
		if *iface.Enabled {
			state = "enabled"
		}
		config = append(config, map[string]any{"name": iface.Name, "enabled": state})
	}
	if len(config) == 0 {
		return nil
	}
	return []any{map[string]any{
		"name": "Render Cisco interface administrative state",
		"cisco.ios.ios_interfaces": map[string]any{
			"config": config,
			"state":  "merged",
		},
	}}
}

func renderCiscoTasks(device domain.Device) []any {
	tasks := make([]any, 0, 2)
	if len(device.Network.Interfaces) > 0 {
		tasks = append(tasks, map[string]any{
			"name": "Render Cisco interface configuration",
			"cisco.ios.ios_l3_interfaces": map[string]any{
				"config": renderCiscoInterfaces(device.Network.Interfaces),
				"state":  "merged",
			},
		})
	}
	return tasks
}

func renderCiscoInterfaces(interfaces []domain.NetworkInterface) []any {
	out := make([]any, 0, len(interfaces))
	for _, item := range interfaces {
		entry := map[string]any{"name": item.Name}
		switch item.IPv4Mode {
		case "static":
			entry["ipv4"] = []any{map[string]any{"address": item.IPv4Address}}
		case "dhcp":
			entry["ipv4"] = []any{map[string]any{"dhcp": map[string]any{"enable": true}}}
		}
		out = append(out, entry)
	}
	return out
}

func renderCiscoRoutes(routes []domain.StaticRoute) []any {
	out := make([]any, 0, len(routes))
	for _, route := range routes {
		out = append(out, map[string]any{
			"address_families": []any{map[string]any{
				"afi": "ipv4",
				"safis": []any{map[string]any{
					"multi_hop": false,
					"routes": []any{map[string]any{
						"dest":     route.Destination,
						"next_hop": map[string]any{"forward_router_address": route.NextHop},
					}},
				}},
			}},
		})
	}
	return out
}

func renderMikroTikTasks(device domain.Device) []any {
	tasks := make([]any, 0, 4)
	if len(device.Network.Interfaces) > 0 {
		tasks = append(tasks, map[string]any{
			"name": "Render MikroTik interface addresses",
			"community.routeros.api_modify": map[string]any{
				"hostname": device.Management.IPv4,
				"username": "{{ ansible_user }}",
				"password": "{{ ansible_password }}",
				"path":     "ip address",
				"data":     renderMikroTikInterfaces(device.Network.Interfaces),
			},
		})
	}
	if len(device.Network.Routes) > 0 {
		tasks = append(tasks, map[string]any{
			"name": "Render MikroTik static routes",
			"community.routeros.api_modify": map[string]any{
				"hostname": device.Management.IPv4,
				"username": "{{ ansible_user }}",
				"password": "{{ ansible_password }}",
				"path":     "ip route",
				"data":     renderMikroTikRoutes(device.Network.Routes),
			},
		})
	}
	return tasks
}

func renderMikroTikInterfaces(interfaces []domain.NetworkInterface) []any {
	out := make([]any, 0, len(interfaces))
	for _, item := range interfaces {
		if item.IPv4Address == "" {
			continue
		}
		out = append(out, map[string]any{"interface": item.Name, "address": item.IPv4Address})
	}
	return out
}

func renderMikroTikRoutes(routes []domain.StaticRoute) []any {
	out := make([]any, 0, len(routes))
	for _, route := range routes {
		gateway := route.NextHop
		if gateway == "" {
			gateway = route.Interface
		}
		out = append(out, map[string]any{"dst": route.Destination, "gateway": gateway})
	}
	return out
}

func renderFortinetTasks(device domain.Device) []any {
	tasks := make([]any, 0, 4)
	if len(device.Network.Interfaces) > 0 {
		tasks = append(tasks, map[string]any{
			"name": "Render FortiGate interface addresses",
			"fortinet.fortios.fortios_system_interface": map[string]any{
				"vdom":  device.Network.VDOM,
				"state": "present",
				"system_interface": map[string]any{
					"name": "{{ item.name }}",
					"mode": "{{ item.mode }}",
					"ip":   "{{ item.ip | default(omit) }}",
				},
			},
			"loop": renderFortinetInterfaces(device.Network.Interfaces),
		})
	}
	if len(device.Network.Routes) > 0 {
		tasks = append(tasks, map[string]any{
			"name": "Render FortiGate static routes",
			"fortinet.fortios.fortios_router_static": map[string]any{
				"vdom":  device.Network.VDOM,
				"state": "present",
				"router_static": map[string]any{
					"dst":      "{{ item.destination }}",
					"gateway":  "{{ item.gateway | default(omit) }}",
					"device":   "{{ item.device }}",
					"seq_num":  "{{ item.sequence }}",
					"distance": "{{ item.distance | default(omit) }}",
				},
			},
			"loop": renderFortinetRoutes(device.Network.Routes),
		})
	}
	return tasks
}

func renderFortinetInterfaces(interfaces []domain.NetworkInterface) []any {
	out := make([]any, 0, len(interfaces))
	for _, item := range interfaces {
		entry := map[string]any{"name": item.Name, "mode": item.IPv4Mode}
		if item.IPv4Mode == "static" {
			entry["ip"] = item.IPv4Address
		}
		out = append(out, entry)
	}
	return out
}

func renderFortinetRoutes(routes []domain.StaticRoute) []any {
	out := make([]any, 0, len(routes))
	for _, route := range routes {
		entry := map[string]any{
			"destination": route.Destination,
			"device":      route.Interface,
			"sequence":    route.FortinetSequence,
		}
		if route.NextHop != "" {
			entry["gateway"] = route.NextHop
		}
		if route.Distance > 0 {
			entry["distance"] = route.Distance
		}
		out = append(out, entry)
	}
	return out
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
