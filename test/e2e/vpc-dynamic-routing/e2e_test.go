package vpc_dynamic_routing

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	dockernetwork "github.com/moby/moby/api/types/network"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
	agentVrfName      = "ovnvrf1002"
	agentVrfID        = 1002
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

type drTopology struct {
	kindNodes          []kind.Node
	nodeIPMap          map[string]string
	gwNodeNames        []string
	torID              string
	torIP              string
	externalSubnetName string
	externalCIDR       string
	externalGateway    string
}

type drWorkload struct {
	vpcName         string
	vrfName         string
	tableID         uint32
	workloadPodName string
	workloadIP      string
	lrpIP           string
	eipName         string
	eipV4           string
	fipName         string
	eipCreated      bool
	fipCreated      bool
}

var _ = framework.SerialDescribe("[group:vpc-dynamic-routing]", func() {
	f := framework.NewDefaultFramework("vpc-dynamic-routing")

	framework.ConformanceIt("should advertise NAT addresses and learn fabric routes via FRR", func() {
		f.SkipVersionPriorTo(1, 17, "dynamic routing requires v1.17+")
		if !f.HasIPv4() {
			ginkgo.Skip("dynamic routing e2e test requires IPv4 support")
		}

		topo := setupTopology(f, 1)
		gwNodeName := topo.gwNodeNames[0]
		w := setupVpcWorkload(f, topo, vrfName, vrfID, framework.RandomCIDR(f.ClusterIPFamily), "", []apiv1.RedistributeType{apiv1.RedistributeNAT})

		var gwNode kind.Node
		for _, node := range topo.kindNodes {
			if node.Name() == gwNodeName {
				gwNode = node
			}
		}

		ginkgo.By("Verifying ovn-controller mirrors the advertised route into VRF " + vrfName + " on node " + gwNodeName)
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := gwNode.Exec("ip", "route", "show", "vrf", vrfName)
			if err != nil {
				return false, nil
			}
			return strings.Contains(string(stdout), w.eipV4) && strings.Contains(string(stdout), "proto ovn"), nil
		}, "advertised route mirrored into VRF kernel table")

		namespaceName := f.Namespace.Name
		podClient := f.PodClient()

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
			localASN, topo.nodeIPMap[gwNodeName], topo.torIP, peerASN, topo.torIP, topo.torIP, vrfName, w.lrpIP)
		startFRRInPod(f, namespaceName, chassisPodName, chassisConf)

		ginkgo.By("Waiting for BGP session between chassis and ToR")
		framework.WaitUntil(3*time.Second, 3*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := framework.ExecCommandInContainer(f, namespaceName, chassisPodName, chassisContainer, "vtysh", "-c", "show bgp summary")
			if err != nil {
				return false, nil
			}
			return strings.Contains(stdout, topo.torIP) &&
				!strings.Contains(stdout, "Active") && !strings.Contains(stdout, "Connect") && !strings.Contains(stdout, "Idle"), nil
		}, "BGP session established between chassis and ToR")

		ginkgo.By("Verifying ToR learns the EIP with the LRP as next hop")
		framework.WaitUntil(3*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := docker.Exec(topo.torID, nil, "vtysh", "-c", "show ip route bgp")
			if err != nil {
				return false, nil
			}
			return strings.Contains(string(stdout), w.eipV4+"/32") && strings.Contains(string(stdout), "via "+w.lrpIP), nil
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
			output, err := e2epodoutput.RunHostCmd(namespaceName, w.workloadPodName,
				fmt.Sprintf("ping -c 3 -W 2 %s", remoteLoopbackIP))
			if err != nil {
				return false, nil
			}
			return strings.Contains(output, " 0% packet loss"), nil
		}, "workload pod reaches fabric loopback via learned route")

		ginkgo.By("Testing ingress connectivity from fabric loopback to EIP " + w.eipV4)
		framework.WaitUntil(3*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := docker.Exec(topo.torID, nil, "ping", "-c", "3", "-W", "2", "-I", remoteLoopbackIP, w.eipV4)
			if err != nil {
				return false, nil
			}
			return strings.Contains(string(stdout), " 0% packet loss"), nil
		}, "fabric reaches EIP via advertised route")

		deleteNatAndVerifyWithdrawal(f, topo, w, func() (string, error) {
			stdout, _, err := gwNode.Exec("ip", "route", "show", "vrf", vrfName)
			return string(stdout), err
		})
	})

	framework.ConformanceIt("should manage FRR via kube-ovn-frr agent with gateway failover", func() {
		f.SkipVersionPriorTo(1, 17, "dynamic routing requires v1.17+")
		if !f.HasIPv4() {
			ginkgo.Skip("dynamic routing e2e test requires IPv4 support")
		}

		topo := setupTopology(f, 2)
		w := setupVpcWorkload(f, topo, agentVrfName, agentVrfID, framework.RandomCIDR(f.ClusterIPFamily), "", []apiv1.RedistributeType{apiv1.RedistributeNAT})
		deployAgent(f, topo)

		ginkgo.By("Verifying ToR learns the EIP with the LRP as next hop")
		framework.WaitUntil(3*time.Second, 3*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := docker.Exec(topo.torID, nil, "vtysh", "-c", "show ip route bgp")
			if err != nil {
				return false, nil
			}
			return strings.Contains(string(stdout), w.eipV4+"/32") && strings.Contains(string(stdout), "via "+w.lrpIP), nil
		}, "ToR learned EIP route via the VPC LRP")

		ginkgo.By("Verifying the advertise filter suppresses non-host routes")
		stdout, _, err := docker.Exec(topo.torID, nil, "vtysh", "-c", "show ip route bgp")
		framework.ExpectNoError(err)
		framework.ExpectFalse(strings.Contains(string(stdout), topo.externalCIDR), "external subnet CIDR must not be advertised")

		ginkgo.By("Testing ingress connectivity from ToR to EIP " + w.eipV4)
		framework.WaitUntil(3*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := docker.Exec(topo.torID, nil, "ping", "-c", "3", "-W", "2", w.eipV4)
			if err != nil {
				return false, nil
			}
			return strings.Contains(string(stdout), " 0% packet loss"), nil
		}, "ToR reaches EIP via advertised route")

		if len(topo.gwNodeNames) >= 2 {
			nodeByName := make(map[string]kind.Node, len(topo.kindNodes))
			for _, node := range topo.kindNodes {
				nodeByName[node.Name()] = node
			}
			var activeName, standbyName string
			ginkgo.By("Locating the active gateway chassis")
			framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
				activeName, standbyName = "", ""
				for _, gwName := range topo.gwNodeNames {
					node := nodeByName[gwName]
					if _, _, err := node.Exec("ip", "link", "show", agentVrfName); err == nil {
						activeName = gwName
					} else {
						standbyName = gwName
					}
				}
				return activeName != "" && standbyName != "", nil
			}, "exactly one gateway chassis holds the VRF")
			framework.Logf("active chassis: %s, standby chassis: %s", activeName, standbyName)
			activeNode, standbyNode := nodeByName[activeName], nodeByName[standbyName]

			ginkgo.By("Moving the gateway binding off " + activeNode.Name())
			moveGatewayBinding(f, topo, w, activeNode.Name())

			ginkgo.By("Verifying the VRF and routes move to " + standbyNode.Name())
			framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
				stdout, _, err := standbyNode.Exec("ip", "route", "show", "vrf", agentVrfName)
				if err != nil {
					return false, nil
				}
				return strings.Contains(string(stdout), w.eipV4), nil
			}, "advertised route mirrored on the standby chassis")

			ginkgo.By("Verifying the ToR relearns the EIP from the standby chassis")
			framework.WaitUntil(3*time.Second, 3*time.Minute, func(_ context.Context) (bool, error) {
				stdout, _, err := docker.Exec(topo.torID, nil, "vtysh", "-c", "show bgp ipv4 unicast "+w.eipV4+"/32")
				if err != nil {
					return false, nil
				}
				return bgpPathFromPeer(string(stdout), topo.nodeIPMap[standbyName]) &&
					strings.Contains(string(stdout), w.lrpIP), nil
			}, "ToR relearned the EIP from the standby chassis without an FRR restart")

			ginkgo.By("Testing ingress connectivity after failover")
			framework.WaitUntil(3*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
				stdout, _, err := docker.Exec(topo.torID, nil, "ping", "-c", "3", "-W", "2", w.eipV4)
				if err != nil {
					return false, nil
				}
				return strings.Contains(string(stdout), " 0% packet loss"), nil
			}, "ToR reaches EIP after failover")
		}

		deleteNatAndVerifyWithdrawal(f, topo, w, nil)
	})

	framework.ConformanceIt("should isolate multiple VPCs with overlapping subnets via kube-ovn-frr agent", func() {
		f.SkipVersionPriorTo(1, 17, "dynamic routing requires v1.17+")
		if !f.HasIPv4() {
			ginkgo.Skip("dynamic routing e2e test requires IPv4 support")
		}

		topo := setupTopology(f, 2)
		if len(topo.gwNodeNames) < 2 {
			ginkgo.Skip("multi-vpc isolation test requires at least 2 gateway nodes")
		}

		sharedCIDR := "10.100.0.0/24"
		sharedPodIP := "10.100.0.100"
		static := []apiv1.RedistributeType{apiv1.RedistributeStatic}
		wa := setupVpcWorkload(f, topo, "ovnvrf1101", 1101, sharedCIDR, sharedPodIP, static)
		wb := setupVpcWorkload(f, topo, "ovnvrf1102", 1102, sharedCIDR, sharedPodIP, static)
		wc := setupVpcWorkload(f, topo, "ovnvrf1103", 1103, "10.200.0.0/24", "", static)

		for _, w := range []*drWorkload{wa, wb, wc} {
			ginkgo.By("Adding per-EIP static route for VPC " + w.vpcName)
			setVpcStaticRoutes(f, w.vpcName, []*apiv1.StaticRoute{eipStaticRoute(w, topo)})
		}

		deployAgent(f, topo)

		for _, w := range []*drWorkload{wa, wb, wc} {
			waitTorLearnsEip(topo, w)
		}

		ginkgo.By("Verifying no tenant subnet or pool CIDR is advertised")
		stdout, _, err := docker.Exec(topo.torID, nil, "vtysh", "-c", "show ip route bgp")
		framework.ExpectNoError(err)
		for _, forbidden := range []string{sharedCIDR, "10.200.0.0/24", topo.externalCIDR} {
			framework.ExpectFalse(strings.Contains(string(stdout), forbidden), "CIDR "+forbidden+" must not be advertised")
		}

		ginkgo.By("Verifying per-VPC kernel table isolation on the binding chassis")
		bindings := make(map[string]string, 3)
		for _, w := range []*drWorkload{wa, wb, wc} {
			framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
				node := bindingNode(topo, w)
				if node == "" {
					return false, nil
				}
				bindings[w.vpcName] = node
				return strings.Contains(nodeTableRoutes(topo, node, w.tableID), w.eipV4), nil
			}, "VPC "+w.vpcName+" table holds its own EIP on the binding chassis")
		}
		for _, w := range []*drWorkload{wa, wb, wc} {
			routes := nodeTableRoutes(topo, bindings[w.vpcName], w.tableID)
			for _, other := range []*drWorkload{wa, wb, wc} {
				if other == w {
					continue
				}
				framework.ExpectFalse(strings.Contains(routes, other.eipV4),
					"VPC "+w.vpcName+" table must not contain EIP of "+other.vpcName)
			}
		}

		ginkgo.By("Testing ingress connectivity to all EIPs with overlapping internal addresses")
		for _, w := range []*drWorkload{wa, wb, wc} {
			framework.WaitUntil(3*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
				stdout, _, err := docker.Exec(topo.torID, nil, "ping", "-c", "3", "-W", "2", w.eipV4)
				if err != nil {
					return false, nil
				}
				return strings.Contains(string(stdout), " 0% packet loss"), nil
			}, "ToR reaches EIP "+w.eipV4)
		}

		ginkgo.By("Deleting the FRR agent pod on " + bindings[wa.vpcName] + " and verifying recovery")
		agentPod, err := f.DaemonSetClientNS(framework.KubeOvnNamespace).GetPodOnNode(
			f.DaemonSetClientNS(framework.KubeOvnNamespace).Get("kube-ovn-frr-e2e"), bindings[wa.vpcName])
		framework.ExpectNoError(err, "finding agent pod on binding chassis")
		f.PodClientNS(framework.KubeOvnNamespace).DeleteSync(agentPod.Name)
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := docker.Exec(topo.torID, nil, "vtysh", "-c", "show ip route bgp")
			if err != nil {
				return false, nil
			}
			return !strings.Contains(string(stdout), wa.eipV4+"/32"), nil
		}, "EIP of "+wa.vpcName+" withdrawn while its chassis agent pod is down")
		f.DaemonSetClientNS(framework.KubeOvnNamespace).RolloutStatus("kube-ovn-frr-e2e")
		for _, w := range []*drWorkload{wa, wb, wc} {
			waitTorLearnsEip(topo, w)
		}

		ginkgo.By("Failing over VPC " + wa.vpcName + " only")
		fromNode := bindingNode(topo, wa)
		framework.ExpectNotEmpty(fromNode)
		var toNode string
		for _, gwName := range topo.gwNodeNames {
			if gwName != fromNode {
				toNode = gwName
			}
		}
		moveGatewayBinding(f, topo, wa, fromNode)

		ginkgo.By("Verifying only " + wa.vpcName + " moves while other VPCs keep their paths")
		framework.WaitUntil(3*time.Second, 3*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := docker.Exec(topo.torID, nil, "vtysh", "-c", "show bgp ipv4 unicast "+wa.eipV4+"/32")
			if err != nil {
				return false, nil
			}
			return bgpPathFromPeer(string(stdout), topo.nodeIPMap[toNode]), nil
		}, "EIP of "+wa.vpcName+" relearned from the standby chassis")
		for _, w := range []*drWorkload{wb, wc} {
			stdout, _, err := docker.Exec(topo.torID, nil, "vtysh", "-c", "show bgp ipv4 unicast "+w.eipV4+"/32")
			framework.ExpectNoError(err)
			framework.ExpectTrue(bgpPathFromPeer(string(stdout), topo.nodeIPMap[bindings[w.vpcName]]),
				"EIP of "+w.vpcName+" must still be advertised from its original chassis")
		}

		ginkgo.By("Withdrawing VPC " + wb.vpcName + " only")
		setVpcStaticRoutes(f, wb.vpcName, nil)
		f.OvnFipClient().DeleteSync(wb.fipName)
		wb.fipCreated = false
		f.OvnEipClient().DeleteSync(wb.eipName)
		wb.eipCreated = false
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := docker.Exec(topo.torID, nil, "vtysh", "-c", "show ip route bgp")
			if err != nil {
				return false, nil
			}
			return !strings.Contains(string(stdout), wb.eipV4+"/32"), nil
		}, "EIP of "+wb.vpcName+" withdrawn on ToR")
		for _, w := range []*drWorkload{wa, wc} {
			stdout, _, err := docker.Exec(topo.torID, nil, "vtysh", "-c", "show ip route bgp")
			framework.ExpectNoError(err)
			framework.ExpectTrue(strings.Contains(string(stdout), w.eipV4+"/32"),
				"EIP of "+w.vpcName+" must survive the withdrawal of "+wb.vpcName)
		}
	})
})

