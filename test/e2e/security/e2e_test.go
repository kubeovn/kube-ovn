package security

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net"
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

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
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

func checkDeployment(f *framework.Framework, name, process string, ports ...string) {
	ginkgo.GinkgoHelper()

	ginkgo.By("Getting deployment " + name)
	deploy, err := f.ClientSet.AppsV1().Deployments(framework.KubeOvnNamespace).Get(context.TODO(), name, metav1.GetOptions{})
	framework.ExpectNoError(err, "failed to to get deployment")
	err = deployment.WaitForDeploymentComplete(f.ClientSet, deploy)
	framework.ExpectNoError(err, "deployment failed to complete")

	ginkgo.By("Getting pods")
	pods, err := deployment.GetPodsForDeployment(context.Background(), f.ClientSet, deploy)
	framework.ExpectNoError(err, "failed to get pods")
	framework.ExpectNotEmpty(pods.Items)

	checkPods(f, pods.Items, process, ports...)
}

func checkPods(f *framework.Framework, pods []corev1.Pod, process string, ports ...string) {
	ginkgo.GinkgoHelper()

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

	if listenPodIP &&
		(len(pods[0].Status.PodIPs) != 1 && (!strings.HasPrefix(process, "kube-ovn-") || f.VersionPriorTo(1, 13))) &&
		(process != "ovsdb-server" || f.VersionPriorTo(1, 12)) {
		// ovn db processes support listening on both ipv4 and ipv6 addresses in versions >= 1.12
		listenPodIP = false
	}

	ginkgo.By("Validating " + process + " listen addresses")
	cmd := fmt.Sprintf(`ss -Hntpl | grep -wE pid=$(pidof %s | sed "s/ /|pid=/g") | awk '{print $4}'`, process)
	if len(ports) != 0 {
		cmd += fmt.Sprintf(`| grep -E ':%s$'`, strings.Join(ports, `$|:`))
	}
	for _, pod := range pods {
		framework.ExpectTrue(pod.Spec.HostNetwork, "pod %s/%s is not using host network", pod.Namespace, pod.Name)

		c, err := docker.ContainerInspect(pod.Spec.NodeName)
		framework.ExpectNoError(err)
		stdout, _, err := docker.Exec(c.ID, nil, "sh", "-c", cmd)
		framework.ExpectNoError(err)

		listenAddresses := strings.Split(string(bytes.TrimSpace(stdout)), "\n")
		if len(ports) != 0 {
			expected := make([]string, 0, len(pod.Status.PodIPs)*len(ports))
			for _, port := range ports {
				if listenPodIP {
					for _, ip := range pod.Status.PodIPs {
						expected = append(expected, net.JoinHostPort(ip.IP, port))
					}
				} else {
					expected = append(expected, net.JoinHostPort("*", port))
				}
			}
			framework.ExpectConsistOf(listenAddresses, expected)
		} else {
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
		checkDeployment(f, "ovn-central", "ovsdb-server", strconv.Itoa(int(util.NBDatabasePort)), strconv.Itoa(int(util.SBDatabasePort)))
	})

	framework.ConformanceIt("kube-ovn-controller should listen on specified addresses", func() {
		checkDeployment(f, "kube-ovn-controller", "kube-ovn-controller", "10660")
	})

	framework.ConformanceIt("kube-ovn-monitor should listen on specified addresses", func() {
		checkDeployment(f, "kube-ovn-monitor", "kube-ovn-monitor", "10661")
	})

	framework.ConformanceIt("kube-ovn-cni should listen on specified addresses", func() {
		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)

		ginkgo.By("Getting daemonset kube-ovn-cni")
		daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
		ds := daemonSetClient.Get("kube-ovn-cni")

		ginkgo.By("Getting kube-ovn-cni pods")
		pods := make([]corev1.Pod, 0, len(nodeList.Items))
		for _, node := range nodeList.Items {
			pod, err := daemonSetClient.GetPodOnNode(ds, node.Name)
			framework.ExpectNoError(err, "failed to get kube-ovn-cni pod running on node %s", node.Name)
			pods = append(pods, *pod)
		}

		checkPods(f, pods, "kube-ovn-daemon", "10665")
	})
})
