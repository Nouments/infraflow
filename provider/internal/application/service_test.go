package application

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	configadapter "infraflow/internal/adapters/config"
	planningadapter "infraflow/internal/adapters/planning"
	"infraflow/internal/domain"
	"infraflow/pkg/protocol"
	"infraflow/provider/internal/adapters/generation"
)

type memoryArtifacts struct {
	items []protocol.Artifact
}

func (repository memoryArtifacts) Catalog() ([]protocol.Artifact, error) {
	return repository.items, nil
}

func (repository memoryArtifacts) Open(path string) (io.ReadCloser, protocol.Artifact, error) {
	for _, artifact := range repository.items {
		if artifact.Path == path {
			return io.NopCloser(bytes.NewReader(nil)), artifact, nil
		}
	}
	return nil, protocol.Artifact{}, nil
}

type memoryReports struct {
	items []protocol.AgentReport
}

type memoryJobs struct {
	items []domain.Job
}

type memoryAgents struct {
	items map[string]domain.Agent
}

func (repository *memoryAgents) Register(agent domain.Agent) (bool, error) {
	if repository.items == nil {
		repository.items = make(map[string]domain.Agent)
	}
	if _, exists := repository.items[agent.ID]; exists {
		return false, nil
	}
	repository.items[agent.ID] = agent
	return true, nil
}

func (repository *memoryAgents) Get(id string) (domain.Agent, error) {
	agent, exists := repository.items[id]
	if !exists {
		return domain.Agent{}, os.ErrNotExist
	}
	return agent, nil
}

func (repository *memoryAgents) List() ([]domain.Agent, error) {
	agents := make([]domain.Agent, 0, len(repository.items))
	for _, agent := range repository.items {
		agents = append(agents, agent)
	}
	return agents, nil
}

func (repository *memoryAgents) Update(agent domain.Agent) error {
	if _, exists := repository.items[agent.ID]; !exists {
		return os.ErrNotExist
	}
	repository.items[agent.ID] = agent
	return nil
}

func (repository *memoryJobs) Create(job domain.Job) error {
	repository.items = append(repository.items, job)
	return nil
}

func (repository *memoryJobs) Get(id string) (domain.Job, error) {
	for _, job := range repository.items {
		if job.ID == id {
			return job, nil
		}
	}
	return domain.Job{}, os.ErrNotExist
}

func (repository *memoryJobs) List() ([]domain.Job, error) {
	return append([]domain.Job(nil), repository.items...), nil
}

func (repository *memoryJobs) Update(updated domain.Job) error {
	for index, job := range repository.items {
		if job.ID == updated.ID {
			repository.items[index] = updated
			return nil
		}
	}
	return os.ErrNotExist
}

type fakeGenerator struct{}

func (fakeGenerator) Generate(domain.Infrastructure, string) ([]protocol.Artifact, error) {
	return []protocol.Artifact{{Type: "inventory"}, {Type: "topology"}}, nil
}

type failingVendorGenerator struct{}

func (failingVendorGenerator) Generate(domain.Infrastructure, string) ([]protocol.Artifact, error) {
	return nil, errors.New("test generator failure")
}

func (failingVendorGenerator) GenerateVendorAnsible(domain.Infrastructure, string) ([]protocol.Artifact, error) {
	return nil, errors.New("test generator failure")
}

func (repository *memoryReports) Append(report protocol.AgentReport) (bool, error) {
	for _, current := range repository.items {
		if current.ReportID == report.ReportID {
			return false, nil
		}
	}
	repository.items = append(repository.items, report)
	return true, nil
}

func (repository *memoryReports) List() []protocol.AgentReport {
	return repository.items
}

func TestServiceValidatesPlansAndGenerates(t *testing.T) {
	service := NewService(memoryArtifacts{}, &memoryReports{}, fakeGenerator{}, testDependencies())
	input := []byte("sites:\n  - name: lab\n    devices:\n      - name: R1\n        vendor: cisco\n        model: ios-xe\n")
	if _, err := service.Validate(input); err != nil {
		t.Fatal(err)
	}
	plan, err := service.Plan(input)
	if err != nil || plan.Status != "PLANNED" {
		t.Fatalf("expected planned execution plan: %#v, %v", plan, err)
	}
	artifacts, err := service.Generate(input, "artifact-output")
	if err != nil || len(artifacts) != 2 {
		t.Fatalf("expected generic inventory/topology artifacts: %#v, %v", artifacts, err)
	}
}