func setupTopology(f *framework.Framework, gwNodeCount int) *drTopology {
	ginkgo.GinkgoHelper()

	ginkgo.By("Checking VRF kernel support")
	kindNodes, err := kind.ListNodes(clusterName, "")
	framework.ExpectNoError(err)
	framework.ExpectNotEmpty(kindNodes)
	if _, _, err = kindNodes[0].Exec("ip", "link", "add", "vrf-probe", "type", "vrf", "table", "9999"); err != nil {
		ginkgo.Skip("VRF kernel module not available, skipping dynamic routing test")
	}
	_, _, _ = kindNodes[0].Exec("ip", "link", "del", "vrf-probe")

	subnetClient := f.SubnetClient()
	vlanClient := f.VlanClient()
	providerNetworkClient := f.ProviderNetworkClient()

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

	if gwNodeCount > len(kindNodes) {
		gwNodeCount = len(kindNodes)
	}
	gwNodeNames := make([]string, 0, gwNodeCount)
	for _, node := range kindNodes[:gwNodeCount] {
		gwNodeName := node.Name()
		ginkgo.By("Labeling node " + gwNodeName + " as external gateway node")
		e2enode.AddOrUpdateLabelOnNode(f.ClientSet, gwNodeName, util.ExGatewayLabel, "true")
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Removing external gateway label from node " + gwNodeName)
			e2enode.RemoveLabelOffNode(f.ClientSet, gwNodeName, util.ExGatewayLabel)
		})
		gwNodeNames = append(gwNodeNames, gwNodeName)
	}

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

	ginkgo.By("Ensuring node addresses are present on the external bridge")
	prefixLen := strings.Split(externalSubnet.Spec.CIDRBlock, "/")[1]
	bridgeName := "br-" + providerNetworkName
	for _, node := range kindNodes {
		nodeAddr := nodeIPMap[node.Name()] + "/" + prefixLen
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			if _, _, err := node.Exec("ip", "link", "show", bridgeName); err != nil {
				return false, nil
			}
			_, _, err := node.Exec("ip", "addr", "replace", nodeAddr, "dev", bridgeName)
			return err == nil, nil
		}, "node address "+nodeAddr+" present on "+bridgeName)
	}

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

	return &drTopology{
		kindNodes:          kindNodes,
		nodeIPMap:          nodeIPMap,
		gwNodeNames:        gwNodeNames,
		torID:              torInfo.ID,
		torIP:              torIP,
		externalSubnetName: externalSubnetName,
		externalCIDR:       externalSubnet.Spec.CIDRBlock,
		externalGateway:    externalSubnet.Spec.Gateway,
	}
}

