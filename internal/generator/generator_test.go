package generator

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"infraflow/internal/config"
)

func TestGenerateIsDeterministic(t *testing.T) {
	firstInput := `sites:
  - name: lab
    devices:
      - name: SW1
        vendor: cisco
        model: ios-xe
      - name: R1
        vendor: cisco
        model: csr1000v
`
	secondInput := `sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        model: csr1000v
      - name: SW1
        vendor: cisco
        model: ios-xe
`
	firstInfrastructure, err := config.Parse([]byte(firstInput))
	if err != nil {
		t.Fatal(err)
	}
	secondInfrastructure, err := config.Parse([]byte(secondInput))
	if err != nil {
		t.Fatal(err)
	}

	firstDirectory := t.TempDir()
	secondDirectory := t.TempDir()
	firstArtifacts, err := Generate(firstInfrastructure, firstDirectory)
	if err != nil {
		t.Fatal(err)
	}
	secondArtifacts, err := Generate(secondInfrastructure, secondDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstArtifacts, secondArtifacts) {
		t.Fatalf("artifact metadata differs:\n%#v\n%#v", firstArtifacts, secondArtifacts)
	}
	for _, path := range []string{"lab/inventory.json", "lab/topology.json", "lab/manifest.json"} {
		first, err := os.ReadFile(filepath.Join(firstDirectory, path))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(secondDirectory, path))
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(second) {
			t.Errorf("%s differs for equivalent input", path)
		}
	}
}

