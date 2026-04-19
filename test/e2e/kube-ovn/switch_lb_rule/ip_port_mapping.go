package switch_lb_rule

import (
	"bytes"
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

	// Parse output format from OVN CSV - can be in two formats:
	// Format 1 (space-separated): key1=value1 key2=value2
	// Format 2 (quoted): "{""key1""=""value1"", ""key2""=""value2""}"
	mappings := make(map[string]string)
	output = bytes.TrimSpace(output)
	if len(output) == 0 {
		return mappings, nil
	}

	// Strip surrounding quotes if present (OVN CSV format for map columns)
	if len(output) >= 2 && output[0] == '"' && output[len(output)-1] == '"' {
		output = output[1 : len(output)-1]
	}

	// Check for empty map representation: {} or empty string
	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" || outputStr == "{}" {
		return mappings, nil
	}

	// Determine format and parse accordingly
	var pairs []string
	if strings.HasPrefix(outputStr, "{") && strings.HasSuffix(outputStr, "}") {
		// Format 2: Quoted format with braces "{""key1""=""value1"", ""key2""=""value2""}"
		// Strip the surrounding braces
		outputStr = outputStr[1 : len(outputStr)-1]
		// Split by comma
		pairs = strings.Split(outputStr, ",")
	} else {
		// Format 1: Space-separated format "key1=value1 key2=value2"
		pairs = strings.Fields(outputStr)
	}

	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		// Split on the first = to separate key and value
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}

		// Trim quotes and spaces from key and value
		key := strings.Trim(strings.TrimSpace(parts[0]), `"`)
		value := strings.Trim(strings.TrimSpace(parts[1]), `"`)

		if key != "" {
			mappings[key] = value
		}
	}

	return mappings, nil
}

