package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"infraflow/internal/adapters/config"
	"infraflow/internal/domain"
	"infraflow/pkg/protocol"
)

func TestGenerateNativeRendersSupportedIntent(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    bootstrap:
      network: 192.0.2.0/24
      gateway: 192.0.2.254
    devices:
      - name: router1
        id: edge-west-1
        vendor: cisco
        family: iosxe
        model: C8000V
        os_version: "17.12.4"
        network:
          interfaces:
            - name: GigabitEthernet1
              role: lan
              ipv4_mode: static
              ipv4_address: 192.0.2.1/24
              enabled: true
            - name: GigabitEthernet2
              role: wan
              ipv4_mode: dhcp
          routes:
            - destination: 198.51.100.0/24
              next_hop: 192.0.2.254
              interface: GigabitEthernet1
              distance: 25
      - name: router2
        vendor: mikrotik
        family: routeros
        model: RB5009UG+S+
        os_version: "7.x"
        network:
          interfaces:
            - name: ether1
              role: wan
              ipv4_mode: static
              ipv4_address: 192.0.2.2/24
          routes:
            - destination: 198.51.100.0/24
              next_hop: 192.0.2.254
              distance: 3
      - name: firewall1
        vendor: fortinet
        family: fortios
        model: FG-60F
        os_version: "7.4.5"
        network:
          vdom: root
          interfaces:
            - name: port1
              role: wan
              ipv4_mode: static
              ipv4_address: 192.0.2.3/24
              enabled: false
          routes:
            - destination: 198.51.100.0/24
              next_hop: 192.0.2.254
              interface: port1
              distance: 7
              fortinet_sequence: 10
`))
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	artifacts, err := GenerateNative(infrastructure, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 4 {
		t.Fatalf("expected three configs and one manifest, got %#v", artifacts)
	}

	forti := readNativeArtifact(t, root, "lab/native/device-firewall1.cfg")
	cisco := readNativeArtifact(t, root, "lab/native/device-edge-west-1.cfg")
	mikrotik := readNativeArtifact(t, root, "lab/native/device-router2.cfg")
	expected := map[string]string{
		"Cisco IOS-XE": `hostname router1
interface GigabitEthernet1
 ip address 192.0.2.1 255.255.255.0
 no shutdown
!
interface GigabitEthernet2
 ip address dhcp
!
ip route 198.51.100.0 255.255.255.0 GigabitEthernet1 192.0.2.254 25
`,
		"MikroTik RouterOS 7": `/system identity set name="router2"
/ip address add address=192.0.2.2/24 interface="ether1"
/ip route add dst-address=198.51.100.0/24 gateway="192.0.2.254" distance=3
`,
		"FortiOS": `config global
    config system global
        set hostname firewall1
    end
end
config vdom
    edit root
        config system interface
            edit "port1"
                set mode static
                set ip 192.0.2.3 255.255.255.0
                set status down
            next
        end
        config router static
            edit 10
                set dst 198.51.100.0 255.255.255.0
                set gateway 192.0.2.254
                set device "port1"
                set distance 7
            next
        end
    next
end`,
	}
	for vendor, got := range map[string]string{
		"Cisco IOS-XE": cisco, "MikroTik RouterOS 7": mikrotik, "FortiOS": forti,
	} {
		if got != expected[vendor] {
			t.Errorf("%s output differs from expected: got=%q want=%q", vendor, got, expected[vendor])
		}
	}
	if strings.Contains(forti, "password") || strings.Contains(mikrotik, ".auto.rsc") {
		t.Fatal("native output included a secret field or RouterOS auto-execution filename")
	}

	var manifest nativeManifest
	data := readNativeArtifact(t, root, "lab/native/native-manifest.json")
	if err := json.Unmarshal([]byte(data), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Status != "rendered_unverified" || len(manifest.Warnings) == 0 || len(manifest.Devices) != 3 ||
		manifest.ConfigSyntaxValidation.Name != configSyntaxValidationName ||
		manifest.ConfigSyntaxValidation.Status != configSyntaxValidationUnavailable {
		t.Fatalf("native manifest must disclose unverified status and limits: %#v", manifest)
	}
	for _, device := range manifest.Devices {
		if device.ConfigSyntaxValidation.Name != configSyntaxValidationName ||
			device.ConfigSyntaxValidation.Status != configSyntaxValidationUnavailable ||
			!strings.Contains(device.ConfigSyntaxValidation.Message, "No offline syntax parser is registered") {
			t.Errorf("native report did not disclose parser availability for %s: %#v", device.Name, device.ConfigSyntaxValidation)
		}
	}
	if manifest.Devices[0].Compatibility != "unknown" || manifest.Devices[0].Model != "FG-60F" || manifest.Devices[0].OSVersion != "7.4.5" {
		t.Fatalf("native manifest must preserve target identity without claiming compatibility: %#v", manifest.Devices[0])
	}
	if manifest.Devices[0].Vendor != "fortinet" || manifest.Devices[0].TemplateID != "infraflow.fortinet.fortios.native" {
		t.Fatalf("native manifest must identify the renderer provenance: %#v", manifest.Devices[0])
	}
}

func TestGenerateNativeRequiresExactCiscoIOSAutoInstallProfile(t *testing.T) {
	device := domain.Device{
		Name: "edge7200", Vendor: "cisco", Family: "ios", Model: "Cisco 7200", OSVersion: "15.3(3)XB12",
		Provisioning: domain.Provisioning{Method: domain.ProvisioningMethodCiscoAutoInstall},
		Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{
			Name: "FastEthernet0/0", Role: "management", IPv4Mode: "static", IPv4Address: "192.0.2.10/24",
		}}},
	}
	root := t.TempDir()
	artifacts, err := GenerateNative(domain.Infrastructure{Sites: []domain.Site{{Name: "lab", Devices: []domain.Device{device}}}}, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("expected native config and manifest, got %#v", artifacts)
	}
	configuration := readNativeArtifact(t, root, "lab/native/device-edge7200.cfg")
	if !strings.Contains(configuration, "hostname edge7200") || !strings.Contains(configuration, "interface FastEthernet0/0") || !strings.Contains(configuration, "ip address 192.0.2.10 255.255.255.0") {
		t.Fatalf("unexpected Cisco IOS native output: %s", configuration)
	}
	var manifest nativeManifest
	if err := json.Unmarshal([]byte(readNativeArtifact(t, root, "lab/native/native-manifest.json")), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Devices) != 1 || manifest.Devices[0].Family != "ios" || manifest.Devices[0].Model != "Cisco 7200" || manifest.Devices[0].OSVersion != "15.3(3)XB12" || manifest.Devices[0].ProvisioningMethod != string(domain.ProvisioningMethodCiscoAutoInstall) || manifest.Devices[0].CompatibilityStatus != "unknown" {
		t.Fatalf("profile provenance or unverified compatibility status missing: %#v", manifest.Devices)
	}

	for _, invalid := range []domain.Device{
		{Name: device.Name, Vendor: device.Vendor, Family: device.Family, Model: "7200", OSVersion: "15.3(3)XB13", Provisioning: device.Provisioning, Network: device.Network},
		{Name: device.Name, Vendor: device.Vendor, Family: device.Family, Model: "7200", OSVersion: device.OSVersion, Provisioning: domain.Provisioning{Method: domain.ProvisioningMethodNativeConfig}, Network: device.Network},
	} {
		if _, err := GenerateNative(domain.Infrastructure{Sites: []domain.Site{{Name: "lab", Devices: []domain.Device{invalid}}}}, filepath.Join(t.TempDir(), "out")); err == nil {
			t.Fatalf("unsupported Cisco IOS AutoInstall identity was accepted: %#v", invalid)
		}
	}
}

func TestGenerateNativeSelectsExactCiscoIOSvAutoInstallTemplate(t *testing.T) {
	device := domain.Device{
		Name: "r1", Vendor: "cisco", Family: "ios", Model: "IOSv", OSVersion: domain.CiscoIOSvVersion,
		Provisioning: domain.Provisioning{Method: domain.ProvisioningMethodCiscoAutoInstall},
		Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{
			Name: "GigabitEthernet0/0", Role: "management", IPv4Mode: "static", IPv4Address: "192.0.2.11/24",
		}}},
	}
	root := t.TempDir()
	if _, err := GenerateNative(domain.Infrastructure{Sites: []domain.Site{{Name: "lab", Devices: []domain.Device{device}}}}, root); err != nil {
		t.Fatal(err)
	}
	configuration := readNativeArtifact(t, root, "lab/native/device-r1.cfg")
	if !strings.Contains(configuration, "hostname r1") || !strings.Contains(configuration, "interface GigabitEthernet0/0") || !strings.Contains(configuration, "ip address 192.0.2.11 255.255.255.0") {
		t.Fatalf("unexpected IOSv output: %s", configuration)
	}
	var manifest nativeManifest
	if err := json.Unmarshal([]byte(readNativeArtifact(t, root, "lab/native/native-manifest.json")), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Devices) != 1 || manifest.Devices[0].TemplateID != "infraflow.cisco.ios.iosv.native" {
		t.Fatalf("IOSv must resolve its own versioned template, got %#v", manifest.Devices)
	}
	// Releases are not shared between models: the IOSv release is not a 7200
	// release and the 7200 releases are not IOSv releases.
	for _, invalid := range []struct{ model, version string }{
		{"7200", domain.CiscoIOSvVersion},
		{"iosv", domain.CiscoIOS7200LabVerifiedVersion},
		{"iosv", "15.9(3)M8"},
	} {
		candidate := device
		candidate.Model, candidate.OSVersion = invalid.model, invalid.version
		if _, err := GenerateNative(domain.Infrastructure{Sites: []domain.Site{{Name: "lab", Devices: []domain.Device{candidate}}}}, filepath.Join(t.TempDir(), "out")); err == nil {
			t.Fatalf("model %q release %q was accepted without an exact profile", invalid.model, invalid.version)
		}
	}
}

func TestFortiOSExampleMatchesExpectedConfiguration(t *testing.T) {
	examplePath := filepath.Join("..", "..", "..", "examples", "fortios-native.yaml")
	data, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	infrastructure, err := config.Parse(data)
	if err != nil {
		t.Fatalf("parse FortiOS example: %v", err)
	}
	root := t.TempDir()
	if _, err := GenerateNative(infrastructure, root); err != nil {
		t.Fatalf("generate FortiOS example: %v", err)
	}
	got := readNativeArtifact(t, root, "fortios-lab/native/device-FG60F.cfg")
	want := `config global
    config system global
        set hostname FG60F
    end
end
config vdom
    edit root
        config system interface
            edit "port1"
                set mode static
                set ip 192.0.2.1 255.255.255.0
                set status up
            next
        end
        config router static
            edit 10
                set dst 198.51.100.0 255.255.255.0
                set gateway 192.0.2.254
                set device "port1"
                set distance 7
            next
        end
    next
end`
	if got != want {
		t.Fatalf("FortiOS example output differs from expected: got=%q want=%q", got, want)
	}
}

func TestGenerateNativeRejectsUnsupportedIntent(t *testing.T) {
	enabled := true
	tests := []struct {
		name        string
		device      domain.Device
		wantMessage string
	}{
		{
			name: "unsupported NAT",
			device: domain.Device{
				Name: "router1", Vendor: "cisco", Family: "iosxe",
				Network: &domain.DeviceNetwork{
					Interfaces: []domain.NetworkInterface{
						{Name: "GigabitEthernet1", Role: "lan", IPv4Mode: "static", IPv4Address: "192.0.2.1/24"},
						{Name: "GigabitEthernet2", Role: "wan", IPv4Mode: "static", IPv4Address: "198.51.100.1/24"},
					},
					NAT: domain.NATConfig{Source: []domain.SourceNATRule{{
						Name: "snat", SourceCIDR: "192.0.2.0/24", IngressInterface: "GigabitEthernet1",
						EgressInterface: "GigabitEthernet2", Mode: "interface",
					}}},
				},
			},
			wantMessage: "does not support NAT intent",
		},
		{
			name: "RouterOS version must be explicit",
			device: domain.Device{
				Name: "router2", Vendor: "mikrotik", Family: "routeros",
				Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{
					{Name: "ether1", Role: "lan", IPv4Mode: "static", IPv4Address: "192.0.2.2/24"},
				}},
			},
			wantMessage: "requires an explicit RouterOS 7 version",
		},
		{
			name: "RouterOS version syntax must be valid",
			device: domain.Device{
				Name: "router2", Vendor: "mikrotik", Family: "routeros", OSVersion: "7..12",
				Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{
					{Name: "ether1", Role: "lan", IPv4Mode: "static", IPv4Address: "192.0.2.2/24"},
				}},
			},
			wantMessage: "requires an explicit RouterOS 7 version",
		},
		{
			name: "RouterOS physical admin state is not guessed",
			device: domain.Device{
				Name: "router3", Vendor: "mikrotik", Family: "routeros", OSVersion: "7.12.1",
				Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{
					{Name: "ether1", Role: "lan", IPv4Mode: "static", IPv4Address: "192.0.2.2/24", Enabled: &enabled},
				}},
			},
			wantMessage: "cannot apply enabled intent",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "not-created")
			_, err := GenerateNative(domain.Infrastructure{Sites: []domain.Site{{
				Name: "lab", Devices: []domain.Device{test.device},
			}}}, output)
			if err == nil || !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("GenerateNative error = %v, want substring %q", err, test.wantMessage)
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("rejected native intent left output root behind: %v", err)
			}
		})
	}
}

func TestGenerateNativeRejectsInvalidParameters(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    devices:
      - name: router1
        vendor: cisco
        family: iosxe
        network:
          interfaces:
            - name: GigabitEthernet1
              role: lan
              ipv4_mode: static
              ipv4_address: not-an-ip
`))
	if err == nil {
		t.Fatal("expected invalid IPv4 input to be rejected by parsing")
	}
	if _, err := GenerateNative(infrastructure, t.TempDir()); err == nil {
		t.Fatal("expected malformed IPv4 parameters to be rejected")
	}

	infrastructure, err = config.Parse([]byte(`sites:
  - name: lab
    devices:
      - name: router1
        vendor: fortinet
        family: fortios
        network:
          vdom: "root\nend"
          interfaces:
            - name: port1
              role: lan
              ipv4_mode: dhcp
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateNative(infrastructure, t.TempDir()); err == nil || !strings.Contains(err.Error(), "cannot safely encode VDOM") {
		t.Fatalf("expected unsafe VDOM to be rejected, got %v", err)
	}
}

func TestGenerateNativeRejectsCaseInsensitiveArtifactIdentityCollision(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    devices:
      - name: router1
        id: Edge
        vendor: cisco
        family: iosxe
        network:
          interfaces:
            - name: GigabitEthernet1
              role: lan
              ipv4_mode: static
              ipv4_address: 192.0.2.1/24
      - name: router2
        id: edge
        vendor: cisco
        family: iosxe
        network:
          interfaces:
            - name: GigabitEthernet1
              role: lan
              ipv4_mode: static
              ipv4_address: 198.51.100.1/24
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateNative(infrastructure, t.TempDir()); err == nil || !strings.Contains(err.Error(), "native artifact identity") {
		t.Fatalf("expected case-insensitive artifact path collision to be rejected, got %v", err)
	}
}

func TestGenerateNativeArtifactsHaveDeterministicHashes(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    devices:
      - name: router1
        vendor: cisco
        family: iosxe
        network:
          interfaces:
            - name: GigabitEthernet1
              role: lan
              ipv4_mode: static
              ipv4_address: 192.0.2.1/24
          routes:
            - destination: 198.51.100.0/24
              next_hop: 192.0.2.254
`))
	if err != nil {
		t.Fatal(err)
	}
	firstRoot, secondRoot := t.TempDir(), t.TempDir()
	first, err := GenerateNative(infrastructure, firstRoot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateNative(infrastructure, secondRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("artifact counts differ: %d != %d", len(first), len(second))
	}
	for index := range first {
		if first[index].Path != second[index].Path || first[index].OutputHash != second[index].OutputHash {
			t.Fatalf("non-deterministic artifacts: %#v != %#v", first[index], second[index])
		}
		firstBytes, err := os.ReadFile(filepath.Join(firstRoot, filepath.FromSlash(first[index].Path)))
		if err != nil {
			t.Fatal(err)
		}
		secondBytes, err := os.ReadFile(filepath.Join(secondRoot, filepath.FromSlash(second[index].Path)))
		if err != nil {
			t.Fatal(err)
		}
		if string(firstBytes) != string(secondBytes) || protocol.SHA256(firstBytes) != first[index].OutputHash {
			t.Fatalf("artifact %q bytes/hash are not deterministic", first[index].Path)
		}
	}
}

