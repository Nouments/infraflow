package observability

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"infraflow/internal/infrastructure/safefs"
)

const (
	DefaultCentralLogLimit = 64 << 20
	MaxQueryLimit          = 1000
	MaxIngestBatch         = 100
)

var ErrCursorExpired = errors.New("log cursor is not retained")

type Query struct {
	After    string
	Before   string
	Limit    int
	Level    string
	Service  string
	Hostname string
	SiteID   string
	AgentID  string
	DeviceID string
	RunID    string
	JobID    string
	TaskID   string
	Since    time.Time
	Until    time.Time
	Text     string
}

type Page struct {
	Events   []Event `json:"events"`
	Next     string  `json:"next,omitempty"`
	Previous string  `json:"previous,omitempty"`
}

type AppendResult struct {
	Accepted  int `json:"accepted"`
	Duplicate int `json:"duplicate"`
}

type Store struct {
	mu        sync.RWMutex
	path      string
	maxBytes  int64
	fileSize  int64
	retention time.Duration
	events    []Event
	// byID maps an event ID to its index in events. Indexes stay valid
	// because events is only appended to or replaced by rewriteLocked,
	// which rebuilds this map; storing a second copy of each Event here
	// doubled the store's memory footprint (D13).
	byID map[string]int
	// oldest caches the earliest stored timestamp so that retention purges
	// only scan the store when something can actually have expired. It is
	// invalidated by every rewrite and rebuilt by the next full scan.
	oldest      time.Time
	oldestKnown bool
}

func NewStore(directory string, maxBytes int64) (*Store, error) {
	return NewStoreWithRetention(directory, maxBytes, 0)
}

func NewStoreWithRetention(directory string, maxBytes int64, retention time.Duration) (*Store, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, fmt.Errorf("central log directory is required")
	}
	if retention < 0 {
		return nil, fmt.Errorf("central log retention must not be negative")
	}
	if maxBytes <= 0 {
		maxBytes = DefaultCentralLogLimit
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create central log directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, fmt.Errorf("secure central log directory: %w", err)
	}
	path := filepath.Join(directory, "central-events.jsonl")
	store := &Store{path: path, maxBytes: maxBytes, retention: retention, byID: make(map[string]int)}
	fileInfo, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect central log store: %w", err)
	}
	if !fileInfo.Mode().IsRegular() || fileInfo.Size() > maxBytes {
		return nil, fmt.Errorf("central log store is not a regular file or exceeds %d bytes", maxBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open central log store: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), MaxEventBytes+1)
	for scanner.Scan() {
		var event Event
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&event); err != nil {
			return nil, fmt.Errorf("decode central log event: %w", err)
		}
		if _, err := NormalizeEvent(event); err != nil {
			return nil, fmt.Errorf("validate central log event: %w", err)
		}
		if _, exists := store.byID[event.ID]; exists {
			return nil, fmt.Errorf("duplicate central log event id %q", event.ID)
		}
		store.byID[event.ID] = len(store.events)
		store.events = append(store.events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read central log store: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close central log store after reading: %w", err)
	}
	store.fileSize = fileInfo.Size()
	if retention > 0 {
		if _, err := store.Purge(time.Now()); err != nil {
			return nil, fmt.Errorf("purge expired central logs: %w", err)
		}
	}
	return store, nil
}

