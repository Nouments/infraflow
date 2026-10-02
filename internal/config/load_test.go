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
