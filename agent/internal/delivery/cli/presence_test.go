package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"infraflow/agent/internal/adapters/providerhttp"
	"infraflow/agent/internal/config"
	"infraflow/pkg/observability"
	"infraflow/pkg/protocol"
)

type recordedCall struct {
	path string
	body map[string]any
}

// fakeProviderAPI records Agent registry calls and can be switched to fail.
type fakeProviderAPI struct {
	mu     sync.Mutex
	calls  []recordedCall
	status int
	seen   chan string
}

func newFakeProviderAPI() *fakeProviderAPI {
	return &fakeProviderAPI{status: http.StatusOK, seen: make(chan string, 64)}
}

func (api *fakeProviderAPI) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	data, _ := io.ReadAll(request.Body)
	var body map[string]any
	_ = json.Unmarshal(data, &body)
	api.mu.Lock()
	api.calls = append(api.calls, recordedCall{path: request.URL.Path, body: body})
	status := api.status
	api.mu.Unlock()
	writer.WriteHeader(status)
	_, _ = writer.Write([]byte(`{}`))
	api.seen <- request.URL.Path
}

func (api *fakeProviderAPI) setStatus(status int) {
	api.mu.Lock()
	api.status = status
	api.mu.Unlock()
}

func (api *fakeProviderAPI) last() recordedCall {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.calls[len(api.calls)-1]
}

