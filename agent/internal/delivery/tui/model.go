package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"infraflow/agent/internal/adapters/providerhttp"
	"infraflow/pkg/observability"
)

const executionPageSize = 10

// ─── Styles & Palette ──────────────────────────────────────────────────────────

var (
	// Professional NOC dark palette
	colorAccent    = lipgloss.Color("39")  // Crisp Blue
	colorGreen     = lipgloss.Color("42")  // Calm Green
	colorOrange    = lipgloss.Color("214") // Amber Warning
	colorRed       = lipgloss.Color("196") // Clear Red
	colorMagenta   = lipgloss.Color("201") // Purple/Magenta
	colorDim       = lipgloss.Color("245") // Subtle Slate
	colorBorder    = lipgloss.Color("236") // Fine Divider
	colorHighlight = lipgloss.Color("234") // Background Wash

	// Brand & Header Styles
	brandStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

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

	// Status styles
	queuedStyle    = lipgloss.NewStyle().Foreground(colorOrange)
	runningStyle   = lipgloss.NewStyle().Foreground(colorAccent)
	successStyle   = lipgloss.NewStyle().Foreground(colorGreen)
	failedStyle    = lipgloss.NewStyle().Foreground(colorRed)
	cancelledStyle = lipgloss.NewStyle().Foreground(colorDim)
	timedOutStyle  = lipgloss.NewStyle().Foreground(colorMagenta)

	// Tab styles
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
	servicesView   viewName = "services"
)

