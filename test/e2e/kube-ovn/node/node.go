package node

import (
	clientset "k8s.io/client-go/kubernetes"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:node]", func() {
	f := framework.NewDefaultFramework("node")
	f.SkipNamespaceCreation = true

	var cs clientset.Interface
	var subnetClient *framework.SubnetClient
	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		subnetClient = f.SubnetClient()
	})

	framework.ConformanceIt("should allocate ip in join subnet to node", func() {
		ginkgo.By("Getting join subnet")
		join := subnetClient.Get("join")

		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)

		ginkgo.By("Validating node annotations")
		for _, node := range nodeList.Items {
			framework.ExpectHaveKeyWithValue(node.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectUUID(node.Annotations[util.ChassisAnnotation])
			framework.ExpectHaveKeyWithValue(node.Annotations, util.CidrAnnotation, join.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(node.Annotations, util.GatewayAnnotation, join.Spec.Gateway)
			framework.ExpectIPInCIDR(node.Annotations[util.IpAddressAnnotation], join.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(node.Annotations, util.LogicalSwitchAnnotation, join.Name)
			framework.ExpectMAC(node.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(node.Annotations, util.PortNameAnnotation, "node-"+node.Name)

			// TODO: check IP/route on ovn0
		}
	})
})
