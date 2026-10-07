package config

import (
	"strings"
	"testing"
)

const validYAML = `sites:
  - name: lab
    bootstrap:
      network: 192.168.100.0/24
      gateway: 192.168.100.1
    devices:
      - name: R1
        vendor: cisco
        model: csr1000v
        identity:
          macs: ["00:11:22:33:44:55"]
        management:
          ipv4: 192.168.100.10
      - name: SW1
        vendor: cisco
        model: ios-xe
    links:
      - a: R1:Gi1
        b: SW1:Gi1
        network: 10.0.0.0/30
`

func TestParseValidInfrastructure(t *testing.T) {
	infrastructure, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if len(infrastructure.Sites) != 1 || len(infrastructure.Sites[0].Devices) != 2 {
		t.Fatalf("unexpected parsed infrastructure: %#v", infrastructure)
	}
}

func TestParseCapabilityRegistryPreservesEvidenceAndStates(t *testing.T) {
	input := `capability_registry:
  entries:
    cisco:iosxe:ios-xe:
      netconf:
        method: netconf
        state: UNVERIFIED
        evidence: no lab execution or verified adapter was recorded
sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        model: ios-xe
`
	infrastructure, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	entry := infrastructure.CapabilityRegistry.Entries["cisco:iosxe:ios-xe"]["netconf"]
	if entry.State != "UNVERIFIED" || entry.Evidence == "" {
		t.Fatalf("unexpected capability entry: %#v", entry)
	}
}

func TestParseCapabilityRegistryRejectsUnknownOrUnprovenStates(t *testing.T) {
	input := `capability_registry:
  entries:
    cisco:iosxe:ios-xe:
      netconf:
        method: netconf
        state: FABRICATED
        evidence: not real
sites:
  - name: lab
`
	_, err := Parse([]byte(input))
	if err == nil || !strings.Contains(err.Error(), "unsupported capability state") {
		t.Fatalf("expected unsupported capability state error, got %v", err)
	}
}

func TestParseTemplateRegistryPreservesMetadataAndRejectsIncompleteEntry(t *testing.T) {
	valid := `template_registry:
  entries:
    example:router:1:
      id: example:router:1
      vendor: example-vendor
      family: router
      model: router-one
      version: "1"
      hash: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      evidence: repository manifest produced by an audited test
sites:
  - name: lab
`
	infrastructure, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if template := infrastructure.TemplateRegistry.Entries["example:router:1"]; template.Hash == "" {
		t.Fatalf("template metadata was not preserved: %#v", template)
	}

	incomplete := strings.Replace(valid, "      evidence: repository manifest produced by an audited test\n", "", 1)
	if _, err := Parse([]byte(incomplete)); err == nil || !strings.Contains(err.Error(), "provenance evidence") {
		t.Fatalf("expected incomplete template metadata error, got %v", err)
	}
}

func TestParseSingleSiteAndEndpointForm(t *testing.T) {
	input := `site:
  name: lab
  devices:
    - name: R1
    - name: SW1
  links:
    - network: 10.0.0.0/30
      endpoints:
        - device: R1
          interface: Gi1
        - device: SW1
          interface: Gi1
`
	infrastructure, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	link := infrastructure.Sites[0].Links[0]
	if link.A != "R1:Gi1" || link.B != "SW1:Gi1" {
		t.Fatalf("endpoints were not normalized: %#v", link)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := Parse([]byte("sites:\n  - name: lab\n    surprise: true\n"))
	if err == nil || !strings.Contains(err.Error(), "field surprise not found") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}

func TestParseRejectsMultipleDocuments(t *testing.T) {
	_, err := Parse([]byte(validYAML + "---\nsites: []\n"))
	if err == nil || !strings.Contains(err.Error(), "exactly one YAML document") {
		t.Fatalf("expected multiple-document error, got %v", err)
	}
}

func TestParseRejectsOversizedInput(t *testing.T) {
	_, err := Parse(make([]byte, MaxInputBytes+1))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected input-size error, got %v", err)
	}
}

func TestParseRejectsSemanticErrors(t *testing.T) {
	input := `sites:
  - name: lab
    bootstrap:
      network: 10.0.0.0/24
      gateway: 10.0.1.1
    devices:
      - name: R1
        identity:
          macs: ["invalid"]
      - name: R1
    links:
      - a: R1:Gi1
        b: missing:Gi1
        network: 10.0.0.0/30
`
	_, err := Parse([]byte(input))
	if err == nil {
		t.Fatal("expected semantic validation errors")
	}
	for _, expected := range []string{"gateway", "invalid MAC", "duplicate device name", "unknown device"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("expected error containing %q, got %v", expected, err)
		}
	}
}

func TestReadEnforcesSizeLimit(t *testing.T) {
	_, err := Read(strings.NewReader(strings.Repeat("x", MaxInputBytes+1)))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected input-size error, got %v", err)
	}
}
