package node

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

const vlanNic = "eth0"

var vlanBr = util.ExternalBridgeName("provider")

//go:embed network.json
var networkJSON []byte

type nodeNetwork struct {
	Gateway             string
	IPAddress           string
	IPPrefixLen         int
	IPv6Gateway         string
	GlobalIPv6Address   string
	GlobalIPv6PrefixLen int
	MacAddress          string
}

var _ = Describe("[Underlay Node]", func() {
	f := framework.NewFramework("node", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))

	var network *nodeNetwork
	BeforeEach(func() {
		if len(networkJSON) != 0 {
			network = new(nodeNetwork)
			Expect(json.Unmarshal(networkJSON, network)).NotTo(HaveOccurred())
		}
	})

	It("Single NIC", func() {
		nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(nodes).NotTo(BeNil())
		Expect(len(nodes.Items)).NotTo(BeZero())

		nodeIPs := make([]string, 0, len(nodes.Items)*2)
		nodeRoutes := make([]string, 0, len(nodes.Items)*4)
		if network != nil {
			if network.IPAddress != "" {
				addr := fmt.Sprintf("%s/%d", network.IPAddress, network.IPPrefixLen)
				nodeIPs = append(nodeIPs, addr)
				_, ipnet, err := net.ParseCIDR(addr)
				Expect(err).NotTo(HaveOccurred())
				nodeRoutes = append(nodeRoutes, fmt.Sprintf("%s ", ipnet.String()))
			}
			if network.GlobalIPv6Address != "" {
				addr := fmt.Sprintf("%s/%d", network.GlobalIPv6Address, network.GlobalIPv6PrefixLen)
				nodeIPs = append(nodeIPs, addr)
				_, ipnet, err := net.ParseCIDR(addr)
				Expect(err).NotTo(HaveOccurred())
				nodeRoutes = append(nodeRoutes, fmt.Sprintf("%s ", ipnet.String()))
			}
			if network.Gateway != "" {
				nodeRoutes = append(nodeRoutes, fmt.Sprintf("default via %s ", network.Gateway))
			}
			if network.IPv6Gateway != "" {
				nodeRoutes = append(nodeRoutes, fmt.Sprintf("default via %s ", network.IPv6Gateway))
			}
		} else {
			for _, node := range nodes.Items {
				if node.Name == "kube-ovn-control-plane" {
					ipv4, ipv6 := util.GetNodeInternalIP(node)
					if ipv4 != "" {
						nodeIPs = append(nodeIPs, ipv4+"/")
					}
					if ipv6 != "" {
						nodeIPs = append(nodeIPs, ipv6+"/")
					}
					break
				}
			}
		}
		Expect(nodeIPs).NotTo(BeEmpty())

		ovsPods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=ovs"})
		Expect(err).NotTo(HaveOccurred())
		Expect(ovsPods).NotTo(BeNil())

		var ovsPod *corev1.Pod
		for _, pod := range ovsPods.Items {
			for _, ip := range nodeIPs {
				if strings.HasPrefix(ip, pod.Status.HostIP+"/") {
					ovsPod = &pod
					break
				}
			}
			if ovsPod != nil {
				break
			}
		}
		Expect(ovsPod).NotTo(BeNil())

		stdout, _, err := f.ExecToPodThroughAPI("ovs-vsctl list-ports "+vlanBr, "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
		Expect(err).NotTo(HaveOccurred())

		var found bool
		for _, port := range strings.Split(stdout, "\n") {
			if port == vlanNic {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue())

		stdout, _, err = f.ExecToPodThroughAPI("ip addr show "+vlanBr, "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(stdout).NotTo(BeEmpty())

		ipFound := make([]bool, len(nodeIPs))
		for i, s := range strings.Split(stdout, "\n") {
			if i == 0 {
				var linkUp bool
				idx1, idx2 := strings.IndexRune(s, '<'), strings.IndexRune(s, '>')
				if idx1 > 0 && idx2 > idx1+1 {
					for _, state := range strings.Split(s[idx1+1:idx2], ",") {
						if state == "UP" {
							linkUp = true
							break
						}
					}
				}
				Expect(linkUp).To(BeTrue())
				continue
			}
			if i == 1 && network != nil && network.MacAddress != "" {
				Expect(strings.TrimSpace(s)).To(HavePrefix("link/ether %s ", network.MacAddress))
				continue
			}

			s = strings.TrimSpace(s)
			for i, ip := range nodeIPs {
				if strings.HasPrefix(s, "inet "+ip) || strings.HasPrefix(s, "inet6 "+ip) {
					ipFound[i] = true
					break
				}
			}
		}
		for _, found := range ipFound {
			Expect(found).To(BeTrue())
		}

		stdout, _, err = f.ExecToPodThroughAPI("ip addr show "+vlanNic, "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(stdout).NotTo(BeEmpty())

		var hasAddr bool
		for _, s := range strings.Split(stdout, "\n") {
			if s = strings.TrimSpace(s); strings.HasPrefix(s, "inet ") || strings.HasPrefix(s, "inet6 ") {
				ip, _, err := net.ParseCIDR(strings.Fields(s)[1])
				Expect(err).NotTo(HaveOccurred())
				if ip.IsLinkLocalUnicast() {
					continue
				}
				hasAddr = true
				break
			}
		}
		Expect(hasAddr).To(BeFalse())

		stdout, _, err = f.ExecToPodThroughAPI("ip -4 route show dev "+vlanBr, "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
		Expect(err).NotTo(HaveOccurred())
		routes := strings.Split(stdout, "\n")

		stdout, _, err = f.ExecToPodThroughAPI("ip -6 route show dev "+vlanBr, "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
		Expect(err).NotTo(HaveOccurred())
		routes = append(routes, strings.Split(stdout, "\n")...)

		routeFound := make([]bool, len(nodeRoutes))
		for i, prefix := range nodeRoutes {
			for _, route := range routes {
				if strings.HasPrefix(route, prefix) {
					routeFound[i] = true
					break
				}
			}
		}
		for _, found := range routeFound {
			Expect(found).To(BeTrue())
		}

		stdout, _, err = f.ExecToPodThroughAPI("ip -4 route show dev "+vlanNic, "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(stdout)).To(BeEmpty())

		stdout, _, err = f.ExecToPodThroughAPI("ip -6 route show dev "+vlanNic, "openvswitch", ovsPod.Name, ovsPod.Namespace, nil)
		Expect(err).NotTo(HaveOccurred())

		var hasRoute bool
		for _, s := range strings.Split(stdout, "\n") {
			if s = strings.TrimSpace(s); s == "" {
				continue
			}

			if !strings.HasPrefix(s, "default ") {
				addr := strings.Split(strings.Fields(s)[0], "/")[0]
				ip := net.ParseIP(addr)
				Expect(ip).NotTo(BeNil())
				if ip.IsLinkLocalUnicast() {
					continue
				}
			}

			hasRoute = true
			break
		}
		Expect(hasRoute).To(BeFalse())
	})
})
