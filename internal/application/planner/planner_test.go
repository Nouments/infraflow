package planner

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
	assertPlannedLifecycle(t, plan.Lifecycle)
}

func TestBuildPlanWithoutInterfacesOnDeviceProducesNoTasks(t *testing.T) {
	plan := Build(domain.Infrastructure{Sites: []domain.Site{{
		Name:    "lab",
		Devices: []domain.Device{{Name: "R1"}},
	}}})
	if len(plan.Tasks) != 0 {
		t.Fatalf("device without interfaces produced tasks: %#v", plan.Tasks)
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
	assertPlannedLifecycle(t, plan.Lifecycle)
	if len(plan.Tasks[0].Artifacts) != 0 || len(plan.Tasks[0].Dependencies) != 0 {
		t.Fatalf("planner invented artifacts or dependencies: %#v", plan.Tasks[0])
	}
}

func TestBuildPlanIsStableAcrossSiteAndDeviceOrdering(t *testing.T) {
	first := domain.Infrastructure{Sites: []domain.Site{
		{Name: "beta", Devices: []domain.Device{
			{Name: "R3", Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "Gi1"}}}},
			{Name: "R2", Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "Gi1"}}}},
		}},
		{Name: "alpha", Devices: []domain.Device{{Name: "R1", Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "Gi1"}}}}}},
	}}
	second := domain.Infrastructure{Sites: []domain.Site{
		{Name: "alpha", Devices: []domain.Device{{Name: "R1", Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "Gi1"}}}}}},
		{Name: "beta", Devices: []domain.Device{
			{Name: "R2", Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "Gi1"}}}},
			{Name: "R3", Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "Gi1"}}}},
		}},
	}}
	firstPlan := Build(first)
	secondPlan := Build(second)
	if firstPlan.ID != secondPlan.ID {
		t.Fatalf("input ordering changed plan ID: %q != %q", firstPlan.ID, secondPlan.ID)
	}
	if len(firstPlan.Tasks) != len(secondPlan.Tasks) {
		t.Fatalf("input ordering changed task count: %#v != %#v", firstPlan.Tasks, secondPlan.Tasks)
	}
	for index := range firstPlan.Tasks {
		if firstPlan.Tasks[index].ID != secondPlan.Tasks[index].ID {
			t.Fatalf("input ordering changed task ordering: %#v != %#v", firstPlan.Tasks, secondPlan.Tasks)
		}
	}
	if firstPlan.Site != "alpha" || firstPlan.Tasks[0].Target != "R1" || firstPlan.Tasks[1].Target != "R2" {
		t.Fatalf("unexpected stable ordering: %#v", firstPlan)
	}
}

func TestBuildPlanIDTracksDesiredIntentAndExcludesRuntimeIdentifiers(t *testing.T) {
	infrastructure := domain.Infrastructure{Sites: []domain.Site{{
		ID:   "site-random-1",
		Name: "lab",
		Devices: []domain.Device{{
			ID:           "device-random-1",
			Name:         "R1",
			Provisioning: domain.Provisioning{Method: "netconf"},
			Network:      &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "Gi1", IPv4Mode: "static", IPv4Address: "192.0.2.1/24"}}},
		}},
		Links: []domain.Link{{ID: "link-random-1", A: "R1/Gi1", B: "R2/Gi1"}},
	}}}
	changedIDs := infrastructure
	changedIDs.Sites = append([]domain.Site(nil), infrastructure.Sites...)
	changedIDs.Sites[0].ID = "site-random-2"
	changedIDs.Sites[0].Devices = append([]domain.Device(nil), infrastructure.Sites[0].Devices...)
	changedIDs.Sites[0].Devices[0].ID = "device-random-2"
	changedIDs.Sites[0].Links = append([]domain.Link(nil), infrastructure.Sites[0].Links...)
	changedIDs.Sites[0].Links[0].ID = "link-random-2"
	if Build(infrastructure).ID != Build(changedIDs).ID {
		t.Fatal("random domain identifiers changed desired plan identity")
	}

	changedIntent := infrastructure
	changedIntent.Sites = append([]domain.Site(nil), infrastructure.Sites...)
	changedIntent.Sites[0].Devices = append([]domain.Device(nil), infrastructure.Sites[0].Devices...)
	changedIntent.Sites[0].Devices[0].Network = &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "Gi1", IPv4Mode: "static", IPv4Address: "192.0.2.2/24"}}}
	if Build(infrastructure).ID == Build(changedIntent).ID {
		t.Fatal("a changed desired interface address did not change plan identity")
	}
}

func TestBuildPlanIDUsesUnambiguousCanonicalEncoding(t *testing.T) {
	first := domain.Infrastructure{Sites: []domain.Site{{
		Name:    "a/b",
		Devices: []domain.Device{{Name: "c", Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "eth0"}}}}},
	}}}
	second := domain.Infrastructure{Sites: []domain.Site{{
		Name:    "a",
		Devices: []domain.Device{{Name: "b/c", Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "eth0"}}}}},
	}}}
	firstPlan := Build(first)
	secondPlan := Build(second)
	if firstPlan.ID == secondPlan.ID {
		t.Fatal("different path components collided in plan identity")
	}
	if firstPlan.Tasks[0].ID == secondPlan.Tasks[0].ID {
		t.Fatal("task path components were not escaped unambiguously")
	}
}

