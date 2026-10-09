package observability

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const DefaultOutboxLimit = 16 << 20

type Sink interface {
	Append(Event) error
}

type Outbox struct {
	mu       sync.Mutex
	path     string
	maxBytes int
	events   []Event
}

func NewOutbox(path string, maxBytes int) (*Outbox, error) {
	if filepath.Base(path) == "." || filepath.Base(path) == string(filepath.Separator) || path == "" {
		return nil, fmt.Errorf("outbox path is required")
	}
	if maxBytes <= 0 {
		maxBytes = DefaultOutboxLimit
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create outbox directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("secure outbox directory: %w", err)
	}
	outbox := &Outbox{path: path, maxBytes: maxBytes}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return outbox, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect event outbox: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > int64(maxBytes) {
		return nil, fmt.Errorf("event outbox is not a regular file or exceeds %d bytes", maxBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read event outbox: %w", err)
	}
	if len(data) == 0 {
		return outbox, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&outbox.events); err != nil {
		return nil, fmt.Errorf("decode event outbox: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("event outbox contains trailing JSON")
	}
	for _, event := range outbox.events {
		if _, err := NormalizeEvent(event); err != nil {
			return nil, fmt.Errorf("validate outbox event: %w", err)
		}
	}
	return outbox, nil
}

func (outbox *Outbox) Append(input Event) error {
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	event, err := NormalizeEvent(input)
	if err != nil {
		return err
	}
	for _, existing := range outbox.events {
		if existing.ID != event.ID {
			continue
		}
		oldData, _ := json.Marshal(existing)
		newData, _ := json.Marshal(event)
		if bytes.Equal(oldData, newData) {
			return nil
		}
		return fmt.Errorf("outbox event id %q was reused with different content", event.ID)
	}
	outbox.events = append(outbox.events, event)
	if err := outbox.persist(); err != nil {
		outbox.events = outbox.events[:len(outbox.events)-1]
		return err
	}
	return nil
}

func (outbox *Outbox) Pending(limit int) ([]Event, error) {
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	if limit <= 0 || limit > MaxIngestBatch {
		limit = MaxIngestBatch
	}
	if len(outbox.events) < limit {
		limit = len(outbox.events)
	}
	return append([]Event(nil), outbox.events[:limit]...), nil
}

func (outbox *Outbox) Ack(ids []string) error {
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	acknowledged := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		acknowledged[id] = struct{}{}
	}
	remaining := make([]Event, 0, len(outbox.events))
	for _, event := range outbox.events {
		if _, ok := acknowledged[event.ID]; !ok {
			remaining = append(remaining, event)
		}
	}
	previous := outbox.events
	outbox.events = remaining
	if err := outbox.persist(); err != nil {
		outbox.events = previous
		return err
	}
	return nil
}

func (outbox *Outbox) persist() error {
	data, err := json.Marshal(outbox.events)
	if err != nil {
		return fmt.Errorf("encode event outbox: %w", err)
	}
	if len(data) > outbox.maxBytes {
		return fmt.Errorf("event outbox reached its %d-byte limit", outbox.maxBytes)
	}
	file, err := os.CreateTemp(filepath.Dir(outbox.path), ".outbox-*.tmp")
	if err != nil {
		return fmt.Errorf("create outbox temporary file: %w", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("secure outbox temporary file: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write event outbox: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync event outbox: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close event outbox: %w", err)
	}
	if err := os.Rename(temporary, outbox.path); err != nil {
		return fmt.Errorf("publish event outbox: %w", err)
	}
	directory, err := os.Open(filepath.Dir(outbox.path))
	if err != nil {
		return fmt.Errorf("open outbox directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync outbox directory: %w", err)
	}
	return nil
}
