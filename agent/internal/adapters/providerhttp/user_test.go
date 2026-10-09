package providerhttp

import (
	"context"
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
