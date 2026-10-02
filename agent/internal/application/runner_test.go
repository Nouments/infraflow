package application

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"infraflow/pkg/protocol"
)

type fakeProvider struct {
	catalog   []protocol.Artifact
	contents  map[string][]byte
	reports   []protocol.AgentReport
	reportErr error
}

func (provider *fakeProvider) Catalog(context.Context) ([]protocol.Artifact, error) {
	return provider.catalog, nil
}

func (provider *fakeProvider) Download(_ context.Context, artifact protocol.Artifact, destination io.Writer) error {
	data, exists := provider.contents[artifact.Path]
	if !exists {
		return errors.New("not found")
	}
	_, err := destination.Write(data)
	return err
}

func (provider *fakeProvider) Report(_ context.Context, report protocol.AgentReport) error {
	provider.reports = append(provider.reports, report)
	return provider.reportErr
}

type fakeState struct {
	artifacts map[string][]byte
	reports   []protocol.AgentReport
}

func (state *fakeState) SaveArtifact(path, expectedHash string, source io.Reader) error {
	data, err := io.ReadAll(source)
	if err != nil {
		return err
	}
	if protocol.SHA256(data) != expectedHash {
		return errors.New("hash mismatch")
	}
	state.artifacts[path] = data
	return nil
}

func (state *fakeState) OpenArtifact(path string) (io.ReadCloser, error) {
	data, exists := state.artifacts[path]
	if !exists {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (state *fakeState) SaveReport(report protocol.AgentReport) error {
	state.reports = append(state.reports, report)
	return nil
}

type fakeProcessor struct {
	status string
	count  int
}

func (processor *fakeProcessor) Process(artifact protocol.Artifact, _ io.Reader) protocol.ArtifactResult {
	processor.count++
	return protocol.ArtifactResult{Path: artifact.Path, OutputHash: artifact.OutputHash, Status: processor.status}
}

func TestRunnerDownloadsProcessesAndReports(t *testing.T) {
	data := []byte(`{"site":"lab","devices":[]}`)
	artifact := protocol.Artifact{
		Type: "inventory", Path: "lab/inventory.json",
		InputHash: protocol.SHA256([]byte("input")), OutputHash: protocol.SHA256(data),
	}
	provider := &fakeProvider{catalog: []protocol.Artifact{artifact}, contents: map[string][]byte{artifact.Path: data}}
	state := &fakeState{artifacts: make(map[string][]byte)}
	processor := &fakeProcessor{status: protocol.StatusCompleted}
	runner, err := NewRunner("agent-01", provider, state, processor)
	if err != nil {
		t.Fatal(err)
	}
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if processor.count != 1 || len(state.reports) != 1 || len(provider.reports) != 1 {
		t.Fatalf("incomplete run: processed %d, local reports %d, remote reports %d", processor.count, len(state.reports), len(provider.reports))
	}
	if string(state.artifacts[artifact.Path]) != string(data) || report.ReportID != protocol.ComputeReportID(report) {
		t.Fatalf("download or report was not persisted: %#v", report)
	}
}

func TestRunnerReportsHashFailureWithoutProcessing(t *testing.T) {
	artifact := protocol.Artifact{
		Type: "inventory", Path: "lab/inventory.json",
		InputHash: protocol.SHA256([]byte("input")), OutputHash: protocol.SHA256([]byte("expected")),
	}
	provider := &fakeProvider{catalog: []protocol.Artifact{artifact}, contents: map[string][]byte{artifact.Path: []byte("tampered")}}
	state := &fakeState{artifacts: make(map[string][]byte)}
	processor := &fakeProcessor{status: protocol.StatusCompleted}
	runner, err := NewRunner("agent-02", provider, state, processor)
	if err != nil {
		t.Fatal(err)
	}
	report, err := runner.Run(context.Background())
	if err == nil || processor.count != 0 || len(provider.reports) != 1 {
		t.Fatalf("hash failure was not safely reported: err=%v processed=%d reports=%d", err, processor.count, len(provider.reports))
	}
	if len(report.Artifacts) != 1 || report.Artifacts[0].Status != protocol.StatusFailed {
		t.Fatalf("expected failed artifact result, got %#v", report.Artifacts)
	}
}

func TestRunnerRejectsInvalidCatalogBeforeDownloads(t *testing.T) {
	provider := &fakeProvider{catalog: []protocol.Artifact{{Type: "other", Path: "../escape", InputHash: "bad", OutputHash: "bad"}}}
	state := &fakeState{artifacts: make(map[string][]byte)}
	processor := &fakeProcessor{status: protocol.StatusCompleted}
	runner, err := NewRunner("agent-03", provider, state, processor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background()); err == nil || len(provider.reports) != 0 || processor.count != 0 {
		t.Fatalf("invalid catalog was not rejected before processing: err=%v", err)
	}
}
