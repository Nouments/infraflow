package toolrunner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreconfig "infraflow/internal/adapters/config"
	coregeneration "infraflow/internal/adapters/generation"
)

const testInventory = "all:\n  children:\n    network:\n      hosts:\n        R1:\n          ansible_host: 192.0.2.10\n          infraflow_vendor: cisco\n          infraflow_model: ios-xe\n          infraflow_role: router\n"

const testPlaybook = `[{"name":"Inspect declared InfraFlow devices","hosts":"network","gather_facts":false,"tasks":[{"name":"Display declared device metadata","ansible.builtin.debug":{"msg":"device={{ inventory_hostname }} vendor={{ hostvars[inventory_hostname].infraflow_vendor | default('unknown') }} model={{ hostvars[inventory_hostname].infraflow_model | default('unknown') }}"}}]}]`

func TestRunAnsibleAllowsOnlyGeneratedDebugPlaybookInCheckMode(t *testing.T) {
	stateDirectory := t.TempDir()
	writeArtifact(t, stateDirectory, "lab/ansible/inventory.yml", testInventory)
	writeArtifact(t, stateDirectory, "lab/ansible/site.yml", testPlaybook)
	installFakeTool(t, "ansible-playbook", "printf '%s\\n' \"$*\"\n")

	result, err := RunAnsible(t.Context(), stateDirectory, "lab", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "--check") || !strings.Contains(result.Output, "--limit network") {
		t.Fatalf("Ansible was not restricted to check mode and the network group: %q", result.Output)
	}
}

func TestRunAnsibleRejectsMutatingTasksBeforeRunningTool(t *testing.T) {
	stateDirectory := t.TempDir()
	writeArtifact(t, stateDirectory, "lab/ansible/inventory.yml", testInventory)
	writeArtifact(t, stateDirectory, "lab/ansible/site.yml", strings.Join([]string{"- hosts: all", "  tasks:", "    - ansible.builtin.command: id"}, "\n"))
	if _, err := RunAnsible(t.Context(), stateDirectory, "lab", time.Second); err == nil {
		t.Fatalf("mutating Ansible playbook was not rejected: %v", err)
	}
}

func TestRunAnsibleRejectsAdditionalYAMLDocuments(t *testing.T) {
	stateDirectory := t.TempDir()
	writeArtifact(t, stateDirectory, "lab/ansible/inventory.yml", testInventory)
	writeArtifact(t, stateDirectory, "lab/ansible/site.yml", testPlaybook+"\n---\n- hosts: all\n  tasks:\n    - ansible.builtin.command: id\n")
	if _, err := RunAnsible(t.Context(), stateDirectory, "lab", time.Second); err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("additional Ansible document was not rejected: %v", err)
	}
}

func TestRunAnsibleHonorsTimeout(t *testing.T) {
	stateDirectory := t.TempDir()
	writeArtifact(t, stateDirectory, "lab/ansible/inventory.yml", testInventory)
	writeArtifact(t, stateDirectory, "lab/ansible/site.yml", testPlaybook)
	installFakeTool(t, "ansible-playbook", "exec sleep 2\n")
	started := time.Now()
	if _, err := RunAnsible(t.Context(), stateDirectory, "lab", 20*time.Millisecond); err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("timed-out Ansible command returned %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("Ansible runner did not enforce its timeout")
	}
}

func TestRunAnsibleReportsExitCodeAndRedactsSensitiveOutput(t *testing.T) {
	stateDirectory := t.TempDir()
	writeArtifact(t, stateDirectory, "lab/ansible/inventory.yml", testInventory)
	writeArtifact(t, stateDirectory, "lab/ansible/site.yml", testPlaybook)
	installFakeTool(t, "ansible-playbook", "printf 'token=secret-value\\n'; exit 7\n")

	result, err := RunAnsible(t.Context(), stateDirectory, "lab", time.Second)
	if err == nil || !strings.Contains(err.Error(), "exit code 7") {
		t.Fatalf("expected a sanitized non-zero process result, got %#v, %v", result, err)
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		t.Fatalf("raw process error should not escape the runner: %v", err)
	}
	if strings.Contains(err.Error(), "secret-value") || strings.Contains(result.Output, "secret-value") || !strings.Contains(result.Output, "[REDACTED]") {
		t.Fatalf("sensitive process output was not redacted: output=%q error=%q", result.Output, err)
	}
}

