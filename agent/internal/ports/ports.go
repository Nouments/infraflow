package ports

import (
	"context"
	"io"

	"infraflow/pkg/observability"
	"infraflow/pkg/protocol"
)

// Provider is the controller-facing contract used by the local agent.
type Provider interface {
	Catalog(context.Context) ([]protocol.Artifact, error)
	Download(context.Context, protocol.Artifact, io.Writer) error
	Report(context.Context, protocol.AgentReport) error
}

type LogReporter interface {
	ReportLogs(context.Context, string, []observability.Event) error
}

// StateStore persists artifacts and execution reports on the agent.
type StateStore interface {
	SaveArtifact(path, expectedHash string, source io.Reader) error
	OpenArtifact(path string) (io.ReadCloser, error)
	SaveReport(protocol.AgentReport) error
}

// ArtifactProcessor applies an artifact after it has been downloaded and verified.
type ArtifactProcessor interface {
	Process(protocol.Artifact, io.Reader) protocol.ArtifactResult
}
