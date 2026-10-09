package generation

import (
	coregeneration "infraflow/internal/adapters/generation"
	"infraflow/internal/domain"
	"infraflow/pkg/protocol"
)

type Generator struct{}

func (Generator) Generate(infrastructure domain.Infrastructure, outputDirectory string) ([]protocol.Artifact, error) {
	return coregeneration.Generate(infrastructure, outputDirectory)
}

func (Generator) GenerateAll(infrastructure domain.Infrastructure, outputDirectory string) ([]protocol.Artifact, error) {
	return coregeneration.GenerateAll(infrastructure, outputDirectory)
}

func (Generator) GenerateAnsible(infrastructure domain.Infrastructure, outputDirectory string) ([]protocol.Artifact, error) {
	return coregeneration.GenerateAnsible(infrastructure, outputDirectory)
}

func (Generator) GenerateVendorAnsible(infrastructure domain.Infrastructure, outputDirectory string) ([]protocol.Artifact, error) {
	return coregeneration.GenerateVendorAnsible(infrastructure, outputDirectory)
}

func (Generator) GenerateTerraform(infrastructure domain.Infrastructure, outputDirectory string) ([]protocol.Artifact, error) {
	return coregeneration.GenerateTerraform(infrastructure, outputDirectory)
}

func (Generator) GenerateBootstrap(infrastructure domain.Infrastructure, outputDirectory string) ([]protocol.Artifact, error) {
	return coregeneration.GenerateBootstrap(infrastructure, outputDirectory)
}
