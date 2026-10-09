package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	configadapter "infraflow/internal/adapters/config"
	"infraflow/internal/infrastructure/security"
	"infraflow/provider/internal/adapters/filesystem"
	"infraflow/provider/internal/adapters/sqlite"
	"infraflow/provider/internal/application"
	"infraflow/provider/internal/delivery/httpapi"

	"github.com/gofiber/fiber/v2"
)

func TestFiberConsoleUsesExistingAuthenticatedAPI(t *testing.T) {
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
	service := application.NewServiceWithJobs(nil, nil, nil, jobs, application.Dependencies{Parser: configadapter.Parser{}})
	apiHandler, err := httpapi.NewHandler(service, strings.Repeat("a", security.MinAgentTokenBytes), authenticator)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(apiHandler, true)

	page := fiberRequest(t, app, http.MethodGet, "/", nil, "")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "InfraFlow Control") || page.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("console page or security headers missing: %d %q %#v", page.Code, page.Body.String(), page.Header())
	}
	if strings.Contains(page.Body.String(), "Provider connected") {
		t.Fatal("console must not claim provider connectivity without a live health signal")
	}
	asset := fiberRequest(t, app, http.MethodGet, "/assets/app.js", nil, "")
	if asset.Code != http.StatusOK || !strings.Contains(asset.Body.String(), "/auth/login") {
		t.Fatalf("console JavaScript was not served: %d %q", asset.Code, asset.Body.String())
	}
	streamAsset := fiberRequest(t, app, http.MethodGet, "/assets/log-stream.js", nil, "")
	if streamAsset.Code != http.StatusOK || !strings.Contains(streamAsset.Body.String(), "decodeFrame") {
		t.Fatalf("technical log stream JavaScript was not served: %d %q", streamAsset.Code, streamAsset.Body.String())
	}
	unauthorized := fiberRequest(t, app, http.MethodGet, "/api/v1/jobs", nil, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("Fiber API route bypassed existing auth: %d %s", unauthorized.Code, unauthorized.Body.String())
	}
	login := fiberRequest(t, app, http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"correct horse battery staple"}`), "")
	if login.Code != http.StatusOK {
		t.Fatalf("login through Fiber returned %d: %s", login.Code, login.Body.String())
	}
	var session struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil || session.Token == "" {
		t.Fatalf("invalid login response: %#v, %v", session, err)
	}
	jobsResponse := fiberRequest(t, app, http.MethodGet, "/api/v1/jobs", nil, session.Token)
	if jobsResponse.Code != http.StatusOK {
		t.Fatalf("authenticated API request through Fiber returned %d: %s", jobsResponse.Code, jobsResponse.Body.String())
	}
}

func TestFiberCanDisableConsoleWithoutDisablingAPI(t *testing.T) {
	apiHandler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})
	app := NewApp(apiHandler, false)
	page := fiberRequest(t, app, http.MethodGet, "/", nil, "")
	if page.Code != http.StatusNotFound {
		t.Fatalf("disabled web UI returned %d", page.Code)
	}
	api := fiberRequest(t, app, http.MethodGet, "/api/v1/health", nil, "")
	if api.Code != http.StatusNoContent {
		t.Fatalf("API was disabled with web UI: %d", api.Code)
	}
}

func fiberRequest(t *testing.T, app *fiber.App, method, path string, body io.Reader, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, body)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	recorder.Code = response.StatusCode
	recorder.HeaderMap = response.Header
	_, _ = recorder.Body.Write(data)
	return recorder
}