func TestGenerateAllPublishesNativeArtifactsThroughManifest(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    bootstrap:
      network: 192.0.2.0/24
      gateway: 192.0.2.254
    devices:
      - name: router1
        vendor: cisco
        family: iosxe
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
	root := t.TempDir()
	generated, err := GenerateAll(infrastructure, root)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Catalog(root)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, artifact := range catalog {
		paths[artifact.Path] = true
		contents, published, err := ReadArtifact(root, artifact.Path)
		if err != nil || protocol.SHA256(contents) != published.OutputHash {
			t.Fatalf("published artifact %s failed manifest integrity validation: %v", artifact.Path, err)
		}
	}
	for _, path := range []string{
		"lab/native/device-router1.cfg",
		"lab/native/native-manifest.json",
	} {
		if !paths[path] {
			t.Errorf("GenerateAll manifest omitted native artifact %q", path)
		}
	}
	var siteManifest protocol.SiteManifest
	manifestBytes, err := os.ReadFile(filepath.Join(root, "lab", "site-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(manifestBytes, &siteManifest); err != nil {
		t.Fatal(err)
	}
	foundNativeConfig := false
	for _, artifact := range siteManifest.Artifacts {
		if artifact.Type != "native_device_config" {
			continue
		}
		foundNativeConfig = true
		if artifact.SiteID != "lab" || artifact.DeviceID != "router1" ||
			artifact.Vendor != "cisco" || artifact.Family != "iosxe" ||
			artifact.Format != "text/plain" || artifact.TemplateID != "infraflow.cisco.iosxe.native" ||
			artifact.TemplateVersion != nativeTemplateVersion ||
			artifact.InputHash != siteManifest.InputHash ||
			artifact.GenerationStatus != "rendered_unverified" ||
			artifact.CompatibilityStatus != "unknown" ||
			artifact.DocumentationReferences == nil {
			t.Fatalf("native artifact metadata is incomplete or overclaims support: %#v", artifact)
		}
	}
	if !foundNativeConfig {
		t.Fatal("site manifest does not declare native configuration provenance")
	}
	if len(catalog) != len(generated) {
		t.Fatalf("published catalog has %d artifacts; GenerateAll returned %d", len(catalog), len(generated))
	}
}

func TestPilotInputPublishesNativeConfigsForEachDeclaredVendor(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("..", "..", "..", "demo-output", "pilot-input.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	infrastructure, err := config.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := GenerateAll(infrastructure, root); err != nil {
		t.Fatalf("pilot generate-all failed: %v", err)
	}
	catalog, err := Catalog(root)
	if err != nil {
		t.Fatal(err)
	}
	expectedPaths := []string{
		"vendor-pilot/native/device-CiscoR1.cfg",
		"vendor-pilot/native/device-MikroTikR1.cfg",
		"vendor-pilot/native/device-FortiGate1.cfg",
	}
	publishedPaths := make(map[string]protocol.Artifact, len(catalog))
	for _, artifact := range catalog {
		publishedPaths[artifact.Path] = artifact
	}
	for _, path := range expectedPaths {
		artifact, exists := publishedPaths[path]
		if !exists || artifact.Type != "native_device_config" {
			t.Errorf("pilot did not publish native device config %q: %#v", path, artifact)
			continue
		}
		contents, verified, err := ReadArtifact(root, path)
		if err != nil || len(contents) == 0 || protocol.SHA256(contents) != verified.OutputHash {
			t.Errorf("pilot native artifact %q failed published hash verification: %v", path, err)
		}
	}
	var manifest nativeManifest
	manifestBytes, err := os.ReadFile(filepath.Join(root, "vendor-pilot", "native", "native-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Devices) != 3 || manifest.Status != "rendered_unverified" {
		t.Fatalf("pilot manifest does not describe the three unverified vendor renders: %#v", manifest)
	}
}

func TestGenerateAllPublishesRouterOSAutoRSCWithProvenance(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    bootstrap:
      network: 198.51.100.0/24
      gateway: 198.51.100.254
    devices:
      - name: edge-r1
        id: r1
        vendor: mikrotik
        family: routeros
        model: RB750Gr3
        os_version: "7.x"
        provisioning:
          method: auto-rsc
          template_version: "1"
        network:
          interfaces:
            - name: ether1
              role: wan
              ipv4_mode: static
              ipv4_address: 198.51.100.1/24
            - name: ether2
              role: lan
              ipv4_mode: dhcp
          routes:
            - destination: 203.0.113.0/24
              next_hop: 198.51.100.254
              interface: ether1
`))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := GenerateAll(infrastructure, root); err != nil {
		t.Fatal(err)
	}
	path := "lab/native/device-r1.auto.rsc"
	contents := readNativeArtifact(t, root, path)
	for _, expected := range []string{
		`/system identity set name="edge-r1"`,
		`/ip address add address=198.51.100.1/24 interface="ether1"`,
		`/ip dhcp-client add interface="ether2" disabled=no`,
		`/ip route add dst-address=203.0.113.0/24 gateway="198.51.100.254"`,
		`find where comment="infraflow:r1:interface:ether1"`,
		`/ip address set $addressEntry address=198.51.100.1/24 interface="ether1"`,
		`/ip dhcp-client set $dhcpEntry interface="ether2" disabled=no`,
		`find where comment="infraflow:r1:route:203.0.113.0/24"`,
		`/ip route set $routeEntry dst-address=203.0.113.0/24 gateway="198.51.100.254" distance=1`,
	} {
		if !strings.Contains(contents, expected) {
			t.Errorf("RouterOS auto-script missing %q:\n%s", expected, contents)
		}
	}
	if strings.Contains(contents, "config terminal") || strings.Contains(contents, "system reset-configuration") {
		t.Fatal("RouterOS auto-script contains another vendor's or destructive commands")
	}
	if strings.Count(contents, "/ip address add") != 1 || strings.Count(contents, "/ip dhcp-client add") != 1 || strings.Count(contents, "/ip route add") != 1 {
		t.Fatal("RouterOS auto-script must contain one guarded create per requested object")
	}
	secondRoot := t.TempDir()
	if _, err := GenerateAll(infrastructure, secondRoot); err != nil {
		t.Fatal(err)
	}
	if second := readNativeArtifact(t, secondRoot, path); second != contents {
		t.Fatalf("identical input produced a different RouterOS script:\n%s\n%s", contents, second)
	}

	catalog, err := Catalog(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, artifact := range catalog {
		if artifact.Path != path {
			continue
		}
		found = true
		if artifact.Type != "native_device_config" || artifact.OutputHash != protocol.SHA256([]byte(contents)) {
			t.Fatalf("catalog metadata does not match the generated RouterOS script: %#v", artifact)
		}
	}
	if !found {
		t.Fatalf("catalog omitted generated RouterOS auto-script %q", path)
	}

	var manifest struct {
		Devices []map[string]any `json:"devices"`
	}
	manifestBytes, err := os.ReadFile(filepath.Join(root, "lab", "native", "native-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Devices) != 1 || manifest.Devices[0]["path"] != path ||
		manifest.Devices[0]["provisioning_method"] != "auto-rsc" ||
		manifest.Devices[0]["verification_status"] != "not_performed" ||
		manifest.Devices[0]["template_id"] != "infraflow.mikrotik.routeros7.auto-rsc" ||
		manifest.Devices[0]["template_version"] != "1" ||
		manifest.Devices[0]["compatibility_status"] != "unknown" ||
		manifest.Devices[0]["validation_status"] != "render-only" ||
		manifest.Devices[0]["output_hash"] != protocol.SHA256([]byte(contents)) {
		t.Fatalf("native manifest omitted RouterOS method or verification provenance: %#v", manifest.Devices)
	}
	inputHash, ok := manifest.Devices[0]["input_hash"].(string)
	if !ok || !protocol.IsSHA256(inputHash) {
		t.Fatalf("native manifest input hash is invalid: %#v", manifest.Devices[0]["input_hash"])
	}
	templateHash, ok := manifest.Devices[0]["template_hash"].(string)
	if !ok || !protocol.IsSHA256(templateHash) {
		t.Fatalf("native manifest template hash is invalid: %#v", manifest.Devices[0]["template_hash"])
	}
	supportedFeatures, ok := manifest.Devices[0]["supported_features"].([]any)
	if !ok || len(supportedFeatures) == 0 {
		t.Fatalf("native manifest supported features are missing: %#v", manifest.Devices[0]["supported_features"])
	}
	sourceReferences, ok := manifest.Devices[0]["source_references"].([]any)
	if !ok || len(sourceReferences) == 0 {
		t.Fatalf("native manifest source references are missing: %#v", manifest.Devices[0]["source_references"])
	}

	var siteManifest protocol.SiteManifest
	siteManifestBytes, err := os.ReadFile(filepath.Join(root, "lab", "site-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(siteManifestBytes, &siteManifest); err != nil {
		t.Fatal(err)
	}
	foundSiteArtifact := false
	for _, artifact := range siteManifest.Artifacts {
		if artifact.Path == path && (artifact.ProvisioningMethod != "auto-rsc" ||
			artifact.VerificationStatus != "not_performed" || artifact.OutputHash != artifact.SHA256 ||
			len(artifact.SourceReferences) == 0 || artifact.CompatibilityStatus != "unknown" ||
			!protocol.IsSHA256(artifact.TemplateHash) || artifact.ValidationStatus != "render-only" ||
			len(artifact.SupportedFeatures) == 0 ||
			artifact.TemplateID != "infraflow.mikrotik.routeros7.auto-rsc" ||
			artifact.TemplateVersion != "1" || artifact.InputHash != siteManifest.InputHash) {
			t.Fatalf("site manifest provenance is incomplete: %#v", artifact)
		}
		if artifact.Path == path {
			foundSiteArtifact = true
		}
	}
	if !foundSiteArtifact {
		t.Fatalf("site manifest omitted RouterOS auto-script %q", path)
	}
}

func TestProvisioningGenerationExampleProducesVerifiableArtifacts(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "provisioning-generation.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	infrastructure, err := config.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := GenerateAll(infrastructure, root); err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string]string{
		"cisco-render-demo/native/device-cisco-r1.cfg":              "ip address 192.0.2.1 255.255.255.0",
		"routeros-auto-rsc-demo/native/device-mikrotik-r1.auto.rsc": `/system identity set name="MikroTikR1"`,
	} {
		data, artifact, err := ReadArtifact(root, path)
		if err != nil {
			t.Fatalf("read generated demo artifact %q: %v", path, err)
		}
		if !strings.Contains(string(data), expected) || artifact.OutputHash != protocol.SHA256(data) {
			t.Fatalf("generated demo artifact %q did not match its content/hash: %#v", path, artifact)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "routeros-auto-rsc-demo", "native", "device-mikrotik-r1.auto.log")); !os.IsNotExist(err) {
		t.Fatalf("generation must not fabricate a RouterOS execution log: %v", err)
	}
}

func TestGenerateNativeRejectsUnsupportedProvisioningProfiles(t *testing.T) {
	cases := []struct {
		name        string
		device      domain.Device
		wantMessage string
	}{
		{
			name: "Cisco ZTP TLS evidence gap",
			device: domain.Device{
				Name: "router1", Vendor: "cisco", Family: "iosxe",
				Provisioning: domain.Provisioning{Method: "cisco-ztp"},
				Network:      &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "GigabitEthernet1", Role: "lan", IPv4Mode: "static", IPv4Address: "192.0.2.1/24"}}},
			},
			wantMessage: "does not confirm the required TLS-only transport",
		},
		{
			name: "Cisco AutoInstall wrong model is rejected",
			device: domain.Device{
				Name: "router1", Vendor: "cisco", Family: "ios", Model: "7500", OSVersion: "15.3(3)XB12",
				Provisioning: domain.Provisioning{Method: "cisco-autoinstall"},
				Network:      &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "GigabitEthernet1", Role: "lan", IPv4Mode: "static", IPv4Address: "192.0.2.1/24"}}},
			},
			wantMessage: "Cisco IOS AutoInstall supports only exact model/release profiles",
		},
		{
			name: "Fortinet ZTP inputs missing",
			device: domain.Device{
				Name: "firewall1", Vendor: "fortinet", Family: "fortios", Model: "FG-60F",
				Provisioning: domain.Provisioning{Method: "fortimanager-ztp"},
				Network:      &domain.DeviceNetwork{VDOM: "root", Interfaces: []domain.NetworkInterface{{Name: "port1", Role: "wan", IPv4Mode: "static", IPv4Address: "192.0.2.1/24"}}},
			},
			wantMessage: "bootstrap.fortimanager.ip and bootstrap.fortimanager.domain",
		},
		{
			name:        "unknown RouterOS model",
			device:      autoRSCDevice("CCR2004", "7.x", "1", "ether1"),
			wantMessage: "RouterOS Auto Import requires an exact model profile",
		},
		{
			name:        "unsupported RouterOS version",
			device:      autoRSCDevice("RB750Gr3", "6.49.10", "1", "ether1"),
			wantMessage: "requires an explicit RouterOS 7 version",
		},
		{
			name:        "missing required template version",
			device:      autoRSCDevice("RB750Gr3", "7.x", "", "ether1"),
			wantMessage: "requires provisioning.template_version",
		},
		{
			name:        "missing network intent",
			device:      autoRSCDeviceWithoutNetwork(),
			wantMessage: "requires network intent",
		},
		{
			name:        "unsafe interface value",
			device:      autoRSCDevice("RB750Gr3", "7.x", "1", `ether1"; /system reset-configuration`),
			wantMessage: "safe device interface identifier",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			output := t.TempDir()
			_, err := GenerateNative(domain.Infrastructure{Sites: []domain.Site{{
				Name: "lab", Devices: []domain.Device{test.device},
			}}}, output)
			if err == nil || !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("GenerateNative error = %v, want substring %q", err, test.wantMessage)
			}
			if _, statErr := os.Stat(filepath.Join(output, "lab", "native", "native-manifest.json")); !os.IsNotExist(statErr) {
				t.Fatalf("rejected profile left a native manifest: %v", statErr)
			}
		})
	}
}

