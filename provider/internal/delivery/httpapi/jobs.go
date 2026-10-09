package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"infraflow/internal/domain"
	"infraflow/internal/infrastructure/security"
	"infraflow/pkg/observability"
	"infraflow/provider/internal/application"
	userdomain "infraflow/provider/internal/domain"
)

const maxRequestBytes = 2 << 20

type Handler struct {
	service       *application.Service
	agentToken    []byte
	authenticator *application.Authenticator
	logger        *observability.Logger
}

type responseStatusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *responseStatusWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *responseStatusWriter) Write(data []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	return writer.ResponseWriter.Write(data)
}

func (writer *responseStatusWriter) Flush() {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

type principal struct {
	agent bool
	user  userdomain.User
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string          `json:"token"`
	ExpiresAt time.Time       `json:"expires_at"`
	User      userdomain.User `json:"user"`
}

type createUserRequest struct {
	Username string              `json:"username"`
	Password string              `json:"password"`
	Role     userdomain.UserRole `json:"role"`
}

type updateUserRequest struct {
	Role     userdomain.UserRole `json:"role"`
	Disabled *bool               `json:"disabled"`
	Password string              `json:"password"`
}

type createJobRequest struct {
	Input string `json:"input"`
}

type previewPlanResponse struct {
	Infrastructure domain.Infrastructure `json:"infrastructure"`
	Plan           domain.Plan           `json:"plan"`
}

type planGenerationErrorResponse struct {
	Error  string                           `json:"error"`
	Result application.PlanGenerationResult `json:"result"`
}

type registerAgentRequest struct {
	AgentID      string   `json:"agent_id"`
	SiteID       string   `json:"site_id"`
	Version      string   `json:"version,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type heartbeatRequest struct {
	SiteID       string   `json:"site_id"`
	Version      string   `json:"version,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	QueueDepth   int      `json:"queue_depth"`
}

type agentLogRequest struct {
	AgentID string                `json:"agent_id"`
	Events  []observability.Event `json:"events"`
}

func NewHandler(service *application.Service, token string, authenticators ...*application.Authenticator) (*Handler, error) {
	if service == nil {
		return nil, fmt.Errorf("provider application service is required")
	}
	if len([]byte(token)) < security.MinAgentTokenBytes {
		return nil, fmt.Errorf("API token must be at least %d bytes", security.MinAgentTokenBytes)
	}
	var authenticator *application.Authenticator
	if len(authenticators) > 1 {
		return nil, fmt.Errorf("only one user authenticator may be configured")
	}
	if len(authenticators) == 1 {
		authenticator = authenticators[0]
	}
	return &Handler{service: service, agentToken: []byte(token), authenticator: authenticator}, nil
}

func (handler *Handler) SetLogger(logger *observability.Logger) { handler.logger = logger }

func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	started := time.Now()
	statusWriter := &responseStatusWriter{ResponseWriter: writer}
	writer = statusWriter
	defer func() {
		if handler.logger == nil {
			return
		}
		status := statusWriter.status
		if status == 0 {
			status = http.StatusOK
		}
		level := "INFO"
		if status >= 500 {
			level = "ERROR"
		} else if status >= 400 {
			level = "WARN"
		}
		duration := time.Since(started).Milliseconds()
		if err := handler.logger.Emit(request.Context(), observability.Event{
			Level: level, Event: "provider.http.request", Message: "HTTP request completed",
			Service: "provider", Source: "httpapi", Operation: request.Method,
			Status: strconv.Itoa(status), DurationMS: &duration, Line: request.URL.Path,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "infraflow-provider: persist HTTP log event: %v\n", err)
		}
	}()
	if request.URL.Path == "/api/v1/auth/login" {
		if request.Method != http.MethodPost {
			writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler.login(writer, request)
		return
	}
	identity, authenticated := handler.authenticate(request)
	if !authenticated {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return
	}

	if request.URL.Path == "/api/v1/auth/me" {
		if identity.agent {
			writeError(writer, http.StatusForbidden, "user session required")
			return
		}
		writeJSON(writer, http.StatusOK, identity.user)
		return
	}
	if request.URL.Path == "/api/v1/auth/logout" {
		if request.Method != http.MethodPost || identity.agent || handler.authenticator == nil {
			writeError(writer, http.StatusForbidden, "user session required")
			return
		}
		token, _ := requestBearerToken(request)
		if err := handler.authenticator.Logout(request.Context(), token); err != nil {
			writeError(writer, http.StatusInternalServerError, "session could not be closed")
			return
		}
		writer.WriteHeader(http.StatusNoContent)
		return
	}

	if strings.HasPrefix(request.URL.Path, "/api/v1/users") {
		if identity.agent || identity.user.Role != userdomain.UserRoleAdmin {
			writeError(writer, http.StatusForbidden, "administrator role required")
			return
		}
		handler.handleUsers(writer, request, identity.user)
		return
	}
	if request.URL.Path == "/api/v1/plan/preview" {
		if identity.agent {
			writeError(writer, http.StatusForbidden, "user session required")
			return
		}
		if request.Method != http.MethodPost {
			writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler.previewPlan(writer, request)
		return
	}
	if request.URL.Path == "/api/v1/plan/generate" {
		if identity.agent {
			writeError(writer, http.StatusForbidden, "user session required")
			return
		}
		if request.Method != http.MethodPost {
			writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler.generatePlan(writer, request)
		return
	}
	if request.URL.Path == "/api/v1/agent/logs" {
		if !identity.agent {
			writeError(writer, http.StatusForbidden, "agent authentication required")
			return
		}
		if request.Method != http.MethodPost {
			writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler.ingestAgentLogs(writer, request)
		return
	}
	if request.URL.Path == "/api/v1/logs" || request.URL.Path == "/api/v1/logs/stream" || strings.HasPrefix(request.URL.Path, "/api/v1/logs/runs/") {
		if identity.agent || identity.user.Role != userdomain.UserRoleAdmin {
			writeError(writer, http.StatusForbidden, "administrator role required")
			return
		}
		switch request.URL.Path {
		case "/api/v1/logs":
			if request.Method != http.MethodGet {
				writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			handler.listTechnicalLogs(writer, request)
		case "/api/v1/logs/stream":
			if request.Method != http.MethodGet {
				writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			handler.streamTechnicalLogs(writer, request)
		default:
			if request.Method != http.MethodGet {
				writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			prefix := "/api/v1/logs/runs/"
			runID := strings.TrimPrefix(request.URL.Path, prefix)
			if runID == "" || strings.Contains(runID, "/") {
				writeError(writer, http.StatusBadRequest, "run id is invalid")
				return
			}
			query := request.URL.Query()
			query.Set("run_id", runID)
			request.URL.RawQuery = query.Encode()
			handler.listTechnicalLogs(writer, request)
		}
		return
	}

	if strings.HasPrefix(request.URL.Path, "/api/v1/agents/register") || strings.Contains(request.URL.Path, "/heartbeat") {
		if !identity.agent {
			writeError(writer, http.StatusForbidden, "agent authentication required")
			return
		}
	}
	if request.URL.Path == "/api/v1/agents" || request.URL.Path == "/api/v1/events" || strings.HasPrefix(request.URL.Path, "/api/v1/agents/") {
		if !identity.agent && identity.user.Role != userdomain.UserRoleAdmin {
			writeError(writer, http.StatusForbidden, "administrator role required")
			return
		}
	}

	const jobsPath = "/api/v1/jobs"
	if request.URL.Path == jobsPath {
		if identity.agent {
			writeError(writer, http.StatusForbidden, "user session required")
			return
		}
		switch request.Method {
		case http.MethodGet:
			handler.listJobs(writer)
		case http.MethodPost:
			handler.createJob(writer, request)
		default:
			writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	const agentsPath = "/api/v1/agents"
	if request.URL.Path == agentsPath {
		switch request.Method {
		case http.MethodGet:
			handler.listAgents(writer)
		case http.MethodPost:
			handler.registerAgent(writer, request)
		default:
			writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	const eventsPath = "/api/v1/events"
	if request.URL.Path == eventsPath {
		if request.Method != http.MethodGet {
			writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		handler.listEvents(writer, request)
		return
	}
	agentsPrefix := agentsPath + "/"
	if strings.HasPrefix(request.URL.Path, agentsPrefix) {
		parts := strings.Split(strings.TrimPrefix(request.URL.Path, agentsPrefix), "/")
		if len(parts) == 1 && parts[0] == "register" && request.Method == http.MethodPost {
			handler.registerAgent(writer, request)
			return
		}
		if len(parts) == 1 && request.Method == http.MethodGet {
			handler.getAgent(writer, parts[0])
			return
		}
		if len(parts) == 2 && request.Method == http.MethodPost && parts[1] == "heartbeat" {
			handler.heartbeatAgent(writer, request, parts[0])
			return
		}
		writeError(writer, http.StatusNotFound, "route not found")
		return
	}

	prefix := jobsPath + "/"
	if !strings.HasPrefix(request.URL.Path, prefix) {
		writeError(writer, http.StatusNotFound, "route not found")
		return
	}
	if identity.agent {
		writeError(writer, http.StatusForbidden, "user session required")
		return
	}
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, prefix), "/")
	if len(parts) == 1 && request.Method == http.MethodGet {
		handler.getJob(writer, parts[0])
		return
	}
	if len(parts) == 2 && request.Method == http.MethodPost && (parts[1] == "cancel" || parts[1] == "retry") {
		handler.changeJob(writer, parts[0], parts[1])
		return
	}
	writeError(writer, http.StatusNotFound, "route not found")
}

func (handler *Handler) authenticate(request *http.Request) (principal, bool) {
	authorizations := request.Header.Values("Authorization")
	if len(authorizations) != 1 {
		return principal{}, false
	}
	if security.BearerTokenMatches(authorizations[0], handler.agentToken) {
		return principal{agent: true}, true
	}
	if handler.authenticator == nil {
		return principal{}, false
	}
	token, ok := security.BearerToken(authorizations[0])
	if !ok {
		return principal{}, false
	}
	user, err := handler.authenticator.Authenticate(request.Context(), token)
	if err != nil {
		return principal{}, false
	}
	return principal{user: user}, true
}

func requestBearerToken(request *http.Request) (string, bool) {
	values := request.Header.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}
	return security.BearerToken(values[0])
}

func (handler *Handler) login(writer http.ResponseWriter, request *http.Request) {
	if handler.authenticator == nil {
		writeError(writer, http.StatusServiceUnavailable, "user authentication is not configured")
		return
	}
	var input loginRequest
	if err := decodeRequest(writer, request, &input); err != nil {
		return
	}
	session, err := handler.authenticator.Login(request.Context(), input.Username, input.Password)
	if err != nil {
		if errors.Is(err, application.ErrInvalidCredentials) {
			writeError(writer, http.StatusUnauthorized, "invalid credentials")
			return
		}
		writeError(writer, http.StatusInternalServerError, "login failed")
		return
	}
	writeJSON(writer, http.StatusOK, loginResponse{Token: session.Token, ExpiresAt: session.ExpiresAt, User: session.User})
}

func (handler *Handler) handleUsers(writer http.ResponseWriter, request *http.Request, actor userdomain.User) {
	if handler.authenticator == nil {
		writeError(writer, http.StatusServiceUnavailable, "user authentication is not configured")
		return
	}
	if request.URL.Path == "/api/v1/users" {
		switch request.Method {
		case http.MethodGet:
			users, err := handler.authenticator.ListUsers(request.Context())
			if err != nil {
				writeError(writer, http.StatusInternalServerError, "users are unavailable")
				return
			}
			writeJSON(writer, http.StatusOK, users)
		case http.MethodPost:
			var input createUserRequest
			if err := decodeRequest(writer, request, &input); err != nil {
				return
			}
			if input.Role == "" {
				input.Role = userdomain.UserRoleUser
			}
			user, err := handler.authenticator.CreateUser(request.Context(), input.Username, input.Password, input.Role)
			if err != nil {
				handler.writeUserError(writer, err)
				return
			}
			writeJSON(writer, http.StatusCreated, user)
		default:
			writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	prefix := "/api/v1/users/"
	if !strings.HasPrefix(request.URL.Path, prefix) || request.Method != http.MethodPatch {
		writeError(writer, http.StatusNotFound, "route not found")
		return
	}
	id := strings.TrimPrefix(request.URL.Path, prefix)
	if id == "" || strings.Contains(id, "/") {
		writeError(writer, http.StatusNotFound, "user not found")
		return
	}
	user, err := handler.authenticatorUser(request.Context(), id)
	if err != nil {
		handler.writeUserError(writer, err)
		return
	}
	var input updateUserRequest
	if err := decodeRequest(writer, request, &input); err != nil {
		return
	}
	if input.Role != "" {
		user.Role = input.Role
	}
	if input.Disabled != nil {
		user.Disabled = *input.Disabled
	}
	if err := application.ValidateUserUpdate(user, input.Password); err != nil {
		handler.writeUserError(writer, err)
		return
	}
	if err := handler.authenticator.EnsureAdminChangeAllowed(request.Context(), actor, user); err != nil {
		handler.writeUserError(writer, err)
		return
	}
	if _, err := handler.authenticator.UpdateUser(request.Context(), user, input.Password); err != nil {
		handler.writeUserError(writer, err)
		return
	}
	updated, err := handler.authenticatorUser(request.Context(), id)
	if err != nil {
		handler.writeUserError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, updated)
}

func (handler *Handler) authenticatorUser(ctx context.Context, id string) (userdomain.User, error) {
	// Lookup through the authenticated repository is intentionally kept behind
	// the application authenticator; the handler never reads SQLite directly.
	return handler.authenticator.User(ctx, id)
}

func (handler *Handler) writeUserError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrInvalidUser):
		writeError(writer, http.StatusBadRequest, err.Error())
	case errors.Is(err, application.ErrUserConflict):
		writeError(writer, http.StatusConflict, "username already exists")
	case errors.Is(err, application.ErrUserNotFound):
		writeError(writer, http.StatusNotFound, "user not found")
	case errors.Is(err, application.ErrLastAdmin):
		writeError(writer, http.StatusConflict, err.Error())
	default:
		writeError(writer, http.StatusInternalServerError, "user operation failed")
	}
}

func (handler *Handler) listAgents(writer http.ResponseWriter) {
	agents, err := handler.service.Agents()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "agents are unavailable")
		return
	}
	if agents == nil {
		agents = []domain.Agent{}
	}
	writeJSON(writer, http.StatusOK, agents)
}

func (handler *Handler) listEvents(writer http.ResponseWriter, request *http.Request) {
	limit := 100
	if rawLimit := request.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeError(writer, http.StatusBadRequest, "limit must be between 1 and 1000")
			return
		}
		limit = parsed
	}
	events := handler.service.Events()
	if len(events) > limit {
		events = events[len(events)-limit:]
	}
	if events == nil {
		events = []domain.Event{}
	}
	writeJSON(writer, http.StatusOK, events)
}

func (handler *Handler) registerAgent(writer http.ResponseWriter, request *http.Request) {
	var input registerAgentRequest
	if err := decodeRequest(writer, request, &input); err != nil {
		return
	}
	agent, err := handler.service.RegisterAgent(application.AgentRegistration{
		ID: input.AgentID, SiteID: input.SiteID, Version: input.Version, Capabilities: input.Capabilities,
	})
	if err != nil {
		handler.writeAgentError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, agent)
}

func (handler *Handler) getAgent(writer http.ResponseWriter, id string) {
	agent, err := handler.service.Agent(id)
	if err != nil {
		handler.writeAgentError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, agent)
}

func (handler *Handler) heartbeatAgent(writer http.ResponseWriter, request *http.Request, id string) {
	var input heartbeatRequest
	if err := decodeRequest(writer, request, &input); err != nil {
		return
	}
	agent, err := handler.service.HeartbeatAgent(id, application.AgentHeartbeat{
		SiteID: input.SiteID, Version: input.Version, Capabilities: input.Capabilities, QueueDepth: input.QueueDepth,
	})
	if err != nil {
		handler.writeAgentError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, agent)
}

func decodeRequest(writer http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(writer, http.StatusBadRequest, "request body must contain one valid JSON object")
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(writer, http.StatusBadRequest, "request body must contain one JSON object")
		return err
	}
	return nil
}

func (handler *Handler) listJobs(writer http.ResponseWriter) {
	jobs, err := handler.service.Jobs()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "jobs are unavailable")
		return
	}
	if jobs == nil {
		jobs = []domain.Job{}
	}
	writeJSON(writer, http.StatusOK, jobs)
}

func (handler *Handler) createJob(writer http.ResponseWriter, request *http.Request) {
	var input createJobRequest
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, "request body must contain a JSON input string")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(writer, http.StatusBadRequest, "request body must contain one JSON object")
		return
	}
	job, err := handler.service.CreateJob([]byte(input.Input))
	if err != nil {
		if errors.Is(err, application.ErrInvalidJobInput) {
			writeError(writer, http.StatusBadRequest, err.Error())
		} else {
			writeError(writer, http.StatusInternalServerError, "job could not be persisted")
		}
		return
	}
	writeJSON(writer, http.StatusCreated, job)
}

