package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"infraflow/internal/infrastructure/security"
	"infraflow/pkg/observability"
	"infraflow/pkg/protocol"
	infrav1 "infraflow/pkg/protocol/infraflow/v1"
)

func TestAgentCLIComposesAdaptersAndReports(t *testing.T) {
	token := strings.Repeat("r", security.MinAgentTokenBytes)
	data := []byte(`{"site":"lab","devices":[]}`)
	artifact := protocol.Artifact{
		Type: "inventory", Path: "lab/inventory.json",
		InputHash: protocol.SHA256([]byte("input")), OutputHash: protocol.SHA256(data),
	}
	server := &fakeAgentProvider{token: token, artifact: artifact, data: data}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	infrav1.RegisterAgentProviderServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() { grpcServer.Stop(); _ = listener.Close() })
	t.Setenv("INFRAFLOW_TEST_TOKEN", token)
	output := filepath.Join(t.TempDir(), "state")
	logDirectory := filepath.Join(t.TempDir(), "logs")
	configPath := filepath.Join(t.TempDir(), "agent.yaml")
	config := strings.Join([]string{
		"agent:", "  id: agent-01", "  state_directory: " + output,
		"provider:", "  address: " + listener.Addr().String(), "  token_env: INFRAFLOW_TEST_TOKEN",
		"  tls:", "    enabled: false",
		"logging:", "  directory: " + logDirectory, "  level: DEBUG", "  format: json", "",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"run", "-config", configPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("agent run failed with %d: %s", code, stderr.String())
	}
	if server.report == nil || server.report.AgentId != "agent-01" || len(server.report.Artifacts) != 1 || server.report.Artifacts[0].Status != protocol.StatusCompleted {
		t.Fatalf("provider did not receive completed agent state: %#v", server.report)
	}
	if _, err := os.Stat(filepath.Join(output, "lab", "inventory.json")); err != nil {
		t.Fatalf("agent artifact missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(output, ".infraflow-agent-state.json")); err != nil {
		t.Fatalf("agent report missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(logDirectory, "events.jsonl")); err != nil {
		t.Fatalf("agent local logs missing: %v", err)
	}
}

func TestAgentCLIHasNoProviderImplementationCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"generate"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("agent must not expose provider commands: %d %q", code, stderr.String())
	}
}

func TestAgentCLIUsageAndUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run(nil, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("help did not print usage: code=%d stdout=%q", code, stdout.String())
	}
	stdout.Reset()
	if code := Run([]string{"unknown"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("unknown command was not rejected: code=%d stderr=%q", code, stderr.String())
	}
}

func TestServeDHCPRequiresExplicitNetworkBinding(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"serve-dhcp", "-config", "dhcp.json"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "requires -config, -interface, and -listen") {
		t.Fatalf("serve-dhcp accepted missing network binding: code=%d stderr=%q", code, stderr.String())
	}
}

func TestServeDHCPRejectsDisabledConfiguration(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "dhcp.json")
	data := []byte(`{"version":1,"service":"dhcp","enabled":false,"site":"lab","network":"192.168.50.0/24","gateway":"192.168.50.1","pools":[{"start":"192.168.50.20","end":"192.168.50.30"}],"reserved_addresses":["192.168.50.1"],"reservations":[],"options":{"dns_servers":[],"next_server":"","next_server_source":"agent_runtime","boot_mode":"ipxe","boot_filename":"undionly.kpxe","tftp_directory":"tftp","ipxe_script":"pxe/ipxe/bootstrap.ipxe"}}`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"serve-dhcp", "-config", configPath, "-interface", "lo", "-listen", "127.0.0.1"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "service is disabled") {
		t.Fatalf("serve-dhcp accepted a disabled service: code=%d stderr=%q", code, stderr.String())
	}
}

