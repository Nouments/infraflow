package cli

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	providerconfig "infraflow/provider/internal/adapters/config"
)

func TestGRPCTLSConfigurationRequiresVerifiedClientCertificatesWhenConfigured(t *testing.T) {
	certificatePath, keyPath, caPath := writeServerTLSFiles(t)
	configuration, err := grpcTLSConfiguration(providerconfig.TLSConfig{
		CertificateFile: certificatePath,
		KeyFile:         keyPath,
		ClientCAFile:    caPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.MinVersion < tls.VersionTLS12 ||
		configuration.ClientAuth != tls.RequireAndVerifyClientCert ||
		configuration.ClientCAs == nil || len(configuration.Certificates) != 1 {
		t.Fatalf("mTLS configuration is not enforcing verified client certificates: %#v", configuration)
	}

	legacy, err := grpcTLSConfiguration(providerconfig.TLSConfig{
		CertificateFile: certificatePath,
		KeyFile:         keyPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.ClientAuth != tls.NoClientCert {
		t.Fatalf("server-authenticated TLS compatibility mode unexpectedly requires client certificates: %v", legacy.ClientAuth)
	}

	invalidCA := filepath.Join(t.TempDir(), "invalid-ca.pem")
	if err := os.WriteFile(invalidCA, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := grpcTLSConfiguration(providerconfig.TLSConfig{
		CertificateFile: certificatePath,
		KeyFile:         keyPath,
		ClientCAFile:    invalidCA,
	}); err == nil {
		t.Fatal("invalid client CA was accepted")
	}
}

func writeServerTLSFiles(t *testing.T) (string, string, string) {
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
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})
	root := t.TempDir()
	certificatePath := filepath.Join(root, "provider.crt")
	keyPath := filepath.Join(root, "provider.key")
	caPath := filepath.Join(root, "agent-ca.crt")
	for path, data := range map[string][]byte{
		certificatePath: certificatePEM,
		keyPath:         privateKeyPEM,
		caPath:          certificatePEM,
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return certificatePath, keyPath, caPath
}