func TestGenerateNativeSupportsExplicitRouterOSAutoRSCMethodAlias(t *testing.T) {
	device := autoRSCDevice("RB750Gr3", "7.x", "1", "ether1")
	device.Provisioning.Method = domain.ProvisioningMethodMikroTikRouterOS7AutoRSC
	root := t.TempDir()
	artifacts, err := GenerateNative(domain.Infrastructure{Sites: []domain.Site{{Name: "lab", Devices: []domain.Device{device}}}}, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 || !strings.Contains(readNativeArtifact(t, root, "lab/native/device-r1.auto.rsc"), "/system identity set name=") {
		t.Fatalf("explicit RouterOS Auto Import alias did not render its profile: %#v", artifacts)
	}
}

func autoRSCDevice(model, version, templateVersion, interfaceName string) domain.Device {
	return domain.Device{
		Name: "edge-r1", ID: "r1", Vendor: "mikrotik", Family: "routeros",
		Model: model, OSVersion: version,
		Provisioning: domain.Provisioning{Method: "auto-rsc", TemplateVersion: templateVersion},
		Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{
			Name: interfaceName, Role: "wan", IPv4Mode: "static", IPv4Address: "198.51.100.1/24",
		}}},
	}
}

func autoRSCDeviceWithoutNetwork() domain.Device {
	device := autoRSCDevice("RB750Gr3", "7.x", "1", "ether1")
	device.Network = nil
	return device
}

