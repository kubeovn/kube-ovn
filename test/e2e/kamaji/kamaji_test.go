package kamaji

import (
	"context"
	"flag"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
	e2e.RunE2ETests(t)
}

// controllerEnv returns the kube-ovn-controller environment variables from the
// tenant cluster behind --kubeconfig.
func controllerEnv(f *framework.Framework) map[string]string {
	ginkgo.GinkgoHelper()

	deploy, err := f.ClientSet.AppsV1().Deployments(framework.KubeOvnNamespace).
		Get(context.TODO(), "kube-ovn-controller", metav1.GetOptions{})
	if err != nil {
		return nil
	}
	for _, c := range deploy.Spec.Template.Spec.Containers {
		if c.Name != "kube-ovn-controller" {
			continue
		}
		values := make(map[string]string, len(c.Env))
		for _, env := range c.Env {
			values[env.Name] = env.Value
		}
		return values
	}
	return nil
}

// usesHostedOVNCentralAddresses reports whether the tenant cluster workloads
// use explicit hosted OVN DB addresses instead of the in-cluster OVN_DB_IPS.
func usesHostedOVNCentralAddresses(f *framework.Framework) bool {
	env := controllerEnv(f)
	return env["OVN_NB_ADDR"] != "" && env["OVN_SB_ADDR"] != "" && env["OVN_DB_IPS"] == ""
}

func hcpOVNNBAddress(f *framework.Framework) string {
	if v := os.Getenv("KUBE_OVN_HCP_OVN_NB_ADDR"); v != "" {
		return v
	}
	return controllerEnv(f)["OVN_NB_ADDR"]
}

func hcpOVNSBAddress(f *framework.Framework) string {
	if v := os.Getenv("KUBE_OVN_HCP_OVN_SB_ADDR"); v != "" {
		return v
	}
	return controllerEnv(f)["OVN_SB_ADDR"]
}

func hostAndPort(addr string) (string, string) {
	ginkgo.GinkgoHelper()

	host, port, err := net.SplitHostPort(strings.TrimPrefix(addr, "tcp:"))
	framework.ExpectNoError(err)
	return strings.Trim(host, "[]"), port
}

var _ = framework.Describe("[group:kamaji]", func() {
	f := framework.NewDefaultFramework("kamaji")

	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 18, "kube-ovn hosted OVN central support is required")
		if !usesHostedOVNCentralAddresses(f) {
			ginkgo.Skip("kube-ovn data-plane workloads are not using hosted OVN DB addresses; skipping the Kamaji-backed suite")
		}
	})

	ginkgo.It("kube-ovn-controller runs with replicas=1 in dataPlaneOnly", func() {
		deploy, err := f.ClientSet.AppsV1().Deployments(framework.KubeOvnNamespace).
			Get(context.TODO(), "kube-ovn-controller", metav1.GetOptions{})
		framework.ExpectNoError(err)
		framework.ExpectNotNil(deploy.Spec.Replicas)
		gomega.Expect(*deploy.Spec.Replicas).To(gomega.BeEquivalentTo(1),
			"dataPlaneOnly should default kube-ovn-controller to 1 replica via kubeovn.controllerReplicas")
	})

	ginkgo.It("data-plane components use the hosted OVN DB addresses", func() {
		env := controllerEnv(f)
		nbAddr := hcpOVNNBAddress(f)
		sbAddr := hcpOVNSBAddress(f)

		gomega.Expect(nbAddr).NotTo(gomega.BeEmpty(),
			"could not determine HCP OVN NB address from chart or env")
		gomega.Expect(sbAddr).NotTo(gomega.BeEmpty(),
			"could not determine HCP OVN SB address from chart or env")
		gomega.Expect(env["OVN_NB_ADDR"]).To(gomega.Equal(nbAddr))
		gomega.Expect(env["OVN_SB_ADDR"]).To(gomega.Equal(sbAddr))
		gomega.Expect(env).NotTo(gomega.HaveKey("OVN_DB_IPS"))
	})

	ginkgo.It("data-plane components dial the hosted ovn-central endpoint", func() {
		sbAddr := hcpOVNSBAddress(f)
		host, port := hostAndPort(sbAddr)
		framework.Logf("expecting ESTAB connections to %s from the tenant cluster", sbAddr)

		pods, err := f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).List(context.TODO(),
			metav1.ListOptions{LabelSelector: "app=ovs"})
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(pods.Items, "no ovs-ovn pod found")

		pod := pods.Items[0]
		stdout, stderr, err := framework.ExecShellInPod(context.TODO(), f, pod.Namespace, pod.Name,
			"ss -tn state established '( dport = :"+port+" )'")
		framework.ExpectNoError(err, "exec ss in ovs-ovn pod: stderr=%s", stderr)
		gomega.Expect(stdout).To(gomega.ContainSubstring(host),
			"expected an ESTAB connection from %s/%s to %s; got:\n%s",
			pod.Namespace, pod.Name, sbAddr, stdout)
	})

	ginkgo.It("tenant pods receive an IP from the OVN subnet through the hosted ovn-central path", func() {
		podClient := f.PodClient()
		name := "kamaji-smoke-" + framework.RandomSuffix()

		pod := framework.MakePod(f.Namespace.Name, name, nil, nil,
			framework.AgnhostImage, nil, nil)
		_ = podClient.CreateSync(pod)
		ginkgo.DeferCleanup(func() { podClient.DeleteSync(name) })

		pod = podClient.GetPod(name)
		framework.ExpectNotNil(pod, "pod should exist after CreateSync")
		gomega.Expect(pod.Annotations[util.AllocatedAnnotation]).To(gomega.Equal("true"),
			"pod should be annotated by kube-ovn-controller via the external OVN DB")
		gomega.Expect(pod.Annotations[util.IPAddressAnnotation]).NotTo(gomega.BeEmpty(),
			"pod should carry an ovn.kubernetes.io/ip_address annotation")
		gomega.Expect(pod.Status.PodIP).NotTo(gomega.BeEmpty(),
			"pod IP should be set by kubelet from the kube-ovn CNI")
		framework.Logf("tenant pod %s/%s allocated %s via OVN (logical_switch=%s)",
			pod.Namespace, pod.Name, pod.Status.PodIP,
			pod.Annotations[util.LogicalSwitchAnnotation])
	})

	ginkgo.It("kube-ovn-controller leader-election Lease lives in the tenant apiserver", func() {
		_, err := f.ClientSet.CoordinationV1().Leases(framework.KubeOvnNamespace).
			Get(context.TODO(), "kube-ovn-controller", metav1.GetOptions{})
		if err != nil {
			leases, listErr := f.ClientSet.CoordinationV1().Leases(framework.KubeOvnNamespace).
				List(context.TODO(), metav1.ListOptions{})
			framework.ExpectNoError(listErr)
			gomega.Expect(leases.Items).NotTo(gomega.BeEmpty(),
				"expected at least one Lease in %s (kube-ovn-controller leader election)",
				framework.KubeOvnNamespace)
		}
	})
})
