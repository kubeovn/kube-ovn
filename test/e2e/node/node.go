package node

import (
	"context"
	"fmt"
	"os"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("[Node Init]", func() {
	f := framework.NewFramework("ip allocation", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))

	It("node annotations", func() {
		nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		subnet, err := f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), "join", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, no := range nodes.Items {
			annotations := no.Annotations
			Expect(annotations[util.AllocatedAnnotation]).To(Equal("true"))
			Expect(annotations[util.CidrAnnotation]).To(Equal(subnet.Spec.CIDRBlock))
			Expect(annotations[util.GatewayAnnotation]).To(Equal(subnet.Spec.Gateway))
			Expect(annotations[util.IpAddressAnnotation]).NotTo(BeEmpty())
			Expect(util.CIDRContainIP(annotations[util.CidrAnnotation], annotations[util.IpAddressAnnotation])).To(BeTrue())
			Expect(annotations[util.MacAddressAnnotation]).NotTo(BeEmpty())
			Expect(annotations[util.PortNameAnnotation]).NotTo(BeEmpty())
			Expect(annotations[util.LogicalSwitchAnnotation]).To(Equal(subnet.Name))
		}
	})
})
