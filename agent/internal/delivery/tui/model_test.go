package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"infraflow/agent/internal/adapters/filesystem"
	"infraflow/agent/internal/adapters/providerhttp"
	"infraflow/pkg/protocol"
)

// ─── Mock Backend ──────────────────────────────────────────────────────────────

type mockBackend struct {
	user          providerhttp.User
	session       providerhttp.UserSession
	jobs          []providerhttp.JobSummary
	agents        []providerhttp.AgentSummary
	events        []providerhttp.EventSummary
	logs          []providerhttp.TechnicalLogEntry
	executionPage providerhttp.ExecutionPage
	execution     providerhttp.Execution
	loginError    error
	jobsError     error
	agentsError   error
	eventsError   error
	logsError     error
	execsError    error
	execError     error
	cancelError   error
}

func (m *mockBackend) Login(_ context.Context, _, _ string) (providerhttp.UserSession, error) {
	return m.session, m.loginError
}
func (m *mockBackend) Logout(_ context.Context) error { return nil }
func (m *mockBackend) CurrentUser(_ context.Context) (providerhttp.User, error) {
	return m.user, nil
}
func (m *mockBackend) Jobs(_ context.Context) ([]providerhttp.JobSummary, error) {
	return m.jobs, m.jobsError
}
func (m *mockBackend) Agents(_ context.Context) ([]providerhttp.AgentSummary, error) {
	return m.agents, m.agentsError
}
func (m *mockBackend) Events(_ context.Context) ([]providerhttp.EventSummary, error) {
	return m.events, m.eventsError
}
func (m *mockBackend) Logs(_ context.Context) ([]providerhttp.TechnicalLogEntry, error) {
	return m.logs, m.logsError
}
func (m *mockBackend) Executions(_ context.Context, _, _ int, _, _ string) (providerhttp.ExecutionPage, error) {
	return m.executionPage, m.execsError
}
func (m *mockBackend) Execution(_ context.Context, _ string) (providerhttp.Execution, error) {
	return m.execution, m.execError
}
func (m *mockBackend) CancelExecution(_ context.Context, _ string) (providerhttp.Execution, error) {
	return m.execution, m.cancelError
}

func adminUser() providerhttp.User {
	return providerhttp.User{ID: "admin-id", Username: "admin", Role: "admin"}
}

func regularUser() providerhttp.User {
	return providerhttp.User{ID: "user-id", Username: "operator", Role: "user"}
}

func testSession() providerhttp.UserSession {
	return providerhttp.UserSession{Token: "test-token", ExpiresAt: time.Now().Add(time.Hour)}
}

func newTestModel(backend *mockBackend, user providerhttp.User) model {
	m := newModel(context.Background(), backend, user, testSession())
	m.width = 120
	m.height = 40
	return m
}

// ─── Tests ─────────────────────────────────────────────────────────────────────

func TestModelInitialState(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())

	if m.view != homeView {
		t.Fatalf("expected initial view %q, got %q", homeView, m.view)
	}
	if m.width != 120 || m.height != 40 {
		t.Fatalf("expected 120x40, got %dx%d", m.width, m.height)
	}
}

func TestDashboardViewRendersCorrectly(t *testing.T) {
	backend := &mockBackend{
		user: adminUser(),
		jobs: []providerhttp.JobSummary{{ID: "job-1", Status: "planned"}},
	}
	m := newTestModel(backend, adminUser())
	m.jobs = backend.jobs
	m.agents = []providerhttp.AgentSummary{}
	m.events = []providerhttp.EventSummary{}
	m.logs = []providerhttp.TechnicalLogEntry{}
	m.lastRefresh = time.Now()

	output := m.View()
	if !strings.Contains(output, "INFRAFLOW") {
		t.Fatal("dashboard view must contain INFRAFLOW brand")
	}
	if !strings.Contains(output, "admin") {
		t.Fatal("dashboard view must show user identity")
	}
	if !strings.Contains(output, "Planning jobs") {
		t.Fatal("dashboard view must show planning jobs count")
	}
	if !strings.Contains(output, "Overview") {
		t.Fatal("dashboard view must show Overview tab")
	}
}

func TestDashboardViewDoesNotContainFakeData(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())

	output := m.View()
	if strings.Contains(output, "192.168") {
		t.Fatal("dashboard must not contain fake IP addresses")
	}
	if strings.Contains(output, "100%") {
		t.Fatal("dashboard must not contain fake uptime percentages")
	}
}

func TestWindowResize(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	resized := updated.(model)
	if resized.width != 60 || resized.height != 20 {
		t.Fatalf("expected 60x20 after resize, got %dx%d", resized.width, resized.height)
	}

	// Verify it still renders with small terminal
	output := resized.View()
	if output == "" {
		t.Fatal("view must not be empty after resize")
	}
}

func TestSmallTerminalRender(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())
	m.width = 40
	m.height = 10

	output := m.View()
	if output == "" {
		t.Fatal("view must render on small terminals")
	}
}

