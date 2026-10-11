package tui

import (
	"context"
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"

	"infraflow/agent/internal/adapters/providerhttp"
)

// Run authenticates with the existing Provider session, then starts the
// asynchronous Bubble Tea interface used for all TUI views.
func Run(ctx context.Context, input io.Reader, output io.Writer, backend Backend, username, password string) error {
	if backend == nil {
		return fmt.Errorf("TUI backend is required")
	}
	session, err := backend.Login(ctx, username, password)
	if err != nil {
		return fmt.Errorf("TUI login failed: %w", err)
	}
	defer backend.Logout(context.Background())
	user, err := backend.CurrentUser(ctx)
	if err != nil {
		return fmt.Errorf("load current user: %w", err)
	}
	if user.Role != "admin" && user.Role != "user" {
		return fmt.Errorf("Provider returned an invalid user role")
	}
	program := tea.NewProgram(
		newModel(ctx, backend, user, session),
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
