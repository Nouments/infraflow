package toolrunner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"infraflow/internal/infrastructure/safefs"
	"infraflow/pkg/protocol"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"gopkg.in/yaml.v3"
)

const (
	defaultTimeout   = 5 * time.Minute
	maxTimeout       = 30 * time.Minute
	maxArtifactBytes = 16 << 20
	maxOutputBytes   = 64 << 10
	debugMessage     = "device={{ inventory_hostname }} vendor={{ hostvars[inventory_hostname].infraflow_vendor | default('unknown') }} model={{ hostvars[inventory_hostname].infraflow_model | default('unknown') }}"
)

type Result struct {
	Tool   string
	Steps  []string
	Output string
}

type ansibleInventory struct {
	All struct {
		Children struct {
			Network struct {
				Hosts map[string]ansibleHost `yaml:"hosts"`
			} `yaml:"network"`
		} `yaml:"children"`
	} `yaml:"all"`
}

type ansibleHost struct {
	AnsibleHost     string `yaml:"ansible_host,omitempty"`
	InfraflowVendor string `yaml:"infraflow_vendor,omitempty"`
	InfraflowFamily string `yaml:"infraflow_family,omitempty"`
	InfraflowModel  string `yaml:"infraflow_model,omitempty"`
	InfraflowRole   string `yaml:"infraflow_role,omitempty"`
}

type ansiblePlay struct {
	Name        string        `yaml:"name"`
	Hosts       string        `yaml:"hosts"`
	GatherFacts bool          `yaml:"gather_facts"`
	Tasks       []ansibleTask `yaml:"tasks"`
}

type ansibleTask struct {
	Name  string `yaml:"name"`
	Debug struct {
		Msg string `yaml:"msg"`
	} `yaml:"ansible.builtin.debug"`
}

type cappedBuffer struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	truncated bool
}

func (buffer *cappedBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	remaining := maxOutputBytes - buffer.buffer.Len()
	if remaining <= 0 {
		buffer.truncated = true
		return len(data), nil
	}
	if len(data) > remaining {
		_, _ = buffer.buffer.Write(data[:remaining])
		buffer.truncated = true
		return len(data), nil
	}
	return buffer.buffer.Write(data)
}

func (buffer *cappedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	value := buffer.buffer.String()
	if buffer.truncated {
		value += "\n[output truncated]"
	}
	return value
}

func RunAnsible(ctx context.Context, stateDirectory, site string, timeout time.Duration) (Result, error) {
	result := Result{Tool: "ansible"}
	if ctx == nil {
		return result, fmt.Errorf("Ansible context is required")
	}
	if !protocol.ValidSiteName(site) {
		return result, fmt.Errorf("invalid site name")
	}
	timeout, err := checkedTimeout(timeout)
	if err != nil {
		return result, err
	}
	inventoryData, err := readArtifact(stateDirectory, filepath.ToSlash(filepath.Join(site, "ansible", "inventory.yml")))
	if err != nil {
		return result, fmt.Errorf("read Ansible inventory: %w", err)
	}
	playbookData, err := readArtifact(stateDirectory, filepath.ToSlash(filepath.Join(site, "ansible", "site.yml")))
	if err != nil {
		return result, fmt.Errorf("read Ansible playbook: %w", err)
	}
	if err := validateAnsibleInventory(inventoryData); err != nil {
		return result, err
	}
	if err := validateAnsiblePlaybook(playbookData); err != nil {
		return result, err
	}
	workDirectory, err := os.MkdirTemp("", "infraflow-ansible-")
	if err != nil {
		return result, fmt.Errorf("create Ansible workspace: %w", err)
	}
	defer os.RemoveAll(workDirectory)
	if err := os.WriteFile(filepath.Join(workDirectory, "inventory.yml"), inventoryData, 0o600); err != nil {
		return result, fmt.Errorf("stage Ansible inventory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workDirectory, "site.yml"), playbookData, 0o600); err != nil {
		return result, fmt.Errorf("stage Ansible playbook: %w", err)
	}
	configPath := filepath.Join(workDirectory, "ansible.cfg")
	if err := os.WriteFile(configPath, []byte("[defaults]\nhost_key_checking = True\nretry_files_enabled = False\ninterpreter_python = auto_silent\n"), 0o600); err != nil {
		return result, fmt.Errorf("write isolated Ansible config: %w", err)
	}
	homeDirectory := filepath.Join(workDirectory, "home")
	localTemp := filepath.Join(workDirectory, "tmp")
	if err := os.MkdirAll(homeDirectory, 0o700); err != nil {
		return result, fmt.Errorf("create Ansible home directory: %w", err)
	}
	if err := os.MkdirAll(localTemp, 0o700); err != nil {
		return result, fmt.Errorf("create Ansible temp directory: %w", err)
	}
	result.Steps = append(result.Steps, "ansible-playbook --check (debug-only playbook)")
	output, err := runCommand(ctx, timeout, workDirectory, []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + homeDirectory, "ANSIBLE_CONFIG=" + configPath,
		"ANSIBLE_LOCAL_TEMP=" + localTemp, "ANSIBLE_NOCOLOR=1", "ANSIBLE_RETRY_FILES_ENABLED=False",
	}, "ansible-playbook", "--inventory", "inventory.yml", "--limit", "network", "--check", "--forks", "1", "--timeout", "10", "site.yml")
	result.Output = output
	if err != nil {
		return result, fmt.Errorf("run read-only Ansible playbook: %w", err)
	}
	return result, nil
}