func TestNavigationKeys(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())

	// j navigates to jobs
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if updated.(model).view != jobsView {
		t.Fatalf("expected jobs view, got %q", updated.(model).view)
	}

	// ? shows help
	m = newTestModel(backend, adminUser())
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !strings.Contains(updated.(model).notice, "help") {
		t.Fatalf("expected help notice, got %q", updated.(model).notice)
	}
}

func TestAdminOnlyNavigation(t *testing.T) {
	backend := &mockBackend{user: regularUser()}
	m := newTestModel(backend, regularUser())

	// a requires admin
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	result := updated.(model)
	if result.view != homeView {
		t.Fatalf("non-admin should stay at home, got %q", result.view)
	}
	if !strings.Contains(result.notice, "Administrator") {
		t.Fatal("non-admin should get administrator notice")
	}
}

func TestQuitKey(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q key must produce a quit command")
	}
}

func TestDashboardLoadSuccess(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())
	m.loading = true

	msg := dashboardLoadedMsg{
		requestID: 0,
		jobs:      []providerhttp.JobSummary{{ID: "j1", Status: "planned"}},
		agents:    []providerhttp.AgentSummary{{ID: "a1", Status: "online"}},
		events:    []providerhttp.EventSummary{},
		logs:      []providerhttp.TechnicalLogEntry{},
	}
	updated, _ := m.Update(msg)
	result := updated.(model)
	if result.loading {
		t.Fatal("loading should be false after dashboard load")
	}
	if result.loadError != "" {
		t.Fatalf("unexpected error: %q", result.loadError)
	}
	if len(result.jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(result.jobs))
	}
	if result.lastRefresh.IsZero() {
		t.Fatal("lastRefresh should be set after successful load")
	}
}

func TestDashboardLoadError(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())
	m.loading = true

	msg := dashboardLoadedMsg{requestID: 0, err: fmt.Errorf("connection refused")}
	updated, _ := m.Update(msg)
	result := updated.(model)
	if result.loadError == "" {
		t.Fatal("loadError should be set on failure")
	}
	if !strings.Contains(result.loadError, "connection refused") {
		t.Fatalf("error message mismatch: %q", result.loadError)
	}
}

func TestExecutionListView(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())
	m.view = executionsView
	now := time.Now()
	m.executions = []providerhttp.Execution{
		{
			ID: "exec-001", Status: "running",
			Tasks:     []providerhttp.ExecutionTask{{ID: "t1", Type: "test", Status: "running", CreatedAt: now, UpdatedAt: now, Attempts: []providerhttp.ExecutionAttempt{}}},
			CreatedAt: now, UpdatedAt: now,
			Events: []providerhttp.ExecutionEvent{},
		},
	}
	m.total = 1

	output := m.View()
	if !strings.Contains(output, "Executions") {
		t.Fatal("execution list view must contain 'Executions' heading")
	}
	if !strings.Contains(output, "exec-001") {
		t.Fatal("execution list must show execution ID")
	}
	if !strings.Contains(output, "RUNNING") {
		t.Fatal("execution list must show execution status")
	}
}

func TestExecutionListEmpty(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())
	m.view = executionsView
	m.executions = []providerhttp.Execution{}
	m.total = 0

	output := m.View()
	if !strings.Contains(output, "No executions") {
		t.Fatal("empty execution list must show 'No executions' message")
	}
}

func TestExecutionDetailView(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())
	m.view = detailView
	now := time.Now()
	m.execution = &providerhttp.Execution{
		ID:        "exec-test-detail",
		Status:    "succeeded",
		CreatedAt: now,
		UpdatedAt: now,
		Tasks: []providerhttp.ExecutionTask{
			{
				ID: "task-1", Type: "deploy", Status: "succeeded",
				CreatedAt: now, UpdatedAt: now,
				Attempts: []providerhttp.ExecutionAttempt{
					{Number: 1, Status: "succeeded", StartedAt: now, DurationMS: 1500},
				},
				Result: json.RawMessage(`{"output":"done"}`),
			},
		},
		Events: []providerhttp.ExecutionEvent{
			{ActorID: "system", Action: "start", Status: "running", Timestamp: now},
			{ActorID: "system", Action: "complete", Status: "succeeded", Timestamp: now},
		},
	}

	output := m.View()
	if !strings.Contains(output, "exec-test-detail") {
		t.Fatal("detail view must show execution ID")
	}
	if !strings.Contains(output, "SUCCEEDED") {
		t.Fatal("detail view must show execution status")
	}
	if !strings.Contains(output, "task-1") {
		t.Fatal("detail view must show task ID")
	}
	if !strings.Contains(output, "attempt 1") {
		t.Fatal("detail view must show attempt number")
	}
}

func TestExecutionDetailWithError(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())
	m.view = detailView
	now := time.Now()
	m.execution = &providerhttp.Execution{
		ID:        "exec-error",
		Status:    "failed",
		Error:     "execution timed out",
		CreatedAt: now,
		UpdatedAt: now,
		Tasks:     []providerhttp.ExecutionTask{},
		Events:    []providerhttp.ExecutionEvent{},
	}

	output := m.View()
	if !strings.Contains(output, "Error:") {
		t.Fatal("detail view must show error label")
	}
	if !strings.Contains(output, "timed out") {
		t.Fatal("detail view must show error message")
	}
}

