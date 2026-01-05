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

const (
	dockerExtNetName       = "kube-ovn-qos"
	networkAttachDefName   = "qos-ovn-vpc-external-network"
	externalSubnetProvider = "qos-ovn-vpc-external-network.kube-system"
)

const (
	eipLimit = iota*5 + 10
	updatedEIPLimit
	newEIPLimit
	specificIPLimit
	defaultNicLimit
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
	vpc1Name       string
	vpc2Name       string
	vpc1SubnetName string
	vpc2SubnetName string
	vpcNat1GwName  string
	vpcNat2GwName  string
	vpc1EIPName    string
	vpc2EIPName    string
	vpc1FIPName    string
	vpc2FIPName    string
	vpc1PodName    string
	vpc2PodName    string
	attachDefName  string
	subnetProvider string
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
	vpc1Pod, vpc2Pod *corev1.Pod, vpc1EIP, vpc2EIP *apiv1.IptablesEIP,
	limit int, expect bool,
) {
	ginkgo.GinkgoHelper()

	if !skipIperf {
		if expect {
			output := iperf(f, vpc1Pod, vpc2EIP)
			framework.ExpectTrue(validRateLimit(output, limit))
			output = iperf(f, vpc2Pod, vpc1EIP)
			framework.ExpectTrue(validRateLimit(output, limit))
		} else {
			output := iperf(f, vpc1Pod, vpc2EIP)
			framework.ExpectFalse(validRateLimit(output, limit))
			output = iperf(f, vpc2Pod, vpc1EIP)
			framework.ExpectFalse(validRateLimit(output, limit))
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
	vpc1Pod, vpc2Pod *corev1.Pod,
	vpc1EIP, vpc2EIP *apiv1.IptablesEIP,
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
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, true)

	ginkgo.By("Delete natgw pod " + natgwName + "-0")
	natGwPodName := util.GenNatGwPodName(natgwName)
	// Use Delete instead of DeleteSync because StatefulSet will recreate the pod
	err := podClient.Delete(natGwPodName)
	framework.ExpectNoError(err, "failed to delete natgw pod "+natGwPodName)

	ginkgo.By("Wait for natgw " + natgwName + " pod to be ready after recreation")
	vpcNatGwClient.WaitGwPodReady(natgwName, 2*time.Minute, f.ClientSet)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + strconv.Itoa(defaultNicLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, true)

	ginkgo.By("Remove qos policy " + qosPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, "")

	ginkgo.By("Check qos " + qosPolicyName + " is not limited to " + strconv.Itoa(defaultNicLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, false)
}

// eipQoSCases test default qos policy
func eipQoSCases(f *framework.Framework,
	vpc1Pod, vpc2Pod *corev1.Pod,
	vpc1EIP, vpc2EIP *apiv1.IptablesEIP,
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
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, eipLimit, true)

	ginkgo.By("Update qos policy " + qosPolicyName + " with new rate limit")

	rules = getEIPQoSRule(updatedEIPLimit)
	modifiedqosPolicy := qosPolicy.DeepCopy()
	modifiedqosPolicy.Spec.BandwidthLimitRules = rules
	qosPolicyClient.Patch(qosPolicy, modifiedqosPolicy)
	qosPolicyClient.WaitToQoSReady(qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is changed to " + strconv.Itoa(updatedEIPLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, updatedEIPLimit, true)

	ginkgo.By("Delete natgw pod " + natgwName + "-0")
	natGwPodName := util.GenNatGwPodName(natgwName)
	// Use Delete instead of DeleteSync because StatefulSet will recreate the pod
	err := podClient.Delete(natGwPodName)
	framework.ExpectNoError(err, "failed to delete natgw pod "+natGwPodName)

	ginkgo.By("Wait for natgw " + natgwName + " pod to be ready after recreation")
	vpcNatGwClient.WaitGwPodReady(natgwName, 2*time.Minute, f.ClientSet)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + strconv.Itoa(updatedEIPLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, updatedEIPLimit, true)

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
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, newEIPLimit, true)

	ginkgo.By("Remove qos policy " + qosPolicyName + " from natgw " + eipName)
	_ = eipClient.PatchQoSPolicySync(eipName, "")

	ginkgo.By("Check qos " + qosPolicyName + " is not limited to " + strconv.Itoa(newEIPLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, newEIPLimit, false)
	// Cleanup is now handled by DeferCleanup
}

// specifyingIPQoSCases test default qos policy
func specifyingIPQoSCases(f *framework.Framework,
	vpc1Pod, vpc2Pod *corev1.Pod,
	vpc1EIP, vpc2EIP *apiv1.IptablesEIP,
	natgwName string,
) {
	ginkgo.GinkgoHelper()

	// Derive clients from framework
	vpcNatGwClient := f.VpcNatGatewayClient()
	qosPolicyClient := f.QoSPolicyClient()

	// create nic qos policy
	qosPolicyName := "specifying-ip-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + qosPolicyName)

	rules := getSpecialQoSRule(specificIPLimit, vpc2EIP.Status.IP)

	qosPolicy := framework.MakeQoSPolicy(qosPolicyName, true, apiv1.QoSBindingTypeNatGw, rules)
	_ = qosPolicyClient.CreateSync(qosPolicy)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up qos policy " + qosPolicyName)
		qosPolicyClient.DeleteSync(qosPolicyName)
	})

	ginkgo.By("Patch natgw " + natgwName + " with qos policy " + qosPolicyName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + strconv.Itoa(specificIPLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, specificIPLimit, true)

	ginkgo.By("Remove qos policy " + qosPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, "")

	ginkgo.By("Check qos " + qosPolicyName + " is not limited to " + strconv.Itoa(specificIPLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, specificIPLimit, false)
	// Cleanup is now handled by DeferCleanup
}

// priorityQoSCases test qos match priority
func priorityQoSCases(f *framework.Framework,
	vpc1Pod, vpc2Pod *corev1.Pod,
	vpc1EIP, vpc2EIP *apiv1.IptablesEIP,
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
	natgwRules = append(natgwRules, getSpecialQoSRule(specificIPLimit, vpc2EIP.Status.IP)...)

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
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, eipLimit, true)

	ginkgo.By("Remove qos policy " + eipQoSPolicyName + " from natgw " + eipName)
	_ = eipClient.PatchQoSPolicySync(eipName, "")

	// match qos of priority 2
	ginkgo.By("Check qos to match priority 2 is limited to " + strconv.Itoa(specificIPLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, specificIPLimit, true)

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
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, true)

	ginkgo.By("Remove qos policy " + natGwQoSPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, "")

	ginkgo.By("Check qos " + natGwQoSPolicyName + " is not limited to " + strconv.Itoa(defaultNicLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, false)
	// Cleanup is now handled by DeferCleanup
}

func createNatGwAndSetQosCases(f *framework.Framework,
	vpc1Pod, vpc2Pod *corev1.Pod,
	vpc2EIP *apiv1.IptablesEIP,
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
	vpc1EIP := framework.MakeIptablesEIP(eipName, "", "", "", natgwName, attachDefName, eipQoSPolicyName)
	_ = eipClient.CreateSync(vpc1EIP)
	vpc1EIP = waitForIptablesEIPReady(eipClient, eipName, 60*time.Second)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up recreated eip " + eipName)
		eipClient.DeleteSync(eipName)
	})

	ginkgo.By("Creating fip " + fipName)
	fip := framework.MakeIptablesFIPRule(fipName, eipName, vpc1Pod.Status.PodIP)
	_ = fipClient.CreateSync(fip)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up recreated fip " + fipName)
		fipClient.DeleteSync(fipName)
	})

	ginkgo.By("Check qos " + eipQoSPolicyName + " is limited to " + strconv.Itoa(eipLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, eipLimit, true)

	ginkgo.By("Remove qos policy " + eipQoSPolicyName + " from natgw " + natgwName)
	_ = eipClient.PatchQoSPolicySync(eipName, "")

	ginkgo.By("Check qos " + natgwQoSPolicyName + " is limited to " + strconv.Itoa(defaultNicLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, true)

	ginkgo.By("Remove qos policy " + natgwQoSPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, "")

	ginkgo.By("Check qos " + natgwQoSPolicyName + " is not limited to " + strconv.Itoa(defaultNicLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, false)

	// Cleanup is now handled by DeferCleanup registered after each resource creation
}

func validRateLimit(text string, limit int) bool {
	maxValue := float64(limit) * 1024 * 1024 * 1.2
	minValue := float64(limit) * 1024 * 1024 * 0.8
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
		if v := float64(number); v >= minValue && v <= maxValue {
			return true
		}
	}
	return false
}

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
	natgwQoS := ""

	// Create VPC1 + NAT GW1
	setupVpcNatGwTestEnvironment(
		f, dockerExtNetNetwork, attachNetClient,
		subnetClient, vpcClient, vpcNatGwClient,
		vpcQosParams.vpc1Name, vpcQosParams.vpc1SubnetName, vpcQosParams.vpcNat1GwName,
		natgwQoS, overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
		dockerExtNetName, vpcQosParams.attachDefName, net1NicName,
		vpcQosParams.subnetProvider,
		true,
	)

	// Create VPC2 + NAT GW2
	setupVpcNatGwTestEnvironment(
		f, dockerExtNetNetwork, attachNetClient,
		subnetClient, vpcClient, vpcNatGwClient,
		vpcQosParams.vpc2Name, vpcQosParams.vpc2SubnetName, vpcQosParams.vpcNat2GwName,
		natgwQoS, overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
		dockerExtNetName, vpcQosParams.attachDefName, net1NicName,
		vpcQosParams.subnetProvider,
		true,
	)

	// Create vpc1 pod
	annotations1 := map[string]string{
		util.LogicalSwitchAnnotation: vpcQosParams.vpc1SubnetName,
	}
	ginkgo.By("Creating pod " + vpcQosParams.vpc1PodName)
	pod1 := framework.MakePod(f.Namespace.Name, vpcQosParams.vpc1PodName, nil, annotations1, framework.AgnhostImage, iperfServerCmd, nil)
	pod1 = podClient.CreateSync(pod1)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up pod " + vpcQosParams.vpc1PodName)
		podClient.DeleteSync(vpcQosParams.vpc1PodName)
	})

	// Create vpc1 EIP
	ginkgo.By("Creating eip " + vpcQosParams.vpc1EIPName)
	eip1 := framework.MakeIptablesEIP(vpcQosParams.vpc1EIPName, "", "", "", vpcQosParams.vpcNat1GwName, vpcQosParams.attachDefName, "")
	_ = iptablesEIPClient.CreateSync(eip1)
	eip1 = waitForIptablesEIPReady(iptablesEIPClient, vpcQosParams.vpc1EIPName, 60*time.Second)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up eip " + vpcQosParams.vpc1EIPName)
		iptablesEIPClient.DeleteSync(vpcQosParams.vpc1EIPName)
	})

	// Create vpc1 FIP
	ginkgo.By("Creating fip " + vpcQosParams.vpc1FIPName)
	fip1 := framework.MakeIptablesFIPRule(vpcQosParams.vpc1FIPName, vpcQosParams.vpc1EIPName, pod1.Status.PodIP)
	_ = iptablesFIPClient.CreateSync(fip1)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up fip " + vpcQosParams.vpc1FIPName)
		iptablesFIPClient.DeleteSync(vpcQosParams.vpc1FIPName)
	})

	// Create vpc2 pod
	annotations2 := map[string]string{
		util.LogicalSwitchAnnotation: vpcQosParams.vpc2SubnetName,
	}
	ginkgo.By("Creating pod " + vpcQosParams.vpc2PodName)
	pod2 := framework.MakePod(f.Namespace.Name, vpcQosParams.vpc2PodName, nil, annotations2, framework.AgnhostImage, iperfServerCmd, nil)
	pod2 = podClient.CreateSync(pod2)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up pod " + vpcQosParams.vpc2PodName)
		podClient.DeleteSync(vpcQosParams.vpc2PodName)
	})

	// Create vpc2 EIP
	ginkgo.By("Creating eip " + vpcQosParams.vpc2EIPName)
	eip2 := framework.MakeIptablesEIP(vpcQosParams.vpc2EIPName, "", "", "", vpcQosParams.vpcNat2GwName, vpcQosParams.attachDefName, "")
	_ = iptablesEIPClient.CreateSync(eip2)
	eip2 = waitForIptablesEIPReady(iptablesEIPClient, vpcQosParams.vpc2EIPName, 60*time.Second)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up eip " + vpcQosParams.vpc2EIPName)
		iptablesEIPClient.DeleteSync(vpcQosParams.vpc2EIPName)
	})

	// Create vpc2 FIP
	ginkgo.By("Creating fip " + vpcQosParams.vpc2FIPName)
	fip2 := framework.MakeIptablesFIPRule(vpcQosParams.vpc2FIPName, vpcQosParams.vpc2EIPName, pod2.Status.PodIP)
	_ = iptablesFIPClient.CreateSync(fip2)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Cleaning up fip " + vpcQosParams.vpc2FIPName)
		iptablesFIPClient.DeleteSync(vpcQosParams.vpc2FIPName)
	})

	return pod1, pod2, eip1, eip2
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
		// Create test-specific resource names (VPCs, pods, EIPs, etc.)
		randomSuffix := framework.RandomSuffix()
		vpcQosParams = &qosParams{
			vpc1Name:       "qos-vpc1-" + randomSuffix,
			vpc2Name:       "qos-vpc2-" + randomSuffix,
			vpc1SubnetName: "qos-vpc1-subnet-" + randomSuffix,
			vpc2SubnetName: "qos-vpc2-subnet-" + randomSuffix,
			vpcNat1GwName:  "qos-gw1-" + randomSuffix,
			vpcNat2GwName:  "qos-gw2-" + randomSuffix,
			vpc1EIPName:    "qos-vpc1-eip-" + randomSuffix,
			vpc2EIPName:    "qos-vpc2-eip-" + randomSuffix,
			vpc1FIPName:    "qos-vpc1-fip-" + randomSuffix,
			vpc2FIPName:    "qos-vpc2-fip-" + randomSuffix,
			vpc1PodName:    "qos-vpc1-pod-" + randomSuffix,
			vpc2PodName:    "qos-vpc2-pod-" + randomSuffix,
			// Use the shared attachDefName
			attachDefName:  networkAttachDefName,
			subnetProvider: externalSubnetProvider,
		}
	})

	framework.ConformanceIt("default nic qos", func() {
		// Setup resources
		vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP := setupQosTestResources(
			f, dockerExtNetNetwork, vpcQosParams, net1NicName,
		)

		// Run test
		defaultQoSCases(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, vpcQosParams.vpcNat1GwName)
		// Cleanup is handled automatically by DeferCleanup in setupQosTestResources
	})

	framework.ConformanceIt("eip qos", func() {
		// Setup resources
		vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP := setupQosTestResources(
			f, dockerExtNetNetwork, vpcQosParams, net1NicName,
		)

		// Run test
		eipQoSCases(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, vpcQosParams.vpc1EIPName, vpcQosParams.vpcNat1GwName)
		// Cleanup is handled automatically by DeferCleanup in setupQosTestResources
	})

	framework.ConformanceIt("specifying ip qos", func() {
		// Setup resources
		vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP := setupQosTestResources(
			f, dockerExtNetNetwork, vpcQosParams, net1NicName,
		)

		// Run test
		specifyingIPQoSCases(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, vpcQosParams.vpcNat1GwName)
		// Cleanup is handled automatically by DeferCleanup in setupQosTestResources
	})

	framework.ConformanceIt("qos priority matching", func() {
		// Setup resources
		vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP := setupQosTestResources(
			f, dockerExtNetNetwork, vpcQosParams, net1NicName,
		)

		// Run test
		priorityQoSCases(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, vpcQosParams.vpcNat1GwName, vpcQosParams.vpc1EIPName)
		// Cleanup is handled automatically by DeferCleanup in setupQosTestResources
	})

	framework.ConformanceIt("create resource with qos policy", func() {
		// Setup resources
		vpc1Pod, vpc2Pod, _, vpc2EIP := setupQosTestResources(
			f, dockerExtNetNetwork, vpcQosParams, net1NicName,
		)

		// Run test (this test deletes and recreates resources internally)
		lanIP := "10.0.0.254"
		createNatGwAndSetQosCases(f, vpc1Pod, vpc2Pod, vpc2EIP,
			vpcQosParams.vpcNat1GwName, vpcQosParams.vpc1EIPName, vpcQosParams.vpc1FIPName,
			vpcQosParams.vpc1Name, vpcQosParams.vpc1SubnetName, lanIP, vpcQosParams.attachDefName)
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
