// Package diagnostics correlates Provider, Agent, bootstrap-service, and
// device-console evidence into a deterministic, evidence-backed explanation of
// an execution. It never contacts devices and never runs remediation.
package diagnostics

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"infraflow/pkg/observability"
)

const (
	maxLogBytes     = 16 << 20
	maxLogLines     = 10000
	maxLineBytes    = 64 << 10
	maxEvidence     = 200
	confidenceHigh  = "high"
	confidenceMed   = "medium"
	confidenceLow   = "low"
	statusFailed    = "failed"
	statusSuccess   = "success"
	statusIncomplet = "incomplete evidence"
)

// Canonical lifecycle stages, in execution order.
const (
	StageProfile       = "profile selection"
	StageConnectivity  = "Provider connectivity"
	StageAuth          = "authentication"
	StageRegistration  = "agent registration"
	StageHeartbeat     = "agent heartbeat"
	StageSync          = "artifact synchronization"
	StageIntegrity     = "artifact integrity"
	StageDHCP          = "DHCP"
	StageTFTP          = "TFTP"
	StageDeviceConfig  = "device configuration"
	StageReport        = "Provider report"
	StageDevicePostChk = "device post-check"
)

var stageOrder = []string{
	StageProfile, StageConnectivity, StageAuth, StageRegistration, StageHeartbeat, StageSync,
	StageIntegrity, StageDHCP, StageTFTP, StageDeviceConfig, StageReport, StageDevicePostChk,
}

var stageRank = func() map[string]int {
	ranks := make(map[string]int, len(stageOrder))
	for index, stage := range stageOrder {
		ranks[stage] = index + 1
	}
	return ranks
}()

// requiredStages must all be confirmed before a device workflow is reported as
// an end-to-end success.
var requiredStages = []struct{ stage, description string }{
	{StageRegistration, "Agent registration/site authorization events"},
	{StageSync, "Agent catalog synchronization events"},
	{StageIntegrity, "Agent SHA-256 integrity verification events"},
	{StageDHCP, "DHCP Discover/Offer/Request/Ack evidence"},
	{StageTFTP, "TFTP RRQ and completed transfer evidence"},
	{StageDeviceConfig, "device configuration execution evidence"},
	{StageReport, "final Provider execution report"},
	{StageDevicePostChk, "device-console post-configuration checks"},
}

type SourceFile struct {
	Name string
	Path string
}

// Event is one normalized log entry. Raw keeps a redacted copy of the original
// line so that unparsed evidence is never discarded.
type Event struct {
	EventID   string `json:"event_id,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Level     string `json:"level,omitempty"`
	Name      string `json:"event,omitempty"`
	Service   string `json:"service,omitempty"`
	Source    string `json:"source"`
	SiteID    string `json:"site_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	DeviceID  string `json:"device_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	Stage     string `json:"stage,omitempty"`
	Code      string `json:"code,omitempty"`
	Operation string `json:"operation,omitempty"`
	Status    string `json:"status,omitempty"`
	Detail    string `json:"detail,omitempty"`
	ErrorText string `json:"error,omitempty"`
	Message   string `json:"message"`
	Raw       string `json:"raw"`
}

type Filter struct {
	RunID            string
	DeviceID         string
	AgentID          string
	SiteID           string
	ExpectedHostname string
	ExpectedIPv4     string
}

// AgentContext is the Provider-observed connectivity of the Agent concerned.
type AgentContext struct {
	AgentID         string `json:"agent_id"`
	SiteID          string `json:"site_id"`
	Status          string `json:"status"`
	Reason          string `json:"reason,omitempty"`
	LastHeartbeatAt string `json:"last_heartbeat_at,omitempty"`
	ReportedProblem string `json:"reported_problem,omitempty"`
}

type Evidence struct {
	EventID   string `json:"event_id,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Source    string `json:"source"`
	SiteID    string `json:"site_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	DeviceID  string `json:"device_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	Stage     string `json:"stage,omitempty"`
	Code      string `json:"code,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
	Message   string `json:"message"`
}

type TimelineEntry struct {
	Stage     string `json:"stage"`
	State     string `json:"state"`
	Code      string `json:"code,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

type Report struct {
	Summary                      string          `json:"summary"`
	CurrentState                 string          `json:"current_state,omitempty"`
	ObservedStatus               string          `json:"observed_status"`
	LastConfirmedSuccessfulStage string          `json:"last_confirmed_successful_stage,omitempty"`
	FailureStage                 string          `json:"failure_stage,omitempty"`
	FailureCode                  string          `json:"failure_code,omitempty"`
	LikelyCause                  string          `json:"likely_cause"`
	Confidence                   string          `json:"confidence"`
	RecommendedNextAction        string          `json:"recommended_next_action"`
	MissingEvidence              []string        `json:"missing_evidence,omitempty"`
	Timeline                     []TimelineEntry `json:"timeline"`
	Agent                        *AgentContext   `json:"agent,omitempty"`
	EventsConsidered             int             `json:"events_considered"`
	Evidence                     []Evidence      `json:"evidence"`
}

type stageInfo struct {
	name   string
	code   string
	fail   string
	action string
	// weak failures are generic summaries (for example "run failed"); they
	// are reported only when no specific failure explains the outcome.
	weak bool
	// info entries are evidence without a success/failure outcome.
	info bool
	// inferred failures were deduced from missing evidence, not observed.
	inferred bool
	keyword  bool
}

var hostnameLine = regexp.MustCompile(`(?im)^\s*hostname\s+([A-Za-z][A-Za-z0-9_-]{0,34})\s*$`)
var ipv4Token = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)

