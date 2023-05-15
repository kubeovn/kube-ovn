package vip

import (
	"context"
	"fmt"
	"math/big"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/onsi/ginkgo/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:webhook-vip]", func() {
	f := framework.NewDefaultFramework("webhook-vip")

	var vip *apiv1.Vip
	var subnet *apiv1.Subnet
	var vipClient *framework.VipClient
	var subnetClient *framework.SubnetClient
	var vipName, subnetName, namespaceName string
	var cidr, lastIPv4 string

	ginkgo.BeforeEach(func() {
		subnetClient = f.SubnetClient()
		subnetName = "subnet-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIpFamily)
		cidrV4, _ := util.SplitStringIP(cidr)
		if cidrV4 == "" {
			lastIPv4 = ""
		} else {
			lastIPv4, _ = util.LastIP(cidrV4)
		}

		vipClient = f.VipClient()
		subnetClient = f.SubnetClient()
		vipName = "vip-" + framework.RandomSuffix()
		namespaceName = f.Namespace.Name

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting vip " + vipName)
		vipClient.Delete(vipName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("check create vip with different errors", func() {
		ginkgo.By("Creating vip " + vipName)
		vip = framework.MakeVip(vipName, "", "", "")

		ginkgo.By("validating subnet")
		vip.Spec.Subnet = ""
		_, err := vipClient.VipInterface.Create(context.TODO(), vip, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf("subnet parameter cannot be empty"))

		ginkgo.By("validating wrong subnet")
		vip.Spec.Subnet = "abc"
		_, err = vipClient.VipInterface.Create(context.TODO(), vip, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf("Subnet.kubeovn.io \"%s\" not found", vip.Spec.Subnet))

		ginkgo.By("Validating vip usage with wrong v4ip")
		vip.Spec.Subnet = subnetName
		vip.Spec.V4ip = "10.10.10.10.10"
		_, err = vipClient.VipInterface.Create(context.TODO(), vip, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf("%s is not a valid ip", vip.Spec.V4ip))

		ginkgo.By("Validating vip usage with wrong v6ip")
		vip.Spec.V4ip = ""
		vip.Spec.V6ip = "2001:250:207::eff2::2"
		_, err = vipClient.VipInterface.Create(context.TODO(), vip, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf("%s is not a valid ip", vip.Spec.V6ip))

		ginkgo.By("validate ip not in subnet cidr")
		vip.Spec.V6ip = ""
		vip.Spec.V4ip = util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(lastIPv4), big.NewInt(10)))
		_, err = vipClient.VipInterface.Create(context.TODO(), vip, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf("%s is not in the range of subnet %s", vip.Spec.V4ip, vip.Spec.Subnet))
	})
})
