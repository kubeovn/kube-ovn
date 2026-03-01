package ovn_eip

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"

	nad "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

const (
	dockerExtNet1Name      = "kube-ovn-ext-net1"
	vpcNatGWConfigMapName  = "ovn-vpc-nat-gw-config"
	vpcNatConfigName       = "ovn-vpc-nat-config"
	networkAttachDefName   = "ovn-vpc-external-network"
	externalSubnetProvider = "ovn-vpc-external-network.kube-system"
)

func setupNetworkAttachmentDefinition(
	f *framework.Framework,
	dockerExtNetNetwork *dockernetwork.Inspect,
	attachNetClient *framework.NetworkAttachmentDefinitionClient,
	subnetClient *framework.SubnetClient,
	externalNetworkName string,
	nicName string,
	provider string,
	dockerExtNetName string,
) {
	ginkgo.GinkgoHelper()

	ginkgo.By("Getting docker network " + dockerExtNetName)
	network, err := docker.NetworkInspect(dockerExtNetName)
	framework.ExpectNoError(err, "getting docker network "+dockerExtNetName)
	ginkgo.By("Getting or creating network attachment definition " + externalNetworkName)

	// Create network attachment configuration using structured data
	type ipamConfig struct {
		Type         string `json:"type"`
		ServerSocket string `json:"server_socket"`
		Provider     string `json:"provider"`
	}
	type nadConfig struct {
		CNIVersion string     `json:"cniVersion"`
		Type       string     `json:"type"`
		Master     string     `json:"master"`
		Mode       string     `json:"mode"`
		IPAM       ipamConfig `json:"ipam"`
	}

	config := nadConfig{
		CNIVersion: "0.3.0",
		Type:       "macvlan",
		Master:     nicName,
		Mode:       "bridge",
		IPAM: ipamConfig{
			Type:         "kube-ovn",
			ServerSocket: "/run/openvswitch/kube-ovn-daemon.sock",
			Provider:     provider,
		},
	}

	attachConfBytes, err := json.Marshal(config)
	framework.ExpectNoError(err, "marshaling network attachment configuration")
	attachConf := string(attachConfBytes)

	// Try to get existing NAD first
	nad, err := attachNetClient.NetworkAttachmentDefinitionInterface.Get(context.TODO(), externalNetworkName, metav1.GetOptions{})
	if err != nil && k8serrors.IsNotFound(err) {
		// NAD doesn't exist, create it
		attachNet := framework.MakeNetworkAttachmentDefinition(externalNetworkName, framework.KubeOvnNamespace, attachConf)
		nad = attachNetClient.Create(attachNet)
	} else {
		framework.ExpectNoError(err, "getting network attachment definition "+externalNetworkName)
	}

	ginkgo.By("Got network attachment definition " + nad.Name)

	ginkgo.By("Creating underlay macvlan subnet " + externalNetworkName)
	var cidrV4, cidrV6, gatewayV4, gatewayV6 string
	for _, config := range dockerExtNetNetwork.IPAM.Config {
		switch util.CheckProtocol(config.Subnet.Addr().String()) {
		case apiv1.ProtocolIPv4:
			if f.HasIPv4() {
				cidrV4 = config.Subnet.String()
				gatewayV4 = config.Gateway.String()
			}
		case apiv1.ProtocolIPv6:
			if f.HasIPv6() {
				cidrV6 = config.Subnet.String()
				gatewayV6 = config.Gateway.String()
			}
		}
	}
	cidr := make([]string, 0, 2)
	gateway := make([]string, 0, 2)
	if f.HasIPv4() {
		cidr = append(cidr, cidrV4)
		gateway = append(gateway, gatewayV4)
	}
	if f.HasIPv6() {
		cidr = append(cidr, cidrV6)
		gateway = append(gateway, gatewayV6)
	}
	excludeIPs := make([]string, 0, len(network.Containers)*2)
	for _, container := range network.Containers {
		if container.IPv4Address.IsValid() && f.HasIPv4() {
			excludeIPs = append(excludeIPs, container.IPv4Address.Addr().String())
		}
		if container.IPv6Address.IsValid() && f.HasIPv6() {
			excludeIPs = append(excludeIPs, container.IPv6Address.Addr().String())
		}
	}

	// Check if subnet already exists
	_, err = subnetClient.SubnetInterface.Get(context.TODO(), externalNetworkName, metav1.GetOptions{})
	if err != nil && k8serrors.IsNotFound(err) {
		// Subnet doesn't exist, create it
		macvlanSubnet := framework.MakeSubnet(externalNetworkName, "", strings.Join(cidr, ","), strings.Join(gateway, ","), "", provider, excludeIPs, nil, nil)
		_ = subnetClient.CreateSync(macvlanSubnet)
	} else {
		framework.ExpectNoError(err, "getting subnet "+externalNetworkName)
	}
}

func setupVpcNatGwTestEnvironment(
	f *framework.Framework,
	dockerExtNetNetwork *dockernetwork.Inspect,
	attachNetClient *framework.NetworkAttachmentDefinitionClient,
	subnetClient *framework.SubnetClient,
	vpcClient *framework.VpcClient,
	vpcNatGwClient *framework.VpcNatGatewayClient,
	vpcName string,
	overlaySubnetName string,
	vpcNatGwName string,
	natGwQosPolicy string,
	overlaySubnetV4Cidr string,
	overlaySubnetV4Gw string,
	lanIP string,
	dockerExtNetName string,
	externalNetworkName string,
	nicName string,
	provider string,
	skipNADSetup bool,
	annotations map[string]string,
) {
	ginkgo.GinkgoHelper()

	if !skipNADSetup {
		setupNetworkAttachmentDefinition(
			f, dockerExtNetNetwork, attachNetClient,
			subnetClient, externalNetworkName, nicName, provider, dockerExtNetName)
	}

	ginkgo.By("Getting config map " + vpcNatGWConfigMapName)
	_, err := f.ClientSet.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Get(context.Background(), vpcNatGWConfigMapName, metav1.GetOptions{})
	framework.ExpectNoError(err, "failed to get ConfigMap")

	ginkgo.By("Creating custom vpc " + vpcName)
	vpc := framework.MakeVpc(vpcName, lanIP, false, false, nil)
	_ = vpcClient.CreateSync(vpc)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up custom vpc " + vpcName)
		vpcClient.DeleteSync(vpcName)
	})

	ginkgo.By("Creating custom overlay subnet " + overlaySubnetName)
	overlaySubnet := framework.MakeSubnet(overlaySubnetName, "", overlaySubnetV4Cidr, overlaySubnetV4Gw, vpcName, "", nil, nil, nil)
	_ = subnetClient.CreateSync(overlaySubnet)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up custom overlay subnet " + overlaySubnetName)
		subnetClient.DeleteSync(overlaySubnetName)
	})

	ginkgo.By("Creating custom vpc nat gw " + vpcNatGwName)
	vpcNatGw := framework.MakeVpcNatGatewayWithAnnotations(vpcNatGwName, vpcName, overlaySubnetName, lanIP, externalNetworkName, natGwQosPolicy, annotations)
	_ = vpcNatGwClient.CreateSync(vpcNatGw, f.ClientSet)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up custom vpc nat gw " + vpcNatGwName)
		vpcNatGwClient.DeleteSync(vpcNatGwName)
	})
}

