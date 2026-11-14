package toolrunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infraflow/agent/internal/testutil"
	coreconfig "infraflow/internal/adapters/config"
	coregeneration "infraflow/internal/adapters/generation"
)

func TestRunCommandCapturesStdoutAndStderrSeparately(t *testing.T) {
	directory := t.TempDir()
	filename := installCommandHelper(t, directory, "capture")
	result, err := runCommandDetailed(t.Context(), 10*time.Second, directory, nil, filename)
	if err == nil || !strings.Contains(err.Error(), "exit code 3") {
		t.Fatalf("expected a non-zero exit code, got result=%#v err=%v", result, err)
	}
	if result.ExitCode != 3 || result.Status != "failed" {
		t.Fatalf("unexpected exit metadata: %#v", result)
	}
	if !strings.Contains(result.Stdout, "hello stdout") || strings.Contains(result.Stderr, "hello stdout") {
		t.Fatalf("stdout and stderr were merged unexpectedly: stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "[REDACTED]") || strings.Contains(result.Stderr, "secret-value") {
		t.Fatalf("stderr was not redacted as expected: stderr=%q", result.Stderr)
	}
	if strings.Contains(result.Output, "secret-value") {
		t.Fatalf("sensitive process output leaked into the combined output: %q", result.Output)
	}
}

func TestRunCommandStreamsRedactedOutputBeforeExitAndKeepsFinalLine(t *testing.T) {
	directory := t.TempDir()
	release := filepath.Join(directory, "release")
	filename := installCommandHelper(t, directory, "stream")
	var received []ProcessOutput
	started := false
	result, err := runCommandDetailedWithOutput(t.Context(), 10*time.Second, directory, nil, func(output ProcessOutput) {
		if output.Started {
			started = true
			return
		}
		received = append(received, output)
		if strings.Contains(output.Text, "[REDACTED]") {
			if strings.Contains(output.Text, "secret-value") || !strings.Contains(output.Text, "[REDACTED]") {
				t.Errorf("observer received unredacted output: %#v", output)
			}
			if err := os.WriteFile(release, nil, 0o600); err != nil {
				t.Errorf("release waiting process: %v", err)
			}
		}
	}, filename)
	if err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("observer did not report successful process start")
	}
	if len(received) != 2 {
		t.Fatalf("observer received %d chunks, want streamed line plus final line: %#v", len(received), received)
	}
	if received[0].Stream != "stdout" || !strings.Contains(received[0].Text, "[REDACTED]") {
		t.Fatalf("unexpected streamed output: %#v", received[0])
	}
	if !strings.Contains(received[1].Text, "last line without newline") {
		t.Fatalf("final unterminated line was lost: %#v", received)
	}
	if !strings.Contains(result.Stdout, "last line without newline") || strings.Contains(result.Stdout, "secret-value") {
		t.Fatalf("final captured output is incomplete or exposed a secret: %#v", result)
	}
}

func TestRunCommandReportsTruncationAndStartupFailure(t *testing.T) {
	directory := t.TempDir()
	filename := installCommandHelper(t, directory, "large")
	result, err := runCommandDetailed(t.Context(), 10*time.Second, directory, nil, filename)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || !strings.Contains(result.Stdout, "[output truncated]") {
		t.Fatalf("large output was not marked as truncated: %#v", result)
	}

	started := false
	missing, err := runCommandDetailedWithOutput(t.Context(), 10*time.Second, directory, nil, func(output ProcessOutput) {
		started = started || output.Started
	}, filepath.Join(directory, "missing"))
	if err == nil || missing.Status != "failed" || started {
		t.Fatalf("missing executable was not reported as a startup failure: result=%#v err=%v", missing, err)
	}
}

func TestRunCommandDrainsLargeStdoutAndStderrConcurrently(t *testing.T) {
	directory := t.TempDir()
	filename := installCommandHelper(t, directory, "large-streams")
	result, err := runCommandDetailed(t.Context(), 10*time.Second, directory, nil, filename)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Stdout, "o") || !strings.Contains(result.Stderr, "e") || !result.Truncated {
		t.Fatalf("large simultaneous output was lost or not marked truncated: %#v", result)
	}
}

func TestRunCommandMarksLongStreamLineAsTruncated(t *testing.T) {
	directory := t.TempDir()
	filename := installCommandHelper(t, directory, "long-line")
	var streamed ProcessOutput
	result, err := runCommandDetailedWithOutput(t.Context(), 10*time.Second, directory, nil, func(output ProcessOutput) {
		if !output.Started {
			streamed = output
		}
	}, filename)
	if err != nil {
		t.Fatal(err)
	}
	if streamed.Stream != "stdout" || !streamed.Truncated || !strings.Contains(streamed.Text, "[output line truncated]") || result.Truncated {
		t.Fatalf("line truncation was not distinguished from full retained output: streamed=%#v result=%#v", streamed, result)
	}
}

