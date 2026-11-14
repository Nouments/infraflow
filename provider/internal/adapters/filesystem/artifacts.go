package filesystem

import (
	"bytes"
	"io"

	coregeneration "infraflow/internal/adapters/generation"
	"infraflow/pkg/protocol"
)

type ArtifactRepository struct {
	root string
}

func NewArtifactRepository(root string) *ArtifactRepository {
	return &ArtifactRepository{root: root}
}

func (repository *ArtifactRepository) Catalog() ([]protocol.Artifact, error) {
	return coregeneration.Catalog(repository.root)
}

func (repository *ArtifactRepository) Open(path string) (io.ReadCloser, protocol.Artifact, error) {
	data, published, err := coregeneration.ReadArtifact(repository.root, path)
	if err != nil {
		return nil, protocol.Artifact{}, err
	}
	return io.NopCloser(bytes.NewReader(data)), published, nil
}
