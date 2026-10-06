package providerhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
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
