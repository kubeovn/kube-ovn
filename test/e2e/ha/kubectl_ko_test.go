package ha

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	e2ekubectl "k8s.io/kubernetes/test/e2e/framework/kubectl"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

var kubectlKoHABinary string

func prepareKubectlKoHA(f *framework.Framework) {
	ginkgo.GinkgoHelper()
	directory, err := os.MkdirTemp("", "kubectl-ko-ha-")
	framework.ExpectNoError(err)
	ginkgo.DeferCleanup(func() {
		kubectlKoHABinary = ""
		framework.ExpectNoError(os.RemoveAll(directory))
	})
	pods, err := f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=kube-ovn-cni"})
	framework.ExpectNoError(err)
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning || pod.DeletionTimestamp != nil {
			continue
		}
		kubectlKoHABinary = filepath.Join(directory, "kubectl-ko")
		e2ekubectl.NewKubectlCommand("", "cp", "-n", pod.Namespace, "-c", "cni-server", pod.Name+":/kube-ovn/kubectl-ko", kubectlKoHABinary).ExecOrDie("")
		framework.ExpectNoError(os.Chmod(kubectlKoHABinary, 0o700))
		return
	}
	framework.Failf("no running CNI pod supplies the matching kubectl-ko binary")
}

func runKubectlKo(args ...string) string {
	ginkgo.GinkgoHelper()
	framework.ExpectNotEmpty(kubectlKoHABinary)
	argv := []string{"--timeout", "15m", "--kube-ovn-namespace", framework.KubeOvnNamespace}
	if config := k8sframework.TestContext.KubeConfig; config != "" {
		argv = append(argv, "--kubeconfig", config)
	}
	if value := k8sframework.TestContext.KubeContext; value != "" {
		argv = append(argv, "--context", value)
	}
	if value := k8sframework.TestContext.Host; value != "" {
		argv = append(argv, "--server", value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, kubectlKoHABinary, append(argv, args...)...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	framework.ExpectNoError(err, "kubectl-ko %v: stdout=%s stderr=%s", args, output, stderr.String())
	if stderr.Len() != 0 {
		framework.Logf("kubectl-ko %v stderr: %s", args, stderr.String())
	}
	return string(output)
}

func kubectlKoNBMarker() func() {
	ginkgo.GinkgoHelper()
	key, value := "ko-e2e-"+framework.RandomSuffix(), framework.RandomSuffix()
	runKubectlKo("exec", "nbctl", "--", "set", "NB_Global", ".", "external_ids:"+key+"="+value)
	ginkgo.DeferCleanup(func() {
		runKubectlKo("exec", "nbctl", "--", "remove", "NB_Global", ".", "external_ids", key)
	})
	return func() {
		output := runKubectlKo("exec", "nbctl", "--", "get", "NB_Global", ".", "external_ids:"+key)
		framework.ExpectEqual(strings.Trim(strings.TrimSpace(output), "\""), value, "NB data must survive the operation")
	}
}

func registerKubectlKoHA(f *framework.Framework, nodes map[string]*kind.Node) {
	// These specs inherit the enclosing serial suite's Kind-only guard.
	for _, operation := range [][]string{{"restart"}, {"perf", "recovery", "--yes"}} {
		framework.DisruptiveIt("kubectl ko should preserve data and connectivity after "+strings.Join(operation, " "), func() {
			f.SkipVersionPriorTo(1, 17, "The structured Go plugin was introduced in v1.17")
			prepareKubectlKoHA(f)
			verify := kubectlKoNBMarker()
			runKubectlKo("diagnose", "cluster")
			output := runKubectlKo(operation...)
			if operation[0] == "perf" {
				for _, role := range []string{"nb", "sb", "northd"} {
					framework.ExpectContainSubstring(output, role+" leader recovered in ")
				}
			}
			runKubectlKo("db", "health")
			verify()
			runKubectlKo("diagnose", "cluster")
		})
	}

	framework.DisruptiveIt("kubectl ko should rebuild databases and preserve NB data and connectivity", func() {
		f.SkipVersionPriorTo(1, 17, "The staged Go recovery was introduced in v1.17")
		prepareKubectlKoHA(f)
		deployment := f.DeploymentClientNS(framework.KubeOvnNamespace).Get("ovn-central")
		source := kubectlKoBootstrapNode(f, deployment)
		verify := kubectlKoNBMarker()
		runKubectlKo("diagnose", "cluster")
		plan := runKubectlKo("db", "nb", "restore", "--source-node", source, "--dry-run")
		var planned struct {
			SourceNode string `json:"sourceNode"`
			Stage      string `json:"stage"`
		}
		framework.ExpectNoError(json.Unmarshal([]byte(plan), &planned))
		framework.ExpectEqual(planned.SourceNode, source)
		framework.ExpectEqual(planned.Stage, "planned")
		current := f.DeploymentClientNS(framework.KubeOvnNamespace).Get("ovn-central")
		framework.ExpectEqual(current.Generation, deployment.Generation, "dry-run must not scale central")
		verify()

		output := runKubectlKo("db", "nb", "restore", "--source-node", source, "--yes")
		kubectlKoCheckRecoveryRecord(f, output, source, *deployment.Spec.Replicas)
		current = f.DeploymentClientNS(framework.KubeOvnNamespace).Get("ovn-central")
		framework.ExpectEqual(*current.Spec.Replicas, *deployment.Spec.Replicas)
		runKubectlKo("db", "health")
		verify()
		runKubectlKo("diagnose", "cluster")
	})

	framework.DisruptiveIt("kubectl ko should repeat traffic measurements without leaking state or disrupting leaders", func() {
		f.SkipVersionPriorTo(1, 17, "The isolated Go performance command was introduced in v1.17")
		prepareKubectlKoHA(f)
		deployment := f.DeploymentClientNS(framework.KubeOvnNamespace).Get("ovn-central")
		central, err := f.DeploymentClientNS(framework.KubeOvnNamespace).GetPods(deployment)
		framework.ExpectNoError(err)
		original := make(map[string]types.UID)
		for _, pod := range central.Items {
			original[pod.Name] = pod.UID
		}
		resources := kubectlKoProbeUIDs(f)
		lbs := kubectlKoPerformanceLBs()
		memberships := kubectlKoHostMemberships(nodes)
		for range 2 {
			output := runKubectlKo("perf", "run", "--duration", "1s", "--bandwidth", "10M")
			for _, network := range []string{"Pod", "Host", "Service"} {
				framework.ExpectContainSubstring(output, "=== "+network+" network ===")
			}
			framework.WaitUntil(time.Second, 2*time.Minute, func(context.Context) (bool, error) {
				for uid := range kubectlKoProbeUIDs(f) {
					if !resources[uid] {
						return false, nil
					}
				}
				return true, nil
			}, "new performance probe resources to disappear")
			framework.ExpectEqual(kubectlKoPerformanceLBs(), lbs)
			framework.ExpectEqual(kubectlKoHostMemberships(nodes), memberships)
		}
		central, err = f.DeploymentClientNS(framework.KubeOvnNamespace).GetPods(deployment)
		framework.ExpectNoError(err)
		current := make(map[string]types.UID)
		for _, pod := range central.Items {
			current[pod.Name] = pod.UID
		}
		framework.ExpectEqual(current, original, "traffic measurements must not delete central leaders")
		runKubectlKo("db", "health")
	})
}

func kubectlKoBootstrapNode(f *framework.Framework, deployment *appsv1.Deployment) string {
	ginkgo.GinkgoHelper()
	var address string
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name != "ovn-central" {
			continue
		}
		for _, env := range container.Env {
			if env.Name == "NODE_IPS" {
				address, _, _ = strings.Cut(env.Value, ",")
			}
		}
	}
	framework.ExpectNotEmpty(address)
	nodes, err := f.ClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	framework.ExpectNoError(err)
	for _, node := range nodes.Items {
		for _, ip := range node.Status.Addresses {
			if ip.Type == corev1.NodeInternalIP && ip.Address == strings.TrimSpace(address) {
				return node.Name
			}
		}
	}
	framework.Failf("cannot find bootstrap node for %s", address)
	return ""
}

func kubectlKoCheckRecoveryRecord(f *framework.Framework, output, source string, replicas int32) {
	ginkgo.GinkgoHelper()
	_, filename, ok := strings.Cut(output, "local record ")
	filename = strings.TrimSpace(filename)
	framework.ExpectEqual(ok, true)
	framework.ExpectEqual(filepath.Base(filename), filename)
	framework.ExpectEqual(strings.HasPrefix(filename, "kubectl-ko-recovery-"), true)
	ginkgo.DeferCleanup(func() { framework.ExpectNoError(os.Remove(filename)) })
	data, err := os.ReadFile(filename)
	framework.ExpectNoError(err)
	var record struct {
		SourceNode string                  `json:"sourceNode"`
		Stage      string                  `json:"stage"`
		Directory  string                  `json:"remoteBackupDirectory"`
		Replicas   int32                   `json:"replicas"`
		Targets    []struct{ Node string } `json:"targets"`
	}
	framework.ExpectNoError(json.Unmarshal(data, &record))
	framework.ExpectEqual(record.Stage, "completed")
	framework.ExpectEqual(record.SourceNode, source)
	framework.ExpectEqual(record.Replicas, replicas)
	framework.ExpectHaveLen(record.Targets, int(replicas))
	framework.ExpectEqual(strings.HasPrefix(record.Directory, "/etc/ovn/.kubectl-ko-recovery-"), true)
	pods, err := f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=ovn-central"})
	framework.ExpectNoError(err)
	for _, target := range record.Targets {
		found := false
		for _, pod := range pods.Items {
			if pod.Spec.NodeName != target.Node || pod.DeletionTimestamp != nil {
				continue
			}
			found = true
			for _, db := range []string{"nb", "sb"} {
				path := record.Directory + "/ovn" + db + "_db.original"
				e2ekubectl.NewKubectlCommand("", "exec", "-n", pod.Namespace, pod.Name, "-c", "ovn-central", "--", "test", "-s", path).ExecOrDie("")
				name := "OVN_Northbound"
				if db == "sb" {
					name = "OVN_Southbound"
				}
				output := e2ekubectl.NewKubectlCommand("", "exec", "-n", pod.Namespace, pod.Name, "-c", "ovn-central", "--", "ovsdb-tool", "db-name", path).ExecOrDie("")
				framework.ExpectEqual(strings.TrimSpace(output), name, "retained database must remain readable on node %s", target.Node)
			}
		}
		framework.ExpectEqual(found, true, "missing central pod on recovery node %s", target.Node)
	}
}

func kubectlKoProbeUIDs(f *framework.Framework) map[types.UID]bool {
	ginkgo.GinkgoHelper()
	options := metav1.ListOptions{LabelSelector: "app=kubectl-ko-probe"}
	pods, err := f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).List(context.Background(), options)
	framework.ExpectNoError(err)
	services, err := f.ClientSet.CoreV1().Services(framework.KubeOvnNamespace).List(context.Background(), options)
	framework.ExpectNoError(err)
	result := make(map[types.UID]bool)
	for _, pod := range pods.Items {
		result[pod.UID] = true
	}
	for _, service := range services.Items {
		result[service.UID] = true
	}
	return result
}

func kubectlKoPerformanceLBs() []string {
	ginkgo.GinkgoHelper()
	output := runKubectlKo("exec", "nbctl", "--", "--format=json", "--columns=name", "list", "Load_Balancer")
	var rows struct {
		Data [][]string `json:"data"`
	}
	framework.ExpectNoError(json.Unmarshal([]byte(output), &rows))
	var names []string
	for _, row := range rows.Data {
		framework.ExpectHaveLen(row, 1)
		if strings.HasPrefix(row[0], "ko-perf-") {
			names = append(names, row[0])
		}
	}
	slices.Sort(names)
	return names
}

func kubectlKoHostMemberships(nodes map[string]*kind.Node) map[string]int {
	ginkgo.GinkgoHelper()
	result := make(map[string]int)
	for name, node := range nodes {
		output, stderr, err := node.Exec("ip", "maddr", "show")
		framework.ExpectNoError(err, fmt.Sprintf("reading multicast memberships on %s: %s", name, stderr))
		result[name] = strings.Count(string(output), "01:00:5e:00:00:64")
	}
	return result
}
