package toolrunner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
