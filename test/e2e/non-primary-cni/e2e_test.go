package non_primary_cni

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	klog "k8s.io/klog/v2"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
	// Note: environment validation will happen during test execution

	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "kube-ovn non-primary cni e2e suite")
}

// Constants for test configuration
const (
	EnvTestConfigPath    = "TEST_CONFIG_PATH"
	EnvKubeOVNPrimaryCNI = "KUBE_OVN_PRIMARY_CNI"

	DefaultNetworkInterface = "net1"
	DefaultConfigPath       = "/opt/testconfigs"
	DefaultCommandTimeout   = 30 * time.Second
)

// Helper functions
func getTestConfigFile(relativePath string) string {
	testConfigPath := os.Getenv(EnvTestConfigPath)
	if testConfigPath == "" {
		testConfigPath = DefaultConfigPath
	}
	return filepath.Join(testConfigPath, relativePath)
}

func runBashCommand(command string) (string, error) {
	cmd := exec.Command("bash", "-c", command)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func isKubeOVNPrimaryCNI() bool {
	return os.Getenv(EnvKubeOVNPrimaryCNI) == "true"
}

// KindBridgeNetwork represents KIND bridge network configuration
type KindBridgeNetwork struct {
	CIDR    string
	Gateway string
}

// detectKindBridgeNetwork dynamically detects KIND bridge network configuration
func detectKindBridgeNetwork() (*KindBridgeNetwork, error) {
	ginkgo.By("Detecting KIND bridge network configuration")

	network, err := docker.NetworkInspect(kind.NetworkName)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect KIND network: %v", err)
	}

	var cidr, gateway string
	for _, config := range network.IPAM.Config {
		if config.Subnet != "" {
			switch util.CheckProtocol(config.Subnet) {
			case apiv1.ProtocolIPv4:
				cidr = config.Subnet
				gateway = config.Gateway
			}
		}
	}

	if cidr == "" {
		return nil, fmt.Errorf("no IPv4 subnet found in KIND network")
	}

	ginkgo.By(fmt.Sprintf("Detected KIND bridge network: CIDR=%s, Gateway=%s", cidr, gateway))
	return &KindBridgeNetwork{
		CIDR:    cidr,
		Gateway: gateway,
	}, nil
}

// generateExcludeIPs creates a YAML list of IPs to exclude based on the gateway
func generateExcludeIPs(cidr, gateway string) string {
	// Extract the base IP from gateway (e.g., "172.19.0.1" -> "172.19.0")
	lastDot := strings.LastIndex(gateway, ".")
	if lastDot == -1 {
		return "- " + gateway // fallback
	}
	baseIP := gateway[:lastDot]

	excludeIPs := []string{
		"- " + baseIP + ".1",
		"- " + baseIP + ".2",
		"- " + baseIP + ".3",
		"- " + baseIP + ".4",
		"- " + baseIP + ".5",
	}

	return strings.Join(excludeIPs, "\n    ")
}

