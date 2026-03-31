package switch_lb_rule

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

// getLoadBalancerIPPortMappings queries OVN NB database for ip_port_mappings of a specific load balancer
func getLoadBalancerIPPortMappings(lbName string) (map[string]string, error) {
	cmd := fmt.Sprintf("ovn-nbctl --format=csv --data=bare --no-heading --columns=ip_port_mappings list Load_Balancer %s", lbName)
	output, _, err := framework.NBExec(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to query load balancer %s: %w", lbName, err)
	}

	// Parse output: format is "key1=value1 key2=value2 ..."
	mappings := make(map[string]string)
	if strings.TrimSpace(string(output)) == "" {
		return mappings, nil
	}

	// Split by space to get individual key=value pairs
	pairs := strings.FieldsSeq(string(output))
	for pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			mappings[parts[0]] = parts[1]
		}
	}

	return mappings, nil
}

// getLSPNameForPodIP gets the logical switch port name for a pod IP
func getLSPNameForPodIP(podIP string) (string, error) {
	// Query for the port with this IP address
	cmd := fmt.Sprintf("ovn-nbctl --format=csv --data=bare --no-heading --columns=name find Logical_Switch_Port addresses~=%s", podIP)
	output, _, err := framework.NBExec(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to find LSP for IP %s: %w", podIP, err)
	}

	lspName := strings.TrimSpace(string(output))
	if lspName == "" {
		return "", fmt.Errorf("no LSP found for IP %s", podIP)
	}

	return lspName, nil
}

// getLoadBalancerNameForVPC returns the load balancer name for a VPC
func getLoadBalancerNameForVPC(vpcName string) string {
	return fmt.Sprintf("cluster-tcp-loadbalancer-%s", vpcName)
}

