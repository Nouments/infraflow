package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readServiceLines(t *testing.T, lines ...string) []Event {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dhcp.log")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	events, err := ReadFiles([]SourceFile{{Name: "bootstrap", Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	return events
}

// Regression (GNS3 IOSv run 2026-11-19): serve-dhcp without -agent-config
// prints "<RFC3339> <LEVEL> <event> <detail>" lines. The diagnostics engine
// treated them as unclassified text and reported DHCP evidence as missing,
// although the IOSv AutoInstall client had been offered a lease ten times
// without ever requesting it.
func TestStandaloneServiceLinesDiagnoseIgnoredOffers(t *testing.T) {
	events := readServiceLines(t,
		"InfraFlow DHCP server listening on eth0/192.0.2.2:67 for 192.0.2.0/24",
		"2026-11-19T15:59:20Z INFO dhcp.discover client 0c:eb:25:a0:00:00 reservation r1 ",
		"2026-11-19T15:59:20Z INFO dhcp.offer client 0c:eb:25:a0:00:00 reservation r1 ip 192.0.2.11 ",
		"2026-11-19T15:59:23Z INFO dhcp.discover client 0c:eb:25:a0:00:00 reservation r1 ",
		"2026-11-19T15:59:23Z INFO dhcp.offer client 0c:eb:25:a0:00:00 reservation r1 ip 192.0.2.11 ",
		"2026-11-19T15:59:27Z INFO dhcp.discover client 0c:eb:25:a0:00:00 reservation r1 ",
		"2026-11-19T15:59:27Z INFO dhcp.offer client 0c:eb:25:a0:00:00 reservation r1 ip 192.0.2.11 ",
	)
	if events[1].Name != "dhcp.discover" || events[1].DeviceID != "r1" || events[1].Level != "INFO" {
		t.Fatalf("service line not parsed: %#v", events[1])
	}
	if events[0].Name != "" {
		t.Fatalf("banner line must stay unclassified text: %#v", events[0])
	}
	report := Analyze(events, Filter{DeviceID: "r1"})
	if report.FailureStage != StageDHCP || report.FailureCode != "DHCP_OFFER_NOT_ACCEPTED" || !strings.Contains(report.LikelyCause, "3 offers") {
		t.Fatalf("expected an ignored-offer DHCP diagnosis, got stage=%q code=%q cause=%q", report.FailureStage, report.FailureCode, report.LikelyCause)
	}
	if !hasEvidence(report, "DHCP_OFFER") || !hasEvidence(report, "DHCP_DISCOVER") {
		t.Fatalf("offers and discovers must be cited as evidence: %#v", report.Evidence)
	}
}

func TestStandaloneServiceLinesConfirmLeaseAndTransfer(t *testing.T) {
	events := readServiceLines(t,
		"2026-11-19T16:12:30Z INFO dhcp.discover client 0c:eb:25:a0:00:00 reservation r1 ",
		"2026-11-19T16:12:30Z INFO dhcp.offer client 0c:eb:25:a0:00:00 reservation r1 ip 192.0.2.11 ",
		"2026-11-19T16:12:30Z INFO dhcp.ack client 0c:eb:25:a0:00:00 reservation r1 ip 192.0.2.11 ",
		"2026-11-19T16:12:42Z INFO tftp.rrq file r1-confg from 192.0.2.11 ",
		"2026-11-19T16:12:42Z INFO tftp.transfer.completed file r1-confg from 192.0.2.11 ",
	)
	report := Analyze(events, Filter{})
	if report.FailureCode != "" || report.LastConfirmedSuccessfulStage != StageTFTP {
		t.Fatalf("expected DHCP and TFTP confirmed without failure, got code=%q last=%q", report.FailureCode, report.LastConfirmedSuccessfulStage)
	}
	// A device-filtered query still sees the TFTP lines (they name no
	// reservation) only as uncorrelated evidence, never as a success claim
	// for another device.
	if events[3].DeviceID != "" {
		t.Fatalf("TFTP line must not invent a device identity: %#v", events[3])
	}
}

// Regression (GNS3 IOSv run 2026-11-19): a local flash probe in the device
// console was reported as TFTP_FILE_MISSING after a successful transfer.
func TestLocalFlashFileNotFoundIsNotATFTPFailure(t *testing.T) {
	events := readServiceLines(t,
		"2026-11-19T16:12:30Z INFO dhcp.ack client 0c:eb:25:a0:00:00 reservation r1 ip 192.0.2.11 ",
		"%Error opening flash0:ios_config.txt (File not found)",
		"2026-11-19T16:12:42Z INFO tftp.transfer.completed file r1-confg from 192.0.2.11 ",
	)
	if report := Analyze(events, Filter{}); report.FailureCode == "TFTP_FILE_MISSING" {
		t.Fatalf("local flash probe misclassified as TFTP failure: %#v", report)
	}
	missing := readServiceLines(t, "%Error opening tftp://192.0.2.2/network-confg (File not found)")
	if report := Analyze(missing, Filter{}); report.FailureCode != "TFTP_FILE_MISSING" {
		t.Fatalf("a TFTP file-not-found console line must still be diagnosed, got %q", report.FailureCode)
	}
}