// FromObservability converts a structured InfraFlow log event.
func FromObservability(structured observability.Event) Event {
	raw, _ := json.Marshal(structured)
	return Event{
		EventID: safe(structured.ID), Timestamp: structured.Timestamp, Level: structured.Level,
		Name: safe(structured.Event), Service: safe(structured.Service), Source: safe(firstNonEmpty(structured.Source, structured.Service)),
		SiteID: safe(structured.SiteID), AgentID: safe(structured.AgentID), DeviceID: safe(structured.DeviceID),
		RunID: safe(structured.RunID), Operation: safe(structured.Operation), Status: safe(structured.Status),
		Detail: safe(structured.Line), ErrorText: safe(structured.Error),
		Message: safe(strings.Join(nonempty(structured.Message, structured.Error, structured.Line), " | ")),
		Raw:     truncate(string(raw), 2000),
	}
}

// ReadFiles reads selected Provider, Agent, bootstrap-service, and device-console
// logs. Structured JSONL events retain their correlation fields; plain console
// lines remain evidence with their source and original text.
func ReadFiles(files []SourceFile) ([]Event, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("at least one log source is required")
	}
	events := make([]Event, 0)
	for _, file := range files {
		if strings.TrimSpace(file.Name) == "" || strings.TrimSpace(file.Path) == "" {
			return nil, fmt.Errorf("log source name and path are required")
		}
		input, err := os.Open(file.Path)
		if err != nil {
			return nil, fmt.Errorf("open %s log: %w", file.Name, err)
		}
		info, err := input.Stat()
		if err != nil {
			_ = input.Close()
			return nil, fmt.Errorf("inspect %s log: %w", file.Name, err)
		}
		if !info.Mode().IsRegular() || info.Size() > maxLogBytes {
			_ = input.Close()
			return nil, fmt.Errorf("%s log must be a regular file no larger than %d bytes", file.Name, maxLogBytes)
		}
		read, readErr := readSource(input, file)
		closeErr := input.Close()
		if readErr != nil || closeErr != nil {
			return nil, fmt.Errorf("read %s log: %w", file.Name, joinErrors(readErr, closeErr))
		}
		events = append(events, read...)
	}
	return events, nil
}

