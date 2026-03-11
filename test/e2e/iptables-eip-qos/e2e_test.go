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
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
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
	dockerExtNetName       = "kube-ovn-qos"
	networkAttachDefName   = "qos-ovn-vpc-external-network"
	externalSubnetProvider = "qos-ovn-vpc-external-network.kube-system"
)

const (
	eipLimit        = 10 // Initial EIP QoS limit
	updatedEIPLimit = 30 // Updated EIP QoS limit (3x of initial)
	newEIPLimit     = 90 // New EIP QoS limit (9x of initial)
	specificIPLimit = 25 // Specific IP matching QoS limit
	defaultNicLimit = 50 // Default NIC QoS limit
)

// Bandwidth limits for multi-EIP isolation test
// These test decimal bandwidth values on the SAME NAT Gateway to verify:
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

var decimalQoSConfigs = []decimalQoSConfig{
	{"0.5", "0.06", 0.5}, // 0.5 Mbps = 500 Kbps, burst = 62,500 bytes
	{"1.5", "0.18", 1.5}, // 1.5 Mbps, burst = 187,500 bytes
	{"3", "0.36", 3},     // 3 Mbps, burst = 375,000 bytes
	{"9", "1.08", 9},     // 9 Mbps, burst = 1,125,000 bytes
}

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
	vpcNatGw := framework.MakeVpcNatGateway(vpcNatGwName, vpcName, overlaySubnetName, lanIP, externalNetworkName, natGwQosPolicy)
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

