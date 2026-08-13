package ovn_eip

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	nad "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	gomegatypes "github.com/onsi/gomega/types"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	iperf2Port = "20288"
	skipIperf  = false
)

// Bandwidth validation constants
// tc uses SI units: 1 mbit = 1,000,000 bits/s
const (
	// bitsPerMbit is the conversion factor from Mbps to bits/s (SI units)
	bitsPerMbit = 1000 * 1000
	// bandwidthToleranceLow is the lower bound multiplier for bandwidth validation (50%)
	bandwidthToleranceLow = 0.5
	// bandwidthToleranceHigh is the upper bound multiplier for bandwidth validation (150%)
	bandwidthToleranceHigh = 1.5
)

const (
	overlaySubnetV4Cidr = "10.0.0.0/24"
	overlaySubnetV4Gw   = "10.0.0.1"
	lanIP               = "10.0.0.254"
)

const (
	dockerExtNetName       = "kube-ovn-qos"
	networkAttachDefName   = "qos-ovn-vpc-external-network"
	externalSubnetProvider = "qos-ovn-vpc-external-network.kube-system"
)

// QoS rate limits used by the tests. Every two limits which are compared against each other are
// spaced by a factor of 5, so that the measured bandwidth of one limit can never fall into the
// 50%~150% tolerance window of the other one.
const (
	eipLimit         = 10 // EIP QoS limit, also the limit after the policy is restored
	updatedEIPLimit  = 50 // EIP QoS limit after the policy is updated in place (5x of eipLimit)
	priorityEIPLimit = 2  // EIP QoS limit used to verify the priority matching (priority 1)
	specificIPLimit  = 10 // Specific IP matching QoS limit (priority 2, 5x of priorityEIPLimit)
	defaultNicLimit  = 50 // Default NIC QoS limit (priority 3, 5x of specificIPLimit)
)

// Decimal bandwidth limits used by the EIP QoS test. They are applied to two different EIPs on
// the SAME NAT Gateway to verify at once that:
// 1. Multiple EIPs with different QoS don't interfere with each other
// 2. Sub-Mbps decimal bandwidth limiting works correctly
//
// Burst calculation: burst = rate × 1s (in MB)
// burst must be > MTU (1500 bytes) for proper packet handling
//
// NOTE: Avoid extremely low rates (< 0.5 Mbps) because:
// - TCP protocol overhead becomes significant relative to the limit
// - HTB burst mechanism causes short-term rate spikes
// - Measurement accuracy degrades at very low rates
type decimalQoSConfig struct {
	Rate      string  // Rate in Mbps (string for API)
	Burst     string  // Burst in MB (string for API)
	LimitMbps float64 // Rate as float for validation
}

var (
	// decimalQoSLow is applied to the EIP created by the test setup (0.5 Mbps, burst = 62,500 bytes)
	decimalQoSLow = decimalQoSConfig{"0.5", "0.06", 0.5}
	// decimalQoSHigh is applied to an extra EIP on the same NAT Gateway (2.5 Mbps, burst = 312,500 bytes)
	decimalQoSHigh = decimalQoSConfig{"2.5", "0.3", 2.5}
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

	// Try to get existing NAD first using raw Kubernetes API to avoid ExpectNoError
	nad, err := attachNetClient.NetworkAttachmentDefinitionInterface.Get(context.TODO(), externalNetworkName, metav1.GetOptions{})
	if err != nil && k8serrors.IsNotFound(err) {
		// NAD doesn't exist, create it
		attachNet := framework.MakeNetworkAttachmentDefinition(externalNetworkName, framework.KubeOvnNamespace, attachConf)
		nad = attachNetClient.Create(attachNet)
	} else {
		framework.ExpectNoError(err, "getting network attachment definition "+externalNetworkName)
	}
	framework.Logf("Got/Created network attachment definition:\n%s", format.Object(nad, 2))

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
		// Note: NAD cleanup is handled in AfterAll, not here, to allow reuse across tests
	}

	ginkgo.By("Getting config map " + util.VpcNatGatewayConfig)
	_, err := f.ClientSet.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Get(context.Background(), util.VpcNatGatewayConfig, metav1.GetOptions{})
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

