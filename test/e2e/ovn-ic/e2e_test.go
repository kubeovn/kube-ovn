package ovn_ic

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
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
	var err error
	if clusters, err = kind.ListClusters(); err != nil {
		t.Fatalf("failed to list kind clusters: %v", err)
	}
	if len(clusters) < 2 {
		t.Fatal("not enough kind clusters to run ovn-ic e2e testing")
	}

	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
	e2e.RunE2ETests(t)
}

func execOrDie(kubeContext, cmd string) string {
	ginkgo.GinkgoHelper()

	ginkgo.By(`Using context ` + kubeContext)
	k8sframework.TestContext.KubeContext = kubeContext

	args := framework.KubectlKoArgs(strings.Fields(cmd)...)
	ginkgo.By(`Executing "kubectl ` + strings.Join(args, " ") + `"`)
	command, cleanup, err := kubectlKoCommand(kubeContext, args...)
	framework.ExpectNoError(err, "failed to prepare kubectl ko command")
	defer func() {
		framework.ExpectNoError(cleanup(), "failed to remove temporary kubeconfig")
	}()
	output, err := command.Output()
	framework.ExpectNoError(err, "kubectl ko command failed")
	return string(output)
}

func kubectlKoCommand(kubeContext string, args ...string) (*exec.Cmd, func() error, error) {
	testConfig := e2ekubectl.NewTestKubeconfig(k8sframework.TestContext.CertDir,
		k8sframework.TestContext.Host, k8sframework.TestContext.KubeConfig,
		kubeContext, k8sframework.TestContext.KubectlPath, "")
	if framework.KubectlKoUsesLegacySyntax() {
		command := exec.Command(testConfig.KubectlPath, "ko")
		command.Args = append(command.Args, args[1:]...)
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		loadingRules.ExplicitPath = testConfig.KubeConfig
		config, err := loadingRules.Load()
		if err != nil {
			return nil, nil, fmt.Errorf("load kubeconfig: %w", err)
		}
		if _, ok := config.Contexts[kubeContext]; !ok {
			return nil, nil, fmt.Errorf("kubeconfig has no context %q", kubeContext)
		}
		config.CurrentContext = kubeContext

		temporaryConfig, err := os.CreateTemp("", ".kube-ovn-e2e-kubeconfig-")
		if err != nil {
			return nil, nil, fmt.Errorf("create temporary kubeconfig: %w", err)
		}
		temporaryPath := temporaryConfig.Name()
		if err := temporaryConfig.Close(); err != nil {
			return nil, nil, fmt.Errorf("close temporary kubeconfig %q: %w", temporaryPath, errors.Join(err, os.Remove(temporaryPath)))
		}
		if err := clientcmd.WriteToFile(*config, temporaryPath); err != nil {
			return nil, nil, fmt.Errorf("write temporary kubeconfig %q: %w", temporaryPath, errors.Join(err, os.Remove(temporaryPath)))
		}

		command.Env = environmentWithKubeconfig(temporaryPath)
		return command, func() error { return os.Remove(temporaryPath) }, nil
	}

	command := testConfig.KubectlCmd()
	pluginArgs := append([]string{"ko"}, command.Args[1:]...)
	pluginArgs = append(pluginArgs, args[1:]...)
	return exec.Command(command.Path, pluginArgs...), func() error { return nil }, nil
}

func environmentWithKubeconfig(path string) []string {
	env := os.Environ()
	filtered := env[:0]
	for _, value := range env {
		if !strings.HasPrefix(value, "KUBECONFIG=") {
			filtered = append(filtered, value)
		}
	}
	return append(filtered, "KUBECONFIG="+path)
}

func execPodOrDie(kubeContext, namespace, pod, cmd string) string {
	ginkgo.GinkgoHelper()

	ginkgo.By(`Using context ` + kubeContext)
	k8sframework.TestContext.KubeContext = kubeContext

	ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, namespace, pod))
	return e2epodoutput.RunHostCmdOrDie(namespace, pod, cmd)
}