type model struct {
	ctx            context.Context
	backend        Backend
	user           providerhttp.User
	session        providerhttp.UserSession
	agentInfo      AgentInfo
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

func newModel(ctx context.Context, backend Backend, user providerhttp.User, session providerhttp.UserSession, info ...AgentInfo) model {
	var aInfo AgentInfo
	if len(info) > 0 {
		aInfo = info[0]
	}
	return model{
		ctx:       ctx,
		backend:   backend,
		user:      user,
		session:   session,
		agentInfo: aInfo,
		view:      homeView,
		width:     80,
		height:    24,
	}
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
			instance.lastRefresh = time.Now()
			instance.jobs = typed.jobs
			instance.agents = typed.agents
			instance.events = typed.events
			instance.logs = typed.logs
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
			instance.lastRefresh = time.Now()
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
			instance.lastRefresh = time.Now()
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
		instance.lastRefresh = time.Now()
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
	k := key.String()
	if k == "ctrl+c" || k == "q" {
		return instance, tea.Quit
	}

	if instance.confirmCancel {
		switch k {
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
		switch k {
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
		switch k {
		case "esc", "backspace", "o":
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

	// Views other than detail/executions:
	switch k {
	case "o", "esc", "backspace":
		instance.view = homeView
		instance.selected = 0
	case "j":
		instance.view = jobsView
		instance.selected = 0
	case "s":
		instance.view = servicesView
		instance.selected = 0
	case "a":
		if instance.user.Role != "admin" {
			instance.notice = "Administrator role required."
			return instance, nil
		}
		instance.view = agentsView
		instance.selected = 0
	case "e":
		if instance.user.Role != "admin" {
			instance.notice = "Administrator role required."
			return instance, nil
		}
		instance.view = eventsView
		instance.selected = 0
	case "l":
		if instance.user.Role != "admin" {
			instance.notice = "Administrator role required."
			return instance, nil
		}
		instance.view = logsView
		instance.selected = 0
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
		instance.requestID++
		return instance, loadDashboard(instance)
	case "up", "k":
		if instance.selected > 0 {
			instance.selected--
		}
	case "down":
		instance.selected++
	case "h", "?":
		instance.notice = "Navigation help: o overview · j jobs · s services · a agents · e events · l logs · x execs · r refresh · q quit"
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

	// Navigation Tab bar
	sections = append(sections, instance.renderTabBar(contentWidth))
	sections = append(sections, makeDivider(contentWidth))

	switch instance.view {
	case executionsView:
		sections = append(sections, instance.executionListView(contentWidth)...)
	case detailView:
		sections = append(sections, instance.executionDetailView(contentWidth)...)
	case jobsView:
		sections = append(sections, instance.jobsListView(contentWidth)...)
	case servicesView:
		sections = append(sections, instance.servicesListView(contentWidth)...)
	case agentsView:
		sections = append(sections, instance.agentsListView(contentWidth)...)
	case eventsView:
		sections = append(sections, instance.eventsListView(contentWidth)...)
	case logsView:
		sections = append(sections, instance.logsListView(contentWidth)...)
	default:
		sections = append(sections, instance.dashboardView(contentWidth)...)
	}

	// Notices and errors
	if instance.notice != "" {
		sections = append(sections, "")
		sections = append(sections, quietStyle.Width(contentWidth).Render("  ℹ "+instance.notice))
	}
	if instance.loadError != "" {
		sections = append(sections, "")
		sections = append(sections, errorStyle.Width(contentWidth).Render("  ✗ "+instance.loadError))
	}

	// Contextual Help bar
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
		{servicesView, "Services"},
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

// ─── Overview View ─────────────────────────────────────────────────────────────

func (instance model) dashboardView(width int) []string {
	var lines []string

	// Header: Brand & Real Version
	header := brandStyle.Render("INFRAFLOW") + "  " +
		quietStyle.Render("v0.1.0")
	lines = append(lines, header)
	lines = append(lines, "")

	// Identity: Agent ID & Site
	agentID := "Non configuré"
	if instance.agentInfo.ID != "" {
		agentID = instance.agentInfo.ID
	}
	siteID := "Non configuré"
	if instance.agentInfo.SiteID != "" {
		siteID = instance.agentInfo.SiteID
	}

	statusText := successStyle.Render("● Opérationnel")
	if instance.loadError != "" {
		statusText = failedStyle.Render("● Dégradé (Erreur Provider)")
	} else if instance.lastRefresh.IsZero() {
		statusText = queuedStyle.Render("● En attente")
	}

	lines = append(lines, fmt.Sprintf("  %s %s  │  %s %s  │  %s",
		labelStyle.Render("Agent:"), valueStyle.Render(agentID),
		labelStyle.Render("Site:"), valueStyle.Render(siteID),
		statusText))

	// Refresh & Session info
	refreshStr := "En attente"
	if !instance.lastRefresh.IsZero() {
		refreshStr = instance.lastRefresh.Local().Format("15:04:05")
	}
	sessionExp := "—"
	if !instance.session.ExpiresAt.IsZero() {
		sessionExp = instance.session.ExpiresAt.Local().Format("15:04")
	}

	lines = append(lines, fmt.Sprintf("  %s %s (%s)  │  %s %s  │  %s %s",
		labelStyle.Render("User:"), valueStyle.Render(instance.user.Username), quietStyle.Render(instance.user.Role),
		labelStyle.Render("Session:"), quietStyle.Render(sessionExp),
		labelStyle.Render("Refresh:"), quietStyle.Render(refreshStr)))
	lines = append(lines, "")

	// Section 1: Processus Agent & Hôte local
	lines = append(lines, sectionStyle.Render("Local Agent Runtime"))
	lines = append(lines, fmt.Sprintf("  %-22s %s",
		labelStyle.Render("Process:"),
		valueStyle.Render(fmt.Sprintf("Actif (PID %d)", os.Getpid()))))
	lines = append(lines, fmt.Sprintf("  %-22s %s",
		labelStyle.Render("Runtime:"),
		quietStyle.Render(fmt.Sprintf("%s on %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH))))
	if instance.agentInfo.StateDirectory != "" {
		lines = append(lines, fmt.Sprintf("  %-22s %s",
			labelStyle.Render("State Directory:"),
			quietStyle.Render(instance.agentInfo.StateDirectory)))
	} else {
		lines = append(lines, fmt.Sprintf("  %-22s %s",
			labelStyle.Render("State Directory:"),
			quietStyle.Render("Non configuré")))
	}
	lines = append(lines, "")

	// Section 2: Connexion Provider & Synchronisation
	lines = append(lines, sectionStyle.Render("Provider Connectivity"))
	provAddr := "Non configuré"
	if instance.agentInfo.ProviderAddress != "" {
		provAddr = observability.Redact(instance.agentInfo.ProviderAddress)
	} else if instance.agentInfo.APIAddress != "" {
		provAddr = observability.Redact(instance.agentInfo.APIAddress)
	}

	syncState := successStyle.Render("Synchronisé")
	if instance.loadError != "" {
		syncState = queuedStyle.Render("Données en cache")
	}

	lines = append(lines, fmt.Sprintf("  %-22s %s",
		labelStyle.Render("Address:"),
		valueStyle.Render(provAddr)))
	lines = append(lines, fmt.Sprintf("  %-22s %s  │  %s %s",
		labelStyle.Render("Connection:"),
		instance.providerConnectionBadge(),
		labelStyle.Render("Sync:"),
		syncState))
	lines = append(lines, "")

	// Section 3: Résumé des Services locaux
	lines = append(lines, sectionStyle.Render("Local Services Summary (press 's' for details)"))
	lines = append(lines, fmt.Sprintf("  %-22s %s",
		labelStyle.Render("Artifact Store:"),
		instance.artifactStoreSummary()))
	lines = append(lines, fmt.Sprintf("  %-22s %s",
		labelStyle.Render("Log Outbox:"),
		instance.logOutboxSummary()))
	lines = append(lines, fmt.Sprintf("  %-22s %s",
		labelStyle.Render("Tool Runner:"),
		instance.toolRunnerSummary()))
	lines = append(lines, fmt.Sprintf("  %-22s %s",
		labelStyle.Render("Bootstrap:"),
		quietStyle.Render("Commandes dédiées (inactif par défaut, pas de DNS)")))
	lines = append(lines, "")

	// Section 4: Métriques Provider State
	lines = append(lines, sectionStyle.Render("Provider State"))
	if instance.loading && len(instance.jobs) == 0 {
		lines = append(lines, quietStyle.Render("  Chargement des données Provider…"))
	} else {
		lines = append(lines, fmt.Sprintf("  %-22s %s",
			labelStyle.Render("Planning jobs:"),
			valueStyle.Render(fmt.Sprint(len(instance.jobs)))))
		if instance.user.Role == "admin" {
			lines = append(lines, fmt.Sprintf("  %-22s %s  │  %-16s %s",
				labelStyle.Render("Agents:"), valueStyle.Render(fmt.Sprint(len(instance.agents))),
				labelStyle.Render("Audit events:"), valueStyle.Render(fmt.Sprint(len(instance.events)))))
			lines = append(lines, fmt.Sprintf("  %-22s %s  │  %-16s %s",
				labelStyle.Render("Log entries:"), valueStyle.Render(fmt.Sprint(len(instance.logs))),
				labelStyle.Render("Executions:"), valueStyle.Render(fmt.Sprint(instance.total))))
		}
	}

	return lines
}

func (instance model) providerConnectionBadge() string {
	if instance.loadError != "" {
		return failedStyle.Render("Déconnecté")
	}
	if instance.lastRefresh.IsZero() {
		return queuedStyle.Render("Initialisation…")
	}
	return successStyle.Render("Connecté")
}

func (instance model) artifactStoreSummary() string {
	if instance.agentInfo.StateDirectory != "" {
		return successStyle.Render("Configuré") + " " + quietStyle.Render("("+instance.agentInfo.StateDirectory+"/artifacts)")
	}
	return quietStyle.Render("Non configuré (aucun agent.yaml chargé)")
}

func (instance model) logOutboxSummary() string {
	if instance.agentInfo.LogDirectory != "" {
		return successStyle.Render("Actif") + " " + quietStyle.Render("("+instance.agentInfo.LogDirectory+"/pending-events.json)")
	}
	return quietStyle.Render("Inactif (chemin local non configuré)")
}

func (instance model) toolRunnerSummary() string {
	return successStyle.Render("Prêt") + " " + quietStyle.Render("(Ansible & Terraform runners disponibles)")
}

// ─── Local Services Detail View ────────────────────────────────────────────────

func (instance model) servicesListView(width int) []string {
	var lines []string
	lines = append(lines, sectionStyle.Render("Local Supervised Services & Components"))
	lines = append(lines, quietStyle.Render("  Composants observables localement sur l'hôte. Aucun état fictif."))
	lines = append(lines, "")

	// 1. Artifacts Repository
	lines = append(lines, sectionStyle.Render("  1. Artifact Repository & Verification"))
	if instance.agentInfo.StateDirectory != "" {
		lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("Path:"), valueStyle.Render(instance.agentInfo.StateDirectory+"/artifacts")))
		lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("Status:"), successStyle.Render("Opérationnel")))
	} else {
		lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("Path:"), quietStyle.Render("Non configuré")))
		lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("Status:"), quietStyle.Render("Inactif (fournir -config <agent.yaml>)")))
	}
	lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("Integrity:"), quietStyle.Render("Vérification stricte SHA256 des flux gRPC")))
	lines = append(lines, "")

	// 2. Log Outbox
	lines = append(lines, sectionStyle.Render("  2. Local Log Outbox"))
	if instance.agentInfo.LogDirectory != "" {
		lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("Buffer:"), valueStyle.Render(instance.agentInfo.LogDirectory+"/pending-events.json")))
		lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("Status:"), successStyle.Render("Buffer prêt")))
	} else {
		lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("Buffer:"), quietStyle.Render("Non configuré")))
		lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("Status:"), quietStyle.Render("En attente de configuration")))
	}
	lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("Sync:"), quietStyle.Render("Transmission asynchrone sécurisée vers le Provider")))
	lines = append(lines, "")

	// 3. Process Runner
	lines = append(lines, sectionStyle.Render("  3. Process Execution (Tool Runner)"))
	lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("Engines:"), valueStyle.Render("Ansible process runner, Terraform validator")))
	lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("Isolation:"), quietStyle.Render("Timeouts explicites et capture des flux standard")))
	lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("Status:"), successStyle.Render("Prêt pour exécution ordonnancée")))
	lines = append(lines, "")

	// 4. Bootstrap Services
	lines = append(lines, sectionStyle.Render("  4. Network Bootstrap Services"))
	lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("DHCPv4:"), quietStyle.Render("Commande 'serve-dhcp' (non démarré par défaut)")))
	lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("TFTP:"), quietStyle.Render("Commande 'serve-tftp' (lecture seule, non démarré par défaut)")))
	lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("HTTP:"), quietStyle.Render("Commande 'serve-bootstrap' (non démarré par défaut)")))
	lines = append(lines, fmt.Sprintf("     %s  %s", labelStyle.Render("DNS:"), queuedStyle.Render("Aucun service DNS n'est implémenté par l'agent")))
	lines = append(lines, "")

	// 5. Operational Safety
	lines = append(lines, sectionStyle.Render("  5. Safety Boundaries"))
	lines = append(lines, quietStyle.Render("     Les exécuteurs réseau matériels restent rejetés tant qu'ils ne sont pas validés en lab."))

	return lines
}

