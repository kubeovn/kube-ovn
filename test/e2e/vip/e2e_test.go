package vip

import (
	"context"
	"flag"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	"github.com/onsi/ginkgo/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func makeOvnVip(namespaceName, name, subnet, v4ip, v6ip, vipType string) *kubeovnv1.Vip {
	return framework.MakeVip(namespaceName, name, subnet, v4ip, v6ip, vipType)
}

var _ = framework.Describe("[group:vip]", func() {
	f := framework.NewDefaultFramework("vip")

	var cs clientset.Interface

	var vpcClient *framework.VpcClient
	var subnetClient *framework.SubnetClient
	var vipClient *framework.VipClient
	var vpc *kubeovnv1.Vpc
	var subnet *kubeovnv1.Subnet
	var podClient *framework.PodClient
	var image, namespaceName, vpcName, subnetName, cidr string

	// test switch lb vip, which ip is in the vpc subnet cidr
	// switch lb vip use gw mac to trigger lb nat flows
	var switchLbVip1Name, switchLbVip2Name string

	// test allowed address pair vip
	var vip1Name, vip2Name, aapPodName1, aapPodName2 string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		vpcClient = f.VpcClient()
		subnetClient = f.SubnetClient()
		vipClient = f.VipClient()
		podClient = f.PodClient()
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
		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
		ginkgo.By("Deleting vpc " + vpcName)
		vpcClient.DeleteSync(vpcName)
	})

	framework.ConformanceIt("Test vip", func() {
		ginkgo.By("1. Test allowed address pair vip")
		annotations := map[string]string{util.AAPsAnnotation: vip1Name}
		cmd := []string{"sh", "-c", "sleep infinity"}
		ginkgo.By("Creating pod1 support allowed address pair using " + vip1Name)
		aapPod1 := framework.MakeNetAdminPod(namespaceName, aapPodName1, nil, annotations, image, cmd, nil)
		_ = podClient.CreateSync(aapPod1)
		ginkgo.By("Creating pod2 support allowed address pair using " + vip1Name)
		aapPod2 := framework.MakeNetAdminPod(namespaceName, aapPodName2, nil, annotations, image, cmd, nil)
		_ = podClient.CreateSync(aapPod2)
		ginkgo.By("Creating allowed address pair vip, should have different ip and mac")
		ginkgo.By("Creating allowed address pair vip " + vip1Name)
		vip1 := makeOvnVip(namespaceName, vip1Name, subnetName, "", "", "")
		vip1 = vipClient.CreateSync(vip1)
		ginkgo.By("Creating allowed address pair vip " + vip2Name)
		vip2 := makeOvnVip(namespaceName, vip2Name, subnetName, "", "", "")
		vip2 = vipClient.CreateSync(vip2)
		// arp proxy vip only used in switch lb rule, the lb vip use the subnet gw mac to use lb nat flow
		framework.ExpectNotEqual(vip1.Status.Mac, vip2.Status.Mac)
		if vip1.Status.V4ip != "" {
			framework.ExpectNotEqual(vip1.Status.V4ip, vip2.Status.V4ip)
			// logical switch port with type virtual should be created
			conditions := fmt.Sprintf("type=virtual name=%s options:virtual-ip=%s ", vip1Name, vip1.Status.V4ip)
			execCmd := "kubectl ko nbctl --format=list --data=bare --no-heading --columns=options find logical-switch-port " + conditions
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
			virtualParents := options["virtual-parents"]
			expectVirtualParents := fmt.Sprintf("%s.%s,%s.%s", aapPodName1, namespaceName, aapPodName2, namespaceName)
			framework.ExpectEqual(expectVirtualParents, virtualParents)
			// other pods can communicate with the aap pod through vip
			ginkgo.By("Test pod ping aap address " + vip1.Status.V4ip)
			addIP := fmt.Sprintf("ip addr add %s/24 dev eth0", vip1.Status.V4ip)
			delIP := fmt.Sprintf("ip addr del %s/24 dev eth0", vip1.Status.V4ip)
			command := fmt.Sprintf("ping -W 1 -c 1 %s", vip1.Status.V4ip)
			// check aapPod2 ping aapPod1 through vip
			stdout, stderr, err := framework.ExecShellInPod(context.Background(), f, namespaceName, aapPodName1, addIP)
			framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", addIP, err, stderr, stdout)
			stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, aapPodName2, command)
			framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", command, err, stderr, stdout)
			framework.ExpectNoError(err)
			// aapPod2 can not ping aapPod1 vip when ip is deleted
			stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, aapPodName1, delIP)
			framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", delIP, err, stderr, stdout)
			stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, aapPodName2, command)
			framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", command, err, stderr, stdout)
			framework.ExpectError(err)
			// check aapPod1 ping aapPod2 through vip
			stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, aapPodName2, addIP)
			framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", addIP, err, stderr, stdout)
			stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, namespaceName, aapPodName1, command)
			framework.Logf("exec %s failed err: %v, stderr: %s, stdout: %s", command, err, stderr, stdout)
			framework.ExpectNoError(err)
		} else {
			framework.ExpectNotEqual(vip1.Status.V6ip, vip2.Status.V6ip)
		}

		ginkgo.By("2. Test switch lb vip")
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
