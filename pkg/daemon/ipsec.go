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
	"strings"
	"time"

	v1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	ipsecKeyDir      = "/etc/ovs_ipsec_keys/"
	ipsecPrivKeyPath = ipsecKeyDir + "ipsec-privkey.pem"
	ipsecReqPath     = ipsecKeyDir + "ipsec-req.pem"
	ipsecCACertPath  = ipsecKeyDir + "ipsec-cacert.pem"
	ipsecCertPath    = ipsecKeyDir + "ipsec-cert.pem"

	expireTime = 365 * 24 * time.Hour
)

func getOVSSystemID() (string, error) {
	cmd := exec.Command("ovs-vsctl", "--retry", "-t", "60", "get", "Open_vSwitch", ".", "external-ids:system-id")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	systemID := strings.ReplaceAll(string(output), "\"", "")
	systemID = systemID[:len(systemID)-1]

	if systemID == "" {
		return "", errors.New("empty system-id")
	}

	return systemID, nil
}

func checkCertExpired() (bool, error) {
	certBytes, err := os.ReadFile(ipsecCertPath)
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

	if time.Since(cert.NotBefore) > expireTime {
		return true, nil
	}

	return false, nil
}

func generateCSRCode() ([]byte, error) {
	cn, err := getOVSSystemID()
	if err != nil {
		return nil, err
	}

	klog.Infof("ovs system id: %s", cn)
	cmd := exec.Command("openssl", "genrsa", "-out", ipsecPrivKeyPath, "2048")
	err = cmd.Run()
	if err != nil {
		return nil, err
	}

	_, err = os.Stat(ipsecPrivKeyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("privkey file %s not exist", ipsecPrivKeyPath)
		}
		return nil, err
	}

	cmd = exec.Command("openssl", "req", "-new", "-text",
		"-extensions", "v3_req",
		"-addext", "subjectAltName = DNS:"+cn,
		"-subj", fmt.Sprintf("/C=CN/O=kubeovn/OU=kind/CN=%s", cn),
		"-key", ipsecPrivKeyPath,
		"-out", ipsecReqPath)
	err = cmd.Run()
	if err != nil {
		return nil, err
	}

	csrBytes, err := os.ReadFile(ipsecReqPath)
	if err != nil {
		return nil, err
	}

	return csrBytes, nil
}

func (c *Controller) createCSR(csrBytes []byte) error {
	csr := &v1.CertificateSigningRequest{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "certificates.k8s.io/v1",
			Kind:       "CertificateSigningRequest",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "ovn-ipsec-" + os.Getenv("HOSTNAME"),
		},
		Spec: v1.CertificateSigningRequestSpec{
			Request:    csrBytes,
			SignerName: util.SignerName,
			Usages: []v1.KeyUsage{
				v1.UsageIPsecTunnel,
			},
		},
	}

	if _, err := c.config.KubeClient.CertificatesV1().CertificateSigningRequests().Create(context.Background(), csr, metav1.CreateOptions{}); err != nil {
		return err
	}

	// Wait until the certificate signing request has been signed.
	var certificateStr string
	counter := 0
	for {
		csr, err := c.config.KubeClient.CertificatesV1().CertificateSigningRequests().Get(context.Background(), csr.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if len(csr.Status.Certificate) != 0 {
			certificateStr = string(csr.Status.Certificate)
			break
		}
		counter++
		time.Sleep(time.Second)
		if counter > 300 {
			return fmt.Errorf("unable to sign certificate after %d seconds", counter)
		}
	}

	klog.Infof("ipsec get certitfcate \n %s ", certificateStr)
	cmd := exec.Command("openssl", "x509", "-outform", "pem", "-text", "-out", ipsecCertPath)
	var stdinBuf bytes.Buffer
	stdinBuf.WriteString(certificateStr)
	cmd.Stdin = &stdinBuf

	_, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	klog.Infof("ipsec Cert file %s generated", ipsecCertPath)
	secret, err := c.config.KubeClient.CoreV1().Secrets("kube-system").Get(context.Background(), util.DefaultOVNIPSecCA, metav1.GetOptions{})
	if err != nil {
		return err
	}

	output := secret.Data["cacert"]
	if err := os.WriteFile(ipsecCACertPath, output, 0o600); err != nil {
		return err
	}

	klog.Infof("ipsec CA Cert file %s generated", ipsecCACertPath)
	// the csr is no longer needed
	if err := c.config.KubeClient.CertificatesV1().CertificateSigningRequests().Delete(context.Background(), csr.Name, metav1.DeleteOptions{}); err != nil {
		return err
	}

	klog.Infof("node %s' ipsec init successfully ", os.Getenv("HOSTNAME"))
	return nil
}

