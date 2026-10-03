package cnp_domain

import (
	"context"
	"flag"
	"fmt"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	netpolv1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.SerialDescribe("[group:cluster-network-policy]", func() {
	f := framework.NewDefaultFramework("cluster-network-policy")
	var namespaceName, podName string
	var policyNames []string
	var podClient *framework.PodClient
	var cnpClient *framework.CnpClient

	ginkgo.BeforeEach(func() {
		policyNames = nil
		podClient = nil
		f.SkipVersionPriorTo(1, 17, "ClusterNetworkPolicy protocols requires the v0.2.0 controller")
		namespaceName = "ns-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		cnpClient = f.CnpClient()
		f.NamespaceClient().Create(framework.MakeNamespace(namespaceName, map[string]string{"kubernetes.io/metadata.name": namespaceName}, nil))
		podClient = f.PodClientNS(namespaceName)
		podClient.CreateSync(framework.MakePrivilegedPod(namespaceName, podName, nil, nil, f.KubeOVNImage, []string{"sleep", "infinity"}, nil))
	})

	ginkgo.AfterEach(func() {
		for _, name := range policyNames {
			err := cnpClient.Delete(context.Background(), name, metav1.DeleteOptions{})
			if !apierrors.IsNotFound(err) {
				framework.ExpectNoError(err)
			}
		}
		if podClient != nil {
			podClient.DeleteSync(podName)
			f.NamespaceClient().Delete(namespaceName)
		}
	})

	connect := func(target string, allowed bool) {
		ginkgo.By(fmt.Sprintf("Checking HTTPS connectivity to %s, allowed=%t", target, allowed))
		framework.WaitUntil(time.Second, 90*time.Second, func(ctx context.Context) (bool, error) {
			// Each attempt opens a new connection, avoiding a retained conntrack decision.
			_, _, err := framework.ExecShellInPod(ctx, f, namespaceName, podName, "curl -ksS --noproxy '*' --connect-timeout 3 --max-time 5 "+target+" >/dev/null")
			return (err == nil) == allowed, nil
		}, fmt.Sprintf("HTTPS connectivity to %s, allowed=%t", target, allowed))
	}
	protocols := []netpolv1alpha2.ClusterNetworkPolicyProtocol{framework.MakeClusterNetworkPolicyPort(443, corev1.ProtocolTCP)}
	allowDomain := func(name, domain string) netpolv1alpha2.ClusterNetworkPolicyEgressRule {
		// DomainNames is valid only with Accept: denial uses a lower catch-all rule.
		return framework.MakeClusterNetworkPolicyEgressRule(name, netpolv1alpha2.ClusterNetworkPolicyRuleActionAccept, protocols, []netpolv1alpha2.DomainName{netpolv1alpha2.DomainName(domain)})
	}
	denyHTTPS := func() netpolv1alpha2.ClusterNetworkPolicyEgressRule {
		return netpolv1alpha2.ClusterNetworkPolicyEgressRule{
			Name: "deny-other-https", Action: netpolv1alpha2.ClusterNetworkPolicyRuleActionDeny, Protocols: protocols,
			To: []netpolv1alpha2.ClusterNetworkPolicyEgressPeer{{Networks: []netpolv1alpha2.CIDR{"0.0.0.0/0", "::/0"}}},
		}
	}
	create := func(priority int32, rules ...netpolv1alpha2.ClusterNetworkPolicyEgressRule) *netpolv1alpha2.ClusterNetworkPolicy {
		name := "cnp-" + framework.RandomSuffix()
		policyNames = append(policyNames, name)
		policy := framework.MakeClusterNetworkPolicy(name, priority, &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespaceName}}, rules, nil)
		created, err := cnpClient.Create(context.Background(), policy, metav1.CreateOptions{})
		framework.ExpectNoError(err)
		return created
	}
	update := func(policy *netpolv1alpha2.ClusterNetworkPolicy, rules ...netpolv1alpha2.ClusterNetworkPolicyEgressRule) *netpolv1alpha2.ClusterNetworkPolicy {
		policy.Spec.Egress = rules
		updated, err := cnpClient.Update(context.Background(), policy, metav1.UpdateOptions{})
		framework.ExpectNoError(err)
		return updated
	}
	waitResolvers := func(name string, count int) {
		// Resolution follows actual DNS queries; resolver creation alone is not
		// evidence that addresses or ACLs have converged. Connectivity is checked next.
		framework.WaitUntil(time.Second, 30*time.Second, func(context.Context) (bool, error) {
			list := f.DNSNameResolverClient().ListByLabel("anp=" + name)
			return len(list.Items) == count, nil
		}, fmt.Sprintf("%d DNSNameResolvers for CNP %s", count, name))
	}
	remove := func(policy *netpolv1alpha2.ClusterNetworkPolicy) {
		framework.ExpectNoError(cnpClient.Delete(context.Background(), policy.Name, metav1.DeleteOptions{}))
	}

	framework.ConformanceIt("allows a domain above a catch-all denial and restores connectivity on deletion", func() {
		connect("https://www.baidu.com", true)
		connect("https://www.google.com", true)
		policy := create(55, allowDomain("allow-baidu", "*.baidu.com."), denyHTTPS())
		waitResolvers(policy.Name, 1)
		connect("https://www.baidu.com", true)
		connect("https://www.google.com", false)
		remove(policy)
		connect("https://www.google.com", true)
	})

	framework.ConformanceIt("combines domain accepts in different policy priorities", func() {
		connect("https://www.baidu.com", true)
		connect("https://www.google.com", true)
		baidu := create(45, allowDomain("allow-baidu", "*.baidu.com."), denyHTTPS())
		waitResolvers(baidu.Name, 1)
		connect("https://www.baidu.com", true)
		connect("https://www.google.com", false)
		google := create(44, allowDomain("allow-google", "*.google.com."))
		waitResolvers(google.Name, 1)
		connect("https://www.google.com", true)
		connect("https://www.baidu.com", true)
		remove(google)
		connect("https://www.google.com", false)
		remove(baidu)
		connect("https://www.google.com", true)
	})

	framework.ConformanceIt("updates domain accepts without losing the catch-all denial", func() {
		connect("https://www.baidu.com", true)
		connect("https://www.google.com", true)
		policy := create(50)
		baidu, google := allowDomain("allow-baidu", "*.baidu.com."), allowDomain("allow-google", "*.google.com.")
		policy = update(policy, baidu, denyHTTPS())
		waitResolvers(policy.Name, 1)
		connect("https://www.baidu.com", true)
		connect("https://www.google.com", false)
		policy = update(policy, baidu, google, denyHTTPS())
		waitResolvers(policy.Name, 2)
		connect("https://www.baidu.com", true)
		connect("https://www.google.com", true)
		policy = update(policy, google, denyHTTPS())
		waitResolvers(policy.Name, 1)
		connect("https://www.baidu.com", false)
		connect("https://www.google.com", true)
		update(policy)
		connect("https://www.baidu.com", true)
		connect("https://www.google.com", true)
	})

	framework.ConformanceIt("combines a domain accept with a higher CIDR denial", func() {
		connect("https://www.baidu.com", true)
		connect("https://www.google.com", true)
		connect("https://8.8.8.8", true)
		denyCIDR := netpolv1alpha2.ClusterNetworkPolicyEgressRule{
			Name: "deny-google-dns", Action: netpolv1alpha2.ClusterNetworkPolicyRuleActionDeny, Protocols: protocols,
			To: []netpolv1alpha2.ClusterNetworkPolicyEgressPeer{{Networks: []netpolv1alpha2.CIDR{"8.8.8.8/32"}}},
		}
		policy := create(80, denyCIDR, allowDomain("allow-baidu", "*.baidu.com."), denyHTTPS())
		waitResolvers(policy.Name, 1)
		connect("https://www.baidu.com", true)
		connect("https://www.google.com", false)
		connect("https://8.8.8.8", false)
	})

	framework.ConformanceIt("allows wildcard subdomains while denying unrelated HTTPS destinations", func() {
		for _, target := range []string{"https://www.baidu.com", "https://api.baidu.com", "https://news.baidu.com", "https://www.google.com"} {
			connect(target, true)
		}
		policy := create(85, allowDomain("allow-baidu-wildcard", "*.baidu.com."), denyHTTPS())
		waitResolvers(policy.Name, 1)
		for _, target := range []string{"https://www.baidu.com", "https://api.baidu.com", "https://news.baidu.com"} {
			connect(target, true)
		}
		connect("https://www.google.com", false)
	})
})

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
