package subnet

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:webhook-subnet]", func() {
	f := framework.NewDefaultFramework("webhook-subnet")

	var subnet *apiv1.Subnet
	var subnetClient *framework.SubnetClient

	var subnetName string
	var cidr, cidrV4, cidrV6, firstIPv4, firstIPv6 string
	var gateways []string

	ginkgo.BeforeEach(func() {
		subnetClient = f.SubnetClient()
		subnetName = "subnet-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIpFamily)
		cidrV4, cidrV6 = util.SplitStringIP(cidr)
		gateways = nil

		if cidrV4 == "" {
			firstIPv4 = ""
		} else {
			firstIPv4, _ = util.FirstIP(cidrV4)
			gateways = append(gateways, firstIPv4)
		}
		if cidrV6 == "" {
			firstIPv6 = ""
		} else {
			firstIPv6, _ = util.FirstIP(cidrV6)
			gateways = append(gateways, firstIPv6)
		}
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("check create subnet with different errors", func() {
		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)

		ginkgo.By("Validating subnet gateway")
		subnet.Spec.Gateway = "100.16.0.1"
		_, err := subnetClient.SubnetInterface.Create(context.TODO(), subnet, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf(" gateway %s is not in cidr %s", subnet.Spec.Gateway, subnet.Spec.CIDRBlock))

		ginkgo.By("Validating subnet cidr conflict with known addresses")
		subnet.Spec.Gateway = ""
		subnet.Spec.CIDRBlock = util.IPv4Loopback
		_, err = subnetClient.SubnetInterface.Create(context.TODO(), subnet, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf("%s conflict with v4 loopback cidr %s", subnet.Spec.CIDRBlock, util.IPv4Loopback))

		ginkgo.By("Validating subnet excludeIPs")
		subnet.Spec.CIDRBlock = cidr
		ipr := "10.1.1.11..10.1.1.30..10.1.1.50"
		subnet.Spec.ExcludeIps = []string{ipr}
		_, err = subnetClient.SubnetInterface.Create(context.TODO(), subnet, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf("%s in excludeIps is not a valid ip range", ipr))

		ginkgo.By("Validating subnet gateway type")
		subnet.Spec.ExcludeIps = []string{}
		subnet.Spec.GatewayType = "test"
		_, err = subnetClient.SubnetInterface.Create(context.TODO(), subnet, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf("%s is not a valid gateway type", subnet.Spec.GatewayType))

		ginkgo.By("Validating subnet protocol")
		subnet.Spec.GatewayType = apiv1.GWDistributedType
		subnet.Spec.Protocol = "test"
		_, err = subnetClient.SubnetInterface.Create(context.TODO(), subnet, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf("%s is not a valid protocol", subnet.Spec.Protocol))

		ginkgo.By("Validating subnet allowSubnets")
		subnet.Spec.Protocol = ""
		subnet.Spec.AllowSubnets = []string{"10.1.1.302/24"}
		_, err = subnetClient.SubnetInterface.Create(context.TODO(), subnet, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf("%s in allowSubnets is not a valid address", subnet.Spec.AllowSubnets[0]))

		ginkgo.By("Validating subnet cidr")
		subnet.Spec.AllowSubnets = []string{}
		subnet.Spec.CIDRBlock = "10.1.1.32/24,"
		_, err = subnetClient.SubnetInterface.Create(context.TODO(), subnet, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf("subnet %s cidr %s is invalid", subnet.Name, subnet.Spec.CIDRBlock))
	})
})
