package cnp_domain

import (
	"context"
	"flag"
	"fmt"
	netpolv1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.SerialDescribe("[group:cluster-network-policy]", func() {
	f := framework.NewDefaultFramework("cluster-network-policy")

	var cs clientset.Interface
	var cnpClient *framework.CnpClient
	var cnpName string
	var cnpName2 string
	var namespaceName string
	var podName string
	var podClient *framework.PodClient
	var nsClient *framework.NamespaceClient

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		nsClient = f.NamespaceClient()
		cnpClient = f.CnpClient()

		cnpName = "cnp-" + framework.RandomSuffix()
		namespaceName = "ns-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		podClient = f.PodClientNS(namespaceName)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Cleaning up test resources")
		ctx := context.Background()

		if cnpName != "" {
			ginkgo.By("Deleting ClusterNetworkPolicy " + cnpName)
			cnpClient.Delete(context.TODO(), cnpName, metav1.DeleteOptions{})
		}

		if cnpName2 != "" {
			ginkgo.By("Deleting second ClusterNetworkPolicy " + cnpName2)
			cnpClient.Delete(context.TODO(), cnpName2, metav1.DeleteOptions{})
		}

		if podName != "" {
			ginkgo.By("Deleting test pod " + podName)
			err := cs.CoreV1().Pods(namespaceName).Delete(ctx, podName, metav1.DeleteOptions{})
			if err != nil {
				framework.Logf("Failed to delete pod %s: %v", podName, err)
			}
		}

		if namespaceName != "" {
			ginkgo.By("Deleting namespace " + namespaceName)
			nsClient.Delete(namespaceName)
		}
	})

	testNetworkConnectivityWithRetry := func(target string, shouldSucceed bool, description string, maxRetries int, retryInterval time.Duration) {
		ginkgo.By(description)

		ctx := context.Background()
		cmd := fmt.Sprintf("curl -s --connect-timeout 5 --max-time 5 %s", target)

		for i := range maxRetries {
			stdout, stderr, err := framework.ExecShellInPod(ctx, f, namespaceName, podName, cmd)
			success := (err == nil && len(stdout) > 0)

			if shouldSucceed {
				if success {
					framework.Logf("Attempt %d: Successfully connected to %s", i+1, target)
					return // Success, no need to retry
				}
				framework.Logf("Attempt %d: Failed to connect to %s - stdout: %s, stderr: %s, err: %v", i+1, target, stdout, stderr, err)
			} else {
				if !success {
					framework.Logf("Attempt %d: Successfully blocked access to %s", i+1, target)
					return // Blocked as expected, no need to retry
				}
				framework.Logf("Attempt %d: Unexpectedly connected to %s - stdout: %s", i+1, target, stdout)
			}

			// Wait between attempts if not the last attempt
			if i < maxRetries-1 {
				time.Sleep(retryInterval)
			}
		}

		// If we reach here, the expected result was not achieved
		if shouldSucceed {
			ginkgo.Fail(fmt.Sprintf("Failed to connect to %s after %d attempts", target, maxRetries))
		} else {
			ginkgo.Fail(fmt.Sprintf("Unexpectedly connected to %s after %d attempts", target, maxRetries))
		}
	}

	testNetworkConnectivity := func(target string, shouldSucceed bool, description string) {
		testNetworkConnectivityWithRetry(target, shouldSucceed, description, 20, 2*time.Second)
	}

	framework.ConformanceIt("should create CNP with domainName deny rule and verify connectivity behavior", func() {
		f.SkipVersionPriorTo(1, 15, "ClusterNetworkPolicy domainName support was introduced in v1.15")
		ginkgo.By("Creating test namespace " + namespaceName)
		labels := map[string]string{
			"kubernetes.io/metadata.name": namespaceName,
		}
		ns := framework.MakeNamespace(namespaceName, labels, nil)
		_ = nsClient.Create(ns)

		ginkgo.By("Creating test pod " + podName + " in namespace " + namespaceName)
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
		_ = podClient.CreateSync(pod)

		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com before applying CNP (should succeed)")

		ginkgo.By("Creating ClusterNetworkPolicy with domainName to deny baidu.com")
		namespaceSelector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": namespaceName,
			},
		}
		ports := []netpolv1alpha2.ClusterNetworkPolicyPort{
			framework.MakeClusterNetworkPolicyPort(443, corev1.ProtocolTCP),
		}
		domainNames := []netpolv1alpha2.DomainName{"*.baidu.com."}
		egressRule := framework.MakeClusterNetworkPolicyEgressRule("deny-baidu", netpolv1alpha2.ClusterNetworkPolicyRuleActionDeny, ports, domainNames)
		cnp := framework.MakeClusterNetworkPolicy(cnpName, 55, namespaceSelector, []netpolv1alpha2.ClusterNetworkPolicyEgressRule{egressRule}, nil)

		ginkgo.By("Creating ClusterNetworkPolicy in the cluster")
		createdCNP, _ := cnpClient.Create(context.TODO(), cnp, metav1.CreateOptions{})
		framework.Logf("Successfully created ClusterNetworkPolicy: %s", createdCNP.Name)

		ginkgo.By("Verifying CNP structure is correct")
		framework.ExpectEqual(len(cnp.Spec.Egress), 1)
		cnpEgressRule := cnp.Spec.Egress[0]
		framework.ExpectEqual(len(cnpEgressRule.To), 1)
		peer := cnpEgressRule.To[0]
		framework.ExpectEqual(len(peer.DomainNames), 1)
		framework.ExpectEqual(string(peer.DomainNames[0]), "*.baidu.com.")

		framework.ExpectEqual(cnp.Spec.Priority, int32(55))
		framework.ExpectEqual(cnp.Spec.Subject.Namespaces.MatchLabels["kubernetes.io/metadata.name"], namespaceName)

		testNetworkConnectivity("https://www.baidu.com", false, "Testing connectivity to baidu.com after applying CNP (should be blocked)")

		ginkgo.By("Deleting ClusterNetworkPolicy " + cnpName)
		cnpClient.Delete(context.TODO(), cnpName, metav1.DeleteOptions{})

		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com after deleting CNP (should succeed again)")
	})

	framework.ConformanceIt("should create multiple CNPs with domainName rules and verify they work together", func() {
		f.SkipVersionPriorTo(1, 15, "ClusterNetworkPolicy domainName support was introduced in v1.15")

		cnpName2 = "cnp2-" + framework.RandomSuffix()

		ginkgo.By("Creating test namespace " + namespaceName)
		labels := map[string]string{
			"kubernetes.io/metadata.name": namespaceName,
		}
		ns := framework.MakeNamespace(namespaceName, labels, nil)
		_ = nsClient.Create(ns)

		ginkgo.By("Creating test pod " + podName + " in namespace " + namespaceName)
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
		_ = podClient.CreateSync(pod)

		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com before applying cnps (should succeed)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com before applying cnps (should succeed)")

		ginkgo.By("Creating first ClusterNetworkPolicy with domainName to deny baidu.com")
		namespaceSelector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": namespaceName,
			},
		}
		ports := []netpolv1alpha2.ClusterNetworkPolicyPort{
			framework.MakeClusterNetworkPolicyPort(443, corev1.ProtocolTCP),
		}
		domainNames1 := []netpolv1alpha2.DomainName{"*.baidu.com."}
		egressRule1 := framework.MakeClusterNetworkPolicyEgressRule("deny-baidu", netpolv1alpha2.ClusterNetworkPolicyRuleActionDeny, ports, domainNames1)
		cnp1 := framework.MakeClusterNetworkPolicy(cnpName, 44, namespaceSelector, []netpolv1alpha2.ClusterNetworkPolicyEgressRule{egressRule1}, nil)

		ginkgo.By("Creating first ClusterNetworkPolicy in the cluster")
		createdcnp1, _ := cnpClient.Create(context.TODO(), cnp1, metav1.CreateOptions{})
		framework.Logf("Successfully created first ClusterNetworkPolicy: %s", createdcnp1.Name)

		ginkgo.By("Creating second ClusterNetworkPolicy with domainName to allow google.com")
		domainNames2 := []netpolv1alpha2.DomainName{"*.google.com."}
		egressRule2 := framework.MakeClusterNetworkPolicyEgressRule("allow-google", netpolv1alpha2.ClusterNetworkPolicyRuleActionAccept, ports, domainNames2)
		cnp2 := framework.MakeClusterNetworkPolicy(cnpName2, 45, namespaceSelector, []netpolv1alpha2.ClusterNetworkPolicyEgressRule{egressRule2}, nil)

		ginkgo.By("Creating second ClusterNetworkPolicy in the cluster")
		createdCNP2, _ := cnpClient.Create(context.TODO(), cnp2, metav1.CreateOptions{})
		framework.Logf("Successfully created second ClusterNetworkPolicy: %s", createdCNP2.Name)

		ginkgo.By("Verifying both cnps structure is correct")
		framework.ExpectEqual(len(cnp1.Spec.Egress), 1)
		cnp1EgressRule := cnp1.Spec.Egress[0]
		framework.ExpectEqual(len(cnp1EgressRule.To), 1)
		peer1 := cnp1EgressRule.To[0]
		framework.ExpectEqual(len(peer1.DomainNames), 1)
		framework.ExpectEqual(string(peer1.DomainNames[0]), "*.baidu.com.")
		framework.ExpectEqual(cnp1.Spec.Priority, int32(44))

		framework.ExpectEqual(len(cnp2.Spec.Egress), 1)
		cnp2EgressRule := cnp2.Spec.Egress[0]
		framework.ExpectEqual(len(cnp2EgressRule.To), 1)
		peer2 := cnp2EgressRule.To[0]
		framework.ExpectEqual(len(peer2.DomainNames), 1)
		framework.ExpectEqual(string(peer2.DomainNames[0]), "*.google.com.")
		framework.ExpectEqual(cnp2.Spec.Priority, int32(45))

		testNetworkConnectivity("https://www.baidu.com", false, "Testing connectivity to baidu.com after applying both CNPs (should be blocked)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com after applying both CNPs (should be allowed)")

		ginkgo.By("Deleting first ClusterNetworkPolicy " + cnpName)
		cnpClient.Delete(context.TODO(), cnpName, metav1.DeleteOptions{})

		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com after deleting first CNP (should be allowed)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com after deleting first CNP (should still be allowed)")

		ginkgo.By("Deleting second ClusterNetworkPolicy " + cnpName2)
		cnpClient.Delete(context.TODO(), cnpName2, metav1.DeleteOptions{})

		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com after deleting both CNPs (should succeed)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com after deleting both CNPs (should succeed)")
	})

	framework.ConformanceIt("should dynamically add and remove domainName deny rules in a single CNP", func() {
		f.SkipVersionPriorTo(1, 15, "ClusterNetworkPolicy domainName support was introduced in v1.15")

		ginkgo.By("Creating test namespace " + namespaceName)
		labels := map[string]string{
			"kubernetes.io/metadata.name": namespaceName,
		}
		ns := framework.MakeNamespace(namespaceName, labels, nil)
		_ = nsClient.Create(ns)

		ginkgo.By("Creating test pod " + podName + " in namespace " + namespaceName)
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
		_ = podClient.CreateSync(pod)

		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com before applying cnp (should succeed)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com before applying cnp (should succeed)")

		ginkgo.By("Creating ClusterNetworkPolicy without any domainName rules initially")
		namespaceSelector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": namespaceName,
			},
		}
		ports := []netpolv1alpha2.ClusterNetworkPolicyPort{
			framework.MakeClusterNetworkPolicyPort(443, corev1.ProtocolTCP),
		}

		cnp := framework.MakeClusterNetworkPolicy(cnpName, 50, namespaceSelector, nil, nil)
		createdCNP, _ := cnpClient.Create(context.TODO(), cnp, metav1.CreateOptions{})
		framework.Logf("Successfully created ClusterNetworkPolicy: %s", createdCNP.Name)

		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com after creating cnp without rules (should succeed)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com after creating cnp without rules (should succeed)")

		ginkgo.By("Adding domainName deny rule for baidu.com to the existing CNP")
		domainNames := []netpolv1alpha2.DomainName{"*.baidu.com."}
		egressRule := framework.MakeClusterNetworkPolicyEgressRule("deny-baidu", netpolv1alpha2.ClusterNetworkPolicyRuleActionDeny, ports, domainNames)

		createdCNP.Spec.Egress = []netpolv1alpha2.ClusterNetworkPolicyEgressRule{egressRule}
		updatedCNP, _ := cnpClient.Update(context.TODO(), createdCNP, metav1.UpdateOptions{})
		framework.Logf("Successfully updated ClusterNetworkPolicy with baidu.com deny rule: %s", updatedCNP.Name)

		testNetworkConnectivity("https://www.baidu.com", false, "Testing connectivity to baidu.com after adding deny rule (should be blocked)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com after adding baidu.com deny rule (should still succeed)")

		ginkgo.By("Adding domainName deny rule for google.com to the existing CNP")
		domainNames2 := []netpolv1alpha2.DomainName{"*.google.com."}
		egressRule2 := framework.MakeClusterNetworkPolicyEgressRule("deny-google", netpolv1alpha2.ClusterNetworkPolicyRuleActionDeny, ports, domainNames2)

		updatedCNP.Spec.Egress = append(updatedCNP.Spec.Egress, egressRule2)
		updatedcnp2, _ := cnpClient.Update(context.TODO(), updatedCNP, metav1.UpdateOptions{})
		framework.Logf("Successfully updated ClusterNetworkPolicy with both deny rules: %s", updatedcnp2.Name)

		testNetworkConnectivity("https://www.baidu.com", false, "Testing connectivity to baidu.com after adding both deny rules (should be blocked)")
		testNetworkConnectivity("https://www.google.com", false, "Testing connectivity to google.com after adding both deny rules (should be blocked)")

		ginkgo.By("Removing baidu.com deny rule from the cnp")
		updatedcnp2.Spec.Egress = []netpolv1alpha2.ClusterNetworkPolicyEgressRule{egressRule2}
		updatedcnp3, _ := cnpClient.Update(context.TODO(), updatedcnp2, metav1.UpdateOptions{})
		framework.Logf("Successfully updated ClusterNetworkPolicy removing baidu.com deny rule: %s", updatedcnp3.Name)

		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com after removing deny rule (should succeed)")
		testNetworkConnectivity("https://www.google.com", false, "Testing connectivity to google.com after removing baidu.com deny rule (should still be blocked)")

		ginkgo.By("Removing all domainName deny rules from the cnp")
		updatedcnp3.Spec.Egress = nil
		updatedcnp4, _ := cnpClient.Update(context.TODO(), updatedcnp3, metav1.UpdateOptions{})
		framework.Logf("Successfully updated ClusterNetworkPolicy removing all deny rules: %s", updatedcnp4.Name)

		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com after removing all deny rules (should succeed)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com after removing all deny rules (should succeed)")
	})

	framework.ConformanceIt("should create cnp with domainName and CIDR rules and verify they work together", func() {
		f.SkipVersionPriorTo(1, 15, "ClusterNetworkPolicy domainName support was introduced in v1.15")

		ginkgo.By("Creating test namespace " + namespaceName)
		labels := map[string]string{
			"kubernetes.io/metadata.name": namespaceName,
		}
		ns := framework.MakeNamespace(namespaceName, labels, nil)
		_ = nsClient.Create(ns)

		ginkgo.By("Creating test pod " + podName + " in namespace " + namespaceName)
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
		_ = podClient.CreateSync(pod)

		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com before applying cnp (should succeed)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com before applying cnp (should succeed)")
		testNetworkConnectivity("https://8.8.8.8", true, "Testing connectivity to 8.8.8.8 before applying cnp (should succeed)")

		ginkgo.By("Creating ClusterNetworkPolicy with both domainName and CIDR rules")
		namespaceSelector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": namespaceName,
			},
		}
		ports := []netpolv1alpha2.ClusterNetworkPolicyPort{
			framework.MakeClusterNetworkPolicyPort(443, corev1.ProtocolTCP),
		}

		domainNames := []netpolv1alpha2.DomainName{"*.baidu.com."}
		egressRule1 := framework.MakeClusterNetworkPolicyEgressRule("deny-baidu", netpolv1alpha2.ClusterNetworkPolicyRuleActionDeny, ports, domainNames)

		egressRule2 := netpolv1alpha2.ClusterNetworkPolicyEgressRule{
			Name:   "deny-google-dns",
			Action: netpolv1alpha2.ClusterNetworkPolicyRuleActionDeny,
			To: []netpolv1alpha2.ClusterNetworkPolicyEgressPeer{
				{
					Networks: []netpolv1alpha2.CIDR{"8.8.8.8/32"},
				},
			},
			Ports: &ports,
		}

		cnp := framework.MakeClusterNetworkPolicy(cnpName, 80, namespaceSelector,
			[]netpolv1alpha2.ClusterNetworkPolicyEgressRule{egressRule1, egressRule2}, nil)

		ginkgo.By("Creating ClusterNetworkPolicy in the cluster")
		createdcnp, _ := cnpClient.Create(context.TODO(), cnp, metav1.CreateOptions{})
		framework.Logf("Successfully created ClusterNetworkPolicy: %s", createdcnp.Name)

		ginkgo.By("Verifying cnp structure is correct")
		framework.ExpectEqual(len(cnp.Spec.Egress), 2)
		framework.ExpectEqual(cnp.Spec.Priority, int32(80))

		testNetworkConnectivity("https://www.baidu.com", false, "Testing connectivity to baidu.com after applying cnp (should be blocked)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com after applying cnp (should be allowed)")
		testNetworkConnectivity("https://8.8.8.8", false, "Testing connectivity to 8.8.8.8 after applying cnp (should be blocked by CIDR rule)")
	})

	framework.ConformanceIt("should create cnp with wildcard domainName rules and verify they work correctly", func() {
		f.SkipVersionPriorTo(1, 15, "ClusterNetworkPolicy domainName support was introduced in v1.15")

		ginkgo.By("Creating test namespace " + namespaceName)
		labels := map[string]string{
			"kubernetes.io/metadata.name": namespaceName,
		}
		ns := framework.MakeNamespace(namespaceName, labels, nil)
		_ = nsClient.Create(ns)

		ginkgo.By("Creating test pod " + podName + " in namespace " + namespaceName)
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
		_ = podClient.CreateSync(pod)

		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to www.baidu.com before applying cnp (should succeed)")
		testNetworkConnectivity("https://api.baidu.com", true, "Testing connectivity to api.baidu.com before applying cnp (should succeed)")
		testNetworkConnectivity("https://news.baidu.com", true, "Testing connectivity to news.baidu.com before applying cnp (should succeed)")

		ginkgo.By("Creating ClusterNetworkPolicy with wildcard domainName rules")
		namespaceSelector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": namespaceName,
			},
		}
		ports := []netpolv1alpha2.ClusterNetworkPolicyPort{
			framework.MakeClusterNetworkPolicyPort(443, corev1.ProtocolTCP),
		}

		domainNames1 := []netpolv1alpha2.DomainName{"*.baidu.com."}
		egressRule1 := framework.MakeClusterNetworkPolicyEgressRule("deny-baidu-wildcard", netpolv1alpha2.ClusterNetworkPolicyRuleActionDeny, ports, domainNames1)

		cnp := framework.MakeClusterNetworkPolicy(cnpName, 85, namespaceSelector,
			[]netpolv1alpha2.ClusterNetworkPolicyEgressRule{egressRule1}, nil)

		ginkgo.By("Creating ClusterNetworkPolicy in the cluster")
		createdcnp, _ := cnpClient.Create(context.TODO(), cnp, metav1.CreateOptions{})
		framework.Logf("Successfully created ClusterNetworkPolicy: %s", createdcnp.Name)

		ginkgo.By("Verifying cnp structure is correct")
		framework.ExpectEqual(len(cnp.Spec.Egress), 1)
		framework.ExpectEqual(cnp.Spec.Priority, int32(85))

		testNetworkConnectivity("https://www.baidu.com", false, "Testing connectivity to www.baidu.com after applying cnp (should be blocked by wildcard)")
		testNetworkConnectivity("https://api.baidu.com", false, "Testing connectivity to api.baidu.com after applying cnp (should be blocked by wildcard)")
		testNetworkConnectivity("https://news.baidu.com", false, "Testing connectivity to news.baidu.com after applying cnp (should be blocked by wildcard)")
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