type qosParams struct {
	qosVpcName      string
	noQosVpcName    string
	qosSubnetName   string
	noQosSubnetName string
	qosNatGwName    string
	noQosNatGwName  string
	qosEIPName      string
	noQosEIPName    string
	qosFIPName      string
	noQosFIPName    string
	qosPodName      string
	noQosPodName    string
	attachDefName   string
	subnetProvider  string
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

// dumpTcRulesOnNatGw dumps tc qdisc, class, and filter rules on NAT Gateway pod for debugging
// This helps diagnose QoS issues by showing the actual tc configuration
func dumpTcRulesOnNatGw(f *framework.Framework, natgwName, eipIP string) {
	ginkgo.GinkgoHelper()

	natGwPodName := getNatGwPodName(f, natgwName, "")
	framework.Logf("=== Dumping tc rules on NAT GW pod %s for EIP %s ===", natGwPodName, eipIP)

	// Dump egress rules on net1
	commands := []struct {
		desc string
		cmd  string
	}{
		{"Egress qdisc on net1", "tc qdisc show dev net1"},
		{"Egress class on net1", "tc class show dev net1"},
		{"Egress filter on net1", "tc -p filter show dev net1 parent 1:"},
		{"Ingress qdisc on net1", "tc qdisc show dev net1 ingress"},
		{"Ingress REDIRECT filter on net1 (CRITICAL)", "tc -p filter show dev net1 parent ffff:"},
		{"IFB device status", "ip link show ifb-net1 2>/dev/null || echo 'IFB device not found'"},
		{"Ingress class on ifb-net1", "tc class show dev ifb-net1 2>/dev/null || echo 'No HTB on ifb-net1'"},
		{"Ingress filter on ifb-net1", "tc -p filter show dev ifb-net1 parent 1: 2>/dev/null || echo 'No filter on ifb-net1'"},
		{"IFB qdisc stats", "tc -s qdisc show dev ifb-net1 2>/dev/null || echo 'No qdisc on ifb-net1'"},
		{"net1 qdisc stats", "tc -s qdisc show dev net1"},
		// Additional diagnostics for network connectivity
		{"iptables NAT DNAT rules", "iptables-save -t nat | grep -E 'DNAT|SNAT' | head -20"},
		{"conntrack entries for EIP", fmt.Sprintf("conntrack -L 2>/dev/null | grep %s | head -10 || echo 'No conntrack entries'", eipIP)},
		{"net1 IP address", "ip addr show dev net1 | grep inet"},
	}

	for _, c := range commands {
		stdOutput, errOutput, err := framework.ExecShellInPod(context.Background(), f, "kube-system", natGwPodName, c.cmd)
		if err != nil {
			framework.Logf("[%s] Error: %v, stderr: %s", c.desc, err, errOutput)
		} else {
			framework.Logf("[%s]\n%s", c.desc, stdOutput)
		}
	}
	framework.Logf("=== End of tc rules dump ===")
}

func iperf(f *framework.Framework, iperfClientPod *corev1.Pod, iperfServerEIP *apiv1.IptablesEIP) string {
	ginkgo.GinkgoHelper()

	for i := range 20 {
		command := fmt.Sprintf("iperf -e -p %s --reportstyle C -i 1 -c %s -t 10", iperf2Port, iperfServerEIP.Status.IP)
		stdOutput, errOutput, err := framework.ExecShellInPod(context.Background(), f, iperfClientPod.Namespace, iperfClientPod.Name, command)
		framework.Logf("output from exec on client pod %s (eip %s)\n", iperfClientPod.Name, iperfServerEIP.Name)
		if stdOutput != "" && err == nil {
			framework.Logf("output:\n%s", stdOutput)
			return stdOutput
		}
		framework.Logf("exec %s failed err: %v, errOutput: %s, stdOutput: %s, retried %d times.", command, err, errOutput, stdOutput, i)
		time.Sleep(6 * time.Second)
	}
	framework.ExpectNoError(errors.New("iperf failed"))
	return ""
}

func checkQos(f *framework.Framework,
	qosPod, noQosPod *corev1.Pod, qosEIP, noQosEIP *apiv1.IptablesEIP,
	limit int,
) {
	checkQosFloat(f, qosPod, noQosPod, qosEIP, noQosEIP, float64(limit))
}

// checkQosFloat validates QoS with float64 limit (supports decimal Mbps values like 0.5)
//
// Test architecture:
//
//	qosPod/qosEIP: Has QoS policy applied (test target)
//	noQosPod/noQosEIP: No QoS policy (clean endpoint for traffic measurement)
//
// Egress test: qosPod → noQosEIP (traffic exits through qosEIP's NAT GW, QoS limits outbound)
// Ingress test: noQosPod → qosEIP (traffic enters through qosEIP's NAT GW, QoS limits inbound)
func checkQosFloat(f *framework.Framework,
	qosPod, noQosPod *corev1.Pod, qosEIP, noQosEIP *apiv1.IptablesEIP,
	limitMbps float64,
) {
	ginkgo.GinkgoHelper()

	if skipIperf {
		return
	}

	// Bandwidth should be within limit * bandwidthToleranceLow ~ limit * bandwidthToleranceHigh.
	// The unlimited case is intentionally not tested: saturating the link is expensive and flaky,
	// removing a policy is instead verified by falling back to another expected rate limit.

	// Test egress: qosPod → noQosEIP (QoS applied on qosEIP's egress)
	output := iperf(f, qosPod, noQosEIP)
	result := validateRateLimitFloatWithResult(output, limitMbps)
	klog.Info(formatBandwidthSummary(result, "Egress: qosPod -> noQosEIP"))
	framework.ExpectTrue(result.Passed, "expected egress bandwidth to be limited to %.2f~%.2f Mbps, but got %.2f Mbps",
		result.MinExpected, result.MaxExpected, result.BestMatch)

	// Test ingress: noQosPod → qosEIP (QoS applied on qosEIP's ingress)
	output = iperf(f, noQosPod, qosEIP)
	result = validateRateLimitFloatWithResult(output, limitMbps)
	klog.Info(formatBandwidthSummary(result, "Ingress: noQosPod -> qosEIP"))
	framework.ExpectTrue(result.Passed, "expected ingress bandwidth to be limited to %.2f~%.2f Mbps, but got %.2f Mbps",
		result.MinExpected, result.MaxExpected, result.BestMatch)
}

func getNicDefaultQoSPolicy(limit int) apiv1.QoSPolicyBandwidthLimitRules {
	return apiv1.QoSPolicyBandwidthLimitRules{
		apiv1.QoSPolicyBandwidthLimitRule{
			Name:      "net1-ingress",
			Interface: "net1",
			RateMax:   strconv.Itoa(limit),
			BurstMax:  strconv.Itoa(limit),
			Priority:  3,
			Direction: apiv1.QoSDirectionIngress,
		},
		apiv1.QoSPolicyBandwidthLimitRule{
			Name:      "net1-egress",
			Interface: "net1",
			RateMax:   strconv.Itoa(limit),
			BurstMax:  strconv.Itoa(limit),
			Priority:  3,
			Direction: apiv1.QoSDirectionEgress,
		},
	}
}

func getEIPQoSRule(limit int) apiv1.QoSPolicyBandwidthLimitRules {
	return apiv1.QoSPolicyBandwidthLimitRules{
		apiv1.QoSPolicyBandwidthLimitRule{
			Name:      "eip-ingress",
			RateMax:   strconv.Itoa(limit),
			BurstMax:  strconv.Itoa(limit),
			Priority:  1,
			Direction: apiv1.QoSDirectionIngress,
		},
		apiv1.QoSPolicyBandwidthLimitRule{
			Name:      "eip-egress",
			RateMax:   strconv.Itoa(limit),
			BurstMax:  strconv.Itoa(limit),
			Priority:  1,
			Direction: apiv1.QoSDirectionEgress,
		},
	}
}

// getEIPQoSRuleWithDecimal creates QoS rules with decimal rate/burst values
// This is used to test sub-Mbps bandwidth limiting (e.g., 0.5 Mbps = 500 Kbps)
//
// Reference: tc-htb(8) Linux manual page
// https://man7.org/linux/man-pages/man8/tc-htb.8.html
//
// From the NOTES section:
//
//	"Due to Unix timing constraints, the maximum ceil rate is not infinite
//	 and may in fact be quite low. On Intel, there are 100 timer events per
//	 second, the maximum rate is that rate at which 'burst' bytes are sent
//	 each timer tick. From this, the minimum burst size for a specified rate
//	 can be calculated. For i386, a 10mbit rate requires a 12 kilobyte burst
//	 as 100*12kb*8 equals 10mbit."
//
// ┌─────────────────────────────────────────────────────────────────────────────┐
// │ CRITICAL: DO NOT set burst smaller than MTU (1500 bytes)!                   │
// │                                                                             │
// │ The tc-htb manual specifies the MINIMUM burst for timer accuracy, but       │
// │ burst MUST also be larger than MTU for proper packet handling:              │
// │                                                                             │
// │   - If burst < MTU: HTB cannot dequeue a full packet per tick               │
// │   - This causes severe rate limiting issues and TCP stalls                  │
// │   - Example failure: burst=629 bytes for 0.5 Mbps caused near-zero          │
// │     throughput because 629 < 1500 (MTU)                                     │
// │                                                                             │
// │ Safe burst value: rate × 1 second (ensures burst >> MTU for low rates)      │
// └─────────────────────────────────────────────────────────────────────────────┘
//
// Formula:
//
//	burst_bytes = rate_Mbps × 1,000,000 / 8 = rate_Mbps × 125,000 bytes
//	burst_MB    = burst_bytes / 1,048,576 = rate_Mbps × 0.1192
//
// Test case values:
//
//	0.5 Mbps: 0.5 × 125,000 = 62,500 bytes  → burst_MB ≈ 0.06 (>> MTU 1500) ✓
//	2.5 Mbps: 2.5 × 125,000 = 312,500 bytes → burst_MB ≈ 0.30 (>> MTU 1500) ✓
func getEIPQoSRuleWithDecimal(rateMax, burstMax string) apiv1.QoSPolicyBandwidthLimitRules {
	return apiv1.QoSPolicyBandwidthLimitRules{
		apiv1.QoSPolicyBandwidthLimitRule{
			Name:      "eip-ingress",
			RateMax:   rateMax,
			BurstMax:  burstMax,
			Priority:  1,
			Direction: apiv1.QoSDirectionIngress,
		},
		apiv1.QoSPolicyBandwidthLimitRule{
			Name:      "eip-egress",
			RateMax:   rateMax,
			BurstMax:  burstMax,
			Priority:  1,
			Direction: apiv1.QoSDirectionEgress,
		},
	}
}

func getSpecialQoSRule(limit int, ip string) apiv1.QoSPolicyBandwidthLimitRules {
	return apiv1.QoSPolicyBandwidthLimitRules{
		apiv1.QoSPolicyBandwidthLimitRule{
			Name:       "net1-extip-ingress",
			Interface:  "net1",
			RateMax:    strconv.Itoa(limit),
			BurstMax:   strconv.Itoa(limit),
			Priority:   2,
			Direction:  apiv1.QoSDirectionIngress,
			MatchType:  apiv1.QoSMatchTypeIP,
			MatchValue: "src " + ip + "/32",
		},
		apiv1.QoSPolicyBandwidthLimitRule{
			Name:       "net1-extip-egress",
			Interface:  "net1",
			RateMax:    strconv.Itoa(limit),
			BurstMax:   strconv.Itoa(limit),
			Priority:   2,
			Direction:  apiv1.QoSDirectionEgress,
			MatchType:  apiv1.QoSMatchTypeIP,
			MatchValue: "dst " + ip + "/32",
		},
	}
}

// restartNatGwPods deletes the NAT gateway pods and waits until the recreated pods become ready.
func restartNatGwPods(f *framework.Framework, natgwName string) {
	ginkgo.GinkgoHelper()

	podClient := f.PodClientNS(framework.KubeOvnNamespace)
	labels := util.GenNatGwLabels(natgwName)
	selector := metav1.FormatLabelSelector(&metav1.LabelSelector{MatchLabels: labels})

	ginkgo.By("Delete natgw pods for " + natgwName)
	pods, err := f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
	framework.ExpectNoError(err)
	oldUIDs := make(map[types.UID]string, len(pods.Items))
	for _, pod := range pods.Items {
		oldUIDs[pod.UID] = pod.Name
		framework.ExpectNoError(podClient.Delete(pod.Name), "failed to delete natgw pod "+pod.Name)
	}

	// The old pods must be gone before waiting for readiness, otherwise the readiness check could
	// observe the pod which is still terminating and return before the gateway is rebuilt.
	ginkgo.By("Wait for the old natgw pods of " + natgwName + " to disappear")
	gomega.Eventually(func(g gomega.Gomega) {
		pods, err := f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		for _, pod := range pods.Items {
			g.Expect(oldUIDs).NotTo(gomega.HaveKey(pod.UID), "natgw pod %s is still terminating", pod.Name)
		}
	}, 2*time.Minute, 2*time.Second).Should(gomega.Succeed())

	ginkgo.By("Wait for natgw " + natgwName + " pod to be ready after recreation")
	f.VpcNatGatewayClient().WaitGwPodReady(natgwName, "", 2*time.Minute, f.ClientSet)
}

// tcRateMatcher builds a matcher for the rate of a tc class as printed by tc, which uses either
// Mbit or Kbit depending on the value, e.g. 50 -> "rate 50Mbit", 0.5 -> "rate 500Kbit".
func tcRateMatcher(rateMbps float64) gomegatypes.GomegaMatcher {
	return gomega.SatisfyAny(
		gomega.ContainSubstring(fmt.Sprintf("rate %gMbit", rateMbps)),
		gomega.ContainSubstring(fmt.Sprintf("rate %gKbit", rateMbps*1000)),
	)
}

// natGwEgressDev and natGwIngressDev are the devices carrying the HTB classes of the NAT gateway:
// the egress rules are applied on the external nic, the ingress ones on its IFB device.
const (
	natGwEgressDev  = "net1"
	natGwIngressDev = "ifb-net1"
)

// expectTcClassOnNatGwDev asserts that a tc class limited to the given rate does (or does not)
// exist on the given device of the NAT gateway. Checking the tc rules is used instead of an extra
// iperf run whenever the expected bandwidth of a direction would be unlimited or unchanged.
func expectTcClassOnNatGwDev(f *framework.Framework, natgwName, dev string, rateMbps float64, expectExists bool) {
	ginkgo.GinkgoHelper()

	matcher := tcRateMatcher(rateMbps)
	if !expectExists {
		matcher = gomega.Not(matcher)
	}
	gomega.Eventually(func(g gomega.Gomega) {
		podName := getNatGwPodName(f, natgwName, "")
		stdOutput, errOutput, err := framework.ExecShellInPod(context.Background(), f, framework.KubeOvnNamespace, podName, "tc class show dev "+dev)
		g.Expect(err).NotTo(gomega.HaveOccurred(), errOutput)
		g.Expect(stdOutput).To(matcher)
	}, 60*time.Second, 3*time.Second).Should(gomega.Succeed(),
		"expected the tc class limited to %gMbps on %s of natgw %s to exist: %v", rateMbps, dev, natgwName, expectExists)
}

// expectTcRateOnNatGw asserts that the HTB classes on the NAT gateway carry the expected rates in
// both directions (egress on net1, ingress on ifb-net1).
func expectTcRateOnNatGw(f *framework.Framework, natgwName string, ratesMbps ...float64) {
	ginkgo.GinkgoHelper()

	for _, dev := range []string{natGwEgressDev, natGwIngressDev} {
		for _, rate := range ratesMbps {
			expectTcClassOnNatGwDev(f, natgwName, dev, rate, true)
		}
	}
}

// natGwQoSCases covers the NAT gateway scoped QoS:
//   - the policy bound at NAT gateway creation time takes effect (default nic rules, priority 3)
//   - the tc rules are restored after the NAT gateway pod is recreated
//   - switching the NAT gateway to another policy takes effect
//   - priority matching: eip rule (priority 1) > specific ip rule (priority 2) > nic rule (priority 3)
//   - unbinding the eip policy falls back to the NAT gateway policy
//   - unbinding the NAT gateway policy removes its tc classes
//
// The NAT gateway is expected to be created with natgwQoSPolicyName (defaultNicLimit) already bound.
func natGwQoSCases(f *framework.Framework,
	qosPod, noQosPod *corev1.Pod,
	qosEIP, noQosEIP *apiv1.IptablesEIP,
	vpcQosParams *qosParams, natgwQoSPolicyName string,
) {
	ginkgo.GinkgoHelper()

	vpcNatGwClient := f.VpcNatGatewayClient()
	eipClient := f.IptablesEIPClient()
	qosPolicyClient := f.QoSPolicyClient()
	natgwName := vpcQosParams.qosNatGwName

	ginkgo.By("Check qos " + natgwQoSPolicyName + " is limited to " + strconv.Itoa(defaultNicLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, defaultNicLimit)

	// switch to a policy which also matches the peer eip with a higher priority rule
	specificQoSPolicyName := "specific-nic-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + specificQoSPolicyName)
	rules := getNicDefaultQoSPolicy(defaultNicLimit)
	rules = append(rules, getSpecialQoSRule(specificIPLimit, noQosEIP.Status.IP)...)
	specificQoSPolicy := framework.MakeQoSPolicy(specificQoSPolicyName, true, apiv1.QoSBindingTypeNatGw, rules)
	_ = qosPolicyClient.CreateSync(specificQoSPolicy)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up natgw qos policy " + specificQoSPolicyName)
		qosPolicyClient.DeleteSync(specificQoSPolicyName)
	})

	ginkgo.By("Change qos policy of natgw " + natgwName + " to " + specificQoSPolicyName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, specificQoSPolicyName)
	// The QoS policy keeps a finalizer while it is bound, so it must be unbound before it gets
	// deleted by the cleanup registered above (deferred cleanups run in reverse order).
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Unbinding the qos policy from natgw " + natgwName)
		_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, "")
	})

	ginkgo.By("Check qos to match priority 2 is limited to " + strconv.Itoa(specificIPLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, specificIPLimit)

	// eip scoped rules have the highest priority
	eipQoSPolicyName := "eip-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + eipQoSPolicyName)
	eipQoSPolicy := framework.MakeQoSPolicy(eipQoSPolicyName, false, apiv1.QoSBindingTypeEIP, getEIPQoSRule(priorityEIPLimit))
	_ = qosPolicyClient.CreateSync(eipQoSPolicy)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up eip qos policy " + eipQoSPolicyName)
		qosPolicyClient.DeleteSync(eipQoSPolicyName)
	})

	ginkgo.By("Patch eip " + vpcQosParams.qosEIPName + " with qos policy " + eipQoSPolicyName)
	_ = eipClient.PatchQoSPolicySync(vpcQosParams.qosEIPName, eipQoSPolicyName)

	ginkgo.By("Check qos to match priority 1 is limited to " + strconv.Itoa(priorityEIPLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, priorityEIPLimit)

	ginkgo.By("Remove qos policy " + eipQoSPolicyName + " from eip " + vpcQosParams.qosEIPName)
	_ = eipClient.PatchQoSPolicySync(vpcQosParams.qosEIPName, "")

	ginkgo.By("Check qos falls back to priority 2 and is limited to " + strconv.Itoa(specificIPLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, specificIPLimit)

	restartNatGwPods(f, natgwName)

	ginkgo.By("Check tc rules of qos " + specificQoSPolicyName + " are restored after natgw pod recreation")
	expectTcRateOnNatGw(f, natgwName, defaultNicLimit, specificIPLimit)

	ginkgo.By("Remove qos policy " + specificQoSPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, "")

	// Unbinding must remove all the tc classes of the policy. Verifying it on the tc rules is both
	// cheaper and stricter than measuring an unlimited bandwidth.
	ginkgo.By("Check tc rules of qos " + specificQoSPolicyName + " are removed from natgw " + natgwName)
	for _, dev := range []string{natGwEgressDev, natGwIngressDev} {
		expectTcClassOnNatGwDev(f, natgwName, dev, defaultNicLimit, false)
		expectTcClassOnNatGwDev(f, natgwName, dev, specificIPLimit, false)
	}
}

// updateEIPQoSPolicyRules updates the bandwidth limit rules of a bound EIP QoS policy in place and
// waits until the policy status reflects the new rules, i.e. until they have been applied on the
// NAT gateway.
func updateEIPQoSPolicyRules(f *framework.Framework, qosPolicyName string, rules apiv1.QoSPolicyBandwidthLimitRules) {
	ginkgo.GinkgoHelper()

	qosPolicyClient := f.QoSPolicyClient()

	qosPolicy := qosPolicyClient.Get(qosPolicyName)
	modifiedQoSPolicy := qosPolicy.DeepCopy()
	modifiedQoSPolicy.Spec.BandwidthLimitRules = rules
	qosPolicyClient.Patch(qosPolicy, modifiedQoSPolicy)
	framework.ExpectTrue(qosPolicyClient.WaitToQoSReady(qosPolicyName),
		"qos policy %s should apply its new bandwidth limit rules", qosPolicyName)
}

// updateEIPQoSPolicyRate updates the rate limit of the ingress and egress rules of a bound EIP QoS
// policy in place.
func updateEIPQoSPolicyRate(f *framework.Framework, qosPolicyName string, limit int) {
	ginkgo.GinkgoHelper()

	ginkgo.By("Update qos policy " + qosPolicyName + " with rate limit " + strconv.Itoa(limit) + "Mbps")
	updateEIPQoSPolicyRules(f, qosPolicyName, getEIPQoSRule(limit))
}

// eipQoSCases covers the EIP scoped QoS:
//   - the policy bound at EIP creation time takes effect
//   - hot updating the rules of a bound policy takes effect, both when the rate is raised from
//     eipLimit to updatedEIPLimit and when it is restored to eipLimit
//   - hot updating the rule set of a bound policy takes effect: deleting a rule removes the tc
//     class of that direction only, adding one creates it again, and renaming a rule (a deletion
//     and an addition within a single update) keeps it
//   - the tc rules are restored after the NAT gateway pod is recreated
//   - switching the EIP to another policy takes effect (rate lowered to a decimal, sub-Mbps value)
//   - multiple EIPs on the SAME NAT gateway with different (decimal) limits don't interfere, both
//     while they are bound and when one of them is unbound
//
// The EIP is expected to be created with eipQoSPolicyName (eipLimit) already bound.
func eipQoSCases(f *framework.Framework,
	qosPod, noQosPod *corev1.Pod,
	qosEIP, noQosEIP *apiv1.IptablesEIP,
	vpcQosParams *qosParams, eipQoSPolicyName string,
) {
	ginkgo.GinkgoHelper()

	podClient := f.PodClient()
	eipClient := f.IptablesEIPClient()
	fipClient := f.IptablesFIPClient()
	qosPolicyClient := f.QoSPolicyClient()
	natgwName := vpcQosParams.qosNatGwName

	ginkgo.By("Check qos " + eipQoSPolicyName + " is limited to " + strconv.Itoa(eipLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, eipLimit)

	// hot update the bound policy: raise the rate limit and restore it afterwards
	updateEIPQoSPolicyRate(f, eipQoSPolicyName, updatedEIPLimit)

	ginkgo.By("Check qos " + eipQoSPolicyName + " is changed to " + strconv.Itoa(updatedEIPLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, updatedEIPLimit)

	updateEIPQoSPolicyRate(f, eipQoSPolicyName, eipLimit)

	ginkgo.By("Check qos " + eipQoSPolicyName + " is restored to " + strconv.Itoa(eipLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, eipLimit)

	// Hot update the rule set (not only the rate) of the bound policy. The effect is verified on
	// the tc rules of the NAT gateway: measuring it would require saturating the link, since a
	// deleted rule means "no limit" for that direction.
	rules := getEIPQoSRule(eipLimit)
	ingressRule, egressRule := rules[0], rules[1]

	ginkgo.By("Delete the egress rule of qos policy " + eipQoSPolicyName)
	updateEIPQoSPolicyRules(f, eipQoSPolicyName, apiv1.QoSPolicyBandwidthLimitRules{ingressRule})
	expectTcClassOnNatGwDev(f, natgwName, natGwEgressDev, eipLimit, false)
	expectTcClassOnNatGwDev(f, natgwName, natGwIngressDev, eipLimit, true)

	ginkgo.By("Add an egress rule to qos policy " + eipQoSPolicyName)
	newEgressRule := *egressRule.DeepCopy()
	newEgressRule.Name = egressRule.Name + "-v2"
	updateEIPQoSPolicyRules(f, eipQoSPolicyName, apiv1.QoSPolicyBandwidthLimitRules{ingressRule, newEgressRule})
	expectTcRateOnNatGw(f, natgwName, eipLimit)

	// Renaming a rule is a deletion and an addition within a single update: the deletion has to be
	// applied first, otherwise the tc class added for the new rule would be removed right away.
	ginkgo.By("Rename the egress rule of qos policy " + eipQoSPolicyName)
	renamedEgressRule := *egressRule.DeepCopy()
	renamedEgressRule.Name = egressRule.Name + "-v3"
	updateEIPQoSPolicyRules(f, eipQoSPolicyName, apiv1.QoSPolicyBandwidthLimitRules{ingressRule, renamedEgressRule})
	expectTcRateOnNatGw(f, natgwName, eipLimit)

	// Create an extra pod/eip/fip on the SAME natgw with a different decimal rate limit, so that
	// the isolation between the QoS policies of both EIPs can be verified.
	randomSuffix := framework.RandomSuffix()
	extraPodName := "isolation-pod-" + randomSuffix
	extraEIPName := "isolation-eip-" + randomSuffix
	extraFIPName := "isolation-fip-" + randomSuffix

	ginkgo.By("Creating pod " + extraPodName + " for the " + decimalQoSHigh.Rate + "Mbps test")
	annotations := map[string]string{util.LogicalSwitchAnnotation: vpcQosParams.qosSubnetName}
	iperfServerCmd := []string{"iperf", "-s", "-i", "1", "-p", iperf2Port}
	extraPod := framework.MakePod(f.Namespace.Name, extraPodName, nil, annotations, framework.AgnhostImage, iperfServerCmd, nil)
	extraPod = podClient.CreateSync(extraPod)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up pod " + extraPodName)
		podClient.DeleteSync(extraPodName)
	})

	extraQoSPolicyName := "isolation-qos-policy-" + randomSuffix
	ginkgo.By("Creating qos policy " + extraQoSPolicyName + " with " + decimalQoSHigh.Rate + "Mbps")
	extraRules := getEIPQoSRuleWithDecimal(decimalQoSHigh.Rate, decimalQoSHigh.Burst)
	extraQoSPolicy := framework.MakeQoSPolicy(extraQoSPolicyName, false, apiv1.QoSBindingTypeEIP, extraRules)
	_ = qosPolicyClient.CreateSync(extraQoSPolicy)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up qos policy " + extraQoSPolicyName)
		qosPolicyClient.DeleteSync(extraQoSPolicyName)
	})

	ginkgo.By("Creating eip " + extraEIPName + " on natgw " + natgwName + " with qos policy " + extraQoSPolicyName)
	extraEIP := framework.MakeIptablesEIP(extraEIPName, "", "", "", natgwName, vpcQosParams.attachDefName, extraQoSPolicyName)
	_ = eipClient.CreateSync(extraEIP)
	extraEIP = waitForIptablesEIPReady(eipClient, extraEIPName, 60*time.Second)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up eip " + extraEIPName)
		eipClient.DeleteSync(extraEIPName)
	})

	ginkgo.By("Creating fip " + extraFIPName + " for pod " + extraPodName + " -> eip " + extraEIPName)
	extraFIP := framework.MakeIptablesFIPRule(extraFIPName, extraEIPName, extraPod.Status.PodIP)
	_ = fipClient.CreateSync(extraFIP)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up fip " + extraFIPName)
		fipClient.DeleteSync(extraFIPName)
	})

	// switch the eip to another policy, using a sub-Mbps decimal rate
	newQoSPolicyName := "new-eip-qos-policy-" + randomSuffix
	ginkgo.By("Creating qos policy " + newQoSPolicyName + " with " + decimalQoSLow.Rate + "Mbps")
	newRules := getEIPQoSRuleWithDecimal(decimalQoSLow.Rate, decimalQoSLow.Burst)
	newQoSPolicy := framework.MakeQoSPolicy(newQoSPolicyName, false, apiv1.QoSBindingTypeEIP, newRules)
	_ = qosPolicyClient.CreateSync(newQoSPolicy)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up qos policy " + newQoSPolicyName)
		qosPolicyClient.DeleteSync(newQoSPolicyName)
	})

	ginkgo.By("Change qos policy of eip " + vpcQosParams.qosEIPName + " to " + newQoSPolicyName)
	_ = eipClient.PatchQoSPolicySync(vpcQosParams.qosEIPName, newQoSPolicyName)
	// The QoS policy keeps a finalizer while it is bound, so it must be unbound before it gets
	// deleted by the cleanup registered above (deferred cleanups run in reverse order).
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Unbinding the qos policy from eip " + vpcQosParams.qosEIPName)
		_ = eipClient.PatchQoSPolicySync(vpcQosParams.qosEIPName, "")
	})

	ginkgo.By("Dumping tc rules on natgw " + natgwName)
	dumpTcRulesOnNatGw(f, natgwName, qosEIP.Status.IP)

	ginkgo.By("Check qos " + newQoSPolicyName + " is limited to " + decimalQoSLow.Rate + "Mbps")
	checkQosFloat(f, qosPod, noQosPod, qosEIP, noQosEIP, decimalQoSLow.LimitMbps)

	ginkgo.By("Check qos " + extraQoSPolicyName + " of eip " + extraEIPName + " is limited to " + decimalQoSHigh.Rate + "Mbps")
	checkQosFloat(f, extraPod, noQosPod, extraEIP, noQosEIP, decimalQoSHigh.LimitMbps)

	restartNatGwPods(f, natgwName)

	ginkgo.By("Check tc rules of the eip qos policies are restored after natgw pod recreation")
	expectTcRateOnNatGw(f, natgwName, decimalQoSLow.LimitMbps, decimalQoSHigh.LimitMbps)

	ginkgo.By("Remove qos policy " + newQoSPolicyName + " from eip " + vpcQosParams.qosEIPName)
	_ = eipClient.PatchQoSPolicySync(vpcQosParams.qosEIPName, "")

	// Unbinding must remove the tc classes of this eip only, the ones of the other eip on the same
	// NAT gateway must be left untouched.
	ginkgo.By("Check tc rules of qos " + newQoSPolicyName + " are removed, the ones of " + extraQoSPolicyName + " are kept")
	for _, dev := range []string{natGwEgressDev, natGwIngressDev} {
		expectTcClassOnNatGwDev(f, natgwName, dev, decimalQoSLow.LimitMbps, false)
		expectTcClassOnNatGwDev(f, natgwName, dev, decimalQoSHigh.LimitMbps, true)
	}
}