// ─── Jobs Dedicated List View ──────────────────────────────────────────────────

func (instance model) jobsListView(width int) []string {
	var lines []string
	lines = append(lines, sectionStyle.Render("Planning Jobs & Declarative Plans"))
	lines = append(lines, fmt.Sprintf("  %s %s", labelStyle.Render("Total plans:"), valueStyle.Render(fmt.Sprint(len(instance.jobs)))))
	lines = append(lines, "")

	if len(instance.jobs) == 0 {
		lines = append(lines, quietStyle.Render("  Aucun plan ou job enregistré auprès du Provider."))
		return lines
	}

	headerLine := fmt.Sprintf("  %-36s %-12s %s", "JOB ID", "STATUS", "UPDATED")
	lines = append(lines, labelStyle.Width(width).Render(headerLine))
	lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", minInt(width-4, 70))))

	for index, job := range instance.jobs {
		prefix := "  "
		style := lipgloss.NewStyle()
		if index == instance.selected {
			prefix = " ▸"
			style = selectedStyle
		}
		updated := "—"
		if !job.UpdatedAt.IsZero() {
			updated = job.UpdatedAt.Local().Format("2006-01-02 15:04")
		}
		line := fmt.Sprintf("%-36s %-12s %s",
			truncateString(job.ID, 34),
			renderStatus(job.Status),
			updated)
		lines = append(lines, style.Width(width).Render(prefix+line))
	}

	return lines
}

