package domain

type Plan struct {
	// ID is plan/sha256-<hex> over canonical desired site configuration. It excludes
	// site, device, and link IDs, registries, observations, and runtime state.
	ID        string         `json:"id"`
	Version   string         `json:"version"`
	Site      string         `json:"site,omitempty"`
	Status    string         `json:"status"`
	Lifecycle LifecycleState `json:"lifecycle"`
	Tasks     []Task         `json:"tasks"`
}

type Task struct {
	ID           string   `json:"id"`
	Site         string   `json:"site"`
	Target       string   `json:"target,omitempty"`
	Action       string   `json:"action"`
	Method       string   `json:"method,omitempty"`
	Status       string   `json:"status"`
	Reason       string   `json:"reason,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	Artifacts    []string `json:"artifacts,omitempty"`
}
