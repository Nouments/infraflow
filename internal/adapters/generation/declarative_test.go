package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"infraflow/internal/adapters/catalog"
	"infraflow/internal/adapters/config"
	"infraflow/pkg/protocol"
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

const validTemplateMetadata = `id: example.template.v1
version: "1"
manufacturer: example
model: model-1
os_family: example-os
status: render-only
source: local rendering template
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
	artifactHashes := make(map[string]string, len(artifacts))
	for _, artifact := range artifacts {
		data, err := os.ReadFile(filepath.Join(output, filepath.FromSlash(artifact.Path)))
		if err != nil {
			t.Fatalf("read returned artifact %q: %v", artifact.Path, err)
		}
		if got := protocol.SHA256(data); got != artifact.OutputHash {
			t.Errorf("artifact %q hash is %s, report returned %s", artifact.Path, got, artifact.OutputHash)
		}
		if artifact.InputHash != protocol.SHA256([]byte(declarativeInput)) {
			t.Errorf("artifact %q has an incorrect input hash: %s", artifact.Path, artifact.InputHash)
		}
		artifactHashes[artifact.Path] = artifact.OutputHash
	}
	if report.Devices[0].ConfigPath != "demo/R1/config.cfg" ||
		report.Devices[0].ConfigHash != artifactHashes[report.Devices[0].ConfigPath] ||
		artifactHashes["demo/generation-report.json"] != protocol.SHA256(reportBytes) {
		t.Fatalf("report paths or hashes do not match published files: %#v", report.Devices[0])
	}
	if report.Status != "rendered_unverified" || report.CommandSyntaxValidation != "not_performed" ||
		report.PhysicalDeviceValidation != "not_performed" || report.Devices[0].TemplateID == "" ||
		report.CompatibilityScope != "declared manufacturer/model/OS matched local template metadata; physical hardware and OS compatibility are not verified" ||
		report.Devices[0].Candidates[0].Score != 93 || !report.Devices[0].Candidates[0].Selected {
		t.Fatalf("report did not expose selected candidate and score: %#v", report)
	}
	if report.Devices[0].Variables["interfaces"] == nil || report.Devices[0].CatalogStatus != "partial" {
		t.Fatalf("report omitted resolved variables or catalog status: %#v", report.Devices[0])
	}
}

func TestDecodeDeviceTemplateMetadataRejectsInvalidMetadata(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "malformed YAML", data: "id: ["},
		{name: "missing required fields", data: "id: example.template.v1\n"},
		{name: "unknown status", data: strings.Replace(validTemplateMetadata, "status: render-only", "status: supported", 1)},
		{name: "unknown field", data: validTemplateMetadata + "unexpected: true\n"},
		{name: "multiple documents", data: validTemplateMetadata + "---\nid: second.document\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeDeviceTemplateMetadata("metadata.yaml", []byte(test.data)); err == nil {
				t.Fatal("expected invalid template metadata to be rejected")
			}
		})
	}

	for _, status := range []string{"verified", "unknown", "unverified", "render-only"} {
		t.Run("status_"+status, func(t *testing.T) {
			data := strings.Replace(validTemplateMetadata, "status: render-only", "status: "+status, 1)
			if _, err := decodeDeviceTemplateMetadata("metadata.yaml", []byte(data)); err != nil {
				t.Fatalf("valid status %q was rejected: %v", status, err)
			}
		})
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

func TestGenerateDeclarativeConfigRejectsLaterDeviceWithoutPublishingPartialOutput(t *testing.T) {
	input := strings.Replace(
		declarativeInput,
		"          enabled: true\n",
		"          enabled: true\n    - name: R2\n      vendor: cisco\n      model: unsupported-model\n      os: ios-xe\n      interfaces:\n        - name: GigabitEthernet0/0/1\n          ipv4: 192.0.2.2/24\n          enabled: true\n",
		1,
	)
	output := t.TempDir()
	if _, err := GenerateDeclarativeConfig([]byte(input), output); err == nil || !strings.Contains(err.Error(), "no hardware catalog record") {
		t.Fatalf("expected unsupported second device to block generation, got %v", err)
	}
	for _, relative := range []string{"demo/R1/config.cfg", "demo/R2/config.cfg", "demo/generation-report.json"} {
		if _, err := os.Stat(filepath.Join(output, relative)); !os.IsNotExist(err) {
			t.Fatalf("failed multi-device generation published %s: %v", relative, err)
		}
	}
}

func TestGenerateDeclarativeConfigDoesNotPublishFilesWhenOutputTargetIsBlocked(t *testing.T) {
	output := t.TempDir()
	input := strings.Replace(
		declarativeInput,
		"          enabled: true\n",
		"          enabled: true\n    - name: R2\n      vendor: cisco\n      model: isr-1100\n      os: ios-xe\n      interfaces:\n        - name: GigabitEthernet0/0/1\n          ipv4: 192.0.2.2/24\n          enabled: true\n",
		1,
	)
	configPath := filepath.Join(output, "demo", "R1", "config.cfg")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("pre-existing user data"), 0o600); err != nil {
		t.Fatal(err)
	}
	blockedReport := filepath.Join(output, "demo", "generation-report.json")
	if err := os.MkdirAll(blockedReport, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := GenerateDeclarativeConfig([]byte(input), output); err == nil {
		t.Fatal("expected blocked report target to fail generation")
	}
	data, err := os.ReadFile(configPath)
	if err != nil || string(data) != "pre-existing user data" {
		t.Fatalf("failed generation changed a pre-existing artifact: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(output, "demo", "R2", "config.cfg")); !os.IsNotExist(err) {
		t.Fatalf("failed generation published a partial configuration: %v", err)
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
