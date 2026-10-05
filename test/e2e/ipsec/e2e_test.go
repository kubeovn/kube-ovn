package ipsec

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2ekubectl "k8s.io/kubernetes/test/e2e/framework/kubectl"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
	e2e.RunE2ETests(t)
}

// Old supported branches keep IPsec in cni-server. New installations must
// execute certificate and runtime checks in the dedicated container.
func runIPsecCommand(pod corev1.Pod, command string) (string, error) {
	container := "cni-server"
	for _, candidate := range pod.Spec.Containers {
		if candidate.Name == "ipsec" {
			container = "ipsec"
			break
		}
	}
	return e2ekubectl.RunKubectl(pod.Namespace, "exec", pod.Name, "-c", container, "--", "/bin/sh", "-c", command)
}

func currentCertificateCommand(pod corev1.Pod, remove bool) string {
	for _, container := range pod.Spec.Containers {
		if container.Name == "ipsec" {
			operation := "cat"
			if remove {
				operation = "rm"
			}
			return `path=$(ovs-vsctl --if-exists get Open_vSwitch . other_config:certificate | tr -d '\"'); test -n "$path" && ` + operation + ` -- "$path"`
		}
	}
	if remove {
		return "rm /etc/ovs_ipsec_keys/ipsec-cert-*.pem"
	}
	return "cat /etc/ovs_ipsec_keys/ipsec-cert-*.pem"
}

func checkPodXfrmState(pod corev1.Pod, node1IP, node2IP string) {
	ginkgo.GinkgoHelper()

	ginkgo.By("Checking ip xfrm state for pod " + pod.Name + " on node " + pod.Spec.NodeName + " from " + node1IP + " to " + node2IP)
	framework.WaitUntil(0, time.Second*120, func(_ context.Context) (bool, error) {
		// Return only SA selector headers, so kubectl diagnostics cannot expose
		// the authentication/encryption keys in a full XFRM state dump.
		cmd := fmt.Sprintf("ip xfrm state list src %s dst %s | sed -n '/^src /p'", node1IP, node2IP)
		output, err := runIPsecCommand(pod, cmd)
		if err != nil {
			return false, err
		}
		return strings.Count(output, fmt.Sprintf("src %s dst %s", node1IP, node2IP)) >= 2, nil
	}, "xfrm state check passed")
}

func checkXfrmState(pods []corev1.Pod, node1IP, node2IP string) {
	ginkgo.GinkgoHelper()

	for _, pod := range pods {
		if ips := util.PodIPs(pod); !slices.Contains(ips, node1IP) && !slices.Contains(ips, node2IP) {
			continue
		}
		checkPodXfrmState(pod, node1IP, node2IP)
		checkPodXfrmState(pod, node2IP, node1IP)
	}
}

func checkPodCACert(pod corev1.Pod, expectedCACerts []string) (bool, error) {
	ginkgo.GinkgoHelper()

	actualCACert, err := runIPsecCommand(pod, "cat /etc/ipsec.d/cacerts/*")
	if err != nil {
		if strings.Contains(err.Error(), "No such file or directory") {
			return false, nil
		}
		return false, fmt.Errorf("reading CA certs: %w", err)
	}

	actualCerts := splitCerts(actualCACert)
	if !slices.Equal(actualCerts, expectedCACerts) {
		return false, nil
	}

	output, err := runIPsecCommand(pod, "ipsec listcacerts")
	if err != nil {
		return false, fmt.Errorf("running ipsec listcacerts: %w", err)
	}
	framework.ExpectEqual(len(expectedCACerts), strings.Count(output, "subject:"))
	return true, nil
}

func getPodCert(pod corev1.Pod) (string, error) {
	ginkgo.GinkgoHelper()

	output, err := runIPsecCommand(pod, currentCertificateCommand(pod, false))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(output) == "" {
		return "", fmt.Errorf("pod %s has an empty IPsec certificate", pod.Name)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(output))), nil
}

func getValueFromSecret(cs clientset.Interface, namespace, secretName, fieldName string) (string, error) {
	secret, err := cs.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	val, ok := secret.Data[fieldName]
	if !ok {
		return "", fmt.Errorf("%s not found in secret %s", fieldName, secretName)
	}

	return string(val), nil
}

// generateSelfSignedCA generates a new self-signed CA certificate
func generateSelfSignedCA(privateKey *rsa.PrivateKey) (string, error) {
	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "testing-common-name",
			Organization: []string{"Test CA"},
			Country:      []string{"US"},
			Province:     []string{""},
			Locality:     []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour), // Valid for 1 year
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// Create the certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode certificate to PEM format
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	return string(certPEM), nil
}

