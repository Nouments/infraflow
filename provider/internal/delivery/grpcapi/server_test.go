package grpcapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	configadapter "infraflow/internal/adapters/config"
	generator "infraflow/internal/adapters/generation"
	planningadapter "infraflow/internal/adapters/planning"
	"infraflow/internal/infrastructure/security"
	"infraflow/pkg/observability"
	"infraflow/pkg/protocol"
	infrav1 "infraflow/pkg/protocol/infraflow/v1"
	"infraflow/provider/internal/adapters/filesystem"
	"infraflow/provider/internal/adapters/generation"
	"infraflow/provider/internal/application"
)

func TestGRPCStreamsArtifactsAndReportsState(t *testing.T) {
	root := t.TempDir()
	infrastructure, err := (configadapter.Parser{}).Parse([]byte("sites:\n  - name: lab\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generator.Generate(infrastructure, root); err != nil {
		t.Fatal(err)
	}
	reports, err := filesystem.NewReportStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewService(filesystem.NewArtifactRepository(root), reports, generation.Generator{}, application.Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}})
	token := strings.Repeat("g", security.MinAgentTokenBytes)
	server, err := NewServer(service, token, 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
		<-serveDone
	})
	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := infrav1.NewAgentProviderClient(connection)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	catalog, err := client.ListArtifacts(ctx, &infrav1.Empty{})
	if err != nil || len(catalog.GetArtifacts()) != 2 {
		t.Fatalf("unexpected gRPC catalog: %#v, %v", catalog, err)
	}

	for _, artifact := range catalog.GetArtifacts() {
		stream, err := client.DownloadArtifact(ctx, &infrav1.DownloadRequest{Path: artifact.GetPath()})
		if err != nil {
			t.Fatal(err)
		}
		var contents bytes.Buffer
		var expectedOffset uint64
		chunkCount := 0
		for {
			chunk, err := stream.Recv()
			if err == io.EOF {
				t.Fatal("stream ended without the explicit EOF chunk")
			}
			if err != nil {
				t.Fatal(err)
			}
			if chunk.GetPath() != artifact.GetPath() || chunk.GetOutputHash() != artifact.GetOutputHash() || chunk.GetOffset() != expectedOffset {
				t.Fatalf("invalid stream chunk metadata: %#v", chunk)
			}
			if chunk.GetEof() {
				break
			}
			if len(chunk.GetData()) > 7 {
				t.Fatalf("chunk exceeded configured size: %d", len(chunk.GetData()))
			}
			_, _ = contents.Write(chunk.GetData())
			expectedOffset += uint64(len(chunk.GetData()))
			chunkCount++
		}
		if chunkCount < 2 || protocol.SHA256(contents.Bytes()) != artifact.GetOutputHash() {
			t.Fatalf("stream was not chunked or did not verify for %s", artifact.GetPath())
		}
	}

	result := protocol.ArtifactResult{Path: catalog.Artifacts[0].Path, OutputHash: catalog.Artifacts[0].OutputHash, Status: protocol.StatusCompleted}
	report := protocol.AgentReport{AgentID: "agent-01", ReportedAt: time.Now().UTC(), Artifacts: []protocol.ArtifactResult{result}}
	report.ReportID = protocol.ComputeReportID(report)
	ack, err := client.ReportState(ctx, &infrav1.AgentStateReport{
		ReportId: report.ReportID, AgentId: report.AgentID, ReportedAt: timestamppb.New(report.ReportedAt),
		Artifacts: []*infrav1.ArtifactExecutionResult{{Path: result.Path, OutputHash: result.OutputHash, Status: result.Status}},
	})
	if err != nil || !ack.GetAccepted() || len(service.Reports()) != 1 {
		t.Fatalf("agent report not accepted: %#v, %v", ack, err)
	}
}