// createQoSMarkedForDeletionWhileBound creates a QoS policy of the given binding type, waits for
// its KubeOVN controller finalizer, runs bind to attach it to a resource, then deletes it while
// still bound and asserts it stays in Terminating. It returns the policy name so the caller can
// exercise the release trigger (deleting or unbinding the referencing resource).
func createQoSMarkedForDeletionWhileBound(
	f *framework.Framework,
	shared bool,
	bindingType apiv1.QoSPolicyBindingType,
	rules apiv1.QoSPolicyBandwidthLimitRules,
	bind func(qosName string),
) (qosName string) {
	ginkgo.GinkgoHelper()

	qosPolicyClient := f.QoSPolicyClient()

	qosName = "qos-life-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + qosName)
	qos := framework.MakeQoSPolicy(qosName, shared, bindingType, rules)
	_ = qosPolicyClient.CreateSync(qos)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up qos policy " + qosName)
		qosPolicyClient.DeleteSync(qosName)
	})

	// A newly created policy must carry the KubeOVN controller finalizer, otherwise deletion
	// would not be blocked while still referenced and there would be nothing to regress on.
	ginkgo.By("Verifying qos policy " + qosName + " has the controller finalizer")
	gomega.Eventually(func(g gomega.Gomega) {
		p, err := qosPolicyClient.QoSPolicyInterface.Get(context.TODO(), qosName, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(p.Finalizers).To(gomega.ContainElement(util.KubeOVNControllerFinalizer))
	}, 10*time.Second, 1*time.Second).Should(gomega.Succeed())

	bind(qosName)

	ginkgo.By("Marking qos policy " + qosName + " for deletion while still bound")
	qosPolicyClient.Delete(qosName)

	// Wait for the policy to enter Terminating, then assert it stays there while still
	// referenced. The initial Eventually avoids a flake if the apiserver update lags.
	stillTerminating := func(g gomega.Gomega) {
		p, err := qosPolicyClient.QoSPolicyInterface.Get(context.TODO(), qosName, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(p.DeletionTimestamp.IsZero()).To(gomega.BeFalse())
	}
	ginkgo.By("Waiting for qos policy " + qosName + " to enter Terminating")
	gomega.Eventually(stillTerminating, 10*time.Second, 1*time.Second).Should(gomega.Succeed())
	ginkgo.By("Verifying qos policy " + qosName + " stays in Terminating while still referenced")
	gomega.Consistently(stillTerminating, 5*time.Second, 1*time.Second).Should(gomega.Succeed())

	return qosName
}

// setupEIPBoundQoSMarkedForDeletion binds a fresh EIP (no FIP, so it can be deleted freely) to a
// new EIP-type QoS policy marked for deletion. It returns the EIP and QoS names.
func setupEIPBoundQoSMarkedForDeletion(f *framework.Framework, vpcQosParams *qosParams) (eipName, qosName string) {
	ginkgo.GinkgoHelper()

	eipClient := f.IptablesEIPClient()

	eipName = "qos-life-eip-" + framework.RandomSuffix()
	ginkgo.By("Creating dedicated eip " + eipName)
	eip := framework.MakeIptablesEIP(eipName, "", "", "", vpcQosParams.qosNatGwName, vpcQosParams.attachDefName, "")
	_ = eipClient.CreateSync(eip)
	waitForIptablesEIPReady(eipClient, eipName, 60*time.Second)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up eip " + eipName)
		eipClient.DeleteSync(eipName)
	})

	qosName = createQoSMarkedForDeletionWhileBound(f, false, apiv1.QoSBindingTypeEIP, getEIPQoSRule(eipLimit), func(qos string) {
		ginkgo.By("Binding eip " + eipName + " to qos policy " + qos)
		_ = eipClient.PatchQoSPolicySync(eipName, qos)
	})
	return eipName, qosName
}

