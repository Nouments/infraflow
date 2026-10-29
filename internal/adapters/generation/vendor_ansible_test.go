package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"infraflow/internal/adapters/config"
	"infraflow/internal/domain"
	"infraflow/pkg/protocol"

	"gopkg.in/yaml.v3"
)

func TestRenderCiscoAdminStateOnlyWhenExplicitlyDeclared(t *testing.T) {
	enabled := true
	disabled := false
	tasks := renderCiscoAdminState([]domain.NetworkInterface{
		{Name: "GigabitEthernet0/0/0", Enabled: &enabled},
		{Name: "GigabitEthernet0/0/1", Enabled: &disabled},
		{Name: "GigabitEthernet0/0/2"},
	})
	if len(tasks) != 1 {
		t.Fatalf("expected one explicit admin-state task, got %#v", tasks)
	}
	task := tasks[0].(map[string]any)
	module := task["cisco.ios.ios_interfaces"].(map[string]any)
	config := module["config"].([]any)
	if len(config) != 2 {
		t.Fatalf("expected only explicitly declared interface states, got %#v", config)
	}
	if config[0].(map[string]any)["enabled"] != "enabled" || config[1].(map[string]any)["enabled"] != "disabled" {
		t.Fatalf("unexpected Cisco administrative states: %#v", config)
	}
	if got := renderCiscoAdminState([]domain.NetworkInterface{{Name: "GigabitEthernet1"}}); got != nil {
		t.Fatalf("renderer inferred undeclared administrative state: %#v", got)
	}
}

func TestRenderCiscoInterfacesMultipleInterfacesFromDeviceNetwork(t *testing.T) {
	interfaces := []domain.NetworkInterface{
		{Name: "GigabitEthernet1", Role: "management", IPv4Mode: "static", IPv4Address: "192.168.100.10/24"},
		{Name: "GigabitEthernet2", Role: "wan", IPv4Mode: "static", IPv4Address: "10.0.0.1/30"},
		{Name: "GigabitEthernet3", Role: "lan", IPv4Mode: "static", IPv4Address: "10.10.10.1/24"},
		{Name: "GigabitEthernet4", Role: "transit", IPv4Mode: "static", IPv4Address: "10.0.0.5/30"},
	}

	result := renderCiscoInterfaces(interfaces)
	if len(result) != len(interfaces) {
		t.Fatalf("expected %d interface entries, got %d", len(interfaces), len(result))
	}

	expected := map[string]string{
		"GigabitEthernet1": "192.168.100.10/24",
		"GigabitEthernet2": "10.0.0.1/30",
		"GigabitEthernet3": "10.10.10.1/24",
		"GigabitEthernet4": "10.0.0.5/30",
	}

	for _, item := range result {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected returned interface entry to be a map[string]any, got %#v", item)
		}
		name, ok := entry["name"].(string)
		if !ok {
			t.Fatalf("expected interface name to be a string, got %#v", entry["name"])
		}
		if _, exists := entry["enabled"]; exists {
			t.Fatalf("renderer invented interface admin state for %q: %#v", name, entry)
		}
		wantIP, exists := expected[name]
		if !exists {
			t.Fatalf("unexpected interface %q in renderer output", name)
		}
		ipv4Value, ok := entry["ipv4"]
		if !ok {
			t.Fatalf("expected interface %q to include ipv4 data", name)
		}
		ipv4List, ok := ipv4Value.([]any)
		if !ok || len(ipv4List) != 1 {
			t.Fatalf("expected interface %q ipv4 to be a single-entry list, got %#v", name, ipv4Value)
		}
		ipv4Map, ok := ipv4List[0].(map[string]any)
		if !ok {
			t.Fatalf("expected interface %q ipv4 entry to be a map, got %#v", name, ipv4List[0])
		}
		if got := ipv4Map["address"]; got != wantIP {
			t.Fatalf("expected interface %q ipv4.address=%q, got %#v", name, wantIP, got)
		}
	}

	for _, forbidden := range []string{"nat", "ospf", "bgp", "route", "dhcp", "vlan"} {
		for _, item := range result {
			if entry, ok := item.(map[string]any); ok {
				for key := range entry {
					if key == forbidden {
						t.Fatalf("unexpected automatic %q key in interface result for %#v", forbidden, entry)
					}
				}
			}
		}
	}
}