func TestBuildPlanCanonicalizesUnorderedDesiredCollections(t *testing.T) {
	first := domain.Infrastructure{Sites: []domain.Site{{
		Name: "lab",
		Devices: []domain.Device{{Name: "R1", Identity: domain.Identity{MACs: []string{"02:00:00:00:00:02", "02:00:00:00:00:01"}}, Network: &domain.DeviceNetwork{
			Interfaces: []domain.NetworkInterface{{Name: "Gi2"}, {Name: "Gi1"}},
		}}},
		Services: map[string]bool{"dns": true, "dhcp": false},
	}}}
	second := domain.Infrastructure{Sites: []domain.Site{{
		Name: "lab",
		Devices: []domain.Device{{Name: "R1", Identity: domain.Identity{MACs: []string{"02:00:00:00:00:01", "02:00:00:00:00:02"}}, Network: &domain.DeviceNetwork{
			Interfaces: []domain.NetworkInterface{{Name: "Gi1"}, {Name: "Gi2"}},
		}}},
		Services: map[string]bool{"dhcp": false, "dns": true},
	}}}
	if Build(first).ID != Build(second).ID {
		t.Fatal("ordering of semantically unordered fields changed plan identity")
	}
}

func TestBuildPlanMethodDoesNotImplyVerification(t *testing.T) {
	tests := []struct {
		name   string
		vendor string
		family string
		method string
	}{
		{name: "netconf", vendor: "cisco", family: "iosxe", method: "netconf"},
		{name: "api", vendor: "mikrotik", family: "routeros", method: "api"},
		{name: "https", vendor: "fortinet", family: "fortios", method: "https"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			device := domain.Device{
				Name:         "R1",
				Vendor:       test.vendor,
				Family:       test.family,
				Provisioning: domain.Provisioning{Method: test.method},
				Network:      &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "Gi1"}}},
			}
			plan := Build(domain.Infrastructure{Sites: []domain.Site{{Name: "lab", Devices: []domain.Device{device}}}})
			if len(plan.Tasks) != 1 || plan.Tasks[0].Method != test.method {
				t.Fatalf("configured method was not preserved: %#v", plan.Tasks)
			}
			profile, ok := domain.VendorProfileFor(device)
			if !ok || profile.Status != domain.CapabilityUnverified {
				t.Fatalf("expected known profile to remain unverified: %#v", profile)
			}
			assertPlannedLifecycle(t, plan.Lifecycle)
		})
	}

	defaulted := domain.Device{
		Name: "R2", Vendor: "cisco", Family: "iosxe",
		Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "Gi1"}}},
	}
	plan := Build(domain.Infrastructure{Sites: []domain.Site{{Name: "lab", Devices: []domain.Device{defaulted}}}})
	if plan.Tasks[0].Method != "netconf" || plan.Lifecycle.Verified {
		t.Fatalf("known default method implied incorrect state: %#v", plan)
	}
}

func TestBuildPlanDoesNotInferActionsFromDeviceRole(t *testing.T) {
	plan := Build(domain.Infrastructure{Sites: []domain.Site{{
		Name: "lab",
		Devices: []domain.Device{{
			Name: "R1", Role: "wan",
			Network: &domain.DeviceNetwork{Interfaces: []domain.NetworkInterface{{Name: "Gi1", Role: "wan"}}},
		}},
	}}})
	if len(plan.Tasks) != 1 || plan.Tasks[0].Action != "configure_interfaces" {
		t.Fatalf("WAN role generated an implicit task: %#v", plan.Tasks)
	}
	if strings.Contains(plan.Tasks[0].Action, "nat") || len(plan.Tasks[0].Artifacts) != 0 || len(plan.Tasks[0].Dependencies) != 0 {
		t.Fatalf("planner invented NAT, artifacts, or dependencies: %#v", plan.Tasks[0])
	}
}

func TestPlannerDoesNotImportNetworkOrCommandExecutionPackages(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate planner test source")
	}
	source := filepath.Join(filepath.Dir(testFile), "planner.go")
	file, err := parser.ParseFile(token.NewFileSet(), source, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imported := range file.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		if path == "net" || (strings.HasPrefix(path, "net/") && path != "net/url") || path == "os/exec" {
			t.Errorf("planner imports forbidden side-effect package %q", path)
		}
	}
}

func assertPlannedLifecycle(t *testing.T, lifecycle domain.LifecycleState) {
	t.Helper()
	if !lifecycle.Desired || !lifecycle.Planned || lifecycle.Generated || lifecycle.Executed || lifecycle.Verified || lifecycle.Observed {
		t.Fatalf("unexpected lifecycle after planning: %#v", lifecycle)
	}
	if lifecycle.LastObservation != nil {
		t.Fatalf("planning created an observation: %#v", lifecycle.LastObservation)
	}
}