// The specs mutate shared interconnection state and must run in declaration order.
var _ = framework.OrderedDescribe("[group:ovn-ic]", func() {
	frameworks := make([]*framework.Framework, len(clusters))
	for i := range clusters {
		frameworks[i] = framework.NewFrameworkWithContext("ovn-ic", "kind-"+clusters[i])
	}

	clientSets := make([]clientset.Interface, len(clusters))
	podClients := make([]*framework.PodClient, len(clusters))
	namespaceNames := make([]string, len(clusters))
	ginkgo.BeforeEach(func() {
		for i := range clusters {
			clientSets[i] = frameworks[i].ClientSet
			podClients[i] = frameworks[i].PodClient()
			namespaceNames[i] = frameworks[i].Namespace.Name
		}
	})

	fnCheckPodHTTP := func() {
		ginkgo.GinkgoHelper()

		podNames := make([]string, len(clusters))
		pods := make([]*corev1.Pod, len(clusters))
		ports := make([]string, len(clusters))
		for i := range clusters {
			podNames[i] = "pod-" + framework.RandomSuffix()
			ginkgo.By("Creating pod " + podNames[i] + " in cluster " + clusters[i])
			port := 8000 + rand.Int32N(1000)
			ports[i] = strconv.Itoa(int(port))
			args := []string{"netexec", "--http-port", ports[i]}
			pods[i] = framework.MakePod(namespaceNames[i], podNames[i], nil, nil, framework.AgnhostImage, nil, args)
			pods[i].Spec.Containers[0].ReadinessProbe = &corev1.Probe{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt32(port),
				},
			}
			pods[i] = podClients[i].CreateSync(pods[i])
		}

		for i := range clusters {
			sourceIPs := util.PodIPs(*pods[i])
			for j := range clusters {
				if j == i {
					continue
				}

				for _, podIP := range pods[j].Status.PodIPs {
					ip := podIP.IP
					protocol := strings.ToLower(util.CheckProtocol(ip))
					ginkgo.By("Checking connection from cluster " + clusters[i] + " to cluster " + clusters[j] + " via " + protocol)
					cmd := fmt.Sprintf("curl -q -s --connect-timeout 2 --max-time 2 %s/clientip", net.JoinHostPort(ip, ports[j]))
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

	framework.ConformanceIt("should query interconnection database leaders with kubectl ko", func() {
		frameworks[0].SkipVersionPriorTo(1, 17, "The structured Go plugin was introduced in v1.17")
		var serverContexts []string
		azNames := make([]string, len(clusters))
		for i := range clusters {
			cm, err := clientSets[i].CoreV1().ConfigMaps(framework.KubeOvnNamespace).Get(context.Background(), util.InterconnectionConfig, metav1.GetOptions{})
			framework.ExpectNoError(err)
			azNames[i] = cm.Data["az-name"]
			framework.ExpectNotEmpty(azNames[i])
			_, err = clientSets[i].AppsV1().Deployments(framework.KubeOvnNamespace).Get(context.Background(), "ovn-ic-server", metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				continue
			}
			framework.ExpectNoError(err)
			serverContexts = append(serverContexts, frameworks[i].KubeContext)
		}
		framework.ExpectHaveLen(serverContexts, 1, "expected exactly one cluster hosting the interconnection server")
		kubeContext := serverContexts[0]
		for _, database := range []struct {
			tool, table string
			names       []string
		}{{"ic-nbctl", "Transit_Switch", []string{util.InterconnectionSwitch}}, {"ic-sbctl", "Availability_Zone", azNames}} {
			ginkgo.By("Querying " + database.table + " through " + database.tool)
			output := execOrDie(kubeContext, "ko exec "+database.tool+" -- --format=json --columns=name list "+database.table)
			var table struct {
				Headings []string   `json:"headings"`
				Data     [][]string `json:"data"`
			}
			framework.ExpectNoError(jsonv2.Unmarshal([]byte(output), &table))
			framework.ExpectEqual(table.Headings, []string{"name"})
			for _, name := range database.names {
				framework.ExpectContainElement(table.Data, []string{name})
			}
		}
	})

	framework.ConformanceIt("should be able to communicate between clusters", func() {
		ginkgo.By("case 1: Pod in different clusters can be communicated")
		fnCheckPodHTTP()

		ginkgo.By("case 2: Delete configmap ovn-ic-config and rebuild it")
		k8sframework.TestContext.KubeContext = frameworks[len(frameworks)-1].KubeContext
		configMap := e2ekubectl.NewKubectlCommand("kube-system", "get", "configmap", "ovn-ic-config", "-o", "yaml").ExecOrDie("")
		e2ekubectl.NewKubectlCommand("kube-system", "delete", "configmap", "ovn-ic-config").ExecOrDie("")
		e2ekubectl.NewKubectlCommand("kube-system", "apply", "-f", "-").WithStdinData(configMap).ExecOrDie("")
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

		azNames[0] = fmt.Sprintf("az%04d", rand.IntN(10000))
		configMapPatchPayload, err := json.Marshal(corev1.ConfigMap{
			Data: map[string]string{
				"az-name": azNames[0],
			},
		})
		framework.ExpectNoError(err, "failed to marshal patch data")

		ginkgo.By("patching the ConfigMap in cluster " + clusters[0])
		_, err = clientSets[0].CoreV1().ConfigMaps(framework.KubeOvnNamespace).Patch(context.TODO(), util.InterconnectionConfig, k8stypes.StrategicMergePatchType, configMapPatchPayload, metav1.PatchOptions{})
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

	framework.ConformanceIt("Should Support ECMP OVN Interconnection", func() {
		frameworks[0].SkipVersionPriorTo(1, 11, "This feature was introduced in v1.11")
		ginkgo.By("case 1: ecmp gateway network test")
		if frameworks[0].ClusterIPFamily == "dual" {
			checkECMPCount(6)
		} else {
			checkECMPCount(3)
		}
		fnCheckPodHTTP()

		ginkgo.By("case 2: reduce two clusters from 3 gateway to 1 gateway")
		oldGatewayStr := make([]string, len(clusters))
		gwNodes := make([]string, len(clusters))
		for i := range clusters {
			ginkgo.By("fetching the ConfigMap in cluster " + clusters[i])
			cm, err := clientSets[i].CoreV1().ConfigMaps(framework.KubeOvnNamespace).Get(context.TODO(), util.InterconnectionConfig, metav1.GetOptions{})
			framework.ExpectNoError(err, "failed to get ConfigMap")
			gwNodes[i] = cm.Data["gw-nodes"]
			oldGatewayStr[i] = cm.Data["gw-nodes"]
			gws := strings.Split(gwNodes[i], ",")
			newGatewayStr := strings.Join(gws[0:len(gws)-2], ",")

			configMapPatchPayload, _ := json.Marshal(corev1.ConfigMap{
				Data: map[string]string{
					"gw-nodes": newGatewayStr,
				},
			})
			_, err = clientSets[i].CoreV1().ConfigMaps(framework.KubeOvnNamespace).Patch(context.TODO(), util.InterconnectionConfig, k8stypes.StrategicMergePatchType, configMapPatchPayload, metav1.PatchOptions{})
			framework.ExpectNoError(err, "patch ovn-ic-config failed")
		}
		fnCheckPodHTTP()

		ginkgo.By("case 3: recover two clusters from 1 gateway to 3 gateway")
		for i := range clusters {
			ginkgo.By("fetching the ConfigMap in cluster " + clusters[i])

			configMapPatchPayload, _ := json.Marshal(corev1.ConfigMap{
				Data: map[string]string{
					"gw-nodes": oldGatewayStr[i],
				},
			})

			_, err := clientSets[i].CoreV1().ConfigMaps(framework.KubeOvnNamespace).Patch(context.TODO(), util.InterconnectionConfig, k8stypes.StrategicMergePatchType, configMapPatchPayload, metav1.PatchOptions{})
			framework.ExpectNoError(err, "patch ovn-ic-config failed")
		}
		fnCheckPodHTTP()

		ginkgo.By("case 4: scale ecmp path from 3 to 5")
		k8sframework.TestContext.KubeContext = "kind-kube-ovn"
		e2ekubectl.NewKubectlCommand("kube-system", "patch", "deployment", "ovn-ic-server", "--type=json", "-p", `[{"op":"replace","path":"/spec/template/spec/containers/0/env/1/value","value":"5"}]`).ExecOrDie("")
		if frameworks[0].ClusterIPFamily == "dual" {
			checkECMPCount(10)
		} else {
			checkECMPCount(5)
		}
		fnCheckPodHTTP()

		ginkgo.By("case 5: reduce ecmp path from 5 to 3")
		k8sframework.TestContext.KubeContext = "kind-kube-ovn"
		e2ekubectl.NewKubectlCommand("kube-system", "patch", "deployment", "ovn-ic-server", "--type=json", "-p", `[{"op":"replace","path":"/spec/template/spec/containers/0/env/1/value","value":"3"}]`).ExecOrDie("")
		if frameworks[0].ClusterIPFamily == "dual" {
			checkECMPCount(6)
		} else {
			checkECMPCount(3)
		}
		fnCheckPodHTTP()

		ginkgo.By("case 6: disable gateway kube-ovn1-worker gateway")
		k8sframework.TestContext.KubeContext = "kind-kube-ovn1"
		disableNetworkCmd := "docker exec kube-ovn1-worker iptables -I INPUT -p udp --dport 6081 -j DROP"
		_, err := exec.Command("bash", "-c", disableNetworkCmd).CombinedOutput()
		framework.ExpectNoError(err, "disable kube-ovn1-worker gateway failed")

		e2ekubectl.NewKubectlCommand("", "taint", "nodes", "kube-ovn1-worker", "e2e=test:NoSchedule").ExecOrDie("")
		fnCheckPodHTTP()

		ginkgo.By("case 7: disable gateway kube-ovn1-worker2 gateway")
		k8sframework.TestContext.KubeContext = "kind-kube-ovn1"
		disableNetworkCmd = "docker exec kube-ovn1-worker2 iptables -I INPUT -p udp --dport 6081 -j DROP"
		_, err = exec.Command("bash", "-c", disableNetworkCmd).CombinedOutput()
		framework.ExpectNoError(err, "disable kube-ovn1-worker2 gateway failed")

		e2ekubectl.NewKubectlCommand("", "taint", "nodes", "kube-ovn1-worker2", "e2e=test:NoSchedule").ExecOrDie("")
		fnCheckPodHTTP()

		ginkgo.By("case 8: enable gateway kube-ovn1-worker gateway")
		k8sframework.TestContext.KubeContext = "kind-kube-ovn1"
		disableNetworkCmd = "docker exec kube-ovn1-worker iptables -D INPUT -p udp --dport 6081 -j DROP"
		_, err = exec.Command("bash", "-c", disableNetworkCmd).CombinedOutput()
		framework.ExpectNoError(err, "enable kube-ovn1-worker gateway failed")

		e2ekubectl.NewKubectlCommand("", "taint", "nodes", "kube-ovn1-worker", "e2e=test:NoSchedule-").ExecOrDie("")
		fnCheckPodHTTP()

		ginkgo.By("case 9: enable gateway kube-ovn1-worker2 gateway")
		k8sframework.TestContext.KubeContext = "kind-kube-ovn1"
		disableNetworkCmd = "docker exec kube-ovn1-worker2 iptables -D INPUT -p udp --dport 6081 -j DROP"
		_, err = exec.Command("bash", "-c", disableNetworkCmd).CombinedOutput()
		framework.ExpectNoError(err, "enable kube-ovn1-worker2 gateway failed")

		e2ekubectl.NewKubectlCommand("", "taint", "nodes", "kube-ovn1-worker2", "e2e=test:NoSchedule-").ExecOrDie("")
		fnCheckPodHTTP()
	})
})

func checkECMPCount(expectCount int) {
	ginkgo.GinkgoHelper()

	ecmpCount := 0
	maxRetryTimes := 30

	for _, cluster := range clusters {
		clusterName := "kind-" + cluster
		ginkgo.By("Using kubectl context " + clusterName)
		k8sframework.TestContext.KubeContext = clusterName

		ginkgo.By("Checking logical router route count")
		for range maxRetryTimes {
			time.Sleep(3 * time.Second)
			cmd := "ovn-nbctl lr-route-list " + util.DefaultVpc
			output, _, err := framework.NBExec(cmd)
			framework.ExpectNoError(err)
			ecmpCount = strings.Count(string(output), "ecmp")
			if ecmpCount == expectCount {
				break
			}
		}
		framework.ExpectEqual(ecmpCount, expectCount)
	}

	k8sframework.TestContext.KubeContext = "kind-kube-ovn"
}
