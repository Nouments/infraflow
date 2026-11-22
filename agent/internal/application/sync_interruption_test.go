package application

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"infraflow/agent/internal/adapters/filesystem"
	"infraflow/agent/internal/adapters/providergrpc"
	"infraflow/pkg/protocol"
	infrav1 "infraflow/pkg/protocol/infraflow/v1"
)

// AGT-10 integration tests. They use the real Agent gRPC client, the real
// on-disk state store and a real TCP connection that is cut in the middle of
// an artifact stream. Nothing here is mocked below the network socket.

const interruptedArtifactPath = "lab/inventory.json"

type streamingProvider struct {
	infrav1.UnimplementedAgentProviderServer
	payload []byte
	reports atomic.Int32
}

func (provider *streamingProvider) ListArtifacts(context.Context, *infrav1.Empty) (*infrav1.ArtifactCatalog, error) {
	return &infrav1.ArtifactCatalog{Artifacts: []*infrav1.ArtifactRecord{{
		Type: "inventory", Path: interruptedArtifactPath, InputHash: strings.Repeat("1", 64), OutputHash: protocol.SHA256(provider.payload),
	}}}, nil
}

func (provider *streamingProvider) DownloadArtifact(request *infrav1.DownloadRequest, stream grpc.ServerStreamingServer[infrav1.ArtifactChunk]) error {
	hash := protocol.SHA256(provider.payload)
	const chunkSize = 16 << 10
	for offset := 0; offset < len(provider.payload); offset += chunkSize {
		end := min(offset+chunkSize, len(provider.payload))
		if err := stream.Send(&infrav1.ArtifactChunk{Path: request.GetPath(), OutputHash: hash, Offset: uint64(offset), Data: provider.payload[offset:end]}); err != nil {
			return err
		}
	}
	return stream.Send(&infrav1.ArtifactChunk{Path: request.GetPath(), OutputHash: hash, Offset: uint64(len(provider.payload)), Eof: true})
}

func (provider *streamingProvider) ReportState(context.Context, *infrav1.AgentStateReport) (*infrav1.ReportAck, error) {
	provider.reports.Add(1)
	return &infrav1.ReportAck{Accepted: true}, nil
}

// cuttingProxy forwards TCP traffic and, while armed, closes both sides once
// it has forwarded cutAfter bytes from the server to the client.
type cuttingProxy struct {
	listener net.Listener
	target   string
	cutAfter atomic.Int64
	cuts     atomic.Int32
	wg       sync.WaitGroup
}

func startCuttingProxy(t *testing.T, target string) *cuttingProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxy := &cuttingProxy{listener: listener, target: target}
	proxy.wg.Add(1)
	go proxy.serve()
	t.Cleanup(func() { _ = listener.Close(); proxy.wg.Wait() })
	return proxy
}

func (proxy *cuttingProxy) serve() {
	defer proxy.wg.Done()
	for {
		client, err := proxy.listener.Accept()
		if err != nil {
			return
		}
		server, err := net.Dial("tcp", proxy.target)
		if err != nil {
			_ = client.Close()
			continue
		}
		closeBoth := sync.OnceFunc(func() { _ = client.Close(); _ = server.Close() })
		proxy.wg.Add(2)
		go func() { defer proxy.wg.Done(); _, _ = io.Copy(server, client); closeBoth() }()
		go func() {
			defer proxy.wg.Done()
			defer closeBoth()
			forwarded := int64(0)
			buffer := make([]byte, 4096)
			for {
				read, err := server.Read(buffer)
				if read > 0 {
					limit := proxy.cutAfter.Load()
					if limit > 0 && forwarded+int64(read) > limit {
						_, _ = client.Write(buffer[:limit-forwarded])
						proxy.cuts.Add(1)
						return
					}
					if _, writeErr := client.Write(buffer[:read]); writeErr != nil {
						return
					}
					forwarded += int64(read)
				}
				if err != nil {
					return
				}
			}
		}()
	}
}

func newInterruptionLab(t *testing.T, payload []byte) (*streamingProvider, *cuttingProxy) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	provider := &streamingProvider{payload: payload}
	server := grpc.NewServer()
	infrav1.RegisterAgentProviderServer(server, provider)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return provider, startCuttingProxy(t, listener.Addr().String())
}

// newAgentRun builds a fresh client and runner, as a restarted Agent process
// would: nothing is shared with the previous run except the state directory.
func newAgentRun(t *testing.T, address, stateRoot string) (*Runner, func()) {
	t.Helper()
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	client := providergrpc.NewWithConnection(connection, strings.Repeat("t", 32), "agent-lab")
	runner, err := NewRunner("agent-lab", client, filesystem.NewStateStore(stateRoot), &fakeProcessor{status: protocol.StatusCompleted})
	if err != nil {
		t.Fatal(err)
	}
	return runner, func() { _ = client.Close() }
}

func testPayload(fill byte) []byte {
	return bytes.Repeat([]byte{fill}, 512<<10)
}

