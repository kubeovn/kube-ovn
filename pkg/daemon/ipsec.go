package daemon

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cert-manager/cert-manager/pkg/apis/certmanager"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/vishvananda/netlink"
	v1 "k8s.io/api/certificates/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	ipsecCADir   = "/etc/ipsec.d/cacerts"
	ipsecKeyDir  = "/etc/ovs_ipsec_keys"
	ipsecReqPath = ipsecKeyDir + "ipsec-req.pem"

	ipsecPrivKeyPathSpec = ipsecKeyDir + "/ipsec-privkey-%d.pem"
	ipsecCertPathSpec    = ipsecKeyDir + "/ipsec-cert-%d.pem"
	ipsecCACertPathSpec  = ipsecKeyDir + "/ipsec-cacert-%s.pem"
)

type pkiFiles struct {
	privateKeyPath  string
	certificatePath string
	caCertPath      string
}

func getOVSSystemID() (string, error) {
	id, err := ovs.Get("Open_vSwitch", ".", "external_ids", "system-id", true)
	if err != nil {
		klog.Errorf("failed to get ovs system-id: %v", err)
		return "", err
	}
	id = strings.Trim(id, `"`)
	if id == "" {
		return "", errors.New("ovs system-id is not set or is empty")
	}

	return id, nil
}

func (c *Controller) needNewCert(p *pkiFiles) (bool, error) {
	if p.certificatePath == "" || p.privateKeyPath == "" {
		klog.Infof("ipsec cert and key not configured")
		return true, nil
	}

	if _, err := os.Stat(p.certificatePath); os.IsNotExist(err) {
		klog.Infof("ipsec cert %s does not exist", p.certificatePath)
		return true, nil
	}
	if _, err := os.Stat(p.privateKeyPath); os.IsNotExist(err) {
		klog.Infof("ipsec key %s does not exist", p.privateKeyPath)
		return true, nil
	}

	certBytes, err := os.ReadFile(p.certificatePath)
	if err != nil {
		return false, fmt.Errorf("failed to read certificate: %w", err)
	}

	block, _ := pem.Decode(certBytes)
	if block == nil {
		return false, errors.New("failed to decode PEM block containing certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// get a new certificate if we're over half way through this certificates validity
	now := time.Now()
	if now.Before(cert.NotBefore) ||
		time.Since(cert.NotBefore) > (cert.NotAfter.Sub(cert.NotBefore))/2 {
		klog.Infof("ipsec cert near expiry")
		return true, nil
	}

	// now check our certificate is signed by our CA
	caCertBytes, err := os.ReadFile(p.caCertPath)
	if err != nil {
		return false, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCertBytes) {
		return false, errors.New("failed to decode CA certificate PEM blocks")
	}

	chain, err := cert.Verify(
		x509.VerifyOptions{
			Roots:     caCertPool,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		})
	if err != nil {
		klog.Infof("certificate failed to validate: %v", err)
		return true, nil
	}

	if len(chain) == 0 {
		klog.Infof("certificate not signed by trust bundle")
		return true, nil
	}

	return false, nil
}

func (c *Controller) untilCertRefresh(certPath string) (time.Duration, error) {
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read certificate: %w", err)
	}

	block, _ := pem.Decode(certBytes)
	if block == nil {
		return 0, errors.New("failed to decode PEM block containing certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return 0, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// get a new certificate if we're over half way through this certificates validity
	refreshTime := cert.NotBefore.Add((cert.NotAfter.Sub(cert.NotBefore)) / 2)
	return time.Until(refreshTime), nil
}

func (c *Controller) getCACert(key string) (string, error) {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.Errorf("failed to split meta namespace key %s: %v", key, err)
		return "", err
	}
	caSecret, err := c.caSecretLister.Secrets(ns).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Infof("ipsec CA secret %s not found, waiting for it to be created", key)
			return "", nil
		}
		klog.Errorf("failed to get secret: %v", err)
		return "", err
	}

	ca := caSecret.Data["cacert"]
	caCertPath := generateCACertFileName(caSecret.ResourceVersion)

	if err = os.WriteFile(caCertPath, ca, 0o600); err != nil {
		klog.Errorf("failed to write file: %v", err)
		return "", err
	}

	if err = linkCACertToIPSecDir(ca); err != nil {
		klog.Errorf("link cacert to ipsec dir error: %v", err)
		return "", err
	}

	cmd := exec.Command("ipsec", "rereadcacerts")
	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("failed to reload ipsec ca cert: %v, output: %s", err, output)
		return "", err
	}

	klog.Infof("ipsec CA Cert file %s written", caCertPath)
	return caCertPath, nil
}

