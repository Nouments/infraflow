package grpcapi

import (
	"bytes"
	"context"
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
