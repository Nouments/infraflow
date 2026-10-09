package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type correlationContextKey struct{}

func WithCorrelationID(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, correlationContextKey{}, value)
}

func CorrelationID(ctx context.Context) string {
	value, _ := ctx.Value(correlationContextKey{}).(string)
	return value
}

const (
	DefaultMaxBytes = 10 << 20
	DefaultMaxFiles = 5
	MaxEventBytes   = 64 << 10
)

var secretPattern = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+|\b(password|passwd|secret|token|api[_-]?key|private[_-]?key|credential)(\s*[:=]\s*))("[^"]*"|'[^']*'|[^\s,;]+)`)

type Event struct {
	ID            string `json:"event_id"`
	Timestamp     string `json:"timestamp"`
	Level         string `json:"level"`
	Event         string `json:"event"`
	Message       string `json:"message"`
	Service       string `json:"service"`
	HostID        string `json:"host_id,omitempty"`
	Hostname      string `json:"hostname,omitempty"`
	SiteID        string `json:"site_id,omitempty"`
	AgentID       string `json:"agent_id,omitempty"`
	RunID         string `json:"run_id,omitempty"`
	JobID         string `json:"job_id,omitempty"`
	TaskID        string `json:"task_id,omitempty"`
	DeviceID      string `json:"device_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	TraceID       string `json:"trace_id,omitempty"`
	Operation     string `json:"operation,omitempty"`
	Status        string `json:"status,omitempty"`
	DurationMS    *int64 `json:"duration_ms,omitempty"`
	Error         string `json:"error,omitempty"`
	Source        string `json:"source"`
	Stream        string `json:"stream,omitempty"`
	Line          string `json:"line,omitempty"`
}

type Config struct {
	Service    string
	Directory  string
	FileName   string
	Level      string
	Format     string
	MaxBytes   int64
	MaxFiles   int
	HostID     string
	Hostname   string
	Console    bool
	ConsoleOut io.Writer
	Sink       Sink
}

type Logger struct {
	config Config
	file   *rotatingFile
	level  slog.Level
	mu     sync.Mutex
}

func DefaultConfig(service string) (Config, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		root, err = os.UserCacheDir()
	}
	if err != nil {
		return Config{}, fmt.Errorf("resolve local configuration directory: %w", err)
	}
	return Config{
		Service: service, Directory: filepath.Join(root, "infraflow", "logs", service),
		FileName: "events.jsonl", Level: "INFO", Format: "text",
		MaxBytes: DefaultMaxBytes, MaxFiles: DefaultMaxFiles,
	}, nil
}

func New(config Config) (*Logger, error) {
	if strings.TrimSpace(config.Service) == "" {
		return nil, fmt.Errorf("log service name is required")
	}
	if strings.TrimSpace(config.Directory) == "" {
		return nil, fmt.Errorf("log directory is required")
	}
	if config.FileName == "" {
		config.FileName = "events.jsonl"
	}
	if filepath.Base(config.FileName) != config.FileName || config.FileName == "." || config.FileName == ".." {
		return nil, fmt.Errorf("log file name must be a single safe path component")
	}
	if config.MaxBytes <= 0 {
		config.MaxBytes = DefaultMaxBytes
	}
	if config.MaxFiles <= 0 {
		config.MaxFiles = DefaultMaxFiles
	}
	if config.MaxFiles > 100 {
		return nil, fmt.Errorf("log max_files must not exceed 100")
	}
	level, err := parseLevel(config.Level)
	if err != nil {
		return nil, err
	}
	if config.Format == "" {
		config.Format = "text"
	}
	if config.Format != "text" && config.Format != "json" {
		return nil, fmt.Errorf("log format must be text or json")
	}
	if config.Hostname == "" {
		config.Hostname, _ = os.Hostname()
	}
	if config.ConsoleOut == nil {
		config.ConsoleOut = os.Stderr
	}
	if err := os.MkdirAll(config.Directory, 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	if err := os.Chmod(config.Directory, 0o700); err != nil {
		return nil, fmt.Errorf("secure log directory: %w", err)
	}
	file, err := newRotatingFile(filepath.Join(config.Directory, config.FileName), config.MaxBytes, config.MaxFiles)
	if err != nil {
		return nil, err
	}
	return &Logger{config: config, file: file, level: level}, nil
}

func (logger *Logger) Directory() string { return logger.config.Directory }

func (logger *Logger) Emit(ctx context.Context, event Event) error {
	if logger == nil {
		return fmt.Errorf("logger is not configured")
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if event.ID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("create log event id: %w", err)
		}
		event.ID = id
	}
	if event.Timestamp == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if event.Service == "" {
		event.Service = logger.config.Service
	}
	if event.Hostname == "" {
		event.Hostname = logger.config.Hostname
	}
	if event.Source == "" {
		event.Source = event.Service
	}
	if event.Level == "" {
		event.Level = "INFO"
	}
	if event.Event == "" || event.Message == "" {
		return fmt.Errorf("log event and message are required")
	}
	event.Message = Redact(event.Message)
	event.Error = Redact(event.Error)
	event.Line = Redact(singleLine(event.Line))
	level, err := parseLevel(event.Level)
	if err != nil {
		return err
	}
	if level < logger.level {
		return nil
	}
	timestamp, err := time.Parse(time.RFC3339Nano, event.Timestamp)
	if err != nil {
		return fmt.Errorf("parse log event timestamp: %w", err)
	}
	record := slog.NewRecord(timestamp, level, event.Message, 0)
	record.AddAttrs(eventAttrs(event)...)
	var jsonBuffer strings.Builder
	jsonHandler := slog.NewJSONHandler(&jsonBuffer, &slog.HandlerOptions{ReplaceAttr: func(_ []string, attribute slog.Attr) slog.Attr {
		switch attribute.Key {
		case "time":
			attribute.Key = "timestamp"
		case "msg":
			attribute.Key = "message"
		}
		return attribute
	}})
	if err := jsonHandler.Handle(ctx, record); err != nil {
		return fmt.Errorf("encode log event: %w", err)
	}
	encoded := []byte(strings.TrimSuffix(jsonBuffer.String(), "\n"))
	if len(encoded) > MaxEventBytes {
		return fmt.Errorf("log event exceeds %d bytes", MaxEventBytes)
	}
	if err := logger.file.WriteLine(encoded); err != nil {
		return fmt.Errorf("persist log event: %w", err)
	}
	if logger.config.Sink != nil {
		if err := logger.config.Sink.Append(event); err != nil {
			return fmt.Errorf("queue log event for synchronization: %w", err)
		}
	}
	if logger.config.Console {
		line := encoded
		if logger.config.Format == "text" {
			var textBuffer strings.Builder
			textHandler := slog.NewTextHandler(&textBuffer, &slog.HandlerOptions{})
			if err := textHandler.Handle(ctx, record); err != nil {
				return fmt.Errorf("format console log event: %w", err)
			}
			line = []byte(strings.TrimSuffix(textBuffer.String(), "\n"))
		}
		if _, err := fmt.Fprintln(logger.config.ConsoleOut, string(line)); err != nil {
			return fmt.Errorf("write console log event: %w", err)
		}
	}
	return nil
}

func eventAttrs(event Event) []slog.Attr {
	attributes := []slog.Attr{
		slog.String("event_id", event.ID), slog.String("event", event.Event),
		slog.String("service", event.Service), slog.String("source", event.Source),
	}
	for key, value := range map[string]string{
		"host_id": event.HostID, "hostname": event.Hostname, "site_id": event.SiteID,
		"agent_id": event.AgentID, "run_id": event.RunID, "job_id": event.JobID,
		"task_id": event.TaskID, "device_id": event.DeviceID,
		"correlation_id": event.CorrelationID, "trace_id": event.TraceID,
		"operation": event.Operation, "status": event.Status, "error": event.Error,
		"stream": event.Stream, "line": event.Line,
	} {
		if value != "" {
			attributes = append(attributes, slog.String(key, value))
		}
	}
	if event.DurationMS != nil {
		attributes = append(attributes, slog.Int64("duration_ms", *event.DurationMS))
	}
	return attributes
}

func (logger *Logger) ReadAll() ([]Event, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is not configured")
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	return logger.file.ReadAll()
}

func (logger *Logger) Close() error {
	if logger == nil {
		return nil
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	return logger.file.Close()
}

func Redact(value string) string {
	return secretPattern.ReplaceAllString(value, "[REDACTED]")
}

func NormalizeEvent(event Event) (Event, error) {
	if event.ID == "" || len(event.ID) > 128 || strings.ContainsAny(event.ID, "\r\n") {
		return Event{}, fmt.Errorf("log event id is invalid")
	}
	if _, err := time.Parse(time.RFC3339Nano, event.Timestamp); err != nil {
		return Event{}, fmt.Errorf("log event timestamp is invalid")
	}
	if _, err := parseLevel(event.Level); err != nil {
		return Event{}, err
	}
	if strings.TrimSpace(event.Event) == "" || strings.TrimSpace(event.Service) == "" || strings.TrimSpace(event.Message) == "" || strings.TrimSpace(event.Source) == "" {
		return Event{}, fmt.Errorf("log event, service, message, and source are required")
	}
	for field, value := range map[string]string{
		"event": event.Event, "service": event.Service, "source": event.Source,
		"message": event.Message, "hostname": event.Hostname, "host id": event.HostID,
		"site id": event.SiteID, "agent id": event.AgentID, "run id": event.RunID,
		"job id": event.JobID, "task id": event.TaskID, "device id": event.DeviceID,
		"correlation id": event.CorrelationID, "trace id": event.TraceID,
		"operation": event.Operation, "status": event.Status, "stream": event.Stream,
	} {
		if len(value) > 512 || strings.ContainsAny(value, "\r\n") {
			return Event{}, fmt.Errorf("log %s is invalid", field)
		}
	}
	if len(event.Message) > 4096 || len(event.Line) > 8192 || len(event.Error) > 4096 {
		return Event{}, fmt.Errorf("log message, line, or error exceeds its size limit")
	}
	event.Message = Redact(event.Message)
	event.Error = Redact(event.Error)
	event.Line = Redact(singleLine(event.Line))
	return event, nil
}

func singleLine(value string) string {
	value = strings.ReplaceAll(value, "\r", "\\r")
	return strings.ReplaceAll(value, "\n", "\\n")
}

func parseLevel(value string) (slog.Level, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "INFO":
		return slog.LevelInfo, nil
	case "DEBUG":
		return slog.LevelDebug, nil
	case "WARN", "WARNING":
		return slog.LevelWarn, nil
	case "ERROR":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("log level must be DEBUG, INFO, WARN, or ERROR")
	}
}

func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func NewEventID() (string, error) { return newID() }

type rotatingFile struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	maxFiles int
	file     *os.File
	size     int64
}

func newRotatingFile(path string, maxBytes int64, maxFiles int) (*rotatingFile, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure log file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("stat log file: %w", err)
	}
	return &rotatingFile{path: path, maxBytes: maxBytes, maxFiles: maxFiles, file: file, size: info.Size()}, nil
}

func (file *rotatingFile) WriteLine(line []byte) error {
	file.mu.Lock()
	defer file.mu.Unlock()
	if int64(len(line)+1) > file.maxBytes {
		return fmt.Errorf("log record exceeds rotation size limit")
	}
	if file.size+int64(len(line)+1) > file.maxBytes {
		if err := file.rotate(); err != nil {
			return err
		}
	}
	line = append(line, '\n')
	count, err := file.file.Write(line)
	file.size += int64(count)
	if err != nil {
		return err
	}
	if count != len(line) {
		return io.ErrShortWrite
	}
	return file.file.Sync()
}

func (file *rotatingFile) rotate() error {
	if err := file.file.Close(); err != nil {
		return fmt.Errorf("close log file for rotation: %w", err)
	}
	for index := file.maxFiles - 1; index >= 1; index-- {
		oldPath := fmt.Sprintf("%s.%d", file.path, index)
		newPath := fmt.Sprintf("%s.%d", file.path, index+1)
		if err := os.Rename(oldPath, newPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rotate log file: %w", err)
		}
	}
	if file.maxFiles > 0 {
		if err := os.Rename(file.path, file.path+".1"); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rotate active log file: %w", err)
		}
	}
	if err := os.Remove(fmt.Sprintf("%s.%d", file.path, file.maxFiles+1)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove expired log file: %w", err)
	}
	active, err := os.OpenFile(file.path, os.O_CREATE|os.O_TRUNC|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("reopen log file after rotation: %w", err)
	}
	if err := active.Chmod(0o600); err != nil {
		_ = active.Close()
		return fmt.Errorf("secure rotated log file: %w", err)
	}
	file.file = active
	file.size = 0
	return nil
}

func (file *rotatingFile) ReadAll() ([]Event, error) {
	if err := file.file.Sync(); err != nil {
		return nil, fmt.Errorf("sync log before reading: %w", err)
	}
	paths := make([]string, 0, file.maxFiles+1)
	for index := file.maxFiles; index >= 1; index-- {
		paths = append(paths, fmt.Sprintf("%s.%d", file.path, index))
	}
	paths = append(paths, file.path)
	var events []Event
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read log file: %w", err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line == "" {
				continue
			}
			var event Event
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				return nil, fmt.Errorf("decode log event: %w", err)
			}
			events = append(events, event)
		}
	}
	return events, nil
}

func (file *rotatingFile) Close() error {
	file.mu.Lock()
	defer file.mu.Unlock()
	if file.file == nil {
		return nil
	}
	return errors.Join(file.file.Sync(), file.file.Close())
}
