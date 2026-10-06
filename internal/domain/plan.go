package domain

type Plan struct {
	Version string `json:"version"`
	Status  string `json:"status"`
	Tasks   []Task `json:"tasks"`
}

type Task struct {
	ID           string   `json:"id"`
	Site         string   `json:"site"`
	Target       string   `json:"target,omitempty"`
	Action       string   `json:"action"`
	Status       string   `json:"status"`
	Reason       string   `json:"reason,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
}
