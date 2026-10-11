package providerhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewUserClientRequiresTLSForRemoteAddresses(t *testing.T) {
	if _, err := NewUserClient("http://10.0.0.5:8080", ""); err == nil {
		t.Fatal("remote plaintext TUI address was accepted")
	}
	if _, err := NewUserClient("https://10.0.0.5:8080", ""); err != nil {
		t.Fatalf("remote HTTPS TUI address was rejected: %v", err)
	}
	if _, err := NewUserClient("http://127.0.0.1:8080", ""); err != nil {
		t.Fatalf("loopback HTTP TUI address was rejected: %v", err)
	}
}

func TestUserClientLogsUsesExistingResponseContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/logs" || request.URL.Query().Get("limit") != "25" {
			t.Errorf("unexpected logs request: %s", request.URL.String())
		}
		if request.Header.Get("Authorization") != "Bearer admin-session" {
			t.Errorf("logs request omitted session authorization")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"events":[{"event_id":"event-01","timestamp":"2026-10-09T12:00:00Z","level":"ERROR","event":"process.exit","service":"agent","message":"exit code 7","source":"process-runner"}]}`))
	}))
	defer server.Close()

	client, err := NewUserClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	client.token = "admin-session"
	logs, err := client.Logs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].ID != "event-01" || logs[0].Message != "exit code 7" {
		t.Fatalf("unexpected decoded log response: %#v", logs)
	}
}

func TestUserClientLogsReportsMalformedAndUnauthorizedResponsesWithoutSecrets(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "malformed", statusCode: http.StatusOK, body: `{"events":[`},
		{name: "malformed entry", statusCode: http.StatusOK, body: `{"events":[{}]}`},
		{name: "missing events", statusCode: http.StatusOK, body: `{}`},
		{name: "null events", statusCode: http.StatusOK, body: `{"events":null}`},
		{name: "forbidden", statusCode: http.StatusForbidden, body: `{"error":"token=secret-value denied"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.statusCode)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			client, err := NewUserClient(server.URL, "")
			if err != nil {
				t.Fatal(err)
			}
			client.token = "session"
			_, err = client.Logs(context.Background())
			if err == nil {
				t.Fatal("expected logs request error")
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Fatalf("server error leaked a secret: %v", err)
			}
		})
	}
}

func TestUserClientExecutionsUsesPageDetailAndCancelContracts(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer admin-session" {
			t.Errorf("execution request omitted user session")
		}
		paths = append(paths, request.Method+" "+request.URL.RequestURI())
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/executions":
			if request.URL.Query().Get("limit") != "5" || request.URL.Query().Get("offset") != "10" || request.URL.Query().Get("status") != "running" || request.URL.Query().Get("type") != "test.operation" {
				t.Errorf("execution list filters were not sent: %s", request.URL.String())
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"items":[{"id":"exec-1","status":"running","created_at":"2026-10-11T10:00:00Z","updated_at":"2026-10-11T10:01:00Z","tasks":[{"id":"task-1","type":"test.operation","status":"running","created_at":"2026-10-11T10:00:00Z","updated_at":"2026-10-11T10:01:00Z","attempts":null,"parameters":{},"result":{"message":"token=hidden-value","access_token":"hidden-value"}}],"events":[]}],"total":11,"limit":5,"offset":10}`))
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/executions/exec-1":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"id":"exec-1","status":"succeeded","created_at":"2026-10-11T10:00:00Z","updated_at":"2026-10-11T10:02:00Z","tasks":[],"events":[]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/executions/exec-1/cancel":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"id":"exec-1","status":"running","cancel_requested":true,"created_at":"2026-10-11T10:00:00Z","updated_at":"2026-10-11T10:02:00Z","tasks":[],"events":[]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := NewUserClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	client.token = "admin-session"
	page, err := client.Executions(context.Background(), 5, 10, "running", "test.operation")
	if err != nil || page.Total != 11 || len(page.Items) != 1 || page.Items[0].Tasks[0].Attempts == nil {
		t.Fatalf("execution page did not decode: %#v, %v", page, err)
	}
	encoded, _ := json.Marshal(page)
	if strings.Contains(string(encoded), "hidden-value") || !strings.Contains(string(encoded), "[REDACTED]") {
		t.Fatalf("execution output was not redacted: %s", encoded)
	}
	detail, err := client.Execution(context.Background(), "exec-1")
	if err != nil || detail.Status != "succeeded" {
		t.Fatalf("execution detail did not decode: %#v, %v", detail, err)
	}
	cancelled, err := client.CancelExecution(context.Background(), "exec-1")
	if err != nil || !cancelled.CancelRequested || cancelled.Status != "running" {
		t.Fatalf("cancel response did not preserve server state: %#v, %v", cancelled, err)
	}
	if len(paths) != 3 {
		t.Fatalf("unexpected execution HTTP requests: %#v", paths)
	}
}

func TestUserClientExecutionsReportsForbiddenAndMalformedResponses(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "forbidden", statusCode: http.StatusForbidden, body: `{"error":"administrator role required"}`},
		{name: "malformed page", statusCode: http.StatusOK, body: `{"items":[`},
		{name: "invalid page contract", statusCode: http.StatusOK, body: `{"items":null,"total":0,"limit":50,"offset":0}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.statusCode)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			client, err := NewUserClient(server.URL, "")
			if err != nil {
				t.Fatal(err)
			}
			client.token = "session"
			_, err = client.Executions(context.Background(), 50, 0, "", "")
			if err == nil {
				t.Fatal("expected execution list error")
			}
		})
	}
	if _, err := NewUserClient("http://127.0.0.1:8080", ""); err != nil {
		t.Fatalf("execution client changes must not alter existing loopback session transport: %v", err)
	}
}
