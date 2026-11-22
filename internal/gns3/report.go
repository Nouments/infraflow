package gns3

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Statuses allowed in lab reports and in the traceability matrix. A PASS is
// only valid for a scenario whose checks were executed and passed.
var Statuses = []string{"PASS", "PARTIAL", "FAIL", "BLOCKED", "NOT_RUN", "UNSUPPORTED", "NOT_APPLICABLE"}

// ProvisioningStages are the provisioning steps a report may record, in
// order. A downloaded file is ARTIFACT_TRANSFERRED, not CONFIG_APPLIED.
var ProvisioningStages = []string{
	"DHCP_DISCOVERED", "DHCP_OFFERED", "DHCP_BOUND", "TFTP_REQUESTED",
	"ARTIFACT_TRANSFERRED", "CONFIG_APPLIED", "CONFIG_VERIFIED", "CONFIG_PERSISTED",
}

// EvidenceRef points to a file stored next to the report. SHA256 is checked
// by VerifyEvidence.
type EvidenceRef struct {
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	Description string `json:"description"`
}

type Action struct {
	At          time.Time `json:"at"`
	Description string    `json:"description"`
	Result      string    `json:"result"`
}

type Check struct {
	Name     string `json:"name"`
	Expected string `json:"expected"`
	Observed string `json:"observed"`
	Status   string `json:"status"`
}

type Stage struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Evidence string `json:"evidence,omitempty"`
}

// Diagnosis explains a non-PASS outcome. A cause is only "confirmed" when
// the evidence shows it; otherwise it is a hypothesis with a confidence.
type Diagnosis struct {
	Expected       string   `json:"expected"`
	Observed       string   `json:"observed"`
	FailedStage    string   `json:"failed_stage,omitempty"`
	Cause          string   `json:"cause"`
	CauseConfirmed bool     `json:"cause_confirmed"`
	Confidence     string   `json:"confidence"`
	Evidence       []string `json:"evidence"`
	RuledOut       []string `json:"ruled_out,omitempty"`
	NextCheck      string   `json:"next_check"`
}

type ReportGNS3 struct {
	URL       string `json:"url"`
	Version   string `json:"version"`
	Project   string `json:"project"`
	ProjectID string `json:"project_id"`
}

// ScenarioReport is the JSON report of one lab scenario.
type ScenarioReport struct {
	SchemaVersion    int               `json:"schema_version"`
	ScenarioID       string            `json:"scenario_id"`
	Title            string            `json:"title"`
	Mandatory        bool              `json:"mandatory"`
	StartedAt        time.Time         `json:"started_at"`
	FinishedAt       time.Time         `json:"finished_at"`
	DurationSeconds  float64           `json:"duration_seconds"`
	InfraFlowVersion string            `json:"infraflow_version"`
	Commit           string            `json:"commit"`
	WorktreeDirty    bool              `json:"worktree_dirty"`
	GNS3             ReportGNS3        `json:"gns3"`
	Site             string            `json:"site"`
	Device           string            `json:"device"`
	Model            string            `json:"model"`
	OS               string            `json:"os"`
	OSVersion        string            `json:"os_version"`
	AgentID          string            `json:"agent_id"`
	ExecutionID      string            `json:"execution_id"`
	InitialState     string            `json:"initial_state"`
	Actions          []Action          `json:"actions"`
	FinalState       string            `json:"final_state"`
	Checks           []Check           `json:"checks"`
	Stages           []Stage           `json:"stages,omitempty"`
	Logs             []EvidenceRef     `json:"logs"`
	Captures         []EvidenceRef     `json:"captures"`
	ArtifactHashes   map[string]string `json:"artifact_hashes"`
	Errors           []string          `json:"errors"`
	Warnings         []string          `json:"warnings"`
	Status           string            `json:"status"`
	StatusReason     string            `json:"status_reason"`
	Diagnosis        *Diagnosis        `json:"diagnosis,omitempty"`
}

// secretPattern flags values that look like credentials. Reports describe
// that a credential was used, never its value.
var secretPattern = regexp.MustCompile(`(?i)(bearer\s+[a-z0-9._~+/-]{20,}|"?(password|passwd|secret|token|api[_-]?key)"?\s*[:=]\s*"?[^\s",}]{4,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|eyJ[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,})`)

