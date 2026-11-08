package filesystem

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"infraflow/internal/domain"
	"infraflow/internal/ports"
	"infraflow/pkg/protocol"
)

func TestJobStorePersistsListsAndUpdatesJobs(t *testing.T) {
	root := t.TempDir()
	store, err := NewJobStore(root)
	if err != nil {
		t.Fatal(err)
	}
	job := testJob("job-first")
	if err := store.Create(job); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(job); !os.IsExist(err) {
		t.Fatalf("expected duplicate job rejection, got %v", err)
	}
	loaded, err := store.Get(job.ID)
	if err != nil || loaded.ID != job.ID {
		t.Fatalf("job was not loaded: %#v, %v", loaded, err)
	}
	job.Status = domain.JobStatusCancelled
	job.UpdatedAt = job.UpdatedAt.Add(time.Second)
	if err := store.Update(job); err != nil {
		t.Fatal(err)
	}
	jobs, err := store.List()
	if err != nil || len(jobs) != 1 || jobs[0].Status != domain.JobStatusCancelled {
		t.Fatalf("unexpected stored jobs: %#v, %v", jobs, err)
	}
	info, err := os.Stat(filepath.Join(root, ".jobs", job.ID+".json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("job file should be private: %v, %v", info, err)
	}
}

func TestJobStoreRejectsCorruptAndUnsafeFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".jobs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".jobs", "job-bad.json"), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewJobStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err == nil {
		t.Fatal("expected corrupt job file to be rejected")
	}

	unsafeRoot := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(unsafeRoot, ".jobs")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := NewJobStore(unsafeRoot); err == nil {
		t.Fatal("expected symlink job directory to be rejected")
	}
}

func TestExecutionStorePersistsLifecycleAndIdempotency(t *testing.T) {
	root := t.TempDir()
	store, err := NewExecutionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	execution := testExecution("exec-first", "request-one")
	created, isNew, err := store.Create(execution)
	if err != nil || !isNew || created.ID != execution.ID {
		t.Fatalf("execution was not created: %#v new=%v err=%v", created, isNew, err)
	}
	if _, isNew, err := store.Create(testExecution("exec-replay", "request-one")); err != nil || isNew {
		t.Fatalf("idempotent replay created another execution: new=%v err=%v", isNew, err)
	}
	if _, _, err := store.Create(testExecution("exec-conflict", "request-two")); err != ports.ErrIdempotencyConflict {
		t.Fatalf("idempotency key reuse with a different request was accepted: %v", err)
	}

	created.Status = domain.ExecutionRunning
	created.StartedAt = created.CreatedAt.Add(time.Second)
	created.UpdatedAt = created.StartedAt
	created.Tasks[0].Status = domain.ExecutionTaskRunning
	created.Tasks[0].StartedAt = created.StartedAt
	created.Tasks[0].UpdatedAt = created.StartedAt
	created.Tasks[0].Attempts = []domain.ExecutionAttempt{{Number: 1, Status: domain.ExecutionTaskRunning, StartedAt: created.StartedAt}}
	created.Events = append(created.Events, domain.ExecutionEvent{ActorID: "user-admin", Action: "task.started", TaskID: "task-one", Status: domain.ExecutionRunning, TaskStatus: domain.ExecutionTaskRunning, Timestamp: created.StartedAt})
	created, err = store.Update(created)
	if err != nil || created.Revision != 2 {
		t.Fatalf("running transition was not persisted: %#v, %v", created, err)
	}

	finished := created.StartedAt.Add(150 * time.Millisecond)
	created.Status = domain.ExecutionSucceeded
	created.FinishedAt = finished
	created.UpdatedAt = finished
	created.Tasks[0].Status = domain.ExecutionTaskSucceeded
	created.Tasks[0].FinishedAt = finished
	created.Tasks[0].UpdatedAt = finished
	created.Tasks[0].Result = json.RawMessage(`{"verified":true}`)
	created.Tasks[0].Attempts[0].Status = domain.ExecutionTaskSucceeded
	created.Tasks[0].Attempts[0].FinishedAt = finished
	created.Tasks[0].Attempts[0].DurationMS = 150
	created.Tasks[0].Attempts[0].Result = json.RawMessage(`{"verified":true}`)
	created.Events = append(created.Events, domain.ExecutionEvent{ActorID: "user-admin", Action: "execution.succeeded", Status: domain.ExecutionSucceeded, Timestamp: finished})
	if _, err := store.Update(created); err != nil {
		t.Fatal(err)
	}

	reloadedStore, err := NewExecutionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := reloadedStore.Get(execution.ID)
	var result map[string]bool
	decodeErr := json.Unmarshal(reloaded.Tasks[0].Result, &result)
	if err != nil || decodeErr != nil || reloaded.Status != domain.ExecutionSucceeded || reloaded.Tasks[0].Attempts[0].DurationMS != 150 || !result["verified"] || len(reloaded.Events) != 2 {
		t.Fatalf("execution lifecycle did not survive reload: %#v, %v", reloaded, err)
	}
	replayed, replayCreated, replayErr := reloadedStore.Create(testExecution("exec-after-restart", "request-one"))
	if replayErr != nil || replayCreated || replayed.ID != execution.ID {
		t.Fatalf("idempotency key did not survive restart: %#v created=%v err=%v", replayed, replayCreated, replayErr)
	}
	info, err := os.Stat(filepath.Join(root, ".executions", execution.ID+".json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("execution persistence must be private: %v, %v", info, err)
	}
}

func TestExecutionStoreSerializesConcurrentIdempotentCreates(t *testing.T) {
	store, err := NewExecutionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	var mu sync.Mutex
	createdCount := 0
	errCount := 0
	for index := 0; index < 16; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			execution := testExecution("exec-race-"+string(rune('a'+index)), "same-request")
			_, created, err := store.Create(execution)
			mu.Lock()
			defer mu.Unlock()
			if created {
				createdCount++
			}
			if err != nil {
				errCount++
			}
		}(index)
	}
	wait.Wait()
	if createdCount != 1 || errCount != 0 {
		t.Fatalf("concurrent idempotent submissions created=%d errors=%d", createdCount, errCount)
	}
}

