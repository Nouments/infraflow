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

var (
	titleStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	selectedStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	quietStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errorStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	queuedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	runningStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	successStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	failedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	cancelledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	timedOutStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("201"))
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
		instance.notice = "Keys: r refresh | j jobs | a agents | e events | l logs | x executions | q quit | ? help"
	}
	return instance, nil
}

var executionFilters = []string{"", "queued", "running", "succeeded", "failed", "cancelled", "timed_out"}

func (instance model) View() string {
	width := instance.width
	if width < 40 {
		width = 40
	}
	contentWidth := width - 4
	var sections []string
	switch instance.view {
	case executionsView:
		sections = instance.executionListView(contentWidth)
	case detailView:
		sections = instance.executionDetailView(contentWidth)
	default:
		sections = instance.dashboardView(contentWidth)
	}
	if instance.notice != "" {
		sections = append(sections, quietStyle.Width(contentWidth).Render(instance.notice))
	}
	if instance.loadError != "" {
		sections = append(sections, errorStyle.Width(contentWidth).Render(instance.loadError))
	}
	sections = append(sections, quietStyle.Width(contentWidth).Render(instance.helpText()))
	body := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return lipgloss.NewStyle().Width(width).Padding(1, 2).Render(body)
}

func (instance model) dashboardView(width int) []string {
	lines := []string{
		titleStyle.Render("INFRAFLOW"),
		fmt.Sprintf("%s  %s (%s)  ·  expires %s", quietStyle.Render("Authenticated"), instance.user.Username, instance.user.Role, instance.session.ExpiresAt.Local().Format("15:04")),
		"",
		titleStyle.Render("OVERVIEW"),
	}
	if instance.loading {
		lines = append(lines, quietStyle.Render("Loading Provider state…"))
	} else if instance.loadError != "" {
		lines = append(lines, errorStyle.Render(instance.loadError))
	} else {
		lines = append(lines, fmt.Sprintf("Planning jobs: %d", len(instance.jobs)))
		if instance.user.Role == "admin" {
			lines = append(lines, fmt.Sprintf("Registered agents: %d  ·  Audit events: %d", len(instance.agents), len(instance.events)))
			lines = append(lines, fmt.Sprintf("Technical log entries: %d", len(instance.logs)))
		}
	}
	lines = append(lines, "", titleStyle.Render("VIEWS"), "  j  Planning jobs")
	if instance.user.Role == "admin" {
		lines = append(lines, "  a  Agents", "  e  Audit events", "  l  Technical logs", "  x  Executions")
	}
	return lines
}

func (instance model) executionListView(width int) []string {
	filter := instance.statusFilter
	if filter == "" {
		filter = "all"
	}
	lines := []string{titleStyle.Render("EXECUTIONS"), fmt.Sprintf("Provider records · status filter: %s · total: %d", filter, instance.total), ""}
	if instance.loading && len(instance.executions) == 0 {
		lines = append(lines, quietStyle.Render("Loading executions…"))
	} else if !instance.loading && len(instance.executions) == 0 && instance.loadError == "" {
		lines = append(lines, quietStyle.Render("No executions returned by the Provider."))
	} else {
		for index, execution := range instance.executions {
			prefix := "  "
			style := lipgloss.NewStyle()
			if index == instance.selected {
				prefix, style = "> ", selectedStyle
			}
			line := fmt.Sprintf("%-34s %-12s %s", execution.ID, execution.Status, execution.UpdatedAt.Local().Format("2006-01-02 15:04:05"))
			lines = append(lines, style.Width(width).Render(prefix+line))
		}
	}
	if instance.loading && len(instance.executions) > 0 {
		lines = append(lines, quietStyle.Render("Refreshing Provider data…"))
	}
	page := 1
	if executionPageSize > 0 {
		page = instance.offset/executionPageSize + 1
	}
	lines = append(lines, "", quietStyle.Render(fmt.Sprintf("Page %d · records %d-%d of %d", page, instance.offset+1, instance.offset+len(instance.executions), instance.total)))
	return lines
}

