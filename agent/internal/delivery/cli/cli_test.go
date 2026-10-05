package cli

import (
	"bytes"
	"context"
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
	configPath := filepath.Join(t.TempDir(), "agent.yaml")
	config := strings.Join([]string{
		"agent:", "  id: agent-01", "  state_directory: " + output,
		"provider:", "  address: " + listener.Addr().String(), "  token_env: INFRAFLOW_TEST_TOKEN",
		"  tls:", "    enabled: false", "",
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
}

func TestAgentCLIHasNoProviderImplementationCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"generate"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("agent must not expose provider commands: %d %q", code, stderr.String())
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

type fakeAgentProvider struct {
	infrav1.UnimplementedAgentProviderServer
	token    string
	artifact protocol.Artifact
	data     []byte
	report   *infrav1.AgentStateReport
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
