package tftpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pin/tftp/v3"
)

func TestTFTPTransfersOnlyAllowedRuntimeFiles(t *testing.T) {
	bootstrapDirectory := t.TempDir()
	root := filepath.Join(bootstrapDirectory, "tftp")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	contents := []byte("pxe loader fixture")
	if err := os.WriteFile(filepath.Join(root, "undionly.kpxe"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	config := Config{
		Version: 1, Service: "tftp", Enabled: true, Site: "lab",
		RootDirectory: "tftp", ReadOnly: true,
		AllowedFiles: []FileRule{{Path: "undionly.kpxe", Source: "agent_runtime"}},
	}
	bootstrapServer, err := NewServer(config, bootstrapDirectory)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	protocolServer := tftp.NewServer(bootstrapServer.readFile, nil)
	protocolServer.SetTimeout(100 * time.Millisecond)
	protocolServer.SetRetries(1)
	serveDone := make(chan error, 1)
	go func() { serveDone <- protocolServer.Serve(listener) }()
	t.Cleanup(func() {
		protocolServer.Shutdown()
		_ = listener.Close()
		select {
		case <-serveDone:
		case <-time.After(time.Second):
			t.Error("TFTP server did not stop")
		}
	})

	client, err := tftp.NewClient(listener.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	client.SetTimeout(time.Second)
	client.SetRetries(1)
	transfer, err := client.Receive("undionly.kpxe", "octet")
	if err != nil {
		t.Fatalf("request allowed file: %v", err)
	}
	var downloaded bytes.Buffer
	if _, err := transfer.WriteTo(&downloaded); err != nil {
		t.Fatalf("download allowed file: %v", err)
	}
	if !bytes.Equal(downloaded.Bytes(), contents) {
		t.Fatalf("downloaded %q, want %q", downloaded.Bytes(), contents)
	}

	client.SetTimeout(50 * time.Millisecond)
	transfer, err = client.Receive("private.txt", "octet")
	if err == nil {
		_, err = transfer.WriteTo(io.Discard)
	}
	if err == nil {
		t.Fatal("TFTP server allowed a file outside the generated allowlist")
	}
}

func TestTFTPConfigRejectsWritableOrUnsafeEntries(t *testing.T) {
	config := Config{
		Version: 1, Service: "tftp", Enabled: true, Site: "lab",
		RootDirectory: "tftp", ReadOnly: false,
		AllowedFiles: []FileRule{{Path: "../secret", Source: "agent_runtime"}},
	}
	if err := config.Validate(); err == nil {
		t.Fatal("expected writable TFTP config to be rejected")
	}
	config.ReadOnly = true
	if err := config.Validate(); err == nil {
		t.Fatal("expected unsafe TFTP path to be rejected")
	}
}

func TestNewServerRequiresEnabledServiceAndRealRoot(t *testing.T) {
	config := Config{
		Version: 1, Service: "tftp", Enabled: false, Site: "lab",
		RootDirectory: "tftp", ReadOnly: true,
		AllowedFiles: []FileRule{{Path: "undionly.kpxe", Source: "agent_runtime"}},
	}
	if _, err := NewServer(config, t.TempDir()); err == nil {
		t.Fatal("expected disabled service to be rejected")
	}
	config.Enabled = true
	if _, err := NewServer(config, t.TempDir()); err == nil {
		t.Fatal("expected missing TFTP root to be rejected")
	}
}

func TestLoadConfigValidatesOneStrictDocument(t *testing.T) {
	config := validTFTPConfig()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "tftp.json")
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(filename)
	if err != nil || loaded.Site != config.Site {
		t.Fatalf("valid TFTP config did not load: %#v, %v", loaded, err)
	}
	if err := os.WriteFile(filename, append(data, []byte(` {}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(filename); err == nil {
		t.Fatal("expected trailing JSON document to be rejected")
	}
	if err := os.WriteFile(filename, []byte(`{"version":1,"service":"tftp","enabled":true,"site":"lab","root_directory":"tftp","read_only":true,"allowed_files":[],"unsafe":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(filename); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown config field rejection, got %v", err)
	}
}

func TestTFTPServeRejectsInvalidRuntimeBindings(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "tftp"), 0o700); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(validTFTPConfig(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		ctx       context.Context
		iface     string
		address   string
		port      int
		wantError string
	}{
		{name: "nil context", ctx: nil, iface: "lo", address: "127.0.0.1", port: 69, wantError: "context"},
		{name: "missing interface", ctx: context.Background(), iface: "", address: "127.0.0.1", port: 69, wantError: "required"},
		{name: "invalid port", ctx: context.Background(), iface: "lo", address: "127.0.0.1", port: 0, wantError: "required"},
		{name: "invalid address", ctx: context.Background(), iface: "lo", address: "not-an-ip", port: 69, wantError: "IPv4"},
		{name: "unknown interface", ctx: context.Background(), iface: "infraflow-missing0", address: "127.0.0.1", port: 69, wantError: "find TFTP interface"},
		{name: "address not assigned", ctx: context.Background(), iface: "lo", address: "192.0.2.22", port: 69, wantError: "not assigned"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := server.Serve(test.ctx, test.iface, test.address, test.port)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected %q error, got %v", test.wantError, err)
			}
		})
	}
}

func TestTFTPReadFileRejectsUnsafeMissingAndSymlinkFiles(t *testing.T) {
	bootstrapDirectory := t.TempDir()
	root := filepath.Join(bootstrapDirectory, "tftp")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(validTFTPConfig(), bootstrapDirectory)
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"../secret", "unlisted.bin", "undionly.kpxe"} {
		var transfer bytes.Buffer
		if err := server.readFile(filename, &transfer); err == nil {
			t.Errorf("unsafe, unlisted, or missing file %q was accepted", filename)
		}
	}
	outside := filepath.Join(bootstrapDirectory, "outside.bin")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "undionly.kpxe")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var transfer bytes.Buffer
	if err := server.readFile("undionly.kpxe", &transfer); err == nil {
		t.Fatal("TFTP followed a symlink out of its root")
	}
}

func validTFTPConfig() Config {
	return Config{
		Version: 1, Service: "tftp", Enabled: true, Site: "lab",
		RootDirectory: "tftp", ReadOnly: true,
		AllowedFiles: []FileRule{{Path: "undionly.kpxe", Source: "agent_runtime"}},
	}
}
