package diagnostics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infraflow/pkg/observability"
)

// fixtureLog writes events in the exact JSONL shape produced by the InfraFlow
// observability logger and reads them back through ReadFiles.
func fixtureLog(t *testing.T, events ...observability.Event) []Event {
	t.Helper()
	base := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	var builder strings.Builder
	for index, event := range events {
		if event.ID == "" {
			event.ID = "evt-" + strings.Repeat("0", 3) + string(rune('a'+index))
		}
		if event.Timestamp == "" {
			event.Timestamp = base.Add(time.Duration(index) * time.Second).Format(time.RFC3339Nano)
		}
		if event.Level == "" {
			event.Level = "INFO"
		}
		if event.Message == "" {
			event.Message = event.Event
		}
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		builder.Write(data)
		builder.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), "central-events.jsonl")
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := ReadFiles([]SourceFile{{Name: "central", Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func agentEvent(name, status, detail, errorText string) observability.Event {
	return observability.Event{Event: name, Service: "agent", Source: "agent-runner", AgentID: "agent-a", SiteID: "site-a", RunID: "run-1", Status: status, Line: detail, Error: errorText}
}

func successfulRun() []observability.Event {
	return []observability.Event{
		{Event: "agent.registration.completed", Message: "agent registered and site authorized", Service: "provider", Source: "agent-registry", AgentID: "agent-a", SiteID: "site-a"},
		agentEvent("agent.run.started", "RUNNING", "", ""),
		agentEvent("agent.artifact.started", "RUNNING", "site-a/native/device-edge1.cfg", ""),
		agentEvent("agent.artifact.completed", "COMPLETED", "site-a/native/device-edge1.cfg", ""),
		agentEvent("agent.run.completed", "COMPLETED", "", ""),
		{Event: "provider.grpc.operation", Service: "provider", Source: "grpcapi", Operation: "/infraflow.v1.AgentProvider/ReportState", Status: "OK", RunID: "run-1"},
		{Event: "dhcp.discover", Service: "agent", Source: "dhcp-server", AgentID: "agent-a", SiteID: "site-a", DeviceID: "edge1", Line: "client 0c:00:00:00:00:01 reservation edge1"},
		{Event: "dhcp.offer", Service: "agent", Source: "dhcp-server", AgentID: "agent-a", SiteID: "site-a", DeviceID: "edge1", Line: "client 0c:00:00:00:00:01 ip 192.0.2.10"},
		{Event: "dhcp.ack", Service: "agent", Source: "dhcp-server", AgentID: "agent-a", SiteID: "site-a", DeviceID: "edge1", Line: "client 0c:00:00:00:00:01 ip 192.0.2.10 bootfile network-confg"},
		{Event: "tftp.rrq", Service: "agent", Source: "tftp-server", AgentID: "agent-a", SiteID: "site-a", DeviceID: "edge1", Line: "file network-confg from 192.0.2.10"},
		{Event: "tftp.transfer.completed", Service: "agent", Source: "tftp-server", AgentID: "agent-a", SiteID: "site-a", DeviceID: "edge1", Line: "file network-confg from 192.0.2.10"},
		{Event: "device.config.applied", Service: "agent", Source: "device-console", AgentID: "agent-a", SiteID: "site-a", DeviceID: "edge1", Message: "AutoInstall applied configuration"},
		{Event: "device.postcheck.passed", Service: "agent", Source: "device-console", AgentID: "agent-a", SiteID: "site-a", DeviceID: "edge1", Message: "hostname edge1 and 192.0.2.10 observed"},
	}
}

func hasEvidence(report Report, code string) bool {
	for _, evidence := range report.Evidence {
		if evidence.Code == code {
			return true
		}
	}
	return false
}

func TestScenarioSuccessfulExecutionFromStructuredLogs(t *testing.T) {
	report := AnalyzeWithAgent(fixtureLog(t, successfulRun()...), Filter{AgentID: "agent-a"}, &AgentContext{AgentID: "agent-a", SiteID: "site-a", Status: "online", Reason: "Fresh authenticated heartbeat"})
	if report.ObservedStatus != "success" || report.FailureStage != "" || report.Confidence != "high" {
		t.Fatalf("complete structured lifecycle was not reported as success: %+v", report)
	}
	for _, code := range []string{"AGENT_REGISTERED", "ARTIFACT_HASH_VERIFIED", "CATALOG_SYNC_COMPLETED", "EXECUTION_REPORT_ACCEPTED", "DHCP_LEASE_ASSIGNED", "TFTP_FILE_TRANSFERRED", "DEVICE_CONFIG_APPLIED", "DEVICE_CONFIG_VERIFIED", "AGENT_HEARTBEAT_FRESH"} {
		if !hasEvidence(report, code) {
			t.Fatalf("success evidence %s missing: %+v", code, report.Evidence)
		}
	}
	if report.LastConfirmedSuccessfulStage != StageDevicePostChk || len(report.MissingEvidence) != 0 {
		t.Fatalf("success not confirmed end to end: last=%q missing=%v", report.LastConfirmedSuccessfulStage, report.MissingEvidence)
	}
	for _, evidence := range report.Evidence {
		if evidence.Code == "AGENT_REGISTERED" && evidence.EventID == "" {
			t.Fatal("evidence does not link back to the log event id")
		}
	}
}

func TestScenarioMissingHeartbeat(t *testing.T) {
	events := fixtureLog(t, successfulRun()[:5]...)
	report := AnalyzeWithAgent(events, Filter{AgentID: "agent-a"}, &AgentContext{AgentID: "agent-a", SiteID: "site-a", Status: "offline", Reason: "No heartbeat for 4m0s (timeout 1m30s)"})
	if report.FailureStage != StageHeartbeat || report.FailureCode != "AGENT_HEARTBEAT_MISSING" || report.Confidence != "medium" {
		t.Fatalf("missing heartbeat not diagnosed: %+v", report)
	}
	if !strings.Contains(report.RecommendedNextAction, "does not by itself prove the Agent process is down") {
		t.Fatalf("recommendation conflates Provider reachability with Agent process health: %q", report.RecommendedNextAction)
	}
	if report.LastConfirmedSuccessfulStage != StageIntegrity {
		t.Fatalf("last confirmed stage = %q", report.LastConfirmedSuccessfulStage)
	}
}

func TestScenarioGracefulShutdownIsNotAFailure(t *testing.T) {
	report := AnalyzeWithAgent(fixtureLog(t, successfulRun()[:5]...), Filter{AgentID: "agent-a"}, &AgentContext{AgentID: "agent-a", Status: "offline", Reason: "Agent announced process shutdown at 2026-11-19T09:00:10Z"})
	if report.ObservedStatus == "failed" || !strings.Contains(report.CurrentState, "stopped gracefully") {
		t.Fatalf("graceful shutdown misreported: %+v", report)
	}
}

func TestScenarioAuthenticationFailure(t *testing.T) {
	report := Analyze(fixtureLog(t,
		agentEvent("agent.run.started", "RUNNING", "", ""),
		observability.Event{Event: "provider.grpc.operation", Service: "provider", Source: "grpcapi", Operation: "/infraflow.v1.AgentProvider/ListArtifacts", Status: "Unauthenticated", RunID: "run-1", Level: "WARN"},
		agentEvent("agent.catalog.failed", "FAILED", "", "rpc error: code = Unauthenticated desc = invalid bearer token"),
		agentEvent("agent.run.failed", "FAILED", "", "rpc error: code = Unauthenticated desc = invalid bearer token"),
	), Filter{RunID: "run-1"})
	if report.FailureStage != StageAuth || report.FailureCode != "AUTHENTICATION_FAILED" || report.Confidence != "high" {
		t.Fatalf("authentication failure not diagnosed: %+v", report)
	}
}

func TestScenarioSiteAuthorizationFailure(t *testing.T) {
	report := Analyze(fixtureLog(t,
		agentEvent("agent.catalog.failed", "FAILED", "", "rpc error: code = PermissionDenied desc = agent is not registered for its authorized site"),
	), Filter{RunID: "run-1"})
	if report.FailureCode != "SITE_AUTHORIZATION_FAILED" {
		t.Fatalf("site authorization failure not diagnosed: %+v", report)
	}
}

func TestScenarioProviderUnreachable(t *testing.T) {
	report := Analyze(fixtureLog(t,
		agentEvent("agent.registration.failed", "FAILED", "", "provider API request: Post \"http://127.0.0.1:8080/api/v1/agents/register\": dial tcp 127.0.0.1:8080: connect: connection refused"),
	), Filter{AgentID: "agent-a"})
	if report.FailureStage != StageConnectivity || report.FailureCode != "PROVIDER_UNREACHABLE" {
		t.Fatalf("unreachable Provider not diagnosed: %+v", report)
	}
}

func TestScenarioArtifactHashMismatch(t *testing.T) {
	report := Analyze(fixtureLog(t,
		agentEvent("agent.artifact.started", "RUNNING", "site-a/native/device-edge1.cfg", ""),
		agentEvent("agent.artifact.completed", "FAILED", "site-a/native/device-edge1.cfg", "artifact SHA-256 integrity check failed: hash mismatch against the published catalog"),
		agentEvent("agent.artifact.completed", "COMPLETED", "site-a/inventory.json", ""),
		agentEvent("agent.run.failed", "FAILED", "", "one or more artifacts were not completed"),
	), Filter{RunID: "run-1"})
	if report.FailureStage != StageIntegrity || report.FailureCode != "ARTIFACT_HASH_MISMATCH" || report.Confidence != "high" {
		t.Fatalf("hash mismatch not diagnosed (a later artifact success or the generic run failure must not mask it): %+v", report)
	}
	if !strings.Contains(report.LikelyCause, "site-a/native/device-edge1.cfg") {
		t.Fatalf("cause does not name the failing artifact: %q", report.LikelyCause)
	}
}

func TestScenarioRetrySupersedesEarlierFailureOfSameArtifact(t *testing.T) {
	report := Analyze(fixtureLog(t,
		agentEvent("agent.artifact.completed", "FAILED", "site-a/inventory.json", "artifact download failed: rpc error: code = Unavailable"),
		agentEvent("agent.artifact.completed", "COMPLETED", "site-a/inventory.json", ""),
		agentEvent("agent.run.completed", "COMPLETED", "", ""),
	), Filter{RunID: "run-1"})
	if report.ObservedStatus == "failed" {
		t.Fatalf("retried and verified artifact still reported as failed: %+v", report)
	}
}

func TestScenarioDHCPFailure(t *testing.T) {
	explicit := Analyze(fixtureLog(t,
		observability.Event{Event: "dhcp.discover", Source: "dhcp-server", DeviceID: "edge1"},
		observability.Event{Event: "dhcp.no_offer", Source: "dhcp-server", DeviceID: "edge1", Level: "ERROR", Error: "dhcp pool exhausted or client not eligible"},
	), Filter{DeviceID: "edge1"})
	if explicit.FailureCode != "DHCP_NO_OFFER" || explicit.Confidence != "high" {
		t.Fatalf("explicit DHCP failure not diagnosed: %+v", explicit)
	}
	inferred := Analyze(fixtureLog(t,
		observability.Event{Event: "dhcp.discover", Source: "dhcp-server", DeviceID: "edge1"},
		observability.Event{Event: "dhcp.discover", Source: "dhcp-server", DeviceID: "edge1"},
	), Filter{DeviceID: "edge1"})
	if inferred.FailureCode != "DHCP_NO_LEASE" || inferred.Confidence != "medium" {
		t.Fatalf("Discover without lease not inferred with medium confidence: %+v", inferred)
	}
}

func TestScenarioMissingBootstrapFile(t *testing.T) {
	report := Analyze(fixtureLog(t,
		observability.Event{Event: "dhcp.ack", Source: "dhcp-server", DeviceID: "edge1", Line: "client 0c:00:00:00:00:01 ip 192.0.2.10 bootfile network-confg"},
		observability.Event{Event: "tftp.rrq", Source: "tftp-server", DeviceID: "edge1", Line: "file router-confg from 192.0.2.10"},
		observability.Event{Event: "tftp.file_missing", Source: "tftp-server", DeviceID: "edge1", Line: "file router-confg from 192.0.2.10", Error: "file not found", Level: "ERROR"},
	), Filter{DeviceID: "edge1"})
	if report.FailureStage != StageTFTP || report.FailureCode != "TFTP_FILE_MISSING" || report.LastConfirmedSuccessfulStage != StageDHCP {
		t.Fatalf("missing bootstrap file not diagnosed: %+v", report)
	}
	if !strings.Contains(report.LikelyCause, "router-confg") {
		t.Fatalf("cause does not name the requested file: %q", report.LikelyCause)
	}
}

func TestScenarioTFTPTimeout(t *testing.T) {
	report := Analyze(fixtureLog(t,
		observability.Event{Event: "tftp.rrq", Source: "tftp-server", DeviceID: "edge1", Line: "file network-confg"},
		observability.Event{Event: "tftp.transfer.failed", Source: "tftp-server", DeviceID: "edge1", Line: "file network-confg", Error: "timeout waiting for ACK", Level: "ERROR"},
	), Filter{DeviceID: "edge1"})
	if report.FailureCode != "TFTP_TRANSFER_FAILED" {
		t.Fatalf("TFTP timeout not diagnosed: %+v", report)
	}
}

func TestScenarioUnsupportedDevice(t *testing.T) {
	report := Analyze(fixtureLog(t,
		observability.Event{Event: "provider.generation.rejected", Service: "provider", Source: "generation", Message: "unsupported model profile cisco/ios/asr9k for method autoinstall", Level: "ERROR"},
	), Filter{})
	if report.FailureStage != StageProfile || report.FailureCode != "UNSUPPORTED_DEVICE_PROFILE" {
		t.Fatalf("unsupported device not diagnosed: %+v", report)
	}
}

func TestScenarioFailedDeviceConfiguration(t *testing.T) {
	report := Analyze(fixtureLog(t,
		observability.Event{Event: "tftp.transfer.completed", Source: "tftp-server", DeviceID: "edge1", Line: "file network-confg"},
		observability.Event{Event: "device.config.rejected", Source: "device-console", DeviceID: "edge1", Message: "% Invalid input detected at '^' marker.", Level: "ERROR"},
	), Filter{DeviceID: "edge1"})
	if report.FailureStage != StageDeviceConfig || report.FailureCode != "DEVICE_CONFIG_REJECTED" || report.LastConfirmedSuccessfulStage != StageTFTP {
		t.Fatalf("device configuration failure not diagnosed: %+v", report)
	}
}

func TestScenarioFailedPostCheck(t *testing.T) {
	report := Analyze(fixtureLog(t,
		observability.Event{Event: "device.config.applied", Source: "device-console", DeviceID: "edge1"},
		observability.Event{Event: "device.postcheck.failed", Source: "device-console", DeviceID: "edge1", Error: "hostname Router does not match expected edge1", Level: "ERROR"},
	), Filter{DeviceID: "edge1"})
	if report.FailureCode != "DEVICE_POSTCHECK_FAILED" {
		t.Fatalf("post-check failure not diagnosed: %+v", report)
	}
}

func TestScenarioInsufficientEvidenceIsNotPresentedAsFact(t *testing.T) {
	report := Analyze(fixtureLog(t, agentEvent("agent.run.started", "RUNNING", "", "")), Filter{RunID: "run-1"})
	if report.ObservedStatus != "incomplete evidence" || report.Confidence != "low" || len(report.MissingEvidence) == 0 {
		t.Fatalf("insufficient evidence was presented as a conclusion: %+v", report)
	}
}

func TestScenarioGenericRunFailureAloneIsMediumConfidence(t *testing.T) {
	report := Analyze(fixtureLog(t, agentEvent("agent.run.failed", "FAILED", "", "one or more artifacts were not completed")), Filter{RunID: "run-1"})
	if report.FailureCode != "AGENT_RUN_FAILED" || report.Confidence != "medium" {
		t.Fatalf("generic failure reported with unjustified certainty: %+v", report)
	}
}

func TestScenarioEvidenceIsRedacted(t *testing.T) {
	report := Analyze(fixtureLog(t,
		agentEvent("agent.catalog.failed", "FAILED", "", "invalid bearer token Bearer abcdefghijklmnopqrstuvwxyz0123456789 password=hunter2-secret"),
	), Filter{RunID: "run-1"})
	encoded, _ := json.Marshal(report)
	for _, secret := range []string{"abcdefghijklmnopqrstuvwxyz0123456789", "hunter2-secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("diagnostic output leaked a secret %q: %s", secret, encoded)
		}
	}
}

func TestScenarioTimelineMarksFailureAndConfirmedStages(t *testing.T) {
	report := Analyze(fixtureLog(t,
		observability.Event{Event: "dhcp.ack", Source: "dhcp-server", DeviceID: "edge1"},
		observability.Event{Event: "tftp.transfer.failed", Source: "tftp-server", DeviceID: "edge1", Error: "timeout"},
	), Filter{DeviceID: "edge1"})
	states := map[string]string{}
	for _, entry := range report.Timeline {
		states[entry.Stage] = entry.State
	}
	if states[StageDHCP] != "confirmed" || states[StageTFTP] != "failed" || states[StageDeviceConfig] != "not observed" {
		t.Fatalf("timeline states = %v", states)
	}
}