// ─── Agents Dedicated List View (Admin) ────────────────────────────────────────

func (instance model) agentsListView(width int) []string {
	var lines []string
	lines = append(lines, sectionStyle.Render("Registered Agents (Admin View)"))
	lines = append(lines, fmt.Sprintf("  %s %s", labelStyle.Render("Registered:"), valueStyle.Render(fmt.Sprint(len(instance.agents)))))
	lines = append(lines, "")

	if len(instance.agents) == 0 {
		lines = append(lines, quietStyle.Render("  Aucun agent enregistré auprès du Provider."))
		return lines
	}

	headerLine := fmt.Sprintf("  %-26s %-18s %-10s %s", "AGENT ID", "SITE", "STATUS", "QUEUE")
	lines = append(lines, labelStyle.Width(width).Render(headerLine))
	lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", minInt(width-4, 70))))

	for index, agent := range instance.agents {
		prefix := "  "
		style := lipgloss.NewStyle()
		if index == instance.selected {
			prefix = " ▸"
			style = selectedStyle
		}
		line := fmt.Sprintf("%-26s %-18s %-10s %d",
			truncateString(agent.ID, 24),
			truncateString(agent.SiteID, 16),
			renderStatus(agent.Status),
			agent.QueueDepth)
		lines = append(lines, style.Width(width).Render(prefix+line))
	}

	return lines
}

