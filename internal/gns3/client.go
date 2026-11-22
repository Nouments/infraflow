// Package gns3 is a small client for the GNS3 Controller HTTP API (v3, with a
// read-only fallback to v2). InfraFlow uses it for lab validation only: to
// discover what a GNS3 server really offers, to build an isolated test
// project, and to record the state of that project in test reports. It never
// deletes projects and it never touches the compute (internal) API.
//
// Credentials come from the caller (normally INFRAFLOW_GNS3_* environment
// variables). The password is never logged, returned, or put in an error.
package gns3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Environment variables read by ConfigFromEnv.
const (
	EnvURL      = "INFRAFLOW_GNS3_URL"
	EnvUsername = "INFRAFLOW_GNS3_USERNAME"
	EnvPassword = "INFRAFLOW_GNS3_PASSWORD"
	DefaultURL  = "http://127.0.0.1:3080"
)

const maxResponseBytes = 16 << 20

// Config selects a GNS3 controller.
type Config struct {
	URL      string
	Username string
	Password string
	// Timeout bounds every request. Zero means 15 seconds.
	Timeout time.Duration
	// HTTPClient is optional; tests inject one.
	HTTPClient *http.Client
}

// ConfigFromEnv reads INFRAFLOW_GNS3_URL, INFRAFLOW_GNS3_USERNAME and
// INFRAFLOW_GNS3_PASSWORD. The URL defaults to the local controller.
func ConfigFromEnv() Config {
	address := strings.TrimSpace(os.Getenv(EnvURL))
	if address == "" {
		address = DefaultURL
	}
	return Config{URL: address, Username: os.Getenv(EnvUsername), Password: os.Getenv(EnvPassword)}
}

// APIError is a non-2xx controller response. Message is the controller's own
// message, truncated; it never contains the request credentials.
type APIError struct {
	Method  string
	Path    string
	Status  int
	Message string
}

func (err *APIError) Error() string {
	return fmt.Sprintf("gns3 %s %s: HTTP %d: %s", err.Method, err.Path, err.Status, err.Message)
}

// ErrUnauthorized is matched by errors.Is for HTTP 401/403 responses.
var ErrUnauthorized = errors.New("gns3: authentication or authorization failed")

// ErrNotFound is matched by errors.Is for HTTP 404 responses.
var ErrNotFound = errors.New("gns3: not found")

func (err *APIError) Is(target error) bool {
	switch target {
	case ErrUnauthorized:
		return err.Status == http.StatusUnauthorized || err.Status == http.StatusForbidden
	case ErrNotFound:
		return err.Status == http.StatusNotFound
	}
	return false
}

// Client talks to one controller. It is safe for concurrent use.
type Client struct {
	base     *url.URL
	username string
	password string
	http     *http.Client
	timeout  time.Duration

	mu         sync.Mutex
	apiVersion string // "v3" or "v2"
	token      string
}

// New validates the configuration. It does not contact the server.
func New(config Config) (*Client, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(config.URL), "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("gns3: controller URL must be an absolute http(s) URL")
	}
	if base.User != nil {
		return nil, fmt.Errorf("gns3: put credentials in %s/%s, not in the URL", EnvUsername, EnvPassword)
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &Client{base: base, username: config.Username, password: config.Password, http: httpClient, timeout: timeout}, nil
}

// BaseURL returns the controller URL without credentials.
func (client *Client) BaseURL() string { return client.base.String() }

// Exposure describes how reachable and how protected the controller is, from
// the client's point of view. It is advisory evidence for reports.
type Exposure struct {
	Loopback           bool     `json:"loopback"`
	PrivateNetwork     bool     `json:"private_network"`
	TLS                bool     `json:"tls"`
	DefaultCredentials bool     `json:"default_credentials"`
	Warnings           []string `json:"warnings,omitempty"`
}