func generateNewPrivateKey(path string) error {
	cmd := exec.Command("openssl", "genrsa", "-out", path, "2048")
	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("failed to generate private key: %v, output: %s", err, string(output))
		return err
	}

	_, err = os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("privkey file %s not exist", path)
		}
		return err
	}
	return nil
}

func generateCSRCode(newPrivKeyPath string) ([]byte, error) {
	cn, err := getOVSSystemID()
	if err != nil {
		klog.Errorf("failed to get ovs system-id: %v", err)
		return nil, err
	}

	klog.Infof("ovs system id: %s", cn)

	cmd := exec.Command("openssl", "req", "-new", "-text",
		"-extensions", "v3_req",
		"-addext", "subjectAltName = DNS:"+cn,
		"-subj", "/C=CN/O=kubeovn/OU=kube-ovn/CN="+cn,
		"-key", newPrivKeyPath,
		"-out", ipsecReqPath) // #nosec
	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("failed to generate csr: %v, output: %s", err, string(output))
		return nil, err
	}

	csrBytes, err := os.ReadFile(ipsecReqPath)
	if err != nil {
		klog.Errorf("failed to read csr: %v", err)
		return nil, err
	}

	return csrBytes, nil
}

func (c *Controller) getCertManagerSignedCert(ctx context.Context, csrBytes []byte) ([]byte, error) {
	namespace := os.Getenv(util.EnvPodNamespace)
	newCR := &certmanagerv1.CertificateRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ovn-ipsec-" + os.Getenv(util.EnvNodeName),
			Namespace: namespace,
		},
		Spec: certmanagerv1.CertificateRequestSpec{
			Request: csrBytes,
			IssuerRef: cmmeta.ObjectReference{
				Name:  c.config.CertManagerIssuerName,
				Kind:  util.ObjectKind[*certmanagerv1.ClusterIssuer](),
				Group: certmanager.GroupName,
			},
			Duration: &metav1.Duration{Duration: time.Second * time.Duration(c.config.IPSecCertDuration)},
			Usages:   []certmanagerv1.KeyUsage{certmanagerv1.UsageIPsecTunnel},
		},
	}

	_, err := c.config.CertManagerClient.CertmanagerV1().CertificateRequests(namespace).Create(ctx, newCR, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			klog.Infof("CR %s already exists: %v", newCR.Name, err)
			err := c.config.CertManagerClient.CertmanagerV1().CertificateRequests(namespace).Delete(context.Background(), newCR.Name, metav1.DeleteOptions{})
			if err != nil {
				klog.Errorf("failed to delete cr: %s; %v", newCR.Name, err)
			}
		}

		return nil, fmt.Errorf("creating certificate request; %w", err)
	}

	defer func() {
		// clean up the request once it's no longer needed
		err := c.config.CertManagerClient.CertmanagerV1().CertificateRequests(namespace).Delete(context.Background(), newCR.Name, metav1.DeleteOptions{})
		if err != nil {
			klog.Errorf("failed to delete cr: %s; %v", newCR.Name, err)
		}
	}()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("failed to sign certificate request; %w", ctx.Err())
		case <-ticker.C:
			cr, err := c.config.CertManagerClient.CertmanagerV1().CertificateRequests(namespace).Get(ctx, newCR.Name, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("getting certificate request; %w", err)
			}

			if len(cr.Status.Certificate) == 0 {
				continue
			}

			return cr.Status.Certificate, nil
		}
	}
}

