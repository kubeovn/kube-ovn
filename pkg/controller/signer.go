package controller

import (
	"bytes"
	"context"
	c "crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"slices"
	"strings"
	"time"

	csrv1 "k8s.io/api/certificates/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) validateCsrName(name string) error {
	after, found := strings.CutPrefix(name, "ovn-ipsec-")
	if !found || len(after) == 0 {
		return fmt.Errorf("CSR name %s is invalid, must be in format ovn-ipsec-<node-name>", name)
	}

	node, err := c.nodesLister.Get(after)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("node %s not found for CSR %s", after, name)
		}
		return fmt.Errorf("failed to get node %s for CSR %s: %w", after, name, err)
	}
	if node.Status.NodeInfo.OperatingSystem != "linux" {
		return fmt.Errorf("node %s is not linux, CSR %s is invalid", after, name)
	}

	return nil
}

func (c *Controller) isOVNIPSecCSR(csr *csrv1.CertificateSigningRequest) bool {
	if csr.Spec.SignerName != util.SignerName ||
		!strings.HasPrefix(csr.Name, "ovn-ipsec-") ||
		!slices.Equal(csr.Spec.Usages, []csrv1.KeyUsage{csrv1.UsageIPsecTunnel}) {
		return false
	}
	if err := c.validateCsrName(csr.Name); err != nil {
		klog.Warningf("CSR %s validation failed: %v", csr.Name, err)
		return false
	}
	return true
}

func (c *Controller) enqueueAddCsr(obj any) {
	req := obj.(*csrv1.CertificateSigningRequest)
	if !c.isOVNIPSecCSR(req) {
		return
	}

	key := cache.MetaObjectToName(req).String()
	klog.V(3).Infof("enqueue add csr %s", key)
	c.addOrUpdateCsrQueue.Add(key)
}

func (c *Controller) enqueueUpdateCsr(oldObj, newObj any) {
	oldCsr := oldObj.(*csrv1.CertificateSigningRequest)
	newCsr := newObj.(*csrv1.CertificateSigningRequest)
	if oldCsr.ResourceVersion == newCsr.ResourceVersion {
		return
	}
	if !c.isOVNIPSecCSR(newCsr) {
		return
	}

	key := cache.MetaObjectToName(newCsr).String()
	klog.V(3).Infof("enqueue update csr %s", key)
	c.addOrUpdateCsrQueue.Add(key)
}

func (c *Controller) handleAddOrUpdateCsr(key string) (err error) {
	csr, err := c.csrLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if len(csr.Status.Certificate) != 0 {
		// Request already has a certificate. There is nothing
		// to do as we will, currently, not re-certify or handle any updates to
		// CSRs.
		return nil
	}

	// We will make the assumption that anyone with permission to issue a
	// certificate signing request to this signer is automatically approved. This
	// is somewhat protected by permissions on the CSR resource.
	// TODO: We may need a more robust way to do this later
	if !isCertificateRequestApproved(csr) {
		csr.Status.Conditions = append(csr.Status.Conditions, csrv1.CertificateSigningRequestCondition{
			Type:    csrv1.CertificateApproved,
			Status:  "True",
			Reason:  "AutoApproved",
			Message: "Automatically approved by " + util.SignerName,
		})
		// Update status to "Approved"
		_, err = c.config.KubeClient.CertificatesV1().CertificateSigningRequests().UpdateApproval(context.TODO(), csr.Name, csr, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("Unable to approve certificate for %v and signer %v: %v", csr.Name, util.SignerName, err)
			return err
		}

		return nil
	}
	// From this, point we are dealing with an approved CSR
	// Get CA in from ovn-ipsec-ca
	caSecret, err := c.config.KubeClient.CoreV1().Secrets(os.Getenv(util.EnvPodNamespace)).Get(context.TODO(), util.DefaultOVNIPSecCA, metav1.GetOptions{})
	if err != nil {
		c.signerFailure(csr, "CAFailure",
			fmt.Sprintf("Could not get CA certificate and key: %v", err))
		return err
	}

	// Decode the certificate request from PEM format.
	certReq, err := decodeCertificateRequest(csr.Spec.Request)
	if err != nil {
		// We dont degrade the status of the controller as this is due to a
		// malformed CSR rather than an issue with the controller.
		if err := c.updateCSRStatusConditions(csr, "CSRDecodeFailure", fmt.Sprintf("Could not decode Certificate Request: %v", err)); err != nil {
			klog.Error(err)
		}
		return nil
	}

	// Decode the CA certificate from PEM format.
	caCert, err := decodeCertificate(caSecret.Data["cacert"])
	if err != nil {
		c.signerFailure(csr, "CorruptCACert",
			fmt.Sprintf("Unable to decode CA certificate for %v: %v", util.SignerName, err))
		return nil
	}

	caKey, err := decodePrivateKey(caSecret.Data["cakey"])
	if err != nil {
		c.signerFailure(csr, "CorruptCAKey",
			fmt.Sprintf("Unable to decode CA private key for %v: %v", util.SignerName, err))
		return nil
	}

	// Create a new certificate using the certificate template and certificate.
	// We can then sign this using the CA.
	signedCert, err := signCSR(newCertificateTemplate(certReq), certReq.PublicKey, caCert, caKey)
	if err != nil {
		c.signerFailure(csr, "SigningFailure",
			fmt.Sprintf("Unable to sign certificate for %v and signer %v: %v", csr.Name, util.SignerName, err))
		return nil
	}

	// Encode the certificate into PEM format and add to the status of the CSR
	csr.Status.Certificate, err = encodeCertificates(signedCert)
	if err != nil {
		c.signerFailure(csr, "EncodeFailure",
			fmt.Sprintf("Could not encode certificate: %v", err))
		return nil
	}

	if err := c.updateCsrStatus(csr); err != nil {
		return err
	}

	klog.Infof("Certificate signed, issued and approved for %s by %s", csr.Name, util.SignerName)
	return nil
}