func TestGRPCRequiresOneAgentCredentialForEveryPublishedSite(t *testing.T) {
	root := t.TempDir()
	infrastructure, err := (configadapter.Parser{}).Parse([]byte(`sites:
  - name: site-a
    bootstrap:
      network: 192.0.2.0/24
      gateway: 192.0.2.1
  - name: site-b
    bootstrap:
      network: 198.51.100.0/24
      gateway: 198.51.100.1
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generator.GenerateAll(infrastructure, root); err != nil {
		t.Fatal(err)
	}
	service := application.NewService(
		filesystem.NewArtifactRepository(root), nil, generation.Generator{},
		application.Dependencies{},
	)
	_, err = NewServerWithAgentCredentials(
		service, strings.Repeat("g", security.MinAgentTokenBytes),
		[]AgentCredential{{
			AgentID: "agent-a", SiteID: "site-a", Token: strings.Repeat("a", security.MinAgentTokenBytes),
		}},
		DefaultChunkSize, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "site-b") {
		t.Fatalf("expected missing credential for published site-b to be rejected, got %v", err)
	}
	_, err = NewServerWithAgentCredentials(
		service, strings.Repeat("g", security.MinAgentTokenBytes),
		[]AgentCredential{
			{AgentID: "agent-a", SiteID: "site-a", Token: strings.Repeat("a", security.MinAgentTokenBytes)},
			{AgentID: "agent-b", SiteID: "site-b", Token: strings.Repeat("b", security.MinAgentTokenBytes)},
			{AgentID: "agent-c", SiteID: "site-c", Token: strings.Repeat("c", security.MinAgentTokenBytes)},
		},
		DefaultChunkSize, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "site-c") {
		t.Fatalf("expected credential for unpublished site-c to be rejected, got %v", err)
	}
}

func TestGRPCRejectsUnauthenticatedAgent(t *testing.T) {
	root := t.TempDir()
	infrastructure, err := (configadapter.Parser{}).Parse([]byte("sites:\n  - name: lab\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generator.Generate(infrastructure, root); err != nil {
		t.Fatal(err)
	}
	reports, err := filesystem.NewReportStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewService(filesystem.NewArtifactRepository(root), reports, generation.Generator{}, application.Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}})
	server, err := NewServer(service, strings.Repeat("g", security.MinAgentTokenBytes), DefaultChunkSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_, err = infrav1.NewAgentProviderClient(connection).ListArtifacts(context.Background(), &infrav1.Empty{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected unauthenticated status, got %v", err)
	}
}

func TestGRPCScopesAgentCatalogAndDownloadsToAuthorizedSite(t *testing.T) {
	root := t.TempDir()
	infrastructure, err := (configadapter.Parser{}).Parse([]byte(`sites:
  - name: site-a
    devices:
      - name: edge-a
  - name: site-b
    devices:
      - name: edge-b
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generator.Generate(infrastructure, root); err != nil {
		t.Fatal(err)
	}
	agents, err := filesystem.NewAgentStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithJobsAndAgents(filesystem.NewArtifactRepository(root), nil, nil, nil, agents, application.Dependencies{})
	for _, registered := range []application.AgentRegistration{
		{ID: "agent-a", SiteID: "site-a"},
		{ID: "agent-b", SiteID: "site-b"},
	} {
		if _, err := service.RegisterAgent(registered); err != nil {
			t.Fatal(err)
		}
	}
	credentials := []AgentCredential{
		{AgentID: "agent-a", SiteID: "site-a", Token: strings.Repeat("a", security.MinAgentTokenBytes)},
		{AgentID: "agent-b", SiteID: "site-b", Token: strings.Repeat("b", security.MinAgentTokenBytes)},
	}
	server, err := NewServerWithAgentCredentials(service, strings.Repeat("g", security.MinAgentTokenBytes), credentials, DefaultChunkSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close(); <-serveDone })
	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := infrav1.NewAgentProviderClient(connection)

	agentAContext := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer "+credentials[0].Token,
		"x-infraflow-agent-id", credentials[0].AgentID,
	))
	catalog, err := client.ListArtifacts(agentAContext, &infrav1.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.GetArtifacts()) != 2 {
		t.Fatalf("site-scoped agent received %d artifacts, want 2", len(catalog.GetArtifacts()))
	}
	for _, artifact := range catalog.GetArtifacts() {
		if !strings.HasPrefix(artifact.GetPath(), "site-a/") {
			t.Errorf("site-scoped catalog leaked another site's artifact %q", artifact.GetPath())
		}
	}
	_, err = client.ListArtifacts(metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer "+strings.Repeat("g", security.MinAgentTokenBytes),
	)), &infrav1.Empty{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("legacy global token remained valid after scoped credentials were configured: %v", err)
	}

	crossSiteStream, err := client.DownloadArtifact(agentAContext, &infrav1.DownloadRequest{Path: "site-b/inventory.json"})
	if err == nil {
		_, err = crossSiteStream.Recv()
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross-site download returned %v, want PermissionDenied", err)
	}
	published, err := service.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	var otherSite protocol.Artifact
	for _, artifact := range published {
		if strings.HasPrefix(artifact.Path, "site-b/") {
			otherSite = artifact
			break
		}
	}
	report := protocol.AgentReport{
		AgentID: "agent-a", ReportedAt: time.Now().UTC(),
		Artifacts: []protocol.ArtifactResult{{Path: otherSite.Path, OutputHash: otherSite.OutputHash, Status: protocol.StatusCompleted}},
	}
	report.ReportID = protocol.ComputeReportID(report)
	_, err = client.ReportState(agentAContext, &infrav1.AgentStateReport{
		ReportId: report.ReportID, AgentId: report.AgentID, ReportedAt: timestamppb.New(report.ReportedAt),
		Artifacts: []*infrav1.ArtifactExecutionResult{{Path: otherSite.Path, OutputHash: otherSite.OutputHash, Status: protocol.StatusCompleted}},
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross-site synchronization report returned %v, want PermissionDenied", err)
	}
	_, err = client.ListArtifacts(metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer "+credentials[0].Token,
		"x-infraflow-agent-id", credentials[1].AgentID,
	)), &infrav1.Empty{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("agent token accepted for a different identity: %v", err)
	}
}

