package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	configadapter "infraflow/internal/adapters/config"
	planningadapter "infraflow/internal/adapters/planning"
	"infraflow/internal/domain"
	"infraflow/internal/infrastructure/security"
	"infraflow/internal/ports"
	"infraflow/pkg/observability"
	"infraflow/provider/internal/adapters/filesystem"
	"infraflow/provider/internal/adapters/generation"
	"infraflow/provider/internal/adapters/sqlite"
	"infraflow/provider/internal/application"
)

func TestJobAPIManagesAuthenticatedPlanningJobs(t *testing.T) {
	root := t.TempDir()
	jobs, err := filesystem.NewJobStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithJobs(nil, nil, nil, jobs, application.Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}})
	agentToken := strings.Repeat("j", security.MinAgentTokenBytes)
	handler, userToken := newUserSessionHandler(t, root, agentToken, service)

	body, err := json.Marshal(createJobRequest{Input: "sites:\n  - name: lab\n"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+userToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
	}
	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || created.ID == "" || created.Status != "planned" {
		t.Fatalf("unexpected create response: %#v, %v", created, err)
	}

	for _, test := range []struct {
		method string
		path   string
		code   int
	}{
		{http.MethodGet, "/api/v1/jobs", http.StatusOK},
		{http.MethodGet, "/api/v1/jobs/" + created.ID, http.StatusOK},
		{http.MethodPost, "/api/v1/jobs/" + created.ID + "/cancel", http.StatusOK},
		{http.MethodPost, "/api/v1/jobs/" + created.ID + "/retry", http.StatusOK},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		request.Header.Set("Authorization", "Bearer "+userToken)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.code {
			t.Errorf("%s %s returned %d, want %d: %s", test.method, test.path, response.Code, test.code, response.Body.String())
		}
	}
}

func TestPlanPreviewValidatesWithoutPersistingJobs(t *testing.T) {
	root := t.TempDir()
	jobs, err := filesystem.NewJobStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithJobs(nil, nil, nil, jobs, application.Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}})
	agentToken := strings.Repeat("p", security.MinAgentTokenBytes)
	handler, userToken := newUserSessionHandler(t, root, agentToken, service)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/plan/preview", strings.NewReader(`{"input":"sites:\n  - name: lab\n    devices:\n      - name: R1\n        vendor: cisco\n        model: ios-xe\n"}`))
	request.Header.Set("Authorization", "Bearer "+userToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("preview returned %d: %s", response.Code, response.Body.String())
	}
	var preview previewPlanResponse
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Infrastructure.Sites) != 1 || preview.Infrastructure.Sites[0].Name != "lab" {
		t.Fatalf("preview did not return normalized configuration: %#v", preview.Infrastructure)
	}
	if preview.Plan.Status != "PLANNED" || len(preview.Plan.Tasks) != 0 {
		t.Fatalf("preview did not report a deterministic planned execution plan without invented tasks: %#v", preview.Plan)
	}
	storedJobs, err := jobs.List()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(storedJobs); got != 0 {
		t.Fatalf("preview persisted %d planning jobs", got)
	}
}

func TestPlanGenerateEndpointReturnsAndAuditsRealArtifacts(t *testing.T) {
	root := t.TempDir()
	events, err := filesystem.NewEventStore(root)
	if err != nil {
		t.Fatal(err)
	}
	dependencies := application.Dependencies{
		Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}, ArtifactDirectory: root,
	}
	service := application.NewServiceWithJobsAgentsEvents(
		filesystem.NewArtifactRepository(root), nil, generation.Generator{}, nil, nil, events, dependencies,
	)
	agentToken := strings.Repeat("g", security.MinAgentTokenBytes)
	handler, userToken := newUserSessionHandler(t, root, agentToken, service)
	input := `sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        network:
          interfaces:
            - name: Gi1
              role: lan
              ipv4_mode: static
              ipv4_address: 192.0.2.1/24
`
	body, err := json.Marshal(createJobRequest{Input: input})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/plan/generate", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+userToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("generation returned %d: %s", response.Code, response.Body.String())
	}
	var result application.PlanGenerationResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.GenerationStatus != "GENERATED" || result.ExecutionStatus != "NOT_REQUESTED" || result.VerificationStatus != "NOT_PERFORMED" {
		t.Fatalf("unexpected generation result states: %#v", result)
	}
	if len(result.Tasks) != 1 || len(result.Tasks[0].Artifacts) == 0 || result.Tasks[0].Artifacts[0].Device != "R1" || result.AuditStatus != "RECORDED" {
		t.Fatalf("response did not include device-level artifact provenance: %#v", result.Tasks)
	}
	storedEvents := service.Events()
	if len(storedEvents) != 1 || storedEvents[0].Type != "plan.generation.result" {
		t.Fatalf("generation result was not recorded in the event store: %#v", storedEvents)
	}
	if !strings.Contains(string(storedEvents[0].Payload), result.Tasks[0].Artifacts[0].SHA256) {
		t.Fatalf("audit event omitted artifact hash provenance: %s", storedEvents[0].Payload)
	}
}

