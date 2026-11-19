package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeCorrelatesSuccessThroughDevicePostCheck(t *testing.T) {
	events := []Event{
		{Name: "agent.registration.completed", Message: "agent registered and site authorized", Source: "provider", RunID: "run-1", DeviceID: "edge1"},
		{Name: "agent.catalog.completed", Message: "catalog sync complete", Source: "agent", RunID: "run-1", DeviceID: "edge1"},
		{Name: "artifact.verified", Message: "artifact integrity verified SHA-256", Source: "agent", RunID: "run-1", DeviceID: "edge1"},
		{Name: "dhcp.offer", Message: "DHCP offer and ACK assigned 192.0.2.10", Source: "bootstrap:dhcp.log", RunID: "run-1", DeviceID: "edge1"},
		{Name: "tftp.transfer", Message: "TFTP RRQ network-confg transfer complete", Source: "bootstrap:tftp.log", RunID: "run-1", DeviceID: "edge1"},
		{Name: "autoinstall.complete", Message: "AutoInstall complete; configuration applied", Source: "device-console:console.log", RunID: "run-1", DeviceID: "edge1", Raw: "hostname edge1\nFastEthernet0/0 192.0.2.10"},
		{Name: "execution.report", Message: "Provider execution report completed and verified", Source: "provider", RunID: "run-1", DeviceID: "edge1"},
	}
	report := Analyze(events, Filter{RunID: "run-1", DeviceID: "edge1", ExpectedHostname: "edge1", ExpectedIPv4: "192.0.2.10"})
	if report.ObservedStatus != "success" || report.LastConfirmedSuccessfulStage != "device post-check" || report.FailureStage != "" {
		t.Fatalf("unexpected successful diagnostic: %#v", report)
	}
	if report.Confidence != "high" || len(report.Evidence) < 6 {
		t.Fatalf("success report omitted correlated evidence: %#v", report)
	}
}

func TestAnalyzeDoesNotCallConsoleOnlyEvidenceEndToEndSuccess(t *testing.T) {
	report := Analyze([]Event{{Source: "device-console:console.log", Raw: "hostname edge1\nGigabitEthernet0/0 192.0.2.10"}}, Filter{DeviceID: "edge1", ExpectedHostname: "edge1", ExpectedIPv4: "192.0.2.10"})
	// The console proves the device post-check, so exactly the seven earlier
	// lifecycle stages must be reported missing (and the post-check must not).
	if report.ObservedStatus != "incomplete evidence" || report.LastConfirmedSuccessfulStage != "" || len(report.MissingEvidence) != 7 {
		t.Fatalf("console state was treated as full workflow proof or missing evidence was incomplete: %#v", report)
	}
	for _, missing := range report.MissingEvidence {
		if strings.Contains(missing, "post-configuration") {
			t.Fatalf("observed device post-check was reported missing: %#v", report.MissingEvidence)
		}
	}
}

func TestAnalyzeFailureScenarios(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		wantStage  string
		wantCode   string
		wantAction string
	}{
		{name: "no DHCP offer", message: "DHCP Discover observed; no DHCP offer received", wantStage: "DHCP", wantCode: "DHCP_NO_OFFER", wantAction: "isolated bootstrap interface"},
		{name: "missing AutoInstall bootfile", message: "TFTP RRQ network-confg failed: file not found", wantStage: "TFTP", wantCode: "TFTP_FILE_MISSING", wantAction: "generated TFTP allowlist"},
		{name: "TFTP timeout", message: "TFTP transfer timed out", wantStage: "TFTP", wantCode: "TFTP_TRANSFER_FAILED", wantAction: "UDP reachability"},
		{name: "artifact hash mismatch", message: "artifact integrity check failed: hash mismatch", wantStage: "artifact integrity", wantCode: "ARTIFACT_HASH_MISMATCH", wantAction: "published SHA-256"},
		{name: "unsupported model", message: "unsupported model profile cisco/ios/unknown", wantStage: "profile selection", wantCode: "UNSUPPORTED_DEVICE_PROFILE", wantAction: "exact device model"},
		{name: "invalid native config", message: "% Invalid input detected at '^' marker", wantStage: "device configuration", wantCode: "DEVICE_CONFIG_REJECTED", wantAction: "native template"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := Analyze([]Event{{Message: test.message, Source: "device-console:console.log", RunID: "run-1", DeviceID: "edge1"}}, Filter{RunID: "run-1", DeviceID: "edge1"})
			if report.ObservedStatus != "failed" || report.FailureStage != test.wantStage || report.LikelyCause == "" || !strings.Contains(report.RecommendedNextAction, test.wantAction) {
				t.Fatalf("unexpected diagnostic: %#v", report)
			}
			found := false
			for _, evidence := range report.Evidence {
				if evidence.Code == test.wantCode {
					found = true
				}
			}
			if !found {
				t.Fatalf("diagnostic evidence omitted code %q: %#v", test.wantCode, report.Evidence)
			}
		})
	}
}

func TestAnalyzeFiltersCorrelatedRunAndDeviceEvents(t *testing.T) {
	report := Analyze([]Event{
		{Message: "no DHCP offer", Source: "bootstrap", RunID: "run-other", DeviceID: "edge1"},
		{Message: "TFTP RRQ network-confg transfer complete", Source: "bootstrap", RunID: "run-1", DeviceID: "edge2"},
		{Message: "catalog sync complete", Source: "agent", RunID: "run-1", DeviceID: "edge1"},
	}, Filter{RunID: "run-1", DeviceID: "edge1"})
	if report.ObservedStatus != "incomplete evidence" || report.LastConfirmedSuccessfulStage != "artifact synchronization" {
		t.Fatalf("filter admitted unrelated failure events: %#v", report)
	}
}

func TestReadFilesParsesStructuredEventsAndRedactsRawSecrets(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "agent.jsonl")
	data := "{\"event_id\":\"evt-1\",\"timestamp\":\"2026-11-18T12:00:00Z\",\"level\":\"INFO\",\"event\":\"agent.catalog.completed\",\"message\":\"catalog sync complete\",\"service\":\"agent\",\"source\":\"runner\",\"run_id\":\"run-1\"}\npassword=should-not-appear\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	events, err := ReadFiles([]SourceFile{{Name: "agent", Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].RunID != "run-1" || events[0].Name != "agent.catalog.completed" {
		t.Fatalf("structured event fields were not retained: %#v", events)
	}
	if strings.Contains(events[1].Raw, "should-not-appear") {
		t.Fatal("raw secret was not redacted")
	}
}
