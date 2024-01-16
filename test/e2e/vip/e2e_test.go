package vip

import (
	"context"
	"flag"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"testing"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func makeOvnVip(namespaceName, name, subnet, v4ip, v6ip, vipType string) *apiv1.Vip {
	return framework.MakeVip(namespaceName, name, subnet, v4ip, v6ip, vipType)
}

func makeSecurityGroup(name string, allowSameGroupTraffic bool, ingressRules, egressRules []*apiv1.SgRule) *apiv1.SecurityGroup {
	return framework.MakeSecurityGroup(name, allowSameGroupTraffic, ingressRules, egressRules)
}

func testConnectivity(ip, namespaceName, srcPod, dstPod string, f *framework.Framework) {
	// other pods can communicate with the allow address pair pod through vip
	var addIP, delIP, command string
	switch util.CheckProtocol(ip) {
	case apiv1.ProtocolIPv4:
		addIP = fmt.Sprintf("ip addr add %s/24 dev eth0", ip)
		delIP = fmt.Sprintf("ip addr del %s/24 dev eth0", ip)
		command = fmt.Sprintf("ping -W 1 -c 1 %s", ip)
	case apiv1.ProtocolIPv6:
		addIP = fmt.Sprintf("ip addr add %s/96 dev eth0", ip)
		delIP = fmt.Sprintf("ip addr del %s/96 dev eth0", ip)
		command = fmt.Sprintf("ping6 -c 1 %s", ip)
	}
	// check srcPod ping dstPod through vip
	stdout, stderr, err := framework.ExecShellInPod(context.Background(), f, namespaceName, dstPod, addIP)
	framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", addIP, err, stderr, stdout)
	stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, srcPod, command)
	framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", command, err, stderr, stdout)
	framework.ExpectNoError(err)
	// srcPod can not ping dstPod vip when ip is deleted
	stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, dstPod, delIP)
	framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", delIP, err, stderr, stdout)
	stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, srcPod, command)
	framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", command, err, stderr, stdout)
	framework.ExpectError(err)
	// check dstPod ping srcPod through vip
	stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, srcPod, addIP)
	framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", addIP, err, stderr, stdout)
	stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, dstPod, command)
	framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", command, err, stderr, stdout)
	framework.ExpectNoError(err)
	// dstPod can not ping srcPod vip when ip is deleted
	stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, srcPod, delIP)
	framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", delIP, err, stderr, stdout)
	stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, dstPod, command)
	framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", command, err, stderr, stdout)
	framework.ExpectError(err)
}

func testVipWithSG(ip, namespaceName, allowPod, denyPod, aapPod, securityGroupName string, f *framework.Framework) {
	// check if security group working
	var sgCheck, conditions string
	switch util.CheckProtocol(ip) {
	case apiv1.ProtocolIPv4:
		sgCheck = fmt.Sprintf("ping -W 1 -c 1 %s", ip)
		conditions = fmt.Sprintf("name=ovn.sg.%s.associated.v4", strings.ReplaceAll(securityGroupName, "-", "."))
	case apiv1.ProtocolIPv6:
		sgCheck = fmt.Sprintf("ping6 -c 1 %s", ip)
		conditions = fmt.Sprintf("name=ovn.sg.%s.associated.v6", strings.ReplaceAll(securityGroupName, "-", "."))
	}
	// allowPod can ping aapPod with security group
	stdout, stderr, err := framework.ExecShellInPod(context.Background(), f, namespaceName, allowPod, sgCheck)
	framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", sgCheck, err, stderr, stdout)
	framework.ExpectNoError(err)
	// denyPod can not ping aapPod with security group
	stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, denyPod, sgCheck)
	framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", sgCheck, err, stderr, stdout)
	framework.ExpectError(err)

	ginkgo.By("Checking ovn address_set and lsp port_security")
	// address_set should have allow address pair ip
	execCmd := "kubectl ko nbctl --format=list --data=bare --no-heading --columns=addresses find address_set " + conditions
	output, err := exec.Command("bash", "-c", execCmd).CombinedOutput()
	addressSet := strings.Split(strings.ReplaceAll(string(output), "\n", ""), " ")
	framework.ExpectNoError(err)
	framework.ExpectContainElement(addressSet, ip)
	// port_security should have allow address pair IP
	execCmd = fmt.Sprintf("kubectl ko nbctl --format=list --data=bare --no-heading --columns=port_security find logical-switch-port name=%s.%s", aapPod, namespaceName)
	output, err = exec.Command("bash", "-c", execCmd).CombinedOutput()
	portSecurity := strings.Split(strings.ReplaceAll(string(output), "\n", ""), " ")
	framework.ExpectNoError(err)
	framework.ExpectContainElement(portSecurity, ip)
	// TODO: Checking allow address pair connectivity with security group
	// AAP does not work fine with security group in kind test env for now
}

