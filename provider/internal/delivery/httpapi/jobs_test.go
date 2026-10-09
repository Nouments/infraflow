package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	configadapter "infraflow/internal/adapters/config"
	planningadapter "infraflow/internal/adapters/planning"
	"infraflow/internal/infrastructure/security"
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
