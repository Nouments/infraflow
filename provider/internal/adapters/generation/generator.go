package generation

import (
	"infraflow/internal/domain"
	"infraflow/internal/generator"
	"infraflow/pkg/protocol"
)

type Generator struct{}

func (Generator) Generate(infrastructure domain.Infrastructure, outputDirectory string) ([]protocol.Artifact, error) {
	return generator.Generate(infrastructure, outputDirectory)
}
