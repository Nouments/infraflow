package observability

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// Regression (GNS3 finalization lab 2026-11-19): two Agents on one host
// shared an outbox; after a rejected cross-site run every batch mixed
// identities, the Provider rejected the whole batch, and no Agent could
// complete a run again.
func TestOutboxPendingForSkipsEventsOfOtherIdentities(t *testing.T) {
	outbox, err := NewOutbox(filepath.Join(t.TempDir(), "pending.json"), 0)
	if err != nil {
		t.Fatal(err)
	}
	add := func(index int, agent, site string) {
		t.Helper()
		err := outbox.Append(Event{
			ID: fmt.Sprintf("%032x", index), Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level: "INFO", Event: "agent.test", Message: "m", Service: "agent", Source: "agent", AgentID: agent, SiteID: site,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	add(1, "agent-b", "site-b")
	add(2, "agent-a", "site-b") // rejected cross-site attempt: never deliverable
	add(3, "agent-a", "site-a")
	add(4, "", "")
	events, err := outbox.PendingFor(MaxIngestBatch, "agent-a", "site-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].AgentID != "agent-a" || events[1].AgentID != "" {
		t.Fatalf("expected only agent-a/site-a and identity-free events, got %+v", events)
	}
	if other, _ := outbox.PendingFor(MaxIngestBatch, "agent-b", "site-b"); len(other) != 2 {
		t.Fatalf("agent-b must still see its own queued events, got %d", len(other))
	}
}
