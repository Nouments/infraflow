package executors

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreconfig "infraflow/internal/adapters/config"
	coregeneration "infraflow/internal/adapters/generation"
	"infraflow/internal/domain"
	"infraflow/internal/ports"
	"infraflow/pkg/protocol"
	"infraflow/provider/internal/adapters/filesystem"
	"infraflow/provider/internal/application"
)

type mockArtifactRepo struct {
	catalog    []protocol.Artifact
	files      map[string][]byte
	openErr    error
	openErrors []error
	openCalls  int
}

func (m *mockArtifactRepo) Catalog() ([]protocol.Artifact, error) {
	return m.catalog, nil
}

func (m *mockArtifactRepo) Open(path string) (io.ReadCloser, protocol.Artifact, error) {
	if m.openCalls < len(m.openErrors) {
		err := m.openErrors[m.openCalls]
		m.openCalls++
		if err != nil {
			return nil, protocol.Artifact{}, err
		}
	}
	if m.openErr != nil {
		return nil, protocol.Artifact{}, m.openErr
	}
	for _, art := range m.catalog {
		if art.Path == path {
			content, exists := m.files[path]
			if !exists {
				return nil, protocol.Artifact{}, os.ErrNotExist
			}
			return io.NopCloser(bytes.NewReader(content)), art, nil
		}
	}
	return nil, protocol.Artifact{}, os.ErrNotExist
}

func TestArtifactVerifyValidateParameters(t *testing.T) {
	executor, err := NewArtifactVerifyExecutor(&mockArtifactRepo{})
	if err != nil {
		t.Fatal(err)
	}

	validJSON := `{"path":"lab/inventory.json"}`
	validated, err := executor.ValidateParameters(json.RawMessage(validJSON))
	if err != nil {
		t.Fatalf("expected valid parameters to succeed, got %v", err)
	}
	if string(validated) != validJSON {
		t.Fatalf("unexpected canonical JSON: %s", string(validated))
	}

	for _, invalid := range []struct {
		name    string
		payload string
	}{
		{"empty payload", ""},
		{"empty object", "{}"},
		{"empty path", `{"path":""}`},
		{"blank path", `{"path":"   "}`},
		{"unknown field", `{"path":"lab/inventory.json","extra":"field"}`},
		{"trailing data", `{"path":"lab/inventory.json"} trailing`},
		{"array payload", `[{"path":"lab/inventory.json"}]`},
		{"string payload", `"lab/inventory.json"`},
		{"numeric payload", `12345`},
		{"absolute path", `{"path":"/lab/inventory.json"}`},
		{"traversal prefix", `{"path":"../lab/inventory.json"}`},
		{"traversal inline", `{"path":"lab/../../etc/passwd"}`},
		{"unnormalized dot", `{"path":"lab/./inventory.json"}`},
		{"single segment", `{"path":"lab"}`},
		{"backslashes", `{"path":"lab\\inventory.json"}`},
		{"null byte", "{\"path\":\"lab/\x00inventory.json\"}"},
		{"newline in path", "{\"path\":\"lab/\ninventory.json\"}"},
		{"invalid site character", `{"path":"-bad/inventory.json"}`},
		{"invalid segment character", `{"path":"lab/inventory;rm.json"}`},
		{"too long path", `{"path":"lab/` + strings.Repeat("a", 300) + `"}`},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			_, err := executor.ValidateParameters(json.RawMessage(invalid.payload))
			if err == nil {
				t.Fatalf("expected validation failure for %s (%s)", invalid.name, invalid.payload)
			}
		})
	}
}

