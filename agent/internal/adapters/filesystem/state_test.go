package filesystem

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infraflow/pkg/protocol"
)

func TestStateStoreSavesArtifactsAndPrivateReport(t *testing.T) {
	root := t.TempDir()
	store := NewStateStore(root)
	artifactData := []byte("inventory")
	if err := store.SaveArtifact("lab/inventory.json", protocol.SHA256(artifactData), strings.NewReader(string(artifactData))); err != nil {
		t.Fatal(err)
	}
	report := protocol.AgentReport{AgentID: "agent-01", ReportedAt: time.Now().UTC()}
	report.ReportID = protocol.ComputeReportID(report)
	if err := store.SaveReport(report); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "lab", "inventory.json"))
	if err != nil || string(data) != "inventory" {
		t.Fatalf("artifact not persisted: %q, %v", data, err)
	}
	artifact, err := store.OpenArtifact("lab/inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	opened, err := io.ReadAll(artifact)
	_ = artifact.Close()
	if err != nil || string(opened) != "inventory" {
		t.Fatalf("artifact could not be reopened: %q, %v", opened, err)
	}
	data, err = os.ReadFile(filepath.Join(root, ".infraflow-agent-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved protocol.AgentReport
	if err := json.Unmarshal(data, &saved); err != nil || saved.ReportID != report.ReportID {
		t.Fatalf("state report not persisted: %#v, %v", saved, err)
	}
	info, err := os.Stat(filepath.Join(root, ".infraflow-agent-state.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("state file should be private, got %v, %v", info, err)
	}
}

func TestStateStoreRejectsSymlinkArtifactParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "lab")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	unsafe := []byte("unsafe")
	if err := NewStateStore(root).SaveArtifact("lab/inventory.json", protocol.SHA256(unsafe), strings.NewReader(string(unsafe))); err == nil {
		t.Fatal("expected symlink parent to be rejected")
	}
	if _, err := os.Stat(filepath.Join(outside, "inventory.json")); !os.IsNotExist(err) {
		t.Fatalf("artifact escaped output root: %v", err)
	}
}

func TestStateStoreRejectsHashMismatch(t *testing.T) {
	root := t.TempDir()
	store := NewStateStore(root)
	previous := []byte("previous")
	if err := store.SaveArtifact("lab/inventory.json", protocol.SHA256(previous), strings.NewReader(string(previous))); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveArtifact("lab/inventory.json", protocol.SHA256([]byte("expected")), strings.NewReader("tampered")); err == nil {
		t.Fatal("expected hash mismatch")
	}
	data, err := os.ReadFile(filepath.Join(root, "lab", "inventory.json"))
	if err != nil || string(data) != "previous" {
		t.Fatalf("mismatched stream replaced existing artifact: %q, %v", data, err)
	}
}