// getLoadBalancerNameForVPC returns the load balancer name for a VPC
// This follows the naming scheme in pkg/controller/vpc.go:239
func getLoadBalancerNameForVPC(vpcName string) string {
	return fmt.Sprintf("vpc-%s-tcp-load", vpcName)
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
		// Defensively clean up any remaining SLRs created by this spec before
		// deleting the subnet/VPC.  This prevents the subnet from getting stuck
		// when VIPs created by the endpoint_slice controller haven't been fully
		// cleaned up yet.  Filter by both suffix and namespace to avoid
		// interfering with other specs in the cluster.
		ginkgo.By("Cleaning up any remaining SwitchLBRules for suffix " + suffix)
		slrs, err := switchLBRuleClient.List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(ginkgo.GinkgoWriter, "failed to list SwitchLBRules for suffix %q during AfterEach cleanup: %v\n", suffix, err)
		} else {
			for i := range slrs.Items {
				if strings.Contains(slrs.Items[i].Name, suffix) && slrs.Items[i].Spec.Namespace == namespaceName {
					switchLBRuleClient.Delete(slrs.Items[i].Name)
				}
			}
			for i := range slrs.Items {
				if strings.Contains(slrs.Items[i].Name, suffix) && slrs.Items[i].Spec.Namespace == namespaceName {
					framework.ExpectNoError(
						switchLBRuleClient.WaitToDisappear(slrs.Items[i].Name, 0, 2*time.Minute),
						"failed to wait for SwitchLBRule %q to disappear before deleting subnet/VPC", slrs.Items[i].Name,
					)
				}
			}
		}

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)

		ginkgo.By("Deleting VPC " + vpcName)
		vpcClient.DeleteSync(vpcName)
	})

	ginkgo.It("should remove orphaned ip_port_mappings when backends are removed", func() {
		f.SkipVersionPriorTo(1, 12, "SwitchLBRule was introduced in v1.12")
		if !f.IsIPv4() {
			ginkgo.Skip("IPv6 is not supported for SwitchLBRule healthchecks yet")
		}

		var err error
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

		lbName := getLoadBalancerNameForVPC(vpcName)

		ginkgo.By("Waiting for initial 3 ip_port_mappings to be created")
		framework.WaitUntil(2*time.Second, 1*time.Minute, func(_ context.Context) (bool, error) {
			mappings, err := getLoadBalancerIPPortMappings(lbName)
			if err != nil {
				return false, nil
			}
			return len(mappings) == 3, nil
		}, "initial 3 ip_port_mappings are created")

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

		ginkgo.By("Waiting for orphaned ip_port_mapping to be removed")
		framework.WaitUntil(2*time.Second, 1*time.Minute, func(_ context.Context) (bool, error) {
			mappings, err := getLoadBalancerIPPortMappings(lbName)
			if err != nil {
				return false, nil
			}
			if len(mappings) == 2 {
				if _, exists := mappings[removedPodIP]; !exists {
					return true, nil
				}
			}
			return false, nil
		}, "orphaned ip_port_mapping is removed")

		ginkgo.By("Cleanup")
		switchLBRuleClient.DeleteSync(slrName)
		stsClient.Delete(stsName)
		framework.ExpectNoError(stsClient.WaitToDisappear(stsName, 0, 2*time.Minute))
	})

	ginkgo.It("should add new ip_port_mappings when backends are added", func() {
		f.SkipVersionPriorTo(1, 12, "SwitchLBRule was introduced in v1.12")
		if !f.IsIPv4() {
			ginkgo.Skip("IPv6 is not supported for SwitchLBRule healthchecks yet")
		}

		var err error
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

		lbName := getLoadBalancerNameForVPC(vpcName)

		ginkgo.By("Waiting for initial 1 ip_port_mapping to be created")
		framework.WaitUntil(2*time.Second, 1*time.Minute, func(_ context.Context) (bool, error) {
			mappings, err := getLoadBalancerIPPortMappings(lbName)
			if err != nil {
				return false, nil
			}
			return len(mappings) == 1, nil
		}, "initial 1 ip_port_mapping is created")

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

		pods := stsClient.GetPods(sts)

		ginkgo.By("Waiting for new ip_port_mappings to be added")
		framework.WaitUntil(2*time.Second, 1*time.Minute, func(_ context.Context) (bool, error) {
			mappings, err := getLoadBalancerIPPortMappings(lbName)
			if err != nil {
				return false, nil
			}
			if len(mappings) == 3 {
				// Verify all pod IPs are present
				for _, pod := range pods.Items {
					if _, exists := mappings[pod.Status.PodIP]; !exists {
						return false, nil
					}
				}
				return true, nil
			}
			return false, nil
		}, "new ip_port_mappings are added")

		ginkgo.By("Cleanup")
		switchLBRuleClient.DeleteSync(slrName)
		stsClient.Delete(stsName)
		framework.ExpectNoError(stsClient.WaitToDisappear(stsName, 0, 2*time.Minute))
	})

	ginkgo.It("should not delete shared backends when updating multiple SwitchLBRules", func() {
		f.SkipVersionPriorTo(1, 12, "SwitchLBRule was introduced in v1.12")
		if !f.IsIPv4() {
			ginkgo.Skip("IPv6 is not supported for SwitchLBRule healthchecks yet")
		}

		var err error
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

		lbName := getLoadBalancerNameForVPC(vpcName)

		ginkgo.By("Waiting for all 3 pod IPs to have ip_port_mappings")
		framework.WaitUntil(2*time.Second, 1*time.Minute, func(_ context.Context) (bool, error) {
			mappings, err := getLoadBalancerIPPortMappings(lbName)
			if err != nil {
				return false, nil
			}
			if len(mappings) == 3 {
				// Verify all pod IPs are present
				for _, pod := range pods.Items {
					if _, exists := mappings[pod.Status.PodIP]; !exists {
						return false, nil
					}
				}
				return true, nil
			}
			return false, nil
		}, "all 3 pod IPs have ip_port_mappings")

		// Update SLR1 to only use pod 0 (removing shared pod 1)
		ginkgo.By("Updating first SwitchLBRule to only use " + pods.Items[0].Status.PodIP)
		slr1, err = switchLBRuleClient.SwitchLBRuleInterface.Get(context.TODO(), slr1Name, metav1.GetOptions{})
		framework.ExpectNoError(err)
		modifiedSlr1 := slr1.DeepCopy()
		modifiedSlr1.Spec.Endpoints = []string{pods.Items[0].Status.PodIP}
		_ = switchLBRuleClient.Patch(slr1, modifiedSlr1)

		ginkgo.By("Verifying shared backend (pod 1) was NOT removed")
		framework.WaitUntil(2*time.Second, 1*time.Minute, func(_ context.Context) (bool, error) {
			mappings, err := getLoadBalancerIPPortMappings(lbName)
			if err != nil {
				return false, nil
			}
			// Verify all 3 pods still have mappings (shared pod should not be removed)
			if len(mappings) == 3 {
				if _, exists := mappings[pods.Items[0].Status.PodIP]; !exists {
					return false, nil
				}
				if _, exists := mappings[pods.Items[1].Status.PodIP]; !exists {
					return false, nil
				}
				if _, exists := mappings[pods.Items[2].Status.PodIP]; !exists {
					return false, nil
				}
				return true, nil
			}
			return false, nil
		}, "shared backend pod 1 is not removed")

		ginkgo.By("Cleanup")
		switchLBRuleClient.DeleteSync(slr1Name)
		switchLBRuleClient.DeleteSync(slr2Name)
		stsClient.Delete(stsName)
		framework.ExpectNoError(stsClient.WaitToDisappear(stsName, 0, 2*time.Minute))
	})
})
