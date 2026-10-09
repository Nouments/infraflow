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

type logsBackend struct {
	fakeBackend
	role    string
	logs    []providerhttp.TechnicalLogEntry
	logsErr error
	calls   int
}

func (backend *logsBackend) CurrentUser(context.Context) (providerhttp.User, error) {
	return providerhttp.User{Username: "operator", Role: backend.role}, nil
}

func (backend *logsBackend) Logs(context.Context) ([]providerhttp.TechnicalLogEntry, error) {
	backend.calls++
	return backend.logs, backend.logsErr
}

func (fakeBackend) Logs(context.Context) ([]providerhttp.TechnicalLogEntry, error) {
	return []providerhttp.TechnicalLogEntry{{ID: "log-1", Timestamp: time.Unix(0, 0).UTC(), Level: "ERROR", Service: "agent", Message: "process exit code 7"}}, nil
}

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

func TestRunDisplaysTechnicalLogs(t *testing.T) {
	var output strings.Builder
	if err := Run(context.Background(), strings.NewReader("l\nq\n"), &output, fakeBackend{}, "admin", "secret"); err != nil {
		t.Fatal(err)
	}
	result := output.String()
	if !strings.Contains(result, "TECHNICAL LOGS") {
		t.Fatalf("technical logs view missing: %s", result)
	}
	if !strings.Contains(result, "process exit code 7") {
		t.Fatalf("technical log payload missing: %s", result)
	}
}

func TestRunTechnicalLogsRedactsSecretsAndKeepsEmptyDistinctFromError(t *testing.T) {
	backend := &logsBackend{role: "admin", logs: []providerhttp.TechnicalLogEntry{{
		Timestamp: time.Unix(0, 0).UTC(), Level: "ERROR", Service: "agent", Message: "token=secret-value",
	}}}
	var output strings.Builder
	if err := Run(context.Background(), strings.NewReader("l\nq\n"), &output, backend, "operator", "secret"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "secret-value") || !strings.Contains(output.String(), "[REDACTED]") {
		t.Fatalf("technical log secret was not masked: %s", output.String())
	}

	emptyBackend := &logsBackend{role: "admin"}
	output.Reset()
	if err := Run(context.Background(), strings.NewReader("l\nq\n"), &output, emptyBackend, "operator", "secret"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "no technical logs") {
		t.Fatalf("empty log list was not reported explicitly: %s", output.String())
	}

	errorBackend := &logsBackend{role: "admin", logsErr: context.DeadlineExceeded}
	output.Reset()
	if err := Run(context.Background(), strings.NewReader("l\n"), &output, errorBackend, "operator", "secret"); err == nil || !strings.Contains(err.Error(), "load technical logs") || strings.Contains(output.String(), "no technical logs") {
		t.Fatalf("logs error was mistaken for an empty result: output=%q err=%v", output.String(), err)
	}
}

func TestRunDeniesTechnicalLogsToNonAdmin(t *testing.T) {
	backend := &logsBackend{role: "user"}
	var output strings.Builder
	if err := Run(context.Background(), strings.NewReader("l\nq\n"), &output, backend, "operator", "secret"); err != nil {
		t.Fatal(err)
	}
	if backend.calls != 0 || !strings.Contains(output.String(), "Permission denied") {
		t.Fatalf("non-admin logs access was not denied locally: calls=%d output=%s", backend.calls, output.String())
	}
}
