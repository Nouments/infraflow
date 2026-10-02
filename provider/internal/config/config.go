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

const (
	maxConfigBytes = 64 << 10
	defaultChunk   = 64 << 10
	maxChunk       = 1 << 20
)

type Config struct {
	ListenAddress     string    `yaml:"listen_address"`
	ArtifactDirectory string    `yaml:"artifact_directory"`
	TokenEnv          string    `yaml:"token_env"`
	ChunkSize         int       `yaml:"chunk_size"`
	TLS               TLSConfig `yaml:"tls"`
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
	if config.ChunkSize < 1 || config.ChunkSize > maxChunk {
		return fmt.Errorf("chunk_size must be between 1 and %d bytes", maxChunk)
	}
	return nil
}
