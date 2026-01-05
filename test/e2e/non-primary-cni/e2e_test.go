package non_primary_cni

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	klog "k8s.io/klog/v2"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

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
	EnvKubeOVNVersion    = "VERSION"
	EnvKubeOVNRegistry   = "REGISTRY"

	DefaultNetworkInterface = "net1"
	DefaultConfigPath       = "/opt/testconfigs"
	DefaultCommandTimeout   = 30 * time.Second
	DefaultKubeOVNVersion   = "v1.15.0"
	DefaultKubeOVNRegistry  = "docker.io/kubeovn"
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

// removeFinalizers removes finalizers from Kube-OVN resources to ensure cleanup
func removeFinalizers(configStage string) {
	ginkgo.By(fmt.Sprintf("Removing finalizers from config-stage=%s resources", configStage))

	// Get all resources with the specific config-stage label
	cmd := fmt.Sprintf("kubectl get all,vpc,subnet,networkattachmentdefinitions,iptableseip,iptablessnatrule,iptablesdnatrule,vpcnatgateway,providernet,vlan -l config-stage=%s -o custom-columns=KIND:.kind,NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers 2>/dev/null || true", configStage)
	output, _ := runBashCommand(cmd)

	if strings.TrimSpace(output) == "" {
		return
	}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) >= 3 {
			kind := fields[0]
			namespace := fields[1]
			name := fields[2]

			var patchCmd string
			if namespace == "<none>" || namespace == "" {
				// Cluster-scoped resource
				patchCmd = fmt.Sprintf("kubectl patch %s %s --type=merge -p '{\"metadata\":{\"finalizers\":[]}}' 2>/dev/null || true", strings.ToLower(kind), name)
			} else {
				// Namespaced resource
				patchCmd = fmt.Sprintf("kubectl patch %s %s -n %s --type=merge -p '{\"metadata\":{\"finalizers\":[]}}' 2>/dev/null || true", strings.ToLower(kind), name, namespace)
			}
			_, _ = runBashCommand(patchCmd)
		}
	}
}

// getKubeOVNVersion dynamically determines the KubeOVN version
func getKubeOVNVersion() string {
	if version := os.Getenv(EnvKubeOVNVersion); version != "" {
		return version
	}
	if versionFromFile := readVersionFile(); versionFromFile != "" {
		return versionFromFile
	}
	return DefaultKubeOVNVersion
}

// getKubeOVNRegistry dynamically determines the KubeOVN registry
func getKubeOVNRegistry() string {
	if registry := os.Getenv(EnvKubeOVNRegistry); registry != "" {
		return registry
	}
	return DefaultKubeOVNRegistry
}

// readVersionFile reads version from VERSION file
func readVersionFile() string {
	// Try multiple possible locations for VERSION file
	possiblePaths := []string{
		"VERSION",
		"../../../VERSION",
		"/tmp/kube-ovn/VERSION",
	}

	for _, path := range possiblePaths {
		if content, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(content))
		}
	}
	return ""
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
		return nil, fmt.Errorf("failed to inspect KIND network: %w", err)
	}

	for _, config := range network.IPAM.Config {
		if config.Subnet.IsValid() && util.CheckProtocol(config.Subnet.String()) == kubeovnv1.ProtocolIPv4 {
			ginkgo.By(fmt.Sprintf("Detected KIND bridge network: CIDR=%s, Gateway=%s", config.Subnet, config.Gateway))
			return &KindBridgeNetwork{CIDR: config.Subnet.String(), Gateway: config.Gateway.String()}, nil
		}
	}
	return nil, errors.New("no IPv4 subnet found in KIND network")
}

// generateExcludeIPs creates a YAML list of IPs to exclude based on the gateway
func generateExcludeIPs(_, gateway string) string {
	lastDot := strings.LastIndex(gateway, ".")
	if lastDot == -1 {
		return "- " + gateway
	}
	baseIP := gateway[:lastDot]
	var ips []string
	for i := 1; i <= 5; i++ {
		ips = append(ips, fmt.Sprintf("- %s.%d", baseIP, i))
	}
	return strings.Join(ips, "\n    ")
}