func setupVpcWorkload(f *framework.Framework, topo *drTopology, vrf string, tableID uint32, cidr, podIP string, redistribute []apiv1.RedistributeType) *drWorkload {
	ginkgo.GinkgoHelper()

	namespaceName := f.Namespace.Name
	vpcClient := f.VpcClient()
	subnetClient := f.SubnetClient()
	ovnEipClient := f.OvnEipClient()
	ovnFipClient := f.OvnFipClient()
	podClient := f.PodClient()

	w := &drWorkload{vrfName: vrf, tableID: tableID}

	w.vpcName = "vpc-" + framework.RandomSuffix()
	ginkgo.By("Creating VPC " + w.vpcName + " with dynamic routing enabled")
	vpc := &apiv1.Vpc{
		ObjectMeta: metav1.ObjectMeta{Name: w.vpcName},
		Spec: apiv1.VpcSpec{
			EnableExternal: true,
			DynamicRouting: &apiv1.VpcDynamicRouting{
				Enabled:      true,
				Redistribute: redistribute,
				LocalOnly:    true,
				MaintainVrf:  true,
				VrfName:      vrf,
				VrfID:        tableID,
			},
		},
	}
	_ = vpcClient.CreateSync(vpc)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Deleting VPC " + w.vpcName)
		vpcClient.DeleteSync(w.vpcName)
	})

	internalSubnetName := "int-" + framework.RandomSuffix()
	ginkgo.By("Creating internal subnet " + internalSubnetName + " with CIDR " + cidr)
	internalSubnet := framework.MakeSubnet(internalSubnetName, "", cidr, "", w.vpcName, "", nil, nil, nil)
	_ = subnetClient.CreateSync(internalSubnet)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Deleting internal subnet " + internalSubnetName)
		subnetClient.DeleteSync(internalSubnetName)
	})

	w.workloadPodName = "workload-" + framework.RandomSuffix()
	ginkgo.By("Creating workload pod " + w.workloadPodName)
	annotations := map[string]string{util.LogicalSwitchAnnotation: internalSubnetName}
	if podIP != "" {
		annotations[util.IPAddressAnnotation] = podIP
	}
	workloadPod := framework.MakePrivilegedPod(namespaceName, w.workloadPodName, nil, annotations, f.KubeOVNImage, []string{"sleep", "infinity"}, nil)
	workloadPod = podClient.CreateSync(workloadPod)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Deleting workload pod " + w.workloadPodName)
		podClient.DeleteSync(w.workloadPodName)
	})
	w.workloadIP = workloadPod.Status.PodIP
	framework.Logf("workload pod IP: %s", w.workloadIP)

	lrpEipName := fmt.Sprintf("%s-%s", w.vpcName, topo.externalSubnetName)
	ginkgo.By("Waiting for VPC external LRP EIP " + lrpEipName)
	framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		eip := ovnEipClient.Get(lrpEipName)
		w.lrpIP = eip.Status.V4Ip
		return w.lrpIP != "", nil
	}, "VPC external LRP EIP has an address")
	framework.Logf("VPC external LRP IP: %s", w.lrpIP)

	w.eipName = "eip-" + framework.RandomSuffix()
	ginkgo.By("Creating OvnEip " + w.eipName)
	eip := framework.MakeOvnEip(w.eipName, topo.externalSubnetName, "", "", "", util.OvnEipTypeNAT)
	_ = ovnEipClient.CreateSync(eip)
	w.eipCreated = true
	ginkgo.DeferCleanup(func() {
		if w.eipCreated {
			ginkgo.By("Deleting OvnEip " + w.eipName)
			ovnEipClient.DeleteSync(w.eipName)
		}
	})
	w.eipV4 = ovnEipClient.Get(w.eipName).Status.V4Ip
	framework.ExpectNotEmpty(w.eipV4)
	framework.Logf("EIP address: %s", w.eipV4)

	w.fipName = "fip-" + framework.RandomSuffix()
	ginkgo.By("Creating OvnFip " + w.fipName + " mapping " + w.eipV4 + " to " + w.workloadIP)
	fip := framework.MakeOvnFip(w.fipName, w.eipName, "", "", w.vpcName, w.workloadIP)
	_ = ovnFipClient.CreateSync(fip)
	w.fipCreated = true
	ginkgo.DeferCleanup(func() {
		if w.fipCreated {
			ginkgo.By("Deleting OvnFip " + w.fipName)
			ovnFipClient.DeleteSync(w.fipName)
		}
	})

	return w
}

