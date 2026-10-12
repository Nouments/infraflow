package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"infraflow/agent/internal/adapters/providerhttp"
	"infraflow/pkg/observability"
)

const executionPageSize = 10

// ─── Styles ────────────────────────────────────────────────────────────────────

var (
	// Color palette
	colorAccent    = lipgloss.Color("39")
	colorGreen     = lipgloss.Color("42")
	colorOrange    = lipgloss.Color("214")
	colorRed       = lipgloss.Color("196")
	colorMagenta   = lipgloss.Color("201")
	colorDim       = lipgloss.Color("245")
	colorBorder    = lipgloss.Color("236")
	colorHighlight = lipgloss.Color("234")

	// Base styles
	brandStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("255")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	sectionStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	labelStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255"))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorGreen)

	quietStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorRed)

	queuedStyle    = lipgloss.NewStyle().Foreground(colorOrange)
	runningStyle   = lipgloss.NewStyle().Foreground(colorAccent)
	successStyle   = lipgloss.NewStyle().Foreground(colorGreen)
	failedStyle    = lipgloss.NewStyle().Foreground(colorRed)
	cancelledStyle = lipgloss.NewStyle().Foreground(colorDim)
	timedOutStyle  = lipgloss.NewStyle().Foreground(colorMagenta)

	panelBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("255")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(colorDim).
				Padding(0, 1)

	helpBarStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			Background(lipgloss.Color("233")).
			Padding(0, 1)

	keyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	dividerChar = lipgloss.NewStyle().
			Foreground(colorBorder).
			Render("─")
)

type viewName string

const (
	homeView       viewName = "home"
	jobsView       viewName = "jobs"
	agentsView     viewName = "agents"
	eventsView     viewName = "events"
	logsView       viewName = "logs"
	executionsView viewName = "executions"
	detailView     viewName = "execution-detail"
)

type model struct {
	ctx            context.Context
	backend        Backend
	user           providerhttp.User
	session        providerhttp.UserSession
	view           viewName
	previousView   viewName
	width          int
	height         int
	loading        bool
	loadError      string
	notice         string
	jobs           []providerhttp.JobSummary
	agents         []providerhttp.AgentSummary
	events         []providerhttp.EventSummary
	logs           []providerhttp.TechnicalLogEntry
	executions     []providerhttp.Execution
	execution      *providerhttp.Execution
	selected       int
	offset         int
	total          int
	statusFilter   string
	filterIndex    int
	confirmCancel  bool
	cancelInFlight bool
	detailScroll   int
	requestID      uint64
	lastRefresh    time.Time
}

type dashboardLoadedMsg struct {
	requestID uint64
	jobs      []providerhttp.JobSummary
	agents    []providerhttp.AgentSummary
	events    []providerhttp.EventSummary
	logs      []providerhttp.TechnicalLogEntry
	err       error
}

type executionsLoadedMsg struct {
	requestID uint64
	page      providerhttp.ExecutionPage
	err       error
}

type executionLoadedMsg struct {
	requestID uint64
	execution providerhttp.Execution
	err       error
}

type executionCancelledMsg struct {
	requestID uint64
	execution providerhttp.Execution
	err       error
}

type refreshTickMsg time.Time

type Backend interface {
	Login(context.Context, string, string) (providerhttp.UserSession, error)
	Logout(context.Context) error
	CurrentUser(context.Context) (providerhttp.User, error)
	Jobs(context.Context) ([]providerhttp.JobSummary, error)
	Agents(context.Context) ([]providerhttp.AgentSummary, error)
	Events(context.Context) ([]providerhttp.EventSummary, error)
	Logs(context.Context) ([]providerhttp.TechnicalLogEntry, error)
	Executions(context.Context, int, int, string, string) (providerhttp.ExecutionPage, error)
	Execution(context.Context, string) (providerhttp.Execution, error)
	CancelExecution(context.Context, string) (providerhttp.Execution, error)
}

