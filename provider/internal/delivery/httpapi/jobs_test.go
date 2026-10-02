package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"infraflow/internal/security"
	"infraflow/provider/internal/adapters/filesystem"
	"infraflow/provider/internal/application"
)

func TestJobAPIManagesAuthenticatedPlanningJobs(t *testing.T) {
	root := t.TempDir()
	jobs, err := filesystem.NewJobStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithJobs(nil, nil, nil, jobs)
	token := strings.Repeat("j", security.MinAgentTokenBytes)
	handler, err := NewHandler(service, token)
	if err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(createJobRequest{Input: "sites:\n  - name: lab\n"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
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
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.code {
			t.Errorf("%s %s returned %d, want %d: %s", test.method, test.path, response.Code, test.code, response.Body.String())
		}
	}
}

func TestJobAPIRejectsUnauthorizedAndMalformedRequests(t *testing.T) {
	root := t.TempDir()
	jobs, err := filesystem.NewJobStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithJobs(nil, nil, nil, jobs)
	token := strings.Repeat("j", security.MinAgentTokenBytes)
	handler, err := NewHandler(service, token)
	if err != nil {
		t.Fatal(err)
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, unauthorized)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing token returned %d", response.Code)
	}
	duplicateAuth := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	duplicateAuth.Header.Add("Authorization", "Bearer "+token)
	duplicateAuth.Header.Add("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, duplicateAuth)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("duplicate token headers returned %d", response.Code)
	}

	malformed := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(`{"input":"sites: []"} {}`))
	malformed.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, malformed)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON returned %d: %s", response.Code, response.Body.String())
	}

	invalidInput := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(`{"input":"sites:\n  - unknown: true\n"}`))
	invalidInput.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, invalidInput)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid input returned %d: %s", response.Code, response.Body.String())
	}

	missing := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/job-missing", nil)
	missing.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, missing)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing job returned %d: %s", response.Code, response.Body.String())
	}
}