func deleteNatAndVerifyWithdrawal(f *framework.Framework, topo *drTopology, w *drWorkload, vrfRoutes func() (string, error)) {
	ginkgo.GinkgoHelper()

	ovnEipClient := f.OvnEipClient()
	ovnFipClient := f.OvnFipClient()

	ginkgo.By("Deleting OvnFip " + w.fipName + " to verify route withdrawal")
	ovnFipClient.DeleteSync(w.fipName)
	w.fipCreated = false
	ovnEipClient.DeleteSync(w.eipName)
	w.eipCreated = false

	if vrfRoutes != nil {
		ginkgo.By("Verifying the advertised route is withdrawn from the VRF kernel table")
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, err := vrfRoutes()
			if err != nil {
				return false, nil
			}
			return !strings.Contains(stdout, w.eipV4), nil
		}, "advertised route removed from VRF kernel table")
	}

	ginkgo.By("Verifying the route is withdrawn on the ToR")
	framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		stdout, _, err := docker.Exec(topo.torID, nil, "vtysh", "-c", "show ip route bgp")
		if err != nil {
			return false, nil
		}
		return !strings.Contains(string(stdout), w.eipV4+"/32"), nil
	}, "EIP route withdrawn on ToR")
}

func makeAgentDaemonSet(name, kubeOvnImage string) *appsv1.DaemonSet {
	nodeNameEnv := corev1.EnvVar{
		Name: "NODE_NAME",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		},
	}
	frrVolumeMount := corev1.VolumeMount{Name: "frr-config", MountPath: "/etc/frr"}
	labels := map[string]string{"app": name}

	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: framework.KubeOvnNamespace},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					HostNetwork:                  true,
					ServiceAccountName:           name,
					AutomountServiceAccountToken: new(false),
					NodeSelector:                 map[string]string{util.ExGatewayLabel: "true"},
					Tolerations:                  []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
					SecurityContext: &corev1.PodSecurityContext{
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					InitContainers: []corev1.Container{{
						Name:         "init-frr",
						Image:        kubeOvnImage,
						Command:      []string{"/kube-ovn/kube-ovn-frr", "init"},
						Env:          []corev1.EnvVar{nodeNameEnv},
						VolumeMounts: []corev1.VolumeMount{frrVolumeMount},
					}},
					Containers: []corev1.Container{{
						Name:    "frr",
						Image:   frrImage,
						Command: []string{"/bin/sh", "-c", "/usr/lib/frr/docker-start & exec sh /etc/frr/kube-ovn-reload.sh"},
						SecurityContext: &corev1.SecurityContext{
							Capabilities: &corev1.Capabilities{
								Add: []corev1.Capability{"NET_ADMIN", "NET_RAW", "NET_BIND_SERVICE", "SYS_ADMIN"},
							},
						},
						VolumeMounts: []corev1.VolumeMount{frrVolumeMount},
					}, {
						Name:    "kube-ovn-frr",
						Image:   kubeOvnImage,
						Command: []string{"/kube-ovn/kube-ovn-frr"},
						Env:     []corev1.EnvVar{nodeNameEnv},
						VolumeMounts: []corev1.VolumeMount{frrVolumeMount, {
							Name:      "kube-ovn-log",
							MountPath: "/var/log/kube-ovn",
						}, {
							Name:      "serviceaccount",
							MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
							ReadOnly:  true,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name:         "frr-config",
						VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
					}, {
						Name:         "kube-ovn-log",
						VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
					}, {
						Name: "serviceaccount",
						VolumeSource: corev1.VolumeSource{
							Projected: &corev1.ProjectedVolumeSource{
								Sources: []corev1.VolumeProjection{{
									ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
										Path:              "token",
										ExpirationSeconds: new(int64(3600)),
									},
								}, {
									ConfigMap: &corev1.ConfigMapProjection{
										LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"},
										Items:                []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
									},
								}, {
									DownwardAPI: &corev1.DownwardAPIProjection{
										Items: []corev1.DownwardAPIVolumeFile{{
											Path:     "namespace",
											FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
										}},
									},
								}},
							},
						},
					}},
				},
			},
		},
	}
}

