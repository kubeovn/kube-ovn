package client

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewTLSConfigUsesClientCertificateAndRootCA(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, caPath := writeTestCertificateFiles(t, dir)

	config, err := newTLSConfig(certPath, keyPath, caPath, false)
	if err != nil {
		t.Fatalf("newTLSConfig returned error: %v", err)
	}

	if config.InsecureSkipVerify {
		t.Fatal("InsecureSkipVerify = true, want false")
	}
	if len(config.Certificates) != 1 {
		t.Fatalf("Certificates length = %d, want 1", len(config.Certificates))
	}
	if config.RootCAs == nil {
		t.Fatal("RootCAs = nil, want populated cert pool")
	}
	// ServerName must stay empty so tls.Dialer derives it per endpoint;
	// pinning it would break verification against other HA endpoints.
	if config.ServerName != "" {
		t.Fatalf("ServerName = %q, want empty", config.ServerName)
	}
}

func TestNewTLSConfigCanPreserveLegacyInsecureSkipVerify(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, caPath := writeTestCertificateFiles(t, dir)

	config, err := newTLSConfig(certPath, keyPath, caPath, true)
	if err != nil {
		t.Fatalf("newTLSConfig returned error: %v", err)
	}

	if !config.InsecureSkipVerify {
		t.Fatal("InsecureSkipVerify = false, want true for legacy kube-ovn-tls fallback")
	}
}

func writeTestCertificateFiles(t *testing.T, dir string) (certPath, keyPath, caPath string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "client",
		},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	certPath = filepath.Join(dir, "client.crt")
	keyPath = filepath.Join(dir, "client.key")
	caPath = filepath.Join(dir, "ca.crt")

	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("failed to write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}
	if err := os.WriteFile(caPath, certPEM, 0o600); err != nil {
		t.Fatalf("failed to write ca: %v", err)
	}

	return certPath, keyPath, caPath
}
