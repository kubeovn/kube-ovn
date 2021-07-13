package subnet

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	kubeovn "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = Describe("[Subnet]", func() {
	f := framework.NewFramework("subnet", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))
	BeforeEach(func() {
		if err := f.OvnClientSet.KubeovnV1().Subnets().Delete(context.Background(), f.GetName(), metav1.DeleteOptions{}); err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Fatalf("failed to delete subnet %s, %v", f.GetName(), err)

			}
		}
		if err := f.KubeClientSet.CoreV1().Namespaces().Delete(context.Background(), f.GetName(), metav1.DeleteOptions{}); err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Fatalf("failed to delete ns %s, %v", f.GetName(), err)
			}
		}
	})
	AfterEach(func() {
		if err := f.OvnClientSet.KubeovnV1().Subnets().Delete(context.Background(), f.GetName(), metav1.DeleteOptions{}); err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Fatalf("failed to delete subnet %s, %v", f.GetName(), err)
			}
		}
		if err := f.KubeClientSet.CoreV1().Namespaces().Delete(context.Background(), f.GetName(), metav1.DeleteOptions{}); err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Fatalf("failed to delete ns %s, %v", f.GetName(), err)
			}
		}
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
			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), &s, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("validate subnet")
			err = f.WaitSubnetReady(name)
			Expect(err).NotTo(HaveOccurred())

			subnet, err := f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), name, metav1.GetOptions{})
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
			Expect(subnet.Status.V4AvailableIPs).To(Equal(float64(65533)))
			Expect(subnet.Status.V4UsingIPs).To(BeZero())

			pods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=ovs"})
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
			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), &s, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("validate subnet")
			err = f.WaitSubnetReady(name)
			Expect(err).NotTo(HaveOccurred())
			time.Sleep(5 * time.Second)

			subnet, err := f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(subnet.Spec.GatewayType).To(Equal(kubeovn.GWCentralizedType))
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
			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), s, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitSubnetReady(name)
			Expect(err).NotTo(HaveOccurred())

			s, err = f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			s.Spec.GatewayType = kubeovn.GWCentralizedType
			s.Spec.GatewayNode = "kube-ovn-control-plane,kube-ovn-worker,kube-ovn-worker2"
			_, err = f.OvnClientSet.KubeovnV1().Subnets().Update(context.Background(), s, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())

			time.Sleep(5 * time.Second)
			s, err = f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			Expect(s.Spec.GatewayType).To(Equal(kubeovn.GWCentralizedType))
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
			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), &s, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			time.Sleep(5 * time.Second)
			err = f.OvnClientSet.KubeovnV1().Subnets().Delete(context.Background(), name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			time.Sleep(5 * time.Second)
			pods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=ovs"})
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

			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), s, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitSubnetReady(name)
			Expect(err).NotTo(HaveOccurred())

			s, err = f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Spec.CIDRBlock).To(Equal("11.14.0.0/16"))
		})
	})

	Describe("available ip calculation", func() {
		It("no available cidr", func() {
			name := f.GetName()
			s := &kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock:  "19.0.0.0/31",
					ExcludeIps: []string{"179.17.0.0..179.17.0.10"},
				},
			}
			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), s, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitSubnetReady(name)
			Expect(err).NotTo(HaveOccurred())

			s, err = f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Status.V4AvailableIPs).To(Equal(float64(0)))
		})

		It("small cidr", func() {
			name := f.GetName()
			s := &kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock:  "29.0.0.0/30",
					ExcludeIps: []string{"179.17.0.0..179.17.0.10"},
				},
			}
			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), s, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitSubnetReady(name)
			Expect(err).NotTo(HaveOccurred())

			s, err = f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Status.V4AvailableIPs).To(Equal(float64(1)))
		})

		It("with excludeips", func() {
			name := f.GetName()
			s := &kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock:  "179.17.0.0/24",
					ExcludeIps: []string{"179.17.0.0..179.17.0.10"},
				},
			}
			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), s, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitSubnetReady(name)
			Expect(err).NotTo(HaveOccurred())

			s, err = f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Status.V4AvailableIPs).To(Equal(float64(244)))
		})
	})

	Describe("External Egress Gateway", func() {
		It("centralized gateway with external egress gateway", func() {
			nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes).NotTo(BeNil())
			Expect(nodes.Items).NotTo(BeEmpty())

			for _, node := range nodes.Items {
				Expect(node.Status.Addresses).NotTo(BeEmpty())
			}

			name := f.GetName()
			egw := nodes.Items[0].Status.Addresses[0].Address
			priority, tableID := uint32(1001), uint32(1002)

			gatewayNodes := make([]string, 0, 2)
			nodeIPs := make(map[string]string)
			for i := 0; i < 2 && i < len(nodes.Items); i++ {
				gatewayNodes = append(gatewayNodes, nodes.Items[i].Name)
				nodeIPs[nodes.Items[i].Status.Addresses[0].Address] = gatewayNodes[i]
			}

			By("create subnet")
			subnet := &kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock:             "11.15.0.0/16",
					GatewayType:           kubeovn.GWCentralizedType,
					GatewayNode:           strings.Join(gatewayNodes, ","),
					ExternalEgressGateway: egw,
					PolicyRoutingPriority: priority,
					PolicyRoutingTableID:  tableID,
				},
			}
			_, err = f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), subnet, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("validate subnet")
			err = f.WaitSubnetReady(name)
			Expect(err).NotTo(HaveOccurred())
			time.Sleep(5 * time.Second)

			subnet, err = f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(subnet.Spec.GatewayType).To(Equal(kubeovn.GWCentralizedType))
			Expect(subnet.Spec.ExternalEgressGateway).To(Equal(egw))
			Expect(subnet.Spec.PolicyRoutingPriority).To(Equal(priority))
			Expect(subnet.Spec.PolicyRoutingTableID).To(Equal(tableID))
			time.Sleep(5 * time.Second)

			ovsPods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=ovs"})
			Expect(err).NotTo(HaveOccurred())

			rulePrefix := fmt.Sprintf("%d:", priority)
			ruleSuffix := fmt.Sprintf("from %s lookup %d", subnet.Spec.CIDRBlock, tableID)
			routePrefix := fmt.Sprintf("default via %s ", egw)

			for _, pod := range ovsPods.Items {
				if nodeIPs[pod.Status.HostIP] == "" {
					continue
				}

				stdout, _, err := f.ExecToPodThroughAPI("ip rule show", "openvswitch", pod.Name, pod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())

				var found bool
				rules := strings.Split(stdout, "\n")
				for _, rule := range rules {
					if strings.HasPrefix(rule, rulePrefix) && strings.HasSuffix(rule, ruleSuffix) {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue())

				stdout, _, err = f.ExecToPodThroughAPI(fmt.Sprintf("ip route show table %d", tableID), "openvswitch", pod.Name, pod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(stdout).To(HavePrefix(routePrefix))
			}

			By("delete subnet")
			err = f.OvnClientSet.KubeovnV1().Subnets().Delete(context.Background(), name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			for _, pod := range ovsPods.Items {
				if nodeIPs[pod.Status.HostIP] == "" {
					continue
				}

				stdout, _, err := f.ExecToPodThroughAPI("ip rule show", "openvswitch", pod.Name, pod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())

				var found bool
				rules := strings.Split(stdout, "\n")
				for _, rule := range rules {
					if strings.HasPrefix(rule, rulePrefix) && strings.HasSuffix(rule, ruleSuffix) {
						found = true
						break
					}
				}
				Expect(found).NotTo(BeTrue())

				stdout, _, err = f.ExecToPodThroughAPI(fmt.Sprintf("ip route show table %d", tableID), "openvswitch", pod.Name, pod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(stdout).NotTo(HavePrefix(routePrefix))
			}
		})

		It("distributed gateway with external egress gateway", func() {
			nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes).NotTo(BeNil())
			Expect(nodes.Items).NotTo(BeEmpty())

			for _, node := range nodes.Items {
				Expect(node.Status.Addresses).NotTo(BeEmpty())
			}

			By("create namespace")
			namespace := f.GetName()
			_, err = f.KubeClientSet.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   namespace,
					Labels: map[string]string{"e2e": "true"},
				},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			name := f.GetName()
			egw := nodes.Items[0].Status.Addresses[0].Address
			priority, tableID := uint32(1003), uint32(1004)

			var selectedNode *corev1.Node
			for i, node := range nodes.Items {
				if node.Spec.Unschedulable {
					continue
				}

				var unschedulable bool
				for _, t := range node.Spec.Taints {
					if t.Effect == corev1.TaintEffectNoSchedule {
						unschedulable = true
						break
					}
				}
				if !unschedulable {
					selectedNode = &nodes.Items[i]
					break
				}
			}
			Expect(selectedNode).NotTo(BeNil())

			By("create subnet")
			s := kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock:             "11.16.0.0/16",
					GatewayType:           kubeovn.GWDistributedType,
					ExternalEgressGateway: egw,
					PolicyRoutingPriority: priority,
					PolicyRoutingTableID:  tableID,
					Namespaces:            []string{namespace},
				},
			}
			_, err = f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), &s, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("validate subnet")
			err = f.WaitSubnetReady(name)
			Expect(err).NotTo(HaveOccurred())
			time.Sleep(5 * time.Second)

			subnet, err := f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(subnet.Spec.GatewayType).To(Equal(kubeovn.GWDistributedType))
			Expect(subnet.Spec.ExternalEgressGateway).To(Equal(egw))
			Expect(subnet.Spec.PolicyRoutingPriority).To(Equal(priority))
			Expect(subnet.Spec.PolicyRoutingTableID).To(Equal(tableID))

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            name,
							Image:           "kubeovn/pause:3.2",
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
					NodeSelector: map[string]string{"kubernetes.io/hostname": selectedNode.Name},
				},
			}

			By("create pod")
			_, err = f.KubeClientSet.CoreV1().Pods(namespace).Create(context.Background(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			_, err = f.WaitPodReady(name, namespace)
			Expect(err).NotTo(HaveOccurred())

			pod, err = f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Annotations[util.AllocatedAnnotation]).To(Equal("true"))
			Expect(pod.Annotations[util.RoutedAnnotation]).To(Equal("true"))
			time.Sleep(1 * time.Second)

			ovsPods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=ovs"})
			Expect(err).NotTo(HaveOccurred())

			rulePrefix := fmt.Sprintf("%d:", priority)
			ruleSuffix := fmt.Sprintf("from %s lookup %d", pod.Status.PodIP, tableID)
			routePrefix := fmt.Sprintf("default via %s ", egw)

			for _, ovsPod := range ovsPods.Items {
				if ovsPod.Status.HostIP != selectedNode.Status.Addresses[0].Address {
					continue
				}

				stdout, _, err := f.ExecToPodThroughAPI("ip rule show", "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())

				var found bool
				rules := strings.Split(stdout, "\n")
				for _, rule := range rules {
					if strings.HasPrefix(rule, rulePrefix) && strings.HasSuffix(rule, ruleSuffix) {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue())

				stdout, _, err = f.ExecToPodThroughAPI(fmt.Sprintf("ip route show table %d", tableID), "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(stdout).To(HavePrefix(routePrefix))
			}

			By("delete pod")
			err = f.KubeClientSet.CoreV1().Pods(namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitPodDeleted(name, namespace)
			Expect(err).NotTo(HaveOccurred())
			time.Sleep(1 * time.Second)

			for _, ovsPod := range ovsPods.Items {
				if ovsPod.Status.HostIP != selectedNode.Status.Addresses[0].Address {
					continue
				}

				stdout, _, err := f.ExecToPodThroughAPI("ip rule show", "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())

				var found bool
				rules := strings.Split(stdout, "\n")
				for _, rule := range rules {
					if strings.HasPrefix(rule, rulePrefix) && strings.HasSuffix(rule, ruleSuffix) {
						found = true
						break
					}
				}
				Expect(found).NotTo(BeTrue())

				stdout, _, err = f.ExecToPodThroughAPI(fmt.Sprintf("ip route show table %d", tableID), "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(stdout).To(HavePrefix(routePrefix))
			}

			By("delete subnet")
			err = f.OvnClientSet.KubeovnV1().Subnets().Delete(context.Background(), name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			for _, ovsPod := range ovsPods.Items {
				if ovsPod.Status.HostIP != selectedNode.Status.Addresses[0].Address {
					continue
				}

				stdout, _, err := f.ExecToPodThroughAPI(fmt.Sprintf("ip route show table %d", tableID), "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(stdout).NotTo(HavePrefix(routePrefix))
			}
		})
	})
})