func TestGRPCLegacyGlobalCredentialRefusesMultisiteCatalog(t *testing.T) {
	root := t.TempDir()
	infrastructure, err := (configadapter.Parser{}).Parse([]byte(`sites:
  - name: site-a
  - name: site-b
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generator.Generate(infrastructure, root); err != nil {
		t.Fatal(err)
	}
	service := application.NewService(filesystem.NewArtifactRepository(root), nil, nil, application.Dependencies{})
	token := strings.Repeat("g", security.MinAgentTokenBytes)
	server, err := NewServer(service, token, DefaultChunkSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close(); <-serveDone })
	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	_, err = infrav1.NewAgentProviderClient(connection).ListArtifacts(ctx, &infrav1.Empty{})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("global credential exposed a multi-site catalog: %v", err)
	}
}

func TestGRPCRejectsReuseOfGlobalTokenAsSiteCredential(t *testing.T) {
	token := strings.Repeat("g", security.MinAgentTokenBytes)
	_, err := NewServerWithAgentCredentials(
		application.NewService(nil, nil, nil, application.Dependencies{}),
		token,
		[]AgentCredential{{AgentID: "agent-a", SiteID: "site-a", Token: token}},
		DefaultChunkSize,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "must differ from the global machine token") {
		t.Fatalf("expected global/site token reuse to be rejected, got %v", err)
	}
}

func TestGRPCRejectsMultipleAgentsForOneSite(t *testing.T) {
	_, err := NewServerWithAgentCredentials(
		application.NewService(nil, nil, nil, application.Dependencies{}),
		strings.Repeat("g", security.MinAgentTokenBytes),
		[]AgentCredential{
			{AgentID: "agent-a", SiteID: "site-a", Token: strings.Repeat("a", security.MinAgentTokenBytes)},
			{AgentID: "agent-b", SiteID: "site-a", Token: strings.Repeat("b", security.MinAgentTokenBytes)},
		},
		DefaultChunkSize,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "site_id") {
		t.Fatalf("expected duplicate site assignment to be rejected, got %v", err)
	}
}

func TestGRPCScopedAgentLogsRejectIdentityAndSiteSpoofing(t *testing.T) {
	root := t.TempDir()
	logs, err := observability.NewStore(root, observability.DefaultCentralLogLimit)
	if err != nil {
		t.Fatal(err)
	}
	agents, err := filesystem.NewAgentStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithJobsAndAgents(filesystem.NewArtifactRepository(root), nil, nil, nil, agents, application.Dependencies{Logs: logs})
	if _, err := service.RegisterAgent(application.AgentRegistration{ID: "agent-a", SiteID: "site-a"}); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", security.MinAgentTokenBytes)
	server, err := NewServerWithAgentCredentials(
		service, strings.Repeat("g", security.MinAgentTokenBytes),
		[]AgentCredential{{AgentID: "agent-a", SiteID: "site-a", Token: token}},
		DefaultChunkSize, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close(); <-serveDone })
	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := infrav1.NewAgentProviderClient(connection)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer "+token,
		"x-infraflow-agent-id", "agent-a",
	))
	encoded, err := json.Marshal(observability.Event{
		ID: "scoped-log-01", Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level: "INFO", Event: "agent.test", Message: "scoped log test",
		AgentID: "agent-a", SiteID: "site-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReportLogs(ctx, &infrav1.AgentLogBatch{AgentId: "agent-b", EventsJson: [][]byte{encoded}}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("scoped token accepted a different log identity: %v", err)
	}
	if _, err := client.ReportLogs(ctx, &infrav1.AgentLogBatch{AgentId: "agent-a", EventsJson: [][]byte{encoded}}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("scoped token accepted a different log site: %v", err)
	}
}

func TestGRPCIngestsAgentLogsIdempotentlyAndRejectsIdentitySpoofing(t *testing.T) {
	root := t.TempDir()
	logs, err := observability.NewStore(root, observability.DefaultCentralLogLimit)
	if err != nil {
		t.Fatal(err)
	}
	agents, err := filesystem.NewAgentStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithJobsAndAgents(nil, nil, nil, nil, agents, application.Dependencies{Logs: logs})
	if _, err := service.RegisterAgent(application.AgentRegistration{ID: "agent-01", SiteID: "site-01"}); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("g", security.MinAgentTokenBytes)
	server, err := NewServer(service, token, DefaultChunkSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close(); <-serveDone })
	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := infrav1.NewAgentProviderClient(connection)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	event := observability.Event{
		ID: "log-event-01", Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level: "ERROR", Event: "process.exit", Message: "controlled test process exited 7",
		Service: "agent", Source: "process-runner", Hostname: "test-host",
		AgentID: "agent-01", SiteID: "site-01", RunID: "run-01", TaskID: "task-01",
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	request := &infrav1.AgentLogBatch{AgentId: "agent-01", EventsJson: [][]byte{encoded}}
	if _, err := client.ReportLogs(context.Background(), request); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated logs were not rejected: %v", err)
	}
	spoofed := event
	spoofed.AgentID = "agent-02"
	spoofedBytes, _ := json.Marshal(spoofed)
	if _, err := client.ReportLogs(ctx, &infrav1.AgentLogBatch{AgentId: "agent-01", EventsJson: [][]byte{spoofedBytes}}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("spoofed agent identity was not rejected: %v", err)
	}
	ack, err := client.ReportLogs(ctx, request)
	if err != nil || ack.GetAcceptedCount() != 1 || ack.GetDuplicateCount() != 0 {
		t.Fatalf("authenticated log event not persisted: %#v, %v", ack, err)
	}
	ack, err = client.ReportLogs(ctx, request)
	if err != nil || ack.GetAcceptedCount() != 0 || ack.GetDuplicateCount() != 1 {
		t.Fatalf("replayed event was not deduplicated: %#v, %v", ack, err)
	}
	page, err := logs.List(observability.Query{RunID: "run-01", Limit: 10})
	if err != nil || len(page.Events) != 1 || page.Events[0].AgentID != "agent-01" {
		t.Fatalf("central store does not contain the authenticated event: %#v, %v", page, err)
	}
}

func TestGRPCDoesNotCommitTamperedArtifactStream(t *testing.T) {
	root := t.TempDir()
	infrastructure, err := (configadapter.Parser{}).Parse([]byte("sites:\n  - name: lab\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generator.Generate(infrastructure, root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lab", "inventory.json"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	reports, err := filesystem.NewReportStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewService(filesystem.NewArtifactRepository(root), reports, generation.Generator{}, application.Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}})
	token := strings.Repeat("g", security.MinAgentTokenBytes)
	server, err := NewServer(service, token, DefaultChunkSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	stream, err := infrav1.NewAgentProviderClient(connection).DownloadArtifact(ctx, &infrav1.DownloadRequest{Path: "lab/inventory.json"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.DataLoss {
		t.Fatalf("expected corrupted artifact to be rejected before streaming, got %v", err)
	}
}

func TestGRPCRejectsOversizedArtifactTransfer(t *testing.T) {
	root := t.TempDir()
	infrastructure, err := (configadapter.Parser{}).Parse([]byte("sites:\n  - name: lab\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generator.Generate(infrastructure, root); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(filepath.Join(root, "lab", "inventory.json"), int64(protocol.MaxArtifactBytes)+1); err != nil {
		t.Fatal(err)
	}
	reports, err := filesystem.NewReportStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewService(filesystem.NewArtifactRepository(root), reports, generation.Generator{}, application.Dependencies{Parser: configadapter.Parser{}, PlanBuilder: planningadapter.Builder{}})
	token := strings.Repeat("g", security.MinAgentTokenBytes)
	server, err := NewServer(service, token, DefaultChunkSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	stream, err := infrav1.NewAgentProviderClient(connection).DownloadArtifact(ctx, &infrav1.DownloadRequest{Path: "lab/inventory.json"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("expected oversized artifact to be rejected, got %v", err)
	}
}
