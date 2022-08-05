package e2e

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	kubeadmscheme "k8s.io/kubernetes/cmd/kubeadm/app/apis/kubeadm/scheme"
	kubeadmapi "k8s.io/kubernetes/cmd/kubeadm/app/apis/kubeadm/v1beta3"
	kubeadmconstants "k8s.io/kubernetes/cmd/kubeadm/app/constants"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	kubeovn "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"

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
	for {
		pods, err := f.KubeClientSet.CoreV1().Pods(corev1.NamespaceAll).List(context.Background(), metav1.ListOptions{LabelSelector: "e2e=true"})
		if err != nil {
			Fail(err.Error())
		}
		if len(pods.Items) == 0 {
			break
		}
		time.Sleep(time.Second)
	}

	for {
		nss, err := f.KubeClientSet.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{LabelSelector: "e2e=true"})
		if err != nil {
			Fail(err.Error())
		}
		if len(nss.Items) == 0 {
			break
		}
		for _, ns := range nss.Items {
			if ns.DeletionTimestamp != nil {
				continue
			}
			err := f.KubeClientSet.CoreV1().Namespaces().Delete(context.Background(), ns.Name, metav1.DeleteOptions{})
			if err != nil {
				Fail(err.Error())
			}
		}
		time.Sleep(time.Second)
	}

	err := f.OvnClientSet.KubeovnV1().Subnets().DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "e2e=true"})
	if err != nil {
		Fail(err.Error())
	}
	for {
		subnets, err := f.OvnClientSet.KubeovnV1().Subnets().List(context.Background(), metav1.ListOptions{LabelSelector: "e2e=true"})
		if err != nil {
			Fail(err.Error())
		}
		if len(subnets.Items) == 0 {
			break
		}
		time.Sleep(time.Second)
	}

	err = f.OvnClientSet.KubeovnV1().Vlans().DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "e2e=true"})
	if err != nil {
		Fail(err.Error())
	}
	for {
		vlans, err := f.OvnClientSet.KubeovnV1().Vlans().List(context.Background(), metav1.ListOptions{LabelSelector: "e2e=true"})
		if err != nil {
			Fail(err.Error())
		}
		if len(vlans.Items) == 0 {
			break
		}
		time.Sleep(time.Second)
	}

	err = f.OvnClientSet.KubeovnV1().ProviderNetworks().DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "e2e=true"})
	if err != nil {
		Fail(err.Error())
	}
	for {
		pns, err := f.OvnClientSet.KubeovnV1().ProviderNetworks().List(context.Background(), metav1.ListOptions{LabelSelector: "e2e=true"})
		if err != nil {
			Fail(err.Error())
		}
		if len(pns.Items) == 0 {
			break
		}
		time.Sleep(time.Second)
	}
})

