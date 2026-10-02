package application

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"infraflow/internal/config"
	"infraflow/internal/domain"
	"infraflow/internal/planner"
	"infraflow/pkg/protocol"
)

var ErrInvalidAgentReport = errors.New("invalid agent report")
var ErrInvalidJobInput = errors.New("invalid job input")
var ErrInvalidAgent = errors.New("invalid agent")
var ErrAgentNotFound = errors.New("agent not found")
var ErrAgentConflict = errors.New("agent identity conflict")
var ErrJobNotFound = errors.New("job not found")
var ErrJobConflict = errors.New("job state does not allow this operation")

type ArtifactRepository interface {
	Catalog() ([]protocol.Artifact, error)
	Open(path string) (io.ReadCloser, protocol.Artifact, error)
}

type ReportRepository interface {
	Append(protocol.AgentReport) (bool, error)
	List() []protocol.AgentReport
}

type ArtifactGenerator interface {
	Generate(domain.Infrastructure, string) ([]protocol.Artifact, error)
}

type JobRepository interface {
	Create(domain.Job) error
	Get(string) (domain.Job, error)
	List() ([]domain.Job, error)
	Update(domain.Job) error
}

type AgentRepository interface {
	Register(domain.Agent) (bool, error)
	Get(string) (domain.Agent, error)
	List() ([]domain.Agent, error)
	Update(domain.Agent) error
}

type EventRepository interface {
	Append(domain.Event) (domain.Event, bool, error)
	List() []domain.Event
}

type AgentRegistration struct {
	ID           string
	SiteID       string
	Version      string
	Capabilities []string
}

type AgentHeartbeat struct {
	SiteID       string
	Version      string
	Capabilities []string
	QueueDepth   int
}

type Service struct {
	artifacts ArtifactRepository
	reports   ReportRepository
	generator ArtifactGenerator
	jobs      JobRepository
	agents    AgentRepository
	events    EventRepository
}

func NewService(artifacts ArtifactRepository, reports ReportRepository, generator ArtifactGenerator) *Service {
	return &Service{artifacts: artifacts, reports: reports, generator: generator}
}

func NewServiceWithJobs(artifacts ArtifactRepository, reports ReportRepository, generator ArtifactGenerator, jobs JobRepository) *Service {
	return &Service{artifacts: artifacts, reports: reports, generator: generator, jobs: jobs}
}

func NewServiceWithJobsAndAgents(artifacts ArtifactRepository, reports ReportRepository, generator ArtifactGenerator, jobs JobRepository, agents AgentRepository) *Service {
	return &Service{artifacts: artifacts, reports: reports, generator: generator, jobs: jobs, agents: agents}
}

func NewServiceWithJobsAgentsEvents(artifacts ArtifactRepository, reports ReportRepository, generator ArtifactGenerator, jobs JobRepository, agents AgentRepository, events EventRepository) *Service {
	return &Service{artifacts: artifacts, reports: reports, generator: generator, jobs: jobs, agents: agents, events: events}
}

func (service *Service) Validate(input []byte) (domain.Infrastructure, error) {
	return config.Parse(input)
}

func (service *Service) Plan(input []byte) (domain.Plan, error) {
	infrastructure, err := service.Validate(input)
	if err != nil {
		return domain.Plan{}, err
	}
	return planner.Build(infrastructure), nil
}

func (service *Service) Generate(input []byte, outputDirectory string) ([]protocol.Artifact, error) {
	infrastructure, err := service.Validate(input)
	if err != nil {
		return nil, err
	}
	if service.generator == nil {
		return nil, fmt.Errorf("artifact generator is not configured")
	}
	return service.generator.Generate(infrastructure, outputDirectory)
}

func (service *Service) CreateJob(input []byte) (domain.Job, error) {
	if service.jobs == nil {
		return domain.Job{}, fmt.Errorf("job repository is not configured")
	}
	infrastructure, err := service.Validate(input)
	if err != nil {
		return domain.Job{}, fmt.Errorf("%w: %v", ErrInvalidJobInput, err)
	}
	plan := planner.Build(infrastructure)
	id, err := newJobID()
	if err != nil {
		return domain.Job{}, fmt.Errorf("create job id: %w", err)
	}
	now := time.Now().UTC()
	status := domain.JobStatusPlanned
	if plan.Status == "blocked" {
		status = domain.JobStatusBlocked
	}
	job := domain.Job{
		ID: id, CreatedAt: now, UpdatedAt: now,
		InputHash: protocol.SHA256(input), Status: status, Plan: plan,
	}
	if err := service.jobs.Create(job); err != nil {
		return domain.Job{}, fmt.Errorf("persist job: %w", err)
	}
	if err := service.recordEvent("job.created", "", "", job.ID, map[string]any{"input_hash": job.InputHash, "status": job.Status}); err != nil {
		return domain.Job{}, fmt.Errorf("record job event: %w", err)
	}
	return job, nil
}

func (service *Service) Jobs() ([]domain.Job, error) {
	if service.jobs == nil {
		return nil, fmt.Errorf("job repository is not configured")
	}
	return service.jobs.List()
}

