package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"infraflow/agent/internal/adapters/providerhttp"
)

type fakeBackend struct{}

func (fakeBackend) Login(context.Context, string, string) (providerhttp.UserSession, error) {
	return providerhttp.UserSession{Token: "session-token", ExpiresAt: time.Now().Add(time.Hour), User: providerhttp.User{Username: "admin", Role: "admin"}}, nil
}
func (fakeBackend) Logout(context.Context) error { return nil }
func (fakeBackend) CurrentUser(context.Context) (providerhttp.User, error) {
	return providerhttp.User{Username: "admin", Role: "admin"}, nil
}
func (fakeBackend) Jobs(context.Context) ([]providerhttp.JobSummary, error) {
	return []providerhttp.JobSummary{{ID: "job-1", Status: "planned", UpdatedAt: time.Unix(0, 0).UTC()}}, nil
}
func (fakeBackend) Agents(context.Context) ([]providerhttp.AgentSummary, error) {
	return []providerhttp.AgentSummary{{ID: "agent-1", SiteID: "site-1", Status: "online", QueueDepth: 1}}, nil
}
func (fakeBackend) Events(context.Context) ([]providerhttp.EventSummary, error) {
	return []providerhttp.EventSummary{{
		EventID: "event-1", Timestamp: time.Unix(0, 0).UTC(), Type: "plan.generation.result",
		Payload: json.RawMessage(`{"generation_status":"GENERATED","execution_status":"NOT_REQUESTED","verification_status":"NOT_PERFORMED"}`),
	}}, nil
}

func TestRunDisplaysBackendStateWithoutExposingToken(t *testing.T) {
	var output strings.Builder
	if err := Run(context.Background(), strings.NewReader("q\n"), &output, fakeBackend{}, "admin", "secret"); err != nil {
		t.Fatal(err)
	}
	result := output.String()
	if !strings.Contains(result, "INFRAFLOW TUI") {
		t.Fatalf("TUI header missing: %s", result)
	}
	if !strings.Contains(result, "job-1") || !strings.Contains(result, "agent-1") || !strings.Contains(result, "generation=GENERATED") || !strings.Contains(result, "verification=NOT_PERFORMED") {
		t.Fatalf("backend state missing: %s", result)
	}
	if strings.Contains(result, "session-token") {
		t.Fatalf("full session token leaked to terminal: %s", result)
	}
}
