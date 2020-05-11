package subnet

import (
	"fmt"
	kubeovn "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/alauda/kube-ovn/test/e2e/framework"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"time"
)

var _ = Describe("[Subnet]", func() {
	f := framework.NewFramework("subnet", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))
	BeforeEach(func() {
		f.OvnClientSet.KubeovnV1().Subnets().Delete(f.GetName(), &metav1.DeleteOptions{})
		f.KubeClientSet.CoreV1().Namespaces().Delete(f.GetName(), &metav1.DeleteOptions{})
	})
	AfterEach(func() {
		f.OvnClientSet.KubeovnV1().Subnets().Delete(f.GetName(), &metav1.DeleteOptions{})
		f.KubeClientSet.CoreV1().Namespaces().Delete(f.GetName(), &metav1.DeleteOptions{})
	})

	Describe("Create", func() {
		It("only cidr", func() {
			name := f.GetName()
			By("create subnet")
			s := kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock: "11.10.0.0/16",
				},
			}
			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(&s)
			Expect(err).NotTo(HaveOccurred())

			By("validate subnet")
			err = f.WaitSubnetReady(name)
			Expect(err).NotTo(HaveOccurred())

			subnet, err := f.OvnClientSet.KubeovnV1().Subnets().Get(name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(subnet.Spec.Default).To(BeFalse())
			Expect(subnet.Spec.Protocol).To(Equal(kubeovn.ProtocolIPv4))
			Expect(subnet.Spec.Namespaces).To(BeEmpty())
			Expect(subnet.Spec.ExcludeIps).To(ContainElement("11.10.0.1"))
			Expect(subnet.Spec.Gateway).To(Equal("11.10.0.1"))
			Expect(subnet.Spec.GatewayType).To(Equal(kubeovn.GWDistributedType))
			Expect(subnet.Spec.GatewayNode).To(BeEmpty())
			Expect(subnet.Spec.NatOutgoing).To(BeFalse())
			Expect(subnet.Spec.Private).To(BeFalse())
			Expect(subnet.Spec.AllowSubnets).To(BeEmpty())
			Expect(subnet.ObjectMeta.Finalizers).To(ContainElement(util.ControllerName))

			By("validate status")
			Expect(subnet.Status.ActivateGateway).To(BeEmpty())
			Expect(subnet.Status.AvailableIPs).To(Equal(float64(65533)))
			Expect(subnet.Status.UsingIPs).To(BeZero())

			pods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(metav1.ListOptions{LabelSelector: "app=ovs"})
			Expect(err).NotTo(HaveOccurred())
			for _, pod := range pods.Items {
				stdout, _, err := f.ExecToPodThroughAPI(fmt.Sprintf("ip route list root %s", subnet.Spec.CIDRBlock), "openvswitch", pod.Name, pod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(stdout).To(ContainSubstring("ovn0"))
			}
		})

		It("centralized gateway", func() {
			name := f.GetName()
			By("create subnet")
			s := kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock:   "11.11.0.0/16",
					GatewayType: kubeovn.GWCentralizedType,
					GatewayNode: "kube-ovn-control-plane,kube-ovn-worker,kube-ovn-worker2",
				},
			}
			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(&s)
			Expect(err).NotTo(HaveOccurred())

			By("validate subnet")
			err = f.WaitSubnetReady(name)
			Expect(err).NotTo(HaveOccurred())
			time.Sleep(5 * time.Second)

			subnet, err := f.OvnClientSet.KubeovnV1().Subnets().Get(name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(subnet.Spec.GatewayType).To(Equal(kubeovn.GWCentralizedType))
			Expect(subnet.Status.ActivateGateway).To(Equal("kube-ovn-control-plane"))
		})
	})

	Describe("Update", func() {
		It("distributed to centralized", func() {
			name := f.GetName()
			By("create subnet")
			s := &kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock: "11.12.0.0/16",
				},
			}
			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(s)
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitSubnetReady(name)
			Expect(err).NotTo(HaveOccurred())

			s, err = f.OvnClientSet.KubeovnV1().Subnets().Get(name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			s.Spec.GatewayType = kubeovn.GWCentralizedType
			s.Spec.GatewayNode = "kube-ovn-control-plane,kube-ovn-worker,kube-ovn-worker2"
			_, err = f.OvnClientSet.KubeovnV1().Subnets().Update(s)
			Expect(err).NotTo(HaveOccurred())

			time.Sleep(5 * time.Second)
			s, err = f.OvnClientSet.KubeovnV1().Subnets().Get(name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			Expect(s.Spec.GatewayType).To(Equal(kubeovn.GWCentralizedType))
			Expect(s.Status.ActivateGateway).To(Equal("kube-ovn-control-plane"))
		})
	})

	Describe("Delete", func() {
		It("normal deletion", func() {
			name := f.GetName()
			By("create subnet")
			s := kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock: "11.13.0.0/16",
				},
			}
			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(&s)
			Expect(err).NotTo(HaveOccurred())

			time.Sleep(5 * time.Second)
			err = f.OvnClientSet.KubeovnV1().Subnets().Delete(name, &metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			time.Sleep(5 * time.Second)
			pods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(metav1.ListOptions{LabelSelector: "app=ovs"})
			Expect(err).NotTo(HaveOccurred())
			for _, pod := range pods.Items {
				stdout, _, err := f.ExecToPodThroughAPI("ip route", "openvswitch", pod.Name, pod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(stdout).NotTo(ContainSubstring(s.Spec.CIDRBlock))
			}
		})
	})

	Describe("cidr with nonstandard style", func() {
		It("cidr ends with nonzero", func() {
			name := f.GetName()
			By("create subnet")
			s := &kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock: "11.14.0.1/16",
				},
			}

			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(s)
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitSubnetReady(name)
			Expect(err).NotTo(HaveOccurred())

			s, err = f.OvnClientSet.KubeovnV1().Subnets().Get(name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Spec.CIDRBlock).To(Equal("11.14.0.0/16"))
		})
	})
})
