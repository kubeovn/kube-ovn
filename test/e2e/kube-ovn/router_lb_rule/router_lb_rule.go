package router_lb_rule

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

const (
	dockerNetworkName = "kube-ovn-vlan"
	backendLabel      = "rlr-backend"
	backendPort       = int32(80)
	rlrFrontPort      = int32(8090)
)

func makeProviderNetwork(providerNetworkName string, exchangeLinkName bool, linkMap map[string]*iproute.Link) *kubeovnv1.ProviderNetwork {
	var defaultInterface string
	customInterfaces := make(map[string][]string)
	for node, link := range linkMap {
		if !strings.ContainsRune(node, '-') {
			continue
		}
		if defaultInterface == "" {
			defaultInterface = link.IfName
		} else if link.IfName != defaultInterface {
			customInterfaces[link.IfName] = append(customInterfaces[link.IfName], node)
		}
	}
	return framework.MakeProviderNetwork(providerNetworkName, exchangeLinkName, defaultInterface, customInterfaces, nil)
}

func curlRLR(f *framework.Framework, clientPodName, eipIP string, port int32) {
	ginkgo.GinkgoHelper()
	cmd := "curl -q -s --connect-timeout 5 --max-time 5 " + util.JoinHostPort(eipIP, port)
	ginkgo.By(fmt.Sprintf("Waiting for %s:%d to be reachable from pod %s/%s", eipIP, port, f.Namespace.Name, clientPodName))
	framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		_, err := e2epodoutput.RunHostCmd(f.Namespace.Name, clientPodName, cmd)
		return err == nil, nil
	}, fmt.Sprintf("%s:%d is reachable", eipIP, port))
	ginkgo.By(fmt.Sprintf("Verifying %s:%d with 5 sequential requests", eipIP, port))
	for i := range 5 {
		_, err := e2epodoutput.RunHostCmd(f.Namespace.Name, clientPodName, cmd)
		framework.ExpectNoError(err, "request %d/5 to %s:%d failed", i+1, eipIP, port)
	}
}