// dumpTcRulesOnNatGw dumps tc qdisc, class, and filter rules on NAT Gateway pod for debugging
// This helps diagnose QoS issues by showing the actual tc configuration
func dumpTcRulesOnNatGw(f *framework.Framework, natgwName, eipIP string) {
	ginkgo.GinkgoHelper()

	natGwPodName := util.GenNatGwPodName(natgwName)
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
	limit int, expect bool,
) {
	checkQosFloat(f, qosPod, noQosPod, qosEIP, noQosEIP, float64(limit), expect)
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
	limitMbps float64, expect bool,
) {
	ginkgo.GinkgoHelper()

	if !skipIperf {
		testType := "QoS Enabled"
		if !expect {
			testType = "QoS Disabled"
		}

		if expect {
			// When QoS is expected to be applied, bandwidth should be within limit * bandwidthToleranceLow ~ limit * bandwidthToleranceHigh

			// Test egress: qosPod → noQosEIP (QoS applied on qosEIP's egress)
			output := iperf(f, qosPod, noQosEIP)
			result := validateRateLimitFloatWithResult(output, limitMbps)
			klog.Info(formatBandwidthSummary(result, testType, "Egress: qosPod -> noQosEIP"))
			framework.ExpectTrue(result.Passed, "expected egress bandwidth to be limited to %.2f~%.2f Mbps, but got %.2f Mbps",
				result.MinExpected, result.MaxExpected, result.BestMatch)

			// Test ingress: noQosPod → qosEIP (QoS applied on qosEIP's ingress)
			output = iperf(f, noQosPod, qosEIP)
			result = validateRateLimitFloatWithResult(output, limitMbps)
			klog.Info(formatBandwidthSummary(result, testType, "Ingress: noQosPod -> qosEIP"))
			framework.ExpectTrue(result.Passed, "expected ingress bandwidth to be limited to %.2f~%.2f Mbps, but got %.2f Mbps",
				result.MinExpected, result.MaxExpected, result.BestMatch)
		} else {
			// When QoS is not expected to be applied, bandwidth should exceed limit * bandwidthToleranceHigh
			// This verifies that QoS was actually working before (not just slow network)

			// Test egress: qosPod → noQosEIP (no QoS, should be fast)
			output := iperf(f, qosPod, noQosEIP)
			result := validateBandwidthExceedsLimitFloatWithResult(output, limitMbps)
			klog.Info(formatBandwidthSummary(result, testType, "Egress: qosPod -> noQosEIP"))
			framework.ExpectTrue(result.Passed, "expected egress bandwidth to exceed %.2f Mbps when QoS is disabled, but got %.2f Mbps",
				result.MinExpected, result.BestMatch)

			// Test ingress: noQosPod → qosEIP (no QoS, should be fast)
			output = iperf(f, noQosPod, qosEIP)
			result = validateBandwidthExceedsLimitFloatWithResult(output, limitMbps)
			klog.Info(formatBandwidthSummary(result, testType, "Ingress: noQosPod -> qosEIP"))
			framework.ExpectTrue(result.Passed, "expected ingress bandwidth to exceed %.2f Mbps when QoS is disabled, but got %.2f Mbps",
				result.MinExpected, result.BestMatch)
		}
	}
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
//	1.5 Mbps: 1.5 × 125,000 = 187,500 bytes → burst_MB ≈ 0.18 (>> MTU 1500) ✓
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

// defaultQoSCases test default qos policy
func defaultQoSCases(f *framework.Framework,
	qosPod, noQosPod *corev1.Pod,
	qosEIP, noQosEIP *apiv1.IptablesEIP,
	natgwName string,
) {
	ginkgo.GinkgoHelper()

	// Derive clients from framework
	vpcNatGwClient := f.VpcNatGatewayClient()
	podClient := f.PodClientNS(framework.KubeOvnNamespace)
	qosPolicyClient := f.QoSPolicyClient()

	// create nic qos policy
	qosPolicyName := "default-nic-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + qosPolicyName)
	rules := getNicDefaultQoSPolicy(defaultNicLimit)

	qosPolicy := framework.MakeQoSPolicy(qosPolicyName, true, apiv1.QoSBindingTypeNatGw, rules)
	_ = qosPolicyClient.CreateSync(qosPolicy)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up qos policy " + qosPolicyName)
		qosPolicyClient.DeleteSync(qosPolicyName)
	})

	ginkgo.By("Patch natgw " + natgwName + " with qos policy " + qosPolicyName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + strconv.Itoa(defaultNicLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, defaultNicLimit, true)

	ginkgo.By("Delete natgw pod " + natgwName + "-0")
	natGwPodName := util.GenNatGwPodName(natgwName)
	// Use Delete instead of DeleteSync because StatefulSet will recreate the pod
	err := podClient.Delete(natGwPodName)
	framework.ExpectNoError(err, "failed to delete natgw pod "+natGwPodName)

	ginkgo.By("Wait for natgw " + natgwName + " pod to be ready after recreation")
	vpcNatGwClient.WaitGwPodReady(natgwName, 2*time.Minute, f.ClientSet)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + strconv.Itoa(defaultNicLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, defaultNicLimit, true)

	ginkgo.By("Remove qos policy " + qosPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, "")

	ginkgo.By("Check qos " + qosPolicyName + " is not limited to " + strconv.Itoa(defaultNicLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, defaultNicLimit, false)
}

// eipQoSCases tests EIP-level QoS policy
func eipQoSCases(f *framework.Framework,
	qosPod, noQosPod *corev1.Pod,
	qosEIP, noQosEIP *apiv1.IptablesEIP,
	eipName, natgwName string,
) {
	ginkgo.GinkgoHelper()

	// Derive clients from framework
	vpcNatGwClient := f.VpcNatGatewayClient()
	eipClient := f.IptablesEIPClient()
	podClient := f.PodClientNS(framework.KubeOvnNamespace)
	qosPolicyClient := f.QoSPolicyClient()

	// create eip qos policy
	qosPolicyName := "eip-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + qosPolicyName)
	rules := getEIPQoSRule(eipLimit)

	qosPolicy := framework.MakeQoSPolicy(qosPolicyName, false, apiv1.QoSBindingTypeEIP, rules)
	qosPolicy = qosPolicyClient.CreateSync(qosPolicy)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up qos policy " + qosPolicyName)
		qosPolicyClient.DeleteSync(qosPolicyName)
	})

	ginkgo.By("Patch eip " + eipName + " with qos policy " + qosPolicyName)
	_ = eipClient.PatchQoSPolicySync(eipName, qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + strconv.Itoa(eipLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, eipLimit, true)

	ginkgo.By("Update qos policy " + qosPolicyName + " with new rate limit")

	rules = getEIPQoSRule(updatedEIPLimit)
	modifiedqosPolicy := qosPolicy.DeepCopy()
	modifiedqosPolicy.Spec.BandwidthLimitRules = rules
	qosPolicyClient.Patch(qosPolicy, modifiedqosPolicy)
	qosPolicyClient.WaitToQoSReady(qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is changed to " + strconv.Itoa(updatedEIPLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, updatedEIPLimit, true)

	ginkgo.By("Delete natgw pod " + natgwName + "-0")
	natGwPodName := util.GenNatGwPodName(natgwName)
	// Use Delete instead of DeleteSync because StatefulSet will recreate the pod
	err := podClient.Delete(natGwPodName)
	framework.ExpectNoError(err, "failed to delete natgw pod "+natGwPodName)

	ginkgo.By("Wait for natgw " + natgwName + " pod to be ready after recreation")
	vpcNatGwClient.WaitGwPodReady(natgwName, 2*time.Minute, f.ClientSet)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + strconv.Itoa(updatedEIPLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, updatedEIPLimit, true)

	newQoSPolicyName := "new-eip-qos-policy-" + framework.RandomSuffix()
	newRules := getEIPQoSRule(newEIPLimit)
	newQoSPolicy := framework.MakeQoSPolicy(newQoSPolicyName, false, apiv1.QoSBindingTypeEIP, newRules)
	_ = qosPolicyClient.CreateSync(newQoSPolicy)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up new qos policy " + newQoSPolicyName)
		qosPolicyClient.DeleteSync(newQoSPolicyName)
	})

	ginkgo.By("Change qos policy of eip " + eipName + " to " + newQoSPolicyName)
	_ = eipClient.PatchQoSPolicySync(eipName, newQoSPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + strconv.Itoa(newEIPLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, newEIPLimit, true)

	ginkgo.By("Remove qos policy " + qosPolicyName + " from natgw " + eipName)
	_ = eipClient.PatchQoSPolicySync(eipName, "")

	ginkgo.By("Check qos " + qosPolicyName + " is not limited to " + strconv.Itoa(newEIPLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, newEIPLimit, false)
	// Cleanup is now handled by DeferCleanup
}

// multiEIPQoSIsolationCases tests that QoS policies on multiple EIPs on the SAME NAT Gateway
// don't interfere with each other. This also tests decimal bandwidth values.
//
// Test structure (setup-first, test-last pattern):
// 1. Create 4 additional EIPs on the same NAT GW with different decimal QoS limits
// 2. Apply QoS policies: 0.01, 0.1, 0.5, 2.5 Mbps
// 3. Wait for all tc rules to stabilize
// 4. Test each EIP's bandwidth sequentially
//
// This verifies:
// - Multiple EIPs with different QoS don't interfere with each other
// - Sub-Mbps decimal bandwidth limiting works correctly (0.01, 0.1, 0.5 Mbps)
// - Decimal values > 1 Mbps work correctly (2.5 Mbps)
func multiEIPQoSIsolationCases(f *framework.Framework,
	_, noQosPod *corev1.Pod,
	_, noQosEIP *apiv1.IptablesEIP,
	_, _ string, // qosEIPName, noQosEIPName - not used in this test
	natgwName string,
	attachDefName string,
	qosSubnetName string,
) {
	ginkgo.GinkgoHelper()

	// Derive clients from framework
	podClient := f.PodClient()
	eipClient := f.IptablesEIPClient()
	fipClient := f.IptablesFIPClient()
	qosPolicyClient := f.QoSPolicyClient()

	// ============================================================
	// PHASE 1: Create additional EIPs and pods on the SAME NAT GW
	// ============================================================

	// We need to create additional EIPs on the same NAT GW to test isolation
	// The test will use decimalQoSConfigs: 0.01, 0.1, 0.5, 2.5 Mbps
	type eipTestResource struct {
		eipName   string
		fipName   string
		podName   string
		pod       *corev1.Pod
		eip       *apiv1.IptablesEIP
		qosPolicy string
		config    decimalQoSConfig
	}

	resources := make([]eipTestResource, len(decimalQoSConfigs))
	randomSuffix := framework.RandomSuffix()

	// Create pods, EIPs, FIPs for each test config
	for i, cfg := range decimalQoSConfigs {
		resources[i] = eipTestResource{
			eipName: fmt.Sprintf("isolation-eip-%d-%s", i, randomSuffix),
			fipName: fmt.Sprintf("isolation-fip-%d-%s", i, randomSuffix),
			podName: fmt.Sprintf("isolation-pod-%d-%s", i, randomSuffix),
			config:  cfg,
		}
	}

	iperfServerCmd := []string{"iperf", "-s", "-i", "1", "-p", iperf2Port}

	// Create all pods first - must be in qosVpc's subnet to use the qosNatGw
	for i := range resources {
		res := &resources[i]
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: qosSubnetName,
		}
		ginkgo.By(fmt.Sprintf("Creating pod %s for %.2f Mbps test", res.podName, res.config.LimitMbps))
		pod := framework.MakePod(f.Namespace.Name, res.podName, nil, annotations, framework.AgnhostImage, iperfServerCmd, nil)
		res.pod = podClient.CreateSync(pod)
		ginkgo.DeferCleanup(func() {
			podClient.DeleteSync(res.podName)
		})
	}

	// Create all EIPs on the same NAT GW
	for i := range resources {
		res := &resources[i]
		ginkgo.By(fmt.Sprintf("Creating EIP %s on NAT GW %s", res.eipName, natgwName))
		eip := framework.MakeIptablesEIP(res.eipName, "", "", "", natgwName, attachDefName, "")
		_ = eipClient.CreateSync(eip)
		res.eip = waitForIptablesEIPReady(eipClient, res.eipName, 60*time.Second)
		ginkgo.DeferCleanup(func() {
			eipClient.DeleteSync(res.eipName)
		})
	}

	// Create all FIPs
	for i := range resources {
		res := &resources[i]
		ginkgo.By(fmt.Sprintf("Creating FIP %s for pod %s -> EIP %s", res.fipName, res.podName, res.eipName))
		fip := framework.MakeIptablesFIPRule(res.fipName, res.eipName, res.pod.Status.PodIP)
		_ = fipClient.CreateSync(fip)
		ginkgo.DeferCleanup(func() {
			fipClient.DeleteSync(res.fipName)
		})
	}

	// ============================================================
	// PHASE 2: Create and apply all QoS policies
	// ============================================================

	for i := range resources {
		res := &resources[i]
		res.qosPolicy = fmt.Sprintf("isolation-qos-%d-%s", i, randomSuffix)

		ginkgo.By(fmt.Sprintf("Creating QoS policy %s with %s Mbps", res.qosPolicy, res.config.Rate))
		rules := getEIPQoSRuleWithDecimal(res.config.Rate, res.config.Burst)
		qosPolicy := framework.MakeQoSPolicy(res.qosPolicy, false, apiv1.QoSBindingTypeEIP, rules)
		_ = qosPolicyClient.CreateSync(qosPolicy)
		ginkgo.DeferCleanup(func() {
			qosPolicyClient.DeleteSync(res.qosPolicy)
		})

		ginkgo.By(fmt.Sprintf("Applying QoS policy %s to EIP %s", res.qosPolicy, res.eipName))
		_ = eipClient.PatchQoSPolicySync(res.eipName, res.qosPolicy)
		ginkgo.DeferCleanup(func() {
			_ = eipClient.PatchQoSPolicySync(res.eipName, "")
		})
	}

	// Wait for all tc rules to be applied
	ginkgo.By("Waiting for all QoS rules to be applied and network to stabilize")
	time.Sleep(10 * time.Second)

	// Dump tc rules for debugging
	ginkgo.By("Dumping tc rules on NAT GW " + natgwName)
	if len(resources) > 0 {
		dumpTcRulesOnNatGw(f, natgwName, resources[0].eip.Status.IP)
	}

	// ============================================================
	// PHASE 3: Test all EIPs' bandwidth - tests at the end
	// ============================================================

	framework.Logf("\n========== Starting Multi-EIP QoS Isolation Tests ==========")
	framework.Logf("Testing %d EIPs on the same NAT GW with different decimal QoS limits", len(resources))

	for i, res := range resources {
		ginkgo.By(fmt.Sprintf("Testing EIP %d (%s) - should be limited to %s Mbps",
			i+1, res.eip.Status.IP, res.config.Rate))

		// Test bandwidth: noQosPod -> res.eip (ingress to the test EIP)
		//                 res.pod -> noQosEIP (egress from the test pod)
		checkQosFloat(f, res.pod, noQosPod, res.eip, noQosEIP, res.config.LimitMbps, true)

		framework.Logf("✓ EIP %d (%s) passed: limited to %s Mbps as expected",
			i+1, res.eip.Status.IP, res.config.Rate)
	}

	framework.Logf("\n========== Multi-EIP QoS Isolation Test PASSED ==========")
	framework.Logf("All %d EIPs on NAT GW %s verified with correct bandwidth limits:", len(resources), natgwName)
	for i, res := range resources {
		framework.Logf("  EIP %d (%s): %s Mbps", i+1, res.eip.Status.IP, res.config.Rate)
	}
}