// createEth1Interfaces adds eth1 interfaces to KIND cluster nodes
func createEth1Interfaces() error {
	ginkgo.By("Creating eth1 interfaces on KIND cluster nodes")

	// Get KIND network bridge name
	network, err := docker.NetworkInspect(kind.NetworkName)
	if err != nil {
		return fmt.Errorf("failed to inspect KIND network: %v", err)
	}

	bridgeName := "br-" + network.ID[:12]
	ginkgo.By(fmt.Sprintf("Using KIND network bridge: %s", bridgeName))

	// Get all KIND nodes
	nodes, err := kind.ListNodes("kind", "")
	if err != nil {
		return fmt.Errorf("failed to get KIND nodes: %v", err)
	}

	for _, node := range nodes {
		nodeName := node.Name()
		ginkgo.By(fmt.Sprintf("Adding eth1 interface to node: %s", nodeName))

		// Get container PID
		pidCmd := fmt.Sprintf("docker inspect -f '{{.State.Pid}}' %s", nodeName)
		pidOutput, err := runBashCommand(pidCmd)
		if err != nil {
			return fmt.Errorf("failed to get container PID for node %s: %v", nodeName, err)
		}
		containerPID := strings.TrimSpace(pidOutput)

		// Create veth pair
		vethHost := fmt.Sprintf("veth_%s_eth1", nodeName[len(nodeName)-4:]) // Use last 4 chars of node name
		createVethCmd := fmt.Sprintf("ip link add %s type veth peer name eth1", vethHost)
		if _, err := runBashCommand(createVethCmd); err != nil {
			return fmt.Errorf("failed to create veth pair for node %s: %v", nodeName, err)
		}

		// Connect host veth to bridge
		attachToBridgeCmd := fmt.Sprintf("ip link set %s master %s", vethHost, bridgeName)
		if _, err := runBashCommand(attachToBridgeCmd); err != nil {
			return fmt.Errorf("failed to attach %s to bridge %s: %v", vethHost, bridgeName, err)
		}

		// Bring up host veth
		upHostVethCmd := fmt.Sprintf("ip link set %s up", vethHost)
		if _, err := runBashCommand(upHostVethCmd); err != nil {
			return fmt.Errorf("failed to bring up %s: %v", vethHost, err)
		}

		// Move eth1 into container namespace
		moveToNsCmd := fmt.Sprintf("ip link set eth1 netns %s", containerPID)
		if _, err := runBashCommand(moveToNsCmd); err != nil {
			return fmt.Errorf("failed to move eth1 to container %s: %v", nodeName, err)
		}

		// Bring up eth1 inside container
		upEth1Cmd := fmt.Sprintf("nsenter -t %s -n ip link set eth1 up", containerPID)
		if _, err := runBashCommand(upEth1Cmd); err != nil {
			return fmt.Errorf("failed to bring up eth1 in container %s: %v", nodeName, err)
		}

		ginkgo.By(fmt.Sprintf("Successfully added eth1 interface to node: %s", nodeName))
	}

	return nil
}

// processConfigWithKindBridge dynamically updates YAML configuration with KIND bridge network
func processConfigWithKindBridge(yamlPath string, kindNetwork *KindBridgeNetwork) (string, error) {
	ginkgo.By(fmt.Sprintf("Processing config file %s with KIND bridge network", yamlPath))

	content, err := ioutil.ReadFile(yamlPath)
	if err != nil {
		return "", fmt.Errorf("failed to read config file: %v", err)
	}

	yamlContent := string(content)

	// Replace common bridge network CIDRs with actual KIND bridge CIDR
	bridgeCIDRs := []string{"172.17.0.0/16", "172.18.0.0/16", "172.19.0.0/16", "172.20.0.0/16"}
	bridgeGateways := []string{"172.17.0.1", "172.18.0.1", "172.19.0.1", "172.20.0.1"}

	for _, cidr := range bridgeCIDRs {
		yamlContent = strings.ReplaceAll(yamlContent, cidr, kindNetwork.CIDR)
	}

	for _, gw := range bridgeGateways {
		yamlContent = strings.ReplaceAll(yamlContent, gw, kindNetwork.Gateway)
	}

	// Replace template placeholders with KIND bridge network values
	templateReplacements := map[string]string{
		"<cidrBlock01>":                          kindNetwork.CIDR,
		"<gateway01>":                            kindNetwork.Gateway,
		"<00-lnet-simple-subnet1-cidr>":          kindNetwork.CIDR,
		"<00-lnet-simple-subnet1-gateway>":       kindNetwork.Gateway,
		"<00-vnet-nat-gw-ext-subnet-exclude-ip>": generateExcludeIPs(kindNetwork.CIDR, kindNetwork.Gateway),
		"<00-lnet-simple-subnet1-exclude-ip>":    generateExcludeIPs(kindNetwork.CIDR, kindNetwork.Gateway),
		"<vpc-nat-gw-ext-cidr>":                  kindNetwork.CIDR,
		"<vpc-nat-gw-ext-gateway>":               kindNetwork.Gateway,
		"<vpc-nat-gw-ext-exclude-ip>":            generateExcludeIPs(kindNetwork.CIDR, kindNetwork.Gateway),
		"<kube-ovn-vpc-nat-image-name>":          "kubeovn/vpc-nat-gateway:v1.12.0",
	}

	for placeholder, value := range templateReplacements {
		yamlContent = strings.ReplaceAll(yamlContent, placeholder, value)
	}

	// Create temporary file with updated configuration
	tmpDir := "/tmp"
	if tmpDirEnv := os.Getenv("TMPDIR"); tmpDirEnv != "" {
		tmpDir = tmpDirEnv
	}

	tmpFile, err := ioutil.TempFile(tmpDir, "kind-config-*.yaml")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary config file: %v", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		return "", fmt.Errorf("failed to write updated config: %v", err)
	}

	ginkgo.By(fmt.Sprintf("Created dynamic config file: %s", tmpFile.Name()))
	return tmpFile.Name(), nil
}