var _ = framework.Describe("[group:slr-ip-port-mapping]", func() {
	f := framework.NewDefaultFramework("slr-ip-port-mapping")

	var (
		switchLBRuleClient *framework.SwitchLBRuleClient
		stsClient          *framework.StatefulSetClient
		podClient          *framework.PodClient
		subnetClient       *framework.SubnetClient
		vpcClient          *framework.VpcClient
		serviceClient      *framework.ServiceClient

		namespaceName, suffix              string
		vpcName, subnetName, overlaySubnet string
	)

	ginkgo.BeforeEach(func() {
		switchLBRuleClient = f.SwitchLBRuleClient()
		stsClient = f.StatefulSetClient()
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		vpcClient = f.VpcClient()
		serviceClient = f.ServiceClient()

		suffix = framework.RandomSuffix()
		namespaceName = f.Namespace.Name
		vpcName = generateVpcName(suffix)
		subnetName = generateSubnetName(suffix)
		overlaySubnet = framework.RandomCIDR(f.ClusterIPFamily)

		ginkgo.By("Creating custom VPC " + vpcName)
		vpc := framework.MakeVpc(vpcName, "", false, false, []string{namespaceName})
		_ = vpcClient.CreateSync(vpc)

		ginkgo.By("Creating custom subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, "", overlaySubnet, "", vpcName, "", nil, nil, nil)
		_ = subnetClient.CreateSync(subnet)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)

		ginkgo.By("Deleting VPC " + vpcName)
		vpcClient.DeleteSync(vpcName)
	})

	ginkgo.It("should update ip_port_mappings when LSPs change (pod restart)", func() {
		f.SkipVersionPriorTo(1, 12, "SwitchLBRule was introduced in v1.12")

		slrName := "slr-lsp-update-" + suffix
		svcName := generateServiceName(slrName)
		stsName := "sts-" + suffix
		label := "app-lsp-update"

		ginkgo.By("Creating statefulset with 2 replicas")
		labels := map[string]string{"app": label}
		sts := framework.MakeStatefulSet(stsName, stsName, 2, labels, framework.AgnhostImage)
		sts.Spec.Template.Annotations = map[string]string{util.LogicalSwitchAnnotation: subnetName}
		sts.Spec.Template.Spec.Containers[0].Command = []string{"/agnhost", "netexec", "--http-port", "80"}
		_ = stsClient.CreateSync(sts)

		// Get initial pods
		pods1 := stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods1.Items, 2)

		ginkgo.By("Creating service to get VIP")
		ports := []corev1.ServicePort{{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       8080,
			TargetPort: intstr.FromInt32(80),
		}}
		svc := framework.MakeService(svcName, corev1.ServiceTypeClusterIP,
			map[string]string{util.LogicalSwitchAnnotation: subnetName},
			labels, ports, corev1.ServiceAffinityNone)
		svc = serviceClient.CreateSync(svc, func(s *corev1.Service) (bool, error) {
			return len(s.Spec.ClusterIPs) != 0, nil
		}, "cluster ips are not empty")
		vip := svc.Spec.ClusterIPs[0]

		ginkgo.By("Creating SwitchLBRule")
		slrPorts := []kubeovnv1.SwitchLBRulePort{{
			Name:       "http",
			Port:       8080,
			TargetPort: 80,
			Protocol:   "TCP",
		}}
		slr := framework.MakeSwitchLBRule(slrName, namespaceName, vip, corev1.ServiceAffinityNone,
			map[string]string{util.LogicalSwitchAnnotation: subnetName},
			[]string{"app:" + label}, nil, slrPorts)
		_ = switchLBRuleClient.Create(slr)

		// Wait for mappings to be created
		time.Sleep(5 * time.Second)

		lbName := getLoadBalancerNameForVPC(vpcName)

		ginkgo.By("Getting initial ip_port_mappings")
		initialMappings, err := getLoadBalancerIPPortMappings(lbName)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(initialMappings, 2, "should have 2 initial mappings")

		// Record initial LSP names for each pod IP
		initialLSPs := make(map[string]string)
		for _, pod := range pods1.Items {
			lspName, err := getLSPNameForPodIP(pod.Status.PodIP)
			framework.ExpectNoError(err)
			initialLSPs[pod.Status.PodIP] = lspName
			framework.ExpectHaveKey(initialMappings, pod.Status.PodIP, "mapping should exist for pod IP")
		}

		ginkgo.By("Deleting pods to trigger recreation (simulating pod restart)")
		for _, pod := range pods1.Items {
			podClient.DeleteSync(pod.Name)
		}

		ginkgo.By("Waiting for pods to be recreated")
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			pods := stsClient.GetPods(sts)
			if len(pods.Items) != 2 {
				return false, nil
			}
			for _, pod := range pods.Items {
				if pod.Status.Phase != corev1.PodRunning {
					return false, nil
				}
			}
			return true, nil
		}, "pods are recreated and running")

		// Wait for endpoint controller to update
		time.Sleep(10 * time.Second)

		ginkgo.By("Verifying ip_port_mappings were updated with new LSPs")
		updatedMappings, err := getLoadBalancerIPPortMappings(lbName)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(updatedMappings, 2, "should still have 2 mappings")

		pods2 := stsClient.GetPods(sts)
		for _, pod := range pods2.Items {
			newLSPName, err := getLSPNameForPodIP(pod.Status.PodIP)
			framework.ExpectNoError(err)

			// Verify mapping exists for this IP
			framework.ExpectHaveKey(updatedMappings, pod.Status.PodIP, "mapping should exist for new pod IP")

			// Verify the LSP name in the mapping matches the current LSP
			mapping := updatedMappings[pod.Status.PodIP]
			framework.ExpectTrue(strings.Contains(mapping, newLSPName), "mapping should contain new LSP name %s, got %s", newLSPName, mapping)

			// If pod IP is the same as before, verify LSP name changed
			if oldLSP, existed := initialLSPs[pod.Status.PodIP]; existed {
				framework.ExpectNotEqual(oldLSP, newLSPName, "LSP should have changed for same IP")
			}
		}

		ginkgo.By("Cleanup")
		switchLBRuleClient.DeleteSync(slrName)
		serviceClient.Delete(svcName)
		stsClient.Delete(stsName)
		framework.ExpectNoError(stsClient.WaitToDisappear(stsName, 0, 2*time.Minute))
	})

	ginkgo.It("should remove orphaned ip_port_mappings when backends are removed", func() {
		f.SkipVersionPriorTo(1, 12, "SwitchLBRule was introduced in v1.12")

		slrName := "slr-orphan-" + suffix
		svcName := generateServiceName(slrName)
		stsName := "sts-" + suffix
		label := "app-orphan"

		ginkgo.By("Creating statefulset with 3 replicas")
		labels := map[string]string{"app": label}
		sts := framework.MakeStatefulSet(stsName, stsName, 3, labels, framework.AgnhostImage)
		sts.Spec.Template.Annotations = map[string]string{util.LogicalSwitchAnnotation: subnetName}
		sts.Spec.Template.Spec.Containers[0].Command = []string{"/agnhost", "netexec", "--http-port", "80"}
		_ = stsClient.CreateSync(sts)

		pods := stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, 3)

		ginkgo.By("Creating service to get VIP")
		ports := []corev1.ServicePort{{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       8080,
			TargetPort: intstr.FromInt32(80),
		}}
		svc := framework.MakeService(svcName, corev1.ServiceTypeClusterIP,
			map[string]string{util.LogicalSwitchAnnotation: subnetName},
			labels, ports, corev1.ServiceAffinityNone)
		svc = serviceClient.CreateSync(svc, func(s *corev1.Service) (bool, error) {
			return len(s.Spec.ClusterIPs) != 0, nil
		}, "cluster ips are not empty")
		vip := svc.Spec.ClusterIPs[0]

		ginkgo.By("Creating SwitchLBRule")
		slrPorts := []kubeovnv1.SwitchLBRulePort{{
			Name:       "http",
			Port:       8080,
			TargetPort: 80,
			Protocol:   "TCP",
		}}
		slr := framework.MakeSwitchLBRule(slrName, namespaceName, vip, corev1.ServiceAffinityNone,
			map[string]string{util.LogicalSwitchAnnotation: subnetName},
			[]string{"app:" + label}, nil, slrPorts)
		_ = switchLBRuleClient.Create(slr)

		time.Sleep(5 * time.Second)

		lbName := getLoadBalancerNameForVPC(vpcName)

		ginkgo.By("Verifying initial 3 ip_port_mappings")
		initialMappings, err := getLoadBalancerIPPortMappings(lbName)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(initialMappings, 3, "should have 3 initial mappings")

		// Record the IP that will be removed
		removedPodName := pods.Items[2].Name
		removedPodIP := pods.Items[2].Status.PodIP

		ginkgo.By("Scaling down statefulset to 2 replicas")
		sts, err = stsClient.StatefulSetInterface.Get(context.TODO(), stsName, metav1.GetOptions{})
		framework.ExpectNoError(err)
		replicas := int32(2)
		sts.Spec.Replicas = &replicas
		_, err = stsClient.Update(context.TODO(), sts, metav1.UpdateOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Waiting for pod to be deleted")
		podClient.WaitForNotFound(removedPodName)

		// Wait for endpoint controller to update
		time.Sleep(10 * time.Second)

		ginkgo.By("Verifying orphaned ip_port_mapping was removed")
		updatedMappings, err := getLoadBalancerIPPortMappings(lbName)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(updatedMappings, 2, "should have 2 mappings after scale down")
		framework.ExpectNotHaveKey(updatedMappings, removedPodIP, "removed pod IP should not be in mappings")

		ginkgo.By("Cleanup")
		switchLBRuleClient.DeleteSync(slrName)
		serviceClient.Delete(svcName)
		stsClient.Delete(stsName)
		framework.ExpectNoError(stsClient.WaitToDisappear(stsName, 0, 2*time.Minute))
	})

	ginkgo.It("should add new ip_port_mappings when backends are added", func() {
		f.SkipVersionPriorTo(1, 12, "SwitchLBRule was introduced in v1.12")

		slrName := "slr-add-" + suffix
		svcName := generateServiceName(slrName)
		stsName := "sts-" + suffix
		label := "app-add"

		ginkgo.By("Creating statefulset with 1 replica")
		labels := map[string]string{"app": label}
		sts := framework.MakeStatefulSet(stsName, stsName, 1, labels, framework.AgnhostImage)
		sts.Spec.Template.Annotations = map[string]string{util.LogicalSwitchAnnotation: subnetName}
		sts.Spec.Template.Spec.Containers[0].Command = []string{"/agnhost", "netexec", "--http-port", "80"}
		_ = stsClient.CreateSync(sts)

		ginkgo.By("Creating service to get VIP")
		ports := []corev1.ServicePort{{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       8080,
			TargetPort: intstr.FromInt32(80),
		}}
		svc := framework.MakeService(svcName, corev1.ServiceTypeClusterIP,
			map[string]string{util.LogicalSwitchAnnotation: subnetName},
			labels, ports, corev1.ServiceAffinityNone)
		svc = serviceClient.CreateSync(svc, func(s *corev1.Service) (bool, error) {
			return len(s.Spec.ClusterIPs) != 0, nil
		}, "cluster ips are not empty")
		vip := svc.Spec.ClusterIPs[0]

		ginkgo.By("Creating SwitchLBRule")
		slrPorts := []kubeovnv1.SwitchLBRulePort{{
			Name:       "http",
			Port:       8080,
			TargetPort: 80,
			Protocol:   "TCP",
		}}
		slr := framework.MakeSwitchLBRule(slrName, namespaceName, vip, corev1.ServiceAffinityNone,
			map[string]string{util.LogicalSwitchAnnotation: subnetName},
			[]string{"app:" + label}, nil, slrPorts)
		_ = switchLBRuleClient.Create(slr)

		time.Sleep(5 * time.Second)

		lbName := getLoadBalancerNameForVPC(vpcName)

		ginkgo.By("Verifying initial 1 ip_port_mapping")
		initialMappings, err := getLoadBalancerIPPortMappings(lbName)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(initialMappings, 1, "should have 1 initial mapping")

		ginkgo.By("Scaling up statefulset to 3 replicas")
		sts, err = stsClient.StatefulSetInterface.Get(context.TODO(), stsName, metav1.GetOptions{})
		framework.ExpectNoError(err)
		replicas := int32(3)
		sts.Spec.Replicas = &replicas
		_, err = stsClient.Update(context.TODO(), sts, metav1.UpdateOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Waiting for new pods to be ready")
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			pods := stsClient.GetPods(sts)
			if len(pods.Items) != 3 {
				return false, nil
			}
			for _, pod := range pods.Items {
				if pod.Status.Phase != corev1.PodRunning {
					return false, nil
				}
			}
			return true, nil
		}, "new pods are ready")

		// Wait for endpoint controller to update
		time.Sleep(10 * time.Second)

		ginkgo.By("Verifying new ip_port_mappings were added")
		updatedMappings, err := getLoadBalancerIPPortMappings(lbName)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(updatedMappings, 3, "should have 3 mappings after scale up")

		pods := stsClient.GetPods(sts)
		for _, pod := range pods.Items {
			framework.ExpectHaveKey(updatedMappings, pod.Status.PodIP, "mapping should exist for pod IP %s", pod.Status.PodIP)
		}

		ginkgo.By("Cleanup")
		switchLBRuleClient.DeleteSync(slrName)
		serviceClient.Delete(svcName)
		stsClient.Delete(stsName)
		framework.ExpectNoError(stsClient.WaitToDisappear(stsName, 0, 2*time.Minute))
	})

	ginkgo.It("should not delete shared backends when updating multiple SwitchLBRules", func() {
		f.SkipVersionPriorTo(1, 12, "SwitchLBRule was introduced in v1.12")

		slr1Name := "slr-shared-1-" + suffix
		slr2Name := "slr-shared-2-" + suffix
		svc1Name := generateServiceName(slr1Name)
		svc2Name := generateServiceName(slr2Name)
		stsName := "sts-shared-" + suffix
		label := "app-shared"

		ginkgo.By("Creating statefulset with 3 replicas")
		labels := map[string]string{"app": label}
		sts := framework.MakeStatefulSet(stsName, stsName, 3, labels, framework.AgnhostImage)
		sts.Spec.Template.Annotations = map[string]string{util.LogicalSwitchAnnotation: subnetName}
		sts.Spec.Template.Spec.Containers[0].Command = []string{"/agnhost", "netexec", "--http-port", "80"}
		_ = stsClient.CreateSync(sts)

		pods := stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, 3)

		// Create two services with different VIPs
		ginkgo.By("Creating first service")
		ports1 := []corev1.ServicePort{{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       8081,
			TargetPort: intstr.FromInt32(80),
		}}
		svc1 := framework.MakeService(svc1Name, corev1.ServiceTypeClusterIP,
			map[string]string{util.LogicalSwitchAnnotation: subnetName},
			labels, ports1, corev1.ServiceAffinityNone)
		svc1 = serviceClient.CreateSync(svc1, func(s *corev1.Service) (bool, error) {
			return len(s.Spec.ClusterIPs) != 0, nil
		}, "cluster ips are not empty")
		vip1 := svc1.Spec.ClusterIPs[0]

		ginkgo.By("Creating second service")
		ports2 := []corev1.ServicePort{{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       8082,
			TargetPort: intstr.FromInt32(80),
		}}
		svc2 := framework.MakeService(svc2Name, corev1.ServiceTypeClusterIP,
			map[string]string{util.LogicalSwitchAnnotation: subnetName},
			labels, ports2, corev1.ServiceAffinityNone)
		svc2 = serviceClient.CreateSync(svc2, func(s *corev1.Service) (bool, error) {
			return len(s.Spec.ClusterIPs) != 0, nil
		}, "cluster ips are not empty")
		vip2 := svc2.Spec.ClusterIPs[0]

		// SLR1 will use pods 0 and 1 (overlapping with SLR2 on pod 1)
		ginkgo.By("Creating first SwitchLBRule with endpoints " + pods.Items[0].Status.PodIP + " and " + pods.Items[1].Status.PodIP)
		slr1Ports := []kubeovnv1.SwitchLBRulePort{{
			Name:       "http",
			Port:       8081,
			TargetPort: 80,
			Protocol:   "TCP",
		}}
		slr1 := framework.MakeSwitchLBRule(slr1Name, namespaceName, vip1, corev1.ServiceAffinityNone,
			map[string]string{util.LogicalSwitchAnnotation: subnetName},
			nil, []string{pods.Items[0].Status.PodIP, pods.Items[1].Status.PodIP}, slr1Ports)
		_ = switchLBRuleClient.Create(slr1)

		// SLR2 will use pods 1 and 2 (overlapping with SLR1 on pod 1)
		ginkgo.By("Creating second SwitchLBRule with endpoints " + pods.Items[1].Status.PodIP + " and " + pods.Items[2].Status.PodIP)
		slr2Ports := []kubeovnv1.SwitchLBRulePort{{
			Name:       "http",
			Port:       8082,
			TargetPort: 80,
			Protocol:   "TCP",
		}}
		slr2 := framework.MakeSwitchLBRule(slr2Name, namespaceName, vip2, corev1.ServiceAffinityNone,
			map[string]string{util.LogicalSwitchAnnotation: subnetName},
			nil, []string{pods.Items[1].Status.PodIP, pods.Items[2].Status.PodIP}, slr2Ports)
		_ = switchLBRuleClient.Create(slr2)

		time.Sleep(5 * time.Second)

		lbName := getLoadBalancerNameForVPC(vpcName)

		ginkgo.By("Verifying all 3 pod IPs have ip_port_mappings")
		initialMappings, err := getLoadBalancerIPPortMappings(lbName)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(initialMappings, 3, "should have mappings for all 3 pods")
		for _, pod := range pods.Items {
			framework.ExpectHaveKey(initialMappings, pod.Status.PodIP)
		}

		// Update SLR1 to only use pod 0 (removing shared pod 1)
		ginkgo.By("Updating first SwitchLBRule to only use " + pods.Items[0].Status.PodIP)
		slr1, err = switchLBRuleClient.SwitchLBRuleInterface.Get(context.TODO(), slr1Name, metav1.GetOptions{})
		framework.ExpectNoError(err)
		modifiedSlr1 := slr1.DeepCopy()
		modifiedSlr1.Spec.Endpoints = []string{pods.Items[0].Status.PodIP}
		_ = switchLBRuleClient.Patch(slr1, modifiedSlr1)

		// Wait for update to propagate
		time.Sleep(10 * time.Second)

		ginkgo.By("Verifying shared backend (pod 1) was NOT removed")
		updatedMappings, err := getLoadBalancerIPPortMappings(lbName)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(updatedMappings, 3, "should still have 3 mappings")
		framework.ExpectHaveKey(updatedMappings, pods.Items[0].Status.PodIP, "pod 0 should remain")
		framework.ExpectHaveKey(updatedMappings, pods.Items[1].Status.PodIP, "shared pod 1 should remain (still used by SLR2)")
		framework.ExpectHaveKey(updatedMappings, pods.Items[2].Status.PodIP, "pod 2 should remain")

		ginkgo.By("Cleanup")
		switchLBRuleClient.DeleteSync(slr1Name)
		switchLBRuleClient.DeleteSync(slr2Name)
		serviceClient.Delete(svc1Name)
		serviceClient.Delete(svc2Name)
		stsClient.Delete(stsName)
		framework.ExpectNoError(stsClient.WaitToDisappear(stsName, 0, 2*time.Minute))
	})
})
