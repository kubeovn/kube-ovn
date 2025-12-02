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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("failed to get ovs system id: %v, output: %s", err, string(output))
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
		klog.Errorf("failed to get ovs system id: %v", err)
		return nil, err
	}

	klog.Infof("ovs system id: %s", cn)
	cmd := exec.Command("openssl", "genrsa", "-out", ipsecPrivKeyPath, "2048")
	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("failed to generate private key: %v, output: %s", err, string(output))
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
		"-subj", "/C=CN/O=kubeovn/OU=kind/CN="+cn,
		"-key", ipsecPrivKeyPath,
		"-out", ipsecReqPath) // #nosec
	output, err = cmd.CombinedOutput()
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

func (c *Controller) createCSR(csrBytes []byte) error {
	csr := &v1.CertificateSigningRequest{
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
		if k8serrors.IsAlreadyExists(err) {
			klog.Infof("CSR %s already exists: %v", csr.Name, err)
		} else {
			klog.Errorf("failed to create csr: %v", err)
			return err
		}
	}

	// Wait until the certificate signing request has been signed.
	var certificateStr string
	counter := 0
	for {
		csr, err := c.config.KubeClient.CertificatesV1().CertificateSigningRequests().Get(context.Background(), csr.Name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get csr: %v", err)
			return err
		}
		if len(csr.Status.Certificate) != 0 {
			certificateStr = string(csr.Status.Certificate)
			break
		}
		counter++
		time.Sleep(time.Second)
		if counter > 300 {
			klog.Errorf("failed to sign certificate after %d seconds", counter)
			return fmt.Errorf("unable to sign certificate after %d seconds", counter)
		}
	}

	klog.V(3).Infof("ipsec get certitfcate\n%s", certificateStr)
	cmd := exec.Command("openssl", "x509", "-outform", "pem", "-text", "-out", ipsecCertPath)
	var stdinBuf bytes.Buffer
	if _, err := stdinBuf.WriteString(certificateStr); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to write certificate: %w", err)
	}
	cmd.Stdin = &stdinBuf

	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("failed to generate cert: %v, output: %s", err, string(output))
		return err
	}

	klog.Infof("ipsec Cert file %s generated", ipsecCertPath)
	secret, err := c.config.KubeClient.CoreV1().Secrets("kube-system").Get(context.Background(), util.DefaultOVNIPSecCA, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get secret: %v", err)
		return err
	}

	output = secret.Data["cacert"]
	if err = os.WriteFile(ipsecCACertPath, output, 0o600); err != nil {
		klog.Errorf("failed to write file: %v", err)
		return err
	}

	klog.Infof("ipsec CA Cert file %s generated", ipsecCACertPath)
	// the csr is no longer needed
	if err := c.config.KubeClient.CertificatesV1().CertificateSigningRequests().Delete(context.Background(), csr.Name, metav1.DeleteOptions{}); err != nil {
		klog.Errorf("failed to delete csr: %v", err)
		return err
	}

	klog.Infof("node %s' ipsec init successfully", os.Getenv("HOSTNAME"))
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
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to unset OVS certificate: %s: %w", string(output), err)
	}

	cmd = exec.Command("ovs-vsctl", "--retry", "-t", "60", "remove", "Open_vSwitch", ".", "other_config", "private_key")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to unset OVS private key: %s: %w", string(output), err)
	}

	cmd = exec.Command("ovs-vsctl", "--retry", "-t", "60", "remove", "Open_vSwitch", ".", "other_config", "ca_cert")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to unset OVS CA certificate: %s: %w", string(output), err)
	}
	return nil
}

func linkCACertToIPSecDir() error {
	targetPath := "/etc/ipsec.d/cacerts/ipsec-cacert.pem"

	if _, err := os.Stat(targetPath); err == nil {
		klog.Infof("Target path %s already exists, skipping link operation", targetPath)
		return nil
	} else if !os.IsNotExist(err) {
		klog.Errorf("failed to check if target path exists: %v", err)
		return err
	}

	cmd := exec.Command("ln", "-s", ipsecCACertPath, targetPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("failed to link cacert: %v, output: %s", err, string(output))
		return err
	}

	klog.V(3).Infof("Successfully linked %s to %s", ipsecCACertPath, targetPath)
	return nil
}

