package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"infraflow/pkg/protocol"
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

func ComputeEventHash(event Event) string {
	payload, _ := json.Marshal(struct {
		EventID   string          `json:"event_id"`
		SiteID    string          `json:"site_id,omitempty"`
		AgentID   string          `json:"agent_id,omitempty"`
		JobID     string          `json:"job_id,omitempty"`
		Sequence  uint64          `json:"sequence"`
		Timestamp time.Time       `json:"timestamp"`
		Type      string          `json:"type"`
		Payload   json.RawMessage `json:"payload"`
		Previous  string          `json:"previous_hash,omitempty"`
	}{
		EventID: event.EventID, SiteID: event.SiteID, AgentID: event.AgentID,
		JobID: event.JobID, Sequence: event.Sequence, Timestamp: event.Timestamp,
		Type: event.Type, Payload: event.Payload, Previous: event.PreviousHash,
	})
	return protocol.SHA256(payload)
}

func ValidateEvent(event Event) error {
	if !protocol.ValidSiteName(event.EventID) || len(event.EventID) > 128 {
		return fmt.Errorf("invalid event id")
	}
	for name, value := range map[string]string{"site id": event.SiteID, "agent id": event.AgentID, "job id": event.JobID} {
		if value != "" && (!protocol.ValidSiteName(value) || len(value) > 128) {
			return fmt.Errorf("invalid %s", name)
		}
	}
	if event.Sequence == 0 || event.Timestamp.IsZero() || !protocol.ValidSiteName(event.Type) || len(event.Type) > 128 {
		return fmt.Errorf("invalid event metadata")
	}
	if len(event.Payload) == 0 || len(event.Payload) > 64<<10 || !json.Valid(event.Payload) {
		return fmt.Errorf("invalid event payload")
	}
	if !protocol.IsSHA256(event.Hash) || (event.Sequence == 1 && event.PreviousHash != "") || (event.Sequence > 1 && !protocol.IsSHA256(event.PreviousHash)) {
		return fmt.Errorf("invalid event hash chain")
	}
	if event.Hash != ComputeEventHash(event) {
		return fmt.Errorf("event hash does not match payload")
	}
	return nil
}
