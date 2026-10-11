package providerhttp

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"infraflow/pkg/observability"
)

const maxUserResponseBytes = 2 << 20

type UserClient struct {
	baseURL *url.URL
	http    *http.Client
	token   string
}

type UserSession struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      User      `json:"user"`
}

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Disabled bool   `json:"disabled"`
}

type JobSummary struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AgentSummary struct {
	ID         string `json:"id"`
	SiteID     string `json:"site_id"`
	Status     string `json:"status"`
	QueueDepth int    `json:"queue_depth"`
}

type EventSummary struct {
	EventID   string          `json:"event_id"`
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	JobID     string          `json:"job_id"`
	Payload   json.RawMessage `json:"payload"`
}

type TechnicalLogEntry struct {
	ID        string    `json:"event_id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Service   string    `json:"service"`
	Hostname  string    `json:"hostname"`
	SiteID    string    `json:"site_id"`
	AgentID   string    `json:"agent_id"`
	RunID     string    `json:"run_id"`
	JobID     string    `json:"job_id"`
	TaskID    string    `json:"task_id"`
	Event     string    `json:"event"`
	Message   string    `json:"message"`
	Error     string    `json:"error"`
	Line      string    `json:"line"`
	Source    string    `json:"source"`
}

type ExecutionPage struct {
	Items  []Execution `json:"items"`
	Total  int         `json:"total"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
}

type Execution struct {
	ID                string           `json:"id"`
	Status            string           `json:"status"`
	Tasks             []ExecutionTask  `json:"tasks"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
	StartedAt         time.Time        `json:"started_at,omitempty"`
	FinishedAt        time.Time        `json:"finished_at,omitempty"`
	CancelRequested   bool             `json:"cancel_requested,omitempty"`
	CancelRequestedAt time.Time        `json:"cancel_requested_at,omitempty"`
	Error             string           `json:"error,omitempty"`
	Events            []ExecutionEvent `json:"events"`
}

type ExecutionTask struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Status       string             `json:"status"`
	Dependencies []string           `json:"dependencies,omitempty"`
	ResourceLock string             `json:"resource_lock,omitempty"`
	Parameters   json.RawMessage    `json:"parameters"`
	Attempts     []ExecutionAttempt `json:"attempts"`
	Result       json.RawMessage    `json:"result,omitempty"`
	Error        string             `json:"error,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
	StartedAt    time.Time          `json:"started_at,omitempty"`
	FinishedAt   time.Time          `json:"finished_at,omitempty"`
}

type ExecutionAttempt struct {
	Number     int             `json:"number"`
	Status     string          `json:"status"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at,omitempty"`
	DurationMS int64           `json:"duration_ms,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
}

type ExecutionEvent struct {
	ActorID    string    `json:"actor_id"`
	Action     string    `json:"action"`
	TaskID     string    `json:"task_id,omitempty"`
	Status     string    `json:"status"`
	TaskStatus string    `json:"task_status,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	Error      string    `json:"error,omitempty"`
}