// VPC Simple Test
var _ = framework.SerialDescribe("[group:non-primary-cni]", func() {
	f := framework.NewDefaultFramework("non-primary-cni-vpc-simple")

	ginkgo.Context("VPC Simple", ginkgo.Label("Feature:VPC-Simple"), func() {
		namespaceName := "vpc-simple-ns"
		podNames := []string{"vpc-simple-pod1", "vpc-simple-pod2"}
		yamlFile := getTestConfigFile("VPC/00-vpc-simple.yaml")

		var nodeNames []string
		var cs clientset.Interface
		var podClient *framework.PodClient

		ginkgo.BeforeEach(func() {
			ginkgo.By("Initialize clients")
			cs = f.ClientSet
			podClient = f.PodClientNS(namespaceName)

			ginkgo.By("Get cluster nodes")
			nodeObjs, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
			framework.ExpectNoError(err)
			for _, node := range nodeObjs.Items {
				nodeNames = append(nodeNames, node.Name)
			}

			ginkgo.By("Apply YAML with config-stage=0")
			cmd := fmt.Sprintf("kubectl apply -f %s --prune -l config-stage=0", yamlFile)
			output, err := runBashCommand(cmd)
			if err != nil {
				framework.Failf("Failed to apply YAML config: %v, output: %s", err, output)
			}
			time.Sleep(1 * time.Second)
		})

		ginkgo.AfterEach(func() {
			ginkgo.By("Cleanup resources")
			cmd := fmt.Sprintf("kubectl delete -f %s --ignore-not-found=true", yamlFile)
			_, _ = runBashCommand(cmd)
		})

		ginkgo.It("Should create pods and test connectivity in VPC", func() {
			ginkgo.By("Apply pods with config-stage=1")
			cmd := fmt.Sprintf("kubectl apply -f %s --prune -l config-stage=1", yamlFile)
			output, err := runBashCommand(cmd)
			framework.ExpectNoError(err, "Failed to apply pods: %s", output)

			ginkgo.By("Wait for pods to be ready")
			for _, podName := range podNames {
				pod := podClient.GetPod(podName)
				podClient.WaitForRunning(pod.Name)
			}

			ginkgo.By("Test connectivity between pods")
			pod1 := podClient.GetPod(podNames[0])
			pod2 := podClient.GetPod(podNames[1])

			// Get pod IPs
			var pod1IP, pod2IP string
			if isKubeOVNPrimaryCNI() {
				pod1IP = pod1.Status.PodIP
				pod2IP = pod2.Status.PodIP
			} else {
				// For non-primary CNI, get IP from network attachment annotation
				pod1IP = getPodNonPrimaryIP(pod1, DefaultNetworkInterface)
				pod2IP = getPodNonPrimaryIP(pod2, DefaultNetworkInterface)
			}

			framework.ExpectNotEmpty(pod1IP, "Pod1 should have an IP address")
			framework.ExpectNotEmpty(pod2IP, "Pod2 should have an IP address")

			description := fmt.Sprintf("from %s (%s) to %s (%s)", pod1.Name, pod1IP, pod2.Name, pod2IP)
			err = testPodConnectivity(pod1, pod2IP, description)
			framework.ExpectNoError(err, "Ping should succeed between pods in VPC")
		})
	})
})

