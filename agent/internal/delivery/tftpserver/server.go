package tftpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"infraflow/pkg/observability"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"infraflow/internal/infrastructure/safefs"
	"infraflow/pkg/protocol"

	"github.com/pin/tftp/v3"
)

const maxConfigBytes = 1 << 20
const maxTFTPFileBytes int64 = 8 << 30

type Config struct {
	Version       int        `json:"version"`
	Service       string     `json:"service"`
	Enabled       bool       `json:"enabled"`
	Site          string     `json:"site"`
	RootDirectory string     `json:"root_directory"`
	ReadOnly      bool       `json:"read_only"`
	AllowedFiles  []FileRule `json:"allowed_files"`
}

type FileRule struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

type Server struct {
	config Config
	root   string
	files  map[string]struct{}
	events EventFunc
}

// EventFunc receives one structured TFTP lifecycle event. It must not block.
type EventFunc func(observability.Event)

// SetEventSink installs a structured receiver for RRQ, transfer completion,
// denial, missing-file, and transfer-failure evidence.
func (server *Server) SetEventSink(events EventFunc) { server.events = events }

func (server *Server) emit(level, name, message, status, filename, detail string) {
	if server.events == nil {
		return
	}
	server.events(observability.Event{
		Level: level, Event: name, Message: message, Service: "agent", Source: "tftp-server",
		SiteID: server.config.Site, Operation: "tftp", Status: status, Line: "file " + filename, Error: detail,
	})
}

func LoadConfig(filename string) (Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return Config{}, fmt.Errorf("open TFTP config: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return Config{}, fmt.Errorf("read TFTP config: %w", err)
	}
	if len(data) > maxConfigBytes {
		return Config{}, fmt.Errorf("TFTP config exceeds %d bytes", maxConfigBytes)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode TFTP config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Config{}, fmt.Errorf("TFTP config must contain exactly one JSON document")
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) Validate() error {
	if config.Version != 1 || config.Service != "tftp" {
		return fmt.Errorf("TFTP config must use version 1 and service tftp")
	}
	if !protocol.ValidSiteName(config.Site) {
		return fmt.Errorf("TFTP config has an invalid site name")
	}
	if config.RootDirectory == "" || path.Clean(config.RootDirectory) != config.RootDirectory || strings.ContainsAny(config.RootDirectory, `/\\`) || config.RootDirectory == "." || config.RootDirectory == ".." {
		return fmt.Errorf("TFTP root_directory must be a single safe directory name")
	}
	if !config.ReadOnly {
		return fmt.Errorf("TFTP server only supports read-only configs")
	}
	if len(config.AllowedFiles) == 0 || len(config.AllowedFiles) > 256 {
		return fmt.Errorf("TFTP config must declare between 1 and 256 allowed files")
	}
	seen := make(map[string]struct{}, len(config.AllowedFiles))
	for _, file := range config.AllowedFiles {
		if file.Path == "" || path.Clean(file.Path) != file.Path || path.IsAbs(file.Path) || strings.ContainsAny(file.Path, `\\`) || file.Source != "agent_runtime" {
			return fmt.Errorf("TFTP allowed files must use safe relative paths and source agent_runtime")
		}
		for _, part := range strings.Split(file.Path, "/") {
			if part == "" || part == "." || part == ".." {
				return fmt.Errorf("TFTP allowed file %q contains an unsafe path component", file.Path)
			}
		}
		if _, exists := seen[file.Path]; exists {
			return fmt.Errorf("TFTP allowed files contain duplicate path %q", file.Path)
		}
		seen[file.Path] = struct{}{}
	}
	return nil
}

func NewServer(config Config, bootstrapDirectory string) (*Server, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !config.Enabled {
		return nil, fmt.Errorf("TFTP service is disabled in the generated config")
	}
	if strings.TrimSpace(bootstrapDirectory) == "" {
		return nil, fmt.Errorf("TFTP bootstrap directory is required")
	}
	root := filepath.Join(bootstrapDirectory, config.RootDirectory)
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("open TFTP root directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("TFTP root must be a real directory")
	}
	files := make(map[string]struct{}, len(config.AllowedFiles))
	for _, file := range config.AllowedFiles {
		files[file.Path] = struct{}{}
	}
	return &Server{config: config, root: root, files: files}, nil
}

func (server *Server) Serve(ctx context.Context, interfaceName, listenAddress string, port int) error {
	if ctx == nil || strings.TrimSpace(interfaceName) == "" || port < 1 || port > 65535 {
		return fmt.Errorf("TFTP context, interface, and valid UDP port are required")
	}
	listenIP := net.ParseIP(listenAddress).To4()
	if listenIP == nil {
		return fmt.Errorf("TFTP listen address must be IPv4")
	}
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return fmt.Errorf("find TFTP interface: %w", err)
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return fmt.Errorf("read TFTP interface addresses: %w", err)
	}
	found := false
	for _, address := range addresses {
		localIP, _, parseErr := net.ParseCIDR(address.String())
		if parseErr == nil && localIP.To4() != nil && localIP.Equal(listenIP) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("TFTP listen address %s is not assigned to interface %s", listenIP, interfaceName)
	}
	conn, err := net.ListenPacket("udp4", net.JoinHostPort(listenIP.String(), fmt.Sprint(port)))
	if err != nil {
		return fmt.Errorf("listen for TFTP on %s/%s:%d: %w", interfaceName, listenIP, port, err)
	}
	tftpServer := tftp.NewServer(server.readFile, nil)
	tftpServer.SetTimeout(3 * time.Second)
	tftpServer.SetRetries(3)
	serveDone := make(chan error, 1)
	go func() { serveDone <- tftpServer.Serve(conn) }()
	select {
	case <-ctx.Done():
		tftpServer.Shutdown()
		_ = conn.Close()
		err := <-serveDone
		if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
			return nil
		}
		return err
	case err := <-serveDone:
		if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	}
}