func TestBootstrapFileServersRequireExplicitBindings(t *testing.T) {
	for _, command := range []string{"serve-bootstrap", "serve-tftp"} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if code := Run([]string{command}, &stdout, &stderr); code != 2 {
			t.Errorf("%s accepted missing interface binding: code=%d stderr=%q", command, code, stderr.String())
		}
	}
}

func TestServeArtifactsRequiresAuthForRemoteListen(t *testing.T) {
	t.Setenv("INFRAFLOW_TEST_ARTIFACT_TOKEN", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"serve-artifacts"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "requires -directory and -listen") {
		t.Fatalf("serve-artifacts accepted missing arguments: code=%d stderr=%q", code, stderr.String())
	}
	stderr.Reset()
	code := Run([]string{"serve-artifacts", "-directory", t.TempDir(), "-listen", "0.0.0.0:8081", "-token-env", "INFRAFLOW_TEST_ARTIFACT_TOKEN"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "non-loopback artifact serving requires") {
		t.Fatalf("unauthenticated remote artifact server was accepted: code=%d stderr=%q", code, stderr.String())
	}
}

func TestBootstrapServersRejectInvalidOrUnassignedAddresses(t *testing.T) {
	for _, command := range []string{"serve-bootstrap", "serve-tftp"} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		wantRuntimeCode := 1
		if command == "serve-bootstrap" {
			wantRuntimeCode = 2
		}
		args := []string{command}
		if command == "serve-bootstrap" {
			args = append(args, "-directory", t.TempDir())
		} else {
			bootstrapRoot := t.TempDir()
			if err := os.Mkdir(filepath.Join(bootstrapRoot, "tftp"), 0o700); err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(t.TempDir(), "tftp.json")
			configData := []byte(`{"version":1,"service":"tftp","enabled":true,"site":"lab","root_directory":"tftp","read_only":true,"allowed_files":[{"path":"undionly.kpxe","source":"agent_runtime"}]}`)
			if err := os.WriteFile(configPath, configData, 0o600); err != nil {
				t.Fatal(err)
			}
			args = append(args, "-config", configPath, "-root", bootstrapRoot)
		}
		args = append(args, "-interface", "lo", "-listen", "not-an-ip")
		if code := Run(args, &stdout, &stderr); code != wantRuntimeCode || !strings.Contains(stderr.String(), "must be IPv4") {
			t.Errorf("%s did not reject an invalid listen address: code=%d stderr=%q", command, code, stderr.String())
		}
		stderr.Reset()
		args[len(args)-1] = "192.0.2.99"
		if code := Run(args, &stdout, &stderr); code != wantRuntimeCode || !strings.Contains(stderr.String(), "not assigned") {
			t.Errorf("%s accepted an IP not assigned to the interface: code=%d stderr=%q", command, code, stderr.String())
		}
	}
}

func TestServeTFTPRejectsDisabledConfiguration(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "tftp.json")
	data := []byte(`{"version":1,"service":"tftp","enabled":false,"site":"lab","root_directory":"tftp","read_only":true,"allowed_files":[{"path":"undionly.kpxe","source":"agent_runtime"}]}`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"serve-tftp", "-config", configPath, "-root", t.TempDir(), "-interface", "lo", "-listen", "127.0.0.1"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "service is disabled") {
		t.Fatalf("serve-tftp accepted disabled service: code=%d stderr=%q", code, stderr.String())
	}
}

func TestToolCommandsRequireExplicitSiteAndDirectory(t *testing.T) {
	for _, command := range []string{"execute-ansible", "validate-terraform"} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if code := Run([]string{command}, &stdout, &stderr); code != 2 {
			t.Errorf("%s accepted missing state directory and site: code=%d stderr=%q", command, code, stderr.String())
		}
	}
}

