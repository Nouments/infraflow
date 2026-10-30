package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	configadapter "infraflow/internal/adapters/config"
	planningadapter "infraflow/internal/adapters/planning"
	"infraflow/internal/domain"
	"infraflow/internal/ports"
	"infraflow/pkg/protocol"
	"infraflow/provider/internal/adapters/filesystem"
	"infraflow/provider/internal/adapters/generation"
)

type memoryArtifacts struct {
	items []protocol.Artifact
}

func (repository memoryArtifacts) Catalog() ([]protocol.Artifact, error) {
	return repository.items, nil
}

func (repository memoryArtifacts) Open(path string) (io.ReadCloser, protocol.Artifact, error) {
	for _, artifact := range repository.items {
		if artifact.Path == path {
			return io.NopCloser(bytes.NewReader(nil)), artifact, nil
		}
	}
	return nil, protocol.Artifact{}, nil
}

type memoryReports struct {
	items []protocol.AgentReport
}

type memoryJobs struct {
	items []domain.Job
}

type memoryAgents struct {
	items map[string]domain.Agent
}

func (repository *memoryAgents) Register(agent domain.Agent) (bool, error) {
	if repository.items == nil {
		repository.items = make(map[string]domain.Agent)
	}
	if _, exists := repository.items[agent.ID]; exists {
		return false, nil
	}
	repository.items[agent.ID] = agent
	return true, nil
}

func (repository *memoryAgents) Get(id string) (domain.Agent, error) {
	agent, exists := repository.items[id]
	if !exists {
		return domain.Agent{}, os.ErrNotExist
	}
	return agent, nil
}

func (repository *memoryAgents) List() ([]domain.Agent, error) {
	agents := make([]domain.Agent, 0, len(repository.items))
	for _, agent := range repository.items {
		agents = append(agents, agent)
	}
	return agents, nil
}

func (repository *memoryAgents) Update(agent domain.Agent) error {
	if _, exists := repository.items[agent.ID]; !exists {
		return os.ErrNotExist
	}
	repository.items[agent.ID] = agent
	return nil
}

func (repository *memoryJobs) Create(job domain.Job) error {
	repository.items = append(repository.items, job)
	return nil
}

func (repository *memoryJobs) Get(id string) (domain.Job, error) {
	for _, job := range repository.items {
		if job.ID == id {
			return job, nil
		}
	}
	return domain.Job{}, os.ErrNotExist
}

func (repository *memoryJobs) List() ([]domain.Job, error) {
	return append([]domain.Job(nil), repository.items...), nil
}

func (repository *memoryJobs) Update(updated domain.Job) error {
	for index, job := range repository.items {
		if job.ID == updated.ID {
			repository.items[index] = updated
			return nil
		}
	}
	return os.ErrNotExist
}

type fakeGenerator struct{}

func (fakeGenerator) Generate(domain.Infrastructure, string) ([]protocol.Artifact, error) {
	return []protocol.Artifact{{Type: "inventory"}, {Type: "topology"}}, nil
}

type failingNativeGenerator struct{}

func (failingNativeGenerator) Generate(domain.Infrastructure, string) ([]protocol.Artifact, error) {
	return nil, errors.New("test generator failure")
}

func (failingNativeGenerator) GenerateNative(domain.Infrastructure, string) ([]protocol.Artifact, error) {
	return nil, errors.New("test generator failure")
}

func (repository *memoryReports) Append(report protocol.AgentReport) (bool, error) {
	for _, current := range repository.items {
		if current.ReportID == report.ReportID {
			return false, nil
		}
	}
	repository.items = append(repository.items, report)
	return true, nil
}

func (repository *memoryReports) List() []protocol.AgentReport {
	return repository.items
}

func TestServiceValidatesPlansAndGenerates(t *testing.T) {
	service := NewService(memoryArtifacts{}, &memoryReports{}, fakeGenerator{}, testDependencies())
	input := []byte("sites:\n  - name: lab\n    devices:\n      - name: R1\n        vendor: cisco\n        model: ios-xe\n")
	if _, err := service.Validate(input); err != nil {
		t.Fatal(err)
	}
	plan, err := service.Plan(input)
	if err != nil || plan.Status != "PLANNED" {
		t.Fatalf("expected planned execution plan: %#v, %v", plan, err)
	}
	artifacts, err := service.Generate(input, "artifact-output")
	if err != nil || len(artifacts) != 2 {
		t.Fatalf("expected generic inventory/topology artifacts: %#v, %v", artifacts, err)
	}
}

