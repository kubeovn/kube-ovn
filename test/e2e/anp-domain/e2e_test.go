package anp_domain

import (
	"context"
	"flag"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	netpolv1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.SerialDescribe("[group:admin-network-policy]", func() {
	f := framework.NewDefaultFramework("admin-network-policy")

	var cs clientset.Interface
	var anpClient *framework.AnpClient
	var anpName string
	var anpName2 string
	var namespaceName string
	var podName string
	var podClient *framework.PodClient
	var nsClient *framework.NamespaceClient

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		nsClient = f.NamespaceClient()
		anpClient = f.AnpClient()

		anpName = "anp-" + framework.RandomSuffix()
		namespaceName = "ns-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		podClient = f.PodClientNS(namespaceName)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Cleaning up test resources")
		ctx := context.Background()

		// Clean up ANP if it exists
		if anpName != "" {
			ginkgo.By("Deleting AdminNetworkPolicy " + anpName)
			anpClient.Delete(anpName)
		}

		// Clean up second ANP if it exists (for multiple ANP test)
		if anpName2 != "" {
			ginkgo.By("Deleting second AdminNetworkPolicy " + anpName2)
			anpClient.Delete(anpName2)
		}

		// Clean up pod if it exists
		if podName != "" {
			ginkgo.By("Deleting test pod " + podName)
			err := cs.CoreV1().Pods(namespaceName).Delete(ctx, podName, metav1.DeleteOptions{})
			if err != nil {
				framework.Logf("Failed to delete pod %s: %v", podName, err)
			}
		}

		// Clean up namespace if it exists
		if namespaceName != "" {
			ginkgo.By("Deleting namespace " + namespaceName)
			nsClient.Delete(namespaceName)
		}
	})

	// testNetworkConnectivityWithRetry tests network connectivity with configurable retry mechanism
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

	// testNetworkConnectivity tests network connectivity with default retry mechanism
	testNetworkConnectivity := func(target string, shouldSucceed bool, description string) {
		testNetworkConnectivityWithRetry(target, shouldSucceed, description, 20, 2*time.Second)
	}

	framework.ConformanceIt("should create ANP with domainName deny rule and verify connectivity behavior", func() {
		f.SkipVersionPriorTo(1, 15, "AdminNetworkPolicy domainName support was introduced in v1.15")
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

		// Test connectivity to baidu.com before applying ANP (should succeed)
		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com before applying ANP (should succeed)")

		ginkgo.By("Creating AdminNetworkPolicy with domainName to deny baidu.com")
		namespaceSelector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": namespaceName,
			},
		}
		ports := []netpolv1alpha1.AdminNetworkPolicyPort{
			framework.MakeAdminNetworkPolicyPort(443, corev1.ProtocolTCP),
		}
		domainNames := []netpolv1alpha1.DomainName{"*.baidu.com."}
		egressRule := framework.MakeAdminNetworkPolicyEgressRule("deny-baidu", netpolv1alpha1.AdminNetworkPolicyRuleActionDeny, ports, domainNames)
		anp := framework.MakeAdminNetworkPolicy(anpName, 55, namespaceSelector, []netpolv1alpha1.AdminNetworkPolicyEgressRule{egressRule}, nil)

		// Create the ANP in the cluster
		ginkgo.By("Creating AdminNetworkPolicy in the cluster")
		createdANP := anpClient.CreateSync(anp)
		framework.Logf("Successfully created AdminNetworkPolicy: %s", createdANP.Name)

		ginkgo.By("Verifying ANP structure is correct")
		framework.ExpectEqual(len(anp.Spec.Egress), 1)
		anpEgressRule := anp.Spec.Egress[0]
		framework.ExpectEqual(len(anpEgressRule.To), 1)
		peer := anpEgressRule.To[0]
		framework.ExpectEqual(len(peer.DomainNames), 1)
		framework.ExpectEqual(string(peer.DomainNames[0]), "*.baidu.com.")

		framework.ExpectEqual(anp.Spec.Priority, int32(55))
		framework.ExpectEqual(anp.Spec.Subject.Namespaces.MatchLabels["kubernetes.io/metadata.name"], namespaceName)

		// Test connectivity to baidu.com after applying ANP (should be blocked)
		testNetworkConnectivity("https://www.baidu.com", false, "Testing connectivity to baidu.com after applying ANP (should be blocked)")

		// Delete the ANP rule
		ginkgo.By("Deleting AdminNetworkPolicy " + anpName)
		anpClient.Delete(anpName)

		// Test connectivity to baidu.com after deleting ANP (should succeed again)
		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com after deleting ANP (should succeed again)")
	})

	framework.ConformanceIt("should create multiple ANPs with domainName rules and verify they work together", func() {
		f.SkipVersionPriorTo(1, 15, "AdminNetworkPolicy domainName support was introduced in v1.15")

		// Create additional ANP name for the second policy
		anpName2 = "anp2-" + framework.RandomSuffix()

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

		// Test connectivity to both domains before applying ANPs (should succeed)
		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com before applying ANPs (should succeed)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com before applying ANPs (should succeed)")

		ginkgo.By("Creating first AdminNetworkPolicy with domainName to deny baidu.com")
		namespaceSelector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": namespaceName,
			},
		}
		ports := []netpolv1alpha1.AdminNetworkPolicyPort{
			framework.MakeAdminNetworkPolicyPort(443, corev1.ProtocolTCP),
		}
		domainNames1 := []netpolv1alpha1.DomainName{"*.baidu.com."}
		egressRule1 := framework.MakeAdminNetworkPolicyEgressRule("deny-baidu", netpolv1alpha1.AdminNetworkPolicyRuleActionDeny, ports, domainNames1)
		anp1 := framework.MakeAdminNetworkPolicy(anpName, 44, namespaceSelector, []netpolv1alpha1.AdminNetworkPolicyEgressRule{egressRule1}, nil)

		// Create the first ANP in the cluster
		ginkgo.By("Creating first AdminNetworkPolicy in the cluster")
		createdANP1 := anpClient.CreateSync(anp1)
		framework.Logf("Successfully created first AdminNetworkPolicy: %s", createdANP1.Name)

		ginkgo.By("Creating second AdminNetworkPolicy with domainName to allow google.com")
		domainNames2 := []netpolv1alpha1.DomainName{"*.google.com."}
		egressRule2 := framework.MakeAdminNetworkPolicyEgressRule("allow-google", netpolv1alpha1.AdminNetworkPolicyRuleActionAllow, ports, domainNames2)
		anp2 := framework.MakeAdminNetworkPolicy(anpName2, 45, namespaceSelector, []netpolv1alpha1.AdminNetworkPolicyEgressRule{egressRule2}, nil)

		// Create the second ANP in the cluster
		ginkgo.By("Creating second AdminNetworkPolicy in the cluster")
		createdANP2 := anpClient.CreateSync(anp2)
		framework.Logf("Successfully created second AdminNetworkPolicy: %s", createdANP2.Name)

		ginkgo.By("Verifying both ANPs structure is correct")
		// Verify first ANP
		framework.ExpectEqual(len(anp1.Spec.Egress), 1)
		anp1EgressRule := anp1.Spec.Egress[0]
		framework.ExpectEqual(len(anp1EgressRule.To), 1)
		peer1 := anp1EgressRule.To[0]
		framework.ExpectEqual(len(peer1.DomainNames), 1)
		framework.ExpectEqual(string(peer1.DomainNames[0]), "*.baidu.com.")
		framework.ExpectEqual(anp1.Spec.Priority, int32(44))

		// Verify second ANP
		framework.ExpectEqual(len(anp2.Spec.Egress), 1)
		anp2EgressRule := anp2.Spec.Egress[0]
		framework.ExpectEqual(len(anp2EgressRule.To), 1)
		peer2 := anp2EgressRule.To[0]
		framework.ExpectEqual(len(peer2.DomainNames), 1)
		framework.ExpectEqual(string(peer2.DomainNames[0]), "*.google.com.")
		framework.ExpectEqual(anp2.Spec.Priority, int32(45))

		// Test connectivity after applying both ANPs
		// baidu.com should be blocked (deny rule with higher priority)
		testNetworkConnectivity("https://www.baidu.com", false, "Testing connectivity to baidu.com after applying both ANPs (should be blocked)")

		// google.com should be allowed (allow rule)
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com after applying both ANPs (should be allowed)")

		// Delete the first ANP (baidu.com deny rule)
		ginkgo.By("Deleting first AdminNetworkPolicy " + anpName)
		anpClient.Delete(anpName)

		// Test connectivity after deleting first ANP
		// baidu.com should be allowed now (no deny rule)
		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com after deleting first ANP (should be allowed)")

		// google.com should still be allowed
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com after deleting first ANP (should still be allowed)")

		// Delete the second ANP (google.com allow rule)
		ginkgo.By("Deleting second AdminNetworkPolicy " + anpName2)
		anpClient.Delete(anpName2)

		// Test connectivity after deleting both ANPs (should succeed for both)
		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com after deleting both ANPs (should succeed)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com after deleting both ANPs (should succeed)")
	})

	framework.ConformanceIt("should dynamically add and remove domainName deny rules in a single ANP", func() {
		f.SkipVersionPriorTo(1, 15, "AdminNetworkPolicy domainName support was introduced in v1.15")

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

		// Test connectivity to both domains before applying any ANP (should succeed)
		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com before applying ANP (should succeed)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com before applying ANP (should succeed)")

		ginkgo.By("Creating AdminNetworkPolicy without any domainName rules initially")
		namespaceSelector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": namespaceName,
			},
		}
		ports := []netpolv1alpha1.AdminNetworkPolicyPort{
			framework.MakeAdminNetworkPolicyPort(443, corev1.ProtocolTCP),
		}

		// Create ANP without any egress rules initially
		anp := framework.MakeAdminNetworkPolicy(anpName, 50, namespaceSelector, nil, nil)
		createdANP := anpClient.CreateSync(anp)
		framework.Logf("Successfully created AdminNetworkPolicy: %s", createdANP.Name)

		// Test connectivity after creating ANP without rules (should still succeed)
		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com after creating ANP without rules (should succeed)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com after creating ANP without rules (should succeed)")

		ginkgo.By("Adding domainName deny rule for baidu.com to the existing ANP")
		domainNames := []netpolv1alpha1.DomainName{"*.baidu.com."}
		egressRule := framework.MakeAdminNetworkPolicyEgressRule("deny-baidu", netpolv1alpha1.AdminNetworkPolicyRuleActionDeny, ports, domainNames)

		// Update the ANP to add the egress rule
		createdANP.Spec.Egress = []netpolv1alpha1.AdminNetworkPolicyEgressRule{egressRule}
		updatedANP := anpClient.Update(createdANP)
		framework.Logf("Successfully updated AdminNetworkPolicy with baidu.com deny rule: %s", updatedANP.Name)

		// Test connectivity after adding baidu.com deny rule
		testNetworkConnectivity("https://www.baidu.com", false, "Testing connectivity to baidu.com after adding deny rule (should be blocked)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com after adding baidu.com deny rule (should still succeed)")

		ginkgo.By("Adding domainName deny rule for google.com to the existing ANP")
		domainNames2 := []netpolv1alpha1.DomainName{"*.google.com."}
		egressRule2 := framework.MakeAdminNetworkPolicyEgressRule("deny-google", netpolv1alpha1.AdminNetworkPolicyRuleActionDeny, ports, domainNames2)

		// Update the ANP to add the second egress rule
		updatedANP.Spec.Egress = append(updatedANP.Spec.Egress, egressRule2)
		updatedANP2 := anpClient.Update(updatedANP)
		framework.Logf("Successfully updated AdminNetworkPolicy with both deny rules: %s", updatedANP2.Name)

		// Test connectivity after adding both deny rules
		testNetworkConnectivity("https://www.baidu.com", false, "Testing connectivity to baidu.com after adding both deny rules (should be blocked)")
		testNetworkConnectivity("https://www.google.com", false, "Testing connectivity to google.com after adding both deny rules (should be blocked)")

		ginkgo.By("Removing baidu.com deny rule from the ANP")
		// Remove the first egress rule (baidu.com deny)
		updatedANP2.Spec.Egress = []netpolv1alpha1.AdminNetworkPolicyEgressRule{egressRule2}
		updatedANP3 := anpClient.Update(updatedANP2)
		framework.Logf("Successfully updated AdminNetworkPolicy removing baidu.com deny rule: %s", updatedANP3.Name)

		// Test connectivity after removing baidu.com deny rule
		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com after removing deny rule (should succeed)")
		testNetworkConnectivity("https://www.google.com", false, "Testing connectivity to google.com after removing baidu.com deny rule (should still be blocked)")

		ginkgo.By("Removing all domainName deny rules from the ANP")
		// Remove all egress rules
		updatedANP3.Spec.Egress = nil
		updatedANP4 := anpClient.Update(updatedANP3)
		framework.Logf("Successfully updated AdminNetworkPolicy removing all deny rules: %s", updatedANP4.Name)

		// Test connectivity after removing all deny rules
		testNetworkConnectivity("https://www.baidu.com", true, "Testing connectivity to baidu.com after removing all deny rules (should succeed)")
		testNetworkConnectivity("https://www.google.com", true, "Testing connectivity to google.com after removing all deny rules (should succeed)")
	})
})

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
	e2e.RunE2ETests(t)
}
