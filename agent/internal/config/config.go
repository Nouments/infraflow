package config

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxConfigBytes = 64 << 10

type Config struct {
	Agent    AgentConfig    `yaml:"agent"`
	Provider ProviderConfig `yaml:"provider"`
}

type AgentConfig struct {
	ID             string `yaml:"id"`
	StateDirectory string `yaml:"state_directory"`
}

type ProviderConfig struct {
	Address  string    `yaml:"address"`
	TokenEnv string    `yaml:"token_env"`
	TLS      TLSConfig `yaml:"tls"`
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
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) Validate() error {
	if strings.TrimSpace(config.Agent.ID) == "" || len(config.Agent.ID) > 128 {
		return fmt.Errorf("agent.id must be set and no longer than 128 characters")
	}
	if strings.TrimSpace(config.Agent.StateDirectory) == "" {
		return fmt.Errorf("agent.state_directory must be set")
	}
	if strings.TrimSpace(config.Provider.TokenEnv) == "" {
		return fmt.Errorf("provider.token_env must name the environment variable holding the token")
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
	return nil
}
