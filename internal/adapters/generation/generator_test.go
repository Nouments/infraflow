package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"infraflow/internal/adapters/config"
	"infraflow/pkg/protocol"
)

func TestGenerateIsDeterministic(t *testing.T) {
	firstInput := `sites:
  - name: lab
    devices:
      - name: SW1
        vendor: cisco
        model: ios-xe
      - name: R1
        vendor: cisco
        model: csr1000v
`
	secondInput := `sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        model: csr1000v
      - name: SW1
        vendor: cisco
        model: ios-xe
`
	firstInfrastructure, err := config.Parse([]byte(firstInput))
	if err != nil {
		t.Fatal(err)
	}
	secondInfrastructure, err := config.Parse([]byte(secondInput))
	if err != nil {
		t.Fatal(err)
	}

	firstDirectory := t.TempDir()
	secondDirectory := t.TempDir()
	firstArtifacts, err := Generate(firstInfrastructure, firstDirectory)
	if err != nil {
		t.Fatal(err)
	}
	secondArtifacts, err := Generate(secondInfrastructure, secondDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstArtifacts, secondArtifacts) {
		t.Fatalf("artifact metadata differs:\n%#v\n%#v", firstArtifacts, secondArtifacts)
	}
	for _, path := range []string{"lab/inventory.json", "lab/topology.json", "lab/manifest.json"} {
		first, err := os.ReadFile(filepath.Join(firstDirectory, path))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(secondDirectory, path))
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(second) {
			t.Errorf("%s differs for equivalent input", path)
		}
	}
}

