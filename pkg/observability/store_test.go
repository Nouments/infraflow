package observability

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersistsDeduplicatesAndFiltersEvents(t *testing.T) {
	directory := t.TempDir()
	store, err := NewStore(directory, DefaultCentralLogLimit)
	if err != nil {
		t.Fatal(err)
	}
	events := []Event{
		storeEvent("event-1", "INFO", "agent-1", "run-1", "download started"),
		storeEvent("event-2", "ERROR", "agent-1", "run-1", "download failed"),
		storeEvent("event-3", "INFO", "agent-2", "run-2", "heartbeat"),
	}
	result, err := store.Append(events)
	if err != nil || result.Accepted != 3 || result.Duplicate != 0 {
		t.Fatalf("unexpected first append: %#v, %v", result, err)
	}
	result, err = store.Append([]Event{events[1]})
	if err != nil || result.Accepted != 0 || result.Duplicate != 1 {
		t.Fatalf("retry was not idempotent: %#v, %v", result, err)
	}
	conflict := events[1]
	conflict.Message = "different body"
	if _, err := store.Append([]Event{conflict}); err == nil {
		t.Fatal("reused event id with different contents was accepted")
	}

	page, err := store.List(Query{RunID: "run-1", Level: "ERROR", Limit: 10})
	if err != nil || len(page.Events) != 1 || page.Events[0].ID != "event-2" {
		t.Fatalf("query filters were not applied: %#v, %v", page, err)
	}
	page, err = store.List(Query{Limit: 1})
	if err != nil || len(page.Events) != 1 || page.Events[0].ID != "event-3" || page.Previous != "event-3" {
		t.Fatalf("first page cursor is incorrect: %#v, %v", page, err)
	}
	page, err = store.List(Query{Before: page.Previous, Limit: 2})
	if err != nil || len(page.Events) != 2 || page.Events[0].ID != "event-1" || page.Next != "event-2" {
		t.Fatalf("cursor did not page backward through history: %#v, %v", page, err)
	}

	reloaded, err := NewStore(directory, DefaultCentralLogLimit)
	if err != nil {
		t.Fatal(err)
	}
	page, err = reloaded.List(Query{RunID: "run-2", Limit: 10})
	if err != nil || len(page.Events) != 1 || page.Events[0].AgentID != "agent-2" {
		t.Fatalf("persisted event missing after reload: %#v, %v", page, err)
	}
	info, err := os.Stat(filepath.Join(directory, "central-events.jsonl"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("central logs are not private: %v, %v", info, err)
	}
}

func TestStoreReportsStorageLimitAndInvalidEvents(t *testing.T) {
	store, err := NewStore(t.TempDir(), 200)
	if err != nil {
		t.Fatal(err)
	}
	event := storeEvent("event-1", "INFO", "agent-1", "run-1", "payload")
	event.Message = "a message larger than the configured store capacity"
	if _, err := store.Append([]Event{event}); err == nil {
		t.Fatal("store saturation was not reported")
	}
	invalid := storeEvent("event-2", "INFO", "agent-1", "run-1", "bad timestamp")
	invalid.Timestamp = "yesterday"
	if _, err := store.Append([]Event{invalid}); err == nil {
		t.Fatal("invalid timestamp was accepted")
	}
}

func TestOutboxSurvivesOfflineRestartUntilCentralAcknowledgement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent", "pending.json")
	outbox, err := NewOutbox(path, DefaultOutboxLimit)
	if err != nil {
		t.Fatal(err)
	}
	event := storeEvent("offline-01", "WARN", "agent-01", "run-01", "provider unavailable")
	if err := outbox.Append(event); err != nil {
		t.Fatal(err)
	}
	if err := outbox.Append(event); err != nil {
		t.Fatalf("duplicate local append should be idempotent: %v", err)
	}
	if err := outbox.Append(storeEvent("offline-02", "INFO", "agent-01", "run-01", "still running locally")); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewOutbox(path, DefaultOutboxLimit)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := restarted.Pending(10)
	if err != nil || len(pending) != 2 {
		t.Fatalf("unacknowledged events were lost on restart: %#v, %v", pending, err)
	}
	if err := restarted.Ack([]string{pending[0].ID}); err != nil {
		t.Fatal(err)
	}
	acknowledged, err := NewOutbox(path, DefaultOutboxLimit)
	if err != nil {
		t.Fatal(err)
	}
	pending, err = acknowledged.Pending(10)
	if err != nil || len(pending) != 1 || pending[0].ID != "offline-02" {
		t.Fatalf("acknowledged event was not removed or remaining event was lost: %#v, %v", pending, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("outbox file is not private: %v, %v", info, err)
	}
}

func storeEvent(id, level, agentID, runID, message string) Event {
	return Event{
		ID: id, Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level: level, Event: "agent.test", Message: message,
		Service: "agent", Source: "agent-runner", AgentID: agentID,
		SiteID: "site-1", RunID: runID,
	}
}
