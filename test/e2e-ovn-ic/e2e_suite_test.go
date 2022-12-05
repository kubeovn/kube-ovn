package e2e_ovn_ic_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func TestE2eOvnIc(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kube-OVN E2E OVN-IC Suite")
}

var _ = SynchronizedAfterSuite(func() {}, func() {

	output, err := exec.Command("kubectl", "config", "use-context", "kind-kube-ovn").CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), string(output))
	f := framework.NewFramework("init", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))

	pods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "ovn-nb-leader=true"})
	Expect(err).NotTo(HaveOccurred())
	if len(pods.Items) != 1 {
		Fail(fmt.Sprintf("pods %s not right", pods))
	}

	cmdLS := "ovn-nbctl --format=csv  --data=bare --columns=name --no-heading find logical_switch name=ts"
	sout, _, err := f.ExecToPodThroughAPI(cmdLS, "ovn-central", pods.Items[0].Name, pods.Items[0].Namespace, nil)
	if err != nil {
		Fail(fmt.Sprintf("switch ts does not exist in pod %s for %s", pods.Items[0].Name, err))
	}
	if strings.TrimSpace(sout) != "ts" {
		Fail(fmt.Sprintf("switch ts is not right as %s", sout))
	}

	checkLSP("ts-az1", pods.Items[0], f)
	checkLSP("ts-az0", pods.Items[0], f)

	output, err = exec.Command("kubectl", "-n", "kube-system", "-l", "app=kube-ovn-pinger", "get", "pod", "-o=jsonpath={.items[0].status.podIP}").CombinedOutput()
	Expect(err).NotTo(HaveOccurred())
	if net.ParseIP(string(output)) == nil {
		Fail(fmt.Sprintf("pinger ip %s not right", output))
	}
	ip0 := string(output)

	// To avoid the situation that the wrong kube-config context is loaded in framework, and then the test cloud always
	// pass the test. a replacement kube-client solution is introduced to force the correct context pod-list to be read.
	// Then if framework read the wrong context, it will get wrong pod which from another cluster.
	output, err = exec.Command("kubectl", "config", "use-context", "kind-kube-ovn1").CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), string(output))
	f = framework.NewFramework("init", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))
	kubecfg1, err := buildConfigFromFlags("kind-kube-ovn1", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))
	Expect(err).NotTo(HaveOccurred())
	kubeClient1, err := kubernetes.NewForConfig(kubecfg1)
	Expect(err).NotTo(HaveOccurred())

	pods1, err := kubeClient1.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "ovn-nb-leader=true"})
	Expect(err).NotTo(HaveOccurred())
	if len(pods1.Items) != 1 {
		Fail(fmt.Sprintf("pods %s length not 1", pods1))
	}

	sout, _, err = f.ExecToPodThroughAPI(cmdLS, "ovn-central", pods1.Items[0].Name, pods1.Items[0].Namespace, nil)
	if err != nil {
		Fail(fmt.Sprintf("switch ts does not exist in pod %s with %s", pods1.Items[0].Name, err))
	}
	if strings.TrimSpace(sout) != "ts" {
		Fail(fmt.Sprintf("switch ts is not right as %s", sout))
	}

	checkLSP("ts-az1", pods1.Items[0], f)
	checkLSP("ts-az0", pods1.Items[0], f)

	pod, err := exec.Command("kubectl", "-n", "kube-system", "-l", "app=kube-ovn-pinger", "get", "pod", "-o=jsonpath={.items[0].metadata.name}").CombinedOutput()
	Expect(err).NotTo(HaveOccurred())
	Expect(pod).ShouldNot(BeEmpty())

	output, err = exec.Command("kubectl", "-n", "kube-system", "exec", "-i", string(pod), "--", "/usr/bin/ping", ip0, "-c2").CombinedOutput()
	Expect(err).NotTo(HaveOccurred())
	Expect(string(output)).Should(ContainSubstring("0% packet loss"))

	output, err = exec.Command("kubectl", "apply", "-f", "/tmp/ovn-ic-1-alter.yaml").CombinedOutput()
	Expect(err).NotTo(HaveOccurred())
	Expect(string(output)).Should(ContainSubstring("configured"))

	time.Sleep(time.Second * 10)

	checkLSP("ts-az1111", pods1.Items[0], f)

	output, err = exec.Command("kubectl", "-n", "kube-system", "exec", "-i", string(pod), "--", "/usr/bin/ping", ip0, "-c2").CombinedOutput()
	Expect(err).NotTo(HaveOccurred())
	Expect(string(output)).Should(ContainSubstring("0% packet loss"))

})

func buildConfigFromFlags(context, kubeconfigPath string) (*rest.Config, error) {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		}).ClientConfig()
}

func checkLSP(lspName string, pod v1.Pod, f *framework.Framework) {
	cmd := fmt.Sprintf("ovn-nbctl --format=csv  --data=bare --columns=name --no-heading find logical_switch_port name=%s", lspName)
	sout, _, err := f.ExecToPodThroughAPI(cmd, "ovn-central", pod.Name, pod.Namespace, nil)
	if err != nil {
		Fail(fmt.Sprintf("switch port %s ts does not exist", lspName))
	}
	if strings.TrimSpace(sout) != lspName {
		Fail(fmt.Sprintf("switch port %s is not right as %s", lspName, sout))
	}
}