func (service *Service) Job(id string) (domain.Job, error) {
	if service.jobs == nil {
		return domain.Job{}, fmt.Errorf("job repository is not configured")
	}
	if !protocol.ValidSiteName(id) || len(id) > 128 {
		return domain.Job{}, ErrJobNotFound
	}
	job, err := service.jobs.Get(id)
	if errors.Is(err, os.ErrNotExist) {
		return domain.Job{}, ErrJobNotFound
	}
	return job, err
}

func (service *Service) CancelJob(id string) (domain.Job, error) {
	job, err := service.Job(id)
	if err != nil {
		return domain.Job{}, err
	}
	if job.Status == domain.JobStatusCancelled {
		return job, nil
	}
	if job.Status != domain.JobStatusPlanned && job.Status != domain.JobStatusBlocked {
		return domain.Job{}, ErrJobConflict
	}
	job.Status = domain.JobStatusCancelled
	job.UpdatedAt = time.Now().UTC()
	if err := service.jobs.Update(job); err != nil {
		return domain.Job{}, fmt.Errorf("update job: %w", err)
	}
	if err := service.recordEvent("job.cancelled", "", "", job.ID, map[string]any{"status": job.Status}); err != nil {
		return domain.Job{}, fmt.Errorf("record job event: %w", err)
	}
	return job, nil
}

func (service *Service) RetryJob(id string) (domain.Job, error) {
	job, err := service.Job(id)
	if err != nil {
		return domain.Job{}, err
	}
	if job.Status != domain.JobStatusCancelled && job.Status != domain.JobStatusFailed && job.Status != domain.JobStatusBlocked {
		return domain.Job{}, ErrJobConflict
	}
	job.Status = domain.JobStatusPlanned
	if job.Plan.Status == "blocked" {
		job.Status = domain.JobStatusBlocked
	}
	job.UpdatedAt = time.Now().UTC()
	if err := service.jobs.Update(job); err != nil {
		return domain.Job{}, fmt.Errorf("update job: %w", err)
	}
	if err := service.recordEvent("job.retried", "", "", job.ID, map[string]any{"status": job.Status}); err != nil {
		return domain.Job{}, fmt.Errorf("record job event: %w", err)
	}
	return job, nil
}

func (service *Service) Catalog() ([]protocol.Artifact, error) {
	return service.artifacts.Catalog()
}

func (service *Service) OpenArtifact(path string) (io.ReadCloser, protocol.Artifact, error) {
	return service.artifacts.Open(path)
}

func (service *Service) SubmitReport(report protocol.AgentReport) (bool, error) {
	if !protocol.ValidSiteName(report.AgentID) || len(report.AgentID) > 128 || report.ReportedAt.IsZero() || report.ReportID != protocol.ComputeReportID(report) || len(report.Artifacts) > 10000 {
		return false, fmt.Errorf("%w: invalid metadata", ErrInvalidAgentReport)
	}
	catalog, err := service.artifacts.Catalog()
	if err != nil {
		return false, fmt.Errorf("load published artifact catalog: %w", err)
	}
	published := make(map[string]string, len(catalog))
	for _, artifact := range catalog {
		published[artifact.Path] = artifact.OutputHash
	}
	seen := make(map[string]struct{}, len(report.Artifacts))
	for _, result := range report.Artifacts {
		if result.Status != protocol.StatusCompleted && result.Status != protocol.StatusFailed && result.Status != protocol.StatusBlocked {
			return false, fmt.Errorf("%w: invalid artifact status", ErrInvalidAgentReport)
		}
		if len(result.Message) > 1024 || strings.TrimSpace(result.Path) == "" {
			return false, fmt.Errorf("%w: invalid artifact result", ErrInvalidAgentReport)
		}
		if expectedHash, exists := published[result.Path]; !exists || result.OutputHash != expectedHash {
			return false, fmt.Errorf("%w: artifact result does not match published state", ErrInvalidAgentReport)
		}
		if _, exists := seen[result.Path]; exists {
			return false, fmt.Errorf("%w: duplicate artifact result", ErrInvalidAgentReport)
		}
		seen[result.Path] = struct{}{}
	}
	added, err := service.reports.Append(report)
	if err != nil {
		return false, err
	}
	if err := service.recordEvent("agent.report.received", "", report.AgentID, "", map[string]any{"report_id": report.ReportID, "duplicate": !added}); err != nil {
		return false, fmt.Errorf("record report event: %w", err)
	}
	return added, nil
}

func (service *Service) Reports() []protocol.AgentReport {
	return service.reports.List()
}

