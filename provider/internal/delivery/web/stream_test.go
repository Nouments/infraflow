package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	configadapter "infraflow/internal/adapters/config"
	"infraflow/internal/infrastructure/security"
	"infraflow/pkg/observability"
	"infraflow/provider/internal/adapters/filesystem"
	"infraflow/provider/internal/adapters/sqlite"
	"infraflow/provider/internal/application"
	"infraflow/provider/internal/delivery/httpapi"
)

// streamingServer starts the real HTTP server stack over TCP with one stored
// log event and returns its URL and an administrator session token.
func streamingServer(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	jobs, err := filesystem.NewJobStore(root)
	if err != nil {
		t.Fatal(err)
	}
	users, err := sqlite.New(root + "/users.sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	authenticator, err := application.NewAuthenticator(users, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := authenticator.EnsureBootstrap(t.Context(), "admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	logs, err := observability.NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := logs.Append([]observability.Event{{ID: "evt-stream-1", Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Level: "INFO", Event: "provider.test", Message: "stream evidence", Service: "provider", Source: "test"}}); err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithJobs(nil, nil, nil, jobs, application.Dependencies{Parser: configadapter.Parser{}, Logs: logs})
	apiHandler, err := httpapi.NewHandler(service, strings.Repeat("a", security.MinAgentTokenBytes), authenticator)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(apiHandler, true))
	t.Cleanup(server.Close)
	response, err := http.Post(server.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{"username":"admin","password":"correct horse battery staple"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var session struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil || session.Token == "" {
		t.Fatalf("login failed: %v", err)
	}
	return server.URL, session.Token
}

func openStream(ctx context.Context, url, token string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/v1/logs/stream", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(request)
}

// The stream must deliver frames while the handler is still running; a
// buffering bridge would only send them after the handler returns (never).
func TestLogStreamDeliversFramesIncrementally(t *testing.T) {
	url, token := streamingServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	response, err := openStream(ctx, url, token)
	if err != nil {
		t.Fatalf("stream headers were not delivered: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream response = %d %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended before the stored event frame: %v", err)
		}
		if strings.HasPrefix(line, "id: evt-stream-1") {
			return
		}
	}
}

// Disconnected browsers must release their stream slot; otherwise the endpoint
// becomes permanently unavailable after maxConcurrentLogStreams visits.
func TestLogStreamReleasesCapacityWhenClientsDisconnect(t *testing.T) {
	url, token := streamingServer(t)
	const capacity = 64
	cancels := make([]context.CancelFunc, 0, capacity)
	for index := 0; index < capacity; index++ {
		ctx, cancel := context.WithCancel(t.Context())
		cancels = append(cancels, cancel)
		response, err := openStream(ctx, url, token)
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("stream %d did not open: %v", index, err)
		}
		// Keep the body open; the response is closed by cancellation.
	}
	full, err := openStream(t.Context(), url, token)
	if err != nil {
		t.Fatal(err)
	}
	full.Body.Close()
	if full.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("capacity limit not enforced: %d", full.StatusCode)
	}
	for _, cancel := range cancels {
		cancel()
	}
	// Release is asynchronous (the server observes each closed connection);
	// wait for it with a bounded deadline instead of a fixed sleep.
	deadline := time.Now().Add(5 * time.Second)
	for {
		ctx, cancel := context.WithCancel(t.Context())
		response, err := openStream(ctx, url, token)
		if err == nil && response.StatusCode == http.StatusOK {
			cancel()
			response.Body.Close()
			return
		}
		if response != nil {
			response.Body.Close()
		}
		cancel()
		if time.Now().After(deadline) {
			t.Fatal("stream capacity was not released after clients disconnected")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
