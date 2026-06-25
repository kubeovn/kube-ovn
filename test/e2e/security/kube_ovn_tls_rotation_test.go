package security

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

const (
	kubeOVNTLSSecretName        = "kube-ovn-tls"
	kubeOVNSSLEnabled           = "ENABLE_SSL"
	kubeOVNControllerDeployment = "kube-ovn-controller"
	kubeOVNControllerContainer  = "kube-ovn-controller"
	kubeOVNMonitorDeployment    = "kube-ovn-monitor"
	kubeOVNMonitorContainer     = "kube-ovn-monitor"
	kubeOVNPingerDaemonSet      = "kube-ovn-pinger"
	kubeOVNPingerContainer      = "pinger"
	ovnCentralDeployment        = "ovn-central"
	ovnCentralContainer         = "ovn-central"
	ovsOVNDaemonSet             = "ovs-ovn"
	ovnCentralTLSHashFile       = "/tmp/kube-ovn-central-tls.hash"
	ovsOVNTLSHashFile           = "/tmp/kube-ovn-tls.hash"

	kubeOVNTLSCertHashAnnotation = "kube-ovn.io/kube-ovn-tls-cert-hash"
)

var kubeOVNTLSDataKeys = []string{"cacert", "cert", "key"}

var _ = framework.Describe("[group:security] kube-ovn TLS rotation", func() {
	f := framework.NewDefaultFramework("security-kube-ovn-tls-rotation")
	f.SkipNamespaceCreation = true

	framework.ConformanceIt("should reload components without restarting pods when kube-ovn-tls secret is updated", func() {
		f.SkipVersionPriorTo(1, 17, "kube-ovn TLS rotation was introduced in v1.17")

		cs := f.ClientSet
		deployClient := f.DeploymentClientNS(framework.KubeOvnNamespace)
		deploy := deployClient.Get(kubeOVNControllerDeployment)
		if !deploymentEnvEnabled(deploy, kubeOVNSSLEnabled) {
			ginkgo.Skip("kube-ovn TLS is disabled")
		}

		ginkgo.By("Saving current kube-ovn-tls secret")
		originalSecret, err := cs.CoreV1().Secrets(framework.KubeOvnNamespace).Get(context.Background(), kubeOVNTLSSecretName, metav1.GetOptions{})
		framework.ExpectNoError(err)
		originalData := copySecretData(originalSecret.Data)
		originalHash := kubeOVNTLSDataHash(originalData)
		originalSerial := kubeOVNTLSCertSerial(originalData)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Restoring kube-ovn-tls secret")
			restoreSecretData(cs, originalData)
		})

		ginkgo.By("Recording kube-ovn-controller restart counts")
		restartCounts := deploymentContainerRestartCounts(cs, deploy, kubeOVNControllerContainer)

		ginkgo.By("Recording kube-ovn-monitor restart counts")
		monitorDeploy := deployClient.Get(kubeOVNMonitorDeployment)
		monitorRestartCounts := deploymentContainerRestartCounts(cs, monitorDeploy, kubeOVNMonitorContainer)

		ginkgo.By("Recording kube-ovn-pinger restart counts")
		daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
		pingerDS := daemonSetClient.Get(kubeOVNPingerDaemonSet)
		pingerRestartCounts := daemonSetContainerRestartCounts(daemonSetClient, pingerDS, kubeOVNPingerContainer)

		ginkgo.By("Recording ovn-central restart counts")
		centralDeploy := deployClient.Get(ovnCentralDeployment)
		centralRestartCounts := deploymentContainerRestartCounts(cs, centralDeploy, ovnCentralContainer)
		seedOVNCentralTLSHash(cs, centralDeploy)
		centralTLSHashes := deploymentTLSHashes(cs, centralDeploy)

		ginkgo.By("Recording ovn-controller pid")
		ovsPods := ovsOVNPods(f)
		seedOVSTLSHashes(ovsPods)
		initialOVNControllerPIDs := ovnControllerPIDs(ovsPods)

		ginkgo.By("Installing updated kube-ovn-tls secret")
		updatedData, err := generateUpdatedKubeOVNTLSSecretData()
		framework.ExpectNoError(err)
		updatedHash := kubeOVNTLSDataHash(updatedData)
		framework.ExpectNotEqual(updatedHash, originalHash)
		framework.ExpectNotEqual(kubeOVNTLSCertSerial(updatedData), originalSerial)
		updateKubeOVNTLSSecretData(cs, updatedData)

		ginkgo.By("Waiting for kube-ovn-tls files to be projected")
		expectedFileHashes := kubeOVNTLSFileHashes(updatedData)
		waitDeploymentTLSFilesProjected(cs, deploy, expectedFileHashes, "kube-ovn-controller")
		waitDeploymentTLSFilesProjected(cs, monitorDeploy, expectedFileHashes, "kube-ovn-monitor")
		waitDaemonSetTLSFilesProjected(daemonSetClient, pingerDS, expectedFileHashes, "kube-ovn-pinger")
		waitDeploymentTLSFilesProjected(cs, centralDeploy, expectedFileHashes, "ovn-central")
		waitPodListTLSFilesProjected(&corev1.PodList{Items: ovsPods}, expectedFileHashes, "ovs-ovn")

		ginkgo.By("Waiting for ovn-central to reload TLS")
		waitOVNCentralTLSReloaded(cs, centralDeploy, centralTLSHashes)
		waitDeploymentTLSHashFiles(cs, centralDeploy, ovnCentralTLSHashFile, "ovn-central")

		ginkgo.By("Waiting for ovn-controller to restart from TLS file change")
		waitOVNControllersRestarted(ovsPods, initialOVNControllerPIDs)
		waitPodListTLSHashFiles(&corev1.PodList{Items: ovsPods}, ovsOVNTLSHashFile, "ovs-ovn")

		ginkgo.By("Ensuring new TLS files can connect to OVN databases")
		assertDeploymentOVNDBSSLConnectivity(cs, deploy)
		assertPodListOVNDBSSLConnectivity(&corev1.PodList{Items: ovsPods}, "ovs-ovn")

		ginkgo.By("Ensuring Go components do not restart from TLS file change")
		time.Sleep(35 * time.Second)
		assertDeploymentContainerNotRestarted(cs, deploy, kubeOVNControllerContainer, restartCounts, "kube-ovn-controller")
		assertDeploymentContainerNotRestarted(cs, monitorDeploy, kubeOVNMonitorContainer, monitorRestartCounts, "kube-ovn-monitor")
		assertDaemonSetContainerNotRestarted(daemonSetClient, pingerDS, kubeOVNPingerContainer, pingerRestartCounts, "kube-ovn-pinger")
		assertDeploymentContainerNotRestarted(cs, centralDeploy, ovnCentralContainer, centralRestartCounts, "ovn-central")
	})
})

