package application

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"infraflow/internal/domain"
	"infraflow/pkg/protocol"
	"infraflow/provider/internal/adapters/filesystem"
)

// AGT-11: the Provider must never turn a missing or partial Agent report into
// a success. These tests pin the outcome of every report shape against the
// site's current catalog.

func outcomeSite(entries ...ArtifactEntry) *SiteSummary {
	return &SiteSummary{ID: "lab", Artifacts: entries, Agents: []domain.Agent{{ID: "agent-lab", SiteID: "lab"}}}
}

func outcomeReport(results ...protocol.ArtifactResult) protocol.AgentReport {
	report := protocol.AgentReport{AgentID: "agent-lab", ReportedAt: time.Now().UTC(), Artifacts: results}
	report.ReportID = protocol.ComputeReportID(report)
	return report
}

func TestReportOutcomeClassification(t *testing.T) {
	a := ArtifactEntry{Path: "lab/a.cfg", OutputHash: "ha"}
	b := ArtifactEntry{Path: "lab/b.cfg", OutputHash: "hb"}
	done := func(entry ArtifactEntry) protocol.ArtifactResult {
		return protocol.ArtifactResult{Path: entry.Path, OutputHash: entry.OutputHash, Status: protocol.StatusCompleted}
	}
	cases := []struct {
		name           string
		site           *SiteSummary
		report         protocol.AgentReport
		outcome        string
		missing, stale int
		reasonFragment string
	}{
		{"complete", outcomeSite(a, b), outcomeReport(done(a), done(b)), ReportOutcomeComplete, 0, 0, "every published"},
		{"empty report is incomplete, not success", outcomeSite(a, b), outcomeReport(), ReportOutcomeIncomplete, 2, 0, "0 of 2"},
		{"partial report is incomplete", outcomeSite(a, b), outcomeReport(done(a)), ReportOutcomeIncomplete, 1, 0, "1 of 2"},
		{"failure wins over missing", outcomeSite(a, b), outcomeReport(protocol.ArtifactResult{Path: a.Path, OutputHash: a.OutputHash, Status: protocol.StatusFailed}), ReportOutcomeFailed, 1, 0, "failed or blocked"},
		{"blocked counts as failure", outcomeSite(a), outcomeReport(protocol.ArtifactResult{Path: a.Path, OutputHash: a.OutputHash, Status: protocol.StatusBlocked}), ReportOutcomeFailed, 0, 0, "failed or blocked"},
		{"regenerated site makes the report stale", outcomeSite(ArtifactEntry{Path: a.Path, OutputHash: "new"}), outcomeReport(done(a)), ReportOutcomeStale, 0, 1, "no longer published"},
		{"site without artifacts", outcomeSite(), outcomeReport(done(a)), ReportOutcomeStale, 0, 0, "publishes no artifact"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			summary := lastSiteReport([]protocol.AgentReport{test.report}, test.site)
			if summary == nil {
				t.Fatal("report not matched to its site")
			}
			if summary.Outcome != test.outcome || summary.Missing != test.missing || summary.Stale != test.stale {
				t.Fatalf("got outcome=%s missing=%d stale=%d, want %s/%d/%d", summary.Outcome, summary.Missing, summary.Stale, test.outcome, test.missing, test.stale)
			}
			if !strings.Contains(summary.OutcomeReason, test.reasonFragment) {
				t.Fatalf("reason %q does not explain the outcome (%q)", summary.OutcomeReason, test.reasonFragment)
			}
			if summary.Expected != len(test.site.Artifacts) {
				t.Fatalf("expected count %d, want %d", summary.Expected, len(test.site.Artifacts))
			}
		})
	}
}

func TestLatestReportDecidesOutcome(t *testing.T) {
	a := ArtifactEntry{Path: "lab/a.cfg", OutputHash: "ha"}
	site := outcomeSite(a)
	older := outcomeReport(protocol.ArtifactResult{Path: a.Path, OutputHash: a.OutputHash, Status: protocol.StatusCompleted})
	older.ReportedAt = time.Now().Add(-time.Hour)
	newer := outcomeReport(protocol.ArtifactResult{Path: a.Path, OutputHash: a.OutputHash, Status: protocol.StatusFailed})
	if summary := lastSiteReport([]protocol.AgentReport{newer, older}, site); summary.Outcome != ReportOutcomeFailed {
		t.Fatalf("an older success must not hide a newer failure: %s", summary.Outcome)
	}
}

func TestRejectedReportsAreRecordedAndDuplicatesAreNotDoubleCounted(t *testing.T) {
	artifact := protocol.Artifact{Type: "inventory", Path: "lab/inventory.json", InputHash: "input", OutputHash: "output"}
	events, err := filesystem.NewEventStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reports := &memoryReports{}
	service := NewServiceWithJobsAgentsEvents(memoryArtifacts{items: []protocol.Artifact{artifact}}, reports, fakeGenerator{}, nil, nil, events, testDependencies())

	valid := outcomeReport(protocol.ArtifactResult{Path: artifact.Path, OutputHash: artifact.OutputHash, Status: protocol.StatusCompleted})
	if added, err := service.SubmitReport(valid); err != nil || !added {
		t.Fatalf("valid report: added=%v err=%v", added, err)
	}
	// Duplicate delivery (Agent retried after a lost acknowledgement).
	if added, err := service.SubmitReport(valid); err != nil || added {
		t.Fatalf("duplicate report must be acknowledged without a second entry: added=%v err=%v", added, err)
	}
	if len(service.Reports()) != 1 {
		t.Fatalf("duplicate stored twice: %d reports", len(service.Reports()))
	}

	invalid := []struct {
		name   string
		report protocol.AgentReport
		reason string
	}{
		{"tampered report id", func() protocol.AgentReport { r := valid; r.ReportID = strings.Repeat("0", 64); return r }(), "invalid metadata"},
		{"unknown status", outcomeReport(protocol.ArtifactResult{Path: artifact.Path, OutputHash: artifact.OutputHash, Status: "done"}), "invalid artifact status"},
		{"unpublished artifact", outcomeReport(protocol.ArtifactResult{Path: "lab/other.cfg", OutputHash: "x", Status: protocol.StatusCompleted}), "does not match published state"},
		{"missing timestamp", func() protocol.AgentReport { r := valid; r.ReportedAt = time.Time{}; return r }(), "invalid metadata"},
	}
	for _, test := range invalid {
		if _, err := service.SubmitReport(test.report); err == nil {
			t.Fatalf("%s: invalid report accepted", test.name)
		}
	}
	if len(service.Reports()) != 1 {
		t.Fatalf("an invalid report was persisted: %d reports", len(service.Reports()))
	}
	rejected := 0
	for _, event := range service.Events() {
		if event.Type != "agent.report.rejected" {
			continue
		}
		rejected++
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if reason, _ := payload["reason"].(string); reason == "" || strings.Contains(reason, ErrInvalidAgentReport.Error()+":") {
			t.Fatalf("rejection reason missing or unstripped: %v", payload)
		}
	}
	if rejected != len(invalid) {
		t.Fatalf("recorded %d rejections, want %d", rejected, len(invalid))
	}
}