// waitForIptablesEIPReady waits for an IptablesEIP to be ready
func waitForIptablesEIPReady(eipClient *framework.IptablesEIPClient, eipName string, timeout time.Duration) *apiv1.IptablesEIP {
	ginkgo.GinkgoHelper()
	var eip *apiv1.IptablesEIP
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		eip = eipClient.Get(eipName)
		if eip != nil && eip.Status.IP != "" && eip.Status.Ready {
			framework.Logf("IptablesEIP %s is ready with IP: %s", eipName, eip.Status.IP)
			return eip
		}
		time.Sleep(2 * time.Second)
	}
	framework.Failf("Timeout waiting for IptablesEIP %s to be ready", eipName)
	return nil
}

// verifySubnetStatusAfterEIPOperation verifies subnet status after EIP operation
func verifySubnetStatusAfterEIPOperation(subnetClient *framework.SubnetClient, subnetName string,
	protocol, operation, shouldContainIP string,
) {
	ginkgo.GinkgoHelper()

	subnet := subnetClient.Get(subnetName)
	framework.Logf("Verifying subnet %s status after %s: Protocol=%s", subnetName, operation, protocol)

	switch protocol {
	case apiv1.ProtocolIPv4:
		framework.Logf("V4 Status: Available=%.0f, Using=%.0f",
			subnet.Status.V4AvailableIPs, subnet.Status.V4UsingIPs)
		if shouldContainIP != "" {
			framework.ExpectTrue(strings.Contains(subnet.Status.V4UsingIPRange, shouldContainIP),
				"IP %s should be in V4UsingIPRange after %s", shouldContainIP, operation)
		}
	case apiv1.ProtocolIPv6:
		framework.Logf("V6 Status: Available=%.0f, Using=%.0f",
			subnet.Status.V6AvailableIPs, subnet.Status.V6UsingIPs)
		if shouldContainIP != "" {
			framework.ExpectTrue(strings.Contains(subnet.Status.V6UsingIPRange, shouldContainIP),
				"IP %s should be in V6UsingIPRange after %s", shouldContainIP, operation)
		}
	case apiv1.ProtocolDual:
		framework.Logf("Dual Stack Status: V4Available=%.0f, V4Using=%.0f, V6Available=%.0f, V6Using=%.0f",
			subnet.Status.V4AvailableIPs, subnet.Status.V4UsingIPs,
			subnet.Status.V6AvailableIPs, subnet.Status.V6UsingIPs)
	}
}

