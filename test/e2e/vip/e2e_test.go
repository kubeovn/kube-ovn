package vip

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func makeOvnVip(namespaceName, name, subnet, v4ip, v6ip, vipType string) *apiv1.Vip {
	return framework.MakeVip(namespaceName, name, subnet, v4ip, v6ip, vipType)
}

func makeSecurityGroup(name string, allowSameGroupTraffic bool, ingressRules, egressRules []apiv1.SecurityGroupRule) *apiv1.SecurityGroup {
	return framework.MakeSecurityGroup(name, allowSameGroupTraffic, ingressRules, egressRules)
}

func testConnectivity(ip, namespaceName, srcPod, dstPod string, f *framework.Framework) {
	ginkgo.GinkgoHelper()

	// other pods can communicate with the allow address pair pod through vip
	var addIP, delIP, command string
	switch util.CheckProtocol(ip) {
	case apiv1.ProtocolIPv4:
		addIP = fmt.Sprintf("ip addr add %s/24 dev eth0", ip)
		delIP = fmt.Sprintf("ip addr del %s/24 dev eth0", ip)
		command = "ping -W 1 -c 1 " + ip
	case apiv1.ProtocolIPv6:
		addIP = fmt.Sprintf("ip addr add %s/96 dev eth0", ip)
		delIP = fmt.Sprintf("ip addr del %s/96 dev eth0", ip)
		command = "ping6 -c 1 " + ip
	default:
		framework.Failf("unexpected ip address: %q", ip)
	}
	// check srcPod ping dstPod through vip
	stdout, stderr, err := framework.ExecShellInPod(context.Background(), f, namespaceName, dstPod, addIP)
	framework.ExpectNoError(err, "exec %q failed, err: %q, stderr: %q, stdout: %q", addIP, err, stderr, stdout)
	stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, srcPod, command)
	framework.ExpectNoError(err, "exec %q failed, err: %q, stderr: %q, stdout: %q", command, err, stderr, stdout)
	// srcPod can not ping dstPod vip when ip is deleted
	stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, dstPod, delIP)
	framework.ExpectNoError(err, "exec %q failed, err: %q, stderr: %q, stdout: %q", delIP, err, stderr, stdout)
	_, _, err = framework.ExecShellInPod(context.Background(), f, namespaceName, srcPod, command)
	framework.ExpectError(err)
	// check dstPod ping srcPod through vip
	stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, srcPod, addIP)
	framework.ExpectNoError(err, "exec %q failed, err: %q, stderr: %q, stdout: %q", addIP, err, stderr, stdout)
	stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, dstPod, command)
	framework.ExpectNoError(err, "exec %q failed, err: %q, stderr: %q, stdout: %q", command, err, stderr, stdout)
	// dstPod can not ping srcPod vip when ip is deleted
	stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, srcPod, delIP)
	framework.ExpectNoError(err, "exec %q failed, err: %q, stderr: %q, stdout: %q", delIP, err, stderr, stdout)
	_, _, err = framework.ExecShellInPod(context.Background(), f, namespaceName, dstPod, command)
	framework.ExpectError(err)
}