// priorityQoSCases test qos match priority
func priorityQoSCases(f *framework.Framework,
	qosPod, noQosPod *corev1.Pod,
	qosEIP, noQosEIP *apiv1.IptablesEIP,
	natgwName, eipName string,
) {
	ginkgo.GinkgoHelper()

	// Derive clients from framework
	vpcNatGwClient := f.VpcNatGatewayClient()
	eipClient := f.IptablesEIPClient()
	qosPolicyClient := f.QoSPolicyClient()

	// create nic qos policy
	natGwQoSPolicyName := "priority-nic-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + natGwQoSPolicyName)
	// default qos policy + special qos policy
	natgwRules := getNicDefaultQoSPolicy(defaultNicLimit)
	natgwRules = append(natgwRules, getSpecialQoSRule(specificIPLimit, noQosEIP.Status.IP)...)

	natgwQoSPolicy := framework.MakeQoSPolicy(natGwQoSPolicyName, true, apiv1.QoSBindingTypeNatGw, natgwRules)
	_ = qosPolicyClient.CreateSync(natgwQoSPolicy)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up natgw qos policy " + natGwQoSPolicyName)
		qosPolicyClient.DeleteSync(natGwQoSPolicyName)
	})

	ginkgo.By("Patch natgw " + natgwName + " with qos policy " + natGwQoSPolicyName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, natGwQoSPolicyName)

	eipQoSPolicyName := "eip-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + eipQoSPolicyName)
	eipRules := getEIPQoSRule(eipLimit)

	eipQoSPolicy := framework.MakeQoSPolicy(eipQoSPolicyName, false, apiv1.QoSBindingTypeEIP, eipRules)
	_ = qosPolicyClient.CreateSync(eipQoSPolicy)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up eip qos policy " + eipQoSPolicyName)
		qosPolicyClient.DeleteSync(eipQoSPolicyName)
	})

	ginkgo.By("Patch eip " + eipName + " with qos policy " + eipQoSPolicyName)
	_ = eipClient.PatchQoSPolicySync(eipName, eipQoSPolicyName)

	// match qos of priority 1
	ginkgo.By("Check qos to match priority 1 is limited to " + strconv.Itoa(eipLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, eipLimit, true)

	ginkgo.By("Remove qos policy " + eipQoSPolicyName + " from natgw " + eipName)
	_ = eipClient.PatchQoSPolicySync(eipName, "")

	// match qos of priority 2
	ginkgo.By("Check qos to match priority 2 is limited to " + strconv.Itoa(specificIPLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, specificIPLimit, true)

	// change qos policy of natgw
	newNatGwQoSPolicyName := "new-priority-nic-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + newNatGwQoSPolicyName)
	newNatgwRules := getNicDefaultQoSPolicy(defaultNicLimit)

	newNatgwQoSPolicy := framework.MakeQoSPolicy(newNatGwQoSPolicyName, true, apiv1.QoSBindingTypeNatGw, newNatgwRules)
	_ = qosPolicyClient.CreateSync(newNatgwQoSPolicy)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up new natgw qos policy " + newNatGwQoSPolicyName)
		qosPolicyClient.DeleteSync(newNatGwQoSPolicyName)
	})

	ginkgo.By("Change qos policy of natgw " + natgwName + " to " + newNatGwQoSPolicyName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, newNatGwQoSPolicyName)

	// match qos of priority 3
	ginkgo.By("Check qos to match priority 3 is limited to " + strconv.Itoa(specificIPLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, defaultNicLimit, true)

	ginkgo.By("Remove qos policy " + natGwQoSPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, "")

	ginkgo.By("Check qos " + natGwQoSPolicyName + " is not limited to " + strconv.Itoa(defaultNicLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, defaultNicLimit, false)
	// Cleanup is now handled by DeferCleanup
}

func createNatGwAndSetQosCases(f *framework.Framework,
	qosPod, noQosPod *corev1.Pod,
	noQosEIP *apiv1.IptablesEIP,
	natgwName, eipName, fipName string,
	vpcName, overlaySubnetName, lanIP, attachDefName string,
) {
	ginkgo.GinkgoHelper()

	// Derive clients from framework
	vpcNatGwClient := f.VpcNatGatewayClient()
	ipClient := f.IPClient()
	eipClient := f.IptablesEIPClient()
	fipClient := f.IptablesFIPClient()
	subnetClient := f.SubnetClient()
	qosPolicyClient := f.QoSPolicyClient()

	// delete fip
	ginkgo.By("Deleting fip " + fipName)
	fipClient.DeleteSync(fipName)

	ginkgo.By("Deleting eip " + eipName)
	eipClient.DeleteSync(eipName)

	// the only pod for vpc nat gateway
	vpcNatGw1PodName := util.GenNatGwPodName(natgwName)

	// delete vpc nat gw statefulset remaining ip for eth0 and net2
	ginkgo.By("Deleting custom vpc nat gw " + natgwName)
	vpcNatGwClient.DeleteSync(natgwName)

	overlaySubnet1 := subnetClient.Get(overlaySubnetName)
	macvlanSubnet := subnetClient.Get(attachDefName)
	eth0IpName := ovs.PodNameToPortName(vpcNatGw1PodName, framework.KubeOvnNamespace, overlaySubnet1.Spec.Provider)
	net1IpName := ovs.PodNameToPortName(vpcNatGw1PodName, framework.KubeOvnNamespace, macvlanSubnet.Spec.Provider)
	ginkgo.By("Deleting vpc nat gw eth0 ip " + eth0IpName)
	ipClient.DeleteSync(eth0IpName)
	ginkgo.By("Deleting vpc nat gw net1 ip " + net1IpName)
	ipClient.DeleteSync(net1IpName)

	natgwQoSPolicyName := "default-nic-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + natgwQoSPolicyName)
	rules := getNicDefaultQoSPolicy(defaultNicLimit)

	qosPolicy := framework.MakeQoSPolicy(natgwQoSPolicyName, true, apiv1.QoSBindingTypeNatGw, rules)
	_ = qosPolicyClient.CreateSync(qosPolicy)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up recreated qos policy " + natgwQoSPolicyName)
		qosPolicyClient.DeleteSync(natgwQoSPolicyName)
	})

	ginkgo.By("Creating custom vpc nat gw")
	vpcNatGw := framework.MakeVpcNatGateway(natgwName, vpcName, overlaySubnetName, lanIP, attachDefName, natgwQoSPolicyName)
	_ = vpcNatGwClient.CreateSync(vpcNatGw, f.ClientSet)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up recreated vpc nat gw " + natgwName)
		vpcNatGwClient.DeleteSync(natgwName)
	})

	eipQoSPolicyName := "eip-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + eipQoSPolicyName)
	rules = getEIPQoSRule(eipLimit)

	eipQoSPolicy := framework.MakeQoSPolicy(eipQoSPolicyName, false, apiv1.QoSBindingTypeEIP, rules)
	_ = qosPolicyClient.CreateSync(eipQoSPolicy)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up recreated qos policy " + eipQoSPolicyName)
		qosPolicyClient.DeleteSync(eipQoSPolicyName)
	})

	ginkgo.By("Creating eip " + eipName)
	qosEIP := framework.MakeIptablesEIP(eipName, "", "", "", natgwName, attachDefName, eipQoSPolicyName)
	_ = eipClient.CreateSync(qosEIP)
	qosEIP = waitForIptablesEIPReady(eipClient, eipName, 60*time.Second)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up recreated eip " + eipName)
		eipClient.DeleteSync(eipName)
	})

	ginkgo.By("Creating fip " + fipName)
	fip := framework.MakeIptablesFIPRule(fipName, eipName, qosPod.Status.PodIP)
	_ = fipClient.CreateSync(fip)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up recreated fip " + fipName)
		fipClient.DeleteSync(fipName)
	})

	ginkgo.By("Check qos " + eipQoSPolicyName + " is limited to " + strconv.Itoa(eipLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, eipLimit, true)

	ginkgo.By("Remove qos policy " + eipQoSPolicyName + " from natgw " + natgwName)
	_ = eipClient.PatchQoSPolicySync(eipName, "")

	ginkgo.By("Check qos " + natgwQoSPolicyName + " is limited to " + strconv.Itoa(defaultNicLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, defaultNicLimit, true)

	ginkgo.By("Remove qos policy " + natgwQoSPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, "")

	ginkgo.By("Check qos " + natgwQoSPolicyName + " is not limited to " + strconv.Itoa(defaultNicLimit) + "Mbps")
	checkQos(f, qosPod, noQosPod, qosEIP, noQosEIP, defaultNicLimit, false)

	// Cleanup is now handled by DeferCleanup registered after each resource creation
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
	MaxExpected  float64   // Expected maximum in Mbps (or 0 for "exceeds limit" case)
	BestMatch    float64   // The bandwidth value that matched (or closest), in Mbps
}

// formatBandwidthSummary creates a human-readable summary of bandwidth test results
func formatBandwidthSummary(result bandwidthValidationResult, testType, direction string) string {
	var sb strings.Builder
	sb.WriteString("\n╔═══════════════════════════════════════════════════════════════════════════════╗\n")
	fmt.Fprintf(&sb, "║  QoS Bandwidth Test Summary - %s (%s)\n", testType, direction)
	sb.WriteString("╠═══════════════════════════════════════════════════════════════════════════════╣\n")
	fmt.Fprintf(&sb, "║  QoS Limit:         %.2f Mbps\n", result.LimitMbps)

	if result.MaxExpected > 0 {
		fmt.Fprintf(&sb, "║  Expected Range:    %.2f ~ %.2f Mbps (%.0f%% ~ %.0f%% of limit)\n",
			result.MinExpected, result.MaxExpected,
			bandwidthToleranceLow*100, bandwidthToleranceHigh*100)
	} else {
		fmt.Fprintf(&sb, "║  Expected:          > %.2f Mbps (QoS disabled, should exceed %.0f%% of limit)\n",
			result.MinExpected, bandwidthToleranceHigh*100)
	}

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

// validateBandwidthExceedsLimitFloatWithResult validates bandwidth exceeds limit and returns detailed result
// This is used to verify that QoS is actually working by confirming that
// without QoS, the bandwidth can exceed the limit significantly
func validateBandwidthExceedsLimitFloatWithResult(text string, limitMbps float64) bandwidthValidationResult {
	// bandwidth should exceed limit by at least 50% when QoS is not applied
	minValue := limitMbps * bitsPerMbit * bandwidthToleranceHigh

	result := bandwidthValidationResult{
		Passed:      false,
		LimitMbps:   limitMbps,
		MinExpected: limitMbps * bandwidthToleranceHigh, // Should exceed this
		MaxExpected: 0,                                  // No upper bound
	}

	bandwidths := parseBandwidthFromIperfOutput(text)
	for _, bw := range bandwidths {
		result.MeasuredMbps = append(result.MeasuredMbps, bw/bitsPerMbit)
	}

	var bestMatch float64
	for _, bw := range bandwidths {
		if bw >= minValue {
			result.Passed = true
			result.BestMatch = bw / bitsPerMbit
			return result
		}
		// Track highest value for failure reporting
		if bw > bestMatch {
			bestMatch = bw
		}
	}
	result.BestMatch = bestMatch / bitsPerMbit
	return result
}

// setupQosTestResources creates two VPCs with NAT Gateways, EIPs, FIPs and pods:
//
//   - qosVpc: The test target where QoS policies will be applied
//   - noQosVpc: Clean endpoint without any QoS (used as traffic source/destination)
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
	overlaySubnetV4Cidr := "10.0.0.0/24"
	overlaySubnetV4Gw := "10.0.0.1"
	lanIP := "10.0.0.254"
	natgwQoS := "" // No default QoS on NAT Gateway - QoS is applied per-EIP

	// Create qosVpc + qosNatGw (test target where QoS will be applied)
	setupVpcNatGwTestEnvironment(
		f, dockerExtNetNetwork, attachNetClient,
		subnetClient, vpcClient, vpcNatGwClient,
		vpcQosParams.qosVpcName, vpcQosParams.qosSubnetName, vpcQosParams.qosNatGwName,
		natgwQoS, overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
		dockerExtNetName, vpcQosParams.attachDefName, net1NicName,
		vpcQosParams.subnetProvider,
		true,
	)

	// Create noQosVpc + noQosNatGw (clean endpoint without QoS)
	setupVpcNatGwTestEnvironment(
		f, dockerExtNetNetwork, attachNetClient,
		subnetClient, vpcClient, vpcNatGwClient,
		vpcQosParams.noQosVpcName, vpcQosParams.noQosSubnetName, vpcQosParams.noQosNatGwName,
		natgwQoS, overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
		dockerExtNetName, vpcQosParams.attachDefName, net1NicName,
		vpcQosParams.subnetProvider,
		true,
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
	qosEIP := framework.MakeIptablesEIP(vpcQosParams.qosEIPName, "", "", "", vpcQosParams.qosNatGwName, vpcQosParams.attachDefName, "")
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

	framework.ConformanceIt("default nic qos", func() {
		// Setup resources
		qosPod, noQosPod, qosEIP, noQosEIP := setupQosTestResources(
			f, dockerExtNetNetwork, vpcQosParams, net1NicName,
		)

		// Run test
		defaultQoSCases(f, qosPod, noQosPod, qosEIP, noQosEIP, vpcQosParams.qosNatGwName)
		// Cleanup is handled automatically by DeferCleanup in setupQosTestResources
	})

	framework.ConformanceIt("eip qos", func() {
		// Setup resources
		qosPod, noQosPod, qosEIP, noQosEIP := setupQosTestResources(
			f, dockerExtNetNetwork, vpcQosParams, net1NicName,
		)

		// Run test
		eipQoSCases(f, qosPod, noQosPod, qosEIP, noQosEIP, vpcQosParams.qosEIPName, vpcQosParams.qosNatGwName)
		// Cleanup is handled automatically by DeferCleanup in setupQosTestResources
	})

	framework.ConformanceIt("multi-eip qos isolation with decimal rates", func() {
		// Setup resources - creates VPCs, NAT gateways, and base EIPs/pods
		qosPod, noQosPod, qosEIP, noQosEIP := setupQosTestResources(
			f, dockerExtNetNetwork, vpcQosParams, net1NicName,
		)

		// Run test - creates additional EIPs on the SAME NAT GW with decimal QoS limits
		// Tests: 0.5, 1.5, 3, 9 Mbps to verify:
		// 1. Multiple EIPs with different QoS don't interfere with each other
		// 2. Decimal bandwidth limiting works correctly
		multiEIPQoSIsolationCases(f, qosPod, noQosPod, qosEIP, noQosEIP,
			vpcQosParams.qosEIPName, vpcQosParams.noQosEIPName,
			vpcQosParams.qosNatGwName, vpcQosParams.attachDefName,
			vpcQosParams.qosSubnetName)
		// Cleanup is handled automatically by DeferCleanup in setupQosTestResources
	})

	framework.ConformanceIt("qos priority matching", func() {
		// Setup resources
		qosPod, noQosPod, qosEIP, noQosEIP := setupQosTestResources(
			f, dockerExtNetNetwork, vpcQosParams, net1NicName,
		)

		// Run test
		priorityQoSCases(f, qosPod, noQosPod, qosEIP, noQosEIP, vpcQosParams.qosNatGwName, vpcQosParams.qosEIPName)
		// Cleanup is handled automatically by DeferCleanup in setupQosTestResources
	})

	framework.ConformanceIt("create resource with qos policy", func() {
		// Setup resources
		qosPod, noQosPod, _, noQosEIP := setupQosTestResources(
			f, dockerExtNetNetwork, vpcQosParams, net1NicName,
		)

		// Run test (this test deletes and recreates resources internally)
		lanIP := "10.0.0.254"
		createNatGwAndSetQosCases(f, qosPod, noQosPod, noQosEIP,
			vpcQosParams.qosNatGwName, vpcQosParams.qosEIPName, vpcQosParams.qosFIPName,
			vpcQosParams.qosVpcName, vpcQosParams.qosSubnetName, lanIP, vpcQosParams.attachDefName)
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
