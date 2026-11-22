package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"infraflow/internal/gns3"
	"infraflow/internal/infrastructure/safefs"
)

// runGNS3 is lab tooling for validating InfraFlow on a GNS3 server. It is not
// part of the Provider runtime: "serve" never contacts GNS3. Credentials come
// only from INFRAFLOW_GNS3_URL / _USERNAME / _PASSWORD.
func runGNS3(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		fmt.Fprintln(stderr, "usage: infraflow-provider gns3 inventory|host-check|project-ensure|topology-apply|start|stop|snapshot|report-check [flags]")
		return 2
	}
	sub := arguments[0]
	flags := flag.NewFlagSet("gns3 "+sub, flag.ContinueOnError)
	flags.SetOutput(stderr)
	outPath := flags.String("out", "", "write the JSON result to this file instead of stdout")
	builtin := flags.Bool("builtin", false, "inventory: include builtin templates")
	topologyPath := flags.String("topology", "", "topology JSON (host-check, topology-apply)")
	diskPath := flags.String("disk", ".", "host-check: filesystem that holds GNS3 images and projects")
	projectName := flags.String("project", "", "project name (exact match)")
	nodeNames := flags.String("nodes", "", "start/stop: comma-separated node names (default: all)")
	timeout := flags.Duration("timeout", 2*time.Minute, "overall timeout")
	reportDir := flags.String("dir", "", "report-check: directory of scenario reports (*.json) and their evidence")
	if err := flags.Parse(arguments[1:]); err != nil || flags.NArg() != 0 {
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	if sub == "report-check" {
		return gns3ReportCheck(*reportDir, stdout, stderr)
	}
	if sub == "host-check" {
		return gns3HostCheck(ctx, *topologyPath, *diskPath, *outPath, stdout, stderr)
	}
	client, err := gns3.New(gns3.ConfigFromEnv())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var result any
	switch sub {
	case "inventory":
		result, err = client.CollectInventory(ctx, *builtin)
	case "project-ensure":
		result, err = gns3EnsureProject(ctx, client, *projectName)
	case "topology-apply":
		result, err = gns3ApplyTopology(ctx, client, *topologyPath)
	case "start", "stop":
		result, err = gns3StartStop(ctx, client, sub, *projectName, *nodeNames)
	case "snapshot":
		result, err = gns3Snapshot(ctx, client, *projectName)
	default:
		fmt.Fprintf(stderr, "infraflow-provider gns3: unknown subcommand %q\n", sub)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "gns3 "+sub+":", err)
		if result == nil {
			return 1
		}
	}
	if writeErr := writeGNS3Result(result, *outPath, stdout); writeErr != nil {
		fmt.Fprintln(stderr, writeErr)
		return 1
	}
	if err != nil {
		return 1
	}
	return 0
}

func writeGNS3Result(result any, outPath string, stdout io.Writer) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if outPath == "" {
		_, err = stdout.Write(data)
		return err
	}
	absolute, err := filepath.Abs(outPath)
	if err != nil {
		return err
	}
	return safefs.AtomicWrite(filepath.Dir(absolute), filepath.Base(absolute), data, 0o644)
}

