package executors

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"infraflow/internal/domain"
	"infraflow/provider/internal/infrastructure/credentials"
)

// slowSession counts how many sessions are mutating at the same time.
type slowSession struct {
	*mockCiscoSession
	active, peak *atomic.Int32
	mu           *sync.Mutex
}

func (s slowSession) RunCommand(ctx context.Context, command string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mockCiscoSession.RunCommand(ctx, command)
}

type countingTransport struct {
	active, peak atomic.Int32
	mu           sync.Mutex
}

func (t *countingTransport) Connect(ctx context.Context, _ AuthorizedDevice, _ credentials.DeviceCredentials) (CiscoIOSSession, error) {
	if n := t.active.Add(1); n > t.peak.Load() {
		t.peak.Store(n)
	}
	time.Sleep(30 * time.Millisecond)
	return &releasingSession{slowSession{standardMockSession(), &t.active, &t.peak, &t.mu}}, nil
}

type releasingSession struct{ slowSession }

func (s *releasingSession) Close() error {
	s.active.Add(-1)
	return nil
}

func TestConcurrentExecutionsNeverOverlapOnOneDevice(t *testing.T) {
	executor, _, _ := setupTestExecutor(standardMockSession(), nil)
	transport := &countingTransport{}
	executor.transport = transport
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := executor.Execute(context.Background(), domain.ExecutionTask{Parameters: json.RawMessage(validTaskPayload())})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("serialized execution failed: %v", err)
		}
	}
	if transport.peak.Load() != 1 {
		t.Fatalf("%d sessions configured the same device concurrently", transport.peak.Load())
	}
}

func TestDeviceLockWaitIsBoundedByContext(t *testing.T) {
	locks := &DeviceLocks{}
	release, err := locks.Acquire(context.Background(), "lab", "r1")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := locks.Acquire(ctx, "LAB", "R1"); err == nil || !strings.Contains(err.Error(), "being configured by another execution") {
		t.Fatalf("second acquisition must wait and then fail with the context, got %v", err)
	}
	other, err := locks.Acquire(context.Background(), "lab", "r2")
	if err != nil {
		t.Fatalf("different devices must not block each other: %v", err)
	}
	other()
}