func TestPlanAndGenerateReturnsVerifiedArtifactProvenance(t *testing.T) {
	outputDirectory := t.TempDir()
	dependencies := testDependencies()
	dependencies.ArtifactDirectory = outputDirectory
	service := NewService(nil, nil, generation.Generator{}, dependencies)
	input := []byte(`sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        model: csr1000v
        provisioning:
          method: netconf
        network:
          interfaces:
            - name: GigabitEthernet1
              role: lan
              ipv4_mode: static
              ipv4_address: 192.0.2.1/24
`)

	result, err := service.PlanAndGenerate(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan.Status != "PLANNED" || !result.Plan.Lifecycle.Generated || result.GenerationStatus != "GENERATED" {
		t.Fatalf("unexpected plan/generation state: %#v", result)
	}
	if result.ExecutionStatus != "NOT_REQUESTED" || result.VerificationStatus != "NOT_PERFORMED" {
		t.Fatalf("generation implied execution or verification: %#v", result)
	}
	if len(result.Tasks) != 1 || result.Tasks[0].Status != "GENERATED" || result.Tasks[0].Capability != domain.CapabilityUnverified || result.Tasks[0].Method != "netconf" {
		t.Fatalf("unexpected per-task state: %#v", result.Tasks)
	}
	if len(result.Tasks[0].Artifacts) != 4 {
		t.Fatalf("expected four vendor artifacts linked to the task: %#v", result.Tasks[0].Artifacts)
	}
	for _, artifact := range result.Tasks[0].Artifacts {
		if artifact.Site != "lab" || artifact.Device != "R1" || artifact.TaskID != result.Plan.Tasks[0].ID || artifact.TemplateVersion != "1" || artifact.Status != "GENERATED" {
			t.Fatalf("artifact provenance is incomplete: %#v", artifact)
		}
		if !protocol.IsSHA256(artifact.SHA256) || artifact.Format == "" {
			t.Fatalf("artifact hash or format is missing: %#v", artifact)
		}
		data, err := os.ReadFile(filepath.Join(outputDirectory, filepath.FromSlash(artifact.Path)))
		if err != nil || protocol.SHA256(data) != artifact.SHA256 {
			t.Fatalf("artifact was not present with the reported hash: %#v, %v", artifact, err)
		}
	}

	secondDirectory := t.TempDir()
	secondDependencies := testDependencies()
	secondDependencies.ArtifactDirectory = secondDirectory
	second, err := NewService(nil, nil, generation.Generator{}, secondDependencies).PlanAndGenerate(input)
	if err != nil {
		t.Fatal(err)
	}
	for index := range result.Tasks[0].Artifacts {
		if result.Tasks[0].Artifacts[index].SHA256 != second.Tasks[0].Artifacts[index].SHA256 {
			t.Fatalf("identical generated content had unstable hash: %#v != %#v", result.Tasks[0].Artifacts[index], second.Tasks[0].Artifacts[index])
		}
	}
}

func TestPlanAndGenerateReportsUnsupportedAndFailuresHonestly(t *testing.T) {
	outputDirectory := t.TempDir()
	dependencies := testDependencies()
	dependencies.ArtifactDirectory = outputDirectory
	inputWithUnsupportedMethod := []byte(`sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        provisioning:
          method: made-up
        network:
          interfaces:
            - name: Gi1
              role: lan
              ipv4_mode: dhcp
`)
	unsupported, err := NewService(nil, nil, generation.Generator{}, dependencies).PlanAndGenerate(inputWithUnsupportedMethod)
	if err != nil {
		t.Fatal(err)
	}
	if unsupported.GenerationStatus != "UNSUPPORTED" || unsupported.Plan.Lifecycle.Generated || len(unsupported.Tasks) != 1 || unsupported.Tasks[0].Status != "UNSUPPORTED" {
		t.Fatalf("unsupported generator path was misreported: %#v", unsupported)
	}

	input := []byte(`sites:
  - name: lab
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        network:
          interfaces:
            - name: Gi1
              role: lan
              ipv4_mode: dhcp
`)
	withoutGenerator, err := NewService(nil, nil, fakeGenerator{}, dependencies).PlanAndGenerate(input)
	if err == nil || !strings.Contains(err.Error(), "vendor Ansible generator is not configured") {
		t.Fatalf("missing vendor generator did not return an explicit error: %v", err)
	}
	if withoutGenerator.Plan.Lifecycle.Generated {
		t.Fatalf("missing generator marked plan generated: %#v", withoutGenerator)
	}

	invalid, err := NewService(nil, nil, generation.Generator{}, dependencies).PlanAndGenerate([]byte("sites:\n  - name: lab\n    unexpected: true\n"))
	if err == nil || invalid.Plan.ID != "" {
		t.Fatalf("invalid configuration was accepted: %#v, %v", invalid, err)
	}

	failed, err := NewService(nil, nil, failingVendorGenerator{}, dependencies).PlanAndGenerate(input)
	if err == nil || failed.GenerationStatus != "FAILED" || failed.Plan.Lifecycle.Generated || failed.Tasks[0].Status != "FAILED" {
		t.Fatalf("generator failure was not represented honestly: %#v, %v", failed, err)
	}
}

func TestServiceSubmitsOnlyCurrentArtifactResults(t *testing.T) {
	artifact := protocol.Artifact{Type: "inventory", Path: "lab/inventory.json", InputHash: "input", OutputHash: "output"}
	repository := &memoryReports{}
	service := NewService(memoryArtifacts{items: []protocol.Artifact{artifact}}, repository, fakeGenerator{}, testDependencies())
	report := protocol.AgentReport{
		AgentID: "agent-01", ReportedAt: time.Now().UTC(),
		Artifacts: []protocol.ArtifactResult{{Path: artifact.Path, OutputHash: artifact.OutputHash, Status: protocol.StatusCompleted}},
	}
	report.ReportID = protocol.ComputeReportID(report)
	added, err := service.SubmitReport(report)
	if err != nil || !added || len(service.Reports()) != 1 {
		t.Fatalf("valid report was not accepted: added=%v err=%v", added, err)
	}
	stale := report
	stale.Artifacts = append([]protocol.ArtifactResult(nil), report.Artifacts...)
	stale.Artifacts[0].OutputHash = "stale"
	stale.ReportID = protocol.ComputeReportID(stale)
	if _, err := service.SubmitReport(stale); err == nil {
		t.Fatal("stale artifact report should be rejected")
	}
}

func TestServiceCreatesAndManagesPlanningJobsWithoutExecutingTasks(t *testing.T) {
	jobs := &memoryJobs{}
	service := NewServiceWithJobs(nil, nil, nil, jobs, testDependencies())
	input := []byte("sites:\n  - name: lab\n    devices:\n      - name: R1\n")
	job, err := service.CreateJob(input)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobStatusPlanned || len(job.Plan.Tasks) != 0 || len(jobs.items) != 1 {
		t.Fatalf("unexpected created job: %#v", job)
	}
	cancelled, err := service.CancelJob(job.ID)
	if err != nil || cancelled.Status != domain.JobStatusCancelled {
		t.Fatalf("job was not cancelled: %#v, %v", cancelled, err)
	}
	retried, err := service.RetryJob(job.ID)
	if err != nil || retried.Status != domain.JobStatusPlanned {
		t.Fatalf("planned job retry should remain planned: %#v, %v", retried, err)
	}
	if _, err := service.CreateJob([]byte("sites:\n  - name: lab\n    unknown: true\n")); err == nil {
		t.Fatal("invalid input should not create a job")
	}
}

func TestServiceBindsAgentIdentityAndUpdatesHeartbeats(t *testing.T) {
	agents := &memoryAgents{}
	service := NewServiceWithJobsAndAgents(nil, nil, nil, nil, agents, testDependencies())
	agent, err := service.RegisterAgent(AgentRegistration{ID: "agent-01", SiteID: "site-01", Version: "0.1.0", Capabilities: []string{"inventory"}})
	if err != nil || agent.Status != domain.AgentStatusOnline {
		t.Fatalf("agent was not registered: %#v, %v", agent, err)
	}
	updated, err := service.HeartbeatAgent(agent.ID, AgentHeartbeat{SiteID: agent.SiteID, Version: agent.Version, QueueDepth: 2})
	if err != nil || updated.QueueDepth != 2 {
		t.Fatalf("heartbeat was not persisted: %#v, %v", updated, err)
	}
	if _, err := service.HeartbeatAgent(agent.ID, AgentHeartbeat{SiteID: "other-site"}); !errors.Is(err, ErrAgentConflict) {
		t.Fatalf("expected site identity conflict, got %v", err)
	}
	if _, err := service.RegisterAgent(AgentRegistration{ID: agent.ID, SiteID: "other-site"}); !errors.Is(err, ErrAgentConflict) {
		t.Fatalf("expected registration identity conflict, got %v", err)
	}
}

func TestServiceReportsTemplateSelectionAndCapabilityState(t *testing.T) {
	service := NewService(nil, nil, nil, testDependencies())
	input := []byte(`capability_registry:
  entries:
    cisco:iosxe:ios-xe:
      netconf:
        method: netconf
        state: UNVERIFIED
        evidence: no lab execution or verified adapter was recorded

template_registry:
  entries:
    cisco:iosxe:ios-xe:1:
      id: cisco:iosxe:ios-xe:1
      vendor: cisco
      family: iosxe
      model: ios-xe
      version: "1"
      hash: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      evidence: sandbox validation manifest checked locally

sites:
  - name: sandbox
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        model: ios-xe
        provisioning:
          method: netconf
          template_version: "1"
`)
	results, err := service.TemplateInfo(input)
	if err != nil {
		t.Fatalf("template info failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly one template result, got %d: %#v", len(results), results)
	}
	if results[0].TemplateID != "cisco:iosxe:ios-xe:1" || results[0].CapabilityState != domain.CapabilityUnverified {
		t.Fatalf("unexpected template selection result: %#v", results[0])
	}
	if !results[0].Blocked {
		t.Fatal("expected result to be blocked while capability is unverified")
	}
}

func TestServiceSummarizesCapabilityAndTemplateReadiness(t *testing.T) {
	service := NewService(nil, nil, nil, testDependencies())
	input := []byte(`capability_registry:
  entries:
    cisco:iosxe:ios-xe:
      netconf:
        method: netconf
        state: LAB-VERIFIED
        evidence: lab test 2026-10-07

template_registry:
  entries:
    cisco:iosxe:ios-xe:1:
      id: cisco:iosxe:ios-xe:1
      vendor: cisco
      family: iosxe
      model: ios-xe
      version: "1"
      hash: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      evidence: sandbox validation manifest checked locally

sites:
  - name: sandbox
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        model: ios-xe
        provisioning:
          method: netconf
          template_version: "1"
`)
	summary, err := service.CapabilitySummary(input)
	if err != nil {
		t.Fatalf("capability summary failed: %v", err)
	}
	if len(summary) != 1 {
		t.Fatalf("expected one device summary, got %d: %#v", len(summary), summary)
	}
	if summary[0].CapabilityState != domain.CapabilityLabVerified || summary[0].TemplateID != "cisco:iosxe:ios-xe:1" {
		t.Fatalf("unexpected capability summary: %#v", summary[0])
	}
	if summary[0].Ready != true {
		t.Fatal("expected device to be ready when capability is lab-verified and template known")
	}
}

func TestServiceBuildsEvidenceBasedCapabilityMatrix(t *testing.T) {
	service := NewService(nil, nil, nil, testDependencies())
	input := []byte(`capability_registry:
  entries:
    cisco:iosxe:ios-xe:
      netconf:
        method: netconf
        state: LAB-VERIFIED
        evidence: lab test 2026-10-07
      ssh:
        method: ssh
        state: UNVERIFIED
        evidence: no adapter execution evidence

template_registry:
  entries:
    cisco:iosxe:ios-xe:1:
      id: cisco:iosxe:ios-xe:1
      vendor: cisco
      family: iosxe
      model: ios-xe
      version: "1"
      hash: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      evidence: audited sandbox manifest

sites:
  - name: sandbox
    devices:
      - name: R1
        vendor: cisco
        family: iosxe
        model: ios-xe
        provisioning:
          method: netconf
          template_version: "1"
`)
	matrix, err := service.CapabilityMatrix(input)
	if err != nil {
		t.Fatalf("capability matrix failed: %v", err)
	}
	if len(matrix) != 2 {
		t.Fatalf("expected two capability matrix rows, got %d: %#v", len(matrix), matrix)
	}
	if matrix[0].Method != "netconf" || matrix[0].State != domain.CapabilityLabVerified || !matrix[0].Ready {
		t.Fatalf("unexpected verified matrix row: %#v", matrix[0])
	}
	if matrix[1].Method != "ssh" || matrix[1].State != domain.CapabilityUnverified || matrix[1].Ready {
		t.Fatalf("unexpected unverified matrix row: %#v", matrix[1])
	}
}

func testDependencies() Dependencies {
	return Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}}
}
