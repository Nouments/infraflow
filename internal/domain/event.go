package domain

import (
	"encoding/json"
	"time"
)

type Event struct {
	EventID      string          `json:"event_id"`
	SiteID       string          `json:"site_id,omitempty"`
	AgentID      string          `json:"agent_id,omitempty"`
	JobID        string          `json:"job_id,omitempty"`
	Sequence     uint64          `json:"sequence"`
	Timestamp    time.Time       `json:"timestamp"`
	Type         string          `json:"type"`
	Payload      json.RawMessage `json:"payload"`
	Hash         string          `json:"hash"`
	PreviousHash string          `json:"previous_hash,omitempty"`
}