func newPresenceFixture(t *testing.T) (*presence, *fakeProviderAPI, *bytes.Buffer, string) {
	t.Helper()
	api := newFakeProviderAPI()
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	client, err := providerhttp.New(server.URL, strings.Repeat("p", 32))
	if err != nil {
		t.Fatal(err)
	}
	logDir := t.TempDir()
	logger, err := observability.New(observability.Config{Service: "agent", Directory: logDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	stderr := &bytes.Buffer{}
	return newPresence(client, providerhttp.Heartbeat{AgentID: "agent-a", SiteID: "site-a", Version: "test"}, logger, stderr), api, stderr, logDir
}

func TestPresenceReportsProblemThenRecovers(t *testing.T) {
	presence, api, _, _ := newPresenceFixture(t)
	presence.setProblem("artifact run failed (1 failed, 0 blocked): boom")
	presence.beat(t.Context())
	call := api.last()
	if call.path != "/api/v1/agents/agent-a/heartbeat" || call.body["problem"] != "artifact run failed (1 failed, 0 blocked): boom" || call.body["site_id"] != "site-a" {
		t.Fatalf("degraded heartbeat = %+v", call)
	}
	presence.setProblem("")
	presence.beat(t.Context())
	if _, ok := api.last().body["problem"]; ok {
		t.Fatalf("recovered heartbeat still carries a problem: %+v", api.last())
	}
	if presence.failed() {
		t.Fatal("accepted heartbeat marked as failed")
	}
}

func TestPresenceRecordsHeartbeatFailureAndRestoration(t *testing.T) {
	presence, api, stderr, logDir := newPresenceFixture(t)
	api.setStatus(http.StatusUnauthorized)
	presence.beat(t.Context())
	if !presence.failed() || !strings.Contains(stderr.String(), "heartbeat") {
		t.Fatalf("rejected heartbeat not surfaced: failed=%v stderr=%q", presence.failed(), stderr.String())
	}
	api.setStatus(http.StatusOK)
	presence.beat(t.Context())
	if presence.failed() {
		t.Fatal("restored heartbeat still marked as failed")
	}
	data, err := os.ReadFile(filepath.Join(logDir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "agent.heartbeat.failed") || !strings.Contains(string(data), "agent.heartbeat.restored") {
		t.Fatalf("heartbeat state changes were not logged for diagnostics: %s", data)
	}
}

func TestPresenceLoopHeartbeatsUntilCancelledAndAnnouncesDisconnect(t *testing.T) {
	presence, api, _, _ := newPresenceFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { presence.loop(ctx, 5*time.Millisecond); close(done) }()
	for beats := 0; beats < 3; {
		select {
		case path := <-api.seen:
			if strings.HasSuffix(path, "/heartbeat") {
				beats++
			}
		case <-time.After(5 * time.Second):
			t.Fatal("periodic heartbeats were not sent")
		}
	}
	cancel()
	<-done
	presence.disconnect()
	if call := api.last(); call.path != "/api/v1/agents/agent-a/disconnect" || call.body["site_id"] != "site-a" {
		t.Fatalf("disconnect notice = %+v", call)
	}
}

func TestPresenceWithoutRegistryIsInert(t *testing.T) {
	presence := newPresence(nil, providerhttp.Heartbeat{}, nil, io.Discard)
	presence.beat(t.Context())
	presence.loop(t.Context(), time.Millisecond)
	presence.disconnect()
	if presence.failed() {
		t.Fatal("inert presence reported failure")
	}
}

func TestRunProblemSummarizesCountsInOneBoundedLine(t *testing.T) {
	report := protocol.AgentReport{Artifacts: []protocol.ArtifactResult{{Status: protocol.StatusFailed}, {Status: protocol.StatusBlocked}, {Status: protocol.StatusFailed}, {Status: protocol.StatusCompleted}}}
	problem := runProblem(report, io.ErrUnexpectedEOF)
	if !strings.HasPrefix(problem, "artifact run failed (2 failed, 1 blocked)") || strings.ContainsAny(problem, "\r\n") {
		t.Fatalf("problem = %q", problem)
	}
	long := runProblem(protocol.AgentReport{}, errorString(strings.Repeat("x", 1000)+"\nsecond line"))
	if len(long) > 240 || strings.Contains(long, "\n") {
		t.Fatalf("problem is not bounded to one line: %d %q", len(long), long[:40])
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }

func TestBootstrapEventSinkWithoutAgentConfigWritesOperatorLines(t *testing.T) {
	stdout := &bytes.Buffer{}
	sink, closeSink, err := bootstrapEventSink(t.Context(), "", stdout, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	sink(observability.Event{Level: "INFO", Event: "dhcp.ack", Line: "client 0c:00:00:00:00:01 ip 192.0.2.10", Error: "password=secret-value"})
	closeSink()
	if !strings.Contains(stdout.String(), "dhcp.ack client 0c:00:00:00:00:01 ip 192.0.2.10") || strings.Contains(stdout.String(), "secret-value") {
		t.Fatalf("operator line = %q", stdout.String())
	}
	if _, _, err := bootstrapEventSink(t.Context(), filepath.Join(t.TempDir(), "missing.yaml"), io.Discard, io.Discard); err == nil {
		t.Fatal("missing agent configuration was accepted")
	}
}

func TestEmitServiceEventCarriesAgentCorrelation(t *testing.T) {
	reporter := &recordingLogReporter{}
	settings := config.Config{}
	settings.Agent.ID = "agent-a"
	settings.Agent.SiteID = "site-a"
	settings.Logging.Directory = t.TempDir()
	session, err := newProcessLogSessionWithReporter(settings, reporter)
	if err != nil {
		t.Fatal(err)
	}
	session.EmitServiceEvent(observability.Event{Level: "INFO", Event: "tftp.transfer.completed", Message: "TFTP transfer complete", Service: "agent", Source: "tftp-server", Line: "file network-confg"})
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	var nilSession *processLogSession
	nilSession.EmitServiceEvent(observability.Event{})
	found := false
	for _, event := range reporter.events {
		if event.Event == "tftp.transfer.completed" && event.AgentID == "agent-a" && event.SiteID == "site-a" && event.RunID != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("service event was not synchronized with correlation fields: %+v", reporter.events)
	}
}

func TestDiagnoseCommandExplainsFailureFromLogFiles(t *testing.T) {
	directory := t.TempDir()
	agentLog := filepath.Join(directory, "agent.jsonl")
	bootstrapLog := filepath.Join(directory, "bootstrap.log")
	if err := os.WriteFile(agentLog, []byte(`{"event_id":"e1","timestamp":"2026-11-19T09:00:00Z","level":"INFO","event":"agent.run.completed","message":"agent artifact run completed","service":"agent","source":"agent-runner","run_id":"run-1","device_id":"edge1","status":"COMPLETED"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bootstrapLog, []byte(`{"event_id":"e2","timestamp":"2026-11-19T09:00:05Z","level":"ERROR","event":"tftp.file_missing","message":"TFTP file not found","service":"agent","source":"tftp-server","run_id":"run-1","device_id":"edge1","line":"file router-confg","error":"file not found"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if code := Run([]string{"diagnose", "-agent-log", agentLog, "-bootstrap-log", bootstrapLog, "-run-id", "run-1", "-device-id", "edge1"}, stdout, stderr); code != 0 {
		t.Fatalf("diagnose exit %d: %s", code, stderr.String())
	}
	for _, want := range []string{"Summary: The workflow stopped at TFTP (TFTP_FILE_MISSING)", "Failure stage: TFTP TFTP_FILE_MISSING", "Evidence [tftp-server/TFTP]"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("diagnose output missing %q:\n%s", want, stdout.String())
		}
	}
	stdout.Reset()
	if code := Run([]string{"diagnose", "-agent-log", agentLog, "-run-id", "run-1", "-json"}, stdout, stderr); code != 0 {
		t.Fatalf("diagnose -json exit %d", code)
	}
	var report map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || report["observed_status"] != "incomplete evidence" {
		t.Fatalf("json report = %v %s", err, stdout.String())
	}
	if code := Run([]string{"diagnose", "-agent-log", agentLog}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("diagnose without selector exit %d", code)
	}
	if code := Run([]string{"diagnose", "-run-id", "run-1"}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("diagnose without sources exit %d", code)
	}
}

func TestVerifyDeviceCommandReadsCaptureOnly(t *testing.T) {
	directory := t.TempDir()
	capture := filepath.Join(directory, "console.log")
	good := "Loading edge1-confg from 192.0.2.2 (via FastEthernet0/0): !\r\n[OK - 93 bytes]\r\nedge1#show running-config | include ^hostname\r\nhostname edge1\r\nFastEthernet0/0            192.0.2.10      YES TFTP   up                    up      \r\n"
	if err := os.WriteFile(capture, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	if code := Run([]string{"verify-device", "-console", capture, "-device-id", "edge1", "-expected-hostname", "edge1", "-expected-ipv4", "192.0.2.10"}, stdout, io.Discard); code != 0 {
		t.Fatalf("verify-device exit %d: %s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "Post-check passed: true") {
		t.Fatalf("output = %s", stdout.String())
	}
	stdout.Reset()
	if code := Run([]string{"verify-device", "-console", capture, "-device-id", "edge1", "-expected-hostname", "edge9", "-json"}, stdout, io.Discard); code != 1 || !strings.Contains(stdout.String(), `"post_check_passed": false`) {
		t.Fatalf("mismatched hostname exit %d: %s", code, stdout.String())
	}
	if code := Run([]string{"verify-device", "-console", capture}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("missing expectations exit %d", code)
	}
	if code := Run([]string{"verify-device", "-console", filepath.Join(directory, "missing"), "-device-id", "edge1", "-expected-hostname", "edge1"}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("missing capture exit %d", code)
	}
}
