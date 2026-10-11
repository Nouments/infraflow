package domain

import (
	"encoding/json"
	"time"
)

type ExecutionStatus string

const (
	ExecutionQueued    ExecutionStatus = "queued"
	ExecutionRunning   ExecutionStatus = "running"
	ExecutionSucceeded ExecutionStatus = "succeeded"
	ExecutionFailed    ExecutionStatus = "failed"
	ExecutionCancelled ExecutionStatus = "cancelled"
	ExecutionTimedOut  ExecutionStatus = "timed_out"
)

type ExecutionTaskStatus string

const (
	ExecutionTaskQueued    ExecutionTaskStatus = "queued"
	ExecutionTaskRunning   ExecutionTaskStatus = "running"
	ExecutionTaskRetrying  ExecutionTaskStatus = "retrying"
	ExecutionTaskSucceeded ExecutionTaskStatus = "succeeded"
	ExecutionTaskFailed    ExecutionTaskStatus = "failed"
	ExecutionTaskCancelled ExecutionTaskStatus = "cancelled"
	ExecutionTaskTimedOut  ExecutionTaskStatus = "timed_out"
	ExecutionTaskBlocked   ExecutionTaskStatus = "blocked"
)

type Execution struct {
	ID                 string           `json:"id"`
	IdempotencyKeyHash string           `json:"idempotency_key_hash"`
	RequestHash        string           `json:"request_hash"`
	ActorID            string           `json:"actor_id"`
	Status             ExecutionStatus  `json:"status"`
	Tasks              []ExecutionTask  `json:"tasks"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	StartedAt          time.Time        `json:"started_at,omitempty"`
	FinishedAt         time.Time        `json:"finished_at,omitempty"`
	CancelRequested    bool             `json:"cancel_requested,omitempty"`
	CancelRequestedAt  time.Time        `json:"cancel_requested_at,omitempty"`
	Error              string           `json:"error,omitempty"`
	Revision           uint64           `json:"revision"`
	Events             []ExecutionEvent `json:"events"`
}

type ExecutionTask struct {
	ID                 string              `json:"id"`
	Type               string              `json:"type"`
	Status             ExecutionTaskStatus `json:"status"`
	Dependencies       []string            `json:"dependencies,omitempty"`
	ResourceLock       string              `json:"resource_lock,omitempty"`
	TimeoutMillis      int64               `json:"timeout_ms"`
	MaxAttempts        int                 `json:"max_attempts"`
	RetryInitialMillis int64               `json:"retry_initial_ms,omitempty"`
	RetryMaxMillis     int64               `json:"retry_max_ms,omitempty"`
	Parameters         json.RawMessage     `json:"parameters"`
	Attempts           []ExecutionAttempt  `json:"attempts"`
	Result             json.RawMessage     `json:"result,omitempty"`
	Error              string              `json:"error,omitempty"`
	CreatedAt          time.Time           `json:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
	StartedAt          time.Time           `json:"started_at,omitempty"`
	FinishedAt         time.Time           `json:"finished_at,omitempty"`
}

type ExecutionAttempt struct {
	Number     int                 `json:"number"`
	Status     ExecutionTaskStatus `json:"status"`
	StartedAt  time.Time           `json:"started_at"`
	FinishedAt time.Time           `json:"finished_at,omitempty"`
	DurationMS int64               `json:"duration_ms,omitempty"`
	Result     json.RawMessage     `json:"result,omitempty"`
	Error      string              `json:"error,omitempty"`
}

type ExecutionEvent struct {
	ActorID    string              `json:"actor_id"`
	Action     string              `json:"action"`
	TaskID     string              `json:"task_id,omitempty"`
	Status     ExecutionStatus     `json:"status"`
	TaskStatus ExecutionTaskStatus `json:"task_status,omitempty"`
	Timestamp  time.Time           `json:"timestamp"`
	Error      string              `json:"error,omitempty"`
}

func CanTransitionExecution(from, to ExecutionStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case ExecutionQueued:
		return to == ExecutionRunning || to == ExecutionFailed || to == ExecutionCancelled
	case ExecutionRunning:
		return to == ExecutionSucceeded || to == ExecutionFailed || to == ExecutionCancelled || to == ExecutionTimedOut
	default:
		return false
	}
}

func CanTransitionExecutionTask(from, to ExecutionTaskStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case ExecutionTaskQueued:
		return to == ExecutionTaskRunning || to == ExecutionTaskBlocked || to == ExecutionTaskCancelled
	case ExecutionTaskRunning:
		return to == ExecutionTaskRetrying || to == ExecutionTaskSucceeded || to == ExecutionTaskFailed || to == ExecutionTaskCancelled || to == ExecutionTaskTimedOut
	case ExecutionTaskRetrying:
		return to == ExecutionTaskRunning || to == ExecutionTaskFailed || to == ExecutionTaskCancelled
	default:
		return false
	}
}

func ValidExecutionResourceLock(value string) bool {
	if value == "" {
		return true
	}
	for index, character := range value {
		valid := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '.' || character == '_' || character == ':' || character == '-'
		if !valid || index == 0 && !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9') {
			return false
		}
	}
	return true
}
