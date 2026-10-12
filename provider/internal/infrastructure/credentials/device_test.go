package credentials

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStaticDeviceCredentialResolver(t *testing.T) {
	resolver := NewStaticDeviceCredentialResolver(map[string]DeviceCredentials{
		"paris/rtr-01": {Username: "cisco", Password: "secretPassword"},
	})

	creds, err := resolver.ResolveDeviceCredentials(context.Background(), "paris", "rtr-01")
	if err != nil {
		t.Fatalf("expected successful resolution, got %v", err)
	}
	if creds.Username != "cisco" || creds.Password != "secretPassword" {
		t.Fatalf("unexpected credentials: %+v", creds)
	}

	_, err = resolver.ResolveDeviceCredentials(context.Background(), "paris", "unknown")
	if err == nil {
		t.Fatal("expected error for unknown target, got nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = resolver.ResolveDeviceCredentials(ctx, "paris", "rtr-01")
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestEnvDeviceCredentialResolver(t *testing.T) {
	tempDir := t.TempDir()
	keyPath := filepath.Join(tempDir, "id_rsa")
	if err := os.WriteFile(keyPath, []byte("fake-private-key"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("INFRAFLOW_DEVICE_PARIS_RTR_01_SSH_USER", "admin")
	t.Setenv("INFRAFLOW_DEVICE_PARIS_RTR_01_SSH_PASSWORD", "adminpass")

	t.Setenv("INFRAFLOW_CISCO_SSH_USER", "globaluser")
	t.Setenv("INFRAFLOW_CISCO_SSH_KEY_FILE", keyPath)

	resolver := NewEnvDeviceCredentialResolver()

	// Specific target credentials
	creds, err := resolver.ResolveDeviceCredentials(context.Background(), "paris", "rtr-01")
	if err != nil {
		t.Fatalf("expected resolution, got %v", err)
	}
	if creds.Username != "admin" || creds.Password != "adminpass" {
		t.Fatalf("unexpected creds: %+v", creds)
	}

	// Global fallback credentials
	creds2, err := resolver.ResolveDeviceCredentials(context.Background(), "lyon", "sw-02")
	if err != nil {
		t.Fatalf("expected resolution via global fallback, got %v", err)
	}
	if creds2.Username != "globaluser" || string(creds2.PrivateKey) != "fake-private-key" {
		t.Fatalf("unexpected creds: %+v", creds2)
	}
}
