package config

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	maxConfigBytes = 64 << 10
	defaultChunk   = 64 << 10
	maxChunk       = 1 << 20
)

type Config struct {
	ListenAddress         string    `yaml:"listen_address"`
	APIListenAddress      string    `yaml:"api_listen_address,omitempty"`
	WebUIEnabled          bool      `yaml:"web_ui_enabled,omitempty"`
	ArtifactDirectory     string    `yaml:"artifact_directory"`
	DatabasePath          string    `yaml:"database_path,omitempty"`
	TokenEnv              string    `yaml:"token_env"`
	AdminUsername         string    `yaml:"admin_username,omitempty"`
	AdminCredentialFile   string    `yaml:"admin_credential_file,omitempty"`
	AdminCredentialScript string    `yaml:"admin_credential_script,omitempty"`
	SessionTTLMinutes     int       `yaml:"session_ttl_minutes,omitempty"`
	ChunkSize             int       `yaml:"chunk_size"`
	TLS                   TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	CertificateFile string `yaml:"certificate_file,omitempty"`
	KeyFile         string `yaml:"key_file,omitempty"`
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open provider config: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return Config{}, fmt.Errorf("read provider config: %w", err)
	}
	if len(data) > maxConfigBytes {
		return Config{}, fmt.Errorf("provider config exceeds %d bytes", maxConfigBytes)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode provider config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Config{}, fmt.Errorf("provider config must contain exactly one YAML document")
	}
	if config.ChunkSize == 0 {
		config.ChunkSize = defaultChunk
	}
	if config.DatabasePath == "" {
		config.DatabasePath = filepath.Join(config.ArtifactDirectory, ".infraflow-users.sqlite3")
	}
	if config.AdminUsername == "" {
		config.AdminUsername = "admin"
	}
	if config.AdminCredentialFile == "" {
		config.AdminCredentialFile = filepath.Join(config.ArtifactDirectory, ".infraflow-admin-password")
	}
	if config.AdminCredentialScript == "" {
		config.AdminCredentialScript = filepath.Join(config.ArtifactDirectory, "get-admin-password.sh")
	}
	if config.SessionTTLMinutes == 0 {
		config.SessionTTLMinutes = 480
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) Validate() error {
	if strings.TrimSpace(config.ArtifactDirectory) == "" {
		return fmt.Errorf("artifact_directory must be set")
	}
	if strings.TrimSpace(config.TokenEnv) == "" {
		return fmt.Errorf("token_env must name the environment variable holding the token")
	}
	if strings.TrimSpace(config.DatabasePath) == "" {
		return fmt.Errorf("database_path must be set")
	}
	if strings.TrimSpace(config.AdminUsername) == "" {
		return fmt.Errorf("admin_username must be set")
	}
	if strings.TrimSpace(config.AdminCredentialFile) == "" || strings.TrimSpace(config.AdminCredentialScript) == "" {
		return fmt.Errorf("admin credential file and script paths must be set")
	}
	if config.SessionTTLMinutes < 5 || config.SessionTTLMinutes > 1440 {
		return fmt.Errorf("session_ttl_minutes must be between 5 and 1440")
	}
	host, portText, err := net.SplitHostPort(config.ListenAddress)
	if err != nil || host == "" {
		return fmt.Errorf("listen_address must use host:port format")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("listen_address contains an invalid port")
	}
	certificateSet := config.TLS.CertificateFile != ""
	keySet := config.TLS.KeyFile != ""
	if certificateSet != keySet {
		return fmt.Errorf("tls.certificate_file and tls.key_file must be configured together")
	}
	if !certificateSet {
		ip := net.ParseIP(host)
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("TLS certificate and key are required for non-loopback listen addresses")
		}
	}
	if config.APIListenAddress != "" {
		apiHost, apiPortText, err := net.SplitHostPort(config.APIListenAddress)
		if err != nil || apiHost == "" {
			return fmt.Errorf("api_listen_address must use host:port format")
		}
		apiPort, err := strconv.Atoi(apiPortText)
		if err != nil || apiPort < 1 || apiPort > 65535 {
			return fmt.Errorf("api_listen_address contains an invalid port")
		}
		apiIP := net.ParseIP(apiHost)
		if !strings.EqualFold(apiHost, "localhost") && (apiIP == nil || !apiIP.IsLoopback()) && !certificateSet {
			return fmt.Errorf("api_listen_address requires TLS certificate and key for non-loopback addresses")
		}
	}
	if config.WebUIEnabled && config.APIListenAddress == "" {
		return fmt.Errorf("web_ui_enabled requires api_listen_address")
	}
	if config.ChunkSize < 1 || config.ChunkSize > maxChunk {
		return fmt.Errorf("chunk_size must be between 1 and %d bytes", maxChunk)
	}
	return nil
}
