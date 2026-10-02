package domain

import "time"

const (
	JobStatusPlanned   = "planned"
	JobStatusBlocked   = "blocked"
	JobStatusCancelled = "cancelled"
	JobStatusFailed    = "failed"
)

type Job struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	InputHash string    `json:"input_hash"`
	Status    string    `json:"status"`
	Plan      Plan      `json:"plan"`
}
