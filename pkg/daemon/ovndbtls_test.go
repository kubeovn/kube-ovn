package daemon

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

func TestNeedNewOVNDBTLSCert(t *testing.T) {
	dir := t.TempDir()

	t.Run("no cert file", func(t *testing.T) {
		got, err := needNewOVNDBTLSCert(filepath.Join(dir, "missing.crt"), filepath.Join(dir, "missing.key"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Fatal("expected true when cert file does not exist")
		}
	})

	t.Run("valid cert not expired not past half", func(t *testing.T) {
		key, _ := rsa.GenerateKey(rand.Reader, 2048)
		tmpl := &x509.Certificate{
			SerialNumber:          big.NewInt(1),
			NotBefore:             time.Now().Add(-1 * time.Hour),
			NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
			BasicConstraintsValid: true,
		}
		certDER, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

		certPath := filepath.Join(dir, "valid.crt")
		keyPath := filepath.Join(dir, "valid.key")
		os.WriteFile(certPath, certPEM, 0o600)
		os.WriteFile(keyPath, keyPEM, 0o600)

		got, err := needNewOVNDBTLSCert(certPath, keyPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got {
			t.Fatal("expected false for valid cert well before half-life")
		}
	})

	t.Run("cert past half life", func(t *testing.T) {
		key, _ := rsa.GenerateKey(rand.Reader, 2048)
		tmpl := &x509.Certificate{
			SerialNumber:          big.NewInt(2),
			NotBefore:             time.Now().Add(-6 * time.Hour),
			NotAfter:              time.Now().Add(4 * time.Hour),
			BasicConstraintsValid: true,
		}
		certDER, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

		certPath := filepath.Join(dir, "old.crt")
		keyPath := filepath.Join(dir, "old.key")
		os.WriteFile(certPath, certPEM, 0o600)
		os.WriteFile(keyPath, keyPEM, 0o600)

		got, err := needNewOVNDBTLSCert(certPath, keyPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Fatal("expected true for cert past half-life")
		}
	})
}

func TestValidateNewOVNDBTLSCert(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)

	t.Run("valid cert matches key", func(t *testing.T) {
		tmpl := &x509.Certificate{
			SerialNumber:          big.NewInt(1),
			Subject:               pkix.Name{CommonName: "test"},
			NotBefore:             time.Now().Add(-time.Minute),
			NotAfter:              time.Now().Add(time.Hour),
			KeyUsage:              x509.KeyUsageDigitalSignature,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			BasicConstraintsValid: true,
		}
		certDER, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

		if err := validateNewOVNDBTLSCert(certPEM, key, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
