package executors

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeResolverInventory(t *testing.T, root, content string) {
	t.Helper()
	path := filepath.Join(root, "lab", "inventory.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestArtifactInventoryDeviceResolverValidatesAuthorizedTarget(t *testing.T) {
	root := t.TempDir()
	writeResolverInventory(t, root, `{"site":"lab","devices":[{"id":"router-one","name":"R1","vendor":"Cisco","family":"ios","management":{"ipv4":"192.0.2.10"}}]}`)
	resolver := NewArtifactInventoryDeviceResolver(root)

	device, err := resolver.ResolveDevice(context.Background(), "lab", "R1")
	if err != nil {
		t.Fatalf("expected authorized Cisco target, got %v", err)
	}
	if device.Site != "lab" || device.DeviceID != "R1" || device.ManagementIP != "192.0.2.10" || device.SSHPort != 22 {
		t.Fatalf("unexpected resolved device: %+v", device)
	}
}

func TestArtifactInventoryDeviceResolverRejectsInvalidInventories(t *testing.T) {
	testCases := []struct {
		name       string
		inventory  string
		wantReason string
	}{
		{name: "malformed JSON", inventory: `{`, wantReason: "decode site inventory"},
		{name: "site mismatch", inventory: `{"site":"other","devices":[{"name":"R1","vendor":"cisco","management":{"ipv4":"192.0.2.10"}}]}`, wantReason: "inconsistent"},
		{name: "empty devices", inventory: `{"site":"lab","devices":[]}`, wantReason: "inconsistent"},
		{name: "invalid identity", inventory: `{"site":"lab","devices":[{"name":"","vendor":"cisco"}]}`, wantReason: "invalid device identity"},
		{name: "duplicate identity", inventory: `{"site":"lab","devices":[{"name":"R1","vendor":"cisco","management":{"ipv4":"192.0.2.10"}},{"name":"R1","vendor":"cisco","management":{"ipv4":"192.0.2.11"}}]}`, wantReason: "duplicate device identities"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			writeResolverInventory(t, root, testCase.inventory)
			_, err := NewArtifactInventoryDeviceResolver(root).ResolveDevice(context.Background(), "lab", "R1")
			if err == nil || !strings.Contains(err.Error(), testCase.wantReason) {
				t.Fatalf("expected %q error, got %v", testCase.wantReason, err)
			}
		})
	}
}

func TestArtifactInventoryDeviceResolverRejectsMissingUnauthorizedAndInvalidTargets(t *testing.T) {
	resolver := NewArtifactInventoryDeviceResolver(t.TempDir())
	if _, err := resolver.ResolveDevice(context.Background(), "lab", "R1"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("expected absent inventory to reject target, got %v", err)
	}

	root := t.TempDir()
	writeResolverInventory(t, root, `{"site":"lab","devices":[{"name":"R1","vendor":"mikrotik","management":{"ipv4":"192.0.2.10"}},{"name":"R2","vendor":"cisco","management":{"ipv4":"not-an-ip"}}]}`)
	resolver = NewArtifactInventoryDeviceResolver(root)
	if _, err := resolver.ResolveDevice(context.Background(), "lab", "unknown"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("expected unknown device to be rejected, got %v", err)
	}
	if _, err := resolver.ResolveDevice(context.Background(), "lab", "R1"); !errors.Is(err, ErrTargetVendorUnsupported) {
		t.Fatalf("expected non-Cisco device to be rejected, got %v", err)
	}
	if _, err := resolver.ResolveDevice(context.Background(), "lab", "R2"); !errors.Is(err, ErrTargetInvalidManagementIP) {
		t.Fatalf("expected invalid management IP to be rejected, got %v", err)
	}
	if _, err := resolver.ResolveDevice(context.Background(), "../lab", "R1"); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("expected invalid site path to be rejected, got %v", err)
	}
	for _, address := range []string{"not-an-ip", "::1", "0.0.0.0", "127.0.0.1", "224.0.0.1", "255.255.255.255"} {
		_, err := validateAuthorizedDevice(AuthorizedDevice{Site: "lab", DeviceID: "R1", Vendor: "cisco", ManagementIP: address})
		if !errors.Is(err, ErrTargetInvalidManagementIP) {
			t.Errorf("expected invalid management IP %q to be rejected, got %v", address, err)
		}
	}
}