func newModel(ctx context.Context, backend Backend, user providerhttp.User, session providerhttp.UserSession) model {
	return model{ctx: ctx, backend: backend, user: user, session: session, view: homeView, width: 80, height: 24}
}

func (instance model) Init() tea.Cmd {
	return loadDashboard(instance)
}

func (instance model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := message.(type) {
	case tea.WindowSizeMsg:
		instance.width, instance.height = typed.Width, typed.Height
		return instance, nil
	case dashboardLoadedMsg:
		if typed.requestID != instance.requestID {
			return instance, nil
		}
		instance.loading = false
		instance.lastRefresh = time.Now()
		if typed.err != nil {
			instance.loadError = displayAPIError(typed.err)
		} else {
			instance.loadError = ""
			instance.jobs, instance.agents, instance.events, instance.logs = typed.jobs, typed.agents, typed.events, typed.logs
		}
		return instance, nil
	case executionsLoadedMsg:
		if typed.requestID != instance.requestID {
			return instance, nil
		}
		instance.loading = false
		instance.lastRefresh = time.Now()
		if typed.err != nil {
			instance.loadError = displayAPIError(typed.err)
		} else {
			instance.loadError = ""
			instance.executions, instance.total = typed.page.Items, typed.page.Total
			if instance.selected >= len(instance.executions) {
				instance.selected = maxInt(0, len(instance.executions)-1)
			}
		}
		return instance, nil
	case executionLoadedMsg:
		if typed.requestID != instance.requestID {
			return instance, nil
		}
		instance.loading = false
		instance.lastRefresh = time.Now()
		if typed.err != nil {
			instance.loadError = displayAPIError(typed.err)
		} else {
			instance.loadError = ""
			instance.execution = &typed.execution
		}
		if instance.view == detailView && instance.loadError == "" && instance.executionActive() {
			return instance, refreshAfter(executionRefreshInterval)
		}
		return instance, nil
	case executionCancelledMsg:
		if typed.requestID != instance.requestID {
			return instance, nil
		}
		instance.loading = false
		instance.cancelInFlight = false
		instance.confirmCancel = false
		if typed.err != nil {
			instance.loadError = displayAPIError(typed.err)
			return instance, nil
		}
		instance.loadError = ""
		instance.execution = &typed.execution
		if typed.execution.CancelRequested && !isTerminalStatus(typed.execution.Status) {
			instance.notice = "Cancellation request accepted; waiting for the Provider's final state."
		} else {
			instance.notice = "Provider returned the current execution state."
		}
		if instance.executionActive() {
			return instance, refreshAfter(executionRefreshInterval)
		}
		return instance, nil
	case refreshTickMsg:
		if instance.view == detailView && instance.executionActive() && !instance.loading {
			instance.loading = true
			instance.requestID++
			return instance, loadExecution(instance, instance.execution.ID, instance.requestID)
		}
		return instance, nil
	case tea.KeyMsg:
		return instance.updateKey(typed)
	}
	return instance, nil
}

const executionRefreshInterval = 2 * time.Second