func TestExecutionCancelConfirmation(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())
	m.view = detailView
	now := time.Now()
	m.execution = &providerhttp.Execution{
		ID: "exec-running", Status: "running",
		CreatedAt: now, UpdatedAt: now,
		Tasks: []providerhttp.ExecutionTask{}, Events: []providerhttp.ExecutionEvent{},
	}

	// Press c to request cancel
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	result := updated.(model)
	if !result.confirmCancel {
		t.Fatal("pressing c should set confirmCancel")
	}

	// Press n to abandon
	updated, _ = result.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	result = updated.(model)
	if result.confirmCancel {
		t.Fatal("pressing n should clear confirmCancel")
	}
}

func TestExecutionCancelNotAvailableForTerminal(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())
	m.view = detailView
	now := time.Now()
	m.execution = &providerhttp.Execution{
		ID: "exec-done", Status: "succeeded",
		CreatedAt: now, UpdatedAt: now,
		Tasks: []providerhttp.ExecutionTask{}, Events: []providerhttp.ExecutionEvent{},
	}

	// c should not activate cancel for terminal status
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	result := updated.(model)
	if result.confirmCancel {
		t.Fatal("cancel should not be available for terminal status")
	}
}

func TestExecutionFilterCycle(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())
	m.view = executionsView

	// Press f to cycle filter
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	result := updated.(model)
	if result.filterIndex != 1 {
		t.Fatalf("expected filter index 1, got %d", result.filterIndex)
	}
	if result.statusFilter != "queued" {
		t.Fatalf("expected filter 'queued', got %q", result.statusFilter)
	}
}

func TestDetailViewBackNavigation(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())
	m.view = detailView

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	result := updated.(model)
	if result.view != executionsView {
		t.Fatalf("esc from detail should go to executions, got %q", result.view)
	}
}

func TestStaleRequestIgnored(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())
	m.requestID = 5

	// Message with old requestID should be ignored
	msg := dashboardLoadedMsg{requestID: 3, jobs: []providerhttp.JobSummary{{ID: "stale"}}}
	updated, _ := m.Update(msg)
	result := updated.(model)
	if len(result.jobs) > 0 {
		t.Fatal("stale request data should be ignored")
	}
}

func TestNoSecretsInViews(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())
	m.session = providerhttp.UserSession{Token: "secret-bearer-token-xyz", ExpiresAt: time.Now().Add(time.Hour)}

	output := m.View()
	if strings.Contains(output, "secret-bearer-token-xyz") {
		t.Fatal("view must not expose session tokens")
	}
}

func TestHelpTextPerView(t *testing.T) {
	backend := &mockBackend{user: adminUser()}

	tests := []struct {
		view     viewName
		contains string
	}{
		{homeView, "refresh"},
		{executionsView, "filter"},
		{detailView, "scroll"},
	}
	for _, test := range tests {
		m := newTestModel(backend, adminUser())
		m.view = test.view
		help := m.helpText()
		if !strings.Contains(help, test.contains) {
			t.Fatalf("help for %q should contain %q, got %q", test.view, test.contains, help)
		}
	}
}

func TestTabBarRender(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newTestModel(backend, adminUser())

	bar := m.renderTabBar(80)
	if !strings.Contains(bar, "Overview") {
		t.Fatal("tab bar must contain Overview")
	}
	if !strings.Contains(bar, "Execs") {
		t.Fatal("admin tab bar must contain Execs")
	}

	// Non-admin should not see Execs
	m2 := newTestModel(backend, regularUser())
	bar2 := m2.renderTabBar(80)
	if strings.Contains(bar2, "Execs") {
		t.Fatal("non-admin tab bar must not contain Execs")
	}
}

func TestRenderStatusColors(t *testing.T) {
	statuses := []string{"queued", "running", "succeeded", "failed", "cancelled", "timed_out", "unknown"}
	for _, status := range statuses {
		result := renderStatus(status)
		if result == "" {
			t.Fatalf("renderStatus(%q) should not return empty", status)
		}
		if !strings.Contains(result, strings.ToUpper(status)) {
			t.Fatalf("renderStatus(%q) should contain %q, got %q", status, strings.ToUpper(status), result)
		}
	}
}

func TestIsTerminalStatus(t *testing.T) {
	terminal := []string{"succeeded", "failed", "cancelled", "timed_out"}
	nonTerminal := []string{"queued", "running", "retrying"}

	for _, s := range terminal {
		if !isTerminalStatus(s) {
			t.Fatalf("%q should be terminal", s)
		}
	}
	for _, s := range nonTerminal {
		if isTerminalStatus(s) {
			t.Fatalf("%q should not be terminal", s)
		}
	}
}