func TestGenerateCanonicalizesLinksWithSameEndpoints(t *testing.T) {
	firstInput := `sites:
  - name: lab
    devices:
      - name: R1
      - name: SW1
    links:
      - a: R1:Gi1
        b: SW1:Gi1
        network: 10.0.0.0/30
      - a: R1:Gi1
        b: SW1:Gi1
        network: 10.0.1.0/30
`
	secondInput := `sites:
  - name: lab
    devices:
      - name: R1
      - name: SW1
    links:
      - a: R1:Gi1
        b: SW1:Gi1
        network: 10.0.1.0/30
      - a: R1:Gi1
        b: SW1:Gi1
        network: 10.0.0.0/30
`
	firstInfrastructure, err := config.Parse([]byte(firstInput))
	if err != nil {
		t.Fatal(err)
	}
	secondInfrastructure, err := config.Parse([]byte(secondInput))
	if err != nil {
		t.Fatal(err)
	}

	firstDirectory := t.TempDir()
	secondDirectory := t.TempDir()
	firstArtifacts, err := Generate(firstInfrastructure, firstDirectory)
	if err != nil {
		t.Fatal(err)
	}
	secondArtifacts, err := Generate(secondInfrastructure, secondDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstArtifacts, secondArtifacts) {
		t.Fatalf("artifact metadata differs:\n%#v\n%#v", firstArtifacts, secondArtifacts)
	}
	for _, relativePath := range []string{"lab/inventory.json", "lab/topology.json", "lab/manifest.json"} {
		first, err := os.ReadFile(filepath.Join(firstDirectory, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(secondDirectory, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(second) {
			t.Errorf("%s differs for equivalent link ordering", relativePath)
		}
	}
}

func TestGenerateRefusesSiteDirectorySymlink(t *testing.T) {
	infrastructure, err := config.Parse([]byte("sites:\n  - name: lab\n"))
	if err != nil {
		t.Fatal(err)
	}
	outputDirectory := t.TempDir()
	outsideDirectory := t.TempDir()
	if err := os.Symlink(outsideDirectory, filepath.Join(outputDirectory, "lab")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Generate(infrastructure, outputDirectory); err == nil {
		t.Fatal("expected symlink output path to be rejected")
	}
	if _, err := os.Stat(filepath.Join(outsideDirectory, "inventory.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected write through symlink, stat error: %v", err)
	}
}

func TestGenerateSupportsMultipleSites(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: office-a
    devices:
      - name: R1
  - name: office-b
    devices:
      - name: R2
`))
	if err != nil {
		t.Fatal(err)
	}
	outputDirectory := t.TempDir()
	artifacts, err := Generate(infrastructure, outputDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 4 {
		t.Fatalf("expected inventory and topology for both sites, got %#v", artifacts)
	}
	for _, relativePath := range []string{
		"office-a/inventory.json", "office-a/topology.json",
		"office-b/inventory.json", "office-b/topology.json",
	} {
		if _, err := os.Stat(filepath.Join(outputDirectory, relativePath)); err != nil {
			t.Errorf("missing site artifact %s: %v", relativePath, err)
		}
	}
}

func TestGenerateAllPublishesCompleteCatalogForAgent(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    bootstrap:
      network: 192.168.100.0/24
      gateway: 192.168.100.1
    services:
      dhcp: true
      tftp: true
      pxe: true
    devices:
      - name: R1
        vendor: cisco
        model: ios-xe
        management:
          ipv4: 192.168.100.10
`))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	artifacts, err := GenerateAll(infrastructure, root)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Catalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != len(artifacts) || len(catalog) != 17 {
		t.Fatalf("combined manifest published %d of %d generated artifacts", len(catalog), len(artifacts))
	}
	seenTypes := make(map[string]bool)
	for _, artifact := range catalog {
		seenTypes[artifact.Type] = true
		data, published, err := ReadArtifact(root, artifact.Path)
		if err != nil || published.OutputHash != protocol.SHA256(data) {
			t.Fatalf("catalog artifact %s failed its hash check: %v", artifact.Path, err)
		}
	}
	for _, artifactType := range []string{"inventory", "topology", "ansible_inventory", "ansible_playbook", "terraform_locals", "bootstrap_dhcp", "bootstrap_tftp", "bootstrap_ipxe_script"} {
		if !seenTypes[artifactType] {
			t.Errorf("combined manifest omitted %s", artifactType)
		}
	}
}

func TestGenerateAnsibleIsDeterministicAndReadOnly(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    devices:
      - name: R1
        role: router
        vendor: cisco
        family: ios-xe
        model: csr1000v
        management:
          ipv4: 10.0.0.1
      - name: SW1
        vendor: cisco
        model: ios-xe
`))
	if err != nil {
		t.Fatal(err)
	}
	firstDirectory := t.TempDir()
	secondDirectory := t.TempDir()
	first, err := GenerateAnsible(infrastructure, firstDirectory)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateAnsible(infrastructure, secondDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || len(first) != 2 {
		t.Fatalf("unexpected Ansible metadata: %#v %#v", first, second)
	}
	for _, relativePath := range []string{"lab/ansible/inventory.yml", "lab/ansible/site.yml"} {
		firstData, err := os.ReadFile(filepath.Join(firstDirectory, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		secondData, err := os.ReadFile(filepath.Join(secondDirectory, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		if string(firstData) != string(secondData) {
			t.Errorf("%s differs between equivalent inputs", relativePath)
		}
	}
	if _, err := os.Stat(filepath.Join(firstDirectory, "lab", "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("Ansible-only generation must not silently publish a provider manifest: %v", err)
	}
}

func TestGenerateAnsibleRefusesSymlinkedParent(t *testing.T) {
	infrastructure, err := config.Parse([]byte("sites:\n  - name: lab\n"))
	if err != nil {
		t.Fatal(err)
	}
	outputDirectory := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outputDirectory, "lab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(outputDirectory, "lab", "ansible")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := GenerateAnsible(infrastructure, outputDirectory); err == nil {
		t.Fatal("expected Ansible generation to reject symlinked parent")
	}
}

func TestGenerateTerraformIsDeterministicAndDataOnly(t *testing.T) {
	input := `sites:
  - name: lab
    bootstrap:
      network: 192.168.100.0/24
    services:
      dns: true
    devices:
      - name: R1
        role: router
        vendor: cisco
        family: ios-xe
        model: csr1000v
        management:
          ipv4: 192.168.100.10
    links:
      - a: R1:Gi1
        b: R1:Gi2
        network: 10.0.0.0/30
`
	infrastructure, err := config.Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	firstDirectory := t.TempDir()
	secondDirectory := t.TempDir()
	first, err := GenerateTerraform(infrastructure, firstDirectory)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateTerraform(infrastructure, secondDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || len(first) != 7 {
		t.Fatalf("unexpected Terraform metadata: %#v %#v", first, second)
	}
	for _, relativePath := range []string{
		"lab/terraform/versions.tf", "lab/terraform/providers.tf", "lab/terraform/variables.tf",
		"lab/terraform/locals.tf", "lab/terraform/main.tf", "lab/terraform/outputs.tf",
		"lab/terraform/terraform.tfvars.example",
	} {
		firstData, err := os.ReadFile(filepath.Join(firstDirectory, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		secondData, err := os.ReadFile(filepath.Join(secondDirectory, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		if string(firstData) != string(secondData) {
			t.Errorf("%s differs between equivalent inputs", relativePath)
		}
	}
	locals, err := os.ReadFile(filepath.Join(firstDirectory, "lab", "terraform", "locals.tf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(locals), `"environment" = var.environment`) {
		t.Fatalf("Terraform variable expression is missing: %s", locals)
	}
	if _, err := os.Stat(filepath.Join(firstDirectory, "lab", "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("Terraform-only generation must not silently publish a provider manifest: %v", err)
	}
}

func TestGenerateTerraformEscapesUserStrings(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    devices:
      - name: '${dangerous_expression}'
`))
	if err != nil {
		t.Fatal(err)
	}
	outputDirectory := t.TempDir()
	if _, err := GenerateTerraform(infrastructure, outputDirectory); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(outputDirectory, "lab", "terraform", "locals.tf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"${dangerous_expression}"`) {
		t.Fatalf("user expression was not escaped as a string: %s", data)
	}
}

