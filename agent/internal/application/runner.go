package application

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"infraflow/pkg/protocol"
)

type Provider interface {
	Catalog(context.Context) ([]protocol.Artifact, error)
	Download(context.Context, protocol.Artifact, io.Writer) error
	Report(context.Context, protocol.AgentReport) error
}

type StateStore interface {
	SaveArtifact(path, expectedHash string, source io.Reader) error
	OpenArtifact(path string) (io.ReadCloser, error)
	SaveReport(protocol.AgentReport) error
}

type ArtifactProcessor interface {
	Process(protocol.Artifact, io.Reader) protocol.ArtifactResult
}

type Runner struct {
	agentID   string
	provider  Provider
	state     StateStore
	processor ArtifactProcessor
}

func NewRunner(agentID string, provider Provider, state StateStore, processor ArtifactProcessor) (*Runner, error) {
	if !protocol.ValidSiteName(agentID) || len(agentID) > 128 {
		return nil, fmt.Errorf("invalid agent id")
	}
	if provider == nil || state == nil || processor == nil {
		return nil, fmt.Errorf("provider, state store, and artifact processor are required")
	}
	return &Runner{agentID: agentID, provider: provider, state: state, processor: processor}, nil
}

func (runner *Runner) Run(ctx context.Context) (protocol.AgentReport, error) {
	artifacts, err := runner.provider.Catalog(ctx)
	if err != nil {
		return protocol.AgentReport{}, fmt.Errorf("load provider catalog: %w", err)
	}
	if err := validateCatalog(artifacts); err != nil {
		return protocol.AgentReport{}, err
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	report := protocol.AgentReport{AgentID: runner.agentID, ReportedAt: time.Now().UTC()}
	failed := false
	for _, artifact := range artifacts {
		result := runner.execute(ctx, artifact)
		report.Artifacts = append(report.Artifacts, result)
		if result.Status != protocol.StatusCompleted {
			failed = true
		}
	}
	report.ReportID = protocol.ComputeReportID(report)
	if err := runner.state.SaveReport(report); err != nil {
		return report, fmt.Errorf("save local execution state: %w", err)
	}
	if err := runner.provider.Report(ctx, report); err != nil {
		return report, fmt.Errorf("report execution state: %w", err)
	}
	if failed {
		return report, fmt.Errorf("one or more artifacts were not completed")
	}
	return report, nil
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
