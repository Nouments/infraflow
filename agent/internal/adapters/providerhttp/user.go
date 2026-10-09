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

func (client *UserClient) request(ctx context.Context, method, endpoint string, payload any, target any, authenticated bool) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode TUI request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL.ResolveReference(&url.URL{Path: endpoint}).String(), body)
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
		return fmt.Errorf("TUI server returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	if target != nil && len(data) > 0 {
		if err := json.Unmarshal(data, target); err != nil {
			return fmt.Errorf("decode TUI server response: %w", err)
		}
	}
	return nil
}
