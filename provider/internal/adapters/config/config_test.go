package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadLoopbackAndRemoteTLSConfigs(t *testing.T) {
	loopback := writeProviderConfig(t, `listen_address: 127.0.0.1:8443
artifact_directory: ./provider-data
token_env: INFRAFLOW_AGENT_TOKEN
`)
	config, err := Load(loopback)
	if err != nil || config.ChunkSize != defaultChunk || config.DatabasePath == "" || config.AdminUsername != "admin" || config.AdminCredentialFile == "" || config.AdminCredentialScript == "" || config.SessionTTLMinutes != 480 {
		t.Fatalf("unexpected local config: %#v, %v", config, err)
	}
	remote := writeProviderConfig(t, `listen_address: 0.0.0.0:8443
artifact_directory: /var/lib/infraflow/artifacts
token_env: INFRAFLOW_AGENT_TOKEN
chunk_size: 32768
tls:
  certificate_file: /etc/infraflow/server.crt
  key_file: /etc/infraflow/server.key
`)
	if config, err := Load(remote); err != nil || config.ChunkSize != 32768 {
		t.Fatalf("unexpected remote config: %#v, %v", config, err)
	}
	api := writeProviderConfig(t, `listen_address: 127.0.0.1:8443
api_listen_address: localhost:8080
web_ui_enabled: true
artifact_directory: ./provider-data
token_env: INFRAFLOW_AGENT_TOKEN
`)
	if config, err := Load(api); err != nil || config.APIListenAddress != "localhost:8080" || !config.WebUIEnabled {
		t.Fatalf("unexpected API config: %#v, %v", config, err)
	}
}

func TestWebUIRequiresAPIListener(t *testing.T) {
	path := writeProviderConfig(t, `listen_address: 127.0.0.1:8443
web_ui_enabled: true
artifact_directory: ./provider-data
token_env: INFRAFLOW_AGENT_TOKEN
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "requires api_listen_address") {
		t.Fatalf("expected web UI to require API listener, got %v", err)
	}
}

func TestLoadAppliesGNS3EnvironmentOverrides(t *testing.T) {
	t.Setenv("INFRAFLOW_PROVIDER_LISTEN_ADDRESS", "0.0.0.0:8443")
	t.Setenv("INFRAFLOW_PROVIDER_API_LISTEN_ADDRESS", "0.0.0.0:8080")
	t.Setenv("INFRAFLOW_PROVIDER_TOKEN_ENV", "INFRAFLOW_AGENT_TOKEN")
	t.Setenv("INFRAFLOW_PROVIDER_TLS_CERTIFICATE", "/run/secrets/provider.crt")
	t.Setenv("INFRAFLOW_PROVIDER_TLS_KEY", "/run/secrets/provider.key")
	t.Setenv("INFRAFLOW_PROVIDER_DATA", "/var/lib/infraflow-provider")
	t.Setenv("INFRAFLOW_PROVIDER_DATABASE", "")
	t.Setenv("INFRAFLOW_PROVIDER_ADMIN_CREDENTIAL_FILE", "")
	t.Setenv("INFRAFLOW_PROVIDER_ADMIN_CREDENTIAL_SCRIPT", "")

	path := writeProviderConfig(t, `listen_address: 127.0.0.1:8443
api_listen_address: 127.0.0.1:8080
artifact_directory: ./provider-data
token_env: TOKEN
`)
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.ListenAddress != "0.0.0.0:8443" || config.APIListenAddress != "0.0.0.0:8080" || config.TokenEnv != "INFRAFLOW_AGENT_TOKEN" {
		t.Fatalf("provider listener/token overrides were not applied: %#v", config)
	}
	if config.ArtifactDirectory != "/var/lib/infraflow-provider" || config.DatabasePath != "/var/lib/infraflow-provider/.infraflow-users.sqlite3" {
		t.Fatalf("provider data paths were not applied: %#v", config)
	}
	if config.TLS.CertificateFile != "/run/secrets/provider.crt" || config.TLS.KeyFile != "/run/secrets/provider.key" {
		t.Fatalf("provider TLS overrides were not applied: %#v", config.TLS)
	}
}

func TestLoadValidatesSiteScopedAgentCredentials(t *testing.T) {
	path := writeProviderConfig(t, `listen_address: localhost:8443
artifact_directory: ./provider-data
token_env: INFRAFLOW_AGENT_TOKEN
agent_credentials:
  - agent_id: agent-a
    site_id: site-a
    token_env: INFRAFLOW_SITE_A_TOKEN
  - agent_id: agent-b
    site_id: site-b
    token_env: INFRAFLOW_SITE_B_TOKEN
`)
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.AgentCredentials) != 2 || config.AgentCredentials[0].SiteID != "site-a" {
		t.Fatalf("site-scoped agent credentials were not loaded: %#v", config.AgentCredentials)
	}

	duplicate := writeProviderConfig(t, `listen_address: localhost:8443
artifact_directory: ./provider-data
token_env: INFRAFLOW_AGENT_TOKEN
agent_credentials:
  - agent_id: agent-a
    site_id: site-a
    token_env: TOKEN_A
  - agent_id: agent-a
    site_id: site-b
    token_env: TOKEN_B
`)
	if _, err := Load(duplicate); err == nil || !strings.Contains(err.Error(), "duplicate agent credential") {
		t.Fatalf("expected duplicate agent credentials to be rejected, got %v", err)
	}
}

func TestLoadRejectsRemotePlaintextAndUnknownFields(t *testing.T) {
	remote := writeProviderConfig(t, `listen_address: 0.0.0.0:8443
artifact_directory: ./provider-data
token_env: TOKEN
`)
	if _, err := Load(remote); err == nil || !strings.Contains(err.Error(), "TLS certificate") {
		t.Fatalf("expected remote TLS requirement, got %v", err)
	}
	unknown := writeProviderConfig(t, "listen_address: localhost:8443\nartifact_directory: ./data\ntoken_env: TOKEN\nsurprise: true\n")
	if _, err := Load(unknown); err == nil || !strings.Contains(err.Error(), "surprise") {
		t.Fatalf("expected unknown field rejection, got %v", err)
	}
	remoteAPI := writeProviderConfig(t, `listen_address: 127.0.0.1:8443
api_listen_address: 0.0.0.0:8080
artifact_directory: ./provider-data
token_env: TOKEN
`)
	if _, err := Load(remoteAPI); err == nil || !strings.Contains(err.Error(), "api_listen_address requires TLS") {
		t.Fatalf("expected remote API rejection, got %v", err)
	}
}

func TestProviderDeploymentTemplateLoads(t *testing.T) {
	if _, err := Load(filepath.Join("..", "..", "..", "..", "examples", "provider-config.yaml")); err != nil {
		t.Fatalf("provider deployment template is invalid: %v", err)
	}
}

func writeProviderConfig(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "provider.yaml")
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
