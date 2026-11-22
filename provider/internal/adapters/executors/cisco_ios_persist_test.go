package executors

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"infraflow/internal/domain"
)

// Persistence is an explicit opt-in (persist: true). These tests pin that it
// never happens implicitly, that it only follows a verified running state,
// and that startup-config is read back before success is claimed.

const persistedStartup = "Using 1520 out of 262144 bytes\n!\nversion 15.9\nhostname rtr-core-01\n!\ninterface GigabitEthernet0/0\n description WAN Uplink to Paris HQ\n ip address 10.0.0.1 255.255.255.252\n negotiation auto\n!\ninterface GigabitEthernet0/1\n no ip address\n shutdown\n!\nend\nrtr-core-01#"

func persistPayload() string {
	return strings.Replace(validTaskPayload(), `"site": "paris",`, `"site": "paris", "persist": true,`, 1)
}

func runPersist(t *testing.T, session *mockCiscoSession) (CiscoIOSConfigureResult, domain.ExecutionTaskStatus, error) {
	t.Helper()
	executor, _, _ := setupTestExecutor(session, nil)
	validated, err := executor.ValidateParameters(json.RawMessage(persistPayload()))
	if err != nil {
		t.Fatal(err)
	}
	res, execErr := executor.Execute(context.Background(), domain.ExecutionTask{Parameters: validated})
	var result CiscoIOSConfigureResult
	if err := json.Unmarshal(res.Output, &result); err != nil {
		t.Fatalf("result not structured: %v (%+v)", err, res)
	}
	return result, res.Status, execErr
}

func TestCiscoIOSPersistSavesAndVerifiesStartupConfig(t *testing.T) {
	session := standardMockSession()
	session.responses["write memory"] = "Building configuration...\n[OK]\nrtr-core-01#"
	session.responses["show startup-config"] = persistedStartup
	result, status, err := runPersist(t, session)
	if err != nil || status != domain.ExecutionTaskSucceeded {
		t.Fatalf("persisted execution failed: %v %s", err, status)
	}
	if result.Persistence == nil || !result.Persistence.Saved || !result.Persistence.Verified {
		t.Fatalf("persistence not verified: %+v", result.Persistence)
	}
	if got := result.Persistence.Startup.Interfaces[0]; got.IPv4Address != "10.0.0.1" || got.AdminState != "up" || got.Description != "WAN Uplink to Paris HQ" {
		t.Fatalf("startup read-back parsed wrongly: %+v", got)
	}
	write, show := slices.Index(session.recordedCmds, "write memory"), slices.Index(session.recordedCmds, "show startup-config")
	verify := slices.Index(session.recordedCmds, "show running-config interface GigabitEthernet0/0")
	if write < 0 || show < write || verify > write {
		t.Fatalf("order must be verify running -> write memory -> show startup-config: %v", session.recordedCmds)
	}
}

func TestCiscoIOSPersistFailureIsReportedAndNotRolledBack(t *testing.T) {
	cases := map[string]func(*mockCiscoSession){
		"no [OK]": func(s *mockCiscoSession) {
			s.responses["write memory"] = "Building configuration...\n% Not enough space\nrtr-core-01#"
		},
		"startup differs": func(s *mockCiscoSession) {
			s.responses["write memory"] = "Building configuration...\n[OK]\nrtr-core-01#"
			s.responses["show startup-config"] = strings.Replace(persistedStartup, "10.0.0.1 255.255.255.252", "10.9.9.9 255.255.255.252", 1)
		},
		"interface missing from startup": func(s *mockCiscoSession) {
			s.responses["write memory"] = "Building configuration...\n[OK]\nrtr-core-01#"
			s.responses["show startup-config"] = "hostname rtr-core-01\n!\nend\nrtr-core-01#"
		},
	}
	for name, mutate := range cases {
		session := standardMockSession()
		mutate(session)
		result, status, err := runPersist(t, session)
		if err == nil || status != domain.ExecutionTaskFailed || !strings.Contains(result.Error, "NOT persisted") {
			t.Fatalf("%s: must fail with a NOT persisted error: %v %s %q", name, err, status, result.Error)
		}
		if !result.Verified || result.Persistence == nil || result.Persistence.Verified || result.Rollback != nil {
			t.Fatalf("%s: running state verified, persistence not, and no rollback expected: %+v", name, result)
		}
	}
}

func TestCiscoIOSNeverPersistsUnlessRequested(t *testing.T) {
	session := standardMockSession()
	executor, _, _ := setupTestExecutor(session, nil)
	res, err := executor.Execute(context.Background(), domain.ExecutionTask{Parameters: json.RawMessage(validTaskPayload())})
	if err != nil || res.Status != domain.ExecutionTaskSucceeded {
		t.Fatalf("default execution: %v %+v", err, res)
	}
	if slices.Contains(session.recordedCmds, "write memory") || slices.Contains(session.recordedCmds, "show startup-config") {
		t.Fatalf("persisted without persist: true: %v", session.recordedCmds)
	}
	validated, err := executor.ValidateParameters(json.RawMessage(persistPayload()))
	if err != nil || !strings.Contains(string(validated), `"persist":true`) {
		t.Fatalf("persist flag not kept by validation: %s %v", validated, err)
	}
}
