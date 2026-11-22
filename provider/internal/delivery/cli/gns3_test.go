package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infraflow/internal/gns3"
)

func writeReport(t *testing.T, dir, name string, report gns3.ScenarioReport) {
	t.Helper()
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func baseReport(status string) gns3.ScenarioReport {
	now := time.Now().UTC()
	report := gns3.ScenarioReport{
		SchemaVersion: 1, ScenarioID: "S-" + status, Title: "t", Commit: "c", InfraFlowVersion: "dev",
		GNS3:      gns3.ReportGNS3{Version: "3.1.0a4", Project: "p"},
		StartedAt: now, FinishedAt: now, InitialState: "i", FinalState: "f",
		Checks: []gns3.Check{{Name: "c", Expected: "e", Observed: "e", Status: status}},
		Status: status, StatusReason: "r", Mandatory: true,
	}
	if status != "PASS" {
		report.Diagnosis = &gns3.Diagnosis{Expected: "e", Observed: "o", Cause: "c", Confidence: "high", Evidence: []string{"x"}, NextCheck: "n"}
	}
	return report
}

func TestGNS3ReportCheckExitCodes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "console.log"), []byte("log"), 0o644); err != nil {
		t.Fatal(err)
	}
	digest, _ := gns3.FileSHA256(filepath.Join(dir, "console.log"))
	pass := baseReport("PASS")
	pass.Logs = []gns3.EvidenceRef{{Path: "console.log", SHA256: digest}}
	writeReport(t, dir, "a.json", pass)
	writeReport(t, dir, "b.json", baseReport("UNSUPPORTED"))
	var out, errOut bytes.Buffer
	if code := Run([]string{"gns3", "report-check", "-dir", dir}, &out, &errOut); code != 0 {
		t.Fatalf("valid reports: exit %d\n%s%s", code, out.String(), errOut.String())
	}
	writeReport(t, dir, "c.json", baseReport("FAIL"))
	out.Reset()
	if code := Run([]string{"gns3", "report-check", "-dir", dir}, &out, &errOut); code != 1 || !strings.Contains(out.String(), "mandatory scenario failed") {
		t.Fatalf("mandatory failure must exit 1: %d\n%s", code, out.String())
	}
	if err := os.WriteFile(filepath.Join(dir, "console.log"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, "c.json"))
	out.Reset()
	if code := Run([]string{"gns3", "report-check", "-dir", dir}, &out, &errOut); code != 1 || !strings.Contains(out.String(), "SHA-256 mismatch") {
		t.Fatalf("tampered evidence must exit 1: %d\n%s", code, out.String())
	}
}

func TestGNS3InventoryAndProjectEnsureUseEnvironmentCredentials(t *testing.T) {
	var created []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v3/version":
			_, _ = io.WriteString(w, `{"version":"3.1.0a4"}`)
		case r.URL.Path == "/v3/access/users/authenticate":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["password"] != "from-env" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"tok"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v3/projects":
			_, _ = io.WriteString(w, `[{"project_id":"other","name":"someone else","status":"opened"}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/v3/projects":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			created = append(created, body["name"].(string))
			_, _ = io.WriteString(w, `{"project_id":"new","name":"InfraFlow-Automated-Validation","status":"opened"}`)
		default:
			_, _ = io.WriteString(w, `[]`)
		}
	}))
	defer server.Close()
	t.Setenv(gns3.EnvURL, server.URL)
	t.Setenv(gns3.EnvUsername, "lab")
	t.Setenv(gns3.EnvPassword, "from-env")
	var out, errOut bytes.Buffer
	if code := Run([]string{"gns3", "inventory"}, &out, &errOut); code != 0 || strings.Contains(out.String(), "from-env") {
		t.Fatalf("inventory: exit %d, password leaked=%v\n%s", code, strings.Contains(out.String(), "from-env"), errOut.String())
	}
	out.Reset()
	if code := Run([]string{"gns3", "project-ensure", "-project", "InfraFlow-Automated-Validation"}, &out, &errOut); code != 0 {
		t.Fatalf("project-ensure: exit %d %s", code, errOut.String())
	}
	if len(created) != 1 || !strings.Contains(out.String(), `"created": true`) {
		t.Fatalf("project not created exactly once: %v %s", created, out.String())
	}
	t.Setenv(gns3.EnvPassword, "wrong")
	errOut.Reset()
	if code := Run([]string{"gns3", "inventory"}, &out, &errOut); code != 0 {
		// inventory reports unauthenticated sections as errors but still returns the version
		t.Logf("inventory with wrong password: exit %d", code)
	}
	if code := Run([]string{"gns3", "project-ensure", "-project", "x"}, &out, &errOut); code == 0 || strings.Contains(errOut.String(), "wrong") {
		t.Fatalf("wrong password must fail without echoing it: %d %s", code, errOut.String())
	}
}