func RunTerraform(ctx context.Context, stateDirectory, site string, timeout time.Duration) (Result, error) {
	result := Result{Tool: "terraform"}
	if ctx == nil {
		return result, fmt.Errorf("Terraform context is required")
	}
	if !protocol.ValidSiteName(site) {
		return result, fmt.Errorf("invalid site name")
	}
	timeout, err := checkedTimeout(timeout)
	if err != nil {
		return result, err
	}
	workDirectory, err := os.MkdirTemp("", "infraflow-terraform-")
	if err != nil {
		return result, fmt.Errorf("create Terraform workspace: %w", err)
	}
	defer os.RemoveAll(workDirectory)
	for _, filename := range []string{"versions.tf", "providers.tf", "variables.tf", "locals.tf", "main.tf", "outputs.tf"} {
		artifactPath := filepath.ToSlash(filepath.Join(site, "terraform", filename))
		data, err := readArtifact(stateDirectory, artifactPath)
		if err != nil {
			return result, fmt.Errorf("read Terraform artifact %s: %w", filename, err)
		}
		if err := validateTerraformFile(filename, data); err != nil {
			return result, err
		}
		if err := os.WriteFile(filepath.Join(workDirectory, filename), data, 0o600); err != nil {
			return result, fmt.Errorf("stage Terraform artifact %s: %w", filename, err)
		}
	}
	if err := os.WriteFile(filepath.Join(workDirectory, "terraform.rc"), []byte(""), 0o600); err != nil {
		return result, fmt.Errorf("write isolated Terraform CLI config: %w", err)
	}
	homeDirectory := filepath.Join(workDirectory, "home")
	if err := os.MkdirAll(homeDirectory, 0o700); err != nil {
		return result, fmt.Errorf("create Terraform home directory: %w", err)
	}
	environment := []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + homeDirectory,
		"TF_IN_AUTOMATION=1", "TF_INPUT=0", "TF_CLI_CONFIG_FILE=" + filepath.Join(workDirectory, "terraform.rc"),
	}
	for _, step := range [][]string{
		{"init", "-backend=false", "-input=false", "-no-color"},
		{"validate", "-no-color"},
	} {
		result.Steps = append(result.Steps, "terraform "+strings.Join(step, " "))
		arguments := append([]string{"-chdir=" + workDirectory, step[0]}, step[1:]...)
		output, err := runCommand(ctx, timeout, workDirectory, environment, "terraform", arguments...)
		if output != "" {
			if result.Output != "" {
				result.Output += "\n"
			}
			result.Output += output
		}
		if err != nil {
			return result, fmt.Errorf("run Terraform %s: %w", step[0], err)
		}
	}
	return result, nil
}

func checkedTimeout(timeout time.Duration) (time.Duration, error) {
	if timeout == 0 {
		return defaultTimeout, nil
	}
	if timeout < 0 || timeout > maxTimeout {
		return 0, fmt.Errorf("tool timeout must be between zero and %s", maxTimeout)
	}
	return timeout, nil
}

