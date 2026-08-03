package kamaji

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

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
	framework.ExpectNoError(err, "get kube-ovn-controller deployment")
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
	framework.Fail("kube-ovn-controller container not found in kube-ovn-controller deployment")
	return nil
}

// usesHostedOVNCentralAddresses reports whether the tenant cluster workloads
// use explicit hosted OVN DB addresses instead of the in-cluster OVN_DB_IPS.
func usesHostedOVNCentralAddresses(f *framework.Framework) bool {
	env := controllerEnv(f)
	return env["OVN_NB_ADDR"] != "" && env["OVN_SB_ADDR"] != "" && env["OVN_DB_IPS"] == ""
}

func hcpOVNNBAddress() string {
	return requiredHostedOVNAddress("KUBE_OVN_HCP_OVN_NB_ADDR")
}

func hcpOVNSBAddress() string {
	return requiredHostedOVNAddress("KUBE_OVN_HCP_OVN_SB_ADDR")
}

func requiredHostedOVNAddress(name string) string {
	ginkgo.GinkgoHelper()

	value := os.Getenv(name)
	gomega.Expect(value).NotTo(gomega.BeEmpty(),
		"%s must be set by the hosted OVN central E2E harness", name)
	return value
}

func hostAndPort(addr string) (string, string) {
	ginkgo.GinkgoHelper()

	host, port, err := net.SplitHostPort(strings.TrimPrefix(addr, "tcp:"))
	framework.ExpectNoError(err)
	return strings.Trim(host, "[]"), port
}

func requiredPodAddressFamilies(ipFamily string) (bool, bool, error) {
	switch ipFamily {
	case "", "ipv4":
		return true, false, nil
	case "ipv6":
		return false, true, nil
	case "dual":
		return true, true, nil
	default:
		return false, false, fmt.Errorf("unsupported E2E_IP_FAMILY %q", ipFamily)
	}
}

func podAddressFamilies(pod *corev1.Pod) (bool, bool) {
	ips := append([]string{}, util.PodIPs(*pod)...)
	if annotation := pod.Annotations[util.IPAddressAnnotation]; annotation != "" {
		ips = append(ips, strings.Split(annotation, ",")...)
	}

	var hasIPv4, hasIPv6 bool
	for _, ipText := range ips {
		ip := net.ParseIP(strings.TrimSpace(ipText))
		if ip == nil {
			continue
		}
		if ip.To4() != nil {
			hasIPv4 = true
		} else {
			hasIPv6 = true
		}
	}
	return hasIPv4, hasIPv6
}

var _ = framework.Describe("[group:kamaji]", func() {
	f := framework.NewDefaultFramework("kamaji")

	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 18, "kube-ovn hosted OVN central support is required")
		hcpOVNNBAddress()
		hcpOVNSBAddress()
		gomega.Expect(usesHostedOVNCentralAddresses(f)).To(gomega.BeTrue(),
			"kube-ovn data-plane workloads must use hosted OVN DB addresses; the Kamaji-backed suite must fail rather than skip when hosted OVN central is not under test")
	})

	ginkgo.It("kube-ovn-controller runs with replicas=1 in hosted OVN central dataPlaneOnly", func() {
		deploy, err := f.ClientSet.AppsV1().Deployments(framework.KubeOvnNamespace).
			Get(context.TODO(), "kube-ovn-controller", metav1.GetOptions{})
		framework.ExpectNoError(err)
		framework.ExpectNotNil(deploy.Spec.Replicas)
		gomega.Expect(*deploy.Spec.Replicas).To(gomega.BeEquivalentTo(1),
			"hosted OVN central dataPlaneOnly should default kube-ovn-controller to 1 replica via kubeovn.controllerReplicas")
	})

	ginkgo.It("data-plane components use the hosted OVN DB addresses", func() {
		env := controllerEnv(f)
		nbAddr := hcpOVNNBAddress()
		sbAddr := hcpOVNSBAddress()

		gomega.Expect(env["OVN_NB_ADDR"]).To(gomega.Equal(nbAddr))
		gomega.Expect(env["OVN_SB_ADDR"]).To(gomega.Equal(sbAddr))
		gomega.Expect(env).NotTo(gomega.HaveKey("OVN_DB_IPS"))
	})

	ginkgo.It("data-plane components dial the hosted ovn-central endpoint", func() {
		sbAddr := hcpOVNSBAddress()
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
		pod = podClient.CreateSync(pod)
		ginkgo.DeferCleanup(func() { podClient.DeleteSync(name) })

		framework.ExpectNotNil(pod, "pod should exist after CreateSync")
		gomega.Expect(pod.Annotations[util.AllocatedAnnotation]).To(gomega.Equal("true"),
			"pod should be annotated by kube-ovn-controller via the external OVN DB")
		gomega.Expect(pod.Annotations[util.IPAddressAnnotation]).NotTo(gomega.BeEmpty(),
			"pod should carry an ovn.kubernetes.io/ip_address annotation")
		gomega.Expect(pod.Status.PodIP).NotTo(gomega.BeEmpty(),
			"pod IP should be set by kubelet from the kube-ovn CNI")
		wantIPv4, wantIPv6, err := requiredPodAddressFamilies(os.Getenv("E2E_IP_FAMILY"))
		framework.ExpectNoError(err)
		hasIPv4, hasIPv6 := podAddressFamilies(pod)
		if wantIPv4 {
			gomega.Expect(hasIPv4).To(gomega.BeTrue(),
				"pod should have an IPv4 address when E2E_IP_FAMILY=%q; status PodIPs=%v annotation=%q",
				os.Getenv("E2E_IP_FAMILY"), pod.Status.PodIPs, pod.Annotations[util.IPAddressAnnotation])
		}
		if wantIPv6 {
			gomega.Expect(hasIPv6).To(gomega.BeTrue(),
				"pod should have an IPv6 address when E2E_IP_FAMILY=%q; status PodIPs=%v annotation=%q",
				os.Getenv("E2E_IP_FAMILY"), pod.Status.PodIPs, pod.Annotations[util.IPAddressAnnotation])
		}
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
