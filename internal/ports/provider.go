// Package ports contains contracts owned by use cases and implemented by
// adapters. It deliberately contains no filesystem, HTTP, gRPC, or vendor
// implementation.
package ports

import (
	"io"

	"infraflow/internal/domain"
	"infraflow/pkg/protocol"
)

type InfrastructureParser interface {
	Parse([]byte) (domain.Infrastructure, error)
}

type PlanBuilder interface {
	Build(domain.Infrastructure) domain.Plan
}

type ArtifactRepository interface {
	Catalog() ([]protocol.Artifact, error)
	Open(string) (io.ReadCloser, protocol.Artifact, error)
}

type ReportRepository interface {
	Append(protocol.AgentReport) (bool, error)
	List() []protocol.AgentReport
}

type ArtifactGenerator interface {
	Generate(domain.Infrastructure, string) ([]protocol.Artifact, error)
}

type AllArtifactGenerator interface {
	GenerateAll(domain.Infrastructure, string) ([]protocol.Artifact, error)
}

type AnsibleArtifactGenerator interface {
	GenerateAnsible(domain.Infrastructure, string) ([]protocol.Artifact, error)
}

type TerraformArtifactGenerator interface {
	GenerateTerraform(domain.Infrastructure, string) ([]protocol.Artifact, error)
}

type BootstrapArtifactGenerator interface {
	GenerateBootstrap(domain.Infrastructure, string) ([]protocol.Artifact, error)
}

type JobRepository interface {
	Create(domain.Job) error
	Get(string) (domain.Job, error)
	List() ([]domain.Job, error)
	Update(domain.Job) error
}

type AgentRepository interface {
	Register(domain.Agent) (bool, error)
	Get(string) (domain.Agent, error)
	List() ([]domain.Agent, error)
	Update(domain.Agent) error
}

type EventRepository interface {
	Append(domain.Event) (domain.Event, bool, error)
	List() []domain.Event
}