func TestTechnicalLogAPIRequiresAdminAndFiltersByRun(t *testing.T) {
	root := t.TempDir()
	logs, err := observability.NewStore(root, observability.DefaultCentralLogLimit)
	if err != nil {
		t.Fatal(err)
	}
	agents, err := filesystem.NewAgentStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithJobsAndAgents(nil, nil, nil, nil, agents, application.Dependencies{Logs: logs})
	if _, err := service.RegisterAgent(application.AgentRegistration{ID: "agent-01", SiteID: "site-01"}); err != nil {
		t.Fatal(err)
	}
	_, err = service.IngestAgentLogs("agent-01", []observability.Event{{
		ID: "event-01", Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Level: "ERROR",
		Event: "process.exit", Message: "token=secret-value", Service: "agent", Source: "process-runner",
		AgentID: "agent-01", SiteID: "site-01", RunID: "run-01", TaskID: "task-01",
	}})
	if err != nil {
		t.Fatal(err)
	}
	agentToken := strings.Repeat("l", security.MinAgentTokenBytes)
	handler, userToken := newUserSessionHandler(t, root, agentToken, service)

	for _, test := range []struct {
		path string
		want int
	}{
		{path: "/api/v1/logs?run_id=run-01&level=ERROR", want: http.StatusOK},
		{path: "/api/v1/logs/runs/run-01", want: http.StatusOK},
		{path: "/api/v1/logs?limit=0", want: http.StatusBadRequest},
		{path: "/api/v1/logs?since=not-a-timestamp", want: http.StatusBadRequest},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("Authorization", "Bearer "+userToken)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Errorf("GET %s returned %d, want %d: %s", test.path, response.Code, test.want, response.Body.String())
		}
		if test.want == http.StatusOK && (!strings.Contains(response.Body.String(), "event-01") || !strings.Contains(response.Body.String(), "[REDACTED]") || strings.Contains(response.Body.String(), "secret-value")) {
			t.Errorf("log response missing the redacted event contract for %s: %s", test.path, response.Body.String())
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/logs?run_id=run-01", nil)
	request.Header.Set("Authorization", "Bearer "+agentToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin user read technical logs: %d %s", response.Code, response.Body.String())
	}

	created := authRequest(t, handler, http.MethodPost, "/api/v1/users", `{"username":"reader","password":"reader password 123","role":"user"}`, userToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("create regular user returned %d: %s", created.Code, created.Body.String())
	}
	login := authRequest(t, handler, http.MethodPost, "/api/v1/auth/login", `{"username":"reader","password":"reader password 123"}`, "")
	var userSession struct {
		Token string `json:"token"`
	}
	if login.Code != http.StatusOK || json.Unmarshal(login.Body.Bytes(), &userSession) != nil || userSession.Token == "" {
		t.Fatalf("regular user login failed: %d %s", login.Code, login.Body.String())
	}
	for _, path := range []string{"/api/v1/logs", "/api/v1/logs/stream", "/api/v1/logs/runs/run-01"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+userSession.Token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Errorf("regular user accessed %s: status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestTechnicalLogStreamProducesSSEFramesAndStopsOnContextCancel(t *testing.T) {
	root := t.TempDir()
	logs, err := observability.NewStore(root, observability.DefaultCentralLogLimit)
	if err != nil {
		t.Fatal(err)
	}
	agents, err := filesystem.NewAgentStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithJobsAndAgents(nil, nil, nil, nil, agents, application.Dependencies{Logs: logs})
	if _, err := service.RegisterAgent(application.AgentRegistration{ID: "agent-02", SiteID: "site-02"}); err != nil {
		t.Fatal(err)
	}
	_, err = service.IngestAgentLogs("agent-02", []observability.Event{{
		ID: "event-02", Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Level: "WARN",
		Event: "process.exit", Message: "token=secret-stream", Service: "agent", Source: "process-runner",
		AgentID: "agent-02", SiteID: "site-02", RunID: "run-02", TaskID: "task-02",
	}})
	if err != nil {
		t.Fatal(err)
	}
	agentToken := strings.Repeat("s", security.MinAgentTokenBytes)
	handler, userToken := newUserSessionHandler(t, root, agentToken, service)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/logs/stream?limit=10", nil)
	request.Header.Set("Authorization", "Bearer "+userToken)
	ctx, cancel := context.WithCancel(request.Context())
	request = request.WithContext(ctx)
	response := httptest.NewRecorder()

	go func() {
		time.Sleep(25 * time.Millisecond)
		cancel()
	}()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("stream returned %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "event: log") {
		t.Fatalf("stream did not emit SSE log events: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "id: event-02") || !strings.Contains(response.Body.String(), `"event_id":"event-02"`) {
		t.Fatalf("SSE frame does not match its id and JSON event_id contract: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret-stream") || !strings.Contains(response.Body.String(), "[REDACTED]") {
		t.Fatalf("SSE exposed an unredacted secret: %s", response.Body.String())
	}

	resumed := httptest.NewRequest(http.MethodGet, "/api/v1/logs/stream?limit=10", nil)
	resumed.Header.Set("Authorization", "Bearer "+userToken)
	resumed.Header.Set("Last-Event-ID", "event-02")
	resumeContext, cancelResume := context.WithCancel(resumed.Context())
	resumed = resumed.WithContext(resumeContext)
	resumeResponse := httptest.NewRecorder()
	go func() {
		time.Sleep(25 * time.Millisecond)
		cancelResume()
	}()
	handler.ServeHTTP(resumeResponse, resumed)
	if resumeResponse.Code != http.StatusOK || strings.Contains(resumeResponse.Body.String(), "event: log") {
		t.Fatalf("Last-Event-ID did not resume after the existing event: status=%d body=%s", resumeResponse.Code, resumeResponse.Body.String())
	}
}

func TestTechnicalLogAPIReportsUnavailableStoreRatherThanEmptyResult(t *testing.T) {
	root := t.TempDir()
	service := application.NewServiceWithJobsAndAgents(nil, nil, nil, nil, nil, application.Dependencies{})
	agentToken := strings.Repeat("u", security.MinAgentTokenBytes)
	handler, userToken := newUserSessionHandler(t, root, agentToken, service)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/logs", nil)
	request.Header.Set("Authorization", "Bearer "+userToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), `"events":[]`) {
		t.Fatalf("unavailable log store was represented as an empty successful page: status=%d body=%s", response.Code, response.Body.String())
	}

	streamRequest := httptest.NewRequest(http.MethodGet, "/api/v1/logs/stream", nil)
	streamRequest.Header.Set("Authorization", "Bearer "+userToken)
	streamResponse := httptest.NewRecorder()
	handler.ServeHTTP(streamResponse, streamRequest)
	if streamResponse.Code != http.StatusOK || !strings.Contains(streamResponse.Body.String(), "event: error") || !strings.Contains(streamResponse.Body.String(), "log_store_unavailable") {
		t.Fatalf("SSE did not signal storage failure: status=%d body=%s", streamResponse.Code, streamResponse.Body.String())
	}
}

func TestAgentTokenIsForbiddenFromUserPlanningRoutes(t *testing.T) {
	root := t.TempDir()
	jobs, err := filesystem.NewJobStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithJobs(nil, nil, nil, jobs, application.Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}})
	token := strings.Repeat("a", security.MinAgentTokenBytes)
	handler, err := NewHandler(service, token)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/jobs", ""},
		{http.MethodPost, "/api/v1/jobs", `{"input":"sites:\n  - name: lab\n"}`},
		{http.MethodPost, "/api/v1/plan/generate", `{"input":"sites:\n  - name: lab\n"}`},
		{http.MethodGet, "/api/v1/jobs/job-missing", ""},
		{http.MethodPost, "/api/v1/jobs/job-missing/cancel", ""},
		{http.MethodPost, "/api/v1/jobs/job-missing/retry", ""},
		{http.MethodPost, "/api/v1/plan/preview", `{"input":"sites:\n  - name: lab\n"}`},
	} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Errorf("agent token %s %s returned %d, want %d: %s", test.method, test.path, response.Code, http.StatusForbidden, response.Body.String())
		}
	}
}

