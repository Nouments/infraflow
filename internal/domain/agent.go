package domain

import (
	"fmt"
	"time"
)

// Provider-observed Agent connectivity states. They are always derived from
// timestamped, authenticated evidence at read time; a persisted status value is
// never trusted on its own.
const (
	// AgentStatusOnline means a fresh authenticated heartbeat was received by
	// this Provider process within the configured timeout.
	AgentStatusOnline = "online"
	// AgentStatusDegraded means the Agent is heartbeating but reported an
	// operational problem (for example a failed artifact run).
	AgentStatusDegraded = "degraded"
	// AgentStatusOffline means the heartbeat expired or the Agent announced
	// that its process stopped.
	AgentStatusOffline = "offline"
	// AgentStatusUnknown means current state cannot be established: the Agent
	// has never heartbeated, or its last heartbeat predates a Provider restart.
	AgentStatusUnknown = "unknown"
)

// DefaultAgentHeartbeatTimeout is the default heartbeat freshness window.
const DefaultAgentHeartbeatTimeout = 90 * time.Second

type Agent struct {
	ID           string    `json:"id"`
	SiteID       string    `json:"site_id"`
	Version      string    `json:"version,omitempty"`
	Capabilities []string  `json:"capabilities,omitempty"`
	Status       string    `json:"status"`
	QueueDepth   int       `json:"queue_depth"`
	RegisteredAt time.Time `json:"registered_at"`
	// LastSeenAt is the last authenticated contact of any kind (registration,
	// heartbeat, or disconnect notice).
	LastSeenAt time.Time `json:"last_seen_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	// LastHeartbeatAt is the Provider clock time of the last authenticated
	// heartbeat. Nil means no heartbeat was ever received.
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
	// DisconnectedAt records an authenticated, graceful shutdown notice.
	DisconnectedAt *time.Time `json:"disconnected_at,omitempty"`
	// ReportedProblem is the last operational problem reported by the Agent in
	// a heartbeat; empty when the Agent reported none.
	ReportedProblem string `json:"reported_problem,omitempty"`

	// Fields below are computed on read and never persisted.
	StatusReason        string     `json:"status_reason,omitempty"`
	HeartbeatAgeSeconds *int64     `json:"heartbeat_age_seconds,omitempty"`
	TimeoutSeconds      int64      `json:"heartbeat_timeout_seconds,omitempty"`
	ObservedAt          *time.Time `json:"observed_at,omitempty"`
}

// ValidAgentStatus reports whether status is a known value.
func ValidAgentStatus(status string) bool {
	switch status {
	case AgentStatusOnline, AgentStatusDegraded, AgentStatusOffline, AgentStatusUnknown:
		return true
	}
	return false
}

// Persisted returns the evidence-only representation stored on disk: computed
// observation fields are cleared and the status is reset to unknown so that a
// stored record can never resurrect an ONLINE state.
func (agent Agent) Persisted() Agent {
	agent.Status = AgentStatusUnknown
	agent.StatusReason = ""
	agent.HeartbeatAgeSeconds = nil
	agent.TimeoutSeconds = 0
	agent.ObservedAt = nil
	return agent
}

// ObserveAgent derives the Provider-observed connectivity state from evidence.
//
// now is the Provider clock, providerStartedAt is when this Provider process
// started serving, and timeout is the heartbeat freshness window. Heartbeats
// received before providerStartedAt are not trusted as proof of current
// connectivity, so a Provider restart never restores a stale ONLINE state.
func ObserveAgent(agent Agent, now, providerStartedAt time.Time, timeout time.Duration) Agent {
	if timeout <= 0 {
		timeout = DefaultAgentHeartbeatTimeout
	}
	observed := agent
	observedAt := now
	observed.ObservedAt = &observedAt
	observed.TimeoutSeconds = int64(timeout / time.Second)
	observed.HeartbeatAgeSeconds = nil
	if agent.LastHeartbeatAt != nil {
		age := int64(now.Sub(*agent.LastHeartbeatAt) / time.Second)
		if age < 0 {
			age = 0
		}
		observed.HeartbeatAgeSeconds = &age
	}
	switch {
	case agent.DisconnectedAt != nil && (agent.LastHeartbeatAt == nil || !agent.DisconnectedAt.Before(*agent.LastHeartbeatAt)):
		observed.Status = AgentStatusOffline
		observed.StatusReason = fmt.Sprintf("Agent announced process shutdown at %s", agent.DisconnectedAt.UTC().Format(time.RFC3339))
	case agent.LastHeartbeatAt == nil:
		observed.Status = AgentStatusUnknown
		observed.StatusReason = "Registered, but no authenticated heartbeat has been received yet"
	case now.Sub(*agent.LastHeartbeatAt) > timeout:
		observed.Status = AgentStatusOffline
		observed.StatusReason = fmt.Sprintf("No heartbeat for %s (timeout %s)", now.Sub(*agent.LastHeartbeatAt).Truncate(time.Second), timeout)
	case agent.LastHeartbeatAt.Before(providerStartedAt):
		observed.Status = AgentStatusUnknown
		observed.StatusReason = "Last heartbeat predates the Provider restart; awaiting a fresh heartbeat"
	case agent.ReportedProblem != "":
		observed.Status = AgentStatusDegraded
		observed.StatusReason = "Heartbeat is fresh, but the Agent reported: " + agent.ReportedProblem
	default:
		observed.Status = AgentStatusOnline
		observed.StatusReason = "Fresh authenticated heartbeat"
	}
	if observed.Status == AgentStatusOffline && agent.ReportedProblem != "" {
		observed.StatusReason += "; last reported problem: " + agent.ReportedProblem
	}
	return observed
}
