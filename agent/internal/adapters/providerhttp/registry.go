package providerhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"infraflow/internal/infrastructure/security"
	"infraflow/pkg/protocol"
)

const maxResponseBytes = 64 << 10

type Client struct {
	baseURL *url.URL
	token   string
	http    *http.Client
}

type Registration struct {
	AgentID      string   `json:"agent_id"`
	SiteID       string   `json:"site_id"`
	Version      string   `json:"version,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type Heartbeat struct {
	AgentID      string   `json:"-"`
	SiteID       string   `json:"site_id"`
	Version      string   `json:"version,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	QueueDepth   int      `json:"queue_depth"`
}

func New(address, token string) (*Client, error) {
	if len([]byte(token)) < security.MinAgentTokenBytes {
		return nil, fmt.Errorf("API token must be at least %d bytes", security.MinAgentTokenBytes)
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" {
		return nil, fmt.Errorf("provider API address must be an absolute HTTP(S) URL without a path or credentials")
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return nil, fmt.Errorf("provider API HTTP address must be loopback-only")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return &Client{baseURL: parsed, token: token, http: &http.Client{Timeout: 10 * time.Second}}, nil
}

func (client *Client) Register(ctx context.Context, registration Registration) error {
	if !protocol.ValidSiteName(registration.AgentID) || !protocol.ValidSiteName(registration.SiteID) {
		return fmt.Errorf("agent and site IDs must be valid")
	}
	return client.post(ctx, "/api/v1/agents/register", registration)
}

func (client *Client) Heartbeat(ctx context.Context, heartbeat Heartbeat) error {
	if !protocol.ValidSiteName(heartbeat.AgentID) || !protocol.ValidSiteName(heartbeat.SiteID) {
		return fmt.Errorf("agent and site IDs must be valid")
	}
	return client.post(ctx, "/api/v1/agents/"+heartbeat.AgentID+"/heartbeat", heartbeat)
}

func (client *Client) post(ctx context.Context, endpoint string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode provider API request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL.ResolveReference(&url.URL{Path: endpoint}).String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create provider API request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("provider API request: %w", err)
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if readErr != nil {
		return fmt.Errorf("read provider API response: %w", readErr)
	}
	if len(responseBody) > maxResponseBytes {
		return fmt.Errorf("provider API response exceeds %d bytes", maxResponseBytes)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message := strings.TrimSpace(string(responseBody))
		if len(message) > 512 {
			message = message[:512]
		}
		return fmt.Errorf("provider API returned HTTP %d: %s", response.StatusCode, message)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}
