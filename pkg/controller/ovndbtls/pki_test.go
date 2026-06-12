package ovndbtls

import (
	"crypto/x509"
	"testing"
	"time"
)

func TestGenerateCAAndSignLeaf(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	ca, err := GenerateCA(now, "test-ca", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA returned error: %v", err)
	}
	if !ca.Cert.IsCA {
		t.Fatal("generated certificate is not a CA")
	}
	if ca.Cert.Subject.CommonName != "test-ca" {
		t.Fatalf("CA common name = %q, want test-ca", ca.Cert.Subject.CommonName)
	}
	if _, err := ParseCA(ca.CertPEM, ca.KeyPEM); err != nil {
		t.Fatalf("ParseCA returned error: %v", err)
	}

	certPEM, keyPEM, err := SignLeaf(now, ca, LeafSpec{
		CommonName:  "client",
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		Duration:    time.Hour,
	})
	if err != nil {
		t.Fatalf("SignLeaf returned error: %v", err)
	}
	if err := ValidateKeyPair(certPEM, keyPEM, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}); err != nil {
		t.Fatalf("ValidateKeyPair returned error: %v", err)
	}
	cert, err := ParseCertificate(certPEM)
	if err != nil {
		t.Fatalf("ParseCertificate returned error: %v", err)
	}
	if len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Fatalf("ExtKeyUsage = %v, want client auth", cert.ExtKeyUsage)
	}
}

func TestValidateKeyPairRejectsWrongUsage(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	ca, err := GenerateCA(now, "test-ca", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA returned error: %v", err)
	}
	certPEM, keyPEM, err := SignLeaf(now, ca, LeafSpec{
		CommonName:  "server",
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		Duration:    time.Hour,
	})
	if err != nil {
		t.Fatalf("SignLeaf returned error: %v", err)
	}
	if err := ValidateKeyPair(certPEM, keyPEM, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}); err == nil {
		t.Fatal("ValidateKeyPair returned nil, want wrong usage error")
	}
}
