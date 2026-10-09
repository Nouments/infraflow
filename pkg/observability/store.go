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
	mu       sync.RWMutex
	path     string
	maxBytes int64
	fileSize int64
	events   []Event
	byID     map[string]Event
}

func NewStore(directory string, maxBytes int64) (*Store, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, fmt.Errorf("central log directory is required")
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
	store := &Store{path: path, maxBytes: maxBytes, byID: make(map[string]Event)}
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
		store.events = append(store.events, event)
		store.byID[event.ID] = event
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read central log store: %w", err)
	}
	store.fileSize = fileInfo.Size()
	return store, nil
}

func (store *Store) Append(events []Event) (AppendResult, error) {
	if len(events) == 0 || len(events) > MaxIngestBatch {
		return AppendResult{}, fmt.Errorf("log batch must contain between 1 and %d events", MaxIngestBatch)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	result := AppendResult{}
	for _, input := range events {
		event, err := NormalizeEvent(input)
		if err != nil {
			return result, err
		}
		if existing, exists := store.byID[event.ID]; exists {
			oldData, _ := json.Marshal(existing)
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
			return result, fmt.Errorf("central log store reached its %d-byte limit", store.maxBytes)
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
		store.events = append(store.events, event)
		store.byID[event.ID] = event
		result.Accepted++
	}
	return result, nil
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
