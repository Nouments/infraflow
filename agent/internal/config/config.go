package config

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"infraflow/pkg/observability"
	"infraflow/pkg/protocol"

	"gopkg.in/yaml.v3"
)

const maxConfigBytes = 64 << 10

type Config struct {
	Agent    AgentConfig    `yaml:"agent"`
	Provider ProviderConfig `yaml:"provider"`
	Logging  LoggingConfig  `yaml:"logging,omitempty"`
}

type LoggingConfig struct {
	Directory string `yaml:"directory,omitempty"`
	Level     string `yaml:"level,omitempty"`
	Format    string `yaml:"format,omitempty"`
	MaxBytes  int64  `yaml:"max_bytes,omitempty"`
	MaxFiles  int    `yaml:"max_files,omitempty"`
}

type AgentConfig struct {
	ID             string   `yaml:"id"`
	SiteID         string   `yaml:"site_id,omitempty"`
	Capabilities   []string `yaml:"capabilities,omitempty"`
	StateDirectory string   `yaml:"state_directory"`
}

type ProviderConfig struct {
	Address          string    `yaml:"address"`
	APIAddress       string    `yaml:"api_address,omitempty"`
	TokenEnv         string    `yaml:"token_env"`
	ArtifactTokenEnv string    `yaml:"artifact_token_env,omitempty"`
	TLS              TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	Enabled bool   `yaml:"enabled"`
	CAFile  string `yaml:"ca_file,omitempty"`
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open agent config: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return Config{}, fmt.Errorf("read agent config: %w", err)
	}
	if len(data) > maxConfigBytes {
		return Config{}, fmt.Errorf("agent config exceeds %d bytes", maxConfigBytes)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode agent config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Config{}, fmt.Errorf("agent config must contain exactly one YAML document")
	}
	if err := applyEnvironment(&config); err != nil {
		return Config{}, err
	}
	defaults, err := observability.DefaultConfig("agent")
	if err != nil {
		return Config{}, fmt.Errorf("configure local agent logs: %w", err)
	}
	if config.Logging.Directory == "" {
		if config.Agent.StateDirectory != "" && os.Getenv("INFRAFLOW_AGENT_DATA") != "" {
			config.Logging.Directory = filepath.Join(config.Agent.StateDirectory, "logs")
		} else {
			config.Logging.Directory = defaults.Directory
		}
	}
	if config.Logging.Level == "" {
		config.Logging.Level = defaults.Level
	}
	if config.Logging.Format == "" {
		config.Logging.Format = defaults.Format
	}
	if config.Logging.MaxBytes == 0 {
		config.Logging.MaxBytes = defaults.MaxBytes
	}
	if config.Logging.MaxFiles == 0 {
		config.Logging.MaxFiles = defaults.MaxFiles
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func applyEnvironment(config *Config) error {
	setIfPresent("INFRAFLOW_AGENT_ID", &config.Agent.ID)
	setIfPresent("INFRAFLOW_AGENT_SITE_ID", &config.Agent.SiteID)
	setIfPresent("INFRAFLOW_AGENT_DATA", &config.Agent.StateDirectory)
	setIfPresent("INFRAFLOW_AGENT_PROVIDER_ADDRESS", &config.Provider.Address)
	setIfPresent("INFRAFLOW_AGENT_API_ADDRESS", &config.Provider.APIAddress)
	setIfPresent("INFRAFLOW_AGENT_TOKEN_ENV", &config.Provider.TokenEnv)
	setIfPresent("INFRAFLOW_AGENT_ARTIFACT_TOKEN_ENV", &config.Provider.ArtifactTokenEnv)
	setIfPresent("INFRAFLOW_AGENT_PROVIDER_CA", &config.Provider.TLS.CAFile)
	if value, exists := os.LookupEnv("INFRAFLOW_AGENT_CAPABILITIES"); exists {
		config.Agent.Capabilities = nil
		for _, capability := range strings.Split(value, ",") {
			capability = strings.TrimSpace(capability)
			if capability != "" {
				config.Agent.Capabilities = append(config.Agent.Capabilities, capability)
			}
		}
	}
	if value, exists := os.LookupEnv("INFRAFLOW_AGENT_TLS_ENABLED"); exists {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("INFRAFLOW_AGENT_TLS_ENABLED must be a boolean: %w", err)
		}
		config.Provider.TLS.Enabled = enabled
	}
	setIfPresent("INFRAFLOW_AGENT_LOG_DIRECTORY", &config.Logging.Directory)
	return nil
}

func setIfPresent(name string, destination *string) {
	if value, exists := os.LookupEnv(name); exists && strings.TrimSpace(value) != "" {
		*destination = value
	}
}

func (config Config) Validate() error {
	if strings.TrimSpace(config.Agent.ID) == "" || len(config.Agent.ID) > 128 {
		return fmt.Errorf("agent.id must be set and no longer than 128 characters")
	}
	if config.Agent.SiteID != "" && (!protocol.ValidSiteName(config.Agent.SiteID) || len(config.Agent.SiteID) > 128) {
		return fmt.Errorf("agent.site_id must be a valid identifier no longer than 128 characters")
	}
	if len(config.Agent.Capabilities) > 64 {
		return fmt.Errorf("agent.capabilities must contain at most 64 values")
	}
	seenCapabilities := make(map[string]struct{}, len(config.Agent.Capabilities))
	for _, capability := range config.Agent.Capabilities {
		if strings.TrimSpace(capability) == "" || len(capability) > 128 || strings.ContainsAny(capability, "\r\n") {
			return fmt.Errorf("agent.capabilities contains an invalid value")
		}
		if _, exists := seenCapabilities[capability]; exists {
			return fmt.Errorf("agent.capabilities contains a duplicate value")
		}
		seenCapabilities[capability] = struct{}{}
	}
	if strings.TrimSpace(config.Agent.StateDirectory) == "" {
		return fmt.Errorf("agent.state_directory must be set")
	}
	if strings.TrimSpace(config.Provider.TokenEnv) == "" {
		return fmt.Errorf("provider.token_env must name the environment variable holding the token")
	}
	if strings.ContainsAny(config.Provider.ArtifactTokenEnv, "\r\n") {
		return fmt.Errorf("provider.artifact_token_env must be a valid environment variable name")
	}
	host, portText, err := net.SplitHostPort(config.Provider.Address)
	if err != nil || host == "" {
		return fmt.Errorf("provider.address must use host:port format")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("provider.address contains an invalid port")
	}
	if !config.Provider.TLS.Enabled {
		ip := net.ParseIP(host)
		if strings.ToLower(host) != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("provider.tls.enabled is required for non-loopback addresses")
		}
		if config.Provider.TLS.CAFile != "" {
			return fmt.Errorf("provider.tls.ca_file requires TLS to be enabled")
		}
	}
	if config.Provider.APIAddress != "" {
		parsed, err := url.Parse(config.Provider.APIAddress)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
			return fmt.Errorf("provider.api_address must be an HTTP(S) URL without a path or credentials")
		}
		apiIP := net.ParseIP(parsed.Hostname())
		if parsed.Scheme == "http" && !strings.EqualFold(parsed.Hostname(), "localhost") && (apiIP == nil || !apiIP.IsLoopback()) {
			return fmt.Errorf("provider.api_address must use HTTPS for non-loopback addresses")
		}
		if parsed.Scheme == "https" && !config.Provider.TLS.Enabled {
			return fmt.Errorf("provider.tls.enabled is required for HTTPS API addresses")
		}
	}
	return nil
}
