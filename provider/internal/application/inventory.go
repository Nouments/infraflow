package application

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"infraflow/internal/diagnostics"
	"infraflow/internal/domain"
	"infraflow/pkg/observability"
	"infraflow/pkg/protocol"
)

const maxInventoryReadBytes = 1 << 20

var ErrSiteNotFound = errors.New("site not found")

// ArtifactEntry is one published, hash-addressed catalog entry.
type ArtifactEntry struct {
	Type       string `json:"type"`
	Path       string `json:"path"`
	OutputHash string `json:"output_hash"`
	InputHash  string `json:"input_hash,omitempty"`
}

// DeviceSummary is a device declared in the published site inventory. It is
// desired state; it says nothing about whether the device was configured.
type DeviceSummary struct {
	Name               string `json:"name"`
	Role               string `json:"role,omitempty"`
	Vendor             string `json:"vendor,omitempty"`
	Family             string `json:"family,omitempty"`
	Model              string `json:"model,omitempty"`
	OSVersion          string `json:"os_version,omitempty"`
	ManagementIPv4     string `json:"management_ipv4,omitempty"`
	ProvisioningMethod string `json:"provisioning_method,omitempty"`
	Compatibility      string `json:"compatibility,omitempty"`
	CompatibilityNote  string `json:"compatibility_evidence,omitempty"`
}

// ReportSummary condenses the most recent Agent execution report for a site.
type ReportSummary struct {
	ReportID   string    `json:"report_id"`
	AgentID    string    `json:"agent_id"`
	ReportedAt time.Time `json:"reported_at"`
	Completed  int       `json:"completed"`
	Failed     int       `json:"failed"`
	Blocked    int       `json:"blocked"`
	Total      int       `json:"total"`
	// Expected is the number of artifacts the site publishes now. Missing
	// counts published artifacts the report does not mention; Stale counts
	// results for a hash that is no longer published (the site was
	// regenerated after the report).
	Expected int `json:"expected"`
	Missing  int `json:"missing"`
	Stale    int `json:"stale"`
	// Outcome is derived only from the report and the current catalog. An
	// absent or partial report is never shown as a success.
	Outcome       string `json:"outcome"`
	OutcomeReason string `json:"outcome_reason"`
}

// Report outcomes, from the strongest evidence of failure down.
const (
	ReportOutcomeNoReport   = "NO_REPORT"
	ReportOutcomeFailed     = "FAILED"
	ReportOutcomeStale      = "STALE"
	ReportOutcomeIncomplete = "INCOMPLETE"
	ReportOutcomeComplete   = "COMPLETE"
)

// SiteSummary joins the published catalog, the site manifest, the declared
// inventory, the observed Agents, and the last persisted report of one site.
type SiteSummary struct {
	ID               string                          `json:"id"`
	Name             string                          `json:"name,omitempty"`
	ManifestStatus   string                          `json:"manifest_status,omitempty"`
	GeneratorVersion string                          `json:"generator_version,omitempty"`
	InputHash        string                          `json:"input_hash,omitempty"`
	ArtifactCount    int                             `json:"artifact_count"`
	Artifacts        []ArtifactEntry                 `json:"artifacts"`
	Devices          []DeviceSummary                 `json:"devices"`
	Capabilities     []protocol.GenerationCapability `json:"capabilities"`
	Agents           []domain.Agent                  `json:"agents"`
	LastReport       *ReportSummary                  `json:"last_report,omitempty"`
	ReportOutcome    string                          `json:"report_outcome"`
	Problems         []string                        `json:"problems,omitempty"`
}

