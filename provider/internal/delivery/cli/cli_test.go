package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	coregeneration "infraflow/internal/adapters/generation"
	"infraflow/internal/domain"
)

const inputYAML = `sites:
  - name: lab
    bootstrap:
      network: 192.168.100.0/24
    devices:
      - name: R1
        vendor: cisco
        model: ios-xe
`

func TestCommandsValidatePlanAndGenerate(t *testing.T) {
	inputPath := writeInput(t, inputYAML)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"validate", "-f", inputPath}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "Valid: 1 site(s)") {
		t.Fatalf("validate failed: %d, %q, %q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"plan", "-f", inputPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("plan failed: %s", stderr.String())
	}
	var plan domain.Plan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil || plan.Status != "blocked" {
		t.Fatalf("unsupported device should remain blocked: %#v, %v", plan, err)
	}

	outputPath := filepath.Join(t.TempDir(), "artifacts")
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"generate", "-f", inputPath, "-out", outputPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("generate failed: %s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(outputPath, "lab", "manifest.json")); err != nil {
		t.Fatalf("provider manifest missing: %v", err)
	}
	ansibleOutput := filepath.Join(t.TempDir(), "ansible")
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"generate-ansible", "-f", inputPath, "-out", ansibleOutput}, &stdout, &stderr); code != 0 {
		t.Fatalf("generate-ansible failed: %s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(ansibleOutput, "lab", "ansible", "inventory.yml")); err != nil {
		t.Fatalf("Ansible inventory missing: %v", err)
	}
	terraformOutput := filepath.Join(t.TempDir(), "terraform")
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"generate-terraform", "-f", inputPath, "-out", terraformOutput}, &stdout, &stderr); code != 0 {
		t.Fatalf("generate-terraform failed: %s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(terraformOutput, "lab", "terraform", "locals.tf")); err != nil {
		t.Fatalf("Terraform locals missing: %v", err)
	}
	bootstrapOutput := filepath.Join(t.TempDir(), "bootstrap")
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"generate-bootstrap", "-f", inputPath, "-out", bootstrapOutput}, &stdout, &stderr); code != 0 {
		t.Fatalf("generate-bootstrap failed: %s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(bootstrapOutput, "lab", "bootstrap", "dhcp", "config.json")); err != nil {
		t.Fatalf("DHCP bootstrap configuration missing: %v", err)
	}
}

func TestGenerateAllPublishesCompleteAgentCatalog(t *testing.T) {
	input := `sites:
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
`
	inputPath := writeInput(t, input)
	outputPath := filepath.Join(t.TempDir(), "published")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"generate-all", "-f", inputPath, "-out", outputPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("generate-all failed: %s", stderr.String())
	}
	catalog, err := coregeneration.Catalog(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 17 {
		t.Fatalf("expected 17 complete artifacts in agent catalog, got %d", len(catalog))
	}
	if !strings.Contains(stdout.String(), "generated and published lab/ansible/site.yml") || !strings.Contains(stdout.String(), "generated and published lab/terraform/main.tf") {
		t.Fatalf("generate-all did not report tool artifacts as published: %s", stdout.String())
	}
}

func TestServeRejectsWeakToken(t *testing.T) {
	t.Setenv("INFRAFLOW_TEST_AGENT_TOKEN", "weak")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	configPath := writeServeConfig(t, t.TempDir())
	code := Run([]string{"serve", "-config", configPath}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "at least") {
		t.Fatalf("expected weak token error, got %d and %q", code, stderr.String())
	}
}

func TestServeRequiresArtifactDirectory(t *testing.T) {
	t.Setenv("INFRAFLOW_TEST_AGENT_TOKEN", strings.Repeat("x", 32))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	missingDirectory := filepath.Join(t.TempDir(), "missing")
	configPath := writeServeConfig(t, missingDirectory)
	code := Run([]string{"serve", "-config", configPath}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "artifact directory") {
		t.Fatalf("expected artifact root error, got %d and %q", code, stderr.String())
	}
}

func writeServeConfig(t *testing.T, artifactDirectory string) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "provider.yaml")
	contents := "listen_address: 127.0.0.1:8443\n" +
		"artifact_directory: " + strconv.Quote(artifactDirectory) + "\n" +
		"token_env: INFRAFLOW_TEST_AGENT_TOKEN\n"
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath
}

func TestValidateRejectsUnknownFields(t *testing.T) {
	inputPath := writeInput(t, "sites:\n  - name: lab\n    unsupported: true\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"validate", "-f", inputPath}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "unsupported") {
		t.Fatalf("expected invalid input error, got %d and %q", code, stderr.String())
	}
}

func TestProviderCLIUsageAndMissingArguments(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run(nil, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("empty invocation did not print usage: code=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--help"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "generate-all") {
		t.Fatalf("help did not list generate-all: code=%d stdout=%q", code, stdout.String())
	}
	for _, command := range []string{"validate", "plan", "generate-all", "serve"} {
		stdout.Reset()
		stderr.Reset()
		if code := Run([]string{command}, &stdout, &stderr); code != 2 {
			t.Errorf("%s accepted missing arguments: code=%d stderr=%q", command, code, stderr.String())
		}
	}
}

func TestServeReportsInvalidConfigAndTLSCertificate(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"serve", "-config", filepath.Join(t.TempDir(), "missing.yaml")}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "open provider config") {
		t.Fatalf("missing provider config returned %d: %q", code, stderr.String())
	}
	artifactDirectory := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "provider.yaml")
	contents := "listen_address: localhost:8443\n" +
		"artifact_directory: " + strconv.Quote(artifactDirectory) + "\n" +
		"token_env: INFRAFLOW_TEST_AGENT_TOKEN\n" +
		"tls:\n  certificate_file: invalid.crt\n  key_file: invalid.key\n"
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INFRAFLOW_TEST_AGENT_TOKEN", strings.Repeat("x", 32))
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"serve", "-config", configPath}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "load TLS certificate") {
		t.Fatalf("invalid TLS certificate returned %d: %q", code, stderr.String())
	}
}

func TestServeReportsOccupiedAPIListener(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	artifactDirectory := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "provider.yaml")
	contents := "listen_address: 127.0.0.1:8444\n" +
		"api_listen_address: " + occupied.Addr().String() + "\n" +
		"artifact_directory: " + strconv.Quote(artifactDirectory) + "\n" +
		"token_env: INFRAFLOW_TEST_AGENT_TOKEN\n"
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INFRAFLOW_TEST_AGENT_TOKEN", strings.Repeat("x", 32))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"serve", "-config", configPath}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "API listen") {
		t.Fatalf("occupied API address returned %d: %q", code, stderr.String())
	}
}

func writeInput(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "infra.yaml")
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
