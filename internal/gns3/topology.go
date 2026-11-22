package gns3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

// TopologySpec is a reproducible lab topology. Templates are referenced by
// the name the controller shows; IDs differ between servers.
type TopologySpec struct {
	Project string     `json:"project"`
	Nodes   []NodeSpec `json:"nodes"`
	Links   []LinkSpec `json:"links"`
}

// NodeSpec is one node. MemMiB/DiskMiB are the budget used for the host
// check when the template does not state its own RAM.
type NodeSpec struct {
	Name     string `json:"name"`
	Template string `json:"template"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	MemMiB   int    `json:"mem_mib,omitempty"`
	DiskMiB  int    `json:"disk_mib,omitempty"`
	Purpose  string `json:"purpose,omitempty"`
	// Properties are applied once, right after the node is created (for
	// example hdb_disk_image "" to boot IOSv without a startup-config disk).
	// They are not re-applied to a reused node.
	Properties map[string]any `json:"properties,omitempty"`
}

// PortRef names a node port.
type PortRef struct {
	Node    string `json:"node"`
	Adapter int    `json:"adapter"`
	Port    int    `json:"port"`
}

// LinkSpec connects two ports.
type LinkSpec struct {
	A PortRef `json:"a"`
	B PortRef `json:"b"`
}

// ParseTopology decodes a spec strictly and validates its internal references.
func ParseTopology(reader io.Reader) (TopologySpec, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var spec TopologySpec
	if err := decoder.Decode(&spec); err != nil {
		return TopologySpec{}, fmt.Errorf("topology: %w", err)
	}
	return spec, spec.Validate()
}

// Validate checks names, duplicate nodes, and that links name declared nodes
// and do not reuse a port.
func (spec TopologySpec) Validate() error {
	var problems []string
	if strings.TrimSpace(spec.Project) == "" {
		problems = append(problems, "project: required")
	}
	names := map[string]bool{}
	for index, node := range spec.Nodes {
		switch {
		case strings.TrimSpace(node.Name) == "":
			problems = append(problems, fmt.Sprintf("nodes[%d].name: required", index))
		case names[node.Name]:
			problems = append(problems, fmt.Sprintf("nodes[%d].name: duplicate %q", index, node.Name))
		}
		if strings.TrimSpace(node.Template) == "" {
			problems = append(problems, fmt.Sprintf("nodes[%d].template: required", index))
		}
		names[node.Name] = true
	}
	used := map[PortRef]bool{}
	for index, link := range spec.Links {
		for side, end := range map[string]PortRef{"a": link.A, "b": link.B} {
			if !names[end.Node] {
				problems = append(problems, fmt.Sprintf("links[%d].%s.node: unknown node %q", index, side, end.Node))
			}
			if used[end] {
				problems = append(problems, fmt.Sprintf("links[%d].%s: port %s %d/%d already linked", index, side, end.Node, end.Adapter, end.Port))
			}
			used[end] = true
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return fmt.Errorf("topology invalid: %s", strings.Join(problems, "; "))
	}
	return nil
}

// Budget estimates the host resources the topology needs.
func (spec TopologySpec) Budget(templates []Template) Budget {
	byName := map[string]Template{}
	for _, template := range templates {
		byName[template.Name] = template
	}
	var budget Budget
	for _, node := range spec.Nodes {
		mem := node.MemMiB
		if template, ok := byName[node.Template]; ok && template.RAM > 0 {
			mem = template.RAM
		}
		budget.MemBytes += uint64(mem) << 20
		budget.DiskBytes += uint64(node.DiskMiB) << 20
		if template, ok := byName[node.Template]; ok && int(math.Ceil(template.CPUs)) > budget.CPUs {
			budget.CPUs = int(math.Ceil(template.CPUs))
		}
	}
	return budget
}

// ErrMissingTemplates is returned before anything is created.
var ErrMissingTemplates = errors.New("gns3: topology references templates the server does not have")

// ApplyResult records what ApplyTopology did; it is written to reports.
type ApplyResult struct {
	ProjectID    string          `json:"project_id"`
	CreatedNodes []string        `json:"created_nodes"`
	ReusedNodes  []string        `json:"reused_nodes"`
	CreatedLinks int             `json:"created_links"`
	ReusedLinks  int             `json:"reused_links"`
	Nodes        map[string]Node `json:"nodes"`
}

// ApplyTopology creates missing nodes and links in an opened project. Nodes
// are matched by name and links by their two ports, so a second run changes
// nothing. A node that exists with a different template is an error: the
// project is not the one the spec describes.
func ApplyTopology(ctx context.Context, client *Client, projectID string, spec TopologySpec) (ApplyResult, error) {
	if err := spec.Validate(); err != nil {
		return ApplyResult{}, err
	}
	templates, err := client.Templates(ctx)
	if err != nil {
		return ApplyResult{}, err
	}
	templateByName := map[string]Template{}
	for _, template := range templates {
		templateByName[template.Name] = template
	}
	var missing []string
	for _, node := range spec.Nodes {
		if _, ok := templateByName[node.Template]; !ok {
			missing = append(missing, fmt.Sprintf("%s (node %s)", node.Template, node.Name))
		}
	}
	if len(missing) > 0 {
		return ApplyResult{}, fmt.Errorf("%w: %s", ErrMissingTemplates, strings.Join(missing, ", "))
	}
	existing, err := client.Nodes(ctx, projectID)
	if err != nil {
		return ApplyResult{}, err
	}
	result := ApplyResult{ProjectID: projectID, Nodes: map[string]Node{}}
	for _, node := range existing {
		result.Nodes[node.Name] = node
	}
	for _, node := range spec.Nodes {
		template := templateByName[node.Template]
		if current, ok := result.Nodes[node.Name]; ok {
			if current.TemplateID != "" && current.TemplateID != template.TemplateID {
				return result, fmt.Errorf("gns3: node %q exists with another template; refusing to reuse this project", node.Name)
			}
			result.ReusedNodes = append(result.ReusedNodes, node.Name)
			continue
		}
		created, err := client.CreateNodeFromTemplate(ctx, projectID, template.TemplateID, node.Name, node.X, node.Y)
		if err != nil {
			return result, fmt.Errorf("create node %s: %w", node.Name, err)
		}
		if created.Name != node.Name {
			return result, fmt.Errorf("gns3: controller named node %q %q; names must be unique in the project", node.Name, created.Name)
		}
		if len(node.Properties) > 0 {
			if created, err = client.UpdateNode(ctx, projectID, created.NodeID, map[string]any{"properties": node.Properties}); err != nil {
				return result, fmt.Errorf("set properties of node %s: %w", node.Name, err)
			}
		}
		result.Nodes[node.Name] = created
		result.CreatedNodes = append(result.CreatedNodes, node.Name)
	}
	links, err := client.Links(ctx, projectID)
	if err != nil {
		return result, err
	}
	type endpoint struct {
		node          string
		adapter, port int
	}
	linked := map[endpoint]bool{}
	pairs := map[[2]endpoint]bool{}
	pair := func(x, y endpoint) [2]endpoint {
		if fmt.Sprint(x) > fmt.Sprint(y) {
			x, y = y, x
		}
		return [2]endpoint{x, y}
	}
	for _, link := range links {
		for _, end := range link.Nodes {
			linked[endpoint{end.NodeID, end.AdapterNumber, end.PortNumber}] = true
		}
		if len(link.Nodes) == 2 {
			x, y := link.Nodes[0], link.Nodes[1]
			pairs[pair(endpoint{x.NodeID, x.AdapterNumber, x.PortNumber}, endpoint{y.NodeID, y.AdapterNumber, y.PortNumber})] = true
		}
	}
	for _, link := range spec.Links {
		a := LinkEnd{NodeID: result.Nodes[link.A.Node].NodeID, AdapterNumber: link.A.Adapter, PortNumber: link.A.Port}
		b := LinkEnd{NodeID: result.Nodes[link.B.Node].NodeID, AdapterNumber: link.B.Adapter, PortNumber: link.B.Port}
		aEnd, bEnd := endpoint{a.NodeID, a.AdapterNumber, a.PortNumber}, endpoint{b.NodeID, b.AdapterNumber, b.PortNumber}
		if pairs[pair(aEnd, bEnd)] {
			result.ReusedLinks++
			continue
		}
		aUsed, bUsed := linked[aEnd], linked[bEnd]
		if aUsed || bUsed {
			return result, fmt.Errorf("gns3: link %s %d/%d <-> %s %d/%d conflicts with an existing link", link.A.Node, link.A.Adapter, link.A.Port, link.B.Node, link.B.Adapter, link.B.Port)
		}
		if _, err := client.CreateLink(ctx, projectID, a, b); err != nil {
			return result, fmt.Errorf("link %s <-> %s: %w", link.A.Node, link.B.Node, err)
		}
		linked[aEnd], linked[bEnd] = true, true
		pairs[pair(aEnd, bEnd)] = true
		result.CreatedLinks++
	}
	return result, nil
}