// ─── Events Dedicated List View (Admin) ────────────────────────────────────────

func (instance model) eventsListView(width int) []string {
	var lines []string
	lines = append(lines, sectionStyle.Render("Audit Trail Events (Admin View)"))
	lines = append(lines, fmt.Sprintf("  %s %s", labelStyle.Render("Events recorded:"), valueStyle.Render(fmt.Sprint(len(instance.events)))))
	lines = append(lines, "")

	if len(instance.events) == 0 {
		lines = append(lines, quietStyle.Render("  Aucun événement d'audit enregistré."))
		return lines
	}

	headerLine := fmt.Sprintf("  %-10s %-24s %-20s %s", "TIME", "EVENT ID", "TYPE", "JOB ID")
	lines = append(lines, labelStyle.Width(width).Render(headerLine))
	lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", minInt(width-4, 70))))

	for index, event := range instance.events {
		prefix := "  "
		style := lipgloss.NewStyle()
		if index == instance.selected {
			prefix = " ▸"
			style = selectedStyle
		}
		timeStr := "—"
		if !event.Timestamp.IsZero() {
			timeStr = event.Timestamp.Local().Format("15:04:05")
		}
		jobStr := "—"
		if event.JobID != "" {
			jobStr = truncateString(event.JobID, 16)
		}
		line := fmt.Sprintf("%-10s %-24s %-20s %s",
			timeStr,
			truncateString(event.EventID, 22),
			truncateString(event.Type, 18),
			jobStr)
		lines = append(lines, style.Width(width).Render(prefix+line))
	}

	return lines
}

// ─── Logs Dedicated List View (Admin) ──────────────────────────────────────────

func (instance model) logsListView(width int) []string {
	var lines []string
	lines = append(lines, sectionStyle.Render("Technical Logs (Admin View)"))
	lines = append(lines, fmt.Sprintf("  %s %s", labelStyle.Render("Entries:"), valueStyle.Render(fmt.Sprint(len(instance.logs)))))
	lines = append(lines, "")

	if len(instance.logs) == 0 {
		lines = append(lines, quietStyle.Render("  Aucun journal technique disponible."))
		return lines
	}

	headerLine := fmt.Sprintf("  %-10s %-8s %-14s %s", "TIME", "LEVEL", "SERVICE", "MESSAGE")
	lines = append(lines, labelStyle.Width(width).Render(headerLine))
	lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", minInt(width-4, 70))))

	for index, logEntry := range instance.logs {
		prefix := "  "
		style := lipgloss.NewStyle()
		if index == instance.selected {
			prefix = " ▸"
			style = selectedStyle
		}
		timeStr := "—"
		if !logEntry.Timestamp.IsZero() {
			timeStr = logEntry.Timestamp.Local().Format("15:04:05")
		}
		levelStyle := quietStyle
		switch logEntry.Level {
		case "ERROR":
			levelStyle = failedStyle
		case "WARN":
			levelStyle = queuedStyle
		case "INFO":
			levelStyle = valueStyle
		}

		line := fmt.Sprintf("%-10s %-8s %-14s %s",
			timeStr,
			levelStyle.Render(logEntry.Level),
			truncateString(logEntry.Service, 12),
			truncateString(observability.Redact(logEntry.Message), 32))
		lines = append(lines, style.Width(width).Render(prefix+line))
	}

	return lines
}

// ─── Executions List View ──────────────────────────────────────────────────────