func (instance model) updateKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" || key.String() == "q" {
		return instance, tea.Quit
	}
	if instance.confirmCancel {
		switch key.String() {
		case "y":
			if instance.cancelInFlight || instance.execution == nil {
				return instance, nil
			}
			instance.cancelInFlight = true
			instance.loading = true
			instance.requestID++
			return instance, cancelExecution(instance, instance.execution.ID, instance.requestID)
		case "n", "esc", "backspace":
			instance.confirmCancel = false
			instance.notice = "Cancellation request abandoned."
		}
		return instance, nil
	}
	if instance.view == detailView {
		switch key.String() {
		case "esc", "backspace":
			instance.requestID++
			instance.view = executionsView
			instance.execution = nil
			instance.loadError = ""
			instance.loading = true
			return instance, loadExecutions(instance, instance.requestID)
		case "up", "k":
			if instance.detailScroll > 0 {
				instance.detailScroll--
			}
		case "down", "j":
			instance.detailScroll++
		case "r":
			if instance.execution != nil && !instance.loading {
				instance.loading = true
				instance.requestID++
				return instance, loadExecution(instance, instance.execution.ID, instance.requestID)
			}
		case "c":
			if instance.execution != nil && !isTerminalStatus(instance.execution.Status) && !instance.cancelInFlight {
				instance.confirmCancel = true
				instance.notice = "Cancel this execution? Press y to request cancellation or n to keep it running."
			}
		}
		return instance, nil
	}
	if instance.view == executionsView {
		switch key.String() {
		case "esc", "backspace":
			instance.view = homeView
			instance.loadError = ""
		case "up", "k":
			if instance.selected > 0 {
				instance.selected--
			} else if instance.offset > 0 {
				instance.offset = maxInt(0, instance.offset-executionPageSize)
				instance.selected = executionPageSize - 1
				instance.loading = true
				instance.requestID++
				return instance, loadExecutions(instance, instance.requestID)
			}
		case "down", "j":
			if instance.selected+1 < len(instance.executions) {
				instance.selected++
			} else if instance.offset+len(instance.executions) < instance.total {
				instance.offset += executionPageSize
				instance.selected = 0
				instance.loading = true
				instance.requestID++
				return instance, loadExecutions(instance, instance.requestID)
			}
		case "right", "enter":
			if len(instance.executions) > 0 && instance.selected < len(instance.executions) {
				instance.view = detailView
				instance.execution = nil
				instance.loading = true
				instance.detailScroll = 0
				instance.requestID++
				return instance, loadExecution(instance, instance.executions[instance.selected].ID, instance.requestID)
			}
		case "n":
			if instance.offset+len(instance.executions) < instance.total {
				instance.offset += executionPageSize
				instance.selected = 0
				instance.loading = true
				instance.requestID++
				return instance, loadExecutions(instance, instance.requestID)
			}
		case "p":
			if instance.offset > 0 {
				instance.offset = maxInt(0, instance.offset-executionPageSize)
				instance.selected = 0
				instance.loading = true
				instance.requestID++
				return instance, loadExecutions(instance, instance.requestID)
			}
		case "f":
			instance.filterIndex = (instance.filterIndex + 1) % len(executionFilters)
			instance.statusFilter = executionFilters[instance.filterIndex]
			instance.offset, instance.selected = 0, 0
			instance.loading = true
			instance.requestID++
			return instance, loadExecutions(instance, instance.requestID)
		case "r":
			if !instance.loading {
				instance.loading = true
				instance.requestID++
				return instance, loadExecutions(instance, instance.requestID)
			}
		}
		return instance, nil
	}
	switch key.String() {
	case "j":
		instance.view = jobsView
	case "a":
		if instance.user.Role != "admin" {
			instance.notice = "Administrator role required."
			return instance, nil
		}
		instance.view = agentsView
	case "e":
		if instance.user.Role != "admin" {
			instance.notice = "Administrator role required."
			return instance, nil
		}
		instance.view = eventsView
	case "l":
		if instance.user.Role != "admin" {
			instance.notice = "Administrator role required."
			return instance, nil
		}
		instance.view = logsView
	case "x":
		if instance.user.Role != "admin" {
			instance.notice = "Administrator role required."
			return instance, nil
		}
		instance.view = executionsView
		instance.offset, instance.selected, instance.filterIndex = 0, 0, 0
		instance.statusFilter = executionFilters[0]
		instance.loading = true
		instance.loadError, instance.notice = "", ""
		instance.requestID++
		return instance, loadExecutions(instance, instance.requestID)
	case "r":
		instance.loading = true
		return instance, loadDashboard(instance)
	case "h", "?":
		instance.notice = "Keys: r refresh │ j jobs │ a agents │ e events │ l logs │ x executions │ q quit │ ? help"
	}
	return instance, nil
}

var executionFilters = []string{"", "queued", "running", "succeeded", "failed", "cancelled", "timed_out"}

// ─── View Rendering ────────────────────────────────────────────────────────────

