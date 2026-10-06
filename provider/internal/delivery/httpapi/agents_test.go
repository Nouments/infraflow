package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"infraflow/internal/infrastructure/security"
	"infraflow/provider/internal/adapters/filesystem"
	"infraflow/provider/internal/application"
)

func TestAgentAPIRegistersAndUpdatesHeartbeats(t *testing.T) {
	root := t.TempDir()
	agents, err := filesystem.NewAgentStore(root)
	if err != nil {
		t.Fatal(err)
	}
	events, err := filesystem.NewEventStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithJobsAgentsEvents(nil, nil, nil, nil, agents, events, application.Dependencies{})
	token := strings.Repeat("a", security.MinAgentTokenBytes)
	handler, err := NewHandler(service, token)
	if err != nil {
		t.Fatal(err)
	}
	register := `{"agent_id":"agent-01","site_id":"site-01","version":"0.1.0","capabilities":["inventory"]}`
	response := agentRequest(t, handler, http.MethodPost, "/api/v1/agents/register", register, token)
	if response.Code != http.StatusOK {
		t.Fatalf("registration returned %d: %s", response.Code, response.Body.String())
	}
	response = agentRequest(t, handler, http.MethodPost, "/api/v1/agents/agent-01/heartbeat", `{"site_id":"site-01","queue_depth":2}`, token)
	if response.Code != http.StatusOK {
		t.Fatalf("heartbeat returned %d: %s", response.Code, response.Body.String())
	}
	response = agentRequest(t, handler, http.MethodGet, "/api/v1/agents", "", token)
	if response.Code != http.StatusOK {
		t.Fatalf("list returned %d: %s", response.Code, response.Body.String())
	}
	var listed []struct {
		ID         string `json:"id"`
		QueueDepth int    `json:"queue_depth"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil || len(listed) != 1 || listed[0].ID != "agent-01" || listed[0].QueueDepth != 2 {
		t.Fatalf("unexpected agent list: %#v, %v", listed, err)
	}
	response = agentRequest(t, handler, http.MethodPost, "/api/v1/agents/agent-01/heartbeat", `{"site_id":"other-site","queue_depth":2}`, token)
	if response.Code != http.StatusConflict {
		t.Fatalf("site rebinding returned %d: %s", response.Code, response.Body.String())
	}
	response = agentRequest(t, handler, http.MethodGet, "/api/v1/events?limit=1", "", token)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "agent.registered") && !strings.Contains(response.Body.String(), "agent.heartbeat") {
		t.Fatalf("event list returned %d: %s", response.Code, response.Body.String())
	}
	response = agentRequest(t, handler, http.MethodGet, "/api/v1/events?limit=0", "", token)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid event limit returned %d: %s", response.Code, response.Body.String())
	}
}

func agentRequest(t *testing.T, handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
