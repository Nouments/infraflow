package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"infraflow/agent/internal/adapters/providerhttp"
	"infraflow/pkg/observability"
)

type Backend interface {
	Login(context.Context, string, string) (providerhttp.UserSession, error)
	Logout(context.Context) error
	CurrentUser(context.Context) (providerhttp.User, error)
	Jobs(context.Context) ([]providerhttp.JobSummary, error)
	Agents(context.Context) ([]providerhttp.AgentSummary, error)
	Events(context.Context) ([]providerhttp.EventSummary, error)
	Logs(context.Context) ([]providerhttp.TechnicalLogEntry, error)
}

// Run starts the line-oriented Linux TUI. It deliberately uses standard
// terminal input/output so it remains easy to operate over SSH and needs no
// terminal framework or dashboard service.
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
	if _, err := io.WriteString(output, "INFRAFLOW TUI\n\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Server session: %s\nUser: %s (%s)\nSession expires: %s\n\n", maskedSession(session.Token), user.Username, user.Role, session.ExpiresAt.UTC().Format("2006-01-02 15:04:05 UTC")); err != nil {
		return err
	}
	if err := refresh(ctx, output, backend, user.Role == "admin"); err != nil {
		return err
	}
	if _, err := io.WriteString(output, "\nCommands: r refresh | j jobs | a agents | e events | l logs | q quit | h help\n> "); err != nil {
		return err
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 1024), 64<<10)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		command := strings.ToLower(strings.TrimSpace(scanner.Text()))
		switch command {
		case "q", "quit", "exit":
			return nil
		case "", "r", "refresh":
			if err := refresh(ctx, output, backend, user.Role == "admin"); err != nil {
				return err
			}
		case "j", "jobs":
			if err := printJobs(ctx, output, backend); err != nil {
				return err
			}
		case "a", "agents":
			if user.Role != "admin" {
				_, _ = io.WriteString(output, "Permission denied: admin role required.\n")
			} else if err := printAgents(ctx, output, backend); err != nil {
				return err
			}
		case "e", "events":
			if user.Role != "admin" {
				_, _ = io.WriteString(output, "Permission denied: admin role required.\n")
			} else if err := printEvents(ctx, output, backend); err != nil {
				return err
			}
		case "l", "logs":
			if user.Role != "admin" {
				_, _ = io.WriteString(output, "Permission denied: admin role required.\n")
			} else if err := printTechnicalLogs(ctx, output, backend); err != nil {
				return err
			}
		case "h", "help":
			_, _ = io.WriteString(output, "Commands: r refresh | j jobs | a agents | e events | l logs | q quit | h help\n")
		default:
			_, _ = io.WriteString(output, "Unknown command. Use h for help.\n")
		}
		if _, err := io.WriteString(output, "> "); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func refresh(ctx context.Context, output io.Writer, backend Backend, admin bool) error {
	if err := printJobs(ctx, output, backend); err != nil {
		return err
	}
	if admin {
		if err := printAgents(ctx, output, backend); err != nil {
			return err
		}
		return printEvents(ctx, output, backend)
	}
	return nil
}

func printEvents(ctx context.Context, output io.Writer, backend Backend) error {
	events, err := backend.Events(ctx)
	if err != nil {
		return fmt.Errorf("load events: %w", err)
	}
	_, _ = io.WriteString(output, "\nRECENT RESULTS\n")
	if len(events) == 0 {
		_, _ = io.WriteString(output, "  no generation or execution results\n")
		return nil
	}
	start := len(events) - 10
	if start < 0 {
		start = 0
	}
	for index := len(events) - 1; index >= start; index-- {
		event := events[index]
		var result struct {
			PlanID             string `json:"plan_id"`
			GenerationStatus   string `json:"generation_status"`
			ExecutionStatus    string `json:"execution_status"`
			VerificationStatus string `json:"verification_status"`
		}
		_ = json.Unmarshal(event.Payload, &result)
		if _, err := fmt.Fprintf(output, "  %s %s generation=%s execution=%s verification=%s\n",
			event.Timestamp.UTC().Format("2006-01-02 15:04:05"), event.Type,
			valueOrUnknown(result.GenerationStatus), valueOrUnknown(result.ExecutionStatus), valueOrUnknown(result.VerificationStatus)); err != nil {
			return err
		}
	}
	return nil
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "not-reported"
	}
	return value
}

func printTechnicalLogs(ctx context.Context, output io.Writer, backend Backend) error {
	logs, err := backend.Logs(ctx)
	if err != nil {
		return fmt.Errorf("load technical logs: %w", err)
	}
	_, _ = io.WriteString(output, "\nTECHNICAL LOGS\n")
	if len(logs) == 0 {
		_, _ = io.WriteString(output, "  no technical logs\n")
		return nil
	}
	for _, log := range logs {
		message := firstNonEmpty(log.Message, log.Error, log.Line, log.Event, "n/a")
		if _, err := fmt.Fprintf(output, "  %s %-5s %-12s %s\n",
			log.Timestamp.UTC().Format("2006-01-02 15:04:05"),
			observability.Redact(strings.ToUpper(log.Level)),
			observability.Redact(strings.TrimSpace(log.Service)),
			observability.Redact(message)); err != nil {
			return err
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func printJobs(ctx context.Context, output io.Writer, backend Backend) error {
	jobs, err := backend.Jobs(ctx)
	if err != nil {
		return fmt.Errorf("load jobs: %w", err)
	}
	_, _ = io.WriteString(output, "\nJOBS\n")
	if len(jobs) == 0 {
		_, _ = io.WriteString(output, "  no planning jobs\n")
		return nil
	}
	for _, job := range jobs {
		if _, err := fmt.Fprintf(output, "  %-24s %-10s %s\n", job.ID, job.Status, job.UpdatedAt.UTC().Format("2006-01-02 15:04:05")); err != nil {
			return err
		}
	}
	return nil
}

func printAgents(ctx context.Context, output io.Writer, backend Backend) error {
	agents, err := backend.Agents(ctx)
	if err != nil {
		return fmt.Errorf("load agents: %w", err)
	}
	_, _ = io.WriteString(output, "\nAGENTS\n")
	if len(agents) == 0 {
		_, _ = io.WriteString(output, "  no registered agents\n")
		return nil
	}
	for _, agent := range agents {
		if _, err := fmt.Fprintf(output, "  %-24s %-10s site=%s queue=%d\n", agent.ID, agent.Status, agent.SiteID, agent.QueueDepth); err != nil {
			return err
		}
	}
	return nil
}

func maskedSession(token string) string {
	if len(token) < 8 {
		return "session"
	}
	return token[:4] + "..." + token[len(token)-4:]
}