// setupNatGwBoundQoSMarkedForDeletion binds the given NAT gateway to a new NatGw-type QoS policy
// marked for deletion. It returns the QoS name.
func setupNatGwBoundQoSMarkedForDeletion(f *framework.Framework, natgwName string) (qosName string) {
	ginkgo.GinkgoHelper()

	natgwClient := f.VpcNatGatewayClient()
	// A NatGw-bound QoS policy must be shared (see validateQosPolicy).
	return createQoSMarkedForDeletionWhileBound(f, true, apiv1.QoSBindingTypeNatGw, getNicDefaultQoSPolicy(defaultNicLimit), func(qos string) {
		ginkgo.By("Binding natgw " + natgwName + " to qos policy " + qos)
		_ = natgwClient.PatchQoSPolicySync(natgwName, qos)
	})
}

// parseBandwidthFromIperfOutput extracts bandwidth values from iperf CSV output
// Returns slice of bandwidth values in bits per second
func parseBandwidthFromIperfOutput(text string) []float64 {
	var bandwidths []float64
	lines := strings.SplitSeq(text, "\n")
	for line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		number, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil {
			continue
		}
		bandwidths = append(bandwidths, float64(number))
	}
	return bandwidths
}

// bandwidthValidationResult holds the result of bandwidth validation for logging
type bandwidthValidationResult struct {
	Passed       bool
	LimitMbps    float64   // Configured QoS limit in Mbps
	MeasuredMbps []float64 // All measured bandwidth values in Mbps
	MinExpected  float64   // Expected minimum in Mbps
	MaxExpected  float64   // Expected maximum in Mbps
	BestMatch    float64   // The bandwidth value that matched (or closest), in Mbps
}

