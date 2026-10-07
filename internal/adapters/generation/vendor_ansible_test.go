package generator

import (
	"strings"
	"testing"

	"infraflow/internal/adapters/config"
	"infraflow/internal/domain"
)

func TestVendorAnsibleGenerationRendersCiscoRoutes(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    devices:
      - name: R1
        role: router
        vendor: cisco
        family: iosxe
        model: ios-xe
        management:
          ipv4: 192.168.90.10
        network:
          interfaces:
            - name: GigabitEthernet1
              role: wan
              ipv4_mode: static
              ipv4_address: 10.0.0.2/30
          routes:
            - destination: 0.0.0.0/0
              next_hop: 10.0.0.1
`))
	if err != nil {
		t.Fatal(err)
	}
	files, _, err := vendorAnsibleFiles(infrastructure.Sites[0], "hash")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range files {
		if strings.HasSuffix(file.path, "vendor-playbook.yml") {
			if strings.Contains(string(file.data), "ios_static_routes") && strings.Contains(string(file.data), "0.0.0.0/0") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected Cisco static route generation in vendor playbook")
	}
}

func TestVendorAnsibleGenerationRendersMikroTikRoutes(t *testing.T) {
	device := domain.Device{
		Name:       "R1",
		Vendor:     "mikrotik",
		Family:     "routeros",
		Model:      "routeros",
		Management: domain.Management{IPv4: "192.168.90.20"},
		Network: &domain.DeviceNetwork{
			Interfaces: []domain.NetworkInterface{{Name: "ether1", Role: "wan", IPv4Mode: "static", IPv4Address: "10.0.0.2/30"}},
			Routes:     []domain.StaticRoute{{Destination: "0.0.0.0/0", NextHop: "10.0.0.1"}},
		},
	}
	playbook := buildVendorTasks(device, vendorProfile{group: "mikrotik_routeros", collection: "community.routeros", family: "routeros"})
	data, err := marshal(playbook)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ip route") || !strings.Contains(string(data), "0.0.0.0/0") {
		t.Fatalf("expected MikroTik route generation in vendor task, got %s", string(data))
	}
}

func TestVendorAnsibleGenerationRendersFortinetRoutes(t *testing.T) {
	device := domain.Device{
		Name:       "FG1",
		Vendor:     "fortinet",
		Family:     "fortios",
		Model:      "fortigate",
		Management: domain.Management{IPv4: "192.168.90.30"},
		Network: &domain.DeviceNetwork{
			VDOM:       "root",
			Interfaces: []domain.NetworkInterface{{Name: "port1", Role: "wan", IPv4Mode: "static", IPv4Address: "10.0.0.2/30"}},
			Routes:     []domain.StaticRoute{{Destination: "0.0.0.0/0", NextHop: "10.0.0.1"}},
		},
	}
	playbook := buildVendorTasks(device, vendorProfile{group: "fortinet_fortios", collection: "fortinet.fortios", family: "fortios"})
	data, err := marshal(playbook)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "fortios_router_static") || !strings.Contains(string(data), "0.0.0.0/0") {
		t.Fatalf("expected FortiGate route generation in vendor task, got %s", string(data))
	}
}

func TestVendorAnsibleGenerationUsesDefaultOSPFArea10WhenNoRoutePolicySpecified(t *testing.T) {
	for _, tc := range []struct {
		name     string
		profile  vendorProfile
		contains []string
	}{
		{name: "cisco", profile: vendorProfile{group: "cisco_iosxe", collection: "cisco.ios", family: "iosxe"}, contains: []string{"ios_ospfv2", "area_id", "10"}},
		{name: "mikrotik", profile: vendorProfile{group: "mikrotik_routeros", collection: "community.routeros", family: "routeros"}, contains: []string{"routing ospf instance", "area=10", "network"}},
		{name: "fortinet", profile: vendorProfile{group: "fortinet_fortios", collection: "fortinet.fortios", family: "fortios"}, contains: []string{"fortios_router_ospf", "area_id", "10"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			device := domain.Device{
				Name:       "R1",
				Vendor:     strings.Title(tc.name),
				Family:     "default",
				Management: domain.Management{IPv4: "192.168.90.10"},
				Network: &domain.DeviceNetwork{
					Interfaces: []domain.NetworkInterface{{Name: "eth0", Role: "lan", IPv4Mode: "static", IPv4Address: "10.0.0.2/24"}},
				},
			}
			playbook := buildVendorTasks(device, tc.profile)
			data, err := marshal(playbook)
			if err != nil {
				t.Fatal(err)
			}
			for _, needle := range tc.contains {
				if !strings.Contains(string(data), needle) {
					t.Fatalf("expected default OSPF area 10 generation to include %q in %s", needle, string(data))
				}
			}
		})
	}
}