func TestToolCommandsRejectMissingArtifactsAndInvalidTimeouts(t *testing.T) {
	for _, command := range []string{"execute-ansible", "validate-terraform"} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if code := Run([]string{command, "-directory", t.TempDir(), "-site", "lab"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "read ") {
			t.Errorf("%s did not report missing generated artifacts: code=%d stderr=%q", command, code, stderr.String())
		}
		stderr.Reset()
		if code := Run([]string{command, "-directory", t.TempDir(), "-site", "lab", "-timeout", "31m"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "timeout") {
			t.Errorf("%s accepted a timeout above the cap: code=%d stderr=%q", command, code, stderr.String())
		}
	}
}

func TestTUIRejectsMissingCredentialsAndNonLoopbackHTTP(t *testing.T) {
	t.Setenv("INFRAFLOW_TEST_TUI_PASSWORD", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	args := []string{"tui", "-address", "http://127.0.0.1:8080", "-username", "admin", "-password-env", "INFRAFLOW_TEST_TUI_PASSWORD"}
	if code := Run(args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "password environment variable") {
		t.Fatalf("TUI accepted an empty password: code=%d stderr=%q", code, stderr.String())
	}
	t.Setenv("INFRAFLOW_TEST_TUI_PASSWORD", "temporary-password")
	stderr.Reset()
	args[2] = "http://192.0.2.1:8080"
	if code := Run(args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "loopback") {
		t.Fatalf("TUI accepted remote plaintext HTTP: code=%d stderr=%q", code, stderr.String())
	}
}

func TestTUIRejectsMissingCredentialsAndInvalidEndpoint(t *testing.T) {
	t.Setenv("INFRAFLOW_TUI_PASSWORD", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"tui", "-address", "http://127.0.0.1:8080", "-username", "admin"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "password environment variable") {
		t.Fatalf("TUI accepted a missing password: code=%d stderr=%q", code, stderr.String())
	}
	t.Setenv("INFRAFLOW_TUI_PASSWORD", "temporary-password")
	stderr.Reset()
	if code := Run([]string{"tui", "-address", "not-a-url", "-username", "admin"}, &stdout, &stderr); code != 2 {
		t.Fatalf("TUI accepted an invalid API address: code=%d stderr=%q", code, stderr.String())
	}
}

func TestInterfaceAddressValidation(t *testing.T) {
	if !interfaceHasIPv4Address("lo", net.ParseIP("127.0.0.1")) {
		t.Fatal("loopback IP was not found on the loopback interface")
	}
	if interfaceHasIPv4Address("infraflow-missing0", net.ParseIP("127.0.0.1")) || interfaceHasIPv4Address("lo", net.ParseIP("192.0.2.9")) {
		t.Fatal("invalid interface/address pair was accepted")
	}
}

func TestToolCommandsRunProviderGeneratedArtifacts(t *testing.T) {
	stateDirectory := t.TempDir()
	writeCLIArtifact(t, stateDirectory, "lab/ansible/inventory.yml", "all:\n  children:\n    network:\n      hosts:\n        R1:\n          ansible_host: 192.0.2.10\n")
	writeCLIArtifact(t, stateDirectory, "lab/ansible/site.yml", `[{"name":"Inspect declared InfraFlow devices","hosts":"network","gather_facts":false,"tasks":[{"name":"Display declared device metadata","ansible.builtin.debug":{"msg":"device={{ inventory_hostname }} vendor={{ hostvars[inventory_hostname].infraflow_vendor | default('unknown') }} model={{ hostvars[inventory_hostname].infraflow_model | default('unknown') }}"}}]}]`)
	installCLITool(t, "ansible-playbook")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"execute-ansible", "-directory", stateDirectory, "-site", "lab"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "--check") {
		t.Fatalf("execute-ansible did not run in check mode: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	for filename, contents := range map[string]string{
		"versions.tf":  `terraform { required_version = ">= 1.4.0" }`,
		"providers.tf": `# no providers`,
		"variables.tf": `variable "environment" { type = string }`,
		"locals.tf":    `locals { site_names = ["lab"] }`,
		"main.tf":      `# data only`,
		"outputs.tf":   `output "site_names" { value = local.site_names }`,
	} {
		writeCLIArtifact(t, stateDirectory, "lab/terraform/"+filename, contents)
	}
	installCLITool(t, "terraform")
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"validate-terraform", "-directory", stateDirectory, "-site", "lab"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "terraform validate") {
		t.Fatalf("validate-terraform did not run validation: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestAnsibleProcessOutputIsStreamedRedactedAndReportedOverGRPC(t *testing.T) {
	token := strings.Repeat("v", security.MinAgentTokenBytes)
	releasePath := filepath.Join(t.TempDir(), "received")
	server := &fakeAgentProvider{token: token, releasePath: releasePath}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	infrav1.RegisterAgentProviderServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() { grpcServer.Stop(); _ = listener.Close() })

	stateDirectory := t.TempDir()
	writeCLIArtifact(t, stateDirectory, "lab/ansible/inventory.yml", "all:\n  children:\n    network:\n      hosts:\n        R1:\n          ansible_host: 192.0.2.10\n")
	writeCLIArtifact(t, stateDirectory, "lab/ansible/site.yml", `[{
	"name":"Inspect declared InfraFlow devices","hosts":"network","gather_facts":false,
	"tasks":[{"name":"Display declared device metadata","ansible.builtin.debug":{"msg":"device={{ inventory_hostname }} vendor={{ hostvars[inventory_hostname].infraflow_vendor | default('unknown') }} model={{ hostvars[inventory_hostname].infraflow_model | default('unknown') }}"}}]}]`)
	toolDirectory := t.TempDir()
	toolPath := filepath.Join(toolDirectory, "ansible-playbook")
	script := "#!/bin/sh\n" +
		"printf 'token=secret-value\\n'\n" +
		"while [ ! -f '" + releasePath + "' ]; do :; done\n" +
		"printf 'stderr marker\\n' >&2\n"
	if err := os.WriteFile(toolPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", toolDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("INFRAFLOW_TEST_PROCESS_TOKEN", token)
	configPath := filepath.Join(t.TempDir(), "agent.yaml")
	logDirectory := filepath.Join(t.TempDir(), "logs")
	configuration := strings.Join([]string{
		"agent:", "  id: agent-01", "  site_id: site-01", "  state_directory: " + stateDirectory,
		"provider:", "  address: " + listener.Addr().String(), "  token_env: INFRAFLOW_TEST_PROCESS_TOKEN",
		"  tls:", "    enabled: false",
		"logging:", "  directory: " + logDirectory, "  level: DEBUG", "  format: json", "",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"execute-ansible", "-directory", stateDirectory, "-site", "lab", "-config", configPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("configured process command failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "secret-value") || !strings.Contains(stdout.String(), "[REDACTED]") || !strings.Contains(stderr.String(), "stderr marker") {
		t.Fatalf("process output was not separated and redacted: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if len(server.logBatches) == 0 {
		t.Fatal("process logs were not reported to the authenticated gRPC server")
	}
	var startedEvent, outputEvent, errorStreamEvent, completionEvent *observability.Event
	for _, batch := range server.logBatches {
		for _, encoded := range batch.GetEventsJson() {
			var event observability.Event
			if err := json.Unmarshal(encoded, &event); err != nil {
				t.Fatalf("reported log event is malformed: %v", err)
			}
			if event.Event == "process.started" {
				copy := event
				startedEvent = &copy
			}
			if event.Event == "process.output" && event.Stream == "stdout" {
				copy := event
				outputEvent = &copy
			}
			if event.Event == "process.output" && event.Stream == "stderr" {
				copy := event
				errorStreamEvent = &copy
			}
			if event.Event == "process.completed" {
				copy := event
				completionEvent = &copy
			}
			if strings.Contains(string(encoded), "secret-value") {
				t.Fatalf("reported process log leaked a secret: %s", encoded)
			}
		}
	}
	if outputEvent == nil || outputEvent.RunID == "" || !strings.Contains(outputEvent.Line, "[REDACTED]") {
		t.Fatalf("server did not receive a redacted, run-correlated stdout event: %#v", outputEvent)
	}
	if startedEvent == nil || startedEvent.RunID != outputEvent.RunID || startedEvent.Status != "RUNNING" {
		t.Fatalf("server did not receive a matching process start event: %#v output=%#v", startedEvent, outputEvent)
	}
	if errorStreamEvent == nil || errorStreamEvent.RunID != outputEvent.RunID || !strings.Contains(errorStreamEvent.Line, "stderr marker") {
		t.Fatalf("server did not receive stderr as a distinct event: %#v", errorStreamEvent)
	}
	if completionEvent == nil || completionEvent.RunID != outputEvent.RunID || completionEvent.Status != "COMPLETED" {
		t.Fatalf("server did not receive the matching process completion: %#v output=%#v", completionEvent, outputEvent)
	}
}

type fakeAgentProvider struct {
	infrav1.UnimplementedAgentProviderServer
	token       string
	artifact    protocol.Artifact
	data        []byte
	report      *infrav1.AgentStateReport
	logBatches  []*infrav1.AgentLogBatch
	releasePath string
}

func (server *fakeAgentProvider) authorize(ctx context.Context) error {
	values, _ := metadata.FromIncomingContext(ctx)
	if len(values.Get("authorization")) != 1 || values.Get("authorization")[0] != "Bearer "+server.token {
		return status.Error(codes.Unauthenticated, "unauthorized")
	}
	return nil
}

func (server *fakeAgentProvider) ListArtifacts(ctx context.Context, _ *infrav1.Empty) (*infrav1.ArtifactCatalog, error) {
	if err := server.authorize(ctx); err != nil {
		return nil, err
	}
	return &infrav1.ArtifactCatalog{Artifacts: []*infrav1.ArtifactRecord{{
		Type: server.artifact.Type, Path: server.artifact.Path,
		InputHash: server.artifact.InputHash, OutputHash: server.artifact.OutputHash,
	}}}, nil
}

func (server *fakeAgentProvider) DownloadArtifact(request *infrav1.DownloadRequest, stream grpc.ServerStreamingServer[infrav1.ArtifactChunk]) error {
	if err := server.authorize(stream.Context()); err != nil {
		return err
	}
	if err := stream.Send(&infrav1.ArtifactChunk{Path: server.artifact.Path, OutputHash: server.artifact.OutputHash, Data: server.data}); err != nil {
		return err
	}
	return stream.Send(&infrav1.ArtifactChunk{Path: server.artifact.Path, OutputHash: server.artifact.OutputHash, Offset: uint64(len(server.data)), Eof: true})
}

func (server *fakeAgentProvider) ReportState(ctx context.Context, report *infrav1.AgentStateReport) (*infrav1.ReportAck, error) {
	if err := server.authorize(ctx); err != nil {
		return nil, err
	}
	server.report = report
	return &infrav1.ReportAck{Accepted: true}, nil
}

func (server *fakeAgentProvider) ReportLogs(ctx context.Context, batch *infrav1.AgentLogBatch) (*infrav1.AgentLogAck, error) {
	if err := server.authorize(ctx); err != nil {
		return nil, err
	}
	if batch.GetAgentId() != "agent-01" {
		return nil, status.Error(codes.FailedPrecondition, "agent identity mismatch")
	}
	server.logBatches = append(server.logBatches, batch)
	if server.releasePath != "" {
		for _, encoded := range batch.GetEventsJson() {
			var event observability.Event
			if json.Unmarshal(encoded, &event) == nil && event.Event == "process.output" {
				_ = os.WriteFile(server.releasePath, nil, 0o600)
				break
			}
		}
	}
	return &infrav1.AgentLogAck{AcceptedCount: uint32(len(batch.GetEventsJson()))}, nil
}

func writeCLIArtifact(t *testing.T, root, relativePath, contents string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func installCLITool(t *testing.T, name string) {
	t.Helper()
	directory := t.TempDir()
	filename := filepath.Join(directory, name)
	if err := os.WriteFile(filename, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}
