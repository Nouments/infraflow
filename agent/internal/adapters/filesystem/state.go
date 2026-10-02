package filesystem

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"infraflow/internal/infrastructure/safefs"
	"infraflow/pkg/protocol"
)

type StateStore struct {
	root string
}

func NewStateStore(root string) *StateStore {
	return &StateStore{root: root}
}

func (store *StateStore) SaveArtifact(path, expectedHash string, source io.Reader) error {
	return safefs.AtomicWriteFromReader(store.root, path, source, 0o644, expectedHash)
}

func (store *StateStore) OpenArtifact(relativePath string) (io.ReadCloser, error) {
	site, filename, found := strings.Cut(relativePath, "/")
	if !found || !protocol.ValidSiteName(site) || filename != "inventory.json" && filename != "topology.json" {
		return nil, fmt.Errorf("invalid artifact path")
	}
	root, err := filepath.Abs(store.root)
	if err != nil {
		return nil, fmt.Errorf("resolve state root: %w", err)
	}
	path := filepath.Join(root, site, filename)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("artifact must be a regular file")
	}
	return os.Open(path)
}

func (store *StateStore) SaveReport(report protocol.AgentReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return safefs.AtomicWrite(store.root, ".infraflow-agent-state.json", data, 0o600)
}
