package daemon

import (
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientfake "k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestExternalLBKernelARPProtection(t *testing.T) {
	require.ErrorContains(t, protectExternalLBInterface(nil), "no link index")
	request := externalLBInterfaceSysctlRequest(17)
	require.Len(t, request.Data, 2)
	require.Equal(t, uint16(unix.RTM_SETLINK), request.Type)
	require.NotEmpty(t, request.Serialize())
	attrs, err := nl.ParseRouteAttr(request.Serialize()[unix.SizeofNlMsghdr+unix.SizeofIfInfomsg:])
	require.NoError(t, err)
	require.Len(t, attrs, 1)
	require.Equal(t, uint16(unix.IFLA_AF_SPEC), attrs[0].Attr.Type)
}

func TestExternalLBAddressRemoval(t *testing.T) {
	link := &netlink.Dummy{Name: "mlb0"}
	mac := util.ExternalLBInterfaceMAC("node1", "uid")
	var calls []string
	ops := externalLBLinkOperations{
		setMAC: func(_ netlink.Link, actual net.HardwareAddr) error {
			require.Equal(t, mac, actual)
			calls = append(calls, "mac")
			return nil
		},
		listAddresses: func(_ netlink.Link, family int) ([]netlink.Addr, error) {
			require.Equal(t, netlink.FAMILY_ALL, family)
			return []netlink.Addr{{IPNet: &net.IPNet{IP: net.ParseIP("192.0.2.1"), Mask: net.CIDRMask(24, 32)}}, {IPNet: &net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)}}}, nil
		},
		deleteAddress: func(_ netlink.Link, address *netlink.Addr) error {
			calls = append(calls, address.IP.String())
			return nil
		},
		setUp: func(netlink.Link) error { calls = append(calls, "up"); return nil },
	}
	require.NoError(t, reconcileExternalLBLink(link, mac, ops))
	require.Equal(t, []string{"mac", "192.0.2.1", "fe80::1", "up"}, calls)
	calls = nil
	denied := errors.New("address deletion denied")
	ops.deleteAddress = func(netlink.Link, *netlink.Addr) error { return denied }
	require.ErrorIs(t, reconcileExternalLBLink(link, mac, ops), denied)
	require.Equal(t, []string{"mac"}, calls, "never mark up after failing to remove an address")
}

func TestExternalLBNodeEligibility(t *testing.T) {
	vlans := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	providers := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	pn := &kubeovnv1.ProviderNetwork{Name: "provider"}
	require.NoError(t, vlans.Add(&kubeovnv1.Vlan{Name: "vlan", Spec: kubeovnv1.VlanSpec{Provider: pn.Name}}))
	require.NoError(t, providers.Add(pn))
	c := &Controller{vlansLister: kubeovnlisters.NewVlanLister(vlans), providerNetworksLister: kubeovnlisters.NewProviderNetworkLister(providers)}
	subnet := &kubeovnv1.Subnet{Spec: kubeovnv1.SubnetSpec{ExternalLBInterface: "mlb0", EnableExternalLBAddress: true, Vlan: "vlan", CIDRBlock: "192.0.2.0/24"}}
	node := &v1.Node{Name: "node1", Labels: map[string]string{"provider.provider-network.kubernetes.io/ready": "true"}}
	eligible, err := c.externalLBNodeEligible(node, subnet)
	require.NoError(t, err)
	require.True(t, eligible)
	pn.Spec.ExcludeNodes = []string{node.Name}
	eligible, err = c.externalLBNodeEligible(node, subnet)
	require.NoError(t, err)
	require.False(t, eligible)
	pn.Spec.ExcludeNodes = nil
	node.Labels = nil
	eligible, err = c.externalLBNodeEligible(node, subnet)
	require.NoError(t, err)
	require.False(t, eligible)
}

func TestExternalLBPrunesLegacyFlows(t *testing.T) {
	c := &Controller{flowCache: map[string]map[string]LbServiceRules{"br-provider": {
		"legacy": {SubnetName: "legacy"}, "localport": {SubnetName: "localport"}, "deleted": {SubnetName: "deleted"},
	}}, flowChan: make(chan struct{}, 1)}
	c.pruneExternalLBFlows([]*kubeovnv1.Subnet{
		{Name: "legacy", Spec: kubeovnv1.SubnetSpec{EnableExternalLBAddress: true}},
		{Name: "localport", Spec: kubeovnv1.SubnetSpec{EnableExternalLBAddress: true, ExternalLBInterface: "mlb0"}},
	})
	require.Len(t, c.flowCache["br-provider"], 1)
	require.Contains(t, c.flowCache["br-provider"], "legacy")
	require.Len(t, c.flowChan, 1)
}