func (c *Controller) getSignedCert(ctx context.Context, csrBytes []byte) ([]byte, error) {
	csr := &v1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ovn-ipsec-" + os.Getenv(util.EnvNodeName),
		},
		Spec: v1.CertificateSigningRequestSpec{
			Request:    csrBytes,
			SignerName: util.SignerName,
			Usages: []v1.KeyUsage{
				v1.UsageIPsecTunnel,
			},
		},
	}

	if _, err := c.config.KubeClient.CertificatesV1().CertificateSigningRequests().Create(ctx, csr, metav1.CreateOptions{}); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			klog.Infof("CSR %s already exists: %v", csr.Name, err)
		} else {
			klog.Errorf("failed to create csr: %v", err)
			return nil, err
		}
	}

	defer func() {
		// clean up the request once it's no longer needed
		if err := c.config.KubeClient.CertificatesV1().CertificateSigningRequests().Delete(context.Background(), csr.Name, metav1.DeleteOptions{}); err != nil {
			klog.Errorf("failed to delete csr: %v", err)
		}
	}()

	// Wait until the certificate signing request has been signed.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("failed to sign certificate request; %w", ctx.Err())
		case <-ticker.C:
			csr, err := c.config.KubeClient.CertificatesV1().CertificateSigningRequests().Get(ctx, csr.Name, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("failed to get csr: %v", err)
				return nil, err
			}
			if len(csr.Status.Certificate) == 0 {
				continue
			}

			return csr.Status.Certificate, nil
		}
	}
}

func (c *Controller) storeCertificate(cert []byte, certPath string) error {
	cmd := exec.Command("openssl", "x509", "-outform", "pem", "-text", "-out", certPath)
	cmd.Stdin = bytes.NewReader(cert)

	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("failed to generate cert: %v, output: %s", err, string(output))
		return err
	}

	klog.Infof("wrote certificate file: %s", certPath)
	return nil
}

func configureOVSWithIPSecKeys(p *pkiFiles) error {
	if err := ovs.Set("Open_vSwitch", ".",
		fmt.Sprintf("other_config:certificate=%s", p.certificatePath),
		fmt.Sprintf("other_config:private_key=%s", p.privateKeyPath),
		fmt.Sprintf("other_config:ca_cert=%s", p.caCertPath),
	); err != nil {
		return fmt.Errorf("failed to configure OVS with IPSec keys: %w", err)
	}
	return nil
}

func getOvsIPSecKeys() (*pkiFiles, error) {
	certificate, err := ovs.Get("Open_vSwitch", ".", "other_config", "certificate", true)
	if err != nil {
		return nil, fmt.Errorf("reading OVS certificate config; %w", err)
	}
	privateKey, err := ovs.Get("Open_vSwitch", ".", "other_config", "private_key", true)
	if err != nil {
		return nil, fmt.Errorf("reading OVS private key config; %w", err)
	}
	caCert, err := ovs.Get("Open_vSwitch", ".", "other_config", "ca_cert", true)
	if err != nil {
		return nil, fmt.Errorf("reading OVS CA certificate config; %w", err)
	}

	return &pkiFiles{
		certificatePath: strings.Trim(certificate, `"`),
		privateKeyPath:  strings.Trim(privateKey, `"`),
		caCertPath:      strings.Trim(caCert, `"`),
	}, nil
}

func clearOVSIPSecConfig() error {
	if err := ovs.Remove("Open_vSwitch", ".", "other_config", "ca_cert", "private_key", "certificate"); err != nil {
		return fmt.Errorf("failed to remove OVS ipsec configuration: %w", err)
	}
	return nil
}

func linkCACertToIPSecDir(ca []byte) error {
	// strongswan is unable to read chains or trust bundles and will only read the first certificate in the file.
	// Split out each CA cert into it's own file in the ipsec cacerts directory.
	if err := os.RemoveAll(ipsecCADir); err != nil {
		return fmt.Errorf("clearing ipsec CA directory: %w", err)
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(ipsecCADir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Split and write individual certificates
	certificates, err := splitPEMCertificates(ca)
	if err != nil {
		return fmt.Errorf("failed to split certificates: %w", err)
	}

	for i, cert := range certificates {
		filename := fmt.Sprintf("ca-%03d.pem", i+1)
		filepath := filepath.Join(ipsecCADir, filename)

		if err := os.WriteFile(filepath, cert, 0o600); err != nil {
			return fmt.Errorf("failed to write certificate %s: %w", filename, err)
		}

		klog.Infof("Wrote CA certificate %s", filename)
	}

	klog.V(3).Infof("Total certificates processed: %d", len(certificates))
	return nil
}

func splitPEMCertificates(data []byte) ([][]byte, error) {
	var certificates [][]byte
	var currentCert strings.Builder

	// Split by lines and process each PEM block
	rest := data
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			break
		}

		// Only process certificate blocks
		if block.Type != "CERTIFICATE" {
			rest = remaining
			continue
		}

		// Validate that it's actually a certificate
		_, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			klog.Errorf("Warning: Skipping invalid certificate: %v\n", err)
			rest = remaining
			continue
		}

		// Encode the certificate block back to PEM format
		currentCert.Reset()
		if err := pem.Encode(&currentCert, block); err != nil {
			return nil, fmt.Errorf("failed to encode certificate: %w", err)
		}

		certificates = append(certificates, []byte(currentCert.String()))
		rest = remaining
	}

	if len(certificates) == 0 {
		return nil, errors.New("no valid certificates found in bundle")
	}

	return certificates, nil
}

