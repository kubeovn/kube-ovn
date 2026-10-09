package kubectl_ko

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	e2ekubectl "k8s.io/kubernetes/test/e2e/framework/kubectl"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func checkIndependentIPsecCollection(f *framework.Framework, pods *framework.PodClient, namespace, name string) {
	ginkgo.GinkgoHelper()
	ginkgo.By("Querying a real isolated charon daemon through the independent agent")
	agents, err := f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=kubectl-ko-node-agent"})
	framework.ExpectNoError(err)
	framework.ExpectNotEmpty(agents.Items)
	agent := agents.Items[0]
	framework.ExpectNotEmpty(agent.Spec.Containers)
	// No tunnels, certificates or host directories are configured. The fixture
	// has its own mount/network namespaces and never changes the running CNI.
	command := "printf '# ko-ipsec-fixture\\n' > /etc/ipsec.conf; exec /usr/lib/ipsec/charon"
	pod := framework.MakePod(namespace, name, nil, nil, agent.Spec.Containers[0].Image, []string{"sh", "-ec", command}, nil)
	pod.Spec.NodeName = agent.Spec.NodeName
	pod.Spec.AutomountServiceAccountToken = new(false)
	pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"NET_ADMIN"}}}
	pod.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
		Exec:          &corev1.ExecAction{Command: []string{"sh", "-c", "test -S /run/charon.ctl"}},
		PeriodSeconds: 1,
	}
	pod = pods.CreateSync(pod)
	args := []string{"exec", "-n", agent.Namespace, agent.Name, "-c", "agent", "--", "/kube-ovn/kubectl-ko-node-agent", "ipsec", string(pod.UID)}
	output := e2ekubectl.NewKubectlCommand("", args...).ExecOrDie("")
	for _, expected := range []string{"# ko-ipsec-fixture", "=== IPsec listcacerts ===", "=== IPsec listcerts ===", "Status of IKE charon daemon"} {
		framework.ExpectContainSubstring(output, expected)
	}
	pods.DeleteGracefully(name)
	pods.WaitForNotFound(name)
	// Once the source Pod has exited, the helper must not fall back to the
	// agent's own socket or to another Pod's charon instance.
	framework.WaitUntil(time.Second, 30*time.Second, func(context.Context) (bool, error) {
		_, err := e2ekubectl.NewKubectlCommand("", args...).Exec()
		return err != nil, nil
	}, "the deleted Pod's charon process to disappear")
}