func (instance model) View() string {
	width := instance.width
	if width < 40 {
		width = 40
	}
	contentWidth := width - 4

	var sections []string

	// Tab bar
	sections = append(sections, instance.renderTabBar(contentWidth))
	sections = append(sections, makeDivider(contentWidth))

	switch instance.view {
	case executionsView:
		sections = append(sections, instance.executionListView(contentWidth)...)
	case detailView:
		sections = append(sections, instance.executionDetailView(contentWidth)...)
	default:
		sections = append(sections, instance.dashboardView(contentWidth)...)
	}

	// Notices and errors
	if instance.notice != "" {
		sections = append(sections, "")
		sections = append(sections, quietStyle.Width(contentWidth).Render("  "+instance.notice))
	}
	if instance.loadError != "" {
		sections = append(sections, "")
		sections = append(sections, errorStyle.Width(contentWidth).Render("  ✗ "+instance.loadError))
	}

	// Help bar
	sections = append(sections, "")
	sections = append(sections, helpBarStyle.Width(contentWidth).Render(instance.helpText()))

	body := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return lipgloss.NewStyle().Width(width).Padding(0, 1).Render(body)
}

func (instance model) renderTabBar(width int) string {
	tabs := []struct {
		name  viewName
		label string
	}{
		{homeView, "Overview"},
		{jobsView, "Jobs"},
	}
	if instance.user.Role == "admin" {
		tabs = append(tabs,
			struct {
				name  viewName
				label string
			}{agentsView, "Agents"},
			struct {
				name  viewName
				label string
			}{eventsView, "Events"},
			struct {
				name  viewName
				label string
			}{logsView, "Logs"},
			struct {
				name  viewName
				label string
			}{executionsView, "Execs"},
		)
	}

	var parts []string
	for _, tab := range tabs {
		style := tabInactiveStyle
		active := instance.view == tab.name
		if tab.name == executionsView && instance.view == detailView {
			active = true
		}
		if active {
			style = tabActiveStyle
		}
		parts = append(parts, style.Render(tab.label))
	}

	bar := lipgloss.JoinHorizontal(lipgloss.Center, parts...)
	return lipgloss.NewStyle().Width(width).Render(bar)
}

func makeDivider(width int) string {
	return lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", width))
}

func (instance model) dashboardView(width int) []string {
	var lines []string

	// Brand header
	header := brandStyle.Render("INFRAFLOW") + "  " +
		quietStyle.Render("v0.1.0")
	lines = append(lines, header)
	lines = append(lines, "")

	// User info
	userLine := labelStyle.Render("User") + "  " +
		valueStyle.Render(instance.user.Username) + " " +
		quietStyle.Render("("+instance.user.Role+")") + "  " +
		labelStyle.Render("session expires") + " " +
		quietStyle.Render(instance.session.ExpiresAt.Local().Format("15:04"))
	lines = append(lines, userLine)

	// Last refresh
	if !instance.lastRefresh.IsZero() {
		lines = append(lines, labelStyle.Render("Last refresh")+"  "+quietStyle.Render(instance.lastRefresh.Local().Format("15:04:05")))
	}
	lines = append(lines, "")

	// Provider state
	providerTitle := sectionStyle.Render("Provider State")
	lines = append(lines, providerTitle)

	if instance.loading {
		lines = append(lines, quietStyle.Render("  Loading Provider state…"))
	} else if instance.loadError != "" {
		lines = append(lines, errorStyle.Render("  "+instance.loadError))
	} else {
		statsLine := fmt.Sprintf("  %-20s %s", labelStyle.Render("Planning jobs:"), valueStyle.Render(fmt.Sprint(len(instance.jobs))))
		lines = append(lines, statsLine)
		if instance.user.Role == "admin" {
			lines = append(lines, fmt.Sprintf("  %-20s %s  │  %-16s %s",
				labelStyle.Render("Agents:"), valueStyle.Render(fmt.Sprint(len(instance.agents))),
				labelStyle.Render("Audit events:"), valueStyle.Render(fmt.Sprint(len(instance.events)))))
			lines = append(lines, fmt.Sprintf("  %-20s %s",
				labelStyle.Render("Log entries:"), valueStyle.Render(fmt.Sprint(len(instance.logs)))))
		}
	}
	lines = append(lines, "")

	// Navigation
	navTitle := sectionStyle.Render("Navigation")
	lines = append(lines, navTitle)
	lines = append(lines, fmt.Sprintf("  %s  Planning jobs", keyStyle.Render("j")))
	if instance.user.Role == "admin" {
		lines = append(lines, fmt.Sprintf("  %s  Agents", keyStyle.Render("a")))
		lines = append(lines, fmt.Sprintf("  %s  Audit events", keyStyle.Render("e")))
		lines = append(lines, fmt.Sprintf("  %s  Technical logs", keyStyle.Render("l")))
		lines = append(lines, fmt.Sprintf("  %s  Executions", keyStyle.Render("x")))
	}

	return lines
}

