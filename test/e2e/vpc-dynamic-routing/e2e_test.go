package vpc_dynamic_routing

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"

	dockernetwork "github.com/moby/moby/api/types/network"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
	commontest "k8s.io/kubernetes/test/e2e/common"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

const (
	frrImage          = "quay.io/frrouting/frr:10.7.0"
	localASN          = 65002
	peerASN           = 65001
	vrfName           = "ovnvrf1001"
	vrfID             = 1001
	remoteLoopbackIP  = "198.51.100.1"
	dockerNetworkName = "kube-ovn-dynamic-routing"
	chassisContainer  = "container"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)

	logs.InitLogs()
	defer logs.FlushLogs()
	klog.EnableContextualLogging(true)

	gomega.RegisterFailHandler(k8sframework.Fail)

	suiteConfig, reporterConfig := k8sframework.CreateGinkgoConfig()
	klog.Infof("Starting e2e run %q on Ginkgo node %d", k8sframework.RunID, suiteConfig.ParallelProcess)
	ginkgo.RunSpecs(t, "Kube-OVN e2e suite", suiteConfig, reporterConfig)
}

var clusterName string

var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	commontest.CurrentSuite = commontest.E2E

	cs, err := k8sframework.LoadClientset()
	framework.ExpectNoError(err)

	ginkgo.By("Getting k8s nodes")
	k8sNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
	framework.ExpectNoError(err)

	var ok bool
	if clusterName, ok = kind.IsKindProvided(k8sNodes.Items[0].Spec.ProviderID); !ok {
		ginkgo.Fail("vpc-dynamic-routing spec only runs on kind clusters")
	}

	return []byte(clusterName)
}, func(data []byte) {
	clusterName = string(data)
})

