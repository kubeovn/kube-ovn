package daemon

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	v1 "k8s.io/api/certificates/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	ovnDBTLSCertPath = util.SslClientCertPath // /var/run/tls/client.crt
	ovnDBTLSKeyPath  = util.SslClientKeyPath  // /var/run/tls/client.key
	ovnDBTLSCAPath   = util.SslCAPath         // /var/run/tls/ca.crt

	ovnDBTLSServerCertPath = util.SslServerCertPath // /var/run/tls/server.crt
	ovnDBTLSServerKeyPath  = util.SslServerKeyPath  // /var/run/tls/server.key

	ovnDBTLSClientKey = "ovn-db-tls-client"
)

// shouldManageOVNDBTLSCert reports whether daemon-side OVN DB TLS cert
// management is active. The rotation switch is meaningless without SSL.
func (c *Controller) shouldManageOVNDBTLSCert() bool {
	if !c.config.EnableOVNDBTLSCertRotation {
		return false
	}
	if !c.config.EnableSSL {
		klog.Warning("enable-ovn-db-tls-cert-rotation requires ENABLE_SSL=true, ignored")
		return false
	}
	return true
}

// needNewOVNDBTLSCert returns true if the cert file does not exist, cannot be
// parsed, is expired, or has passed its half-life.
func needNewOVNDBTLSCert(certPath, keyPath string) (bool, error) {
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("failed to read cert: %w", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("failed to stat key: %w", err)
	}

	block, _ := pem.Decode(certBytes)
	if block == nil {
		return true, nil // corrupt
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true, nil // corrupt
	}
	if time.Now().After(cert.NotAfter) {
		return true, nil // expired
	}

	refreshTime := cert.NotBefore.Add(cert.NotAfter.Sub(cert.NotBefore) / 2)
	return time.Now().After(refreshTime), nil
}

// validateNewOVNDBTLSCert checks that a newly-signed certificate is valid
// before writing it to disk. Returns nil only when all checks pass.
func validateNewOVNDBTLSCert(certPEM []byte, privateKey *rsa.PrivateKey, expectedExtKeyUsage []x509.ExtKeyUsage) error {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return errors.New("failed to decode certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}
	if time.Now().After(cert.NotAfter) {
		return errors.New("certificate is already expired")
	}
	if !cert.PublicKey.(*rsa.PublicKey).Equal(privateKey.Public()) {
		return errors.New("certificate public key does not match private key")
	}
	// Check ExtKeyUsage: at least one of the expected usages must be present
	if len(expectedExtKeyUsage) > 0 {
		found := false
		for _, expected := range expectedExtKeyUsage {
			for _, actual := range cert.ExtKeyUsage {
				if actual == expected {
					found = true
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("certificate ExtKeyUsage %v does not contain any of %v", cert.ExtKeyUsage, expectedExtKeyUsage)
		}
	}
	return nil
}

// atomicWriteCert writes cert PEM and key to the target paths atomically
// (write to temp file, then rename).
func atomicWriteCert(certPath string, certPEM []byte, keyPath string, key *rsa.PrivateKey) error {
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	if err := atomicWriteFile(certPath, certPEM, 0o600); err != nil {
		return fmt.Errorf("failed to write cert: %w", err)
	}
	if err := atomicWriteFile(keyPath, keyPEM, 0o600); err != nil {
		return fmt.Errorf("failed to write key: %w", err)
	}
	return nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// untilOVNDBTLSCertRefresh returns the duration until the certificate's
// half-life point.
func untilOVNDBTLSCertRefresh(certPath string) (time.Duration, error) {
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read certificate: %w", err)
	}
	block, _ := pem.Decode(certBytes)
	if block == nil {
		return 0, errors.New("failed to decode PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return 0, fmt.Errorf("failed to parse certificate: %w", err)
	}
	refreshTime := cert.NotBefore.Add(cert.NotAfter.Sub(cert.NotBefore) / 2)
	return time.Until(refreshTime), nil
}

// requestOVNDBTLSCSR creates a CSR, waits for the controller signer to sign
// it, and returns the signed certificate PEM.
func (c *Controller) requestOVNDBTLSCSR(ctx context.Context, csrName string, key *rsa.PrivateKey, usage v1.KeyUsage) ([]byte, error) {
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		return nil, fmt.Errorf("failed to create CSR: %w", err)
	}

	csr := &v1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{Name: csrName},
		Spec: v1.CertificateSigningRequestSpec{
			Request:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}),
			SignerName: util.SignerName,
			Usages:     []v1.KeyUsage{usage},
		},
	}

	if _, err = c.config.KubeClient.CertificatesV1().CertificateSigningRequests().Create(ctx, csr, metav1.CreateOptions{}); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("failed to create CSR: %w", err)
		}
		klog.Infof("CSR %s already exists", csrName)
	}

	defer func() {
		if err := c.config.KubeClient.CertificatesV1().CertificateSigningRequests().Delete(context.Background(), csrName, metav1.DeleteOptions{}); err != nil {
			klog.Errorf("failed to delete CSR %s: %v", csrName, err)
		}
	}()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for CSR %s: %w", csrName, ctx.Err())
		case <-ticker.C:
			got, err := c.config.KubeClient.CertificatesV1().CertificateSigningRequests().Get(ctx, csrName, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to get CSR: %w", err)
			}
			if len(got.Status.Certificate) != 0 {
				return got.Status.Certificate, nil
			}
		}
	}
}

// SyncOVNDBTLSCerts checks whether a new certificate is needed, requests one
// from the controller signer, validates it, writes it to disk, and schedules
// the next rotation check. On any failure, the old certificate stays in place.
func (c *Controller) SyncOVNDBTLSCerts(key string) error {
	var certPath, keyPath string
	var usage v1.KeyUsage
	var expectedExtKeyUsage []x509.ExtKeyUsage

	switch key {
	case ovnDBTLSClientKey:
		certPath = ovnDBTLSCertPath
		keyPath = ovnDBTLSKeyPath
		usage = v1.UsageClientAuth
		expectedExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	default:
		return fmt.Errorf("unknown ovn db tls key: %s", key)
	}

	needsRenewal, err := needNewOVNDBTLSCert(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("check cert: %w", err)
	}
	if !needsRenewal {
		klog.V(4).Infof("ovn db tls cert %s still valid, skipping", key)
	} else {
		klog.Infof("requesting new ovn db tls cert for %s", key)
		newKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return fmt.Errorf("generate key: %w", err)
		}

		csrName := fmt.Sprintf("%s-%s", ovnDBTLSClientKey, c.config.NodeName)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()

		certPEM, err := c.requestOVNDBTLSCSR(ctx, csrName, newKey, usage)
		if err != nil {
			return fmt.Errorf("request cert: %w", err)
		}

		if err := validateNewOVNDBTLSCert(certPEM, newKey, expectedExtKeyUsage); err != nil {
			return fmt.Errorf("validate new cert (discarding): %w", err)
		}

		if err := atomicWriteCert(certPath, certPEM, keyPath, newKey); err != nil {
			return fmt.Errorf("write cert: %w", err)
		}
		klog.Infof("ovn db tls cert %s written successfully", key)
	}

	// Schedule next check at half-life
	untilRefresh, err := untilOVNDBTLSCertRefresh(certPath)
	if err != nil {
		klog.Errorf("calculating cert refresh time for %s: %v", key, err)
		untilRefresh = 5 * time.Minute // fallback: retry soon
	}
	c.ovnDBTLSQueue.AddAfter(key, untilRefresh)
	return nil
}
