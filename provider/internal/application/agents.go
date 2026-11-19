package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"infraflow/internal/domain"
	"infraflow/pkg/observability"
	"infraflow/pkg/protocol"
)

const maxAgentProblemBytes = 256

// agentClock holds the time source and freshness policy used to derive
// Provider-observed Agent status. It is shared by pointer so that tests can
// inject a deterministic clock.
type agentClock struct {
	mu        *sync.Mutex
	now       func() time.Time
	startedAt time.Time
	timeout   time.Duration
}

func newAgentClock() agentClock {
	return agentClock{mu: &sync.Mutex{}, now: time.Now, startedAt: time.Now().UTC(), timeout: domain.DefaultAgentHeartbeatTimeout}
}

func (service *Service) clockNow() time.Time {
	if service.agentClock.now == nil {
		return time.Now().UTC()
	}
	return service.agentClock.now().UTC()
}

func (service *Service) agentLock() func() {
	if service.agentClock.mu == nil {
		service.agentClock.mu = &sync.Mutex{}
	}
	service.agentClock.mu.Lock()
	return service.agentClock.mu.Unlock
}

// SetAgentClock replaces the time source and records the Provider start time
// as the current instant of that source. It is intended for deterministic
// tests and must be called before the service handles requests.
func (service *Service) SetAgentClock(now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	service.agentClock.now = now
	service.agentClock.startedAt = now().UTC()
}

// SetAgentHeartbeatTimeout configures the heartbeat freshness window.
func (service *Service) SetAgentHeartbeatTimeout(timeout time.Duration) error {
	if timeout < 5*time.Second || timeout > 24*time.Hour {
		return fmt.Errorf("agent heartbeat timeout must be between 5s and 24h")
	}
	service.agentClock.timeout = timeout
	return nil
}

// AgentHeartbeatTimeout returns the configured heartbeat freshness window.
func (service *Service) AgentHeartbeatTimeout() time.Duration {
	if service.agentClock.timeout <= 0 {
		return domain.DefaultAgentHeartbeatTimeout
	}
	return service.agentClock.timeout
}

// ProviderStartedAt returns when this Provider process started serving.
func (service *Service) ProviderStartedAt() time.Time { return service.agentClock.startedAt }

func (service *Service) observe(agent domain.Agent) domain.Agent {
	return domain.ObserveAgent(agent, service.clockNow(), service.agentClock.startedAt, service.AgentHeartbeatTimeout())
}

func (service *Service) RegisterAgent(input AgentRegistration) (domain.Agent, error) {
	return service.RegisterAgentContext(context.Background(), input)
}

