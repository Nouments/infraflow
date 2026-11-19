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
	maxLogBytes  = 16 << 20
	maxLogLines  = 10000
	maxLineBytes = 64 << 10
)

type SourceFile struct {
	Name string
	Path string
}

type Event struct {
	Timestamp string `json:"timestamp,omitempty"`
	Level     string `json:"level,omitempty"`
	Name      string `json:"event,omitempty"`
	Source    string `json:"source"`
	SiteID    string `json:"site_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	DeviceID  string `json:"device_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	Stage     string `json:"stage,omitempty"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message"`
	Raw       string `json:"raw"`
}

type Filter struct {
	RunID            string
	DeviceID         string
	ExpectedHostname string
	ExpectedIPv4     string
}

type Evidence struct {
	Timestamp string `json:"timestamp,omitempty"`
	Source    string `json:"source"`
	SiteID    string `json:"site_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	DeviceID  string `json:"device_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	Stage     string `json:"stage,omitempty"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message"`
}

type Report struct {
	ObservedStatus               string     `json:"observed_status"`
	LastConfirmedSuccessfulStage string     `json:"last_confirmed_successful_stage,omitempty"`
	FailureStage                 string     `json:"failure_stage,omitempty"`
	LikelyCause                  string     `json:"likely_cause"`
	Confidence                   string     `json:"confidence"`
	RecommendedNextAction        string     `json:"recommended_next_action"`
	MissingEvidence              []string   `json:"missing_evidence,omitempty"`
	Evidence                     []Evidence `json:"evidence"`
}

type stageInfo struct {
	name   string
	rank   int
	code   string
	fail   string
	action string
}

var hostnameLine = regexp.MustCompile(`(?im)^\s*hostname\s+([A-Za-z][A-Za-z0-9_-]{0,34})\s*$`)
var ipv4Token = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)

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
			events = append(events, Event{
				Timestamp: structured.Timestamp, Level: structured.Level, Name: safe(structured.Event), Source: safe(structured.Source),
				SiteID: safe(structured.SiteID), AgentID: safe(structured.AgentID), DeviceID: safe(structured.DeviceID),
				RunID: safe(structured.RunID), Message: safe(strings.Join(nonempty(structured.Message, structured.Error, structured.Line), " | ")),
				Raw: safe(line),
			})
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

