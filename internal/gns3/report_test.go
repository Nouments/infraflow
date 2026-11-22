package gns3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validReport() ScenarioReport {
	now := time.Now().UTC()
	return ScenarioReport{
		SchemaVersion: 1, ScenarioID: "S1", Title: "t", Commit: "abc", InfraFlowVersion: "dev",
		GNS3:      ReportGNS3{Version: "3.1.0a4", Project: "InfraFlow-Automated-Validation"},
		StartedAt: now, FinishedAt: now.Add(time.Second),
		InitialState: "stopped", FinalState: "started",
		Checks: []Check{{Name: "c", Expected: "x", Observed: "x", Status: "PASS"}},
		Status: "PASS", StatusReason: "all checks passed",
	}
}

func hasProblem(problems []string, fragment string) bool {
	for _, problem := range problems {
		if strings.Contains(problem, fragment) {
			return true
		}
	}
	return false
}

func TestReportValidation(t *testing.T) {
	if problems := validReport().Validate(); len(problems) != 0 {
		t.Fatalf("valid report rejected: %v", problems)
	}
	cases := []struct {
		name     string
		mutate   func(*ScenarioReport)
		fragment string
	}{
		{"pass without checks", func(r *ScenarioReport) { r.Checks = nil }, "at least one executed check"},
		{"pass with failed check", func(r *ScenarioReport) { r.Checks[0].Status = "FAIL" }, "contradicts checks[0]"},
		{"pass with not-run check", func(r *ScenarioReport) { r.Checks[0].Status = "NOT_RUN" }, "contradicts checks[0]"},
		{"unknown status", func(r *ScenarioReport) { r.Status = "OK" }, "is not one of"},
		{"non-pass without diagnosis", func(r *ScenarioReport) { r.Status = "PARTIAL" }, "requires a diagnosis"},
		{"unknown stage", func(r *ScenarioReport) { r.Stages = []Stage{{Name: "TFTP_OK", Status: "PASS"}} }, "not a provisioning stage"},
		{"password leaked", func(r *ScenarioReport) { r.Warnings = []string{`password: admin123`} }, "credential-like"},
		{"bearer leaked", func(r *ScenarioReport) { r.Errors = []string{"Authorization: Bearer abcdefghijklmnopqrstuv1234"} }, "credential-like"},
		{"jwt leaked", func(r *ScenarioReport) {
			r.FinalState = "token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhZG1pbiJ9.x"
		}, "credential-like"},
		{"bad confidence", func(r *ScenarioReport) {
			r.Status = "UNSUPPORTED"
			r.Diagnosis = &Diagnosis{Expected: "e", Observed: "o", Cause: "c", Confidence: "certain", Evidence: []string{"x"}, NextCheck: "n"}
		}, "confidence"},
	}
	for _, test := range cases {
		report := validReport()
		test.mutate(&report)
		if problems := report.Validate(); !hasProblem(problems, test.fragment) {
			t.Fatalf("%s: want problem containing %q, got %v", test.name, test.fragment, problems)
		}
	}
	// Mentioning that a credential was used is fine.
	report := validReport()
	report.Warnings = []string{"controller uses the default admin/admin account; change it", "v3 API, bearer authentication"}
	if problems := report.Validate(); len(problems) != 0 {
		t.Fatalf("descriptive warning flagged: %v", problems)
	}
}

func TestEvidenceIsVerifiedAndConfined(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "console.log"), []byte("boot"), 0o644); err != nil {
		t.Fatal(err)
	}
	digest, _ := FileSHA256(filepath.Join(root, "console.log"))
	report := validReport()
	report.Logs = []EvidenceRef{{Path: "console.log", SHA256: digest}}
	if problems := report.VerifyEvidence(root); len(problems) != 0 {
		t.Fatalf("valid evidence rejected: %v", problems)
	}
	report.Logs = []EvidenceRef{{Path: "console.log", SHA256: strings.Repeat("0", 64)}, {Path: "../etc/passwd", SHA256: "x"}, {Path: "missing.pcap", SHA256: "x"}}
	problems := report.VerifyEvidence(root)
	for _, fragment := range []string{"SHA-256 mismatch", "relative to the report directory", "missing.pcap"} {
		if !hasProblem(problems, fragment) {
			t.Fatalf("missing %q in %v", fragment, problems)
		}
	}
}