func clearCACertToIPSecDir() error {
	if err := os.Remove("/etc/ipsec.d/cacerts/ipsec-cacert.pem"); err != nil && !os.IsNotExist(err) {
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

func clearIPSecKeysDir() error {
	if err := os.Remove(ipsecPrivKeyPath); err != nil && !os.IsNotExist(err) {
		klog.Errorf("failed to remove %s: %v", ipsecPrivKeyPath, err)
		return err
	}
	if err := os.Remove(ipsecReqPath); err != nil && !os.IsNotExist(err) {
		klog.Errorf("failed to remove %s: %v", ipsecReqPath, err)
		return err
	}
	if err := os.Remove(ipsecCACertPath); err != nil && !os.IsNotExist(err) {
		klog.Errorf("failed to remove %s: %v", ipsecCACertPath, err)
		return err
	}
	if err := os.Remove(ipsecCertPath); err != nil && !os.IsNotExist(err) {
		klog.Errorf("failed to remove %s: %v", ipsecCertPath, err)
		return err
	}
	return nil
}

func (c *Controller) ManageIPSecKeys() error {
	if _, err := os.Stat(ipsecCertPath); os.IsNotExist(err) {
		if err := c.CreateIPSecKeys(); err != nil {
			klog.Errorf("create ipsec keys error: %v", err)
			return err
		}
	} else {
		checkCertExpired, err := checkCertExpired()
		if err != nil {
			klog.Errorf("failed to check ipsec cert expired: %v", err)
			return err
		}
		if !checkCertExpired {
			klog.V(3).Infof("ipsec cert exist and not expired, skip")
		} else {
			if err := c.RemoveIPSecKeys(); err != nil {
				klog.Errorf("remove ipsec keys error: %v", err)
			}

			if err := c.CreateIPSecKeys(); err != nil {
				klog.Errorf("create ipsec keys error: %v", err)
				return err
			}
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
		klog.Errorf("init ipsec keys dir error: %v", err)
		return err
	}

	csr64, err := generateCSRCode()
	if err != nil {
		klog.Errorf("generate csr code error: %v", err)
		return err
	}

	err = c.createCSR(csr64)
	if err != nil {
		klog.Errorf("create csr error: %v", err)
		return err
	}

	err = configureOVSWithIPSecKeys()
	if err != nil {
		klog.Errorf("configure ovs with ipsec keys error: %v", err)
		return err
	}

	return nil
}

func (c *Controller) RemoveIPSecKeys() error {
	err := clearIPSecKeysDir()
	if err != nil {
		klog.Errorf("clear ipsec keys dir error: %v", err)
		return err
	}

	err = unconfigureOVSWithIPSecKeys()
	if err != nil {
		klog.Errorf("unconfigure ovs with ipsec keys error: %v", err)
		return err
	}

	err = clearCACertToIPSecDir()
	if err != nil {
		klog.Errorf("clear cacert to ipsec dir error: %v", err)
		return err
	}

	return nil
}

func (c *Controller) FlushIPxfrmRule() error {
	output, err := exec.Command("ip", "xfrm", "policy", "flush").CombinedOutput()
	if err != nil {
		klog.Errorf("flush ip xfrm policy rule error: %v, output: %s", err, string(output))
		return err
	}

	output, err = exec.Command("ip", "xfrm", "state", "flush").CombinedOutput()
	if err != nil {
		klog.Errorf("flush ip xfrm state rule error: %v, output: %s", err, string(output))
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

	if err := c.FlushIPxfrmRule(); err != nil {
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
	// ipsec can't use the specified dir in /etc/ovs_ipsec_keys/ipsec-cacert.pem (perhap strongwan's bug), so link it to the default dir /etc/ipsec.d/cacerts/
	err := linkCACertToIPSecDir()
	if err != nil {
		klog.Errorf("link cacert to ipsec dir error: %v", err)
		return err
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