func stagedFiles(t *testing.T, root string) []string {
	t.Helper()
	var staged []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(entry.Name(), ".infraflow-write-") {
			staged = append(staged, path)
		}
		return nil
	})
	return staged
}

func TestInterruptedSyncNeverPublishesAPartialArtifact(t *testing.T) {
	provider, proxy := newInterruptionLab(t, testPayload('A'))
	stateRoot := t.TempDir()
	target := filepath.Join(stateRoot, interruptedArtifactPath)

	// 1. Connection cut after ~100 KiB of a 512 KiB artifact.
	proxy.cutAfter.Store(100 << 10)
	runner, closeRun := newAgentRun(t, proxy.listener.Addr().String(), stateRoot)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	report, err := runner.Run(ctx)
	cancel()
	closeRun()
	if err == nil {
		t.Fatal("an interrupted synchronization must fail")
	}
	if proxy.cuts.Load() == 0 {
		t.Fatal("the proxy never cut the stream; the test did not exercise an interruption")
	}
	if _, statErr := os.Stat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a partial artifact was published at %s: %v", target, statErr)
	}
	if staged := stagedFiles(t, stateRoot); len(staged) != 0 {
		t.Fatalf("temporary files left behind: %v", staged)
	}
	for _, result := range report.Artifacts {
		if result.Status == protocol.StatusCompleted {
			t.Fatalf("interrupted artifact reported completed: %+v", result)
		}
	}

	// 2. Agent restarts, network is back: the artifact is fetched whole and verified.
	proxy.cutAfter.Store(0)
	runner, closeRun = newAgentRun(t, proxy.listener.Addr().String(), stateRoot)
	defer closeRun()
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	report, err = runner.Run(ctx)
	if err != nil {
		t.Fatalf("resynchronization after reconnect failed: %v", err)
	}
	stored, err := os.ReadFile(target)
	if err != nil || protocol.SHA256(stored) != protocol.SHA256(provider.payload) {
		t.Fatalf("resynchronized artifact does not match the published hash: %v", err)
	}
	if len(report.Artifacts) != 1 || report.Artifacts[0].Status != protocol.StatusCompleted {
		t.Fatalf("unexpected report after resync: %+v", report.Artifacts)
	}
}

func TestInterruptedSyncKeepsThePreviousVerifiedVersion(t *testing.T) {
	provider, proxy := newInterruptionLab(t, testPayload('A'))
	stateRoot := t.TempDir()
	target := filepath.Join(stateRoot, interruptedArtifactPath)

	runner, closeRun := newAgentRun(t, proxy.listener.Addr().String(), stateRoot)
	if _, err := runner.Run(context.Background()); err != nil {
		closeRun()
		t.Fatalf("initial sync: %v", err)
	}
	closeRun()
	previous, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}

	// The Provider publishes a new version; its transfer is cut.
	provider.payload = testPayload('B')
	proxy.cutAfter.Store(64 << 10)
	runner, closeRun = newAgentRun(t, proxy.listener.Addr().String(), stateRoot)
	defer closeRun()
	if _, err := runner.Run(context.Background()); err == nil {
		t.Fatal("interrupted update must fail")
	}
	current, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(current, previous) {
		t.Fatalf("previous verified version was replaced or damaged by an interrupted update: %v", err)
	}
	if staged := stagedFiles(t, stateRoot); len(staged) != 0 {
		t.Fatalf("temporary files left behind: %v", staged)
	}
}

func TestSyncRejectsAStreamThatEndsEarlyWithoutTransportError(t *testing.T) {
	// A Provider (or a middlebox) that closes the stream cleanly before the
	// final chunk must not produce a "complete" artifact either.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	payload := testPayload('C')
	server := grpc.NewServer()
	infrav1.RegisterAgentProviderServer(server, &truncatingProvider{streamingProvider{payload: payload}})
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	stateRoot := t.TempDir()
	runner, closeRun := newAgentRun(t, listener.Addr().String(), stateRoot)
	defer closeRun()
	report, err := runner.Run(context.Background())
	if err == nil {
		t.Fatal("truncated stream accepted")
	}
	if _, statErr := os.Stat(filepath.Join(stateRoot, interruptedArtifactPath)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("truncated artifact published: %v", statErr)
	}
	if len(report.Artifacts) != 1 || !strings.Contains(report.Artifacts[0].Message, "without a final chunk") {
		t.Fatalf("failure class not reported: %+v", report.Artifacts)
	}
}

type truncatingProvider struct{ streamingProvider }

func (provider *truncatingProvider) DownloadArtifact(request *infrav1.DownloadRequest, stream grpc.ServerStreamingServer[infrav1.ArtifactChunk]) error {
	hash := protocol.SHA256(provider.payload)
	return stream.Send(&infrav1.ArtifactChunk{Path: request.GetPath(), OutputHash: hash, Data: provider.payload[:1024]})
}
