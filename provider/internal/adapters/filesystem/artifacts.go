package filesystem

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"infraflow/internal/generator"
	"infraflow/pkg/protocol"
)

type ArtifactRepository struct {
	root string
}

func NewArtifactRepository(root string) *ArtifactRepository {
	return &ArtifactRepository{root: root}
}

func (repository *ArtifactRepository) Catalog() ([]protocol.Artifact, error) {
	return generator.Catalog(repository.root)
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
	filePath := filepath.Join(root, filepath.FromSlash(published.Path))
	info, err := os.Lstat(filePath)
	if err != nil {
		return nil, protocol.Artifact{}, err
	}
	if !info.Mode().IsRegular() {
		return nil, protocol.Artifact{}, fmt.Errorf("artifact is not a regular file")
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, protocol.Artifact{}, err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, protocol.Artifact{}, err
	}
	if !openedInfo.Mode().IsRegular() {
		_ = file.Close()
		return nil, protocol.Artifact{}, fmt.Errorf("opened artifact is not a regular file")
	}
	return file, published, nil
}