func TestExternalLBRemovesServiceFlowsAfterSubnetDeletion(t *testing.T) {
	service := &v1.Service{Annotations: map[string]string{util.ServiceExternalIPFromSubnetAnnotation: "deleted"}, Spec: v1.ServiceSpec{Ports: []v1.ServicePort{{Port: 80, Protocol: v1.ProtocolTCP}}}, Status: v1.ServiceStatus{LoadBalancer: v1.LoadBalancerStatus{Ingress: []v1.LoadBalancerIngress{{IP: "192.0.2.100"}}}}}
	rule := genLBServiceRules(service, "br-provider", "eth1", "02:00:00:00:00:01", "deleted")[0]
	c := &Controller{flowCache: map[string]map[string]LbServiceRules{"br-provider": {buildFlowKey(flowKindUnderlayService, rule.IP, rule.Port, rule.Protocol, ""): rule}}}
	add, remove, err := c.diffExternalLBServiceRules(service, service, false)
	require.NoError(t, err)
	require.Empty(t, add)
	require.Equal(t, []LbServiceRules{rule}, remove)
}

func TestExternalLBRejectsForeignOVSInterface(t *testing.T) {
	c := &Controller{vswitchTables: flowInterfaceProvider{"mlb0": &vswitch.Interface{Name: "mlb0", Type: "internal"}}}
	subnet := &kubeovnv1.Subnet{Name: "underlay", Spec: kubeovnv1.SubnetSpec{ExternalLBInterface: "mlb0"}}
	require.ErrorContains(t, c.ensureExternalLBInterface(subnet), "not owned")
}

func TestExternalLBPreservesForeignOVSPort(t *testing.T) {
	c := &Controller{vswitchTables: &bridgePortProvider{ports: []vswitch.Port{{Name: "mlb0"}}}}
	require.ErrorContains(t, c.deleteExternalLBInterface("mlb0"), "refusing to delete foreign")
}

func TestExternalLBDuplicateInterfaceWithdrawsReadiness(t *testing.T) {
	nodes := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	subnets := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	vlans := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	providers := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	node := &v1.Node{Name: "node1", Labels: map[string]string{
		"provider.provider-network.kubernetes.io/ready": "true", util.ExternalLBReadyLabel("mlb0"): "true",
	}}
	require.NoError(t, nodes.Add(node))
	require.NoError(t, vlans.Add(&kubeovnv1.Vlan{Name: "vlan", Spec: kubeovnv1.VlanSpec{Provider: "provider"}}))
	require.NoError(t, providers.Add(&kubeovnv1.ProviderNetwork{Name: "provider"}))
	for _, name := range []string{"subnet1", "subnet2"} {
		require.NoError(t, subnets.Add(&kubeovnv1.Subnet{Name: name, Spec: kubeovnv1.SubnetSpec{
			ExternalLBInterface: "mlb0", EnableExternalLBAddress: true, Vlan: "vlan", CIDRBlock: "192.0.2.0/24",
		}}))
	}
	client := clientfake.NewClientset(node)
	c := &Controller{
		config:      &Configuration{NodeName: node.Name, KubeClient: client},
		nodesLister: corelisters.NewNodeLister(nodes), subnetsLister: kubeovnlisters.NewSubnetLister(subnets),
		vlansLister: kubeovnlisters.NewVlanLister(vlans), providerNetworksLister: kubeovnlisters.NewProviderNetworkLister(providers),
		vswitchTables: flowInterfaceProvider{},
	}
	require.ErrorContains(t, c.reconcileExternalLBInterfaces(), "multiple subnets")
	updated, err := client.CoreV1().Nodes().Get(t.Context(), node.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, updated.Labels[util.ExternalLBReadyLabel("mlb0")])
	require.Equal(t, "true", updated.Labels["provider.provider-network.kubernetes.io/ready"])
}
