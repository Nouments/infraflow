package application

import (
	"bytes"
	"io"
	"testing"
	"time"

	"infraflow/internal/domain"
	"infraflow/pkg/protocol"
)

type memoryArtifacts struct {
	items []protocol.Artifact
}

func (repository memoryArtifacts) Catalog() ([]protocol.Artifact, error) {
	return repository.items, nil
}

func (repository memoryArtifacts) Open(path string) (io.ReadCloser, protocol.Artifact, error) {
	for _, artifact := range repository.items {
		if artifact.Path == path {
			return io.NopCloser(bytes.NewReader(nil)), artifact, nil
		}
	}
	return nil, protocol.Artifact{}, nil
}

type memoryReports struct {
	items []protocol.AgentReport
}

type fakeGenerator struct{}

func (fakeGenerator) Generate(domain.Infrastructure, string) ([]protocol.Artifact, error) {
	return []protocol.Artifact{{Type: "inventory"}, {Type: "topology"}}, nil
}

func (repository *memoryReports) Append(report protocol.AgentReport) (bool, error) {
	for _, current := range repository.items {
		if current.ReportID == report.ReportID {
			return false, nil
		}
	}
	repository.items = append(repository.items, report)
	return true, nil
}

func (repository *memoryReports) List() []protocol.AgentReport {
	return repository.items
}

func TestServiceValidatesPlansAndGenerates(t *testing.T) {
	service := NewService(memoryArtifacts{}, &memoryReports{}, fakeGenerator{})
	input := []byte("sites:\n  - name: lab\n    devices:\n      - name: R1\n        vendor: cisco\n        model: ios-xe\n")
	if _, err := service.Validate(input); err != nil {
		t.Fatal(err)
	}
	plan, err := service.Plan(input)
	if err != nil || plan.Status != "blocked" {
		t.Fatalf("expected unsupported provisioning to be blocked: %#v, %v", plan, err)
	}
	artifacts, err := service.Generate(input, "artifact-output")
	if err != nil || len(artifacts) != 2 {
		t.Fatalf("expected generic inventory/topology artifacts: %#v, %v", artifacts, err)
	}
}

func TestServiceSubmitsOnlyCurrentArtifactResults(t *testing.T) {
	artifact := protocol.Artifact{Type: "inventory", Path: "lab/inventory.json", InputHash: "input", OutputHash: "output"}
	repository := &memoryReports{}
	service := NewService(memoryArtifacts{items: []protocol.Artifact{artifact}}, repository, fakeGenerator{})
	report := protocol.AgentReport{
		AgentID: "agent-01", ReportedAt: time.Now().UTC(),
		Artifacts: []protocol.ArtifactResult{{Path: artifact.Path, OutputHash: artifact.OutputHash, Status: protocol.StatusCompleted}},
	}
	report.ReportID = protocol.ComputeReportID(report)
	added, err := service.SubmitReport(report)
	if err != nil || !added || len(service.Reports()) != 1 {
		t.Fatalf("valid report was not accepted: added=%v err=%v", added, err)
	}
	stale := report
	stale.Artifacts = append([]protocol.ArtifactResult(nil), report.Artifacts...)
	stale.Artifacts[0].OutputHash = "stale"
	stale.ReportID = protocol.ComputeReportID(stale)
	if _, err := service.SubmitReport(stale); err == nil {
		t.Fatal("stale artifact report should be rejected")
	}
}
