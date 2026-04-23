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
	rlrFrontPortEP    = int32(8091)
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
	ginkgo.By(fmt.Sprintf("Executing %q in pod %s/%s", cmd, f.Namespace.Name, clientPodName))
	framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		_, err := e2epodoutput.RunHostCmd(f.Namespace.Name, clientPodName, cmd)
		return err == nil, nil
	}, fmt.Sprintf("%s:%d is reachable", eipIP, port))
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
		namespaceName                                          string
		suffix                                                 string
		providerNetworkName, vlanName                          string
		externalSubnetName, vpcName, overlaySubnetName         string
		eipName, lrpEipName                                    string
		stsName, stsSvcName, clientPodName                     string
		selRlrName, epRlrName                                  string
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
		lrpEipName = vpcName + "-" + externalSubnetName
		stsName = "rlr-sts-" + suffix
		stsSvcName = stsName
		clientPodName = "rlr-client-" + suffix
		selRlrName = "sel-rlr-" + suffix
		epRlrName = "ep-rlr-" + suffix

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
		dockerNetwork, err := docker.NetworkCreate(dockerNetworkName, false, true)
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
		rlrClient.Delete(epRlrName)
		podClient.DeleteGracefully(clientPodName)
		stsClient.Delete(stsName)
		serviceClient.Delete(stsSvcName)

		framework.ExpectNoError(rlrClient.WaitToDisappear(selRlrName, 0, 2*time.Minute))
		framework.ExpectNoError(rlrClient.WaitToDisappear(epRlrName, 0, 2*time.Minute))
		podClient.WaitForNotFound(clientPodName)
		framework.ExpectNoError(stsClient.WaitToDisappear(stsName, 0, 2*time.Minute))

		ovnEipClient.DeleteSync(eipName)
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

	framework.ConformanceIt("should create Service and Endpoints for selector and direct-endpoint modes, and update on scale", func() {
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

		var cidr, gateway string
		excludeIPs := make([]string, 0)
		for _, config := range dockerNetwork.IPAM.Config {
			if util.CheckProtocol(config.Subnet.String()) == kubeovnv1.ProtocolIPv4 && f.HasIPv4() {
				cidr = config.Subnet.String()
				gateway = config.Gateway.String()
			}
		}
		for _, container := range dockerNetwork.Containers {
			if container.IPv4Address.IsValid() && f.HasIPv4() {
				excludeIPs = append(excludeIPs, container.IPv4Address.Addr().String())
			}
		}
		framework.ExpectNotEmpty(cidr, "docker network must have an IPv4 subnet")

		ginkgo.By("Creating external underlay subnet " + externalSubnetName)
		externalSubnet := framework.MakeSubnet(externalSubnetName, vlanName, cidr, gateway, "", "", excludeIPs, nil, nil)
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
		framework.ExpectNotEmpty(eip.Status.V4Ip, "EIP must have an IPv4 address")

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
		framework.ExpectEqual(svc.Annotations[util.RouterLBRuleVipsAnnotation], eip.Status.V4Ip)
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
		curlRLR(f, clientPodName, eip.Status.V4Ip, rlrFrontPort)

		// =========================================================
		// 2. Scale backends: 2 → 3
		// =========================================================
		ginkgo.By("2. Scaling StatefulSet to 3 replicas")
		newSts := sts.DeepCopy()
		replicas := int32(3)
		newSts.Spec.Replicas = &replicas
		sts = stsClient.PatchSync(sts, newSts)

		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			eps, err := endpointsClient.EndpointsInterface.Get(context.TODO(), svcName, metav1.GetOptions{})
			if err != nil || len(eps.Subsets) == 0 {
				return false, nil
			}
			return len(eps.Subsets[0].Addresses) == 3, nil
		}, "Endpoints has 3 addresses after scale-up")

		ginkgo.By("Scaling StatefulSet back to 1 replica")
		curSts := stsClient.Get(stsName)
		newSts = curSts.DeepCopy()
		one := int32(1)
		newSts.Spec.Replicas = &one
		sts = stsClient.PatchSync(curSts, newSts)

		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			eps, err := endpointsClient.EndpointsInterface.Get(context.TODO(), svcName, metav1.GetOptions{})
			if err != nil || len(eps.Subsets) == 0 {
				return false, nil
			}
			return len(eps.Subsets[0].Addresses) == 1, nil
		}, "Endpoints has 1 address after scale-down")

		// =========================================================
		// 3. Direct endpoint mode
		// =========================================================
		ginkgo.By("3. Creating RouterLBRule with direct endpoints")
		livePods := stsClient.GetPods(sts)
		presetEndpoints := make([]string, 0, len(livePods.Items))
		for _, pod := range livePods.Items {
			presetEndpoints = append(presetEndpoints, pod.Status.PodIP)
		}

		epPorts := []kubeovnv1.RouterLBRulePort{{
			Name:       "http",
			Port:       rlrFrontPortEP,
			TargetPort: backendPort,
			Protocol:   "TCP",
		}}
		epRule := framework.MakeRouterLBRule(epRlrName, vpcName, eipName, namespaceName,
			string(corev1.ServiceAffinityClientIP), nil, presetEndpoints, epPorts)
		epRule = rlrClient.CreateSync(epRule, framework.RouterLBRuleIsReady, "Status.Service is set")

		ginkgo.By("Verifying direct-endpoint RLR Status.Service")
		framework.ExpectEqual(epRule.Status.Service, fmt.Sprintf("%s/rlr-%s", namespaceName, epRlrName))

		ginkgo.By("Verifying Endpoints contain preset IPs")
		epSvcName := "rlr-" + epRlrName
		framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
			eps, err := endpointsClient.EndpointsInterface.Get(context.TODO(), epSvcName, metav1.GetOptions{})
			if err != nil || len(eps.Subsets) == 0 {
				return false, nil
			}
			return len(eps.Subsets[0].Addresses) == len(presetEndpoints), nil
		}, "Endpoints has preset addresses")

		eps, err = endpointsClient.EndpointsInterface.Get(context.TODO(), epSvcName, metav1.GetOptions{})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(eps.Subsets, 1)
		epIPs = make([]string, 0, len(eps.Subsets[0].Addresses))
		for _, addr := range eps.Subsets[0].Addresses {
			epIPs = append(epIPs, addr.IP)
		}
		for _, ip := range presetEndpoints {
			framework.ExpectContainElement(epIPs, ip)
		}
		framework.ExpectEqual(eps.Subsets[0].Ports[0].Port, backendPort)

		ginkgo.By("Checking TCP connectivity via EIP (direct endpoint mode)")
		curlRLR(f, clientPodName, eip.Status.V4Ip, rlrFrontPortEP)

		// =========================================================
		// 4. Delete cleans up Service
		// =========================================================
		ginkgo.By("4. Deleting selector RLR and verifying Service is removed")
		rlrClient.DeleteSync(selRlrName)

		framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, err := serviceClient.ServiceInterface.Get(context.TODO(), "rlr-"+selRlrName, metav1.GetOptions{})
			return err != nil, nil
		}, "Service rlr-"+selRlrName+" is deleted")

		// Direct-endpoint RLR deleted in AfterEach.

		_ = nodeNames // used in BeforeEach linkMap construction
	})
})