func TestRenderCiscoInterfacesMapsDHCPIntentWithoutInventingAddress(t *testing.T) {
	interfaces := []domain.NetworkInterface{
		{Name: "GigabitEthernet5", Role: "management", IPv4Mode: "dhcp"},
	}

	result := renderCiscoInterfaces(interfaces)
	if len(result) != 1 {
		t.Fatalf("expected one interface entry, got %d", len(result))
	}

	entry, ok := result[0].(map[string]any)
	if !ok {
		t.Fatalf("expected returned interface entry to be a map[string]any, got %#v", result[0])
	}
	if got, ok := entry["name"].(string); !ok || got != "GigabitEthernet5" {
		t.Fatalf("expected interface name to be %q, got %#v", "GigabitEthernet5", entry["name"])
	}
	ipv4, ok := entry["ipv4"].([]any)
	if !ok || len(ipv4) != 1 {
		t.Fatalf("expected DHCP configuration for the declared mode, got %#v", entry["ipv4"])
	}
	dhcp, ok := ipv4[0].(map[string]any)["dhcp"].(map[string]any)
	if !ok || dhcp["enable"] != true {
		t.Fatalf("expected explicit DHCP enablement, got %#v", ipv4[0])
	}
	for _, forbidden := range []string{"nat", "ospf", "bgp", "route", "vlan", "enabled"} {
		if _, exists := entry[forbidden]; exists {
			t.Fatalf("unexpected automatic %q field for interface without IP, got %#v", forbidden, entry)
		}
	}
}

func TestRenderCiscoInterfacesDoesNotInjectRoleBasedConfiguration(t *testing.T) {
	interfaces := []domain.NetworkInterface{
		{Name: "GigabitEthernet1", Role: "management", IPv4Mode: "static", IPv4Address: "192.168.100.10/24"},
		{Name: "GigabitEthernet2", Role: "wan", IPv4Mode: "static", IPv4Address: "10.0.0.1/30"},
		{Name: "GigabitEthernet3", Role: "lan", IPv4Mode: "static", IPv4Address: "10.10.10.1/24"},
		{Name: "GigabitEthernet4", Role: "transit", IPv4Mode: "static", IPv4Address: "10.0.0.5/30"},
	}

	result := renderCiscoInterfaces(interfaces)
	for _, item := range result {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected returned interface entry to be a map[string]any, got %#v", item)
		}
		for _, forbidden := range []string{"nat", "ospf", "bgp", "static_route", "default_route", "vlan", "dhcp"} {
			if _, exists := entry[forbidden]; exists {
				t.Fatalf("unexpected automatic %q field in result entry %#v", forbidden, entry)
			}
		}
	}
}

func TestRenderCiscoInterfacesUsesExactIPv4AddressFromInput(t *testing.T) {
	interfaces := []domain.NetworkInterface{
		{Name: "GigabitEthernet2", Role: "wan", IPv4Mode: "static", IPv4Address: "10.0.0.1/30"},
		{Name: "GigabitEthernet3", Role: "lan", IPv4Mode: "static", IPv4Address: "10.10.10.1/24"},
	}

	result := renderCiscoInterfaces(interfaces)
	if len(result) != 2 {
		t.Fatalf("expected 2 interfaces, got %d", len(result))
	}
	seen := map[string]string{}
	for _, item := range result {
		entry := item.(map[string]any)
		name := entry["name"].(string)
		ipv4Value, ok := entry["ipv4"].([]any)
		if !ok || len(ipv4Value) != 1 {
			t.Fatalf("expected interface %q to have a single ipv4 address entry, got %#v", name, entry["ipv4"])
		}
		addr := ipv4Value[0].(map[string]any)["address"]
		seen[name] = addr.(string)
	}
	if seen["GigabitEthernet2"] != "10.0.0.1/30" {
		t.Fatalf("expected exact address 10.0.0.1/30 for GigabitEthernet2, got %q", seen["GigabitEthernet2"])
	}
	if seen["GigabitEthernet3"] != "10.10.10.1/24" {
		t.Fatalf("expected exact address 10.10.10.1/24 for GigabitEthernet3, got %q", seen["GigabitEthernet3"])
	}
	for _, address := range seen {
		if address == "10.0.0.2/30" || address == "10.10.10.254/24" {
			t.Fatalf("unexpected invented address %q in renderer output", address)
		}
	}
}

