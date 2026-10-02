package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Artifact struct {
	Type       string `json:"type"`
	Path       string `json:"path"`
	InputHash  string `json:"input_hash"`
	OutputHash string `json:"output_hash"`
}

type AgentReport struct {
	ReportID   string           `json:"report_id"`
	AgentID    string           `json:"agent_id"`
	ReportedAt time.Time        `json:"reported_at"`
	Artifacts  []ArtifactResult `json:"artifacts"`
}

type ArtifactResult struct {
	Path       string `json:"path"`
	OutputHash string `json:"output_hash"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
}

const (
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusBlocked   = "blocked"
)

var siteNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

func ValidSiteName(value string) bool {
	return siteNamePattern.MatchString(value)
}

func ValidArtifactPath(site string, artifact Artifact) bool {
	value := artifact.Path
	if !ValidSiteName(site) || value == "" || path.IsAbs(value) || path.Clean(value) != value || strings.Contains(value, "\\") {
		return false
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] != site {
		return false
	}
	switch artifact.Type {
	case "inventory":
		return parts[1] == "inventory.json"
	case "topology":
		return parts[1] == "topology.json"
	default:
		return false
	}
}

func IsSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func SHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func ComputeReportID(report AgentReport) string {
	artifacts := append([]ArtifactResult(nil), report.Artifacts...)
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].Path != artifacts[j].Path {
			return artifacts[i].Path < artifacts[j].Path
		}
		return artifacts[i].OutputHash < artifacts[j].OutputHash
	})
	payload, _ := json.Marshal(struct {
		AgentID   string           `json:"agent_id"`
		Artifacts []ArtifactResult `json:"artifacts"`
	}{AgentID: report.AgentID, Artifacts: artifacts})
	return SHA256(payload)
}