func (instance model) executionListView(width int) []string {
	filter := instance.statusFilter
	if filter == "" {
		filter = "all"
	}

	var lines []string
	lines = append(lines, sectionStyle.Render("Executions"))

	// Filter and count info
	infoLine := fmt.Sprintf("  %s %s  │  %s %s",
		labelStyle.Render("Filter:"), valueStyle.Render(filter),
		labelStyle.Render("Total:"), valueStyle.Render(fmt.Sprint(instance.total)))
	lines = append(lines, infoLine)
	lines = append(lines, "")

	if instance.loading && len(instance.executions) == 0 {
		lines = append(lines, quietStyle.Render("  Loading executions…"))
	} else if !instance.loading && len(instance.executions) == 0 && instance.loadError == "" {
		lines = append(lines, quietStyle.Render("  No executions returned by the Provider."))
	} else {
		// Table header
		headerLine := fmt.Sprintf("  %-34s %-12s %s", "ID", "STATUS", "UPDATED")
		lines = append(lines, labelStyle.Width(width).Render(headerLine))
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", minInt(width-4, 70))))

		for index, execution := range instance.executions {
			prefix := "  "
			style := lipgloss.NewStyle()
			if index == instance.selected {
				prefix = " ▸"
				style = selectedStyle
			}
			line := fmt.Sprintf("%-34s %-12s %s",
				truncateString(execution.ID, 32),
				renderStatus(execution.Status),
				execution.UpdatedAt.Local().Format("2006-01-02 15:04"))
			lines = append(lines, style.Width(width).Render(prefix+line))
		}
	}

	if instance.loading && len(instance.executions) > 0 {
		lines = append(lines, "")
		lines = append(lines, quietStyle.Render("  Refreshing…"))
	}

	// Pagination
	page := 1
	if executionPageSize > 0 {
		page = instance.offset/executionPageSize + 1
	}
	lines = append(lines, "")
	lines = append(lines, quietStyle.Render(fmt.Sprintf("  Page %d · records %d–%d of %d",
		page, instance.offset+1, instance.offset+len(instance.executions), instance.total)))

	return lines
}