var _ = framework.SerialDescribe("[group:vpc-dynamic-routing]", func() {
	f := framework.NewDefaultFramework("vpc-dynamic-routing")

	framework.ConformanceIt("should advertise NAT addresses and learn fabric routes via FRR", func() {
		f.SkipVersionPriorTo(1, 17, "dynamic routing requires v1.17+")
		if !f.HasIPv4() {
			ginkgo.Skip("dynamic routing e2e test requires IPv4 support")
		}

		ginkgo.By("Checking VRF kernel support")
		kindNodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(kindNodes)
		if _, _, err = kindNodes[0].Exec("ip", "link", "add", "vrf-probe", "type", "vrf", "table", "9999"); err != nil {
			ginkgo.Skip("VRF kernel module not available, skipping dynamic routing test")
		}
		_, _, _ = kindNodes[0].Exec("ip", "link", "del", "vrf-probe")

		namespaceName := f.Namespace.Name
		vpcClient := f.VpcClient()
		subnetClient := f.SubnetClient()
		vlanClient := f.VlanClient()
		providerNetworkClient := f.ProviderNetworkClient()
		ovnEipClient := f.OvnEipClient()
		ovnFipClient := f.OvnFipClient()
		podClient := f.PodClient()

		ginkgo.By("Ensuring docker network " + dockerNetworkName + " exists")
		network, err := docker.NetworkCreate(dockerNetworkName, true, true)
		framework.ExpectNoError(err, "creating docker network "+dockerNetworkName)

		ginkgo.By("Connecting kind nodes to docker network " + dockerNetworkName)
		framework.ExpectNoError(kind.NetworkConnect(network.ID, kindNodes))
		kindNodes, err = kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err)

		linkMap := make(map[string]*iproute.Link, len(kindNodes))
		nodeIPMap := make(map[string]string, len(kindNodes))
		for _, node := range kindNodes {
			links, err := node.ListLinks()
			framework.ExpectNoError(err)
			endpoint := node.NetworkSettings.Networks[dockerNetworkName]
			for _, link := range links {
				if link.Address == endpoint.MacAddress.String() {
					linkMap[node.Name()] = &link
					break
				}
			}
			framework.ExpectHaveKey(linkMap, node.Name())
			nodeIPMap[node.Name()] = endpoint.IPAddress.String()
		}

		gwNodeName := kindNodes[0].Name()
		ginkgo.By("Labeling node " + gwNodeName + " as external gateway node")
		e2enode.AddOrUpdateLabelOnNode(f.ClientSet, gwNodeName, util.ExGatewayLabel, "true")
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Removing external gateway label from node " + gwNodeName)
			e2enode.RemoveLabelOffNode(f.ClientSet, gwNodeName, util.ExGatewayLabel)
		})

		providerNetworkName := "external"
		ginkgo.By("Creating provider network " + providerNetworkName)
		var defaultInterface string
		customInterfaces := make(map[string][]string)
		for node, link := range linkMap {
			if defaultInterface == "" {
				defaultInterface = link.IfName
			} else if link.IfName != defaultInterface {
				customInterfaces[link.IfName] = append(customInterfaces[link.IfName], node)
			}
		}
		pn := framework.MakeProviderNetwork(providerNetworkName, false, defaultInterface, customInterfaces, nil)
		_ = providerNetworkClient.CreateSync(pn)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting provider network " + providerNetworkName)
			providerNetworkClient.DeleteSync(providerNetworkName)
		})

		vlanName := "vlan-" + framework.RandomSuffix()
		ginkgo.By("Creating vlan " + vlanName)
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting vlan " + vlanName)
			vlanClient.Delete(vlanName)
		})

		torName := "dynamic-routing-tor-" + framework.RandomSuffix()
		ginkgo.By("Creating FRR ToR container " + torName)
		torInfo, err := docker.ContainerCreate(torName, frrImage, dockerNetworkName, []string{"sleep", "infinity"})
		framework.ExpectNoError(err, "creating FRR ToR container")
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting FRR ToR container " + torName)
			framework.ExpectNoError(docker.ContainerRemove(torInfo.ID))
		})
		torIP := torInfo.NetworkSettings.Networks[dockerNetworkName].IPAddress.String()
		framework.ExpectNotEmpty(torIP)
		framework.Logf("ToR IP: %s", torIP)
		network, err = docker.NetworkInspect(dockerNetworkName)
		framework.ExpectNoError(err)

		externalSubnetName := "external"
		ginkgo.By("Creating external subnet " + externalSubnetName)
		externalSubnet := generateSubnetFromDockerNetwork(externalSubnetName, network)
		externalSubnet.Spec.Vlan = vlanName
		_ = subnetClient.CreateSync(externalSubnet)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting external subnet " + externalSubnetName)
			subnetClient.DeleteSync(externalSubnetName)
		})

		vpcName := "vpc-" + framework.RandomSuffix()
		ginkgo.By("Creating VPC " + vpcName + " with dynamic routing enabled")
		vpc := &apiv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{Name: vpcName},
			Spec: apiv1.VpcSpec{
				EnableExternal: true,
				DynamicRouting: &apiv1.VpcDynamicRouting{
					Enabled:      true,
					Redistribute: []apiv1.RedistributeType{apiv1.RedistributeNAT},
					LocalOnly:    true,
					MaintainVrf:  true,
					VrfName:      vrfName,
					VrfID:        vrfID,
				},
			},
		}
		_ = vpcClient.CreateSync(vpc)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting VPC " + vpcName)
			vpcClient.DeleteSync(vpcName)
		})

		internalSubnetName := "int-" + framework.RandomSuffix()
		ginkgo.By("Creating internal subnet " + internalSubnetName)
		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		internalSubnet := framework.MakeSubnet(internalSubnetName, "", cidr, "", vpcName, "", nil, nil, nil)
		_ = subnetClient.CreateSync(internalSubnet)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting internal subnet " + internalSubnetName)
			subnetClient.DeleteSync(internalSubnetName)
		})

		workloadPodName := "workload-" + framework.RandomSuffix()
		ginkgo.By("Creating workload pod " + workloadPodName)
		annotations := map[string]string{util.LogicalSwitchAnnotation: internalSubnetName}
		workloadPod := framework.MakePrivilegedPod(namespaceName, workloadPodName, nil, annotations, f.KubeOVNImage, []string{"sleep", "infinity"}, nil)
		workloadPod = podClient.CreateSync(workloadPod)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting workload pod " + workloadPodName)
			podClient.DeleteSync(workloadPodName)
		})
		workloadIP := workloadPod.Status.PodIP
		framework.Logf("workload pod IP: %s", workloadIP)

		lrpEipName := fmt.Sprintf("%s-%s", vpcName, externalSubnetName)
		ginkgo.By("Waiting for VPC external LRP EIP " + lrpEipName)
		var lrpIP string
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			eip := ovnEipClient.Get(lrpEipName)
			lrpIP = eip.Status.V4Ip
			return lrpIP != "", nil
		}, "VPC external LRP EIP has an address")
		framework.Logf("VPC external LRP IP: %s", lrpIP)

		eipName := "eip-" + framework.RandomSuffix()
		ginkgo.By("Creating OvnEip " + eipName)
		eip := framework.MakeOvnEip(eipName, externalSubnetName, "", "", "", util.OvnEipTypeNAT)
		_ = ovnEipClient.CreateSync(eip)
		eipCreated := true
		ginkgo.DeferCleanup(func() {
			if eipCreated {
				ginkgo.By("Deleting OvnEip " + eipName)
				ovnEipClient.DeleteSync(eipName)
			}
		})
		eipV4 := ovnEipClient.Get(eipName).Status.V4Ip
		framework.ExpectNotEmpty(eipV4)
		framework.Logf("EIP address: %s", eipV4)

		fipName := "fip-" + framework.RandomSuffix()
		ginkgo.By("Creating OvnFip " + fipName + " mapping " + eipV4 + " to " + workloadIP)
		fip := framework.MakeOvnFip(fipName, eipName, "", "", vpcName, workloadIP)
		_ = ovnFipClient.CreateSync(fip)
		fipCreated := true
		ginkgo.DeferCleanup(func() {
			if fipCreated {
				ginkgo.By("Deleting OvnFip " + fipName)
				ovnFipClient.DeleteSync(fipName)
			}
		})

		gwNode := kindNodes[0]
		ginkgo.By("Verifying ovn-controller mirrors the advertised route into VRF " + vrfName + " on node " + gwNodeName)
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := gwNode.Exec("ip", "route", "show", "vrf", vrfName)
			if err != nil {
				return false, nil
			}
			return strings.Contains(string(stdout), eipV4) && strings.Contains(string(stdout), "proto ovn"), nil
		}, "advertised route mirrored into VRF kernel table")

		ginkgo.By("Configuring FRR on ToR container")
		torConf := fmt.Sprintf(`frr defaults datacenter
router bgp %d
 bgp router-id %s
 no bgp ebgp-requires-policy
 neighbor CHASSIS peer-group
 neighbor CHASSIS remote-as %d
 bgp listen range %s peer-group CHASSIS
 address-family ipv4 unicast
  redistribute connected
 exit-address-family
`, peerASN, torIP, localASN, externalSubnet.Spec.CIDRBlock)
		startFRRInDockerContainer(torInfo.ID, torConf)
		_, _, err = docker.Exec(torInfo.ID, nil, "ip", "addr", "add", remoteLoopbackIP+"/32", "dev", "lo")
		framework.ExpectNoError(err, "adding loopback address on ToR")
		_, _, err = docker.Exec(torInfo.ID, nil, "sysctl", "-w", "net.ipv4.ip_forward=1")
		framework.ExpectNoError(err, "enabling ip forwarding on ToR")

		chassisPodName := "frr-chassis-" + framework.RandomSuffix()
		ginkgo.By("Creating chassis FRR pod " + chassisPodName + " on node " + gwNodeName)
		chassisPod := framework.MakePrivilegedPod(namespaceName, chassisPodName, nil, nil, frrImage, []string{"sh", "-c", "sleep infinity"}, nil)
		chassisPod.Spec.HostNetwork = true
		chassisPod.Spec.NodeName = gwNodeName
		chassisPod.Spec.DNSPolicy = corev1.DNSClusterFirstWithHostNet
		_ = podClient.CreateSync(chassisPod)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting chassis FRR pod " + chassisPodName)
			podClient.DeleteSync(chassisPodName)
		})

		ginkgo.By("Configuring FRR on chassis pod")
		chassisConf := fmt.Sprintf(`frr defaults datacenter
router bgp %d vrf %s
 address-family ipv4 unicast
  redistribute kernel
  redistribute static
  redistribute connected
  import vrf default
 exit-address-family
router bgp %d
 bgp router-id %s
 no bgp ebgp-requires-policy
 neighbor %s remote-as %d
 address-family ipv4 unicast
  neighbor %s activate
  neighbor %s route-map LRP-NEXTHOP out
  import vrf %s
 exit-address-family
route-map LRP-NEXTHOP permit 10
 set ip next-hop %s
route-map OVN-NO-FIB deny 10
ip protocol bgp route-map OVN-NO-FIB
`, localASN, vrfName,
			localASN, nodeIPMap[gwNodeName], torIP, peerASN, torIP, torIP, vrfName, lrpIP)
		startFRRInPod(f, namespaceName, chassisPodName, chassisConf)

		ginkgo.By("Waiting for BGP session between chassis and ToR")
		framework.WaitUntil(3*time.Second, 3*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := framework.ExecCommandInContainer(f, namespaceName, chassisPodName, chassisContainer, "vtysh", "-c", "show bgp summary")
			if err != nil {
				return false, nil
			}
			return strings.Contains(stdout, torIP) &&
				!strings.Contains(stdout, "Active") && !strings.Contains(stdout, "Connect") && !strings.Contains(stdout, "Idle"), nil
		}, "BGP session established between chassis and ToR")

		ginkgo.By("Verifying ToR learns the EIP with the LRP as next hop")
		framework.WaitUntil(3*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := docker.Exec(torInfo.ID, nil, "vtysh", "-c", "show ip route bgp")
			if err != nil {
				return false, nil
			}
			return strings.Contains(string(stdout), eipV4+"/32") && strings.Contains(string(stdout), "via "+lrpIP), nil
		}, "ToR learned EIP route via the VPC LRP")

		ginkgo.By("Verifying the fabric route is learned into the VRF kernel table")
		framework.WaitUntil(3*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := gwNode.Exec("ip", "route", "show", "vrf", vrfName)
			if err != nil {
				return false, nil
			}
			return strings.Contains(string(stdout), remoteLoopbackIP), nil
		}, "fabric route learned into VRF kernel table")

		ginkgo.By("Testing egress connectivity from workload pod to fabric loopback " + remoteLoopbackIP)
		framework.WaitUntil(3*time.Second, 3*time.Minute, func(_ context.Context) (bool, error) {
			output, err := e2epodoutput.RunHostCmd(namespaceName, workloadPodName,
				fmt.Sprintf("ping -c 3 -W 2 %s", remoteLoopbackIP))
			if err != nil {
				return false, nil
			}
			return strings.Contains(output, " 0% packet loss"), nil
		}, "workload pod reaches fabric loopback via learned route")

		ginkgo.By("Testing ingress connectivity from fabric loopback to EIP " + eipV4)
		framework.WaitUntil(3*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := docker.Exec(torInfo.ID, nil, "ping", "-c", "3", "-W", "2", "-I", remoteLoopbackIP, eipV4)
			if err != nil {
				return false, nil
			}
			return strings.Contains(string(stdout), " 0% packet loss"), nil
		}, "fabric reaches EIP via advertised route")

		ginkgo.By("Deleting OvnFip " + fipName + " to verify route withdrawal")
		ovnFipClient.DeleteSync(fipName)
		fipCreated = false
		ovnEipClient.DeleteSync(eipName)
		eipCreated = false

		ginkgo.By("Verifying the advertised route is withdrawn from the VRF kernel table")
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := gwNode.Exec("ip", "route", "show", "vrf", vrfName)
			if err != nil {
				return false, nil
			}
			return !strings.Contains(string(stdout), eipV4), nil
		}, "advertised route removed from VRF kernel table")

		ginkgo.By("Verifying the route is withdrawn on the ToR")
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := docker.Exec(torInfo.ID, nil, "vtysh", "-c", "show ip route bgp")
			if err != nil {
				return false, nil
			}
			return !strings.Contains(string(stdout), eipV4+"/32"), nil
		}, "EIP route withdrawn on ToR")
	})
})

