package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadLoopbackAndRemoteTLSConfigurations(t *testing.T) {
	loopback := writeConfig(t, `agent:
  id: site-agent-01
  state_directory: ./agent-state
provider:
  address: 127.0.0.1:8443
  token_env: INFRAFLOW_AGENT_TOKEN
  tls:
    enabled: false
`)
	if _, err := Load(loopback); err != nil {
		t.Fatal(err)
	}
	remote := writeConfig(t, `agent:
  id: site-agent-02
  state_directory: /var/lib/infraflow
provider:
  address: 203.0.113.10:8443
  token_env: INFRAFLOW_AGENT_TOKEN
  tls:
    enabled: true
    ca_file: /etc/infraflow/provider-ca.pem
`)
	if _, err := Load(remote); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsRemotePlaintextAndUnknownFields(t *testing.T) {
	remotePlaintext := writeConfig(t, `agent:
  id: agent-01
  state_directory: ./state
provider:
  address: 203.0.113.10:8443
  token_env: INFRAFLOW_AGENT_TOKEN
  tls:
    enabled: false
`)
	if _, err := Load(remotePlaintext); err == nil || !strings.Contains(err.Error(), "tls.enabled is required") {
		t.Fatalf("expected remote plaintext rejection, got %v", err)
	}
	unknown := writeConfig(t, "agent:\n  id: agent-01\n  state_directory: ./state\n  surprise: true\nprovider:\n  address: localhost:8443\n  token_env: TOKEN\n")
	if _, err := Load(unknown); err == nil || !strings.Contains(err.Error(), "surprise") {
		t.Fatalf("expected unknown field rejection, got %v", err)
	}
}

func TestLoadValidatesAgentAPIAddressAndIdentity(t *testing.T) {
	valid := writeConfig(t, `agent:
  id: agent-01
  site_id: site-01
  capabilities: [inventory, topology]
  state_directory: ./state
provider:
  address: localhost:8443
  api_address: http://127.0.0.1:8080
  token_env: TOKEN
`)
	if _, err := Load(valid); err != nil {
		t.Fatal(err)
	}
	unsafe := writeConfig(t, `agent:
  id: agent-01
  site_id: "bad site"
  state_directory: ./state
provider:
  address: localhost:8443
  api_address: http://203.0.113.10:8080
  token_env: TOKEN
`)
	if _, err := Load(unsafe); err == nil {
		t.Fatal("expected unsafe agent identity and API address to be rejected")
	}
}

func TestValidateRequiresProviderPortAndTokenEnvironment(t *testing.T) {
	config := Config{
		Agent:    AgentConfig{ID: "agent-01", StateDirectory: "./state"},
		Provider: ProviderConfig{Address: "localhost:0", TokenEnv: "TOKEN"},
	}
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "invalid port") {
		t.Fatalf("expected port validation error, got %v", err)
	}
}

func TestAgentDeploymentTemplateLoads(t *testing.T) {
	if _, err := Load(filepath.Join("..", "..", "..", "examples", "agent-config.yaml")); err != nil {
		t.Fatalf("agent deployment template is invalid: %v", err)
	}
}

func writeConfig(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
