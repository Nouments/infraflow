package observability

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	if err != nil {
		t.Fatalf("central logs are not private: %v, %v", info, err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("central log permissions are %04o, want 0600", info.Mode().Perm())
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

func TestStoreRetentionPurgesExpiredEventsAndPersistsTheBoundedSet(t *testing.T) {
	directory := t.TempDir()
	retention := 24 * time.Hour
	store, err := NewStoreWithRetention(directory, DefaultCentralLogLimit, retention)
	if err != nil {
		t.Fatal(err)
	}
	old := storeEvent("old-event", "INFO", "agent-1", "run-1", "expired")
	old.Timestamp = time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339Nano)
	current := storeEvent("current-event", "INFO", "agent-1", "run-1", "retained")
	if _, err := store.Append([]Event{old, current}); err != nil {
		t.Fatal(err)
	}
	removed, err := store.Purge(time.Now())
	if err != nil || removed != 1 {
		t.Fatalf("purge removed %d events, %v; want one expired event", removed, err)
	}
	if _, err := store.List(Query{After: old.ID, Limit: 10}); err != ErrCursorExpired {
		t.Fatalf("expired cursor returned %v, want ErrCursorExpired", err)
	}
	reloaded, err := NewStoreWithRetention(directory, DefaultCentralLogLimit, retention)
	if err != nil {
		t.Fatal(err)
	}
	page, err := reloaded.List(Query{Limit: 10})
	if err != nil || len(page.Events) != 1 || page.Events[0].ID != current.ID {
		t.Fatalf("purged store did not persist only retained events: %#v, %v", page, err)
	}
}

// PRV-19: the purge skips its scan while nothing can have expired. The cache
// must never hide an expiry: neither for an event ingested late with an older
// timestamp (Agent outbox replay) nor for events that expire as time passes.
func TestStoreRetentionCacheNeverHidesAnExpiredEvent(t *testing.T) {
	retention := time.Hour
	store, err := NewStoreWithRetention(t.TempDir(), DefaultCentralLogLimit, retention)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	fresh := storeEvent("fresh", "INFO", "agent-1", "run-1", "fresh")
	fresh.Timestamp = now.Format(time.RFC3339Nano)
	if _, err := store.Append([]Event{fresh}); err != nil {
		t.Fatal(err)
	}
	if removed, err := store.Purge(now); err != nil || removed != 0 {
		t.Fatalf("nothing should expire yet: %d %v", removed, err)
	}
	late := storeEvent("late", "INFO", "agent-1", "run-0", "replayed from an Agent outbox")
	late.Timestamp = now.Add(-30 * time.Minute).Format(time.RFC3339Nano)
	if _, err := store.Append([]Event{late}); err != nil {
		t.Fatal(err)
	}
	if removed, err := store.Purge(now.Add(45 * time.Minute)); err != nil || removed != 1 {
		t.Fatalf("late event with an older timestamp must expire first: removed=%d %v", removed, err)
	}
	if removed, err := store.Purge(now.Add(2 * time.Hour)); err != nil || removed != 1 {
		t.Fatalf("remaining event must expire as time passes: removed=%d %v", removed, err)
	}
	if removed, err := store.Purge(now.Add(3 * time.Hour)); err != nil || removed != 0 {
		t.Fatalf("empty store purge: %d %v", removed, err)
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
	if err != nil {
		t.Fatalf("outbox file is not private: %v, %v", info, err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("outbox permissions are %04o, want 0600", info.Mode().Perm())
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

func TestStoreCompactsOldestEventsInsteadOfFailingWhenFull(t *testing.T) {
	directory := t.TempDir()
	store, err := NewStore(directory, 8<<10)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	for index := 0; index < 200; index++ {
		event := Event{ID: fmt.Sprintf("evt-%04d", index), Timestamp: base.Add(time.Duration(index) * time.Second).Format(time.RFC3339Nano), Level: "INFO", Event: "provider.http.request", Message: "HTTP request completed", Service: "provider", Source: "httpapi"}
		if _, err := store.Append([]Event{event}); err != nil {
			t.Fatalf("append %d failed after the size limit: %v", index, err)
		}
	}
	page, err := store.List(Query{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) == 0 || len(page.Events) >= 200 || page.Events[len(page.Events)-1].ID != "evt-0199" {
		t.Fatalf("compaction did not keep the newest events: %d events, last %q", len(page.Events), page.Events[len(page.Events)-1].ID)
	}
	reopened, err := NewStore(directory, 8<<10)
	if err != nil {
		t.Fatalf("compacted store could not be reopened: %v", err)
	}
	reloaded, _ := reopened.List(Query{Limit: 1000})
	if len(reloaded.Events) != len(page.Events) {
		t.Fatalf("reopened store has %d events, want %d", len(reloaded.Events), len(page.Events))
	}
}

// TestStoreIDIndexSurvivesCompactionAndReload pins D13: byID holds indexes
// into events, so after a compaction shifts every position, and after a
// reload from disk, duplicate and conflict detection must still find the
// right event.
func TestStoreIDIndexSurvivesCompactionAndReload(t *testing.T) {
	directory := t.TempDir()
	store, err := NewStore(directory, 16<<10)
	if err != nil {
		t.Fatal(err)
	}
	var last Event
	for index := 0; index < 200; index++ {
		last = storeEvent(fmt.Sprintf("event-%03d", index), "INFO", "agent-1", "run-1", strings.Repeat("x", 100))
		if _, err := store.Append([]Event{last}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(store *Store, stage string) {
		t.Helper()
		if len(store.byID) != len(store.events) {
			t.Fatalf("%s: index has %d ids for %d events", stage, len(store.byID), len(store.events))
		}
		for id, index := range store.byID {
			if store.events[index].ID != id {
				t.Fatalf("%s: id %s points to %s", stage, id, store.events[index].ID)
			}
		}
		if result, err := store.Append([]Event{last}); err != nil || result.Duplicate != 1 {
			t.Fatalf("%s: identical retry not detected: %#v %v", stage, result, err)
		}
		conflict := last
		conflict.Message = "different body"
		if _, err := store.Append([]Event{conflict}); err == nil {
			t.Fatalf("%s: conflicting reuse of a retained id was accepted", stage)
		}
	}
	if _, present := store.byID["event-000"]; present {
		t.Fatal("compaction did not run: the first event is still stored")
	}
	check(store, "after compaction")
	reloaded, err := NewStore(directory, 16<<10)
	if err != nil {
		t.Fatal(err)
	}
	check(reloaded, "after reload")
}
