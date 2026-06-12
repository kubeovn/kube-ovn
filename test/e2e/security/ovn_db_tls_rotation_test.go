package security

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/controller/ovndbtls"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

const (
	forcedOVNDBTLSNotBefore = "2024-01-01T00:00:00Z"
	forcedOVNDBTLSNotAfter  = "2025-01-01T00:00:00Z"
)

var _ = framework.SerialDescribe("[group:ovn-db-tls]", func() {
	f := framework.NewDefaultFramework("ovn-db-tls-rotation")
	f.SkipNamespaceCreation = true

	framework.DisruptiveIt("should rotate OVN DB TLS certificates without breaking client connectivity", func() {
		f.SkipVersionPriorTo(1, 17, "OVN DB TLS certificate management was introduced in v1.17")

		cs := f.ClientSet
		ctx := context.Background()

		ginkgo.By("Getting OVN DB TLS Secrets")
		serverSecret := getOVNDBTLSSecretOrSkip(ctx, cs, ovndbtls.ServerSecretName)
		clientSecret := getOVNDBTLSSecretOrSkip(ctx, cs, ovndbtls.ClientSecretName)
		oldServerSerial := certSerialFromSecret(serverSecret, ovndbtls.KeyServerCert)
		oldClientSerial := certSerialFromSecret(clientSecret, ovndbtls.KeyClientCert)

		ginkgo.By("Forcing OVN DB TLS leaf certificate renewal")
		forceOVNDBTLSRenewal(ctx, cs, ovndbtls.ServerSecretName)
		forceOVNDBTLSRenewal(ctx, cs, ovndbtls.ClientSecretName)

		ginkgo.By("Restarting kube-ovn-controller to trigger certificate reconciliation")
		deployClient := f.DeploymentClientNS(framework.KubeOvnNamespace)
		deployClient.RestartSync(deployClient.Get("kube-ovn-controller"))

		ginkgo.By("Waiting for server and client Secrets to be reissued")
		newServerSerial := waitOVNDBTLSSecretSerialChanged(ctx, cs, ovndbtls.ServerSecretName, ovndbtls.KeyServerCert, oldServerSerial)
		newClientSerial := waitOVNDBTLSSecretSerialChanged(ctx, cs, ovndbtls.ClientSecretName, ovndbtls.KeyClientCert, oldClientSerial)
		framework.Logf("rotated OVN DB TLS cert serials: server %s -> %s, client %s -> %s", oldServerSerial, newServerSerial, oldClientSerial, newClientSerial)

		ginkgo.By("Waiting for ovn-central to consume rotated certificates")
		waitOVNCentralTLSCerts(deployClient, newServerSerial, newClientSerial)

		ginkgo.By("Checking OVN DB SSL connectivity and listener remotes after ovn-central reload")
		waitOVNDBTLSConnectivity(deployClient)

		ginkgo.By("Waiting for ovs-ovn pods to consume rotated client certificate")
		waitOVSOVNTLSClientCert(f.DaemonSetClientNS(framework.KubeOvnNamespace), newClientSerial)
	})
})

func getOVNDBTLSSecretOrSkip(ctx context.Context, cs clientset.Interface, name string) *corev1.Secret {
	ginkgo.GinkgoHelper()

	secret, err := cs.CoreV1().Secrets(framework.KubeOvnNamespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		ginkgo.Skip("OVN DB TLS certificate management is not enabled")
	}
	framework.ExpectNoError(err)
	return secret
}

func forceOVNDBTLSRenewal(ctx context.Context, cs clientset.Interface, name string) {
	ginkgo.GinkgoHelper()

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		secret, err := cs.CoreV1().Secrets(framework.KubeOvnNamespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if secret.Annotations == nil {
			secret.Annotations = map[string]string{}
		}
		secret.Annotations[ovndbtls.AnnotationNotBefore] = forcedOVNDBTLSNotBefore
		secret.Annotations[ovndbtls.AnnotationNotAfter] = forcedOVNDBTLSNotAfter
		_, err = cs.CoreV1().Secrets(framework.KubeOvnNamespace).Update(ctx, secret, metav1.UpdateOptions{})
		return err
	})
	framework.ExpectNoError(err)
}

