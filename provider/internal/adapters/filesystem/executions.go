package filesystem

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"infraflow/internal/domain"
	"infraflow/internal/infrastructure/safefs"
	"infraflow/internal/ports"
	"infraflow/pkg/protocol"
)

const maxExecutionBytes = 64 << 20

type ExecutionStore struct {
	mu            sync.Mutex
	root          string
	byID          map[string]domain.Execution
	byIdempotency map[string]domain.Execution
}

func NewExecutionStore(root string) (*ExecutionStore, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect artifact directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("artifact root must be a directory")
	}
	store := &ExecutionStore{root: root, byID: make(map[string]domain.Execution), byIdempotency: make(map[string]domain.Execution)}
	directory := filepath.Join(root, ".executions")
	if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("create execution directory: %w", err)
	}
	info, err = os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("execution directory must be a real directory")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read execution directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("execution directory contains symlink %q", entry.Name())
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		execution, err := store.read(id)
		if err != nil {
			return nil, err
		}
		if _, exists := store.byIdempotency[execution.IdempotencyKeyHash]; exists {
			return nil, fmt.Errorf("duplicate execution idempotency key hash")
		}
		store.byID[id] = execution
		store.byIdempotency[execution.IdempotencyKeyHash] = execution
	}
	return store, nil
}

func (store *ExecutionStore) Create(execution domain.Execution) (domain.Execution, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := validateExecution(execution); err != nil {
		return domain.Execution{}, false, err
	}
	if existing, exists := store.byIdempotency[execution.IdempotencyKeyHash]; exists {
		if existing.RequestHash != execution.RequestHash {
			return domain.Execution{}, false, ports.ErrIdempotencyConflict
		}
		return cloneExecution(existing), false, nil
	}
	if _, exists := store.byID[execution.ID]; exists {
		return domain.Execution{}, false, os.ErrExist
	}
	if err := store.write(execution); err != nil {
		return domain.Execution{}, false, err
	}
	store.byID[execution.ID] = cloneExecution(execution)
	store.byIdempotency[execution.IdempotencyKeyHash] = cloneExecution(execution)
	return cloneExecution(execution), true, nil
}

func (store *ExecutionStore) Get(id string) (domain.Execution, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	execution, exists := store.byID[id]
	if !exists {
		return domain.Execution{}, os.ErrNotExist
	}
	return cloneExecution(execution), nil
}

func (store *ExecutionStore) List() ([]domain.Execution, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	executions := make([]domain.Execution, 0, len(store.byID))
	for _, execution := range store.byID {
		executions = append(executions, cloneExecution(execution))
	}
	sort.Slice(executions, func(i, j int) bool {
		if !executions[i].CreatedAt.Equal(executions[j].CreatedAt) {
			return executions[i].CreatedAt.After(executions[j].CreatedAt)
		}
		return executions[i].ID > executions[j].ID
	})
	return executions, nil
}

func (store *ExecutionStore) Update(execution domain.Execution) (domain.Execution, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	previous, exists := store.byID[execution.ID]
	if !exists {
		return domain.Execution{}, os.ErrNotExist
	}
	if isTerminalExecution(previous.Status) {
		return domain.Execution{}, ports.ErrExecutionConflict
	}
	if execution.Revision != previous.Revision || execution.UpdatedAt.Before(previous.UpdatedAt) || !sameExecutionRequest(previous, execution) {
		return domain.Execution{}, ports.ErrExecutionConflict
	}
	if !domain.CanTransitionExecution(previous.Status, execution.Status) || !validTaskTransitions(previous.Tasks, execution.Tasks) || !appendOnly(previous.Events, execution.Events) {
		return domain.Execution{}, ports.ErrExecutionConflict
	}
	execution.Revision++
	if err := validateExecution(execution); err != nil {
		return domain.Execution{}, err
	}
	if err := store.write(execution); err != nil {
		return domain.Execution{}, err
	}
	store.byID[execution.ID] = cloneExecution(execution)
	store.byIdempotency[execution.IdempotencyKeyHash] = cloneExecution(execution)
	return cloneExecution(execution), nil
}

