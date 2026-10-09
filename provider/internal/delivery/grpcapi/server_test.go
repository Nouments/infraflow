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
	"infraflow/internal/adapters/generation"
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
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.DataLoss {
		t.Fatalf("expected hash mismatch to abort before final EOF, got %v", err)
	}
}