func testVipWithSG(ip, namespaceName, allowPod, denyPod, aapPod, securityGroupName string, f *framework.Framework) {
	ginkgo.GinkgoHelper()

	// check if security group working
	var sgCheck, conditions string
	switch util.CheckProtocol(ip) {
	case apiv1.ProtocolIPv4:
		sgCheck = "ping -W 1 -c 1 " + ip
		conditions = fmt.Sprintf("name=ovn.sg.%s.associated.v4", strings.ReplaceAll(securityGroupName, "-", "."))
	case apiv1.ProtocolIPv6:
		sgCheck = "ping6 -c 1 " + ip
		conditions = fmt.Sprintf("name=ovn.sg.%s.associated.v6", strings.ReplaceAll(securityGroupName, "-", "."))
	}
	// allowPod can ping aapPod with security group
	stdout, stderr, err := framework.ExecShellInPod(context.Background(), f, namespaceName, allowPod, sgCheck)
	framework.ExpectNoError(err, "exec %q failed, err: %q, stderr: %q, stdout: %q", sgCheck, err, stderr, stdout)
	// denyPod can not ping aapPod with security group
	_, _, err = framework.ExecShellInPod(context.Background(), f, namespaceName, denyPod, sgCheck)
	framework.ExpectError(err)

	ginkgo.By("Checking ovn address_set and lsp port_security")
	// address_set should have allow address pair ip
	cmd := "ovn-nbctl --format=list --data=bare --no-heading --columns=addresses find Address_Set " + conditions
	output, _, err := framework.NBExec(cmd)
	framework.ExpectNoError(err)
	addressSet := strings.Split(strings.ReplaceAll(string(output), "\n", ""), " ")
	framework.ExpectContainElement(addressSet, ip)
	// port_security should have allow address pair IP
	cmd = fmt.Sprintf("ovn-nbctl --format=list --data=bare --no-heading --columns=port_security list Logical_Switch_Port %s.%s", aapPod, namespaceName)
	output, _, err = framework.NBExec(cmd)
	framework.ExpectNoError(err)
	portSecurity := strings.Split(strings.ReplaceAll(string(output), "\n", ""), " ")
	framework.ExpectContainElement(portSecurity, ip)
	// TODO: Checking allow address pair connectivity with security group
	// AAP does not work fine with security group in kind test env for now
}