func TestArtifactVerifyExecuteSuccess(t *testing.T) {
	content := []byte(`{"site":"lab","devices":[{"name":"R1"}]}`)
	hasher := sha256.Sum256(content)
	contentHash := hex.EncodeToString(hasher[:])
	inputHash := hex.EncodeToString(sha256.New().Sum(nil))

	artifact := protocol.Artifact{
		Type:       "inventory",
		Path:       "lab/inventory.json",
		InputHash:  inputHash,
		OutputHash: contentHash,
	}

	repo := &mockArtifactRepo{
		catalog: []protocol.Artifact{artifact},
		files: map[string][]byte{
			"lab/inventory.json": content,
		},
	}

	executor, err := NewArtifactVerifyExecutor(repo)
	if err != nil {
		t.Fatal(err)
	}

	task := domain.ExecutionTask{
		ID:         "task-verify-1",
		Type:       ArtifactVerifyType,
		Parameters: json.RawMessage(`{"path":"lab/inventory.json"}`),
	}

	res, err := executor.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("expected execute success, got %v", err)
	}
	if res.Status != domain.ExecutionTaskSucceeded {
		t.Fatalf("expected Succeeded status, got %s", res.Status)
	}
	if res.Error != "" {
		t.Fatalf("expected empty error on success, got %q", res.Error)
	}

	var verified ArtifactVerificationResult
	if err := json.Unmarshal(res.Output, &verified); err != nil {
		t.Fatalf("failed to decode output: %v", err)
	}
	if verified.Path != "lab/inventory.json" || verified.Type != "inventory" {
		t.Fatalf("unexpected verified metadata: %#v", verified)
	}
	if verified.ActualHash != contentHash || verified.OutputHash != contentHash {
		t.Fatalf("hash mismatch in verified result: actual=%s output=%s", verified.ActualHash, verified.OutputHash)
	}
	if verified.SizeBytes != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), verified.SizeBytes)
	}
	if !verified.Verified {
		t.Fatal("verified flag must be true")
	}
	if verified.VerifiedAt == "" {
		t.Fatal("verified_at must not be empty")
	}
}

func TestArtifactVerifyExecuteNotFound(t *testing.T) {
	repo := &mockArtifactRepo{
		catalog: []protocol.Artifact{},
		files:   map[string][]byte{},
	}
	executor, _ := NewArtifactVerifyExecutor(repo)

	task := domain.ExecutionTask{
		ID:         "task-verify-missing",
		Type:       ArtifactVerifyType,
		Parameters: json.RawMessage(`{"path":"lab/inventory.json"}`),
	}

	res, err := executor.Execute(context.Background(), task)
	if err == nil || res.Status != domain.ExecutionTaskFailed {
		t.Fatalf("expected failure for missing artifact, got status=%s err=%v", res.Status, err)
	}
	if !strings.Contains(res.Error, "not found") && !strings.Contains(res.Error, "inaccessible") {
		t.Fatalf("expected not found message, got %q", res.Error)
	}
}

func TestArtifactVerifyExecuteRejectsOversizedArtifact(t *testing.T) {
	repository := &mockArtifactRepo{
		catalog: []protocol.Artifact{{
			Type: "inventory", Path: "lab/inventory.json",
			InputHash: protocol.SHA256([]byte("input")), OutputHash: protocol.SHA256([]byte("output")),
		}},
		openErrors: []error{coregeneration.ErrArtifactTooLarge},
	}
	executor, err := NewArtifactVerifyExecutor(repository)
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(context.Background(), domain.ExecutionTask{
		Parameters: json.RawMessage(`{"path":"lab/inventory.json"}`),
	})
	if err == nil || result.Status != domain.ExecutionTaskFailed || !strings.Contains(result.Error, "exceeds maximum verification size") {
		t.Fatalf("oversized artifact was not reported as a verification failure: result=%#v err=%v", result, err)
	}
}