func clearCACertToIPSecDir() error {
	if err := os.Remove(filepath.Join(ipsecCADir, "ipsec-cacert.pem")); err != nil && !os.IsNotExist(err) {
		klog.Error(err)
		return fmt.Errorf("failed to remove ipsec-cacert.pem: %w", err)
	}
	return nil
}

func initIPSecKeysDir() error {
	if err := os.MkdirAll(ipsecKeyDir, 0o755); err != nil {
		klog.Errorf("failed to create %s: %v", ipsecKeyDir, err)
		return err
	}
	return nil
}

func clearIPSecKeysDir(toKeep pkiFiles) error {
	filesToKeep := map[string]bool{
		toKeep.caCertPath:      true,
		toKeep.certificatePath: true,
		toKeep.privateKeyPath:  true,
	}

	// Get all files in the directory
	files, err := os.ReadDir(ipsecKeyDir)
	if err != nil {
		klog.Errorf("reading ipsec keys directory: %v\n", err)
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue // Skip directories
		}

		filePath := filepath.Join(ipsecKeyDir, file.Name())
		if !filesToKeep[filePath] {
			// Delete the file
			if err = os.Remove(filePath); err != nil {
				klog.Errorf("deleting %s: %v\n", filePath, err)
			}
		}
	}

	return nil
}

func (c *Controller) SyncIPSecKeys(key string) error {
	if !c.config.EnableOVNIPSec {
		return nil
	}

	klog.Infof("syncing IPSec CA from secret %s", key)

	if err := initIPSecKeysDir(); err != nil {
		klog.Errorf("init ipsec keys dir: %v", err)
		return err
	}

	pkiFiles, err := getOvsIPSecKeys()
	if err != nil {
		klog.Errorf("reading OVS ipsec config: %v", err)
		return err
	}

	ca, err := c.getCACert(key)
	if err != nil {
		klog.Errorf("failed to get new CA cert: %v", err)
		return err
	}
	if ca == "" {
		return nil
	}
	pkiFiles.caCertPath = ca

	needNewCert, err := c.needNewCert(pkiFiles)
	if err != nil {
		klog.Errorf("checking certificate valid: %v", err)
		return err
	}

	if needNewCert {
		now := time.Now()
		pkiFiles.privateKeyPath = generatePrivKeyFileName(now)
		pkiFiles.certificatePath = generateCertFileName(now)
		if err := c.CreateIPSecKeys(pkiFiles); err != nil {
			klog.Errorf("create ipsec keys error: %v", err)
			return err
		}
	}

	if needNewCert {
		err := configureOVSWithIPSecKeys(pkiFiles)
		if err != nil {
			klog.Errorf("configure ovs with ipsec keys error: %v", err)
			return err
		}

		if err := clearIPSecKeysDir(*pkiFiles); err != nil {
			// don't return here; we've already programmed the new keys
			klog.Errorf("cleaning old ipsec files: %v", err)
		}
	}

	untilRefresh, err := c.untilCertRefresh(pkiFiles.certificatePath)
	if err != nil {
		klog.Errorf("calculating cert refresh time: %v", err)
		return err
	}

	c.ipsecQueue.AddAfter(key, untilRefresh)

	return nil
}