var _ = framework.Describe("[group:vip]", func() {
	f := framework.NewDefaultFramework("vip")

	var vpcClient *framework.VpcClient
	var subnetClient *framework.SubnetClient
	var vipClient *framework.VipClient
	var vpc *apiv1.Vpc
	var subnet *apiv1.Subnet
	var podClient *framework.PodClient
	var securityGroupClient *framework.SecurityGroupClient
	var namespaceName, vpcName, subnetName, cidr string

	// test switch lb vip, which ip is in the vpc subnet cidr
	// switch lb vip use gw mac to trigger lb nat flows
	var switchLbVip1Name, switchLbVip2Name string

	// test allowed address pair vip
	var countingVipName, vip1Name, vip2Name, aapPodName1, aapPodName2, aapPodName3 string

	// test ipv6 vip
	var lowerCaseStaticIpv6VipName, upperCaseStaticIpv6VipName, lowerCaseV6IP, upperCaseV6IP string

	// test allowed address pair connectivity in the security group scenario
	var securityGroupName string

	ginkgo.BeforeEach(func() {
		vpcClient = f.VpcClient()
		subnetClient = f.SubnetClient()
		vipClient = f.VipClient()
		podClient = f.PodClient()
		securityGroupClient = f.SecurityGroupClient()
		namespaceName = f.Namespace.Name
		cidr = framework.RandomCIDR(f.ClusterIPFamily)

		// should create lower case static ipv6 address vip in ovn-default
		lowerCaseStaticIpv6VipName = "lower-case-static-ipv6-vip-" + framework.RandomSuffix()
		lowerCaseV6IP = "fd00:10:16::a1"
		// should not create upper case static ipv6 address vip in ovn-default
		upperCaseStaticIpv6VipName = "Upper-Case-Static-Ipv6-Vip-" + framework.RandomSuffix()
		upperCaseV6IP = "fd00:10:16::A1"

		// should have the same mac, which mac is the same as its vpc overlay subnet gw mac
		randomSuffix := framework.RandomSuffix()
		switchLbVip1Name = "switch-lb-vip1-" + randomSuffix
		switchLbVip2Name = "switch-lb-vip2-" + randomSuffix

		// subnet status counting vip
		countingVipName = "counting-vip-" + randomSuffix

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

	framework.ConformanceIt("Test vip subnet status update with finalizer", func() {
		f.SkipVersionPriorTo(1, 13, "This feature was introduced in v1.13")

		ginkgo.By("1. Get initial subnet status")
		initialSubnet := subnetClient.Get(subnetName)
		initialV4AvailableIPs := initialSubnet.Status.V4AvailableIPs
		initialV4UsingIPs := initialSubnet.Status.V4UsingIPs
		initialV6AvailableIPs := initialSubnet.Status.V6AvailableIPs
		initialV6UsingIPs := initialSubnet.Status.V6UsingIPs
		initialV4AvailableIPRange := initialSubnet.Status.V4AvailableIPRange
		initialV4UsingIPRange := initialSubnet.Status.V4UsingIPRange
		initialV6AvailableIPRange := initialSubnet.Status.V6AvailableIPRange
		initialV6UsingIPRange := initialSubnet.Status.V6UsingIPRange

		ginkgo.By("2. Create a VIP and verify finalizer is added")
		testVipName := "test-vip-finalizer-" + framework.RandomSuffix()
		testVip := makeOvnVip(namespaceName, testVipName, subnetName, "", "", "")
		testVip = vipClient.CreateSync(testVip)

		// Verify VIP has finalizer
		framework.ExpectContainElement(testVip.Finalizers, util.KubeOVNControllerFinalizer)

		ginkgo.By("3. Wait for subnet status to be updated after VIP creation")
		time.Sleep(5 * time.Second)

		ginkgo.By("4. Verify subnet status after VIP creation")
		afterCreateSubnet := subnetClient.Get(subnetName)
		switch afterCreateSubnet.Spec.Protocol {
		case apiv1.ProtocolIPv4:
			// Verify IP count changed
			framework.ExpectEqual(initialV4AvailableIPs-1, afterCreateSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should decrease by 1 after VIP creation")
			framework.ExpectEqual(initialV4UsingIPs+1, afterCreateSubnet.Status.V4UsingIPs,
				"V4UsingIPs should increase by 1 after VIP creation")

			// Verify IP range changed
			framework.ExpectNotEqual(initialV4AvailableIPRange, afterCreateSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after VIP creation")
			framework.ExpectNotEqual(initialV4UsingIPRange, afterCreateSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after VIP creation")

			// Verify the VIP's IP is in the using range
			vipIP := testVip.Status.V4ip
			framework.ExpectTrue(strings.Contains(afterCreateSubnet.Status.V4UsingIPRange, vipIP),
				"VIP IP %s should be in V4UsingIPRange %s", vipIP, afterCreateSubnet.Status.V4UsingIPRange)
		case apiv1.ProtocolIPv6:
			// Verify IP count changed
			framework.ExpectEqual(initialV6AvailableIPs-1, afterCreateSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should decrease by 1 after VIP creation")
			framework.ExpectEqual(initialV6UsingIPs+1, afterCreateSubnet.Status.V6UsingIPs,
				"V6UsingIPs should increase by 1 after VIP creation")

			// Verify IP range changed
			framework.ExpectNotEqual(initialV6AvailableIPRange, afterCreateSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after VIP creation")
			framework.ExpectNotEqual(initialV6UsingIPRange, afterCreateSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after VIP creation")

			// Verify the VIP's IP is in the using range
			vipIP := testVip.Status.V6ip
			framework.ExpectTrue(strings.Contains(afterCreateSubnet.Status.V6UsingIPRange, vipIP),
				"VIP IP %s should be in V6UsingIPRange %s", vipIP, afterCreateSubnet.Status.V6UsingIPRange)
		default:
			// Dual stack
			framework.ExpectEqual(initialV4AvailableIPs-1, afterCreateSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should decrease by 1 after VIP creation")
			framework.ExpectEqual(initialV4UsingIPs+1, afterCreateSubnet.Status.V4UsingIPs,
				"V4UsingIPs should increase by 1 after VIP creation")
			framework.ExpectEqual(initialV6AvailableIPs-1, afterCreateSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should decrease by 1 after VIP creation")
			framework.ExpectEqual(initialV6UsingIPs+1, afterCreateSubnet.Status.V6UsingIPs,
				"V6UsingIPs should increase by 1 after VIP creation")

			framework.ExpectNotEqual(initialV4AvailableIPRange, afterCreateSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after VIP creation")
			framework.ExpectNotEqual(initialV4UsingIPRange, afterCreateSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after VIP creation")
			framework.ExpectNotEqual(initialV6AvailableIPRange, afterCreateSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after VIP creation")
			framework.ExpectNotEqual(initialV6UsingIPRange, afterCreateSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after VIP creation")
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

		ginkgo.By("5. Delete the VIP")
		vipClient.DeleteSync(testVipName)

		ginkgo.By("6. Wait for subnet status to be updated after VIP deletion")
		time.Sleep(5 * time.Second)

		ginkgo.By("7. Verify subnet status after VIP deletion")
		afterDeleteSubnet := subnetClient.Get(subnetName)
		switch afterDeleteSubnet.Spec.Protocol {
		case apiv1.ProtocolIPv4:
			// Verify IP count is restored
			framework.ExpectEqual(afterCreateV4AvailableIPs+1, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should increase by 1 after VIP deletion")
			framework.ExpectEqual(afterCreateV4UsingIPs-1, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should decrease by 1 after VIP deletion")

			// Verify IP range changed
			framework.ExpectNotEqual(afterCreateV4AvailableIPRange, afterDeleteSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after VIP deletion")
			framework.ExpectNotEqual(afterCreateV4UsingIPRange, afterDeleteSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after VIP deletion")

			// Verify counts match initial state
			framework.ExpectEqual(initialV4AvailableIPs, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should return to initial value after VIP deletion")
			framework.ExpectEqual(initialV4UsingIPs, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should return to initial value after VIP deletion")
		case apiv1.ProtocolIPv6:
			// Verify IP count is restored
			framework.ExpectEqual(afterCreateV6AvailableIPs+1, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should increase by 1 after VIP deletion")
			framework.ExpectEqual(afterCreateV6UsingIPs-1, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should decrease by 1 after VIP deletion")

			// Verify IP range changed
			framework.ExpectNotEqual(afterCreateV6AvailableIPRange, afterDeleteSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after VIP deletion")
			framework.ExpectNotEqual(afterCreateV6UsingIPRange, afterDeleteSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after VIP deletion")

			// Verify counts match initial state
			framework.ExpectEqual(initialV6AvailableIPs, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should return to initial value after VIP deletion")
			framework.ExpectEqual(initialV6UsingIPs, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should return to initial value after VIP deletion")
		default:
			// Dual stack
			framework.ExpectEqual(afterCreateV4AvailableIPs+1, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should increase by 1 after VIP deletion")
			framework.ExpectEqual(afterCreateV4UsingIPs-1, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should decrease by 1 after VIP deletion")
			framework.ExpectEqual(afterCreateV6AvailableIPs+1, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should increase by 1 after VIP deletion")
			framework.ExpectEqual(afterCreateV6UsingIPs-1, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should decrease by 1 after VIP deletion")

			framework.ExpectNotEqual(afterCreateV4AvailableIPRange, afterDeleteSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after VIP deletion")
			framework.ExpectNotEqual(afterCreateV4UsingIPRange, afterDeleteSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after VIP deletion")
			framework.ExpectNotEqual(afterCreateV6AvailableIPRange, afterDeleteSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after VIP deletion")
			framework.ExpectNotEqual(afterCreateV6UsingIPRange, afterDeleteSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after VIP deletion")

			framework.ExpectEqual(initialV4AvailableIPs, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should return to initial value after VIP deletion")
			framework.ExpectEqual(initialV4UsingIPs, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should return to initial value after VIP deletion")
			framework.ExpectEqual(initialV6AvailableIPs, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should return to initial value after VIP deletion")
			framework.ExpectEqual(initialV6UsingIPs, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should return to initial value after VIP deletion")
		}

		ginkgo.By("8. Test completed: VIP creation and deletion properly updates subnet status via finalizer handlers")
	})

	framework.ConformanceIt("Test vip", func() {
		f.SkipVersionPriorTo(1, 13, "This feature was introduced in v1.13")
		ginkgo.By("0. Test subnet status counting vip")
		oldSubnet := subnetClient.Get(subnetName)
		countingVip := makeOvnVip(namespaceName, countingVipName, subnetName, "", "", "")
		countingVip = vipClient.CreateSync(countingVip)

		// Wait for finalizer to be added
		ginkgo.By("Waiting for VIP finalizer to be added")
		for range 10 {
			countingVip = vipClient.Get(countingVipName)
			if len(countingVip.Finalizers) > 0 {
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectContainElement(countingVip.Finalizers, util.KubeOVNControllerFinalizer)

		// Wait for subnet status to be updated
		ginkgo.By("Waiting for subnet status to be updated after VIP creation")
		time.Sleep(5 * time.Second)
		newSubnet := subnetClient.Get(subnetName)
		if newSubnet.Spec.Protocol == apiv1.ProtocolIPv4 {
			framework.ExpectEqual(oldSubnet.Status.V4AvailableIPs-1, newSubnet.Status.V4AvailableIPs)
			framework.ExpectEqual(oldSubnet.Status.V4UsingIPs+1, newSubnet.Status.V4UsingIPs)
			framework.ExpectNotEqual(oldSubnet.Status.V4AvailableIPRange, newSubnet.Status.V4AvailableIPRange)
			framework.ExpectNotEqual(oldSubnet.Status.V4UsingIPRange, newSubnet.Status.V4UsingIPRange)
		} else {
			framework.ExpectEqual(oldSubnet.Status.V6AvailableIPs-1, newSubnet.Status.V6AvailableIPs)
			framework.ExpectEqual(oldSubnet.Status.V6UsingIPs+1, newSubnet.Status.V6UsingIPs)
			framework.ExpectNotEqual(oldSubnet.Status.V6AvailableIPRange, newSubnet.Status.V6AvailableIPRange)
			framework.ExpectNotEqual(oldSubnet.Status.V6UsingIPRange, newSubnet.Status.V6UsingIPRange)
		}
		oldSubnet = newSubnet
		// delete counting vip
		ginkgo.By("Deleting counting VIP and waiting for subnet status update")
		vipClient.DeleteSync(countingVipName)
		time.Sleep(5 * time.Second)
		newSubnet = subnetClient.Get(subnetName)
		if newSubnet.Spec.Protocol == apiv1.ProtocolIPv4 {
			framework.ExpectEqual(oldSubnet.Status.V4AvailableIPs+1, newSubnet.Status.V4AvailableIPs)
			framework.ExpectEqual(oldSubnet.Status.V4UsingIPs-1, newSubnet.Status.V4UsingIPs)
			framework.ExpectNotEqual(oldSubnet.Status.V4AvailableIPRange, newSubnet.Status.V4AvailableIPRange)
			framework.ExpectNotEqual(oldSubnet.Status.V4UsingIPRange, newSubnet.Status.V4UsingIPRange)
		} else {
			framework.ExpectEqual(oldSubnet.Status.V6AvailableIPs+1, newSubnet.Status.V6AvailableIPs)
			framework.ExpectEqual(oldSubnet.Status.V6UsingIPs-1, newSubnet.Status.V6UsingIPs)
			framework.ExpectNotEqual(oldSubnet.Status.V6AvailableIPRange, newSubnet.Status.V6AvailableIPRange)
			framework.ExpectNotEqual(oldSubnet.Status.V6UsingIPRange, newSubnet.Status.V6UsingIPRange)
		}
		ginkgo.By("1. Test allowed address pair vip")
		if f.IsIPv6() {
			ginkgo.By("Should create lower case static ipv6 address vip " + lowerCaseStaticIpv6VipName)
			lowerCaseStaticIpv6Vip := makeOvnVip(namespaceName, lowerCaseStaticIpv6VipName, util.DefaultSubnet, "", lowerCaseV6IP, "")
			lowerCaseStaticIpv6Vip = vipClient.CreateSync(lowerCaseStaticIpv6Vip)
			framework.ExpectEqual(lowerCaseStaticIpv6Vip.Status.V6ip, lowerCaseV6IP)
			ginkgo.By("Should not create upper case static ipv6 address vip " + upperCaseStaticIpv6VipName)
			upperCaseStaticIpv6Vip := makeOvnVip(namespaceName, upperCaseStaticIpv6VipName, util.DefaultSubnet, "", upperCaseV6IP, "")
			_ = vipClient.Create(upperCaseStaticIpv6Vip)
			time.Sleep(10 * time.Second)
			upperCaseStaticIpv6Vip = vipClient.Get(upperCaseStaticIpv6VipName)
			framework.ExpectEqual(upperCaseStaticIpv6Vip.Status.V6ip, "")
		}
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
		cmd := []string{"sleep", "infinity"}
		ginkgo.By("Creating pod1 support allowed address pair using " + vip1Name)
		aapPod1 := framework.MakePrivilegedPod(namespaceName, aapPodName1, nil, annotations, f.KubeOVNImage, cmd, nil)
		aapPod1 = podClient.CreateSync(aapPod1)
		ginkgo.By("Creating pod2 support allowed address pair using " + vip1Name)
		aapPod2 := framework.MakePrivilegedPod(namespaceName, aapPodName2, nil, annotations, f.KubeOVNImage, cmd, nil)
		_ = podClient.CreateSync(aapPod2)
		// logical switch port with type virtual should be created
		conditions := fmt.Sprintf("type=virtual name=%s options:virtual-ip=%q", vip1Name, virtualIP1)
		nbctlCmd := "ovn-nbctl --format=list --data=bare --no-heading --columns=options find logical-switch-port " + conditions
		output, _, err := framework.NBExec(nbctlCmd)
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
		rules := make([]apiv1.SecurityGroupRule, 0, 4)
		if f.HasIPv4() {
			// gateway should be added for pinger
			rules = append(rules, apiv1.SecurityGroupRule{
				IPVersion:     "ipv4",
				Protocol:      apiv1.SgProtocolALL,
				Priority:      1,
				RemoteType:    apiv1.SgRemoteTypeAddress,
				RemoteAddress: gatewayV4,
				Policy:        apiv1.SgPolicyAllow,
			})
			// aapPod1 should be allowed by aapPod3 for security group allow address pair test
			rules = append(rules, apiv1.SecurityGroupRule{
				IPVersion:     "ipv4",
				Protocol:      apiv1.SgProtocolALL,
				Priority:      1,
				RemoteType:    apiv1.SgRemoteTypeAddress,
				RemoteAddress: allowAddressV4,
				Policy:        apiv1.SgPolicyAllow,
			})
		}
		if f.HasIPv6() {
			// gateway should be added for pinger
			rules = append(rules, apiv1.SecurityGroupRule{
				IPVersion:     "ipv6",
				Protocol:      apiv1.SgProtocolALL,
				Priority:      1,
				RemoteType:    apiv1.SgRemoteTypeAddress,
				RemoteAddress: gatewayV6,
				Policy:        apiv1.SgPolicyAllow,
			})
			// aapPod1 should be allowed by aapPod3 for security group allow address pair test
			rules = append(rules, apiv1.SecurityGroupRule{
				IPVersion:     "ipv6",
				Protocol:      apiv1.SgProtocolALL,
				Priority:      1,
				RemoteType:    apiv1.SgRemoteTypeAddress,
				RemoteAddress: allowAddressV6,
				Policy:        apiv1.SgPolicyAllow,
			})
		}
		sg := makeSecurityGroup(securityGroupName, true, rules, rules)
		_ = securityGroupClient.CreateSync(sg)

		ginkgo.By("Creating pod3 support allowed address pair with security group")
		annotations[util.PortSecurityAnnotation] = "true"
		annotations[fmt.Sprintf(util.SecurityGroupAnnotationTemplate, "ovn")] = securityGroupName
		aapPod3 := framework.MakePod(namespaceName, aapPodName3, nil, annotations, f.KubeOVNImage, cmd, nil)
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
