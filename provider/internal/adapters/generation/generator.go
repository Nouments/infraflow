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

func (Generator) GenerateAnsible(infrastructure domain.Infrastructure, outputDirectory string) ([]protocol.Artifact, error) {
	return generator.GenerateAnsible(infrastructure, outputDirectory)
}

func (Generator) GenerateTerraform(infrastructure domain.Infrastructure, outputDirectory string) ([]protocol.Artifact, error) {
	return generator.GenerateTerraform(infrastructure, outputDirectory)
}
