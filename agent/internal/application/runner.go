package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"infraflow/agent/internal/ports"
	"infraflow/pkg/observability"
	"infraflow/pkg/protocol"
)

type Runner struct {
	agentID   string
	provider  ports.Provider
	state     ports.StateStore
	processor ports.ArtifactProcessor
	logger    *observability.Logger
	outbox    *observability.Outbox
	siteID    string
}

func NewRunner(agentID string, provider ports.Provider, state ports.StateStore, processor ports.ArtifactProcessor) (*Runner, error) {
	if !protocol.ValidSiteName(agentID) || len(agentID) > 128 {
		return nil, fmt.Errorf("invalid agent id")
	}
	if provider == nil || state == nil || processor == nil {
		return nil, fmt.Errorf("provider, state store, and artifact processor are required")
	}
	return &Runner{agentID: agentID, provider: provider, state: state, processor: processor}, nil
}

func (runner *Runner) SetObservability(logger *observability.Logger, outbox *observability.Outbox, siteID string) {
	runner.logger = logger
	runner.outbox = outbox
	runner.siteID = siteID
}

func (runner *Runner) Run(ctx context.Context) (protocol.AgentReport, error) {
	runID, err := observability.NewEventID()
	if err != nil {
		return protocol.AgentReport{}, fmt.Errorf("create run id: %w", err)
	}
	started := time.Now().UTC()
	if err := runner.emit(ctx, runID, "agent.run.started", "agent run started", "download", "RUNNING", "", nil, nil); err != nil {
		return protocol.AgentReport{}, err
	}
	var syncErr error
	if err := runner.syncLogs(ctx); err != nil {
		syncErr = err
	}
	artifacts, err := runner.provider.Catalog(ctx)
	if err != nil {
		operationErr := fmt.Errorf("load provider catalog: %w", err)
		logErr := runner.emit(ctx, runID, "agent.catalog.failed", "provider catalog request failed", "download", "FAILED", "", operationErr, nil)
		_ = runner.emit(ctx, runID, "agent.run.failed", "agent run failed", "download", "FAILED", "", operationErr, &started)
		return protocol.AgentReport{}, errors.Join(operationErr, logErr, runner.syncLogs(ctx), syncErr)
	}
	if err := validateCatalog(artifacts); err != nil {
		logErr := runner.emit(ctx, runID, "agent.catalog.invalid", "provider catalog validation failed", "download", "FAILED", "", err, nil)
		_ = runner.emit(ctx, runID, "agent.run.failed", "agent run failed", "download", "FAILED", "", err, &started)
		return protocol.AgentReport{}, errors.Join(err, logErr, runner.syncLogs(ctx), syncErr)
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	report := protocol.AgentReport{AgentID: runner.agentID, ReportedAt: time.Now().UTC()}
	failed := false
	for _, artifact := range artifacts {
		if err := runner.emit(ctx, runID, "agent.artifact.started", "artifact processing started", "download", "RUNNING", artifact.Path, nil, nil); err != nil {
			return report, errors.Join(err, runner.syncLogs(ctx))
		}
		result := runner.execute(ctx, artifact)
		report.Artifacts = append(report.Artifacts, result)
		status := strings.ToUpper(result.Status)
		var resultErr error
		if result.Status != protocol.StatusCompleted {
			resultErr = errors.New(result.Message)
		}
		if err := runner.emit(ctx, runID, "agent.artifact.completed", "artifact processing finished", "download", status, artifact.Path, resultErr, nil); err != nil {
			return report, errors.Join(err, runner.syncLogs(ctx))
		}
		if result.Status != protocol.StatusCompleted {
			failed = true
		}
	}
	report.ReportID = protocol.ComputeReportID(report)
	if err := runner.state.SaveReport(report); err != nil {
		operationErr := fmt.Errorf("save local execution state: %w", err)
		logErr := runner.emit(ctx, runID, "agent.report.persist_failed", "agent report could not be persisted locally", "report", "FAILED", "", operationErr, nil)
		return report, errors.Join(operationErr, logErr, runner.syncLogs(ctx), syncErr)
	}
	if err := runner.provider.Report(ctx, report); err != nil {
		operationErr := fmt.Errorf("report execution state: %w", err)
		logErr := runner.emit(ctx, runID, "agent.report.submit_failed", "agent report could not be submitted", "report", "FAILED", "", operationErr, nil)
		return report, errors.Join(operationErr, logErr, runner.syncLogs(ctx), syncErr)
	}
	if failed {
		operationErr := errors.New("one or more artifacts were not completed")
		logErr := runner.emit(ctx, runID, "agent.run.failed", "one or more artifacts were not completed", "download", "FAILED", "", operationErr, &started)
		return report, errors.Join(operationErr, logErr, runner.syncLogs(ctx), syncErr)
	}
	if err := runner.emit(ctx, runID, "agent.run.completed", "agent artifact run completed", "download", "COMPLETED", "", nil, &started); err != nil {
		return report, errors.Join(err, runner.syncLogs(ctx), syncErr)
	}
	return report, errors.Join(runner.syncLogs(ctx), syncErr)
}

func (runner *Runner) emit(ctx context.Context, runID, eventName, message, operation, status, detail string, eventErr error, durationStart *time.Time) error {
	if runner.logger == nil {
		return nil
	}
	id, err := observability.NewEventID()
	if err != nil {
		return fmt.Errorf("create agent log event id: %w", err)
	}
	event := observability.Event{
		ID: id, Level: "INFO", Event: eventName, Message: message,
		Service: "agent", Source: "agent-runner", AgentID: runner.agentID,
		SiteID: runner.siteID, RunID: runID, CorrelationID: runID,
		Operation: operation, Status: status, Line: detail,
	}
	if status == "FAILED" {
		event.Level = "ERROR"
	}
	if eventErr != nil {
		event.Error = eventErr.Error()
	}
	if durationStart != nil {
		duration := time.Since(*durationStart).Milliseconds()
		event.DurationMS = &duration
	}
	return runner.logger.Emit(ctx, event)
}

func (runner *Runner) syncLogs(ctx context.Context) error {
	if runner.outbox == nil {
		return nil
	}
	reporter, ok := runner.provider.(ports.LogReporter)
	if !ok {
		return nil
	}
	for {
		events, err := runner.outbox.Pending(observability.MaxIngestBatch)
		if err != nil {
			return fmt.Errorf("read local log outbox: %w", err)
		}
		if len(events) == 0 {
			return nil
		}
		if err := reporter.ReportLogs(ctx, runner.agentID, events); err != nil {
			return fmt.Errorf("synchronize local logs; events remain queued: %w", err)
		}
		ids := make([]string, 0, len(events))
		for _, event := range events {
			ids = append(ids, event.ID)
		}
		if err := runner.outbox.Ack(ids); err != nil {
			return fmt.Errorf("acknowledge synchronized local logs: %w", err)
		}
	}
}

func (runner *Runner) execute(ctx context.Context, artifact protocol.Artifact) protocol.ArtifactResult {
	result := protocol.ArtifactResult{Path: artifact.Path, OutputHash: artifact.OutputHash, Status: protocol.StatusFailed}
	pipeReader, pipeWriter := io.Pipe()
	downloadDone := make(chan error, 1)
	go func() {
		err := runner.provider.Download(ctx, artifact, pipeWriter)
		if err != nil {
			_ = pipeWriter.CloseWithError(err)
		} else {
			_ = pipeWriter.Close()
		}
		downloadDone <- err
	}()
	storeErr := runner.state.SaveArtifact(artifact.Path, artifact.OutputHash, pipeReader)
	_ = pipeReader.Close()
	downloadErr := <-downloadDone
	if storeErr != nil || downloadErr != nil {
		result.Message = "artifact download failed"
		return result
	}
	artifactReader, err := runner.state.OpenArtifact(artifact.Path)
	if err != nil {
		result.Message = "verified artifact could not be opened"
		return result
	}
	defer artifactReader.Close()
	result = runner.processor.Process(artifact, artifactReader)
	if result.Path != artifact.Path || result.OutputHash != artifact.OutputHash {
		return protocol.ArtifactResult{
			Path: artifact.Path, OutputHash: artifact.OutputHash,
			Status: protocol.StatusFailed, Message: "artifact processor returned mismatched state",
		}
	}
	if result.Status != protocol.StatusCompleted && result.Status != protocol.StatusFailed && result.Status != protocol.StatusBlocked {
		return protocol.ArtifactResult{
			Path: artifact.Path, OutputHash: artifact.OutputHash,
			Status: protocol.StatusFailed, Message: "artifact processor returned an invalid status",
		}
	}
	if len(result.Message) > 1024 {
		result.Message = strings.TrimSpace(result.Message[:1024])
	}
	return result
}

func validateCatalog(artifacts []protocol.Artifact) error {
	seen := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		site, _, found := strings.Cut(artifact.Path, "/")
		if !found || !protocol.ValidArtifactPath(site, artifact) || !protocol.IsSHA256(artifact.InputHash) || !protocol.IsSHA256(artifact.OutputHash) {
			return fmt.Errorf("provider catalog contains invalid artifact metadata")
		}
		if _, exists := seen[artifact.Path]; exists {
			return fmt.Errorf("provider catalog contains duplicate artifact paths")
		}
		seen[artifact.Path] = struct{}{}
	}
	return nil
}
