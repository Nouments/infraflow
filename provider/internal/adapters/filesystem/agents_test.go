package filesystem

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"infraflow/internal/domain"
)

func TestAgentStorePersistsAndProtectsAgentState(t *testing.T) {
	root := t.TempDir()
	store, err := NewAgentStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	agent := domain.Agent{ID: "agent-01", SiteID: "site-01", Version: "0.1.0", Status: domain.AgentStatusOnline, RegisteredAt: now, LastSeenAt: now, UpdatedAt: now, Capabilities: []string{"inventory"}}
	created, err := store.Register(agent)
	if err != nil || !created {
		t.Fatalf("agent was not registered: %v, %v", created, err)
	}
	created, err = store.Register(agent)
	if err != nil || created {
		t.Fatalf("duplicate agent should be idempotently rejected: %v, %v", created, err)
	}
	loaded, err := store.Get(agent.ID)
	if err != nil || loaded.SiteID != agent.SiteID {
		t.Fatalf("agent was not loaded: %#v, %v", loaded, err)
	}
	info, err := os.Stat(filepath.Join(root, ".agents", agent.ID+".json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("agent file should be private: %v, %v", info, err)
	}

	updated := agent
	updated.QueueDepth = 3
	updated.UpdatedAt = now.Add(time.Second)
	updated.LastSeenAt = updated.UpdatedAt
	if err := store.Update(updated); err != nil {
		t.Fatal(err)
	}
	agents, err := store.List()
	if err != nil || len(agents) != 1 || agents[0].QueueDepth != 3 {
		t.Fatalf("unexpected agent list: %#v, %v", agents, err)
	}
}

func TestAgentStoreRejectsCorruptAndUnsafeFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agents", "agent-bad.json"), []byte(`{"id":"agent-bad"} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewAgentStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err == nil {
		t.Fatal("expected corrupt agent file to be rejected")
	}

	unsafeRoot := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(unsafeRoot, ".agents")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := NewAgentStore(unsafeRoot); err == nil {
		t.Fatal("expected symlink agent directory to be rejected")
	}
}