func deploymentEnvEnabled(deploy *appsv1.Deployment, name string) bool {
	ginkgo.GinkgoHelper()

	value, ok := deploymentEnvValue(deploy, name)
	return ok && value == "true"
}

func deploymentEnvValue(deploy *appsv1.Deployment, name string) (string, bool) {
	ginkgo.GinkgoHelper()

	for _, container := range deploy.Spec.Template.Spec.Containers {
		if container.Name != kubeOVNControllerContainer {
			continue
		}
		for _, env := range container.Env {
			if env.Name == name {
				return env.Value, true
			}
		}
	}
	return "", false
}

func copySecretData(data map[string][]byte) map[string][]byte {
	ginkgo.GinkgoHelper()

	copied := make(map[string][]byte, len(data))
	for key, value := range data {
		copied[key] = bytes.Clone(value)
	}
	return copied
}

func restoreSecretData(cs kubernetes.Interface, data map[string][]byte) {
	ginkgo.GinkgoHelper()

	updateKubeOVNTLSSecretData(cs, data)
}

func updateKubeOVNTLSSecretData(cs kubernetes.Interface, data map[string][]byte) *corev1.Secret {
	ginkgo.GinkgoHelper()

	hash := kubeOVNTLSDataHash(data)
	var updated *corev1.Secret
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		secret, err := cs.CoreV1().Secrets(framework.KubeOvnNamespace).Get(context.Background(), kubeOVNTLSSecretName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		secret = secret.DeepCopy()
		secret.Data = copySecretData(data)
		if secret.Annotations == nil {
			secret.Annotations = map[string]string{}
		}
		secret.Annotations[kubeOVNTLSCertHashAnnotation] = hash
		updated, err = cs.CoreV1().Secrets(framework.KubeOvnNamespace).Update(context.Background(), secret, metav1.UpdateOptions{})
		return err
	})
	framework.ExpectNoError(err)
	return updated
}