func (server *Server) readFile(filename string, transfer io.ReaderFrom) error {
	safeName := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, filename)
	if len(safeName) > 256 {
		safeName = safeName[:256]
	}
	if peer, ok := transfer.(interface{ RemoteAddr() net.UDPAddr }); ok {
		remote := peer.RemoteAddr()
		safeName += " from " + remote.IP.String()
	}
	server.emit("INFO", "tftp.rrq", "TFTP read request received", "RECEIVED", safeName, "")
	if filename == "" || path.Clean(filename) != filename || path.IsAbs(filename) || strings.Contains(filename, `\`) {
		server.emit("WARN", "tftp.denied", "TFTP request denied: invalid path", "DENIED", safeName, "invalid path")
		return fmt.Errorf("TFTP request path is invalid")
	}
	if _, exists := server.files[filename]; !exists {
		server.emit("ERROR", "tftp.file_missing", "TFTP file not found: not in the generated allowlist", "FAILED", safeName, "file not found")
		return fmt.Errorf("TFTP file is not in the generated allowlist")
	}
	file, err := safefs.OpenReadOnly(server.root, filename)
	if err != nil {
		server.emit("ERROR", "tftp.file_missing", "TFTP file not found: allowlisted file is not staged", "FAILED", safeName, "no such file")
		return fmt.Errorf("open TFTP file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat TFTP file: %w", err)
	}
	if info.Size() > maxTFTPFileBytes {
		server.emit("ERROR", "tftp.transfer.failed", "TFTP transfer refused: file too large", "FAILED", safeName, "file too large")
		return fmt.Errorf("TFTP file exceeds %d bytes", maxTFTPFileBytes)
	}
	sent, err := transfer.ReadFrom(file)
	if err != nil {
		server.emit("ERROR", "tftp.transfer.failed", "TFTP transfer failed or timed out", "FAILED", safeName, err.Error())
		return err
	}
	server.emit("INFO", "tftp.transfer.completed", fmt.Sprintf("TFTP transfer complete (%d bytes)", sent), "COMPLETED", safeName, "")
	return nil
}
