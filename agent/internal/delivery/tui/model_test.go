package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"infraflow/agent/internal/adapters/providerhttp"
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