func kubeOVNTLSDataHash(data map[string][]byte) string {
	ginkgo.GinkgoHelper()

	h := sha256.New()
	for _, key := range kubeOVNTLSDataKeys {
		if len(data[key]) == 0 {
			framework.Failf("kube-ovn-tls missing %s", key)
		}
		h.Write([]byte(key))
		h.Write([]byte{0})
		h.Write(data[key])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func kubeOVNTLSFileHashes(data map[string][]byte) map[string]string {
	ginkgo.GinkgoHelper()

	hashes := make(map[string]string, len(kubeOVNTLSDataKeys))
	for _, key := range kubeOVNTLSDataKeys {
		if len(data[key]) == 0 {
			framework.Failf("kube-ovn-tls missing %s", key)
		}
		sum := sha256.Sum256(data[key])
		hashes[key] = hex.EncodeToString(sum[:])
	}
	return hashes
}

func kubeOVNTLSCertSerial(data map[string][]byte) string {
	ginkgo.GinkgoHelper()

	cert := parseKubeOVNTLSCert(data)
	return cert.SerialNumber.String()
}

func parseKubeOVNTLSCert(data map[string][]byte) *x509.Certificate {
	ginkgo.GinkgoHelper()

	cert, err := parseKubeOVNTLSCertWithError(data)
	framework.ExpectNoError(err)
	return cert
}

func parseKubeOVNTLSCertWithError(data map[string][]byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data["cert"])
	if block == nil {
		return nil, errors.New("failed to decode kube-ovn-tls cert")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	return cert, nil
}

func generateUpdatedKubeOVNTLSSecretData() (map[string][]byte, error) {
	return generateKubeOVNTLSSecretDataAt(time.Now(), 10*365*24*time.Hour, 10*365*24*time.Hour)
}

func generateKubeOVNTLSSecretDataAt(now time.Time, caDuration, certDuration time.Duration) (map[string][]byte, error) {
	return util.GenerateKubeOVNTLSSecretData(now, caDuration, certDuration, "ovn")
}

func deploymentContainerRestartCounts(cs kubernetes.Interface, deploy *appsv1.Deployment, containerName string) map[string]int32 {
	ginkgo.GinkgoHelper()

	pods := deploymentPods(cs, deploy)
	framework.ExpectNotEmpty(pods.Items, "no %s pod found", deploy.Name)

	counts := make(map[string]int32, len(pods.Items))
	for _, pod := range pods.Items {
		counts[pod.Name] = containerRestartCount(pod, containerName)
	}
	return counts
}

func daemonSetContainerRestartCounts(client *framework.DaemonSetClient, ds *appsv1.DaemonSet, containerName string) map[string]int32 {
	ginkgo.GinkgoHelper()

	pods, err := client.GetPods(ds)
	framework.ExpectNoError(err)
	framework.ExpectNotEmpty(pods.Items, "no %s pod found", ds.Name)

	counts := make(map[string]int32, len(pods.Items))
	for _, pod := range pods.Items {
		counts[pod.Name] = containerRestartCount(pod, containerName)
	}
	return counts
}

func assertDeploymentContainerNotRestarted(cs kubernetes.Interface, deploy *appsv1.Deployment, containerName string, previous map[string]int32, name string) {
	ginkgo.GinkgoHelper()

	assertPodsContainerNotRestarted(deploymentPods(cs, deploy), containerName, previous, name)
}

func assertDaemonSetContainerNotRestarted(client *framework.DaemonSetClient, ds *appsv1.DaemonSet, containerName string, previous map[string]int32, name string) {
	ginkgo.GinkgoHelper()

	pods, err := client.GetPods(ds)
	framework.ExpectNoError(err)
	assertPodsContainerNotRestarted(pods, containerName, previous, name)
}

func assertPodsContainerNotRestarted(pods *corev1.PodList, containerName string, previous map[string]int32, name string) {
	ginkgo.GinkgoHelper()

	framework.ExpectNotEmpty(pods.Items, "no %s pod found", name)
	for _, pod := range pods.Items {
		oldCount, ok := previous[pod.Name]
		if !ok {
			framework.Failf("%s pod %s/%s was replaced after TLS Secret update", name, pod.Namespace, pod.Name)
		}
		if got := containerRestartCount(pod, containerName); got != oldCount {
			framework.Failf("%s container in pod %s/%s restarted after TLS Secret update: got %d, want %d", name, pod.Namespace, pod.Name, got, oldCount)
		}
	}
}

func deploymentPods(cs kubernetes.Interface, deploy *appsv1.Deployment) *corev1.PodList {
	ginkgo.GinkgoHelper()

	selector, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
	framework.ExpectNoError(err)
	pods, err := cs.CoreV1().Pods(deploy.Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector.String()})
	framework.ExpectNoError(err)
	return pods
}

func containerRestartCount(pod corev1.Pod, containerName string) int32 {
	ginkgo.GinkgoHelper()

	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName {
			return status.RestartCount
		}
	}
	framework.Failf("container %s not found in pod %s/%s", containerName, pod.Namespace, pod.Name)
	return 0
}

