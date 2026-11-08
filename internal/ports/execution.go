package ports

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"infraflow/internal/domain"
)

var (
	ErrExecutionConflict   = errors.New("execution repository conflict")
	ErrIdempotencyConflict = errors.New("idempotency key was used for a different request")
)

type ExecutionRepository interface {
	Create(domain.Execution) (domain.Execution, bool, error)
	Get(string) (domain.Execution, error)
	List() ([]domain.Execution, error)
	Update(domain.Execution) (domain.Execution, error)
}

type ExecutionExecutor interface {
	Type() string
	ValidateParameters(json.RawMessage) (json.RawMessage, error)
	Execute(context.Context, domain.ExecutionTask) (ExecutionResult, error)
	Retryable(error) bool
}

type ExecutionResult struct {
	Status   domain.ExecutionTaskStatus `json:"status"`
	Output   json.RawMessage            `json:"output,omitempty"`
	Error    string                     `json:"error,omitempty"`
	Duration time.Duration              `json:"duration,omitempty"`
}