func readSource(input io.Reader, file SourceFile) ([]Event, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), maxLineBytes)
	var events []Event
	for scanner.Scan() {
		if len(events) >= maxLogLines {
			return nil, fmt.Errorf("log exceeds %d lines", maxLogLines)
		}
		line := scanner.Text()
		var structured observability.Event
		if err := json.Unmarshal([]byte(line), &structured); err == nil && structured.Event != "" && structured.Message != "" {
			event := FromObservability(structured)
			event.Raw = safe(line)
			events = append(events, event)
			continue
		}
		raw := safe(line)
		timestamp := ""
		fields := strings.Fields(raw)
		if len(fields) > 0 {
			if _, err := time.Parse(time.RFC3339Nano, fields[0]); err == nil {
				timestamp = fields[0]
			}
		}
		events = append(events, Event{Timestamp: timestamp, Source: safe(file.Name + ":" + filepath.Base(file.Path)), Message: raw, Raw: raw})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

// Analyze correlates events without Provider-side Agent connectivity context.
func Analyze(input []Event, filter Filter) Report {
	return AnalyzeWithAgent(input, filter, nil)
}

type outcome struct {
	success  bool
	info     stageInfo
	event    Event
	position int
}

// AnalyzeWithAgent correlates events and, when supplied, the Provider-observed
// connectivity of the Agent. The result separates observed facts (evidence),
// conclusions (failure stage, cause, confidence), and missing evidence.
func AnalyzeWithAgent(input []Event, filter Filter, agent *AgentContext) Report {
	events := append([]Event(nil), input...)
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Timestamp == "" || events[j].Timestamp == "" {
			return events[i].Timestamp != "" && events[j].Timestamp == ""
		}
		return events[i].Timestamp < events[j].Timestamp
	})
	events = filterEvents(events, filter)
	report := Report{
		ObservedStatus: statusIncomplet, LikelyCause: "The supplied logs do not identify a confirmed failure.",
		Confidence: confidenceLow, RecommendedNextAction: "Collect correlated Provider, Agent, DHCP/TFTP, and device-console evidence for the same run.",
		MissingEvidence: []string{}, Evidence: []Evidence{}, Timeline: []TimelineEntry{}, Agent: agent,
		EventsConsidered: len(events),
	}
	// Latest outcome per (stage, subject): a retry of the same subject
	// supersedes an earlier failure, a failure of another subject does not.
	latest := map[string]outcome{}
	keys := []string{}
	successful := map[string]bool{}
	firstSeen := map[string]string{}
	uncorrelated := false
	var joined strings.Builder
	sawDiscover := false
	for position, event := range events {
		if (filter.RunID != "" && event.RunID == "") || (filter.DeviceID != "" && event.DeviceID == "") {
			uncorrelated = true
		}
		joined.WriteString("\n")
		joined.WriteString(strings.ToLower(event.Message))
		joined.WriteString(" ")
		joined.WriteString(strings.ToLower(event.Raw))
		info, success := classifyStructured(event)
		if info.name == "" {
			info, success = classify(event)
			info.keyword = info.name != ""
		} else if info.info {
			// An informational name (offer, RRQ) may carry a message that
			// states a stronger outcome; prefer the explicit outcome.
			if keyword, keywordSuccess := classify(event); keyword.name == info.name && !keyword.info {
				info, success = keyword, keywordSuccess
				info.keyword = true
			}
		}
		if info.name == "" {
			continue
		}
		if info.code == "DHCP_DISCOVER" {
			sawDiscover = true
		}
		outcomeLabel := "observed"
		if !info.info {
			outcomeLabel = map[bool]string{true: "success", false: "failure"}[success]
		}
		if len(report.Evidence) < maxEvidence {
			report.Evidence = append(report.Evidence, Evidence{
				EventID: event.EventID, Timestamp: event.Timestamp, Source: event.Source, SiteID: event.SiteID, AgentID: event.AgentID,
				DeviceID: event.DeviceID, RunID: event.RunID, Stage: info.name, Code: info.code, Outcome: outcomeLabel,
				Message: truncate(event.Message, 500),
			})
		}
		if _, ok := firstSeen[info.name]; !ok {
			firstSeen[info.name] = event.Timestamp
		}
		if info.info {
			continue
		}
		key := info.name + "|" + subjectOf(event)
		if strings.HasPrefix(event.Name, "agent.artifact.") && event.Detail != "" {
			// One artifact's latest outcome wins across sync/integrity stages.
			key = "artifact|" + event.Detail
		}
		if _, ok := latest[key]; !ok {
			keys = append(keys, key)
		}
		latest[key] = outcome{success: success, info: info, event: event, position: position}
	}
	var lastSuccess, failure, weakFailure outcome
	failureByStage := map[string]outcome{}
	for _, key := range keys {
		current := latest[key]
		if current.success {
			successful[current.info.name] = true
			if lastSuccess.info.name == "" || stageRank[current.info.name] > stageRank[lastSuccess.info.name] {
				lastSuccess = current
			}
			continue
		}
		if current.info.fail == "" {
			continue
		}
		failureByStage[current.info.name] = current
		target := &failure
		if current.info.weak {
			target = &weakFailure
		}
		if target.info.name == "" || stageRank[current.info.name] < stageRank[target.info.name] ||
			(stageRank[current.info.name] == stageRank[target.info.name] && current.position > target.position) {
			*target = current
		}
	}
	if failure.info.name == "" {
		failure = weakFailure
	}
	// Inferred DHCP failure: Discover observed, no lease confirmed.
	if failure.info.name == "" && sawDiscover && !successful[StageDHCP] {
		failure = outcome{info: stageInfo{
			name: StageDHCP, code: "DHCP_NO_LEASE", inferred: true,
			fail:   "DHCP Discover was observed but no lease acknowledgement was recorded for this client.",
			action: "Check that the DHCP service is bound to the isolated bootstrap interface, that the client MAC has a reservation or the pool has free addresses, and that the DHCP config matches the bootstrap network.",
		}}
	}
	// Provider-observed connectivity of the Agent.
	if agent != nil {
		applyAgentContext(&report, agent, &failure, successful)
	}
	if lastSuccess.info.name != "" {
		report.LastConfirmedSuccessfulStage = lastSuccess.info.name
	}
	if failure.info.name != "" {
		report.ObservedStatus = statusFailed
		report.FailureStage = failure.info.name
		report.FailureCode = failure.info.code
		report.LikelyCause = failure.info.fail
		report.RecommendedNextAction = failure.info.action
		switch {
		case failure.info.inferred:
			report.Confidence = confidenceMed
		case failure.info.weak:
			report.Confidence = confidenceMed
			report.MissingEvidence = append(report.MissingEvidence, "a specific error event explaining the generic failure")
		default:
			report.Confidence = confidenceHigh
		}
		if detail := failureDetail(failure.event); detail != "" {
			report.LikelyCause += " Observed: " + detail
		}
	}
	combined := joined.String()
	if filter.ExpectedHostname != "" || filter.ExpectedIPv4 != "" {
		if hasDeviceChecks(events, filter) {
			successful[StageDevicePostChk] = true
			report.Evidence = append(report.Evidence, Evidence{
				Source: "device-console", DeviceID: filter.DeviceID, RunID: filter.RunID,
				Stage: StageDevicePostChk, Code: "DEVICE_CONFIG_VERIFIED", Outcome: "success",
				Message: "Observed expected hostname and addressing in supplied device-console output.",
			})
			if failure.info.name == "" && completeLifecycle(successful) {
				report.ObservedStatus = statusSuccess
				report.LastConfirmedSuccessfulStage = StageDevicePostChk
				report.LikelyCause = "No failure was identified; the expected device state is present in the supplied console output."
				report.Confidence = confidenceHigh
				report.RecommendedNextAction = "Retain the run evidence and confirm the Provider execution report records the same device and run as verified."
			} else if failure.info.name == "" {
				report.LikelyCause = "The expected device state is present, but one or more earlier lifecycle stages are missing from the supplied evidence."
				report.RecommendedNextAction = "Collect the missing Provider, Agent, DHCP/TFTP, device configuration, or report events for the same run before marking the workflow successful."
			}
		} else if failure.info.name == "" {
			report.MissingEvidence = append(report.MissingEvidence, "device-console output showing the expected hostname and management IPv4 address")
		}
	}
	// Structured post-check evidence (device.postcheck.passed) completes the
	// lifecycle without console expectations being supplied to this call.
	if failure.info.name == "" && report.ObservedStatus != statusSuccess && completeLifecycle(successful) {
		report.ObservedStatus = statusSuccess
		report.LastConfirmedSuccessfulStage = StageDevicePostChk
		report.LikelyCause = "No failure was identified; every lifecycle stage, including the device post-check, is confirmed by correlated events."
		report.Confidence = confidenceHigh
		report.RecommendedNextAction = "Retain the run evidence. Note that AutoInstall applies configuration to running-config; save it on the device if it must survive a reload."
	}
	if failure.info.name == "" && strings.Contains(combined, "unsupported") && (strings.Contains(combined, "model") || strings.Contains(combined, "profile")) {
		report.ObservedStatus = statusFailed
		report.FailureStage = StageProfile
		report.FailureCode = "UNSUPPORTED_DEVICE_PROFILE"
		report.LikelyCause = "The declared vendor, model, OS family, version, or provisioning method has no supported profile."
		report.Confidence = confidenceHigh
		report.RecommendedNextAction = "Check the exact device model and OS version against the registered capability profile before retrying."
	}
	if report.ObservedStatus != statusFailed && report.ObservedStatus != statusSuccess {
		for _, requirement := range requiredStages {
			if !successful[requirement.stage] {
				report.MissingEvidence = append(report.MissingEvidence, requirement.description)
			}
		}
	}
	if uncorrelated && report.Confidence == confidenceHigh {
		report.Confidence = confidenceMed
		report.MissingEvidence = append(report.MissingEvidence, "run_id/device_id correlation fields on every selected event source")
	}
	report.Timeline = buildTimeline(latest, keys, successful, failureByStage, firstSeen, report.FailureStage, report.FailureCode)
	report.Summary = summarize(report)
	if report.CurrentState == "" {
		report.CurrentState = map[string]string{
			statusFailed:    "Stopped at " + report.FailureStage,
			statusSuccess:   "Workflow confirmed end to end",
			statusIncomplet: "No confirmed failure; evidence is incomplete",
		}[report.ObservedStatus]
	}
	return report
}