func TestPlanAndGenerateReturnsVerifiedArtifactProvenance(t *testing.T) {
	outputDirectory := t.TempDir()
	dependencies := testDependencies()
	dependencies.ArtifactDirectory = outputDirectory
	service := NewService(nil, nil, generation.Generator{}, dependencies)
	input := []byte(`sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        model: csr1000v
        provisioning:
          method: ssh
        network:
          interfaces:
            - name: GigabitEthernet1
              role: lan
              ipv4_mode: static
              ipv4_address: 192.0.2.1/24
`)

	result, err := service.PlanAndGenerate(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan.Status != "PLANNED" || !result.Plan.Lifecycle.Generated || result.GenerationStatus != "GENERATED" {
		t.Fatalf("unexpected plan/generation state: %#v", result)
	}
	if result.ExecutionStatus != "NOT_REQUESTED" || result.VerificationStatus != "NOT_PERFORMED" {
		t.Fatalf("generation implied execution or verification: %#v", result)
	}
	if len(result.Tasks) != 1 || result.Tasks[0].Status != "GENERATED" || result.Tasks[0].Capability != domain.CapabilityUnverified || result.Tasks[0].Method != "ssh" {
		t.Fatalf("unexpected per-task state: %#v", result.Tasks)
	}
	if len(result.Tasks[0].Artifacts) != 1 || result.Tasks[0].Artifacts[0].Type != "native_device_config" {
		t.Fatalf("expected one native configuration linked to the task: %#v", result.Tasks[0].Artifacts)
	}
	for _, artifact := range result.Tasks[0].Artifacts {
		if artifact.Site != "lab" || artifact.Device != "R1" || artifact.TaskID != result.Plan.Tasks[0].ID || artifact.TemplateVersion != "1" || artifact.Status != "GENERATED" {
			t.Fatalf("artifact provenance is incomplete: %#v", artifact)
		}
		if !protocol.IsSHA256(artifact.SHA256) || artifact.Format == "" {
			t.Fatalf("artifact hash or format is missing: %#v", artifact)
		}
		data, err := os.ReadFile(filepath.Join(outputDirectory, filepath.FromSlash(artifact.Path)))
		if err != nil || protocol.SHA256(data) != artifact.SHA256 {
			t.Fatalf("artifact was not present with the reported hash: %#v, %v", artifact, err)
		}
	}

	secondDirectory := t.TempDir()
	secondDependencies := testDependencies()
	secondDependencies.ArtifactDirectory = secondDirectory
	second, err := NewService(nil, nil, generation.Generator{}, secondDependencies).PlanAndGenerate(input)
	if err != nil {
		t.Fatal(err)
	}
	for index := range result.Tasks[0].Artifacts {
		if result.Tasks[0].Artifacts[index].SHA256 != second.Tasks[0].Artifacts[index].SHA256 {
			t.Fatalf("identical generated content had unstable hash: %#v != %#v", result.Tasks[0].Artifacts[index], second.Tasks[0].Artifacts[index])
		}
	}
}

func TestPlanAndGenerateReportsUnsupportedAndFailuresHonestly(t *testing.T) {
	outputDirectory := t.TempDir()
	dependencies := testDependencies()
	dependencies.ArtifactDirectory = outputDirectory
	inputWithUnsupportedMethod := []byte(`sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        provisioning:
          method: made-up
        network:
          interfaces:
            - name: Gi1
              role: lan
              ipv4_mode: dhcp
`)
	unsupported, err := NewService(nil, nil, generation.Generator{}, dependencies).PlanAndGenerate(inputWithUnsupportedMethod)
	if err != nil {
		t.Fatal(err)
	}
	if unsupported.GenerationStatus != "UNSUPPORTED" || unsupported.Plan.Lifecycle.Generated || len(unsupported.Tasks) != 1 || unsupported.Tasks[0].Status != "UNSUPPORTED" {
		t.Fatalf("unsupported generator path was misreported: %#v", unsupported)
	}

	input := []byte(`sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        network:
          interfaces:
            - name: Gi1
              role: lan
              ipv4_mode: dhcp
`)
	withoutGenerator, err := NewService(nil, nil, fakeGenerator{}, dependencies).PlanAndGenerate(input)
	if err == nil || !strings.Contains(err.Error(), "native configuration generator is not configured") {
		t.Fatalf("missing vendor generator did not return an explicit error: %v", err)
	}
	if withoutGenerator.Plan.Lifecycle.Generated {
		t.Fatalf("missing generator marked plan generated: %#v", withoutGenerator)
	}

	invalid, err := NewService(nil, nil, generation.Generator{}, dependencies).PlanAndGenerate([]byte("sites:\n  - name: lab\n    unexpected: true\n"))
	if err == nil || invalid.Plan.ID != "" {
		t.Fatalf("invalid configuration was accepted: %#v, %v", invalid, err)
	}

	failed, err := NewService(nil, nil, failingNativeGenerator{}, dependencies).PlanAndGenerate(input)
	if err == nil || failed.GenerationStatus != "FAILED" || failed.Plan.Lifecycle.Generated || failed.Tasks[0].Status != "FAILED" {
		t.Fatalf("generator failure was not represented honestly: %#v, %v", failed, err)
	}
}

func TestPlanAndGenerateRejectsIntentOutsidePlannedInterfaceTask(t *testing.T) {
	outputDirectory := t.TempDir()
	dependencies := testDependencies()
	dependencies.ArtifactDirectory = outputDirectory
	service := NewService(nil, nil, generation.Generator{}, dependencies)
	input := []byte(`sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        provisioning:
          method: ssh
        network:
          interfaces:
            - name: Gi1
              role: lan
              ipv4_mode: static
              ipv4_address: 192.0.2.1/24
          routes:
            - destination: 198.51.100.0/24
              next_hop: 192.0.2.254
`)

	result, err := service.PlanAndGenerate(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.GenerationStatus != "UNSUPPORTED" || result.Plan.Lifecycle.Generated || len(result.Tasks) != 1 || result.Tasks[0].Status != "UNSUPPORTED" {
		t.Fatalf("route intent absent from the planned interface task was misreported: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(outputDirectory, "lab", "native", "device-R1.cfg")); !os.IsNotExist(err) {
		t.Fatalf("unsupported route intent produced native artifacts: %v", err)
	}
}

func TestServiceSubmitsOnlyCurrentArtifactResults(t *testing.T) {
	artifact := protocol.Artifact{Type: "inventory", Path: "lab/inventory.json", InputHash: "input", OutputHash: "output"}
	repository := &memoryReports{}
	service := NewService(memoryArtifacts{items: []protocol.Artifact{artifact}}, repository, fakeGenerator{}, testDependencies())
	report := protocol.AgentReport{
		AgentID: "agent-01", ReportedAt: time.Now().UTC(),
		Artifacts: []protocol.ArtifactResult{{Path: artifact.Path, OutputHash: artifact.OutputHash, Status: protocol.StatusCompleted}},
	}
	report.ReportID = protocol.ComputeReportID(report)
	added, err := service.SubmitReport(report)
	if err != nil || !added || len(service.Reports()) != 1 {
		t.Fatalf("valid report was not accepted: added=%v err=%v", added, err)
	}
	stale := report
	stale.Artifacts = append([]protocol.ArtifactResult(nil), report.Artifacts...)
	stale.Artifacts[0].OutputHash = "stale"
	stale.ReportID = protocol.ComputeReportID(stale)
	if _, err := service.SubmitReport(stale); err == nil {
		t.Fatal("stale artifact report should be rejected")
	}
}

func TestServiceCreatesAndManagesPlanningJobsWithoutExecutingTasks(t *testing.T) {
	jobs := &memoryJobs{}
	service := NewServiceWithJobs(nil, nil, nil, jobs, testDependencies())
	input := []byte("sites:\n  - name: lab\n    devices:\n      - name: R1\n")
	job, err := service.CreateJob(input)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobStatusPlanned || len(job.Plan.Tasks) != 0 || len(jobs.items) != 1 {
		t.Fatalf("unexpected created job: %#v", job)
	}
	cancelled, err := service.CancelJob(job.ID)
	if err != nil || cancelled.Status != domain.JobStatusCancelled {
		t.Fatalf("job was not cancelled: %#v, %v", cancelled, err)
	}
	retried, err := service.RetryJob(job.ID)
	if err != nil || retried.Status != domain.JobStatusPlanned {
		t.Fatalf("planned job retry should remain planned: %#v, %v", retried, err)
	}
	if _, err := service.CreateJob([]byte("sites:\n  - name: lab\n    unknown: true\n")); err == nil {
		t.Fatal("invalid input should not create a job")
	}
}

type deterministicExecutionExecutor struct {
	typeName  string
	execute   func(context.Context, domain.ExecutionTask) (ports.ExecutionResult, error)
	retryable func(error) bool
}

func (executor deterministicExecutionExecutor) Type() string { return executor.typeName }

func (deterministicExecutionExecutor) ValidateParameters(parameters json.RawMessage) (json.RawMessage, error) {
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(parameters, &decoded); err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), parameters...), nil
}

func (executor deterministicExecutionExecutor) Execute(ctx context.Context, task domain.ExecutionTask) (ports.ExecutionResult, error) {
	return executor.execute(ctx, task)
}

func (executor deterministicExecutionExecutor) Retryable(err error) bool {
	return executor.retryable != nil && executor.retryable(err)
}

func newPersistentExecutionService(t *testing.T, executors []ports.ExecutionExecutor, concurrency int) (*ExecutionService, *filesystem.ExecutionStore, *filesystem.EventStore, string) {
	t.Helper()
	root := t.TempDir()
	repository, err := filesystem.NewExecutionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	events, err := filesystem.NewEventStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewExecutionService(repository, events, executors, concurrency)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Close(ctx); err != nil {
			t.Errorf("close execution service: %v", err)
		}
	})
	return service, repository, events, root
}

