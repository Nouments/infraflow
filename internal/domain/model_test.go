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