// VPC NAT Gateway Test
var _ = framework.SerialDescribe("[group:non-primary-cni]", func() {
	f := framework.NewDefaultFramework("non-primary-cni-vpc-nat-gw")

	ginkgo.Context("VPC NAT Gateway", ginkgo.Label("Feature:VPC-NAT-Gateway"), func() {
		namespaceName := "vpc-nat-gw-ns"
		eipNames := []string{"vpc-nat-gw-eip1", "vpc-nat-gw-eip2"}
		snatNames := []string{"vpc-nat-gw-snat01", "vpc-nat-gw-snat02"}
		dnatNames := []string{"vpc-nat-gw-dnat01", "vpc-nat-gw-dnat02"}
		natGwName := "vpc-nat-gw-gw1"
		podNames := []string{"vpc-nat-gw-pod1", "vpc-nat-gw-pod2"}
		originalYamlFile := getTestConfigFile("VPC/01-vpc-nat-gw.yaml")
		var yamlFile string // Will be set dynamically
		var ipTablesEipClient *framework.IptablesEIPClient
		var ipTablesSnatRuleClient *framework.IptablesSnatClient
		var ipTablesDnatRuleClient *framework.IptablesDnatClient
		var natGwClient *framework.VpcNatGatewayClient
		var podClient *framework.PodClient
		var eipObjs []*kubeovnv1.IptablesEIP
		var podObjs []*corev1.Pod

		ginkgo.BeforeEach(func() {
			ginkgo.By("Initialize clients")
			podClient = f.PodClientNS(namespaceName)
			ipTablesEipClient = f.IptablesEIPClient()
			ipTablesSnatRuleClient = f.IptablesSnatClient()
			ipTablesDnatRuleClient = f.IptablesDnatClient()
			natGwClient = f.VpcNatGatewayClient()

			ginkgo.By("Create eth1 interfaces on KIND nodes")
			err := createEth1Interfaces()
			framework.ExpectNoError(err)

			ginkgo.By("Detect KIND bridge network and generate dynamic config")
			kindNetwork, err := detectKindBridgeNetwork()
			framework.ExpectNoError(err)

			yamlFile, err = processConfigWithKindBridge(originalYamlFile, kindNetwork)
			framework.ExpectNoError(err)
			ginkgo.DeferCleanup(func() {
				if yamlFile != originalYamlFile {
					os.Remove(yamlFile)
				}
			})

			ginkgo.By("Apply YAML with config-stage=0")
			cmd := fmt.Sprintf("kubectl apply -f %s --prune -l config-stage=0", yamlFile)
			output, err := runBashCommand(cmd)
			framework.ExpectNoError(err, "Failed to apply YAML config: %s", output)
			time.Sleep(5 * time.Second)

			ginkgo.By("Apply YAML with config-stage=1")
			cmd = fmt.Sprintf("kubectl apply -f %s --prune -l config-stage=1", yamlFile)
			output, err = runBashCommand(cmd)
			framework.ExpectNoError(err, "Failed to apply stage 1 config: %s", output)
			time.Sleep(10 * time.Second)
		})

		ginkgo.AfterEach(func() {
			ginkgo.By("Cleanup resources")
			cmd := fmt.Sprintf("kubectl delete -f %s --ignore-not-found=true", yamlFile)
			_, _ = runBashCommand(cmd)
		})

		ginkgo.It("Should create VPC NAT Gateway and test SNAT/DNAT", func() {
			ginkgo.By("Verify NAT Gateway is ready")
			natGw := natGwClient.Get(natGwName)
			framework.ExpectNotNil(natGw, "NAT Gateway should exist")

			ginkgo.By("Verify EIPs are created")
			for _, eipName := range eipNames {
				eip := ipTablesEipClient.Get(eipName)
				framework.ExpectNotNil(eip, "EIP %s should exist", eipName)
				framework.ExpectNotEmpty(eip.Status.IP, "EIP should have an IP address")
				eipObjs = append(eipObjs, eip)
			}

			ginkgo.By("Apply pods with config-stage=2")
			cmd := fmt.Sprintf("kubectl apply -f %s --prune -l config-stage=2", yamlFile)
			output, err := runBashCommand(cmd)
			framework.ExpectNoError(err, "Failed to apply pods: %s", output)

			ginkgo.By("Wait for pods to be ready")
			for _, podName := range podNames {
				pod := podClient.GetPod(podName)
				podClient.WaitForRunning(pod.Name)
				podObjs = append(podObjs, pod)
			}

			ginkgo.By("Verify SNAT rules")
			for _, snatName := range snatNames {
				snat := ipTablesSnatRuleClient.Get(snatName)
				framework.ExpectNotNil(snat, "SNAT rule %s should exist", snatName)
				framework.ExpectEqual(snat.Status.Ready, true, "SNAT rule should be ready")
			}

			ginkgo.By("Verify DNAT rules")
			for _, dnatName := range dnatNames {
				dnat := ipTablesDnatRuleClient.Get(dnatName)
				framework.ExpectNotNil(dnat, "DNAT rule %s should exist", dnatName)
				framework.ExpectEqual(dnat.Status.Ready, true, "DNAT rule should be ready")
			}

			ginkgo.By("Test SNAT - external connectivity through NAT Gateway")
			// Test SNAT - pods should be able to reach external IPs
			for _, pod := range podObjs {
				description := fmt.Sprintf("SNAT external connectivity from pod %s to 8.8.8.8", pod.Name)
				err = testPodConnectivityWithInterface(pod, "8.8.8.8", description, "net2")
				framework.ExpectNoError(err, "Pod should have external connectivity via SNAT")
			}

			ginkgo.By("Test DNAT - external access to pods through NAT Gateway")
			// Test DNAT - external traffic should be able to reach pods through EIP
			dnatList, err := ipTablesDnatRuleClient.List(context.Background(), metav1.ListOptions{})
			framework.ExpectNoError(err, "Failed to list DNAT rules")
			for i, dnatRule := range dnatList.Items {
				ginkgo.By(fmt.Sprintf("Testing DNAT rule %s", dnatRule.Name))

				// Get the EIP associated with this DNAT rule
				var eipIP string
				if len(eipObjs) > i {
					eipIP = eipObjs[i].Status.IP
				}
				framework.ExpectNotEmpty(eipIP, "EIP should have an IP address for DNAT testing")

				// Get target pod IP for DNAT
				var targetPodIP string
				if len(podObjs) > i {
					if isKubeOVNPrimaryCNI() {
						targetPodIP = podObjs[i].Status.PodIP
					} else {
						targetPodIP = getPodNonPrimaryIP(podObjs[i], DefaultNetworkInterface)
					}
				}
				framework.ExpectNotEmpty(targetPodIP, "Target pod should have an IP address for DNAT testing")

				ginkgo.By(fmt.Sprintf("Verifying DNAT mapping from EIP %s to pod %s (%s)",
					eipIP, podObjs[i].Name, targetPodIP))
				// We do not test the actual packet forwarding here, just the rule configuration
				// The actual packet forwarding is not tested since it needs done from outside the cluster
				// Use helper function to verify DNAT rule configuration
				verifyDNATRule(&dnatRule, eipIP, targetPodIP)
				ginkgo.By(fmt.Sprintf("DNAT rule %s properly configured: EIP=%s -> Internal=%s",
					dnatRule.Name, dnatRule.Spec.EIP, dnatRule.Spec.InternalIP))
			}

			ginkgo.By("Test pod-to-pod connectivity within VPC")
			// Test connectivity between pods in the same VPC
			if len(podObjs) >= 2 {
				pod1 := podObjs[0]
				pod2 := podObjs[1]

				var pod1IP, pod2IP string
				if isKubeOVNPrimaryCNI() {
					pod1IP = pod1.Status.PodIP
					pod2IP = pod2.Status.PodIP
				} else {
					pod1IP = getPodNonPrimaryIP(pod1, DefaultNetworkInterface)
					pod2IP = getPodNonPrimaryIP(pod2, DefaultNetworkInterface)
				}

				framework.ExpectNotEmpty(pod1IP, "Pod1 should have an IP address")
				framework.ExpectNotEmpty(pod2IP, "Pod2 should have an IP address")

				description := fmt.Sprintf("pod-to-pod within VPC from %s (%s) to %s (%s)",
					pod1.Name, pod1IP, pod2.Name, pod2IP)
				err = testPodConnectivity(pod1, pod2IP, description)
				framework.ExpectNoError(err, "Pods should be able to communicate within VPC")
			}
		})
	})
})

