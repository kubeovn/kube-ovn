package vtep

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:vtep]", func() {
	f := framework.NewDefaultFramework("vtep")

	var (
		subnetName    string
		bindingName   string
		subnetCIDR    string
		binding       *apiv1.VtepBinding
		subnetClient  *framework.SubnetClient
		bindingClient *framework.VtepBindingClient
	)

	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 17, "VtepBinding was introduced in v1.17")
		subnetClient = f.SubnetClient()
		bindingClient = f.VtepBindingClient()
		subnetName = "subnet-" + framework.RandomSuffix()
		bindingName = "vtepb-" + framework.RandomSuffix()
		subnetCIDR = framework.RandomCIDR(f.ClusterIPFamily)

		ginkgo.By("Creating subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, "", subnetCIDR, "", "", "", nil, nil, nil)
		subnetClient.CreateSync(subnet)
	})

	ginkgo.AfterEach(func() {
		if binding != nil {
			ginkgo.By("Deleting vtep binding " + bindingName)
			bindingClient.DeleteSync(bindingName)
		}
		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("should create OVN type=vtep Logical Switch Port for VtepBinding", func() {
		ginkgo.By("Creating vtep binding " + bindingName)
		binding = &apiv1.VtepBinding{
			ObjectMeta: metav1.ObjectMeta{Name: bindingName},
			Spec: apiv1.VtepBindingSpec{
				Subnet:         subnetName,
				PhysicalSwitch: "fake-vtep-switch",
				PhysicalPort:   "Ethernet1/1",
				VlanID:         100,
			},
		}
		binding = bindingClient.Create(binding)

		lspName := ovs.GetVtepLogicalSwitchPortName(bindingName)
		ginkgo.By("Waiting for status.logicalSwitchPort=" + lspName)
		binding = bindingClient.WaitUntil(bindingName, func(b *apiv1.VtepBinding) (bool, error) {
			return b.Status.LogicalSwitchPort == lspName && b.Status.VtepLogicalSwitch == subnetName, nil
		}, "have NB LSP status populated", time.Second, 2*time.Minute)

		framework.ExpectEqual(binding.Status.LogicalSwitch, subnetName)
		framework.ExpectEqual(binding.Status.Ready, false)

		ginkgo.By("Verifying OVN NB Logical Switch Port type=vtep")
		cmd := fmt.Sprintf("ovn-nbctl --format=csv --data=bare --no-heading --columns=type,options get Logical_Switch_Port %s", lspName)
		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			output, _, err := framework.NBExec(cmd)
			if err != nil {
				return false, nil
			}
			line := strings.TrimSpace(string(output))
			return strings.HasPrefix(line, "vtep,") &&
				strings.Contains(line, "vtep-physical-switch=fake-vtep-switch") &&
				strings.Contains(line, "vtep-logical-switch="+subnetName), nil
		}, fmt.Sprintf("OVN LSP %s to be type=vtep", lspName))

		ginkgo.By("Deleting vtep binding and verifying NB LSP removal")
		bindingClient.DeleteSync(bindingName)
		binding = nil
		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			_, stderr, err := framework.NBExec("ovn-nbctl get Logical_Switch_Port " + lspName + " name")
			if err != nil || strings.Contains(string(stderr), "no row") || strings.Contains(string(stderr), "not found") {
				return true, nil
			}
			return false, nil
		}, fmt.Sprintf("OVN LSP %s to be deleted", lspName))
	})
})
