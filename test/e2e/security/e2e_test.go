package security

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	"k8s.io/kubernetes/test/e2e/framework/deployment"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)

	// Parse all the flags
	flag.Parse()
	if k8sframework.TestContext.KubeConfig == "" {
		k8sframework.TestContext.KubeConfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
}

func TestE2E(t *testing.T) {
	e2e.RunE2ETests(t)
}

func checkDeployment(cs clientset.Interface, name, process string, ports ...string) {
	ginkgo.By("Getting deployment " + name)
	deploy, err := cs.AppsV1().Deployments(framework.KubeOvnNamespace).Get(context.TODO(), name, metav1.GetOptions{})
	framework.ExpectNoError(err, "failed to to get deployment")
	err = deployment.WaitForDeploymentComplete(cs, deploy)
	framework.ExpectNoError(err, "deployment failed to complete")

	ginkgo.By("Getting pods")
	pods, err := deployment.GetPodsForDeployment(cs, deploy)
	framework.ExpectNoError(err, "failed to get pods")
	framework.ExpectNotEmpty(pods.Items)

	checkPods(pods.Items, process, ports...)
}

func checkPods(pods []corev1.Pod, process string, ports ...string) {
	ginkgo.By("Parsing environment variable")
	var envValue string
	for _, env := range pods[0].Spec.Containers[0].Env {
		if env.Name == "ENABLE_BIND_LOCAL_IP" {
			envValue = env.Value
			break
		}
	}
	if envValue == "" {
		envValue = "false"
	}
	listenPodIP, err := strconv.ParseBool(envValue)
	framework.ExpectNoError(err)

	ginkgo.By("Validating " + process + " listen addresses")
	cmd := fmt.Sprintf(`ss -Hntpl | grep -wE pid=$(pidof %s | sed "s/ /|pid=/g") | awk '{print $4}'`, process)
	if len(ports) != 0 {
		cmd += fmt.Sprintf(`| grep -E ':%s$'`, strings.Join(ports, `$|:`))
	}
	for _, pod := range pods {
		stdout, _, err := framework.KubectlExec(pod.Namespace, pod.Name, cmd)
		framework.ExpectNoError(err)

		listenAddresses := strings.Split(string(bytes.TrimSpace(stdout)), "\n")
		podIPPrefix := strings.TrimSuffix(net.JoinHostPort(pod.Status.PodIP, "999"), "999")
		for _, addr := range listenAddresses {
			if listenPodIP {
				framework.ExpectTrue(strings.HasPrefix(addr, podIPPrefix))
			} else {
				framework.ExpectTrue(strings.HasPrefix(addr, "*:"))
			}
		}
	}
}

var _ = framework.Describe("[group:security]", func() {
	f := framework.NewDefaultFramework("security")
	f.SkipNamespaceCreation = true

	var cs clientset.Interface
	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 9, "Support for listening on Pod IP was introduced in v1.9")
		cs = f.ClientSet
	})

	framework.ConformanceIt("ovn db should listen on specified addresses for client connections", func() {
		checkDeployment(cs, "ovn-central", "ovsdb-server", "6641", "6642")
	})

	framework.ConformanceIt("kube-ovn-controller should listen on specified addresses", func() {
		checkDeployment(cs, "kube-ovn-controller", "kube-ovn-controller")
	})

	framework.ConformanceIt("kube-ovn-monitor should listen on specified addresses", func() {
		checkDeployment(cs, "kube-ovn-monitor", "kube-ovn-monitor")
	})

	framework.ConformanceIt("kube-ovn-cni should listen on specified addresses", func() {
		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)

		ginkgo.By("Getting daemonset kube-ovn-cni")
		ds, err := cs.AppsV1().DaemonSets(framework.KubeOvnNamespace).Get(context.TODO(), "kube-ovn-cni", metav1.GetOptions{})
		framework.ExpectNoError(err, "failed to to get daemonset")

		ginkgo.By("Getting kube-ovn-cni pods")
		pods := make([]corev1.Pod, 0, len(nodeList.Items))
		for _, node := range nodeList.Items {
			pod, err := framework.GetPodOnNodeForDaemonSet(cs, ds, node.Name)
			framework.ExpectNoError(err, "failed to get kube-ovn-cni pod running on node %s", node.Name)
			pods = append(pods, *pod)
		}

		checkPods(pods, "kube-ovn-daemon")
	})
})