func TestGenerateCanonicalizesLinksWithSameEndpoints(t *testing.T) {
	firstInput := `sites:
  - name: lab
    devices:
      - name: R1
      - name: SW1
    links:
      - a: R1:Gi1
        b: SW1:Gi1
        network: 10.0.0.0/30
      - a: R1:Gi1
        b: SW1:Gi1
        network: 10.0.1.0/30
`
	secondInput := `sites:
  - name: lab
    devices:
      - name: R1
      - name: SW1
    links:
      - a: R1:Gi1
        b: SW1:Gi1
        network: 10.0.1.0/30
      - a: R1:Gi1
        b: SW1:Gi1
        network: 10.0.0.0/30
`
	firstInfrastructure, err := config.Parse([]byte(firstInput))
	if err != nil {
		t.Fatal(err)
	}
	secondInfrastructure, err := config.Parse([]byte(secondInput))
	if err != nil {
		t.Fatal(err)
	}

	firstDirectory := t.TempDir()
	secondDirectory := t.TempDir()
	firstArtifacts, err := Generate(firstInfrastructure, firstDirectory)
	if err != nil {
		t.Fatal(err)
	}
	secondArtifacts, err := Generate(secondInfrastructure, secondDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstArtifacts, secondArtifacts) {
		t.Fatalf("artifact metadata differs:\n%#v\n%#v", firstArtifacts, secondArtifacts)
	}
	for _, relativePath := range []string{"lab/inventory.json", "lab/topology.json", "lab/manifest.json"} {
		first, err := os.ReadFile(filepath.Join(firstDirectory, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(secondDirectory, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(second) {
			t.Errorf("%s differs for equivalent link ordering", relativePath)
		}
	}
}

func TestGenerateRefusesSiteDirectorySymlink(t *testing.T) {
	infrastructure, err := config.Parse([]byte("sites:\n  - name: lab\n"))
	if err != nil {
		t.Fatal(err)
	}
	outputDirectory := t.TempDir()
	outsideDirectory := t.TempDir()
	if err := os.Symlink(outsideDirectory, filepath.Join(outputDirectory, "lab")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Generate(infrastructure, outputDirectory); err == nil {
		t.Fatal("expected symlink output path to be rejected")
	}
	if _, err := os.Stat(filepath.Join(outsideDirectory, "inventory.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected write through symlink, stat error: %v", err)
	}
}

func TestGenerateSupportsMultipleSites(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: office-a
    devices:
      - name: R1
  - name: office-b
    devices:
      - name: R2
`))
	if err != nil {
		t.Fatal(err)
	}
	outputDirectory := t.TempDir()
	artifacts, err := Generate(infrastructure, outputDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 4 {
		t.Fatalf("expected inventory and topology for both sites, got %#v", artifacts)
	}
	for _, relativePath := range []string{
		"office-a/inventory.json", "office-a/topology.json",
		"office-b/inventory.json", "office-b/topology.json",
	} {
		if _, err := os.Stat(filepath.Join(outputDirectory, relativePath)); err != nil {
			t.Errorf("missing site artifact %s: %v", relativePath, err)
		}
	}
}

func TestGenerateAnsibleIsDeterministicAndReadOnly(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    devices:
      - name: R1
        role: router
        vendor: cisco
        family: ios-xe
        model: csr1000v
        management:
          ipv4: 10.0.0.1
      - name: SW1
        vendor: cisco
        model: ios-xe
`))
	if err != nil {
		t.Fatal(err)
	}
	firstDirectory := t.TempDir()
	secondDirectory := t.TempDir()
	first, err := GenerateAnsible(infrastructure, firstDirectory)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateAnsible(infrastructure, secondDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || len(first) != 2 {
		t.Fatalf("unexpected Ansible metadata: %#v %#v", first, second)
	}
	for _, relativePath := range []string{"lab/ansible/inventory.yml", "lab/ansible/site.yml"} {
		firstData, err := os.ReadFile(filepath.Join(firstDirectory, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		secondData, err := os.ReadFile(filepath.Join(secondDirectory, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		if string(firstData) != string(secondData) {
			t.Errorf("%s differs between equivalent inputs", relativePath)
		}
	}
	if _, err := os.Stat(filepath.Join(firstDirectory, "lab", "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("Ansible-only generation must not silently publish a provider manifest: %v", err)
	}
}

func TestGenerateAnsibleRefusesSymlinkedParent(t *testing.T) {
	infrastructure, err := config.Parse([]byte("sites:\n  - name: lab\n"))
	if err != nil {
		t.Fatal(err)
	}
	outputDirectory := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outputDirectory, "lab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(outputDirectory, "lab", "ansible")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := GenerateAnsible(infrastructure, outputDirectory); err == nil {
		t.Fatal("expected Ansible generation to reject symlinked parent")
	}
}

func TestGenerateTerraformIsDeterministicAndDataOnly(t *testing.T) {
	input := `sites:
  - name: lab
    bootstrap:
      network: 192.168.100.0/24
    services:
      dns: true
    devices:
      - name: R1
        role: router
        vendor: cisco
        family: ios-xe
        model: csr1000v
        management:
          ipv4: 192.168.100.10
    links:
      - a: R1:Gi1
        b: R1:Gi2
        network: 10.0.0.0/30
`
	infrastructure, err := config.Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	firstDirectory := t.TempDir()
	secondDirectory := t.TempDir()
	first, err := GenerateTerraform(infrastructure, firstDirectory)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateTerraform(infrastructure, secondDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || len(first) != 7 {
		t.Fatalf("unexpected Terraform metadata: %#v %#v", first, second)
	}
	for _, relativePath := range []string{
		"lab/terraform/versions.tf", "lab/terraform/providers.tf", "lab/terraform/variables.tf",
		"lab/terraform/locals.tf", "lab/terraform/main.tf", "lab/terraform/outputs.tf",
		"lab/terraform/terraform.tfvars.example",
	} {
		firstData, err := os.ReadFile(filepath.Join(firstDirectory, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		secondData, err := os.ReadFile(filepath.Join(secondDirectory, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		if string(firstData) != string(secondData) {
			t.Errorf("%s differs between equivalent inputs", relativePath)
		}
	}
	locals, err := os.ReadFile(filepath.Join(firstDirectory, "lab", "terraform", "locals.tf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(locals), `"environment" = var.environment`) {
		t.Fatalf("Terraform variable expression is missing: %s", locals)
	}
	if _, err := os.Stat(filepath.Join(firstDirectory, "lab", "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("Terraform-only generation must not silently publish a provider manifest: %v", err)
	}
}

func TestGenerateTerraformEscapesUserStrings(t *testing.T) {
	infrastructure, err := config.Parse([]byte(`sites:
  - name: lab
    devices:
      - name: '${dangerous_expression}'
`))
	if err != nil {
		t.Fatal(err)
	}
	outputDirectory := t.TempDir()
	if _, err := GenerateTerraform(infrastructure, outputDirectory); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(outputDirectory, "lab", "terraform", "locals.tf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"${dangerous_expression}"`) {
		t.Fatalf("user expression was not escaped as a string: %s", data)
	}
}