var _ = framework.Describe("[group:vip]", func() {
	f := framework.NewDefaultFramework("vip")

	var cs clientset.Interface

	var vpcClient *framework.VpcClient
	var subnetClient *framework.SubnetClient
	var vipClient *framework.VipClient
	var vpc *apiv1.Vpc
	var subnet *apiv1.Subnet
	var podClient *framework.PodClient
	var securityGroupClient *framework.SecurityGroupClient
	var image, namespaceName, vpcName, subnetName, cidr string

	// test switch lb vip, which ip is in the vpc subnet cidr
	// switch lb vip use gw mac to trigger lb nat flows
	var switchLbVip1Name, switchLbVip2Name string

	// test allowed address pair vip
	var vip1Name, vip2Name, aapPodName1, aapPodName2, aapPodName3 string

	// test allowed address pair connectivity in the security group scenario
	var securityGroupName string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		vpcClient = f.VpcClient()
		subnetClient = f.SubnetClient()
		vipClient = f.VipClient()
		podClient = f.PodClient()
		securityGroupClient = f.SecurityGroupClient()
		namespaceName = f.Namespace.Name
		cidr = framework.RandomCIDR(f.ClusterIPFamily)

		// should have the same mac, which mac is the same as its vpc overlay subnet gw mac
		randomSuffix := framework.RandomSuffix()
		switchLbVip1Name = "switch-lb-vip1-" + randomSuffix
		switchLbVip2Name = "switch-lb-vip2-" + randomSuffix

		// should have different mac
		vip1Name = "vip1-" + randomSuffix
		vip2Name = "vip2-" + randomSuffix

		aapPodName1 = "pod1-" + randomSuffix
		aapPodName2 = "pod2-" + randomSuffix
		aapPodName3 = "pod3-" + randomSuffix

		securityGroupName = "sg-" + randomSuffix

		vpcName = "vpc-" + randomSuffix
		subnetName = "subnet-" + randomSuffix
		ginkgo.By("Creating vpc " + vpcName)
		vpc = framework.MakeVpc(vpcName, "", false, false, []string{namespaceName})
		vpc = vpcClient.CreateSync(vpc)
		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", vpcName, "", nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)

		if image == "" {
			image = framework.GetKubeOvnImage(cs)
		}
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting switch lb vip " + switchLbVip1Name)
		vipClient.DeleteSync(switchLbVip1Name)
		ginkgo.By("Deleting switch lb vip " + switchLbVip2Name)
		vipClient.DeleteSync(switchLbVip2Name)
		ginkgo.By("Deleting allowed address pair vip " + vip1Name)
		vipClient.DeleteSync(vip1Name)
		ginkgo.By("Deleting allowed address pair vip " + vip2Name)
		vipClient.DeleteSync(vip2Name)

		// clean fip pod
		ginkgo.By("Deleting pod " + aapPodName1)
		podClient.DeleteSync(aapPodName1)
		ginkgo.By("Deleting pod " + aapPodName2)
		podClient.DeleteSync(aapPodName2)
		ginkgo.By("Deleting pod " + aapPodName3)
		podClient.DeleteSync(aapPodName3)
		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
		ginkgo.By("Deleting vpc " + vpcName)
		vpcClient.DeleteSync(vpcName)
		// clean security group
		ginkgo.By("Deleting security group " + securityGroupName)
		securityGroupClient.DeleteSync(securityGroupName)
	})

	framework.ConformanceIt("Test vip", func() {
		f.SkipVersionPriorTo(1, 13, "This feature was introduced in v1.13")

		ginkgo.By("1. Test allowed address pair vip")
		// create vip1 and vip2, should have different ip and mac
		ginkgo.By("Creating allowed address pair vip, should have different ip and mac")
		ginkgo.By("Creating allowed address pair vip " + vip1Name)
		vip1 := makeOvnVip(namespaceName, vip1Name, subnetName, "", "", "")
		vip1 = vipClient.CreateSync(vip1)
		ginkgo.By("Creating allowed address pair vip " + vip2Name)
		vip2 := makeOvnVip(namespaceName, vip2Name, subnetName, "", "", "")
		vip2 = vipClient.CreateSync(vip2)
		virtualIP1 := util.GetStringIP(vip1.Status.V4ip, vip1.Status.V6ip)
		virtualIP2 := util.GetStringIP(vip2.Status.V4ip, vip2.Status.V6ip)
		framework.ExpectNotEqual(virtualIP1, virtualIP2)
		framework.ExpectNotEqual(vip1.Status.Mac, vip2.Status.Mac)

		annotations := map[string]string{util.AAPsAnnotation: vip1Name}
		cmd := []string{"sh", "-c", "sleep infinity"}
		ginkgo.By("Creating pod1 support allowed address pair using " + vip1Name)
		aapPod1 := framework.MakeNetAdminPod(namespaceName, aapPodName1, nil, annotations, image, cmd, nil)
		aapPod1 = podClient.CreateSync(aapPod1)
		ginkgo.By("Creating pod2 support allowed address pair using " + vip1Name)
		aapPod2 := framework.MakeNetAdminPod(namespaceName, aapPodName2, nil, annotations, image, cmd, nil)
		_ = podClient.CreateSync(aapPod2)
		// logical switch port with type virtual should be created
		conditions := fmt.Sprintf("type=virtual name=%s options:virtual-ip=\\\"%s\\\" ", vip1Name, virtualIP1)
		execCmd := "kubectl ko nbctl --format=list --data=bare --no-heading --columns=options find logical-switch-port " + conditions
		framework.Logf("exec cmd %s", execCmd)
		output, err := exec.Command("bash", "-c", execCmd).CombinedOutput()
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(strings.TrimSpace(string(output)))
		// virtual parents should be set correctlly
		pairs := strings.Split(string(output), " ")
		options := make(map[string]string)
		for _, pair := range pairs {
			keyValue := strings.Split(pair, "=")
			if len(keyValue) == 2 {
				options[keyValue[0]] = strings.ReplaceAll(keyValue[1], "\n", "")
			}
		}
		virtualParents := strings.Split(options["virtual-parents"], ",")
		sort.Strings(virtualParents)
		expectVirtualParents := []string{fmt.Sprintf("%s.%s", aapPodName1, namespaceName), fmt.Sprintf("%s.%s", aapPodName2, namespaceName)}
		sort.Strings(expectVirtualParents)
		framework.ExpectEqual(expectVirtualParents, virtualParents)

		ginkgo.By("Test allow address pair connectivity")
		if f.HasIPv4() {
			ginkgo.By("Test pod ping allow address pair " + vip1.Status.V4ip)
			testConnectivity(vip1.Status.V4ip, namespaceName, aapPodName2, aapPodName1, f)
		}
		if f.HasIPv6() {
			ginkgo.By("Test pod ping allow address pair " + vip1.Status.V6ip)
			testConnectivity(vip1.Status.V6ip, namespaceName, aapPodName2, aapPodName1, f)
		}

		ginkgo.By("3. Test vip with security group")
		ginkgo.By("Creating security group " + securityGroupName)
		gatewayV4, gatewayV6 := util.SplitStringIP(aapPod1.Annotations[util.GatewayAnnotation])
		allowAddressV4, allowAddressV6 := util.SplitStringIP(aapPod1.Annotations[util.IPAddressAnnotation])
		rules := make([]*apiv1.SgRule, 0)
		if f.HasIPv4() {
			// gateway should be added for pinger
			rules = append(rules, &apiv1.SgRule{
				IPVersion:     "ipv4",
				Protocol:      apiv1.ProtocolALL,
				Priority:      1,
				RemoteType:    apiv1.SgRemoteTypeAddress,
				RemoteAddress: gatewayV4,
				Policy:        apiv1.PolicyAllow,
			})
			// aapPod1 should be allowed by aapPod3 for security group allow address pair test
			rules = append(rules, &apiv1.SgRule{
				IPVersion:     "ipv4",
				Protocol:      apiv1.ProtocolALL,
				Priority:      1,
				RemoteType:    apiv1.SgRemoteTypeAddress,
				RemoteAddress: allowAddressV4,
				Policy:        apiv1.PolicyAllow,
			})
		}
		if f.HasIPv6() {
			// gateway should be added for pinger
			rules = append(rules, &apiv1.SgRule{
				IPVersion:     "ipv6",
				Protocol:      apiv1.ProtocolALL,
				Priority:      1,
				RemoteType:    apiv1.SgRemoteTypeAddress,
				RemoteAddress: gatewayV6,
				Policy:        apiv1.PolicyAllow,
			})
			// aapPod1 should be allowed by aapPod3 for security group allow address pair test
			rules = append(rules, &apiv1.SgRule{
				IPVersion:     "ipv6",
				Protocol:      apiv1.ProtocolALL,
				Priority:      1,
				RemoteType:    apiv1.SgRemoteTypeAddress,
				RemoteAddress: allowAddressV6,
				Policy:        apiv1.PolicyAllow,
			})
		}
		sg := makeSecurityGroup(securityGroupName, true, rules, rules)
		_ = securityGroupClient.CreateSync(sg)

		ginkgo.By("Creating pod3 support allowed address pair with security group")
		annotations[util.PortSecurityAnnotation] = "true"
		annotations[fmt.Sprintf(util.SecurityGroupAnnotationTemplate, "ovn")] = securityGroupName
		aapPod3 := framework.MakeNetAdminPod(namespaceName, aapPodName3, nil, annotations, image, cmd, nil)
		aapPod3 = podClient.CreateSync(aapPod3)
		v4ip, v6ip := util.SplitStringIP(aapPod3.Annotations[util.IPAddressAnnotation])
		if f.HasIPv4() {
			ginkgo.By("Test allow address pair with security group for ipv4")
			testVipWithSG(v4ip, namespaceName, aapPodName1, aapPodName2, aapPodName3, securityGroupName, f)
		}
		if f.HasIPv6() {
			ginkgo.By("Test allow address pair with security group for ipv6")
			testVipWithSG(v6ip, namespaceName, aapPodName1, aapPodName2, aapPodName3, securityGroupName, f)
		}

		ginkgo.By("3. Test switch lb vip")
		ginkgo.By("Creating two arp proxy vips, should have the same mac which is from gw subnet mac")
		ginkgo.By("Creating arp proxy switch lb vip " + switchLbVip1Name)
		switchLbVip1 := makeOvnVip(namespaceName, switchLbVip1Name, subnetName, "", "", util.SwitchLBRuleVip)
		switchLbVip1 = vipClient.CreateSync(switchLbVip1)
		ginkgo.By("Creating arp proxy switch lb vip " + switchLbVip2Name)
		switchLbVip2 := makeOvnVip(namespaceName, switchLbVip2Name, subnetName, "", "", util.SwitchLBRuleVip)
		switchLbVip2 = vipClient.CreateSync(switchLbVip2)
		// arp proxy vip only used in switch lb rule, the lb vip use the subnet gw mac to use lb nat flow
		framework.ExpectEqual(switchLbVip1.Status.Mac, switchLbVip2.Status.Mac)
		if vip1.Status.V4ip != "" {
			framework.ExpectNotEqual(vip1.Status.V4ip, vip2.Status.V4ip)
		} else {
			framework.ExpectNotEqual(vip1.Status.V6ip, vip2.Status.V6ip)
		}
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
