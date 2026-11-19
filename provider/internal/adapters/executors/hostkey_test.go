package executors

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func testHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func writeKnownHosts(t *testing.T, path string, key ssh.PublicKey) {
	t.Helper()
	line := knownhosts.Line([]string{"192.0.2.11"}, key) + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Regression (GNS3 IOSv run 2026-11-19): known_hosts was loaded once at
// startup, so replacing a trusted key in the file had no effect until the
// Provider restarted, and a mismatch reported only "expected ssh-rsa,
// presented ssh-rsa".
func TestKnownHostsFileVerifierAppliesFileChangesWithoutRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	trusted, attacker := testHostKey(t), testHostKey(t)
	writeKnownHosts(t, path, trusted)
	verifier, err := NewKnownHostsFileVerifier(path)
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("192.0.2.11"), Port: 22}
	if err := verifier.Verify("192.0.2.11:22", remote, trusted); err != nil {
		t.Fatalf("trusted key rejected: %v", err)
	}

	err = verifier.Verify("192.0.2.11:22", remote, attacker)
	if !errors.Is(err, ErrHostKeyMismatch) || !strings.Contains(err.Error(), ssh.FingerprintSHA256(trusted)) || !strings.Contains(err.Error(), ssh.FingerprintSHA256(attacker)) {
		t.Fatalf("mismatch must fail closed and name both fingerprints, got %v", err)
	}

	// Rotate the trusted key in place: the old key must stop working at once.
	writeKnownHosts(t, path, attacker)
	if err := verifier.Verify("192.0.2.11:22", remote, trusted); !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("revoked key must be rejected without a restart, got %v", err)
	}
	if err := verifier.Verify("192.0.2.11:22", remote, attacker); err != nil {
		t.Fatalf("rotated key must be accepted without a restart: %v", err)
	}

	other := &net.TCPAddr{IP: net.ParseIP("192.0.2.12"), Port: 22}
	if err := verifier.Verify("192.0.2.12:22", other, attacker); !errors.Is(err, ErrHostKeyUnknown) {
		t.Fatalf("unknown host must fail closed, got %v", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := verifier.Verify("192.0.2.11:22", remote, attacker); err == nil || !strings.Contains(err.Error(), "reload known_hosts") {
		t.Fatalf("unreadable known_hosts must fail closed, got %v", err)
	}
}
