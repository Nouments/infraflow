package httpapi

import (
	"errors"
	"net/http"
	"time"

	"infraflow/internal/domain"
	"infraflow/pkg/observability"
	"infraflow/provider/internal/application"
)

type providerStatusResponse struct {
	Status                  string         `json:"status"`
	Time                    time.Time      `json:"time"`
	StartedAt               time.Time      `json:"started_at"`
	UptimeSeconds           int64          `json:"uptime_seconds"`
	HeartbeatTimeoutSeconds int64          `json:"agent_heartbeat_timeout_seconds"`
	Agents                  map[string]int `json:"agents"`
	AgentsAvailable         bool           `json:"agents_available"`
	ExecutionsAvailable     bool           `json:"executions_available"`
	LogsAvailable           bool           `json:"logs_available"`
	Problems                []string       `json:"problems"`
}

// providerStatus reports what this Provider process can observe right now. It
// never claims connectivity it has not observed.
func (handler *Handler) providerStatus(writer http.ResponseWriter) {
	now := time.Now().UTC()
	started := handler.service.ProviderStartedAt()
	response := providerStatusResponse{
		Status: "ok", Time: now, StartedAt: started, UptimeSeconds: int64(now.Sub(started) / time.Second),
		HeartbeatTimeoutSeconds: int64(handler.service.AgentHeartbeatTimeout() / time.Second),
		Agents:                  map[string]int{domain.AgentStatusOnline: 0, domain.AgentStatusDegraded: 0, domain.AgentStatusOffline: 0, domain.AgentStatusUnknown: 0},
		ExecutionsAvailable:     handler.executions != nil, Problems: []string{},
	}
	agents, err := handler.service.Agents()
	switch {
	case err == nil:
		response.AgentsAvailable = true
		for _, agent := range agents {
			response.Agents[agent.Status]++
		}
	case errors.Is(err, application.ErrAgentRepositoryNotConfigured):
		response.Problems = append(response.Problems, "agent registry is not configured")
	default:
		response.Status = "degraded"
		response.Problems = append(response.Problems, "agent registry is unavailable")
	}
	if _, err := handler.service.Logs(observabilityProbe()); err == nil {
		response.LogsAvailable = true
	} else {
		response.Problems = append(response.Problems, "technical log store is unavailable")
	}
	if !response.ExecutionsAvailable {
		response.Problems = append(response.Problems, "execution service is not configured")
	}
	writeJSON(writer, http.StatusOK, response)
}

func (handler *Handler) listSites(writer http.ResponseWriter) {
	sites, err := handler.service.Sites()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "sites are unavailable")
		return
	}
	if sites == nil {
		sites = []application.SiteSummary{}
	}
	writeJSON(writer, http.StatusOK, sites)
}

func (handler *Handler) getSite(writer http.ResponseWriter, id string) {
	site, err := handler.service.Site(id)
	if errors.Is(err, application.ErrSiteNotFound) {
		writeError(writer, http.StatusNotFound, "site not found")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "site is unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, site)
}

func (handler *Handler) diagnose(writer http.ResponseWriter, request *http.Request) {
	values := request.URL.Query()
	report, err := handler.service.Diagnose(application.DiagnosticRequest{
		RunID: values.Get("run_id"), AgentID: values.Get("agent_id"), SiteID: values.Get("site_id"),
		DeviceID: values.Get("device_id"), ExpectedHostname: values.Get("expected_hostname"),
		ExpectedIPv4: values.Get("expected_ipv4"),
	})
	if errors.Is(err, application.ErrInvalidAgent) {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "diagnostics are unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, report)
}

func observabilityProbe() observability.Query { return observability.Query{Limit: 1} }