func TestRunCommandHonorsCancellation(t *testing.T) {
	directory := t.TempDir()
	filename := installCommandHelper(t, directory, "wait")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := runCommandDetailed(ctx, 10*time.Second, directory, nil, filename)
	if err == nil || result.Status != "timeout" {
		t.Fatalf("cancelled command was not marked interrupted: result=%#v err=%v", result, err)
	}
}

func TestRunCommandStopsAfterRunningContextIsCancelled(t *testing.T) {
	directory := t.TempDir()
	filename := installCommandHelper(t, directory, "wait-after-start")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result, err := runCommandDetailedWithOutput(ctx, 10*time.Second, directory, nil, func(output ProcessOutput) {
		if strings.Contains(output.Text, "started") {
			cancel()
		}
	}, filename)
	if err == nil || result.Status != "timeout" {
		t.Fatalf("running command ignored context cancellation: result=%#v err=%v", result, err)
	}
}

func TestRunTerraformInitializesAndValidatesOnly(t *testing.T) {
	stateDirectory := t.TempDir()
	for filename, data := range map[string]string{
		"versions.tf":  `terraform { required_version = ">= 1.4.0" }`,
		"providers.tf": `# no providers`,
		"variables.tf": `variable "environment" {
  type = string
  default = "generated"
}`,
		"locals.tf":  `locals { site_names = ["lab"] }`,
		"main.tf":    `# data only`,
		"outputs.tf": `output "site_names" { value = local.site_names }`,
	} {
		writeArtifact(t, stateDirectory, "lab/terraform/"+filename, data)
	}
	installFakeTool(t, "terraform")

	result, err := RunTerraform(t.Context(), stateDirectory, "lab", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) != 2 || !strings.Contains(result.Steps[0], "init") || !strings.Contains(result.Steps[1], "validate") {
		t.Fatalf("Terraform runner executed unexpected steps: %#v", result.Steps)
	}
	if !strings.Contains(result.Output, "-backend=false") || !strings.Contains(result.Output, "validate") || strings.Contains(result.Output, "apply") || strings.Contains(result.Output, "plan") {
		t.Fatalf("Terraform runner must not apply or plan resources: %q", result.Output)
	}
}

func TestRunTerraformRejectsProvisioningBlocksBeforeRunningTool(t *testing.T) {
	stateDirectory := t.TempDir()
	for filename, data := range map[string]string{
		"versions.tf":  `terraform { required_version = ">= 1.4.0" }`,
		"providers.tf": `# no providers`,
		"variables.tf": `variable "environment" { type = string }`,
		"locals.tf":    `locals { site_names = ["lab"] }`,
		"main.tf":      `resource "null_resource" "danger" {}`,
		"outputs.tf":   `output "site_names" { value = local.site_names }`,
	} {
		writeArtifact(t, stateDirectory, "lab/terraform/"+filename, data)
	}
	if _, err := RunTerraform(t.Context(), stateDirectory, "lab", 10*time.Second); err == nil || !strings.Contains(err.Error(), "could provision") {
		t.Fatalf("Terraform provisioning block was not rejected: %v", err)
	}
}

func TestToolRunnerAcceptsProviderGeneratedTerraformWithFakeBinary(t *testing.T) {
	infrastructure, err := coreconfig.Parse([]byte(`sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: ios-xe
        model: csr1000v
        management:
          ipv4: 192.0.2.10
`))
	if err != nil {
		t.Fatal(err)
	}
	generated := t.TempDir()
	if _, err := coregeneration.GenerateTerraform(infrastructure, generated); err != nil {
		t.Fatal(err)
	}
	stateDirectory := t.TempDir()
	for _, artifact := range []string{
		"lab/terraform/versions.tf", "lab/terraform/providers.tf", "lab/terraform/variables.tf",
		"lab/terraform/locals.tf", "lab/terraform/main.tf", "lab/terraform/outputs.tf",
	} {
		data, err := os.ReadFile(filepath.Join(generated, filepath.FromSlash(artifact)))
		if err != nil {
			t.Fatal(err)
		}
		writeArtifact(t, stateDirectory, artifact, string(data))
	}
	installFakeTool(t, "terraform")
	terraformResult, err := RunTerraform(t.Context(), stateDirectory, "lab", 10*time.Second)
	if err != nil || len(terraformResult.Steps) != 2 || !strings.Contains(terraformResult.Output, "validate") {
		t.Fatalf("generated Terraform artifacts failed fake init/validate: %#v, %v", terraformResult, err)
	}
}

func writeArtifact(t *testing.T, root, name, contents string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func installFakeTool(t *testing.T, name string) {
	t.Helper()
	directory := t.TempDir()
	if _, err := testutil.BuildFakeCommand(directory, name); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func installCommandHelper(t *testing.T, directory, name string) string {
	t.Helper()
	path, err := testutil.BuildFakeCommand(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
