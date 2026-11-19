package application

import (
	"sync"
	"testing"
	"time"

	"infraflow/internal/domain"
	"infraflow/pkg/observability"
	"infraflow/provider/internal/adapters/filesystem"
)

// fakeClock is a deterministic, manually advanced time source.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *fakeClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}

type agentFixture struct {
	root  string
	clock *fakeClock
	logs  *observability.Store
}

// newProvider simulates one Provider process over a persistent data root.
func (fixture *agentFixture) newProvider(t *testing.T) *Service {
	t.Helper()
	agents, err := filesystem.NewAgentStore(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	events, err := filesystem.NewEventStore(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	service := NewServiceWithJobsAgentsEvents(nil, nil, nil, nil, agents, events, Dependencies{Logs: fixture.logs})
	service.SetAgentClock(fixture.clock.Now)
	if err := service.SetAgentHeartbeatTimeout(30 * time.Second); err != nil {
		t.Fatal(err)
	}
	return service
}

func newAgentFixture(t *testing.T) *agentFixture {
	t.Helper()
	root := t.TempDir()
	logs, err := observability.NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	return &agentFixture{root: root, clock: &fakeClock{now: time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)}, logs: logs}
}

func heartbeat(t *testing.T, service *Service, id, site string) domain.Agent {
	t.Helper()
	agent, err := service.HeartbeatAgent(id, AgentHeartbeat{SiteID: site, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func status(t *testing.T, service *Service, id string) domain.Agent {
	t.Helper()
	agent, err := service.Agent(id)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func register(t *testing.T, service *Service, id, site string) domain.Agent {
	t.Helper()
	agent, err := service.RegisterAgent(AgentRegistration{ID: id, SiteID: site, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// 1. A newly registered Agent without fresh heartbeat evidence is not ONLINE.
func TestAgentStatusNewRegistrationIsNotOnline(t *testing.T) {
	fixture := newAgentFixture(t)
	service := fixture.newProvider(t)
	if got := register(t, service, "agent-a", "site-a"); got.Status != domain.AgentStatusUnknown {
		t.Fatalf("registration response status = %q, want unknown", got.Status)
	}
	if got := status(t, service, "agent-a"); got.Status != domain.AgentStatusUnknown || got.LastHeartbeatAt != nil {
		t.Fatalf("registered agent without heartbeat = %q (%v), want unknown", got.Status, got.LastHeartbeatAt)
	}
}

// 2. A fresh authenticated heartbeat changes status to ONLINE.
func TestAgentStatusFreshHeartbeatIsOnline(t *testing.T) {
	fixture := newAgentFixture(t)
	service := fixture.newProvider(t)
	register(t, service, "agent-a", "site-a")
	fixture.clock.Advance(time.Second)
	if got := heartbeat(t, service, "agent-a", "site-a"); got.Status != domain.AgentStatusOnline {
		t.Fatalf("heartbeat response status = %q, want online", got.Status)
	}
	got := status(t, service, "agent-a")
	if got.Status != domain.AgentStatusOnline || got.HeartbeatAgeSeconds == nil || *got.HeartbeatAgeSeconds != 0 || got.TimeoutSeconds != 30 {
		t.Fatalf("fresh heartbeat observation = %+v", got)
	}
}

// 3 and 9. Expired heartbeat becomes OFFLINE exactly after the timeout, and
// recovery happens only on a new heartbeat.
func TestAgentStatusTimeoutAndRecoveryTransitions(t *testing.T) {
	fixture := newAgentFixture(t)
	service := fixture.newProvider(t)
	register(t, service, "agent-a", "site-a")
	heartbeat(t, service, "agent-a", "site-a")
	fixture.clock.Advance(30 * time.Second)
	if got := status(t, service, "agent-a"); got.Status != domain.AgentStatusOnline {
		t.Fatalf("status at exactly the timeout = %q, want online", got.Status)
	}
	fixture.clock.Advance(time.Second)
	got := status(t, service, "agent-a")
	if got.Status != domain.AgentStatusOffline || got.HeartbeatAgeSeconds == nil || *got.HeartbeatAgeSeconds != 31 {
		t.Fatalf("status after timeout = %+v, want offline at 31s", got)
	}
	// Listing applies the same freshness rule.
	list, err := service.Agents()
	if err != nil || len(list) != 1 || list[0].Status != domain.AgentStatusOffline {
		t.Fatalf("listed status after timeout = %+v, %v", list, err)
	}
	fixture.clock.Advance(10 * time.Minute)
	if got := status(t, service, "agent-a"); got.Status != domain.AgentStatusOffline {
		t.Fatalf("status long after timeout = %q", got.Status)
	}
	heartbeat(t, service, "agent-a", "site-a")
	if got := status(t, service, "agent-a"); got.Status != domain.AgentStatusOnline {
		t.Fatalf("status after recovery heartbeat = %q, want online", got.Status)
	}
}

// 4. A closed session (graceful disconnect, or silent loss of the process)
// does not remain ONLINE indefinitely.
func TestAgentStatusClosedSessionDoesNotStayOnline(t *testing.T) {
	fixture := newAgentFixture(t)
	service := fixture.newProvider(t)
	register(t, service, "agent-a", "site-a")
	heartbeat(t, service, "agent-a", "site-a")
	if _, err := service.DisconnectAgentContext(t.Context(), "agent-a", "site-a"); err != nil {
		t.Fatal(err)
	}
	if got := status(t, service, "agent-a"); got.Status != domain.AgentStatusOffline {
		t.Fatalf("status after graceful disconnect = %q, want offline immediately", got.Status)
	}
	// Silent loss: process dies without notice; only the timeout applies.
	register(t, service, "agent-b", "site-b")
	heartbeat(t, service, "agent-b", "site-b")
	fixture.clock.Advance(31 * time.Second)
	if got := status(t, service, "agent-b"); got.Status != domain.AgentStatusOffline {
		t.Fatalf("status after silent loss = %q, want offline", got.Status)
	}
	if _, err := service.DisconnectAgentContext(t.Context(), "agent-b", "site-a"); err == nil {
		t.Fatal("disconnect for the wrong site was accepted")
	}
}

// 5. A Provider restart does not restore a stale ONLINE state, even when the
// persisted heartbeat is still inside the timeout window.
func TestAgentStatusProviderRestartDoesNotRestoreOnline(t *testing.T) {
	fixture := newAgentFixture(t)
	first := fixture.newProvider(t)
	register(t, first, "agent-a", "site-a")
	heartbeat(t, first, "agent-a", "site-a")
	fixture.clock.Advance(5 * time.Second)

	restarted := fixture.newProvider(t)
	got := status(t, restarted, "agent-a")
	if got.Status == domain.AgentStatusOnline {
		t.Fatalf("restarted Provider restored ONLINE from persisted evidence: %+v", got)
	}
	if got.Status != domain.AgentStatusUnknown {
		t.Fatalf("status after restart within window = %q, want unknown", got.Status)
	}
	fixture.clock.Advance(30 * time.Second)
	if got := status(t, restarted, "agent-a"); got.Status != domain.AgentStatusOffline {
		t.Fatalf("status after restart and expiry = %q, want offline", got.Status)
	}
	heartbeat(t, restarted, "agent-a", "site-a")
	if got := status(t, restarted, "agent-a"); got.Status != domain.AgentStatusOnline {
		t.Fatalf("status after post-restart heartbeat = %q, want online", got.Status)
	}
}

// 6. A stale persisted record (as a cached ONLINE value would be) cannot
// restore ONLINE: status is never read from storage.
func TestAgentStatusPersistedOnlineValueIsIgnored(t *testing.T) {
	fixture := newAgentFixture(t)
	service := fixture.newProvider(t)
	register(t, service, "agent-a", "site-a")
	store, err := filesystem.NewAgentStore(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.Get("agent-a")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.AgentStatusUnknown || stored.StatusReason != "" || stored.ObservedAt != nil {
		t.Fatalf("persisted record carries an observed status: %+v", stored)
	}
	// Simulate a legacy or tampered record claiming ONLINE.
	stored.Status = domain.AgentStatusOnline
	if err := store.Update(stored); err != nil {
		t.Fatal(err)
	}
	if got := status(t, service, "agent-a"); got.Status != domain.AgentStatusUnknown {
		t.Fatalf("persisted online value was trusted: %q", got.Status)
	}
}

// 7. Re-registration after an Agent restart does not inherit the previous
// process's liveness; ONLINE needs a fresh heartbeat.
func TestAgentStatusReconnectionRequiresFreshEvidence(t *testing.T) {
	fixture := newAgentFixture(t)
	service := fixture.newProvider(t)
	register(t, service, "agent-a", "site-a")
	heartbeat(t, service, "agent-a", "site-a")
	if _, err := service.DisconnectAgentContext(t.Context(), "agent-a", "site-a"); err != nil {
		t.Fatal(err)
	}
	fixture.clock.Advance(2 * time.Second)
	if got := register(t, service, "agent-a", "site-a"); got.Status != domain.AgentStatusUnknown {
		t.Fatalf("re-registration status = %q, want unknown", got.Status)
	}
	if got := heartbeat(t, service, "agent-a", "site-a"); got.Status != domain.AgentStatusOnline {
		t.Fatalf("status after reconnection heartbeat = %q, want online", got.Status)
	}
}

// 8. Different sites and Agents remain isolated.
func TestAgentStatusSitesAreIsolated(t *testing.T) {
	fixture := newAgentFixture(t)
	service := fixture.newProvider(t)
	register(t, service, "agent-a", "site-a")
	register(t, service, "agent-b", "site-b")
	heartbeat(t, service, "agent-a", "site-a")
	fixture.clock.Advance(20 * time.Second)
	heartbeat(t, service, "agent-b", "site-b")
	fixture.clock.Advance(15 * time.Second)
	if got := status(t, service, "agent-a"); got.Status != domain.AgentStatusOffline {
		t.Fatalf("agent-a = %q, want offline", got.Status)
	}
	if got := status(t, service, "agent-b"); got.Status != domain.AgentStatusOnline {
		t.Fatalf("agent-b = %q, want online (its own fresh heartbeat)", got.Status)
	}
	if _, err := service.HeartbeatAgent("agent-a", AgentHeartbeat{SiteID: "site-b"}); err == nil {
		t.Fatal("heartbeat for agent-a claiming site-b was accepted")
	}
	if got := status(t, service, "agent-a"); got.Status != domain.AgentStatusOffline {
		t.Fatalf("rejected cross-site heartbeat changed agent-a to %q", got.Status)
	}
}

func TestAgentStatusDegradedWhenAgentReportsProblem(t *testing.T) {
	fixture := newAgentFixture(t)
	service := fixture.newProvider(t)
	register(t, service, "agent-a", "site-a")
	got, err := service.HeartbeatAgent("agent-a", AgentHeartbeat{SiteID: "site-a", Problem: "artifact run failed: 1 artifact blocked"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.AgentStatusDegraded || got.ReportedProblem == "" {
		t.Fatalf("problem heartbeat = %+v, want degraded", got)
	}
	if _, err := service.HeartbeatAgent("agent-a", AgentHeartbeat{SiteID: "site-a", Problem: "two\nlines"}); err == nil {
		t.Fatal("multi-line problem was accepted")
	}
	if got := heartbeat(t, service, "agent-a", "site-a"); got.Status != domain.AgentStatusOnline {
		t.Fatalf("clean heartbeat after problem = %q, want online", got.Status)
	}
	page, err := fixture.logs.List(observability.Query{AgentID: "agent-a", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, event := range page.Events {
		names[event.Event] = true
	}
	if !names["agent.registration.completed"] || !names["agent.heartbeat.degraded"] {
		t.Fatalf("connectivity lifecycle was not logged for diagnostics: %v", names)
	}
}
