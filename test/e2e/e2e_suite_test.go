package e2e

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	kubeovn "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	// tests to run
	_ "github.com/kubeovn/kube-ovn/test/e2e/ip"
	_ "github.com/kubeovn/kube-ovn/test/e2e/kubectl-ko"
	_ "github.com/kubeovn/kube-ovn/test/e2e/node"
	_ "github.com/kubeovn/kube-ovn/test/e2e/service"
	_ "github.com/kubeovn/kube-ovn/test/e2e/subnet"
	"github.com/kubeovn/kube-ovn/test/e2e/underlay"
)

//go:embed network.json
var networkJSON []byte

var nodeNetworks map[string]nodeNetwork

type nodeNetwork struct {
	Gateway             string
	IPAddress           string
	IPPrefixLen         int
	IPv6Gateway         string
	GlobalIPv6Address   string
	GlobalIPv6PrefixLen int
	MacAddress          string
}

func init() {
	if err := json.Unmarshal(networkJSON, &nodeNetworks); err != nil {
		panic(err)
	}
}

func TestE2e(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kube-OVN E2E Suite")
}

var _ = SynchronizedAfterSuite(func() {}, func() {
	f := framework.NewFramework("init", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))
	nss, err := f.KubeClientSet.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{LabelSelector: "e2e=true"})
	if err != nil {
		Fail(err.Error())
	}
	if nss != nil {
		for _, ns := range nss.Items {
			err := f.KubeClientSet.CoreV1().Namespaces().Delete(context.Background(), ns.Name, metav1.DeleteOptions{})
			if err != nil {
				Fail(err.Error())
			}
		}
	}

	err = f.OvnClientSet.KubeovnV1().Subnets().DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "e2e=true"})
	if err != nil {
		Fail(err.Error())
	}

	err = f.OvnClientSet.KubeovnV1().Vlans().DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "e2e=true"})
	if err != nil {
		Fail(err.Error())
	}

	err = f.OvnClientSet.KubeovnV1().ProviderNetworks().DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "e2e=true"})
	if err != nil {
		Fail(err.Error())
	}
})

