package executors

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"infraflow/provider/internal/infrastructure/credentials"

	"golang.org/x/crypto/ssh"
)

type transportTestWriter struct {
	mu     sync.Mutex
	closed bool
	writes bytes.Buffer
}

func (writer *transportTestWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.closed {
		return 0, ErrSessionClosed
	}
	return writer.writes.Write(data)
}

func (writer *transportTestWriter) Close() error {
	writer.mu.Lock()
	writer.closed = true
	writer.mu.Unlock()
	return nil
}

type transportTestReader struct {
	reader  io.Reader
	started chan struct{}
	once    sync.Once
}

func (reader *transportTestReader) Read(buffer []byte) (int, error) {
	reader.once.Do(func() { close(reader.started) })
	return reader.reader.Read(buffer)
}

func newPipeTransportSession(timeout time.Duration) (*liveCiscoIOSSession, *io.PipeWriter, *transportTestReader, *atomic.Int32) {
	pipeReader, pipeWriter := io.Pipe()
	reader := &transportTestReader{reader: pipeReader, started: make(chan struct{})}
	writer := &transportTestWriter{}
	resourceCloseCount := &atomic.Int32{}
	session := newLiveCiscoIOSSession(writer, reader, timeout, func() error {
		resourceCloseCount.Add(1)
		_ = pipeWriter.Close()
		return pipeReader.Close()
	})
	return session, pipeWriter, reader, resourceCloseCount
}

func awaitTransportSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func TestSSHSessionCloseReleasesBlockedReader(t *testing.T) {
	session, _, reader, closeCount := newPipeTransportSession(time.Second)
	awaitTransportSignal(t, reader.started, "reader to block")

	if err := session.Close(); err != nil {
		t.Fatalf("Close returned an error: %v", err)
	}
	awaitTransportSignal(t, session.readerDone, "reader shutdown")
	if closeCount.Load() != 1 {
		t.Fatalf("expected resources to close once, got %d", closeCount.Load())
	}
}

func TestSSHSessionNormalCloseStopsContextWatcher(t *testing.T) {
	session, _, reader, _ := newPipeTransportSession(time.Second)
	awaitTransportSignal(t, reader.started, "reader to block")
	ctx, cancel := context.WithCancel(context.Background())
	session.watchContext(ctx)
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	cancel()
	session.watchMu.Lock()
	watcherDetached := session.stopContextWatch == nil
	session.watchMu.Unlock()
	if !watcherDetached {
		t.Fatal("normal Close retained its context watcher")
	}
}

func TestSSHSessionContextCancellationClosesResources(t *testing.T) {
	session, _, reader, closeCount := newPipeTransportSession(time.Second)
	awaitTransportSignal(t, reader.started, "reader to block")
	ctx, cancel := context.WithCancel(context.Background())
	session.watchContext(ctx)
	cancel()

	awaitTransportSignal(t, session.readerDone, "reader shutdown after cancellation")
	if !session.closed.Load() || closeCount.Load() != 1 {
		t.Fatalf("cancellation did not close the session exactly once: closed=%v closes=%d", session.closed.Load(), closeCount.Load())
	}
}

func TestSSHSessionPromptTimeoutClosesResources(t *testing.T) {
	session, _, reader, closeCount := newPipeTransportSession(20 * time.Millisecond)
	awaitTransportSignal(t, reader.started, "reader to block")

	_, err := session.RunCommand(context.Background(), "show version")
	if !errors.Is(err, ErrPromptTimeout) {
		t.Fatalf("expected prompt timeout, got %v", err)
	}
	if !session.closed.Load() || closeCount.Load() != 1 {
		t.Fatalf("timeout did not close the session: closed=%v closes=%d", session.closed.Load(), closeCount.Load())
	}
}

func startSilentSSHPeer(t *testing.T) (net.Listener, <-chan struct{}, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan struct{})
	closed := make(chan struct{})
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			close(accepted)
			close(closed)
			return
		}
		close(accepted)
		_, _ = io.Copy(io.Discard, connection)
		_ = connection.Close()
		close(closed)
	}()
	return listener, accepted, closed
}

func TestSSHConnectTimeoutClosesHandshakeSocket(t *testing.T) {
	listener, accepted, closed := startSilentSSHPeer(t)
	defer listener.Close()
	transport, err := NewSSHIOSTransport(NewStaticPublicKeyVerifier(map[string]ssh.PublicKey{}), 30*time.Millisecond, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().(*net.TCPAddr)
	_, err = transport.Connect(context.Background(), AuthorizedDevice{ManagementIP: "127.0.0.1", SSHPort: address.Port}, credentials.DeviceCredentials{Username: "test", Password: "placeholder"})
	if err == nil {
		t.Fatal("expected silent SSH peer to time out")
	}
	awaitTransportSignal(t, accepted, "SSH peer acceptance")
	awaitTransportSignal(t, closed, "handshake socket closure")
}

func TestSSHConnectCancellationClosesHandshakeSocket(t *testing.T) {
	listener, accepted, closed := startSilentSSHPeer(t)
	defer listener.Close()
	transport, err := NewSSHIOSTransport(NewStaticPublicKeyVerifier(map[string]ssh.PublicKey{}), time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, connectErr := transport.Connect(ctx, AuthorizedDevice{ManagementIP: "127.0.0.1", SSHPort: address.Port}, credentials.DeviceCredentials{Username: "test", Password: "placeholder"})
		result <- connectErr
	}()
	awaitTransportSignal(t, accepted, "SSH peer acceptance")
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("expected cancelled SSH handshake to fail")
		}
	case <-time.After(time.Second):
		t.Fatal("SSH handshake did not stop after cancellation")
	}
	awaitTransportSignal(t, closed, "cancelled handshake socket closure")
}

