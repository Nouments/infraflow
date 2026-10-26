package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"infraflow/internal/adapters/catalog"
	"infraflow/internal/adapters/config"
)

const declarativeInput = `apiVersion: infraflow/v1
kind: NetworkDeployment
metadata:
  name: demo-isr1100
spec:
  site: demo
  devices:
    - name: R1
      vendor: cisco
      model: isr-1100
      os: ios-xe
      interfaces:
        - name: GigabitEthernet0/0/0
          ipv4: 192.0.2.1/24
          enabled: true
`

func TestGenerateDeclarativeConfigResolvesTemplateAndWritesReport(t *testing.T) {
	output := t.TempDir()
	artifacts, err := GenerateDeclarativeConfig([]byte(declarativeInput), output)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("expected configuration and report artifacts, got %#v", artifacts)
	}
	configPath := filepath.Join(output, "demo", "R1", "config.cfg")
	generated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Template: infraflow.cisco.iosxe.isr-1100.interfaces.v1 version 1",
		"interface GigabitEthernet0/0/0",
		" ip address 192.0.2.1 255.255.255.0",
		" no shutdown",
	} {
		if !strings.Contains(string(generated), expected) {
			t.Errorf("generated configuration does not contain %q:\n%s", expected, generated)
		}
	}
	var report DeclarativeGenerationReport
	reportBytes, err := os.ReadFile(filepath.Join(output, "demo", "generation-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "rendered_unverified" || report.Devices[0].TemplateID == "" ||
		report.Devices[0].Candidates[0].Score != 93 || !report.Devices[0].Candidates[0].Selected {
		t.Fatalf("report did not expose selected candidate and score: %#v", report)
	}
	if report.Devices[0].Variables["interfaces"] == nil || report.Devices[0].CatalogStatus != "partial" {
		t.Fatalf("report omitted resolved variables or catalog status: %#v", report.Devices[0])
	}
}

func TestGenerateDeclarativeConfigIsDeterministic(t *testing.T) {
	firstDirectory, secondDirectory := t.TempDir(), t.TempDir()
	if _, err := GenerateDeclarativeConfig([]byte(declarativeInput), firstDirectory); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateDeclarativeConfig([]byte(declarativeInput), secondDirectory); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"demo/R1/config.cfg", "demo/generation-report.json"} {
		first, err := os.ReadFile(filepath.Join(firstDirectory, relative))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(secondDirectory, relative))
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(second) {
			t.Fatalf("generation is not deterministic for %s", relative)
		}
	}
}

func TestGenerateDeclarativeConfigRejectsUnsupportedTemplateCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"unknown model", strings.Replace(declarativeInput, "isr-1100", "catalyst-9300", 1), "no hardware catalog record"},
		{"unknown OS family", strings.Replace(declarativeInput, "ios-xe", "ios-x", 1), "no compatible template"},
		{"unsupported OS version", strings.Replace(declarativeInput, "os: ios-xe", "os: ios-xe\n      os_version: 17.9.4", 1), "OS version"},
		{"unsupported OSPF", declarativeInput + "      routing:\n        ospf:\n          process_id: 10\n", "routing.ospf"},
		{"malformed YAML", "apiVersion: [", "decode NetworkDeployment"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "out")
			_, err := GenerateDeclarativeConfig([]byte(test.input), output)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
			if _, statErr := os.Stat(filepath.Join(output, "demo", "R1", "config.cfg")); !os.IsNotExist(statErr) {
				t.Fatalf("incompatible input left a generated configuration behind: %v", statErr)
			}
		})
	}
}

func TestSelectDeviceTemplateRejectsMissingFeatureAndManufacturer(t *testing.T) {
	equipmentCatalog, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	templates, err := loadDeviceTemplates()
	if err != nil {
		t.Fatal(err)
	}
	infra, deployment, err := config.ParseNetworkDeployment([]byte(declarativeInput))
	if err != nil {
		t.Fatal(err)
	}
	record, _ := equipmentCatalog.FindByID("cisco.isr-1100")
	device := infra.Sites[0].Devices[0]
	template := templates[0]
	template.Metadata.Features = []string{"interfaces.ipv4_static"}
	evaluations, _, err := selectDeviceTemplate(device, deployment.Spec.Devices[0], record, []templateCandidate{template})
	if err == nil || len(evaluations) != 1 || len(evaluations[0].Incompatibilities) == 0 {
		t.Fatalf("missing feature was not rejected with an explanation: %#v, %v", evaluations, err)
	}
	device.Vendor = "fortinet"
	evaluations, _, err = selectDeviceTemplate(device, deployment.Spec.Devices[0], record, templates)
	if err == nil || len(evaluations[0].Incompatibilities) == 0 {
		t.Fatalf("manufacturer mismatch was not rejected: %#v, %v", evaluations, err)
	}
}

func TestValidateCatalogTemplateReferenceFailsForMissingTemplate(t *testing.T) {
	equipmentCatalog, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCatalogTemplateReferences(equipmentCatalog, nil); err == nil || !strings.Contains(err.Error(), "missing template") {
		t.Fatalf("expected absent catalog template reference to fail, got %v", err)
	}
}
