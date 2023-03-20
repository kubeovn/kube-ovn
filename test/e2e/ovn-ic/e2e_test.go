package ovn_ic

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2ekubectl "k8s.io/kubernetes/test/e2e/framework/kubectl"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

var clusters []string

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	if k8sframework.TestContext.KubeConfig == "" {
		k8sframework.TestContext.KubeConfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)

	var err error
	if clusters, err = kind.ListClusters(); err != nil {
		t.Fatalf("failed to list kind clusters: %v", err)
	}
	if len(clusters) < 2 {
		t.Fatal("no enough kind clusters to run ovn-ic e2e testing")
	}

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

var _ = framework.OrderedDescribe("[group:ovn-ic]", func() {
	frameworks := make([]*framework.Framework, len(clusters))
	for i := range clusters {
		frameworks[i] = framework.NewFrameworkWithContext("ovn-ic", "kind-"+clusters[i])
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

	framework.ConformanceIt("should create logical switch ts", func() {
		azNames := make([]string, len(clusters))
		for i := range clusters {
			ginkgo.By("fetching the ConfigMap in cluster " + clusters[i])
			cm, err := clientSets[i].CoreV1().ConfigMaps(framework.KubeOvnNamespace).Get(context.TODO(), util.InterconnectionConfig, metav1.GetOptions{})
			framework.ExpectNoError(err, "failed to get ConfigMap")
			azNames[i] = cm.Data["az-name"]
		}

		for i := range clusters {
			ginkgo.By("Ensuring logical switch ts exists in cluster " + clusters[i])
			output := execOrDie(frameworks[i].KubeContext, "ko nbctl show ts")
			for _, az := range azNames {
				framework.ExpectTrue(strings.Contains(output, "ts-"+az), "should have lsp ts-"+az)
			}
		}
	})

	framework.ConformanceIt("should be able to communicate between clusters", func() {
		fnCheckPodHTTP()
	})

	framework.ConformanceIt("should be able to update az name", func() {
		frameworks[0].SkipVersionPriorTo(1, 11, "This feature was introduced in v1.11")

		azNames := make([]string, len(clusters))
		for i := range clusters {
			ginkgo.By("fetching the ConfigMap in cluster " + clusters[i])
			cm, err := clientSets[i].CoreV1().ConfigMaps(framework.KubeOvnNamespace).Get(context.TODO(), util.InterconnectionConfig, metav1.GetOptions{})
			framework.ExpectNoError(err, "failed to get ConfigMap")
			azNames[i] = cm.Data["az-name"]
		}

		azNames[0] = fmt.Sprintf("az%04d", rand.Intn(10000))
		configMapPatchPayload, err := json.Marshal(corev1.ConfigMap{
			Data: map[string]string{
				"az-name": azNames[0],
			},
		})
		framework.ExpectNoError(err, "failed to marshal patch data")

		ginkgo.By("patching the ConfigMap in cluster " + clusters[0])
		_, err = clientSets[0].CoreV1().ConfigMaps(framework.KubeOvnNamespace).Patch(context.TODO(), util.InterconnectionConfig, k8stypes.StrategicMergePatchType, []byte(configMapPatchPayload), metav1.PatchOptions{})
		framework.ExpectNoError(err, "failed to patch ConfigMap")

		ginkgo.By("Waiting for new az names to be applied")
		time.Sleep(10 * time.Second)

		pods, err := clientSets[0].CoreV1().Pods(framework.KubeOvnNamespace).List(context.TODO(), metav1.ListOptions{LabelSelector: "app=ovs"})
		framework.ExpectNoError(err, "failed to get ovs-ovn pods")
		cmd := "ovn-appctl -t ovn-controller inc-engine/recompute"
		for _, pod := range pods.Items {
			execPodOrDie(frameworks[0].KubeContext, pod.Namespace, pod.Name, cmd)
		}
		time.Sleep(2 * time.Second)

		ginkgo.By("Ensuring logical switch ts exists in cluster " + clusters[0])
		output := execOrDie(frameworks[0].KubeContext, "ko nbctl show ts")
		for _, az := range azNames {
			lsp := "ts-" + az
			framework.ExpectTrue(strings.Contains(output, lsp), "should have lsp "+lsp)
			framework.ExpectTrue(strings.Contains(output, lsp), "should have lsp "+lsp)
		}

		fnCheckPodHTTP()
	})
})
