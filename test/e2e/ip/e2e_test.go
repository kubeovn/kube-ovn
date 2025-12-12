package ip

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:ip]", func() {
	f := framework.NewDefaultFramework("ip")

	var vpcClient *framework.VpcClient
	var subnetClient *framework.SubnetClient
	var podClient *framework.PodClient
	var ipClient *framework.IPClient
	var namespaceName, vpcName, subnetName, cidr string

	ginkgo.BeforeEach(func() {
		vpcClient = f.VpcClient()
		subnetClient = f.SubnetClient()
		podClient = f.PodClient()
		ipClient = f.IPClient()
		namespaceName = f.Namespace.Name
		cidr = framework.RandomCIDR(f.ClusterIPFamily)

		randomSuffix := framework.RandomSuffix()
		vpcName = "vpc-" + randomSuffix
		subnetName = "subnet-" + randomSuffix

		ginkgo.By("Creating vpc " + vpcName)
		vpc := framework.MakeVpc(vpcName, "", false, false, []string{namespaceName})
		_ = vpcClient.CreateSync(vpc)

		ginkgo.By("Creating subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", vpcName, "", nil, nil, []string{namespaceName})
		_ = subnetClient.CreateSync(subnet)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
		ginkgo.By("Deleting vpc " + vpcName)
		vpcClient.DeleteSync(vpcName)
	})

	framework.ConformanceIt("Test IP CR creation and deletion with finalizer and subnet status update", func() {
		f.SkipVersionPriorTo(1, 13, "This feature was introduced in v1.13")

		ginkgo.By("1. Get initial subnet status")
		initialSubnet := subnetClient.Get(subnetName)
		initialV4AvailableIPs := initialSubnet.Status.V4AvailableIPs
		initialV4UsingIPs := initialSubnet.Status.V4UsingIPs
		initialV6AvailableIPs := initialSubnet.Status.V6AvailableIPs
		initialV6UsingIPs := initialSubnet.Status.V6UsingIPs
		initialV4AvailableIPRange := initialSubnet.Status.V4AvailableIPRange
		initialV4UsingIPRange := initialSubnet.Status.V4UsingIPRange
		initialV6AvailableIPRange := initialSubnet.Status.V6AvailableIPRange
		initialV6UsingIPRange := initialSubnet.Status.V6UsingIPRange

		ginkgo.By("2. Create a pod to trigger IP CR creation")
		podName := "test-ip-pod-" + framework.RandomSuffix()
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
		_ = podClient.CreateSync(pod)

		ginkgo.By("3. Wait for IP CR to be created and get IP CR name")
		var ipCR *apiv1.IP
		var ipName string
		for range 30 {
			// IP CR name format: podName.namespaceName
			ipName = fmt.Sprintf("%s.%s", podName, namespaceName)
			ipCR = ipClient.Get(ipName)
			if ipCR != nil && ipCR.Name != "" {
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectNotNil(ipCR, "IP CR should be created for pod %s", podName)
		framework.ExpectEqual(ipCR.Spec.Subnet, subnetName, "IP CR should be in the correct subnet")

		ginkgo.By("4. Wait for IP CR finalizer to be added")
		for range 60 {
			ipCR = ipClient.Get(ipName)
			if ipCR != nil && len(ipCR.Finalizers) > 0 {
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectNotNil(ipCR, "IP CR should exist")
		framework.ExpectContainElement(ipCR.Finalizers, util.KubeOVNControllerFinalizer,
			"IP CR should have finalizer after creation")

		ginkgo.By("5. Wait for subnet status to be updated after IP creation")
		time.Sleep(5 * time.Second)

		ginkgo.By("6. Verify subnet status after IP CR creation")
		afterCreateSubnet := subnetClient.Get(subnetName)
		switch afterCreateSubnet.Spec.Protocol {
		case apiv1.ProtocolIPv4:
			// Verify IP count changed
			framework.ExpectEqual(initialV4AvailableIPs-1, afterCreateSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should decrease by 1 after IP creation")
			framework.ExpectEqual(initialV4UsingIPs+1, afterCreateSubnet.Status.V4UsingIPs,
				"V4UsingIPs should increase by 1 after IP creation")

			// Verify IP range changed
			framework.ExpectNotEqual(initialV4AvailableIPRange, afterCreateSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after IP creation")
			framework.ExpectNotEqual(initialV4UsingIPRange, afterCreateSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after IP creation")

			// Verify the IP's address is in the using range
			podIP := ipCR.Spec.V4IPAddress
			framework.ExpectTrue(strings.Contains(afterCreateSubnet.Status.V4UsingIPRange, podIP),
				"Pod IP %s should be in V4UsingIPRange %s", podIP, afterCreateSubnet.Status.V4UsingIPRange)
		case apiv1.ProtocolIPv6:
			// Verify IP count changed
			framework.ExpectEqual(initialV6AvailableIPs-1, afterCreateSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should decrease by 1 after IP creation")
			framework.ExpectEqual(initialV6UsingIPs+1, afterCreateSubnet.Status.V6UsingIPs,
				"V6UsingIPs should increase by 1 after IP creation")

			// Verify IP range changed
			framework.ExpectNotEqual(initialV6AvailableIPRange, afterCreateSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after IP creation")
			framework.ExpectNotEqual(initialV6UsingIPRange, afterCreateSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after IP creation")

			// Verify the IP's address is in the using range
			podIP := ipCR.Spec.V6IPAddress
			framework.ExpectTrue(strings.Contains(afterCreateSubnet.Status.V6UsingIPRange, podIP),
				"Pod IP %s should be in V6UsingIPRange %s", podIP, afterCreateSubnet.Status.V6UsingIPRange)
		default:
			// Dual stack
			framework.ExpectEqual(initialV4AvailableIPs-1, afterCreateSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should decrease by 1 after IP creation")
			framework.ExpectEqual(initialV4UsingIPs+1, afterCreateSubnet.Status.V4UsingIPs,
				"V4UsingIPs should increase by 1 after IP creation")
			framework.ExpectEqual(initialV6AvailableIPs-1, afterCreateSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should decrease by 1 after IP creation")
			framework.ExpectEqual(initialV6UsingIPs+1, afterCreateSubnet.Status.V6UsingIPs,
				"V6UsingIPs should increase by 1 after IP creation")

			framework.ExpectNotEqual(initialV4AvailableIPRange, afterCreateSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after IP creation")
			framework.ExpectNotEqual(initialV4UsingIPRange, afterCreateSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after IP creation")
			framework.ExpectNotEqual(initialV6AvailableIPRange, afterCreateSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after IP creation")
			framework.ExpectNotEqual(initialV6UsingIPRange, afterCreateSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after IP creation")
		}

		// Store the status after creation for later comparison
		afterCreateV4AvailableIPs := afterCreateSubnet.Status.V4AvailableIPs
		afterCreateV4UsingIPs := afterCreateSubnet.Status.V4UsingIPs
		afterCreateV6AvailableIPs := afterCreateSubnet.Status.V6AvailableIPs
		afterCreateV6UsingIPs := afterCreateSubnet.Status.V6UsingIPs
		afterCreateV4AvailableIPRange := afterCreateSubnet.Status.V4AvailableIPRange
		afterCreateV4UsingIPRange := afterCreateSubnet.Status.V4UsingIPRange
		afterCreateV6AvailableIPRange := afterCreateSubnet.Status.V6AvailableIPRange
		afterCreateV6UsingIPRange := afterCreateSubnet.Status.V6UsingIPRange

		ginkgo.By("7. Delete the pod to trigger IP CR deletion")
		podClient.DeleteSync(podName)

		ginkgo.By("8. Wait for IP CR to be deleted")
		deleted := false
		for range 30 {
			_, err := f.KubeOVNClientSet.KubeovnV1().IPs().Get(context.Background(), ipName, metav1.GetOptions{})
			if err != nil && k8serrors.IsNotFound(err) {
				deleted = true
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectTrue(deleted, "IP CR should be deleted")

		ginkgo.By("9. Wait for subnet status to be updated after IP deletion")
		time.Sleep(5 * time.Second)

		ginkgo.By("10. Verify subnet status after IP CR deletion")
		afterDeleteSubnet := subnetClient.Get(subnetName)
		switch afterDeleteSubnet.Spec.Protocol {
		case apiv1.ProtocolIPv4:
			// Verify IP count is restored
			framework.ExpectEqual(afterCreateV4AvailableIPs+1, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should increase by 1 after IP deletion")
			framework.ExpectEqual(afterCreateV4UsingIPs-1, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should decrease by 1 after IP deletion")

			// Verify IP range changed
			framework.ExpectNotEqual(afterCreateV4AvailableIPRange, afterDeleteSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after IP deletion")
			framework.ExpectNotEqual(afterCreateV4UsingIPRange, afterDeleteSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after IP deletion")

			// Verify counts match initial state
			framework.ExpectEqual(initialV4AvailableIPs, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should return to initial value after IP deletion")
			framework.ExpectEqual(initialV4UsingIPs, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should return to initial value after IP deletion")
		case apiv1.ProtocolIPv6:
			// Verify IP count is restored
			framework.ExpectEqual(afterCreateV6AvailableIPs+1, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should increase by 1 after IP deletion")
			framework.ExpectEqual(afterCreateV6UsingIPs-1, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should decrease by 1 after IP deletion")

			// Verify IP range changed
			framework.ExpectNotEqual(afterCreateV6AvailableIPRange, afterDeleteSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after IP deletion")
			framework.ExpectNotEqual(afterCreateV6UsingIPRange, afterDeleteSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after IP deletion")

			// Verify counts match initial state
			framework.ExpectEqual(initialV6AvailableIPs, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should return to initial value after IP deletion")
			framework.ExpectEqual(initialV6UsingIPs, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should return to initial value after IP deletion")
		default:
			// Dual stack
			framework.ExpectEqual(afterCreateV4AvailableIPs+1, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should increase by 1 after IP deletion")
			framework.ExpectEqual(afterCreateV4UsingIPs-1, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should decrease by 1 after IP deletion")
			framework.ExpectEqual(afterCreateV6AvailableIPs+1, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should increase by 1 after IP deletion")
			framework.ExpectEqual(afterCreateV6UsingIPs-1, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should decrease by 1 after IP deletion")

			framework.ExpectNotEqual(afterCreateV4AvailableIPRange, afterDeleteSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after IP deletion")
			framework.ExpectNotEqual(afterCreateV4UsingIPRange, afterDeleteSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after IP deletion")
			framework.ExpectNotEqual(afterCreateV6AvailableIPRange, afterDeleteSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after IP deletion")
			framework.ExpectNotEqual(afterCreateV6UsingIPRange, afterDeleteSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after IP deletion")

			framework.ExpectEqual(initialV4AvailableIPs, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should return to initial value after IP deletion")
			framework.ExpectEqual(initialV4UsingIPs, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should return to initial value after IP deletion")
			framework.ExpectEqual(initialV6AvailableIPs, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should return to initial value after IP deletion")
			framework.ExpectEqual(initialV6UsingIPs, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should return to initial value after IP deletion")
		}

		ginkgo.By("11. Test completed: IP CR creation and deletion properly updates subnet status via finalizer handlers")
	})

	framework.ConformanceIt("Test multiple IPs with pod lifecycle", func() {
		f.SkipVersionPriorTo(1, 13, "This feature was introduced in v1.13")

		ginkgo.By("1. Get initial subnet status")
		initialSubnet := subnetClient.Get(subnetName)
		initialV4AvailableIPs := initialSubnet.Status.V4AvailableIPs
		initialV4UsingIPs := initialSubnet.Status.V4UsingIPs
		initialV6AvailableIPs := initialSubnet.Status.V6AvailableIPs
		initialV6UsingIPs := initialSubnet.Status.V6UsingIPs

		ginkgo.By("2. Create multiple pods to trigger multiple IP CR creations")
		numPods := 3
		podNames := make([]string, numPods)
		ipNames := make([]string, numPods)
		cmd := []string{"sleep", "infinity"}

		for i := range numPods {
			podName := fmt.Sprintf("test-multi-ip-pod-%d-%s", i, framework.RandomSuffix())
			podNames[i] = podName
			ipNames[i] = fmt.Sprintf("%s.%s", podName, namespaceName)

			pod := framework.MakePod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
			_ = podClient.CreateSync(pod)
		}

		ginkgo.By("3. Wait for all IP CRs to be created and get finalizers")
		for i := range numPods {
			var ipCR *apiv1.IP
			for range 60 {
				ipCR = ipClient.Get(ipNames[i])
				if ipCR != nil && ipCR.Name != "" && len(ipCR.Finalizers) > 0 {
					break
				}
				time.Sleep(1 * time.Second)
			}
			framework.ExpectNotNil(ipCR, "IP CR should be created for pod %s", podNames[i])
			framework.ExpectContainElement(ipCR.Finalizers, util.KubeOVNControllerFinalizer,
				"IP CR should have finalizer for pod %s", podNames[i])
		}

		ginkgo.By("4. Wait for subnet status to be updated after all IPs created")
		time.Sleep(5 * time.Second)

		ginkgo.By("5. Verify subnet status after multiple IP CR creations")
		afterCreateSubnet := subnetClient.Get(subnetName)
		switch afterCreateSubnet.Spec.Protocol {
		case apiv1.ProtocolIPv4:
			framework.ExpectEqual(initialV4AvailableIPs-float64(numPods), afterCreateSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should decrease by %d after creating %d IPs", numPods, numPods)
			framework.ExpectEqual(initialV4UsingIPs+float64(numPods), afterCreateSubnet.Status.V4UsingIPs,
				"V4UsingIPs should increase by %d after creating %d IPs", numPods, numPods)
		case apiv1.ProtocolIPv6:
			framework.ExpectEqual(initialV6AvailableIPs-float64(numPods), afterCreateSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should decrease by %d after creating %d IPs", numPods, numPods)
			framework.ExpectEqual(initialV6UsingIPs+float64(numPods), afterCreateSubnet.Status.V6UsingIPs,
				"V6UsingIPs should increase by %d after creating %d IPs", numPods, numPods)
		default:
			// Dual stack
			framework.ExpectEqual(initialV4AvailableIPs-float64(numPods), afterCreateSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should decrease by %d after creating %d IPs", numPods, numPods)
			framework.ExpectEqual(initialV4UsingIPs+float64(numPods), afterCreateSubnet.Status.V4UsingIPs,
				"V4UsingIPs should increase by %d after creating %d IPs", numPods, numPods)
			framework.ExpectEqual(initialV6AvailableIPs-float64(numPods), afterCreateSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should decrease by %d after creating %d IPs", numPods, numPods)
			framework.ExpectEqual(initialV6UsingIPs+float64(numPods), afterCreateSubnet.Status.V6UsingIPs,
				"V6UsingIPs should increase by %d after creating %d IPs", numPods, numPods)
		}

		ginkgo.By("6. Delete all pods to trigger IP CR deletions")
		for i := range numPods {
			podClient.DeleteSync(podNames[i])
		}

		ginkgo.By("7. Wait for all IP CRs to be deleted")
		for i := range numPods {
			deleted := false
			for range 30 {
				_, err := f.KubeOVNClientSet.KubeovnV1().IPs().Get(context.Background(), ipNames[i], metav1.GetOptions{})
				if err != nil && k8serrors.IsNotFound(err) {
					deleted = true
					break
				}
				time.Sleep(1 * time.Second)
			}
			framework.ExpectTrue(deleted, "IP CR %s should be deleted", ipNames[i])
		}

		ginkgo.By("8. Wait for subnet status to be updated after all IPs deleted")
		time.Sleep(5 * time.Second)

		ginkgo.By("9. Verify subnet status after multiple IP CR deletions")
		afterDeleteSubnet := subnetClient.Get(subnetName)
		switch afterDeleteSubnet.Spec.Protocol {
		case apiv1.ProtocolIPv4:
			framework.ExpectEqual(initialV4AvailableIPs, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should return to initial value after all IPs deleted")
			framework.ExpectEqual(initialV4UsingIPs, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should return to initial value after all IPs deleted")
		case apiv1.ProtocolIPv6:
			framework.ExpectEqual(initialV6AvailableIPs, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should return to initial value after all IPs deleted")
			framework.ExpectEqual(initialV6UsingIPs, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should return to initial value after all IPs deleted")
		default:
			// Dual stack
			framework.ExpectEqual(initialV4AvailableIPs, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should return to initial value after all IPs deleted")
			framework.ExpectEqual(initialV4UsingIPs, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should return to initial value after all IPs deleted")
			framework.ExpectEqual(initialV6AvailableIPs, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should return to initial value after all IPs deleted")
			framework.ExpectEqual(initialV6UsingIPs, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should return to initial value after all IPs deleted")
		}

		ginkgo.By("10. Test completed: Multiple IP CRs lifecycle properly updates subnet status")
	})

	framework.ConformanceIt("Test IP finalizer prevents premature deletion", func() {
		f.SkipVersionPriorTo(1, 13, "This feature was introduced in v1.13")

		ginkgo.By("1. Create a pod to trigger IP CR creation")
		podName := "test-ip-finalizer-pod-" + framework.RandomSuffix()
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
		_ = podClient.CreateSync(pod)

		ginkgo.By("2. Wait for IP CR to be created with finalizer")
		ipName := fmt.Sprintf("%s.%s", podName, namespaceName)
		var ipCR *apiv1.IP
		for range 60 {
			ipCR = ipClient.Get(ipName)
			if ipCR != nil && ipCR.Name != "" && len(ipCR.Finalizers) > 0 {
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectNotNil(ipCR, "IP CR should be created for pod %s", podName)
		framework.ExpectContainElement(ipCR.Finalizers, util.KubeOVNControllerFinalizer,
			"IP CR should have finalizer")

		ginkgo.By("3. Verify IP CR details")
		framework.ExpectEqual(ipCR.Spec.PodName, podName,
			"IP CR should have correct pod name")
		framework.ExpectEqual(ipCR.Spec.Namespace, namespaceName,
			"IP CR should have correct namespace")
		framework.ExpectEqual(ipCR.Spec.Subnet, subnetName,
			"IP CR should reference correct subnet")
		framework.ExpectNotEmpty(ipCR.Spec.V4IPAddress, "IP CR should have V4 or V6 address assigned")

		ginkgo.By("4. Delete the pod")
		podClient.DeleteSync(podName)

		ginkgo.By("5. Verify IP CR is eventually deleted")
		deleted := false
		for range 30 {
			_, err := f.KubeOVNClientSet.KubeovnV1().IPs().Get(context.Background(), ipName, metav1.GetOptions{})
			if err != nil && k8serrors.IsNotFound(err) {
				deleted = true
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectTrue(deleted, "IP CR should be deleted after pod deletion")

		ginkgo.By("6. Test completed: IP CR finalizer works correctly")
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
