package subnet

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:webhook-subnet]", func() {
	f := framework.NewDefaultFramework("webhook-subnet")

	var subnetName, cidr, cidrV4, cidrV6, firstIPv4, firstIPv6 string
	var gateways []string
	var subnetClient *framework.SubnetClient

	ginkgo.BeforeEach(func() {
		subnetClient = f.SubnetClient()
		subnetName = "subnet-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
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
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)

		ginkgo.By("Validating subnet gateway")
		subnet.Spec.Gateway = "100.16.0.1"
		_, err := subnetClient.SubnetInterface.Create(context.TODO(), subnet, metav1.CreateOptions{})
		framework.ExpectError(err, "gateway %s is not in cidr %s", subnet.Spec.Gateway, subnet.Spec.CIDRBlock)

		ginkgo.By("Validating subnet cidr conflict with known addresses")
		subnet.Spec.Gateway = ""
		subnet.Spec.CIDRBlock = util.IPv4Loopback
		_, err = subnetClient.SubnetInterface.Create(context.TODO(), subnet, metav1.CreateOptions{})
		framework.ExpectError(err, "%s conflict with v4 loopback cidr %s", subnet.Spec.CIDRBlock, util.IPv4Loopback)

		ginkgo.By("Validating subnet excludeIPs")
		subnet.Spec.CIDRBlock = cidr
		ipr := "10.1.1.11..10.1.1.30..10.1.1.50"
		subnet.Spec.ExcludeIps = []string{ipr}
		_, err = subnetClient.SubnetInterface.Create(context.TODO(), subnet, metav1.CreateOptions{})
		framework.ExpectError(err, "%s in excludeIps is not a valid ip range", ipr)

		ginkgo.By("Validating subnet gateway type")
		subnet.Spec.ExcludeIps = []string{}
		subnet.Spec.GatewayType = "test"
		_, err = subnetClient.SubnetInterface.Create(context.TODO(), subnet, metav1.CreateOptions{})
		framework.ExpectError(err, "%s is not a valid gateway type", subnet.Spec.GatewayType)

		ginkgo.By("Validating subnet protocol")
		subnet.Spec.GatewayType = apiv1.GWDistributedType
		subnet.Spec.Protocol = "test"
		_, err = subnetClient.SubnetInterface.Create(context.TODO(), subnet, metav1.CreateOptions{})
		framework.ExpectError(err, "%s is not a valid protocol", subnet.Spec.Protocol)

		ginkgo.By("Validating subnet allowSubnets")
		subnet.Spec.Protocol = ""
		subnet.Spec.AllowSubnets = []string{"10.1.1.302/24"}
		_, err = subnetClient.SubnetInterface.Create(context.TODO(), subnet, metav1.CreateOptions{})
		framework.ExpectError(err, "%s in allowSubnets is not a valid address", subnet.Spec.AllowSubnets[0])

		ginkgo.By("Validating subnet cidr")
		subnet.Spec.AllowSubnets = []string{}
		subnet.Spec.CIDRBlock = "10.1.1.32/24,"
		_, err = subnetClient.SubnetInterface.Create(context.TODO(), subnet, metav1.CreateOptions{})
		framework.ExpectError(err, "subnet %s cidr %s is invalid", subnet.Name, subnet.Spec.CIDRBlock)
	})

	framework.ConformanceIt("check subnet vpc update validation", func() {
		f.SkipVersionPriorTo(1, 15, "vpc cannot be set to non-ovn-cluster on update is not supported before 1.15.0")
		ginkgo.By("Creating subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnetClient.CreateSync(subnet)

		// TODO: Use Patch instead of Update to modify only the Spec.Vpc field.
		// This would avoid resource conflicts caused by concurrent status updates from the controller.
		// Example: subnetClient.Patch(subnet, modifiedSubnet, framework.Timeout())
		ginkgo.By("Validating vpc can be changed from empty to ovn-cluster")
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			subnet = subnetClient.Get(subnetName)
			modifiedSubnet := subnet.DeepCopy()
			modifiedSubnet.Spec.Vpc = util.DefaultVpc
			_, err := subnetClient.SubnetInterface.Update(context.TODO(), modifiedSubnet, metav1.UpdateOptions{})
			return err
		})
		framework.ExpectNoError(err)

		ginkgo.By("Validating vpc cannot be changed from ovn-cluster to another value")
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			subnet = subnetClient.Get(subnetName)
			modifiedSubnet := subnet.DeepCopy()
			modifiedSubnet.Spec.Vpc = "test-vpc"
			_, err := subnetClient.SubnetInterface.Update(context.TODO(), modifiedSubnet, metav1.UpdateOptions{})
			return err
		})
		framework.ExpectError(err, "vpc can only be changed from empty to ovn-cluster")

		ginkgo.By("Validating vpc cannot be changed from ovn-cluster to empty")
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			subnet = subnetClient.Get(subnetName)
			modifiedSubnet := subnet.DeepCopy()
			modifiedSubnet.Spec.Vpc = ""
			_, err := subnetClient.SubnetInterface.Update(context.TODO(), modifiedSubnet, metav1.UpdateOptions{})
			return err
		})
		framework.ExpectError(err, "vpc can only be changed from empty to ovn-cluster")
	})

	framework.ConformanceIt("check subnet vpc cannot be set to non-ovn-cluster on update", func() {
		f.SkipVersionPriorTo(1, 15, "vpc cannot be set to non-ovn-cluster on update is not supported before 1.15.0")
		ginkgo.By("Creating subnet " + subnetName + " with empty vpc")
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating vpc cannot be changed from empty to non-ovn-cluster value")
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			subnet = subnetClient.Get(subnetName)
			modifiedSubnet := subnet.DeepCopy()
			modifiedSubnet.Spec.Vpc = "test-vpc"
			_, err := subnetClient.SubnetInterface.Update(context.TODO(), modifiedSubnet, metav1.UpdateOptions{})
			return err
		})
		framework.ExpectError(err, "vpc can only be changed from empty to ovn-cluster")
	})
})