func seedOVNCentralTLSHash(cs kubernetes.Interface, deploy *appsv1.Deployment) {
	ginkgo.GinkgoHelper()

	pods := deploymentPods(cs, deploy)
	framework.ExpectNotEmpty(pods.Items, "no ovn-central pod found")
	for _, pod := range pods.Items {
		_, err := e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, "bash /kube-ovn/kube-ovn-tls-reload.sh ovn-central once")
		framework.ExpectNoError(err)
	}
}

func deploymentTLSHashes(cs kubernetes.Interface, deploy *appsv1.Deployment) map[string]string {
	ginkgo.GinkgoHelper()

	pods := deploymentPods(cs, deploy)
	framework.ExpectNotEmpty(pods.Items, "no %s pod found", deploy.Name)
	hashes := make(map[string]string, len(pods.Items))
	for _, pod := range pods.Items {
		hashes[pod.Name] = podTLSHash(pod)
	}
	return hashes
}

func waitDeploymentTLSFilesProjected(cs kubernetes.Interface, deploy *appsv1.Deployment, expectedHashes map[string]string, name string) {
	ginkgo.GinkgoHelper()

	waitPodListTLSFilesProjected(deploymentPods(cs, deploy), expectedHashes, name)
}

func waitDaemonSetTLSFilesProjected(client *framework.DaemonSetClient, ds *appsv1.DaemonSet, expectedHashes map[string]string, name string) {
	ginkgo.GinkgoHelper()

	pods, err := client.GetPods(ds)
	framework.ExpectNoError(err)
	waitPodListTLSFilesProjected(pods, expectedHashes, name)
}

func waitPodListTLSFilesProjected(pods *corev1.PodList, expectedHashes map[string]string, name string) {
	ginkgo.GinkgoHelper()

	framework.ExpectNotEmpty(pods.Items, "no %s pod found", name)
	framework.WaitUntil(5*time.Second, 90*time.Second, func(_ context.Context) (bool, error) {
		for _, pod := range pods.Items {
			hashes, err := podTLSFileHashes(pod)
			if err != nil {
				return false, nil
			}
			if !stringMapEqual(hashes, expectedHashes) {
				return false, nil
			}
		}
		return true, nil
	}, fmt.Sprintf("%s projected kube-ovn-tls files", name))
}

