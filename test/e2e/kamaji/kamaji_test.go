package kamaji

import (
	"context"
	"flag"
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

// isDataPlaneOnlyMode reports whether the cluster behind --kubeconfig was
// installed with kube-ovn's installMode=dataPlaneOnly. We detect via the
// kube-ovn-controller env vars: in dataPlaneOnly the chart wires
// `OVN_DB_IPS` to externalOvnCentral.endpoint, which is non-empty.
func isDataPlaneOnlyMode(f *framework.Framework) bool {
	ginkgo.GinkgoHelper()

	deploy, err := f.ClientSet.AppsV1().Deployments(framework.KubeOvnNamespace).
		Get(context.TODO(), "kube-ovn-controller", metav1.GetOptions{})
	if err != nil {
		return false
	}
	for _, c := range deploy.Spec.Template.Spec.Containers {
		if c.Name != "kube-ovn-controller" {
			continue
		}
		for _, env := range c.Env {
			if env.Name == "OVN_DB_IPS" && env.Value != "" {
				return true
			}
		}
	}
	return false
}

// externalOvnVIP returns the configured ovn-central VIP the data plane was
// pointed at. Read from KUBE_OVN_KAMAJI_MGMT_VIP (set by hack/kamaji-e2e.sh)
// when present, otherwise from kube-ovn-controller's OVN_DB_IPS env.
func externalOvnVIP(f *framework.Framework) string {
	if v := os.Getenv("KUBE_OVN_KAMAJI_MGMT_VIP"); v != "" {
		return v
	}
	deploy, err := f.ClientSet.AppsV1().Deployments(framework.KubeOvnNamespace).
		Get(context.TODO(), "kube-ovn-controller", metav1.GetOptions{})
	framework.ExpectNoError(err)
	for _, c := range deploy.Spec.Template.Spec.Containers {
		if c.Name != "kube-ovn-controller" {
			continue
		}
		for _, env := range c.Env {
			if env.Name == "OVN_DB_IPS" {
				return strings.Split(env.Value, ",")[0]
			}
		}
	}
	return ""
}

var _ = framework.Describe("[group:kamaji]", func() {
	f := framework.NewDefaultFramework("kamaji")

	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 18, "kube-ovn Kamaji split-cluster mode was introduced in v1.18")
		if !isDataPlaneOnlyMode(f) {
			ginkgo.Skip("kube-ovn is not installed in dataPlaneOnly mode; skipping the Kamaji suite")
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

	ginkgo.It("data-plane components dial the external ovn-central endpoint", func() {
		vip := externalOvnVIP(f)
		gomega.Expect(vip).NotTo(gomega.BeEmpty(),
			"could not determine external ovn-central VIP from chart or env")
		framework.Logf("expecting ESTAB connections to %s:6642 from the tenant cluster", vip)

		pods, err := f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).List(context.TODO(),
			metav1.ListOptions{LabelSelector: "app=ovs"})
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(pods.Items, "no ovs-ovn pod found")

		pod := pods.Items[0]
		stdout, stderr, err := framework.ExecShellInPod(context.TODO(), f, pod.Namespace, pod.Name,
			"ss -tn state established '( dport = :6642 )'")
		framework.ExpectNoError(err, "exec ss in ovs-ovn pod: stderr=%s", stderr)
		gomega.Expect(stdout).To(gomega.ContainSubstring(vip),
			"expected an ESTAB connection from %s/%s to %s:6642; got:\n%s",
			pod.Namespace, pod.Name, vip, stdout)
	})

	ginkgo.It("tenant pods receive an IP from the OVN subnet via the external control plane", func() {
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
