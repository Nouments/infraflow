package filesystem

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	coregeneration "infraflow/internal/adapters/generation"
	"infraflow/internal/infrastructure/safefs"
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
	catalog, err := repository.Catalog()
	if err != nil {
		return nil, protocol.Artifact{}, err
	}
	var published protocol.Artifact
	for _, artifact := range catalog {
		if artifact.Path == path {
			published = artifact
			break
		}
	}
	if published.Path == "" {
		return nil, protocol.Artifact{}, os.ErrNotExist
	}
	root, err := filepath.Abs(repository.root)
	if err != nil {
		return nil, protocol.Artifact{}, fmt.Errorf("resolve artifact root: %w", err)
	}
	file, err := safefs.OpenReadOnly(root, published.Path)
	if err != nil {
		return nil, protocol.Artifact{}, err
	}
	return file, published, nil
}
