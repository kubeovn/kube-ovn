package controller

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const caCertDuration = 10 * 365 * 24 * time.Hour

func (c *Controller) InitDefaultOVNIPsecCA() error {
	return c.initDefaultOVNCA(util.DefaultOVNIPSecCA, "OVN IPSec")
}

func (c *Controller) InitDefaultOVNDBTLSCA() error {
	return c.initDefaultOVNCA(util.DefaultOVNDBTLSCA, "OVN DB TLS")
}

// shouldManageOVNDBTLSCert reports whether built-in OVN DB TLS certificate
// management (CA + CSR signer) is active. The cert switch is meaningless
// without SSL, so it is ignored (with a warning at startup) in that case.
func (c *Controller) shouldManageOVNDBTLSCert() bool {
	if !c.config.EnableOVNDBTLSCert {
		return false
	}
	if !c.config.EnableSSL {
		klog.Warning("enable-ovn-db-tls-cert requires ENABLE_SSL=true, ignored")
		return false
	}
	return true
}

func (c *Controller) initDefaultOVNCA(secretName, displayName string) error {
	namespace := os.Getenv(util.EnvPodNamespace)
	_, err := c.config.KubeClient.CoreV1().Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err == nil {
		klog.Infof("%s CA secret already exists, skip", displayName)
		return nil
	}

	if !k8serrors.IsNotFound(err) {
		return err
	}

	cacert, cakey, err := generateCACertificate()
	if err != nil {
		return fmt.Errorf("failed to generate CA: %w", err)
	}

	if err = c.ensureOVNCASecret(namespace, secretName, cacert, cakey); err != nil {
		return err
	}

	klog.Infof("%s CA secret init successfully", displayName)
	return nil
}

func generateCACertificate() (certPEM, keyPEM []byte, err error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "kube-ovn-ca",
			Organization: []string{"kube-ovn"},
		},
		NotBefore:             time.Now().Add(-1 * time.Second),
		NotAfter:              time.Now().Add(caCertDuration),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	// the signer (decodePrivateKey in signer.go) only accepts PKCS#8 "PRIVATE KEY" blocks
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

func (c *Controller) ensureOVNCASecret(namespace, secretName string, cacert, cakey []byte) error {
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"cacert": cacert,
			"cakey":  cakey,
		},
	}

	if _, err := c.config.KubeClient.CoreV1().Secrets(namespace).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
		// another controller replica may have created it concurrently
		if k8serrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}

	return nil
}
