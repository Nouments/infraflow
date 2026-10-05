package tftpserver

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
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
