package providergrpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"infraflow/internal/infrastructure/security"
)

func TestNewWithTLSConfigPerformsVerifiedMutualTLS(t *testing.T) {
	serverCA, serverCAPEM, serverKey := newTestCertificateAuthority(t)
	serverCert := issueTestCertificate(t, serverCA, serverKey, "provider", x509.ExtKeyUsageServerAuth, true)
	clientCert := issueTestCertificate(t, serverCA, serverKey, "agent", x509.ExtKeyUsageClientAuth, false)
	_, wrongCAPEM, _ := newTestCertificateAuthority(t)

	certificatePath := filepath.Join(t.TempDir(), "agent.crt")
	keyPath := filepath.Join(t.TempDir(), "agent.key")
	caPath := filepath.Join(t.TempDir(), "provider-ca.crt")
	wrongCAPath := filepath.Join(t.TempDir(), "wrong-ca.crt")
	writeTestFile(t, certificatePath, clientCert.certificatePEM)
	writeTestFile(t, keyPath, clientCert.privateKeyPEM)
	writeTestFile(t, caPath, serverCAPEM)
	writeTestFile(t, wrongCAPath, wrongCAPEM)

	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(serverCAPEM) {
		t.Fatal("test CA was not accepted")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{serverCert.certificate},
		ClientCAs:    clientCAs,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	})))
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthServer)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
		<-serveDone
	})

	validTLS := TLSConfig{
		Enabled: true, CAFile: caPath, CertificateFile: certificatePath, KeyFile: keyPath,
	}
	if err := checkProviderHealth(t, listener.Addr().String(), TLSConfig{Enabled: true, CAFile: caPath}); err == nil {
		t.Fatal("provider accepted a client connection without the required client certificate")
	}
	wrongTrust := validTLS
	wrongTrust.CAFile = wrongCAPath
	if err := checkProviderHealth(t, listener.Addr().String(), wrongTrust); err == nil {
		t.Fatal("agent accepted a provider certificate signed by an untrusted CA")
	}
	if err := checkProviderHealth(t, listener.Addr().String(), validTLS); err != nil {
		t.Fatalf("valid mutual TLS connection failed: %v", err)
	}
}

func checkProviderHealth(t *testing.T, address string, tlsConfig TLSConfig) error {
	t.Helper()
	client, err := NewWithTLSConfig(address, strings.Repeat("t", security.MinAgentTokenBytes), tlsConfig, "agent-01")
	if err != nil {
		return err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = healthpb.NewHealthClient(client.connection).Check(ctx, &healthpb.HealthCheckRequest{})
	return err
}

type testCertificate struct {
	certificate    tls.Certificate
	certificatePEM []byte
	privateKeyPEM  []byte
}

func newTestCertificateAuthority(t *testing.T) (*x509.Certificate, []byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "InfraFlow test CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), key
}

func issueTestCertificate(t *testing.T, authority *x509.Certificate, authorityKey *ecdsa.PrivateKey, name string, usage x509.ExtKeyUsage, server bool) testCertificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	if server {
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		template.DNSNames = []string{"localhost"}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, authority, &key.PublicKey, authorityKey)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})
	certificate, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return testCertificate{certificate: certificate, certificatePEM: certificatePEM, privateKeyPEM: privateKeyPEM}
}

func writeTestFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}
