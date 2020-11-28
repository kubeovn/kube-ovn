package node

import (
	"fmt"
	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/alauda/kube-ovn/test/e2e/framework"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
)

var _ = Describe("[Node Init]", func() {
	f := framework.NewFramework("ip allocation", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))

	It("node annotations", func() {
		nodes, err := f.KubeClientSet.CoreV1().Nodes().List(metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		subnet, err := f.OvnClientSet.KubeovnV1().Subnets().Get("join", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, no := range nodes.Items {
			annotations := no.Annotations
			ipDual, _ := util.StringToDualStack(annotations[util.IpAddressAnnotation])
			cidrDual, _ := util.StringToDualStack(annotations[util.CidrAnnotation])
			Expect(annotations[util.AllocatedAnnotation]).To(Equal("true"))
			Expect(annotations[util.CidrAnnotation]).To(Equal(util.DualStackToString(subnet.Spec.CIDRBlock)))
			Expect(annotations[util.GatewayAnnotation]).To(Equal(util.DualStackToString(subnet.Spec.Gateway)))
			Expect(annotations[util.IpAddressAnnotation]).NotTo(BeEmpty())
			for _, ip := range ipDual {
				Expect(util.SubnetContainIp(cidrDual, ip)).To(BeTrue())
			}
			Expect(annotations[util.MacAddressAnnotation]).NotTo(BeEmpty())
			Expect(annotations[util.PortNameAnnotation]).NotTo(BeEmpty())
			Expect(annotations[util.LogicalSwitchAnnotation]).To(Equal(subnet.Name))
		}
	})
})