func (instance model) executionListView(width int) []string {
	filter := instance.statusFilter
	if filter == "" {
		filter = "all"
	}

	var lines []string
	lines = append(lines, sectionStyle.Render("Executions"))

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

	page := 1
	if executionPageSize > 0 {
		page = instance.offset/executionPageSize + 1
	}
	lines = append(lines, "")
	lines = append(lines, quietStyle.Render(fmt.Sprintf("  Page %d · records %d–%d of %d",
		page, instance.offset+1, instance.offset+len(instance.executions), instance.total)))

	return lines
}

// ─── Execution Detail View ─────────────────────────────────────────────────────

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

	visibleLines := instance.height - 6
	if visibleLines < 5 {
		visibleLines = 5
	}
	scroll := instance.detailScroll
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
		return "↑/↓ select  enter view  n/p page  f filter  r refresh  esc overview  q quit"
	case detailView:
		if instance.confirmCancel {
			return "y request cancellation  n/esc abandon  q quit"
		}
		if instance.execution != nil && !isTerminalStatus(instance.execution.Status) {
			return "c cancel  r refresh  ↑/↓ scroll  esc list  q quit"
		}
		return "r refresh  ↑/↓ scroll  esc list  q quit"
	case jobsView, agentsView, eventsView, logsView, servicesView:
		return "↑/↓ scroll  r refresh  esc overview  q quit"
	default:
		return "r refresh  j jobs  s services  a agents  e events  l logs  x execs  ? help  q quit"
	}
}

// ─── Asynchronous Commands ────────────────────────────────────────────────────

func loadDashboard(instance model) tea.Cmd {
	requestID := instance.requestID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		jobs, err := instance.backend.Jobs(ctx)
		if err != nil {
			return dashboardLoadedMsg{requestID: requestID, err: err}
		}
		var agents []providerhttp.AgentSummary
		var events []providerhttp.EventSummary
		var logs []providerhttp.TechnicalLogEntry
		if instance.user.Role == "admin" {
			agents, _ = instance.backend.Agents(ctx)
			events, _ = instance.backend.Events(ctx)
			logs, _ = instance.backend.Logs(ctx)
		}
		return dashboardLoadedMsg{
			requestID: requestID,
			jobs:      jobs,
			agents:    agents,
			events:    events,
			logs:      logs,
		}
	}
}

func loadExecutions(instance model, requestID ...uint64) tea.Cmd {
	reqID := instance.requestID
	if len(requestID) > 0 {
		reqID = requestID[0]
	}
	status := instance.statusFilter
	offset := instance.offset
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		page, err := instance.backend.Executions(ctx, executionPageSize, offset, status, "")
		return executionsLoadedMsg{requestID: reqID, page: page, err: err}
	}
}

func loadExecution(instance model, id string, requestID uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		execution, err := instance.backend.Execution(ctx, id)
		return executionLoadedMsg{requestID: requestID, execution: execution, err: err}
	}
}

func cancelExecution(instance model, id string, requestID uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		execution, err := instance.backend.CancelExecution(ctx, id)
		return executionCancelledMsg{requestID: requestID, execution: execution, err: err}
	}
}

func (instance model) executionActive() bool {
	return instance.execution != nil && !isTerminalStatus(instance.execution.Status)
}

func refreshAfter(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(now time.Time) tea.Msg { return refreshTickMsg(now) })
}

// ─── Helpers ───────────────────────────────────────────────────────────────────

func renderStatus(status string) string {
	label := strings.ToUpper(status)
	switch status {
	case "queued", "retrying", "blocked", "offline":
		return queuedStyle.Render(label)
	case "running":
		return runningStyle.Render(label)
	case "succeeded", "completed", "planned", "ready", "online":
		return successStyle.Render(label)
	case "failed", "error":
		return failedStyle.Render(label)
	case "cancelled":
		return cancelledStyle.Render(label)
	case "timed_out":
		return timedOutStyle.Render(label)
	default:
		return quietStyle.Render(label)
	}
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

func compactJSON(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	text := strings.TrimSpace(string(data))
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\t", " ")
	return truncateString(observability.Redact(text), 80)
}

func displayAPIError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if strings.Contains(message, "401") || strings.Contains(message, "unauthorized") {
		return "Session expired or credentials rejected. Sign in again."
	}
	if strings.Contains(message, "403") || strings.Contains(message, "forbidden") {
		return "Action requires administrative privileges."
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