func waitOVNDBTLSSecretSerialChanged(ctx context.Context, cs clientset.Interface, name, certKey, oldSerial string) string {
	ginkgo.GinkgoHelper()

	var serial string
	framework.WaitUntil(5*time.Second, 3*time.Minute, func(_ context.Context) (bool, error) {
		secret, err := cs.CoreV1().Secrets(framework.KubeOvnNamespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		serial = certSerialFromSecret(secret, certKey)
		return serial != oldSerial, nil
	}, "waiting for "+name+" certificate serial to change")
	return serial
}

func waitOVNCentralTLSCerts(deployClient *framework.DeploymentClient, serverSerial, clientSerial string) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(5*time.Second, 5*time.Minute, func(_ context.Context) (bool, error) {
		deploy := deployClient.Get("ovn-central")
		pods, err := deployClient.GetPods(deploy)
		if err != nil {
			return false, err
		}
		if len(pods.Items) == 0 {
			return false, nil
		}
		pod := pods.Items[0]
		mountedServerSerial, err := podCertSerial(pod.Namespace, pod.Name, "/var/run/tls/server.crt")
		if err != nil || mountedServerSerial != serverSerial {
			return false, nil
		}
		mountedClientSerial, err := podCertSerial(pod.Namespace, pod.Name, "/var/run/tls/client.crt")
		if err != nil || mountedClientSerial != clientSerial {
			return false, nil
		}
		return true, nil
	}, "waiting for ovn-central to mount rotated OVN DB TLS certificates")
}

func waitOVNDBTLSConnectivity(deployClient *framework.DeploymentClient) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(5*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		deploy := deployClient.Get("ovn-central")
		pods, err := deployClient.GetPods(deploy)
		if err != nil {
			return false, err
		}
		if len(pods.Items) == 0 {
			return false, nil
		}
		pod := pods.Items[0]
		cmd := fmt.Sprintf(`'ovn-appctl -t /var/run/ovn/ovnnb_db.ctl ovsdb-server/list-remotes | grep -q "pssl:%d:" && `+
			`ovn-appctl -t /var/run/ovn/ovnsb_db.ctl ovsdb-server/list-remotes | grep -q "pssl:%d:" && `+
			`ovsdb-client -p /var/run/tls/client.key -c /var/run/tls/client.crt -C /var/run/tls/ca.crt --timeout 3 list-dbs ssl:[$POD_IP]:%d >/dev/null && `+
			`ovsdb-client -p /var/run/tls/client.key -c /var/run/tls/client.crt -C /var/run/tls/ca.crt --timeout 3 list-dbs ssl:[$POD_IP]:%d >/dev/null'`,
			util.NBDatabasePort, util.SBDatabasePort, util.NBDatabasePort, util.SBDatabasePort)
		_, _, err = framework.KubectlExec(pod.Namespace, pod.Name, "sh", "-c", cmd)
		return err == nil, nil
	}, "waiting for OVN DB TLS connectivity after certificate rotation")
}

func waitOVSOVNTLSClientCert(dsClient *framework.DaemonSetClient, clientSerial string) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(5*time.Second, 5*time.Minute, func(_ context.Context) (bool, error) {
		ds := dsClient.Get("ovs-ovn")
		pods, err := dsClient.GetPods(ds)
		if err != nil {
			return false, err
		}
		if len(pods.Items) == 0 {
			return false, nil
		}
		for _, pod := range pods.Items {
			serial, err := podCertSerial(pod.Namespace, pod.Name, "/var/run/tls/client.crt")
			if err != nil || serial != clientSerial {
				return false, nil
			}
		}
		return true, nil
	}, "waiting for ovs-ovn pods to mount rotated OVN DB TLS client certificate")
}

func podCertSerial(namespace, pod, path string) (string, error) {
	ginkgo.GinkgoHelper()

	stdout, _, err := framework.KubectlExec(namespace, pod, "cat", path)
	if err != nil {
		return "", err
	}
	return certSerialFromPEM(stdout), nil
}

func certSerialFromSecret(secret *corev1.Secret, key string) string {
	ginkgo.GinkgoHelper()

	certPEM, ok := secret.Data[key]
	framework.ExpectTrue(ok, "Secret %s/%s does not contain %s", secret.Namespace, secret.Name, key)
	return certSerialFromPEM(certPEM)
}

func certSerialFromPEM(certPEM []byte) string {
	ginkgo.GinkgoHelper()

	block, _ := pem.Decode(certPEM)
	framework.ExpectNotNil(block, "failed to decode certificate PEM")
	cert, err := x509.ParseCertificate(block.Bytes)
	framework.ExpectNoError(err)
	return cert.SerialNumber.String()
}
