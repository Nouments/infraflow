package filesystem

import (
	"bytes"
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
	statePath := filepath.Join(root, ".infraflow-agent-state.json")
	if err := verifyPrivateReport(statePath); err != nil {
		t.Fatalf("state file should be private: %v", err)
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

func TestStateStoreAllowsVerifiedBootstrapArtifacts(t *testing.T) {
	root := t.TempDir()
	data := []byte("{\"service\":\"dns\"}\n")
	path := "lab/bootstrap/dns/config.json"
	store := NewStateStore(root)
	if err := store.SaveArtifact(path, protocol.SHA256(data), bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	opened, err := store.OpenArtifact(path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	got, err := io.ReadAll(opened)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("bootstrap artifact could not be reopened: %q, %v", got, err)
	}
	if err := store.SaveArtifact("lab/.infraflow-agent-state.json", protocol.SHA256(data), bytes.NewReader(data)); err == nil {
		t.Fatal("accepted private agent state as a publishable artifact")
	}
}

func TestStateStoreAllowsVerifiedCiscoAutoInstallFiles(t *testing.T) {
	root := t.TempDir()
	store := NewStateStore(root)
	for _, path := range []string{
		"lab/bootstrap/tftp/network-confg",
		"lab/bootstrap/tftp/edge7200-confg",
	} {
		data := []byte("IOS bootstrap artifact\n")
		if err := store.SaveArtifact(path, protocol.SHA256(data), bytes.NewReader(data)); err != nil {
			t.Fatalf("save Cisco AutoInstall artifact %q: %v", path, err)
		}
		opened, err := store.OpenArtifact(path)
		if err != nil {
			t.Fatalf("open Cisco AutoInstall artifact %q: %v", path, err)
		}
		got, err := io.ReadAll(opened)
		_ = opened.Close()
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("Cisco AutoInstall artifact %q changed in Agent state: %q, %v", path, got, err)
		}
	}
}

func TestStateStoreAllowsVerifiedNativeArtifacts(t *testing.T) {
	root := t.TempDir()
	store := NewStateStore(root)
	for _, test := range []struct {
		path string
		data []byte
	}{
		{path: "lab/native/device-edge-west-1.cfg", data: []byte("hostname edge-west-1\n")},
		{path: "lab/native/native-manifest.json", data: []byte(`{"schema_version":1}`)},
	} {
		if err := store.SaveArtifact(test.path, protocol.SHA256(test.data), bytes.NewReader(test.data)); err != nil {
			t.Fatalf("save verified native artifact %q: %v", test.path, err)
		}
		artifact, err := store.OpenArtifact(test.path)
		if err != nil {
			t.Fatalf("open verified native artifact %q: %v", test.path, err)
		}
		got, err := io.ReadAll(artifact)
		_ = artifact.Close()
		if err != nil || !bytes.Equal(got, test.data) {
			t.Fatalf("native artifact %q changed while staging: %q, %v", test.path, got, err)
		}
	}
}