func executionTask(id string, dependencies []string, lock string, parameters string) ExecutionTaskRequest {
	return ExecutionTaskRequest{ID: id, Type: "test.operation", Dependencies: dependencies, ResourceLock: lock, Parameters: json.RawMessage(parameters)}
}

func waitExecution(t *testing.T, service *ExecutionService, id string) domain.Execution {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	execution, err := service.Wait(ctx, id)
	if err != nil {
		t.Fatalf("wait for execution: %v", err)
	}
	return execution
}

func TestExecutionServiceRunsDependenciesLocksAndPersistsSanitizedResult(t *testing.T) {
	startedA := make(chan struct{})
	startedB := make(chan struct{})
	releaseA := make(chan struct{})
	var mu sync.Mutex
	var order []string
	bFinished := false
	executor := deterministicExecutionExecutor{
		typeName: "test.operation",
		execute: func(_ context.Context, task domain.ExecutionTask) (ports.ExecutionResult, error) {
			var input struct {
				Operation string `json:"operation"`
			}
			if err := json.Unmarshal(task.Parameters, &input); err != nil {
				return ports.ExecutionResult{}, err
			}
			mu.Lock()
			order = append(order, input.Operation)
			mu.Unlock()
			switch input.Operation {
			case "a":
				close(startedA)
				<-releaseA
			case "b":
				close(startedB)
			case "c":
				mu.Lock()
				completed := bFinished
				mu.Unlock()
				if !completed {
					return ports.ExecutionResult{}, errors.New("dependency ran out of order")
				}
			}
			if input.Operation == "b" {
				mu.Lock()
				bFinished = true
				mu.Unlock()
			}
			return ports.ExecutionResult{Status: domain.ExecutionTaskSucceeded, Output: json.RawMessage(`{"password":"must-not-persist","message":"token=must-not-persist","state":"done"}`), Duration: 17 * time.Millisecond}, nil
		},
	}
	service, _, events, _ := newPersistentExecutionService(t, []ports.ExecutionExecutor{executor}, 3)
	request := ExecutionRequest{Tasks: []ExecutionTaskRequest{
		executionTask("task-a", nil, "device:r1", `{"operation":"a"}`),
		executionTask("task-b", nil, "device:r1", `{"operation":"b"}`),
		executionTask("task-c", []string{"task-b"}, "", `{"operation":"c"}`),
	}}
	submitted, created, err := service.Submit(context.Background(), "user-admin", "execution-key-0001", request)
	if err != nil || !created {
		t.Fatalf("execution submission failed: created=%v err=%v", created, err)
	}
	select {
	case <-startedA:
	case <-time.After(time.Second):
		t.Fatal("first locked task did not start")
	}
	select {
	case <-startedB:
		t.Fatal("scheduler ran tasks sharing a resource lock concurrently")
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseA)
	select {
	case <-startedB:
	case <-time.After(time.Second):
		t.Fatal("second task did not start after the resource lock was released")
	}
	result := waitExecution(t, service, submitted.ID)
	if result.Status != domain.ExecutionSucceeded || len(result.Tasks) != 3 || result.Tasks[2].Status != domain.ExecutionTaskSucceeded || result.Tasks[0].Attempts[0].DurationMS != 17 {
		t.Fatalf("execution did not complete through the scheduler: %#v", result)
	}
	mu.Lock()
	if !bFinished || len(order) != 3 {
		mu.Unlock()
		t.Fatalf("dependent task did not follow its prerequisite: %v", order)
	}
	mu.Unlock()
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), "must-not-persist") || !strings.Contains(string(encoded), "[REDACTED]") {
		t.Fatalf("executor result secret was not redacted: %s, %v", encoded, err)
	}
	for _, event := range events.List() {
		if strings.Contains(string(event.Payload), "must-not-persist") {
			t.Fatalf("audit event captured executor output: %s", event.Payload)
		}
	}
}