func deployAgent(f *framework.Framework, topo *drTopology) {
	ginkgo.GinkgoHelper()

	bgpConfName := "kube-ovn-frr-" + framework.RandomSuffix()
	ginkgo.By("Creating BgpConf " + bgpConfName)
	bgpConfClient := f.BgpConfClient()
	bgpConf := &apiv1.BgpConf{
		ObjectMeta: metav1.ObjectMeta{Name: bgpConfName},
		Spec: apiv1.BgpConfSpec{
			LocalASN:        localASN,
			PeerASN:         peerASN,
			NodeSelector:    map[string]string{util.ExGatewayLabel: "true"},
			Peers:           []apiv1.BgpPeer{{Address: topo.torIP}},
			AdvertiseFilter: []string{topo.externalCIDR + " ge 32 le 32"},
		},
	}
	_ = bgpConfClient.Create(bgpConf)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Deleting BgpConf " + bgpConfName)
		bgpConfClient.Delete(bgpConfName)
	})

	agentName := "kube-ovn-frr-e2e"
	ginkgo.By("Creating RBAC for agent " + agentName)
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: agentName, Namespace: framework.KubeOvnNamespace}}
	_, err := f.ClientSet.CoreV1().ServiceAccounts(framework.KubeOvnNamespace).Create(context.TODO(), sa, metav1.CreateOptions{})
	framework.ExpectNoError(err, "creating agent service account")
	ginkgo.DeferCleanup(func() {
		framework.ExpectNoError(f.ClientSet.CoreV1().ServiceAccounts(framework.KubeOvnNamespace).Delete(context.TODO(), agentName, metav1.DeleteOptions{}))
	})
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: agentName},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"kubeovn.io"},
			Resources: []string{"bgp-confs", "vpcs", "ovn-eips"},
			Verbs:     []string{"get", "list", "watch"},
		}, {
			APIGroups: []string{""},
			Resources: []string{"nodes", "pods"},
			Verbs:     []string{"get", "list", "watch"},
		}},
	}
	_, err = f.ClientSet.RbacV1().ClusterRoles().Create(context.TODO(), role, metav1.CreateOptions{})
	framework.ExpectNoError(err, "creating agent cluster role")
	ginkgo.DeferCleanup(func() {
		framework.ExpectNoError(f.ClientSet.RbacV1().ClusterRoles().Delete(context.TODO(), agentName, metav1.DeleteOptions{}))
	})
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: agentName},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: agentName},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: agentName, Namespace: framework.KubeOvnNamespace}},
	}
	_, err = f.ClientSet.RbacV1().ClusterRoleBindings().Create(context.TODO(), binding, metav1.CreateOptions{})
	framework.ExpectNoError(err, "creating agent cluster role binding")
	ginkgo.DeferCleanup(func() {
		framework.ExpectNoError(f.ClientSet.RbacV1().ClusterRoleBindings().Delete(context.TODO(), agentName, metav1.DeleteOptions{}))
	})

	ginkgo.By("Deploying agent DaemonSet " + agentName)
	agentDS := makeAgentDaemonSet(agentName, f.KubeOVNImage)
	_, err = f.ClientSet.AppsV1().DaemonSets(framework.KubeOvnNamespace).Create(context.TODO(), agentDS, metav1.CreateOptions{})
	framework.ExpectNoError(err, "creating agent daemonset")
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Deleting agent DaemonSet " + agentName)
		err := f.ClientSet.AppsV1().DaemonSets(framework.KubeOvnNamespace).Delete(context.TODO(), agentName, metav1.DeleteOptions{})
		framework.ExpectNoError(err, "deleting agent daemonset")
	})
	f.DaemonSetClientNS(framework.KubeOvnNamespace).RolloutStatus(agentName)
}