// Sites returns every site present in the published catalog or bound to a
// registered Agent. Unreadable site files are reported as problems instead of
// being hidden.
func (service *Service) Sites() ([]SiteSummary, error) {
	sites := map[string]*SiteSummary{}
	get := func(id string) *SiteSummary {
		if site, ok := sites[id]; ok {
			return site
		}
		site := &SiteSummary{ID: id, Artifacts: []ArtifactEntry{}, Devices: []DeviceSummary{}, Capabilities: []protocol.GenerationCapability{}, Agents: []domain.Agent{}}
		sites[id] = site
		return site
	}
	if service.artifacts != nil {
		catalog, err := service.artifacts.Catalog()
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read artifact catalog: %w", err)
		}
		for _, artifact := range catalog {
			siteID, _, found := strings.Cut(artifact.Path, "/")
			if !found || !protocol.ValidSiteName(siteID) {
				continue
			}
			site := get(siteID)
			site.Artifacts = append(site.Artifacts, ArtifactEntry{Type: artifact.Type, Path: artifact.Path, OutputHash: artifact.OutputHash, InputHash: artifact.InputHash})
		}
	}
	if service.agents != nil {
		agents, err := service.Agents()
		if err != nil {
			return nil, err
		}
		for _, agent := range agents {
			site := get(agent.SiteID)
			site.Agents = append(site.Agents, agent)
		}
	}
	reports := []protocol.AgentReport(nil)
	if service.reports != nil {
		reports = service.reports.List()
	}
	result := make([]SiteSummary, 0, len(sites))
	for _, site := range sites {
		site.ArtifactCount = len(site.Artifacts)
		sort.Slice(site.Artifacts, func(i, j int) bool { return site.Artifacts[i].Path < site.Artifacts[j].Path })
		service.loadSiteManifest(site)
		service.loadSiteInventory(site)
		site.LastReport = lastSiteReport(reports, site)
		site.ReportOutcome = ReportOutcomeNoReport
		if site.LastReport != nil {
			site.ReportOutcome = site.LastReport.Outcome
		}
		result = append(result, *site)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// Site returns one site summary.
func (service *Service) Site(id string) (SiteSummary, error) {
	sites, err := service.Sites()
	if err != nil {
		return SiteSummary{}, err
	}
	for _, site := range sites {
		if site.ID == id {
			return site, nil
		}
	}
	return SiteSummary{}, ErrSiteNotFound
}

func (service *Service) readSiteJSON(site *SiteSummary, artifactType string, target any) bool {
	for _, artifact := range site.Artifacts {
		if artifact.Type != artifactType {
			continue
		}
		reader, _, err := service.artifacts.Open(artifact.Path)
		if err != nil {
			site.Problems = append(site.Problems, fmt.Sprintf("%s could not be opened: %v", artifact.Path, err))
			return false
		}
		data, err := io.ReadAll(io.LimitReader(reader, maxInventoryReadBytes+1))
		_ = reader.Close()
		if err != nil || len(data) > maxInventoryReadBytes {
			site.Problems = append(site.Problems, artifact.Path+" could not be read within the size limit")
			return false
		}
		if err := json.Unmarshal(data, target); err != nil {
			site.Problems = append(site.Problems, fmt.Sprintf("%s is not valid JSON: %v", artifact.Path, err))
			return false
		}
		return true
	}
	return false
}

func (service *Service) loadSiteManifest(site *SiteSummary) {
	var manifest protocol.SiteManifest
	if !service.readSiteJSON(site, "site_manifest", &manifest) {
		return
	}
	site.Name = manifest.SiteName
	site.ManifestStatus = manifest.Status
	site.GeneratorVersion = manifest.GeneratorVersion
	site.InputHash = manifest.InputHash
	if manifest.Capabilities != nil {
		site.Capabilities = manifest.Capabilities
	}
}

func (service *Service) loadSiteInventory(site *SiteSummary) {
	var inventory struct {
		Devices []domain.Device `json:"devices"`
	}
	if !service.readSiteJSON(site, "inventory", &inventory) {
		return
	}
	compatibility := map[string]protocol.GenerationCapability{}
	for _, capability := range site.Capabilities {
		if device, ok := strings.CutPrefix(capability.Feature, "device_compatibility:"); ok {
			compatibility[device] = capability
		}
	}
	for _, device := range inventory.Devices {
		summary := DeviceSummary{
			Name: device.Name, Role: device.Role, Vendor: device.Vendor, Family: device.Family,
			Model: device.Model, OSVersion: device.OSVersion, ManagementIPv4: device.Management.IPv4,
			ProvisioningMethod: string(device.Provisioning.Method),
		}
		if capability, ok := compatibility[device.Name]; ok {
			summary.Compatibility, summary.CompatibilityNote = capability.Status, capability.Evidence
		}
		site.Devices = append(site.Devices, summary)
	}
}

func lastSiteReport(reports []protocol.AgentReport, site *SiteSummary) *ReportSummary {
	agentIDs := map[string]bool{}
	for _, agent := range site.Agents {
		agentIDs[agent.ID] = true
	}
	var latest *protocol.AgentReport
	for index := range reports {
		report := &reports[index]
		matches := agentIDs[report.AgentID]
		if !matches {
			for _, artifact := range report.Artifacts {
				if strings.HasPrefix(artifact.Path, site.ID+"/") {
					matches = true
					break
				}
			}
		}
		if matches && (latest == nil || report.ReportedAt.After(latest.ReportedAt)) {
			latest = report
		}
	}
	if latest == nil {
		return nil
	}
	summary := &ReportSummary{ReportID: latest.ReportID, AgentID: latest.AgentID, ReportedAt: latest.ReportedAt, Total: len(latest.Artifacts)}
	for _, artifact := range latest.Artifacts {
		switch artifact.Status {
		case protocol.StatusCompleted:
			summary.Completed++
		case protocol.StatusFailed:
			summary.Failed++
		case protocol.StatusBlocked:
			summary.Blocked++
		}
	}
	classifyReport(summary, latest, site.Artifacts)
	return summary
}

// classifyReport compares a report with the site's current catalog. Only a
// COMPLETED result for the exact published hash counts towards COMPLETE.
func classifyReport(summary *ReportSummary, report *protocol.AgentReport, published []ArtifactEntry) {
	results := make(map[string]protocol.ArtifactResult, len(report.Artifacts))
	for _, result := range report.Artifacts {
		results[result.Path] = result
	}
	summary.Expected = len(published)
	currentFailed := 0
	for _, artifact := range published {
		result, ok := results[artifact.Path]
		switch {
		case !ok:
			summary.Missing++
		case result.OutputHash != artifact.OutputHash:
			summary.Stale++
		case result.Status != protocol.StatusCompleted:
			currentFailed++
		}
	}
	switch {
	case currentFailed > 0:
		summary.Outcome = ReportOutcomeFailed
		summary.OutcomeReason = fmt.Sprintf("%d published artifact(s) reported failed or blocked", currentFailed)
	case summary.Stale > 0:
		summary.Outcome = ReportOutcomeStale
		summary.OutcomeReason = fmt.Sprintf("%d result(s) refer to a hash that is no longer published; the site changed after this report", summary.Stale)
	case summary.Missing > 0:
		summary.Outcome = ReportOutcomeIncomplete
		summary.OutcomeReason = fmt.Sprintf("report covers %d of %d published artifact(s); the outcome of the others is unknown", summary.Expected-summary.Missing, summary.Expected)
	case summary.Expected == 0 && len(report.Artifacts) > 0:
		summary.Outcome = ReportOutcomeStale
		summary.OutcomeReason = "the site publishes no artifact now; the report refers to an earlier catalog"
	default:
		summary.Outcome = ReportOutcomeComplete
		summary.OutcomeReason = "every published artifact was reported completed with its published SHA-256"
	}
}

// DiagnosticRequest selects the execution, Agent, site, or device to explain.
type DiagnosticRequest struct {
	RunID            string
	AgentID          string
	SiteID           string
	DeviceID         string
	ExpectedHostname string
	ExpectedIPv4     string
}

// Diagnose correlates the central technical log store (Provider and ingested
// Agent events) with the Provider-observed Agent status. It reads only; it
// never contacts devices and never runs remediation.
func (service *Service) Diagnose(request DiagnosticRequest) (diagnostics.Report, error) {
	if request.RunID == "" && request.AgentID == "" && request.SiteID == "" && request.DeviceID == "" {
		return diagnostics.Report{}, fmt.Errorf("%w: one of run_id, agent_id, site_id, or device_id is required", ErrInvalidAgent)
	}
	for _, value := range []string{request.RunID, request.AgentID, request.SiteID, request.DeviceID} {
		if len(value) > 128 || strings.ContainsAny(value, "\r\n\x00") {
			return diagnostics.Report{}, fmt.Errorf("%w: diagnostic selector is invalid", ErrInvalidAgent)
		}
	}
	var agentContext *diagnostics.AgentContext
	if request.AgentID == "" && request.SiteID != "" && service.agents != nil {
		if agents, err := service.Agents(); err == nil {
			for _, agent := range agents {
				if agent.SiteID == request.SiteID {
					request.AgentID = agent.ID
					break
				}
			}
		}
	}
	if request.AgentID != "" && service.agents != nil {
		agent, err := service.Agent(request.AgentID)
		if err != nil && !errors.Is(err, ErrAgentNotFound) {
			return diagnostics.Report{}, err
		}
		if err == nil {
			agentContext = &diagnostics.AgentContext{AgentID: agent.ID, SiteID: agent.SiteID, Status: agent.Status, Reason: agent.StatusReason, ReportedProblem: agent.ReportedProblem}
			if agent.LastHeartbeatAt != nil {
				agentContext.LastHeartbeatAt = agent.LastHeartbeatAt.UTC().Format(time.RFC3339Nano)
			}
			if request.SiteID == "" {
				request.SiteID = agent.SiteID
			}
		}
	}
	events := []diagnostics.Event{}
	if service.logs != nil {
		// Collect each correlated slice separately; the store filters with
		// AND semantics, while a diagnosis needs Agent, run, and device
		// evidence together (for example Provider gRPC events carry only the
		// run ID).
		queries := []observability.Query{}
		if request.RunID != "" {
			queries = append(queries, observability.Query{RunID: request.RunID})
		}
		if request.AgentID != "" {
			queries = append(queries, observability.Query{AgentID: request.AgentID})
		}
		if request.SiteID != "" && request.AgentID == "" {
			queries = append(queries, observability.Query{SiteID: request.SiteID})
		}
		if request.DeviceID != "" {
			queries = append(queries, observability.Query{DeviceID: request.DeviceID}, observability.Query{Text: request.DeviceID})
		}
		seen := map[string]bool{}
		for _, query := range queries {
			query.Limit = observability.MaxQueryLimit
			page, err := service.logs.List(query)
			if err != nil {
				return diagnostics.Report{}, fmt.Errorf("read technical logs: %w", err)
			}
			for _, event := range page.Events {
				if seen[event.ID] || event.Event == "provider.http.request" {
					continue
				}
				seen[event.ID] = true
				events = append(events, diagnostics.FromObservability(event))
			}
		}
	}
	return diagnostics.AnalyzeWithAgent(events, diagnostics.Filter{
		RunID: request.RunID, DeviceID: request.DeviceID, AgentID: request.AgentID,
		ExpectedHostname: request.ExpectedHostname, ExpectedIPv4: request.ExpectedIPv4,
	}, agentContext), nil
}
