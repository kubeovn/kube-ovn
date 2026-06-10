package controller

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestGenerateCACertificate(t *testing.T) {
	certPEM, keyPEM, err := generateCACertificate()
	if err != nil {
		t.Fatalf("generateCACertificate returned error: %v", err)
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		t.Fatal("failed to decode certificate PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}
	if !cert.IsCA {
		t.Fatal("certificate IsCA = false, want true")
	}
	if cert.Subject.CommonName != "kube-ovn-ca" {
		t.Fatalf("CommonName = %q, want %q", cert.Subject.CommonName, "kube-ovn-ca")
	}
	if cert.NotAfter.Sub(cert.NotBefore) < 9*365*24*time.Hour {
		t.Fatalf("cert validity = %v, want at least 9 years", cert.NotAfter.Sub(cert.NotBefore))
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "PRIVATE KEY" {
		t.Fatal("failed to decode key PEM, want PKCS#8 PRIVATE KEY block")
	}
	if _, err = x509.ParsePKCS8PrivateKey(keyBlock.Bytes); err != nil {
		t.Fatalf("failed to parse private key: %v", err)
	}
}

// TestGeneratedCAIsConsumableBySigner closes the loop between CA generation and
// the CSR signer: the secret written by initDefaultOVNCA must decode with the
// exact functions the signer uses, otherwise every CSR fails with CorruptCAKey.
func TestGeneratedCAIsConsumableBySigner(t *testing.T) {
	certPEM, keyPEM, err := generateCACertificate()
	if err != nil {
		t.Fatalf("generateCACertificate returned error: %v", err)
	}

	if _, err := decodeCertificate(certPEM); err != nil {
		t.Fatalf("signer decodeCertificate rejected generated CA cert: %v", err)
	}
	if _, err := decodePrivateKey(keyPEM); err != nil {
		t.Fatalf("signer decodePrivateKey rejected generated CA key: %v", err)
	}
}

func TestEnsureOVNCASecretCreatesMissingSecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	c := &Controller{
		config: &Configuration{
			KubeClient: client,
		},
	}

	err := c.ensureOVNCASecret("kube-system", util.DefaultOVNDBTLSCA, []byte("ca"), []byte("key"))
	if err != nil {
		t.Fatalf("ensureOVNCASecret returned error: %v", err)
	}

	secret, err := client.CoreV1().Secrets("kube-system").Get(context.Background(), util.DefaultOVNDBTLSCA, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get created secret: %v", err)
	}
	if string(secret.Data["cacert"]) != "ca" {
		t.Fatalf("cacert = %q, want %q", string(secret.Data["cacert"]), "ca")
	}
	if string(secret.Data["cakey"]) != "key" {
		t.Fatalf("cakey = %q, want %q", string(secret.Data["cakey"]), "key")
	}
}

func TestEnsureOVNCASecretKeepsExistingSecret(t *testing.T) {
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.DefaultOVNDBTLSCA,
			Namespace: "kube-system",
		},
		Data: map[string][]byte{
			"cacert": []byte("old-ca"),
			"cakey":  []byte("old-key"),
		},
	}
	client := fake.NewSimpleClientset(existing)
	c := &Controller{
		config: &Configuration{
			KubeClient: client,
		},
	}

	err := c.ensureOVNCASecret("kube-system", util.DefaultOVNDBTLSCA, []byte("new-ca"), []byte("new-key"))
	if err != nil {
		t.Fatalf("ensureOVNCASecret returned error: %v", err)
	}

	secret, err := client.CoreV1().Secrets("kube-system").Get(context.Background(), util.DefaultOVNDBTLSCA, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get existing secret: %v", err)
	}
	if string(secret.Data["cacert"]) != "old-ca" {
		t.Fatalf("cacert = %q, want existing %q", string(secret.Data["cacert"]), "old-ca")
	}
	if string(secret.Data["cakey"]) != "old-key" {
		t.Fatalf("cakey = %q, want existing %q", string(secret.Data["cakey"]), "old-key")
	}
}
