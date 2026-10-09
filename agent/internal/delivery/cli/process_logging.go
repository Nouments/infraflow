package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"infraflow/agent/internal/adapters/providergrpc"
	"infraflow/agent/internal/application/toolrunner"
	"infraflow/agent/internal/config"
	"infraflow/agent/internal/ports"
	"infraflow/pkg/observability"
)

const processLogSyncInterval = 250 * time.Millisecond

type processLogSession struct {
	agentID       string
	siteID        string
	runID         string
	logger        *observability.Logger
	outbox        *observability.Outbox
	reporter      ports.LogReporter
	closeReporter func() error
	mu            sync.Mutex
	syncMu        sync.Mutex
	firstErr      error
	cancel        context.CancelFunc
	done          chan struct{}
}

func newProcessLogSession(configPath string) (*processLogSession, error) {
	if configPath == "" {
		return nil, nil
	}
	settings, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("load process logging configuration: %w", err)
	}
	token := os.Getenv(settings.Provider.TokenEnv)
	if token == "" {
		return nil, fmt.Errorf("provider token environment variable %s is empty", settings.Provider.TokenEnv)
	}
	reporter, err := providergrpc.New(settings.Provider.Address, token, settings.Provider.TLS.Enabled, settings.Provider.TLS.CAFile)
	if err != nil {
		return nil, fmt.Errorf("configure process log transport: %w", err)
	}
	session, err := newProcessLogSessionWithReporter(settings, reporter)
	if err != nil {
		_ = reporter.Close()
		return nil, err
	}
	session.closeReporter = reporter.Close
	return session, nil
}

func newProcessLogSessionWithReporter(settings config.Config, reporter ports.LogReporter) (*processLogSession, error) {
	if reporter == nil {
		return nil, fmt.Errorf("process log reporter is required")
	}
	runID, err := observability.NewEventID()
	if err != nil {
		return nil, fmt.Errorf("create process run id: %w", err)
	}
	siteID := settings.Agent.SiteID
	if siteID == "" {
		siteID = settings.Agent.ID
	}
	outbox, err := observability.NewOutbox(filepath.Join(settings.Logging.Directory, "process-pending-events.json"), observability.DefaultOutboxLimit)
	if err != nil {
		return nil, fmt.Errorf("initialize process log outbox: %w", err)
	}
	logger, err := observability.New(observability.Config{
		Service: "agent", Directory: settings.Logging.Directory,
		Level: settings.Logging.Level, Format: settings.Logging.Format,
		MaxBytes: settings.Logging.MaxBytes, MaxFiles: settings.Logging.MaxFiles,
		Sink: outbox,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize process logger: %w", err)
	}
	return &processLogSession{
		agentID: settings.Agent.ID, siteID: siteID, runID: runID,
		logger: logger, outbox: outbox, reporter: reporter,
	}, nil
}

func (session *processLogSession) Start(parent context.Context) {
	if session == nil || session.done != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	session.cancel = cancel
	session.done = make(chan struct{})
	go func() {
		defer close(session.done)
		ticker := time.NewTicker(processLogSyncInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = session.syncPending(ctx)
			}
		}
	}()
}

func (session *processLogSession) Observe(output toolrunner.ProcessOutput) {
	if session == nil {
		return
	}
	if output.Started {
		if err := session.logger.Emit(context.Background(), observability.Event{
			Level: "INFO", Event: "process.started", Message: "external process started",
			Service: "agent", Source: "process-runner", AgentID: session.agentID,
			SiteID: session.siteID, RunID: session.runID, Operation: "tool-execution", Status: "RUNNING",
		}); err != nil {
			session.recordError(err)
		}
		return
	}
	level := "INFO"
	if output.Stream == "stderr" {
		level = "WARN"
	}
	status, message := "RUNNING", "external process output"
	if output.Truncated {
		status, message = "TRUNCATED", "external process output line truncated"
	}
	if err := session.logger.Emit(context.Background(), observability.Event{
		Level: level, Event: "process.output", Message: message,
		Service: "agent", Source: "process-runner", AgentID: session.agentID,
		SiteID: session.siteID, RunID: session.runID, Operation: "tool-execution",
		Status: status, Stream: output.Stream,
		Line: strings.TrimSuffix(output.Text, "\n"),
	}); err != nil {
		session.recordError(err)
	}
}

func (session *processLogSession) Complete(operation string, processErr error) {
	if session == nil {
		return
	}
	level, eventName, status, message := "INFO", "process.completed", "COMPLETED", "external process completed"
	var errorText string
	if processErr != nil {
		level, eventName, status, message = "ERROR", "process.failed", "FAILED", "external process failed"
		errorText = processErr.Error()
	}
	if err := session.logger.Emit(context.Background(), observability.Event{
		Level: level, Event: eventName, Message: message, Error: errorText,
		Service: "agent", Source: "process-runner", AgentID: session.agentID,
		SiteID: session.siteID, RunID: session.runID, Operation: operation, Status: status,
	}); err != nil {
		session.recordError(err)
	}
}

func (session *processLogSession) Close() error {
	if session == nil {
		return nil
	}
	if session.cancel != nil {
		session.cancel()
		<-session.done
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	syncErr := session.syncPending(ctx)
	cancel()
	closeErr := session.logger.Close()
	if session.closeReporter != nil {
		closeErr = errors.Join(closeErr, session.closeReporter())
	}
	session.mu.Lock()
	firstErr := session.firstErr
	session.mu.Unlock()
	if syncErr != nil || closeErr != nil || firstErr != nil {
		return fmt.Errorf("process logs could not be fully synchronized: %s", observability.Redact(errors.Join(syncErr, closeErr, firstErr).Error()))
	}
	return nil
}

func (session *processLogSession) syncPending(parent context.Context) error {
	session.syncMu.Lock()
	defer session.syncMu.Unlock()
	for {
		events, err := session.outbox.Pending(observability.MaxIngestBatch)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		ctx, cancel := context.WithTimeout(parent, 3*time.Second)
		err = session.reporter.ReportLogs(ctx, session.agentID, events)
		cancel()
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(events))
		for _, event := range events {
			ids = append(ids, event.ID)
		}
		if err := session.outbox.Ack(ids); err != nil {
			return err
		}
	}
}

func (session *processLogSession) recordError(err error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.firstErr == nil {
		session.firstErr = err
	}
}