func setExternalRoute(af int, dst, gw string) {
	if dst == "" || gw == "" {
		return
	}

	cmd := exec.Command("docker", "exec", "kube-ovn-e2e", "ip", fmt.Sprintf("-%d", af), "route", "replace", dst, "via", gw)
	output, err := cmd.CombinedOutput()
	if err != nil {
		Fail((fmt.Sprintf(`failed to execute command "%s": %v, output: %s`, cmd.String(), err, strings.TrimSpace(string(output)))))
	}
}

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

	nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		Fail(err.Error())
	}
	kubeadmConfigMap, err := f.KubeClientSet.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(context.Background(), kubeadmconstants.KubeadmConfigConfigMap, metav1.GetOptions{})
	if err != nil {
		Fail(err.Error())
	}

	clusterConfig := &kubeadmapi.ClusterConfiguration{}
	if err = k8sruntime.DecodeInto(kubeadmscheme.Codecs.UniversalDecoder(), []byte(kubeadmConfigMap.Data[kubeadmconstants.ClusterConfigurationConfigMapKey]), clusterConfig); err != nil {
		Fail(fmt.Sprintf("failed to decode kubeadm cluster configuration from bytes: %v", err))
	}

	nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(nodes.Items[0])
	podSubnetV4, podSubnetV6 := util.SplitStringIP(clusterConfig.Networking.PodSubnet)
	svcSubnetV4, svcSubnetV6 := util.SplitStringIP(clusterConfig.Networking.ServiceSubnet)
	setExternalRoute(4, podSubnetV4, nodeIPv4)
	setExternalRoute(4, svcSubnetV4, nodeIPv4)
	setExternalRoute(6, podSubnetV6, nodeIPv6)
	setExternalRoute(6, svcSubnetV6, nodeIPv6)

	// underlay
	var vlanID int
	providerInterface := underlay.UnderlayInterface
	if underlay.VlanID != "" {
		if vlanID, err = strconv.Atoi(underlay.VlanID); err != nil || vlanID <= 0 || vlanID > 4095 {
			Fail(underlay.VlanID + " is not a valid VLAN ID")
		}
		providerInterface = underlay.VlanInterface
	}

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

	cniPods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=kube-ovn-cni"})
	if err != nil {
		Fail(err.Error())
	}

	for i := range nodes.Items {
		var cniPod *corev1.Pod
		nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(nodes.Items[i])
		for _, pod := range cniPods.Items {
			if pod.Status.HostIP == nodeIPv4 || pod.Status.HostIP == nodeIPv6 {
				cniPod = &pod
				break
			}
		}
		if cniPod == nil {
			Fail("failed to get CNI pod on node " + nodes.Items[i].Name)
			return nil
		}

		// change MTU
		mtu := 1500 - (i+1)*5
		cmd := fmt.Sprintf("ip link set %s mtu %d", providerInterface, mtu)
		if _, _, err = f.ExecToPodThroughAPI(cmd, "cni-server", cniPod.Name, cniPod.Namespace, nil); err != nil {
			Fail(fmt.Sprintf("failed to set MTU of %s on node %s: %v", providerInterface, nodes.Items[i].Name, err))
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
	pn := &kubeovn.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:   underlay.ProviderNetwork,
			Labels: map[string]string{"e2e": "true"},
		},
		Spec: kubeovn.ProviderNetworkSpec{
			DefaultInterface: providerInterface,
			ExchangeLinkName: underlay.ExchangeLinkName,
		},
	}
	if _, err = f.OvnClientSet.KubeovnV1().ProviderNetworks().Create(context.Background(), pn, metav1.CreateOptions{}); err != nil {
		Fail("failed to create provider network: " + err.Error())
	}
	if err = f.WaitProviderNetworkReady(pn.Name); err != nil {
		Fail("provider network failed: " + err.Error())
	}
	if pn, err = f.OvnClientSet.KubeovnV1().ProviderNetworks().Get(context.Background(), pn.Name, metav1.GetOptions{}); err != nil {
		Fail("failed to get provider network: " + err.Error())
	}
	for _, node := range nodes.Items {
		if !pn.Status.NodeIsReady(node.Name) {
			Fail(fmt.Sprintf("provider network on node %s is not ready", node.Name))
		}
	}

	// create vlan
	vlan := kubeovn.Vlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:   underlay.Vlan,
			Labels: map[string]string{"e2e": "true"},
		},
		Spec: kubeovn.VlanSpec{
			ID:       vlanID,
			Provider: pn.Name,
		},
	}
	if _, err = f.OvnClientSet.KubeovnV1().Vlans().Create(context.Background(), &vlan, metav1.CreateOptions{}); err != nil {
		Fail("failed to create vlan: " + err.Error())
	}

	// create subnet
	subnet := kubeovn.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:   underlay.Subnet,
			Labels: map[string]string{"e2e": "true"},
		},
		Spec: kubeovn.SubnetSpec{
			CIDRBlock:  underlayCIDR,
			Gateway:    underlayGateway,
			ExcludeIps: underlayNodeIPs,
			Vlan:       vlan.Name,
			Namespaces: []string{underlay.Namespace},
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