func configureOVSWithIPSecKeys() error {
	cmd := exec.Command("ovs-vsctl", "--retry", "-t", "60", "set", "Open_vSwitch", ".", "other_config:certificate="+ipsecCertPath, "other_config:private_key="+ipsecPrivKeyPath, "other_config:ca_cert="+ipsecCACertPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to configure OVS with IPSec keys: %q: %w", string(output), err)
	}
	return nil
}

func unconfigureOVSWithIPSecKeys() error {
	cmd := exec.Command("ovs-vsctl", "--retry", "-t", "60", "remove", "Open_vSwitch", ".", "other_config", "certificate")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to unset OVS certificate: %w", err)
	}

	cmd = exec.Command("ovs-vsctl", "--retry", "-t", "60", "remove", "Open_vSwitch", ".", "other_config", "private_key")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to unset OVS private key: %w", err)
	}

	cmd = exec.Command("ovs-vsctl", "--retry", "-t", "60", "remove", "Open_vSwitch", ".", "other_config", "ca_cert")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to unset OVS CA certificate: %w", err)
	}
	return nil
}

func linkCACertToIPSecDir() error {
	cmd := exec.Command("ln", "-s", ipsecCACertPath, "/etc/ipsec.d/cacerts/")
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func clearCACertToIPSecDir() error {
	// clear /etc/openvswitch/keys/ipsec-cacert.pem
	cmd := exec.Command("rm", "-f", "/etc/openvswitch/keys/ipsec-cacert.pem")
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func initIPSecKeysDir() error {
	if err := os.MkdirAll(ipsecKeyDir, 0o755); err != nil {
		return err
	}
	return nil
}

func clearIPSecKeysDir() error {
	if err := os.Remove(ipsecPrivKeyPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(ipsecReqPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(ipsecCACertPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(ipsecCertPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (c *Controller) ManageIPSecKeys() error {
	_, err := os.Stat(ipsecCertPath)
	if os.IsNotExist(err) {
		if err := c.CreateIPSecKeys(); err != nil {
			return err
		}
	} else {
		checkCertExpired, err := checkCertExpired()
		if err != nil {
			return err
		}
		if !checkCertExpired {
			klog.Infof("ipsec cert exist and not expired, skip")
			return nil
		}

		if err := c.RemoveIPSecKeys(); err != nil {
			klog.Errorf("remove ipsec keys error: %v", err)
		}

		if err := c.CreateIPSecKeys(); err != nil {
			return err
		}
	}

	if err := c.StartIPSecService(); err != nil {
		klog.Errorf("Start ipsec service error: %v", err)
		return err
	}

	return nil
}

func (c *Controller) CreateIPSecKeys() error {
	err := initIPSecKeysDir()
	if err != nil {
		return err
	}

	csr64, err := generateCSRCode()
	if err != nil {
		return err
	}

	err = c.createCSR(csr64)
	if err != nil {
		return err
	}

	err = configureOVSWithIPSecKeys()
	if err != nil {
		return err
	}

	// ipsec can't use the specified dir in /etc/openvswitch/keys/ipsec-cacert.pem, so link it to the default dir /etc/ipsec.d/cacerts/
	err = linkCACertToIPSecDir()
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) RemoveIPSecKeys() error {
	err := clearIPSecKeysDir()
	if err != nil {
		return err
	}

	err = unconfigureOVSWithIPSecKeys()
	if err != nil {
		return err
	}

	err = clearCACertToIPSecDir()
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) StopAndClearIPSecResouce() error {
	if err := c.StopIPSecService(); err != nil {
		klog.Errorf("stop ipsec service error: %v", err)
	}

	if err := c.RemoveIPSecKeys(); err != nil {
		klog.Errorf("remove ipsec keys error: %v", err)
	}
	return nil
}

func (c *Controller) StartIPSecService() error {
	cmd := exec.Command("service", "openvswitch-ipsec", "restart")
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command("service", "ipsec", "restart")
	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func (c *Controller) StopIPSecService() error {
	cmd := exec.Command("service", "openvswitch-ipsec", "stop")
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command("service", "ipsec", "stop")
	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}