func TestBuildVendorTasksRendersCiscoInterfacesIntegration(t *testing.T) {
	device := domain.Device{
		Name:   "R1",
		Vendor: "cisco",
		Family: "iosxe",
		Network: &domain.DeviceNetwork{
			Interfaces: []domain.NetworkInterface{
				{Name: "GigabitEthernet1", Role: "management", IPv4Mode: "static", IPv4Address: "192.168.100.10/24"},
				{Name: "GigabitEthernet2", Role: "wan", IPv4Mode: "static", IPv4Address: "10.0.0.1/30"},
				{Name: "GigabitEthernet3", Role: "lan", IPv4Mode: "static", IPv4Address: "10.10.10.1/24"},
				{Name: "GigabitEthernet4", Role: "transit", IPv4Mode: "static", IPv4Address: "10.0.0.5/30"},
			},
		},
	}

	tasks, err := buildVendorTasks(device, vendorProfile{group: "cisco_iosxe", collection: "cisco.ios", family: "iosxe"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) < 2 {
		t.Fatalf("expected Cisco tasks plus safety assertion, got %d", len(tasks))
	}

	configTask, ok := tasks[1].(map[string]any)
	if !ok {
		t.Fatalf("expected second task to be a map, got %#v", tasks[1])
	}
	if got := configTask["name"]; got != "Render Cisco interface configuration" {
		t.Fatalf("expected task name %q, got %#v", "Render Cisco interface configuration", got)
	}

	iosTask, ok := configTask["cisco.ios.ios_l3_interfaces"].(map[string]any)
	if !ok {
		t.Fatalf("expected cisco.ios.ios_l3_interfaces task payload, got %#v", configTask["cisco.ios.ios_l3_interfaces"])
	}
	if got := iosTask["state"]; got != "merged" {
		t.Fatalf("expected interface state to be merged, got %#v", got)
	}

	configEntries, ok := iosTask["config"].([]any)
	if !ok || len(configEntries) != 4 {
		t.Fatalf("expected 4 config entries, got %#v", iosTask["config"])
	}

	expected := map[string]string{
		"GigabitEthernet1": "192.168.100.10/24",
		"GigabitEthernet2": "10.0.0.1/30",
		"GigabitEthernet3": "10.10.10.1/24",
		"GigabitEthernet4": "10.0.0.5/30",
	}
	for _, item := range configEntries {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected config entry to be a map, got %#v", item)
		}
		name := entry["name"].(string)
		if _, exists := entry["enabled"]; exists {
			t.Fatalf("renderer invented interface admin state for %q: %#v", name, entry)
		}
		wantIP, exists := expected[name]
		if !exists {
			t.Fatalf("unexpected interface %q in generated config", name)
		}
		ipv4Value, ok := entry["ipv4"].([]any)
		if !ok || len(ipv4Value) != 1 {
			t.Fatalf("expected interface %q to have a single ipv4 entry, got %#v", name, entry["ipv4"])
		}
		addr := ipv4Value[0].(map[string]any)["address"]
		if addr != wantIP {
			t.Fatalf("expected interface %q address=%q, got %#v", name, wantIP, addr)
		}
	}
	for _, forbidden := range []string{"nat", "ospf", "bgp", "route", "dhcp", "vlan"} {
		for _, item := range configEntries {
			entry := item.(map[string]any)
			if _, exists := entry[forbidden]; exists {
				t.Fatalf("unexpected automatic %q field in generated Cisco config: %#v", forbidden, entry)
			}
		}
	}
}