func TestTruncateString(t *testing.T) {
	if truncateString("short", 10) != "short" {
		t.Fatal("short string should not be truncated")
	}
	result := truncateString("a-very-long-execution-id-that-exceeds", 20)
	if len(result) > 20 {
		t.Fatalf("truncated string should be at most 20 chars, got %d", len(result))
	}
}

func TestFormatExecutionTime(t *testing.T) {
	zero := formatExecutionTime(time.Time{})
	if zero != "—" {
		t.Fatalf("zero time should render as dash, got %q", zero)
	}
	now := formatExecutionTime(time.Now())
	if now == "—" || now == "" {
		t.Fatal("non-zero time should render a timestamp")
	}
}

func TestAgentInfoRenderingConfigured(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	info := AgentInfo{
		ID:              "agent-edge-01",
		SiteID:          "datacenter-a",
		StateDirectory:  "/var/lib/infraflow-agent",
		LogDirectory:    "/var/log/infraflow",
		Configured:      true,
		ProviderAddress: "https://provider.internal:8080",
	}
	m := newModel(context.Background(), backend, adminUser(), testSession(), info)
	m.width = 120
	m.height = 40

	output := m.View()
	if !strings.Contains(output, "agent-edge-01") {
		t.Fatal("configured agent ID must be visible in overview")
	}
	if !strings.Contains(output, "datacenter-a") {
		t.Fatal("configured site ID must be visible in overview")
	}
	if !strings.Contains(output, "/var/lib/infraflow-agent") {
		t.Fatal("configured state directory must be visible in overview")
	}
	if !strings.Contains(output, "Local Agent Runtime") {
		t.Fatal("overview must contain Local Agent Runtime section")
	}
	if !strings.Contains(output, "TUI Process") || !strings.Contains(output, "PID") {
		t.Fatal("overview must display local process PID under TUI Process")
	}
	if !strings.Contains(output, "Agent Daemon") || !strings.Contains(output, "Non vérifié") {
		t.Fatal("overview must display unverified agent daemon state")
	}
}

func TestAgentInfoRenderingUnconfigured(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newModel(context.Background(), backend, adminUser(), testSession())
	m.width = 120
	m.height = 40

	output := m.View()
	if !strings.Contains(output, "Non configuré") {
		t.Fatal("unconfigured agent must display 'Non configuré'")
	}
}

func TestServicesViewRendering(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	info := AgentInfo{
		ID:             "agent-edge-01",
		StateDirectory: "/var/lib/infraflow-agent",
		LogDirectory:   "/var/log/infraflow",
		Configured:     true,
	}
	m := newModel(context.Background(), backend, adminUser(), testSession(), info)
	m.width = 120
	m.height = 40
	m.view = servicesView

	output := m.View()
	if !strings.Contains(output, "Local Supervised Services") {
		t.Fatal("services view must display title")
	}
	if !strings.Contains(output, "Artifact Repository") {
		t.Fatal("services view must describe Artifact Repository")
	}
	if !strings.Contains(output, "Local Log Outbox") {
		t.Fatal("services view must describe Local Log Outbox")
	}
	if !strings.Contains(output, "Tool Runner") {
		t.Fatal("services view must describe Tool Runner")
	}
	if !strings.Contains(output, "DHCPv4") || !strings.Contains(output, "TFTP") {
		t.Fatal("services view must describe bootstrap services")
	}
	if !strings.Contains(output, "DNS") {
		t.Fatal("services view must explicitly state absence of DNS daemon")
	}
}

func TestOfflineResilienceAndCachedData(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newModel(context.Background(), backend, adminUser(), testSession())
	m.width = 120
	m.height = 40
	m.jobs = []providerhttp.JobSummary{{ID: "cached-job-1", Status: "planned"}}
	m.lastRefresh = time.Now().Add(-5 * time.Minute)
	m.loadError = "Provider connection failed: connection refused"

	output := m.View()
	if !strings.Contains(output, "Dégradé") {
		t.Fatal("offline view must indicate degraded state")
	}
	if !strings.Contains(output, "Données en mémoire de session") {
		t.Fatal("offline view must clarify session memory data")
	}
	if !strings.Contains(output, "Planning jobs") {
		t.Fatal("cached job data must remain visible during disconnect")
	}
	if !strings.Contains(output, "Dernière synchro:") || !strings.Contains(output, m.lastRefresh.Local().Format("15:04:05")) {
		t.Fatal("offline view must display last successful refresh timestamp")
	}

	// TUI must remain navigable even with load error
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if updated.(model).view != servicesView {
		t.Fatalf("expected services view, got %q", updated.(model).view)
	}
}