func Analyze(input []Event, filter Filter) Report {
	events := append([]Event(nil), input...)
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Timestamp == "" || events[j].Timestamp == "" {
			return events[i].Timestamp != "" && events[j].Timestamp == ""
		}
		return events[i].Timestamp < events[j].Timestamp
	})
	events = filterEvents(events, filter)
	report := Report{
		ObservedStatus: "incomplete evidence", LikelyCause: "The supplied logs do not identify a confirmed failure.",
		Confidence: "low", RecommendedNextAction: "Collect correlated Provider, Agent, DHCP/TFTP, and device-console evidence for the same run.",
		MissingEvidence: []string{}, Evidence: []Evidence{},
	}
	var lastSuccess stageInfo
	var failure stageInfo
	successful := map[string]bool{}
	uncorrelated := false
	var joined strings.Builder
	for _, event := range events {
		if (filter.RunID != "" && event.RunID == "") || (filter.DeviceID != "" && event.DeviceID == "") {
			uncorrelated = true
		}
		joined.WriteString("\n")
		joined.WriteString(strings.ToLower(event.Message))
		joined.WriteString(" ")
		joined.WriteString(strings.ToLower(event.Raw))
		info, success := classify(event)
		if info.name == "" {
			continue
		}
		report.Evidence = append(report.Evidence, Evidence{
			Timestamp: event.Timestamp, Source: event.Source, SiteID: event.SiteID, AgentID: event.AgentID,
			DeviceID: event.DeviceID, RunID: event.RunID, Stage: info.name, Code: info.code,
			Message: truncate(event.Message, 500),
		})
		if success && info.rank > lastSuccess.rank {
			lastSuccess = info
		}
		if success {
			successful[info.name] = true
		}
		if !success && info.fail != "" && (failure.rank == 0 || info.rank < failure.rank || info.rank == failure.rank) {
			failure = info
		}
	}
	if lastSuccess.name != "" {
		report.LastConfirmedSuccessfulStage = lastSuccess.name
	}
	if failure.name != "" {
		report.ObservedStatus = "failed"
		report.FailureStage = failure.name
		report.LikelyCause = failure.fail
		report.Confidence = "high"
		report.RecommendedNextAction = failure.action
	}
	combined := joined.String()
	if filter.ExpectedHostname != "" || filter.ExpectedIPv4 != "" {
		if hasDeviceChecks(events, filter) {
			successful["device post-check"] = true
			report.Evidence = append(report.Evidence, Evidence{
				Source: "device-console", DeviceID: filter.DeviceID, RunID: filter.RunID,
				Stage: "device post-check", Code: "DEVICE_CONFIG_VERIFIED",
				Message: "Observed expected hostname and addressing in supplied device-console output.",
			})
			if failure.name == "" && completeLifecycle(successful) {
				report.ObservedStatus = "success"
				report.LastConfirmedSuccessfulStage = "device post-check"
				report.LikelyCause = "No failure was identified; the expected device state is present in the supplied console output."
				report.Confidence = "high"
				report.RecommendedNextAction = "Retain the run evidence and confirm the Provider execution report records the same device and run as verified."
			} else if failure.name == "" {
				report.LikelyCause = "The expected device state is present, but one or more earlier lifecycle stages are missing from the supplied evidence."
				report.RecommendedNextAction = "Collect the missing Provider, Agent, DHCP/TFTP, device configuration, or report events for the same run before marking the workflow successful."
			}
		} else if failure.name == "" {
			report.MissingEvidence = append(report.MissingEvidence, "device-console output showing the expected hostname and management IPv4 address")
		}
	}
	if failure.name == "" && strings.Contains(combined, "unsupported") && (strings.Contains(combined, "model") || strings.Contains(combined, "profile")) {
		report.ObservedStatus = "failed"
		report.FailureStage = "profile selection"
		report.LikelyCause = "The declared vendor, model, OS family, version, or provisioning method has no supported profile."
		report.Confidence = "high"
		report.RecommendedNextAction = "Check the exact device model and OS version against the registered capability profile before retrying."
	}
	if failure.name == "" && report.ObservedStatus != "success" {
		for _, requirement := range []struct{ stage, description string }{
			{"agent registration", "Agent registration/site authorization events"},
			{"artifact synchronization", "Agent catalog synchronization events"},
			{"artifact integrity", "Agent SHA-256 integrity verification events"},
			{"DHCP", "DHCP Discover/Offer/Request/Ack evidence"},
			{"TFTP", "TFTP RRQ and completed transfer evidence"},
			{"device configuration", "device configuration execution evidence"},
			{"Provider report", "final Provider execution report"},
			{"device post-check", "device-console post-configuration checks"},
		} {
			if !successful[requirement.stage] {
				report.MissingEvidence = append(report.MissingEvidence, requirement.description)
			}
		}
	}
	if uncorrelated && report.Confidence == "high" {
		report.Confidence = "medium"
		report.MissingEvidence = append(report.MissingEvidence, "run_id/device_id correlation fields on every selected event source")
	}
	return report
}