var _ = framework.OrderedDescribe("[group:iptables-vpc-nat-gw]", func() {
	f := framework.NewDefaultFramework("iptables-vpc-nat-gw")

	var skip bool
	var cs clientset.Interface
	var attachNetClient *framework.NetworkAttachmentDefinitionClient
	var clusterName, vpcName, vpcNatGwName, overlaySubnetName string
	var vpcClient *framework.VpcClient
	var vpcNatGwClient *framework.VpcNatGatewayClient
	var subnetClient *framework.SubnetClient
	var vipClient *framework.VipClient
	var iptablesEIPClient *framework.IptablesEIPClient
	var iptablesFIPClient *framework.IptablesFIPClient
	var iptablesSnatRuleClient *framework.IptablesSnatClient
	var iptablesDnatRuleClient *framework.IptablesDnatClient

	var dockerExtNet1Network *dockernetwork.Inspect
	var net1NicName string

	ginkgo.BeforeAll(func() {
		// Initialize clients manually for BeforeAll without calling f.BeforeEach()
		// since f.BeforeEach() is designed to be called per-test
		var err error
		config, err := k8sframework.LoadConfig()
		framework.ExpectNoError(err, "loading kubeconfig")

		cs, err = clientset.NewForConfig(config)
		framework.ExpectNoError(err, "creating kubernetes clientset")

		// Initialize framework clients needed for BeforeAll
		if f.KubeOVNClientSet == nil {
			f.KubeOVNClientSet, err = framework.LoadKubeOVNClientSet()
			framework.ExpectNoError(err, "creating kube-ovn clientset")
		}
		if f.AttachNetClient == nil {
			nadClient, err := nad.NewForConfig(config)
			framework.ExpectNoError(err, "creating network attachment definition clientset")
			f.AttachNetClient = nadClient
		}

		attachNetClient = f.NetworkAttachmentDefinitionClientNS(framework.KubeOvnNamespace)
		subnetClient = f.SubnetClient()
		vpcClient = f.VpcClient()
		vpcNatGwClient = f.VpcNatGatewayClient()
		iptablesEIPClient = f.IptablesEIPClient()
		vipClient = f.VipClient()
		iptablesFIPClient = f.IptablesFIPClient()
		iptablesSnatRuleClient = f.IptablesSnatClient()
		iptablesDnatRuleClient = f.IptablesDnatClient()

		if skip {
			ginkgo.Skip("underlay spec only runs on kind clusters")
		}
		f.SkipVersionPriorTo(1, 15, "Skip e2e tests for Kube-OVN versions prior to 1.15 temporarily")

		if clusterName == "" {
			ginkgo.By("Getting k8s nodes")
			k8sNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
			framework.ExpectNoError(err)

			cluster, ok := kind.IsKindProvided(k8sNodes.Items[0].Spec.ProviderID)
			if !ok {
				skip = true
				ginkgo.Skip("underlay spec only runs on kind clusters")
			}
			clusterName = cluster
		}

		ginkgo.By("Ensuring docker network " + dockerExtNet1Name + " exists")
		network1, err := docker.NetworkCreate(dockerExtNet1Name, true, true)
		framework.ExpectNoError(err, "creating docker network "+dockerExtNet1Name)
		dockerExtNet1Network = network1

		ginkgo.By("Getting kind nodes")
		nodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")
		framework.ExpectNotEmpty(nodes)

		ginkgo.By("Connecting nodes to the docker network")
		err = kind.NetworkConnect(dockerExtNet1Network.ID, nodes)
		framework.ExpectNoError(err, "connecting nodes to network "+dockerExtNet1Name)

		ginkgo.By("Getting node links that belong to the docker network")
		nodes, err = kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")

		ginkgo.By("Validating node links")
		gomega.Eventually(func() error {
			network1, err := docker.NetworkInspect(dockerExtNet1Name)
			if err != nil {
				return fmt.Errorf("failed to inspect docker network %s: %w", dockerExtNet1Name, err)
			}

			for _, node := range nodes {
				container, exists := network1.Containers[node.ID]
				if !exists || container.MacAddress.String() == "" {
					return fmt.Errorf("node %s not ready in network containers (exists=%v, MAC=%s)", node.ID, exists, container.MacAddress.String())
				}

				links, err := node.ListLinks()
				if err != nil {
					return fmt.Errorf("failed to list links on node %s: %w", node.Name(), err)
				}

				net1Mac := container.MacAddress
				var eth0Exist, net1Exist bool
				for _, link := range links {
					if link.IfName == "eth0" {
						eth0Exist = true
					}
					if link.Address == net1Mac.String() {
						net1NicName = link.IfName
						net1Exist = true
					}
				}

				if !eth0Exist {
					return fmt.Errorf("eth0 not found on node %s", node.Name())
				}
				if !net1Exist {
					return fmt.Errorf("net1 interface with MAC %s not found on node %s", net1Mac.String(), node.Name())
				}
				framework.Logf("Node %s has eth0 and net1 with MAC %s", node.Name(), net1Mac.String())
			}
			return nil
		}, 30*time.Second, 500*time.Millisecond).Should(gomega.Succeed(), "timed out waiting for all nodes to have their network interfaces ready")

		ginkgo.By("Creating shared NAD and subnet for all tests")
		setupNetworkAttachmentDefinition(
			f, dockerExtNet1Network, attachNetClient,
			subnetClient, networkAttachDefName, net1NicName,
			externalSubnetProvider, dockerExtNet1Name)

		ginkgo.DeferCleanup(func() {
			ginkgo.By("Waiting for all EIPs using subnet " + networkAttachDefName + " to be deleted")
			gomega.Eventually(func() int {
				eips, err := f.KubeOVNClientSet.KubeovnV1().IptablesEIPs().List(context.Background(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("%s=%s", util.SubnetNameLabel, networkAttachDefName),
				})
				if err != nil {
					framework.Logf("Failed to list EIPs: %v", err)
					return -1
				}
				if len(eips.Items) > 0 {
					framework.Logf("Still waiting for %d EIP(s) to be deleted", len(eips.Items))
				}
				return len(eips.Items)
			}, 2*time.Minute, time.Second).Should(gomega.Equal(0), "All EIPs should be deleted before cleaning up subnet")

			ginkgo.By("Cleaning up shared macvlan underlay subnet " + networkAttachDefName)
			subnetClient.DeleteSync(networkAttachDefName)
			ginkgo.By("Cleaning up shared nad " + networkAttachDefName)
			attachNetClient.Delete(networkAttachDefName)

			// Clean up docker network infrastructure after all resources are deleted
			ginkgo.By("Getting nodes")
			nodes, err := kind.ListNodes(clusterName, "")
			framework.ExpectNoError(err, "getting nodes in cluster")

			if dockerExtNet1Network != nil {
				ginkgo.By("Disconnecting nodes from the docker network")
				err = kind.NetworkDisconnect(dockerExtNet1Network.ID, nodes)
				framework.ExpectNoError(err, "disconnecting nodes from network "+dockerExtNet1Name)
			}
		})
	})

	ginkgo.BeforeEach(func() {
		randomSuffix := framework.RandomSuffix()
		vpcName = "vpc-" + randomSuffix
		vpcNatGwName = "gw-" + randomSuffix
		overlaySubnetName = "overlay-subnet-" + randomSuffix
	})

	framework.ConformanceIt("[1] change gateway image and custom annotations", func() {
		f.SkipVersionPriorTo(1, 16, "This feature was introduced in v1.16")
		overlaySubnetV4Cidr := "10.0.2.0/24"
		overlaySubnetV4Gw := "10.0.2.1"
		lanIP := "10.0.2.254"
		natgwQoS := ""
		cm, err := f.ClientSet.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Get(context.Background(), vpcNatConfigName, metav1.GetOptions{})
		framework.ExpectNoError(err)
		oldImage := cm.Data["image"]
		cm.Data["image"] = "docker.io/kubeovn/vpc-nat-gateway:v1.14.25"
		cm, err = f.ClientSet.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Update(context.Background(), cm, metav1.UpdateOptions{})
		framework.ExpectNoError(err)
		time.Sleep(3 * time.Second)

		// Test custom annotations on VpcNatGateway
		customAnnotations := map[string]string{
			"e2e-test.kubeovn.io/custom-annotation": "test-value",
		}
		setupVpcNatGwTestEnvironment(
			f, dockerExtNet1Network, attachNetClient,
			subnetClient, vpcClient, vpcNatGwClient,
			vpcName, overlaySubnetName+"image", vpcNatGwName, natgwQoS,
			overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
			dockerExtNet1Name, networkAttachDefName, net1NicName,
			externalSubnetProvider,
			true, // skipNADSetup: shared NAD created in BeforeAll
			customAnnotations,
		)
		vpcNatGwPodName := util.GenNatGwPodName(vpcNatGwName)
		pod := f.PodClientNS(metav1.NamespaceSystem).GetPod(vpcNatGwPodName)
		framework.ExpectNotNil(pod)
		framework.ExpectEqual(pod.Spec.Containers[0].Image, cm.Data["image"])

		// Verify custom annotations are present on the pod
		ginkgo.By("Verifying custom annotations on NAT gateway pod")
		for k, v := range customAnnotations {
			framework.ExpectHaveKeyWithValue(pod.Annotations, k, v)
		}

		// recover the image
		cm.Data["image"] = oldImage
		_, err = f.ClientSet.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Update(context.Background(), cm, metav1.UpdateOptions{})
		framework.ExpectNoError(err)
		// Cleanup is handled by DeferCleanup in setupVpcNatGwTestEnvironment
	})

	framework.ConformanceIt("[2] iptables EIP FIP SNAT DNAT", func() {
		// Test-specific variables
		randomSuffix := framework.RandomSuffix()
		fipVipName := "fip-vip-" + randomSuffix
		fipEipName := "fip-eip-" + randomSuffix
		fipName := "fip-" + randomSuffix
		dnatVipName := "dnat-vip-" + randomSuffix
		dnatEipName := "dnat-eip-" + randomSuffix
		dnatName := "dnat-" + randomSuffix
		snatEipName := "snat-eip-" + randomSuffix
		snatName := "snat-" + randomSuffix
		// sharing case
		sharedVipName := "shared-vip-" + randomSuffix
		sharedEipName := "shared-eip-" + randomSuffix
		sharedEipDnatName := "shared-eip-dnat-" + randomSuffix
		sharedEipSnatName := "shared-eip-snat-" + randomSuffix
		sharedEipFipShouldOkName := "shared-eip-fip-should-ok-" + randomSuffix
		sharedEipFipShouldFailName := "shared-eip-fip-should-fail-" + randomSuffix

		overlaySubnetV4Cidr := "10.0.1.0/24"
		overlaySubnetV4Gw := "10.0.1.1"
		lanIP := "10.0.1.254"
		natgwQoS := ""
		setupVpcNatGwTestEnvironment(
			f, dockerExtNet1Network, attachNetClient,
			subnetClient, vpcClient, vpcNatGwClient,
			vpcName, overlaySubnetName, vpcNatGwName, natgwQoS,
			overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
			dockerExtNet1Name, networkAttachDefName, net1NicName,
			externalSubnetProvider,
			true, // skipNADSetup: shared NAD created in BeforeAll
			nil,  // no custom annotations
		)

		ginkgo.By("Creating iptables vip for fip")
		fipVip := framework.MakeVip(f.Namespace.Name, fipVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(fipVip)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up fip vip " + fipVipName)
			vipClient.DeleteSync(fipVipName)
		})
		fipVip = vipClient.Get(fipVipName)
		ginkgo.By("Creating iptables eip for fip")
		fipEip := framework.MakeIptablesEIP(fipEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(fipEip)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up fip eip " + fipEipName)
			iptablesEIPClient.DeleteSync(fipEipName)
		})

		ginkgo.By("Creating iptables fip")
		fip := framework.MakeIptablesFIPRule(fipName, fipEipName, fipVip.Status.V4ip)
		_ = iptablesFIPClient.CreateSync(fip)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up fip " + fipName)
			iptablesFIPClient.DeleteSync(fipName)
		})

		ginkgo.By("Creating iptables eip for snat")
		snatEip := framework.MakeIptablesEIP(snatEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(snatEip)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up snat eip " + snatEipName)
			iptablesEIPClient.DeleteSync(snatEipName)
		})

		ginkgo.By("Creating iptables snat")
		snat := framework.MakeIptablesSnatRule(snatName, snatEipName, overlaySubnetV4Cidr)
		_ = iptablesSnatRuleClient.CreateSync(snat)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up snat " + snatName)
			iptablesSnatRuleClient.DeleteSync(snatName)
		})

		ginkgo.By("Creating iptables vip for dnat")
		dnatVip := framework.MakeVip(f.Namespace.Name, dnatVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(dnatVip)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up dnat vip " + dnatVipName)
			vipClient.DeleteSync(dnatVipName)
		})
		dnatVip = vipClient.Get(dnatVipName)

		ginkgo.By("Creating iptables eip for dnat")
		dnatEip := framework.MakeIptablesEIP(dnatEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(dnatEip)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up dnat eip " + dnatEipName)
			iptablesEIPClient.DeleteSync(dnatEipName)
		})

		ginkgo.By("Creating iptables dnat")
		dnat := framework.MakeIptablesDnatRule(dnatName, dnatEipName, "80", "tcp", dnatVip.Status.V4ip, "8080")
		_ = iptablesDnatRuleClient.CreateSync(dnat)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up dnat " + dnatName)
			iptablesDnatRuleClient.DeleteSync(dnatName)
		})

		// share eip case
		ginkgo.By("Creating share vip")
		shareVip := framework.MakeVip(f.Namespace.Name, sharedVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(shareVip)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up shared vip " + sharedVipName)
			vipClient.DeleteSync(sharedVipName)
		})
		fipVip = vipClient.Get(fipVipName)

		ginkgo.By("Creating share iptables eip")
		shareEip := framework.MakeIptablesEIP(sharedEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(shareEip)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up shared eip " + sharedEipName)
			iptablesEIPClient.DeleteSync(sharedEipName)
		})

		ginkgo.By("Creating the first iptables fip with share eip vip should be ok")
		shareFipShouldOk := framework.MakeIptablesFIPRule(sharedEipFipShouldOkName, sharedEipName, fipVip.Status.V4ip)
		_ = iptablesFIPClient.CreateSync(shareFipShouldOk)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up shared fip (should ok) " + sharedEipFipShouldOkName)
			iptablesFIPClient.DeleteSync(sharedEipFipShouldOkName)
		})

		ginkgo.By("Creating the second iptables fip with share eip vip should be failed")
		shareFipShouldFail := framework.MakeIptablesFIPRule(sharedEipFipShouldFailName, sharedEipName, fipVip.Status.V4ip)
		_ = iptablesFIPClient.Create(shareFipShouldFail)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up shared fip (should fail) " + sharedEipFipShouldFailName)
			iptablesFIPClient.DeleteSync(sharedEipFipShouldFailName)
		})

		ginkgo.By("Creating iptables dnat for dnat with share eip vip")
		shareDnat := framework.MakeIptablesDnatRule(sharedEipDnatName, sharedEipName, "80", "tcp", fipVip.Status.V4ip, "8080")
		_ = iptablesDnatRuleClient.CreateSync(shareDnat)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up shared dnat " + sharedEipDnatName)
			iptablesDnatRuleClient.DeleteSync(sharedEipDnatName)
		})

		ginkgo.By("Creating iptables snat with share eip vip")
		shareSnat := framework.MakeIptablesSnatRule(sharedEipSnatName, sharedEipName, overlaySubnetV4Cidr)
		_ = iptablesSnatRuleClient.CreateSync(shareSnat)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up shared snat " + sharedEipSnatName)
			iptablesSnatRuleClient.DeleteSync(sharedEipSnatName)
		})

		ginkgo.By("Get share eip")
		shareEip = iptablesEIPClient.Get(sharedEipName)
		ginkgo.By("Get share dnat")
		shareDnat = iptablesDnatRuleClient.Get(sharedEipDnatName)
		ginkgo.By("Get share snat")
		shareSnat = iptablesSnatRuleClient.Get(sharedEipSnatName)
		ginkgo.By("Get share fip should ok")
		shareFipShouldOk = iptablesFIPClient.Get(sharedEipFipShouldOkName)
		ginkgo.By("Get share fip should fail")
		shareFipShouldFail = iptablesFIPClient.Get(sharedEipFipShouldFailName)

		ginkgo.By("Check share eip should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareEip.Labels, util.EipV4IpLabel, shareEip.Spec.V4ip)
		ginkgo.By("Check share dnat should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareDnat.Labels, util.EipV4IpLabel, shareEip.Spec.V4ip)
		ginkgo.By("Check share snat should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareSnat.Labels, util.EipV4IpLabel, shareEip.Spec.V4ip)
		ginkgo.By("Check share fip should ok should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareFipShouldOk.Labels, util.EipV4IpLabel, shareEip.Spec.V4ip)
		ginkgo.By("Check share fip should fail should not be ready")
		framework.ExpectEqual(shareFipShouldFail.Status.Ready, false)

		// make sure eip is shared
		nats := []string{util.DnatUsingEip, util.FipUsingEip, util.SnatUsingEip}
		framework.ExpectEqual(shareEip.Status.Nat, strings.Join(nats, ","))
		// All cleanup is handled by DeferCleanup above, no need for manual cleanup
	})

	framework.ConformanceIt("[3] manage IptablesEIP lifecycle with finalizer and update subnet status", func() {
		f.SkipVersionPriorTo(1, 14, "This feature was introduced in v1.14")

		overlaySubnetV4Cidr := "10.0.3.0/24"
		overlaySubnetV4Gw := "10.0.3.1"
		lanIP := "10.0.3.254"
		natgwQoS := ""
		setupVpcNatGwTestEnvironment(
			f, dockerExtNet1Network, attachNetClient,
			subnetClient, vpcClient, vpcNatGwClient,
			vpcName, overlaySubnetName, vpcNatGwName, natgwQoS,
			overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
			dockerExtNet1Name, networkAttachDefName, net1NicName,
			externalSubnetProvider,
			true, // skipNADSetup: shared NAD created in BeforeAll
			nil,  // no custom annotations
		)

		ginkgo.By("1. Get initial external subnet status")
		externalSubnetName := util.GetExternalNetwork(networkAttachDefName)
		initialSubnet := subnetClient.Get(externalSubnetName)
		initialV4AvailableIPs := initialSubnet.Status.V4AvailableIPs
		initialV4UsingIPs := initialSubnet.Status.V4UsingIPs
		initialV6AvailableIPs := initialSubnet.Status.V6AvailableIPs
		initialV6UsingIPs := initialSubnet.Status.V6UsingIPs
		initialV4AvailableIPRange := initialSubnet.Status.V4AvailableIPRange
		initialV4UsingIPRange := initialSubnet.Status.V4UsingIPRange
		initialV6AvailableIPRange := initialSubnet.Status.V6AvailableIPRange
		initialV6UsingIPRange := initialSubnet.Status.V6UsingIPRange

		ginkgo.By("2. Create IptablesEIP to trigger IP allocation")
		eipName := "test-eip-finalizer-" + framework.RandomSuffix()
		eip := framework.MakeIptablesEIP(eipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(eip)

		ginkgo.By("3. Wait for IptablesEIP CR to be ready")
		eipCR := waitForIptablesEIPReady(iptablesEIPClient, eipName, 60*time.Second)
		framework.ExpectNotNil(eipCR, "IptablesEIP CR should be created and ready")
		framework.ExpectNotEmpty(eipCR.Status.IP, "IptablesEIP should have IP assigned")

		ginkgo.By("4. Wait for IptablesEIP CR finalizer to be added")
		for range 60 {
			eipCR = iptablesEIPClient.Get(eipName)
			if eipCR != nil && len(eipCR.Finalizers) > 0 {
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectNotNil(eipCR, "IptablesEIP CR should exist")
		framework.ExpectContainElement(eipCR.Finalizers, util.KubeOVNControllerFinalizer,
			"IptablesEIP CR should have finalizer after creation")

		ginkgo.By("5. Wait for external subnet status to be updated after IptablesEIP creation")
		time.Sleep(5 * time.Second)

		ginkgo.By("6. Verify external subnet status after IptablesEIP CR creation")
		afterCreateSubnet := subnetClient.Get(externalSubnetName)
		verifySubnetStatusAfterEIPOperation(subnetClient, externalSubnetName,
			afterCreateSubnet.Spec.Protocol, "IptablesEIP creation", eipCR.Status.IP)

		// Verify IP count and range changes
		switch afterCreateSubnet.Spec.Protocol {
		case apiv1.ProtocolIPv4:
			framework.ExpectEqual(initialV4AvailableIPs-1, afterCreateSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should decrease by 1 after IptablesEIP creation")
			framework.ExpectEqual(initialV4UsingIPs+1, afterCreateSubnet.Status.V4UsingIPs,
				"V4UsingIPs should increase by 1 after IptablesEIP creation")
			framework.ExpectNotEqual(initialV4AvailableIPRange, afterCreateSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after IptablesEIP creation")
			framework.ExpectNotEqual(initialV4UsingIPRange, afterCreateSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after IptablesEIP creation")
		case apiv1.ProtocolIPv6:
			framework.ExpectEqual(initialV6AvailableIPs-1, afterCreateSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should decrease by 1 after IptablesEIP creation")
			framework.ExpectEqual(initialV6UsingIPs+1, afterCreateSubnet.Status.V6UsingIPs,
				"V6UsingIPs should increase by 1 after IptablesEIP creation")
			framework.ExpectNotEqual(initialV6AvailableIPRange, afterCreateSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after IptablesEIP creation")
			framework.ExpectNotEqual(initialV6UsingIPRange, afterCreateSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after IptablesEIP creation")
		default:
			// Dual stack
			framework.ExpectEqual(initialV4AvailableIPs-1, afterCreateSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should decrease by 1 after IptablesEIP creation")
			framework.ExpectEqual(initialV4UsingIPs+1, afterCreateSubnet.Status.V4UsingIPs,
				"V4UsingIPs should increase by 1 after IptablesEIP creation")
			framework.ExpectEqual(initialV6AvailableIPs-1, afterCreateSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should decrease by 1 after IptablesEIP creation")
			framework.ExpectEqual(initialV6UsingIPs+1, afterCreateSubnet.Status.V6UsingIPs,
				"V6UsingIPs should increase by 1 after IptablesEIP creation")
			framework.ExpectNotEqual(initialV4AvailableIPRange, afterCreateSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after IptablesEIP creation")
			framework.ExpectNotEqual(initialV4UsingIPRange, afterCreateSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after IptablesEIP creation")
			framework.ExpectNotEqual(initialV6AvailableIPRange, afterCreateSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after IptablesEIP creation")
			framework.ExpectNotEqual(initialV6UsingIPRange, afterCreateSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after IptablesEIP creation")
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

		ginkgo.By("7. Delete the IptablesEIP to trigger IP release")
		iptablesEIPClient.DeleteSync(eipName)

		ginkgo.By("8. Wait for IptablesEIP CR to be deleted")
		deleted := false
		for range 30 {
			_, err := f.KubeOVNClientSet.KubeovnV1().IptablesEIPs().Get(context.Background(), eipName, metav1.GetOptions{})
			if err != nil && k8serrors.IsNotFound(err) {
				deleted = true
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectTrue(deleted, "IptablesEIP CR should be deleted")

		ginkgo.By("9. Wait for external subnet status to be updated after IptablesEIP deletion")
		time.Sleep(5 * time.Second)

		ginkgo.By("10. Verify external subnet status after IptablesEIP CR deletion")
		afterDeleteSubnet := subnetClient.Get(externalSubnetName)
		verifySubnetStatusAfterEIPOperation(subnetClient, externalSubnetName,
			afterDeleteSubnet.Spec.Protocol, "IptablesEIP deletion", "")

		// Verify IP count and range restoration
		switch afterDeleteSubnet.Spec.Protocol {
		case apiv1.ProtocolIPv4:
			// Verify IP count is restored
			framework.ExpectEqual(afterCreateV4AvailableIPs+1, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should increase by 1 after IptablesEIP deletion")
			framework.ExpectEqual(afterCreateV4UsingIPs-1, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should decrease by 1 after IptablesEIP deletion")

			// Verify IP range changed
			framework.ExpectNotEqual(afterCreateV4AvailableIPRange, afterDeleteSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after IptablesEIP deletion")
			framework.ExpectNotEqual(afterCreateV4UsingIPRange, afterDeleteSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after IptablesEIP deletion")

			// Verify counts match initial state
			framework.ExpectEqual(initialV4AvailableIPs, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should return to initial value after IptablesEIP deletion")
			framework.ExpectEqual(initialV4UsingIPs, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should return to initial value after IptablesEIP deletion")
		case apiv1.ProtocolIPv6:
			// Verify IP count is restored
			framework.ExpectEqual(afterCreateV6AvailableIPs+1, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should increase by 1 after IptablesEIP deletion")
			framework.ExpectEqual(afterCreateV6UsingIPs-1, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should decrease by 1 after IptablesEIP deletion")

			// Verify IP range changed
			framework.ExpectNotEqual(afterCreateV6AvailableIPRange, afterDeleteSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after IptablesEIP deletion")
			framework.ExpectNotEqual(afterCreateV6UsingIPRange, afterDeleteSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after IptablesEIP deletion")

			// Verify counts match initial state
			framework.ExpectEqual(initialV6AvailableIPs, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should return to initial value after IptablesEIP deletion")
			framework.ExpectEqual(initialV6UsingIPs, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should return to initial value after IptablesEIP deletion")
		default:
			// Dual stack
			framework.ExpectEqual(afterCreateV4AvailableIPs+1, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should increase by 1 after IptablesEIP deletion")
			framework.ExpectEqual(afterCreateV4UsingIPs-1, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should decrease by 1 after IptablesEIP deletion")
			framework.ExpectEqual(afterCreateV6AvailableIPs+1, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should increase by 1 after IptablesEIP deletion")
			framework.ExpectEqual(afterCreateV6UsingIPs-1, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should decrease by 1 after IptablesEIP deletion")

			framework.ExpectNotEqual(afterCreateV4AvailableIPRange, afterDeleteSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after IptablesEIP deletion")
			framework.ExpectNotEqual(afterCreateV4UsingIPRange, afterDeleteSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after IptablesEIP deletion")
			framework.ExpectNotEqual(afterCreateV6AvailableIPRange, afterDeleteSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after IptablesEIP deletion")
			framework.ExpectNotEqual(afterCreateV6UsingIPRange, afterDeleteSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after IptablesEIP deletion")

			framework.ExpectEqual(initialV4AvailableIPs, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should return to initial value after IptablesEIP deletion")
			framework.ExpectEqual(initialV4UsingIPs, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should return to initial value after IptablesEIP deletion")
			framework.ExpectEqual(initialV6AvailableIPs, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should return to initial value after IptablesEIP deletion")
			framework.ExpectEqual(initialV6UsingIPs, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should return to initial value after IptablesEIP deletion")
		}

		ginkgo.By("11. Test completed: IptablesEIP CR creation and deletion properly updates external subnet status via finalizer handlers")
	})

	framework.ConformanceIt("[4] prevent IptablesEIP finalizer removal when used by NAT rules", func() {
		f.SkipVersionPriorTo(1, 14, "This feature was introduced in v1.14")

		overlaySubnetV4Cidr := "10.0.4.0/24"
		overlaySubnetV4Gw := "10.0.4.1"
		lanIP := "10.0.4.254"
		natgwQoS := ""
		setupVpcNatGwTestEnvironment(
			f, dockerExtNet1Network, attachNetClient,
			subnetClient, vpcClient, vpcNatGwClient,
			vpcName, overlaySubnetName, vpcNatGwName, natgwQoS,
			overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
			dockerExtNet1Name, networkAttachDefName, net1NicName,
			externalSubnetProvider,
			true, // skipNADSetup: shared NAD created in BeforeAll
			nil,  // no custom annotations
		)

		ginkgo.By("1. Create a VIP for FIP")
		vipName := "test-vip-" + framework.RandomSuffix()
		vip := framework.MakeVip(f.Namespace.Name, vipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(vip)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up vip " + vipName)
			vipClient.DeleteSync(vipName)
		})
		vip = vipClient.Get(vipName)

		ginkgo.By("2. Create IptablesEIP")
		eipName := "test-eip-with-fip-" + framework.RandomSuffix()
		eip := framework.MakeIptablesEIP(eipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(eip)

		ginkgo.By("3. Wait for IptablesEIP to be ready")
		eipCR := waitForIptablesEIPReady(iptablesEIPClient, eipName, 60*time.Second)
		framework.ExpectNotNil(eipCR, "IptablesEIP CR should be created and ready")

		ginkgo.By("4. Create IptablesFIP using the EIP")
		fipName := "test-fip-" + framework.RandomSuffix()
		fip := framework.MakeIptablesFIPRule(fipName, eipName, vip.Status.V4ip)
		_ = iptablesFIPClient.CreateSync(fip)

		ginkgo.By("5. Wait for EIP status to show it's being used by FIP")
		for range 60 {
			eipCR = iptablesEIPClient.Get(eipName)
			if eipCR != nil && strings.Contains(eipCR.Status.Nat, util.FipUsingEip) {
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectTrue(strings.Contains(eipCR.Status.Nat, util.FipUsingEip),
			"EIP status.Nat should contain 'fip' when used by FIP rule")

		ginkgo.By("6. Delete the IptablesEIP (should not remove finalizer while FIP exists)")
		err := f.KubeOVNClientSet.KubeovnV1().IptablesEIPs().Delete(context.Background(), eipName, metav1.DeleteOptions{})
		framework.ExpectNoError(err, "Deleting IptablesEIP should succeed")

		ginkgo.By("7. Wait and verify EIP still exists with finalizer (blocked by FIP)")
		time.Sleep(5 * time.Second)
		eipCR = iptablesEIPClient.Get(eipName)
		framework.ExpectNotNil(eipCR, "IptablesEIP should still exist")
		framework.ExpectNotNil(eipCR.DeletionTimestamp, "IptablesEIP should have DeletionTimestamp")
		framework.ExpectContainElement(eipCR.Finalizers, util.KubeOVNControllerFinalizer,
			"IptablesEIP should still have finalizer because it's being used by FIP")

		ginkgo.By("8. Delete the FIP to unblock EIP deletion")
		iptablesFIPClient.DeleteSync(fipName)

		ginkgo.By("9. Wait for FIP to be deleted")
		fipDeleted := false
		for range 30 {
			_, err := f.KubeOVNClientSet.KubeovnV1().IptablesFIPRules().Get(context.Background(), fipName, metav1.GetOptions{})
			if err != nil && k8serrors.IsNotFound(err) {
				fipDeleted = true
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectTrue(fipDeleted, "FIP should be deleted")

		ginkgo.By("10. Wait for EIP status.Nat to be cleared or EIP to be deleted")
		for range 30 {
			eipCR, err := f.KubeOVNClientSet.KubeovnV1().IptablesEIPs().Get(context.Background(), eipName, metav1.GetOptions{})
			if err != nil && k8serrors.IsNotFound(err) {
				// EIP already deleted, which is expected
				break
			}
			framework.ExpectNoError(err, "Failed to get IptablesEIP")
			if eipCR.Status.Nat == "" {
				break
			}
			time.Sleep(1 * time.Second)
		}

		ginkgo.By("11. Verify EIP is now deleted after FIP is removed")
		eipDeleted := false
		for range 30 {
			_, err := f.KubeOVNClientSet.KubeovnV1().IptablesEIPs().Get(context.Background(), eipName, metav1.GetOptions{})
			if err != nil && k8serrors.IsNotFound(err) {
				eipDeleted = true
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectTrue(eipDeleted, "IptablesEIP should be deleted after FIP is removed")

		// VIP cleanup is handled by DeferCleanup above
		ginkgo.By("12. Test completed: IptablesEIP finalizer correctly blocks deletion when used by NAT rules")
	})

	framework.ConformanceIt("[5] VPC NAT Gateway with no IPAM NAD and noDefaultEIP", func() {
		f.SkipVersionPriorTo(1, 15, "This feature was introduced in v1.15")

		overlaySubnetV4Cidr := "10.0.5.0/24"
		overlaySubnetV4Gw := "10.0.5.1"
		lanIP := "10.0.5.254"
		natgwQoS := ""

		ginkgo.By("1. Updating shared NAD to no-IPAM configuration")
		// Get the existing NAD and save its original config
		nad, err := attachNetClient.NetworkAttachmentDefinitionInterface.Get(context.TODO(), networkAttachDefName, metav1.GetOptions{})
		framework.ExpectNoError(err, "getting network attachment definition "+networkAttachDefName)
		originalNadConfig := nad.Spec.Config
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Restoring shared NAD " + networkAttachDefName + " to original configuration")
			nadToRestore, err := attachNetClient.NetworkAttachmentDefinitionInterface.Get(context.TODO(), networkAttachDefName, metav1.GetOptions{})
			if err != nil {
				if k8serrors.IsNotFound(err) {
					return
				}
				framework.Logf("failed to get NAD %s for restoration: %v", networkAttachDefName, err)
				return
			}
			nadToRestore.Spec.Config = originalNadConfig
			_, err = attachNetClient.Update(context.TODO(), nadToRestore, metav1.UpdateOptions{})
			if err != nil {
				framework.Logf("failed to restore NAD %s config: %v", networkAttachDefName, err)
			}
		})

		// Update NAD config to remove IPAM section
		type nadConfigNoIPAM struct {
			CNIVersion string `json:"cniVersion"`
			Type       string `json:"type"`
			Master     string `json:"master"`
			Mode       string `json:"mode"`
		}

		configNoIPAM := nadConfigNoIPAM{
			CNIVersion: "0.3.0",
			Type:       "macvlan",
			Master:     net1NicName,
			Mode:       "bridge",
		}

		attachConfBytes, err := json.Marshal(configNoIPAM)
		framework.ExpectNoError(err, "marshaling network attachment configuration")
		nad.Spec.Config = string(attachConfBytes)

		ginkgo.By("Updating NAD " + networkAttachDefName + " to no-IPAM config")
		_, err = attachNetClient.Update(context.TODO(), nad, metav1.UpdateOptions{})
		framework.ExpectNoError(err, "updating network attachment definition")

		ginkgo.By("2. Creating custom vpc " + vpcName)
		vpc := framework.MakeVpc(vpcName, lanIP, false, false, nil)
		_ = vpcClient.CreateSync(vpc)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up custom vpc " + vpcName)
			vpcClient.DeleteSync(vpcName)
		})

		ginkgo.By("3. Creating custom overlay subnet " + overlaySubnetName)
		overlaySubnet := framework.MakeSubnet(overlaySubnetName, "", overlaySubnetV4Cidr, overlaySubnetV4Gw, vpcName, "", nil, nil, nil)
		_ = subnetClient.CreateSync(overlaySubnet)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up custom overlay subnet " + overlaySubnetName)
			subnetClient.DeleteSync(overlaySubnetName)
		})

		ginkgo.By("4. Creating custom vpc nat gw with noDefaultEIP=true " + vpcNatGwName)
		vpcNatGw := framework.MakeVpcNatGatewayWithNoDefaultEIP(vpcNatGwName, vpcName, overlaySubnetName, lanIP, networkAttachDefName, natgwQoS, true)
		_ = vpcNatGwClient.CreateSync(vpcNatGw, f.ClientSet)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up custom vpc nat gw " + vpcNatGwName)
			vpcNatGwClient.DeleteSync(vpcNatGwName)
		})

		ginkgo.By("5. Verifying VPC NAT Gateway is created")
		createdGw := vpcNatGwClient.Get(vpcNatGwName)
		framework.ExpectNotNil(createdGw, "VPC NAT Gateway should be created")
		framework.ExpectTrue(createdGw.Spec.NoDefaultEIP, "noDefaultEIP should be true")

		ginkgo.By("6. Verifying no default EIP is created")
		time.Sleep(10 * time.Second)
		eips, err := f.KubeOVNClientSet.KubeovnV1().IptablesEIPs().List(context.Background(), metav1.ListOptions{})
		framework.ExpectNoError(err, "Failed to list IptablesEIPs")
		hasDefaultEIP := false
		for _, eip := range eips.Items {
			if eip.Spec.NatGwDp == vpcNatGwName {
				hasDefaultEIP = true
				break
			}
		}
		framework.ExpectFalse(hasDefaultEIP, "No default EIP should be created when noDefaultEIP is true")

		ginkgo.By("7. Testing manual EIP creation")
		eipName := "manual-eip-" + framework.RandomSuffix()
		eip := framework.MakeIptablesEIP(eipName, "", "", "", vpcNatGwName, networkAttachDefName, "")
		_ = iptablesEIPClient.CreateSync(eip)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up manual eip " + eipName)
			iptablesEIPClient.DeleteSync(eipName)
		})

		ginkgo.By("8. Verifying manually created EIP")
		eipCR := waitForIptablesEIPReady(iptablesEIPClient, eipName, 60*time.Second)
		framework.ExpectNotNil(eipCR, "Manual EIP should be created successfully")
		framework.ExpectNotEmpty(eipCR.Status.IP, "Manual EIP should have IP assigned")

		ginkgo.By("9. Testing VIP and FIP with manual EIP")
		vipName := "test-vip-no-ipam-" + framework.RandomSuffix()
		vip := framework.MakeVip(f.Namespace.Name, vipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(vip)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up vip " + vipName)
			vipClient.DeleteSync(vipName)
		})
		vip = vipClient.Get(vipName)

		fipName := "test-fip-no-ipam-" + framework.RandomSuffix()
		fip := framework.MakeIptablesFIPRule(fipName, eipName, vip.Status.V4ip)
		_ = iptablesFIPClient.CreateSync(fip)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up fip " + fipName)
			iptablesFIPClient.DeleteSync(fipName)
		})

		ginkgo.By("10. Verifying FIP is created successfully")
		createdFip := iptablesFIPClient.Get(fipName)
		framework.ExpectNotNil(createdFip, "FIP should be created successfully")
		framework.ExpectTrue(createdFip.Status.Ready, "FIP should be ready")

		// All cleanup is handled by DeferCleanup above
		ginkgo.By("11. Test completed: VPC NAT Gateway with no IPAM NAD and noDefaultEIP works correctly")
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