func TestArtifactVerifyExecuteTamperedHashMismatch(t *testing.T) {
	originalContent := []byte(`{"site":"lab"}`)
	hasher := sha256.Sum256(originalContent)
	expectedHash := hex.EncodeToString(hasher[:])

	tamperedContent := []byte(`{"site":"lab","tampered":true}`)

	artifact := protocol.Artifact{
		Type:       "inventory",
		Path:       "lab/inventory.json",
		InputHash:  expectedHash,
		OutputHash: expectedHash,
	}

	repo := &mockArtifactRepo{
		catalog: []protocol.Artifact{artifact},
		files: map[string][]byte{
			"lab/inventory.json": tamperedContent,
		},
	}

	executor, _ := NewArtifactVerifyExecutor(repo)
	task := domain.ExecutionTask{
		ID:         "task-verify-tampered",
		Type:       ArtifactVerifyType,
		Parameters: json.RawMessage(`{"path":"lab/inventory.json"}`),
	}

	res, err := executor.Execute(context.Background(), task)
	if err == nil || res.Status != domain.ExecutionTaskFailed {
		t.Fatalf("expected failure for tampered artifact, got status=%s err=%v", res.Status, err)
	}
	if !strings.Contains(res.Error, "integrity check failed") {
		t.Fatalf("expected integrity check failed error, got %q", res.Error)
	}
}

func TestArtifactVerifyExecuteUnauthorizedArtifactSpecification(t *testing.T) {
	content := []byte("some-data")
	hasher := sha256.Sum256(content)
	contentHash := hex.EncodeToString(hasher[:])

	// An unauthorized artifact type or path
	artifact := protocol.Artifact{
		Type:       "unauthorized_type",
		Path:       "lab/unauthorized.sh",
		InputHash:  contentHash,
		OutputHash: contentHash,
	}

	repo := &mockArtifactRepo{
		catalog: []protocol.Artifact{artifact},
		files: map[string][]byte{
			"lab/unauthorized.sh": content,
		},
	}

	executor, _ := NewArtifactVerifyExecutor(repo)
	task := domain.ExecutionTask{
		ID:         "task-verify-unauth",
		Type:       ArtifactVerifyType,
		Parameters: json.RawMessage(`{"path":"lab/unauthorized.sh"}`),
	}

	res, err := executor.Execute(context.Background(), task)
	if err == nil || res.Status != domain.ExecutionTaskFailed {
		t.Fatalf("expected failure for unauthorized artifact specification, got status=%s err=%v", res.Status, err)
	}
	if !strings.Contains(res.Error, "not an authorized artifact specification") {
		t.Fatalf("expected unauthorized error, got %q", res.Error)
	}
}

func TestArtifactVerifyExecuteInvalidCatalogHashMetadata(t *testing.T) {
	content := []byte(`{"site":"lab"}`)
	artifact := protocol.Artifact{
		Type:       "inventory",
		Path:       "lab/inventory.json",
		InputHash:  "not-a-valid-sha256",
		OutputHash: "not-a-valid-sha256",
	}

	repo := &mockArtifactRepo{
		catalog: []protocol.Artifact{artifact},
		files: map[string][]byte{
			"lab/inventory.json": content,
		},
	}

	executor, _ := NewArtifactVerifyExecutor(repo)
	task := domain.ExecutionTask{
		ID:         "task-verify-bad-hash",
		Type:       ArtifactVerifyType,
		Parameters: json.RawMessage(`{"path":"lab/inventory.json"}`),
	}

	res, err := executor.Execute(context.Background(), task)
	if err == nil || res.Status != domain.ExecutionTaskFailed {
		t.Fatalf("expected failure for invalid catalog hash metadata, got status=%s err=%v", res.Status, err)
	}
}

func TestArtifactVerifyExecuteContextCancellation(t *testing.T) {
	repo := &mockArtifactRepo{}
	executor, _ := NewArtifactVerifyExecutor(repo)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	task := domain.ExecutionTask{
		ID:         "task-verify-cancelled",
		Type:       ArtifactVerifyType,
		Parameters: json.RawMessage(`{"path":"lab/inventory.json"}`),
	}

	res, err := executor.Execute(ctx, task)
	if !errors.Is(err, context.Canceled) || res.Status != domain.ExecutionTaskCancelled {
		t.Fatalf("expected context.Canceled with Cancelled status, got status=%s err=%v", res.Status, err)
	}
}

