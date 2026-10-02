// Package scheduler provides the execution primitives used by future
// provisioning adapters. It deliberately knows nothing about vendors or
// external commands.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusRetrying  Status = "retrying"
	StatusSuccess   Status = "success"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
	StatusBlocked   Status = "blocked"
)

type ErrorKind string

const (
	Retryable    ErrorKind = "retryable"
	NonRetryable ErrorKind = "non_retryable"
)

var (
	ErrInvalidTaskGraph = errors.New("invalid task graph")
	ErrTaskFailed       = errors.New("task failed")
)

type TaskError struct {
	Kind ErrorKind
	Err  error
}

func (err *TaskError) Error() string {
	if err == nil || err.Err == nil {
		return string(NonRetryable)
	}
	return err.Err.Error()
}

func (err *TaskError) Unwrap() error { return err.Err }

func Retry(err error) error {
	if err == nil {
		return nil
	}
	return &TaskError{Kind: Retryable, Err: err}
}

func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &TaskError{Kind: NonRetryable, Err: err}
}

type RetryPolicy struct {
	MaxAttempts int
	InitialWait time.Duration
	MaxWait     time.Duration
}

type Task struct {
	ID           string
	Dependencies []string
	Lock         string
	Timeout      time.Duration
	Retry        RetryPolicy
	Action       func(context.Context) error
}