// processConfigWithKindBridge dynamically updates YAML configuration with KIND bridge network
func processConfigWithKindBridge(yamlPath string, kindNetwork *KindBridgeNetwork) (string, error) {
	ginkgo.By(fmt.Sprintf("Processing config file %s with KIND bridge network", yamlPath))

	content, err := os.ReadFile(yamlPath)
	if err != nil {
		return "", fmt.Errorf("failed to read config file: %w", err)
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
	registry := getKubeOVNRegistry()
	version := getKubeOVNVersion()
	vpcNatGatewayImage := fmt.Sprintf("%s/vpc-nat-gateway:%s", registry, version)

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
		"<kube-ovn-vpc-nat-image-name>":          vpcNatGatewayImage,
	}

	for placeholder, value := range templateReplacements {
		yamlContent = strings.ReplaceAll(yamlContent, placeholder, value)
	}

	// Create temporary file with updated configuration
	tmpFile, err := os.CreateTemp(os.TempDir(), "kind-config-*.yaml")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary config file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(yamlContent); err != nil {
		return "", fmt.Errorf("failed to write updated config: %w", err)
	}

	ginkgo.By(fmt.Sprintf("Created dynamic config file: %s", tmpFile.Name()))
	return tmpFile.Name(), nil
}

// Helper function to wait for resource to be ready with Eventually
func waitForResourceReady(name string, getFunc func(string) any, readyFunc func(any) bool) {
	gomega.Eventually(func() bool {
		resource := getFunc(name)
		if resource == nil {
			return false
		}
		return readyFunc(resource)
	}, 60*time.Second, 2*time.Second).Should(gomega.BeTrue(), fmt.Sprintf("Resource %s should be ready", name))
}

