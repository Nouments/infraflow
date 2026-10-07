package planner

import (
	"encoding/json"
	"strings"
	"testing"

	"infraflow/internal/domain"
)

func TestBuildPlanIsDeterministicAndBlocksUnverifiedAdapters(t *testing.T) {
	infrastructure := domain.Infrastructure{Sites: []domain.Site{
		{Name: "zeta", Devices: []domain.Device{{Name: "R2", Vendor: "cisco", Model: "ios-xe"}}},
		{Name: "alpha", Devices: []domain.Device{{Name: "R1", Vendor: "unknown", Model: "unknown"}}},
	}}
	first, _ := json.Marshal(Build(infrastructure))
	second, _ := json.Marshal(Build(infrastructure))
	if string(first) != string(second) {
		t.Fatal("same infrastructure produced different plans")
	}
	plan := Build(infrastructure)
	if plan.Status != "blocked" {
		t.Fatalf("expected blocked plan, got %q", plan.Status)
	}
	if plan.Tasks[0].Site != "alpha" || plan.Tasks[2].Status != "blocked" {
		t.Fatalf("unexpected task ordering or readiness: %#v", plan.Tasks)
	}
}

func TestBuildPlanWithoutDevicesIsReady(t *testing.T) {
	plan := Build(domain.Infrastructure{Sites: []domain.Site{{Name: "lab"}}})
	if plan.Status != "ready" || len(plan.Tasks) != 2 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestBuildPlanMarksUnknownCapabilityRegistryAsBlocked(t *testing.T) {
	infrastructure := domain.Infrastructure{
		CapabilityRegistry: domain.CapabilityRegistry{Entries: map[string]map[string]domain.DeviceCapability{
			"cisco:iosxe:ios-xe": {
				"netconf": {
					Method:   "netconf",
					State:    domain.CapabilityUnverified,
					Evidence: "no lab execution or verified adapter was recorded",
				},
			},
		}},
		TemplateRegistry: domain.TemplateRegistry{Entries: map[string]domain.ProviderTemplate{
			"cisco:iosxe:ios-xe:1": {
				ID:       "cisco:iosxe:ios-xe:1",
				Vendor:   "cisco",
				Family:   "iosxe",
				Model:    "ios-xe",
				Version:  "1",
				Hash:     "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				Evidence: "repository manifest produced by an audited test",
			},
		}},
		Sites: []domain.Site{{
			Name: "site-a",
			Devices: []domain.Device{{
				Name:         "R1",
				Vendor:       "cisco",
				Model:        "ios-xe",
				Family:       "iosxe",
				Role:         "router",
				Provisioning: domain.Provisioning{Method: "netconf", TemplateVersion: "1"},
			}},
		}},
	}

	plan := Build(infrastructure)
	if plan.Status != "blocked" {
		t.Fatalf("expected blocked plan, got %q", plan.Status)
	}
	if len(plan.Tasks) < 3 {
		t.Fatalf("expected at least inventory/topology + device task, got %#v", plan.Tasks)
	}

	deviceTask := plan.Tasks[2]
	if deviceTask.Status != "blocked" {
		t.Fatalf("expected device task to be blocked, got %#v", deviceTask)
	}
	if !contains(deviceTask.Reason, "UNVERIFIED") {
		t.Fatalf("expected explicit unverified status in reason, got %q", deviceTask.Reason)
	}
	if !contains(deviceTask.Reason, "no lab execution") {
		t.Fatalf("expected evidence source in reason, got %q", deviceTask.Reason)
	}
	if !contains(deviceTask.Reason, "template cisco:iosxe:ios-xe:1 version 1 is known") {
		t.Fatalf("expected selected template metadata in reason, got %q", deviceTask.Reason)
	}
	if !contains(deviceTask.Reason, "execution remains blocked") {
		t.Fatalf("expected execution-blocked evidence in reason, got %q", deviceTask.Reason)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