func TestDedicatedListViews(t *testing.T) {
	backend := &mockBackend{
		user:   adminUser(),
		jobs:   []providerhttp.JobSummary{{ID: "job-001", Status: "planned"}},
		agents: []providerhttp.AgentSummary{{ID: "agent-001", SiteID: "site-a", Status: "online", QueueDepth: 2}},
		events: []providerhttp.EventSummary{{EventID: "evt-001", Type: "audit.run", JobID: "job-001"}},
		logs:   []providerhttp.TechnicalLogEntry{{ID: "log-001", Level: "INFO", Service: "agent", Message: "process started"}},
	}
	m := newModel(context.Background(), backend, adminUser(), testSession())
	m.width = 120
	m.height = 40
	m.jobs = backend.jobs
	m.agents = backend.agents
	m.events = backend.events
	m.logs = backend.logs

	// Jobs view
	m.view = jobsView
	jobsOut := m.View()
	if !strings.Contains(jobsOut, "job-001") || !strings.Contains(jobsOut, "JOB ID") {
		t.Fatal("jobs view must display job list")
	}

	// Agents view
	m.view = agentsView
	agentsOut := m.View()
	if !strings.Contains(agentsOut, "agent-001") || !strings.Contains(agentsOut, "AGENT ID") {
		t.Fatal("agents view must display agent list")
	}

	// Events view
	m.view = eventsView
	eventsOut := m.View()
	if !strings.Contains(eventsOut, "evt-001") || !strings.Contains(eventsOut, "EVENT ID") {
		t.Fatal("events view must display event list")
	}

	// Logs view
	m.view = logsView
	logsOut := m.View()
	if !strings.Contains(logsOut, "process started") || !strings.Contains(logsOut, "SERVICE") {
		t.Fatal("logs view must display log entries")
	}
}

func TestNewNavigationShortcuts(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newModel(context.Background(), backend, adminUser(), testSession())

	// 's' navigates to services
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if updated.(model).view != servicesView {
		t.Fatalf("expected services view, got %q", updated.(model).view)
	}

	// 'o' navigates back to overview
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if updated.(model).view != homeView {
		t.Fatalf("expected home view, got %q", updated.(model).view)
	}
}