func setVpcStaticRoutes(f *framework.Framework, vpcName string, routes []*apiv1.StaticRoute) {
	ginkgo.GinkgoHelper()

	vpc := f.VpcClient().Get(vpcName).DeepCopy()
	vpc.Spec.StaticRoutes = routes
	_, err := f.KubeOVNClientSet.KubeovnV1().Vpcs().Update(context.TODO(), vpc, metav1.UpdateOptions{})
	framework.ExpectNoError(err, "updating static routes of vpc "+vpcName)
}

func eipStaticRoute(w *drWorkload, topo *drTopology) *apiv1.StaticRoute {
	return &apiv1.StaticRoute{
		Policy:    apiv1.PolicyDst,
		CIDR:      w.eipV4 + "/32",
		NextHopIP: topo.externalGateway,
	}
}

func bindingNode(topo *drTopology, w *drWorkload) string {
	for _, gwName := range topo.gwNodeNames {
		for _, node := range topo.kindNodes {
			if node.Name() != gwName {
				continue
			}
			if _, _, err := node.Exec("ip", "link", "show", w.vrfName); err == nil {
				return gwName
			}
		}
	}
	return ""
}

func nodeTableRoutes(topo *drTopology, nodeName string, tableID uint32) string {
	for _, node := range topo.kindNodes {
		if node.Name() != nodeName {
			continue
		}
		stdout, _, err := node.Exec("ip", "route", "show", "table", strconv.FormatUint(uint64(tableID), 10))
		if err != nil {
			return ""
		}
		return string(stdout)
	}
	return ""
}