func TestExecutionServiceRetriesFailuresAndBlocksDependents(t *testing.T) {
	var mu sync.Mutex
	attempts := map[string]int{}
	executor := deterministicExecutionExecutor{
		typeName: "test.operation",
		execute: func(_ context.Context, task domain.ExecutionTask) (ports.ExecutionResult, error) {
			mu.Lock()
			defer mu.Unlock()
			attempts[task.ID]++
			switch task.ID {
			case "retry-task":
				if attempts[task.ID] == 1 {
					return ports.ExecutionResult{}, errors.New("temporary token=retry-secret")
				}
				return ports.ExecutionResult{Status: domain.ExecutionTaskSucceeded, Output: json.RawMessage(`{"ok":true}`)}, nil
			case "failed-task":
				return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: "authorization: Bearer failure-secret"}, nil
			default:
				return ports.ExecutionResult{Status: domain.ExecutionTaskSucceeded, Output: json.RawMessage(`{}`)}, nil
			}
		},
		retryable: func(err error) bool { return strings.Contains(err.Error(), "temporary") },
	}
	service, _, _, _ := newPersistentExecutionService(t, []ports.ExecutionExecutor{executor}, 2)
	retryTask := executionTask("retry-task", nil, "", `{"operation":"retry"}`)
	retryTask.MaxAttempts = 2
	failedTask := executionTask("failed-task", nil, "", `{"operation":"fail"}`)
	request := ExecutionRequest{Tasks: []ExecutionTaskRequest{
		retryTask, failedTask,
		executionTask("dependent-task", []string{"failed-task"}, "", `{"operation":"dependent"}`),
	}}
	submitted, _, err := service.Submit(context.Background(), "user-admin", "execution-key-0002", request)
	if err != nil {
		t.Fatal(err)
	}
	result := waitExecution(t, service, submitted.ID)
	if result.Status != domain.ExecutionFailed || result.Error == "" || result.Tasks[0].Status != domain.ExecutionTaskSucceeded || len(result.Tasks[0].Attempts) != 2 || result.Tasks[0].Attempts[0].Status != domain.ExecutionTaskRetrying || result.Tasks[1].Status != domain.ExecutionTaskFailed || result.Tasks[2].Status != domain.ExecutionTaskBlocked {
		t.Fatalf("retry or dependency failure was not persisted: %#v", result)
	}
	encoded, _ := json.Marshal(result)
	for _, secret := range []string{"retry-secret", "failure-secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("execution persisted a secret-like error: %s", encoded)
		}
	}
}