func (instance model) executionDetailView(width int) []string {
	if instance.loading && instance.execution == nil {
		return []string{
			sectionStyle.Render("Execution Detail"),
			quietStyle.Render("  Loading execution from Provider…"),
		}
	}
	if instance.execution == nil {
		return []string{
			sectionStyle.Render("Execution Detail"),
			quietStyle.Render("  No execution detail is available."),
		}
	}

	execution := instance.execution
	var lines []string

	// Header
	lines = append(lines, sectionStyle.Render("Execution Detail"))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s  %s", labelStyle.Render("ID:"), valueStyle.Render(execution.ID)))
	lines = append(lines, fmt.Sprintf("  %s  %s", labelStyle.Render("Status:"), renderStatus(execution.Status)))
	lines = append(lines, fmt.Sprintf("  %s  %s  │  %s  %s",
		labelStyle.Render("Created:"), quietStyle.Render(formatExecutionTime(execution.CreatedAt)),
		labelStyle.Render("Updated:"), quietStyle.Render(formatExecutionTime(execution.UpdatedAt))))
	if !execution.StartedAt.IsZero() {
		lines = append(lines, fmt.Sprintf("  %s  %s", labelStyle.Render("Started:"), quietStyle.Render(formatExecutionTime(execution.StartedAt))))
	}
	if !execution.FinishedAt.IsZero() {
		lines = append(lines, fmt.Sprintf("  %s  %s", labelStyle.Render("Finished:"), quietStyle.Render(formatExecutionTime(execution.FinishedAt))))
	}
	if execution.CancelRequested {
		lines = append(lines, "")
		lines = append(lines, queuedStyle.Render("  ⚠ Cancellation requested; Provider has not necessarily completed cancellation."))
	}
	if execution.Error != "" {
		lines = append(lines, "")
		lines = append(lines, errorStyle.Width(width).Render("  Error: "+observability.Redact(execution.Error)))
	}

	// Tasks
	lines = append(lines, "")
	lines = append(lines, sectionStyle.Render(fmt.Sprintf("  Tasks (%d)", len(execution.Tasks))))
	for _, task := range execution.Tasks {
		lines = append(lines, fmt.Sprintf("  %s  %s  %s",
			renderStatus(string(task.Status)),
			valueStyle.Render(task.ID),
			quietStyle.Render(task.Type)))
		if len(task.Dependencies) > 0 {
			lines = append(lines, quietStyle.Width(width).Render("    depends on: "+strings.Join(task.Dependencies, ", ")))
		}
		if task.Error != "" {
			lines = append(lines, errorStyle.Width(width).Render("    error: "+observability.Redact(task.Error)))
		}
		if len(task.Result) > 0 {
			lines = append(lines, quietStyle.Width(width).Render("    result: "+compactJSON(task.Result)))
		}
		for _, attempt := range task.Attempts {
			duration := "—"
			if attempt.DurationMS > 0 {
				duration = time.Duration(attempt.DurationMS * int64(time.Millisecond)).String()
			}
			lines = append(lines, fmt.Sprintf("    attempt %d · %s · %s",
				attempt.Number, renderStatus(attempt.Status), quietStyle.Render(duration)))
			if attempt.Error != "" {
				lines = append(lines, errorStyle.Width(width).Render("      "+observability.Redact(attempt.Error)))
			}
			if len(attempt.Result) > 0 {
				lines = append(lines, quietStyle.Width(width).Render("      "+compactJSON(attempt.Result)))
			}
		}
	}

	// Events
	lines = append(lines, "")
	lines = append(lines, sectionStyle.Render(fmt.Sprintf("  Events (%d)", len(execution.Events))))
	for _, event := range execution.Events {
		line := fmt.Sprintf("  %s  %s  %s",
			quietStyle.Render(event.Timestamp.Local().Format("15:04:05")),
			valueStyle.Render(event.Action),
			renderStatus(event.Status))
		if event.TaskID != "" {
			line += "  " + quietStyle.Render("task="+event.TaskID)
		}
		if event.Error != "" {
			line += "  " + errorStyle.Render(observability.Redact(event.Error))
		}
		lines = append(lines, quietStyle.Width(width).Render(line))
	}

	if instance.confirmCancel {
		lines = append(lines, "")
		lines = append(lines, errorStyle.Width(width).Render("  Cancel request · y confirm · n/esc keep running"))
	}
	if instance.loading {
		lines = append(lines, "")
		lines = append(lines, quietStyle.Render("  Waiting for Provider response…"))
	}

	// Scroll handling
	visibleLines := instance.height - 6
	if visibleLines < 5 {
		visibleLines = 5
	}
	maxScroll := maxInt(0, len(lines)-visibleLines)
	scroll := instance.detailScroll
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	if scroll > 0 || len(lines) > visibleLines {
		end := minInt(len(lines), scroll+visibleLines)
		lines = append([]string(nil), lines[scroll:end]...)
		lines = append(lines, quietStyle.Render(fmt.Sprintf("  lines %d–%d · ↑/↓ scroll", scroll+1, end)))
	}
	return lines
}

