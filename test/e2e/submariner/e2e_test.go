package submariner

import (
	"flag"
	"fmt"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2ekubectl "k8s.io/kubernetes/test/e2e/framework/kubectl"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var clusters []string

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

	var err error
	if clusters, err = kind.ListClusters(); err != nil {
		panic(fmt.Sprintf("failed to list kind clusters: %v", err))
	}
	if len(clusters) < 2 {
		panic("no enough kind clusters to run ovn-ic e2e testing")
	}
}

func TestE2E(t *testing.T) {
	e2e.RunE2ETests(t)
}

func execOrDie(kubeContext, cmd string) string {
	ginkgo.By(`Switching context to ` + kubeContext)
	e2ekubectl.NewKubectlCommand("", "config", "use-context", kubeContext).ExecOrDie("")

	ginkgo.By(`Executing "kubectl ` + cmd + `"`)
	return e2ekubectl.NewKubectlCommand("", strings.Fields(cmd)...).ExecOrDie("")
}

func execPodOrDie(kubeContext, namespace, pod, cmd string) string {
	ginkgo.By(`Switching context to ` + kubeContext)
	e2ekubectl.NewKubectlCommand("", "config", "use-context", kubeContext).ExecOrDie("")

	ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, namespace, pod))
	return e2epodoutput.RunHostCmdOrDie(namespace, pod, cmd)
}

var _ = framework.OrderedDescribe("[group:submariner]", func() {
	frameworks := make([]*framework.Framework, len(clusters))
	for i := range clusters {
		frameworks[i] = framework.NewFrameworkWithContext("submariner", "kind-"+clusters[i])
	}

	clientSets := make([]clientset.Interface, len(clusters))
	podClients := make([]*framework.PodClient, len(clusters))
	namespaceNames := make([]string, len(clusters))
	var kubectlConfig string
	ginkgo.BeforeEach(func() {
		for i := range clusters {
			clientSets[i] = frameworks[i].ClientSet
			podClients[i] = frameworks[i].PodClient()
			namespaceNames[i] = frameworks[i].Namespace.Name
		}
		kubectlConfig = k8sframework.TestContext.KubeConfig
		k8sframework.TestContext.KubeConfig = ""
	})
	ginkgo.AfterEach(func() {
		k8sframework.TestContext.KubeConfig = kubectlConfig
	})

	fnCheckPodHTTP := func() {
		podNames := make([]string, len(clusters))
		pods := make([]*corev1.Pod, len(clusters))
		ports := make([]string, len(clusters))
		for i := range clusters {
			podNames[i] = "pod-" + framework.RandomSuffix()
			ginkgo.By("Creating pod " + podNames[i] + " in cluster " + clusters[i])
			port := 8000 + rand.Intn(1000)
			ports[i] = strconv.Itoa(port)
			args := []string{"netexec", "--http-port", ports[i]}
			pods[i] = framework.MakePod(namespaceNames[i], podNames[i], nil, nil, framework.AgnhostImage, nil, args)
			pods[i].Spec.Containers[0].ReadinessProbe = &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(port),
					},
				},
			}
			pods[i] = podClients[i].CreateSync(pods[i])
		}

		for i := range clusters {
			sourceIPs := make([]string, 0, len(pods[i].Status.PodIPs))
			for _, podIP := range pods[i].Status.PodIPs {
				sourceIPs = append(sourceIPs, podIP.IP)
			}

			for j := range clusters {
				if j == i {
					continue
				}

				for _, podIP := range pods[j].Status.PodIPs {
					ip := podIP.IP
					protocol := strings.ToLower(util.CheckProtocol(ip))
					ginkgo.By("Checking connection from cluster " + clusters[i] + " to cluster " + clusters[j] + " via " + protocol)
					cmd := fmt.Sprintf("curl -q -s --connect-timeout 5 %s/clientip", net.JoinHostPort(ip, ports[j]))
					output := execPodOrDie(frameworks[i].KubeContext, pods[i].Namespace, pods[i].Name, cmd)
					client, _, err := net.SplitHostPort(strings.TrimSpace(output))
					framework.ExpectNoError(err)
					framework.ExpectContainElement(sourceIPs, client)
				}
			}
		}
	}

	fnCheckSvcHTTP := func() {
		podNames := make([]string, len(clusters))
		pods := make([]*corev1.Pod, len(clusters))
		ports := make([]string, len(clusters))
		for i := range clusters {
			podNames[i] = "pod-" + framework.RandomSuffix()
			ginkgo.By("Creating pod " + podNames[i] + " in cluster " + clusters[i])
			port := 8000 + rand.Intn(1000)
			ports[i] = strconv.Itoa(port)
			args := []string{"netexec", "--http-port", ports[i]}
			pods[i] = framework.MakePod(namespaceNames[i], podNames[i], nil, nil, framework.AgnhostImage, nil, args)
			pods[i].Spec.Containers[0].ReadinessProbe = &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(port),
					},
				},
			}
			pods[i] = podClients[i].CreateSync(pods[i])
		}
	}

	framework.ConformanceIt("should be able to communicate between clusters", func() {
		fnCheckPodHTTP()
	})

})
