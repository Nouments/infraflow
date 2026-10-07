package generator

import (
	"strings"
	"testing"

	"infraflow/internal/adapters/config"
	"infraflow/internal/domain"
)

func TestVendorAnsibleGenerationRendersCiscoInterfacesFromDeviceNetworkOnly(t *testing.T) {
	device := domain.Device{
		Name:   "R1",
		Vendor: "cisco",
		Family: "iosxe",
		Network: &domain.DeviceNetwork{
			Interfaces: []domain.NetworkInterface{
				{Name: "GigabitEthernet2", Role: "wan", IPv4Mode: "static", IPv4Address: "10.0.0.1/30"},
				{Name: "GigabitEthernet3", Role: "lan", IPv4Mode: "static", IPv4Address: "10.10.10.1/24"},
			},
		},
	}
	playbook := buildVendorTasks(device, vendorProfile{group: "cisco_iosxe", collection: "cisco.ios", family: "iosxe"})
	data, err := marshal(playbook)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "GigabitEthernet2") || !strings.Contains(text, "10.0.0.1/30") || !strings.Contains(text, "GigabitEthernet3") || !strings.Contains(text, "10.10.10.1/24") {
		t.Fatalf("expected Cisco interface rendering from DeviceNetwork.Interfaces, got %s", text)
	}
	if strings.Contains(strings.ToLower(text), "ospf") {
		t.Fatalf("expected no automatic OSPF rendering in interface-only step, got %s", text)
	}
}

func TestVendorAnsibleGenerationRejectsInvalidInterfaceCIDRAndName(t *testing.T) {
	for _, input := range []string{
		`sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        network:
          interfaces:
            - name: GigabitEthernet1
              role: wan
              ipv4_mode: static
              ipv4_address: 10.0.0.1/99
`,
		`sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        network:
          interfaces:
            - name: ""
              role: wan
              ipv4_mode: static
              ipv4_address: 10.0.0.1/30
`,
	} {
		_, err := config.Parse([]byte(input))
		if err == nil {
			t.Fatalf("expected invalid Cisco interface input to be rejected: %s", input)
		}
	}
}