func waitTorLearnsEip(topo *drTopology, w *drWorkload) {
	ginkgo.GinkgoHelper()

	ginkgo.By("Verifying ToR learns EIP " + w.eipV4 + " via LRP " + w.lrpIP)
	framework.WaitUntil(3*time.Second, 3*time.Minute, func(_ context.Context) (bool, error) {
		stdout, _, err := docker.Exec(topo.torID, nil, "vtysh", "-c", "show ip route bgp")
		if err != nil {
			return false, nil
		}
		return strings.Contains(string(stdout), w.eipV4+"/32") && strings.Contains(string(stdout), "via "+w.lrpIP), nil
	}, "ToR learned EIP "+w.eipV4+" via the owning VPC LRP")
}

func moveGatewayBinding(f *framework.Framework, topo *drTopology, w *drWorkload, fromNode string) {
	ginkgo.GinkgoHelper()

	ovnCentralPod := getOvnCentralPod(f)
	chassisName, _, err := framework.ExecCommandInContainer(f, framework.KubeOvnNamespace, ovnCentralPod, "ovn-central",
		"ovn-sbctl", "--data=bare", "--no-heading", "--columns=name", "find", "chassis", "hostname="+fromNode)
	framework.ExpectNoError(err, "resolving chassis name")
	chassisName = strings.TrimSpace(chassisName)
	framework.ExpectNotEmpty(chassisName)
	lrpName := fmt.Sprintf("%s-%s", w.vpcName, topo.externalSubnetName)
	_, _, err = framework.ExecCommandInContainer(f, framework.KubeOvnNamespace, ovnCentralPod, "ovn-central",
		"ovn-nbctl", "lrp-del-gateway-chassis", lrpName, chassisName)
	framework.ExpectNoError(err, "removing gateway chassis from LRP")
}

func bgpPathFromPeer(showOutput, peerIP string) bool {
	for line := range strings.SplitSeq(showOutput, "\n") {
		if strings.Contains(line, " from ") && strings.Contains(line, peerIP) {
			return true
		}
	}
	return false
}

func getOvnCentralPod(f *framework.Framework) string {
	ginkgo.GinkgoHelper()

	pods, err := f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app=ovn-central",
	})
	framework.ExpectNoError(err, "listing ovn-central pods")
	framework.ExpectNotEmpty(pods.Items)
	return pods.Items[0].Name
}

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
