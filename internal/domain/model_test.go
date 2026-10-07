package domain

import "testing"

func TestResolveRejectsUsableCapabilityWithoutEvidence(t *testing.T) {
	registry := CapabilityRegistry{Entries: map[string]map[string]DeviceCapability{
		"cisco:iosxe:ios-xe": {
			"netconf": {
				Method: "netconf",
				State:  CapabilityImplemented,
			},
		},
	}}

	state, evidence := registry.Resolve(Device{
		Vendor:       "cisco",
		Family:       "iosxe",
		Model:        "ios-xe",
		Provisioning: Provisioning{Method: "netconf"},
	})
	if state != CapabilityUnknown {
		t.Fatalf("expected UNKNOWN without evidence, got %q", state)
	}
	if evidence == "" {
		t.Fatal("expected an explicit missing-evidence reason")
	}
}

func TestResolveReturnsVerifiedCapabilityWithEvidence(t *testing.T) {
	registry := CapabilityRegistry{Entries: map[string]map[string]DeviceCapability{
		"cisco:iosxe:ios-xe": {
			"netconf": {
				Method:   "netconf",
				State:    CapabilityLabVerified,
				Evidence: "lab test 2026-10-06",
			},
		},
	}}

	state, evidence := registry.Resolve(Device{
		Vendor:       "cisco",
		Family:       "iosxe",
		Model:        "ios-xe",
		Provisioning: Provisioning{Method: "netconf"},
	})
	if state != CapabilityLabVerified || evidence == "" {
		t.Fatalf("expected verified capability with evidence, got state=%q evidence=%q", state, evidence)
	}
}

func TestInitialStateOnlyRepresentsDesired(t *testing.T) {
	state := NewLifecycleState()
	if !state.Desired || state.Planned || state.Generated || state.Executed || state.Verified || state.Observed {
		t.Fatalf("initial state should only reflect desired input: %#v", state)
	}
	if state.HasObserved() {
		t.Fatal("initial desired state must not be treated as observed")
	}
}

func TestGenerationDoesNotCreateExecutionState(t *testing.T) {
	state := NewLifecycleState()
	if err := state.Transition(StatePlanned); err != nil {
		t.Fatalf("planned transition should be valid: %v", err)
	}
	if err := state.Transition(StateGenerated); err != nil {
		t.Fatalf("generated transition should be valid: %v", err)
	}
	if state.Executed || state.Verified || state.Observed {
		t.Fatalf("generated artifact must not imply execution or verification: %#v", state)
	}
}

func TestExecutionDoesNotAutoVerify(t *testing.T) {
	state := NewLifecycleState()
	if err := state.Transition(StatePlanned); err != nil {
		t.Fatalf("planned transition should be valid: %v", err)
	}
	if err := state.Transition(StateGenerated); err != nil {
		t.Fatalf("generated transition should be valid: %v", err)
	}
	if err := state.Transition(StateExecuted); err != nil {
		t.Fatalf("execution transition should be valid after generation: %v", err)
	}
	if state.Verified || state.Observed {
		t.Fatalf("execution must not automatically produce verification or observation: %#v", state)
	}
}

func TestInferredStateIsNotObserved(t *testing.T) {
	state := NewLifecycleState()
	if _, err := state.RecordObservation("GigabitEthernet1", "192.168.100.10/24", ProvenanceInferred, "derived from desired configuration"); err != nil {
		t.Fatalf("inferred observation should be accepted as metadata, not as real observation: %v", err)
	}
	if state.HasObserved() {
		t.Fatal("inferred data must not be considered observed")
	}
	if state.LastObservation.Provenance != ProvenanceInferred {
		t.Fatalf("expected inferred provenance, got %q", state.LastObservation.Provenance)
	}
}

func TestInvalidTransitionsAreRejected(t *testing.T) {
	state := NewLifecycleState()
	if err := state.Transition(StateVerified); err == nil {
		t.Fatal("verification without execution should be rejected")
	}

	if err := state.Transition(StatePlanned); err != nil {
		t.Fatalf("planned transition should be valid: %v", err)
	}
	if err := state.Transition(StateGenerated); err != nil {
		t.Fatalf("generated transition should be valid: %v", err)
	}
	if err := state.Transition(StateVerified); err == nil {
		t.Fatal("generated-to-verified transition should be rejected without execution")
	}
	if err := state.Transition(StateObserved); err == nil {
		t.Fatal("observed state should require prior verification")
	}
}