func readNativeArtifact(t *testing.T, root, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read native artifact %q: %v", path, err)
	}
	return string(data)
}

// Regression (GNS3 CHR run 2026-11-19): metadata declares the RouterOS train
// "7.x", but only the literal string "7.x" was accepted, so every concrete
// RouterOS release (7.8, 7.20.1) was rejected at generation.
func TestGenerateNativeRouterOSAutoRSCAcceptsConcreteReleaseOfDeclaredTrain(t *testing.T) {
	for _, test := range []struct{ model, version, templateID string }{
		{"CHR", "7.8", "infraflow.mikrotik.routeros7.chr.auto-rsc"},
		{"RB750Gr3", "7.20.1", "infraflow.mikrotik.routeros7.auto-rsc"},
	} {
		root := t.TempDir()
		device := autoRSCDevice(test.model, test.version, "1", "ether2")
		if _, err := GenerateNative(domain.Infrastructure{Sites: []domain.Site{{Name: "lab", Devices: []domain.Device{device}}}}, root); err != nil {
			t.Fatalf("%s %s: %v", test.model, test.version, err)
		}
		var manifest nativeManifest
		if err := json.Unmarshal([]byte(readNativeArtifact(t, root, "lab/native/native-manifest.json")), &manifest); err != nil {
			t.Fatal(err)
		}
		if len(manifest.Devices) != 1 || manifest.Devices[0].TemplateID != test.templateID {
			t.Fatalf("%s %s resolved %#v, want %s", test.model, test.version, manifest.Devices, test.templateID)
		}
	}
}

func TestMatchesReleaseTrainIsNumericAndPrefixBound(t *testing.T) {
	for _, test := range []struct {
		train, version string
		want           bool
	}{
		{"7.x", "7.8", true}, {"7.x", "7.20.1", true}, {"7.x", "7.x", false},
		{"7.x", "17.8", false}, {"7.x", "6.49.10", false}, {"7.x", "7.8beta1", false},
		{"7.x", "7", false}, {"x", "7.8", false}, {"7.8", "7.8", false}, {"*", "7.8", false},
	} {
		if got := matchesReleaseTrain(test.train, test.version); got != test.want {
			t.Errorf("matchesReleaseTrain(%q, %q) = %t, want %t", test.train, test.version, got, test.want)
		}
	}
}
