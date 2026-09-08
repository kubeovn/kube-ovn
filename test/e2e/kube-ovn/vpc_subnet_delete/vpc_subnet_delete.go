package vpc_subnet_delete

import (
	"context"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.SerialDescribe("[group:vpc-subnet-delete]", func() {
	f := framework.NewDefaultFramework("vpc-subnet-delete")

	ginkgo.It("keeps a terminating VPC alive until its subnet is gone", func() {
		f.SkipVersionPriorTo(1, 10, "VPC and Subnet finalizers are required")

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

		ginkgo.By("deleting the VPC while its subnet still exists")
		err := vpcClient.VpcInterface.Delete(context.Background(), vpc.Name, metav1.DeleteOptions{})
		if err != nil {
			framework.Logf("VPC deletion rejected by admission: %v", err)
		} else {
			// The controller finalizer must hold the VPC in Terminating while the subnet exists.
			ginkgo.By("checking the VPC stays in Terminating")
			framework.ExpectError(vpcClient.WaitToDisappear(vpc.Name, 2*time.Second, 20*time.Second),
				"vpc %s should not disappear while subnet %s exists", vpc.Name, subnet.Name)
			terminating := vpcClient.Get(vpc.Name)
			framework.ExpectNotNil(terminating.DeletionTimestamp)
		}

		ginkgo.By("deleting the subnet and VPC")
		subnetClient.DeleteSync(subnet.Name)
		if err != nil {
			framework.ExpectNoError(vpcClient.VpcInterface.Delete(context.Background(), vpc.Name, metav1.DeleteOptions{}))
		}
		framework.ExpectNoError(vpcClient.WaitToDisappear(vpc.Name, 2*time.Second, 2*time.Minute))
	})
})
