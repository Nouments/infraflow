package filesystem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"infraflow/internal/domain"
	"infraflow/internal/infrastructure/safefs"
	"infraflow/pkg/protocol"
)

const maxAgentBytes = 64 << 10

type AgentStore struct {
	mu   sync.Mutex
	root string
}

func NewAgentStore(root string) (*AgentStore, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect artifact directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("artifact root must be a directory")
	}
	agentsPath := filepath.Join(root, ".agents")
	agentsInfo, err := os.Lstat(agentsPath)
	if os.IsNotExist(err) {
		if err := os.Mkdir(agentsPath, 0o700); err != nil {
			return nil, fmt.Errorf("create agent directory: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("inspect agent directory: %w", err)
	} else if agentsInfo.Mode()&os.ModeSymlink != 0 || !agentsInfo.IsDir() {
		return nil, fmt.Errorf("agent directory must be a directory")
	}
	return &AgentStore{root: root}, nil
}

func (store *AgentStore) Register(agent domain.Agent) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := validateAgent(agent); err != nil {
		return false, err
	}
	path, err := store.agentPath(agent.ID)
	if err != nil {
		return false, err
	}
	if _, err := os.Lstat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("inspect agent file: %w", err)
	}
	return true, store.write(agent)
}

func (store *AgentStore) Get(id string) (domain.Agent, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.read(id)
}

func (store *AgentStore) List() ([]domain.Agent, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(store.root, ".agents"))
	if err != nil {
		return nil, fmt.Errorf("read agent directory: %w", err)
	}
	agents := make([]domain.Agent, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("agent directory contains symlink %q", entry.Name())
		}
		id := entry.Name()[:len(entry.Name())-len(filepath.Ext(entry.Name()))]
		agent, err := store.read(id)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	sort.Slice(agents, func(i, j int) bool {
		if !agents[i].UpdatedAt.Equal(agents[j].UpdatedAt) {
			return agents[i].UpdatedAt.Before(agents[j].UpdatedAt)
		}
		return agents[i].ID < agents[j].ID
	})
	return agents, nil
}

func (store *AgentStore) Update(agent domain.Agent) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := validateAgent(agent); err != nil {
		return err
	}
	path, err := store.agentPath(agent.ID)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return os.ErrNotExist
		}
		return fmt.Errorf("inspect agent file: %w", err)
	}
	return store.write(agent)
}

func (store *AgentStore) read(id string) (domain.Agent, error) {
	path, err := store.agentPath(id)
	if err != nil {
		return domain.Agent{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return domain.Agent{}, err
	}
	if !info.Mode().IsRegular() {
		return domain.Agent{}, fmt.Errorf("agent file must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return domain.Agent{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAgentBytes+1))
	if err != nil {
		return domain.Agent{}, fmt.Errorf("read agent %q: %w", id, err)
	}
	if len(data) > maxAgentBytes {
		return domain.Agent{}, fmt.Errorf("agent %q exceeds %d bytes", id, maxAgentBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var agent domain.Agent
	if err := decoder.Decode(&agent); err != nil {
		return domain.Agent{}, fmt.Errorf("decode agent %q: %w", id, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return domain.Agent{}, fmt.Errorf("agent %q contains trailing JSON", id)
	}
	if err := validateAgent(agent); err != nil {
		return domain.Agent{}, fmt.Errorf("validate agent %q: %w", id, err)
	}
	return agent, nil
}

func (store *AgentStore) write(agent domain.Agent) error {
	data, err := json.MarshalIndent(agent, "", "  ")
	if err != nil {
		return fmt.Errorf("encode agent: %w", err)
	}
	data = append(data, '\n')
	return safefs.AtomicWrite(store.root, filepath.ToSlash(filepath.Join(".agents", agent.ID+".json")), data, 0o600)
}

func (store *AgentStore) agentPath(id string) (string, error) {
	if !protocol.ValidSiteName(id) || len(id) > 128 {
		return "", fmt.Errorf("invalid agent id")
	}
	return filepath.Join(store.root, ".agents", id+".json"), nil
}

func validateAgent(agent domain.Agent) error {
	if !protocol.ValidSiteName(agent.ID) || len(agent.ID) > 128 {
		return fmt.Errorf("invalid agent id")
	}
	if !protocol.ValidSiteName(agent.SiteID) || len(agent.SiteID) > 128 {
		return fmt.Errorf("invalid site id")
	}
	if len(agent.Version) > 128 || len(agent.Capabilities) > 64 || agent.QueueDepth < 0 || agent.QueueDepth > 100000 {
		return fmt.Errorf("invalid agent details")
	}
	if agent.Status != domain.AgentStatusOnline {
		return fmt.Errorf("invalid agent status")
	}
	if agent.RegisteredAt.IsZero() || agent.LastSeenAt.IsZero() || agent.UpdatedAt.IsZero() || agent.LastSeenAt.Before(agent.RegisteredAt) || agent.UpdatedAt.Before(agent.LastSeenAt) {
		return fmt.Errorf("invalid agent timestamps")
	}
	seen := make(map[string]struct{}, len(agent.Capabilities))
	for _, capability := range agent.Capabilities {
		if capability == "" || len(capability) > 128 {
			return fmt.Errorf("invalid capability")
		}
		if _, exists := seen[capability]; exists {
			return fmt.Errorf("duplicate capability")
		}
		seen[capability] = struct{}{}
	}
	return nil
}