func (c *Controller) CreateIPSecKeys(p *pkiFiles) error {
	err := generateNewPrivateKey(p.privateKeyPath)
	if err != nil {
		klog.Errorf("generate private key error: %v", err)
		return err
	}

	csr64, err := generateCSRCode(p.privateKeyPath)
	if err != nil {
		klog.Errorf("generate csr code error: %v", err)
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	var cert []byte
	if c.config.CertManagerIPSecCert {
		if cert, err = c.getCertManagerSignedCert(ctx, csr64); err != nil {
			err = fmt.Errorf("create cr error: %w", err)
			klog.Error(err)
			return err
		}
	} else if cert, err = c.getSignedCert(ctx, csr64); err != nil {
		klog.Errorf("create csr error: %v", err)
		return err
	}

	if err = c.storeCertificate(cert, p.certificatePath); err != nil {
		err := fmt.Errorf("storing certificate; %w", err)
		klog.Error(err)
		return err
	}

	return nil
}

func (c *Controller) RemoveIPSecKeys() error {
	err := clearIPSecKeysDir(pkiFiles{})
	if err != nil {
		klog.Errorf("clear ipsec keys dir error: %v", err)
		return err
	}

	if err = clearOVSIPSecConfig(); err != nil {
		klog.Errorf("clear OVS IPSec configuration error: %v", err)
		return err
	}

	if err = clearCACertToIPSecDir(); err != nil {
		klog.Errorf("clear cacert to ipsec dir error: %v", err)
		return err
	}

	return nil
}

func (c *Controller) FlushIPXfrm() error {
	if err := netlink.XfrmPolicyFlush(); err != nil {
		klog.Errorf("failed to flush xfrm policy: %v", err)
		return err
	}
	if err := netlink.XfrmStateFlush(netlink.XFRM_PROTO_ESP); err != nil {
		klog.Errorf("failed to flush xfrm state: %v", err)
		return err
	}

	return nil
}

func (c *Controller) StopAndClearIPSecResource() error {
	if err := c.StopIPSecService(); err != nil {
		klog.Errorf("stop ipsec service error: %v", err)
	}

	if err := c.RemoveIPSecKeys(); err != nil {
		klog.Errorf("remove ipsec keys error: %v", err)
	}

	if err := c.FlushIPXfrm(); err != nil {
		klog.Errorf("flush ip xfrm rules error: %v", err)
	}

	return nil
}

func isServiceActive(serviceName string) (bool, error) {
	cmd := exec.Command("service", serviceName, "status")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if exitError.ExitCode() != 0 {
				return false, nil
			}
		}
		klog.Errorf("failed to check service status: %v, output: %s", err, string(output))
		return false, err
	}
	return true, nil
}

func restartService(serviceName string) error {
	active, err := isServiceActive(serviceName)
	if err != nil {
		klog.Errorf("check %s service error: %v", serviceName, err)
		return err
	}

	if !active {
		klog.Infof("restarting service %s", serviceName)
		cmd := exec.Command("service", serviceName, "restart")
		output, err := cmd.CombinedOutput()
		if err != nil {
			klog.Errorf("restart %s service error: %v, output: %s", serviceName, err, string(output))
			return err
		}
		klog.Infof("%s service restarted successfully", serviceName)
	} else {
		klog.V(3).Infof("%s service is already active", serviceName)
	}

	return nil
}

func (c *Controller) StartIPSecService() error {
	if !c.config.EnableOVNIPSec {
		return nil
	}

	if err := restartService("openvswitch-ipsec"); err != nil {
		return err
	}
	if err := restartService("ipsec"); err != nil {
		return err
	}

	return nil
}

func (c *Controller) StopIPSecService() error {
	cmd := exec.Command("service", "openvswitch-ipsec", "stop")
	if output, err := cmd.CombinedOutput(); err != nil {
		klog.Errorf("stop openvswitch-ipsec service error: %v, output: %s", err, string(output))
		return err
	}

	cmd = exec.Command("service", "ipsec", "stop")
	if output, err := cmd.CombinedOutput(); err != nil {
		klog.Errorf("stop ipsec service error: %v, output: %s", err, string(output))
		return err
	}

	return nil
}

func generateCACertFileName(resourceVersion string) string {
	return fmt.Sprintf(ipsecCACertPathSpec, resourceVersion)
}

func generateCertFileName(startTime time.Time) string {
	return fmt.Sprintf(ipsecCertPathSpec, startTime.UnixMilli())
}

func generatePrivKeyFileName(startTime time.Time) string {
	return fmt.Sprintf(ipsecPrivKeyPathSpec, startTime.UnixMilli())
}