// formatBandwidthSummary creates a human-readable summary of bandwidth test results
func formatBandwidthSummary(result bandwidthValidationResult, direction string) string {
	var sb strings.Builder
	sb.WriteString("\n╔═══════════════════════════════════════════════════════════════════════════════╗\n")
	fmt.Fprintf(&sb, "║  QoS Bandwidth Test Summary (%s)\n", direction)
	sb.WriteString("╠═══════════════════════════════════════════════════════════════════════════════╣\n")
	fmt.Fprintf(&sb, "║  QoS Limit:         %.2f Mbps\n", result.LimitMbps)

	fmt.Fprintf(&sb, "║  Expected Range:    %.2f ~ %.2f Mbps (%.0f%% ~ %.0f%% of limit)\n",
		result.MinExpected, result.MaxExpected,
		bandwidthToleranceLow*100, bandwidthToleranceHigh*100)

	sb.WriteString("║  Measured Values:   ")
	for i, bw := range result.MeasuredMbps {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%.2f", bw)
	}
	sb.WriteString(" Mbps\n")

	if result.Passed {
		fmt.Fprintf(&sb, "║  Best Match:        %.2f Mbps ✓ PASS\n", result.BestMatch)
	} else {
		fmt.Fprintf(&sb, "║  Best Match:        %.2f Mbps ✗ FAIL\n", result.BestMatch)
	}

	sb.WriteString("╚═══════════════════════════════════════════════════════════════════════════════╝\n")
	return sb.String()
}

