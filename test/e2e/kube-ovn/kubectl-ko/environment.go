package kubectl_ko

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	e2ekubectl "k8s.io/kubernetes/test/e2e/framework/kubectl"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func checkEnvironmentOutput(output string, nodes []corev1.Node) {
	ginkgo.GinkgoHelper()
	for _, node := range nodes {
		framework.ExpectContainSubstring(output, "Environment check on "+node.Name+"\n")
	}
	framework.ExpectEqual(strings.Count(output, "Environment check on "), len(nodes))
	for _, step := range []string{"check cni configuration", "check system ipv4 config", "check checksum value", "check dns config", "check firewall config", "check geneve 6081 connection"} {
		framework.ExpectEqual(strings.Count(output, step), len(nodes), "every Linux node must run the complete image checker")
	}
}

func checkAgentOnlyEnvironment(f *framework.Framework, nodes []corev1.Node) {
	ginkgo.GinkgoHelper()
	ginkgo.By("Running independent agents in a namespace without CNI Pods")
	source := f.DaemonSetClientNS(framework.KubeOvnNamespace).Get("kubectl-ko-node-agent")
	framework.ExpectEqual(source.Spec.Template.Spec.ServiceAccountName, "")
	framework.ExpectNotNil(source.Spec.Template.Spec.AutomountServiceAccountToken)
	framework.ExpectFalse(*source.Spec.Template.Spec.AutomountServiceAccountToken)
	client := f.DaemonSetClient()
	agents, err := client.Create(context.Background(), &appsv1.DaemonSet{
		Name: "ko-agent-only", Namespace: f.Namespace.Name, Spec: *source.Spec.DeepCopy(),
	}, metav1.CreateOptions{})
	framework.ExpectNoError(err)
	ginkgo.DeferCleanup(func() {
		framework.ExpectNoError(client.Delete(context.Background(), agents.Name, metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{UID: new(agents.UID)}, PropagationPolicy: new(metav1.DeletePropagationForeground),
		}))
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(ctx context.Context) (bool, error) {
			_, err := client.DaemonSetInterface.Get(ctx, agents.Name, metav1.GetOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				return false, err
			}
			pods, podErr := client.GetPods(agents)
			if podErr != nil {
				return false, podErr
			}
			return k8serrors.IsNotFound(err) && len(pods.Items) == 0, nil
		}, "agent-only DaemonSet and its owned Pods must disappear")
	})
	client.RolloutStatus(agents.Name)
	pods, err := f.ClientSet.CoreV1().Pods(f.Namespace.Name).List(context.Background(), metav1.ListOptions{LabelSelector: "app=kube-ovn-cni"})
	framework.ExpectNoError(err)
	framework.ExpectEmpty(pods.Items)
	output := e2ekubectl.NewKubectlCommand("", "ko", "--kube-ovn-namespace", f.Namespace.Name, "diagnose", "environment").ExecOrDie("")
	checkEnvironmentOutput(output, nodes)
	checkAgentOnlyLogs(f.Namespace.Name, nodes)
}

func checkAgentOnlyLogs(namespace string, nodes []corev1.Node) {
	ginkgo.GinkgoHelper()
	directory, err := os.MkdirTemp("", "ko-agent-only-logs-*")
	framework.ExpectNoError(err)
	ginkgo.DeferCleanup(func() { framework.ExpectNoError(os.RemoveAll(directory)) })
	e2ekubectl.NewKubectlCommand("", "ko", "--kube-ovn-namespace", namespace, "logs", "--component", "linux", "--output-dir", directory).ExecOrDie("")
	data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	framework.ExpectNoError(err)
	var manifest struct {
		DiscoveryError string `json:"discoveryError"`
		Items          []struct {
			Target struct {
				Node string `json:"Node"`
			} `json:"target"`
			Name  string `json:"name"`
			Error string `json:"error"`
		} `json:"items"`
	}
	framework.ExpectNoError(json.Unmarshal(data, &manifest))
	framework.ExpectEmpty(manifest.DiscoveryError)
	for _, node := range nodes {
		for _, name := range []string{"link", "addr", "ipsec"} {
			matched := 0
			for _, item := range manifest.Items {
				if item.Target.Node != node.Name || item.Name != name {
					continue
				}
				matched++
				if name == "ipsec" {
					framework.ExpectContainSubstring(item.Error, "IPsec source")
				} else {
					framework.ExpectEmpty(item.Error)
					data, err := os.ReadFile(filepath.Join(directory, node.Name, "linux", name+".log"))
					framework.ExpectNoError(err)
					framework.ExpectContainSubstring(string(data), `"-s"`)
					framework.ExpectContainSubstring(string(data), "lo:")
				}
			}
			framework.ExpectEqual(matched, 1, "each Linux node must retain %s", name)
		}
	}
}