func TestGenerateBootstrapCreatesPoolsReservationsDNSAndPXE(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: agence-01
    bootstrap:
      network: 192.168.100.0/29
      gateway: 192.168.100.1
    services:
      dhcp: true
      dns: true
      tftp: true
      pxe: true
    devices:
      - name: R1
        management:
          ipv4: 192.168.100.2
        identity:
          macs: ["00:11:22:33:44:55"]
      - name: SW1
        management:
          ipv4: 192.168.100.3
`))
	if err != nil {
		t.Fatal(err)
	}
	outputDirectory := t.TempDir()
	artifacts, err := GenerateBootstrap(infrastructure, outputDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 6 {
		t.Fatalf("expected six bootstrap artifacts, got %d: %#v", len(artifacts), artifacts)
	}
	if _, err := os.Stat(filepath.Join(outputDirectory, "agence-01", "manifest.json")); err != nil {
		t.Fatalf("bootstrap manifest missing: %v", err)
	}
	catalog, err := Catalog(outputDirectory)
	if err != nil || len(catalog) != 6 {
		t.Fatalf("bootstrap artifacts were not cataloged: %d, %v", len(catalog), err)
	}
	var dhcp bootstrapDHCP
	data, err := os.ReadFile(filepath.Join(outputDirectory, "agence-01", "bootstrap", "dhcp", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &dhcp); err != nil {
		t.Fatal(err)
	}
	if len(dhcp.Reservations) != 1 || dhcp.Reservations[0].IP != "192.168.100.2" || dhcp.Reservations[0].MAC != "00:11:22:33:44:55" {
		t.Fatalf("unexpected DHCP reservations: %#v", dhcp.Reservations)
	}
	// The /29 has .0 network, .1 gateway, .2/.3 devices, and .4-.6 usable.
	if len(dhcp.Pools) != 1 || dhcp.Pools[0].Start != "192.168.100.4" || dhcp.Pools[0].End != "192.168.100.6" {
		t.Fatalf("unexpected DHCP pools: %#v", dhcp.Pools)
	}
	if dhcp.Options.BootFilename != "undionly.kpxe" || dhcp.Options.TFTPDirectory != "tftp" {
		t.Fatalf("TFTP-enabled DHCP config omitted boot options 66/67: %#v", dhcp.Options)
	}
	var dns bootstrapDNS
	data, err = os.ReadFile(filepath.Join(outputDirectory, "agence-01", "bootstrap", "dns", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &dns); err != nil {
		t.Fatal(err)
	}
	if dns.Zone != "agence-01.infraflow.local" || len(dns.Records) != 2 || dns.Records[0].Type != "A" || len(dns.ReverseRecords) != 2 {
		t.Fatalf("unexpected DNS configuration: %#v", dns)
	}
	tftp, err := os.ReadFile(filepath.Join(outputDirectory, "agence-01", "bootstrap", "tftp", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tftp), `"read_only": true`) || strings.Contains(string(tftp), "password") {
		t.Fatalf("unsafe TFTP configuration: %s", tftp)
	}
	ipxe, err := os.ReadFile(filepath.Join(outputDirectory, "agence-01", "bootstrap", "pxe", "ipxe", "bootstrap.ipxe"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ipxe), "next-server") || !strings.Contains(string(ipxe), "agence-01") {
		t.Fatalf("unexpected iPXE script: %s", ipxe)
	}
}

func TestGenerateBootstrapRequiresIPv4Network(t *testing.T) {
	infrastructure, err := config.Parse([]byte("sites:\n  - name: lab\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateBootstrap(infrastructure, t.TempDir()); err == nil || !strings.Contains(err.Error(), "bootstrap network") {
		t.Fatalf("expected bootstrap network error, got %v", err)
	}
}

func TestGenerateBootstrapDisablesServicesWithoutExplicitOptIn(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    bootstrap:
      network: 192.168.100.0/24
`))
	if err != nil {
		t.Fatal(err)
	}
	outputDirectory := t.TempDir()
	if _, err := GenerateBootstrap(infrastructure, outputDirectory); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(outputDirectory, "lab", "bootstrap", "dhcp", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var dhcp bootstrapDHCP
	if err := json.Unmarshal(data, &dhcp); err != nil {
		t.Fatal(err)
	}
	if dhcp.Enabled || dhcp.Options.BootFilename != "" || dhcp.Options.TFTPDirectory != "" {
		t.Fatalf("DHCP/TFTP boot must remain disabled without explicit service opt-in: %#v", dhcp)
	}
}