func TestJobAPIRejectsUnauthorizedAndMalformedRequests(t *testing.T) {
	root := t.TempDir()
	jobs, err := filesystem.NewJobStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithJobs(nil, nil, nil, jobs, application.Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}})
	agentToken := strings.Repeat("j", security.MinAgentTokenBytes)
	handler, userToken := newUserSessionHandler(t, root, agentToken, service)

	unauthorized := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, unauthorized)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing token returned %d", response.Code)
	}
	duplicateAuth := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	duplicateAuth.Header.Add("Authorization", "Bearer "+userToken)
	duplicateAuth.Header.Add("Authorization", "Bearer "+userToken)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, duplicateAuth)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("duplicate token headers returned %d", response.Code)
	}

	malformed := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(`{"input":"sites: []"} {}`))
	malformed.Header.Set("Authorization", "Bearer "+userToken)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, malformed)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON returned %d: %s", response.Code, response.Body.String())
	}

	invalidInput := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(`{"input":"sites:\n  - unknown: true\n"}`))
	invalidInput.Header.Set("Authorization", "Bearer "+userToken)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, invalidInput)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid input returned %d: %s", response.Code, response.Body.String())
	}

	missing := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/job-missing", nil)
	missing.Header.Set("Authorization", "Bearer "+userToken)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, missing)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing job returned %d: %s", response.Code, response.Body.String())
	}
}

