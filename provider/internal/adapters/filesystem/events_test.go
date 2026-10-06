package filesystem

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infraflow/internal/domain"
)

func TestEventStoreAppendsIdempotentlyAndReloadsHashChain(t *testing.T) {
	root := t.TempDir()
	store, err := NewEventStore(root)
	if err != nil {
		t.Fatal(err)
	}
	first, created, err := store.Append(domain.Event{EventID: "evt-1", JobID: "job-1", Timestamp: time.Now().UTC(), Type: "job.created", Payload: json.RawMessage(`{"status":"planned"}`)})
	if err != nil || !created || first.Sequence != 1 || first.PreviousHash != "" {
		t.Fatalf("unexpected first event: %#v, %v, %v", first, created, err)
	}
	second, created, err := store.Append(domain.Event{EventID: "evt-2", AgentID: "agent-1", Timestamp: time.Now().UTC(), Type: "agent.heartbeat", Payload: json.RawMessage(`{"queue_depth":0}`)})
	if err != nil || !created || second.Sequence != 2 || second.PreviousHash != first.Hash {
		t.Fatalf("unexpected second event: %#v, %v, %v", second, created, err)
	}
	duplicate, created, err := store.Append(domain.Event{EventID: first.EventID})
	if err != nil || created || duplicate.Hash != first.Hash {
		t.Fatalf("duplicate event was not idempotent: %#v, %v, %v", duplicate, created, err)
	}
	reloaded, err := NewEventStore(root)
	if err != nil {
		t.Fatal(err)
	}
	events := reloaded.List()
	if len(events) != 2 || events[1].PreviousHash != events[0].Hash {
		t.Fatalf("reloaded chain is invalid: %#v", events)
	}
	info, err := os.Stat(filepath.Join(root, ".events.jsonl"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("event store should be private: %v, %v", info, err)
	}
}

func TestEventStoreRejectsTamperingAndUnsafeDirectory(t *testing.T) {
	root := t.TempDir()
	store, err := NewEventStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Append(domain.Event{EventID: "evt-1", Timestamp: time.Now().UTC(), Type: "job.created", Payload: json.RawMessage(`{"status":"planned"}`)}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "planned", "changed", 1))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewEventStore(root); err == nil {
		t.Fatal("expected tampered event store to be rejected")
	}

	unsafeRoot := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(unsafeRoot, ".events.jsonl")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := NewEventStore(unsafeRoot); err == nil {
		t.Fatal("expected symlink event store to be rejected")
	}
}
