package ovn_eip

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"regexp"
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
	gwNamespace string,
	replicas int32,
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
	if gwNamespace != "" {
		vpcNatGw.Spec.Namespace = gwNamespace
	}
	if replicas > 0 {
		vpcNatGw.Spec.Replicas = replicas
	}
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

// getNatGwPodName returns the name of the first NAT gateway pod found by labels.
func getNatGwPodName(f *framework.Framework, name, namespace string) string {
	ginkgo.GinkgoHelper()
	if namespace == "" {
		namespace = framework.KubeOvnNamespace
	}
	labels := util.GenNatGwLabels(name)
	selector := metav1.FormatLabelSelector(&metav1.LabelSelector{MatchLabels: labels})
	pods, err := f.ClientSet.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
	framework.ExpectNoError(err)
	framework.ExpectTrue(len(pods.Items) > 0, "no NAT gateway pods found for "+name)
	return pods.Items[0].Name
}

// iptablesSaveNat returns the iptables-save output from the NAT gateway pod,
// using the exact same detection logic as nat-gateway.sh to determine whether
// to use iptables-legacy-save or iptables-save (nft backend).
func iptablesSaveNat(natGwPodName string) string {
	// Replicate nat-gateway.sh detection: if iptables-legacy -t nat -S INPUT 1 succeeds,
	// rules were written via iptables-legacy, so use iptables-legacy-save to read them.
	// NOTE: KubectlExec joins args with space and passes to "/bin/sh -c", so pass
	// the entire script as a single string to avoid double shell wrapping.
	cmd := []string{"if iptables-legacy -t nat -S INPUT 1 2>/dev/null; then iptables-legacy-save -t nat; else iptables-save -t nat; fi"}
	stdout, _, err := framework.KubectlExec(framework.KubeOvnNamespace, natGwPodName, cmd...)
	framework.ExpectNoError(err, "failed to exec iptables-save in NAT gateway pod %s", natGwPodName)
	return string(stdout)
}

// hairpinSnatChainExists checks if the HAIRPIN_SNAT chain exists in the NAT gateway pod.
// Returns false on older versions that don't support this feature.
func hairpinSnatChainExists(natGwPodName string) bool {
	output := iptablesSaveNat(natGwPodName)
	return strings.Contains(output, ":HAIRPIN_SNAT") || strings.Contains(output, "-N HAIRPIN_SNAT")
}

// hairpinSnatRuleExists checks if hairpin SNAT rule exists in the NAT gateway pod
// for the given EIP.
// Returns true if rule exists, false otherwise (including when HAIRPIN_SNAT chain doesn't exist).
func hairpinSnatRuleExists(natGwPodName, eip string) bool {
	output := iptablesSaveNat(natGwPodName)
	if !strings.Contains(output, ":HAIRPIN_SNAT") && !strings.Contains(output, "-N HAIRPIN_SNAT") {
		return false
	}

	// Use regex with \b word boundaries for precise matching, and allow
	// optional trailing args like --random-fully after the EIP.
	hairpinRulePattern := fmt.Sprintf(
		`-A HAIRPIN_SNAT -o \b.*\b -m mark --mark 0x1/0x1 -m conntrack --ctstate DNAT --ctorigdst \b%s\b -j SNAT --to-source \b%s\b`,
		regexp.QuoteMeta(eip), regexp.QuoteMeta(eip),
	)
	re := regexp.MustCompile(hairpinRulePattern)
	return re.MatchString(output)
}

// fipDnatRuleExists checks if the FIP DNAT rule exists in the NAT gateway pod.
// iptables-save format: -A EXCLUSIVE_DNAT -d <eip>/32 -j DNAT --to-destination <internalIp>
func fipDnatRuleExists(natGwPodName, eip, internalIP string) bool {
	output := iptablesSaveNat(natGwPodName)
	pattern := fmt.Sprintf(`-A EXCLUSIVE_DNAT -d \b%s/32\b .* --to-destination \b%s\b`,
		regexp.QuoteMeta(eip), regexp.QuoteMeta(internalIP))
	re := regexp.MustCompile(pattern)
	return re.MatchString(output)
}

// fipSnatRuleExists checks if the FIP SNAT rule exists in the NAT gateway pod.
// iptables-save format: -A EXCLUSIVE_SNAT -s <internalIp>/32 -j SNAT --to-source <eip>
func fipSnatRuleExists(natGwPodName, eip, internalIP string) bool {
	output := iptablesSaveNat(natGwPodName)
	pattern := fmt.Sprintf(`-A EXCLUSIVE_SNAT -s \b%s/32\b .* --to-source \b%s\b`,
		regexp.QuoteMeta(internalIP), regexp.QuoteMeta(eip))
	re := regexp.MustCompile(pattern)
	return re.MatchString(output)
}

// dnatRuleExists checks if the DNAT rule exists in the NAT gateway pod.
// iptables-save format: -A SHARED_DNAT -d <eip>/32 -p <protocol> -m <protocol> --dport <externalPort> -j DNAT --to-destination <internalIp>:<internalPort>
// Note: iptables-save inserts "-m <protocol>" after "-p <protocol>", use .* to absorb it.
func dnatRuleExists(natGwPodName, eip, externalPort, protocol, internalIP, internalPort string) bool {
	output := iptablesSaveNat(natGwPodName)
	pattern := fmt.Sprintf(`-A SHARED_DNAT -d \b%s/32\b -p %s .* --dport %s -j DNAT --to-destination \b%s:%s\b`,
		regexp.QuoteMeta(eip), regexp.QuoteMeta(protocol),
		regexp.QuoteMeta(externalPort), regexp.QuoteMeta(internalIP), regexp.QuoteMeta(internalPort))
	re := regexp.MustCompile(pattern)
	return re.MatchString(output)
}

// snatRuleExists checks if the SNAT rule exists in the NAT gateway pod.
// iptables-save format: -A SHARED_SNAT -s <internalCIDR> ... -j SNAT --to-source <eip>
func snatRuleExists(natGwPodName, eip, internalCIDR string) bool {
	output := iptablesSaveNat(natGwPodName)
	pattern := fmt.Sprintf(`-A SHARED_SNAT\b.*-s %s\b.*--to-source %s\b`,
		regexp.QuoteMeta(internalCIDR), regexp.QuoteMeta(eip))
	re := regexp.MustCompile(pattern)
	return re.MatchString(output)
}