// Logical Network Simple Test
var _ = framework.SerialDescribe("[group:non-primary-cni]", func() {
	f := framework.NewDefaultFramework("non-primary-cni-lnet-simple")

	ginkgo.Context("Logical Network Simple", ginkgo.Label("Feature:LogicalNetwork-Simple"), func() {
		namespaceName := "lnet-simple-ns"
		podNames := []string{"lnet-simple-pod1", "lnet-simple-pod2"}
		originalYamlFile := getTestConfigFile("LogicalNetwork/00-lnet-simple.yaml")
		var yamlFile string // Will be set dynamically

		var nodeNames []string
		var cs clientset.Interface
		var podClient *framework.PodClient

		ginkgo.BeforeEach(func() {
			ginkgo.By("Initialize clients")
			cs = f.ClientSet
			podClient = f.PodClientNS(namespaceName)

			ginkgo.By("Get cluster nodes")
			nodeObjs, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
			framework.ExpectNoError(err)
			for _, node := range nodeObjs.Items {
				nodeNames = append(nodeNames, node.Name)
			}

			ginkgo.By("Create eth1 interfaces on KIND nodes")
			err = createEth1Interfaces()
			framework.ExpectNoError(err)

			ginkgo.By("Detect KIND bridge network and generate dynamic config")
			kindNetwork, err := detectKindBridgeNetwork()
			framework.ExpectNoError(err)

			yamlFile, err = processConfigWithKindBridge(originalYamlFile, kindNetwork)
			framework.ExpectNoError(err)
			ginkgo.DeferCleanup(func() {
				if yamlFile != originalYamlFile {
					os.Remove(yamlFile)
				}
			})

			ginkgo.By("Apply YAML with config-stage=0")
			cmd := fmt.Sprintf("kubectl apply -f %s --prune -l config-stage=0", yamlFile)
			output, err := runBashCommand(cmd)
			framework.ExpectNoError(err, "Failed to apply YAML config: %s", output)
			time.Sleep(1 * time.Second)
		})

		ginkgo.AfterEach(func() {
			ginkgo.By("Cleanup resources")
			cmd := fmt.Sprintf("kubectl delete -f %s --ignore-not-found=true", yamlFile)
			_, _ = runBashCommand(cmd)
		})

		ginkgo.It("Should create pods and test connectivity in logical network", func() {
			ginkgo.By("Apply pods with config-stage=1")
			cmd := fmt.Sprintf("kubectl apply -f %s --prune -l config-stage=1", yamlFile)
			output, err := runBashCommand(cmd)
			framework.ExpectNoError(err, "Failed to apply pods: %s", output)

			ginkgo.By("Wait for pods to be ready")
			for _, podName := range podNames {
				pod := podClient.GetPod(podName)
				podClient.WaitForRunning(pod.Name)
			}

			ginkgo.By("Test connectivity between pods")
			pod1 := podClient.GetPod(podNames[0])
			pod2 := podClient.GetPod(podNames[1])

			// Get pod IPs
			var pod1IP, pod2IP string
			if isKubeOVNPrimaryCNI() {
				pod1IP = pod1.Status.PodIP
				pod2IP = pod2.Status.PodIP
			} else {
				pod1IP = getPodNonPrimaryIP(pod1, DefaultNetworkInterface)
				pod2IP = getPodNonPrimaryIP(pod2, DefaultNetworkInterface)
			}

			framework.ExpectNotEmpty(pod1IP, "Pod1 should have an IP address")
			framework.ExpectNotEmpty(pod2IP, "Pod2 should have an IP address")

			description := fmt.Sprintf("from %s (%s) to %s (%s)", pod1.Name, pod1IP, pod2.Name, pod2IP)
			err = testPodConnectivity(pod1, pod2IP, description)
			framework.ExpectNoError(err, "Ping should succeed between pods in logical network")
		})
	})
})

