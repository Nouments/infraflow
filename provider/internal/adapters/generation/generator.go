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

func (Generator) GenerateNative(infrastructure domain.Infrastructure, outputDirectory string) ([]protocol.Artifact, error) {
	return coregeneration.GenerateNative(infrastructure, outputDirectory)
}

func (Generator) GenerateTerraform(infrastructure domain.Infrastructure, outputDirectory string) ([]protocol.Artifact, error) {
	return coregeneration.GenerateTerraform(infrastructure, outputDirectory)
}

func (Generator) GenerateBootstrap(infrastructure domain.Infrastructure, outputDirectory string) ([]protocol.Artifact, error) {
	return coregeneration.GenerateBootstrap(infrastructure, outputDirectory)
}

func (Generator) GenerateDeclarativeConfig(input []byte, outputDirectory string) ([]protocol.Artifact, error) {
	return coregeneration.GenerateDeclarativeConfig(input, outputDirectory)
}
