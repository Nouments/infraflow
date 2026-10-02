package application

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"infraflow/internal/config"
	"infraflow/internal/domain"
	"infraflow/internal/planner"
	"infraflow/pkg/protocol"
)

var ErrInvalidAgentReport = errors.New("invalid agent report")

type ArtifactRepository interface {
	Catalog() ([]protocol.Artifact, error)
	Open(path string) (io.ReadCloser, protocol.Artifact, error)
}

type ReportRepository interface {
	Append(protocol.AgentReport) (bool, error)
	List() []protocol.AgentReport
}

type ArtifactGenerator interface {
	Generate(domain.Infrastructure, string) ([]protocol.Artifact, error)
}

type Service struct {
	artifacts ArtifactRepository
	reports   ReportRepository
	generator ArtifactGenerator
}

func NewService(artifacts ArtifactRepository, reports ReportRepository, generator ArtifactGenerator) *Service {
	return &Service{artifacts: artifacts, reports: reports, generator: generator}
}

func (service *Service) Validate(input []byte) (domain.Infrastructure, error) {
	return config.Parse(input)
}

func (service *Service) Plan(input []byte) (domain.Plan, error) {
	infrastructure, err := service.Validate(input)
	if err != nil {
		return domain.Plan{}, err
	}
	return planner.Build(infrastructure), nil
}

func (service *Service) Generate(input []byte, outputDirectory string) ([]protocol.Artifact, error) {
	infrastructure, err := service.Validate(input)
	if err != nil {
		return nil, err
	}
	if service.generator == nil {
		return nil, fmt.Errorf("artifact generator is not configured")
	}
	return service.generator.Generate(infrastructure, outputDirectory)
}

func (service *Service) Catalog() ([]protocol.Artifact, error) {
	return service.artifacts.Catalog()
}

func (service *Service) OpenArtifact(path string) (io.ReadCloser, protocol.Artifact, error) {
	return service.artifacts.Open(path)
}

func (service *Service) SubmitReport(report protocol.AgentReport) (bool, error) {
	if !protocol.ValidSiteName(report.AgentID) || len(report.AgentID) > 128 || report.ReportedAt.IsZero() || report.ReportID != protocol.ComputeReportID(report) || len(report.Artifacts) > 10000 {
		return false, fmt.Errorf("%w: invalid metadata", ErrInvalidAgentReport)
	}
	catalog, err := service.artifacts.Catalog()
	if err != nil {
		return false, fmt.Errorf("load published artifact catalog: %w", err)
	}
	published := make(map[string]string, len(catalog))
	for _, artifact := range catalog {
		published[artifact.Path] = artifact.OutputHash
	}
	seen := make(map[string]struct{}, len(report.Artifacts))
	for _, result := range report.Artifacts {
		if result.Status != protocol.StatusCompleted && result.Status != protocol.StatusFailed && result.Status != protocol.StatusBlocked {
			return false, fmt.Errorf("%w: invalid artifact status", ErrInvalidAgentReport)
		}
		if len(result.Message) > 1024 || strings.TrimSpace(result.Path) == "" {
			return false, fmt.Errorf("%w: invalid artifact result", ErrInvalidAgentReport)
		}
		if expectedHash, exists := published[result.Path]; !exists || result.OutputHash != expectedHash {
			return false, fmt.Errorf("%w: artifact result does not match published state", ErrInvalidAgentReport)
		}
		if _, exists := seen[result.Path]; exists {
			return false, fmt.Errorf("%w: duplicate artifact result", ErrInvalidAgentReport)
		}
		seen[result.Path] = struct{}{}
	}
	return service.reports.Append(report)
}

func (service *Service) Reports() []protocol.AgentReport {
	return service.reports.List()
}
