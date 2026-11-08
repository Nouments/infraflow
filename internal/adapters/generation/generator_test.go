package generator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"infraflow/internal/adapters/config"
	"infraflow/internal/domain"
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
      - a: R1:Gi2
        b: SW1:Gi2
        network: 10.0.1.0/30
`
	secondInput := `sites:
  - name: lab
    devices:
      - name: R1
      - name: SW1
    links:
      - a: R1:Gi2
        b: SW1:Gi2
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
	for _, path := range []string{
		"manifest.json", "generation-report.json",
		"lab/manifest.json", "lab/site-manifest.json", "lab/generation-report.json",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Errorf("missing generation metadata %s: %v", path, err)
		}
	}
	var globalReport protocol.GenerationReport
	globalReportBytes, err := os.ReadFile(filepath.Join(root, "generation-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(globalReportBytes, &globalReport); err != nil {
		t.Fatal(err)
	}
	if len(globalReport.Sites) != 1 || globalReport.Sites[0].ArtifactCount != len(catalog) {
		t.Fatalf("generation report artifact count does not match the published catalog: %#v", globalReport.Sites)
	}
	var siteManifest protocol.SiteManifest
	siteManifestBytes, err := os.ReadFile(filepath.Join(root, "lab", "site-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(siteManifestBytes, &siteManifest); err != nil {
		t.Fatal(err)
	}
	if siteManifest.SiteID != "lab" || siteManifest.SiteName != "lab" || siteManifest.Status != "succeeded" {
		t.Fatalf("unexpected site manifest identity/status: %#v", siteManifest)
	}
	for _, detail := range siteManifest.Artifacts {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(detail.Path)))
		if err != nil || int64(len(data)) != detail.SizeBytes || protocol.SHA256(data) != detail.SHA256 {
			t.Errorf("site manifest entry %s does not match the generated file: size=%d err=%v", detail.Path, detail.SizeBytes, err)
		}
	}
	seenTypes := make(map[string]bool)
	for _, artifact := range catalog {
		seenTypes[artifact.Type] = true
		data, published, err := ReadArtifact(root, artifact.Path)
		if err != nil || published.OutputHash != protocol.SHA256(data) {
			t.Fatalf("catalog artifact %s failed its hash check: %v", artifact.Path, err)
		}
	}
	for _, artifactType := range []string{"inventory", "topology", "terraform_locals", "bootstrap_dhcp", "bootstrap_tftp", "bootstrap_ipxe_script"} {
		if !seenTypes[artifactType] {
			t.Errorf("combined manifest omitted %s", artifactType)
		}
	}
}

func TestRunSitesInParallelStartsIndependentWorkersConcurrently(t *testing.T) {
	sites := []domain.Site{{Name: "site-a"}, {Name: "site-b"}, {Name: "site-c"}}
	started := make(chan string, len(sites))
	release := make(chan struct{})
	resultsDone := make(chan []siteGenerationResult, 1)
	go func() {
		resultsDone <- runSitesInParallel(context.Background(), sites, 2, func(site domain.Site) siteGenerationResult {
			started <- site.Name
			<-release
			return siteGenerationResult{site: site}
		})
	}()
	startedSites := make(map[string]bool)
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for len(startedSites) < 2 {
		select {
		case name := <-started:
			startedSites[name] = true
		case <-timer.C:
			close(release)
			t.Fatal("two site workers did not start concurrently")
		}
	}
	close(release)
	results := <-resultsDone
	if len(results) != len(sites) {
		t.Fatalf("worker pool returned %d results for %d sites", len(results), len(sites))
	}
}

func TestGenerateAllKeepsSuccessfulSitesWhenAnotherSiteFails(t *testing.T) {
	infrastructure := domain.Infrastructure{Sites: []domain.Site{
		{Name: "site-good", Bootstrap: domain.Bootstrap{Network: "192.0.2.0/24"}},
		{Name: "site-invalid", Bootstrap: domain.Bootstrap{Network: "192.0.2.0/31"}, Services: map[string]bool{"dhcp": true}},
	}}
	root := t.TempDir()
	artifacts, err := GenerateAll(infrastructure, root)
	if err == nil {
		t.Fatal("expected invalid bootstrap configuration to fail the global operation")
	}
	if len(artifacts) == 0 {
		t.Fatal("successful site artifacts were discarded after an independent site failure")
	}
	if _, err := os.Stat(filepath.Join(root, "site-good", "manifest.json")); err != nil {
		t.Fatalf("successful site was not published: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "site-invalid", "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("failed site has a published catalog manifest: %v", err)
	}
	var global GlobalManifest
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil || json.Unmarshal(data, &global) != nil {
		t.Fatalf("global manifest was not published: %v", err)
	}
	if global.Status != "partial_failure" || len(global.Sites) != 2 ||
		global.Sites[0].Status != "succeeded" || global.Sites[1].Status != "failed" {
		t.Fatalf("global manifest does not describe the independent site result: %#v", global)
	}
}

func TestGenerateAllPreflightsSitePublicationBeforeWritingArtifacts(t *testing.T) {
	infrastructure, err := config.Parse([]byte("sites:\n  - name: lab\n"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	conflictingPath := filepath.Join(root, "lab", "terraform", "main.tf")
	if err := os.MkdirAll(conflictingPath, 0o700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := GenerateAll(infrastructure, root)
	if err == nil {
		t.Fatal("expected destination conflict to fail site publication")
	}
	if len(artifacts) != 0 {
		t.Fatalf("failed site unexpectedly returned published artifacts: %#v", artifacts)
	}
	for _, path := range []string{"lab/inventory.json", "lab/native/device-R1.cfg"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); !os.IsNotExist(err) {
			t.Errorf("site publication left a partial artifact %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "lab", "site-manifest.json")); err != nil {
		t.Fatalf("failure status manifest was not retained: %v", err)
	}
	if catalog, err := Catalog(root); err != nil || len(catalog) != 0 {
		t.Fatalf("failed site's partial artifacts became Agent-visible: catalog=%#v err=%v", catalog, err)
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
	if !strings.Contains(string(locals), `"input_hash"  =`) {
		t.Fatalf("Terraform object attributes are not aligned:\n%s", locals)
	}
	if _, err := os.Stat(filepath.Join(firstDirectory, "lab", "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("Terraform-only generation must not silently publish a provider manifest: %v", err)
	}
}

func TestGenerateTerraformEscapesUserStrings(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    devices:
      - name: R1
        vendor: '${dangerous_expression}'
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