func applyAgentContext(report *Report, agent *AgentContext, failure *outcome, successful map[string]bool) {
	label := agent.AgentID
	if agent.SiteID != "" {
		label += " (site " + agent.SiteID + ")"
	}
	evidence := Evidence{
		Source: "provider-agent-registry", AgentID: agent.AgentID, SiteID: agent.SiteID,
		Stage: StageHeartbeat, Message: fmt.Sprintf("Provider-observed status %s: %s", strings.ToUpper(agent.Status), agent.Reason),
		Timestamp: agent.LastHeartbeatAt,
	}
	switch agent.Status {
	case "online":
		successful[StageHeartbeat] = true
		evidence.Code, evidence.Outcome = "AGENT_HEARTBEAT_FRESH", "success"
		report.CurrentState = "Agent " + label + " is ONLINE (fresh heartbeat)"
	case "degraded":
		successful[StageHeartbeat] = true
		evidence.Code, evidence.Outcome = "AGENT_REPORTED_PROBLEM", "observed"
		report.CurrentState = "Agent " + label + " is DEGRADED: " + agent.ReportedProblem
	case "offline":
		evidence.Code, evidence.Outcome = "AGENT_OFFLINE", "failure"
		if strings.Contains(agent.Reason, "announced process shutdown") {
			evidence.Code, evidence.Outcome = "AGENT_PROCESS_STOPPED", "observed"
			report.CurrentState = "Agent " + label + " is OFFLINE: its process stopped gracefully"
		} else {
			report.CurrentState = "Agent " + label + " is OFFLINE: " + agent.Reason
			if failure.info.name == "" || stageRank[failure.info.name] > stageRank[StageHeartbeat] {
				*failure = outcome{info: stageInfo{
					name: StageHeartbeat, code: "AGENT_HEARTBEAT_MISSING", inferred: true,
					fail:   fmt.Sprintf("The Provider has not received a fresh authenticated heartbeat from Agent %s (%s). The Agent process may be stopped, or it cannot reach the Provider API.", label, agent.Reason),
					action: "On the site host, check that the Agent process or service is running, then check reachability of provider.api_address, TLS trust, and the token environment variable from that host. The Provider being unreachable does not by itself prove the Agent process is down.",
				}}
			}
		}
	default:
		evidence.Code, evidence.Outcome = "AGENT_STATUS_UNKNOWN", "observed"
		report.CurrentState = "Agent " + label + " status is UNKNOWN: " + agent.Reason
		report.MissingEvidence = append(report.MissingEvidence, "a fresh authenticated heartbeat from the Agent")
	}
	report.Evidence = append([]Evidence{evidence}, report.Evidence...)
}

