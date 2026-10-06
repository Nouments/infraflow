package filesystem

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"infraflow/internal/domain"
	"infraflow/pkg/protocol"
)

const maxEventStoreBytes = 16 << 20

type EventStore struct {
	mu     sync.Mutex
	path   string
	events []domain.Event
	index  map[string]domain.Event
}

func NewEventStore(root string) (*EventStore, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect artifact directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("artifact root must be a directory")
	}
	store := &EventStore{path: filepath.Join(root, ".events.jsonl"), index: make(map[string]domain.Event)}
	fileInfo, err := os.Lstat(store.path)
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect event store: %w", err)
	}
	if !fileInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("event store must be a regular file")
	}
	file, err := os.Open(store.path)
	if err != nil {
		return nil, fmt.Errorf("open event store: %w", err)
	}
	defer file.Close()
	if fileInfo.Size() > maxEventStoreBytes {
		return nil, fmt.Errorf("event store exceeds %d bytes", maxEventStoreBytes)
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 128<<10)
	var previous string
	var expectedSequence uint64 = 1
	for scanner.Scan() {
		var event domain.Event
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&event); err != nil {
			return nil, fmt.Errorf("decode event: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return nil, fmt.Errorf("event contains trailing JSON")
		}
		if event.Sequence != expectedSequence || event.PreviousHash != previous {
			return nil, fmt.Errorf("event sequence or previous hash is invalid")
		}
		if err := validateEvent(event); err != nil {
			return nil, fmt.Errorf("validate stored event: %w", err)
		}
		if _, exists := store.index[event.EventID]; exists {
			return nil, fmt.Errorf("duplicate event id %q", event.EventID)
		}
		store.events = append(store.events, event)
		store.index[event.EventID] = event
		previous = event.Hash
		expectedSequence++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read event store: %w", err)
	}
	return store, nil
}

func (store *EventStore) Append(event domain.Event) (domain.Event, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if event.EventID == "" {
		return domain.Event{}, false, fmt.Errorf("event id is required")
	}
	if existing, exists := store.index[event.EventID]; exists {
		if event.Type != "" && (event.Type != existing.Type || string(event.Payload) != string(existing.Payload)) {
			return domain.Event{}, false, fmt.Errorf("event id already exists with different content")
		}
		return existing, false, nil
	}
	if event.Payload == nil {
		event.Payload = json.RawMessage(`{}`)
	}
	event.Sequence = uint64(len(store.events) + 1)
	event.PreviousHash = ""
	if len(store.events) > 0 {
		event.PreviousHash = store.events[len(store.events)-1].Hash
	}
	event.Hash = computeEventHash(event)
	if err := validateEvent(event); err != nil {
		return domain.Event{}, false, err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return domain.Event{}, false, fmt.Errorf("encode event: %w", err)
	}
	if currentSize := store.fileSize(); currentSize+int64(len(data))+1 > maxEventStoreBytes {
		return domain.Event{}, false, fmt.Errorf("event store exceeds %d bytes", maxEventStoreBytes)
	}
	file, err := os.OpenFile(store.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return domain.Event{}, false, fmt.Errorf("open event store for append: %w", err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return domain.Event{}, false, fmt.Errorf("write event: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return domain.Event{}, false, fmt.Errorf("sync event store: %w", err)
	}
	if err := file.Close(); err != nil {
		return domain.Event{}, false, fmt.Errorf("close event store: %w", err)
	}
	store.events = append(store.events, event)
	store.index[event.EventID] = event
	return event, true, nil
}

func (store *EventStore) List() []domain.Event {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]domain.Event(nil), store.events...)
}

func (store *EventStore) fileSize() int64 {
	info, err := os.Stat(store.path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func computeEventHash(event domain.Event) string {
	payload, _ := json.Marshal(struct {
		EventID   string          `json:"event_id"`
		SiteID    string          `json:"site_id,omitempty"`
		AgentID   string          `json:"agent_id,omitempty"`
		JobID     string          `json:"job_id,omitempty"`
		Sequence  uint64          `json:"sequence"`
		Timestamp time.Time       `json:"timestamp"`
		Type      string          `json:"type"`
		Payload   json.RawMessage `json:"payload"`
		Previous  string          `json:"previous_hash,omitempty"`
	}{
		EventID: event.EventID, SiteID: event.SiteID, AgentID: event.AgentID,
		JobID: event.JobID, Sequence: event.Sequence, Timestamp: event.Timestamp,
		Type: event.Type, Payload: event.Payload, Previous: event.PreviousHash,
	})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func validateEvent(event domain.Event) error {
	if !protocol.ValidSiteName(event.EventID) || len(event.EventID) > 128 {
		return fmt.Errorf("invalid event id")
	}
	for name, value := range map[string]string{"site id": event.SiteID, "agent id": event.AgentID, "job id": event.JobID} {
		if value != "" && (!protocol.ValidSiteName(value) || len(value) > 128) {
			return fmt.Errorf("invalid %s", name)
		}
	}
	if event.Sequence == 0 || event.Timestamp.IsZero() || !protocol.ValidSiteName(event.Type) || len(event.Type) > 128 {
		return fmt.Errorf("invalid event metadata")
	}
	if len(event.Payload) == 0 || len(event.Payload) > 64<<10 || !json.Valid(event.Payload) {
		return fmt.Errorf("invalid event payload")
	}
	if !protocol.IsSHA256(event.Hash) || (event.Sequence == 1 && event.PreviousHash != "") || (event.Sequence > 1 && !protocol.IsSHA256(event.PreviousHash)) {
		return fmt.Errorf("invalid event hash chain")
	}
	if event.Hash != computeEventHash(event) {
		return fmt.Errorf("event hash does not match payload")
	}
	return nil
}
