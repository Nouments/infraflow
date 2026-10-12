package tui

import (
	"context"
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"

	"infraflow/agent/internal/adapters/providerhttp"
)

// AgentInfo captures local agent configuration and runtime environment.
type AgentInfo struct {
	ID              string
	SiteID          string
	Capabilities    []string
	StateDirectory  string
	Configured      bool
	ConfigFile      string
	ProviderAddress string
	APIAddress      string
	LogDirectory    string
}

// Run authenticates with the existing Provider session, then starts the
// asynchronous Bubble Tea interface used for all TUI views.
func Run(ctx context.Context, input io.Reader, output io.Writer, backend Backend, username, password string) error {
	return RunWithAgent(ctx, input, output, backend, username, password, AgentInfo{})
}

// RunWithAgent runs the TUI with optional local agent configuration.
// If the Provider is initially unreachable but local agent configuration is available,
// it launches in degraded local mode rather than abruptly crashing.
func RunWithAgent(ctx context.Context, input io.Reader, output io.Writer, backend Backend, username, password string, agentInfo AgentInfo) error {
	if backend == nil {
		return fmt.Errorf("TUI backend is required")
	}
	var (
		user    providerhttp.User
		session providerhttp.UserSession
		initErr error
	)
	session, err := backend.Login(ctx, username, password)
	if err != nil {
		initErr = fmt.Errorf("Provider authentication unavailable: %w", err)
		user = providerhttp.User{
			Username: username,
			Role:     "user",
		}
	} else {
		defer backend.Logout(context.Background())
		user, err = backend.CurrentUser(ctx)
		if err != nil {
			initErr = fmt.Errorf("load current user: %w", err)
			user = providerhttp.User{
				Username: username,
				Role:     "user",
			}
		}
	}

	m := newModel(ctx, backend, user, session, agentInfo)
	if initErr != nil {
		m.loadError = initErr.Error()
	}

	program := tea.NewProgram(
		m,
		tea.WithInput(input),
		tea.WithOutput(output),
		tea.WithAltScreen(),
	)
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("run InfraFlow TUI: %w", err)
	}
	return nil
}

var _ Backend = (*providerhttp.UserClient)(nil)