func (store *Store) Append(events []Event) (AppendResult, error) {
	if len(events) == 0 || len(events) > MaxIngestBatch {
		return AppendResult{}, fmt.Errorf("log batch must contain between 1 and %d events", MaxIngestBatch)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.retention > 0 {
		if _, err := store.purgeLocked(time.Now()); err != nil {
			return AppendResult{}, fmt.Errorf("purge expired central logs: %w", err)
		}
	}
	result := AppendResult{}
	for _, input := range events {
		event, err := NormalizeEvent(input)
		if err != nil {
			return result, err
		}
		if index, exists := store.byID[event.ID]; exists {
			oldData, _ := json.Marshal(store.events[index])
			newData, _ := json.Marshal(event)
			if !bytes.Equal(oldData, newData) {
				return result, fmt.Errorf("log event id %q was reused with different content", event.ID)
			}
			result.Duplicate++
			continue
		}
		data, err := json.Marshal(event)
		if err != nil {
			return result, fmt.Errorf("encode central log event: %w", err)
		}
		if len(data) > MaxEventBytes {
			return result, fmt.Errorf("central log event exceeds %d bytes", MaxEventBytes)
		}
		if store.fileSize+int64(len(data)+1) > store.maxBytes {
			if _, err := store.compactLocked(store.maxBytes * 3 / 4); err != nil {
				return result, fmt.Errorf("compact central log store: %w", err)
			}
			if store.fileSize+int64(len(data)+1) > store.maxBytes {
				return result, fmt.Errorf("central log store reached its %d-byte limit", store.maxBytes)
			}
		}
		file, err := os.OpenFile(store.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return result, fmt.Errorf("open central log store for append: %w", err)
		}
		if err := file.Chmod(0o600); err != nil {
			_ = file.Close()
			return result, fmt.Errorf("secure central log file: %w", err)
		}
		line := append(data, '\n')
		count, writeErr := file.Write(line)
		if writeErr == nil && count != len(line) {
			writeErr = io.ErrShortWrite
		}
		if writeErr == nil {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return result, fmt.Errorf("persist central log event: %w", errors.Join(writeErr, closeErr))
		}
		store.fileSize += int64(len(line))
		store.byID[event.ID] = len(store.events)
		store.events = append(store.events, event)
		store.noteAppendedLocked(event)
		result.Accepted++
	}
	return result, nil
}

func (store *Store) Purge(now time.Time) (int, error) {
	if store == nil {
		return 0, fmt.Errorf("central log store is not configured")
	}
	if store.retention <= 0 {
		return 0, nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.purgeLocked(now)
}

func (store *Store) purgeLocked(now time.Time) (int, error) {
	cutoff := now.Add(-store.retention)
	if len(store.events) == 0 || (store.oldestKnown && !store.oldest.Before(cutoff)) {
		return 0, nil
	}
	retained := make([]Event, 0, len(store.events))
	var oldest time.Time
	for _, event := range store.events {
		timestamp, err := time.Parse(time.RFC3339Nano, event.Timestamp)
		if err != nil {
			return 0, fmt.Errorf("parse stored log timestamp: %w", err)
		}
		if !timestamp.Before(cutoff) {
			retained = append(retained, event)
			if oldest.IsZero() || timestamp.Before(oldest) {
				oldest = timestamp
			}
		}
	}
	removed := len(store.events) - len(retained)
	if removed > 0 {
		if err := store.rewriteLocked(retained); err != nil {
			return 0, err
		}
	}
	store.oldest, store.oldestKnown = oldest, len(retained) > 0
	return removed, nil
}

// noteAppendedLocked keeps the cached oldest timestamp exact after an append.
func (store *Store) noteAppendedLocked(event Event) {
	if !store.oldestKnown {
		return
	}
	timestamp, err := time.Parse(time.RFC3339Nano, event.Timestamp)
	if err != nil {
		store.oldestKnown = false
		return
	}
	if timestamp.Before(store.oldest) {
		store.oldest = timestamp
	}
}

// compactLocked drops the oldest events until the encoded store fits in
// target bytes. It keeps the central store bounded and writable instead of
// failing every later append once the size limit is reached.
func (store *Store) compactLocked(target int64) (int, error) {
	sizes := make([]int64, len(store.events))
	var total int64
	for index, event := range store.events {
		encoded, err := json.Marshal(event)
		if err != nil {
			return 0, fmt.Errorf("encode stored log event: %w", err)
		}
		sizes[index] = int64(len(encoded) + 1)
		total += sizes[index]
	}
	drop := 0
	for drop < len(store.events) && total > target {
		total -= sizes[drop]
		drop++
	}
	if drop == 0 {
		return 0, nil
	}
	retained := append([]Event(nil), store.events[drop:]...)
	if err := store.rewriteLocked(retained); err != nil {
		return 0, err
	}
	return drop, nil
}

func (store *Store) rewriteLocked(retained []Event) error {
	store.oldestKnown = false
	// bytes.Buffer, not strings.Builder: AtomicWrite takes []byte, and
	// converting a Builder's string would copy the whole store once more.
	var data bytes.Buffer
	for _, event := range retained {
		encoded, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("encode retained log event: %w", err)
		}
		data.Write(encoded)
		data.WriteByte('\n')
	}
	if int64(data.Len()) > store.maxBytes {
		return fmt.Errorf("retained log data exceeds %d-byte limit", store.maxBytes)
	}
	if err := safefs.AtomicWrite(filepath.Dir(store.path), filepath.Base(store.path), data.Bytes(), 0o600); err != nil {
		return fmt.Errorf("publish retained central logs: %w", err)
	}
	store.events = retained
	store.byID = make(map[string]int, len(retained))
	for index, event := range retained {
		store.byID[event.ID] = index
	}
	store.fileSize = int64(data.Len())
	return nil
}

func (store *Store) List(query Query) (Page, error) {
	if query.Limit == 0 {
		query.Limit = 100
	}
	if query.Limit < 1 || query.Limit > MaxQueryLimit {
		return Page{}, fmt.Errorf("log query limit must be between 1 and %d", MaxQueryLimit)
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	start := len(store.events) - query.Limit
	if start < 0 {
		start = 0
	}
	end := len(store.events)
	if query.After != "" {
		found := false
		for index, event := range store.events {
			if event.ID == query.After {
				start = index + 1
				found = true
				break
			}
		}
		if !found {
			return Page{}, ErrCursorExpired
		}
	} else if query.Before != "" {
		found := false
		for index, event := range store.events {
			if event.ID == query.Before {
				end = index
				start = end - query.Limit
				if start < 0 {
					start = 0
				}
				found = true
				break
			}
		}
		if !found {
			return Page{}, ErrCursorExpired
		}
	}
	page := Page{Events: make([]Event, 0, query.Limit)}
	for _, event := range store.events[start:end] {
		if !matches(event, query) {
			continue
		}
		if len(page.Events) == query.Limit {
			page.Next = page.Events[len(page.Events)-1].ID
			break
		}
		page.Events = append(page.Events, event)
	}
	if start > 0 && len(page.Events) > 0 {
		page.Previous = page.Events[0].ID
	}
	if end < len(store.events) && page.Next == "" && len(page.Events) > 0 {
		page.Next = page.Events[len(page.Events)-1].ID
	}
	return page, nil
}

func matches(event Event, query Query) bool {
	if query.Level != "" && !strings.EqualFold(event.Level, query.Level) ||
		query.Service != "" && event.Service != query.Service ||
		query.Hostname != "" && event.Hostname != query.Hostname ||
		query.SiteID != "" && event.SiteID != query.SiteID ||
		query.AgentID != "" && event.AgentID != query.AgentID ||
		query.DeviceID != "" && event.DeviceID != query.DeviceID ||
		query.RunID != "" && event.RunID != query.RunID ||
		query.JobID != "" && event.JobID != query.JobID ||
		query.TaskID != "" && event.TaskID != query.TaskID {
		return false
	}
	timestamp, _ := time.Parse(time.RFC3339Nano, event.Timestamp)
	if !query.Since.IsZero() && timestamp.Before(query.Since) || !query.Until.IsZero() && timestamp.After(query.Until) {
		return false
	}
	if query.Text != "" && !strings.Contains(strings.ToLower(event.Message+" "+event.Error+" "+event.Line+" "+event.Event), strings.ToLower(query.Text)) {
		return false
	}
	return true
}

// StoreSink adapts a central Store to the logger Sink interface so that a
// process's own log events are queryable through the same store that holds
// ingested Agent events.
type StoreSink struct{ Store *Store }

func (sink StoreSink) Append(event Event) error {
	if sink.Store == nil {
		return fmt.Errorf("central log store is not configured")
	}
	_, err := sink.Store.Append([]Event{event})
	return err
}
