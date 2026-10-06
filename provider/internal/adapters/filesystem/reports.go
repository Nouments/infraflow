package filesystem

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"infraflow/pkg/protocol"
)

type ReportStore struct {
	mu      sync.Mutex
	path    string
	reports map[string]protocol.AgentReport
}

func NewReportStore(root string) (*ReportStore, error) {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect artifact directory: %w", err)
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("artifact root must be a directory")
	}
	store := &ReportStore{
		path:    filepath.Join(root, ".agent-reports.jsonl"),
		reports: make(map[string]protocol.AgentReport),
	}
	info, err := os.Lstat(store.path)
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect agent report store: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("agent report store must be a regular file")
	}
	file, err := os.Open(store.path)
	if err != nil {
		return nil, fmt.Errorf("open agent report store: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var report protocol.AgentReport
		if err := json.Unmarshal(scanner.Bytes(), &report); err != nil {
			return nil, fmt.Errorf("decode stored agent report: %w", err)
		}
		if report.ReportID == "" || report.ReportID != protocol.ComputeReportID(report) {
			return nil, fmt.Errorf("stored agent report has invalid id")
		}
		store.reports[report.ReportID] = report
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read agent report store: %w", err)
	}
	return store, nil
}

func (store *ReportStore) Append(report protocol.AgentReport) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.reports[report.ReportID]; exists {
		return false, nil
	}
	data, err := json.Marshal(report)
	if err != nil {
		return false, fmt.Errorf("encode agent report: %w", err)
	}
	file, err := os.OpenFile(store.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return false, fmt.Errorf("append agent report: %w", err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return false, fmt.Errorf("write agent report: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return false, fmt.Errorf("sync agent report: %w", err)
	}
	if err := file.Close(); err != nil {
		return false, fmt.Errorf("close agent report store: %w", err)
	}
	store.reports[report.ReportID] = report
	return true, nil
}

func (store *ReportStore) List() []protocol.AgentReport {
	store.mu.Lock()
	defer store.mu.Unlock()
	reports := make([]protocol.AgentReport, 0, len(store.reports))
	for _, report := range store.reports {
		reports = append(reports, report)
	}
	sort.Slice(reports, func(i, j int) bool {
		if !reports[i].ReportedAt.Equal(reports[j].ReportedAt) {
			return reports[i].ReportedAt.Before(reports[j].ReportedAt)
		}
		return reports[i].ReportID < reports[j].ReportID
	})
	return reports
}