var _ = framework.Describe("[group:rlr]", func() {
	f := framework.NewDefaultFramework("rlr")

	var skip bool
	var cs clientset.Interface
	var clusterName string
	var linkMap map[string]*iproute.Link
	var nodeNames []string

	var providerNetworkClient *framework.ProviderNetworkClient
	var vlanClient *framework.VlanClient
	var subnetClient *framework.SubnetClient
	var vpcClient *framework.VpcClient
	var vipClient *framework.VipClient
	var ovnEipClient *framework.OvnEipClient
	var stsClient *framework.StatefulSetClient
	var podClient *framework.PodClient
	var serviceClient *framework.ServiceClient
	var endpointsClient *framework.EndpointsClient
	var rlrClient *framework.RouterLBRuleClient

	var (
		namespaceName                                  string
		suffix                                         string
		providerNetworkName, vlanName                  string
		externalSubnetName, vpcName, overlaySubnetName string
		eipName, lrpEipName, newEipName                string
		stsName, stsSvcName, clientPodName             string
		selRlrName                                     string
	)

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		providerNetworkClient = f.ProviderNetworkClient()
		vlanClient = f.VlanClient()
		subnetClient = f.SubnetClient()
		vpcClient = f.VpcClient()
		vipClient = f.VipClient()
		ovnEipClient = f.OvnEipClient()
		stsClient = f.StatefulSetClient()
		podClient = f.PodClient()
		serviceClient = f.ServiceClient()
		endpointsClient = f.EndpointClient()
		rlrClient = f.RouterLBRuleClient()

		namespaceName = f.Namespace.Name
		suffix = framework.RandomSuffix()

		providerNetworkName = "external"
		vlanName = "vlan-" + suffix
		externalSubnetName = "external"
		vpcName = "vpc-" + suffix
		overlaySubnetName = "overlay-" + suffix
		eipName = "rlr-eip-" + suffix
		newEipName = "rlr-eip-new-" + suffix
		lrpEipName = vpcName + "-" + externalSubnetName
		stsName = "rlr-sts-" + suffix
		stsSvcName = stsName
		clientPodName = "rlr-client-" + suffix
		selRlrName = "sel-rlr-" + suffix

		if skip {
			ginkgo.Skip("RouterLBRule e2e only runs on Kind clusters")
		}
		f.SkipVersionPriorTo(1, 16, "RouterLBRule was introduced in v1.16")

		if clusterName == "" {
			ginkgo.By("Getting k8s nodes")
			k8sNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
			framework.ExpectNoError(err)

			cluster, ok := kind.IsKindProvided(k8sNodes.Items[0].Spec.ProviderID)
			if !ok {
				skip = true
				ginkgo.Skip("RouterLBRule e2e only runs on Kind clusters")
			}
			clusterName = cluster
		}

		ginkgo.By("Ensuring docker network " + dockerNetworkName + " exists")
		dockerNetwork, err := docker.NetworkCreate(dockerNetworkName, f.HasIPv6(), false)
		framework.ExpectNoError(err, "creating docker network "+dockerNetworkName)

		ginkgo.By("Getting Kind nodes")
		nodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodes)

		ginkgo.By("Connecting nodes to the docker network")
		err = kind.NetworkConnect(dockerNetwork.ID, nodes)
		framework.ExpectNoError(err, "connecting nodes to "+dockerNetworkName)

		ginkgo.By("Getting node links for the docker network")
		nodes, err = kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err)

		linkMap = make(map[string]*iproute.Link, len(nodes))
		nodeNames = make([]string, 0, len(nodes))
		for _, node := range nodes {
			links, err := node.ListLinks()
			framework.ExpectNoError(err)
			for _, link := range links {
				if link.Address == node.NetworkSettings.Networks[dockerNetworkName].MacAddress.String() {
					linkMap[node.ID] = &link
					break
				}
			}
			framework.ExpectHaveKey(linkMap, node.ID)
			linkMap[node.Name()] = linkMap[node.ID]
			nodeNames = append(nodeNames, node.Name())
		}
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Cleaning up RouterLBRule resources")
		rlrClient.Delete(selRlrName)
		podClient.DeleteGracefully(clientPodName)
		stsClient.Delete(stsName)
		serviceClient.Delete(stsSvcName)

		framework.ExpectNoError(rlrClient.WaitToDisappear(selRlrName, 0, 2*time.Minute))
		podClient.WaitForNotFound(clientPodName)
		framework.ExpectNoError(stsClient.WaitToDisappear(stsName, 0, 2*time.Minute))

		ovnEipClient.DeleteSync(eipName)
		ovnEipClient.DeleteSync(newEipName)
		ovnEipClient.DeleteSync(lrpEipName)

		// Health-check VIP (named after the subnet) is created by the endpoint-slice
		// controller for OVN LB health checks but not deleted by handleDelRouterLBRule.
		vipClient.DeleteSync(overlaySubnetName)
		subnetClient.DeleteSync(overlaySubnetName)
		vpcClient.DeleteSync(vpcName)

		time.Sleep(time.Second)
		subnetClient.DeleteSync(externalSubnetName)
		vlanClient.Delete(vlanName)
		providerNetworkClient.DeleteSync(providerNetworkName)

		ginkgo.By("Disconnecting nodes from the docker network")
		dockerNetwork, err := docker.NetworkInspect(dockerNetworkName)
		if err == nil {
			nodes, err := kind.ListNodes(clusterName, "")
			if err == nil {
				_ = kind.NetworkDisconnect(dockerNetwork.ID, nodes)
			}
		}
	})

	framework.ConformanceIt("should create Service and Endpoints for selector mode, update on scale, and clean up on delete", func() {
		// --- Setup underlay networking ---
		ginkgo.By("Creating provider network " + providerNetworkName)
		pn := makeProviderNetwork(providerNetworkName, false, linkMap)
		_ = providerNetworkClient.CreateSync(pn)

		ginkgo.By("Creating vlan " + vlanName)
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)

		ginkgo.By("Getting docker network IPAM config")
		dockerNetwork, err := docker.NetworkInspect(dockerNetworkName)
		framework.ExpectNoError(err)

		var cidrV4, cidrV6, gatewayV4, gatewayV6 string
		for _, config := range dockerNetwork.IPAM.Config {
			switch util.CheckProtocol(config.Subnet.String()) {
			case kubeovnv1.ProtocolIPv4:
				if f.HasIPv4() {
					cidrV4 = config.Subnet.String()
					gatewayV4 = config.Gateway.String()
				}
			case kubeovnv1.ProtocolIPv6:
				if f.HasIPv6() {
					cidrV6 = config.Subnet.String()
					gatewayV6 = config.Gateway.String()
				}
			}
		}
		cidrParts := make([]string, 0, 2)
		gatewayParts := make([]string, 0, 2)
		if f.HasIPv4() {
			framework.ExpectNotEmpty(cidrV4, "docker network must have an IPv4 subnet")
			cidrParts = append(cidrParts, cidrV4)
			gatewayParts = append(gatewayParts, gatewayV4)
		}
		if f.HasIPv6() {
			framework.ExpectNotEmpty(cidrV6, "docker network must have an IPv6 subnet")
			cidrParts = append(cidrParts, cidrV6)
			gatewayParts = append(gatewayParts, gatewayV6)
		}
		excludeIPs := make([]string, 0)
		for _, container := range dockerNetwork.Containers {
			if container.IPv4Address.IsValid() && f.HasIPv4() {
				excludeIPs = append(excludeIPs, container.IPv4Address.Addr().String())
			}
			if container.IPv6Address.IsValid() && f.HasIPv6() {
				excludeIPs = append(excludeIPs, container.IPv6Address.Addr().String())
			}
		}

		ginkgo.By("Creating external underlay subnet " + externalSubnetName)
		externalSubnet := framework.MakeSubnet(externalSubnetName, vlanName, strings.Join(cidrParts, ","), strings.Join(gatewayParts, ","), "", "", excludeIPs, nil, nil)
		_ = subnetClient.CreateSync(externalSubnet)

		// --- Setup VPC ---
		ginkgo.By("Creating VPC " + vpcName + " with enableExternal=true")
		vpc := framework.MakeVpc(vpcName, "", true, false, []string{namespaceName})
		_ = vpcClient.CreateSync(vpc)

		ginkgo.By("Creating overlay subnet " + overlaySubnetName)
		overlayCIDR := framework.RandomCIDR(f.ClusterIPFamily)
		overlaySubnet := framework.MakeSubnet(overlaySubnetName, "", overlayCIDR, "", vpcName, util.OvnProvider, nil, nil, nil)
		_ = subnetClient.CreateSync(overlaySubnet)

		// --- Create NAT EIP ---
		ginkgo.By("Creating NAT OvnEip " + eipName)
		eip := framework.MakeOvnEip(eipName, externalSubnetName, "", "", "", util.OvnEipTypeNAT)
		eip = ovnEipClient.CreateSync(eip)
		eipIPs := make([]string, 0, 2)
		if eip.Status.V4Ip != "" {
			eipIPs = append(eipIPs, eip.Status.V4Ip)
		}
		if eip.Status.V6Ip != "" {
			eipIPs = append(eipIPs, eip.Status.V6Ip)
		}
		framework.ExpectNotEmpty(eipIPs, "EIP must have at least one IP address")
		// eipVip matches the controller's annotation format: "v4" or "v6" or "v4,v6"
		eipVipParts := make([]string, 0, 2)
		if eip.Status.V4Ip != "" {
			eipVipParts = append(eipVipParts, eip.Status.V4Ip)
		}
		if eip.Status.V6Ip != "" {
			eipVipParts = append(eipVipParts, eip.Status.V6Ip)
		}
		eipVip := strings.Join(eipVipParts, ",")

		// --- Wait for LRP EIP auto-created by VPC controller ---
		ginkgo.By("Waiting for LRP EIP " + lrpEipName + " to become ready")
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			lrpEip, err := ovnEipClient.OvnEipInterface.Get(context.TODO(), lrpEipName, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			return lrpEip.Status.Ready, nil
		}, "LRP EIP "+lrpEipName+" is ready")

		// --- Deploy backend pods ---
		ginkgo.By("Creating StatefulSet " + stsName + " in subnet " + overlaySubnetName)
		labels := map[string]string{"app": backendLabel}
		annotations := map[string]string{util.LogicalSwitchAnnotation: overlaySubnetName}
		sts := framework.MakeStatefulSet(stsName, stsSvcName, 2, labels, framework.AgnhostImage)
		sts.Spec.Template.Annotations = annotations
		sts.Spec.Template.Spec.Containers[0].Command = []string{"/agnhost", "netexec", "--http-port", "80"}
		sts = stsClient.CreateSync(sts)

		// --- Client pod for connectivity checks ---
		ginkgo.By("Creating client pod " + clientPodName)
		clientPod := framework.MakePod(namespaceName, clientPodName, nil,
			map[string]string{util.LogicalSwitchAnnotation: overlaySubnetName},
			framework.AgnhostImage, nil, nil)
		podClient.CreateSync(clientPod)

		pods := stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, 2)

		// =========================================================
		// 1. Selector mode
		// =========================================================
		ginkgo.By("1. Creating RouterLBRule with selector")
		selPorts := []kubeovnv1.RouterLBRulePort{{
			Name:       "http",
			Port:       rlrFrontPort,
			TargetPort: backendPort,
			Protocol:   "TCP",
		}}
		selRule := framework.MakeRouterLBRule(selRlrName, vpcName, eipName, namespaceName,
			"", []string{"app:" + backendLabel}, nil, selPorts)
		selRule = rlrClient.CreateSync(selRule, framework.RouterLBRuleIsReady, "Status.Service is set")

		ginkgo.By("Verifying selector RLR Status.Service")
		expectedSvc := fmt.Sprintf("%s/rlr-%s", namespaceName, selRlrName)
		framework.ExpectEqual(selRule.Status.Service, expectedSvc)

		ginkgo.By("Verifying headless Service rlr-" + selRlrName + " exists")
		svcName := "rlr-" + selRlrName
		svc, err := serviceClient.ServiceInterface.Get(context.TODO(), svcName, metav1.GetOptions{})
		framework.ExpectNoError(err)
		framework.ExpectEqual(svc.Spec.ClusterIP, corev1.ClusterIPNone)
		framework.ExpectEqual(svc.Annotations[util.RouterLBRuleVipsAnnotation], eipVip)
		framework.ExpectEqual(svc.Annotations[util.LogicalRouterAnnotation], vpcName)

		ginkgo.By("Verifying Endpoints contain backend pod IPs")
		framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
			eps, err := endpointsClient.EndpointsInterface.Get(context.TODO(), svcName, metav1.GetOptions{})
			if err != nil || len(eps.Subsets) == 0 {
				return false, nil
			}
			return len(eps.Subsets[0].Addresses) == 2, nil
		}, "Endpoints has 2 addresses")

		eps, err := endpointsClient.EndpointsInterface.Get(context.TODO(), svcName, metav1.GetOptions{})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(eps.Subsets, 1)
		epIPs := make([]string, 0, len(eps.Subsets[0].Addresses))
		for _, addr := range eps.Subsets[0].Addresses {
			epIPs = append(epIPs, addr.IP)
		}
		for _, pod := range pods.Items {
			framework.ExpectContainElement(epIPs, pod.Status.PodIP)
		}
		framework.ExpectEqual(eps.Subsets[0].Ports[0].Port, backendPort)

		ginkgo.By("Checking TCP connectivity via EIP (selector mode)")
		for _, ip := range eipIPs {
			curlRLR(f, clientPodName, ip, rlrFrontPort)
		}

		// =========================================================
		// 2. Scale backends: 2 → 3
		// =========================================================
		ginkgo.By("2. Scaling StatefulSet to 3 replicas")
		newSts := sts.DeepCopy()
		replicas := int32(3)
		newSts.Spec.Replicas = &replicas
		_ = stsClient.PatchSync(sts, newSts)

		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			eps, err := endpointsClient.EndpointsInterface.Get(context.TODO(), svcName, metav1.GetOptions{})
			if err != nil || len(eps.Subsets) == 0 {
				return false, nil
			}
			return len(eps.Subsets[0].Addresses) == 3, nil
		}, "Endpoints has 3 addresses after scale-up")

		ginkgo.By("Checking TCP connectivity with 3 replicas")
		for _, ip := range eipIPs {
			curlRLR(f, clientPodName, ip, rlrFrontPort)
		}

		ginkgo.By("Scaling StatefulSet back to 1 replica")
		curSts := stsClient.Get(stsName)
		newSts = curSts.DeepCopy()
		one := int32(1)
		newSts.Spec.Replicas = &one
		_ = stsClient.PatchSync(curSts, newSts)

		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			eps, err := endpointsClient.EndpointsInterface.Get(context.TODO(), svcName, metav1.GetOptions{})
			if err != nil || len(eps.Subsets) == 0 {
				return false, nil
			}
			return len(eps.Subsets[0].Addresses) == 1, nil
		}, "Endpoints has 1 address after scale-down")

		ginkgo.By("Checking TCP connectivity with 1 replica")
		for _, ip := range eipIPs {
			curlRLR(f, clientPodName, ip, rlrFrontPort)
		}

		// =========================================================
		// 3. Detach EIP, verify drop, attach new EIP, verify restore
		// =========================================================
		ginkgo.By("3. Detaching EIP from RLR by clearing Spec.OvnEip")
		curRule := rlrClient.Get(selRlrName)
		detRule := curRule.DeepCopy()
		detRule.Spec.OvnEip = ""
		_ = rlrClient.Patch(curRule, detRule)

		ginkgo.By("Verifying connectivity drops after EIP detach")
		framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			for _, ip := range eipIPs {
				_, err := e2epodoutput.RunHostCmd(namespaceName, clientPodName,
					"curl -q -s --connect-timeout 3 --max-time 3 "+util.JoinHostPort(ip, rlrFrontPort))
				if err == nil {
					return false, nil
				}
			}
			return true, nil
		}, fmt.Sprintf("all EIP IPs %v unreachable after EIP detach", eipIPs))

		ginkgo.By("Creating new EIP " + newEipName)
		newEip := framework.MakeOvnEip(newEipName, externalSubnetName, "", "", "", util.OvnEipTypeNAT)
		newEip = ovnEipClient.CreateSync(newEip)
		newEipIPs := make([]string, 0, 2)
		if newEip.Status.V4Ip != "" {
			newEipIPs = append(newEipIPs, newEip.Status.V4Ip)
		}
		if newEip.Status.V6Ip != "" {
			newEipIPs = append(newEipIPs, newEip.Status.V6Ip)
		}
		framework.ExpectNotEmpty(newEipIPs, "new EIP must have at least one IP address")
		newEipVipParts := make([]string, 0, 2)
		if newEip.Status.V4Ip != "" {
			newEipVipParts = append(newEipVipParts, newEip.Status.V4Ip)
		}
		if newEip.Status.V6Ip != "" {
			newEipVipParts = append(newEipVipParts, newEip.Status.V6Ip)
		}
		newEipVip := strings.Join(newEipVipParts, ",")

		ginkgo.By("Attaching new EIP " + newEipName + " to RLR")
		curRule = rlrClient.Get(selRlrName)
		updRule := curRule.DeepCopy()
		updRule.Spec.OvnEip = newEipName
		_ = rlrClient.Patch(curRule, updRule)

		ginkgo.By("Waiting for Service annotation to reflect new EIP VIP " + newEipVip)
		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			svc, err := serviceClient.ServiceInterface.Get(context.TODO(), svcName, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			return svc.Annotations[util.RouterLBRuleVipsAnnotation] == newEipVip, nil
		}, "Service annotation has new EIP VIP")

		ginkgo.By("Verifying connectivity via new EIP")
		for _, ip := range newEipIPs {
			curlRLR(f, clientPodName, ip, rlrFrontPort)
		}

		// =========================================================
		// 4. Delete new EIP, verify connectivity drops
		// =========================================================
		ginkgo.By("4. Deleting new EIP " + newEipName)
		ovnEipClient.DeleteSync(newEipName)

		ginkgo.By("Verifying connectivity drops after EIP deletion")
		framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			for _, ip := range newEipIPs {
				_, err := e2epodoutput.RunHostCmd(namespaceName, clientPodName,
					"curl -q -s --connect-timeout 3 --max-time 3 "+util.JoinHostPort(ip, rlrFrontPort))
				if err == nil {
					return false, nil
				}
			}
			return true, nil
		}, fmt.Sprintf("all EIP IPs %v unreachable after EIP deletion", newEipIPs))

		// =========================================================
		// 5. Delete cleans up Service
		// =========================================================
		ginkgo.By("5. Deleting selector RLR and verifying Service is removed")
		rlrClient.DeleteSync(selRlrName)

		framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, err := serviceClient.ServiceInterface.Get(context.TODO(), "rlr-"+selRlrName, metav1.GetOptions{})
			return err != nil, nil
		}, "Service rlr-"+selRlrName+" is deleted")

		// Direct-endpoint RLR deleted in AfterEach.

		_ = nodeNames // used in BeforeEach linkMap construction
	})
})