func TestArtifactVerifyRetryable(t *testing.T) {
	executor, _ := NewArtifactVerifyExecutor(&mockArtifactRepo{})
	if !executor.Retryable(context.DeadlineExceeded) || !executor.Retryable(temporaryArtifactTestError{}) {
		t.Fatal("transient artifact I/O errors should remain retryable")
	}
	if executor.Retryable(errors.New("artifact integrity check failed")) || executor.Retryable(errors.New("artifact not found")) {
		t.Fatal("deterministic artifact errors must not be retried")
	}
}

type temporaryArtifactTestError struct{}

func (temporaryArtifactTestError) Error() string   { return "temporary storage failure" }
func (temporaryArtifactTestError) Temporary() bool { return true }

func TestArtifactVerifyServiceRetriesTransientReadFailure(t *testing.T) {
	content := []byte(`{"site":"lab","devices":[{"name":"R1"}]}`)
	artifact := protocol.Artifact{
		Type:       "inventory",
		Path:       "lab/inventory.json",
		InputHash:  protocol.SHA256([]byte("input")),
		OutputHash: protocol.SHA256(content),
	}
	repository := &mockArtifactRepo{
		catalog:    []protocol.Artifact{artifact},
		files:      map[string][]byte{"lab/inventory.json": content},
		openErrors: []error{temporaryArtifactTestError{}, nil},
	}
	executor, err := NewArtifactVerifyExecutor(repository)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	executionStore, err := filesystem.NewExecutionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	eventStore, err := filesystem.NewEventStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service, err := application.NewExecutionService(executionStore, eventStore, []ports.ExecutionExecutor{executor}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close(context.Background())

	submission, _, err := service.Submit(context.Background(), "admin", "artifact-transient-retry", application.ExecutionRequest{
		Tasks: []application.ExecutionTaskRequest{{
			ID: "verify-transient", Type: ArtifactVerifyType,
			Parameters:  json.RawMessage(`{"path":"lab/inventory.json"}`),
			MaxAttempts: 2,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := service.Wait(waitContext, submission.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.ExecutionSucceeded || len(result.Tasks) != 1 || len(result.Tasks[0].Attempts) != 2 || result.Tasks[0].Status != domain.ExecutionTaskSucceeded {
		t.Fatalf("safe artifact verification retry did not complete: %#v", result)
	}
}

func TestArtifactVerifyNewExecutorRejectsNil(t *testing.T) {
	if _, err := NewArtifactVerifyExecutor(nil); err == nil {
		t.Fatal("expected error when constructing executor with nil repository")
	}
}

func TestArtifactVerifyIntegrationWithRealGeneratorAndExecutionService(t *testing.T) {
	root := t.TempDir()
	infraYAML := []byte("sites:\n  - name: lab\n    devices:\n      - name: R1\n        vendor: cisco\n        model: ios-xe\n")
	infra, err := coreconfig.Parse(infraYAML)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Generate real artifacts
	artifactsDir := filepath.Join(root, "artifacts")
	generated, err := coregeneration.Generate(infra, artifactsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(generated) == 0 {
		t.Fatal("expected artifacts to be generated")
	}

	// 2. Setup real repository and executor
	artifactRepo := filesystem.NewArtifactRepository(artifactsDir)
	verifier, err := NewArtifactVerifyExecutor(artifactRepo)
	if err != nil {
		t.Fatal(err)
	}

	// 3. Setup real execution engine
	executionStore, err := filesystem.NewExecutionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	eventStore, err := filesystem.NewEventStore(root)
	if err != nil {
		t.Fatal(err)
	}

	service, err := application.NewExecutionService(executionStore, eventStore, []ports.ExecutionExecutor{verifier}, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close(context.Background())

	// 4. Test Scenario A: Successful verification of real inventory.json
	subA, createdA, err := service.Submit(context.Background(), "user-admin", "idem-verify-0001", application.ExecutionRequest{
		Tasks: []application.ExecutionTaskRequest{
			{
				ID:         "task-verify-inventory",
				Type:       ArtifactVerifyType,
				Parameters: json.RawMessage(`{"path":"lab/inventory.json"}`),
			},
		},
	})
	if err != nil || !createdA {
		t.Fatalf("submit failed: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	execA, err := service.Wait(waitCtx, subA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if execA.Status != domain.ExecutionSucceeded {
		t.Fatalf("expected ExecutionSucceeded, got %s (error: %q)", execA.Status, execA.Error)
	}
	if len(execA.Tasks) != 1 || execA.Tasks[0].Status != domain.ExecutionTaskSucceeded {
		t.Fatalf("expected task succeeded: %#v", execA.Tasks)
	}

	var outputA ArtifactVerificationResult
	if err := json.Unmarshal(execA.Tasks[0].Result, &outputA); err != nil {
		t.Fatalf("failed to decode task result: %v", err)
	}
	if !outputA.Verified || outputA.Path != "lab/inventory.json" || outputA.SizeBytes == 0 {
		t.Fatalf("invalid verification result: %#v", outputA)
	}

	// Read real file on disk to verify actual SHA matches outputA.ActualHash
	realDiskBytes, err := os.ReadFile(filepath.Join(artifactsDir, "lab", "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	if protocol.SHA256(realDiskBytes) != outputA.ActualHash {
		t.Fatalf("output hash %s did not match real disk content hash %s", outputA.ActualHash, protocol.SHA256(realDiskBytes))
	}

	// 5. Test Scenario B: Tampered artifact detection on disk
	topologyPath := filepath.Join(artifactsDir, "lab", "topology.json")
	if err := os.WriteFile(topologyPath, []byte(`{"tampered":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	subB, createdB, err := service.Submit(context.Background(), "user-admin", "idem-verify-0002", application.ExecutionRequest{
		Tasks: []application.ExecutionTaskRequest{
			{
				ID:         "task-verify-tampered-topology",
				Type:       ArtifactVerifyType,
				Parameters: json.RawMessage(`{"path":"lab/topology.json"}`),
			},
		},
	})
	if err != nil || !createdB {
		t.Fatalf("submit B failed: %v", err)
	}

	waitCtxB, cancelB := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelB()
	execB, err := service.Wait(waitCtxB, subB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if execB.Status != domain.ExecutionFailed {
		t.Fatalf("expected ExecutionFailed for tampered file, got %s", execB.Status)
	}
	if execB.Tasks[0].Status != domain.ExecutionTaskFailed {
		t.Fatalf("expected task failed: %s", execB.Tasks[0].Status)
	}
	if !strings.Contains(execB.Tasks[0].Error, "integrity check failed") {
		t.Fatalf("expected integrity check failed in task error, got %q", execB.Tasks[0].Error)
	}

	// 6. Test Scenario C: Absent artifact in catalog
	subC, createdC, err := service.Submit(context.Background(), "user-admin", "idem-verify-0003", application.ExecutionRequest{
		Tasks: []application.ExecutionTaskRequest{
			{
				ID:         "task-verify-absent",
				Type:       ArtifactVerifyType,
				Parameters: json.RawMessage(`{"path":"lab/ansible/site.yml"}`),
			},
		},
	})
	if err != nil || !createdC {
		t.Fatalf("submit C failed: %v", err)
	}

	waitCtxC, cancelC := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelC()
	execC, err := service.Wait(waitCtxC, subC.ID)
	if err != nil {
		t.Fatal(err)
	}
	if execC.Status != domain.ExecutionFailed {
		t.Fatalf("expected ExecutionFailed for absent artifact, got %s", execC.Status)
	}

	// 7. Test Scenario D: Rejection of unregistered task type
	_, _, err = service.Submit(context.Background(), "user-admin", "idem-verify-0004", application.ExecutionRequest{
		Tasks: []application.ExecutionTaskRequest{
			{
				ID:         "task-unregistered",
				Type:       "device.deploy",
				Parameters: json.RawMessage(`{}`),
			},
		},
	})
	if !errors.Is(err, application.ErrExecutionUnsupported) {
		t.Fatalf("expected ErrExecutionUnsupported, got %v", err)
	}
}
