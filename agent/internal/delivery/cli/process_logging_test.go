package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"infraflow/agent/internal/application/toolrunner"
	"infraflow/agent/internal/config"
	"infraflow/pkg/observability"
)

type recordingLogReporter struct {
	agentID string
	events  []observability.Event
	err     error
}

func (reporter *recordingLogReporter) ReportLogs(_ context.Context, agentID string, events []observability.Event) error {
	reporter.agentID = agentID
	reporter.events = append(reporter.events, events...)
	return reporter.err
}

func TestProcessLogSessionSyncsRedactedProcessEvents(t *testing.T) {
	logConfig, err := observability.DefaultConfig("agent")
	if err != nil {
		t.Fatal(err)
	}
	logConfig.Directory = t.TempDir()
	settings := config.Config{
		Agent: config.AgentConfig{ID: "agent-01", SiteID: "site-01"},
		Logging: config.LoggingConfig{
			Directory: logConfig.Directory, Level: "DEBUG", Format: "json",
			MaxBytes: logConfig.MaxBytes, MaxFiles: logConfig.MaxFiles,
		},
	}
	reporter := &recordingLogReporter{}
	session, err := newProcessLogSessionWithReporter(settings, reporter)
	if err != nil {
		t.Fatal(err)
	}
	session.Start(context.Background())
	session.Start(context.Background())
	session.Observe(toolrunner.ProcessOutput{Started: true})
	session.Observe(toolrunner.ProcessOutput{Stream: "stdout", Text: "token=process-secret\n"})
	session.Observe(toolrunner.ProcessOutput{Stream: "stderr", Text: "truncated output\n", Truncated: true})
	session.Complete("validation", nil)
	session.Complete("execution", errors.New("password=process-secret"))
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if reporter.agentID != "agent-01" || len(reporter.events) < 5 {
		t.Fatalf("process events were not synchronized for the configured Agent: %#v", reporter)
	}
	for _, event := range reporter.events {
		encoded := event.Message + event.Line + event.Error
		if strings.Contains(encoded, "process-secret") {
			t.Fatalf("process secret leaked into a synchronized event: %#v", event)
		}
	}
}

func TestProcessLogSessionSurfacesSyncFailuresWithoutLeakingSecrets(t *testing.T) {
	logConfig, err := observability.DefaultConfig("agent")
	if err != nil {
		t.Fatal(err)
	}
	settings := config.Config{
		Agent: config.AgentConfig{ID: "agent-01"},
		Logging: config.LoggingConfig{
			Directory: t.TempDir(), Level: "DEBUG", Format: "json",
			MaxBytes: logConfig.MaxBytes, MaxFiles: logConfig.MaxFiles,
		},
	}
	reporter := &recordingLogReporter{err: errors.New("token=reporter-secret")}
	session, err := newProcessLogSessionWithReporter(settings, reporter)
	if err != nil {
		t.Fatal(err)
	}
	session.Observe(toolrunner.ProcessOutput{Stream: "stdout", Text: "work completed\n"})
	err = session.Close()
	if err == nil || !strings.Contains(err.Error(), "[REDACTED]") || strings.Contains(err.Error(), "reporter-secret") {
		t.Fatalf("sync failure was not surfaced safely: %v", err)
	}
}

func TestProcessLogSessionNilLifecycleMethodsAreSafe(t *testing.T) {
	var session *processLogSession
	session.Start(context.Background())
	session.Observe(toolrunner.ProcessOutput{Started: true})
	session.Complete("run", nil)
	if err := session.Close(); err != nil {
		t.Fatalf("closing a nil logging session failed: %v", err)
	}
}