// RegisterAgentContext records an authenticated registration. Registration is
// identity evidence, not liveness evidence: a newly registered Agent stays
// UNKNOWN until it sends a heartbeat.
func (service *Service) RegisterAgentContext(ctx context.Context, input AgentRegistration) (domain.Agent, error) {
	if service.agents == nil {
		return domain.Agent{}, fmt.Errorf("agent repository is not configured")
	}
	if err := validateAgentRegistration(input); err != nil {
		return domain.Agent{}, fmt.Errorf("%w: %v", ErrInvalidAgent, err)
	}
	unlock := service.agentLock()
	defer unlock()
	now := service.clockNow()
	agent := domain.Agent{
		ID: input.ID, SiteID: input.SiteID, Version: input.Version,
		Capabilities: append([]string(nil), input.Capabilities...),
		Status:       domain.AgentStatusUnknown, RegisteredAt: now, LastSeenAt: now, UpdatedAt: now,
	}
	existing, err := service.agents.Get(input.ID)
	switch {
	case err == nil:
		if existing.SiteID != input.SiteID {
			return domain.Agent{}, ErrAgentConflict
		}
		// Re-registration (for example after an Agent restart) keeps the
		// original registration time but discards liveness evidence from the
		// previous process: a fresh heartbeat is required to become ONLINE.
		agent.RegisteredAt = existing.RegisteredAt
		agent.QueueDepth = existing.QueueDepth
		if err := service.agents.Update(agent.Persisted()); err != nil {
			return domain.Agent{}, fmt.Errorf("update agent: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		created, err := service.agents.Register(agent.Persisted())
		if err != nil {
			return domain.Agent{}, fmt.Errorf("register agent: %w", err)
		}
		if !created {
			existing, err := service.agents.Get(input.ID)
			if err != nil {
				return domain.Agent{}, fmt.Errorf("reload registered agent: %w", err)
			}
			if existing.SiteID != input.SiteID {
				return domain.Agent{}, ErrAgentConflict
			}
			agent.RegisteredAt = existing.RegisteredAt
			if err := service.agents.Update(agent.Persisted()); err != nil {
				return domain.Agent{}, fmt.Errorf("update concurrently registered agent: %w", err)
			}
		}
	default:
		return domain.Agent{}, fmt.Errorf("inspect agent: %w", err)
	}
	if err := service.recordEvent(ctx, "agent.registered", agent.SiteID, agent.ID, "", map[string]any{"version": agent.Version, "capabilities": agent.Capabilities}); err != nil {
		return domain.Agent{}, fmt.Errorf("record agent event: %w", err)
	}
	service.emitAgentLog(ctx, agent, "agent.registration.completed", "agent registered and site authorized", "COMPLETED", "")
	return service.observe(agent), nil
}

func (service *Service) Agents() ([]domain.Agent, error) {
	if service.agents == nil {
		return nil, ErrAgentRepositoryNotConfigured
	}
	agents, err := service.agents.List()
	if err != nil {
		return nil, err
	}
	observed := make([]domain.Agent, 0, len(agents))
	for _, agent := range agents {
		observed = append(observed, service.observe(agent))
	}
	return observed, nil
}

func (service *Service) Agent(id string) (domain.Agent, error) {
	agent, err := service.storedAgent(id)
	if err != nil {
		return domain.Agent{}, err
	}
	return service.observe(agent), nil
}

func (service *Service) storedAgent(id string) (domain.Agent, error) {
	if service.agents == nil {
		return domain.Agent{}, fmt.Errorf("agent repository is not configured")
	}
	if !protocol.ValidSiteName(id) || len(id) > 128 {
		return domain.Agent{}, ErrAgentNotFound
	}
	agent, err := service.agents.Get(id)
	if errors.Is(err, os.ErrNotExist) {
		return domain.Agent{}, ErrAgentNotFound
	}
	return agent, err
}

func (service *Service) HeartbeatAgent(id string, input AgentHeartbeat) (domain.Agent, error) {
	return service.HeartbeatAgentContext(context.Background(), id, input)
}

// HeartbeatAgentContext records fresh, authenticated liveness evidence.
func (service *Service) HeartbeatAgentContext(ctx context.Context, id string, input AgentHeartbeat) (domain.Agent, error) {
	if err := validateAgentHeartbeat(input); err != nil {
		return domain.Agent{}, fmt.Errorf("%w: %v", ErrInvalidAgent, err)
	}
	unlock := service.agentLock()
	defer unlock()
	agent, err := service.storedAgent(id)
	if err != nil {
		return domain.Agent{}, err
	}
	if input.SiteID != agent.SiteID {
		return domain.Agent{}, ErrAgentConflict
	}
	now := service.clockNow()
	previousProblem := agent.ReportedProblem
	agent.Version = input.Version
	agent.Capabilities = append([]string(nil), input.Capabilities...)
	agent.QueueDepth = input.QueueDepth
	agent.ReportedProblem = observability.Redact(strings.TrimSpace(input.Problem))
	agent.LastHeartbeatAt = &now
	agent.DisconnectedAt = nil
	agent.LastSeenAt = now
	agent.UpdatedAt = now
	if err := service.agents.Update(agent.Persisted()); err != nil {
		return domain.Agent{}, fmt.Errorf("update agent heartbeat: %w", err)
	}
	if err := service.recordEvent(ctx, "agent.heartbeat", agent.SiteID, agent.ID, "", map[string]any{"queue_depth": agent.QueueDepth, "version": agent.Version, "problem": agent.ReportedProblem}); err != nil {
		return domain.Agent{}, fmt.Errorf("record heartbeat event: %w", err)
	}
	if agent.ReportedProblem != "" && agent.ReportedProblem != previousProblem {
		service.emitAgentLog(ctx, agent, "agent.heartbeat.degraded", "agent heartbeat reported an operational problem", "DEGRADED", agent.ReportedProblem)
	}
	return service.observe(agent), nil
}

// DisconnectAgentContext records an authenticated, graceful shutdown notice.
// The Agent becomes OFFLINE immediately instead of after the timeout.
func (service *Service) DisconnectAgentContext(ctx context.Context, id, siteID string) (domain.Agent, error) {
	if !protocol.ValidSiteName(siteID) || len(siteID) > 128 {
		return domain.Agent{}, fmt.Errorf("%w: site id is invalid", ErrInvalidAgent)
	}
	unlock := service.agentLock()
	defer unlock()
	agent, err := service.storedAgent(id)
	if err != nil {
		return domain.Agent{}, err
	}
	if siteID != agent.SiteID {
		return domain.Agent{}, ErrAgentConflict
	}
	now := service.clockNow()
	agent.DisconnectedAt = &now
	agent.LastSeenAt = now
	agent.UpdatedAt = now
	if err := service.agents.Update(agent.Persisted()); err != nil {
		return domain.Agent{}, fmt.Errorf("update agent disconnect: %w", err)
	}
	if err := service.recordEvent(ctx, "agent.disconnected", agent.SiteID, agent.ID, "", map[string]any{"graceful": true}); err != nil {
		return domain.Agent{}, fmt.Errorf("record disconnect event: %w", err)
	}
	service.emitAgentLog(ctx, agent, "agent.disconnected", "agent announced graceful process shutdown", "STOPPED", "")
	return service.observe(agent), nil
}

// emitAgentLog writes a best-effort technical log entry into the central log
// store so that diagnostics can correlate the connectivity lifecycle. Failure
// to log never changes the outcome of the authenticated operation.
func (service *Service) emitAgentLog(_ context.Context, agent domain.Agent, name, message, status, detail string) {
	if service.logs == nil {
		return
	}
	id, err := observability.NewEventID()
	if err != nil {
		return
	}
	level := "INFO"
	if status == "DEGRADED" {
		level = "WARN"
	}
	_, _ = service.logs.Append([]observability.Event{{
		ID: id, Timestamp: service.clockNow().Format(time.RFC3339Nano), Level: level, Event: name, Message: message,
		Service: "provider", Source: "agent-registry", SiteID: agent.SiteID, AgentID: agent.ID,
		Operation: "agent-connectivity", Status: status, Error: detail,
	}})
}

func validateAgentProblem(problem string) error {
	if len(problem) > maxAgentProblemBytes || strings.ContainsAny(problem, "\r\n\x00") {
		return fmt.Errorf("agent problem must be a single line of at most %d bytes", maxAgentProblemBytes)
	}
	return nil
}