func completeLifecycle(successful map[string]bool) bool {
	for _, stage := range []string{
		"agent registration", "artifact synchronization", "artifact integrity", "DHCP", "TFTP",
		"device configuration", "Provider report", "device post-check",
	} {
		if !successful[stage] {
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
		filtered = append(filtered, event)
	}
	return filtered
}

func classify(event Event) (stageInfo, bool) {
	text := strings.ToLower(event.Name + " " + event.Message + " " + event.Raw)
	if strings.Contains(text, "hash mismatch") || strings.Contains(text, "integrity check failed") || strings.Contains(text, "checksum mismatch") {
		return stageInfo{name: "artifact integrity", rank: 3, code: "ARTIFACT_HASH_MISMATCH", fail: "The Agent or downstream verifier observed bytes that do not match the published SHA-256 digest.", action: "Quarantine the staged file, compare its published SHA-256 catalog entry with the Agent copy, then resynchronize the authorized artifact."}, false
	}
	if strings.Contains(text, "unsupported") && (strings.Contains(text, "model") || strings.Contains(text, "profile")) {
		return stageInfo{name: "profile selection", rank: 1, code: "UNSUPPORTED_DEVICE_PROFILE", fail: "The requested device identity or bootstrap method is not covered by an explicit profile.", action: "Check the exact device model, OS family, version, and method against a profile that explicitly supports the combination."}, false
	}
	if strings.Contains(text, "no dhcp offer") || strings.Contains(text, "dhcp timeout") || strings.Contains(text, "discover without offer") || strings.Contains(text, "dhcp failed") {
		return stageInfo{name: "DHCP", rank: 4, code: "DHCP_NO_OFFER", fail: "A DHCP client request was observed without a matching offer or lease acknowledgement.", action: "Check that the DHCP service is bound to the isolated bootstrap interface and that the pool contains an unreserved address."}, false
	}
	if strings.Contains(text, "dhcp offer") || strings.Contains(text, "dhcpoffer") || strings.Contains(text, "lease assigned") || strings.Contains(text, "dhcp ack") || strings.Contains(text, "dhcpack") {
		return stageInfo{name: "DHCP", rank: 4, code: "DHCP_LEASE_ASSIGNED"}, true
	}
	if strings.Contains(text, "file not found") || strings.Contains(text, "no such file") || strings.Contains(text, "unknown transfer id") {
		return stageInfo{name: "TFTP", rank: 5, code: "TFTP_FILE_MISSING", fail: "The device requested a bootstrap filename that the TFTP server could not serve.", action: "Compare the device RRQ filename with the generated TFTP allowlist and staged file names."}, false
	}
	if strings.Contains(text, "tftp timeout") || strings.Contains(text, "tftp failed") || strings.Contains(text, "transfer timed out") {
		return stageInfo{name: "TFTP", rank: 5, code: "TFTP_TRANSFER_FAILED", fail: "The TFTP request did not complete successfully.", action: "Check UDP reachability on the isolated link, the TFTP bind address, and the server transfer log."}, false
	}
	if strings.Contains(text, "tftp rrq") || strings.Contains(text, "read request for") || strings.Contains(text, "tftp transfer complete") || strings.Contains(text, "sent tftp file") {
		return stageInfo{name: "TFTP", rank: 5, code: "TFTP_FILE_TRANSFERRED"}, true
	}
	if strings.Contains(text, "invalid input detected") || strings.Contains(text, "configuration rejected") || strings.Contains(text, "config load failed") || strings.Contains(text, "error parsing configuration") {
		return stageInfo{name: "device configuration", rank: 6, code: "DEVICE_CONFIG_REJECTED", fail: "The device console reports that the downloaded configuration was rejected or contained invalid syntax.", action: "Use the console error location to correct the native template, then regenerate and review the artifact before retrying."}, false
	}
	if strings.Contains(text, "configuration applied") || strings.Contains(text, "config file applied") || strings.Contains(text, "autoinstall complete") {
		return stageInfo{name: "device configuration", rank: 6, code: "DEVICE_CONFIG_APPLIED"}, true
	}
	if strings.Contains(text, "integrity verified") || strings.Contains(text, "sha-256 verified") || strings.Contains(text, "artifact verified") {
		return stageInfo{name: "artifact integrity", rank: 3, code: "ARTIFACT_HASH_VERIFIED"}, true
	}
	if strings.Contains(text, "catalog sync complete") || strings.Contains(text, "catalog synchronized") || strings.Contains(text, "agent.catalog.completed") {
		return stageInfo{name: "artifact synchronization", rank: 2, code: "CATALOG_SYNC_COMPLETED"}, true
	}
	if strings.Contains(text, "agent registered") || strings.Contains(text, "agent.registration.completed") || strings.Contains(text, "site authorized") {
		return stageInfo{name: "agent registration", rank: 1, code: "AGENT_REGISTERED"}, true
	}
	if strings.Contains(text, "execution report") && (strings.Contains(text, "failed") || strings.Contains(text, "blocked")) {
		return stageInfo{name: "Provider report", rank: 7, code: "EXECUTION_REPORT_FAILED", fail: "The Provider recorded a failed or blocked execution report.", action: "Inspect the report's failing artifact result and correlate it with the Agent and device-console events."}, false
	}
	if strings.Contains(text, "execution report") && (strings.Contains(text, "completed") || strings.Contains(text, "verified")) {
		return stageInfo{name: "Provider report", rank: 7, code: "EXECUTION_REPORT_COMPLETED"}, true
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
