package multus

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"
)

const (
	frrImage      = "quay.io/frrouting/frr:10.5.1"
	evpnLocalASN  = uint32(65002)
	evpnPeerASN   = uint32(65001)
	evpnVNI       = uint32(1016)
	evpnRT        = "65000:1016"
	backendCIDR   = "10.99.0.0/24"
	backendIP     = "10.99.0.1/24"
	backendPingIP = "10.99.0.1"
	// container name used by framework.MakePrivilegedPod
	frrPeerContainerName = "container"
)

var _ = framework.SerialDescribe("[group:veg-evpn]", func() {
	f := framework.NewDefaultFramework("veg-evpn")

	var namespaceName string
	var replicas int32

	ginkgo.BeforeEach(func() {
		namespaceName = f.Namespace.Name

		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), f.ClientSet)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)
		replicas = min(int32(len(nodeList.Items)), 3)
	})

	framework.ConformanceIt("should establish EVPN session and route traffic through VXLAN tunnel", func() {
		f.SkipVersionPriorTo(1, 16, "EVPN feature requires v1.16+")

		ginkgo.By("Checking VRF kernel support")
		kindNodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(kindNodes)
		_, _, err = kindNodes[0].Exec("ip", "link", "add", "vrf-test", "type", "vrf", "table", "9999")
		if err != nil {
			ginkgo.Skip("VRF kernel module not available, skipping EVPN test")
		}
		_, _, _ = kindNodes[0].Exec("ip", "link", "del", "vrf-test")

		if !f.HasIPv4() {
			ginkgo.Skip("EVPN e2e test requires IPv4 support")
		}

		vpcClient := f.VpcClient()
		subnetClient := f.SubnetClient()
		nadClient := f.NetworkAttachmentDefinitionClient()
		bgpConfClient := f.BgpConfClient()
		evpnConfClient := f.EvpnConfClient()
		vegClient := f.VpcEgressGatewayClient()
		deployClient := f.DeploymentClient()

		// Phase 1: Kubernetes resource setup
		// FRR peer must be a K8s pod with macvlan attachment (not a docker container)
		// because macvlan bridge mode doesn't allow communication between
		// macvlan sub-interfaces and the parent's docker bridge.

		nadName := "nad-" + framework.RandomSuffix()
		provider := fmt.Sprintf("%s.%s", nadName, namespaceName)

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeMacvlanNetworkAttachmentDefinition(nadName, namespaceName, "eth0", "bridge", provider, nil)
		_ = nadClient.Create(nad)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting network attachment definition " + nadName)
			nadClient.Delete(nadName)
		})

		ginkgo.By("Getting docker network " + kindNetwork)
		networkInfo, err := docker.NetworkInspect(kindNetwork)
		framework.ExpectNoError(err, "getting docker network "+kindNetwork)

		externalSubnetName := "ext-" + framework.RandomSuffix()
		ginkgo.By("Creating external subnet " + externalSubnetName)
		externalSubnet := generateSubnetFromDockerNetwork(externalSubnetName, networkInfo, true, false)
		externalSubnet.Spec.Provider = provider
		_ = subnetClient.CreateSync(externalSubnet)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting external subnet " + externalSubnetName)
			subnetClient.DeleteSync(externalSubnetName)
		})

		// Create FRR peer as a K8s pod with macvlan attachment so it can communicate
		// with VEG pods (macvlan sub-interfaces can only talk to other macvlan sub-interfaces)
		frrPeerPodName := "frr-peer-" + framework.RandomSuffix()
		ginkgo.By("Creating FRR peer pod " + frrPeerPodName)
		attachmentNetworkName := fmt.Sprintf("%s/%s", namespaceName, nadName)
		frrPeerAnnotations := map[string]string{
			"k8s.v1.cni.cncf.io/networks": attachmentNetworkName,
		}
		frrPeerPod := framework.MakePrivilegedPod(namespaceName, frrPeerPodName, nil, frrPeerAnnotations,
			frrImage, []string{"sh", "-c", "sleep infinity"}, nil)
		frrPeerPod = f.PodClient().CreateSync(frrPeerPod)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting FRR peer pod " + frrPeerPodName)
			f.PodClient().DeleteSync(frrPeerPodName)
		})

		ginkgo.By("Getting FRR peer macvlan IP")
		frrPeerIPs, err := util.PodAttachmentIPs(frrPeerPod, attachmentNetworkName)
		framework.ExpectNoError(err, "getting FRR peer attachment IPs")
		framework.ExpectNotEmpty(frrPeerIPs, "FRR peer should have macvlan IP")
		frrPeerIP := frrPeerIPs[0]
		framework.Logf("FRR peer macvlan IP: %s", frrPeerIP)

		// Get the external subnet CIDR for bgp listen range
		extSubnet := subnetClient.Get(externalSubnetName)
		externalCIDR := extSubnet.Spec.CIDRBlock
		framework.Logf("External subnet CIDR (bgp listen range): %s", externalCIDR)

		ginkgo.By("Setting up VRF and VXLAN on FRR peer pod")
		setupFRRPeerNetworkingViaPod(f, namespaceName, frrPeerPodName, frrPeerIP)

		ginkgo.By("Configuring FRR on peer pod")
		configureFRRPeerViaPod(f, namespaceName, frrPeerPodName, frrPeerIP, externalCIDR)

		ginkgo.By("Waiting for FRR daemon to be ready on peer pod")
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, stderr, err := framework.ExecCommandInContainer(f, namespaceName, frrPeerPodName, frrPeerContainerName, "vtysh", "-c", "show bgp summary")
			if err != nil {
				framework.Logf("FRR peer vtysh error: %v, stderr=%s", err, stderr)
				return false, nil
			}
			framework.Logf("FRR peer bgp summary: stdout=[%s]", stdout)
			return !strings.Contains(stdout, "is not running") && !strings.Contains(stdout, "failed to connect") &&
				(strings.Contains(stdout, "Neighbor") || strings.Contains(stdout, "No BGP")), nil
		}, "FRR daemon ready on peer pod")

		vpcName := "vpc-" + framework.RandomSuffix()
		ginkgo.By("Creating VPC " + vpcName)
		vpc := &apiv1.Vpc{ObjectMeta: metav1.ObjectMeta{Name: vpcName}}
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

		bgpConfName := "bgp-conf-" + framework.RandomSuffix()
		ginkgo.By("Creating BgpConf " + bgpConfName)
		bgpConf := framework.MakeBgpConf(bgpConfName, evpnLocalASN, evpnPeerASN, []string{frrPeerIP},
			30*time.Second, 10*time.Second, 5*time.Second, true)
		bgpConfClient.Create(bgpConf)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting BgpConf " + bgpConfName)
			bgpConfClient.DeleteSync(bgpConfName)
		})

		evpnConfName := "evpn-conf-" + framework.RandomSuffix()
		ginkgo.By("Creating EvpnConf " + evpnConfName)
		evpnConf := framework.MakeEvpnConf(evpnConfName, evpnVNI, []string{evpnRT})
		evpnConfClient.Create(evpnConf)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting EvpnConf " + evpnConfName)
			evpnConfClient.DeleteSync(evpnConfName)
		})

		vegName := "veg-" + framework.RandomSuffix()
		ginkgo.By("Creating VpcEgressGateway " + vegName + " with EVPN")
		veg := framework.MakeVpcEgressGateway(namespaceName, vegName, vpcName, replicas, internalSubnetName, externalSubnetName)
		veg.Spec.BgpConf = bgpConfName
		veg.Spec.EvpnConf = evpnConfName
		veg.Spec.Policies = []apiv1.VpcEgressGatewayPolicy{{
			SNAT:    false,
			Subnets: []string{internalSubnetName},
		}}
		veg = vegClient.CreateSync(veg)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting VpcEgressGateway " + vegName)
			vegClient.DeleteSync(vegName)
		})

		// Phase 3: Verification

		ginkgo.By("Validating VpcEgressGateway status")
		framework.ExpectTrue(veg.Status.Ready)
		framework.ExpectEqual(veg.Status.Phase, apiv1.PhaseCompleted)
		framework.ExpectHaveLen(veg.Status.InternalIPs, int(replicas))
		framework.ExpectHaveLen(veg.Status.ExternalIPs, int(replicas))

		ginkgo.By("Validating workload deployment")
		deploy := deployClient.Get(veg.Status.Workload.Name)
		framework.ExpectEqual(deploy.Status.ReadyReplicas, replicas)
		workloadPods, err := deployClient.GetPods(deploy)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(workloadPods.Items, int(replicas))

		ginkgo.By("Verifying FRR sidecar container exists in workload pods")
		for _, pod := range workloadPods.Items {
			var hasFRR bool
			for _, c := range pod.Spec.Containers {
				if c.Name == "frr" {
					hasFRR = true
					break
				}
			}
			framework.ExpectTrue(hasFRR, "pod %s should have FRR sidecar container", pod.Name)
		}

		ginkgo.By("Verifying VXLAN and VRF interfaces in VEG pods")
		for _, pod := range workloadPods.Items {
			mainContainer := pod.Spec.Containers[0].Name

			// Check VRF exists
			stdout, _, err := framework.ExecShellInContainer(f, namespaceName, pod.Name, mainContainer, "ip link show vrf-vpn")
			framework.ExpectNoError(err, "checking VRF in pod %s", pod.Name)
			framework.ExpectContainSubstring(stdout, "vrf-vpn")

			// Check VXLAN interface exists with correct VNI
			stdout, _, err = framework.ExecShellInContainer(f, namespaceName, pod.Name, mainContainer, "ip -d link show vxlan-vpn")
			framework.ExpectNoError(err, "checking VXLAN in pod %s", pod.Name)
			framework.ExpectContainSubstring(stdout, fmt.Sprintf("vxlan id %d", evpnVNI))

			// Check eth0 is enslaved to VRF
			stdout, _, err = framework.ExecShellInContainer(f, namespaceName, pod.Name, mainContainer, "ip link show eth0")
			framework.ExpectNoError(err, "checking eth0 VRF membership in pod %s", pod.Name)
			framework.ExpectContainSubstring(stdout, "master vrf-vpn")
		}

		ginkgo.By("Checking network connectivity from VEG pods to FRR peer " + frrPeerIP)
		for _, pod := range workloadPods.Items {
			stdout, _, _ := framework.ExecShellInContainer(f, namespaceName, pod.Name, "frr", "ip -4 addr show net1")
			framework.Logf("Pod %s net1 addr:\n%s", pod.Name, stdout)
			stdout, _, _ = framework.ExecShellInContainer(f, namespaceName, pod.Name, "frr",
				fmt.Sprintf("ping -c 2 -W 2 %s 2>&1 || true", frrPeerIP))
			framework.Logf("Pod %s ping to FRR peer %s:\n%s", pod.Name, frrPeerIP, stdout)
		}
		// Check FRR peer's bgp state
		peerBGP, _, _ := framework.ExecCommandInContainer(f, namespaceName, frrPeerPodName, frrPeerContainerName, "vtysh", "-c", "show bgp summary")
		framework.Logf("External FRR peer BGP summary:\n%s", peerBGP)

		ginkgo.By("Waiting for BGP sessions to be established in VEG pods")
		framework.WaitUntil(5*time.Second, 3*time.Minute, func(_ context.Context) (bool, error) {
			for _, pod := range workloadPods.Items {
				stdout, stderr, err := framework.ExecCommandInContainer(f, namespaceName, pod.Name, "frr", "vtysh", "-c", "show bgp summary")
				if err != nil {
					framework.Logf("BGP summary exec error on pod %s: err=%v, stderr=%s", pod.Name, err, stderr)
					return false, nil
				}
				framework.Logf("BGP summary on pod %s: stdout=[%s] stderr=[%s]", pod.Name, stdout, stderr)
				// When BGP session is established, FRR shows prefix count instead of state name.
				// Check that neighbor line exists and doesn't show "Active"/"Connect"/"Idle" states.
				if !strings.Contains(stdout, frrPeerIP) ||
					strings.Contains(stdout, "Active") || strings.Contains(stdout, "Connect") || strings.Contains(stdout, "Idle") {
					return false, nil
				}
			}
			return true, nil
		}, "BGP sessions established in VEG pods")

		ginkgo.By("Verifying BGP session on external FRR peer")
		framework.WaitUntil(3*time.Second, 3*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := framework.ExecCommandInContainer(f, namespaceName, frrPeerPodName, frrPeerContainerName, "vtysh", "-c", "show bgp summary")
			if err != nil {
				return false, nil
			}
			framework.Logf("FRR peer BGP summary:\n%s", stdout)
			// When established, shows prefix count; when not, shows "Active"/"Connect"/"Idle"
			return strings.Contains(stdout, "Neighbor") &&
				!strings.Contains(stdout, "Active") && !strings.Contains(stdout, "Connect") && !strings.Contains(stdout, "Idle"), nil
		}, "BGP session established on external FRR peer")

		ginkgo.By("Verifying EVPN routes learned in VEG pods")
		framework.WaitUntil(3*time.Second, 3*time.Minute, func(_ context.Context) (bool, error) {
			for _, pod := range workloadPods.Items {
				stdout, _, err := framework.ExecCommandInContainer(f, namespaceName, pod.Name, "frr", "vtysh", "-c", "show bgp l2vpn evpn")
				if err != nil {
					return false, nil
				}
				if !strings.Contains(stdout, "10.99.0") {
					return false, nil
				}
			}
			return true, nil
		}, "EVPN routes learned in VEG pods")

		ginkgo.By("Verifying VRF route table contains backend routes in VEG pods")
		for _, pod := range workloadPods.Items {
			stdout, _, err := framework.ExecCommandInContainer(f, namespaceName, pod.Name, "frr", "vtysh", "-c", "show ip route vrf vrf-vpn")
			framework.ExpectNoError(err, "checking VRF routes in pod %s", pod.Name)
			framework.ExpectContainSubstring(stdout, "10.99.0")
		}

		ginkgo.By("Verifying external FRR peer learned internal subnet routes")
		framework.WaitUntil(3*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err := framework.ExecCommandInContainer(f, namespaceName, frrPeerPodName, frrPeerContainerName, "vtysh", "-c", "show ip route vrf vrf-vpn")
			if err != nil {
				return false, nil
			}
			// The internal subnet CIDR should be learned via EVPN
			return strings.Contains(stdout, strings.Split(cidr, "/")[0]) ||
				strings.Contains(stdout, strings.Split(cidr, ",")[0]), nil
		}, "external FRR peer learned internal subnet routes")

		// Phase 4: End-to-end connectivity test

		ginkgo.By("Creating client pod for connectivity test")
		clientPodName := "client-" + framework.RandomSuffix()
		annotations := map[string]string{util.LogicalSwitchAnnotation: internalSubnetName}
		image := workloadPods.Items[0].Spec.Containers[0].Image
		clientPod := framework.MakePrivilegedPod(namespaceName, clientPodName, nil, annotations, image, []string{"sleep", "infinity"}, nil)
		_ = f.PodClient().CreateSync(clientPod)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting client pod " + clientPodName)
			f.PodClient().DeleteSync(clientPodName)
		})

		ginkgo.By("Recording FRR peer VXLAN RX statistics before connectivity test")
		peerRxBefore := getVxlanPackets(f, namespaceName, frrPeerPodName, frrPeerContainerName, "RX")
		framework.Logf("Before ping: Peer vxlan-vpn RX=%d", peerRxBefore)

		ginkgo.By("Testing connectivity from client pod to backend network via EVPN tunnel")
		framework.WaitUntil(3*time.Second, 3*time.Minute, func(_ context.Context) (bool, error) {
			output, err := e2epodoutput.RunHostCmd(namespaceName, clientPodName,
				fmt.Sprintf("ping -c 3 -W 2 %s", backendPingIP))
			if err != nil {
				return false, nil
			}
			return strings.Contains(output, " 0% packet loss") || strings.Contains(output, "3 received"), nil
		}, "ping from client pod to backend network "+backendPingIP+" via EVPN tunnel")

		ginkgo.By("Verifying VXLAN encapsulation via FRR peer RX packet count increase")
		peerRxAfter := getVxlanPackets(f, namespaceName, frrPeerPodName, frrPeerContainerName, "RX")
		framework.Logf("After ping: Peer vxlan-vpn RX=%d", peerRxAfter)
		framework.ExpectTrue(peerRxAfter > peerRxBefore,
			"FRR peer vxlan-vpn RX packets should increase, proving traffic was VXLAN-encapsulated (before=%d, after=%d)", peerRxBefore, peerRxAfter)
	})
})

