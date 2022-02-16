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
	"k8s.io/klog/v2"

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

	isIPv6 := strings.EqualFold(os.Getenv("IPV6"), "true")

	Describe("Create", func() {
		It("only cidr", func() {
			name := f.GetName()
			af, cidr, protocol := 4, "11.10.0.0/16", kubeovn.ProtocolIPv4
			if isIPv6 {
				af, cidr, protocol = 6, "fd00:11:10::/112", kubeovn.ProtocolIPv6
			}
			gateway, _ := util.FirstIP(cidr)

			By("create subnet")
			s := kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock: cidr,
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
			Expect(subnet.Spec.Protocol).To(Equal(protocol))
			Expect(subnet.Spec.Namespaces).To(BeEmpty())
			Expect(subnet.Spec.ExcludeIps).To(ContainElement(gateway))
			Expect(subnet.Spec.Gateway).To(Equal(gateway))
			Expect(subnet.Spec.GatewayType).To(Equal(kubeovn.GWDistributedType))
			Expect(subnet.Spec.GatewayNode).To(BeEmpty())
			Expect(subnet.Spec.NatOutgoing).To(BeFalse())
			Expect(subnet.Spec.Private).To(BeFalse())
			Expect(subnet.Spec.AllowSubnets).To(BeEmpty())
			Expect(subnet.ObjectMeta.Finalizers).To(ContainElement(util.ControllerName))

			By("validate status")
			Expect(subnet.Status.ActivateGateway).To(BeEmpty())
			if isIPv6 {
				Expect(subnet.Status.V6AvailableIPs).To(Equal(float64(65533)))
			} else {
				Expect(subnet.Status.V4AvailableIPs).To(Equal(float64(65533)))
			}
			Expect(subnet.Status.V4UsingIPs).To(BeZero())
			Expect(subnet.Status.V6UsingIPs).To(BeZero())

			pods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=ovs"})
			Expect(err).NotTo(HaveOccurred())
			for _, pod := range pods.Items {
				stdout, _, err := f.ExecToPodThroughAPI(fmt.Sprintf("ip -%d route list root %s", af, subnet.Spec.CIDRBlock), "openvswitch", pod.Name, pod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(stdout).To(ContainSubstring("ovn0"))
			}
		})

		It("centralized gateway", func() {
			name := f.GetName()
			cidr := "11.11.0.0/16"
			if isIPv6 {
				cidr = "fd00:11:11::/112"
			}

			By("create subnet")
			s := kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock:   cidr,
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
			cidr := "11.12.0.0/16"
			if isIPv6 {
				cidr = "fd00:11:12::/112"
			}

			By("create subnet")
			s := &kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock: cidr,
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
			cidr := "11.13.0.0/16"
			if isIPv6 {
				cidr = "fd00:11:13::/112"
			}
			By("create subnet")
			s := kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock: cidr,
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
			cidr := "11.14.0.10/16"
			if isIPv6 {
				cidr = "fd00:11:14::10/112"
			}
			By("create subnet")
			s := &kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock: cidr,
				},
			}

			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), s, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitSubnetReady(name)
			Expect(err).NotTo(HaveOccurred())

			s, err = f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			if !isIPv6 {
				Expect(s.Spec.CIDRBlock).To(Equal("11.14.0.0/16"))
			} else {
				Expect(s.Spec.CIDRBlock).To(Equal("fd00:11:14::/112"))
			}

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
			priority, tableID := uint32(1001), uint32(1002)

			af, cidr := 4, "11.15.0.0/16"
			if isIPv6 {
				af, cidr = 6, "fd00:11:15::/112"
			}

			var egw string
			nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(nodes.Items[0])
			if isIPv6 {
				egw, _ = util.FirstIP(fmt.Sprintf("%s/%d", nodeIPv6, 64))
			} else {
				egw, _ = util.FirstIP(fmt.Sprintf("%s/%d", nodeIPv4, 16))
			}

			gatewayNodes := make([]string, 0, 2)
			nodeIPs := make(map[string]string)
			for i := 0; i < 2 && i < len(nodes.Items); i++ {
				gatewayNodes = append(gatewayNodes, nodes.Items[i].Name)
				nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(nodes.Items[i])
				if nodeIPv4 != "" {
					nodeIPs[nodeIPv4] = gatewayNodes[i]
				}
				if nodeIPv6 != "" {
					nodeIPs[nodeIPv6] = gatewayNodes[i]
				}
			}

			By("create subnet")
			subnet := &kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock:             cidr,
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

				stdout, _, err := f.ExecToPodThroughAPI(fmt.Sprintf("ip -%d rule show", af), "openvswitch", pod.Name, pod.Namespace, nil)
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

				stdout, _, err = f.ExecToPodThroughAPI(fmt.Sprintf("ip -%d route show table %d", af, tableID), "openvswitch", pod.Name, pod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(stdout).To(HavePrefix(routePrefix))
			}

			By("delete subnet")
			err = f.OvnClientSet.KubeovnV1().Subnets().Delete(context.Background(), name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			time.Sleep(5 * time.Second)

			for _, pod := range ovsPods.Items {
				if nodeIPs[pod.Status.HostIP] == "" {
					continue
				}

				stdout, _, err := f.ExecToPodThroughAPI(fmt.Sprintf("ip -%d rule show", af), "openvswitch", pod.Name, pod.Namespace, nil)
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

				stdout, _, err = f.ExecToPodThroughAPI(fmt.Sprintf("ip -%d route show table %d", af, tableID), "openvswitch", pod.Name, pod.Namespace, nil)
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
			priority, tableID := uint32(1003), uint32(1004)

			af, cidr := 4, "11.16.0.0/16"
			if isIPv6 {
				af, cidr = 6, "fd00:11:16::/112"
			}

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

			var egw string
			nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*selectedNode)
			if isIPv6 {
				egw, _ = util.FirstIP(fmt.Sprintf("%s/%d", nodeIPv6, 64))
			} else {
				egw, _ = util.FirstIP(fmt.Sprintf("%s/%d", nodeIPv4, 16))
			}

			By("create subnet")
			s := kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock:             cidr,
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

			pod, err = f.WaitPodReady(name, namespace)
			Expect(err).NotTo(HaveOccurred())

			ovsPods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=ovs"})
			Expect(err).NotTo(HaveOccurred())

			rulePrefix := fmt.Sprintf("%d:", priority)
			ruleSuffix := fmt.Sprintf("from %s lookup %d", pod.Status.PodIP, tableID)
			routePrefix := fmt.Sprintf("default via %s ", egw)

			var ovsPod *corev1.Pod
			for i := range ovsPods.Items {
				if ovsPods.Items[i].Spec.NodeName == selectedNode.Name {
					ovsPod = &ovsPods.Items[i]
					break
				}
			}
			Expect(ovsPod).NotTo(BeNil())

			stdout, _, err := f.ExecToPodThroughAPI(fmt.Sprintf("ip -%d rule show", af), "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
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

			stdout, _, err = f.ExecToPodThroughAPI(fmt.Sprintf("ip -%d route show table %d", af, tableID), "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(stdout).To(HavePrefix(routePrefix))

			By("delete pod")
			err = f.KubeClientSet.CoreV1().Pods(namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitPodDeleted(name, namespace)
			Expect(err).NotTo(HaveOccurred())

			stdout, _, err = f.ExecToPodThroughAPI(fmt.Sprintf("ip -%d rule show", af), "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
			Expect(err).NotTo(HaveOccurred())

			found = false
			rules = strings.Split(stdout, "\n")
			for _, rule := range rules {
				if strings.HasPrefix(rule, rulePrefix) && strings.HasSuffix(rule, ruleSuffix) {
					found = true
					break
				}
			}
			Expect(found).NotTo(BeTrue())

			stdout, _, err = f.ExecToPodThroughAPI(fmt.Sprintf("ip -%d route show table %d", af, tableID), "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(stdout).To(HavePrefix(routePrefix))

			By("delete subnet")
			err = f.OvnClientSet.KubeovnV1().Subnets().Delete(context.Background(), name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			time.Sleep(5 * time.Second)

			stdout, _, err = f.ExecToPodThroughAPI(fmt.Sprintf("ip -%d route show table %d", af, tableID), "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(stdout).NotTo(HavePrefix(routePrefix))
		})
	})
})