var _ = SynchronizedBeforeSuite(func() []byte {
	subnetName := "static-ip"
	namespace := "static-ip"
	f := framework.NewFramework("init", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))

	_, err := f.KubeClientSet.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: map[string]string{"e2e": "true"}}}, metav1.CreateOptions{})
	if err != nil {
		Fail(err.Error())
	}

	s := kubeovn.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:   subnetName,
			Labels: map[string]string{"e2e": "true"},
		},
		Spec: kubeovn.SubnetSpec{
			CIDRBlock:  "12.10.0.0/16",
			Namespaces: []string{namespace},
		},
	}
	_, err = f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), &s, metav1.CreateOptions{})
	if err != nil {
		Fail(err.Error())
	}
	err = f.WaitSubnetReady(subnetName)
	if err != nil {
		Fail(err.Error())
	}

	// underlay
	var underlayNodeIPs []string
	var underlayCIDR, underlayGateway string
	for node, network := range nodeNetworks {
		underlay.SetNodeMac(node, network.MacAddress)
		if network.IPAddress != "" {
			underlay.AddNodeIP(network.IPAddress)
			underlayNodeIPs = append(underlayNodeIPs, network.IPAddress)
			underlay.AddNodeAddrs(node, fmt.Sprintf("%s/%d", network.IPAddress, network.IPPrefixLen))
			if underlayCIDR == "" {
				underlayCIDR = fmt.Sprintf("%s/%d", network.IPAddress, network.IPPrefixLen)
			}
		}
		if network.GlobalIPv6Address != "" {
			underlay.AddNodeAddrs(node, fmt.Sprintf("%s/%d", network.GlobalIPv6Address, network.GlobalIPv6PrefixLen))
		}
		if network.Gateway != "" {
			underlay.AddNodeRoutes(node, fmt.Sprintf("default via %s ", network.Gateway))
			if underlayGateway == "" {
				underlayGateway = network.Gateway
			}
		}
		if network.IPv6Gateway != "" {
			underlay.AddNodeRoutes(node, fmt.Sprintf("default via %s ", network.IPv6Gateway))
		}
	}
	underlay.SetCIDR(underlayCIDR)

	nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		Fail(err.Error())
	}
	cniPods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=kube-ovn-cni"})
	if err != nil {
		Fail(err.Error())
	}

	for i := range nodes.Items {
		var nodeIP string
		for _, addr := range nodes.Items[i].Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				nodeIP = addr.Address
				break
			}
		}
		if nodeIP == "" {
			Fail("failed to get IP of node " + nodes.Items[i].Name)
		}

		var cniPod *corev1.Pod
		for _, pod := range cniPods.Items {
			if pod.Status.HostIP == nodeIP {
				cniPod = &pod
				break
			}
		}
		if cniPod == nil {
			Fail("failed to get CNI pod on node " + nodes.Items[i].Name)
		}

		// change MTU
		mtu := 1500 - (i+1)*5
		cmd := fmt.Sprintf("ip link set %s mtu %d", underlay.ProviderInterface, mtu)
		if _, _, err = f.ExecToPodThroughAPI(cmd, "cni-server", cniPod.Name, cniPod.Namespace, nil); err != nil {
			Fail(fmt.Sprintf("failed to set MTU of %s on node %s: %v", underlay.ProviderInterface, nodes.Items[i].Name, err))
		}
		underlay.SetNodeMTU(nodes.Items[i].Name, mtu)
	}

	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   underlay.Namespace,
			Labels: map[string]string{"e2e": "true"},
		},
	}
	if _, err = f.KubeClientSet.CoreV1().Namespaces().Create(context.Background(), &ns, metav1.CreateOptions{}); err != nil {
		Fail(err.Error())
	}

	// create provider network
	pn := kubeovn.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:   underlay.ProviderNetwork,
			Labels: map[string]string{"e2e": "true"},
		},
		Spec: kubeovn.ProviderNetworkSpec{
			DefaultInterface: underlay.ProviderInterface,
		},
	}
	if _, err = f.OvnClientSet.KubeovnV1().ProviderNetworks().Create(context.Background(), &pn, metav1.CreateOptions{}); err != nil {
		Fail("failed to create provider network: " + err.Error())
	}
	if err = f.WaitProviderNetworkReady(pn.Name); err != nil {
		Fail("provider network failed: " + err.Error())
	}

	// create vlan
	vlan := kubeovn.Vlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:   underlay.Vlan,
			Labels: map[string]string{"e2e": "true"},
		},
		Spec: kubeovn.VlanSpec{
			ID:       0,
			Provider: pn.Name,
		},
	}
	if _, err = f.OvnClientSet.KubeovnV1().Vlans().Create(context.Background(), &vlan, metav1.CreateOptions{}); err != nil {
		Fail("failed to create vlan: " + err.Error())
	}
	if err = f.WaitProviderNetworkReady(pn.Name); err != nil {
		Fail("provider network failed: " + err.Error())
	}

	// create subnet
	subnet := kubeovn.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:   underlay.Subnet,
			Labels: map[string]string{"e2e": "true"},
		},
		Spec: kubeovn.SubnetSpec{
			CIDRBlock:       underlayCIDR,
			Gateway:         underlayGateway,
			ExcludeIps:      underlayNodeIPs,
			Vlan:            vlan.Name,
			UnderlayGateway: true,
			Namespaces:      []string{underlay.Namespace},
		},
	}
	if _, err = f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), &subnet, metav1.CreateOptions{}); err != nil {
		Fail("failed to create subnet: " + err.Error())
	}
	if err = f.WaitSubnetReady(subnet.Name); err != nil {
		Fail("subnet failed: " + err.Error())
	}

	return nil
}, func(data []byte) {})