func TestExecutionStoreRejectsTerminalStatusReversal(t *testing.T) {
	store, err := NewExecutionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	execution, _, err := store.Create(testExecution("exec-terminal", "request-terminal"))
	if err != nil {
		t.Fatal(err)
	}
	execution.Status = domain.ExecutionRunning
	execution.StartedAt = execution.CreatedAt.Add(time.Second)
	execution.UpdatedAt = execution.StartedAt
	execution.Tasks[0].Status = domain.ExecutionTaskRunning
	execution.Tasks[0].StartedAt = execution.StartedAt
	execution.Tasks[0].UpdatedAt = execution.StartedAt
	execution, err = store.Update(execution)
	if err != nil {
		t.Fatal(err)
	}
	finished := execution.UpdatedAt.Add(time.Second)
	execution.Status = domain.ExecutionSucceeded
	execution.FinishedAt = finished
	execution.UpdatedAt = finished
	execution.Tasks[0].Status = domain.ExecutionTaskSucceeded
	execution.Tasks[0].FinishedAt = finished
	execution.Tasks[0].UpdatedAt = finished
	if execution, err = store.Update(execution); err != nil {
		t.Fatal(err)
	}
	mutated := execution
	mutated.Tasks = append([]domain.ExecutionTask(nil), execution.Tasks...)
	mutated.Tasks[0].Result = json.RawMessage(`{"rewritten":true}`)
	mutated.UpdatedAt = finished.Add(time.Second)
	if _, err := store.Update(mutated); err != ports.ErrExecutionConflict {
		t.Fatalf("terminal task result was modified: %v", err)
	}
	execution.Status = domain.ExecutionRunning
	execution.UpdatedAt = finished.Add(time.Second)
	if _, err := store.Update(execution); err != ports.ErrExecutionConflict {
		t.Fatalf("terminal execution was reopened: %v", err)
	}
}

func TestExecutionStoreRejectsStaleRevision(t *testing.T) {
	store, err := NewExecutionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	execution, _, err := store.Create(testExecution("exec-revision", "request-revision"))
	if err != nil {
		t.Fatal(err)
	}
	updated := execution
	updated.Status = domain.ExecutionRunning
	updated.StartedAt = updated.CreatedAt.Add(time.Second)
	updated.UpdatedAt = updated.StartedAt
	updated.Tasks = append([]domain.ExecutionTask(nil), updated.Tasks...)
	updated.Tasks[0].Status = domain.ExecutionTaskRunning
	updated.Tasks[0].StartedAt = updated.StartedAt
	updated.Tasks[0].UpdatedAt = updated.StartedAt
	if _, err := store.Update(updated); err != nil {
		t.Fatal(err)
	}
	execution.Status = domain.ExecutionCancelled
	execution.FinishedAt = execution.CreatedAt.Add(2 * time.Second)
	execution.UpdatedAt = execution.FinishedAt
	if _, err := store.Update(execution); err != ports.ErrExecutionConflict {
		t.Fatalf("stale execution revision was accepted: %v", err)
	}
}

func testJob(id string) domain.Job {
	now := time.Now().UTC()
	return domain.Job{
		ID: id, CreatedAt: now, UpdatedAt: now,
		InputHash: protocol.SHA256([]byte("input")), Status: domain.JobStatusPlanned,
		Plan: domain.Plan{Version: "v1", Status: "ready"},
	}
}

func testExecution(id, request string) domain.Execution {
	now := time.Now().UTC()
	return domain.Execution{
		ID: id, IdempotencyKeyHash: protocol.SHA256([]byte("same-idempotency-key")),
		RequestHash: protocol.SHA256([]byte(request)), ActorID: "user-admin",
		Status: domain.ExecutionQueued, CreatedAt: now, UpdatedAt: now, Revision: 1,
		Tasks: []domain.ExecutionTask{{
			ID: "task-one", Type: "inventory.inspect", Status: domain.ExecutionTaskQueued,
			TimeoutMillis: 30000, MaxAttempts: 1, Parameters: json.RawMessage(`{"value":"safe"}`),
			CreatedAt: now, UpdatedAt: now,
		}},
	}
}
