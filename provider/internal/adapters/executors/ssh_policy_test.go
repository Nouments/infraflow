package executors

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"infraflow/provider/internal/infrastructure/credentials"

	"golang.org/x/crypto/ssh"
)

func TestSSHAlgorithmPolicyExcludesSHA1ByDefault(t *testing.T) {
	policy, err := NewSSHAlgorithmPolicy([]string{"lab/legacy-r1"})
	if err != nil {
		t.Fatal(err)
	}
	strict := &ssh.ClientConfig{}
	policy.Apply(AuthorizedDevice{Site: "lab", DeviceID: "modern-r2"}, strict)
	for _, list := range [][]string{strict.KeyExchanges, strict.MACs, strict.HostKeyAlgorithms} {
		if len(list) == 0 {
			t.Fatal("policy must set explicit algorithm lists")
		}
		for _, name := range list {
			if strings.Contains(name, "sha1") || name == ssh.KeyAlgoRSA || strings.Contains(name, "dss") {
				t.Fatalf("SHA-1/DSA algorithm %q offered to a non-exempt device", name)
			}
		}
	}
	legacy := &ssh.ClientConfig{}
	policy.Apply(AuthorizedDevice{Site: "LAB", DeviceID: "Legacy-R1"}, legacy)
	if !slices.Contains(legacy.KeyExchanges, ssh.InsecureKeyExchangeDH14SHA1) || !slices.Contains(legacy.HostKeyAlgorithms, ssh.KeyAlgoRSA) {
		t.Fatal("exempt device did not receive the legacy SHA-1 algorithms")
	}
	if slices.Contains(legacy.KeyExchanges, ssh.InsecureKeyExchangeDH1SHA1) || slices.Contains(legacy.KeyExchanges, ssh.InsecureKeyExchangeDHGEXSHA1) {
		t.Fatal("the exception must not enable group1 or group-exchange SHA-1")
	}
	if strict.KeyExchanges[0] != legacy.KeyExchanges[0] {
		t.Fatal("strong algorithms must stay preferred for exempt devices")
	}
}

func TestSSHAlgorithmPolicyRejectsMalformedTargets(t *testing.T) {
	for _, target := range []string{"", "lab", "lab/", "/r1", "lab/r1/x", "../r1"} {
		if _, err := NewSSHAlgorithmPolicy([]string{target}); err == nil {
			t.Fatalf("accepted malformed target %q", target)
		}
	}
}

// legacyOnlySSHServer accepts one connection offering only SHA-1 key exchange
// and an ssh-rsa (SHA-1) host key signature, like Cisco IOSv 15.9.
func legacyOnlySSHServer(t *testing.T) (string, int, ssh.PublicKey) {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(rsaKey)
	if err != nil {
		t.Fatal(err)
	}
	sha1Signer, err := ssh.NewSignerWithAlgorithms(signer.(ssh.AlgorithmSigner), []string{ssh.KeyAlgoRSA})
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{
		Config:           ssh.Config{KeyExchanges: []string{ssh.InsecureKeyExchangeDH14SHA1}, MACs: []string{ssh.HMACSHA1}, Ciphers: []string{ssh.CipherAES128CTR}},
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil },
	}
	config.AddHostKey(sha1Signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, chans, reqs, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					_ = ch.Reject(ssh.Prohibited, "test server")
				}
			}()
		}
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return host, port, signer.PublicKey()
}

func TestSSHTransportNegotiatesSHA1OnlyWithExplicitException(t *testing.T) {
	host, port, hostKey := legacyOnlySSHServer(t)
	target := AuthorizedDevice{Site: "lab", DeviceID: "r1", Vendor: "cisco", ManagementIP: host, SSHPort: port}
	verifier := NewStaticPublicKeyVerifier(map[string]ssh.PublicKey{net.JoinHostPort(host, strconv.Itoa(port)): hostKey})
	creds := credentials.DeviceCredentials{Username: "u", Password: "p"}

	strictTransport, _ := NewSSHIOSTransport(verifier, 2*time.Second, time.Second)
	_, err := strictTransport.Connect(context.Background(), target, creds)
	if err == nil || !strings.Contains(err.Error(), "no common algorithm") || !strings.Contains(err.Error(), `"lab/r1"`) {
		t.Fatalf("SHA-1-only device must be refused by default with an actionable message, got %v", err)
	}

	policy, _ := NewSSHAlgorithmPolicy([]string{"lab/r1"})
	legacyTransport, _ := NewSSHIOSTransport(verifier, 2*time.Second, time.Second)
	legacyTransport.WithAlgorithmPolicy(policy)
	_, err = legacyTransport.Connect(context.Background(), target, creds)
	// The handshake and authentication succeed; the test server then rejects
	// the session channel, which proves negotiation passed.
	if err == nil || !strings.Contains(err.Error(), "create ssh session") {
		t.Fatalf("exempt device did not complete the SHA-1 handshake: %v", err)
	}
}