func (service *Service) RegisterAgent(input AgentRegistration) (domain.Agent, error) {
	if service.agents == nil {
		return domain.Agent{}, fmt.Errorf("agent repository is not configured")
	}
	if err := validateAgentRegistration(input); err != nil {
		return domain.Agent{}, fmt.Errorf("%w: %v", ErrInvalidAgent, err)
	}
	now := time.Now().UTC()
	agent := domain.Agent{
		ID: input.ID, SiteID: input.SiteID, Version: input.Version,
		Capabilities: append([]string(nil), input.Capabilities...),
		Status:       domain.AgentStatusOnline, RegisteredAt: now, LastSeenAt: now, UpdatedAt: now,
	}
	existing, err := service.agents.Get(input.ID)
	if err == nil {
		if existing.SiteID != input.SiteID {
			return domain.Agent{}, ErrAgentConflict
		}
		agent.RegisteredAt = existing.RegisteredAt
		if err := service.agents.Update(agent); err != nil {
			return domain.Agent{}, fmt.Errorf("update agent: %w", err)
		}
		if err := service.recordEvent("agent.registered", agent.SiteID, agent.ID, "", map[string]any{"version": agent.Version, "capabilities": agent.Capabilities}); err != nil {
			return domain.Agent{}, fmt.Errorf("record agent event: %w", err)
		}
		return agent, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return domain.Agent{}, fmt.Errorf("inspect agent: %w", err)
	}
	created, err := service.agents.Register(agent)
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
		if err := service.agents.Update(agent); err != nil {
			return domain.Agent{}, fmt.Errorf("update concurrently registered agent: %w", err)
		}
	}
	if err := service.recordEvent("agent.registered", agent.SiteID, agent.ID, "", map[string]any{"version": agent.Version, "capabilities": agent.Capabilities}); err != nil {
		return domain.Agent{}, fmt.Errorf("record agent event: %w", err)
	}
	return agent, nil
}

func (service *Service) Agents() ([]domain.Agent, error) {
	if service.agents == nil {
		return nil, fmt.Errorf("agent repository is not configured")
	}
	return service.agents.List()
}

func (service *Service) Agent(id string) (domain.Agent, error) {
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
	agent, err := service.Agent(id)
	if err != nil {
		return domain.Agent{}, err
	}
	if input.SiteID != agent.SiteID {
		return domain.Agent{}, ErrAgentConflict
	}
	if err := validateAgentHeartbeat(input); err != nil {
		return domain.Agent{}, fmt.Errorf("%w: %v", ErrInvalidAgent, err)
	}
	agent.Version = input.Version
	agent.Capabilities = append([]string(nil), input.Capabilities...)
	agent.QueueDepth = input.QueueDepth
	agent.Status = domain.AgentStatusOnline
	agent.LastSeenAt = time.Now().UTC()
	agent.UpdatedAt = agent.LastSeenAt
	if err := service.agents.Update(agent); err != nil {
		return domain.Agent{}, fmt.Errorf("update agent heartbeat: %w", err)
	}
	if err := service.recordEvent("agent.heartbeat", agent.SiteID, agent.ID, "", map[string]any{"queue_depth": agent.QueueDepth, "version": agent.Version}); err != nil {
		return domain.Agent{}, fmt.Errorf("record heartbeat event: %w", err)
	}
	return agent, nil
}

func (service *Service) Events() []domain.Event {
	if service.events == nil {
		return nil
	}
	return service.events.List()
}

func (service *Service) recordEvent(eventType, siteID, agentID, jobID string, payload any) error {
	if service.events == nil {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	eventID, err := newEventID()
	if err != nil {
		return err
	}
	_, _, err = service.events.Append(domain.Event{
		EventID: eventID, SiteID: siteID, AgentID: agentID, JobID: jobID,
		Timestamp: time.Now().UTC(), Type: eventType, Payload: data,
	})
	return err
}

func validateAgentRegistration(input AgentRegistration) error {
	if !protocol.ValidSiteName(input.ID) || len(input.ID) > 128 {
		return fmt.Errorf("agent id is invalid")
	}
	if !protocol.ValidSiteName(input.SiteID) || len(input.SiteID) > 128 {
		return fmt.Errorf("site id is invalid")
	}
	return validateAgentDetails(input.Version, input.Capabilities, 0)
}

func validateAgentHeartbeat(input AgentHeartbeat) error {
	if !protocol.ValidSiteName(input.SiteID) || len(input.SiteID) > 128 {
		return fmt.Errorf("site id is invalid")
	}
	return validateAgentDetails(input.Version, input.Capabilities, input.QueueDepth)
}

func validateAgentDetails(version string, capabilities []string, queueDepth int) error {
	if len(version) > 128 {
		return fmt.Errorf("agent version is too long")
	}
	if queueDepth < 0 || queueDepth > 100000 {
		return fmt.Errorf("queue depth is out of range")
	}
	if len(capabilities) > 64 {
		return fmt.Errorf("too many capabilities")
	}
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if strings.TrimSpace(capability) == "" || len(capability) > 128 || strings.ContainsAny(capability, "\r\n") {
			return fmt.Errorf("invalid capability")
		}
		if _, exists := seen[capability]; exists {
			return fmt.Errorf("duplicate capability")
		}
		seen[capability] = struct{}{}
	}
	return nil
}

func newJobID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "job-" + hex.EncodeToString(raw[:]), nil
}

func newEventID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "evt-" + hex.EncodeToString(raw[:]), nil
}
