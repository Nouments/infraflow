package planner

import (
	"encoding/json"
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