// validateRateLimitFloatWithResult validates bandwidth and returns detailed result for logging
func validateRateLimitFloatWithResult(text string, limitMbps float64) bandwidthValidationResult {
	// Allow wide tolerance to account for:
	// - HTB overhead and scheduling variance
	// - TCP protocol overhead
	// - iperf measurement variance
	maxValue := limitMbps * bitsPerMbit * bandwidthToleranceHigh
	minValue := limitMbps * bitsPerMbit * bandwidthToleranceLow

	result := bandwidthValidationResult{
		Passed:      false,
		LimitMbps:   limitMbps,
		MinExpected: limitMbps * bandwidthToleranceLow,
		MaxExpected: limitMbps * bandwidthToleranceHigh,
	}

	bandwidths := parseBandwidthFromIperfOutput(text)
	for _, bw := range bandwidths {
		result.MeasuredMbps = append(result.MeasuredMbps, bw/bitsPerMbit)
	}

	var bestMatch float64
	var bestDistance float64 = -1
	for _, bw := range bandwidths {
		if bw >= minValue && bw <= maxValue {
			result.Passed = true
			result.BestMatch = bw / bitsPerMbit
			return result
		}
		// Track closest value for failure reporting
		target := (minValue + maxValue) / 2
		distance := bw - target
		if distance < 0 {
			distance = -distance
		}
		if bestDistance < 0 || distance < bestDistance {
			bestDistance = distance
			bestMatch = bw
		}
	}
	result.BestMatch = bestMatch / bitsPerMbit
	return result
}

// setupQosNatGwEnvironment creates only the VPC, the overlay subnet and the NAT gateway where the
// QoS policies are applied. It is used by the tests which don't need to send any traffic.
func setupQosNatGwEnvironment(
	f *framework.Framework,
	dockerExtNetNetwork *dockernetwork.Inspect,
	vpcQosParams *qosParams,
	net1NicName string,
	natgwQoSPolicy string,
) {
	ginkgo.GinkgoHelper()

	setupVpcNatGwTestEnvironment(
		f, dockerExtNetNetwork, f.NetworkAttachmentDefinitionClientNS(framework.KubeOvnNamespace),
		f.SubnetClient(), f.VpcClient(), f.VpcNatGatewayClient(),
		vpcQosParams.qosVpcName, vpcQosParams.qosSubnetName, vpcQosParams.qosNatGwName,
		natgwQoSPolicy, overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
		dockerExtNetName, vpcQosParams.attachDefName, net1NicName,
		vpcQosParams.subnetProvider,
		true,
		nil,
		"",
		0,
	)
}

