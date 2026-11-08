package providerhttp

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"infraflow/internal/infrastructure/security"
)

func TestClientRegistersAndSendsHeartbeatWithBearerToken(t *testing.T) {
	token := strings.Repeat("t", security.MinAgentTokenBytes)
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+token {
			t.Error("missing or invalid bearer token")
		}
		paths = append(paths, request.URL.Path)
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	client, err := New(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Register(context.Background(), Registration{AgentID: "agent-01", SiteID: "site-01"}); err != nil {
		t.Fatal(err)
	}
	if err := client.Heartbeat(context.Background(), Heartbeat{AgentID: "agent-01", SiteID: "site-01", QueueDepth: 2}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(paths, ","); got != "/api/v1/agents/register,/api/v1/agents/agent-01/heartbeat" {
		t.Fatalf("unexpected request paths: %s", got)
	}
}

func TestClientRejectsUnsafeAddressesAndInvalidResponses(t *testing.T) {
	token := strings.Repeat("t", security.MinAgentTokenBytes)
	for _, address := range []string{"http://203.0.113.10", "http://127.0.0.1:8080/path", "http://user@127.0.0.1:8080"} {
		if _, err := New(address, token); err == nil {
			t.Fatalf("expected unsafe address %q to be rejected", address)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusConflict)
		_, _ = writer.Write([]byte(`{"error":"conflict"}`))
	}))
	t.Cleanup(server.Close)
	client, err := New(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Register(context.Background(), Registration{AgentID: "agent-01", SiteID: "site-01"}); err == nil {
		t.Fatal("expected non-2xx response")
	}
}

func TestClientUsesConfiguredCAForHTTPS(t *testing.T) {
	token := strings.Repeat("t", security.MinAgentTokenBytes)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+token {
			t.Error("missing or invalid bearer token")
		}
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	caPath := filepath.Join(t.TempDir(), "provider-ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := New(server.URL, token, caPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Register(context.Background(), Registration{AgentID: "agent-01", SiteID: "site-01"}); err != nil {
		t.Fatalf("trusted HTTPS registration failed: %v", err)
	}

	untrustedClient, err := New(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	if err := untrustedClient.Register(context.Background(), Registration{AgentID: "agent-01", SiteID: "site-01"}); err == nil {
		t.Fatal("expected HTTPS certificate from an untrusted CA to be rejected")
	}
}

func TestClientRejectsInvalidConfiguredCA(t *testing.T) {
	caPath := filepath.Join(t.TempDir(), "invalid-ca.pem")
	if err := os.WriteFile(caPath, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New("https://provider.example:8080", strings.Repeat("t", security.MinAgentTokenBytes), caPath)
	if err == nil || !strings.Contains(err.Error(), "contains no valid certificates") {
		t.Fatalf("expected invalid CA to be rejected, got %v", err)
	}
}