func generateSubnetFromDockerNetwork(subnetName string, network *dockernetwork.Inspect) *apiv1.Subnet {
	ginkgo.GinkgoHelper()

	var cidrV4, gatewayV4 string
	for _, config := range network.IPAM.Config {
		if util.CheckProtocol(config.Subnet.String()) == apiv1.ProtocolIPv4 {
			cidrV4 = config.Subnet.String()
			gatewayV4 = config.Gateway.String()
		}
	}
	framework.ExpectNotEmpty(cidrV4)

	excludeIPs := make([]string, 0, len(network.Containers)+1)
	excludeIPs = append(excludeIPs, gatewayV4)
	for _, container := range network.Containers {
		if container.IPv4Address.IsValid() {
			excludeIPs = append(excludeIPs, container.IPv4Address.Addr().String())
		}
	}

	subnet := framework.MakeSubnet(subnetName, "", cidrV4, gatewayV4, "", "", excludeIPs, nil, nil)
	subnet.Spec.DisableGatewayCheck = true
	return subnet
}

func startFRRInPod(f *framework.Framework, namespace, podName, conf string) {
	ginkgo.GinkgoHelper()

	_, _, err := framework.ExecShellInContainer(f, namespace, podName, chassisContainer,
		"sed -i -e 's/^bgpd=no/bgpd=yes/' /etc/frr/daemons && touch /etc/frr/vtysh.conf")
	framework.ExpectNoError(err, "enabling bgpd on chassis pod")

	encoded := base64.StdEncoding.EncodeToString([]byte(conf))
	_, _, err = framework.ExecShellInContainer(f, namespace, podName, chassisContainer,
		fmt.Sprintf("echo '%s' | base64 -d > /etc/frr/frr.conf", encoded))
	framework.ExpectNoError(err, "writing frr.conf on chassis pod")

	_, _, err = framework.ExecShellInContainer(f, namespace, podName, chassisContainer,
		"nohup /usr/lib/frr/docker-start > /dev/null 2>&1 &")
	framework.ExpectNoError(err, "starting FRR on chassis pod")
}

func startFRRInDockerContainer(containerID, conf string) {
	ginkgo.GinkgoHelper()

	_, _, err := docker.Exec(containerID, nil, "sh", "-c",
		"sed -i -e 's/^bgpd=no/bgpd=yes/' /etc/frr/daemons && touch /etc/frr/vtysh.conf")
	framework.ExpectNoError(err, "enabling bgpd on ToR container")

	encoded := base64.StdEncoding.EncodeToString([]byte(conf))
	_, _, err = docker.Exec(containerID, nil, "sh", "-c",
		fmt.Sprintf("echo '%s' | base64 -d > /etc/frr/frr.conf", encoded))
	framework.ExpectNoError(err, "writing frr.conf on ToR container")

	_, _, err = docker.Exec(containerID, nil, "sh", "-c", "nohup /usr/lib/frr/docker-start > /dev/null 2>&1 &")
	framework.ExpectNoError(err, "starting FRR on ToR container")
}