func NewUserClient(address, caFile string) (*UserClient, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("TUI server address must be an absolute HTTP(S) URL without a path or credentials")
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return nil, fmt.Errorf("HTTP TUI connections must target loopback; use HTTPS for remote servers")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	if caFile != "" {
		data, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read TUI CA file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("TUI CA file does not contain a valid certificate")
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return &UserClient{
		baseURL: parsed,
		http: &http.Client{
			Timeout:   15 * time.Second,
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (client *UserClient) Login(ctx context.Context, username, password string) (UserSession, error) {
	var session UserSession
	if err := client.request(ctx, http.MethodPost, "/api/v1/auth/login", map[string]string{"username": username, "password": password}, &session, false); err != nil {
		return UserSession{}, err
	}
	client.token = session.Token
	return session, nil
}

func (client *UserClient) Logout(ctx context.Context) error {
	if client.token == "" {
		return nil
	}
	err := client.request(ctx, http.MethodPost, "/api/v1/auth/logout", nil, nil, true)
	client.token = ""
	return err
}

func (client *UserClient) CurrentUser(ctx context.Context) (User, error) {
	var user User
	if err := client.request(ctx, http.MethodGet, "/api/v1/auth/me", nil, &user, true); err != nil {
		return User{}, err
	}
	return user, nil
}

func (client *UserClient) Jobs(ctx context.Context) ([]JobSummary, error) {
	var jobs []JobSummary
	if err := client.request(ctx, http.MethodGet, "/api/v1/jobs", nil, &jobs, true); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (client *UserClient) Agents(ctx context.Context) ([]AgentSummary, error) {
	var agents []AgentSummary
	if err := client.request(ctx, http.MethodGet, "/api/v1/agents", nil, &agents, true); err != nil {
		return nil, err
	}
	return agents, nil
}

func (client *UserClient) Events(ctx context.Context) ([]EventSummary, error) {
	var events []EventSummary
	if err := client.request(ctx, http.MethodGet, "/api/v1/events", nil, &events, true); err != nil {
		return nil, err
	}
	return events, nil
}

func (client *UserClient) Logs(ctx context.Context) ([]TechnicalLogEntry, error) {
	var payload struct {
		Events []TechnicalLogEntry `json:"events"`
	}
	if err := client.request(ctx, http.MethodGet, "/api/v1/logs?limit=25", nil, &payload, true); err != nil {
		return nil, err
	}
	if payload.Events == nil || len(payload.Events) > 25 {
		return nil, fmt.Errorf("TUI server returned an invalid technical logs response")
	}
	for index := range payload.Events {
		entry := &payload.Events[index]
		if entry.Timestamp.IsZero() {
			return nil, fmt.Errorf("TUI server returned an invalid technical log entry")
		}
		normalized, err := observability.NormalizeEvent(observability.Event{
			ID: entry.ID, Timestamp: entry.Timestamp.UTC().Format(time.RFC3339Nano),
			Level: entry.Level, Event: entry.Event, Message: entry.Message,
			Service: entry.Service, Hostname: entry.Hostname, SiteID: entry.SiteID,
			AgentID: entry.AgentID, RunID: entry.RunID, JobID: entry.JobID, TaskID: entry.TaskID,
			Error: entry.Error, Source: entry.Source, Line: entry.Line,
		})
		if err != nil {
			return nil, fmt.Errorf("TUI server returned an invalid technical log entry")
		}
		entry.Message, entry.Error, entry.Line = normalized.Message, normalized.Error, normalized.Line
	}
	return payload.Events, nil
}

func (client *UserClient) Executions(ctx context.Context, limit, offset int, status, taskType string) (ExecutionPage, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return ExecutionPage{}, fmt.Errorf("TUI execution limit or offset is invalid")
	}
	query := url.Values{}
	query.Set("limit", fmt.Sprint(limit))
	query.Set("offset", fmt.Sprint(offset))
	if status != "" {
		query.Set("status", status)
	}
	if taskType != "" {
		query.Set("type", taskType)
	}
	var page ExecutionPage
	if err := client.request(ctx, http.MethodGet, "/api/v1/executions?"+query.Encode(), nil, &page, true); err != nil {
		return ExecutionPage{}, err
	}
	if page.Items == nil || page.Limit < 1 || page.Limit > 100 || page.Offset < 0 || page.Total < 0 || len(page.Items) > page.Limit || page.Offset > page.Total {
		return ExecutionPage{}, fmt.Errorf("TUI server returned an invalid executions page")
	}
	for index := range page.Items {
		if err := normalizeExecution(&page.Items[index]); err != nil {
			return ExecutionPage{}, err
		}
	}
	return page, nil
}

func (client *UserClient) Execution(ctx context.Context, id string) (Execution, error) {
	if id == "" || strings.ContainsAny(id, "/\\\r\n") {
		return Execution{}, fmt.Errorf("TUI execution id is invalid")
	}
	var execution Execution
	if err := client.request(ctx, http.MethodGet, "/api/v1/executions/"+url.PathEscape(id), nil, &execution, true); err != nil {
		return Execution{}, err
	}
	if err := normalizeExecution(&execution); err != nil {
		return Execution{}, err
	}
	return execution, nil
}

func (client *UserClient) CancelExecution(ctx context.Context, id string) (Execution, error) {
	if id == "" || strings.ContainsAny(id, "/\\\r\n") {
		return Execution{}, fmt.Errorf("TUI execution id is invalid")
	}
	var execution Execution
	if err := client.request(ctx, http.MethodPost, "/api/v1/executions/"+url.PathEscape(id)+"/cancel", nil, &execution, true); err != nil {
		return Execution{}, err
	}
	if err := normalizeExecution(&execution); err != nil {
		return Execution{}, err
	}
	return execution, nil
}

func normalizeExecution(execution *Execution) error {
	if execution == nil || execution.ID == "" || execution.Status == "" || execution.CreatedAt.IsZero() || execution.UpdatedAt.IsZero() || execution.Tasks == nil || execution.Events == nil {
		return fmt.Errorf("TUI server returned an invalid execution")
	}
	execution.Error = observability.Redact(execution.Error)
	for index := range execution.Tasks {
		task := &execution.Tasks[index]
		if task.ID == "" || task.Type == "" || task.Status == "" || task.CreatedAt.IsZero() || task.UpdatedAt.IsZero() {
			return fmt.Errorf("TUI server returned an invalid execution task")
		}
		if task.Attempts == nil {
			task.Attempts = []ExecutionAttempt{}
		}
		task.Error = observability.Redact(task.Error)
		for attemptIndex := range task.Attempts {
			attempt := &task.Attempts[attemptIndex]
			if attempt.Number < 1 || attempt.Status == "" || attempt.StartedAt.IsZero() || attempt.DurationMS < 0 {
				return fmt.Errorf("TUI server returned an invalid execution attempt")
			}
			attempt.Error = observability.Redact(attempt.Error)
			attempt.Result = redactJSONStrings(attempt.Result)
		}
		task.Result = redactJSONStrings(task.Result)
	}
	for index := range execution.Events {
		event := &execution.Events[index]
		if event.ActorID == "" || event.Action == "" || event.Status == "" || event.Timestamp.IsZero() {
			return fmt.Errorf("TUI server returned an invalid execution event")
		}
		event.Error = observability.Redact(event.Error)
	}
	return nil
}

func redactJSONStrings(payload json.RawMessage) json.RawMessage {
	if len(payload) == 0 || !json.Valid(payload) {
		return payload
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return payload
	}
	value = redactJSONValue(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return payload
	}
	return encoded
}

func redactJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if sensitiveUserField(key) {
				typed[key] = "[REDACTED]"
				continue
			}
			typed[key] = redactJSONValue(child)
		}
		return typed
	case []any:
		for index, child := range typed {
			typed[index] = redactJSONValue(child)
		}
		return typed
	case string:
		return observability.Redact(typed)
	}
	return value
}

func sensitiveUserField(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
	for _, marker := range []string{"password", "passwd", "secret", "token", "apikey", "privatekey", "credential", "authorization", "accesskey"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func (client *UserClient) request(ctx context.Context, method, endpoint string, payload any, target any, authenticated bool) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode TUI request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	reference, err := url.Parse(endpoint)
	if err != nil || reference.IsAbs() || reference.Host != "" {
		return fmt.Errorf("invalid TUI server endpoint")
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL.ResolveReference(reference).String(), body)
	if err != nil {
		return fmt.Errorf("create TUI request: %w", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if authenticated {
		request.Header.Set("Authorization", "Bearer "+client.token)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("TUI server request: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxUserResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read TUI server response: %w", err)
	}
	if len(data) > maxUserResponseBytes {
		return fmt.Errorf("TUI server response exceeds %d bytes", maxUserResponseBytes)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("TUI server returned HTTP %d: %s", response.StatusCode, observability.Redact(strings.TrimSpace(string(data))))
	}
	if target != nil && len(data) > 0 {
		if err := json.Unmarshal(data, target); err != nil {
			return fmt.Errorf("decode TUI server response: %w", err)
		}
	}
	return nil
}
