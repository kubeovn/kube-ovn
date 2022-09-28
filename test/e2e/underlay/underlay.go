package underlay

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
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

const (
	UnderlayInterface = "eth1"
	VlanInterface     = "vlan1"

	ProviderNetwork = "net1"
	Vlan            = "vlan-e2e"
	Subnet          = "e2e-underlay"
	Namespace       = "underlay"

	testImage = "kubeovn/pause:3.2"
)

var (
	ExchangeLinkName bool

	VlanID = os.Getenv("VLAN_ID")

	cidr    string
	nodeIPs []string

	nodeMac    = make(map[string]string)
	nodeAddrs  = make(map[string][]string)
	nodeRoutes = make(map[string][]string)
	nodeMTU    = make(map[string]int)
)

func init() {
	rand.Seed(time.Now().UnixNano())
	ExchangeLinkName = rand.Intn(2) != 0
}

func SetCIDR(s string) {
	cidr = s
}
func AddNodeIP(ip string) {
	nodeIPs = append(nodeIPs, ip)
}

func SetNodeMac(node, mac string) {
	nodeMac[node] = mac
}
func AddNodeAddrs(node, addr string) {
	nodeAddrs[node] = append(nodeAddrs[node], addr)
}
func AddNodeRoutes(node, route string) {
	nodeRoutes[node] = append(nodeRoutes[node], route)
}
func SetNodeMTU(node string, mtu int) {
	nodeMTU[node] = mtu
}

