package gns3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// statefulController keeps nodes and links in memory so that ApplyTopology
// can be run twice against the same "project".
type statefulController struct {
	mu        sync.Mutex
	nodes     []Node
	links     []Link
	nodeCalls int
	linkCalls int
	updates   []string
}

func (fake *statefulController) serve(t *testing.T) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v3/version", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"version":"3.1.0"}`)) })
	mux.HandleFunc("POST /v3/access/users/authenticate", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"access_token":"tok"}`)) })
	mux.HandleFunc("GET /v3/templates", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"template_id":"t-ios","name":"Cisco IOSv","template_type":"qemu","ram":512},{"template_id":"t-vpcs","name":"VPCS","template_type":"vpcs","builtin":true}]`))
	})
	mux.HandleFunc("GET /v3/projects/p/nodes", func(w http.ResponseWriter, _ *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		_ = json.NewEncoder(w).Encode(fake.nodes)
	})
	mux.HandleFunc("GET /v3/projects/p/links", func(w http.ResponseWriter, _ *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		_ = json.NewEncoder(w).Encode(fake.links)
	})
	mux.HandleFunc("POST /v3/projects/p/templates/{template}", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Name string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.nodeCalls++
		node := Node{NodeID: fmt.Sprintf("n%d", len(fake.nodes)+1), Name: body.Name, TemplateID: r.PathValue("template"), Status: "stopped"}
		fake.nodes = append(fake.nodes, node)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(node)
	})
	mux.HandleFunc("PUT /v3/projects/p/nodes/{node}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.updates = append(fake.updates, r.PathValue("node")+":"+fmt.Sprint(body["properties"]))
		for _, node := range fake.nodes {
			if node.NodeID == r.PathValue("node") {
				_ = json.NewEncoder(w).Encode(node)
			}
		}
	})
	mux.HandleFunc("POST /v3/projects/p/links", func(w http.ResponseWriter, r *http.Request) {
		var link Link
		_ = json.NewDecoder(r.Body).Decode(&link)
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.linkCalls++
		link.LinkID = fmt.Sprintf("l%d", len(fake.links)+1)
		fake.links = append(fake.links, link)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(link)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client, err := New(Config{URL: server.URL, Username: "lab", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func labSpec() TopologySpec {
	return TopologySpec{
		Project: "InfraFlow-Automated-Validation",
		Nodes: []NodeSpec{
			{Name: "R1", Template: "Cisco IOSv", Properties: map[string]any{"hdb_disk_image": ""}},
			{Name: "PC1", Template: "VPCS", MemMiB: 16},
		},
		Links: []LinkSpec{{A: PortRef{Node: "R1", Adapter: 0, Port: 0}, B: PortRef{Node: "PC1", Adapter: 0, Port: 0}}},
	}
}

func TestApplyTopologyIsIdempotent(t *testing.T) {
	fake := &statefulController{}
	client := fake.serve(t)
	first, err := ApplyTopology(context.Background(), client, "p", labSpec())
	if err != nil || len(first.CreatedNodes) != 2 || first.CreatedLinks != 1 {
		t.Fatalf("first apply: %+v %v", first, err)
	}
	second, err := ApplyTopology(context.Background(), client, "p", labSpec())
	if err != nil || len(second.CreatedNodes) != 0 || len(second.ReusedNodes) != 2 || second.ReusedLinks != 1 || second.CreatedLinks != 0 {
		t.Fatalf("second apply changed the project: %+v %v", second, err)
	}
	if len(fake.updates) != 1 || fake.updates[0] != "n1:map[hdb_disk_image:]" {
		t.Fatalf("properties must be applied exactly once, at creation: %v", fake.updates)
	}
	if fake.nodeCalls != 2 || fake.linkCalls != 1 {
		t.Fatalf("controller mutated on re-apply: nodes=%d links=%d", fake.nodeCalls, fake.linkCalls)
	}
}

func TestApplyTopologyRefusesMissingTemplatesBeforeCreatingAnything(t *testing.T) {
	fake := &statefulController{}
	client := fake.serve(t)
	spec := labSpec()
	spec.Nodes = append(spec.Nodes, NodeSpec{Name: "FW", Template: "FortiGate 7.4"})
	_, err := ApplyTopology(context.Background(), client, "p", spec)
	if !errors.Is(err, ErrMissingTemplates) || !strings.Contains(err.Error(), "FortiGate 7.4 (node FW)") {
		t.Fatalf("missing template not reported precisely: %v", err)
	}
	if fake.nodeCalls != 0 {
		t.Fatal("nodes were created although the topology cannot be completed")
	}
}

func TestApplyTopologyRefusesAForeignProject(t *testing.T) {
	fake := &statefulController{nodes: []Node{{NodeID: "x", Name: "R1", TemplateID: "someone-elses"}}}
	client := fake.serve(t)
	if _, err := ApplyTopology(context.Background(), client, "p", labSpec()); err == nil || !strings.Contains(err.Error(), "another template") {
		t.Fatalf("project with a conflicting node reused: %v", err)
	}
}

func TestApplyTopologyRefusesAConflictingLink(t *testing.T) {
	fake := &statefulController{}
	client := fake.serve(t)
	if _, err := ApplyTopology(context.Background(), client, "p", labSpec()); err != nil {
		t.Fatal(err)
	}
	spec := labSpec()
	spec.Nodes = append(spec.Nodes, NodeSpec{Name: "PC2", Template: "VPCS"})
	spec.Links = []LinkSpec{{A: PortRef{Node: "R1", Adapter: 0, Port: 0}, B: PortRef{Node: "PC2", Adapter: 0, Port: 0}}}
	if _, err := ApplyTopology(context.Background(), client, "p", spec); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("re-wiring a used port must be refused: %v", err)
	}
}

func TestParseTopologyReportsEveryProblemWithItsPath(t *testing.T) {
	_, err := ParseTopology(strings.NewReader(`{"project":"","nodes":[{"name":"A","template":"x"},{"name":"A","template":""}],"links":[{"a":{"node":"A"},"b":{"node":"B"}}]}`))
	for _, fragment := range []string{"project: required", "nodes[1].name: duplicate", "nodes[1].template: required", `links[0].b.node: unknown node "B"`} {
		if err == nil || !strings.Contains(err.Error(), fragment) {
			t.Fatalf("missing %q in %v", fragment, err)
		}
	}
	if _, err := ParseTopology(strings.NewReader(`{"project":"p","nodes":[],"links":[],"extra":1}`)); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestBudgetUsesTemplateRAMAndHostCheckFailsClosed(t *testing.T) {
	budget := labSpec().Budget([]Template{{Name: "Cisco IOSv", RAM: 512, CPUs: 1}})
	if budget.MemBytes != (512+16)<<20 {
		t.Fatalf("budget: %+v", budget)
	}
	if problems := (HostResources{CPUs: 8}).Check(budget); len(problems) != 2 {
		t.Fatalf("unknown memory/disk must be problems, got %v", problems)
	}
	host := HostResources{CPUs: 8, MemAvailBytes: 8 << 30, DiskFreeBytes: 50 << 30}
	if problems := host.Check(budget); len(problems) != 0 {
		t.Fatalf("sufficient host rejected: %v", problems)
	}
	if problems := host.Check(Budget{MemBytes: 8 << 30}); len(problems) != 1 {
		t.Fatalf("RAM shortfall not detected: %v", problems)
	}
}
