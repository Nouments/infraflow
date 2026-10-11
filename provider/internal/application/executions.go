package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"infraflow/internal/application/scheduler"
	"infraflow/internal/domain"
	"infraflow/internal/ports"
	"infraflow/pkg/observability"
	"infraflow/pkg/protocol"
)

const (
	defaultExecutionTimeout = 30_000
	maxExecutionTimeout     = 600_000
	maxExecutionAttempts    = 5
	maxExecutionConcurrency = 64
	maxExecutionTasks       = 128
	maxExecutionPayload     = 64 << 10
)

var (
	ErrInvalidExecution       = errors.New("invalid execution request")
	ErrExecutionUnsupported   = errors.New("execution type is not supported")
	ErrExecutionNotFound      = errors.New("execution not found")
	ErrExecutionStateConflict = errors.New("execution state does not allow this operation")
	ErrExecutionServiceClosed = errors.New("execution service is shutting down")
)

type ExecutionTaskRequest struct {
	ID                 string          `json:"id"`
	Type               string          `json:"type"`
	Dependencies       []string        `json:"dependencies,omitempty"`
	ResourceLock       string          `json:"resource_lock,omitempty"`
	TimeoutMillis      int64           `json:"timeout_ms,omitempty"`
	MaxAttempts        int             `json:"max_attempts,omitempty"`
	RetryInitialMillis int64           `json:"retry_initial_ms,omitempty"`
	RetryMaxMillis     int64           `json:"retry_max_ms,omitempty"`
	Parameters         json.RawMessage `json:"parameters"`
}

type ExecutionRequest struct {
	Tasks []ExecutionTaskRequest `json:"tasks"`
}

