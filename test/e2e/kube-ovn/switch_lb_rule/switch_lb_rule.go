package switch_lb_rule

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func generateSwitchLBRuleName(ruleName string) string {
	return "lr-" + ruleName
}

func generateServiceName(slrName string) string {
	return "slr-" + slrName
}

func generateVpcName(name string) string {
	return "vpc-" + name
}

func generateSubnetName(name string) string {
	return "subnet-" + name
}

func curlSvc(f *framework.Framework, clientPodName, vip string, port int32) {
	ginkgo.GinkgoHelper()
	cmd := "curl -q -s --connect-timeout 5 --max-time 5 " + util.JoinHostPort(vip, port)
	ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, f.Namespace.Name, clientPodName))
	framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		_, err := e2epodoutput.RunHostCmd(f.Namespace.Name, clientPodName, cmd)
		return err == nil, nil
	}, fmt.Sprintf("%s:%d is reachable", vip, port))
}

func isMultusInstalled(f *framework.Framework) bool {
	_, err := f.ExtClientSet.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), "network-attachment-definitions.k8s.cni.cncf.io", metav1.GetOptions{})
	return err == nil
}

var _ = framework.Describe("[group:slr]", func() {
	f := framework.NewDefaultFramework("slr")

	var (
		switchLBRuleClient *framework.SwitchLBRuleClient
		endpointsClient    *framework.EndpointsClient
		serviceClient      *framework.ServiceClient
		stsClient          *framework.StatefulSetClient
		podClient          *framework.PodClient
		subnetClient       *framework.SubnetClient
		vpcClient          *framework.VpcClient
		nadClient          *framework.NetworkAttachmentDefinitionClient

		namespaceName, suffix                     string
		vpcName, subnetName, clientPodName, label string
		stsName, stsSvcName                       string
		selSlrName, selSvcName                    string
		epSlrName, epSvcName                      string
		nadName                                   string
		overlaySubnetCidr, vip                    string
		// TODO:// slr support dual-stack
		frontPort, selSlrFrontPort, epSlrFrontPort, backendPort int32
	)

	ginkgo.BeforeEach(func() {
		switchLBRuleClient = f.SwitchLBRuleClient()
		endpointsClient = f.EndpointClient()
		serviceClient = f.ServiceClient()
		stsClient = f.StatefulSetClient()
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		vpcClient = f.VpcClient()
		nadClient = f.NetworkAttachmentDefinitionClient()
		suffix = framework.RandomSuffix()
		namespaceName = f.Namespace.Name
		selSlrName = "sel-" + generateSwitchLBRuleName(suffix)
		selSvcName = generateServiceName(selSlrName)
		epSlrName = "ep-" + generateSwitchLBRuleName(suffix)
		epSvcName = generateServiceName(epSlrName)
		stsName = "sts-" + suffix
		stsSvcName = stsName
		label = "slr"
		clientPodName = "client-" + suffix
		subnetName = generateSubnetName(suffix)
		nadName = subnetName
		vpcName = generateVpcName(suffix)
		frontPort = 8090
		selSlrFrontPort = 8091
		epSlrFrontPort = 8092
		backendPort = 80
		vip = ""
		overlaySubnetCidr = framework.RandomCIDR(f.ClusterIPFamily)
		ginkgo.By("Creating custom vpc")
		vpc := framework.MakeVpc(vpcName, "", false, false, []string{namespaceName})
		_ = vpcClient.CreateSync(vpc)
	})

	ginkgo.AfterEach(func() {
		// Level 1: Initiate all independent deletes
		ginkgo.By("Deleting client pod " + clientPodName)
		podClient.DeleteGracefully(clientPodName)
		ginkgo.By("Deleting statefulset " + stsName)
		stsClient.Delete(stsName)
		ginkgo.By("Deleting switch-lb-rule " + selSlrName)
		switchLBRuleClient.Delete(selSlrName)
		ginkgo.By("Deleting switch-lb-rule " + epSlrName)
		switchLBRuleClient.Delete(epSlrName)
		ginkgo.By("Deleting service " + stsSvcName)
		serviceClient.Delete(stsSvcName)
		ginkgo.By("Deleting network attachment definition " + nadName)
		nadClient.Delete(nadName)

		// Level 1: Wait for all to disappear
		podClient.WaitForNotFound(clientPodName)
		framework.ExpectNoError(stsClient.WaitToDisappear(stsName, 0, 2*time.Minute))
		framework.ExpectNoError(switchLBRuleClient.WaitToDisappear(selSlrName, 0, 2*time.Minute))
		framework.ExpectNoError(switchLBRuleClient.WaitToDisappear(epSlrName, 0, 2*time.Minute))
		framework.ExpectNoError(serviceClient.WaitToDisappear(stsSvcName, 0, 2*time.Minute))

		// Level 2: Subnet (needs workloads deleted first)
		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)

		// Level 3: VPC (needs subnet deleted first)
		ginkgo.By("Deleting vpc " + vpcName)
		vpcClient.DeleteSync(vpcName)
	})

	ginkgo.DescribeTable("Test SLR connectivity", ginkgo.Label("Conformance"), func(customProvider bool) {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")

		var provider string
		annotations := make(map[string]string)

		if customProvider {
			f.SkipVersionPriorTo(1, 15, "This feature was introduced in v1.15")

			if !isMultusInstalled(f) { // Multus must be installed for some tests
				ginkgo.Skip("Multus must be activated to run the SLR tests with custom providers")
			}

			provider = fmt.Sprintf("%s.%s.%s", subnetName, f.Namespace.Name, util.OvnProvider)
			annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)] = subnetName
			annotations[util.DefaultNetworkAnnotation] = fmt.Sprintf("%s/%s", f.Namespace.Name, nadName)
		} else {
			annotations[util.LogicalSwitchAnnotation] = subnetName
		}

		ginkgo.By("Creating custom overlay subnet")

		overlaySubnet := framework.MakeSubnet(subnetName, "", overlaySubnetCidr, "", vpcName, provider, nil, nil, nil)
		_ = subnetClient.CreateSync(overlaySubnet)

		if customProvider {
			nad := framework.MakeOVNNetworkAttachmentDefinition(subnetName, f.Namespace.Namespace, provider, []request.Route{})
			nadClient.Create(nad)
		}

		ginkgo.By("Creating client pod " + clientPodName)
		newClientPod := framework.MakePod(namespaceName, clientPodName, nil, annotations, framework.AgnhostImage, nil, nil)
		podClient.CreateSync(newClientPod)

		ginkgo.By("1. Creating sts svc with slr")
		var (
			clientPod             *corev1.Pod
			err                   error
			stsSvc, selSvc, epSvc *corev1.Service
			selSlrEps, epSlrEps   *corev1.Endpoints
		)
		replicas := 1
		labels := map[string]string{"app": label}
		ginkgo.By("Creating statefulset " + stsName + " with subnet " + subnetName)
		sts := framework.MakeStatefulSet(stsName, stsSvcName, int32(replicas), labels, framework.AgnhostImage)
		ginkgo.By("Creating sts " + stsName)

		sts.Spec.Template.Annotations = annotations

		portStr := strconv.Itoa(80)
		webServerCmd := []string{"/agnhost", "netexec", "--http-port", portStr}
		sts.Spec.Template.Spec.Containers[0].Command = webServerCmd
		_ = stsClient.CreateSync(sts)
		ginkgo.By("Creating service " + stsSvcName)
		ports := []corev1.ServicePort{{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       frontPort,
			TargetPort: intstr.FromInt32(80),
		}}
		selector := map[string]string{"app": label}
		svcAnnotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		stsSvc = framework.MakeService(stsSvcName, corev1.ServiceTypeClusterIP, svcAnnotations, selector, ports, corev1.ServiceAffinityNone)
		stsSvc = serviceClient.CreateSync(stsSvc, func(s *corev1.Service) (bool, error) {
			return len(s.Spec.ClusterIPs) != 0, nil
		}, "cluster ips are not empty")

		ginkgo.By("Waiting for sts service " + stsSvcName + " to be ready")
		framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
			stsSvc, err = serviceClient.ServiceInterface.Get(context.TODO(), stsSvcName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("service %s is created", stsSvcName))
		framework.ExpectNotNil(stsSvc)

		ginkgo.By("Get client pod " + clientPodName)
		clientPod, err = podClient.Get(context.TODO(), clientPodName, metav1.GetOptions{})
		framework.ExpectNil(err)
		framework.ExpectNotNil(clientPod)
		ginkgo.By("Checking sts service " + stsSvc.Name)
		for _, ip := range stsSvc.Spec.ClusterIPs {
			curlSvc(f, clientPodName, ip, frontPort)
		}
		vip = stsSvc.Spec.ClusterIP

		ginkgo.By("2. Creating switch-lb-rule with selector with lb front vip " + vip)
		ginkgo.By("Creating selector SwitchLBRule " + epSlrName)
		var (
			selRule           *kubeovnv1.SwitchLBRule
			slrSelector       []string
			slrPorts, epPorts []kubeovnv1.SwitchLBRulePort
			sessionAffinity   corev1.ServiceAffinity
		)
		sessionAffinity = corev1.ServiceAffinityNone
		slrPorts = []kubeovnv1.SwitchLBRulePort{
			{
				Name:       "http",
				Port:       selSlrFrontPort,
				TargetPort: backendPort,
				Protocol:   "TCP",
			},
		}
		slrSelector = []string{"app:" + label}
		selRule = framework.MakeSwitchLBRule(selSlrName, namespaceName, vip, sessionAffinity, nil, slrSelector, nil, slrPorts)
		_ = switchLBRuleClient.Create(selRule)

		ginkgo.By("Waiting for switch-lb-rule " + selSlrName + " to be ready")
		framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, err = switchLBRuleClient.SwitchLBRuleInterface.Get(context.TODO(), selSlrName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("switch-lb-rule %s is created", selSlrName))

		ginkgo.By("Waiting for headless service " + selSvcName + " to be ready")
		framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
			selSvc, err = serviceClient.ServiceInterface.Get(context.TODO(), selSvcName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("service %s is created", selSvcName))
		framework.ExpectNotNil(selSvc)

		ginkgo.By("Waiting for endpoints " + selSvcName + " to be ready")
		framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
			selSlrEps, err = endpointsClient.EndpointsInterface.Get(context.TODO(), selSvcName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("endpoints %s is created", selSvcName))
		framework.ExpectNotNil(selSlrEps)

		pods := stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, replicas)

		for i, subset := range selSlrEps.Subsets {
			var (
				ips       []string
				tps       []int32
				protocols = make(map[int32]string)
			)

			ginkgo.By("Checking endpoint address")
			for _, address := range subset.Addresses {
				ips = append(ips, address.IP)
			}
			framework.ExpectContainElement(ips, pods.Items[i].Status.PodIP)

			ginkgo.By("Checking endpoint ports")
			for _, port := range subset.Ports {
				tps = append(tps, port.Port)
				protocols[port.Port] = string(port.Protocol)
			}
			for _, port := range slrPorts {
				framework.ExpectContainElement(tps, port.TargetPort)
				framework.ExpectHaveKeyWithValue(protocols, port.TargetPort, port.Protocol)
			}
		}

		ginkgo.By("Checking selector switch lb service " + selSvc.Name)
		curlSvc(f, clientPodName, vip, selSlrFrontPort)

		ginkgo.By("3. Creating switch-lb-rule with endpoints with lb front vip " + vip)
		ginkgo.By("Creating endpoint SwitchLBRule " + epSlrName)
		sessionAffinity = corev1.ServiceAffinityClientIP
		epPorts = []kubeovnv1.SwitchLBRulePort{
			{
				Name:       "http",
				Port:       epSlrFrontPort,
				TargetPort: backendPort,
				Protocol:   "TCP",
			},
		}
		presetEndpoints := []string{}
		for _, pod := range pods.Items {
			presetEndpoints = append(presetEndpoints, pod.Status.PodIP)
		}
		epRule := framework.MakeSwitchLBRule(epSlrName, namespaceName, vip, sessionAffinity, annotations, nil, presetEndpoints, epPorts)
		_ = switchLBRuleClient.Create(epRule)

		ginkgo.By("Waiting for switch-lb-rule " + epSlrName + " to be ready")
		framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, err := switchLBRuleClient.SwitchLBRuleInterface.Get(context.TODO(), epSlrName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("switch-lb-rule %s is created", epSlrName))

		ginkgo.By("Waiting for headless service " + epSvcName + " to be ready")
		framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
			epSvc, err = serviceClient.ServiceInterface.Get(context.TODO(), epSvcName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("service %s is created", epSvcName))
		framework.ExpectNotNil(epSvc)

		ginkgo.By("Waiting for endpoints " + epSvcName + " to be ready")
		framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
			epSlrEps, err = endpointsClient.EndpointsInterface.Get(context.TODO(), epSvcName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("endpoints %s is created", epSvcName))
		framework.ExpectNotNil(epSlrEps)

		for i, subset := range epSlrEps.Subsets {
			var (
				ips       []string
				tps       []int32
				protocols = make(map[int32]string)
			)

			ginkgo.By("Checking endpoint address")
			for _, address := range subset.Addresses {
				ips = append(ips, address.IP)
			}
			framework.ExpectContainElement(ips, pods.Items[i].Status.PodIP)

			ginkgo.By("Checking endpoint ports")
			for _, port := range subset.Ports {
				tps = append(tps, port.Port)
				protocols[port.Port] = string(port.Protocol)
			}
			for _, port := range epPorts {
				framework.ExpectContainElement(tps, port.TargetPort)
				framework.ExpectHaveKeyWithValue(protocols, port.TargetPort, port.Protocol)
			}
		}
		ginkgo.By("Checking endpoint switch lb service " + epSvc.Name)
		curlSvc(f, clientPodName, vip, epSlrFrontPort)
	},
		ginkgo.Entry("SLR with default provider", false),
		ginkgo.Entry("SLR with custom provider", true),
	)

	ginkgo.It("should not delete VIPs in other VPCs when deleting SLR with same VIP IP", func() {
		f.SkipVersionPriorTo(1, 16, "This fix was introduced in v1.16")

		// Health check VIPs are only created for IPv4 endpoints.
		if !f.HasIPv4() {
			ginkgo.Skip("Health check VIPs require IPv4")
		}

		// --- Setup: 2 VPCs + 2 Subnets, using the same VIP IP ---
		suffix2 := framework.RandomSuffix()
		vpcName2 := generateVpcName(suffix2)
		subnetName2 := generateSubnetName(suffix2)
		cidr2 := framework.RandomCIDR(f.ClusterIPFamily)
		slrName2 := "sel-" + generateSwitchLBRuleName(suffix2)

		// Create subnet-1 in VPC-1 (VPC-1 is created in BeforeEach)
		ginkgo.By("Creating subnet " + subnetName + " in VPC " + vpcName)
		subnet1 := framework.MakeSubnet(subnetName, "", overlaySubnetCidr, "", vpcName, "", nil, nil, nil)
		_ = subnetClient.CreateSync(subnet1)

		// Create VPC-2 + subnet-2
		ginkgo.By("Creating VPC " + vpcName2 + " and subnet " + subnetName2)
		vpc2 := framework.MakeVpc(vpcName2, "", false, false, []string{namespaceName})
		_ = vpcClient.CreateSync(vpc2)
		subnet2 := framework.MakeSubnet(subnetName2, "", cidr2, "", vpcName2, "", nil, nil, nil)
		_ = subnetClient.CreateSync(subnet2)

		// --- Deploy backend pods in each VPC's subnet ---
		stsName1 := "sts-a-" + suffix
		stsName2 := "sts-b-" + suffix2
		stsSvcName1 := stsName1
		stsSvcName2 := stsName2
		labels1 := map[string]string{"app": "slr-vpc1"}
		labels2 := map[string]string{"app": "slr-vpc2"}

		ginkgo.By("Creating statefulset " + stsName1 + " in subnet " + subnetName)
		sts1 := framework.MakeStatefulSet(stsName1, stsSvcName1, 1, labels1, framework.AgnhostImage)
		sts1.Spec.Template.Annotations = map[string]string{util.LogicalSwitchAnnotation: subnetName}
		sts1.Spec.Template.Spec.Containers[0].Command = []string{"/agnhost", "netexec", "--http-port", "80"}
		_ = stsClient.CreateSync(sts1)

		ginkgo.By("Creating statefulset " + stsName2 + " in subnet " + subnetName2)
		sts2 := framework.MakeStatefulSet(stsName2, stsSvcName2, 1, labels2, framework.AgnhostImage)
		sts2.Spec.Template.Annotations = map[string]string{util.LogicalSwitchAnnotation: subnetName2}
		sts2.Spec.Template.Spec.Containers[0].Command = []string{"/agnhost", "netexec", "--http-port", "80"}
		_ = stsClient.CreateSync(sts2)

		// --- Create a regular service to obtain a ClusterIP as shared VIP ---
		ports := []corev1.ServicePort{{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       8090,
			TargetPort: intstr.FromInt32(80),
		}}
		svc1 := framework.MakeService(stsSvcName1, corev1.ServiceTypeClusterIP,
			map[string]string{util.LogicalSwitchAnnotation: subnetName},
			labels1, ports, corev1.ServiceAffinityNone)
		svc1 = serviceClient.CreateSync(svc1, func(s *corev1.Service) (bool, error) {
			return len(s.Spec.ClusterIPs) != 0, nil
		}, "cluster ips are not empty")
		sharedVip := svc1.Spec.ClusterIPs[0]

		// --- Create two SLRs with the same VIP in different VPCs ---
		slrPorts := []kubeovnv1.SwitchLBRulePort{{
			Name:       "http",
			Port:       8090,
			TargetPort: 80,
			Protocol:   "TCP",
		}}

		ginkgo.By("Creating SLR " + selSlrName + " in VPC " + vpcName)
		slr1 := framework.MakeSwitchLBRule(selSlrName, namespaceName, sharedVip,
			corev1.ServiceAffinityNone,
			map[string]string{
				util.LogicalSwitchAnnotation: subnetName,
				util.LogicalRouterAnnotation: vpcName,
			},
			[]string{"app:slr-vpc1"}, nil, slrPorts)
		_ = switchLBRuleClient.Create(slr1)

		ginkgo.By("Creating SLR " + slrName2 + " in VPC " + vpcName2)
		slr2 := framework.MakeSwitchLBRule(slrName2, namespaceName, sharedVip,
			corev1.ServiceAffinityNone,
			map[string]string{
				util.LogicalSwitchAnnotation: subnetName2,
				util.LogicalRouterAnnotation: vpcName2,
			},
			[]string{"app:slr-vpc2"}, nil, slrPorts)
		_ = switchLBRuleClient.Create(slr2)

		// --- Wait for health check VIP CRDs to be created and ready ---
		vipClient := f.VipClient()

		ginkgo.By("Waiting for health check VIP " + subnetName + " to be created and ready")
		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			vip, err := vipClient.VipInterface.Get(context.TODO(), subnetName, metav1.GetOptions{})
			if err != nil {
				if k8serrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			return vip.Status.V4ip != "" || vip.Status.V6ip != "", nil
		}, "health check VIP "+subnetName+" is ready")

		ginkgo.By("Waiting for health check VIP " + subnetName2 + " to be created and ready")
		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			vip, err := vipClient.VipInterface.Get(context.TODO(), subnetName2, metav1.GetOptions{})
			if err != nil {
				if k8serrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			return vip.Status.V4ip != "" || vip.Status.V6ip != "", nil
		}, "health check VIP "+subnetName2+" is ready")

		// --- Core verification: delete SLR-1, VIP-2 must survive ---
		ginkgo.By("Deleting SLR " + selSlrName + " and verifying VIP for " + subnetName2 + " survives")
		switchLBRuleClient.Delete(selSlrName)
		framework.ExpectNoError(switchLBRuleClient.WaitToDisappear(selSlrName, 0, 2*time.Minute))

		// VIP for subnet-1 should be deleted
		ginkgo.By("Waiting for VIP " + subnetName + " to disappear")
		framework.ExpectNoError(vipClient.WaitToDisappear(subnetName, 0, 2*time.Minute))

		// VIP for subnet-2 should still exist
		ginkgo.By("Verifying VIP " + subnetName2 + " still exists")
		vip2, err := vipClient.VipInterface.Get(context.TODO(), subnetName2, metav1.GetOptions{})
		framework.ExpectNoError(err, "VIP "+subnetName2+" should still exist")
		framework.ExpectNotNil(vip2)

		// --- Cleanup (in order: SLRs → workloads → subnets → VPCs) ---
		ginkgo.By("Cleaning up cross-VPC test resources")
		switchLBRuleClient.Delete(slrName2)
		framework.ExpectNoError(switchLBRuleClient.WaitToDisappear(slrName2, 0, 2*time.Minute))
		stsClient.Delete(stsName1)
		stsClient.Delete(stsName2)
		serviceClient.Delete(stsSvcName1)
		framework.ExpectNoError(stsClient.WaitToDisappear(stsName1, 0, 2*time.Minute))
		framework.ExpectNoError(stsClient.WaitToDisappear(stsName2, 0, 2*time.Minute))
		framework.ExpectNoError(serviceClient.WaitToDisappear(stsSvcName1, 0, 2*time.Minute))
		subnetClient.DeleteSync(subnetName2)
		vpcClient.DeleteSync(vpcName2)
	})
})