// Exposure inspects the configured URL and credentials without network I/O.
func (client *Client) Exposure() Exposure {
	result := Exposure{TLS: client.base.Scheme == "https"}
	host := client.base.Hostname()
	if strings.EqualFold(host, "localhost") {
		result.Loopback = true
	} else if ip := net.ParseIP(host); ip != nil {
		result.Loopback = ip.IsLoopback()
		result.PrivateNetwork = ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	result.DefaultCredentials = client.username == "admin" && client.password == "admin"
	if !result.Loopback && !result.PrivateNetwork {
		result.Warnings = append(result.Warnings, "controller is not on a loopback or private address")
	}
	if !result.TLS && !result.Loopback {
		result.Warnings = append(result.Warnings, "credentials and tokens cross the network without TLS")
	}
	if result.DefaultCredentials {
		result.Warnings = append(result.Warnings, "controller uses the default admin/admin account; change it before exposing the server")
	}
	return result
}

// Version is GET /version.
type Version struct {
	Version        string `json:"version"`
	Local          bool   `json:"local"`
	ControllerHost string `json:"controller_host,omitempty"`
	APIVersion     string `json:"api_version"`
}

// Version probes /v3/version, then /v2/version, without credentials.
func (client *Client) Version(ctx context.Context) (Version, error) {
	var lastErr error
	for _, api := range []string{"v3", "v2"} {
		var version Version
		err := client.do(ctx, http.MethodGet, "/"+api+"/version", nil, &version, false)
		if err == nil {
			version.APIVersion = api
			client.mu.Lock()
			client.apiVersion = api
			client.mu.Unlock()
			return version, nil
		}
		lastErr = err
		if !errors.Is(err, ErrNotFound) {
			break
		}
	}
	return Version{}, lastErr
}

func (client *Client) api(ctx context.Context) (string, error) {
	client.mu.Lock()
	api := client.apiVersion
	client.mu.Unlock()
	if api != "" {
		return api, nil
	}
	version, err := client.Version(ctx)
	return version.APIVersion, err
}

// authenticate obtains a v3 bearer token. v2 servers use HTTP Basic, and only
// when credentials are configured.
func (client *Client) authenticate(ctx context.Context) error {
	api, err := client.api(ctx)
	if err != nil || api != "v3" {
		return err
	}
	if client.username == "" || client.password == "" {
		return fmt.Errorf("%w: %s and %s are required by this controller", ErrUnauthorized, EnvUsername, EnvPassword)
	}
	var response struct {
		AccessToken string `json:"access_token"`
	}
	body := map[string]string{"username": client.username, "password": client.password}
	if err := client.do(ctx, http.MethodPost, "/v3/access/users/authenticate", body, &response, false); err != nil {
		return err
	}
	if response.AccessToken == "" {
		return fmt.Errorf("%w: controller returned no access token", ErrUnauthorized)
	}
	client.mu.Lock()
	client.token = response.AccessToken
	client.mu.Unlock()
	return nil
}

// call performs an authenticated request against the detected API version.
// A 401 triggers one re-authentication (expired token), never more.
func (client *Client) call(ctx context.Context, method, path string, payload, out any) error {
	api, err := client.api(ctx)
	if err != nil {
		return err
	}
	client.mu.Lock()
	haveToken := client.token != ""
	client.mu.Unlock()
	if api == "v3" && !haveToken {
		if err := client.authenticate(ctx); err != nil {
			return err
		}
	}
	err = client.do(ctx, method, "/"+api+path, payload, out, true)
	if api == "v3" && errors.Is(err, ErrUnauthorized) {
		if authErr := client.authenticate(ctx); authErr != nil {
			return authErr
		}
		err = client.do(ctx, method, "/"+api+path, payload, out, true)
	}
	return err
}

func (client *Client) do(ctx context.Context, method, path string, payload, out any, authenticated bool) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	ctx, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, client.base.String()+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if authenticated {
		client.mu.Lock()
		token, api := client.token, client.apiVersion
		client.mu.Unlock()
		switch {
		case api == "v3" && token != "":
			request.Header.Set("Authorization", "Bearer "+token)
		case api == "v2" && client.username != "":
			request.SetBasicAuth(client.username, client.password)
		}
	}
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("gns3 %s %s: %w", method, path, redactURL(err))
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("gns3 %s %s: read response: %w", method, path, err)
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("gns3 %s %s: response exceeds %d bytes", method, path, maxResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return &APIError{Method: method, Path: path, Status: response.StatusCode, Message: errorMessage(data)}
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("gns3 %s %s: decode response: %w", method, path, err)
	}
	return nil
}

func errorMessage(data []byte) string {
	var body struct {
		Message string `json:"message"`
		Detail  any    `json:"detail"`
	}
	if json.Unmarshal(data, &body) == nil {
		if body.Message != "" {
			return truncate(body.Message, 300)
		}
		if body.Detail != nil {
			detail, _ := json.Marshal(body.Detail)
			return truncate(string(detail), 300)
		}
	}
	return truncate(strings.TrimSpace(string(data)), 300)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

// redactURL strips any userinfo an error might echo from the request URL.
func redactURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if parsed, parseErr := url.Parse(urlErr.URL); parseErr == nil {
			parsed.User = nil
			urlErr.URL = parsed.String()
		}
	}
	return err
}
