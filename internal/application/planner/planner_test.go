package planner

import (
	"encoding/json"
	"testing"

	"infraflow/internal/domain"
)

func TestBuildPlanProducesDeterministicPlannedTasks(t *testing.T) {
	infrastructure := domain.Infrastructure{Sites: []domain.Site{{
		Name: "lab",
		Devices: []domain.Device{
			{Name: "R3", Vendor: "cisco", Family: "iosxe", Model: "ios-xe", Provisioning: domain.Provisioning{Method: "netconf"}, Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "Gi1", Role: "lan", IPv4Mode: "static"}}}},
			{Name: "R1", Vendor: "cisco", Family: "iosxe", Model: "ios-xe", Provisioning: domain.Provisioning{Method: "netconf"}, Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "Gi1", Role: "lan", IPv4Mode: "static"}}}},
			{Name: "R2", Vendor: "cisco", Family: "iosxe", Model: "ios-xe", Provisioning: domain.Provisioning{Method: "netconf"}, Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "Gi1", Role: "lan", IPv4Mode: "static"}}}},
		},
	}}}

	first, err := json.Marshal(Build(infrastructure))
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(Build(infrastructure))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("same infrastructure produced different plans")
	}

	plan := Build(infrastructure)
	if plan.Status != "PLANNED" {
		t.Fatalf("expected planned plan, got %q", plan.Status)
	}
	if len(plan.Tasks) != 3 {
		t.Fatalf("expected 3 interface tasks, got %d: %#v", len(plan.Tasks), plan.Tasks)
	}
	if plan.Tasks[0].Target != "R1" || plan.Tasks[0].Action != "configure_interfaces" {
		t.Fatalf("unexpected first task ordering: %#v", plan.Tasks)
	}
	if plan.Tasks[0].Method != "netconf" {
		t.Fatalf("expected netconf method from provisioning, got %q", plan.Tasks[0].Method)
	}
}

func TestBuildPlanWithoutInterfacesProducesEmptyPlanningTasks(t *testing.T) {
	plan := Build(domain.Infrastructure{Sites: []domain.Site{{Name: "lab"}}})
	if plan.Status != "PLANNED" {
		t.Fatalf("expected planned plan, got %q", plan.Status)
	}
	if len(plan.Tasks) != 0 {
		t.Fatalf("unexpected tasks for empty desired state: %#v", plan.Tasks)
	}
}

func TestBuildPlanCreatesOnlyRealInterfaceConfigurationTasks(t *testing.T) {
	infrastructure := domain.Infrastructure{Sites: []domain.Site{{
		Name: "lab",
		Devices: []domain.Device{{
			Name:         "R1",
			Vendor:       "cisco",
			Family:       "iosxe",
			Model:        "ios-xe",
			Provisioning: domain.Provisioning{Method: "netconf"},
			Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{
				{Name: "GigabitEthernet1", Role: "lan", IPv4Mode: "static"},
				{Name: "GigabitEthernet2", Role: "wan", IPv4Mode: "static"},
			}},
		}},
	}}}

	plan := Build(infrastructure)
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected a single interface task for real desired state, got %#v", plan.Tasks)
	}
	if plan.Tasks[0].Action != "configure_interfaces" || plan.Tasks[0].Method != "netconf" {
		t.Fatalf("unexpected action or method in task: %#v", plan.Tasks[0])
	}
	if plan.Tasks[0].Status != "PLANNED" {
		t.Fatalf("expected task to remain planned, got %q", plan.Tasks[0].Status)
	}
}

func TestBuildPlanDoesNotInventExecutionOrVerificationState(t *testing.T) {
	plan := Build(domain.Infrastructure{Sites: []domain.Site{{
		Name: "lab",
		Devices: []domain.Device{{
			Name:    "R1",
			Vendor:  "cisco",
			Family:  "iosxe",
			Model:   "ios-xe",
			Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "Gi1", Role: "lan", IPv4Mode: "static"}}},
		}},
	}}})
	if plan.Status != "PLANNED" {
		t.Fatalf("plan should be planned only: got %q", plan.Status)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("unexpected task count: %#v", plan.Tasks)
	}
	if plan.Tasks[0].Status != "PLANNED" {
		t.Fatalf("task status must remain planned: got %q", plan.Tasks[0].Status)
	}
}
