package filesystem

import (
	"encoding/json"
	"fmt"
	"io"
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
	if !validStoredArtifactPath(path) {
		return fmt.Errorf("invalid artifact path")
	}
	return safefs.AtomicWriteFromReader(store.root, path, source, 0o644, expectedHash)
}

func (store *StateStore) OpenArtifact(relativePath string) (io.ReadCloser, error) {
	if !validStoredArtifactPath(relativePath) {
		return nil, fmt.Errorf("invalid artifact path")
	}
	return safefs.OpenReadOnly(store.root, relativePath)
}

func (store *StateStore) SaveReport(report protocol.AgentReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return safefs.AtomicWrite(store.root, ".infraflow-agent-state.json", data, 0o600)
}

func validStoredArtifactPath(relativePath string) bool {
	site, _, found := strings.Cut(relativePath, "/")
	if !found || !protocol.ValidSiteName(site) {
		return false
	}
	for _, artifactType := range []string{
		"inventory", "topology", "ansible_inventory", "ansible_playbook",
		"ansible_vendor_inventory", "ansible_requirements", "ansible_vendor_manifest",
		"terraform_versions", "terraform_providers", "terraform_variables", "terraform_locals",
		"terraform_main", "terraform_outputs", "terraform_tfvars_example",
		"bootstrap_dhcp", "bootstrap_dns", "bootstrap_tftp", "bootstrap_pxe",
		"bootstrap_ipxe_script", "bootstrap_ipxe_menu",
	} {
		if protocol.ValidArtifactPath(site, protocol.Artifact{Type: artifactType, Path: relativePath}) {
			return true
		}
	}
	return false
}