func setupFRRPeerNetworkingViaPod(f *framework.Framework, namespace, podName, localIP string) {
	ginkgo.GinkgoHelper()

	cmds := []string{
		"sysctl -w net.ipv4.ip_forward=1",
		"ip link add vrf-vpn type vrf table 2000",
		"ip link set vrf-vpn up",
		"ip link add br-vpn type bridge",
		"ip link set br-vpn master vrf-vpn",
		"ip link set br-vpn up",
		fmt.Sprintf("ip link add vxlan-vpn type vxlan id %d dstport 4789 local %s", evpnVNI, localIP),
		"ip link set vxlan-vpn master br-vpn",
		"ip link set vxlan-vpn up",
		"ip addr add " + backendIP + " dev vrf-vpn",
	}

	for _, cmd := range cmds {
		stdout, stderr, err := framework.ExecShellInContainer(f, namespace, podName, frrPeerContainerName, cmd)
		framework.ExpectNoError(err, "failed to run %q on FRR peer (stdout=%s, stderr=%s)", cmd, stdout, stderr)
	}
}

func writeFileViaPod(f *framework.Framework, namespace, podName, path, content string) {
	ginkgo.GinkgoHelper()
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	_, _, err := framework.ExecShellInContainer(f, namespace, podName, frrPeerContainerName,
		fmt.Sprintf("echo '%s' | base64 -d > %s", encoded, path))
	framework.ExpectNoError(err, "writing file %s", path)
}