func buildTimeline(latest map[string]outcome, keys []string, successful map[string]bool, failures map[string]outcome, firstSeen map[string]string, failureStage, failureCode string) []TimelineEntry {
	timeline := make([]TimelineEntry, 0, len(stageOrder))
	for _, stage := range stageOrder {
		entry := TimelineEntry{Stage: stage, State: "not observed", Timestamp: firstSeen[stage]}
		if failed, ok := failures[stage]; ok {
			entry.State, entry.Code, entry.Timestamp = "failed", failed.info.code, failed.event.Timestamp
		} else if successful[stage] {
			entry.State = "confirmed"
			for _, key := range keys {
				if current := latest[key]; current.info.name == stage && current.success {
					entry.Code, entry.Timestamp = current.info.code, current.event.Timestamp
				}
			}
		} else if _, seen := firstSeen[stage]; seen {
			entry.State = "observed"
		}
		if stage == failureStage && entry.State != "failed" {
			entry.State, entry.Code = "failed", failureCode
		}
		timeline = append(timeline, entry)
	}
	return timeline
}

func summarize(report Report) string {
	switch report.ObservedStatus {
	case statusFailed:
		summary := fmt.Sprintf("The workflow stopped at %s (%s).", report.FailureStage, report.FailureCode)
		if report.LastConfirmedSuccessfulStage != "" {
			summary += " Last confirmed stage: " + report.LastConfirmedSuccessfulStage + "."
		}
		return summary
	case statusSuccess:
		return "Every lifecycle stage, including the device post-check, is confirmed by correlated evidence."
	default:
		if report.LastConfirmedSuccessfulStage != "" {
			return fmt.Sprintf("No failure was found. Last confirmed stage: %s. %d expected evidence item(s) are missing, so success cannot be claimed.", report.LastConfirmedSuccessfulStage, len(report.MissingEvidence))
		}
		return fmt.Sprintf("No failure was found and no lifecycle stage is confirmed; %d expected evidence item(s) are missing.", len(report.MissingEvidence))
	}
}

func failureDetail(event Event) string {
	detail := strings.TrimSpace(strings.Join(nonempty(event.ErrorText, event.Detail), " — "))
	if detail == "" {
		return ""
	}
	return truncate(detail, 300)
}

func subjectOf(event Event) string {
	switch {
	case strings.HasPrefix(event.Name, "agent.artifact."):
		return event.Detail
	case strings.HasPrefix(event.Name, "dhcp.") || strings.HasPrefix(event.Name, "tftp.") || strings.HasPrefix(event.Name, "device."):
		return event.DeviceID
	}
	return ""
}

func completeLifecycle(successful map[string]bool) bool {
	for _, requirement := range requiredStages {
		if !successful[requirement.stage] {
			return false
		}
	}
	return true
}

