package domain

import "time"

const AgentStatusOnline = "online"

type Agent struct {
	ID           string    `json:"id"`
	SiteID       string    `json:"site_id"`
	Version      string    `json:"version,omitempty"`
	Capabilities []string  `json:"capabilities,omitempty"`
	Status       string    `json:"status"`
	QueueDepth   int       `json:"queue_depth"`
	RegisteredAt time.Time `json:"registered_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