func configureFRRPeerViaPod(f *framework.Framework, namespace, podName, routerID, listenRange string) {
	ginkgo.GinkgoHelper()

	frrConf := fmt.Sprintf(`frr version 10.5.1
frr defaults traditional
hostname frr-peer
log file /etc/frr/frr.log informational

vrf vrf-vpn
  vni %d
exit-vrf

router bgp %d
  no bgp ebgp-requires-policy
  no bgp network import-check
  no bgp default ipv4-unicast
  bgp router-id %s
  neighbor NEIGHBORS peer-group
  neighbor NEIGHBORS remote-as %d
  neighbor NEIGHBORS timers 10 30
  neighbor NEIGHBORS timers connect 5
  neighbor NEIGHBORS ebgp-multihop
  bgp listen range %s peer-group NEIGHBORS

  address-family l2vpn evpn
    neighbor NEIGHBORS activate
    advertise-all-vni
    advertise-svi-ip
  exit-address-family
exit

router bgp %d vrf vrf-vpn
  no bgp ebgp-requires-policy
  no bgp network import-check
  no bgp default ipv4-unicast

  address-family ipv4 unicast
    redistribute connected
    redistribute kernel
  exit-address-family

  address-family l2vpn evpn
    advertise ipv4 unicast
    rd %d:%d
    route-target import %s
    route-target export %s
  exit-address-family
exit
`, evpnVNI,
		evpnPeerASN, routerID, evpnLocalASN, listenRange,
		evpnPeerASN,
		evpnPeerASN, evpnVNI, evpnRT, evpnRT)

	// Enable bgpd in the default daemons config and bind to all interfaces
	_, _, err := framework.ExecShellInContainer(f, namespace, podName, frrPeerContainerName,
		"sed -i -e 's/^bgpd=no/bgpd=yes/' -e 's/^bgpd_options=.*/bgpd_options=\"  -A 0.0.0.0\"/' /etc/frr/daemons")
	framework.ExpectNoError(err, "enabling bgpd in daemons config")

	// Create vtysh.conf to suppress warning
	_, _, err = framework.ExecShellInContainer(f, namespace, podName, frrPeerContainerName, "touch /etc/frr/vtysh.conf")
	framework.ExpectNoError(err, "creating vtysh.conf")

	// Write FRR config via base64 to avoid shell escaping issues
	writeFileViaPod(f, namespace, podName, "/etc/frr/frr.conf", frrConf)

	// Start FRR daemons in the background (container was started with sleep infinity)
	_, _, err = framework.ExecShellInContainer(f, namespace, podName, frrPeerContainerName, "nohup /usr/lib/frr/docker-start > /dev/null 2>&1 &")
	framework.ExpectNoError(err, "starting FRR daemons")
}

// getVxlanPackets parses `ip -s link show vxlan-vpn` output and returns the packet count
// for the given direction ("RX" or "TX").
func getVxlanPackets(f *framework.Framework, namespace, podName, container, direction string) int64 {
	ginkgo.GinkgoHelper()
	stdout, _, err := framework.ExecShellInContainer(f, namespace, podName, container, "ip -s link show vxlan-vpn")
	framework.ExpectNoError(err, "getting vxlan-vpn stats from pod %s", podName)

	lines := strings.Split(stdout, "\n")
	for i, line := range lines {
		if strings.Contains(strings.TrimSpace(line), direction+":") && i+1 < len(lines) {
			fields := strings.Fields(lines[i+1])
			if len(fields) >= 2 {
				packets, err := strconv.ParseInt(fields[1], 10, 64)
				framework.ExpectNoError(err, "parsing %s packets from vxlan-vpn stats", direction)
				return packets
			}
		}
	}
	framework.Failf("failed to parse %s packets from vxlan-vpn stats:\n%s", direction, stdout)
	return 0
}