func (handler *Handler) previewPlan(writer http.ResponseWriter, request *http.Request) {
	var input createJobRequest
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, "request body must contain a JSON input string")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(writer, http.StatusBadRequest, "request body must contain one JSON object")
		return
	}
	infrastructure, plan, err := handler.service.Preview([]byte(input.Input))
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, previewPlanResponse{Infrastructure: infrastructure, Plan: plan})
}

func (handler *Handler) generatePlan(writer http.ResponseWriter, request *http.Request) {
	var input createJobRequest
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, "request body must contain a JSON input string")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(writer, http.StatusBadRequest, "request body must contain one JSON object")
		return
	}
	result, err := handler.service.PlanAndGenerate([]byte(input.Input))
	if err != nil {
		if result.Plan.ID == "" {
			writeError(writer, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(writer, http.StatusUnprocessableEntity, planGenerationErrorResponse{Error: err.Error(), Result: result})
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (handler *Handler) getJob(writer http.ResponseWriter, id string) {
	job, err := handler.service.Job(id)
	if err != nil {
		handler.writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, job)
}

func (handler *Handler) changeJob(writer http.ResponseWriter, id, action string) {
	var (
		job domain.Job
		err error
	)
	if action == "cancel" {
		job, err = handler.service.CancelJob(id)
	} else {
		job, err = handler.service.RetryJob(id)
	}
	if err != nil {
		handler.writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, job)
}

func (handler *Handler) writeServiceError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrJobNotFound):
		writeError(writer, http.StatusNotFound, "job not found")
	case errors.Is(err, application.ErrJobConflict):
		writeError(writer, http.StatusConflict, "job state does not allow this operation")
	default:
		writeError(writer, http.StatusInternalServerError, "job is unavailable")
	}
}

func (handler *Handler) writeAgentError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrInvalidAgent):
		writeError(writer, http.StatusBadRequest, err.Error())
	case errors.Is(err, application.ErrAgentNotFound):
		writeError(writer, http.StatusNotFound, "agent not found")
	case errors.Is(err, application.ErrAgentConflict):
		writeError(writer, http.StatusConflict, "agent identity is already bound to another site")
	default:
		writeError(writer, http.StatusInternalServerError, "agents are unavailable")
	}
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}
