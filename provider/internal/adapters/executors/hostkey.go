package executors

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var (
	ErrHostKeyUnknown  = errors.New("ssh: host key is unknown or not approved")
	ErrHostKeyMismatch = errors.New("ssh: host key mismatch (possible man-in-the-middle attack)")
)

// HostKeyVerifier verifies remote SSH host keys. Insecure host key acceptance is forbidden.
type HostKeyVerifier interface {
	Verify(hostname string, remote net.Addr, key ssh.PublicKey) error
}

// KnownHostsFileVerifier verifies host keys against an OpenSSH known_hosts file using knownhosts.New.
// The file is re-read on every verification so that a revoked or rotated key
// takes effect for the next connection without a Provider restart; a file that
// can no longer be read fails closed.
type KnownHostsFileVerifier struct {
	path string
}

// NewKnownHostsFileVerifier creates a known_hosts verifier. The file must be
// loadable at construction so configuration errors surface at startup.
func NewKnownHostsFileVerifier(path string) (*KnownHostsFileVerifier, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("known_hosts file path is required")
	}
	if _, err := knownhosts.New(path); err != nil {
		return nil, fmt.Errorf("load known_hosts file %q: %w", path, err)
	}
	return &KnownHostsFileVerifier{path: path}, nil
}

// Verify checks the key against the current contents of known_hosts.
func (v *KnownHostsFileVerifier) Verify(hostname string, remote net.Addr, key ssh.PublicKey) error {
	callback, err := knownhosts.New(v.path)
	if err != nil {
		return fmt.Errorf("verify host key: reload known_hosts: %w", err)
	}
	err = callback(hostname, remote, key)
	if err == nil {
		return nil
	}
	var keyErr *knownhosts.KeyError
	if errors.As(err, &keyErr) {
		if len(keyErr.Want) > 0 {
			return fmt.Errorf("%w for %s: expected %s %s, presented %s %s", ErrHostKeyMismatch, hostname,
				keyErr.Want[0].Key.Type(), ssh.FingerprintSHA256(keyErr.Want[0].Key), key.Type(), ssh.FingerprintSHA256(key))
		}
		return fmt.Errorf("%w for %s: presented %s %s", ErrHostKeyUnknown, hostname, key.Type(), ssh.FingerprintSHA256(key))
	}
	return fmt.Errorf("verify host key: %w", err)
}

// FixedFingerprintVerifier checks the SHA-256 fingerprint of the presented host key against a configured fingerprint.
// Format: "SHA256:..." or base64 raw hash.
type FixedFingerprintVerifier struct {
	expectedFingerprint string
}

// NewFixedFingerprintVerifier creates a fixed SHA-256 fingerprint verifier.
func NewFixedFingerprintVerifier(fingerprint string) (*FixedFingerprintVerifier, error) {
	clean := strings.TrimSpace(fingerprint)
	if clean == "" {
		return nil, errors.New("host key fingerprint must not be empty")
	}
	if !strings.HasPrefix(clean, "SHA256:") {
		clean = "SHA256:" + clean
	}
	return &FixedFingerprintVerifier{expectedFingerprint: clean}, nil
}

// Verify compares the SHA-256 fingerprint.
func (v *FixedFingerprintVerifier) Verify(hostname string, remote net.Addr, key ssh.PublicKey) error {
	sum := sha256.Sum256(key.Marshal())
	actual := "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	if actual != v.expectedFingerprint {
		return fmt.Errorf("%w: expected %s, got %s", ErrHostKeyMismatch, v.expectedFingerprint, actual)
	}
	return nil
}

// StaticPublicKeyVerifier checks that the presented host key strictly matches a pre-approved ssh.PublicKey.
type StaticPublicKeyVerifier struct {
	trustedKeys map[string]ssh.PublicKey
}

// NewStaticPublicKeyVerifier creates a static key verifier.
func NewStaticPublicKeyVerifier(trustedKeys map[string]ssh.PublicKey) *StaticPublicKeyVerifier {
	return &StaticPublicKeyVerifier{trustedKeys: trustedKeys}
}

// Verify checks if the key matches the approved key for the given host.
func (v *StaticPublicKeyVerifier) Verify(hostname string, remote net.Addr, key ssh.PublicKey) error {
	hostOnly, _, err := net.SplitHostPort(hostname)
	if err != nil {
		hostOnly = hostname
	}
	trusted, exists := v.trustedKeys[hostOnly]
	if !exists {
		trusted, exists = v.trustedKeys[hostname]
	}
	if !exists {
		return fmt.Errorf("%w for %s", ErrHostKeyUnknown, hostname)
	}
	if !bytes.Equal(trusted.Marshal(), key.Marshal()) {
		return fmt.Errorf("%w for %s", ErrHostKeyMismatch, hostname)
	}
	return nil
}