func podTLSFileHashes(pod corev1.Pod) (map[string]string, error) {
	ginkgo.GinkgoHelper()

	output, err := e2epodoutput.RunHostCmd(
		pod.Namespace,
		pod.Name,
		`set -eu
for file in cacert cert key; do
  test -s "/var/run/tls/${file}"
  printf "%s %s\n" "${file}" "$(sha256sum "/var/run/tls/${file}" | awk '{print $1}')"
done`,
	)
	if err != nil {
		return nil, err
	}
	hashes := make(map[string]string, len(kubeOVNTLSDataKeys))
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("unexpected TLS file hash output from pod %s/%s: %q", pod.Namespace, pod.Name, output)
		}
		hashes[fields[0]] = fields[1]
	}
	return hashes, nil
}

func stringMapEqual(actual, expected map[string]string) bool {
	for _, key := range kubeOVNTLSDataKeys {
		if actual[key] != expected[key] {
			return false
		}
	}
	return true
}

func waitOVNCentralTLSReloaded(cs kubernetes.Interface, deploy *appsv1.Deployment, previousHashes map[string]string) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(5*time.Second, time.Minute, func(_ context.Context) (bool, error) {
		pods := deploymentPods(cs, deploy)
		if len(pods.Items) == 0 {
			return false, nil
		}
		for _, pod := range pods.Items {
			oldHash, ok := previousHashes[pod.Name]
			if !ok {
				return false, nil
			}
			if podTLSHash(pod) == oldHash {
				return false, nil
			}
			if _, err := e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, "/kube-ovn/ovn-healthcheck.sh"); err != nil {
				return false, nil
			}
		}
		return true, nil
	}, "ovn-central reloaded TLS after TLS Secret update")
}

func waitDeploymentTLSHashFiles(cs kubernetes.Interface, deploy *appsv1.Deployment, hashFile, name string) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(5*time.Second, time.Minute, func(_ context.Context) (bool, error) {
		return podListTLSHashFilesMatch(deploymentPods(cs, deploy), hashFile, name)
	}, fmt.Sprintf("%s TLS hash files match mounted TLS files", name))
}

func waitPodListTLSHashFiles(pods *corev1.PodList, hashFile, name string) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(5*time.Second, time.Minute, func(_ context.Context) (bool, error) {
		return podListTLSHashFilesMatch(pods, hashFile, name)
	}, fmt.Sprintf("%s TLS hash files match mounted TLS files", name))
}

func podListTLSHashFilesMatch(pods *corev1.PodList, hashFile, name string) (bool, error) {
	ginkgo.GinkgoHelper()

	framework.ExpectNotEmpty(pods.Items, "no %s pod found", name)
	for _, pod := range pods.Items {
		expected := podTLSHash(pod)
		actual, err := podFileContent(pod, hashFile)
		if err != nil || actual != expected {
			return false, nil
		}
	}
	return true, nil
}

func podFileContent(pod corev1.Pod, path string) (string, error) {
	ginkgo.GinkgoHelper()

	output, err := e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, fmt.Sprintf("cat %s", path))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func ovsOVNPods(f *framework.Framework) []corev1.Pod {
	ginkgo.GinkgoHelper()

	daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
	ds := daemonSetClient.Get(ovsOVNDaemonSet)
	pods, err := daemonSetClient.GetPods(ds)
	framework.ExpectNoError(err)
	framework.ExpectNotEmpty(pods.Items, "no ovs-ovn pod found")
	return pods.Items
}

func seedOVSTLSHashes(pods []corev1.Pod) {
	ginkgo.GinkgoHelper()

	for _, pod := range pods {
		_, err := e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, "bash /kube-ovn/kube-ovn-tls-reload.sh ovs once")
		framework.ExpectNoError(err)
	}
}

