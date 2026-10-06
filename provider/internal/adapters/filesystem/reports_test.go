package filesystem

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"infraflow/pkg/protocol"
)

func TestReportStorePersistsAndDeduplicates(t *testing.T) {
	root := t.TempDir()
	store, err := NewReportStore(root)
	if err != nil {
		t.Fatal(err)
	}
	report := testReport("agent-01")
	if added, err := store.Append(report); err != nil || !added {
		t.Fatalf("first append = %v, %v", added, err)
	}
	if added, err := store.Append(report); err != nil || added {
		t.Fatalf("duplicate append = %v, %v", added, err)
	}
	reloaded, err := NewReportStore(root)
	if err != nil || len(reloaded.List()) != 1 || reloaded.List()[0].ReportID != report.ReportID {
		t.Fatalf("report did not persist: %#v, %v", reloaded, err)
	}
}

func TestReportStoreSerializesConcurrentDuplicateAppends(t *testing.T) {
	store, err := NewReportStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	report := testReport("agent-02")
	var wait sync.WaitGroup
	var added int
	var addedMu sync.Mutex
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			inserted, err := store.Append(report)
			if err != nil {
				t.Errorf("append report: %v", err)
				return
			}
			if inserted {
				addedMu.Lock()
				added++
				addedMu.Unlock()
			}
		}()
	}
	wait.Wait()
	if added != 1 || len(store.List()) != 1 {
		t.Fatalf("expected one record, got %d insertions and %d reports", added, len(store.List()))
	}
}

func TestReportStoreRejectsCorruptAndSymlinkFiles(t *testing.T) {
	corruptRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(corruptRoot, ".agent-reports.jsonl"), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewReportStore(corruptRoot); err == nil {
		t.Fatal("expected corrupt report file to be rejected")
	}

	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "reports.jsonl")
	if err := os.WriteFile(outside, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".agent-reports.jsonl")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := NewReportStore(root); err == nil {
		t.Fatal("expected report-store symlink to be rejected")
	}
}

func TestReportStoreRequiresExistingDirectory(t *testing.T) {
	if _, err := NewReportStore(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected missing artifact directory to be rejected")
	}
}

func testReport(agentID string) protocol.AgentReport {
	report := protocol.AgentReport{AgentID: agentID, ReportedAt: time.Now().UTC()}
	report.ReportID = protocol.ComputeReportID(report)
	return report
}
