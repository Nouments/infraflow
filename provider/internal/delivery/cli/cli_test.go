package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"infraflow/internal/domain"
)

const inputYAML = `sites:
  - name: lab
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

func writeInput(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "infra.yaml")
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