func filterEvents(events []Event, filter Filter) []Event {
	filtered := make([]Event, 0, len(events))
	for _, event := range events {
		if filter.RunID != "" && event.RunID != "" && event.RunID != filter.RunID {
			continue
		}
		if filter.DeviceID != "" && event.DeviceID != "" && event.DeviceID != filter.DeviceID {
			continue
		}
		if filter.AgentID != "" && event.AgentID != "" && event.AgentID != filter.AgentID {
			continue
		}
		if filter.SiteID != "" && event.SiteID != "" && event.SiteID != filter.SiteID {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func isAuthError(text string) bool {
	text = strings.ToLower(text)
	for _, marker := range []string{"unauthenticated", "invalid bearer token", "authentication required", "status 401", "(401)", " 401 ", "invalid agent credentials", "credentials have expired", "token has expired"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func isSiteAuthzError(text string) bool {
	text = strings.ToLower(text)
	return strings.Contains(text, "permissiondenied") || strings.Contains(text, "permission denied") ||
		strings.Contains(text, "not registered for its authorized site") || strings.Contains(text, "already bound to another site") ||
		strings.Contains(text, "status 403") || strings.Contains(text, "(403)") || strings.Contains(text, "status 409")
}

func isUnreachable(text string) bool {
	text = strings.ToLower(text)
	for _, marker := range []string{"connection refused", "no such host", "i/o timeout", "deadline exceeded", "network is unreachable", "code = unavailable", "connect: ", "eof"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

var (
	authFailure = stageInfo{name: StageAuth, code: "AUTHENTICATION_FAILED",
		fail:   "The Provider rejected the Agent's credentials.",
		action: "Check that the Agent's token environment variable holds the current Provider token (at least 32 bytes, not expired) and that the Agent ID matches a configured agent credential. Rotate the token if it may be exposed."}
	siteAuthzFailure = stageInfo{name: StageAuth, code: "SITE_AUTHORIZATION_FAILED",
		fail:   "The Agent authenticated but is not authorized for the requested site, or its identity is bound to another site.",
		action: "Compare agent.site_id with the site bound to this Agent ID in the Provider and with agent_credentials[].site_id; an Agent identity cannot be rebound to another site."}
	unreachableFailure = stageInfo{name: StageConnectivity, code: "PROVIDER_UNREACHABLE",
		fail:   "The Agent could not reach the Provider endpoint.",
		action: "From the site host, check DNS/IP reachability and the port of provider.address / provider.api_address, local firewall rules, and TLS settings. This proves Agent-to-Provider connectivity is broken, not that the Agent process is down."}
)

// classifyStructured interprets InfraFlow's own structured event names.
func classifyStructured(event Event) (stageInfo, bool) {
	errText := event.ErrorText + " " + event.Message
	switch event.Name {
	case "agent.registration.completed":
		return stageInfo{name: StageRegistration, code: "AGENT_REGISTERED"}, true
	case "agent.registration.failed", "agent.heartbeat.failed", "agent.catalog.failed":
		switch {
		case isAuthError(errText):
			return authFailure, false
		case isSiteAuthzError(errText):
			return siteAuthzFailure, false
		case isUnreachable(errText):
			return unreachableFailure, false
		case event.Name == "agent.heartbeat.failed":
			return stageInfo{name: StageHeartbeat, code: "HEARTBEAT_REJECTED", fail: "The Provider did not accept the Agent heartbeat.", action: "Inspect the Provider HTTP API log for the heartbeat request status and the Agent heartbeat error text."}, false
		case event.Name == "agent.registration.failed":
			return stageInfo{name: StageRegistration, code: "AGENT_REGISTRATION_FAILED", fail: "The Agent could not register with the Provider.", action: "Inspect the registration error, the Agent ID/site ID format, and the Provider HTTP API log."}, false
		default:
			return stageInfo{name: StageSync, code: "CATALOG_SYNC_FAILED", fail: "The Agent could not obtain the Provider artifact catalog.", action: "Check that artifacts were published for this site and inspect the Provider gRPC operation log for the catalog request."}, false
		}
	case "agent.heartbeat.restored":
		return stageInfo{name: StageHeartbeat, code: "AGENT_HEARTBEAT_ACCEPTED"}, true
	case "agent.heartbeat.degraded":
		return stageInfo{name: StageHeartbeat, code: "AGENT_REPORTED_PROBLEM", info: true}, true
	case "agent.disconnected":
		return stageInfo{name: StageHeartbeat, code: "AGENT_PROCESS_STOPPED", info: true}, true
	case "agent.provider.connect_failed":
		return unreachableFailure, false
	case "agent.catalog.invalid":
		return stageInfo{name: StageSync, code: "CATALOG_INVALID", fail: "The Provider catalog failed Agent-side validation.", action: "Regenerate and republish the site catalog, then compare the catalog entries with the site manifest."}, false
	case "agent.site_manifest.invalid":
		if strings.Contains(strings.ToLower(errText), "sha-256") {
			return stageInfo{name: StageIntegrity, code: "SITE_MANIFEST_HASH_MISMATCH", fail: "The site manifest did not match its published size or SHA-256 digest.", action: "Republish the site catalog and verify that no intermediary modified the manifest; do not process the site until it verifies."}, false
		}
		return stageInfo{name: StageSync, code: "SITE_MANIFEST_INVALID", fail: "The site manifest was missing, duplicated, or did not match the catalog or this Agent's identity.", action: "Check agent.site_id and regenerate the site catalog with generate-all."}, false
	case "agent.artifact.completed":
		switch strings.ToUpper(event.Status) {
		case "COMPLETED":
			return stageInfo{name: StageIntegrity, code: "ARTIFACT_HASH_VERIFIED"}, true
		case "BLOCKED":
			return stageInfo{name: StageSync, code: "ARTIFACT_BLOCKED", fail: "The Agent refused to process an artifact because no verified handler exists for it.", action: "Review the blocked artifact type; blocked artifacts are a safety stop, not a transfer failure."}, false
		}
		lower := strings.ToLower(errText)
		if strings.Contains(lower, "sha-256") || strings.Contains(lower, "hash mismatch") {
			return stageInfo{name: StageIntegrity, code: "ARTIFACT_HASH_MISMATCH", fail: "The Agent received bytes that do not match the published SHA-256 digest and rejected the artifact.", action: "Compare the published SHA-256 catalog entry with the Provider file, regenerate if the source changed after publication, then resynchronize. The rejected file was not staged."}, false
		}
		return stageInfo{name: StageSync, code: "ARTIFACT_DOWNLOAD_FAILED", fail: "An artifact transfer from the Provider did not complete.", action: "Check Provider gRPC reachability and the Provider gRPC log for the download, then rerun the Agent."}, false
	case "agent.run.completed":
		return stageInfo{name: StageSync, code: "CATALOG_SYNC_COMPLETED"}, true
	case "agent.run.failed":
		return stageInfo{name: StageSync, code: "AGENT_RUN_FAILED", weak: true, fail: "The Agent run did not complete every artifact.", action: "Inspect the per-artifact events of the same run for the specific failure."}, false
	case "agent.report.submitted":
		return stageInfo{name: StageReport, code: "EXECUTION_REPORT_ACCEPTED"}, true
	case "agent.report.submit_failed", "agent.report.persist_failed":
		return stageInfo{name: StageReport, code: "REPORT_SUBMIT_FAILED", fail: "The Agent could not persist or submit its execution report.", action: "Check Provider gRPC reachability and local state directory permissions; the report remains on the Agent until it is accepted."}, false
	case "provider.grpc.operation":
		switch event.Status {
		case "Unauthenticated":
			return authFailure, false
		case "PermissionDenied":
			return siteAuthzFailure, false
		case "OK":
			if strings.Contains(event.Operation, "ReportState") {
				return stageInfo{name: StageReport, code: "EXECUTION_REPORT_ACCEPTED"}, true
			}
		}
		return stageInfo{}, false
	case "dhcp.discover":
		return stageInfo{name: StageDHCP, code: "DHCP_DISCOVER", info: true}, true
	case "dhcp.offer":
		return stageInfo{name: StageDHCP, code: "DHCP_OFFER", info: true}, true
	case "dhcp.ack":
		return stageInfo{name: StageDHCP, code: "DHCP_LEASE_ASSIGNED"}, true
	case "dhcp.no_offer":
		return stageInfo{name: StageDHCP, code: "DHCP_NO_OFFER", fail: "A DHCP client request was observed without a matching offer or lease acknowledgement.", action: "Check that the DHCP service is bound to the isolated bootstrap interface and that the pool contains an unreserved address or the client MAC has a reservation."}, false
	case "dhcp.nak":
		return stageInfo{name: StageDHCP, code: "DHCP_NAK", fail: "The DHCP server refused the address requested by the client.", action: "Clear the client's stale lease (or wait for it to restart DISCOVER) and check that reservations match the device MAC."}, false
	case "tftp.rrq":
		return stageInfo{name: StageTFTP, code: "TFTP_RRQ", info: true}, true
	case "tftp.transfer.completed":
		return stageInfo{name: StageTFTP, code: "TFTP_FILE_TRANSFERRED"}, true
	case "tftp.file_missing":
		return stageInfo{name: StageTFTP, code: "TFTP_FILE_MISSING", fail: "The device requested a bootstrap filename that the TFTP server could not serve (missing bootstrap file or incorrect bootfile name).", action: "Compare the requested filename with the DHCP bootfile option, the generated TFTP allowlist, and the staged files in the Agent bootstrap directory."}, false
	case "tftp.denied":
		return stageInfo{name: StageTFTP, code: "TFTP_REQUEST_DENIED", fail: "The TFTP server denied a request with an unsafe path.", action: "Check the DHCP bootfile option; requests must be plain relative names from the generated allowlist."}, false
	case "tftp.transfer.failed":
		return stageInfo{name: StageTFTP, code: "TFTP_TRANSFER_FAILED", fail: "The TFTP transfer did not complete (timeout or client abort).", action: "Check UDP reachability on the isolated link, the TFTP bind address, MTU/blocksize, and the server transfer log."}, false
	case "device.config.applied":
		return stageInfo{name: StageDeviceConfig, code: "DEVICE_CONFIG_APPLIED"}, true
	case "device.config.rejected":
		return stageInfo{name: StageDeviceConfig, code: "DEVICE_CONFIG_REJECTED", fail: "The device reported that the downloaded configuration was rejected or contained invalid syntax.", action: "Use the console error location to correct the native template, then regenerate and review the artifact before retrying."}, false
	case "device.postcheck.passed":
		return stageInfo{name: StageDevicePostChk, code: "DEVICE_CONFIG_VERIFIED"}, true
	case "device.postcheck.failed":
		return stageInfo{name: StageDevicePostChk, code: "DEVICE_POSTCHECK_FAILED", fail: "The device is reachable but its running state does not match the expected configuration.", action: "Compare the expected hostname/addressing with the device running configuration; the configuration may have been partially applied."}, false
	}
	return stageInfo{}, false
}

// classify is the keyword fallback for unstructured console and service lines.
func classify(event Event) (stageInfo, bool) {
	text := strings.ToLower(event.Name + " " + event.Message + " " + event.Raw)
	if strings.Contains(text, "hash mismatch") || strings.Contains(text, "integrity check failed") || strings.Contains(text, "checksum mismatch") {
		return stageInfo{name: StageIntegrity, code: "ARTIFACT_HASH_MISMATCH", fail: "The Agent or downstream verifier observed bytes that do not match the published SHA-256 digest.", action: "Quarantine the staged file, compare its published SHA-256 catalog entry with the Agent copy, then resynchronize the authorized artifact."}, false
	}
	if strings.Contains(text, "unsupported") && (strings.Contains(text, "model") || strings.Contains(text, "profile")) {
		return stageInfo{name: StageProfile, code: "UNSUPPORTED_DEVICE_PROFILE", fail: "The requested device identity or bootstrap method is not covered by an explicit profile.", action: "Check the exact device model, OS family, version, and method against a profile that explicitly supports the combination."}, false
	}
	if strings.Contains(text, "no dhcp offer") || strings.Contains(text, "dhcp timeout") || strings.Contains(text, "discover without offer") || strings.Contains(text, "dhcp failed") {
		return stageInfo{name: StageDHCP, code: "DHCP_NO_OFFER", fail: "A DHCP client request was observed without a matching offer or lease acknowledgement.", action: "Check that the DHCP service is bound to the isolated bootstrap interface and that the pool contains an unreserved address."}, false
	}
	if strings.Contains(text, "dhcp offer") || strings.Contains(text, "dhcpoffer") || strings.Contains(text, "lease assigned") || strings.Contains(text, "dhcp ack") || strings.Contains(text, "dhcpack") {
		return stageInfo{name: StageDHCP, code: "DHCP_LEASE_ASSIGNED"}, true
	}
	if strings.Contains(text, "file not found") || strings.Contains(text, "no such file") || strings.Contains(text, "unknown transfer id") {
		return stageInfo{name: StageTFTP, code: "TFTP_FILE_MISSING", fail: "The device requested a bootstrap filename that the TFTP server could not serve.", action: "Compare the device RRQ filename with the generated TFTP allowlist and staged file names."}, false
	}
	if strings.Contains(text, "tftp timeout") || strings.Contains(text, "tftp failed") || strings.Contains(text, "transfer timed out") {
		return stageInfo{name: StageTFTP, code: "TFTP_TRANSFER_FAILED", fail: "The TFTP request did not complete successfully.", action: "Check UDP reachability on the isolated link, the TFTP bind address, and the server transfer log."}, false
	}
	if strings.Contains(text, "tftp rrq") || strings.Contains(text, "read request for") || strings.Contains(text, "tftp transfer complete") || strings.Contains(text, "sent tftp file") {
		return stageInfo{name: StageTFTP, code: "TFTP_FILE_TRANSFERRED"}, true
	}
	if strings.Contains(text, "invalid input detected") || strings.Contains(text, "configuration rejected") || strings.Contains(text, "config load failed") || strings.Contains(text, "error parsing configuration") {
		return stageInfo{name: StageDeviceConfig, code: "DEVICE_CONFIG_REJECTED", fail: "The device console reports that the downloaded configuration was rejected or contained invalid syntax.", action: "Use the console error location to correct the native template, then regenerate and review the artifact before retrying."}, false
	}
	if strings.Contains(text, "configuration applied") || strings.Contains(text, "config file applied") || strings.Contains(text, "autoinstall complete") {
		return stageInfo{name: StageDeviceConfig, code: "DEVICE_CONFIG_APPLIED"}, true
	}
	if strings.Contains(text, "integrity verified") || strings.Contains(text, "sha-256 verified") || strings.Contains(text, "artifact verified") {
		return stageInfo{name: StageIntegrity, code: "ARTIFACT_HASH_VERIFIED"}, true
	}
	if strings.Contains(text, "catalog sync complete") || strings.Contains(text, "catalog synchronized") || strings.Contains(text, "agent.catalog.completed") {
		return stageInfo{name: StageSync, code: "CATALOG_SYNC_COMPLETED"}, true
	}
	if strings.Contains(text, "agent registered") || strings.Contains(text, "agent.registration.completed") || strings.Contains(text, "site authorized") {
		return stageInfo{name: StageRegistration, code: "AGENT_REGISTERED"}, true
	}
	if strings.Contains(text, "execution report") && (strings.Contains(text, "failed") || strings.Contains(text, "blocked")) {
		return stageInfo{name: StageReport, code: "EXECUTION_REPORT_FAILED", fail: "The Provider recorded a failed or blocked execution report.", action: "Inspect the report's failing artifact result and correlate it with the Agent and device-console events."}, false
	}
	if strings.Contains(text, "execution report") && (strings.Contains(text, "completed") || strings.Contains(text, "verified")) {
		return stageInfo{name: StageReport, code: "EXECUTION_REPORT_COMPLETED"}, true
	}
	return stageInfo{}, false
}

func hasDeviceChecks(events []Event, filter Filter) bool {
	var console strings.Builder
	for _, event := range events {
		if strings.Contains(strings.ToLower(event.Source), "device-console") {
			console.WriteString(event.Raw)
			console.WriteByte('\n')
		}
	}
	text := console.String()
	if filter.ExpectedHostname != "" {
		match := hostnameLine.FindStringSubmatch(text)
		if len(match) != 2 || !strings.EqualFold(match[1], filter.ExpectedHostname) {
			return false
		}
	}
	if filter.ExpectedIPv4 != "" {
		found := false
		for _, ip := range ipv4Token.FindAllString(text, -1) {
			if ip == filter.ExpectedIPv4 {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func safe(value string) string { return observability.Redact(strings.TrimSpace(value)) }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func nonempty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func truncate(value string, limit int) string {
	value = safe(value)
	if len(value) > limit {
		return value[:limit] + "…"
	}
	return value
}

func joinErrors(a, b error) error {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return fmt.Errorf("%v; %w", a, b)
}

func EventID(source, path string, line int, raw string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s", source, path, line, raw)))
	return hex.EncodeToString(digest[:])
}