type ExecutionPage struct {
	Items  []domain.Execution `json:"items"`
	Total  int                `json:"total"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
}

type ExecutionService struct {
	repository  ports.ExecutionRepository
	events      ports.EventRepository
	executors   map[string]ports.ExecutionExecutor
	concurrency int

	mu      sync.Mutex
	stateMu sync.Mutex
	runs    map[string]*executionRun
	closed  bool
}

type executionRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func NewExecutionService(repository ports.ExecutionRepository, events ports.EventRepository, executors []ports.ExecutionExecutor, concurrency int) (*ExecutionService, error) {
	if repository == nil || events == nil {
		return nil, fmt.Errorf("execution and audit repositories are required")
	}
	if concurrency < 1 || concurrency > maxExecutionConcurrency {
		return nil, fmt.Errorf("execution concurrency must be between 1 and %d", maxExecutionConcurrency)
	}
	service := &ExecutionService{
		repository: repository, events: events, concurrency: concurrency,
		executors: make(map[string]ports.ExecutionExecutor), runs: make(map[string]*executionRun),
	}
	for _, executor := range executors {
		if executor == nil || !protocol.ValidSiteName(executor.Type()) || len(executor.Type()) > 128 {
			return nil, fmt.Errorf("execution executor has an invalid type")
		}
		if _, exists := service.executors[executor.Type()]; exists {
			return nil, fmt.Errorf("duplicate execution executor type %q", executor.Type())
		}
		service.executors[executor.Type()] = executor
	}
	if err := service.recoverInterrupted(); err != nil {
		return nil, fmt.Errorf("recover interrupted executions: %w", err)
	}
	return service, nil
}

func (service *ExecutionService) Submit(ctx context.Context, actorID, idempotencyKey string, request ExecutionRequest) (domain.Execution, bool, error) {
	if ctx == nil || ctx.Err() != nil {
		return domain.Execution{}, false, fmt.Errorf("execution submission context is unavailable")
	}
	if !protocol.ValidSiteName(actorID) || len(actorID) > 128 || len(idempotencyKey) < 16 || len(idempotencyKey) > 128 || strings.ContainsAny(idempotencyKey, "\r\n") {
		return domain.Execution{}, false, fmt.Errorf("%w: actor or idempotency key is invalid", ErrInvalidExecution)
	}
	tasks, requestHash, err := service.validateRequest(request)
	if err != nil {
		return domain.Execution{}, false, err
	}
	id, err := newExecutionID()
	if err != nil {
		return domain.Execution{}, false, fmt.Errorf("create execution id: %w", err)
	}
	now := time.Now().UTC()
	keyHash := protocol.SHA256([]byte(actorID + "\x00" + idempotencyKey))
	execution := domain.Execution{
		ID: id, IdempotencyKeyHash: keyHash, RequestHash: requestHash, ActorID: actorID,
		Status: domain.ExecutionQueued, Tasks: tasks, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	queuedEvent := domain.ExecutionEvent{ActorID: actorID, Action: "execution.queued", Status: domain.ExecutionQueued, Timestamp: now}
	execution.Events = []domain.ExecutionEvent{queuedEvent}
	created, isNew, err := service.repository.Create(execution)
	if err != nil {
		return domain.Execution{}, false, err
	}
	if !isNew {
		return created, false, nil
	}
	if err := service.writeAudit(created.ID, queuedEvent); err != nil {
		service.failUnstarted(created.ID, actorID, "execution audit persistence failed")
		return created, true, fmt.Errorf("record execution submission audit: %w", err)
	}
	service.mu.Lock()
	if service.closed {
		service.mu.Unlock()
		service.failUnstarted(created.ID, actorID, "provider shutdown before execution dispatch")
		return domain.Execution{}, false, ErrExecutionServiceClosed
	}
	runContext, cancel := context.WithCancel(context.Background())
	run := &executionRun{cancel: cancel, done: make(chan struct{})}
	service.runs[created.ID] = run
	service.mu.Unlock()
	go service.runExecution(runContext, created.ID, actorID, run)
	return created, true, nil
}

func (service *ExecutionService) Get(id string) (domain.Execution, error) {
	if !protocol.ValidSiteName(id) || len(id) > 128 {
		return domain.Execution{}, ErrExecutionNotFound
	}
	execution, err := service.repository.Get(id)
	if errors.Is(err, os.ErrNotExist) {
		return domain.Execution{}, ErrExecutionNotFound
	}
	return execution, err
}

func (service *ExecutionService) List(statusFilter, taskType string, limit, offset int) (ExecutionPage, error) {
	if limit < 1 || limit > 100 || offset < 0 || offset > 1_000_000 {
		return ExecutionPage{}, fmt.Errorf("execution limit or offset is out of range")
	}
	if statusFilter != "" && !validExecutionStatus(domain.ExecutionStatus(statusFilter)) {
		return ExecutionPage{}, fmt.Errorf("execution status filter is invalid")
	}
	if taskType != "" && (!protocol.ValidSiteName(taskType) || len(taskType) > 128) {
		return ExecutionPage{}, fmt.Errorf("execution task type filter is invalid")
	}
	executions, err := service.repository.List()
	if err != nil {
		return ExecutionPage{}, err
	}
	filtered := make([]domain.Execution, 0, len(executions))
	for _, execution := range executions {
		if statusFilter != "" && string(execution.Status) != statusFilter {
			continue
		}
		if taskType != "" && !executionHasTaskType(execution, taskType) {
			continue
		}
		filtered = append(filtered, execution)
	}
	total := len(filtered)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	items := append([]domain.Execution(nil), filtered[offset:end]...)
	if items == nil {
		items = []domain.Execution{}
	}
	return ExecutionPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (service *ExecutionService) Cancel(id, actorID string) (domain.Execution, error) {
	execution, err := service.Get(id)
	if err != nil {
		return domain.Execution{}, err
	}
	if terminalExecution(execution.Status) {
		return domain.Execution{}, ErrExecutionStateConflict
	}
	if execution.CancelRequested {
		return execution, nil
	}
	service.mu.Lock()
	run := service.runs[id]
	service.mu.Unlock()
	if run == nil && execution.Status == domain.ExecutionQueued {
		return service.change(id, actorID, "execution.cancelled", "", "", nil, func(current *domain.Execution) error {
			current.CancelRequested = true
			current.CancelRequestedAt = nextTimestamp(current.UpdatedAt)
			current.Status = domain.ExecutionCancelled
			current.FinishedAt = current.CancelRequestedAt
			for index := range current.Tasks {
				task := &current.Tasks[index]
				task.Status = domain.ExecutionTaskCancelled
				task.Error = "cancelled before execution started"
				task.FinishedAt = current.CancelRequestedAt
				task.UpdatedAt = current.CancelRequestedAt
			}
			return nil
		})
	}
	if run == nil {
		return domain.Execution{}, ErrExecutionStateConflict
	}
	updated, err := service.change(id, actorID, "execution.cancel_requested", "", "", nil, func(current *domain.Execution) error {
		if terminalExecution(current.Status) {
			return ErrExecutionStateConflict
		}
		current.CancelRequested = true
		current.CancelRequestedAt = nextTimestamp(current.UpdatedAt)
		return nil
	})
	if err != nil {
		return domain.Execution{}, err
	}
	run.cancel()
	return updated, nil
}

func (service *ExecutionService) Wait(ctx context.Context, id string) (domain.Execution, error) {
	if ctx == nil {
		return domain.Execution{}, fmt.Errorf("wait context is required")
	}
	service.mu.Lock()
	run := service.runs[id]
	service.mu.Unlock()
	if run != nil {
		select {
		case <-run.done:
		case <-ctx.Done():
			return domain.Execution{}, ctx.Err()
		}
	}
	return service.Get(id)
}

func (service *ExecutionService) Close(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("shutdown context is required")
	}
	service.mu.Lock()
	service.closed = true
	runs := make([]*executionRun, 0, len(service.runs))
	for _, run := range service.runs {
		runs = append(runs, run)
		run.cancel()
	}
	service.mu.Unlock()
	for _, run := range runs {
		select {
		case <-run.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (service *ExecutionService) validateRequest(request ExecutionRequest) ([]domain.ExecutionTask, string, error) {
	if len(request.Tasks) == 0 || len(request.Tasks) > maxExecutionTasks {
		return nil, "", fmt.Errorf("%w: task count must be between 1 and %d", ErrInvalidExecution, maxExecutionTasks)
	}
	now := time.Now().UTC()
	tasks := make([]domain.ExecutionTask, 0, len(request.Tasks))
	canonical := make([]ExecutionTaskRequest, 0, len(request.Tasks))
	seen := make(map[string]struct{}, len(request.Tasks))
	schedulerTasks := make([]scheduler.Task, 0, len(request.Tasks))
	for _, input := range request.Tasks {
		if !protocol.ValidSiteName(input.ID) || len(input.ID) > 128 || !protocol.ValidSiteName(input.Type) || len(input.Type) > 128 {
			return nil, "", fmt.Errorf("%w: task id or type is invalid", ErrInvalidExecution)
		}
		if _, exists := seen[input.ID]; exists {
			return nil, "", fmt.Errorf("%w: task id is duplicated", ErrInvalidExecution)
		}
		seen[input.ID] = struct{}{}
		executor, exists := service.executors[input.Type]
		if !exists {
			return nil, "", fmt.Errorf("%w: %s", ErrExecutionUnsupported, input.Type)
		}
		if len(input.Dependencies) > maxExecutionTasks || len(input.ResourceLock) > 256 || !domain.ValidExecutionResourceLock(input.ResourceLock) {
			return nil, "", fmt.Errorf("%w: task dependencies or resource lock are invalid", ErrInvalidExecution)
		}
		if input.TimeoutMillis == 0 {
			input.TimeoutMillis = defaultExecutionTimeout
		}
		if input.TimeoutMillis < 1 || input.TimeoutMillis > maxExecutionTimeout {
			return nil, "", fmt.Errorf("%w: timeout_ms must be between 1 and %d", ErrInvalidExecution, maxExecutionTimeout)
		}
		if input.MaxAttempts == 0 {
			input.MaxAttempts = 1
		}
		if input.MaxAttempts < 1 || input.MaxAttempts > maxExecutionAttempts || input.RetryInitialMillis < 0 || input.RetryInitialMillis > 30000 || input.RetryMaxMillis < 0 || input.RetryMaxMillis > 60000 || input.RetryInitialMillis > 0 && input.RetryMaxMillis == 0 || input.RetryMaxMillis > 0 && input.RetryInitialMillis > input.RetryMaxMillis {
			return nil, "", fmt.Errorf("%w: retry policy is out of range", ErrInvalidExecution)
		}
		parameters, err := normalizeExecutionJSON(input.Parameters, true)
		if err != nil {
			return nil, "", fmt.Errorf("%w: task parameters are unsafe or invalid", ErrInvalidExecution)
		}
		validated, err := executor.ValidateParameters(parameters)
		if err != nil {
			return nil, "", fmt.Errorf("%w: task parameters failed validation", ErrInvalidExecution)
		}
		parameters, err = normalizeExecutionJSON(validated, true)
		if err != nil {
			return nil, "", fmt.Errorf("%w: executor returned unsafe validated parameters", ErrInvalidExecution)
		}
		input.Parameters = append(json.RawMessage(nil), parameters...)
		for _, dependency := range input.Dependencies {
			if !protocol.ValidSiteName(dependency) || dependency == input.ID {
				return nil, "", fmt.Errorf("%w: task dependency is invalid", ErrInvalidExecution)
			}
		}
		task := domain.ExecutionTask{
			ID: input.ID, Type: input.Type, Status: domain.ExecutionTaskQueued,
			Dependencies: append([]string(nil), input.Dependencies...), ResourceLock: input.ResourceLock,
			TimeoutMillis: input.TimeoutMillis, MaxAttempts: input.MaxAttempts,
			RetryInitialMillis: input.RetryInitialMillis, RetryMaxMillis: input.RetryMaxMillis,
			Parameters: parameters, CreatedAt: now, UpdatedAt: now,
		}
		tasks = append(tasks, task)
		canonical = append(canonical, input)
		schedulerTasks = append(schedulerTasks, scheduler.Task{
			ID: input.ID, Dependencies: input.Dependencies, Lock: input.ResourceLock,
			Timeout: time.Duration(input.TimeoutMillis) * time.Millisecond,
			Retry: scheduler.RetryPolicy{
				MaxAttempts: input.MaxAttempts,
				InitialWait: time.Duration(input.RetryInitialMillis) * time.Millisecond,
				MaxWait:     time.Duration(input.RetryMaxMillis) * time.Millisecond,
			},
			Action: func(context.Context) error { return nil },
		})
	}
	if _, err := scheduler.New(schedulerTasks, service.concurrency); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidExecution, err)
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, "", fmt.Errorf("canonicalize execution request: %w", err)
	}
	return tasks, protocol.SHA256(encoded), nil
}

func (service *ExecutionService) runExecution(ctx context.Context, id, actorID string, run *executionRun) {
	defer func() {
		service.mu.Lock()
		delete(service.runs, id)
		close(run.done)
		service.mu.Unlock()
	}()
	started, err := service.change(id, actorID, "execution.running", "", "", nil, func(execution *domain.Execution) error {
		if execution.Status != domain.ExecutionQueued {
			return ErrExecutionStateConflict
		}
		now := nextTimestamp(execution.UpdatedAt)
		execution.Status = domain.ExecutionRunning
		execution.StartedAt = now
		return nil
	})
	if err != nil {
		service.failUnstarted(id, actorID, "execution could not be audited before dispatch")
		return
	}
	schedulerTasks := make([]scheduler.Task, 0, len(started.Tasks))
	for _, task := range started.Tasks {
		task := task
		executor := service.executors[task.Type]
		schedulerTasks = append(schedulerTasks, scheduler.Task{
			ID: task.ID, Dependencies: task.Dependencies, Lock: task.ResourceLock,
			Timeout: time.Duration(task.TimeoutMillis) * time.Millisecond,
			Retry: scheduler.RetryPolicy{
				MaxAttempts: task.MaxAttempts,
				InitialWait: time.Duration(task.RetryInitialMillis) * time.Millisecond,
				MaxWait:     time.Duration(task.RetryMaxMillis) * time.Millisecond,
			},
			Action: func(taskContext context.Context) error {
				return service.executeTask(taskContext, id, actorID, task, executor)
			},
		})
	}
	engine, err := scheduler.New(schedulerTasks, service.concurrency)
	if err != nil {
		service.finishExecution(id, actorID, nil, err)
		return
	}
	results, err := engine.Run(ctx)
	service.finishExecution(id, actorID, results, err)
}

func (service *ExecutionService) executeTask(ctx context.Context, executionID, actorID string, task domain.ExecutionTask, executor ports.ExecutionExecutor) error {
	attemptNumber, err := service.startAttempt(executionID, actorID, task.ID)
	if err != nil {
		return scheduler.Permanent(err)
	}
	current, err := service.repository.Get(executionID)
	if err != nil {
		return scheduler.Permanent(err)
	}
	var storedTask domain.ExecutionTask
	for _, candidate := range current.Tasks {
		if candidate.ID == task.ID {
			storedTask = candidate
			break
		}
	}
	startedAt := time.Now()
	executionResult, executeErr := executor.Execute(ctx, storedTask)
	duration := executionResult.Duration
	if duration < 0 {
		duration = 0
	}
	if duration == 0 {
		duration = time.Since(startedAt)
	}
	if executeErr == nil && ctx.Err() != nil {
		executeErr = ctx.Err()
	}
	if ctx.Err() != nil {
		executionResult.Output = nil
	}
	if executeErr == nil {
		switch executionResult.Status {
		case domain.ExecutionTaskSucceeded:
			if executionResult.Error != "" {
				executeErr = errors.New("executor reported success with an error")
			}
		case domain.ExecutionTaskFailed:
			executeErr = errors.New(executionResult.Error)
			if executionResult.Error == "" {
				executeErr = errors.New("executor reported failure without an error")
			}
		default:
			executeErr = errors.New("executor returned an invalid result status")
		}
	}
	var output json.RawMessage
	if len(executionResult.Output) > 0 {
		var outputErr error
		output, outputErr = normalizeExecutionJSON(executionResult.Output, false)
		if outputErr != nil {
			output = nil
			if executeErr == nil {
				executeErr = errors.New("executor returned an invalid or unsafe result")
			}
		}
	}
	if executeErr == nil {
		if err := service.finishAttempt(executionID, actorID, task.ID, attemptNumber, domain.ExecutionTaskSucceeded, output, "", duration); err != nil {
			return scheduler.Permanent(err)
		}
		return nil
	}
	message := safeExecutionError(executeErr)
	status := domain.ExecutionTaskFailed
	retryable := executor.Retryable(executeErr) && attemptNumber < task.MaxAttempts
	switch ctx.Err() {
	case context.Canceled:
		status = domain.ExecutionTaskCancelled
	case context.DeadlineExceeded:
		if attemptNumber < task.MaxAttempts {
			status = domain.ExecutionTaskRetrying
		} else {
			status = domain.ExecutionTaskTimedOut
		}
	default:
		if retryable {
			status = domain.ExecutionTaskRetrying
		}
	}
	if err := service.finishAttempt(executionID, actorID, task.ID, attemptNumber, status, output, message, duration); err != nil {
		return scheduler.Permanent(err)
	}
	if status == domain.ExecutionTaskRetrying {
		return scheduler.Retry(errors.New(message))
	}
	if status == domain.ExecutionTaskCancelled {
		return context.Canceled
	}
	if status == domain.ExecutionTaskTimedOut {
		return context.DeadlineExceeded
	}
	return scheduler.Permanent(errors.New(message))
}

func (service *ExecutionService) startAttempt(id, actorID, taskID string) (int, error) {
	var number int
	_, err := service.change(id, actorID, "execution.task.attempt_started", taskID, domain.ExecutionTaskRunning, nil, func(execution *domain.Execution) error {
		for index := range execution.Tasks {
			task := &execution.Tasks[index]
			if task.ID != taskID {
				continue
			}
			if task.Status != domain.ExecutionTaskQueued && task.Status != domain.ExecutionTaskRetrying {
				return ErrExecutionStateConflict
			}
			now := nextTimestamp(task.UpdatedAt)
			task.Status = domain.ExecutionTaskRunning
			if task.StartedAt.IsZero() {
				task.StartedAt = now
			}
			task.UpdatedAt = now
			number = len(task.Attempts) + 1
			task.Attempts = append(task.Attempts, domain.ExecutionAttempt{Number: number, Status: domain.ExecutionTaskRunning, StartedAt: now})
			return nil
		}
		return ErrExecutionNotFound
	})
	return number, err
}

func (service *ExecutionService) finishAttempt(id, actorID, taskID string, number int, status domain.ExecutionTaskStatus, output json.RawMessage, message string, duration time.Duration) error {
	_, err := service.change(id, actorID, "execution.task.attempt_"+string(status), taskID, status, errors.New(message), func(execution *domain.Execution) error {
		for index := range execution.Tasks {
			task := &execution.Tasks[index]
			if task.ID != taskID || len(task.Attempts) == 0 || task.Attempts[len(task.Attempts)-1].Number != number {
				continue
			}
			now := nextTimestamp(task.UpdatedAt)
			attempt := &task.Attempts[len(task.Attempts)-1]
			if attempt.Status != domain.ExecutionTaskRunning {
				return ErrExecutionStateConflict
			}
			attempt.Status = status
			attempt.FinishedAt = now
			attempt.DurationMS = duration.Milliseconds()
			attempt.Result = append(json.RawMessage(nil), output...)
			attempt.Error = safeExecutionError(errors.New(message))
			task.Status = status
			task.UpdatedAt = now
			task.Error = attempt.Error
			if status == domain.ExecutionTaskRetrying {
				task.FinishedAt = time.Time{}
			} else {
				task.FinishedAt = now
			}
			if status == domain.ExecutionTaskSucceeded {
				task.Result = append(json.RawMessage(nil), output...)
				task.Error = ""
			}
			return nil
		}
		return ErrExecutionNotFound
	})
	return err
}

func (service *ExecutionService) finishExecution(id, actorID string, results []scheduler.Result, runErr error) {
	resultByID := make(map[string]scheduler.Result, len(results))
	for _, result := range results {
		resultByID[result.ID] = result
	}
	execution, err := service.Get(id)
	if err != nil {
		return
	}
	for _, task := range execution.Tasks {
		result, exists := resultByID[task.ID]
		if !exists {
			continue
		}
		status := schedulerTaskStatus(result.Status)
		if task.Status == status && (status == domain.ExecutionTaskSucceeded || status == domain.ExecutionTaskFailed || status == domain.ExecutionTaskCancelled || status == domain.ExecutionTaskTimedOut || status == domain.ExecutionTaskBlocked) {
			continue
		}
		message := safeExecutionError(errors.New(result.Error))
		_, updateErr := service.change(id, actorID, "execution.task."+string(status), task.ID, status, errors.New(message), func(current *domain.Execution) error {
			for index := range current.Tasks {
				currentTask := &current.Tasks[index]
				if currentTask.ID != task.ID {
					continue
				}
				now := nextTimestamp(currentTask.UpdatedAt)
				currentTask.Status = status
				currentTask.UpdatedAt = now
				currentTask.FinishedAt = now
				currentTask.Error = message
				if len(currentTask.Attempts) > 0 {
					attempt := &currentTask.Attempts[len(currentTask.Attempts)-1]
					if attempt.Status == domain.ExecutionTaskRunning {
						attempt.Status = status
						attempt.FinishedAt = now
						attempt.DurationMS = now.Sub(attempt.StartedAt).Milliseconds()
						attempt.Error = message
					}
				}
				return nil
			}
			return ErrExecutionNotFound
		})
		if updateErr != nil {
			runErr = errors.Join(runErr, updateErr)
		}
	}
	final, err := service.Get(id)
	if err != nil || terminalExecution(final.Status) {
		return
	}
	status := aggregateExecutionStatus(final.Tasks)
	message := safeExecutionError(runErr)
	if message == "" {
		switch status {
		case domain.ExecutionFailed:
			message = "one or more tasks failed or were blocked"
		case domain.ExecutionTimedOut:
			message = "one or more tasks timed out"
		case domain.ExecutionCancelled:
			message = "execution was cancelled"
		}
	}
	auditErr := runErr
	if auditErr == nil && message != "" {
		auditErr = errors.New(message)
	}
	_, _ = service.change(id, actorID, "execution."+string(status), "", "", auditErr, func(current *domain.Execution) error {
		current.Status = status
		current.FinishedAt = nextTimestamp(current.UpdatedAt)
		current.Error = message
		return nil
	})
}

func (service *ExecutionService) change(id, actorID, action, taskID string, taskStatus domain.ExecutionTaskStatus, eventErr error, mutate func(*domain.Execution) error) (domain.Execution, error) {
	service.stateMu.Lock()
	defer service.stateMu.Unlock()
	execution, err := service.repository.Get(id)
	if err != nil {
		return domain.Execution{}, err
	}
	if mutate != nil {
		if err := mutate(&execution); err != nil {
			return domain.Execution{}, err
		}
	}
	timestamp := nextTimestamp(execution.UpdatedAt)
	execution.UpdatedAt = timestamp
	if taskStatus == "" && taskID != "" {
		for _, task := range execution.Tasks {
			if task.ID == taskID {
				taskStatus = task.Status
				break
			}
		}
	}
	event := domain.ExecutionEvent{
		ActorID: actorID, Action: action, TaskID: taskID, Status: execution.Status,
		TaskStatus: taskStatus, Timestamp: timestamp, Error: safeExecutionError(eventErr),
	}
	execution.Events = append(execution.Events, event)
	updated, err := service.repository.Update(execution)
	if err != nil {
		return domain.Execution{}, err
	}
	if err := service.writeAudit(updated.ID, event); err != nil {
		return updated, err
	}
	return updated, nil
}

func (service *ExecutionService) writeAudit(executionID string, event domain.ExecutionEvent) error {
	eventID, err := newAuditEventID()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"actor_id": event.ActorID, "execution_id": executionID, "task_id": event.TaskID,
		"action": event.Action, "status": event.Status, "task_status": event.TaskStatus,
		"error": event.Error,
	})
	if err != nil {
		return err
	}
	_, _, err = service.events.Append(domain.Event{
		EventID: eventID, Timestamp: event.Timestamp, Type: event.Action, Payload: payload,
	})
	return err
}

func (service *ExecutionService) recoverInterrupted() error {
	executions, err := service.repository.List()
	if err != nil {
		return err
	}
	for _, execution := range executions {
		if terminalExecution(execution.Status) {
			continue
		}
		actorID := "system"
		recovered, err := service.change(execution.ID, actorID, "execution.recovered_after_restart", "", "", errors.New("provider restarted before execution completed; outcome is unknown"), func(current *domain.Execution) error {
			now := nextTimestamp(current.UpdatedAt)
			current.Status = domain.ExecutionFailed
			current.FinishedAt = now
			current.Error = "provider restarted before execution completed; outcome is unknown"
			for index := range current.Tasks {
				task := &current.Tasks[index]
				switch task.Status {
				case domain.ExecutionTaskQueued:
					task.Status = domain.ExecutionTaskCancelled
					task.Error = "cancelled during provider restart recovery"
				case domain.ExecutionTaskRunning, domain.ExecutionTaskRetrying:
					task.Status = domain.ExecutionTaskFailed
					task.Error = "provider restarted while task outcome was unknown"
					if len(task.Attempts) > 0 {
						attempt := &task.Attempts[len(task.Attempts)-1]
						if attempt.Status == domain.ExecutionTaskRunning {
							attempt.Status = domain.ExecutionTaskFailed
							attempt.FinishedAt = now
							attempt.DurationMS = now.Sub(attempt.StartedAt).Milliseconds()
							attempt.Error = task.Error
						}
					}
				}
				task.UpdatedAt = now
				if terminalTask(task.Status) {
					task.FinishedAt = now
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("recover execution %s: %w", execution.ID, err)
		}
		if len(recovered.Events) == 0 {
			return fmt.Errorf("recovery audit event missing for %s", execution.ID)
		}
	}
	return nil
}

func (service *ExecutionService) failUnstarted(id, actorID, message string) {
	service.stateMu.Lock()
	defer service.stateMu.Unlock()
	execution, err := service.repository.Get(id)
	if err != nil || terminalExecution(execution.Status) {
		return
	}
	now := nextTimestamp(execution.UpdatedAt)
	execution.Status = domain.ExecutionFailed
	execution.FinishedAt = now
	execution.UpdatedAt = now
	execution.Error = safeExecutionError(errors.New(message))
	for index := range execution.Tasks {
		task := &execution.Tasks[index]
		if task.Status == domain.ExecutionTaskQueued {
			task.Status = domain.ExecutionTaskCancelled
			task.Error = "execution did not start"
			task.FinishedAt = now
			task.UpdatedAt = now
		}
	}
	event := domain.ExecutionEvent{ActorID: actorID, Action: "execution.dispatch_failed", Status: execution.Status, Timestamp: now, Error: execution.Error}
	execution.Events = append(execution.Events, event)
	updated, err := service.repository.Update(execution)
	if err == nil {
		_ = service.writeAudit(updated.ID, event)
	}
}

func (service *ExecutionService) validateRequestTaskTypes(request ExecutionRequest) bool {
	for _, task := range request.Tasks {
		if _, ok := service.executors[task.Type]; !ok {
			return false
		}
	}
	return true
}

func normalizeExecutionJSON(value json.RawMessage, rejectSecrets bool) (json.RawMessage, error) {
	if len(value) == 0 {
		value = json.RawMessage(`{}`)
	}
	if len(value) > maxExecutionPayload {
		return nil, fmt.Errorf("JSON payload exceeds its size limit")
	}
	decoder := json.NewDecoder(strings.NewReader(string(value)))
	decoder.UseNumber()
	var parsed any
	if err := decoder.Decode(&parsed); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("JSON payload contains trailing data")
	}
	if _, ok := parsed.(map[string]any); !ok {
		return nil, fmt.Errorf("JSON payload must be an object")
	}
	sanitized, err := sanitizeExecutionValue(parsed, rejectSecrets, 0)
	if err != nil {
		return nil, err
	}
	parsed = sanitized
	encoded, err := json.Marshal(parsed)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxExecutionPayload {
		return nil, fmt.Errorf("JSON payload exceeds its size limit")
	}
	return encoded, nil
}

func sanitizeExecutionValue(value any, rejectSecrets bool, depth int) (any, error) {
	if depth > 16 {
		return nil, fmt.Errorf("JSON payload nesting is too deep")
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if sensitiveExecutionKey(key) {
				if rejectSecrets {
					return nil, fmt.Errorf("secret-bearing fields are not accepted")
				}
				typed[key] = "[REDACTED]"
				continue
			}
			sanitized, err := sanitizeExecutionValue(child, rejectSecrets, depth+1)
			if err != nil {
				return nil, err
			}
			typed[key] = sanitized
		}
	case []any:
		for index := range typed {
			sanitized, err := sanitizeExecutionValue(typed[index], rejectSecrets, depth+1)
			if err != nil {
				return nil, err
			}
			typed[index] = sanitized
		}
	case string:
		if len(typed) > 8192 {
			return nil, fmt.Errorf("string value exceeds its size limit")
		}
		redacted := observability.Redact(typed)
		if rejectSecrets && redacted != typed {
			return nil, fmt.Errorf("secret-like values are not accepted")
		}
		return redacted, nil
	}
	return value, nil
}

func sensitiveExecutionKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
	for _, marker := range []string{"password", "passwd", "secret", "token", "apikey", "privatekey", "credential", "authorization", "accesskey"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func safeExecutionError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(observability.Redact(err.Error()))
	if len(message) > 1024 {
		message = message[:1024]
	}
	return message
}

func schedulerTaskStatus(status scheduler.Status) domain.ExecutionTaskStatus {
	switch status {
	case scheduler.StatusSuccess:
		return domain.ExecutionTaskSucceeded
	case scheduler.StatusTimedOut:
		return domain.ExecutionTaskTimedOut
	case scheduler.StatusCancelled:
		return domain.ExecutionTaskCancelled
	case scheduler.StatusBlocked:
		return domain.ExecutionTaskBlocked
	default:
		return domain.ExecutionTaskFailed
	}
}

func aggregateExecutionStatus(tasks []domain.ExecutionTask) domain.ExecutionStatus {
	failed, blocked, timedOut, cancelled := false, false, false, false
	for _, task := range tasks {
		switch task.Status {
		case domain.ExecutionTaskFailed:
			failed = true
		case domain.ExecutionTaskBlocked:
			blocked = true
		case domain.ExecutionTaskTimedOut:
			timedOut = true
		case domain.ExecutionTaskCancelled:
			cancelled = true
		}
	}
	if failed {
		return domain.ExecutionFailed
	}
	if timedOut {
		return domain.ExecutionTimedOut
	}
	if cancelled {
		return domain.ExecutionCancelled
	}
	if blocked {
		return domain.ExecutionFailed
	}
	return domain.ExecutionSucceeded
}

func executionHasTaskType(execution domain.Execution, taskType string) bool {
	for _, task := range execution.Tasks {
		if task.Type == taskType {
			return true
		}
	}
	return false
}

func terminalExecution(status domain.ExecutionStatus) bool {
	return status == domain.ExecutionSucceeded || status == domain.ExecutionFailed || status == domain.ExecutionCancelled || status == domain.ExecutionTimedOut
}

func terminalTask(status domain.ExecutionTaskStatus) bool {
	return status == domain.ExecutionTaskSucceeded || status == domain.ExecutionTaskFailed || status == domain.ExecutionTaskCancelled || status == domain.ExecutionTaskTimedOut || status == domain.ExecutionTaskBlocked
}

func nextTimestamp(previous time.Time) time.Time {
	now := time.Now().UTC()
	if !now.After(previous) {
		return previous.Add(time.Nanosecond)
	}
	return now
}

func newExecutionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "exec-" + hex.EncodeToString(raw[:]), nil
}

func newAuditEventID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "evt-" + hex.EncodeToString(raw[:]), nil
}

func validExecutionStatus(status domain.ExecutionStatus) bool {
	switch status {
	case domain.ExecutionQueued, domain.ExecutionRunning, domain.ExecutionSucceeded, domain.ExecutionFailed, domain.ExecutionCancelled, domain.ExecutionTimedOut:
		return true
	default:
		return false
	}
}