// Helper function to get pod IPs (primary or non-primary)
func getPodIPs(pod *corev1.Pod) []string {
	ginkgo.GinkgoHelper()

	if isKubeOVNPrimaryCNI() {
		return util.PodIPs(*pod)
	}
	return getPodNonPrimaryIP(pod)
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

			for stage := 0; stage <= 1; stage++ {
				ginkgo.By(fmt.Sprintf("Apply YAML with config-stage=%d", stage))
				cmd := fmt.Sprintf("kubectl apply -f %s --prune -l config-stage=%d", yamlFile, stage)
				output, err := runBashCommand(cmd)
				framework.ExpectNoError(err, "Failed to apply stage %d config: %s", stage, output)
				if stage == 0 {
					time.Sleep(5 * time.Second)
				}
			}
		})

		ginkgo.AfterEach(func() {
			ginkgo.By("Cleanup resources")
			for stage := 1; stage >= 0; stage-- {
				removeFinalizers(strconv.Itoa(stage))
				cmd := fmt.Sprintf("kubectl delete -f %s --ignore-not-found=true -l config-stage=%d --timeout=30s", yamlFile, stage)
				_, _ = runBashCommand(cmd)
			}
		})

		ginkgo.It("Should create pods and test connectivity in VPC", func() {
			ginkgo.By("Wait for pods to be ready")
			for _, podName := range podNames {
				pod := podClient.GetPod(podName)
				podClient.WaitForRunning(pod.Name)
			}

			ginkgo.By("Test connectivity between pods")
			pod1 := podClient.GetPod(podNames[0])
			pod2 := podClient.GetPod(podNames[1])

			// Get pod IPs
			pod1IPs := getPodIPs(pod1)
			pod2IPs := getPodIPs(pod2)

			framework.ExpectNotEmpty(pod1IPs, "Pod1 should have at least one IP address")
			framework.ExpectNotEmpty(pod2IPs, "Pod2 should have at least one IP address")

			pod1IP := strings.Join(pod1IPs, ",")
			for _, pod2IP := range pod2IPs {
				description := fmt.Sprintf("from %s (%s) to %s (%s)", pod1.Name, pod1IP, pod2.Name, pod2IP)
				err := testPodConnectivity(pod1, pod2IP, description)
				framework.ExpectNoError(err, "Ping should succeed between pods in VPC")
			}
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
		natGwName := "vpc-nat-gw-gateway"
		podNames := []string{"vpc-nat-gw-pod1", "vpc-nat-gw-pod2"}
		originalYamlFile := getTestConfigFile("VPC/01-vpc-nat-gw.yaml")
		var yamlFile string
		var ipTablesEipClient *framework.IptablesEIPClient
		var ipTablesSnatRuleClient *framework.IptablesSnatClient
		var ipTablesDnatRuleClient *framework.IptablesDnatClient
		var natGwClient *framework.VpcNatGatewayClient
		var podClient *framework.PodClient
		var eipObjs []*kubeovnv1.IptablesEIP
		var snatObjs []*kubeovnv1.IptablesSnatRule
		var dnatObjs []*kubeovnv1.IptablesDnatRule
		var podObjs []*corev1.Pod

		ginkgo.BeforeEach(func() {
			ginkgo.By("Initialize clients")
			podClient = f.PodClientNS(namespaceName)
			ipTablesEipClient = f.IptablesEIPClient()
			ipTablesSnatRuleClient = f.IptablesSnatClient()
			ipTablesDnatRuleClient = f.IptablesDnatClient()
			natGwClient = f.VpcNatGatewayClient()

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

			stages := []struct {
				stage int
				sleep time.Duration
			}{
				{0, 5 * time.Second}, {1, 10 * time.Second}, {2, 0}, {3, 0},
			}
			for _, s := range stages {
				ginkgo.By(fmt.Sprintf("Apply YAML with config-stage=%d", s.stage))
				cmd := fmt.Sprintf("kubectl apply -f %s --prune -l config-stage=%d", yamlFile, s.stage)
				output, err := runBashCommand(cmd)
				framework.ExpectNoError(err, "Failed to apply stage %d config: %s", s.stage, output)
				if s.sleep > 0 {
					time.Sleep(s.sleep)
				}
			}
		})

		ginkgo.AfterEach(func() {
			ginkgo.By("Cleanup resources")
			for stage := 3; stage >= 0; stage-- {
				removeFinalizers(strconv.Itoa(stage))
				cmd := fmt.Sprintf("kubectl delete -f %s --ignore-not-found=true -l config-stage=%d --timeout=30s", yamlFile, stage)
				_, _ = runBashCommand(cmd)
			}
		})

		ginkgo.It("Should create VPC NAT Gateway and test SNAT/DNAT", func() {
			ginkgo.By("Verify NAT Gateway is ready")
			natGw := natGwClient.Get(natGwName)
			framework.ExpectNotNil(natGw, "NAT Gateway should exist")

			ginkgo.By("Verify EIPs are created")
			for _, eipName := range eipNames {
				waitForResourceReady(eipName,
					func(name string) any { return ipTablesEipClient.Get(name) },
					func(r any) bool {
						if eip, ok := r.(*kubeovnv1.IptablesEIP); ok {
							return eip.Status.Ready && eip.Status.IP != ""
						}
						return false
					})
				eip := ipTablesEipClient.Get(eipName)
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
				waitForResourceReady(snatName,
					func(name string) any { return ipTablesSnatRuleClient.Get(name) },
					func(r any) bool {
						if snat, ok := r.(*kubeovnv1.IptablesSnatRule); ok {
							return snat.Status.Ready
						}
						return false
					})
				snat := ipTablesSnatRuleClient.Get(snatName)
				snatObjs = append(snatObjs, snat)
			}
			snatList, err := ipTablesSnatRuleClient.List(context.Background(), metav1.ListOptions{})
			if err != nil {
				framework.Failf("Failed to list SNAT rules: %v", err)
			}
			for i, snatRule := range snatList.Items {
				ginkgo.By(fmt.Sprintf("Testing SNAT rule %s", snatRule.Name))

				// Get the EIP associated with this SNAT rule
				var snatEip *kubeovnv1.IptablesEIP
				for _, eip := range eipObjs {
					if eip.Name == snatRule.Spec.EIP {
						snatEip = eip
						break
					}
				}
				framework.ExpectNotEmpty(snatEip.Status.IP, "EIP should have an IP address for SNAT testing")

				// Get source pod IP for SNAT
				sourcePodIPs := getPodIPs(podObjs[i])
				sourcePodIP := strings.Join(sourcePodIPs, ",")
				framework.ExpectNotEmpty(sourcePodIPs, "Source pod should have an IP address for SNAT testing")

				ginkgo.By(fmt.Sprintf("Verifying SNAT mapping from pod %s (%s) to EIP %s",
					podObjs[i].Name, sourcePodIP, snatEip.Status.IP))
				// We do not test the actual packet forwarding here, just the rule configuration
				// The actual packet forwarding is not tested since it needs done from outside the cluster
				// Use helper function to verify SNAT rule configuration
				verifySNATRule(&snatRule, sourcePodIP, snatEip)
				ginkgo.By(fmt.Sprintf("SNAT rule %s properly configured: Internal=%s -> EIP=%s",
					snatRule.Name, snatRule.Spec.InternalCIDR, snatRule.Spec.EIP))
			}
			ginkgo.By("Verify DNAT rules")
			for _, dnatName := range dnatNames {
				waitForResourceReady(dnatName,
					func(name string) any { return ipTablesDnatRuleClient.Get(name) },
					func(r any) bool {
						if dnat, ok := r.(*kubeovnv1.IptablesDnatRule); ok {
							return dnat.Status.Ready
						}
						return false
					})
				dnat := ipTablesDnatRuleClient.Get(dnatName)
				dnatObjs = append(dnatObjs, dnat)
			}
			dnatList, err := ipTablesDnatRuleClient.List(context.Background(), metav1.ListOptions{})
			framework.ExpectNoError(err, "Failed to list DNAT rules")
			for i, dnatRule := range dnatList.Items {
				ginkgo.By(fmt.Sprintf("Testing DNAT rule %s", dnatRule.Name))

				// Get the EIP associated with this SNAT rule
				var dnatEip *kubeovnv1.IptablesEIP
				for _, eip := range eipObjs {
					if eip.Name == dnatRule.Spec.EIP {
						dnatEip = eip
						break
					}
				}
				framework.ExpectNotEmpty(dnatEip.Status.IP, "EIP should have an IP address for DNAT testing")

				// Get target pod IP for DNAT
				targetPodIPs := getPodIPs(podObjs[i])
				framework.ExpectNotEmpty(targetPodIPs, "Target pod should have an IP address for DNAT testing")

				for _, targetPodIP := range targetPodIPs {
					if util.CheckProtocol(targetPodIP) != util.CheckProtocol(dnatEip.Status.IP) {
						continue
					}
					ginkgo.By(fmt.Sprintf("Verifying DNAT mapping from EIP %s to pod %s (%s)",
						dnatEip.Status.IP, podObjs[i].Name, targetPodIP))
					// We do not test the actual packet forwarding here, just the rule configuration
					// The actual packet forwarding is not tested since it needs done from outside the cluster
					// Use helper function to verify DNAT rule configuration
					verifyDNATRule(&dnatRule, targetPodIP, dnatEip)
					ginkgo.By(fmt.Sprintf("DNAT rule %s properly configured: EIP=%s -> Internal=%s",
						dnatRule.Name, dnatRule.Spec.EIP, dnatRule.Spec.InternalIP))
				}
			}

			ginkgo.By("Test pod-to-pod connectivity within VPC")
			// Test connectivity between pods in the same VPC
			if len(podObjs) >= 2 {
				pod1 := podObjs[0]
				pod2 := podObjs[1]

				pod1IPs := getPodIPs(pod1)
				pod2IPs := getPodIPs(pod2)

				framework.ExpectNotEmpty(pod1IPs, "Pod1 should have at lease one IP address")
				framework.ExpectNotEmpty(pod2IPs, "Pod2 should have at lease one IP address")

				pod1IP := strings.Join(pod1IPs, ",")
				pod2IP := strings.Join(pod2IPs, ",")
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

			for stage := 0; stage <= 1; stage++ {
				ginkgo.By(fmt.Sprintf("Apply YAML with config-stage=%d", stage))
				cmd := fmt.Sprintf("kubectl apply -f %s --prune -l config-stage=%d", yamlFile, stage)
				output, err := runBashCommand(cmd)
				framework.ExpectNoError(err, "Failed to apply stage %d config: %s", stage, output)
				if stage == 0 {
					time.Sleep(1 * time.Second)
				} else {
					time.Sleep(10 * time.Second)
				}
			}
		})

		ginkgo.AfterEach(func() {
			ginkgo.By("Cleanup resources")
			for stage := 1; stage >= 0; stage-- {
				removeFinalizers(strconv.Itoa(stage))
				cmd := fmt.Sprintf("kubectl delete -f %s --ignore-not-found=true -l config-stage=%d --timeout=30s", yamlFile, stage)
				_, _ = runBashCommand(cmd)
			}
		})

		ginkgo.It("Should create pods and test connectivity in logical network", func() {
			ginkgo.By("Wait for pods to be ready")
			for _, podName := range podNames {
				pod := podClient.GetPod(podName)
				podClient.WaitForRunning(pod.Name)
			}

			ginkgo.By("Test connectivity between pods")
			pod1 := podClient.GetPod(podNames[0])
			pod2 := podClient.GetPod(podNames[1])

			// Get pod IPs
			pod1IPs := getPodIPs(pod1)
			pod2IPs := getPodIPs(pod2)
			framework.ExpectNotEmpty(pod1IPs, "Pod1 should have at lease one IP address")
			framework.ExpectNotEmpty(pod2IPs, "Pod2 should have at lease one IP address")

			pod1IP := strings.Join(pod1IPs, ",")
			for _, pod2IP := range pod2IPs {
				description := fmt.Sprintf("from %s (%s) to %s (%s)", pod1.Name, pod1IP, pod2.Name, pod2IP)
				err := testPodConnectivity(pod1, pod2IP, description)
				framework.ExpectNoError(err, "Ping should succeed between pods in logical network")
			}
		})
	})
})