// setupQosTestResources creates two VPCs with NAT Gateways, EIPs, FIPs and pods:
//
//   - qosVpc: The test target where QoS policies will be applied
//   - noQosVpc: Clean endpoint without any QoS (used as traffic source/destination)
//
// natgwQoSPolicy and eipQoSPolicy are bound to the qosNatGw resp. the qosEIP at creation time,
// both may be empty to create the resources without any QoS policy.
//
// This setup allows testing qosEIP's ingress/egress QoS in isolation:
//
//	Egress test:  qosPod → noQosEIP (measures qosEIP's egress limiting)
//	Ingress test: noQosPod → qosEIP (measures qosEIP's ingress limiting)
//
// Returns: (qosPod, noQosPod, qosEIP, noQosEIP)
func setupQosTestResources(
	f *framework.Framework,
	dockerExtNetNetwork *dockernetwork.Inspect,
	vpcQosParams *qosParams,
	net1NicName string,
	natgwQoSPolicy string,
	eipQoSPolicy string,
) (*corev1.Pod, *corev1.Pod, *apiv1.IptablesEIP, *apiv1.IptablesEIP) {
	ginkgo.GinkgoHelper()

	// Derive clients from framework
	attachNetClient := f.NetworkAttachmentDefinitionClientNS(framework.KubeOvnNamespace)
	subnetClient := f.SubnetClient()
	vpcClient := f.VpcClient()
	vpcNatGwClient := f.VpcNatGatewayClient()
	podClient := f.PodClient()
	iptablesEIPClient := f.IptablesEIPClient()
	iptablesFIPClient := f.IptablesFIPClient()

	iperfServerCmd := []string{"iperf", "-s", "-i", "1", "-p", iperf2Port}

	// Create qosVpc + qosNatGw (test target where QoS will be applied)
	setupVpcNatGwTestEnvironment(
		f, dockerExtNetNetwork, attachNetClient,
		subnetClient, vpcClient, vpcNatGwClient,
		vpcQosParams.qosVpcName, vpcQosParams.qosSubnetName, vpcQosParams.qosNatGwName,
		natgwQoSPolicy, overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
		dockerExtNetName, vpcQosParams.attachDefName, net1NicName,
		vpcQosParams.subnetProvider,
		true,
		nil,
		"",
		0,
	)

	// Create noQosVpc + noQosNatGw (clean endpoint without QoS)
	setupVpcNatGwTestEnvironment(
		f, dockerExtNetNetwork, attachNetClient,
		subnetClient, vpcClient, vpcNatGwClient,
		vpcQosParams.noQosVpcName, vpcQosParams.noQosSubnetName, vpcQosParams.noQosNatGwName,
		"", overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
		dockerExtNetName, vpcQosParams.attachDefName, net1NicName,
		vpcQosParams.subnetProvider,
		true,
		nil,
		"",
		0,
	)

	// Create qosPod (traffic source/destination in qosVpc)
	annotations1 := map[string]string{
		util.LogicalSwitchAnnotation: vpcQosParams.qosSubnetName,
	}
	ginkgo.By("Creating pod " + vpcQosParams.qosPodName)
	qosPod := framework.MakePod(f.Namespace.Name, vpcQosParams.qosPodName, nil, annotations1, framework.AgnhostImage, iperfServerCmd, nil)
	qosPod = podClient.CreateSync(qosPod)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up pod " + vpcQosParams.qosPodName)
		podClient.DeleteSync(vpcQosParams.qosPodName)
	})

	// Create qosEIP (test target where QoS policies will be applied)
	ginkgo.By("Creating eip " + vpcQosParams.qosEIPName)
	qosEIP := framework.MakeIptablesEIP(vpcQosParams.qosEIPName, "", "", "", vpcQosParams.qosNatGwName, vpcQosParams.attachDefName, eipQoSPolicy)
	_ = iptablesEIPClient.CreateSync(qosEIP)
	qosEIP = waitForIptablesEIPReady(iptablesEIPClient, vpcQosParams.qosEIPName, 60*time.Second)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up eip " + vpcQosParams.qosEIPName)
		iptablesEIPClient.DeleteSync(vpcQosParams.qosEIPName)
	})

	// Create qosFIP (maps qosPod to qosEIP)
	ginkgo.By("Creating fip " + vpcQosParams.qosFIPName)
	qosFIP := framework.MakeIptablesFIPRule(vpcQosParams.qosFIPName, vpcQosParams.qosEIPName, qosPod.Status.PodIP)
	_ = iptablesFIPClient.CreateSync(qosFIP)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up fip " + vpcQosParams.qosFIPName)
		iptablesFIPClient.DeleteSync(vpcQosParams.qosFIPName)
	})

	// Create noQosPod (clean endpoint without QoS)
	annotations2 := map[string]string{
		util.LogicalSwitchAnnotation: vpcQosParams.noQosSubnetName,
	}
	ginkgo.By("Creating pod " + vpcQosParams.noQosPodName)
	noQosPod := framework.MakePod(f.Namespace.Name, vpcQosParams.noQosPodName, nil, annotations2, framework.AgnhostImage, iperfServerCmd, nil)
	noQosPod = podClient.CreateSync(noQosPod)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up pod " + vpcQosParams.noQosPodName)
		podClient.DeleteSync(vpcQosParams.noQosPodName)
	})

	// Create noQosEIP (clean endpoint without QoS)
	ginkgo.By("Creating eip " + vpcQosParams.noQosEIPName)
	noQosEIP := framework.MakeIptablesEIP(vpcQosParams.noQosEIPName, "", "", "", vpcQosParams.noQosNatGwName, vpcQosParams.attachDefName, "")
	_ = iptablesEIPClient.CreateSync(noQosEIP)
	noQosEIP = waitForIptablesEIPReady(iptablesEIPClient, vpcQosParams.noQosEIPName, 60*time.Second)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up eip " + vpcQosParams.noQosEIPName)
		iptablesEIPClient.DeleteSync(vpcQosParams.noQosEIPName)
	})

	// Create noQosFIP (maps noQosPod to noQosEIP)
	ginkgo.By("Creating fip " + vpcQosParams.noQosFIPName)
	noQosFIP := framework.MakeIptablesFIPRule(vpcQosParams.noQosFIPName, vpcQosParams.noQosEIPName, noQosPod.Status.PodIP)
	_ = iptablesFIPClient.CreateSync(noQosFIP)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up fip " + vpcQosParams.noQosFIPName)
		iptablesFIPClient.DeleteSync(vpcQosParams.noQosFIPName)
	})

	// Silence unused variable warnings for FIPs (used only for NAT mapping)
	_ = qosFIP
	_ = noQosFIP

	return qosPod, noQosPod, qosEIP, noQosEIP
}

