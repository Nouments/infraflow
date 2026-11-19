package executors

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// DeviceLocks serializes mutations of one device across executions. Resource
// locks in an execution request are optional and only order tasks inside that
// request; this lock is enforced by the executors themselves, so two
// concurrent executions can never interleave commands on the same device.
type DeviceLocks struct {
	mu    sync.Mutex
	slots map[string]chan struct{}
}

// sharedDeviceLocks is used by every device executor in the process.
var sharedDeviceLocks = &DeviceLocks{}

// Acquire blocks until the device is free or ctx ends.
func (l *DeviceLocks) Acquire(ctx context.Context, site, device string) (func(), error) {
	key := strings.ToLower(site + "/" + device)
	l.mu.Lock()
	if l.slots == nil {
		l.slots = map[string]chan struct{}{}
	}
	slot, ok := l.slots[key]
	if !ok {
		slot = make(chan struct{}, 1)
		l.slots[key] = slot
	}
	l.mu.Unlock()
	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("device %s is being configured by another execution: %w", key, ctx.Err())
	}
}

// NeverRetry marks executors whose failures may leave a device partially
// changed; the execution service refuses automatic retry policies for them.
func (e *CiscoIOSConfigureExecutor) NeverRetry() bool { return true }

// NeverRetry implements the same contract for RouterOS imports.
func (e *RouterOSImportExecutor) NeverRetry() bool { return true }