type httpExecutionExecutor struct{}

func (httpExecutionExecutor) Type() string { return "test.operation" }

func (httpExecutionExecutor) ValidateParameters(parameters json.RawMessage) (json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(parameters, &object); err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), parameters...), nil
}

func (httpExecutionExecutor) Execute(context.Context, domain.ExecutionTask) (ports.ExecutionResult, error) {
	return ports.ExecutionResult{Status: domain.ExecutionTaskSucceeded, Output: json.RawMessage(`{"test_result":"ok"}`)}, nil
}

func (httpExecutionExecutor) Retryable(error) bool { return false }

func TestExecutionAPIRequiresAdminAndExposesLifecycle(t *testing.T) {
	root := t.TempDir()
	jobs, err := filesystem.NewJobStore(root)
	if err != nil {
		t.Fatal(err)
	}
	executions, err := filesystem.NewExecutionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	events, err := filesystem.NewEventStore(root)
	if err != nil {
		t.Fatal(err)
	}
	executionService, err := application.NewExecutionService(executions, events, []ports.ExecutionExecutor{httpExecutionExecutor{}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := executionService.Close(ctx); err != nil {
			t.Errorf("close execution service: %v", err)
		}
	})
	service := application.NewServiceWithJobs(nil, nil, nil, jobs, application.Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}})
	agentToken := strings.Repeat("e", security.MinAgentTokenBytes)
	handler, adminToken := newUserSessionHandler(t, root, agentToken, service)
	providerHandler := handler.(*Handler)
	providerHandler.SetExecutionService(executionService)
	operator, err := providerHandler.authenticator.CreateUser(t.Context(), "operator", "operator password 123", "user")
	if err != nil {
		t.Fatal(err)
	}
	operatorSession, err := providerHandler.authenticator.Login(t.Context(), operator.Username, "operator password 123")
	if err != nil {
		t.Fatal(err)
	}

	body := `{"tasks":[{"id":"task-one","type":"test.operation","parameters":{"operation":"inspect"}}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/executions", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+adminToken)
	request.Header.Set("Idempotency-Key", "execution-http-key-0001")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("submit returned %d: %s", response.Code, response.Body.String())
	}
	var submitted domain.Execution
	if err := json.Unmarshal(response.Body.Bytes(), &submitted); err != nil || submitted.ID == "" {
		t.Fatalf("invalid execution submission: %#v, %v", submitted, err)
	}
	completed, err := executionService.Wait(t.Context(), submitted.ID)
	if err != nil || completed.Status != domain.ExecutionSucceeded || len(completed.Tasks[0].Attempts) != 1 {
		t.Fatalf("execution did not complete: %#v, %v", completed, err)
	}

	replay := httptest.NewRequest(http.MethodPost, "/api/v1/executions", strings.NewReader(body))
	replay.Header.Set("Authorization", "Bearer "+adminToken)
	replay.Header.Set("Idempotency-Key", "execution-http-key-0001")
	replayResponse := httptest.NewRecorder()
	handler.ServeHTTP(replayResponse, replay)
	var replayed domain.Execution
	if replayResponse.Code != http.StatusOK || json.Unmarshal(replayResponse.Body.Bytes(), &replayed) != nil || replayed.ID != submitted.ID {
		t.Fatalf("idempotent replay changed execution: %d %s", replayResponse.Code, replayResponse.Body.String())
	}
	conflicting := httptest.NewRequest(http.MethodPost, "/api/v1/executions", strings.NewReader(`{"tasks":[{"id":"task-one","type":"test.operation","parameters":{"operation":"different"}}]}`))
	conflicting.Header.Set("Authorization", "Bearer "+adminToken)
	conflicting.Header.Set("Idempotency-Key", "execution-http-key-0001")
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflicting)
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("idempotency key was reused for a different request: %d %s", conflictResponse.Code, conflictResponse.Body.String())
	}

	for _, test := range []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/api/v1/executions?status=succeeded&type=test.operation&limit=1&offset=0", http.StatusOK},
		{http.MethodGet, "/api/v1/executions/" + submitted.ID, http.StatusOK},
		{http.MethodPost, "/api/v1/executions/" + submitted.ID + "/cancel", http.StatusConflict},
		{http.MethodGet, "/api/v1/executions/exec-missing", http.StatusNotFound},
		{http.MethodGet, "/api/v1/executions?limit=101", http.StatusBadRequest},
	} {
		result := authRequest(t, handler, test.method, test.path, "", adminToken)
		if result.Code != test.want {
			t.Errorf("%s %s returned %d, want %d: %s", test.method, test.path, result.Code, test.want, result.Body.String())
		}
	}

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/executions", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list returned %d", unauthenticated.Code)
	}
	for _, token := range []string{agentToken, operatorSession.Token} {
		for _, route := range []struct{ method, path, body string }{
			{http.MethodPost, "/api/v1/executions", body},
			{http.MethodGet, "/api/v1/executions", ""},
			{http.MethodGet, "/api/v1/executions/" + submitted.ID, ""},
			{http.MethodPost, "/api/v1/executions/" + submitted.ID + "/cancel", ""},
		} {
			result := authRequest(t, handler, route.method, route.path, route.body, token)
			if result.Code != http.StatusForbidden {
				t.Errorf("non-admin %s %s returned %d, want forbidden", route.method, route.path, result.Code)
			}
		}
	}
}

func TestExecutionAPIRejectsUnsupportedTypesAndMissingIdempotency(t *testing.T) {
	root := t.TempDir()
	jobs, err := filesystem.NewJobStore(root)
	if err != nil {
		t.Fatal(err)
	}
	executions, err := filesystem.NewExecutionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	events, err := filesystem.NewEventStore(root)
	if err != nil {
		t.Fatal(err)
	}
	executionService, err := application.NewExecutionService(executions, events, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = executionService.Close(context.Background()) })
	service := application.NewServiceWithJobs(nil, nil, nil, jobs, application.Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}})
	handler, adminToken := newUserSessionHandler(t, root, strings.Repeat("u", security.MinAgentTokenBytes), service)
	handler.(*Handler).SetExecutionService(executionService)

	for _, test := range []struct {
		key  string
		want int
	}{
		{"", http.StatusBadRequest},
		{"execution-http-key-unsupported", http.StatusConflict},
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/executions", strings.NewReader(`{"tasks":[{"id":"task-one","type":"network.ssh","parameters":{}}]}`))
		request.Header.Set("Authorization", "Bearer "+adminToken)
		if test.key != "" {
			request.Header.Set("Idempotency-Key", test.key)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Errorf("unsupported submission key=%q returned %d, want %d: %s", test.key, response.Code, test.want, response.Body.String())
		}
	}
	stored, err := executions.List()
	if err != nil || len(stored) != 0 {
		t.Fatalf("unsupported operation was persisted: %#v, %v", stored, err)
	}
}

func TestUserAuthenticationAndRoles(t *testing.T) {
	root := t.TempDir()
	jobs, err := filesystem.NewJobStore(root)
	if err != nil {
		t.Fatal(err)
	}
	users, err := sqlite.New(root + "/users.sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	defer users.Close()
	authenticator, err := application.NewAuthenticator(users, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := authenticator.EnsureBootstrap(t.Context(), "admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithJobs(nil, nil, nil, jobs, application.Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}})
	agentToken := strings.Repeat("a", security.MinAgentTokenBytes)
	handler, err := NewHandler(service, agentToken, authenticator)
	if err != nil {
		t.Fatal(err)
	}

	login := authRequest(t, handler, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"correct horse battery staple"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("administrator login returned %d: %s", login.Code, login.Body.String())
	}
	var adminSession struct {
		Token string `json:"token"`
		User  struct {
			Role string `json:"role"`
		} `json:"user"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &adminSession); err != nil || adminSession.Token == "" || adminSession.User.Role != "admin" {
		t.Fatalf("invalid administrator session: %#v, %v", adminSession, err)
	}
	created := authRequest(t, handler, http.MethodPost, "/api/v1/users", `{"username":"operator","password":"operator password 123","role":"user"}`, adminSession.Token)
	if created.Code != http.StatusCreated {
		t.Fatalf("operator creation returned %d: %s", created.Code, created.Body.String())
	}
	var operator struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &operator); err != nil || operator.ID == "" {
		t.Fatalf("invalid operator response: %#v, %v", operator, err)
	}
	operatorLogin := authRequest(t, handler, http.MethodPost, "/api/v1/auth/login", `{"username":"operator","password":"operator password 123"}`, "")
	if operatorLogin.Code != http.StatusOK {
		t.Fatalf("operator login returned %d: %s", operatorLogin.Code, operatorLogin.Body.String())
	}
	var operatorSession struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(operatorLogin.Body.Bytes(), &operatorSession); err != nil || operatorSession.Token == "" {
		t.Fatalf("invalid operator session: %#v, %v", operatorSession, err)
	}
	job := authRequest(t, handler, http.MethodPost, "/api/v1/jobs", `{"input":"sites:\n  - name: lab\n"}`, operatorSession.Token)
	if job.Code != http.StatusCreated {
		t.Fatalf("operator job creation returned %d: %s", job.Code, job.Body.String())
	}
	usersResponse := authRequest(t, handler, http.MethodGet, "/api/v1/users", "", operatorSession.Token)
	if usersResponse.Code != http.StatusForbidden {
		t.Fatalf("operator user listing returned %d: %s", usersResponse.Code, usersResponse.Body.String())
	}
	lastAdmin := authRequest(t, handler, http.MethodPatch, "/api/v1/users/"+operator.ID, `{"role":"admin","disabled":true}`, adminSession.Token)
	if lastAdmin.Code != http.StatusOK {
		t.Fatalf("operator update returned %d: %s", lastAdmin.Code, lastAdmin.Body.String())
	}
	adminList := authRequest(t, handler, http.MethodGet, "/api/v1/users", "", adminSession.Token)
	if adminList.Code != http.StatusOK {
		t.Fatalf("administrator user listing returned %d: %s", adminList.Code, adminList.Body.String())
	}
	selfDisable := authRequest(t, handler, http.MethodPatch, "/api/v1/users/"+extractUserID(t, adminSession.Token, handler), `{"disabled":true}`, adminSession.Token)
	if selfDisable.Code != http.StatusConflict {
		t.Fatalf("last administrator disable returned %d: %s", selfDisable.Code, selfDisable.Body.String())
	}
}

func newUserSessionHandler(t *testing.T, root, agentToken string, service *application.Service) (http.Handler, string) {
	t.Helper()
	users, err := sqlite.New(root + "/users.sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	authenticator, err := application.NewAuthenticator(users, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := authenticator.EnsureBootstrap(t.Context(), "admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	session, err := authenticator.Login(t.Context(), "admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(service, agentToken, authenticator)
	if err != nil {
		t.Fatal(err)
	}
	return handler, session.Token
}

func authRequest(t *testing.T, handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func extractUserID(t *testing.T, token string, handler http.Handler) string {
	t.Helper()
	response := authRequest(t, handler, http.MethodGet, "/api/v1/auth/me", "", token)
	if response.Code != http.StatusOK {
		t.Fatalf("current user lookup returned %d: %s", response.Code, response.Body.String())
	}
	var user struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &user); err != nil || user.ID == "" {
		t.Fatalf("invalid current user response: %#v, %v", user, err)
	}
	return user.ID
}