// Something has gone wrong with the signer controller so we update the statusmanager, the csr
// and log.
func (c *Controller) signerFailure(csr *csrv1.CertificateSigningRequest, reason, message string) {
	klog.Errorf("%s: %s", reason, message)
	if err := c.updateCSRStatusConditions(csr, reason, message); err != nil {
		klog.Error(err)
	}
}

// Update the status conditions on the CSR object
func (c *Controller) updateCSRStatusConditions(csr *csrv1.CertificateSigningRequest, reason, message string) error {
	csr.Status.Conditions = append(csr.Status.Conditions, csrv1.CertificateSigningRequestCondition{
		Type:    csrv1.CertificateFailed,
		Status:  "True",
		Reason:  reason,
		Message: message,
	})

	if err := c.updateCsrStatus(csr); err != nil {
		return err
	}

	return nil
}

// updateCsrStatus updates the status of a CSR using the Update method instead of Patch
func (c *Controller) updateCsrStatus(csr *csrv1.CertificateSigningRequest) error {
	if _, err := c.config.KubeClient.CertificatesV1().CertificateSigningRequests().UpdateStatus(context.Background(), csr, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("failed to update status for csr %s: %v", csr.Name, err)
		return err
	}
	return nil
}

// isCertificateRequestApproved returns true if a certificate request has the
// "Approved" condition and no "Denied" conditions; false otherwise.
func isCertificateRequestApproved(csr *csrv1.CertificateSigningRequest) bool {
	approved, denied := getCertApprovalCondition(&csr.Status)
	return approved && !denied
}

func getCertApprovalCondition(status *csrv1.CertificateSigningRequestStatus) (approved, denied bool) {
	for _, c := range status.Conditions {
		if c.Type == csrv1.CertificateApproved {
			approved = true
		}
		if c.Type == csrv1.CertificateDenied {
			denied = true
		}
	}
	return approved, denied
}

func newCertificateTemplate(certReq *x509.CertificateRequest) *x509.Certificate {
	serialNumber, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		klog.Errorf("failed to generate serial number: %v", err)
		return nil
	}

	template := &x509.Certificate{
		Subject: certReq.Subject,

		SignatureAlgorithm: x509.SHA512WithRSA,

		NotBefore:    time.Now().Add(-1 * time.Second),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour), // CA expire Time 10 year
		SerialNumber: serialNumber,

		DNSNames:              certReq.DNSNames,
		BasicConstraintsValid: true,
	}

	return template
}

func signCSR(template *x509.Certificate, requestKey c.PublicKey, issuer *x509.Certificate, issuerKey c.PrivateKey) (*x509.Certificate, error) {
	derBytes, err := x509.CreateCertificate(rand.Reader, template, issuer, requestKey, issuerKey)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	certs, err := x509.ParseCertificates(derBytes)
	if err != nil {
		klog.Errorf("failed to parse certificate: %v", err)
		return nil, err
	}
	if len(certs) != 1 {
		return nil, errors.New("expected a single certificate")
	}
	return certs[0], nil
}

func decodeCertificateRequest(pemBytes []byte) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		err := errors.New("certificate PEM block type must be CERTIFICATE_REQUEST")
		return nil, err
	}

	return x509.ParseCertificateRequest(block.Bytes)
}

func decodeCertificate(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		err := errors.New("certificate PEM block type must be CERTIFICATE")
		return nil, err
	}

	return x509.ParseCertificate(block.Bytes)
}

func decodePrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "PRIVATE KEY" {
		fmt.Println(block.Type)
		err := errors.New("certificate PEM block type must be PRIVATE KEY")
		return nil, err
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		err := errors.New("failed to convert private key to RSA private key")
		return nil, err
	}

	return rsaKey, nil
}

func encodeCertificates(certs ...*x509.Certificate) ([]byte, error) {
	b := bytes.Buffer{}
	for _, cert := range certs {
		if err := pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}); err != nil {
			return []byte{}, err
		}
	}
	return b.Bytes(), nil
}