var _ = Describe("[Underlay]", func() {
	providerInterface := UnderlayInterface
	if VlanID != "" {
		providerInterface = VlanInterface
	}

	f := framework.NewFramework("underlay", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))

	Context("[Provider Network]", func() {
		It("normal", func() {
			By("validate node labels")
			nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())

			for _, node := range nodes.Items {
				Expect(node.Labels[fmt.Sprintf(util.ProviderNetworkExcludeTemplate, ProviderNetwork)]).To(BeEmpty())
				Expect(node.Labels[fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, ProviderNetwork)]).To(Equal(providerInterface))
				Expect(node.Labels[fmt.Sprintf(util.ProviderNetworkReadyTemplate, ProviderNetwork)]).To(Equal("true"))
				Expect(node.Labels[fmt.Sprintf(util.ProviderNetworkMtuTemplate, ProviderNetwork)]).NotTo(BeEmpty())
			}

			By("validate OVS bridge")
			ovsPods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=ovs"})
			Expect(err).NotTo(HaveOccurred())
			Expect(ovsPods).NotTo(BeNil())
			for _, node := range nodes.Items {
				var ovsPod *corev1.Pod
				for _, pod := range ovsPods.Items {
					if pod.Spec.NodeName == node.Name {
						ovsPod = &pod
						break
					}
				}
				Expect(ovsPod).NotTo(BeNil())

				nic, br := providerInterface, util.ExternalBridgeName(ProviderNetwork)
				if ExchangeLinkName {
					nic, br = br, nic
				}
				stdout, _, err := f.ExecToPodThroughAPI("ip addr show "+nic, "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())

				addrNotFound := make([]bool, len(nodeAddrs[node.Name]))
				for _, s := range strings.Split(stdout, "\n") {
					s = strings.TrimSpace(s)
					for i, addr := range nodeAddrs[node.Name] {
						if !strings.HasPrefix(s, fmt.Sprintf("inet %s ", addr)) && !strings.HasPrefix(s, fmt.Sprintf("inet6 %s ", addr)) {
							addrNotFound[i] = true
							break
						}
					}
				}
				for _, found := range addrNotFound {
					Expect(found).To(BeTrue())
				}

				stdout, _, err = f.ExecToPodThroughAPI("ovs-vsctl list-ports "+br, "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())

				var portFound bool
				for _, port := range strings.Split(stdout, "\n") {
					if port == nic {
						portFound = true
						break
					}
				}
				Expect(portFound).To(BeTrue())

				stdout, _, err = f.ExecToPodThroughAPI("ip addr show "+br, "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())

				var isUp bool
				addrFound := make([]bool, len(nodeAddrs[node.Name]))
				for i, s := range strings.Split(stdout, "\n") {
					if i == 0 {
						idx1, idx2 := strings.IndexRune(s, '<'), strings.IndexRune(s, '>')
						if idx1 > 0 && idx2 > idx1+1 {
							for _, state := range strings.Split(s[idx1+1:idx2], ",") {
								if state == "UP" {
									isUp = true
									break
								}
							}
						}
						continue
					}
					if VlanID == "" {
						if i == 1 {
							if mac := nodeMac[node.Name]; mac != "" {
								Expect(strings.TrimSpace(s)).To(HavePrefix("link/ether %s ", mac))
								continue
							}
						}

						s = strings.TrimSpace(s)
						for i, addr := range nodeAddrs[node.Name] {
							if strings.HasPrefix(s, fmt.Sprintf("inet %s ", addr)) || strings.HasPrefix(s, fmt.Sprintf("inet6 %s ", addr)) {
								addrFound[i] = true
								break
							}
						}
					}
				}
				Expect(isUp).To(BeTrue())
				if VlanID == "" {
					for _, found := range addrFound {
						Expect(found).To(BeTrue())
					}
				}
			}
		})

		It("node annotation", func() {
			By("add exclude annotation")
			nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())

			for _, node := range nodes.Items {
				newNode := node.DeepCopy()
				newNode.Annotations[fmt.Sprintf(util.ProviderNetworkExcludeTemplate, ProviderNetwork)] = "true"
				_, err = f.KubeClientSet.CoreV1().Nodes().Update(context.Background(), newNode, metav1.UpdateOptions{})
				Expect(err).NotTo(HaveOccurred())
			}

			By("wait provider network to be ready")
			time.Sleep(3 * time.Second)
			err = f.WaitProviderNetworkReady(ProviderNetwork)
			Expect(err).NotTo(HaveOccurred())

			By("validate provider network")
			pn, err := f.OvnClientSet.KubeovnV1().ProviderNetworks().Get(context.Background(), ProviderNetwork, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			for _, node := range nodes.Items {
				Expect(util.ContainsString(pn.Spec.ExcludeNodes, node.Name)).To(BeTrue())
			}

			By("validate node annotation")
			nodes, err = f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())

			for _, node := range nodes.Items {
				Expect(node.Annotations).NotTo(HaveKey(fmt.Sprintf(util.ProviderNetworkExcludeTemplate, ProviderNetwork)))
			}

			By("restore provider network")
			pn, err = f.OvnClientSet.KubeovnV1().ProviderNetworks().Get(context.Background(), ProviderNetwork, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			newPn := pn.DeepCopy()
			newPn.Spec.ExcludeNodes = nil
			_, err = f.OvnClientSet.KubeovnV1().ProviderNetworks().Update(context.Background(), newPn, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("wait provider network to be ready")
			time.Sleep(3 * time.Second)
			err = f.WaitProviderNetworkReady(ProviderNetwork)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("[Subnet]", func() {
		BeforeEach(func() {
			err := f.OvnClientSet.KubeovnV1().Subnets().Delete(context.Background(), f.GetName(), metav1.DeleteOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				klog.Fatalf("failed to delete subnet %s: %v", f.GetName(), err)
			}
		})
		AfterEach(func() {
			err := f.OvnClientSet.KubeovnV1().Subnets().Delete(context.Background(), f.GetName(), metav1.DeleteOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				klog.Fatalf("failed to delete subnet %s: %v", f.GetName(), err)
			}
		})

		It("logical gateway", func() {
			name := f.GetName()

			By("create subnet")
			subnet := &kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock:      "99.11.0.0/16",
					Vlan:           Vlan,
					LogicalGateway: true,
				},
			}
			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), subnet, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("validate subnet")
			err = f.WaitSubnetReady(subnet.Name)
			Expect(err).NotTo(HaveOccurred())

			By("validate OVN logical router port")
			ovnPods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=ovn-central"})
			Expect(err).NotTo(HaveOccurred())
			Expect(ovnPods).NotTo(BeNil())

			ovnPod := ovnPods.Items[0]
			lsp := fmt.Sprintf("%s-%s", name, util.DefaultVpc)
			cmd := fmt.Sprintf("ovn-nbctl --no-heading --columns=_uuid find logical_switch_port name=%s", lsp)
			uuid, _, err := f.ExecToPodThroughAPI(cmd, "ovn-central", ovnPod.Name, ovnPod.Namespace, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(uuid).NotTo(BeEmpty())

			lrp := fmt.Sprintf("%s-%s", util.DefaultVpc, name)
			cmd = fmt.Sprintf("ovn-nbctl --no-heading --columns=_uuid find logical_router_port name=%s", lrp)
			uuid, _, err = f.ExecToPodThroughAPI(cmd, "ovn-central", ovnPod.Name, ovnPod.Namespace, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(uuid).NotTo(BeEmpty())
		})

		It("disable gateway check", func() {
			name := f.GetName()

			By("create subnet")
			subnet := &kubeovn.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovn.SubnetSpec{
					CIDRBlock:           "99.12.0.0/16",
					Vlan:                Vlan,
					DisableGatewayCheck: true,
				},
			}
			_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), subnet, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("validate subnet")
			err = f.WaitSubnetReady(subnet.Name)
			Expect(err).NotTo(HaveOccurred())

			By("create pod")
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        name,
					Namespace:   Namespace,
					Annotations: map[string]string{util.LogicalSwitchAnnotation: subnet.Name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            name,
							Image:           "kubeovn/pause:3.2",
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
				},
			}
			_, err = f.KubeClientSet.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			_, err = f.WaitPodReady(pod.Name, pod.Namespace)
			Expect(err).NotTo(HaveOccurred())

			By("delete pod")
			err = f.KubeClientSet.CoreV1().Pods(pod.Namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("[Pod]", func() {
		var cniPods map[string]corev1.Pod
		BeforeEach(func() {
			nodeList, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(nodeList).NotTo(BeNil())

			podList, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=kube-ovn-cni"})
			Expect(err).NotTo(HaveOccurred())
			Expect(podList).NotTo(BeNil())
			Expect(len(podList.Items)).To(Equal(len(nodeList.Items)))

			cniPods = make(map[string]corev1.Pod)
			for _, node := range nodeList.Items {
				var cniPod *corev1.Pod
				for _, pod := range podList.Items {
					if pod.Spec.NodeName == node.Name {
						cniPod = &pod
						break
					}
				}
				Expect(cniPod).NotTo(BeNil())
				cniPods[node.Name] = *cniPod
			}
		})

		Context("[MTU]", func() {
			BeforeEach(func() {
				err := f.KubeClientSet.CoreV1().Pods(Namespace).Delete(context.Background(), f.GetName(), metav1.DeleteOptions{})
				if err != nil && !k8serrors.IsNotFound(err) {
					klog.Fatalf("failed to delete pod %s: %v", f.GetName(), err)
				}
			})
			AfterEach(func() {
				err := f.KubeClientSet.CoreV1().Pods(Namespace).Delete(context.Background(), f.GetName(), metav1.DeleteOptions{})
				if err != nil && !k8serrors.IsNotFound(err) {
					klog.Fatalf("failed to delete pod %s: %v", f.GetName(), err)
				}
			})

			It("normal", func() {
				By("create pod")
				var autoMount bool
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:        f.GetName(),
						Namespace:   Namespace,
						Labels:      map[string]string{"e2e": "true"},
						Annotations: map[string]string{util.LogicalSwitchAnnotation: Subnet},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:            f.GetName(),
								Image:           testImage,
								ImagePullPolicy: corev1.PullIfNotPresent,
							},
						},
						AutomountServiceAccountToken: &autoMount,
					},
				}
				_, err := f.KubeClientSet.CoreV1().Pods(Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
				pod, err = f.WaitPodReady(pod.Name, Namespace)
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.Spec.NodeName).NotTo(BeEmpty())

				By("get cni pod")
				cniPod, ok := cniPods[pod.Spec.NodeName]
				Expect(ok).To(BeTrue())

				By("get pod's netns")
				cmd := fmt.Sprintf("ovs-vsctl --no-heading --columns=external_ids find interface external-ids:pod_name=%s external-ids:pod_namespace=%s", pod.Name, Namespace)
				stdout, _, err := f.ExecToPodThroughAPI(cmd, "cni-server", cniPod.Name, cniPod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())
				var netns string
				for _, field := range strings.Fields(stdout) {
					if strings.HasPrefix(field, "pod_netns=") {
						netns = strings.TrimPrefix(field, "pod_netns=")
						netns = strings.Trim(netns[:len(netns)-1], `"`)
						break
					}
				}
				Expect(netns).NotTo(BeEmpty())

				By("validate pod's MTU")
				cmd = fmt.Sprintf("nsenter --net=%s ip link show eth0", netns)
				stdout, _, err = f.ExecToPodThroughAPI(cmd, "cni-server", cniPod.Name, cniPod.Namespace, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(stdout).To(ContainSubstring(" mtu %d ", nodeMTU[pod.Spec.NodeName]))
			})
		})

		Context("[Connectivity]", func() {
			Context("[Host-Pod]", func() {
				if VlanID != "" {
					return
				}

				BeforeEach(func() {
					err := f.KubeClientSet.CoreV1().Pods(Namespace).Delete(context.Background(), f.GetName(), metav1.DeleteOptions{})
					if err != nil && !k8serrors.IsNotFound(err) {
						klog.Fatalf("failed to delete pod %s: %v", f.GetName(), err)
					}
				})
				AfterEach(func() {
					err := f.KubeClientSet.CoreV1().Pods(Namespace).Delete(context.Background(), f.GetName(), metav1.DeleteOptions{})
					if err != nil && !k8serrors.IsNotFound(err) {
						klog.Fatalf("failed to delete pod %s: %v", f.GetName(), err)
					}
				})

				It("hp", func() {
					By("create pod")
					var autoMount bool
					pod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:        f.GetName(),
							Namespace:   Namespace,
							Labels:      map[string]string{"e2e": "true"},
							Annotations: map[string]string{util.LogicalSwitchAnnotation: Subnet},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:            f.GetName(),
									Image:           testImage,
									ImagePullPolicy: corev1.PullIfNotPresent,
								},
							},
							AutomountServiceAccountToken: &autoMount,
						},
					}
					_, err := f.KubeClientSet.CoreV1().Pods(Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
					Expect(err).NotTo(HaveOccurred())
					pod, err = f.WaitPodReady(pod.Name, Namespace)
					Expect(err).NotTo(HaveOccurred())
					Expect(pod.Spec.NodeName).NotTo(BeEmpty())

					By("get pod's netns")
					cniPod := cniPods[pod.Spec.NodeName]
					cmd := fmt.Sprintf("ovs-vsctl --no-heading --columns=external_ids find interface external-ids:pod_name=%s external-ids:pod_namespace=%s", pod.Name, Namespace)
					stdout, _, err := f.ExecToPodThroughAPI(cmd, "cni-server", cniPod.Name, cniPod.Namespace, nil)
					Expect(err).NotTo(HaveOccurred())
					var netns string
					for _, field := range strings.Fields(stdout) {
						if strings.HasPrefix(field, "pod_netns=") {
							netns = strings.TrimPrefix(field, "pod_netns=")
							netns = strings.Trim(netns[:len(netns)-1], `"`)
							break
						}
					}
					Expect(netns).NotTo(BeEmpty())

					By("get host IP")
					var hostIP string
					for _, addr := range nodeAddrs[pod.Spec.NodeName] {
						if util.CIDRContainIP(cidr, strings.Split(addr, "/")[0]) {
							hostIP = strings.Split(addr, "/")[0]
							break
						}
					}
					Expect(hostIP).ToNot(BeEmpty())

					By("ping host")
					cmd = fmt.Sprintf("nsenter --net=%s ping -c1 -W1 %s", netns, hostIP)
					stdout, _, err = f.ExecToPodThroughAPI(cmd, "cni-server", cniPod.Name, cniPod.Namespace, nil)
					Expect(err).NotTo(HaveOccurred())
					Expect(stdout).To(ContainSubstring(" 0% packet loss"))
				})
			})

			Context("[Host-Host-Pod]", func() {
				if VlanID != "" {
					return
				}

				BeforeEach(func() {
					err := f.KubeClientSet.CoreV1().Pods(Namespace).Delete(context.Background(), f.GetName(), metav1.DeleteOptions{})
					if err != nil && !k8serrors.IsNotFound(err) {
						klog.Fatalf("failed to delete pod %s: %v", f.GetName(), err)
					}
				})
				AfterEach(func() {
					err := f.KubeClientSet.CoreV1().Pods(Namespace).Delete(context.Background(), f.GetName(), metav1.DeleteOptions{})
					if err != nil && !k8serrors.IsNotFound(err) {
						klog.Fatalf("failed to delete pod %s: %v", f.GetName(), err)
					}
				})

				It("hhp", func() {
					if len(cniPods) < 2 {
						return
					}

					By("select nodes")
					nodes := make([]string, 0, 2)
					for node := range cniPods {
						nodes = append(nodes, node)
						if len(nodes) == 2 {
							break
						}
					}
					Expect(len(nodes)).To(Equal(2))

					By("create pod")
					var autoMount bool
					pod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:        f.GetName(),
							Namespace:   Namespace,
							Labels:      map[string]string{"e2e": "true"},
							Annotations: map[string]string{util.LogicalSwitchAnnotation: Subnet},
						},
						Spec: corev1.PodSpec{
							NodeName: nodes[0],
							Containers: []corev1.Container{
								{
									Name:            f.GetName(),
									Image:           testImage,
									ImagePullPolicy: corev1.PullIfNotPresent,
								},
							},
							AutomountServiceAccountToken: &autoMount,
						},
					}
					_, err := f.KubeClientSet.CoreV1().Pods(Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
					Expect(err).NotTo(HaveOccurred())
					pod, err = f.WaitPodReady(pod.Name, Namespace)
					Expect(err).NotTo(HaveOccurred())
					Expect(pod.Spec.NodeName).NotTo(BeEmpty())

					By("get pod's netns")
					cniPod := cniPods[pod.Spec.NodeName]
					cmd := fmt.Sprintf("ovs-vsctl --no-heading --columns=external_ids find interface external-ids:pod_name=%s external-ids:pod_namespace=%s", pod.Name, Namespace)
					stdout, _, err := f.ExecToPodThroughAPI(cmd, "cni-server", cniPod.Name, cniPod.Namespace, nil)
					Expect(err).NotTo(HaveOccurred())
					var netns string
					for _, field := range strings.Fields(stdout) {
						if strings.HasPrefix(field, "pod_netns=") {
							netns = strings.TrimPrefix(field, "pod_netns=")
							netns = strings.Trim(netns[:len(netns)-1], `"`)
							break
						}
					}
					Expect(netns).NotTo(BeEmpty())

					By("get host IP")
					var hostIP string
					for _, addr := range nodeAddrs[nodes[1]] {
						if util.CIDRContainIP(cidr, strings.Split(addr, "/")[0]) {
							hostIP = strings.Split(addr, "/")[0]
							break
						}
					}
					Expect(hostIP).ToNot(BeEmpty())

					By("ping host")
					cmd = fmt.Sprintf("nsenter --net=%s ping -c1 -W1 %s", netns, hostIP)
					stdout, _, err = f.ExecToPodThroughAPI(cmd, "cni-server", cniPod.Name, cniPod.Namespace, nil)
					Expect(err).NotTo(HaveOccurred())
					Expect(stdout).To(ContainSubstring(" 0% packet loss"))
				})
			})

			Context("Pod-Host-Host-Pod", func() {
				BeforeEach(func() {
					for i := 0; i < len(cniPods); i++ {
						name := fmt.Sprintf("%s-%d", f.GetName(), i+1)
						err := f.KubeClientSet.CoreV1().Pods(Namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
						if err != nil && !k8serrors.IsNotFound(err) {
							klog.Fatalf("failed to delete pod %s: %v", name, err)
						}
					}
				})
				AfterEach(func() {
					for i := 0; i < len(cniPods); i++ {
						name := fmt.Sprintf("%s-%d", f.GetName(), i+1)
						err := f.KubeClientSet.CoreV1().Pods(Namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
						if err != nil && !k8serrors.IsNotFound(err) {
							klog.Fatalf("failed to delete pod %s: %v", name, err)
						}
					}
				})

				It("phhp", func() {
					if len(cniPods) < 2 {
						return
					}

					By("select nodes")
					nodes := make([]string, 0, len(cniPods))
					for node := range cniPods {
						nodes = append(nodes, node)
					}

					By("create pods")
					name := f.GetName()
					pods := make([]*corev1.Pod, 2)
					var autoMount bool
					for i := range nodes {
						pods[i] = &corev1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:        fmt.Sprintf("%s-%d", name, i+1),
								Namespace:   Namespace,
								Labels:      map[string]string{"e2e": "true"},
								Annotations: map[string]string{util.LogicalSwitchAnnotation: Subnet},
							},
							Spec: corev1.PodSpec{
								NodeName: nodes[i],
								Containers: []corev1.Container{
									{
										Name:            name,
										Image:           testImage,
										ImagePullPolicy: corev1.PullIfNotPresent,
									},
								},
								AutomountServiceAccountToken: &autoMount,
							},
						}
						_, err := f.KubeClientSet.CoreV1().Pods(Namespace).Create(context.Background(), pods[i], metav1.CreateOptions{})
						Expect(err).NotTo(HaveOccurred())
						pods[i], err = f.WaitPodReady(pods[i].Name, Namespace)
						Expect(err).NotTo(HaveOccurred())
					}

					for i := range pods {
						By("get pod's netns")
						cmd := fmt.Sprintf("ovs-vsctl --no-heading --columns=external_ids find interface external-ids:pod_name=%s external-ids:pod_namespace=%s", pods[i].Name, Namespace)
						stdout, _, err := f.ExecToPodThroughAPI(cmd, "cni-server", cniPods[nodes[i]].Name, cniPods[nodes[i]].Namespace, nil)
						Expect(err).NotTo(HaveOccurred())
						var netns string
						for _, field := range strings.Fields(stdout) {
							if strings.HasPrefix(field, "pod_netns=") {
								netns = strings.TrimPrefix(field, "pod_netns=")
								netns = strings.Trim(netns[:len(netns)-1], `"`)
								break
							}
						}
						Expect(netns).NotTo(BeEmpty())

						By("ping another pod")
						cmd = fmt.Sprintf("nsenter --net=%s ping -c1 -W1 %s", netns, pods[(i+len(pods)+1)%len(pods)].Status.PodIP)
						stdout, _, err = f.ExecToPodThroughAPI(cmd, "cni-server", cniPods[nodes[i]].Name, cniPods[nodes[i]].Namespace, nil)
						Expect(err).NotTo(HaveOccurred())
						Expect(stdout).To(ContainSubstring(" 0% packet loss"))
					}
				})
			})

			Context("[Overlay-Underlay]", func() {
				if VlanID != "" {
					return
				}

				defaultSubnet, err := f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), util.DefaultSubnet, metav1.GetOptions{})
				if err != nil && !k8serrors.IsNotFound(err) {
					klog.Fatalf("failed to get subnet %s: %v", util.DefaultSubnet, err)
				}
				if defaultSubnet.Spec.LogicalGateway {
					return
				}

				overlayNamespace := "default"

				BeforeEach(func() {
					err := f.KubeClientSet.CoreV1().Pods(Namespace).Delete(context.Background(), f.GetName(), metav1.DeleteOptions{})
					if err != nil && !k8serrors.IsNotFound(err) {
						klog.Fatalf("failed to delete pod %s/%s: %v", Namespace, f.GetName(), err)
					}
					err = f.KubeClientSet.CoreV1().Pods(overlayNamespace).Delete(context.Background(), f.GetName(), metav1.DeleteOptions{})
					if err != nil && !k8serrors.IsNotFound(err) {
						klog.Fatalf("failed to delete pod %s/%s: %v", overlayNamespace, f.GetName(), err)
					}
				})
				AfterEach(func() {
					err := f.KubeClientSet.CoreV1().Pods(Namespace).Delete(context.Background(), f.GetName(), metav1.DeleteOptions{})
					if err != nil && !k8serrors.IsNotFound(err) {
						klog.Fatalf("failed to delete pod %s/%s: %v", Namespace, f.GetName(), err)
					}
					err = f.KubeClientSet.CoreV1().Pods(overlayNamespace).Delete(context.Background(), f.GetName(), metav1.DeleteOptions{})
					if err != nil && !k8serrors.IsNotFound(err) {
						klog.Fatalf("failed to delete pod %s/%s: %v", overlayNamespace, f.GetName(), err)
					}
				})

				It("o2u", func() {
					if strings.EqualFold(os.Getenv("IPV6"), "true") {
						return
					}

					By("create underlay pod")
					var autoMount bool
					upod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:        f.GetName(),
							Namespace:   Namespace,
							Labels:      map[string]string{"e2e": "true"},
							Annotations: map[string]string{util.LogicalSwitchAnnotation: Subnet},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:            f.GetName(),
									Image:           testImage,
									ImagePullPolicy: corev1.PullIfNotPresent,
								},
							},
							AutomountServiceAccountToken: &autoMount,
						},
					}
					_, err := f.KubeClientSet.CoreV1().Pods(upod.Namespace).Create(context.Background(), upod, metav1.CreateOptions{})
					Expect(err).NotTo(HaveOccurred())
					upod, err = f.WaitPodReady(upod.Name, upod.Namespace)
					Expect(err).NotTo(HaveOccurred())
					Expect(upod.Spec.NodeName).NotTo(BeEmpty())

					By("create overlay pod")
					opod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      f.GetName(),
							Namespace: overlayNamespace,
							Labels:    map[string]string{"e2e": "true"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:            f.GetName(),
									Image:           testImage,
									ImagePullPolicy: corev1.PullIfNotPresent,
								},
							},
							AutomountServiceAccountToken: &autoMount,
						},
					}
					_, err = f.KubeClientSet.CoreV1().Pods(opod.Namespace).Create(context.Background(), opod, metav1.CreateOptions{})
					Expect(err).NotTo(HaveOccurred())
					opod, err = f.WaitPodReady(opod.Name, opod.Namespace)
					Expect(err).NotTo(HaveOccurred())

					By("get overlay pod's netns")
					cniPod := cniPods[opod.Spec.NodeName]
					cmd := fmt.Sprintf("ovs-vsctl --no-heading --columns=external_ids find interface external-ids:pod_name=%s external-ids:pod_namespace=%s", opod.Name, opod.Namespace)
					stdout, _, err := f.ExecToPodThroughAPI(cmd, "cni-server", cniPod.Name, cniPod.Namespace, nil)
					Expect(err).NotTo(HaveOccurred())
					var netns string
					for _, field := range strings.Fields(stdout) {
						if strings.HasPrefix(field, "pod_netns=") {
							netns = strings.TrimPrefix(field, "pod_netns=")
							netns = strings.Trim(netns[:len(netns)-1], `"`)
							break
						}
					}
					Expect(netns).NotTo(BeEmpty())

					By("ping underlay pod")
					cmd = fmt.Sprintf("nsenter --net=%s ping -c1 -W1 %s", netns, upod.Status.PodIP)
					stdout, _, err = f.ExecToPodThroughAPI(cmd, "cni-server", cniPod.Name, cniPod.Namespace, nil)
					Expect(err).NotTo(HaveOccurred())
					Expect(stdout).To(ContainSubstring(" 0% packet loss"))
				})
			})
		})
	})

	Context("[kubectl-ko]", func() {
		BeforeEach(func() {
			err := f.KubeClientSet.CoreV1().Pods(Namespace).Delete(context.Background(), f.GetName(), metav1.DeleteOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				klog.Fatalf("failed to delete pod %s: %v", f.GetName(), err)
			}
		})
		AfterEach(func() {
			err := f.KubeClientSet.CoreV1().Pods(Namespace).Delete(context.Background(), f.GetName(), metav1.DeleteOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				klog.Fatalf("failed to delete pod %s: %v", f.GetName(), err)
			}
		})

		It("trace", func() {
			By("create pod")
			var autoMount bool
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        f.GetName(),
					Namespace:   Namespace,
					Labels:      map[string]string{"e2e": "true"},
					Annotations: map[string]string{util.LogicalSwitchAnnotation: Subnet},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            f.GetName(),
							Image:           testImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
					AutomountServiceAccountToken: &autoMount,
				},
			}
			_, err := f.KubeClientSet.CoreV1().Pods(Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			pod, err = f.WaitPodReady(pod.Name, Namespace)
			Expect(err).NotTo(HaveOccurred())

			dst := "114.114.114.114"
			if util.CheckProtocol(pod.Status.PodIP) == kubeovn.ProtocolIPv6 {
				dst = "2400:3200::1"
			}

			output, err := exec.Command("kubectl", "ko", "trace", fmt.Sprintf("%s/%s", Namespace, pod.Name), dst, "icmp").CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(output))

			output, err = exec.Command("kubectl", "ko", "trace", fmt.Sprintf("%s/%s", Namespace, pod.Name), dst, "tcp", "80").CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(output))

			output, err = exec.Command("kubectl", "ko", "trace", fmt.Sprintf("%s/%s", Namespace, pod.Name), dst, "udp", "53").CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(output))
		})
	})
})