func (instance model) executionDetailView(width int) []string {
	if instance.loading && instance.execution == nil {
		return []string{titleStyle.Render("EXECUTION DETAIL"), quietStyle.Render("Loading execution from Provider…")}
	}
	if instance.execution == nil {
		return []string{titleStyle.Render("EXECUTION DETAIL"), quietStyle.Render("No execution detail is available.")}
	}
	execution := instance.execution
	lines := []string{
		titleStyle.Render("EXECUTION DETAIL"),
		"ID: " + execution.ID,
		"Status: " + renderStatus(execution.Status),
		fmt.Sprintf("Created: %s  ·  Updated: %s", formatExecutionTime(execution.CreatedAt), formatExecutionTime(execution.UpdatedAt)),
	}
	if !execution.StartedAt.IsZero() {
		lines = append(lines, "Started: "+formatExecutionTime(execution.StartedAt))
	}
	if !execution.FinishedAt.IsZero() {
		lines = append(lines, "Finished: "+formatExecutionTime(execution.FinishedAt))
	}
	if execution.CancelRequested {
		lines = append(lines, queuedStyle.Render("Cancellation requested; Provider has not necessarily completed cancellation."))
	}
	if execution.Error != "" {
		lines = append(lines, errorStyle.Width(width).Render("Error: "+observability.Redact(execution.Error)))
	}
	lines = append(lines, "", titleStyle.Render(fmt.Sprintf("TASKS (%d)", len(execution.Tasks))))
	for _, task := range execution.Tasks {
		lines = append(lines, fmt.Sprintf("%s  %s  %s", renderStatus(string(task.Status)), task.ID, task.Type))
		if len(task.Dependencies) > 0 {
			lines = append(lines, quietStyle.Width(width).Render("  depends on: "+strings.Join(task.Dependencies, ", ")))
		}
		if task.Error != "" {
			lines = append(lines, errorStyle.Width(width).Render("  error: "+observability.Redact(task.Error)))
		}
		if len(task.Result) > 0 {
			lines = append(lines, quietStyle.Width(width).Render("  result: "+compactJSON(task.Result)))
		}
		for _, attempt := range task.Attempts {
			duration := "duration unavailable"
			if attempt.DurationMS > 0 {
				duration = time.Duration(attempt.DurationMS * int64(time.Millisecond)).String()
			}
			lines = append(lines, fmt.Sprintf("  attempt %d · %s · %s", attempt.Number, renderStatus(attempt.Status), duration))
			if attempt.Error != "" {
				lines = append(lines, errorStyle.Width(width).Render("    "+observability.Redact(attempt.Error)))
			}
			if len(attempt.Result) > 0 {
				lines = append(lines, quietStyle.Width(width).Render("    "+compactJSON(attempt.Result)))
			}
		}
	}
	lines = append(lines, "", titleStyle.Render(fmt.Sprintf("EVENTS (%d)", len(execution.Events))))
	for _, event := range execution.Events {
		line := fmt.Sprintf("%s  %s  %s", event.Timestamp.Local().Format("15:04:05"), event.Action, event.Status)
		if event.TaskID != "" {
			line += "  task=" + event.TaskID
		}
		if event.Error != "" {
			line += "  " + observability.Redact(event.Error)
		}
		lines = append(lines, quietStyle.Width(width).Render(line))
	}
	if instance.confirmCancel {
		lines = append(lines, errorStyle.Width(width).Render("Cancel request confirmation · y confirm · n/esc keep running"))
	}
	if instance.loading {
		lines = append(lines, quietStyle.Render("Waiting for Provider response…"))
	}
	visibleLines := instance.height - 7
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
		lines = append(lines, quietStyle.Render(fmt.Sprintf("Detail lines %d-%d · ↑/↓ scroll", scroll+1, end)))
	}
	return lines
}

func (instance model) helpText() string {
	switch instance.view {
	case executionsView:
		return "↑/k · ↓/j select  enter view  n/p page  f filter  r refresh  esc back  q quit"
	case detailView:
		if instance.confirmCancel {
			return "y request cancellation  n/esc abandon  q quit"
		}
		if instance.execution != nil && !isTerminalStatus(instance.execution.Status) {
			return "c request cancel  r refresh  ↑/k · ↓/j scroll  esc list  q quit"
		}
		return "r refresh  ↑/k · ↓/j scroll  esc list  q quit"
	default:
		return "r refresh  j jobs  a agents  e events  l logs  x executions  ? help  q quit"
	}
}

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
		return "not started"
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
