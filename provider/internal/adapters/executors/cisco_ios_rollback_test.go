package executors

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"infraflow/internal/domain"
)

// rollbackSession models a router whose Gi0/1 starts with a different
// description, a DHCP or static address and admin down, so every rollback
// command is observable.
func rollbackSession(preflightInterface string) *mockCiscoSession {
	session := standardMockSession()
	session.responses["show running-config | include ^hostname"] = "hostname old-name\nold-name#"
	session.sequences = map[string][]string{
		"show running-config interface GigabitEthernet0/0": {preflightInterface, preflightInterface},
	}
	return session
}

func executeRollbackCase(t *testing.T, session *mockCiscoSession) (CiscoIOSConfigureResult, string) {
	t.Helper()
	executor, _, _ := setupTestExecutor(session, nil)
	res, err := executor.Execute(context.Background(), domain.ExecutionTask{Parameters: json.RawMessage(validTaskPayload())})
	if err == nil || res.Status != domain.ExecutionTaskFailed {
		t.Fatalf("expected failed execution, got %+v %v", res, err)
	}
	var result CiscoIOSConfigureResult
	if err := json.Unmarshal(res.Output, &result); err != nil {
		t.Fatalf("failed execution must carry structured rollback evidence: %v", err)
	}
	if result.Verified || result.Rollback == nil {
		t.Fatalf("failed execution reported as verified or without rollback: %+v", result)
	}
	return result, res.Error
}

const staticShutdownPreflight = "interface GigabitEthernet0/0\n description old uplink\n ip address 192.0.2.9 255.255.255.0\n shutdown\nend\nold-name#"

func TestCiscoIOSConfigureRollsBackFailedApplyToPreflightState(t *testing.T) {
	session := rollbackSession(staticShutdownPreflight)
	session.errors["no shutdown"] = errors.New("% Interface cannot be enabled")
	session.responses["hostname old-name"] = "hostname old-name\nold-name(config)#"

	result, message := executeRollbackCase(t, session)
	if !result.Rollback.Attempted || !result.Rollback.Verified || !strings.Contains(message, "rollback was verified") {
		t.Fatalf("rollback was not verified: %+v %s", result.Rollback, message)
	}
	tail := session.recordedCmds[slices.Index(session.recordedCmds, "hostname old-name"):]
	for _, want := range []string{"hostname old-name", "interface GigabitEthernet0/0", "description old uplink", "ip address 192.0.2.9 255.255.255.0", "shutdown", "end"} {
		if !slices.Contains(tail, want) {
			t.Fatalf("rollback did not issue %q: %v", want, tail)
		}
	}
	if slices.Contains(tail, "write memory") || slices.Contains(session.recordedCmds, "copy running-config startup-config") {
		t.Fatal("rollback must never persist configuration")
	}
}

func TestCiscoIOSConfigureRollbackRestoresDHCPAddressing(t *testing.T) {
	preflight := "interface GigabitEthernet0/0\n description old uplink\n ip address dhcp\nend\nold-name#"
	session := rollbackSession(preflight)
	session.errors["no shutdown"] = errors.New("% failure")
	result, _ := executeRollbackCase(t, session)
	if !slices.Contains(session.recordedCmds, "ip address dhcp") || slices.Contains(session.recordedCmds, "no ip address") {
		t.Fatalf("DHCP addressing was not restored as DHCP: %v", session.recordedCmds)
	}
	if !result.Rollback.Verified {
		t.Fatalf("rollback not verified: %+v", result.Rollback)
	}
}

func TestCiscoIOSConfigureRollbackRefusesUnmanagedAddressing(t *testing.T) {
	preflight := "interface GigabitEthernet0/0\n ip address 192.0.2.9 255.255.255.0\n ip address 198.51.100.9 255.255.255.0 secondary\nend\nold-name#"
	session := rollbackSession(preflight)
	session.errors["no shutdown"] = errors.New("% failure")
	result, message := executeRollbackCase(t, session)
	if result.Rollback.Attempted || !strings.Contains(message, "not managed by this executor") || !strings.Contains(message, "router state is uncertain") {
		t.Fatalf("rollback must not delete addressing it cannot represent: %+v %s", result.Rollback, message)
	}
}

func TestCiscoIOSConfigureReportsFailedRollback(t *testing.T) {
	session := rollbackSession(staticShutdownPreflight)
	session.errors["no shutdown"] = errors.New("% failure")
	session.errors["description old uplink"] = errors.New("read tcp: connection reset by peer")
	result, message := executeRollbackCase(t, session)
	if result.Rollback.Verified || !strings.Contains(message, "rollback failed") || !strings.Contains(message, "router state is uncertain and must be re-read before any retry") {
		t.Fatalf("failed rollback was not reported as uncertain: %+v %s", result.Rollback, message)
	}
}

func TestCiscoIOSConfigureRollbackReadBackMismatchIsNotVerified(t *testing.T) {
	session := rollbackSession(staticShutdownPreflight)
	session.errors["no shutdown"] = errors.New("% failure")
	// After rollback the router still shows the requested description.
	session.sequences["show running-config interface GigabitEthernet0/0"] = []string{staticShutdownPreflight,
		"interface GigabitEthernet0/0\n description WAN Uplink to Paris HQ\n ip address 192.0.2.9 255.255.255.0\n shutdown\nend\nold-name#"}
	result, message := executeRollbackCase(t, session)
	if result.Rollback.Verified || !strings.Contains(message, "rollback verification failed") {
		t.Fatalf("mismatching rollback read-back was accepted: %+v %s", result.Rollback, message)
	}
}

func TestCiscoIOSConfigureVerificationFailureTriggersRollback(t *testing.T) {
	session := rollbackSession(staticShutdownPreflight)
	wrong := "interface GigabitEthernet0/0\n description WAN Uplink to Paris HQ\n ip address 10.0.0.2 255.255.255.252\nend\nold-name#"
	session.sequences["show running-config interface GigabitEthernet0/0"] = []string{staticShutdownPreflight, wrong, staticShutdownPreflight}
	session.responses["show running-config | include ^hostname"] = "hostname rtr-core-01\nrtr-core-01#"
	session.sequences["show running-config | include ^hostname"] = []string{"hostname old-name\nold-name#", "hostname rtr-core-01\nrtr-core-01#", "hostname old-name\nold-name#"}
	result, message := executeRollbackCase(t, session)
	if !strings.HasPrefix(message, "verification failed") || !result.Rollback.Verified {
		t.Fatalf("verification failure did not roll back: %+v %s", result.Rollback, message)
	}
}
