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
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil || plan.Status != "PLANNED" {
		t.Fatalf("unsupported device should still produce a planned task model: %#v, %v", plan, err)
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

func TestCapabilitySummaryCommandReportsReadyState(t *testing.T) {
	input := `capability_registry:
  entries:
    cisco:iosxe:ios-xe:
      netconf:
        method: netconf
        state: LAB-VERIFIED
        evidence: lab test 2026-10-07

template_registry:
  entries:
    cisco:iosxe:ios-xe:1:
      id: cisco:iosxe:ios-xe:1
      vendor: cisco
      family: iosxe
      model: ios-xe
      version: "1"
      hash: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      evidence: sandbox validation manifest checked locally

sites:
  - name: sandbox
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        model: ios-xe
        provisioning:
          method: netconf
          template_version: "1"
`
	inputPath := writeInput(t, input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"capability-summary", "-f", inputPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("capability-summary failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "LAB-VERIFIED") || !strings.Contains(stdout.String(), "ready") || !strings.Contains(stdout.String(), "cisco:iosxe:ios-xe:1") {
		t.Fatalf("capability-summary did not report readiness: %s", stdout.String())
	}
}

func TestCapabilityMatrixCommandReportsEvidenceAndReadiness(t *testing.T) {
	inputPath := writeInput(t, `capability_registry:
  entries:
    cisco:iosxe:ios-xe:
      netconf:
        method: netconf
        state: LAB-VERIFIED
        evidence: lab test 2026-10-07
      ssh:
        method: ssh
        state: UNVERIFIED
        evidence: no adapter execution evidence

template_registry:
  entries:
    cisco:iosxe:ios-xe:1:
      id: cisco:iosxe:ios-xe:1
      vendor: cisco
      family: iosxe
      model: ios-xe
      version: "1"
      hash: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      evidence: audited sandbox manifest

sites:
  - name: sandbox
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        model: ios-xe
        provisioning:
          method: netconf
          template_version: "1"
`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"capability-matrix", "-f", inputPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("capability-matrix failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "state=LAB-VERIFIED status=ready") || !strings.Contains(stdout.String(), "state=UNVERIFIED status=not-ready") {
		t.Fatalf("capability-matrix did not report evidence-based states: %s", stdout.String())
	}
}

func TestApplyCreatesBlockedPlanningJobAndAuditEvent(t *testing.T) {
	inputPath := writeInput(t, `sites:
  - name: sandbox
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        model: ios-xe
        provisioning:
          method: netconf
`)
	dataDir := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"apply", "-f", inputPath, "-data-dir", dataDir}, &stdout, &stderr); code != 0 {
		t.Fatalf("apply failed: %d, %s", code, stderr.String())
	}
	jobFiles, err := filepath.Glob(filepath.Join(dataDir, ".jobs", "*.json"))
	if err != nil || len(jobFiles) != 1 {
		t.Fatalf("expected one persisted planning job, got %d files: %v", len(jobFiles), err)
	}
	eventFiles, err := filepath.Glob(filepath.Join(dataDir, ".events.jsonl"))
	if err != nil || len(eventFiles) != 1 {
		t.Fatalf("expected one audit event store, got %d files: %v", len(eventFiles), err)
	}
	if !strings.Contains(stdout.String(), "job=") || !strings.Contains(stdout.String(), "status=planned") {
		t.Fatalf("apply did not report persisted planned job: %s", stdout.String())
	}
}

func TestTemplateInfoCommandReportsSelectionAndBlocking(t *testing.T) {
	input := `capability_registry:
  entries:
    cisco:iosxe:ios-xe:
      netconf:
        method: netconf
        state: UNVERIFIED
        evidence: no lab execution or verified adapter was recorded

template_registry:
  entries:
    cisco:iosxe:ios-xe:1:
      id: cisco:iosxe:ios-xe:1
      vendor: cisco
      family: iosxe
      model: ios-xe
      version: "1"
      hash: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      evidence: sandbox validation manifest checked locally

sites:
  - name: sandbox
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        model: ios-xe
        provisioning:
          method: netconf
          template_version: "1"
`
	inputPath := writeInput(t, input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"template-info", "-f", inputPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("template-info failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "selected template") || !strings.Contains(stdout.String(), "UNVERIFIED") || !strings.Contains(stdout.String(), "blocked") {
		t.Fatalf("template-info did not report selection and blocking: %s", stdout.String())
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