func (instance model) helpText() string {
	switch instance.view {
	case executionsView:
		return "↑/↓ select  enter view  n/p page  f filter  r refresh  esc back  q quit"
	case detailView:
		if instance.confirmCancel {
			return "y request cancellation  n/esc abandon  q quit"
		}
		if instance.execution != nil && !isTerminalStatus(instance.execution.Status) {
			return "c cancel  r refresh  ↑/↓ scroll  esc list  q quit"
		}
		return "r refresh  ↑/↓ scroll  esc list  q quit"
	default:
		return "r refresh  j jobs  a agents  e events  l logs  x execs  ? help  q quit"
	}
}

// ─── Commands ──────────────────────────────────────────────────────────────────

func loadDashboard(instance model) tea.Cmd {
	requestID := instance.requestID
	return func() tea.Msg {
		jobs, err := instance.backend.Jobs(instance.ctx)
		if err != nil {
			return dashboardLoadedMsg{requestID: requestID, err: err}
		}
		loaded := dashboardLoadedMsg{requestID: requestID, jobs: jobs}
		if instance.user.Role == "admin" {
			loaded.agents, err = instance.backend.Agents(instance.ctx)
			if err == nil {
				loaded.events, err = instance.backend.Events(instance.ctx)
			}
			if err == nil {
				loaded.logs, err = instance.backend.Logs(instance.ctx)
			}
		}
		loaded.err = err
		return loaded
	}
}

func loadExecutions(instance model, requestID uint64) tea.Cmd {
	return func() tea.Msg {
		page, err := instance.backend.Executions(instance.ctx, executionPageSize, instance.offset, instance.statusFilter, "")
		return executionsLoadedMsg{requestID: requestID, page: page, err: err}
	}
}

func loadExecution(instance model, id string, requestID uint64) tea.Cmd {
	return func() tea.Msg {
		execution, err := instance.backend.Execution(instance.ctx, id)
		return executionLoadedMsg{requestID: requestID, execution: execution, err: err}
	}
}

func cancelExecution(instance model, id string, requestID uint64) tea.Cmd {
	return func() tea.Msg {
		execution, err := instance.backend.CancelExecution(instance.ctx, id)
		return executionCancelledMsg{requestID: requestID, execution: execution, err: err}
	}
}

func refreshAfter(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(now time.Time) tea.Msg { return refreshTickMsg(now) })
}

// ─── Helpers ───────────────────────────────────────────────────────────────────

func renderStatus(status string) string {
	label := strings.ToUpper(status)
	switch status {
	case "queued", "retrying":
		return queuedStyle.Render(label)
	case "running":
		return runningStyle.Render(label)
	case "succeeded":
		return successStyle.Render(label)
	case "failed", "blocked":
		return failedStyle.Render(label)
	case "cancelled":
		return cancelledStyle.Render(label)
	case "timed_out":
		return timedOutStyle.Render(label)
	default:
		return quietStyle.Render(label)
	}
}

func (instance model) executionActive() bool {
	return instance.execution != nil && (instance.execution.Status == "queued" || instance.execution.Status == "running")
}

func isTerminalStatus(status string) bool {
	switch status {
	case "succeeded", "failed", "cancelled", "timed_out":
		return true
	default:
		return false
	}
}

func formatExecutionTime(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.Local().Format("2006-01-02 15:04:05 MST")
}

func compactJSON(value json.RawMessage) string {
	if len(value) == 0 {
		return ""
	}
	var formatted any
	if err := json.Unmarshal(value, &formatted); err != nil {
		return observability.Redact(string(value))
	}
	encoded, err := json.Marshal(formatted)
	if err != nil {
		return "[invalid JSON]"
	}
	return observability.Redact(string(encoded))
}

func displayAPIError(err error) string {
	message := err.Error()
	if strings.Contains(message, "HTTP 401") {
		return "Session expired; sign in again. " + message
	}
	if strings.Contains(message, "HTTP 403") {
		return "Provider denied this action. " + message
	}
	return message
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
