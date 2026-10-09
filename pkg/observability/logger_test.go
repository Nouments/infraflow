package observability

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoggerWritesStructuredJSONLAndRedactsSecrets(t *testing.T) {
	directory := t.TempDir()
	var console bytes.Buffer
	config := testConfig(directory)
	config.Console = true
	config.ConsoleOut = &console
	logger, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	duration := int64(42)
	if err := logger.Emit(t.Context(), Event{
		Level: "WARN", Event: "process.output", Message: "command output",
		RunID: "run-01", TaskID: "task-02", CorrelationID: "corr-03",
		Operation: "execution", Status: "running", DurationMS: &duration,
		Error: "password=hunter2 token=top-secret",
		Line:  "line one\nline two", Stream: "stdout",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one JSONL event, got %d", len(lines))
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("log line is not JSON: %v", err)
	}
	if event["event"] != "process.output" || event["service"] != "test" || event["run_id"] != "run-01" || event["task_id"] != "task-02" || event["correlation_id"] != "corr-03" {
		t.Fatalf("event context was not preserved: %#v", event)
	}
	timestamp, ok := event["timestamp"].(string)
	if !ok {
		t.Fatalf("timestamp is missing: %#v", event)
	}
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil || parsed.Location() != time.UTC {
		t.Fatalf("timestamp must be RFC3339 UTC, got %q: %v", timestamp, err)
	}
	encoded := string(data) + console.String()
	for _, secret := range []string{"hunter2", "top-secret"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("secret appeared in logs: %q", secret)
		}
	}
	if !strings.Contains(string(data), "[REDACTED]") || !strings.Contains(string(data), `\\n`) {
		t.Fatalf("secret or multiline output was not safely encoded: %s", data)
	}
}

func TestLoggerFiltersBelowConfiguredLevel(t *testing.T) {
	config := testConfig(t.TempDir())
	config.Level = "WARN"
	logger, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	if err := logger.Emit(t.Context(), Event{Level: "INFO", Event: "ignored", Message: "filtered"}); err != nil {
		t.Fatal(err)
	}
	if err := logger.Emit(t.Context(), Event{Level: "ERROR", Event: "kept", Message: "visible"}); err != nil {
		t.Fatal(err)
	}
	events, err := logger.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Event != "kept" {
		t.Fatalf("unexpected filtered events: %#v", events)
	}
}

func TestLoggerRotatesWithinConfiguredRetention(t *testing.T) {
	directory := t.TempDir()
	config := testConfig(directory)
	config.MaxBytes = 420
	config.MaxFiles = 2
	logger, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 12; index++ {
		if err := logger.Emit(t.Context(), Event{Event: "rotation.test", Message: strings.Repeat("x", 120)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"events.jsonl", "events.jsonl.1", "events.jsonl.2"} {
		info, err := os.Stat(filepath.Join(directory, path))
		if err != nil {
			t.Fatalf("expected retained file %s: %v", path, err)
		}
		if info.Size() > config.MaxBytes {
			t.Fatalf("retained file %s exceeds configured bound: %d", path, info.Size())
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "events.jsonl.3")); !os.IsNotExist(err) {
		t.Fatalf("retention exceeded configured file count: %v", err)
	}
}

func TestLoggerReportsPersistenceFailures(t *testing.T) {
	root := t.TempDir()
	fileAsDirectory := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(fileAsDirectory, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := testConfig(filepath.Join(fileAsDirectory, "logs"))
	if _, err := New(config); err == nil {
		t.Fatal("logger initialization silently accepted an inaccessible directory")
	}

	config = testConfig(t.TempDir())
	logger, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if err := logger.Emit(t.Context(), Event{Event: "write.failure", Message: "must surface"}); err == nil {
		t.Fatal("logger silently discarded a write after close")
	}
}

func testConfig(directory string) Config {
	return Config{
		Service: "test", Directory: directory, FileName: "events.jsonl",
		Level: "DEBUG", Format: "text", MaxBytes: DefaultMaxBytes, MaxFiles: DefaultMaxFiles,
	}
}
