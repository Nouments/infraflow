package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"infraflow/internal/domain"
	"infraflow/internal/security"
	"infraflow/provider/internal/application"
)

const maxRequestBytes = 2 << 20

type Handler struct {
	service *application.Service
	token   []byte
}

type createJobRequest struct {
	Input string `json:"input"`
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

func NewHandler(service *application.Service, token string) (*Handler, error) {
	if service == nil {
		return nil, fmt.Errorf("provider application service is required")
	}
	if len([]byte(token)) < security.MinAgentTokenBytes {
		return nil, fmt.Errorf("API token must be at least %d bytes", security.MinAgentTokenBytes)
	}
	return &Handler{service: service, token: []byte(token)}, nil
}

func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	authorizations := request.Header.Values("Authorization")
	if len(authorizations) != 1 || !security.BearerTokenMatches(authorizations[0], handler.token) {
		writeError(writer, http.StatusUnauthorized, "authentication required")
		return
	}

	const jobsPath = "/api/v1/jobs"
	if request.URL.Path == jobsPath {
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
