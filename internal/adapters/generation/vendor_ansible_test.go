package generator

import (
	"testing"

	"infraflow/internal/domain"
)

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
		enabled, ok := entry["enabled"].(bool)
		if !ok {
			t.Fatalf("expected interface enabled to be bool, got %#v", entry["enabled"])
		}
		if !enabled {
			t.Fatalf("expected interface %q to be enabled", name)
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

func TestRenderCiscoInterfacesWithoutIPv4DoesNotInventData(t *testing.T) {
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
	if got, ok := entry["enabled"].(bool); !ok || !got {
		t.Fatalf("expected interface to be enabled, got %#v", entry["enabled"])
	}
	if _, exists := entry["ipv4"]; exists {
		t.Fatalf("expected no ipv4 field for interface without IPv4Address, got %#v", entry)
	}
	for _, forbidden := range []string{"nat", "ospf", "bgp", "route", "dhcp", "vlan"} {
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

	tasks := buildVendorTasks(device, vendorProfile{group: "cisco_iosxe", collection: "cisco.ios", family: "iosxe"})
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
		if got, ok := entry["enabled"].(bool); !ok || !got {
			t.Fatalf("expected interface %q enabled=true, got %#v", name, entry["enabled"])
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

	tasks := buildVendorTasks(device, vendorProfile{group: "cisco_iosxe", collection: "cisco.ios", family: "iosxe"})
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

	tasks := buildVendorTasks(device, vendorProfile{group: "cisco_iosxe", collection: "cisco.ios", family: "iosxe"})
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
	if got, ok := entry["enabled"].(bool); !ok || !got {
		t.Fatalf("expected explicit enabled=true for interface without IP, got %#v", entry["enabled"])
	}
	if _, exists := entry["ipv4"]; exists {
		t.Fatalf("unexpected ipv4 field invented for interface without IP: %#v", entry)
	}
	for _, forbidden := range []string{"nat", "ospf", "bgp", "route", "dhcp", "vlan"} {
		if _, exists := entry[forbidden]; exists {
			t.Fatalf("unexpected automatic %q field for interface without IP: %#v", forbidden, entry)
		}
	}
}