// Convert RSA private key to PEM string
func privateKeyToBytes(privateKey *rsa.PrivateKey) ([]byte, error) {
	// Convert private key to PKCS#1 ASN.1 DER format
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)

	// Create PEM block
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	}

	// Encode to PEM format
	privateKeyPEMBytes := pem.EncodeToMemory(privateKeyPEM)

	return privateKeyPEMBytes, nil
}

var _ = framework.OrderedDescribe("[group:ipsec]", func() {
	f := framework.NewDefaultFramework("ipsec")

	var cs clientset.Interface
	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
	})

	framework.ConformanceIt("Should support OVN IPSec", func() {
		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)
		framework.ExpectTrue(len(nodeList.Items) >= 2)

		ginkgo.By("Getting kube-ovn-cni pods")
		daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
		ds := daemonSetClient.Get("kube-ovn-cni")
		podList, err := daemonSetClient.GetPods(ds)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(podList.Items, len(nodeList.Items))
		nodeIPs := make([]string, 0, len(nodeList.Items))
		for _, node := range nodeList.Items {
			for _, addr := range node.Status.Addresses {
				if addr.Type == corev1.NodeInternalIP {
					nodeIPs = append(nodeIPs, addr.Address)
					break
				}
			}
		}
		framework.ExpectHaveLen(nodeIPs, len(nodeList.Items))

		ginkgo.By("Checking ip xfrm state")
		checkXfrmState(podList.Items, nodeIPs[0], nodeIPs[1])

		ginkgo.By("Restarting ds kube-ovn-cni")
		daemonSetClient.RestartSync(ds)

		ds = daemonSetClient.Get("kube-ovn-cni")
		podList, err = daemonSetClient.GetPods(ds)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(podList.Items, len(nodeList.Items))

		ginkgo.By("Checking ip xfrm state")
		checkXfrmState(podList.Items, nodeIPs[0], nodeIPs[1])

		ginkgo.By("Restarting ds ovs-ovn")
		ds = daemonSetClient.Get("ovs-ovn")
		daemonSetClient.RestartSync(ds)

		ginkgo.By("Checking ip xfrm state")
		checkXfrmState(podList.Items, nodeIPs[0], nodeIPs[1])
	})

	framework.ConformanceIt("Should keep working when rotating CA", ginkgo.Label("cert-manager"), func() {
		f.SkipVersionPriorTo(1, 15, "IPSec with backend cert-manager is not supported in versions prior to v1.15")

		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)
		framework.ExpectTrue(len(nodeList.Items) >= 2)
		nodeIPs := make([]string, 0, len(nodeList.Items))
		for _, node := range nodeList.Items {
			for _, addr := range node.Status.Addresses {
				if addr.Type == corev1.NodeInternalIP {
					nodeIPs = append(nodeIPs, addr.Address)
					break
				}
			}
		}
		framework.ExpectHaveLen(nodeIPs, len(nodeList.Items))

		ginkgo.By("Getting current CA")
		initialOVNCA, err := getValueFromSecret(cs, framework.KubeOvnNamespace, util.DefaultOVNIPSecCA, "cacert")
		framework.ExpectNoError(err)
		initialCAKey, err := getValueFromSecret(cs, "cert-manager", "kube-ovn-ca", "tls.key")
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(initialCAKey)

		ginkgo.By("Getting kube-ovn-cni pods")
		daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
		ds := daemonSetClient.Get("kube-ovn-cni")
		podList, err := daemonSetClient.GetPods(ds)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(podList.Items, len(nodeList.Items))

		ginkgo.By("Getting kube-ovn-cni pod client certificates")
		initialPodCerts := make(map[string]string)
		for _, pod := range podList.Items {
			cert, err := getPodCert(pod)
			framework.ExpectNoError(err)
			initialPodCerts[pod.Spec.NodeName] = cert
		}

		ginkgo.By("Generating secondary CA")
		secondaryKey, err := rsa.GenerateKey(rand.Reader, 2048)
		framework.ExpectNoError(err)
		secondaryCA, err := generateSelfSignedCA(secondaryKey)
		framework.ExpectNoError(err)

		ginkgo.By("Adding secondary CA to secret bundle")
		ovnIpsecSecret, err := cs.CoreV1().Secrets(framework.KubeOvnNamespace).Get(context.Background(), util.DefaultOVNIPSecCA, metav1.GetOptions{})
		framework.ExpectNoError(err)
		updatedCA := initialOVNCA + secondaryCA
		ovnIpsecSecret.Data["cacert"] = []byte(updatedCA)
		_, err = cs.CoreV1().Secrets(framework.KubeOvnNamespace).Update(context.Background(), ovnIpsecSecret, metav1.UpdateOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Verifying new trust bundle distributed")
		for _, pod := range podList.Items {
			framework.Logf("Checking CA cert for pod %s", pod.Name)
			expectedCerts := splitCerts(updatedCA)
			framework.WaitUntil(0, time.Second*30, func(_ context.Context) (bool, error) {
				return checkPodCACert(pod, expectedCerts)
			}, "Verifying new trust bundle distributed")
		}

		// changing the CA cert will cause ovs-ipsec-monitor to spin up new
		// tunnels and spin down the old ones so wait for that processing to
		// complete before checking xfrm state.
		checkXfrmState(podList.Items, nodeIPs[0], nodeIPs[1])

		ginkgo.By("Verifying client certificates not changed")
		for _, pod := range podList.Items {
			cert, err := getPodCert(pod)
			framework.ExpectNoError(err)
			framework.ExpectEqual(initialPodCerts[pod.Spec.NodeName], cert)
		}

		ginkgo.By("Setting new CA on issuer")

		issuerSecret, err := cs.CoreV1().Secrets("cert-manager").Get(context.Background(), "kube-ovn-ca", metav1.GetOptions{})
		framework.ExpectNoError(err)
		issuerSecret.Data["tls.crt"] = []byte(secondaryCA)
		keyBytes, err := privateKeyToBytes(secondaryKey)
		framework.ExpectNoError(err)
		issuerSecret.Data["tls.key"] = keyBytes
		_, err = cs.CoreV1().Secrets("cert-manager").Update(context.Background(), issuerSecret, metav1.UpdateOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Triggering client cert reissue on worker")
		for _, pod := range podList.Items {
			// clearing the certificate on disk and restarting the pod should
			// trigger a new certificate request
			_, err := runIPsecCommand(pod, currentCertificateCommand(pod, true))
			framework.ExpectNoError(err)
		}
		daemonSetClient.RestartSync(ds)

		podList, err = daemonSetClient.GetPods(ds)
		framework.ExpectNoError(err)

		ginkgo.By("Verifying new client cert issued on kube-ovn-cni pods")

		for _, pod := range podList.Items {
			framework.WaitUntil(0, time.Second*30, func(_ context.Context) (bool, error) {
				cert, err := getPodCert(pod)
				if err != nil {
					if strings.Contains(err.Error(), "No such file or directory") {
						return false, nil
					}
					framework.ExpectNoError(err)
				}
				framework.ExpectNotEqual(initialPodCerts[pod.Spec.NodeName], cert)
				return true, nil
			}, "Verifying new trust bundle distributed")
		}

		ginkgo.By("Verifying IPsec state is functional")
		checkXfrmState(podList.Items, nodeIPs[0], nodeIPs[1])

		ginkgo.By("Removing initial CA from bundle")

		ovnIpsecSecret, err = cs.CoreV1().Secrets(framework.KubeOvnNamespace).Get(context.Background(), "ovn-ipsec-ca", metav1.GetOptions{})
		framework.ExpectNoError(err)
		ovnIpsecSecret.Data["cacert"] = []byte(secondaryCA)
		_, err = cs.CoreV1().Secrets(framework.KubeOvnNamespace).Update(context.Background(), ovnIpsecSecret, metav1.UpdateOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Verifying IPsec CA updated")

		for _, pod := range podList.Items {
			framework.WaitUntil(0, time.Second*30, func(_ context.Context) (bool, error) {
				return checkPodCACert(pod, splitCerts(secondaryCA))
			}, "Verifying new trust bundle distributed")
		}

		ginkgo.By("Verifying IPsec state is functional")

		checkXfrmState(podList.Items, nodeIPs[0], nodeIPs[1])
	})
})

// split the content by the certificate delimiter
func splitCerts(content string) []string {
	parts := strings.Split(content, "-----BEGIN CERTIFICATE-----")
	certs := make([]string, 0, len(parts))
	for i := range parts {
		prefix, found := strings.CutSuffix(strings.TrimSpace(parts[i]), "-----END CERTIFICATE-----")
		if !found {
			continue
		}
		// Assertions need equality only; retain fingerprints so a mismatch
		// does not cause Ginkgo to print certificate contents.
		certs = append(certs, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(prefix)))))
	}
	slices.Sort(certs) // Sort to ensure consistent order
	return certs
}