// Helper function to get non-primary IP from pod annotation
func getPodNonPrimaryIP(pod *corev1.Pod) []string {
	ginkgo.GinkgoHelper()

	// For non-primary CNI, look for k8s.v1.cni.cncf.io/networks annotation
	network := pod.Annotations[nadv1.NetworkAttachmentAnnot]
	if network == "" {
		return nil
	}

	ips, err := util.PodAttachmentIPs(pod, network)
	framework.ExpectNoError(err, "Failed to get pod attachment IPs for pod %s", pod.Name)
	if len(ips) != 0 {
		return ips
	}

	// For Kube-OVN non-primary CNI, the IP is stored in a specific annotation format:
	// {network-attachment-name}.{namespace}.ovn.kubernetes.io/ip_address
	// Example: vpc-simple-nad.vpc-simple-ns.ovn.kubernetes.io/ip_address: 10.100.0.2
	// Extract the network attachment definition name from the networks annotation
	// Format: namespace/nad-name (e.g., "vpc-simple-ns/vpc-simple-nad")
	// Convert namespace/nad-name to nad-name.namespace.ovn.kubernetes.io/ip_address format
	parts := strings.Split(network, "/")
	if len(parts) != 2 {
		return nil
	}
	namespace := parts[0]
	name := parts[1]

	// Construct the Kube-OVN IP annotation key
	ipAnnotationKey := fmt.Sprintf(util.IPAddressAnnotationTemplate, fmt.Sprintf("%s.%s", name, namespace))
	// Get the IP from the annotation
	ip := pod.Annotations[ipAnnotationKey]
	if ip != "" {
		return strings.Split(ip, ",")
	}

	return nil
}

