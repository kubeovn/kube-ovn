package vpc_subnet_delete

import (
	"context"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.SerialDescribe("[group:vpc-subnet-delete]", func() {
	f := framework.NewDefaultFramework("vpc-subnet-delete")

	ginkgo.It("rejects deleting a VPC while its subnet exists", func() {
		f.SkipVersionPriorTo(1, 10, "VPC and Subnet deletion ordering requires finalizers")

		vpcName := "vpc-" + framework.RandomSuffix()
		subnetName := "subnet-" + framework.RandomSuffix()
		vpcClient := f.VpcClient()
		subnetClient := f.SubnetClient()

		vpc := vpcClient.CreateSync(framework.MakeVpc(vpcName, "", false, false, nil))
		ipFamily := os.Getenv("E2E_IP_FAMILY")
		if ipFamily == "" {
			ipFamily = "ipv4"
		}
		subnet := subnetClient.CreateSync(framework.MakeSubnet(
			subnetName, "", framework.RandomCIDR(ipFamily), "", vpc.Name, "", nil, nil, nil,
		))

		ginkgo.By("confirming the webhook rejects deleting a VPC with a subnet")
		err := vpcClient.VpcInterface.Delete(context.Background(), vpc.Name, metav1.DeleteOptions{})
		framework.ExpectError(err)
		_, err = vpcClient.VpcInterface.Get(context.Background(), vpc.Name, metav1.GetOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("deleting the subnet before the VPC")
		subnetClient.DeleteSync(subnet.Name)
		vpcClient.DeleteSync(vpc.Name)
	})
})
