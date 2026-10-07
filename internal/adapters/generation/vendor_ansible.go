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

func renderCiscoTasks(device domain.Device) []any {
	tasks := make([]any, 0, 5)
	if len(device.Network.Interfaces) > 0 {
		tasks = append(tasks, map[string]any{
			"name": "Render Cisco interface configuration",
			"cisco.ios.ios_l3_interfaces": map[string]any{
				"config": renderCiscoInterfaces(device.Network.Interfaces),
				"state":  "merged",
			},
		})
	}
	if len(device.Network.Routes) > 0 {
		tasks = append(tasks, map[string]any{
			"name": "Render Cisco static routes",
			"cisco.ios.ios_static_routes": map[string]any{
				"config": renderCiscoRoutes(device.Network.Routes),
				"state":  "merged",
			},
		})
	} else if shouldRenderDefaultOSPF(device.Network) {
		tasks = append(tasks, map[string]any{
			"name": "Render Cisco default OSPF routing",
			"cisco.ios.ios_ospfv2": map[string]any{
				"config": renderCiscoOSPF(device),
				"state":  "merged",
			},
		})
	}
	return tasks
}

func renderCiscoInterfaces(interfaces []domain.NetworkInterface) []any {
	out := make([]any, 0, len(interfaces))
	for _, item := range interfaces {
		entry := map[string]any{"name": item.Name, "enabled": true}
		if item.IPv4Address != "" {
			entry["ipv4"] = []any{map[string]any{"address": item.IPv4Address}}
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

func renderCiscoOSPF(device domain.Device) []any {
	network := device.Network
	areaID := defaultRoutingArea(network)
	routerID := device.Management.IPv4
	if routerID == "" {
		routerID = "192.168.90.10"
	}
	return []any{map[string]any{
		"process_id": 1,
		"router_id": routerID,
		"areas":      []any{map[string]any{"area_id": areaID, "type": "normal"}},
		"networks":   renderOSPFNetworks(network.Interfaces, areaID),
	}}
}

func renderMikroTikTasks(device domain.Device) []any {
	tasks := make([]any, 0, 5)
	if len(device.Network.Interfaces) > 0 {
		tasks = append(tasks, map[string]any{
			"name": "Render MikroTik interface addresses",
			"community.routeros.api_modify": map[string]any{
				"hostname": device.Management.IPv4,
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
				"path":     "ip route",
				"data":     renderMikroTikRoutes(device.Network.Routes),
			},
		})
	} else if shouldRenderDefaultOSPF(device.Network) {
		tasks = append(tasks, map[string]any{
			"name": "Render MikroTik default OSPF routing",
			"community.routeros.api_modify": map[string]any{
				"hostname": device.Management.IPv4,
				"path":     "routing ospf instance",
				"data":     renderMikroTikOSPF(device),
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
		out = append(out, map[string]any{"dst": route.Destination, "gateway": route.NextHop})
	}
	return out
}

func renderMikroTikOSPF(device domain.Device) map[string]any {
	network := device.Network
	areaID := defaultRoutingArea(network)
	routerID := device.Management.IPv4
	if routerID == "" {
		routerID = "192.168.90.10"
	}
	return map[string]any{
		"name":      "default",
		"router-id": routerID,
		"area":      fmt.Sprintf("area=%d", areaID),
		"network":   renderOSPFNetworks(network.Interfaces, areaID),
	}
}

func renderFortinetTasks(device domain.Device) []any {
	tasks := make([]any, 0, 5)
	if len(device.Network.Interfaces) > 0 {
		tasks = append(tasks, map[string]any{
			"name": "Render FortiGate interface addresses",
			"fortinet.fortios.fortios_system_interface": map[string]any{
				"vdom":        device.Network.VDOM,
				"state":       "present",
				"name":        "{{ item.name }}",
				"ip":          "{{ item.ipv4_address }}",
				"allowaccess": "ping",
				"with_items":  renderFortinetInterfaces(device.Network.Interfaces),
			},
		})
	}
	if len(device.Network.Routes) > 0 {
		tasks = append(tasks, map[string]any{
			"name": "Render FortiGate static routes",
			"fortinet.fortios.fortios_router_static": map[string]any{
				"vdom":        device.Network.VDOM,
				"state":       "present",
				"destination": "{{ item.destination }}",
				"gateway":     "{{ item.gateway }}",
				"device":      "{{ item.device }}",
				"with_items":  renderFortinetRoutes(device.Network.Routes),
			},
		})
	} else if shouldRenderDefaultOSPF(device.Network) {
		routerID := device.Management.IPv4
		if routerID == "" {
			routerID = "192.168.90.10"
		}
		tasks = append(tasks, map[string]any{
			"name": "Render FortiGate default OSPF routing",
			"fortinet.fortios.fortios_router_ospf": map[string]any{
				"vdom":      device.Network.VDOM,
				"state":     "present",
				"router_id": routerID,
				"areas":     renderFortinetOSPFAreas(device.Network),
			},
		})
	}
	return tasks
}

func renderFortinetInterfaces(interfaces []domain.NetworkInterface) []any {
	out := make([]any, 0, len(interfaces))
	for _, item := range interfaces {
		if item.IPv4Address == "" {
			continue
		}
		out = append(out, map[string]any{"name": item.Name, "ipv4_address": item.IPv4Address})
	}
	return out
}

func renderFortinetRoutes(routes []domain.StaticRoute) []any {
	out := make([]any, 0, len(routes))
	for _, route := range routes {
		out = append(out, map[string]any{"destination": route.Destination, "gateway": route.NextHop, "device": "port1"})
	}
	return out
}

func renderFortinetOSPFAreas(network *domain.DeviceNetwork) []any {
	areaID := defaultRoutingArea(network)
	interfaces := make([]any, 0, len(network.Interfaces))
	for _, iface := range network.Interfaces {
		if iface.IPv4Address == "" {
			continue
		}
		interfaces = append(interfaces, map[string]any{
			"name":    iface.Name,
			"area_id": areaID,
			"network": iface.IPv4Address,
		})
	}
	return []any{map[string]any{"area_id": areaID, "interface": interfaces}}
}

func shouldRenderDefaultOSPF(network *domain.DeviceNetwork) bool {
	if network == nil {
		return false
	}
	if len(network.Routes) > 0 {
		return false
	}
	if strings.TrimSpace(network.RoutingProtocol) == "" {
		return len(network.Interfaces) > 0
	}
	return strings.EqualFold(network.RoutingProtocol, "ospf")
}

func defaultRoutingArea(network *domain.DeviceNetwork) int {
	if network == nil {
		return 10
	}
	if network.RoutingArea != 0 {
		return network.RoutingArea
	}
	return 10
}

func renderOSPFNetworks(interfaces []domain.NetworkInterface, areaID int) []any {
	out := make([]any, 0, len(interfaces))
	for _, iface := range interfaces {
		if iface.IPv4Address == "" {
			continue
		}
		out = append(out, map[string]any{
			"name":    iface.Name,
			"prefix":  iface.IPv4Address,
			"area_id": areaID,
		})
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
