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
	"slices"
	"strings"
	"time"

	"github.com/kubeovn/kube-ovn/pkg/ipsec"

	csrv1 "k8s.io/api/certificates/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) validateCsrName(csr *csrv1.CertificateSigningRequest) error {
	name := csr.Name
	after, found := strings.CutPrefix(name, "ovn-ipsec-")
	if !found || len(after) == 0 {
		return fmt.Errorf("CSR name %s is invalid, must be in format ovn-ipsec-<node-name>", name)
	}

	nodeName := after
	if csr.Annotations[ipsec.NodeNameAnnotation] != "" {
		nodeName = csr.Annotations[ipsec.NodeNameAnnotation]
	}
	node, err := c.nodesLister.Get(nodeName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("node %s not found for CSR %s", nodeName, name)
		}
		return fmt.Errorf("failed to get node %s for CSR %s: %w", nodeName, name, err)
	}
	if node.Status.NodeInfo.OperatingSystem != "linux" {
		return fmt.Errorf("node %s is not linux, CSR %s is invalid", nodeName, name)
	}

	return nil
}

func (c *Controller) isOVNIPSecCSR(csr *csrv1.CertificateSigningRequest) bool {
	if csr.Spec.SignerName != util.SignerName ||
		!strings.HasPrefix(csr.Name, "ovn-ipsec-") ||
		!slices.Equal(csr.Spec.Usages, []csrv1.KeyUsage{csrv1.UsageIPsecTunnel}) {
		return false
	}
	if err := c.validateCsrName(csr); err != nil {
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

	for _, condition := range csr.Status.Conditions {
		if condition.Status == "True" && (condition.Type == csrv1.CertificateDenied || condition.Type == csrv1.CertificateFailed) {
			return nil
		}
	}
	csr = csr.DeepCopy()
	certReq, err := decodeCertificateRequest(csr.Spec.Request)
	if err != nil {
		return c.updateCSRStatusConditions(csr, "InvalidRequest", err.Error())
	}
	if err := c.validateIPsecRequester(csr, certReq); err != nil {
		if _, invalid := errors.AsType[*ipsecIdentityError](err); invalid {
			return c.updateCSRStatusConditions(csr, "InvalidIdentity", err.Error())
		}
		// API/authentication lookup failures are retryable. Never make a
		// temporary API outage terminal for a persisted pending private key.
		return err
	}
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
	// From this point we are dealing with an approved CSR
	if c.config.CertManagerIPSecCert {
		return c.signIPsecWithCertManager(csr)
	}
	// Read the private CA from the controller-only signer Secret.
	caSecret, err := c.config.KubeClient.CoreV1().Secrets(c.config.PodNamespace).Get(context.TODO(), util.DefaultOVNIPSecSigner, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// Decode the CA certificate from PEM format.
	if err := validateIPsecCA(caSecret.Data["cacert"], caSecret.Data["cakey"]); err != nil {
		return err
	}
	caCert, err := decodeCertificate(caSecret.Data["cacert"])
	if err != nil {
		return err
	}

	caKey, err := decodePrivateKey(caSecret.Data["cakey"])
	if err != nil {
		return err
	}

	// Create a new certificate using the certificate template and certificate.
	// We can then sign this using the CA.
	duration := 2 * 365 * 24 * time.Hour
	if csr.Spec.ExpirationSeconds != nil {
		duration = min(duration, time.Duration(*csr.Spec.ExpirationSeconds)*time.Second)
	}
	template, err := newCertificateTemplate(certReq)
	if err != nil {
		return err
	}
	template.NotAfter = minTime(time.Now().Add(duration), caCert.NotAfter.Add(-time.Minute))
	if duration < 10*time.Minute || !template.NotAfter.After(time.Now().Add(10*time.Minute)) {
		return c.updateCSRStatusConditions(csr, "InvalidDuration", "CA or requested lifetime is too short")
	}
	signedCert, err := signCSR(template, certReq.PublicKey, caCert, caKey)
	if err != nil {
		return err
	}

	// Encode the certificate into PEM format and add to the status of the CSR
	csr.Status.Certificate = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: signedCert.Raw})

	if err := c.updateCsrStatus(csr); err != nil {
		return err
	}

	klog.Infof("Certificate signed, issued and approved for %s by %s", csr.Name, util.SignerName)
	return nil
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
		if c.Status != "True" {
			continue
		}
		if c.Type == csrv1.CertificateApproved {
			approved = true
		}
		if c.Type == csrv1.CertificateDenied {
			denied = true
		}
	}
	return approved, denied
}

func newCertificateTemplate(certReq *x509.CertificateRequest) (*x509.Certificate, error) {
	serialNumber, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		klog.Errorf("failed to generate serial number: %v", err)
		return nil, err
	}

	template := &x509.Certificate{
		Subject: certReq.Subject,

		SignatureAlgorithm: x509.SHA512WithRSA,

		NotBefore:    time.Now().Add(-1 * time.Second),
		NotAfter:     time.Now().Add(2 * 365 * 24 * time.Hour),
		SerialNumber: serialNumber,

		DNSNames:              certReq.DNSNames,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
	}

	return template, nil
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
	block, rest := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE REQUEST" || len(bytes.TrimSpace(rest)) != 0 {
		err := errors.New("certificate PEM block type must be CERTIFICATE_REQUEST")
		return nil, err
	}

	req, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, err
	}
	if err := req.CheckSignature(); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeCertificate(pemBytes []byte) (*x509.Certificate, error) {
	certs, err := ipsec.Certificates(pemBytes)
	if err != nil {
		return nil, err
	}
	if len(certs) != 1 {
		return nil, errors.New("expected exactly one CA certificate")
	}
	return certs[0], nil
}

func decodePrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, rest := pem.Decode(pemBytes)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("invalid IPsec CA private key PEM")
	}
	if block.Type == "RSA PRIVATE KEY" {
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return key, key.Validate()
	}
	if block.Type != "PRIVATE KEY" {
		return nil, errors.New("unsupported IPsec CA private key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("IPsec CA key is not RSA")
	}
	return rsaKey, rsaKey.Validate()
}