// Validate checks structure and consistency. It does not read evidence.
func (report ScenarioReport) Validate() []string {
	var problems []string
	need := func(field, value string) {
		if strings.TrimSpace(value) == "" {
			problems = append(problems, field+": required")
		}
	}
	if report.SchemaVersion != 1 {
		problems = append(problems, "schema_version: must be 1")
	}
	need("scenario_id", report.ScenarioID)
	need("title", report.Title)
	need("commit", report.Commit)
	need("infraflow_version", report.InfraFlowVersion)
	need("gns3.version", report.GNS3.Version)
	need("gns3.project", report.GNS3.Project)
	need("initial_state", report.InitialState)
	need("final_state", report.FinalState)
	need("status_reason", report.StatusReason)
	if report.StartedAt.IsZero() || report.FinishedAt.Before(report.StartedAt) {
		problems = append(problems, "started_at/finished_at: invalid interval")
	}
	if !slices.Contains(Statuses, report.Status) {
		problems = append(problems, fmt.Sprintf("status: %q is not one of %v", report.Status, Statuses))
	}
	for index, check := range report.Checks {
		if !slices.Contains(Statuses, check.Status) {
			problems = append(problems, fmt.Sprintf("checks[%d].status: %q not allowed", index, check.Status))
		}
		if check.Expected == "" || check.Observed == "" {
			problems = append(problems, fmt.Sprintf("checks[%d]: expected and observed are required", index))
		}
	}
	for index, stage := range report.Stages {
		if !slices.Contains(ProvisioningStages, stage.Name) {
			problems = append(problems, fmt.Sprintf("stages[%d].name: %q is not a provisioning stage", index, stage.Name))
		}
		if !slices.Contains(Statuses, stage.Status) {
			problems = append(problems, fmt.Sprintf("stages[%d].status: %q not allowed", index, stage.Status))
		}
	}
	if report.Status == "PASS" {
		if len(report.Checks) == 0 {
			problems = append(problems, "status PASS requires at least one executed check")
		}
		for index, check := range report.Checks {
			if check.Status != "PASS" && check.Status != "NOT_APPLICABLE" {
				problems = append(problems, fmt.Sprintf("status PASS contradicts checks[%d] = %s", index, check.Status))
			}
		}
		if len(report.Errors) > 0 {
			problems = append(problems, "status PASS contradicts recorded errors")
		}
	}
	if report.Status != "PASS" && report.Status != "NOT_APPLICABLE" && report.Diagnosis == nil {
		problems = append(problems, "a non-PASS status requires a diagnosis")
	}
	if diagnosis := report.Diagnosis; diagnosis != nil {
		need("diagnosis.expected", diagnosis.Expected)
		need("diagnosis.observed", diagnosis.Observed)
		need("diagnosis.cause", diagnosis.Cause)
		need("diagnosis.next_check", diagnosis.NextCheck)
		if !slices.Contains([]string{"high", "medium", "low"}, diagnosis.Confidence) {
			problems = append(problems, "diagnosis.confidence: must be high, medium or low")
		}
		if len(diagnosis.Evidence) == 0 {
			problems = append(problems, "diagnosis.evidence: required")
		}
	}
	if data, err := json.Marshal(report); err == nil {
		if match := secretPattern.Find(data); match != nil {
			problems = append(problems, fmt.Sprintf("report contains a credential-like value near %q", redactedPrefix(string(match))))
		}
	}
	return problems
}

func redactedPrefix(value string) string {
	if len(value) > 12 {
		value = value[:12]
	}
	return value + "…"
}

// VerifyEvidence checks that every referenced file exists under root, stays
// inside it, and matches its SHA-256.
func (report ScenarioReport) VerifyEvidence(root string) []string {
	var problems []string
	for _, ref := range append(append([]EvidenceRef(nil), report.Logs...), report.Captures...) {
		clean := filepath.Clean(filepath.FromSlash(ref.Path))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			problems = append(problems, fmt.Sprintf("evidence %q: must be a path relative to the report directory", ref.Path))
			continue
		}
		digest, err := fileSHA256(filepath.Join(root, clean))
		if err != nil {
			problems = append(problems, fmt.Sprintf("evidence %q: %v", ref.Path, err))
			continue
		}
		if digest != ref.SHA256 {
			problems = append(problems, fmt.Sprintf("evidence %q: SHA-256 mismatch", ref.Path))
		}
	}
	return problems
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// FileSHA256 is exported for report writers.
func FileSHA256(path string) (string, error) { return fileSHA256(path) }