func TestBuildVendorTasksOmitsCiscoInterfaceTaskWhenNoInterfaces(t *testing.T) {
	device := domain.Device{
		Name:    "R1",
		Vendor:  "cisco",
		Family:  "iosxe",
		Network: &domain.DeviceNetwork{},
	}

	tasks, err := buildVendorTasks(device, vendorProfile{group: "cisco_iosxe", collection: "cisco.ios", family: "iosxe"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected only the safety assertion when Cisco network intent is empty, got %d tasks", len(tasks))
	}
	entry, ok := tasks[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first task to be a map, got %#v", tasks[0])
	}
	if got := entry["name"]; got != "Require explicit safety opt-in" {
		t.Fatalf("expected safety assertion task, got %#v", got)
	}
	if _, exists := entry["cisco.ios.ios_l3_interfaces"]; exists {
		t.Fatalf("unexpected interface task for empty Cisco network intent: %#v", entry)
	}
}

func TestBuildVendorTasksKeepsCiscoInterfaceWithoutIPv4ButNoInventedData(t *testing.T) {
	device := domain.Device{
		Name:   "R1",
		Vendor: "cisco",
		Family: "iosxe",
		Network: &domain.DeviceNetwork{
			Interfaces: []domain.NetworkInterface{{Name: "GigabitEthernet5", Role: "management", IPv4Mode: "dhcp"}},
		},
	}

	tasks, err := buildVendorTasks(device, vendorProfile{group: "cisco_iosxe", collection: "cisco.ios", family: "iosxe"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected safety assertion plus interface task, got %d", len(tasks))
	}

	configTask := tasks[1].(map[string]any)
	iosTask := configTask["cisco.ios.ios_l3_interfaces"].(map[string]any)
	configEntries := iosTask["config"].([]any)
	if len(configEntries) != 1 {
		t.Fatalf("expected one config entry without IPv4, got %#v", iosTask["config"])
	}
	entry := configEntries[0].(map[string]any)
	if got := entry["name"]; got != "GigabitEthernet5" {
		t.Fatalf("expected interface name GigabitEthernet5, got %#v", got)
	}
	ipv4, ok := entry["ipv4"].([]any)
	if !ok || len(ipv4) != 1 {
		t.Fatalf("expected explicit DHCP configuration for interface: %#v", entry)
	}
	dhcp, ok := ipv4[0].(map[string]any)["dhcp"].(map[string]any)
	if !ok || dhcp["enable"] != true {
		t.Fatalf("expected DHCP configuration, got %#v", ipv4[0])
	}
	for _, forbidden := range []string{"nat", "ospf", "bgp", "route", "vlan", "enabled"} {
		if _, exists := entry[forbidden]; exists {
			t.Fatalf("unexpected automatic %q field for interface without IP: %#v", forbidden, entry)
		}
	}
}

func TestBuildVendorTasksRendersCiscoStaticRoutes(t *testing.T) {
	device := domain.Device{
		Name:   "R1",
		Vendor: "cisco",
		Family: "iosxe",
		Network: &domain.DeviceNetwork{
			Interfaces: []domain.NetworkInterface{{
				Name: "GigabitEthernet1", Role: "wan", IPv4Mode: "static", IPv4Address: "192.0.2.1/24",
			}},
			Routes: []domain.StaticRoute{{
				Destination: "203.0.113.0/24",
				NextHop:     "192.0.2.2",
				Interface:   "GigabitEthernet1",
				Distance:    25,
			}},
		},
	}

	tasks, err := buildVendorTasks(device, vendorProfile{group: "cisco_iosxe", collection: "cisco.ios", family: "iosxe"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected safety, interface, and route tasks; got %#v", tasks)
	}
	routeTask, ok := tasks[2].(map[string]any)
	if !ok {
		t.Fatalf("expected route task map, got %#v", tasks[2])
	}
	if routeTask["name"] != "Render Cisco static routes" {
		t.Fatalf("unexpected route task name: %#v", routeTask["name"])
	}
	iosTask, ok := routeTask["cisco.ios.ios_static_routes"].(map[string]any)
	if !ok || iosTask["state"] != "merged" {
		t.Fatalf("expected merged cisco.ios.ios_static_routes task, got %#v", routeTask)
	}
	configItems, ok := iosTask["config"].([]any)
	if !ok || len(configItems) != 1 {
		t.Fatalf("expected one route configuration, got %#v", iosTask["config"])
	}
	addressFamilies := configItems[0].(map[string]any)["address_families"].([]any)
	family := addressFamilies[0].(map[string]any)
	if family["afi"] != "ipv4" {
		t.Fatalf("expected IPv4 route family, got %#v", family["afi"])
	}
	routes := family["routes"].([]any)
	route := routes[0].(map[string]any)
	if route["dest"] != "203.0.113.0/24" {
		t.Fatalf("unexpected Cisco route destination: %#v", route["dest"])
	}
	nextHops := route["next_hops"].([]any)
	nextHop := nextHops[0].(map[string]any)
	expectedNextHop := map[string]any{
		"forward_router_address": "192.0.2.2",
		"interface":              "GigabitEthernet1",
		"distance_metric":        25,
	}
	if !reflect.DeepEqual(nextHop, expectedNextHop) {
		t.Fatalf("unexpected Cisco route next-hop: got %#v, want %#v", nextHop, expectedNextHop)
	}
}

func TestGenerateVendorAnsibleWritesCiscoVendorPlaybookArtifact(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        management:
          ipv4: 192.168.100.10
        network:
          interfaces:
            - name: GigabitEthernet1
              role: management
              ipv4_mode: static
              ipv4_address: 192.168.100.10/24
            - name: GigabitEthernet2
              role: wan
              ipv4_mode: static
              ipv4_address: 10.0.0.1/30
            - name: GigabitEthernet3
              role: lan
              ipv4_mode: static
              ipv4_address: 10.10.10.1/24
            - name: GigabitEthernet4
              role: transit
              ipv4_mode: static
              ipv4_address: 10.0.0.5/30
          routes:
            - destination: 203.0.113.0/24
              next_hop: 10.0.0.6
              interface: GigabitEthernet4
              distance: 25
`))
	if err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	if _, err := GenerateVendorAnsible(infrastructure, outDir); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(outDir, "lab", "ansible", "vendor-playbook.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated vendor playbook: %v", err)
	}

	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode generated vendor playbook: %v", err)
	}
	plays, ok := doc.([]any)
	if !ok || len(plays) == 0 {
		t.Fatalf("expected generated playbook to be a YAML list of plays, got %#v", doc)
	}
	play, ok := plays[0].(map[string]any)
	if !ok {
		t.Fatalf("expected play entry to be a map, got %#v", plays[0])
	}
	if got := play["name"]; got != "InfraFlow vendor configuration for R1" {
		t.Fatalf("expected first play name %q, got %#v", "InfraFlow vendor configuration for R1", got)
	}
	if got := play["hosts"]; got != "R1" {
		t.Fatalf("expected hosts R1, got %#v", got)
	}

	tasks, ok := play["tasks"].([]any)
	if !ok || len(tasks) < 2 {
		t.Fatalf("expected generated play to contain tasks, got %#v", play["tasks"])
	}
	configTask, ok := tasks[1].(map[string]any)
	if !ok {
		t.Fatalf("expected second task to be the Cisco config task, got %#v", tasks[1])
	}
	if got := configTask["name"]; got != "Render Cisco interface configuration" {
		t.Fatalf("expected Cisco task name %q, got %#v", "Render Cisco interface configuration", got)
	}
	iosTask, ok := configTask["cisco.ios.ios_l3_interfaces"].(map[string]any)
	if !ok {
		t.Fatalf("expected cisco.ios.ios_l3_interfaces payload, got %#v", configTask["cisco.ios.ios_l3_interfaces"])
	}
	if got := iosTask["state"]; got != "merged" {
		t.Fatalf("expected state merged, got %#v", got)
	}

	configEntries, ok := iosTask["config"].([]any)
	if !ok || len(configEntries) != 4 {
		t.Fatalf("expected 4 Cisco config entries, got %#v", iosTask["config"])
	}
	seen := map[string]string{}
	for _, item := range configEntries {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected config entry to be a map, got %#v", item)
		}
		name, _ := entry["name"].(string)
		if name == "" {
			t.Fatalf("expected config entry name to be non-empty, got %#v", entry)
		}
		if _, exists := entry["enabled"]; exists {
			t.Fatalf("renderer invented interface admin state for %q: %#v", name, entry)
		}
		if ipv4Raw, exists := entry["ipv4"]; exists {
			ipv4List, ok := ipv4Raw.([]any)
			if !ok || len(ipv4List) != 1 {
				t.Fatalf("expected single ipv4 list for %q, got %#v", name, ipv4Raw)
			}
			ipv4Map, ok := ipv4List[0].(map[string]any)
			if !ok {
				t.Fatalf("expected ipv4 list item to be a map for %q, got %#v", name, ipv4List[0])
			}
			seen[name] = ipv4Map["address"].(string)
		} else {
			seen[name] = ""
		}
		for _, forbidden := range []string{"nat", "ospf", "bgp", "route", "dhcp", "vlan"} {
			if _, exists := entry[forbidden]; exists {
				t.Fatalf("unexpected automatic %q field in generated artifact: %#v", forbidden, entry)
			}
		}
	}
	for expectedName, expectedAddr := range map[string]string{
		"GigabitEthernet1": "192.168.100.10/24",
		"GigabitEthernet2": "10.0.0.1/30",
		"GigabitEthernet3": "10.10.10.1/24",
		"GigabitEthernet4": "10.0.0.5/30",
	} {
		if seen[expectedName] != expectedAddr {
			t.Fatalf("expected %q=%q in generated artifact, got %q", expectedName, expectedAddr, seen[expectedName])
		}
	}

	if len(tasks) != 3 {
		t.Fatalf("expected safety, interface, and route tasks in generated playbook; got %#v", tasks)
	}
	routeTask, ok := tasks[2].(map[string]any)
	if !ok || routeTask["name"] != "Render Cisco static routes" {
		t.Fatalf("expected Cisco static route task in generated playbook, got %#v", tasks[2])
	}
	staticRoutes, ok := routeTask["cisco.ios.ios_static_routes"].(map[string]any)
	if !ok || staticRoutes["state"] != "merged" {
		t.Fatalf("expected merged Cisco static route payload, got %#v", routeTask)
	}
	routeConfig := staticRoutes["config"].([]any)[0].(map[string]any)
	routeFamily := routeConfig["address_families"].([]any)[0].(map[string]any)
	generatedRoute := routeFamily["routes"].([]any)[0].(map[string]any)
	if generatedRoute["dest"] != "203.0.113.0/24" {
		t.Fatalf("expected canonical Cisco route destination in generated YAML, got %#v", generatedRoute["dest"])
	}
	generatedNextHop := generatedRoute["next_hops"].([]any)[0].(map[string]any)
	if generatedNextHop["forward_router_address"] != "10.0.0.6" ||
		generatedNextHop["interface"] != "GigabitEthernet4" ||
		generatedNextHop["distance_metric"] != 25 {
		t.Fatalf("unexpected generated Cisco static route next-hop: %#v", generatedNextHop)
	}
}

func TestGenerateVendorAnsibleWritesOnlyTraceableVendorArtifacts(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        model: csr1000v
        provisioning:
          method: ssh
        network:
          interfaces:
            - name: GigabitEthernet1
              role: lan
              ipv4_mode: static
              ipv4_address: 192.0.2.1/24
`))
	if err != nil {
		t.Fatal(err)
	}

	outputDirectory := t.TempDir()
	artifacts, err := GenerateVendorAnsible(infrastructure, outputDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 4 {
		t.Fatalf("expected exactly four vendor artifacts, got %#v", artifacts)
	}
	wantPaths := map[string]string{
		"lab/ansible/vendor-inventory.yml": "ansible_vendor_inventory",
		"lab/ansible/vendor-playbook.yml":  "ansible_playbook",
		"lab/ansible/requirements.yml":     "ansible_requirements",
		"lab/ansible/vendor-template.json": "ansible_vendor_manifest",
	}
	for _, artifact := range artifacts {
		if wantType, exists := wantPaths[artifact.Path]; !exists || wantType != artifact.Type {
			t.Fatalf("unexpected artifact metadata: %#v", artifact)
		}
		data, err := os.ReadFile(filepath.Join(outputDirectory, filepath.FromSlash(artifact.Path)))
		if err != nil {
			t.Fatalf("generated artifact %q is missing: %v", artifact.Path, err)
		}
		if got := protocol.SHA256(data); got != artifact.OutputHash {
			t.Fatalf("artifact %q output hash mismatch: got %s want %s", artifact.Path, got, artifact.OutputHash)
		}
		delete(wantPaths, artifact.Path)
	}
	if len(wantPaths) != 0 {
		t.Fatalf("vendor artifacts missing: %#v", wantPaths)
	}
	if _, err := os.Stat(filepath.Join(outputDirectory, "lab", "inventory.json")); !os.IsNotExist(err) {
		t.Fatalf("targeted generation wrote an unrelated inventory: %v", err)
	}
	manifestData, err := os.ReadFile(filepath.Join(outputDirectory, "lab", "ansible", "vendor-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest vendorTemplateManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.TemplateVersion != TemplateVersion || manifest.Status != "UNVERIFIED" || manifest.Methods["R1"] != "ssh" {
		t.Fatalf("generated manifest overstated provenance: %#v", manifest)
	}
}

func TestGenerateVendorAnsibleWritesCiscoDHCPIntentWithoutInventingAddress(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        network:
          interfaces:
            - name: GigabitEthernet5
              role: management
              ipv4_mode: dhcp
`))
	if err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	if _, err := GenerateVendorAnsible(infrastructure, outDir); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(outDir, "lab", "ansible", "vendor-playbook.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated vendor playbook: %v", err)
	}

	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode generated vendor playbook: %v", err)
	}
	plays := doc.([]any)
	play := plays[0].(map[string]any)
	tasks := play["tasks"].([]any)
	configTask := tasks[1].(map[string]any)
	iosTask := configTask["cisco.ios.ios_l3_interfaces"].(map[string]any)
	configEntries := iosTask["config"].([]any)
	if len(configEntries) != 1 {
		t.Fatalf("expected 1 config entry for interface without IPv4, got %#v", iosTask["config"])
	}
	entry := configEntries[0].(map[string]any)
	if got := entry["name"]; got != "GigabitEthernet5" {
		t.Fatalf("expected interface name GigabitEthernet5, got %#v", got)
	}
	ipv4, ok := entry["ipv4"].([]any)
	if !ok || len(ipv4) != 1 {
		t.Fatalf("expected declared DHCP mode to produce one DHCP entry, got %#v", entry)
	}
	dhcp, ok := ipv4[0].(map[string]any)["dhcp"].(map[string]any)
	if !ok || dhcp["enable"] != true {
		t.Fatalf("expected explicit DHCP configuration, got %#v", ipv4[0])
	}
	for _, forbidden := range []string{"nat", "ospf", "bgp", "route", "vlan", "enabled"} {
		if _, exists := entry[forbidden]; exists {
			t.Fatalf("unexpected automatic %q field in artifact for GigabitEthernet5: %#v", forbidden, entry)
		}
	}
}

func TestRenderFortinetTasksUsesCollectionSchemaAndExplicitRouteInterface(t *testing.T) {
	device := domain.Device{
		Name:   "FG1",
		Vendor: "fortinet",
		Family: "fortios",
		Network: &domain.DeviceNetwork{
			VDOM: "root",
			Interfaces: []domain.NetworkInterface{{
				Name: "port2", IPv4Mode: "static", IPv4Address: "192.0.2.1/24",
			}},
			Routes: []domain.StaticRoute{{
				Destination: "198.51.100.0/24", NextHop: "192.0.2.254", Interface: "port2",
				Distance: 25, FortinetSequence: 17,
			}},
		},
	}

	tasks := renderFortinetTasks(device)
	if len(tasks) != 2 {
		t.Fatalf("expected interface and route tasks, got %#v", tasks)
	}

	interfaceTask, ok := tasks[0].(map[string]any)
	if !ok {
		t.Fatalf("expected interface task map, got %#v", tasks[0])
	}
	interfaceModule, ok := interfaceTask["fortinet.fortios.fortios_system_interface"].(map[string]any)
	if !ok {
		t.Fatalf("expected FortiOS interface module arguments, got %#v", interfaceTask)
	}
	interfaceConfig, ok := interfaceModule["system_interface"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested system_interface payload, got %#v", interfaceModule)
	}
	if interfaceConfig["name"] != "{{ item.name }}" || interfaceConfig["mode"] != "{{ item.mode }}" || interfaceConfig["ip"] != "{{ item.ip | default(omit) }}" {
		t.Fatalf("interface payload does not map explicit input fields: %#v", interfaceConfig)
	}
	if _, exists := interfaceConfig["allowaccess"]; exists {
		t.Fatalf("renderer invented FortiGate management access: %#v", interfaceConfig)
	}
	if _, exists := interfaceTask["loop"]; !exists {
		t.Fatalf("expected Ansible loop at task level, got %#v", interfaceTask)
	}
	if _, exists := interfaceModule["with_items"]; exists {
		t.Fatalf("Ansible loop was incorrectly nested in module arguments: %#v", interfaceModule)
	}

	routeTask, ok := tasks[1].(map[string]any)
	if !ok {
		t.Fatalf("expected route task map, got %#v", tasks[1])
	}
	routeModule, ok := routeTask["fortinet.fortios.fortios_router_static"].(map[string]any)
	if !ok {
		t.Fatalf("expected FortiOS route module arguments, got %#v", routeTask)
	}
	routeConfig, ok := routeModule["router_static"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested router_static payload, got %#v", routeModule)
	}
	if routeConfig["dst"] != "{{ item.destination }}" || routeConfig["gateway"] != "{{ item.gateway | default(omit) }}" || routeConfig["device"] != "{{ item.device }}" {
		t.Fatalf("route payload does not map explicit input fields: %#v", routeConfig)
	}
	if routeConfig["seq_num"] != "{{ item.sequence }}" || routeConfig["distance"] != "{{ item.distance | default(omit) }}" {
		t.Fatalf("route sequence or distance is not mapped from input: %#v", routeConfig)
	}
	if _, exists := routeTask["loop"]; !exists {
		t.Fatalf("expected route loop at task level, got %#v", routeTask)
	}
	if _, exists := routeModule["with_items"]; exists {
		t.Fatalf("Ansible loop was incorrectly nested in route module arguments: %#v", routeModule)
	}
}

func TestGenerateVendorAnsibleRejectsMethodIncompatibleWithCollection(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        provisioning:
          method: netconf
        network:
          interfaces:
            - name: GigabitEthernet1
              role: lan
              ipv4_mode: static
              ipv4_address: 192.0.2.1/24
`))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := GenerateVendorAnsible(infrastructure, t.TempDir()); err == nil {
		t.Fatal("expected Ansible IOS L3 renderer to reject NETCONF, which its network_cli module does not use")
	}
}

func TestVendorProfilesGenerateDeterministicValidYAMLWithExplicitMethods(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		method         string
		connection     string
		networkOS      string
		moduleRequired string
	}{
		{
			name: "cisco iosxe",
			input: `sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        provisioning:
          method: ssh
        network:
          interfaces:
            - name: GigabitEthernet1
              role: lan
              ipv4_mode: static
              ipv4_address: 192.0.2.1/24
`,
			method: "ssh", connection: "ansible.netcommon.network_cli", networkOS: "cisco.ios.ios",
			moduleRequired: "cisco.ios.ios_l3_interfaces",
		},
		{
			name: "mikrotik routeros",
			input: `sites:
  - name: lab
    devices:
      - name: R1
        vendor: mikrotik
        family: routeros
        provisioning:
          method: api
        management:
          ipv4: 192.0.2.10
        network:
          interfaces:
            - name: ether1
              role: lan
              ipv4_mode: static
              ipv4_address: 192.0.2.1/24
`,
			method: "api", moduleRequired: "community.routeros.api_modify",
		},
		{
			name: "fortigate fortios",
			input: `sites:
  - name: lab
    devices:
      - name: FG1
        vendor: fortigate
        family: fortios
        provisioning:
          method: https
        management:
          ipv4: 192.0.2.20
        network:
          vdom: root
          interfaces:
            - name: port2
              role: lan
              ipv4_mode: static
              ipv4_address: 192.0.2.1/24
`,
			method: "https", connection: "ansible.netcommon.httpapi", networkOS: "fortinet.fortios.fortios",
			moduleRequired: "fortinet.fortios.fortios_system_interface",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			infrastructure, err := config.Parse([]byte(test.input))
			if err != nil {
				t.Fatal(err)
			}
			firstDirectory, secondDirectory := t.TempDir(), t.TempDir()
			first, err := GenerateVendorAnsible(infrastructure, firstDirectory)
			if err != nil {
				t.Fatal(err)
			}
			second, err := GenerateVendorAnsible(infrastructure, secondDirectory)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(first, second) || len(first) != 4 {
				t.Fatalf("vendor generation is not deterministic or complete: %#v %#v", first, second)
			}

			var inventory map[string]any
			inventoryData, err := os.ReadFile(filepath.Join(firstDirectory, "lab", "ansible", "vendor-inventory.yml"))
			if err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal(inventoryData, &inventory); err != nil {
				t.Fatalf("generated inventory is not valid YAML: %v", err)
			}
			var playbook []any
			playbookData, err := os.ReadFile(filepath.Join(firstDirectory, "lab", "ansible", "vendor-playbook.yml"))
			if err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal(playbookData, &playbook); err != nil || len(playbook) != 1 {
				t.Fatalf("generated vendor playbook is not valid YAML: plays=%#v err=%v", playbook, err)
			}
			if !strings.Contains(string(playbookData), test.moduleRequired) {
				t.Fatalf("playbook omitted profile module %q: %s", test.moduleRequired, playbookData)
			}
			for _, safetyMarker := range []string{"ansible.builtin.assert", "ansible_check_mode", "INFRAFLOW_APPLY"} {
				if !strings.Contains(string(playbookData), safetyMarker) {
					t.Fatalf("vendor playbook omitted mutation guard marker %q", safetyMarker)
				}
			}

			all := inventory["all"].(map[string]any)
			children := all["children"].(map[string]any)
			network := children["network"].(map[string]any)
			hosts := network["hosts"].(map[string]any)
			var host map[string]any
			for _, value := range hosts {
				host = value.(map[string]any)
			}
			if host["infraflow_method"] != test.method {
				t.Fatalf("inventory method = %#v, want %q", host["infraflow_method"], test.method)
			}
			if test.connection != "" && (host["ansible_connection"] != test.connection || host["ansible_network_os"] != test.networkOS) {
				t.Fatalf("inventory connection contract is wrong: %#v", host)
			}
			if _, exists := host["ansible_password"]; exists {
				t.Fatalf("inventory must not contain credentials: %#v", host)
			}
			for _, artifact := range first {
				data, err := os.ReadFile(filepath.Join(firstDirectory, filepath.FromSlash(artifact.Path)))
				if err != nil || protocol.SHA256(data) != artifact.OutputHash {
					t.Fatalf("artifact %s hash mismatch: %v", artifact.Path, err)
				}
			}
		})
	}
}