func loadTopology(path string) (gns3.TopologySpec, error) {
	if path == "" {
		return gns3.TopologySpec{}, errors.New("-topology is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return gns3.TopologySpec{}, err
	}
	defer file.Close()
	return gns3.ParseTopology(file)
}

type hostCheckResult struct {
	Host     gns3.HostResources `json:"host"`
	Budget   *gns3.Budget       `json:"budget,omitempty"`
	Problems []string           `json:"problems"`
	OK       bool               `json:"ok"`
}

func gns3HostCheck(ctx context.Context, topologyPath, diskPath, outPath string, stdout, stderr io.Writer) int {
	result := hostCheckResult{Host: gns3.ReadHostResources(diskPath), Problems: []string{}}
	if topologyPath != "" {
		spec, err := loadTopology(topologyPath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		var templates []gns3.Template
		if client, err := gns3.New(gns3.ConfigFromEnv()); err == nil {
			templates, _ = client.Templates(ctx) // template RAM refines the budget when reachable
		}
		budget := spec.Budget(templates)
		result.Budget = &budget
		result.Problems = append(result.Problems, result.Host.Check(budget)...)
	}
	result.OK = len(result.Problems) == 0
	if err := writeGNS3Result(result, outPath, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !result.OK {
		return 1
	}
	return 0
}

type ensureResult struct {
	Project gns3.Project `json:"project"`
	Created bool         `json:"created"`
	Opened  bool         `json:"opened"`
}

// gns3EnsureProject reuses a project only on an exact name match and never
// deletes or renames anything.
func gns3EnsureProject(ctx context.Context, client *gns3.Client, name string) (any, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("-project is required")
	}
	project, err := client.FindProject(ctx, name)
	if errors.Is(err, gns3.ErrNotFound) {
		created, createErr := client.CreateProject(ctx, name)
		return ensureResult{Project: created, Created: true, Opened: created.Status == "opened"}, createErr
	}
	if err != nil {
		return nil, err
	}
	result := ensureResult{Project: project}
	if project.Status != "opened" {
		opened, err := client.OpenProject(ctx, project.ProjectID)
		if err != nil {
			return result, err
		}
		result.Project, result.Opened = opened, true
	}
	return result, nil
}

func gns3ApplyTopology(ctx context.Context, client *gns3.Client, topologyPath string) (any, error) {
	spec, err := loadTopology(topologyPath)
	if err != nil {
		return nil, err
	}
	project, err := client.FindProject(ctx, spec.Project)
	if err != nil {
		return nil, fmt.Errorf("%w (run project-ensure first)", err)
	}
	if project.Status != "opened" {
		return nil, fmt.Errorf("project %q is %s; open it with project-ensure", spec.Project, project.Status)
	}
	return gns3.ApplyTopology(ctx, client, project.ProjectID, spec)
}

type startStopResult struct {
	Project string            `json:"project"`
	Action  string            `json:"action"`
	Nodes   map[string]string `json:"nodes"`
	Errors  map[string]string `json:"errors,omitempty"`
}

func gns3StartStop(ctx context.Context, client *gns3.Client, action, projectName, names string) (any, error) {
	project, err := client.FindProject(ctx, projectName)
	if err != nil {
		return nil, err
	}
	nodes, err := client.Nodes(ctx, project.ProjectID)
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, name := range strings.Split(names, ",") {
		if name = strings.TrimSpace(name); name != "" {
			wanted[name] = true
		}
	}
	result := startStopResult{Project: project.Name, Action: action, Nodes: map[string]string{}, Errors: map[string]string{}}
	for _, node := range nodes {
		if len(wanted) > 0 && !wanted[node.Name] {
			continue
		}
		delete(wanted, node.Name)
		if action == "start" {
			err = client.StartNode(ctx, project.ProjectID, node.NodeID)
		} else {
			err = client.StopNode(ctx, project.ProjectID, node.NodeID)
		}
		if err != nil {
			result.Errors[node.Name] = err.Error()
		}
	}
	for name := range wanted {
		result.Errors[name] = "no such node in the project"
	}
	if after, err := client.Nodes(ctx, project.ProjectID); err == nil {
		for _, node := range after {
			if _, failed := result.Errors[node.Name]; len(names) == 0 || strings.Contains(","+names+",", ","+node.Name+",") || failed {
				result.Nodes[node.Name] = node.Status
			}
		}
	}
	if len(result.Errors) > 0 {
		return result, fmt.Errorf("%d node(s) failed to %s", len(result.Errors), action)
	}
	result.Errors = nil
	return result, nil
}

type snapshotResult struct {
	CollectedAt time.Time    `json:"collected_at"`
	Controller  gns3.Version `json:"controller"`
	Project     gns3.Project `json:"project"`
	Nodes       []gns3.Node  `json:"nodes"`
	Links       []gns3.Link  `json:"links"`
}

func gns3Snapshot(ctx context.Context, client *gns3.Client, projectName string) (any, error) {
	version, err := client.Version(ctx)
	if err != nil {
		return nil, err
	}
	project, err := client.FindProject(ctx, projectName)
	if err != nil {
		return nil, err
	}
	result := snapshotResult{CollectedAt: time.Now().UTC(), Controller: version, Project: project}
	if project.Status != "opened" {
		return result, nil
	}
	if result.Nodes, err = client.Nodes(ctx, project.ProjectID); err != nil {
		return result, err
	}
	result.Links, err = client.Links(ctx, project.ProjectID)
	return result, err
}

type reportCheckEntry struct {
	File      string   `json:"file"`
	Scenario  string   `json:"scenario_id"`
	Status    string   `json:"status"`
	Mandatory bool     `json:"mandatory"`
	Problems  []string `json:"problems,omitempty"`
}

// gns3ReportCheck validates every scenario report in dir and verifies its
// evidence hashes. It fails when a report is invalid or a mandatory scenario
// did not pass; UNSUPPORTED and BLOCKED are reported, not hidden.
func gns3ReportCheck(dir string, stdout, stderr io.Writer) int {
	if dir == "" {
		fmt.Fprintln(stderr, "gns3 report-check: -dir is required")
		return 2
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		fmt.Fprintln(stderr, "gns3 report-check: no reports in", dir)
		return 1
	}
	summary := struct {
		Reports  []reportCheckEntry `json:"reports"`
		ByStatus map[string]int     `json:"by_status"`
		OK       bool               `json:"ok"`
	}{ByStatus: map[string]int{}, OK: true}
	for _, file := range files {
		entry := reportCheckEntry{File: filepath.Base(file)}
		data, err := os.ReadFile(file)
		var report gns3.ScenarioReport
		if err == nil {
			decoder := json.NewDecoder(strings.NewReader(string(data)))
			decoder.DisallowUnknownFields()
			err = decoder.Decode(&report)
		}
		if err != nil {
			entry.Problems = []string{"unreadable report: " + err.Error()}
		} else {
			entry.Scenario, entry.Status, entry.Mandatory = report.ScenarioID, report.Status, report.Mandatory
			entry.Problems = append(report.Validate(), report.VerifyEvidence(dir)...)
			if report.Mandatory && report.Status == "FAIL" {
				entry.Problems = append(entry.Problems, "mandatory scenario failed")
			}
			summary.ByStatus[report.Status]++
		}
		if len(entry.Problems) > 0 {
			summary.OK = false
		}
		summary.Reports = append(summary.Reports, entry)
	}
	if err := writeGNS3Result(summary, "", stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !summary.OK {
		return 1
	}
	return 0
}