func TestGenerateBootstrapRejectsNetworkAndBroadcastReservations(t *testing.T) {
	for _, address := range []string{"192.168.100.0", "192.168.100.7"} {
		t.Run(address, func(t *testing.T) {
			infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    bootstrap:
      network: 192.168.100.0/29
    devices:
      - name: invalid-host
        management:
          ipv4: ` + address + `
        identity:
          macs: ["00:11:22:33:44:55"]
`))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := GenerateBootstrap(infrastructure, t.TempDir()); err == nil || !strings.Contains(err.Error(), "usable DHCP host address") {
				t.Fatalf("expected invalid DHCP host address error, got %v", err)
			}
		})
	}
}

func TestGenerateBootstrapRejectsNetworkAndBroadcastGateways(t *testing.T) {
	for _, address := range []string{"192.168.100.0", "192.168.100.7"} {
		t.Run(address, func(t *testing.T) {
			infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    bootstrap:
      network: 192.168.100.0/29
      gateway: ` + address + `
`))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := GenerateBootstrap(infrastructure, t.TempDir()); err == nil || !strings.Contains(err.Error(), "usable DHCP host address") {
				t.Fatalf("expected invalid DHCP gateway error, got %v", err)
			}
		})
	}
}

func TestGenerateBootstrapRejectsDeviceAddressMatchingGateway(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    bootstrap:
      network: 192.168.100.0/24
      gateway: 192.168.100.1
    devices:
      - name: gateway-device
        management:
          ipv4: 192.168.100.1
        identity:
          macs: ["00:11:22:33:44:55"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateBootstrap(infrastructure, t.TempDir()); err == nil || !strings.Contains(err.Error(), "conflicts with the bootstrap gateway") {
		t.Fatalf("expected gateway reservation conflict, got %v", err)
	}
}
