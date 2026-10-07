package domain

type Plan struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Site    string `json:"site,omitempty"`
	Status  string `json:"status"`
	Tasks   []Task `json:"tasks"`
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
