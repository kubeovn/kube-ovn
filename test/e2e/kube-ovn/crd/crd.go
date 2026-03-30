package crd

import (
	"fmt"
	"os/exec"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:crd]", func() {
	f := framework.NewDefaultFramework("crd")

	ginkgo.Context("CRD Generation and Installation", func() {
		ginkgo.It("should generate and install successfully, and support basic networking", func() {
			ginkgo.By("Verifying Subnet CRD is established")
			err := waitForCRDEstablished("subnets.kubeovn.io")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("Creating a subnet to verify basic CRD usage")
			subnetClient := f.SubnetClient()
			podClient := f.PodClient()
			subnetName := "crd-test-subnet-" + framework.RandomSuffix()
			podName := "crd-test-pod-" + framework.RandomSuffix()
			cidr := framework.RandomCIDR(f.ClusterIPFamily)

			subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
			_ = subnetClient.CreateSync(subnet)

			ginkgo.By("Creating a pod in the new subnet")
			annotations := map[string]string{
				util.LogicalSwitchAnnotation: subnetName,
			}
			pod := framework.MakePod(f.Namespace.Name, podName, nil, annotations, framework.AgnhostImage, nil, nil)
			_ = podClient.CreateSync(pod)

			ginkgo.By("Verifying pod has IP from the subnet")
			pod = podClient.GetPod(podName)
			gomega.Expect(pod.Annotations[util.AllocatedAnnotation]).To(gomega.Equal("true"))
			gomega.Expect(pod.Annotations[util.IPAddressAnnotation]).NotTo(gomega.BeEmpty())

			ginkgo.By("Deleting the pod")
			podClient.DeleteSync(podName)

			ginkgo.By("Deleting the subnet")
			subnetClient.DeleteSync(subnetName)
		})
	})
})

func waitForCRDEstablished(name string) error {
	// Wait for the CRD to be Established
	cmd := exec.Command("kubectl", "wait", "--for=condition=Established", fmt.Sprintf("crd/%s", name), "--timeout=30s")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("CRD %s is not established: %s", name, string(out))
	}
	return nil
}