// snatRulePosition returns the 1-based index of the SHARED_SNAT rule matching
// (eip, internalCIDR) as it appears in iptables-save output. Returns 0 if the
// rule is not present. Used to verify ordering (longest-prefix-first) across
// multiple SNAT rules on the same NAT gateway.
func snatRulePosition(natGwPodName, eip, internalCIDR string) int {
	output := iptablesSaveNat(natGwPodName)
	pattern := fmt.Sprintf(`-s %s\b.*--to-source %s\b`,
		regexp.QuoteMeta(internalCIDR), regexp.QuoteMeta(eip))
	re := regexp.MustCompile(pattern)
	idx := 0
	for line := range strings.SplitSeq(output, "\n") {
		if !strings.HasPrefix(line, "-A SHARED_SNAT ") {
			continue
		}
		idx++
		if re.MatchString(line) {
			return idx
		}
	}
	return 0
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
	var podClient *framework.PodClient

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
		podClient = f.PodClient()
	})

	framework.ConformanceIt("[1] change gateway image, custom annotations and custom namespace", func() {
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

		// Custom namespace for the NAT gateway pod is a v1.17+ feature; on older
		// versions the pod always lives in the controller PodNamespace.
		// Register the namespace cleanup BEFORE setupVpcNatGwTestEnvironment so it is
		// deleted last (after gw/vpc/subnet cleanup) due to Ginkgo's LIFO DeferCleanup.
		var customNs string
		expectedPodNs := framework.KubeOvnNamespace
		if !f.VersionPriorTo(1, 17) {
			customNs = "ns-" + framework.RandomSuffix()
			expectedPodNs = customNs
			ginkgo.By("Creating custom namespace " + customNs + " for NAT gateway pod")
			_ = f.NamespaceClient().Create(framework.MakeNamespace(customNs, nil, nil))
			ginkgo.DeferCleanup(func() {
				ginkgo.By("Cleaning up custom namespace " + customNs)
				f.NamespaceClient().DeleteSync(customNs)
			})
		}

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
			customNs, // gwNamespace: empty falls back to PodNamespace on pre-v1.17
			0,
		)
		ginkgo.By("Verifying NAT gateway pod is in namespace " + expectedPodNs)
		labels := util.GenNatGwLabels(vpcNatGwName)
		selector := metav1.FormatLabelSelector(&metav1.LabelSelector{MatchLabels: labels})
		pods, err := f.ClientSet.CoreV1().Pods(expectedPodNs).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
		framework.ExpectNoError(err)
		framework.ExpectTrue(len(pods.Items) > 0, "no NAT gateway pods found")
		pod := pods.Items[0]
		framework.ExpectEqual(pod.Namespace, expectedPodNs)
		framework.ExpectEqual(pod.Spec.Containers[0].Image, cm.Data["image"])

		// Verify custom annotations are present on the pod
		ginkgo.By("Verifying custom annotations on NAT gateway pod")
		for k, v := range customAnnotations {
			framework.ExpectHaveKeyWithValue(pod.Annotations, k, v)
		}

		// IptablesEIP spec.namespace plumbing and auto-backfill from the referenced
		// VpcNatGateway are also v1.17+ features.
		if !f.VersionPriorTo(1, 17) {
			// Create an IptablesEIP with spec.namespace pointing to customNs,
			// verify it becomes ready and its namespace matches the gw pod namespace.
			ginkgo.By("Creating IptablesEIP with spec.namespace=" + customNs)
			nsEipName := "ns-eip-" + framework.RandomSuffix()
			nsEip := framework.MakeIptablesEIP(nsEipName, "", "", "", vpcNatGwName, "", "")
			nsEip.Spec.Namespace = customNs
			_ = iptablesEIPClient.CreateSync(nsEip)
			ginkgo.DeferCleanup(func() {
				ginkgo.By("Cleaning up ns eip " + nsEipName)
				iptablesEIPClient.DeleteSync(nsEipName)
			})
			ginkgo.By("Verifying IptablesEIP is ready and its namespace matches " + customNs)
			nsEipCR := waitForIptablesEIPReady(iptablesEIPClient, nsEipName, 60*time.Second)
			framework.ExpectNotNil(nsEipCR, "IptablesEIP with custom namespace should be ready")
			framework.ExpectEqual(nsEipCR.Spec.Namespace, customNs,
				"IptablesEIP spec.namespace should match the custom namespace of the NAT gateway pod")

			// Create an IptablesEIP WITHOUT spec.namespace set; the controller should auto-backfill
			// it from the referenced VpcNatGateway's spec.namespace.
			ginkgo.By("Creating IptablesEIP without spec.namespace to verify auto-backfill from VpcNatGateway")
			autoNsEipName := "auto-ns-eip-" + framework.RandomSuffix()
			autoNsEip := framework.MakeIptablesEIP(autoNsEipName, "", "", "", vpcNatGwName, "", "")
			_ = iptablesEIPClient.CreateSync(autoNsEip)
			ginkgo.DeferCleanup(func() {
				ginkgo.By("Cleaning up auto ns eip " + autoNsEipName)
				iptablesEIPClient.DeleteSync(autoNsEipName)
			})
			ginkgo.By("Verifying IptablesEIP spec.namespace is auto-backfilled to " + customNs)
			gomega.Eventually(func() string {
				return iptablesEIPClient.Get(autoNsEipName).Spec.Namespace
			}, 30*time.Second, 2*time.Second).Should(gomega.Equal(customNs),
				"controller should auto-backfill spec.namespace from the referenced VpcNatGateway")
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
			"",   // gwNamespace: use default (PodNamespace)
			0,
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

		// Verify hairpin SNAT rule is automatically created for each EIP
		ginkgo.By("[hairpin SNAT] Verifying hairpin SNAT rule exists after EIP creation")
		vpcNatGwPodName := getNatGwPodName(f, vpcNatGwName, "")

		ginkgo.By("Verifying FIP iptables rules exist in NAT gateway pod")
		fipEip = iptablesEIPClient.Get(fipEipName)
		framework.ExpectNotEmpty(fipEip.Status.IP, "fipEip.Status.IP should not be empty")
		gomega.Eventually(func() bool {
			return fipDnatRuleExists(vpcNatGwPodName, fipEip.Status.IP, fipVip.Status.V4ip)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"FIP DNAT rule should exist in iptables after FIP creation")
		gomega.Eventually(func() bool {
			return fipSnatRuleExists(vpcNatGwPodName, fipEip.Status.IP, fipVip.Status.V4ip)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"FIP SNAT rule should exist in iptables after FIP creation")

		// Verify SNAT iptables rule exists after creation
		ginkgo.By("Verifying SNAT iptables rule exists in NAT gateway pod")
		snatEip = iptablesEIPClient.Get(snatEipName)
		framework.ExpectNotEmpty(snatEip.Status.IP, "snatEip.Status.IP should not be empty")
		gomega.Eventually(func() bool {
			return snatRuleExists(vpcNatGwPodName, snatEip.Status.IP, overlaySubnetV4Cidr)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"SNAT rule should exist in iptables after SNAT creation")
		if !hairpinSnatChainExists(vpcNatGwPodName) {
			framework.Logf("HAIRPIN_SNAT chain not found, skipping hairpin EIP verification (feature requires v1.15+)")
		} else {
			gomega.Eventually(func() bool {
				return hairpinSnatRuleExists(vpcNatGwPodName, snatEip.Status.IP)
			}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
				"Hairpin SNAT rule should be created after EIP creation")

			// Verify real data-path: internal pod accessing another internal pod via FIP EIP
			// Packet flow: client -> NAT GW (DNAT to serverIP + hairpin SNAT to EIP) -> server -> NAT GW (un-SNAT/DNAT) -> client
			ginkgo.By("[hairpin SNAT] Verifying data-path connectivity: internal pod accessing another internal pod via FIP EIP")
			serverPodName := "server-" + randomSuffix
			clientPodName := "client-" + randomSuffix
			hairpinFipEipName := "hairpin-fip-eip-" + randomSuffix
			hairpinFipName := "hairpin-fip-" + randomSuffix

			// [hairpin SNAT] Create server pod in overlay subnet with auto-assigned IP
			serverAnnotations := map[string]string{
				util.LogicalSwitchAnnotation: overlaySubnetName,
			}
			serverPort := "8080"
			serverArgs := []string{"netexec", "--http-port", serverPort}
			serverPod := framework.MakePod(f.Namespace.Name, serverPodName, nil, serverAnnotations, framework.AgnhostImage, nil, serverArgs)
			_ = podClient.CreateSync(serverPod)
			ginkgo.DeferCleanup(func() {
				ginkgo.By("Cleaning up server pod " + serverPodName)
				podClient.DeleteSync(serverPodName)
			})

			// Get server pod's auto-assigned IP for FIP binding
			createdServerPod := podClient.GetPod(serverPodName)
			serverPodIP := createdServerPod.Annotations[util.IPAddressAnnotation]
			framework.ExpectNotEmpty(serverPodIP, "server pod should have an IP assigned")
			framework.Logf("Server pod %s has IP %s", serverPodName, serverPodIP)

			// [hairpin SNAT] Create a dedicated EIP and FIP for the server pod
			hairpinFipEip := framework.MakeIptablesEIP(hairpinFipEipName, "", "", "", vpcNatGwName, "", "")
			_ = iptablesEIPClient.CreateSync(hairpinFipEip)
			ginkgo.DeferCleanup(func() {
				ginkgo.By("Cleaning up hairpin FIP EIP " + hairpinFipEipName)
				iptablesEIPClient.DeleteSync(hairpinFipEipName)
			})
			hairpinFipEip = iptablesEIPClient.Get(hairpinFipEipName)
			framework.ExpectNotEmpty(hairpinFipEip.Status.IP, "hairpin FIP EIP should have an IP assigned")

			gomega.Eventually(func() bool {
				return hairpinSnatRuleExists(vpcNatGwPodName, hairpinFipEip.Status.IP)
			}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
				"Hairpin SNAT rule should be created for FIP EIP")

			hairpinFip := framework.MakeIptablesFIPRule(hairpinFipName, hairpinFipEipName, serverPodIP)
			_ = iptablesFIPClient.CreateSync(hairpinFip)
			ginkgo.DeferCleanup(func() {
				ginkgo.By("Cleaning up hairpin FIP " + hairpinFipName)
				iptablesFIPClient.DeleteSync(hairpinFipName)
			})

			// [hairpin SNAT] Create client pod in same subnet
			clientAnnotations := map[string]string{
				util.LogicalSwitchAnnotation: overlaySubnetName,
			}
			clientPod := framework.MakePod(f.Namespace.Name, clientPodName, nil, clientAnnotations, framework.AgnhostImage, nil, []string{"pause"})
			_ = podClient.CreateSync(clientPod)
			ginkgo.DeferCleanup(func() {
				ginkgo.By("Cleaning up client pod " + clientPodName)
				podClient.DeleteSync(clientPodName)
			})

			// [hairpin SNAT] Test connectivity: client -> FIP EIP -> server (same subnet)
			ginkgo.By("[hairpin SNAT] Checking data-path: client pod -> FIP EIP " + hairpinFipEip.Status.IP + " -> server pod")
			cmd := []string{"curl", "-m", "10", fmt.Sprintf("http://%s:%s/clientip", hairpinFipEip.Status.IP, serverPort)}
			output, _, err := framework.KubectlExec(f.Namespace.Name, clientPodName, cmd...)
			framework.ExpectNoError(err, "[hairpin SNAT] client pod should reach server pod via FIP EIP")
			framework.Logf("[hairpin SNAT] connectivity verified, response: %s", string(output))
			gomega.Expect(string(output)).To(gomega.ContainSubstring(hairpinFipEip.Status.IP),
				"[hairpin SNAT] server should see request from SNAT EIP, not client pod IP")
		}

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

		// Verify DNAT iptables rule exists after creation
		ginkgo.By("Verifying DNAT iptables rule exists in NAT gateway pod")
		vpcNatGwPodName = getNatGwPodName(f, vpcNatGwName, "")
		dnatEip = iptablesEIPClient.Get(dnatEipName)
		framework.ExpectNotEmpty(dnatEip.Status.IP, "dnatEip.Status.IP should not be empty")
		gomega.Eventually(func() bool {
			return dnatRuleExists(vpcNatGwPodName, dnatEip.Status.IP, "80", "tcp", dnatVip.Status.V4ip, "8080")
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"DNAT rule should exist in iptables after DNAT creation")

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

		// Verify hairpin SNAT rule is created for the shared SNAT (different EIP).
		// Hairpin mirrors EIP 1:1: each EIP creates its own hairpin rule.
		ginkgo.By("Getting share eip")
		shareEip = iptablesEIPClient.Get(sharedEipName)
		framework.ExpectNotEmpty(shareEip.Status.IP, "shareEip.Status.IP should not be empty")
		if hairpinSnatChainExists(vpcNatGwPodName) {
			ginkgo.By("[hairpin SNAT] Verifying hairpin SNAT rule exists for the shared SNAT EIP")
			gomega.Eventually(func() bool {
				return hairpinSnatRuleExists(vpcNatGwPodName, shareEip.Status.IP)
			}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
				"Hairpin SNAT rule should be created for shared SNAT EIP")
		}

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
		// Use Eventually: after SNAT CreateSync returns the controller may not have finished
		// patching EIP.Status.Nat yet (informer cache lag between patchSnatLabel and
		// patchEipStatus). The controller schedules a delayed reset to correct this.
		nats := []string{util.DnatUsingEip, util.FipUsingEip, util.SnatUsingEip}
		expectedNat := strings.Join(nats, ",")
		ginkgo.By("Waiting for shareEip Status.Nat to reflect all nat types: " + expectedNat)
		gomega.Eventually(func() string {
			shareEip = iptablesEIPClient.Get(sharedEipName)
			return shareEip.Status.Nat
		}, 10*time.Second, 2*time.Second).Should(gomega.Equal(expectedNat))

		// Delete SNAT and EIP to verify iptables rule and hairpin cleanup.
		// Deletion must happen unconditionally; hairpin verification is conditional on chain support.
		snatEipIP := snatEip.Status.IP
		ginkgo.By("Deleting SNAT and its EIP to verify rule cleanup")
		iptablesSnatRuleClient.DeleteSync(snatName)
		iptablesEIPClient.DeleteSync(snatEipName)

		// Verify hairpin SNAT rule cleanup when EIP is deleted.
		// Hairpin lifecycle is 1:1 with EIP: created together, deleted together.
		if hairpinSnatChainExists(vpcNatGwPodName) {
			ginkgo.By("[hairpin SNAT] Verifying hairpin rule for the deleted SNAT EIP is removed")
			gomega.Eventually(func() bool {
				return hairpinSnatRuleExists(vpcNatGwPodName, snatEipIP)
			}, 30*time.Second, 2*time.Second).Should(gomega.BeFalse(),
				"Hairpin SNAT rule should be deleted after EIP deletion")
			ginkgo.By("[hairpin SNAT] Verifying hairpin rule for the shared SNAT EIP still exists")
			gomega.Expect(hairpinSnatRuleExists(vpcNatGwPodName, shareEip.Status.IP)).To(gomega.BeTrue(),
				"Hairpin SNAT rule for the shared SNAT EIP should NOT be affected by deleting a different SNAT")
		}

		// Verify SNAT iptables rule is removed after deletion (always, regardless of hairpin support)
		ginkgo.By("Verifying SNAT iptables rule is removed from NAT gateway pod after deletion")
		gomega.Eventually(func() bool {
			return snatRuleExists(vpcNatGwPodName, snatEip.Status.IP, overlaySubnetV4Cidr)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeFalse(),
			"SNAT rule should be removed from iptables after SNAT deletion")

		// Verify FIP iptables rules are removed after deletion
		ginkgo.By("Deleting FIP to verify iptables rule cleanup")
		iptablesFIPClient.DeleteSync(fipName)
		ginkgo.By("Verifying FIP iptables rules are removed from NAT gateway pod after deletion")
		gomega.Eventually(func() bool {
			return fipDnatRuleExists(vpcNatGwPodName, fipEip.Status.IP, fipVip.Status.V4ip)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeFalse(),
			"FIP DNAT rule should be removed from iptables after FIP deletion")
		gomega.Eventually(func() bool {
			return fipSnatRuleExists(vpcNatGwPodName, fipEip.Status.IP, fipVip.Status.V4ip)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeFalse(),
			"FIP SNAT rule should be removed from iptables after FIP deletion")

		// Verify DNAT iptables rule is removed after deletion
		ginkgo.By("Deleting DNAT to verify iptables rule cleanup")
		iptablesDnatRuleClient.DeleteSync(dnatName)
		ginkgo.By("Verifying DNAT iptables rule is removed from NAT gateway pod after deletion")
		gomega.Eventually(func() bool {
			return dnatRuleExists(vpcNatGwPodName, dnatEip.Status.IP, "80", "tcp", dnatVip.Status.V4ip, "8080")
		}, 30*time.Second, 2*time.Second).Should(gomega.BeFalse(),
			"DNAT rule should be removed from iptables after DNAT deletion")

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
			"",   // gwNamespace: use default (PodNamespace)
			0,
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
			framework.ExpectTrue(initialV4AvailableIPs.SubInt(1).Equal(afterCreateSubnet.Status.V4AvailableIPs),
				"V4AvailableIPs should decrease by 1 after IptablesEIP creation")
			framework.ExpectTrue(initialV4UsingIPs.AddInt(1).Equal(afterCreateSubnet.Status.V4UsingIPs),
				"V4UsingIPs should increase by 1 after IptablesEIP creation")
			framework.ExpectNotEqual(initialV4AvailableIPRange, afterCreateSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after IptablesEIP creation")
			framework.ExpectNotEqual(initialV4UsingIPRange, afterCreateSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after IptablesEIP creation")
		case apiv1.ProtocolIPv6:
			framework.ExpectTrue(initialV6AvailableIPs.SubInt(1).Equal(afterCreateSubnet.Status.V6AvailableIPs),
				"V6AvailableIPs should decrease by 1 after IptablesEIP creation")
			framework.ExpectTrue(initialV6UsingIPs.AddInt(1).Equal(afterCreateSubnet.Status.V6UsingIPs),
				"V6UsingIPs should increase by 1 after IptablesEIP creation")
			framework.ExpectNotEqual(initialV6AvailableIPRange, afterCreateSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after IptablesEIP creation")
			framework.ExpectNotEqual(initialV6UsingIPRange, afterCreateSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after IptablesEIP creation")
		default:
			// Dual stack
			framework.ExpectTrue(initialV4AvailableIPs.SubInt(1).Equal(afterCreateSubnet.Status.V4AvailableIPs),
				"V4AvailableIPs should decrease by 1 after IptablesEIP creation")
			framework.ExpectTrue(initialV4UsingIPs.AddInt(1).Equal(afterCreateSubnet.Status.V4UsingIPs),
				"V4UsingIPs should increase by 1 after IptablesEIP creation")
			framework.ExpectTrue(initialV6AvailableIPs.SubInt(1).Equal(afterCreateSubnet.Status.V6AvailableIPs),
				"V6AvailableIPs should decrease by 1 after IptablesEIP creation")
			framework.ExpectTrue(initialV6UsingIPs.AddInt(1).Equal(afterCreateSubnet.Status.V6UsingIPs),
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
			framework.ExpectTrue(afterCreateV4AvailableIPs.AddInt(1).Equal(afterDeleteSubnet.Status.V4AvailableIPs),
				"V4AvailableIPs should increase by 1 after IptablesEIP deletion")
			framework.ExpectTrue(afterCreateV4UsingIPs.SubInt(1).Equal(afterDeleteSubnet.Status.V4UsingIPs),
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
			framework.ExpectTrue(afterCreateV6AvailableIPs.AddInt(1).Equal(afterDeleteSubnet.Status.V6AvailableIPs),
				"V6AvailableIPs should increase by 1 after IptablesEIP deletion")
			framework.ExpectTrue(afterCreateV6UsingIPs.SubInt(1).Equal(afterDeleteSubnet.Status.V6UsingIPs),
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
			framework.ExpectTrue(afterCreateV4AvailableIPs.AddInt(1).Equal(afterDeleteSubnet.Status.V4AvailableIPs),
				"V4AvailableIPs should increase by 1 after IptablesEIP deletion")
			framework.ExpectTrue(afterCreateV4UsingIPs.SubInt(1).Equal(afterDeleteSubnet.Status.V4UsingIPs),
				"V4UsingIPs should decrease by 1 after IptablesEIP deletion")
			framework.ExpectTrue(afterCreateV6AvailableIPs.AddInt(1).Equal(afterDeleteSubnet.Status.V6AvailableIPs),
				"V6AvailableIPs should increase by 1 after IptablesEIP deletion")
			framework.ExpectTrue(afterCreateV6UsingIPs.SubInt(1).Equal(afterDeleteSubnet.Status.V6UsingIPs),
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
			"",   // gwNamespace: use default (PodNamespace)
			0,
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

	framework.ConformanceIt("[6] HA VPC NAT Gateway with 2 replicas", func() {
		f.SkipVersionPriorTo(1, 17, "HA NAT gateway support was introduced in v1.17")

		// NAT gateway pods are in kube-system namespace
		natGwPodClient := f.PodClientNS(framework.KubeOvnNamespace)
		deploymentClient := f.DeploymentClientNS(framework.KubeOvnNamespace)

		overlaySubnetV4Cidr := "10.0.7.0/24"
		overlaySubnetV4Gw := "10.0.7.1"
		bfdIP := "10.0.7.254"
		haVpcName := "ha-test-vpc-" + framework.RandomSuffix()
		haOverlaySubnetName := "ha-test-overlay-subnet-" + framework.RandomSuffix()
		haVpcNatGwName := "ha-test-natgw-" + framework.RandomSuffix()

		// Helper function to get current NAT gateway pod names
		// Pods may restart when configuration changes, so fetch fresh names each time
		getNatGwPodNames := func() []string {
			deploymentName := util.GenNatGwName(haVpcNatGwName)
			deploy := deploymentClient.Get(deploymentName)
			pods, err := deploymentClient.GetAllPods(deploy)
			if err != nil || len(pods.Items) == 0 {
				return nil
			}
			var podNames []string
			for _, pod := range pods.Items {
				if pod.Status.Phase == "Running" {
					podNames = append(podNames, pod.Name)
				}
			}
			return podNames
		}

		// Create VPC with BFD enabled and no static route
		ginkgo.By("Creating custom HA VPC with BFD enabled " + haVpcName)
		vpc := framework.MakeVpc(haVpcName, "", false, true, nil)
		vpc.Spec.BFDPort = &apiv1.BFDPort{
			Enabled: true,
			IP:      bfdIP,
		}
		_ = vpcClient.CreateSync(vpc)

		ginkgo.By("Creating custom overlay subnet " + haOverlaySubnetName)
		overlaySubnet := framework.MakeSubnet(haOverlaySubnetName, "", overlaySubnetV4Cidr, overlaySubnetV4Gw, haVpcName, "", nil, nil, nil)
		_ = subnetClient.CreateSync(overlaySubnet)

		ginkgo.By("Creating custom vpc nat gw " + haVpcNatGwName)
		vpcNatGw := framework.MakeVpcNatGatewayWithAnnotations(haVpcNatGwName, haVpcName, haOverlaySubnetName, "", networkAttachDefName, "", nil)
		vpcNatGw.Spec.Replicas = 2
		_ = vpcNatGwClient.CreateSync(vpcNatGw, f.ClientSet)

		ginkgo.By("1. Setting NAT gateway HA mode")
		natGw := vpcNatGwClient.Get(haVpcNatGwName)
		modifiedNatGw := natGw.DeepCopy()
		modifiedNatGw.Spec.BFD.Enabled = true
		modifiedNatGw.Spec.BFD.MinRX = 1000
		modifiedNatGw.Spec.BFD.MinTX = 1000
		modifiedNatGw.Spec.BFD.Multiplier = 3
		modifiedNatGw.Spec.InternalSubnets = []string{haOverlaySubnetName}
		vpcNatGwClient.PatchSync(natGw, modifiedNatGw, 4*time.Minute)

		ginkgo.By("2. Verifying NAT gateway becomes a Deployment with 2 replicas")
		deploymentName := util.GenNatGwName(haVpcNatGwName)
		gomega.Eventually(func() bool {
			deploy, err := deploymentClient.DeploymentInterface.Get(context.TODO(), deploymentName, metav1.GetOptions{})
			if err != nil {
				framework.Logf("Failed to get deployment %s: %v", deploymentName, err)
				return false
			}
			return deploy.Spec.Replicas != nil && *deploy.Spec.Replicas == 2
		}, 2*time.Minute, 5*time.Second).Should(gomega.BeTrue(),
			"NAT gateway should become a Deployment with 2 replicas")

		ginkgo.By("3. Waiting for both NAT gateway pods to be ready")
		gomega.Eventually(func() bool {
			deploy := deploymentClient.Get(deploymentName)
			pods, err := deploymentClient.GetAllPods(deploy)
			if err != nil {
				framework.Logf("Failed to get pods for deployment: %v", err)
				return false
			}
			if len(pods.Items) != 2 {
				framework.Logf("Expected 2 pods, got %d", len(pods.Items))
				for _, pod := range pods.Items {
					framework.Logf("Found pod %s", pod.Name)
				}
				return false
			}
			for _, pod := range pods.Items {
				if pod.Status.Phase != "Running" {
					framework.Logf("Pod %s not running: %s", pod.Name, pod.Status.Phase)
					return false
				}
				if pod.Annotations[util.VpcNatGatewayInitAnnotation] != "true" {
					framework.Logf("Pod %s not initialized yet", pod.Name)
					return false
				}
			}
			return true
		}, 4*time.Minute, 5*time.Second).Should(gomega.BeTrue(),
			"Both NAT gateway pods should be ready")

		ginkgo.By("4. Creating EIPs for testing")
		randomSuffix := framework.RandomSuffix()
		eipName1 := "ha-eip1-" + randomSuffix
		eipName2 := "ha-eip2-" + randomSuffix
		eip1 := framework.MakeIptablesEIP(eipName1, "", "", "", haVpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(eip1)
		eip1 = iptablesEIPClient.Get(eipName1)

		eip2 := framework.MakeIptablesEIP(eipName2, "", "", "", haVpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(eip2)
		eip2 = iptablesEIPClient.Get(eipName2)

		ginkgo.By("5. Creating VIPs for DNAT/FIP testing")
		fipVipName := "ha-fip-vip-" + randomSuffix
		dnatVipName := "ha-dnat-vip-" + randomSuffix
		fipVip := framework.MakeVip(f.Namespace.Name, fipVipName, haOverlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(fipVip)
		fipVip = vipClient.Get(fipVipName)

		dnatVip := framework.MakeVip(f.Namespace.Name, dnatVipName, haOverlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(dnatVip)
		dnatVip = vipClient.Get(dnatVipName)

		ginkgo.By("6. Creating FIP rule")
		fipName := "ha-fip-" + randomSuffix
		fip := framework.MakeIptablesFIPRule(fipName, eipName1, fipVip.Status.V4ip)
		_ = iptablesFIPClient.CreateSync(fip)

		ginkgo.By("7. Creating DNAT rule")
		dnatName := "ha-dnat-" + randomSuffix
		dnat := framework.MakeIptablesDnatRule(dnatName, eipName1, "80", "tcp", dnatVip.Status.V4ip, "8080")
		_ = iptablesDnatRuleClient.CreateSync(dnat)

		ginkgo.By("8. Creating SNAT rule")
		snatName := "ha-snat-" + randomSuffix
		snatCIDR := "10.0.7.0/25"
		snat := framework.MakeIptablesSnatRule(snatName, eipName2, snatCIDR)
		_ = iptablesSnatRuleClient.CreateSync(snat)

		ginkgo.By("9. Verifying FIP/DNAT/SNAT iptables rules exist on BOTH NAT gateway pods")
		natGwPods := getNatGwPodNames()
		framework.ExpectNotEmpty(natGwPods, "NAT gateway pods should exist")
		for _, podName := range natGwPods {
			framework.Logf("Checking iptables rules on pod %s", podName)

			gomega.Eventually(func() bool {
				return fipDnatRuleExists(podName, eip1.Status.IP, fipVip.Status.V4ip)
			}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
				"FIP DNAT rule should exist on pod %s", podName)

			gomega.Eventually(func() bool {
				return fipSnatRuleExists(podName, eip1.Status.IP, fipVip.Status.V4ip)
			}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
				"FIP SNAT rule should exist on pod %s", podName)

			gomega.Eventually(func() bool {
				return dnatRuleExists(podName, eip1.Status.IP, "80", "tcp", dnatVip.Status.V4ip, "8080")
			}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
				"DNAT rule should exist on pod %s", podName)

			gomega.Eventually(func() bool {
				return snatRuleExists(podName, eip2.Status.IP, snatCIDR)
			}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
				"SNAT rule should exist on pod %s", podName)
		}

		ginkgo.By("10. Verifying EIP IPs are added on both NAT gateway pods")
		natGwPods = getNatGwPodNames()
		for _, podName := range natGwPods {
			framework.Logf("Checking EIP IPs on pod %s", podName)

			gomega.Eventually(func() bool {
				cmd := []string{"ip", "addr", "show"}
				stdout, _, err := framework.KubectlExec(framework.KubeOvnNamespace, podName, cmd...)
				if err != nil {
					framework.Logf("Failed to exec ip addr on pod %s: %v", podName, err)
					return false
				}
				output := string(stdout)
				return strings.Contains(output, eip1.Status.IP) && strings.Contains(output, eip2.Status.IP)
			}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
				"Both EIP IPs should be configured on pod %s", podName)
		}

		ginkgo.By("11. Verifying BFD sessions are established and UP for each NAT gateway instance")
		vpc = vpcClient.Get(haVpcName)
		natGwPods = getNatGwPodNames()
		for _, podName := range natGwPods {
			framework.Logf("Checking BFD session for pod %s", podName)

			// Get pod's LAN IP and verify BFD session is UP
			gomega.Eventually(func() bool {
				podObj := natGwPodClient.GetPod(podName)
				if podObj.Annotations[util.LogicalRouterAnnotation] != haVpcName {
					framework.Logf("Pod %s not attached to VPC %s yet", podName, haVpcName)
					return false
				}
				podIP := podObj.Annotations[util.IPAddressAnnotation]
				if podIP == "" {
					framework.Logf("Pod %s has no IP yet", podName)
					return false
				}

				// Check if BFD session exists and is UP in OVN
				// Output format: dst_ip,status (one line per BFD endpoint)
				cmd := fmt.Sprintf("ovn-nbctl --format=csv --data=bare --no-heading --columns=dst_ip,status,logical_port find BFD logical_port=bfd@%s", vpc.Name)
				stdout, _, err := framework.NBExec(cmd)
				if err != nil {
					framework.Logf("Failed to query BFD sessions: %v", err)
					return false
				}
				output := string(stdout)

				// Parse output to find this pod's BFD session and check status
				// Expected format: <ip>,<status> (e.g., "10.0.7.100,up")
				lines := strings.SplitSeq(strings.TrimSpace(output), "\n")
				for line := range lines {
					parts := strings.Split(line, ",")
					if len(parts) == 3 && strings.TrimSpace(parts[0]) == podIP {
						status := strings.TrimSpace(parts[1])
						if status == "up" {
							framework.Logf("BFD session for pod %s (IP: %s) is UP", podName, podIP)
							return true
						}
						framework.Logf("BFD session for pod %s (IP: %s) exists but status is: %s", podName, podIP, status)
						return false
					}
				}
				framework.Logf("BFD session for pod %s (IP: %s) not found in output: %s", podName, podIP, output)
				return false
			}, 2*time.Minute, 5*time.Second).Should(gomega.BeTrue(),
				"BFD session should exist and be UP for pod %s", podName)
		}

		ginkgo.By("12. Verifying policy routes are added for each NAT gateway instance")
		natGwPods = getNatGwPodNames()
		for _, podName := range natGwPods {
			framework.Logf("Checking policy routes for pod %s", podName)

			gomega.Eventually(func() bool {
				podObj := natGwPodClient.GetPod(podName)
				podIP := podObj.Annotations[util.IPAddressAnnotation]
				if podIP == "" {
					return false
				}

				// Check for policy routes pointing to this pod's IP
				cmd := fmt.Sprintf("ovn-nbctl lr-policy-list %s", haVpcName)
				stdout, _, err := framework.NBExec(cmd)
				if err != nil {
					framework.Logf("Failed to list routes: %v", err)
					return false
				}
				output := string(stdout)
				// Should have routes with this pod as nexthop
				return strings.Contains(output, podIP)
			}, 2*time.Minute, 5*time.Second).Should(gomega.BeTrue(),
				"Policy routes should exist for pod %s", podName)
		}

		ginkgo.By("Cleaning up: Deleting SNAT rule")
		iptablesSnatRuleClient.DeleteSync(snatName)

		ginkgo.By("Cleaning up: Deleting DNAT rule")
		iptablesDnatRuleClient.DeleteSync(dnatName)

		ginkgo.By("Cleaning up: Deleting FIP rule")
		iptablesFIPClient.DeleteSync(fipName)

		ginkgo.By("Cleaning up: Deleting VIPs")
		vipClient.DeleteSync(dnatVipName)
		vipClient.DeleteSync(fipVipName)

		ginkgo.By("Cleaning up: Deleting EIPs")
		iptablesEIPClient.DeleteSync(eipName2)
		iptablesEIPClient.DeleteSync(eipName1)

		ginkgo.By("13. Deleting NAT gateway and verifying cleanup")
		vpcNatGwClient.DeleteSync(haVpcNatGwName)

		ginkgo.By("14. Verifying Deployment is deleted")
		gomega.Eventually(func() bool {
			_, err := deploymentClient.DeploymentInterface.Get(context.TODO(), deploymentName, metav1.GetOptions{})
			return k8serrors.IsNotFound(err)
		}, 2*time.Minute, 5*time.Second).Should(gomega.BeTrue(),
			"Deployment should be deleted")

		ginkgo.By("15. Verifying BFD sessions are removed")
		gomega.Eventually(func() bool {
			cmd := fmt.Sprintf("ovn-nbctl --format=csv --data=bare --no-heading --columns=dst_ip,status,logical_port find BFD logical_port=bfd@%s", haVpcName)
			stdout, _, err := framework.NBExec(cmd)
			if err != nil {
				framework.Logf("Failed to query BFD sessions: %v", err)
				return false
			}
			// Should have no BFD sessions
			return strings.TrimSpace(string(stdout)) == ""
		}, 2*time.Minute, 5*time.Second).Should(gomega.BeTrue(),
			"All BFD sessions should be removed")

		ginkgo.By("16. Verifying policy routes are removed")
		gomega.Eventually(func() bool {
			// Verify that no routes exist for the HA VPC (besides the default routes)
			cmd := fmt.Sprintf("ovn-nbctl lr-policy-list %s", haVpcName)
			stdout, _, err := framework.NBExec(cmd)
			if err != nil {
				framework.Logf("Failed to list routes: %v", err)
				return false
			}
			output := strings.TrimSpace(string(stdout))
			// When NAT gateway is deleted, only the subnet routes should remain
			// The policy routes added for NAT gateway pod IPs should be gone
			// We can check that there are no bfd routes (which are the NAT GW routes)
			return !strings.Contains(output, "bfd")
		}, 2*time.Minute, 5*time.Second).Should(gomega.BeTrue(),
			"All static routes for NAT gateway instances should be removed")

		ginkgo.By("Cleaning up: Deleting overlay subnet (after NAT Gateway)")
		subnetClient.DeleteSync(haOverlaySubnetName)

		ginkgo.By("Cleaning up: Deleting VPC (after subnet)")
		vpcClient.DeleteSync(haVpcName)
	})

	framework.ConformanceIt("[7] FIP/DNAT/SNAT spec update with iptables rule verification", func() {
		f.SkipVersionPriorTo(1, 16, "FIP/DNAT/SNAT spec update was introduced in v1.16")

		overlaySubnetV4Cidr := "10.0.6.0/24"
		overlaySubnetV4Gw := "10.0.6.1"
		lanIP := "10.0.6.254"
		setupVpcNatGwTestEnvironment(
			f, dockerExtNet1Network, attachNetClient,
			subnetClient, vpcClient, vpcNatGwClient,
			vpcName, overlaySubnetName, vpcNatGwName, "",
			overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
			dockerExtNet1Name, networkAttachDefName, net1NicName,
			externalSubnetProvider,
			true, nil,
			"", // gwNamespace: use default (PodNamespace)
			0,
		)
		vpcNatGwPodName := getNatGwPodName(f, vpcNatGwName, "")

		// ===================== FIP spec update =====================
		ginkgo.By("1. Creating two VIPs for FIP (old and new InternalIP)")
		randomSuffix := framework.RandomSuffix()
		oldFipVipName := "old-fip-vip-" + randomSuffix
		newFipVipName := "new-fip-vip-" + randomSuffix
		oldFipVip := framework.MakeVip(f.Namespace.Name, oldFipVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(oldFipVip)
		ginkgo.DeferCleanup(func() { vipClient.DeleteSync(oldFipVipName) })
		oldFipVip = vipClient.Get(oldFipVipName)

		newFipVip := framework.MakeVip(f.Namespace.Name, newFipVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(newFipVip)
		ginkgo.DeferCleanup(func() { vipClient.DeleteSync(newFipVipName) })
		newFipVip = vipClient.Get(newFipVipName)

		ginkgo.By("2. Creating two EIPs for FIP (old and new EIP)")
		oldFipEipName := "old-fip-eip-" + randomSuffix
		newFipEipName := "new-fip-eip-" + randomSuffix
		oldFipEip := framework.MakeIptablesEIP(oldFipEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(oldFipEip)
		ginkgo.DeferCleanup(func() { iptablesEIPClient.DeleteSync(oldFipEipName) })
		oldFipEip = iptablesEIPClient.Get(oldFipEipName)

		newFipEip := framework.MakeIptablesEIP(newFipEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(newFipEip)
		ginkgo.DeferCleanup(func() { iptablesEIPClient.DeleteSync(newFipEipName) })
		newFipEip = iptablesEIPClient.Get(newFipEipName)

		ginkgo.By("3. Creating FIP with old EIP and old InternalIP")
		fipName := "fip-update-" + randomSuffix
		fip := framework.MakeIptablesFIPRule(fipName, oldFipEipName, oldFipVip.Status.V4ip)
		_ = iptablesFIPClient.CreateSync(fip)
		ginkgo.DeferCleanup(func() { iptablesFIPClient.DeleteSync(fipName) })

		ginkgo.By("4. Verifying old FIP iptables rules exist")
		gomega.Eventually(func() bool {
			return fipDnatRuleExists(vpcNatGwPodName, oldFipEip.Status.IP, oldFipVip.Status.V4ip)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"FIP DNAT rule should exist with old EIP and old InternalIP")
		gomega.Eventually(func() bool {
			return fipSnatRuleExists(vpcNatGwPodName, oldFipEip.Status.IP, oldFipVip.Status.V4ip)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"FIP SNAT rule should exist with old EIP and old InternalIP")

		ginkgo.By("5. Updating FIP: changing both EIP and InternalIP simultaneously")
		fip = iptablesFIPClient.Get(fipName)
		modifiedFip := fip.DeepCopy()
		modifiedFip.Spec.EIP = newFipEipName
		modifiedFip.Spec.InternalIP = newFipVip.Status.V4ip
		iptablesFIPClient.PatchSync(fip, modifiedFip, nil, 2*time.Minute)

		ginkgo.By("6. Verifying old FIP iptables rules are removed")
		gomega.Eventually(func() bool {
			return fipDnatRuleExists(vpcNatGwPodName, oldFipEip.Status.IP, oldFipVip.Status.V4ip)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeFalse(),
			"Old FIP DNAT rule should be removed after spec update")
		gomega.Eventually(func() bool {
			return fipSnatRuleExists(vpcNatGwPodName, oldFipEip.Status.IP, oldFipVip.Status.V4ip)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeFalse(),
			"Old FIP SNAT rule should be removed after spec update")

		ginkgo.By("7. Verifying new FIP iptables rules exist")
		gomega.Eventually(func() bool {
			return fipDnatRuleExists(vpcNatGwPodName, newFipEip.Status.IP, newFipVip.Status.V4ip)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"New FIP DNAT rule should exist after spec update")
		gomega.Eventually(func() bool {
			return fipSnatRuleExists(vpcNatGwPodName, newFipEip.Status.IP, newFipVip.Status.V4ip)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"New FIP SNAT rule should exist after spec update")

		ginkgo.By("8. Verifying FIP Status reflects new values (dimension 2)")
		fip = iptablesFIPClient.Get(fipName)
		framework.ExpectEqual(fip.Status.V4ip, newFipEip.Status.IP,
			"FIP Status.V4ip should match new EIP IP")
		framework.ExpectEqual(fip.Status.InternalIP, newFipVip.Status.V4ip,
			"FIP Status.InternalIP should match new InternalIP")
		framework.ExpectTrue(fip.Status.Ready, "FIP should be ready after update")

		ginkgo.By("9. Verifying FIP Label reflects new EIP (dimension 3)")
		framework.ExpectHaveKeyWithValue(fip.Labels, util.EipV4IpLabel, newFipEip.Spec.V4ip)
		framework.ExpectHaveKeyWithValue(fip.Annotations, util.VpcEipAnnotation, newFipEipName)

		// ===================== DNAT spec update =====================
		ginkgo.By("10. Creating VIPs for DNAT (old and new InternalIP)")
		oldDnatVipName := "old-dnat-vip-" + randomSuffix
		newDnatVipName := "new-dnat-vip-" + randomSuffix
		oldDnatVip := framework.MakeVip(f.Namespace.Name, oldDnatVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(oldDnatVip)
		ginkgo.DeferCleanup(func() { vipClient.DeleteSync(oldDnatVipName) })
		oldDnatVip = vipClient.Get(oldDnatVipName)

		newDnatVip := framework.MakeVip(f.Namespace.Name, newDnatVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(newDnatVip)
		ginkgo.DeferCleanup(func() { vipClient.DeleteSync(newDnatVipName) })
		newDnatVip = vipClient.Get(newDnatVipName)

		ginkgo.By("11. Creating EIPs for DNAT")
		dnatEipName := "dnat-eip-" + randomSuffix
		dnatEip := framework.MakeIptablesEIP(dnatEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(dnatEip)
		ginkgo.DeferCleanup(func() { iptablesEIPClient.DeleteSync(dnatEipName) })
		dnatEip = iptablesEIPClient.Get(dnatEipName)

		dnatEip2Name := "dnat-eip2-" + randomSuffix
		dnatEip2 := framework.MakeIptablesEIP(dnatEip2Name, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(dnatEip2)
		ginkgo.DeferCleanup(func() { iptablesEIPClient.DeleteSync(dnatEip2Name) })
		dnatEip2 = iptablesEIPClient.Get(dnatEip2Name)

		ginkgo.By("12. Creating DNAT with old InternalIP and port 80->8080")
		dnatName := "dnat-update-" + randomSuffix
		dnat := framework.MakeIptablesDnatRule(dnatName, dnatEipName, "80", "tcp", oldDnatVip.Status.V4ip, "8080")
		_ = iptablesDnatRuleClient.CreateSync(dnat)
		ginkgo.DeferCleanup(func() { iptablesDnatRuleClient.DeleteSync(dnatName) })

		ginkgo.By("13. Verifying old DNAT iptables rule exists")
		gomega.Eventually(func() bool {
			return dnatRuleExists(vpcNatGwPodName, dnatEip.Status.IP, "80", "tcp", oldDnatVip.Status.V4ip, "8080")
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"DNAT rule should exist with old values")

		ginkgo.By("14. Updating DNAT: changing InternalIP and InternalPort")
		dnat = iptablesDnatRuleClient.Get(dnatName)
		modifiedDnat := dnat.DeepCopy()
		modifiedDnat.Spec.InternalIP = newDnatVip.Status.V4ip
		modifiedDnat.Spec.InternalPort = "9090"
		iptablesDnatRuleClient.PatchSync(dnat, modifiedDnat, nil, 2*time.Minute)

		ginkgo.By("15. Verifying old DNAT iptables rule is removed")
		gomega.Eventually(func() bool {
			return dnatRuleExists(vpcNatGwPodName, dnatEip.Status.IP, "80", "tcp", oldDnatVip.Status.V4ip, "8080")
		}, 30*time.Second, 2*time.Second).Should(gomega.BeFalse(),
			"Old DNAT rule should be removed after spec update")

		ginkgo.By("16. Verifying new DNAT iptables rule exists")
		gomega.Eventually(func() bool {
			return dnatRuleExists(vpcNatGwPodName, dnatEip.Status.IP, "80", "tcp", newDnatVip.Status.V4ip, "9090")
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"New DNAT rule should exist after spec update")

		ginkgo.By("17. Verifying DNAT Status reflects new values")
		dnat = iptablesDnatRuleClient.Get(dnatName)
		framework.ExpectEqual(dnat.Status.InternalIP, newDnatVip.Status.V4ip,
			"DNAT Status.InternalIP should match new InternalIP")
		framework.ExpectEqual(dnat.Status.InternalPort, "9090",
			"DNAT Status.InternalPort should match new InternalPort")
		framework.ExpectTrue(dnat.Status.Ready, "DNAT should be ready after update")

		// --- DNAT second update: change EIP + ExternalPort + Protocol (identity side) ---
		ginkgo.By("17a. Updating DNAT: changing EIP to dnatEip2, ExternalPort, and Protocol (all identity fields)")
		dnat = iptablesDnatRuleClient.Get(dnatName)
		modifiedDnat2 := dnat.DeepCopy()
		modifiedDnat2.Spec.EIP = dnatEip2Name
		modifiedDnat2.Spec.ExternalPort = "443"
		modifiedDnat2.Spec.Protocol = "udp"
		iptablesDnatRuleClient.PatchSync(dnat, modifiedDnat2, nil, 2*time.Minute)

		ginkgo.By("17b. Verifying old DNAT rule (from first update) is removed")
		gomega.Eventually(func() bool {
			return dnatRuleExists(vpcNatGwPodName, dnatEip.Status.IP, "80", "tcp", newDnatVip.Status.V4ip, "9090")
		}, 30*time.Second, 2*time.Second).Should(gomega.BeFalse(),
			"Previous DNAT rule should be removed after identity change")

		ginkgo.By("17c. Verifying new DNAT rule with updated identity exists")
		gomega.Eventually(func() bool {
			return dnatRuleExists(vpcNatGwPodName, dnatEip2.Status.IP, "443", "udp", newDnatVip.Status.V4ip, "9090")
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"New DNAT rule should exist with new EIP, ExternalPort, Protocol")

		ginkgo.By("17d. Verifying DNAT Status reflects all new values")
		dnat = iptablesDnatRuleClient.Get(dnatName)
		framework.ExpectEqual(dnat.Status.V4ip, dnatEip2.Status.IP,
			"DNAT Status.V4ip should match new EIP IP")
		framework.ExpectEqual(dnat.Status.ExternalPort, "443",
			"DNAT Status.ExternalPort should match new ExternalPort")
		framework.ExpectEqual(dnat.Status.Protocol, "udp",
			"DNAT Status.Protocol should match new Protocol")
		framework.ExpectTrue(dnat.Status.Ready, "DNAT should be ready after identity change")
		framework.ExpectHaveKeyWithValue(dnat.Labels, util.EipV4IpLabel, dnatEip2.Spec.V4ip)
		framework.ExpectHaveKeyWithValue(dnat.Annotations, util.VpcEipAnnotation, dnatEip2Name)

		// ===================== SNAT spec update =====================
		ginkgo.By("18. Creating EIPs for SNAT (old, new, and third)")
		oldSnatEipName := "old-snat-eip-" + randomSuffix
		newSnatEipName := "new-snat-eip-" + randomSuffix
		snatEip3Name := "snat-eip3-" + randomSuffix
		oldSnatEip := framework.MakeIptablesEIP(oldSnatEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(oldSnatEip)
		ginkgo.DeferCleanup(func() { iptablesEIPClient.DeleteSync(oldSnatEipName) })
		oldSnatEip = iptablesEIPClient.Get(oldSnatEipName)

		newSnatEip := framework.MakeIptablesEIP(newSnatEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(newSnatEip)
		ginkgo.DeferCleanup(func() { iptablesEIPClient.DeleteSync(newSnatEipName) })
		newSnatEip = iptablesEIPClient.Get(newSnatEipName)

		snatEip3 := framework.MakeIptablesEIP(snatEip3Name, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(snatEip3)
		ginkgo.DeferCleanup(func() { iptablesEIPClient.DeleteSync(snatEip3Name) })
		snatEip3 = iptablesEIPClient.Get(snatEip3Name)

		ginkgo.By("19. Creating SNAT with old EIP and CIDR 10.0.6.0/25")
		snatName := "snat-update-" + randomSuffix
		oldSnatCIDR := "10.0.6.0/25"
		newSnatCIDR := "10.0.6.128/25"
		snat := framework.MakeIptablesSnatRule(snatName, oldSnatEipName, oldSnatCIDR)
		_ = iptablesSnatRuleClient.CreateSync(snat)
		ginkgo.DeferCleanup(func() { iptablesSnatRuleClient.DeleteSync(snatName) })

		ginkgo.By("20. Verifying old SNAT iptables rule exists")
		gomega.Eventually(func() bool {
			return snatRuleExists(vpcNatGwPodName, oldSnatEip.Status.IP, oldSnatCIDR)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"SNAT rule should exist with old EIP and old CIDR")

		ginkgo.By("21. Updating SNAT: changing both EIP and InternalCIDR")
		snat = iptablesSnatRuleClient.Get(snatName)
		modifiedSnat := snat.DeepCopy()
		modifiedSnat.Spec.EIP = newSnatEipName
		modifiedSnat.Spec.InternalCIDR = newSnatCIDR
		iptablesSnatRuleClient.PatchSync(snat, modifiedSnat, nil, 2*time.Minute)

		ginkgo.By("22. Verifying old SNAT iptables rule is removed")
		gomega.Eventually(func() bool {
			return snatRuleExists(vpcNatGwPodName, oldSnatEip.Status.IP, oldSnatCIDR)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeFalse(),
			"Old SNAT rule should be removed after spec update")

		ginkgo.By("23. Verifying new SNAT iptables rule exists")
		gomega.Eventually(func() bool {
			return snatRuleExists(vpcNatGwPodName, newSnatEip.Status.IP, newSnatCIDR)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"New SNAT rule should exist after spec update")

		ginkgo.By("24. Verifying SNAT Status reflects new values")
		snat = iptablesSnatRuleClient.Get(snatName)
		framework.ExpectEqual(snat.Status.V4ip, newSnatEip.Status.IP,
			"SNAT Status.V4ip should match new EIP IP")
		framework.ExpectEqual(snat.Status.InternalCIDR, newSnatCIDR,
			"SNAT Status.InternalCIDR should match new InternalCIDR")
		framework.ExpectTrue(snat.Status.Ready, "SNAT should be ready after update")
		framework.ExpectHaveKeyWithValue(snat.Labels, util.EipV4IpLabel, newSnatEip.Spec.V4ip)
		framework.ExpectHaveKeyWithValue(snat.Annotations, util.VpcEipAnnotation, newSnatEipName)

		// --- SNAT second update: change only EIP, keep InternalCIDR unchanged ---
		ginkgo.By("24a. Updating SNAT: changing only EIP, keeping InternalCIDR=" + newSnatCIDR)
		snat = iptablesSnatRuleClient.Get(snatName)
		modifiedSnat2 := snat.DeepCopy()
		modifiedSnat2.Spec.EIP = snatEip3Name
		iptablesSnatRuleClient.PatchSync(snat, modifiedSnat2, nil, 2*time.Minute)

		ginkgo.By("24b. Verifying old SNAT rule (newSnatEip + newSnatCIDR) is removed")
		gomega.Eventually(func() bool {
			return snatRuleExists(vpcNatGwPodName, newSnatEip.Status.IP, newSnatCIDR)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeFalse(),
			"Previous SNAT rule should be removed after EIP-only change")

		ginkgo.By("24c. Verifying new SNAT rule with third EIP exists")
		gomega.Eventually(func() bool {
			return snatRuleExists(vpcNatGwPodName, snatEip3.Status.IP, newSnatCIDR)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"New SNAT rule should exist with third EIP and same CIDR")

		ginkgo.By("24d. Verifying SNAT Status and labels reflect third EIP")
		snat = iptablesSnatRuleClient.Get(snatName)
		framework.ExpectEqual(snat.Status.V4ip, snatEip3.Status.IP,
			"SNAT Status.V4ip should match third EIP IP")
		framework.ExpectEqual(snat.Status.InternalCIDR, newSnatCIDR,
			"SNAT Status.InternalCIDR should remain unchanged after EIP-only change")
		framework.ExpectTrue(snat.Status.Ready, "SNAT should be ready after EIP-only change")
		framework.ExpectHaveKeyWithValue(snat.Labels, util.EipV4IpLabel, snatEip3.Spec.V4ip)
		framework.ExpectHaveKeyWithValue(snat.Annotations, util.VpcEipAnnotation, snatEip3Name)
	})

	framework.ConformanceIt("[7] SHARED_SNAT rules are ordered by longest-prefix-first", func() {
		// When multiple IptablesSnatRule share overlapping CIDRs, iptables evaluates
		// SHARED_SNAT rules top-down (first-match wins). A less-specific rule (e.g.,
		// /16) placed before a more-specific one (e.g., /24) would shadow the /24,
		// producing the wrong SNAT source IP. The NAT gateway script therefore
		// inserts each new rule at a position computed from existing prefix lengths
		// so the chain is kept sorted by descending prefix length.
		f.SkipVersionPriorTo(1, 18, "Longest-prefix-first SNAT ordering was introduced in v1.18")

		overlaySubnetV4Cidr := "10.0.7.0/24"
		overlaySubnetV4Gw := "10.0.7.1"
		lanIP := "10.0.7.254"
		setupVpcNatGwTestEnvironment(
			f, dockerExtNet1Network, attachNetClient,
			subnetClient, vpcClient, vpcNatGwClient,
			vpcName, overlaySubnetName, vpcNatGwName, "",
			overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
			dockerExtNet1Name, networkAttachDefName, net1NicName,
			externalSubnetProvider,
			true, nil,
			"", // gwNamespace: use default (PodNamespace)
			0,
		)
		vpcNatGwPodName := getNatGwPodName(f, vpcNatGwName, "")

		randomSuffix := framework.RandomSuffix()
		// Use CIDRs that are unrelated to any real subnet; they only drive the
		// iptables -s match, so the SNAT rule works regardless of whether pods
		// actually exist in these ranges. The three CIDRs are chosen so each
		// strictly contains the next one, making ordering observable.
		cidr16 := "172.31.0.0/16"
		cidr20 := "172.31.0.0/20"
		cidr24 := "172.31.0.0/24"

		eip16Name := "order-eip-16-" + randomSuffix
		eip20Name := "order-eip-20-" + randomSuffix
		eip24Name := "order-eip-24-" + randomSuffix
		snat16Name := "order-snat-16-" + randomSuffix
		snat20Name := "order-snat-20-" + randomSuffix
		snat24Name := "order-snat-24-" + randomSuffix

		ginkgo.By("Creating three EIPs for overlapping SNAT rules")
		eip16 := framework.MakeIptablesEIP(eip16Name, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(eip16)
		ginkgo.DeferCleanup(func() { iptablesEIPClient.DeleteSync(eip16Name) })
		eip16 = iptablesEIPClient.Get(eip16Name)

		eip20 := framework.MakeIptablesEIP(eip20Name, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(eip20)
		ginkgo.DeferCleanup(func() { iptablesEIPClient.DeleteSync(eip20Name) })
		eip20 = iptablesEIPClient.Get(eip20Name)

		eip24 := framework.MakeIptablesEIP(eip24Name, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(eip24)
		ginkgo.DeferCleanup(func() { iptablesEIPClient.DeleteSync(eip24Name) })
		eip24 = iptablesEIPClient.Get(eip24Name)

		// Create rules in the worst order for iptables first-match semantics:
		// broadest first, then increasingly specific. Without prefix-based
		// ordering the /16 would sit at the top and shadow the /20 and /24.
		ginkgo.By("Creating SNAT rule with CIDR " + cidr16 + " (broadest) first")
		snat16 := framework.MakeIptablesSnatRule(snat16Name, eip16Name, cidr16)
		_ = iptablesSnatRuleClient.CreateSync(snat16)
		ginkgo.DeferCleanup(func() { iptablesSnatRuleClient.DeleteSync(snat16Name) })
		gomega.Eventually(func() bool {
			return snatRuleExists(vpcNatGwPodName, eip16.Status.IP, cidr16)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"broadest SNAT rule should be installed")

		ginkgo.By("Creating SNAT rule with CIDR " + cidr20)
		snat20 := framework.MakeIptablesSnatRule(snat20Name, eip20Name, cidr20)
		_ = iptablesSnatRuleClient.CreateSync(snat20)
		ginkgo.DeferCleanup(func() { iptablesSnatRuleClient.DeleteSync(snat20Name) })
		gomega.Eventually(func() bool {
			return snatRuleExists(vpcNatGwPodName, eip20.Status.IP, cidr20)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"mid-prefix SNAT rule should be installed")

		ginkgo.By("Creating SNAT rule with CIDR " + cidr24 + " (most specific) last")
		snat24 := framework.MakeIptablesSnatRule(snat24Name, eip24Name, cidr24)
		_ = iptablesSnatRuleClient.CreateSync(snat24)
		ginkgo.DeferCleanup(func() { iptablesSnatRuleClient.DeleteSync(snat24Name) })
		gomega.Eventually(func() bool {
			return snatRuleExists(vpcNatGwPodName, eip24.Status.IP, cidr24)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"most-specific SNAT rule should be installed")

		ginkgo.By("Verifying SHARED_SNAT chain is ordered /24 -> /20 -> /16")
		gomega.Eventually(func() bool {
			p24 := snatRulePosition(vpcNatGwPodName, eip24.Status.IP, cidr24)
			p20 := snatRulePosition(vpcNatGwPodName, eip20.Status.IP, cidr20)
			p16 := snatRulePosition(vpcNatGwPodName, eip16.Status.IP, cidr16)
			framework.Logf("SHARED_SNAT positions: /24=%d /20=%d /16=%d", p24, p20, p16)
			return p24 > 0 && p20 > 0 && p16 > 0 && p24 < p20 && p20 < p16
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"more-specific SNAT rules must appear before less-specific ones in SHARED_SNAT")

		ginkgo.By("Removing the middle (/20) rule and verifying remaining order /24 -> /16 is preserved")
		iptablesSnatRuleClient.DeleteSync(snat20Name)
		gomega.Eventually(func() bool {
			return !snatRuleExists(vpcNatGwPodName, eip20.Status.IP, cidr20)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"/20 SNAT rule should be removed")
		gomega.Eventually(func() bool {
			p24 := snatRulePosition(vpcNatGwPodName, eip24.Status.IP, cidr24)
			p16 := snatRulePosition(vpcNatGwPodName, eip16.Status.IP, cidr16)
			return p24 > 0 && p16 > 0 && p24 < p16
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"remaining SNAT rules must retain longest-prefix-first ordering after deletion")

		// Bare-IP InternalCIDR (e.g., "10.0.0.5" without "/len") is a supported
		// input shape per validateSnatRule in vpc_nat_gw_nat.go. iptables normalizes
		// it to /32 in iptables-save output, so the host rule must sort before the
		// /24 rule (most specific first).
		//
		// The chosen IP has a small leading octet (10) on purpose. If the shell
		// extracts new_prefix via `${internalCIDR##*/}` on a bare IP (the original
		// bug), awk parses "10.0.0.5" as 10, so existing /16 and /24 rules all
		// satisfy `existing_prefix >= 10` and the host rule ends up at the tail of
		// the chain (shadowed). A bare IP with a large leading octet such as
		// 172.31.0.5 would be parsed as ~172 by awk and accidentally sort first,
		// masking the regression — hence 10.0.0.5 here.
		ginkgo.By("Creating a host SNAT rule with bare IPv4 InternalCIDR")
		eipHostName := "order-eip-host-" + randomSuffix
		snatHostName := "order-snat-host-" + randomSuffix
		bareHostIP := "10.0.0.5"
		normalizedHostCIDR := bareHostIP + "/32"
		eipHost := framework.MakeIptablesEIP(eipHostName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(eipHost)
		ginkgo.DeferCleanup(func() { iptablesEIPClient.DeleteSync(eipHostName) })
		eipHost = iptablesEIPClient.Get(eipHostName)

		snatHost := framework.MakeIptablesSnatRule(snatHostName, eipHostName, bareHostIP)
		_ = iptablesSnatRuleClient.CreateSync(snatHost)
		ginkgo.DeferCleanup(func() { iptablesSnatRuleClient.DeleteSync(snatHostName) })
		gomega.Eventually(func() bool {
			return snatRuleExists(vpcNatGwPodName, eipHost.Status.IP, normalizedHostCIDR)
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"host SNAT rule should be installed and normalized to /32 in iptables")

		ginkgo.By("Verifying bare-IP host rule sorts before the /24 rule (treated as /32)")
		gomega.Eventually(func() bool {
			pHost := snatRulePosition(vpcNatGwPodName, eipHost.Status.IP, normalizedHostCIDR)
			p24 := snatRulePosition(vpcNatGwPodName, eip24.Status.IP, cidr24)
			p16 := snatRulePosition(vpcNatGwPodName, eip16.Status.IP, cidr16)
			framework.Logf("SHARED_SNAT positions: host=%d /24=%d /16=%d", pHost, p24, p16)
			return pHost > 0 && p24 > 0 && p16 > 0 && pHost < p24 && p24 < p16
		}, 30*time.Second, 2*time.Second).Should(gomega.BeTrue(),
			"bare-IP SNAT rule must be treated as /32 and sort before less-specific rules")
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