func TestRunCommandCapturesStdoutAndStderrSeparately(t *testing.T) {
	directory := t.TempDir()
	script := "#!/bin/sh\n" +
		"echo 'hello stdout'\n" +
		"echo 'token=secret-value' >&2\n" +
		"echo 'verbose stderr' >&2\n" +
		"exit 3\n"
	filename := filepath.Join(directory, "capture.sh")
	if err := os.WriteFile(filename, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := runCommandDetailed(t.Context(), time.Second, directory, nil, filename)
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
	filename := filepath.Join(directory, "stream.sh")
	script := "#!/bin/sh\n" +
		"printf 'token=secret-value\\n'\n" +
		"while [ ! -f '" + release + "' ]; do :; done\n" +
		"printf 'last line without newline'\n"
	if err := os.WriteFile(filename, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	var received []ProcessOutput
	started := false
	result, err := runCommandDetailedWithOutput(t.Context(), time.Second, directory, nil, func(output ProcessOutput) {
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
	filename := filepath.Join(directory, "large.sh")
	if err := os.WriteFile(filename, []byte("#!/bin/sh\ndd if=/dev/zero bs=70000 count=1 2>/dev/null | tr '\\000' x\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := runCommandDetailed(t.Context(), time.Second, directory, nil, filename)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || !strings.Contains(result.Stdout, "[output truncated]") {
		t.Fatalf("large output was not marked as truncated: %#v", result)
	}

	started := false
	missing, err := runCommandDetailedWithOutput(t.Context(), time.Second, directory, nil, func(output ProcessOutput) {
		started = started || output.Started
	}, filepath.Join(directory, "missing"))
	if err == nil || missing.Status != "failed" || started {
		t.Fatalf("missing executable was not reported as a startup failure: result=%#v err=%v", missing, err)
	}
}

func TestRunCommandDrainsLargeStdoutAndStderrConcurrently(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "large-streams.sh")
	script := "#!/bin/sh\n" +
		"head -c 131072 /dev/zero | tr '\\000' o &\n" +
		"head -c 131072 /dev/zero | tr '\\000' e >&2 &\n" +
		"wait\n"
	if err := os.WriteFile(filename, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := runCommandDetailed(t.Context(), 3*time.Second, directory, nil, filename)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Stdout, "o") || !strings.Contains(result.Stderr, "e") || !result.Truncated {
		t.Fatalf("large simultaneous output was lost or not marked truncated: %#v", result)
	}
}

func TestRunCommandMarksLongStreamLineAsTruncated(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "long-line.sh")
	if err := os.WriteFile(filename, []byte("#!/bin/sh\nhead -c 12000 /dev/zero | tr '\\000' x\nprintf '\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var streamed ProcessOutput
	result, err := runCommandDetailedWithOutput(t.Context(), time.Second, directory, nil, func(output ProcessOutput) {
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
	filename := filepath.Join(directory, "wait.sh")
	if err := os.WriteFile(filename, []byte("#!/bin/sh\nwhile :; do :; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := runCommandDetailed(ctx, time.Second, directory, nil, filename)
	if err == nil || result.Status != "timeout" {
		t.Fatalf("cancelled command was not marked interrupted: result=%#v err=%v", result, err)
	}
}

func TestRunCommandStopsAfterRunningContextIsCancelled(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "cancel-after-start.sh")
	if err := os.WriteFile(filename, []byte("#!/bin/sh\nprintf 'started\\n'\nwhile :; do :; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result, err := runCommandDetailedWithOutput(ctx, time.Second, directory, nil, func(output ProcessOutput) {
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
	installFakeTool(t, "terraform", "printf '%s\\n' \"$*\"\n")

	result, err := RunTerraform(t.Context(), stateDirectory, "lab", time.Second)
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
	if _, err := RunTerraform(t.Context(), stateDirectory, "lab", time.Second); err == nil || !strings.Contains(err.Error(), "could provision") {
		t.Fatalf("Terraform provisioning block was not rejected: %v", err)
	}
}

func TestToolRunnersAcceptProviderGeneratedArtifactsWithFakeBinaries(t *testing.T) {
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
	if _, err := coregeneration.GenerateAnsible(infrastructure, generated); err != nil {
		t.Fatal(err)
	}
	if _, err := coregeneration.GenerateTerraform(infrastructure, generated); err != nil {
		t.Fatal(err)
	}
	stateDirectory := t.TempDir()
	for _, artifact := range []string{
		"lab/ansible/inventory.yml", "lab/ansible/site.yml",
		"lab/terraform/versions.tf", "lab/terraform/providers.tf", "lab/terraform/variables.tf",
		"lab/terraform/locals.tf", "lab/terraform/main.tf", "lab/terraform/outputs.tf",
	} {
		data, err := os.ReadFile(filepath.Join(generated, filepath.FromSlash(artifact)))
		if err != nil {
			t.Fatal(err)
		}
		writeArtifact(t, stateDirectory, artifact, string(data))
	}
	installFakeTool(t, "ansible-playbook", "printf '%s\\n' \"$*\"\n")
	ansibleResult, err := RunAnsible(t.Context(), stateDirectory, "lab", time.Second)
	if err != nil || !strings.Contains(ansibleResult.Output, "--check") {
		t.Fatalf("generated Ansible artifacts failed read-only execution: %#v, %v", ansibleResult, err)
	}
	installFakeTool(t, "terraform", "printf '%s\\n' \"$*\"\n")
	terraformResult, err := RunTerraform(t.Context(), stateDirectory, "lab", time.Second)
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

func installFakeTool(t *testing.T, name, script string) {
	t.Helper()
	directory := t.TempDir()
	filename := filepath.Join(directory, name)
	if err := os.WriteFile(filename, []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}
