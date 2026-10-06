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