var _ = framework.OrderedDescribe("[group:qos-policy]", func() {
	f := framework.NewDefaultFramework("qos-policy")

	var skip bool
	var cs clientset.Interface
	var attachNetClient *framework.NetworkAttachmentDefinitionClient
	var clusterName string
	var subnetClient *framework.SubnetClient

	var net1NicName string

	// docker network
	var dockerExtNetNetwork *dockernetwork.Inspect

	var vpcQosParams *qosParams

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

		// Initialize only the clients needed at the OrderedDescribe level
		// Other clients are derived from f within helper functions
		attachNetClient = f.NetworkAttachmentDefinitionClientNS(framework.KubeOvnNamespace)
		subnetClient = f.SubnetClient()

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

		ginkgo.By("Ensuring docker network " + dockerExtNetName + " exists")
		network, err := docker.NetworkCreate(dockerExtNetName, true, true)
		framework.ExpectNoError(err, "creating docker network "+dockerExtNetName)
		dockerExtNetNetwork = network

		ginkgo.By("Getting kind nodes")
		nodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")
		framework.ExpectNotEmpty(nodes)

		ginkgo.By("Connecting nodes to the docker network")
		err = kind.NetworkConnect(dockerExtNetNetwork.ID, nodes)
		framework.ExpectNoError(err, "connecting nodes to network "+dockerExtNetName)

		ginkgo.By("Getting node links that belong to the docker network")
		nodes, err = kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")

		ginkgo.By("Validating node links")
		gomega.Eventually(func() error {
			network1, err := docker.NetworkInspect(dockerExtNetName)
			if err != nil {
				return fmt.Errorf("failed to inspect docker network %s: %w", dockerExtNetName, err)
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
			f, dockerExtNetNetwork, attachNetClient,
			subnetClient, networkAttachDefName, net1NicName, externalSubnetProvider, dockerExtNetName)

		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up shared macvlan underlay subnet " + networkAttachDefName)
			subnetClient.DeleteSync(networkAttachDefName)
			ginkgo.By("Cleaning up shared nad " + networkAttachDefName)
			attachNetClient.Delete(networkAttachDefName)

			// Clean up docker network infrastructure after all resources are deleted
			ginkgo.By("Getting nodes")
			nodes, err := kind.ListNodes(clusterName, "")
			framework.ExpectNoError(err, "getting nodes in cluster")

			if dockerExtNetNetwork != nil {
				ginkgo.By("Disconnecting nodes from the docker network")
				err = kind.NetworkDisconnect(dockerExtNetNetwork.ID, nodes)
				framework.ExpectNoError(err, "disconnecting nodes from network "+dockerExtNetName)
			}
		})
	})

	ginkgo.BeforeEach(func() {
		// Create test-specific resource names
		// qos*: Resources with QoS policy applied (test target)
		// noQos*: Resources without QoS policy (clean endpoint for traffic testing)
		randomSuffix := framework.RandomSuffix()
		vpcQosParams = &qosParams{
			qosVpcName:      "qos-vpc-" + randomSuffix,
			noQosVpcName:    "noqos-vpc-" + randomSuffix,
			qosSubnetName:   "qos-subnet-" + randomSuffix,
			noQosSubnetName: "noqos-subnet-" + randomSuffix,
			qosNatGwName:    "qos-gw-" + randomSuffix,
			noQosNatGwName:  "noqos-gw-" + randomSuffix,
			qosEIPName:      "qos-eip-" + randomSuffix,
			noQosEIPName:    "noqos-eip-" + randomSuffix,
			qosFIPName:      "qos-fip-" + randomSuffix,
			noQosFIPName:    "noqos-fip-" + randomSuffix,
			qosPodName:      "qos-pod-" + randomSuffix,
			noQosPodName:    "noqos-pod-" + randomSuffix,
			// Use the shared attachDefName
			attachDefName:  networkAttachDefName,
			subnetProvider: externalSubnetProvider,
		}
	})

	framework.ConformanceIt("eip qos policy finalizer is released after the bound eip is deleted", func() {
		// Regression: deleting an EIP without first unbinding its QoS policy must still let
		// the QoS policy (already marked for deletion) drop its finalizer.
		setupQosNatGwEnvironment(f, dockerExtNetNetwork, vpcQosParams, net1NicName, "")

		eipClient := f.IptablesEIPClient()
		qosPolicyClient := f.QoSPolicyClient()
		eipName, qosName := setupEIPBoundQoSMarkedForDeletion(f, vpcQosParams)

		ginkgo.By("Deleting eip " + eipName + " without unbinding qos policy " + qosName)
		eipClient.DeleteSync(eipName)

		ginkgo.By("Expecting qos policy " + qosName + " to be cleaned up after the eip is deleted")
		gomega.Expect(qosPolicyClient.WaitToDisappear(qosName, 2*time.Second, 2*time.Minute)).To(gomega.Succeed(),
			"qos policy should drop its finalizer once the bound eip is deleted")
	})

	framework.ConformanceIt("eip qos policy finalizer is released after the qos policy is unbound", func() {
		// Regression: unbinding the QoS policy from an EIP must re-trigger the QoS reconcile
		// so a policy already marked for deletion can drop its finalizer.
		setupQosNatGwEnvironment(f, dockerExtNetNetwork, vpcQosParams, net1NicName, "")

		eipClient := f.IptablesEIPClient()
		qosPolicyClient := f.QoSPolicyClient()
		eipName, qosName := setupEIPBoundQoSMarkedForDeletion(f, vpcQosParams)

		ginkgo.By("Unbinding qos policy " + qosName + " from eip " + eipName)
		_ = eipClient.PatchQoSPolicySync(eipName, "")

		ginkgo.By("Expecting qos policy " + qosName + " to be cleaned up after being unbound")
		gomega.Expect(qosPolicyClient.WaitToDisappear(qosName, 2*time.Second, 2*time.Minute)).To(gomega.Succeed(),
			"qos policy should drop its finalizer once it is unbound from the eip")
	})

	framework.ConformanceIt("natgw qos policy finalizer is released after the qos policy is unbound", func() {
		// Regression: unbinding a NatGw-level QoS must re-trigger the QoS reconcile (keyed on the
		// QoSLabel) so a policy already marked for deletion can drop its finalizer.
		setupQosNatGwEnvironment(f, dockerExtNetNetwork, vpcQosParams, net1NicName, "")

		natgwClient := f.VpcNatGatewayClient()
		qosPolicyClient := f.QoSPolicyClient()
		qosName := setupNatGwBoundQoSMarkedForDeletion(f, vpcQosParams.qosNatGwName)

		ginkgo.By("Unbinding qos policy " + qosName + " from natgw " + vpcQosParams.qosNatGwName)
		_ = natgwClient.PatchQoSPolicySync(vpcQosParams.qosNatGwName, "")

		ginkgo.By("Expecting qos policy " + qosName + " to be cleaned up after being unbound")
		gomega.Expect(qosPolicyClient.WaitToDisappear(qosName, 2*time.Second, 2*time.Minute)).To(gomega.Succeed(),
			"qos policy should drop its finalizer once it is unbound from the natgw")
	})

	framework.ConformanceIt("natgw qos", func() {
		qosPolicyClient := f.QoSPolicyClient()

		// bind the qos policy to the natgw at creation time
		natgwQoSPolicyName := "default-nic-qos-policy-" + framework.RandomSuffix()
		ginkgo.By("Creating qos policy " + natgwQoSPolicyName)
		natgwQoSPolicy := framework.MakeQoSPolicy(natgwQoSPolicyName, true, apiv1.QoSBindingTypeNatGw, getNicDefaultQoSPolicy(defaultNicLimit))
		_ = qosPolicyClient.CreateSync(natgwQoSPolicy)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up natgw qos policy " + natgwQoSPolicyName)
			qosPolicyClient.DeleteSync(natgwQoSPolicyName)
		})

		qosPod, noQosPod, qosEIP, noQosEIP := setupQosTestResources(
			f, dockerExtNetNetwork, vpcQosParams, net1NicName, natgwQoSPolicyName, "",
		)

		natGwQoSCases(f, qosPod, noQosPod, qosEIP, noQosEIP, vpcQosParams, natgwQoSPolicyName)
		// Cleanup is handled automatically by DeferCleanup in setupQosTestResources
	})

	framework.ConformanceIt("eip qos", func() {
		qosPolicyClient := f.QoSPolicyClient()

		// bind the qos policy to the eip at creation time
		eipQoSPolicyName := "eip-qos-policy-" + framework.RandomSuffix()
		ginkgo.By("Creating qos policy " + eipQoSPolicyName)
		eipQoSPolicy := framework.MakeQoSPolicy(eipQoSPolicyName, false, apiv1.QoSBindingTypeEIP, getEIPQoSRule(eipLimit))
		_ = qosPolicyClient.CreateSync(eipQoSPolicy)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up eip qos policy " + eipQoSPolicyName)
			qosPolicyClient.DeleteSync(eipQoSPolicyName)
		})

		qosPod, noQosPod, qosEIP, noQosEIP := setupQosTestResources(
			f, dockerExtNetNetwork, vpcQosParams, net1NicName, "", eipQoSPolicyName,
		)

		eipQoSCases(f, qosPod, noQosPod, qosEIP, noQosEIP, vpcQosParams, eipQoSPolicyName)
		// Cleanup is handled automatically by DeferCleanup in setupQosTestResources
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