func TestExecutionServiceRejectsCiscoAutomaticRetries(t *testing.T) {
	executor := deterministicExecutionExecutor{
		typeName: "cisco.ios.configure",
		execute: func(context.Context, domain.ExecutionTask) (ports.ExecutionResult, error) {
			return ports.ExecutionResult{Status: domain.ExecutionTaskSucceeded}, nil
		},
	}
	service, _, _, _ := newPersistentExecutionService(t, []ports.ExecutionExecutor{executor}, 1)
	_, _, err := service.Submit(context.Background(), "user-admin", "cisco-no-auto-retry", ExecutionRequest{
		Tasks: []ExecutionTaskRequest{{
			ID: "configure-router", Type: "cisco.ios.configure",
			Parameters:  json.RawMessage(`{"configuration":true}`),
			MaxAttempts: 2,
		}},
	})
	if !errors.Is(err, ErrInvalidExecution) || !strings.Contains(err.Error(), "does not allow automatic retries") {
		t.Fatalf("expected Cisco automatic retry policy rejection, got %v", err)
	}
}

func TestExecutionServiceTimesOutNonCooperativeExecutor(t *testing.T) {
	executor := deterministicExecutionExecutor{
		typeName: "test.operation",
		execute: func(context.Context, domain.ExecutionTask) (ports.ExecutionResult, error) {
			time.Sleep(25 * time.Millisecond)
			return ports.ExecutionResult{Status: domain.ExecutionTaskSucceeded, Output: json.RawMessage(`{"late":true}`)}, nil
		},
	}
	service, _, _, _ := newPersistentExecutionService(t, []ports.ExecutionExecutor{executor}, 1)
	task := executionTask("slow-task", nil, "", `{"operation":"slow"}`)
	task.TimeoutMillis = 5
	submitted, _, err := service.Submit(context.Background(), "user-admin", "execution-key-0003", ExecutionRequest{Tasks: []ExecutionTaskRequest{
		task,
		executionTask("dependent-task", []string{"slow-task"}, "", `{"operation":"after-timeout"}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	result := waitExecution(t, service, submitted.ID)
	if result.Status != domain.ExecutionTimedOut || result.Tasks[0].Status != domain.ExecutionTaskTimedOut || result.Tasks[0].Result != nil || len(result.Tasks[0].Attempts) != 1 || result.Tasks[0].Attempts[0].Result != nil || result.Tasks[1].Status != domain.ExecutionTaskBlocked {
		t.Fatalf("late executor response was recorded as success: %#v", result)
	}
}

func TestExecutionServiceCancellationWaitsForActualStop(t *testing.T) {
	started := make(chan struct{})
	executor := deterministicExecutionExecutor{
		typeName: "test.operation",
		execute: func(ctx context.Context, _ domain.ExecutionTask) (ports.ExecutionResult, error) {
			close(started)
			<-ctx.Done()
			return ports.ExecutionResult{}, ctx.Err()
		},
	}
	service, _, _, _ := newPersistentExecutionService(t, []ports.ExecutionExecutor{executor}, 1)
	submitted, _, err := service.Submit(context.Background(), "user-admin", "execution-key-0004", ExecutionRequest{Tasks: []ExecutionTaskRequest{
		executionTask("running-task", nil, "", `{"operation":"block"}`),
		executionTask("queued-task", nil, "", `{"operation":"queued"}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("execution task did not start")
	}
	requested, err := service.Cancel(submitted.ID, "user-admin")
	if err != nil || !requested.CancelRequested || requested.Status == domain.ExecutionCancelled {
		t.Fatalf("cancel request was reported as an already completed cancellation: %#v, %v", requested, err)
	}
	result := waitExecution(t, service, submitted.ID)
	if result.Status != domain.ExecutionCancelled || result.Tasks[0].Status != domain.ExecutionTaskCancelled || result.Tasks[1].Status != domain.ExecutionTaskCancelled {
		t.Fatalf("running or queued tasks were not cancelled after the stop: %#v", result)
	}
}

func TestExecutionServiceShutdownCancelsAndWaitsForActiveWork(t *testing.T) {
	started := make(chan struct{})
	executor := deterministicExecutionExecutor{
		typeName: "test.operation",
		execute: func(ctx context.Context, _ domain.ExecutionTask) (ports.ExecutionResult, error) {
			close(started)
			<-ctx.Done()
			return ports.ExecutionResult{}, ctx.Err()
		},
	}
	service, repository, _, _ := newPersistentExecutionService(t, []ports.ExecutionExecutor{executor}, 1)
	submitted, _, err := service.Submit(context.Background(), "user-admin", "execution-key-shutdown", ExecutionRequest{Tasks: []ExecutionTaskRequest{
		executionTask("shutdown-task", nil, "", `{"operation":"wait"}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("execution task did not start")
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Close(shutdownContext); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(submitted.ID)
	if err != nil || stored.Status != domain.ExecutionCancelled || stored.Tasks[0].Status != domain.ExecutionTaskCancelled {
		t.Fatalf("service shutdown left active execution state: %#v, %v", stored, err)
	}
}

func TestExecutionServiceRecoversInterruptedWorkAndRejectsUnsupportedSecrets(t *testing.T) {
	service, repository, events, root := newPersistentExecutionService(t, nil, 1)
	unsupported, created, err := service.Submit(context.Background(), "user-admin", "execution-key-0005", ExecutionRequest{Tasks: []ExecutionTaskRequest{
		{ID: "task-one", Type: "network.ssh", Parameters: json.RawMessage(`{}`)},
	}})
	if !errors.Is(err, ErrExecutionUnsupported) || created || unsupported.ID != "" {
		t.Fatalf("unsupported operation was accepted: %#v created=%v err=%v", unsupported, created, err)
	}
	if _, err := NewExecutionService(repository, events, []ports.ExecutionExecutor{deterministicExecutionExecutor{typeName: "test.operation"}}, 1); err != nil {
		t.Fatal(err)
	}
	service.Close(context.Background())

	store, err := filesystem.NewExecutionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	interrupted := domain.Execution{
		ID: "exec-interrupted", IdempotencyKeyHash: protocol.SHA256([]byte("restart-key")), RequestHash: protocol.SHA256([]byte("restart-request")),
		ActorID: "user-admin", Status: domain.ExecutionRunning, CreatedAt: now.Add(-time.Second), UpdatedAt: now,
		StartedAt: now, Revision: 1,
		Tasks: []domain.ExecutionTask{{ID: "task-restart", Type: "test.operation", Status: domain.ExecutionTaskRunning, TimeoutMillis: 30000, MaxAttempts: 1, Parameters: json.RawMessage(`{}`), CreatedAt: now.Add(-time.Second), UpdatedAt: now, StartedAt: now,
			Attempts: []domain.ExecutionAttempt{{Number: 1, Status: domain.ExecutionTaskRunning, StartedAt: now}},
		}},
	}
	if _, _, err := store.Create(interrupted); err != nil {
		t.Fatal(err)
	}
	eventStore, err := filesystem.NewEventStore(root)
	if err != nil {
		t.Fatal(err)
	}
	recoveredService, err := NewExecutionService(store, eventStore, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := recoveredService.Get(interrupted.ID)
	if err != nil || recovered.Status != domain.ExecutionFailed || recovered.Tasks[0].Status != domain.ExecutionTaskFailed || !strings.Contains(recovered.Error, "outcome is unknown") {
		t.Fatalf("interrupted execution was not recovered explicitly: %#v, %v", recovered, err)
	}
	if len(eventStore.List()) != 1 || eventStore.List()[0].Type != "execution.recovered_after_restart" {
		t.Fatalf("restart recovery was not audited: %#v", eventStore.List())
	}
	if err := recoveredService.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestExecutionServiceRejectsSecretParametersAndConcurrentReplays(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	executor := deterministicExecutionExecutor{
		typeName: "test.operation",
		execute: func(context.Context, domain.ExecutionTask) (ports.ExecutionResult, error) {
			select {
			case <-started:
			default:
				close(started)
			}
			<-release
			return ports.ExecutionResult{Status: domain.ExecutionTaskSucceeded, Output: json.RawMessage(`{"ok":true}`)}, nil
		},
	}
	service, repository, _, _ := newPersistentExecutionService(t, []ports.ExecutionExecutor{executor}, 1)
	_, _, err := service.Submit(context.Background(), "user-admin", "execution-key-0006", ExecutionRequest{Tasks: []ExecutionTaskRequest{
		{ID: "secret-task", Type: "test.operation", Parameters: json.RawMessage(`{"access_token":"must-not-store"}`)},
	}})
	if !errors.Is(err, ErrInvalidExecution) {
		t.Fatalf("secret-bearing task parameters were accepted: %v", err)
	}
	unsafeLock := executionTask("lock-task", nil, "token=lock-secret", `{"operation":"lock"}`)
	if _, _, err := service.Submit(context.Background(), "user-admin", "execution-key-unsafe-lock", ExecutionRequest{Tasks: []ExecutionTaskRequest{unsafeLock}}); !errors.Is(err, ErrInvalidExecution) {
		t.Fatalf("secret-like resource lock was accepted: %v", err)
	}

	request := ExecutionRequest{Tasks: []ExecutionTaskRequest{executionTask("replay-task", nil, "", `{"operation":"replay"}`)}}
	var wait sync.WaitGroup
	var mu sync.Mutex
	createdCount := 0
	ids := make(map[string]struct{})
	for index := 0; index < 12; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			execution, created, submitErr := service.Submit(context.Background(), "user-admin", "execution-key-0007", request)
			mu.Lock()
			defer mu.Unlock()
			if submitErr != nil {
				t.Errorf("concurrent replay failed: %v", submitErr)
				return
			}
			if created {
				createdCount++
			}
			ids[execution.ID] = struct{}{}
		}()
	}
	wait.Wait()
	if createdCount != 1 || len(ids) != 1 {
		t.Fatalf("concurrent API replays created=%d execution IDs=%v", createdCount, ids)
	}
	var executionID string
	for id := range ids {
		executionID = id
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("idempotent execution was not dispatched")
	}
	close(release)
	result := waitExecution(t, service, executionID)
	if result.Status != domain.ExecutionSucceeded {
		t.Fatalf("idempotent execution did not finish: %#v", result)
	}
	stored, err := repository.List()
	if err != nil || len(stored) != 1 {
		t.Fatalf("idempotent submission created duplicate persisted executions: %d, %v", len(stored), err)
	}
}

func TestServiceBindsAgentIdentityAndUpdatesHeartbeats(t *testing.T) {
	agents := &memoryAgents{}
	service := NewServiceWithJobsAndAgents(nil, nil, nil, nil, agents, testDependencies())
	agent, err := service.RegisterAgent(AgentRegistration{ID: "agent-01", SiteID: "site-01", Version: "0.1.0", Capabilities: []string{"inventory"}})
	if err != nil || agent.Status != domain.AgentStatusOnline {
		t.Fatalf("agent was not registered: %#v, %v", agent, err)
	}
	updated, err := service.HeartbeatAgent(agent.ID, AgentHeartbeat{SiteID: agent.SiteID, Version: agent.Version, QueueDepth: 2})
	if err != nil || updated.QueueDepth != 2 {
		t.Fatalf("heartbeat was not persisted: %#v, %v", updated, err)
	}
	if _, err := service.HeartbeatAgent(agent.ID, AgentHeartbeat{SiteID: "other-site"}); !errors.Is(err, ErrAgentConflict) {
		t.Fatalf("expected site identity conflict, got %v", err)
	}
	if _, err := service.RegisterAgent(AgentRegistration{ID: agent.ID, SiteID: "other-site"}); !errors.Is(err, ErrAgentConflict) {
		t.Fatalf("expected registration identity conflict, got %v", err)
	}
}

func TestServiceReportsTemplateSelectionAndCapabilityState(t *testing.T) {
	service := NewService(nil, nil, nil, testDependencies())
	input := []byte(`capability_registry:
  entries:
    cisco:iosxe:ios-xe:
      netconf:
        method: netconf
        state: UNVERIFIED
        evidence: no lab execution or verified adapter was recorded

template_registry:
  entries:
    cisco:iosxe:ios-xe:1:
      id: cisco:iosxe:ios-xe:1
      vendor: cisco
      family: iosxe
      model: ios-xe
      version: "1"
      hash: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      evidence: sandbox validation manifest checked locally

sites:
  - name: sandbox
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        model: ios-xe
        provisioning:
          method: netconf
          template_version: "1"
`)
	results, err := service.TemplateInfo(input)
	if err != nil {
		t.Fatalf("template info failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly one template result, got %d: %#v", len(results), results)
	}
	if results[0].TemplateID != "cisco:iosxe:ios-xe:1" || results[0].CapabilityState != domain.CapabilityUnverified {
		t.Fatalf("unexpected template selection result: %#v", results[0])
	}
	if !results[0].Blocked {
		t.Fatal("expected result to be blocked while capability is unverified")
	}
}

func TestServiceSummarizesCapabilityAndTemplateReadiness(t *testing.T) {
	service := NewService(nil, nil, nil, testDependencies())
	input := []byte(`capability_registry:
  entries:
    cisco:iosxe:ios-xe:
      netconf:
        method: netconf
        state: LAB-VERIFIED
        evidence: lab test 2026-10-07

template_registry:
  entries:
    cisco:iosxe:ios-xe:1:
      id: cisco:iosxe:ios-xe:1
      vendor: cisco
      family: iosxe
      model: ios-xe
      version: "1"
      hash: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      evidence: sandbox validation manifest checked locally

sites:
  - name: sandbox
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        model: ios-xe
        provisioning:
          method: netconf
          template_version: "1"
`)
	summary, err := service.CapabilitySummary(input)
	if err != nil {
		t.Fatalf("capability summary failed: %v", err)
	}
	if len(summary) != 1 {
		t.Fatalf("expected one device summary, got %d: %#v", len(summary), summary)
	}
	if summary[0].CapabilityState != domain.CapabilityLabVerified || summary[0].TemplateID != "cisco:iosxe:ios-xe:1" {
		t.Fatalf("unexpected capability summary: %#v", summary[0])
	}
	if summary[0].Ready != true {
		t.Fatal("expected device to be ready when capability is lab-verified and template known")
	}
}

func TestServiceBuildsEvidenceBasedCapabilityMatrix(t *testing.T) {
	service := NewService(nil, nil, nil, testDependencies())
	input := []byte(`capability_registry:
  entries:
    cisco:iosxe:ios-xe:
      netconf:
        method: netconf
        state: LAB-VERIFIED
        evidence: lab test 2026-10-07
      ssh:
        method: ssh
        state: UNVERIFIED
        evidence: no adapter execution evidence

template_registry:
  entries:
    cisco:iosxe:ios-xe:1:
      id: cisco:iosxe:ios-xe:1
      vendor: cisco
      family: iosxe
      model: ios-xe
      version: "1"
      hash: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      evidence: audited sandbox manifest

sites:
  - name: sandbox
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        model: ios-xe
        provisioning:
          method: netconf
          template_version: "1"
`)
	matrix, err := service.CapabilityMatrix(input)
	if err != nil {
		t.Fatalf("capability matrix failed: %v", err)
	}
	if len(matrix) != 2 {
		t.Fatalf("expected two capability matrix rows, got %d: %#v", len(matrix), matrix)
	}
	if matrix[0].Method != "netconf" || matrix[0].State != domain.CapabilityLabVerified || !matrix[0].Ready {
		t.Fatalf("unexpected verified matrix row: %#v", matrix[0])
	}
	if matrix[1].Method != "ssh" || matrix[1].State != domain.CapabilityUnverified || matrix[1].Ready {
		t.Fatalf("unexpected unverified matrix row: %#v", matrix[1])
	}
}

func testDependencies() Dependencies {
	return Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}}
}