func TestArtifactStoreInspectionScenarios(t *testing.T) {
	// 1. Unconfigured
	unconf := inspectArtifactStore("")
	if unconf.status != "Non configuré" {
		t.Fatalf("expected 'Non configuré', got %q", unconf.status)
	}
	if !strings.Contains(unconf.integrity, "Non vérifiée") {
		t.Fatalf("unconfigured integrity must be unverified: %q", unconf.integrity)
	}

	// 2. Absent directory
	absentDir := filepath.Join(t.TempDir(), "nonexistent-dir")
	abs := inspectArtifactStore(absentDir)
	if abs.status != "Absent" {
		t.Fatalf("expected 'Absent', got %q", abs.status)
	}
	if !strings.Contains(abs.integrity, "Non vérifiée") {
		t.Fatalf("absent integrity must be unverified: %q", abs.integrity)
	}
	if !strings.Contains(abs.detail, "non initialisé sur le disque") {
		t.Fatalf("absent detail must clarify non-initialized on disk: %q", abs.detail)
	}

	// 3. Accessible empty directory (read-only verified, write capability not verified)
	emptyDir := t.TempDir()
	opEmpty := inspectArtifactStore(emptyDir)
	if opEmpty.status != "Accessible" {
		t.Fatalf("expected 'Accessible' empty directory, got %q", opEmpty.status)
	}
	if opEmpty.status == "Opérationnel" {
		t.Fatal("empty directory must not be qualified as Opérationnel")
	}
	if !strings.Contains(opEmpty.detail, "0 entrée stockée") {
		t.Fatalf("expected 0 entries in detail, got %q", opEmpty.detail)
	}
	if !strings.Contains(opEmpty.detail, "capacité d'écriture non vérifiée") {
		t.Fatalf("detail must explicitly indicate unverified write capability: %q", opEmpty.detail)
	}
	if !strings.Contains(opEmpty.detail, "Répertoire accessible en lecture") {
		t.Fatalf("detail must state read-accessible directory: %q", opEmpty.detail)
	}
	if !strings.Contains(opEmpty.integrity, "Non auditée par le TUI") {
		t.Fatalf("integrity must clarify TUI does not re-audit hash: %q", opEmpty.integrity)
	}

	// 4. Accessible with entries (still Accessible, never Opérationnel without write check)
	if err := os.WriteFile(filepath.Join(emptyDir, "file1.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(emptyDir, "file2.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	opPop := inspectArtifactStore(emptyDir)
	if opPop.status != "Accessible" {
		t.Fatalf("expected 'Accessible' with 2 entries, got %q", opPop.status)
	}
	if opPop.status == "Opérationnel" {
		t.Fatal("populated directory must not be qualified as Opérationnel")
	}
	if !strings.Contains(opPop.detail, "2 entrées stockées") {
		t.Fatalf("expected 2 entries in detail, got %q", opPop.detail)
	}
	if !strings.Contains(opPop.detail, "capacité d'écriture non vérifiée") {
		t.Fatalf("detail must explicitly indicate unverified write capability: %q", opPop.detail)
	}

	// 5. Inaccessible (non-readable) directory
	if os.Geteuid() != 0 {
		noReadDir := filepath.Join(t.TempDir(), "locked")
		if err := os.Mkdir(noReadDir, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(noReadDir, 0o700) })
		inaccessible := inspectArtifactStore(noReadDir)
		if inaccessible.status != "Inaccessible" {
			t.Fatalf("expected 'Inaccessible' for locked directory, got %q", inaccessible.status)
		}
	} else {
		t.Log("running as root; skipping directory permission denial scenario as root bypasses 0000 mode")
	}
}

func TestArtifactStorePathIsAFile(t *testing.T) {
	tempFile := filepath.Join(t.TempDir(), "regular-file.txt")
	if err := os.WriteFile(tempFile, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	insp := inspectArtifactStore(tempFile)
	if insp.status != "Dégradé" || !strings.Contains(insp.detail, "pointe vers un fichier") {
		t.Fatalf("expected Dégradé for regular file path, got %#v", insp)
	}
}

func TestArtifactStoreRealAdapterIntegration(t *testing.T) {
	stateDir := t.TempDir()
	store := filesystem.NewStateStore(stateDir)

	// Initially empty directory
	insp := inspectArtifactStore(stateDir)
	if insp.status != "Accessible" || !strings.Contains(insp.detail, "0 entrée") {
		t.Fatalf("expected initial empty directory to be accessible with 0 entries, got %#v", insp)
	}
	if insp.status == "Opérationnel" {
		t.Fatal("initial state store directory must not be qualified as Opérationnel")
	}
	if !strings.Contains(insp.detail, "capacité d'écriture non vérifiée") {
		t.Fatalf("expected unverified write capability in detail, got %#v", insp)
	}

	// Save an artifact via the real filesystem StateStore
	data := []byte(`{"site":"lab","devices":[]}`)
	hash := protocol.SHA256(data)
	if err := store.SaveArtifact("lab/inventory.json", hash, bytes.NewReader(data)); err != nil {
		t.Fatalf("failed to save artifact: %v", err)
	}

	// Verify that inspectArtifactStore directly observes the store.root directory
	inspAfter := inspectArtifactStore(stateDir)
	if inspAfter.status != "Accessible" || !strings.Contains(inspAfter.detail, "1 entrée") {
		t.Fatalf("expected 1 entry (lab directory) in state store, got %#v", inspAfter)
	}
	if inspAfter.status == "Opérationnel" {
		t.Fatal("state store with artifact must not be qualified as Opérationnel")
	}
	if !strings.Contains(inspAfter.detail, "capacité d'écriture non vérifiée") {
		t.Fatalf("expected unverified write capability in detail, got %#v", inspAfter)
	}

	// Save a report via the real StateStore
	if err := store.SaveReport(protocol.AgentReport{AgentID: "agent-1"}); err != nil {
		t.Fatalf("failed to save report: %v", err)
	}
	inspWithReport := inspectArtifactStore(stateDir)
	if inspWithReport.status != "Accessible" || !strings.Contains(inspWithReport.detail, "2 entrées") {
		t.Fatalf("expected 2 entries in state store, got %#v", inspWithReport)
	}
	if inspWithReport.status == "Opérationnel" {
		t.Fatal("state store with report must not be qualified as Opérationnel")
	}
	if !strings.Contains(inspWithReport.detail, "capacité d'écriture non vérifiée") {
		t.Fatalf("expected unverified write capability in detail, got %#v", inspWithReport)
	}
}

func TestArtifactStoreViewsSummaryAndDetail(t *testing.T) {
	stateDir := t.TempDir()
	backend := &mockBackend{user: adminUser()}
	m := newModel(context.Background(), backend, adminUser(), testSession())
	m.agentInfo.StateDirectory = stateDir
	m.width = 120
	m.height = 40

	// 1. Summary view verification
	summary := m.artifactStoreSummary()
	if !strings.Contains(summary, "Accessible") {
		t.Fatalf("summary must display Accessible, got %q", summary)
	}
	if strings.Contains(summary, "Opérationnel") {
		t.Fatalf("summary must NOT display Opérationnel, got %q", summary)
	}
	if !strings.Contains(summary, "capacité d'écriture non vérifiée") {
		t.Fatalf("summary must specify unverified write capability, got %q", summary)
	}

	// 2. Overview home view verification
	homeOut := m.View()
	if !strings.Contains(homeOut, "Artifact Store:") || !strings.Contains(homeOut, "Accessible") {
		t.Fatalf("home view must display Accessible artifact store: %s", homeOut)
	}

	// 3. Services detailed view verification
	m.view = servicesView
	servicesOut := m.View()
	if !strings.Contains(servicesOut, "1. Artifact Repository & Verification") {
		t.Fatalf("services view must contain artifact repository section: %s", servicesOut)
	}
	if !strings.Contains(servicesOut, "Accessible") {
		t.Fatalf("services view must display Accessible badge: %s", servicesOut)
	}
	if !strings.Contains(servicesOut, "Répertoire accessible en lecture") {
		t.Fatalf("services view must indicate read-accessible directory: %s", servicesOut)
	}
	if !strings.Contains(servicesOut, "capacité d'écriture non vérifiée") {
		t.Fatalf("services view must indicate unverified write capability: %s", servicesOut)
	}

	// 4. Absent directory in summary
	m.agentInfo.StateDirectory = filepath.Join(t.TempDir(), "absent-dir")
	absSummary := m.artifactStoreSummary()
	if !strings.Contains(absSummary, "Absent") {
		t.Fatalf("expected Absent in summary for absent directory, got %q", absSummary)
	}

	// 5. File path in summary
	filePath := filepath.Join(t.TempDir(), "dummy.txt")
	if err := os.WriteFile(filePath, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.agentInfo.StateDirectory = filePath
	fileSummary := m.artifactStoreSummary()
	if !strings.Contains(fileSummary, "Dégradé") {
		t.Fatalf("expected Dégradé in summary for regular file, got %q", fileSummary)
	}
}

func TestLogOutboxInspectionScenarios(t *testing.T) {
	// 1. Unconfigured
	unconf := inspectLogOutbox("")
	if unconf.status != "Non configuré" {
		t.Fatalf("expected 'Non configuré', got %q", unconf.status)
	}

	// 2. Absent before initialization -> Non initialisé
	logDir := t.TempDir()
	abs := inspectLogOutbox(logDir)
	if abs.status != "Non initialisé" {
		t.Fatalf("expected 'Non initialisé' for uncreated outbox file, got %q", abs.status)
	}
	if !strings.Contains(abs.detail, "non créé") {
		t.Fatalf("expected uncreated file detail, got %q", abs.detail)
	}

	// 3. Empty outbox file
	outboxPath := filepath.Join(logDir, "pending-events.json")
	if err := os.WriteFile(outboxPath, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	empty := inspectLogOutbox(logDir)
	if empty.status != "Opérationnel" || !strings.Contains(empty.detail, "vide") {
		t.Fatalf("expected operational empty file, got %#v", empty)
	}

	// 4. Populated outbox file with valid JSON
	eventsJSON := `[{"event":"agent.test","level":"INFO"},{"event":"agent.metric","level":"DEBUG"}]`
	if err := os.WriteFile(outboxPath, []byte(eventsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	populated := inspectLogOutbox(logDir)
	if populated.status != "Opérationnel" || !strings.Contains(populated.detail, "2 événement(s)") {
		t.Fatalf("expected operational with 2 events, got %#v", populated)
	}

	// 5. Corrupted JSON file
	if err := os.WriteFile(outboxPath, []byte("invalid-json-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	corrupt := inspectLogOutbox(logDir)
	if corrupt.status != "Dégradé" || !strings.Contains(corrupt.detail, "Format JSON invalide") {
		t.Fatalf("expected degraded for corrupt JSON, got %#v", corrupt)
	}

	// 6. Inaccessible file
	if os.Geteuid() != 0 {
		if err := os.Chmod(outboxPath, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(outboxPath, 0o600) })
		inaccessible := inspectLogOutbox(logDir)
		if inaccessible.status != "Inaccessible" {
			t.Fatalf("expected 'Inaccessible', got %q", inaccessible.status)
		}
	} else {
		t.Log("running as root; skipping file permission denial scenario")
	}
}

func TestToolRunnerInspectionScenarios(t *testing.T) {
	// 1. Isolated empty PATH -> Absent
	emptyBinDir := t.TempDir()
	t.Setenv("PATH", emptyBinDir)
	abs := inspectToolRunner()
	if abs.status != "Absent" {
		t.Fatalf("expected Absent with empty PATH, got %q", abs.status)
	}
	if !strings.Contains(abs.detail, "introuvables dans PATH") {
		t.Fatalf("expected missing tools detail, got %q", abs.detail)
	}

	// 2. Only ansible-playbook present -> Dégradé
	ansibleBin := filepath.Join(emptyBinDir, "ansible-playbook")
	if err := os.WriteFile(ansibleBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	deg := inspectToolRunner()
	if deg.status != "Dégradé" {
		t.Fatalf("expected Dégradé with only ansible, got %q", deg.status)
	}
	if !strings.Contains(deg.detail, "terraform") {
		t.Fatalf("expected missing terraform detail, got %q", deg.detail)
	}

	// 3. Both ansible-playbook and terraform present in PATH -> Détecté (non vérifié)
	terraformBin := filepath.Join(emptyBinDir, "terraform")
	if err := os.WriteFile(terraformBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	op := inspectToolRunner()
	if op.status != "Détecté" {
		t.Fatalf("expected 'Détecté' with both tools in PATH, got %q", op.status)
	}
	if !strings.Contains(op.detail, "fonctionnement non vérifié") {
		t.Fatalf("expected unverified execution detail, got %q", op.detail)
	}
}

func TestToolRunnerLabelCorrespondence(t *testing.T) {
	emptyBinDir := t.TempDir()
	t.Setenv("PATH", emptyBinDir)

	m := newModel(context.Background(), &mockBackend{user: adminUser()}, adminUser(), testSession())
	m.width = 120
	m.height = 40

	// Absent tools
	summaryAbsent := m.toolRunnerSummary()
	if !strings.Contains(summaryAbsent, "Absent") {
		t.Fatalf("summary should display Absent, got %q", summaryAbsent)
	}

	// Detected tools
	if err := os.WriteFile(filepath.Join(emptyBinDir, "ansible-playbook"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(emptyBinDir, "terraform"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	summaryDetected := m.toolRunnerSummary()
	if !strings.Contains(summaryDetected, "Détecté") || !strings.Contains(summaryDetected, "non vérifié") {
		t.Fatalf("summary must display Détecté and unverified functioning, got %q", summaryDetected)
	}
}

func TestOfflineLifecycleFullSequence(t *testing.T) {
	backend := &mockBackend{user: adminUser()}
	m := newModel(context.Background(), backend, adminUser(), testSession())
	m.width = 120
	m.height = 40

	// 1. Initial state: Aucune donnée reçue, provider en échec
	m.loadError = "connect: connection refused"
	v1 := m.View()
	if !strings.Contains(v1, "Indisponible (aucune donnée reçue)") {
		t.Fatalf("step 1: expected unavailable with no data received, got:\n%s", v1)
	}
	if !strings.Contains(v1, "Aucune synchronisation") {
		t.Fatalf("step 1: expected no sync timestamp, got:\n%s", v1)
	}
	if !strings.Contains(v1, "● Dégradé (Provider indisponible)") {
		t.Fatalf("step 1: expected degraded status, got:\n%s", v1)
	}

	// 2. Première actualisation réussie
	jobs := []providerhttp.JobSummary{{ID: "job-initial-1", Status: "planned"}}
	updated, _ := m.Update(dashboardLoadedMsg{
		requestID: m.requestID,
		jobs:      jobs,
		err:       nil,
	})
	m = updated.(model)
	v2 := m.View()
	if !strings.Contains(v2, "● Opérationnel") {
		t.Fatalf("step 2: expected operational after sync, got:\n%s", v2)
	}
	if !strings.Contains(v2, "Planning jobs:") {
		t.Fatalf("step 2: expected planning jobs metric")
	}
	// Verify jobs list view shows the job
	m.view = jobsView
	if !strings.Contains(m.View(), "job-initial-1") {
		t.Fatalf("step 2: expected job-initial-1 in jobs list view")
	}
	m.view = homeView

	t1 := m.lastRefresh.Local().Format("15:04:05")
	if !strings.Contains(v2, t1) {
		t.Fatalf("step 2: expected refresh timestamp %s in view", t1)
	}
	if !strings.Contains(v2, "Synchronisé") {
		t.Fatalf("step 2: expected Synchronisé badge")
	}

	// 3. Échec d'actualisation après réussite (perte de connexion)
	time.Sleep(10 * time.Millisecond)
	updated, _ = m.Update(dashboardLoadedMsg{
		requestID: m.requestID,
		err:       fmt.Errorf("temporary network failure: connection reset"),
	})
	m = updated.(model)
	v3 := m.View()
	// Data must be preserved in memory
	if len(m.jobs) != 1 || m.jobs[0].ID != "job-initial-1" {
		t.Fatalf("step 3: cached jobs must remain in session memory during failure")
	}
	m.view = jobsView
	if !strings.Contains(m.View(), "job-initial-1") {
		t.Fatalf("step 3: cached job-initial-1 must remain visible in jobs list during disconnect")
	}
	m.view = homeView

	// Status must state session memory
	if !strings.Contains(v3, "Données en mémoire de session (non persistées sur disque)") {
		t.Fatalf("step 3: expected session memory label without claiming disk persistence, got:\n%s", v3)
	}
	// Must retain the previous successful refresh timestamp t1, not a newly generated one
	if !strings.Contains(v3, t1) {
		t.Fatalf("step 3: must retain previous successful refresh timestamp %s, got:\n%s", t1, v3)
	}
	if !strings.Contains(v3, "Indisponible") {
		t.Fatalf("step 3: connection badge must be Indisponible")
	}

	// 4. Nouvelle réussite après reconnexion
	updatedJobs := []providerhttp.JobSummary{
		{ID: "job-initial-1", Status: "completed"},
		{ID: "job-new-2", Status: "running"},
	}
	updated, _ = m.Update(dashboardLoadedMsg{
		requestID: m.requestID,
		jobs:      updatedJobs,
		err:       nil,
	})
	m = updated.(model)
	v4 := m.View()
	if !strings.Contains(v4, "● Opérationnel") {
		t.Fatalf("step 4: expected operational after reconnection, got:\n%s", v4)
	}
	m.view = jobsView
	if !strings.Contains(m.View(), "job-new-2") {
		t.Fatalf("step 4: expected job-new-2 to be present in jobs view")
	}
	m.view = homeView

	if !strings.Contains(v4, "Synchronisé") {
		t.Fatalf("step 4: expected Synchronisé badge")
	}
	if m.loadError != "" {
		t.Fatalf("step 4: loadError must be cleared, got %q", m.loadError)
	}
}