func podTLSHash(pod corev1.Pod) string {
	ginkgo.GinkgoHelper()

	output, err := e2epodoutput.RunHostCmd(
		pod.Namespace,
		pod.Name,
		"sha256sum /var/run/tls/cacert /var/run/tls/cert /var/run/tls/key | sha256sum | awk '{print $1}'",
	)
	framework.ExpectNoError(err)
	return strings.TrimSpace(output)
}

func ovnControllerPIDs(pods []corev1.Pod) map[string]string {
	ginkgo.GinkgoHelper()

	pids := make(map[string]string, len(pods))
	for _, pod := range pods {
		output, err := e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, "cat /var/run/ovn/ovn-controller.pid")
		framework.ExpectNoError(err)
		pids[pod.Name] = strings.TrimSpace(output)
	}
	return pids
}

func waitOVNControllersRestarted(pods []corev1.Pod, initialPIDs map[string]string) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(5*time.Second, time.Minute, func(_ context.Context) (bool, error) {
		for _, pod := range pods {
			output, err := e2epodoutput.RunHostCmd(
				pod.Namespace,
				pod.Name,
				"/kube-ovn/ovs-healthcheck.sh >/dev/null && cat /var/run/ovn/ovn-controller.pid",
			)
			if err != nil {
				return false, nil
			}
			pid := strings.TrimSpace(output)
			if pid == "" || pid == initialPIDs[pod.Name] {
				return false, nil
			}
		}
		return true, nil
	}, "ovn-controller restarted on every ovs-ovn pod after TLS Secret update")
}

func assertDeploymentOVNDBSSLConnectivity(cs kubernetes.Interface, deploy *appsv1.Deployment) {
	ginkgo.GinkgoHelper()

	pods := deploymentPods(cs, deploy)
	framework.ExpectNotEmpty(pods.Items, "no %s pod found", deploy.Name)
	for _, pod := range pods.Items {
		output, err := e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, ovnDBSSLConnectivityCommand(true))
		framework.ExpectNoError(err, "failed to connect to OVN DBs over SSL from pod %s/%s: %s", pod.Namespace, pod.Name, output)
	}
}

func assertPodListOVNDBSSLConnectivity(pods *corev1.PodList, name string) {
	ginkgo.GinkgoHelper()

	framework.ExpectNotEmpty(pods.Items, "no %s pod found", name)
	for _, pod := range pods.Items {
		output, err := e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, ovnDBSSLConnectivityCommand(false))
		framework.ExpectNoError(err, "failed to connect to OVN SB DB over SSL from pod %s/%s: %s", pod.Namespace, pod.Name, output)
	}
}

func ovnDBSSLConnectivityCommand(includeNB bool) string {
	command := `set -eu
gen_conn_str() {
  db="$1"
  if [ "${db}" = "nb" ]; then
    port="${KUBE_OVN_NB_PORT:-6641}"
  else
    port="${KUBE_OVN_SB_PORT:-6642}"
  fi
  if [ -z "${OVN_DB_IPS:-}" ]; then
    if [ "${db}" = "nb" ]; then
      echo "ssl:[${OVN_NB_SERVICE_HOST}]:${OVN_NB_SERVICE_PORT}"
    else
      echo "ssl:[${OVN_SB_SERVICE_HOST}]:${OVN_SB_SERVICE_PORT}"
    fi
    return
  fi
  endpoints=""
  for ip in $(printf "%s" "${OVN_DB_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g'); do
    endpoints="${endpoints}ssl:[${ip}]:${port},"
  done
  echo "${endpoints%,}"
}
ssl_options="-p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert"
`
	if includeNB {
		command += `ovn-nbctl ${ssl_options} --db="$(gen_conn_str nb)" --timeout=10 get NB_Global . _uuid >/dev/null
`
	}
	command += `ovn-sbctl ${ssl_options} --db="$(gen_conn_str sb)" --timeout=10 get SB_Global . _uuid >/dev/null
`
	return command
}