func readArtifact(root, relativePath string) ([]byte, error) {
	file, err := safefs.OpenReadOnly(root, relativePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArtifactBytes {
		return nil, fmt.Errorf("artifact exceeds %d bytes", maxArtifactBytes)
	}
	return data, nil
}

func validateAnsibleInventory(data []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var inventory ansibleInventory
	if err := decoder.Decode(&inventory); err != nil {
		return fmt.Errorf("invalid Ansible inventory: %w", err)
	}
	if err := requireSingleYAMLDocument(decoder); err != nil {
		return fmt.Errorf("invalid Ansible inventory: %w", err)
	}
	if len(inventory.All.Children.Network.Hosts) == 0 {
		return fmt.Errorf("Ansible inventory must contain at least one network host")
	}
	for name, host := range inventory.All.Children.Network.Hosts {
		if !protocol.ValidSiteName(name) {
			return fmt.Errorf("Ansible inventory contains an unsafe host name")
		}
		if host.AnsibleHost != "" && net.ParseIP(host.AnsibleHost).To4() == nil {
			return fmt.Errorf("Ansible inventory host %q has an invalid ansible_host address", name)
		}
	}
	return nil
}

func validateAnsiblePlaybook(data []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var plays []ansiblePlay
	if err := decoder.Decode(&plays); err != nil {
		return fmt.Errorf("invalid Ansible playbook: %w", err)
	}
	if err := requireSingleYAMLDocument(decoder); err != nil {
		return fmt.Errorf("invalid Ansible playbook: %w", err)
	}
	if len(plays) != 1 || plays[0].Hosts != "network" || plays[0].GatherFacts || len(plays[0].Tasks) == 0 {
		return fmt.Errorf("Ansible runner only accepts the generated network inspection playbook")
	}
	for _, task := range plays[0].Tasks {
		if task.Debug.Msg != debugMessage {
			return fmt.Errorf("Ansible runner only accepts the generated debug task; mutating or dynamic tasks are blocked")
		}
	}
	return nil
}

func requireSingleYAMLDocument(decoder *yaml.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple YAML documents are not allowed")
		}
		return err
	}
	return nil
}

func validateTerraformFile(filename string, data []byte) error {
	file, diagnostics := hclsyntax.ParseConfig(data, filename, hcl.Pos{Line: 1, Column: 1})
	if diagnostics.HasErrors() {
		return fmt.Errorf("invalid Terraform HCL in %s: %s", filename, diagnostics.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return fmt.Errorf("Terraform file %s has an unsupported body", filename)
	}
	for _, block := range body.Blocks {
		switch block.Type {
		case "terraform":
			if len(block.Labels) != 0 || len(block.Body.Blocks) != 0 {
				return fmt.Errorf("Terraform backend/provider blocks are not allowed")
			}
			for name := range block.Body.Attributes {
				if name != "required_version" {
					return fmt.Errorf("Terraform setting %q is not allowed", name)
				}
			}
		case "locals":
			if len(block.Labels) != 0 || len(block.Body.Blocks) != 0 {
				return fmt.Errorf("nested Terraform local blocks are not allowed")
			}
		case "variable":
			if len(block.Labels) != 1 {
				return fmt.Errorf("Terraform variable blocks require one label")
			}
			for _, nested := range block.Body.Blocks {
				if nested.Type != "validation" {
					return fmt.Errorf("Terraform variable nested block %q is not allowed", nested.Type)
				}
			}
		case "output":
			if len(block.Labels) != 1 || len(block.Body.Blocks) != 0 {
				return fmt.Errorf("Terraform output blocks require one label and no nested blocks")
			}
		default:
			return fmt.Errorf("Terraform block %q could provision or execute external code and is blocked", block.Type)
		}
	}
	return nil
}

func runCommand(parent context.Context, timeout time.Duration, directory string, environment []string, executable string, arguments ...string) (string, error) {
	commandPath, err := exec.LookPath(executable)
	if err != nil {
		return "", fmt.Errorf("%s is not installed or not on PATH", executable)
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	command := exec.CommandContext(ctx, commandPath, arguments...)
	command.Dir = directory
	command.Env = environment
	output := &cappedBuffer{}
	command.Stdout = output
	command.Stderr = output
	err = command.Run()
	if ctx.Err() != nil {
		return output.String(), fmt.Errorf("%s", ctx.Err())
	}
	if err != nil {
		return output.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(output.String()))
	}
	return output.String(), nil
}