// Helper function to verify DNAT rule configuration
func verifyDNATRule(dnatRule *kubeovnv1.IptablesDnatRule, expectedInternalIP string, expectedEIP *kubeovnv1.IptablesEIP) {
	framework.ExpectEqual(dnatRule.Status.Ready, true, "DNAT rule %s should be ready", dnatRule.Name)
	framework.ExpectNotEmpty(dnatRule.Spec.EIP, "DNAT rule %s should specify an EIP", dnatRule.Name)
	framework.ExpectNotEmpty(dnatRule.Spec.InternalIP, "DNAT rule %s should specify internal IP", dnatRule.Name)

	if expectedEIP != nil {
		framework.ExpectEqual(dnatRule.Spec.EIP, expectedEIP.Name, "DNAT rule %s should map correct EIP", dnatRule.Name)
	}
	if expectedInternalIP != "" {
		framework.ExpectEqual(dnatRule.Spec.InternalIP, expectedInternalIP, "DNAT rule %s should map to correct internal IP", dnatRule.Name)
	}
}

// Helper function to verify SNAT rule configuration
func verifySNATRule(snatRule *kubeovnv1.IptablesSnatRule, expectedPodIP string, expectedEIP *kubeovnv1.IptablesEIP) {
	framework.ExpectEqual(snatRule.Status.Ready, true, "SNAT rule %s should be ready", snatRule.Name)
	framework.ExpectNotEmpty(snatRule.Spec.InternalCIDR, "SNAT rule %s should specify an internal CIDR", snatRule.Name)
	framework.ExpectNotEmpty(snatRule.Spec.EIP, "SNAT rule %s should specify an EIP", snatRule.Name)

	if expectedPodIP != "" {
		internalIP := strings.Split(snatRule.Spec.InternalCIDR, "/")[0]
		framework.ExpectEqual(internalIP, expectedPodIP, "SNAT rule %s should map correct internal CIDR", snatRule.Name)
	}
	if expectedEIP != nil {
		framework.ExpectEqual(snatRule.Spec.EIP, expectedEIP.Name, "SNAT rule %s should map to correct EIP", snatRule.Name)
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
	}
	// For non-primary CNI, use specific interface
	if interfaceName == "" {
		interfaceName = DefaultNetworkInterface
	}
	cmd = fmt.Sprintf("ping -I %s -c 3 %s", interfaceName, targetIP)
	_, _, err := framework.KubectlExec(sourcePod.Namespace, sourcePod.Name, cmd)
	return err
}