// Helper function to get non-primary IP from pod annotation
func getPodNonPrimaryIP(pod *corev1.Pod, interfaceName string) string {
	// For non-primary CNI, look for network status annotation
	networkStatus := pod.Annotations["k8s.v1.cni.cncf.io/networks-status"]
	if networkStatus == "" {
		return ""
	}

	// Use provided interface name or default
	if interfaceName == "" {
		interfaceName = DefaultNetworkInterface
	}

	// Parse the network status JSON to extract IP
	// Look for interface matching the specified interface name
	lines := strings.Split(networkStatus, "\n")
	for _, line := range lines {
		if strings.Contains(line, "ips") && strings.Contains(line, interfaceName) {
			// Extract IP using simple string parsing
			start := strings.Index(line, "[\"")
			if start == -1 {
				continue
			}
			start += 2
			end := strings.Index(line[start:], "\"")
			if end != -1 {
				return line[start : start+end]
			}
		}
	}
	return ""
}

// Helper function to verify DNAT rule configuration
func verifyDNATRule(dnatRule *kubeovnv1.IptablesDnatRule, expectedEIP, expectedInternalIP string) {
	framework.ExpectEqual(dnatRule.Status.Ready, true, "DNAT rule %s should be ready", dnatRule.Name)
	framework.ExpectNotEmpty(dnatRule.Spec.EIP, "DNAT rule %s should specify an EIP", dnatRule.Name)
	framework.ExpectNotEmpty(dnatRule.Spec.InternalIP, "DNAT rule %s should specify internal IP", dnatRule.Name)

	if expectedEIP != "" {
		framework.ExpectEqual(dnatRule.Spec.EIP, expectedEIP, "DNAT rule %s should map correct EIP", dnatRule.Name)
	}
	if expectedInternalIP != "" {
		framework.ExpectEqual(dnatRule.Spec.InternalIP, expectedInternalIP, "DNAT rule %s should map to correct internal IP", dnatRule.Name)
	}
}

// Helper function to test network connectivity with proper interface handling
func testPodConnectivity(sourcePod *corev1.Pod, targetIP, description string) error {
	return testPodConnectivityWithInterface(sourcePod, targetIP, description, DefaultNetworkInterface)
}

// Helper function to test network connectivity with specified interface
func testPodConnectivityWithInterface(sourcePod *corev1.Pod, targetIP, description, interfaceName string) error {
	ginkgo.By(fmt.Sprintf("Testing connectivity: %s", description))

	var cmd string
	if isKubeOVNPrimaryCNI() {
		cmd = fmt.Sprintf("ping -c 3 %s", targetIP)
		_, _, err := framework.KubectlExec(sourcePod.Namespace, sourcePod.Name, cmd)
		return err
	} else {
		// For non-primary CNI, use specific interface
		if interfaceName == "" {
			interfaceName = DefaultNetworkInterface
		}
		cmd = fmt.Sprintf("ping -I %s -c 3 %s", interfaceName, targetIP)
		_, _, err := framework.KubectlExec(sourcePod.Namespace, sourcePod.Name, cmd)
		return err
	}
}