func TestSSHSessionInterruptedReadClosesResources(t *testing.T) {
	session, pipeWriter, reader, closeCount := newPipeTransportSession(time.Second)
	awaitTransportSignal(t, reader.started, "reader to block")
	writeDone := make(chan struct{})
	go func() {
		_, _ = io.WriteString(pipeWriter, "partial output")
		close(writeDone)
	}()
	awaitTransportSignal(t, writeDone, "partial output write")
	_ = pipeWriter.Close()

	_, err := session.ReadInitialPrompt(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected interrupted read error, got %v", err)
	}
	if !session.closed.Load() || closeCount.Load() != 1 {
		t.Fatalf("read interruption did not close resources: closed=%v closes=%d", session.closed.Load(), closeCount.Load())
	}
}

func TestSSHSessionConcurrentCloseIsIdempotent(t *testing.T) {
	session, _, reader, closeCount := newPipeTransportSession(time.Second)
	awaitTransportSignal(t, reader.started, "reader to block")

	const closeCalls = 32
	var wait sync.WaitGroup
	wait.Add(closeCalls)
	for range closeCalls {
		go func() {
			defer wait.Done()
			if err := session.Close(); err != nil {
				t.Errorf("concurrent Close returned an error: %v", err)
			}
		}()
	}
	wait.Wait()
	if closeCount.Load() != 1 {
		t.Fatalf("expected one resource close, got %d", closeCount.Load())
	}
}

func TestSSHSessionRejectsCommandsAfterClose(t *testing.T) {
	session, _, _, _ := newPipeTransportSession(time.Second)
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.RunCommand(context.Background(), "show version"); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("expected closed-session error, got %v", err)
	}
}

func TestSSHSessionOutputLimitFailsAndCloses(t *testing.T) {
	session, pipeWriter, reader, closeCount := newPipeTransportSession(time.Second)
	awaitTransportSignal(t, reader.started, "reader to block")
	writeDone := make(chan struct{})
	go func() {
		_, _ = pipeWriter.Write(bytes.Repeat([]byte{'x'}, maxCiscoCommandOutputBytes+1))
		close(writeDone)
	}()

	_, err := session.ReadInitialPrompt(context.Background())
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("expected output limit error, got %v", err)
	}
	if !session.closed.Load() || closeCount.Load() != 1 {
		t.Fatalf("output limit did not close resources: closed=%v closes=%d", session.closed.Load(), closeCount.Load())
	}
	select {
	case <-writeDone:
	case <-time.After(time.Second):
		t.Fatal("oversized output writer remained blocked")
	}
}

func TestSSHSessionDetectsOnlyFinalPrompt(t *testing.T) {
	input := "\x1b[32mIOS banner\x1b[0m\r\nRouter#\r\ncontinuation output\r\nRouter(config)#   "
	session := newLiveCiscoIOSSession(&transportTestWriter{}, strings.NewReader(input), time.Second, func() error { return nil })
	defer session.Close()

	output, err := session.ReadInitialPrompt(context.Background())
	if err != nil {
		t.Fatalf("expected final prompt, got %v", err)
	}
	if !strings.Contains(output, "Router#\ncontinuation output") || !isFinalCiscoPrompt(output) {
		t.Fatalf("did not preserve intermediate prompt or normalize final prompt: %q", output)
	}
	if isFinalCiscoPrompt("") || isFinalCiscoPrompt("Router#\n") || isFinalCiscoPrompt("banner\nRouter#\nmore output") {
		t.Fatal("empty output or a prompt in the middle was accepted as a final prompt")
	}
	if _, ok := finalCiscoPrompt("Router#", "Router(config)#"); ok {
		t.Fatal("a prompt from a different IOS mode was accepted")
	}
}

func TestSSHSessionPreservesIOSError(t *testing.T) {
	input := "% Invalid input detected at '^' marker\r\nRouter(config)#"
	reader := strings.NewReader(input)
	session := newLiveCiscoIOSSession(&transportTestWriter{}, reader, time.Second, func() error { return nil })
	defer session.Close()

	output, err := session.RunCommand(context.Background(), "bad command")
	if !errors.Is(err, ErrCommandRejected) || !strings.Contains(err.Error(), "Invalid input") || !strings.Contains(output, "Invalid input") {
		t.Fatalf("IOS error was not preserved: output=%q err=%v", output, err)
	}
}

func TestSSHSessionEmptyOutputAndClosedConnectionFail(t *testing.T) {
	session := newLiveCiscoIOSSession(&transportTestWriter{}, strings.NewReader(""), time.Second, func() error { return nil })
	defer session.Close()
	if _, err := session.ReadInitialPrompt(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF for empty closed connection, got %v", err)
	}
}