func (store *ExecutionStore) read(id string) (domain.Execution, error) {
	if !protocol.ValidSiteName(id) || len(id) > 128 {
		return domain.Execution{}, fmt.Errorf("invalid execution id")
	}
	path := filepath.Join(store.root, ".executions", id+".json")
	info, err := os.Lstat(path)
	if err != nil {
		return domain.Execution{}, err
	}
	if !info.Mode().IsRegular() {
		return domain.Execution{}, fmt.Errorf("execution file must be regular")
	}
	file, err := os.Open(path)
	if err != nil {
		return domain.Execution{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxExecutionBytes+1))
	if err != nil {
		return domain.Execution{}, fmt.Errorf("read execution %q: %w", id, err)
	}
	if len(data) > maxExecutionBytes {
		return domain.Execution{}, fmt.Errorf("execution %q exceeds %d bytes", id, maxExecutionBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var execution domain.Execution
	if err := decoder.Decode(&execution); err != nil {
		return domain.Execution{}, fmt.Errorf("decode execution %q: %w", id, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return domain.Execution{}, fmt.Errorf("execution %q contains trailing JSON", id)
	}
	if execution.ID != id {
		return domain.Execution{}, fmt.Errorf("execution filename does not match its id")
	}
	if err := validateExecution(execution); err != nil {
		return domain.Execution{}, fmt.Errorf("validate execution %q: %w", id, err)
	}
	return execution, nil
}

func (store *ExecutionStore) write(execution domain.Execution) error {
	data, err := json.MarshalIndent(execution, "", "  ")
	if err != nil {
		return fmt.Errorf("encode execution: %w", err)
	}
	if len(data) > maxExecutionBytes {
		return fmt.Errorf("execution exceeds %d bytes", maxExecutionBytes)
	}
	data = append(data, '\n')
	return safefs.AtomicWrite(store.root, filepath.ToSlash(filepath.Join(".executions", execution.ID+".json")), data, 0o600)
}

func validateExecution(execution domain.Execution) error {
	if !protocol.ValidSiteName(execution.ID) || !strings.HasPrefix(execution.ID, "exec-") || len(execution.ID) > 128 {
		return fmt.Errorf("invalid execution id")
	}
	if !protocol.IsSHA256(execution.IdempotencyKeyHash) || !protocol.IsSHA256(execution.RequestHash) {
		return fmt.Errorf("invalid execution idempotency or request hash")
	}
	if !protocol.ValidSiteName(execution.ActorID) || len(execution.ActorID) > 128 {
		return fmt.Errorf("invalid execution actor")
	}
	if execution.Revision == 0 || execution.CreatedAt.IsZero() || execution.UpdatedAt.Before(execution.CreatedAt) || execution.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid execution revision or timestamps")
	}
	if !validExecutionStatus(execution.Status) || len(execution.Tasks) == 0 || len(execution.Tasks) > 128 || len(execution.Events) > 4096 || len(execution.Error) > 1024 {
		return fmt.Errorf("invalid execution state or size")
	}
	if execution.Status == domain.ExecutionRunning && execution.StartedAt.IsZero() || execution.Status == domain.ExecutionQueued && !execution.StartedAt.IsZero() {
		return fmt.Errorf("invalid execution start timestamp")
	}
	if isTerminalExecution(execution.Status) != !execution.FinishedAt.IsZero() {
		return fmt.Errorf("invalid execution finish timestamp")
	}
	seen := make(map[string]struct{}, len(execution.Tasks))
	for _, task := range execution.Tasks {
		if !protocol.ValidSiteName(task.ID) || len(task.ID) > 128 || !protocol.ValidSiteName(task.Type) || len(task.Type) > 128 {
			return fmt.Errorf("invalid execution task identity")
		}
		if _, exists := seen[task.ID]; exists {
			return fmt.Errorf("duplicate execution task id")
		}
		seen[task.ID] = struct{}{}
		if !validTaskStatus(task.Status) || task.TimeoutMillis < 1 || task.TimeoutMillis > 600000 || task.MaxAttempts < 1 || task.MaxAttempts > 5 || len(task.Attempts) > task.MaxAttempts {
			return fmt.Errorf("invalid execution task policy or status")
		}
		if len(task.Parameters) == 0 || len(task.Parameters) > 64<<10 || !json.Valid(task.Parameters) || len(task.Result) > 64<<10 || len(task.Result) > 0 && !json.Valid(task.Result) || len(task.Error) > 1024 {
			return fmt.Errorf("invalid execution task payload")
		}
		if task.CreatedAt.IsZero() || task.UpdatedAt.Before(task.CreatedAt) || task.UpdatedAt.IsZero() {
			return fmt.Errorf("invalid execution task timestamps")
		}
		if len(task.Dependencies) > 128 || len(task.ResourceLock) > 256 || !domain.ValidExecutionResourceLock(task.ResourceLock) {
			return fmt.Errorf("invalid execution task dependencies or resource lock")
		}
		for _, dependency := range task.Dependencies {
			if !protocol.ValidSiteName(dependency) || dependency == task.ID {
				return fmt.Errorf("invalid execution task dependency")
			}
		}
		if len(task.Attempts) > 0 && task.StartedAt.IsZero() || task.Status == domain.ExecutionTaskRunning && task.StartedAt.IsZero() || task.Status == domain.ExecutionTaskQueued && !task.StartedAt.IsZero() || isTerminalTask(task.Status) != !task.FinishedAt.IsZero() {
			return fmt.Errorf("invalid execution task lifecycle timestamps")
		}
		for index, attempt := range task.Attempts {
			if attempt.Number != index+1 || !validAttemptStatus(attempt.Status) || attempt.StartedAt.IsZero() || len(attempt.Result) > 64<<10 || len(attempt.Result) > 0 && !json.Valid(attempt.Result) || len(attempt.Error) > 1024 {
				return fmt.Errorf("invalid execution attempt")
			}
			if (isTerminalTask(attempt.Status) || attempt.Status == domain.ExecutionTaskRetrying) != !attempt.FinishedAt.IsZero() {
				return fmt.Errorf("invalid execution attempt timestamps")
			}
		}
	}
	for _, event := range execution.Events {
		if !protocol.ValidSiteName(event.ActorID) || !protocol.ValidSiteName(event.Action) || len(event.ActorID) > 128 || len(event.Action) > 128 || !validExecutionStatus(event.Status) || event.Timestamp.IsZero() || len(event.Error) > 1024 {
			return fmt.Errorf("invalid execution audit event")
		}
		if event.TaskID != "" && !protocol.ValidSiteName(event.TaskID) || event.TaskStatus != "" && !validTaskStatus(event.TaskStatus) {
			return fmt.Errorf("invalid execution audit task")
		}
	}
	return nil
}

func validExecutionStatus(status domain.ExecutionStatus) bool {
	switch status {
	case domain.ExecutionQueued, domain.ExecutionRunning, domain.ExecutionSucceeded, domain.ExecutionFailed, domain.ExecutionCancelled, domain.ExecutionTimedOut:
		return true
	default:
		return false
	}
}

func validTaskStatus(status domain.ExecutionTaskStatus) bool {
	switch status {
	case domain.ExecutionTaskQueued, domain.ExecutionTaskRunning, domain.ExecutionTaskRetrying, domain.ExecutionTaskSucceeded, domain.ExecutionTaskFailed, domain.ExecutionTaskCancelled, domain.ExecutionTaskTimedOut, domain.ExecutionTaskBlocked:
		return true
	default:
		return false
	}
}

func validAttemptStatus(status domain.ExecutionTaskStatus) bool {
	return status == domain.ExecutionTaskRunning || status == domain.ExecutionTaskRetrying || status == domain.ExecutionTaskSucceeded || status == domain.ExecutionTaskFailed || status == domain.ExecutionTaskCancelled || status == domain.ExecutionTaskTimedOut
}

func isTerminalExecution(status domain.ExecutionStatus) bool {
	return status == domain.ExecutionSucceeded || status == domain.ExecutionFailed || status == domain.ExecutionCancelled || status == domain.ExecutionTimedOut
}

func isTerminalTask(status domain.ExecutionTaskStatus) bool {
	return status == domain.ExecutionTaskSucceeded || status == domain.ExecutionTaskFailed || status == domain.ExecutionTaskCancelled || status == domain.ExecutionTaskTimedOut || status == domain.ExecutionTaskBlocked
}

func sameExecutionRequest(previous, current domain.Execution) bool {
	if previous.ID != current.ID || previous.IdempotencyKeyHash != current.IdempotencyKeyHash || previous.RequestHash != current.RequestHash || previous.ActorID != current.ActorID || !previous.CreatedAt.Equal(current.CreatedAt) || len(previous.Tasks) != len(current.Tasks) {
		return false
	}
	for index := range previous.Tasks {
		left, right := previous.Tasks[index], current.Tasks[index]
		if left.ID != right.ID || left.Type != right.Type || left.ResourceLock != right.ResourceLock || left.TimeoutMillis != right.TimeoutMillis || left.MaxAttempts != right.MaxAttempts || left.RetryInitialMillis != right.RetryInitialMillis || left.RetryMaxMillis != right.RetryMaxMillis || string(left.Parameters) != string(right.Parameters) || !equalStrings(left.Dependencies, right.Dependencies) || !left.CreatedAt.Equal(right.CreatedAt) {
			return false
		}
	}
	return true
}

func validTaskTransitions(previous, current []domain.ExecutionTask) bool {
	if len(previous) != len(current) {
		return false
	}
	byID := make(map[string]domain.ExecutionTask, len(previous))
	for _, task := range previous {
		byID[task.ID] = task
	}
	for _, task := range current {
		old, exists := byID[task.ID]
		if !exists || !domain.CanTransitionExecutionTask(old.Status, task.Status) || len(task.Attempts) < len(old.Attempts) {
			return false
		}
		if isTerminalTask(old.Status) && !reflect.DeepEqual(old, task) {
			return false
		}
		for index := range old.Attempts {
			oldAttempt, newAttempt := old.Attempts[index], task.Attempts[index]
			if index == len(old.Attempts)-1 && oldAttempt.Status == domain.ExecutionTaskRunning {
				if oldAttempt.Number != newAttempt.Number || !oldAttempt.StartedAt.Equal(newAttempt.StartedAt) || !domain.CanTransitionExecutionTask(oldAttempt.Status, newAttempt.Status) {
					return false
				}
				continue
			}
			if !reflect.DeepEqual(oldAttempt, newAttempt) {
				return false
			}
		}
	}
	return true
}

func appendOnly(previous, current []domain.ExecutionEvent) bool {
	if len(current) < len(previous) {
		return false
	}
	for index := range previous {
		if previous[index] != current[index] {
			return false
		}
	}
	return true
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cloneExecution(execution domain.Execution) domain.Execution {
	clone := execution
	clone.Tasks = append([]domain.ExecutionTask(nil), execution.Tasks...)
	clone.Events = append([]domain.ExecutionEvent(nil), execution.Events...)
	for index := range clone.Tasks {
		task := &clone.Tasks[index]
		task.Dependencies = append([]string(nil), task.Dependencies...)
		task.Parameters = append([]byte(nil), task.Parameters...)
		task.Result = append([]byte(nil), task.Result...)
		task.Attempts = append([]domain.ExecutionAttempt(nil), task.Attempts...)
		for attemptIndex := range task.Attempts {
			task.Attempts[attemptIndex].Result = append([]byte(nil), task.Attempts[attemptIndex].Result...)
		}
	}
	return clone
}