type Result struct {
	ID         string    `json:"id"`
	Status     Status    `json:"status"`
	Attempts   int       `json:"attempts"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type Scheduler struct {
	tasks          map[string]Task
	maxConcurrency int
}

type taskOutcome struct {
	result Result
}

func New(tasks []Task, maxConcurrency int) (*Scheduler, error) {
	if maxConcurrency < 1 || maxConcurrency > 1024 {
		return nil, fmt.Errorf("%w: concurrency must be between 1 and 1024", ErrInvalidTaskGraph)
	}
	graph := make(map[string]Task, len(tasks))
	for _, task := range tasks {
		if err := validateTask(task); err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrInvalidTaskGraph, task.ID, err)
		}
		if _, exists := graph[task.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate task %q", ErrInvalidTaskGraph, task.ID)
		}
		graph[task.ID] = task
	}
	for _, task := range tasks {
		seen := make(map[string]struct{}, len(task.Dependencies))
		for _, dependency := range task.Dependencies {
			if _, exists := graph[dependency]; !exists {
				return nil, fmt.Errorf("%w: task %q depends on unknown task %q", ErrInvalidTaskGraph, task.ID, dependency)
			}
			if _, exists := seen[dependency]; exists {
				return nil, fmt.Errorf("%w: task %q has duplicate dependency %q", ErrInvalidTaskGraph, task.ID, dependency)
			}
			seen[dependency] = struct{}{}
		}
	}
	if hasCycle(graph) {
		return nil, fmt.Errorf("%w: dependency cycle detected", ErrInvalidTaskGraph)
	}
	return &Scheduler{tasks: graph, maxConcurrency: maxConcurrency}, nil
}

func (scheduler *Scheduler) Run(ctx context.Context) ([]Result, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is required", ErrInvalidTaskGraph)
	}
	status := make(map[string]Status, len(scheduler.tasks))
	results := make(map[string]Result, len(scheduler.tasks))
	for id := range scheduler.tasks {
		status[id] = StatusPending
	}
	ready := make([]string, 0, len(scheduler.tasks))
	for id := range scheduler.tasks {
		if len(scheduler.tasks[id].Dependencies) == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	locks := make(map[string]string)
	outcomes := make(chan taskOutcome, len(scheduler.tasks))
	running := 0

	for len(results) < len(scheduler.tasks) {
		if ctx.Err() != nil {
			scheduler.cancelPending(ctx, status, results)
		}

		launched := true
		for launched && running < scheduler.maxConcurrency && ctx.Err() == nil {
			launched = false
			for index, id := range ready {
				if running >= scheduler.maxConcurrency {
					break
				}
				task := scheduler.tasks[id]
				if task.Lock != "" {
					if _, held := locks[task.Lock]; held {
						continue
					}
					locks[task.Lock] = id
				}
				ready = append(ready[:index], ready[index+1:]...)
				status[id] = StatusRunning
				running++
				launched = true
				go func(task Task) { outcomes <- taskOutcome{result: execute(ctx, task)} }(task)
				break
			}
		}

		if len(results) == len(scheduler.tasks) {
			break
		}
		if running == 0 {
			if ctx.Err() != nil {
				continue
			}
			// A valid graph cannot reach this state unless a dependency was
			// terminal and the dependent has not yet been classified.
			scheduler.blockDependents(status, results, &ready)
			continue
		}

		outcome := <-outcomes
		running--
		result := outcome.result
		results[result.ID] = result
		status[result.ID] = result.Status
		if task := scheduler.tasks[result.ID]; task.Lock != "" {
			delete(locks, task.Lock)
		}
		if ctx.Err() != nil {
			scheduler.cancelPending(ctx, status, results)
		} else {
			scheduler.updateReady(status, results, &ready)
		}
	}

	ordered := make([]Result, 0, len(results))
	ids := make([]string, 0, len(results))
	for id := range results {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		ordered = append(ordered, results[id])
	}
	return ordered, nil
}

func (scheduler *Scheduler) cancelPending(ctx context.Context, status map[string]Status, results map[string]Result) {
	for id, current := range status {
		if current == StatusPending || current == StatusRetrying {
			status[id] = StatusCancelled
			results[id] = Result{ID: id, Status: StatusCancelled, FinishedAt: time.Now().UTC(), Error: ctx.Err().Error()}
		}
	}
}

func (scheduler *Scheduler) updateReady(status map[string]Status, results map[string]Result, ready *[]string) {
	queued := make(map[string]struct{}, len(*ready))
	for _, id := range *ready {
		queued[id] = struct{}{}
	}
	for id, task := range scheduler.tasks {
		if status[id] != StatusPending {
			continue
		}
		allSuccess := true
		blocked := false
		for _, dependency := range task.Dependencies {
			switch status[dependency] {
			case StatusSuccess:
			case StatusFailed, StatusCancelled, StatusBlocked:
				blocked = true
			default:
				allSuccess = false
			}
		}
		if blocked {
			status[id] = StatusBlocked
			results[id] = Result{ID: id, Status: StatusBlocked, FinishedAt: time.Now().UTC(), Error: "dependency did not succeed"}
			continue
		}
		if allSuccess {
			if _, exists := queued[id]; !exists {
				*ready = append(*ready, id)
				queued[id] = struct{}{}
			}
		}
	}
	sort.Strings(*ready)
}

func (scheduler *Scheduler) blockDependents(status map[string]Status, results map[string]Result, ready *[]string) {
	scheduler.updateReady(status, results, ready)
	if len(results) == len(scheduler.tasks) {
		return
	}
	for id, current := range status {
		if current == StatusPending {
			status[id] = StatusBlocked
			results[id] = Result{ID: id, Status: StatusBlocked, FinishedAt: time.Now().UTC(), Error: "task could not become runnable"}
		}
	}
}

func execute(parent context.Context, task Task) (result Result) {
	result = Result{ID: task.ID, Status: StatusFailed, StartedAt: time.Now().UTC()}
	defer func() {
		if recover() != nil {
			result.Status = StatusFailed
			result.Error = "task action panicked"
		}
		result.FinishedAt = time.Now().UTC()
	}()
	policy := task.Retry
	if policy.MaxAttempts == 0 {
		policy.MaxAttempts = 1
	}
	if policy.MaxAttempts < 1 {
		policy.MaxAttempts = 1
	}
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		result.Attempts = attempt
		if parent.Err() != nil {
			result.Status = StatusCancelled
			result.Error = parent.Err().Error()
			break
		}
		taskContext := parent
		cancel := func() {}
		if task.Timeout > 0 {
			taskContext, cancel = context.WithTimeout(parent, task.Timeout)
		}
		err := task.Action(taskContext)
		cancel()
		if err == nil {
			result.Status = StatusSuccess
			break
		}
		if parent.Err() != nil {
			result.Status = StatusCancelled
			result.Error = parent.Err().Error()
			break
		}
		if taskContext.Err() == context.DeadlineExceeded {
			err = Retry(fmt.Errorf("task timeout: %w", err))
		}
		result.Error = limitError(err)
		if !isRetryable(err) || attempt == policy.MaxAttempts {
			result.Status = StatusFailed
			break
		}
		wait := backoff(policy, attempt)
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-parent.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				result.Status = StatusCancelled
				result.Error = parent.Err().Error()
				attempt = policy.MaxAttempts
			}
		}
	}
	return result
}

func backoff(policy RetryPolicy, attempt int) time.Duration {
	if policy.InitialWait <= 0 {
		return 0
	}
	value := policy.InitialWait
	for index := 1; index < attempt; index++ {
		if policy.MaxWait > 0 && value >= policy.MaxWait/2 {
			value = policy.MaxWait
			break
		}
		value *= 2
	}
	if policy.MaxWait > 0 && value > policy.MaxWait {
		return policy.MaxWait
	}
	return value
}

func isRetryable(err error) bool {
	var taskErr *TaskError
	return errors.As(err, &taskErr) && taskErr.Kind == Retryable
}

func limitError(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 1024 {
		return message[:1024]
	}
	return message
}

func validateTask(task Task) error {
	if strings.TrimSpace(task.ID) == "" || len(task.ID) > 256 || strings.ContainsAny(task.ID, "\r\n") {
		return errors.New("task id is invalid")
	}
	if task.Action == nil {
		return errors.New("task action is required")
	}
	if task.Timeout < 0 || task.Retry.InitialWait < 0 || task.Retry.MaxWait < 0 || task.Retry.MaxAttempts < 0 || task.Retry.MaxAttempts > 100 {
		return errors.New("task timing or retry policy is invalid")
	}
	if task.Lock != "" && (len(task.Lock) > 256 || strings.ContainsAny(task.Lock, "\r\n")) {
		return errors.New("task lock is invalid")
	}
	return nil
}

func hasCycle(graph map[string]Task) bool {
	visiting := make(map[string]bool, len(graph))
	visited := make(map[string]bool, len(graph))
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			return true
		}
		if visited[id] {
			return false
		}
		visiting[id] = true
		for _, dependency := range graph[id].Dependencies {
			if visit(dependency) {
				return true
			}
		}
		delete(visiting, id)
		visited[id] = true
		return false
	}
	for id := range graph {
		if visit(id) {
			return true
		}
	}
	return false
}
