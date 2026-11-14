package daemon

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovninformerfactory "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type errSubnetLister struct{}

func (errSubnetLister) List(labels.Selector) ([]*kubeovnv1.Subnet, error) {
	return nil, errors.New("list failed")
}

func (errSubnetLister) Get(string) (*kubeovnv1.Subnet, error) {
	return nil, errors.New("get failed")
}

func TestSetGatewayBandwidthReturnsHTBQueryError(t *testing.T) {
	node := &corev1.Node{Name: "node-a"}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(node))
	wantErr := errors.New("query HTB QoS")
	controller := &Controller{
		config:        &Configuration{NodeName: node.Name},
		nodesLister:   listerv1.NewNodeLister(indexer),
		vswitchTables: cniEventProvider{filterErr: wantErr},
	}

	err := controller.setGatewayBandwidth()
	require.ErrorIs(t, err, wantErr)
	require.ErrorContains(t, err, "check HTB QoS")
}

func TestGetCidrByProtocol(t *testing.T) {
	cases := []struct {
		name     string
		cidr     string
		protocol string
		wantErr  bool
		expected string
	}{{
		name:     "ipv4 only",
		cidr:     "1.1.1.0/24",
		protocol: kubeovnv1.ProtocolIPv4,
		expected: "1.1.1.0/24",
	}, {
		name:     "ipv6 only",
		cidr:     "2001:db8::/120",
		protocol: kubeovnv1.ProtocolIPv6,
		expected: "2001:db8::/120",
	}, {
		name:     "get ipv4 from ipv6",
		cidr:     "2001:db8::/120",
		protocol: kubeovnv1.ProtocolIPv4,
	}, {
		name:     "get ipv4 from dual stack",
		cidr:     "1.1.1.0/24,2001:db8::/120",
		protocol: kubeovnv1.ProtocolIPv4,
		expected: "1.1.1.0/24",
	}, {
		name:     "get ipv6 from ipv4",
		cidr:     "1.1.1.0/24",
		protocol: kubeovnv1.ProtocolIPv6,
	}, {
		name:     "get ipv6 from dual stack",
		cidr:     "1.1.1.0/24,2001:db8::/120",
		protocol: kubeovnv1.ProtocolIPv6,
		expected: "2001:db8::/120",
	}, {
		name:     "invalid cidr",
		cidr:     "foo bar",
		protocol: kubeovnv1.ProtocolIPv4,
		wantErr:  true,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := getCidrByProtocol(c.cidr, c.protocol)
			if (err != nil) != c.wantErr {
				t.Errorf("getCidrByProtocol(%q, %q) error = %v, wantErr = %v", c.cidr, c.protocol, err, c.wantErr)
			}
			require.Equal(t, c.expected, got)
		})
	}
}

func mkTProxyPod(ns, name string, annotations map[string]string, podIPs ...string) *corev1.Pod {
	ips := make([]corev1.PodIP, 0, len(podIPs))
	for _, ip := range podIPs {
		ips = append(ips, corev1.PodIP{IP: ip})
	}
	return &corev1.Pod{
		Namespace: ns, Name: name, Annotations: annotations,
		Status: corev1.PodStatus{PodIPs: ips},
	}
}

func TestGetPodPrimaryNetworkProvider(t *testing.T) {
	attachProvider := "macvlan.default.ovn"
	cases := []struct {
		name        string
		annotations map[string]string
		podIPs      []string
		expected    string
		found       bool
	}{{
		name: "primary cni",
		annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.16.0.5",
		},
		podIPs:   []string{"10.16.0.5"},
		expected: util.OvnProvider,
		found:    true,
	}, {
		name: "primary cni dual stack",
		annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.16.0.5,fd00:10:16::5",
		},
		podIPs:   []string{"10.16.0.5", "fd00:10:16::5"},
		expected: util.OvnProvider,
		found:    true,
	}, {
		name: "attachment network as multus default network",
		annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, attachProvider): "172.17.0.5",
		},
		podIPs:   []string{"172.17.0.5"},
		expected: attachProvider,
		found:    true,
	}, {
		name: "pure secondary cni",
		annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.16.0.5",
			fmt.Sprintf(util.IPAddressAnnotationTemplate, attachProvider):   "172.17.0.5",
		},
		podIPs: []string{"192.168.0.5"},
	}, {
		name: "default provider preferred on identical ips",
		annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.16.0.5",
			fmt.Sprintf(util.IPAddressAnnotationTemplate, attachProvider):   "10.16.0.5",
		},
		podIPs:   []string{"10.16.0.5"},
		expected: util.OvnProvider,
		found:    true,
	}, {
		name: "partial ip coverage does not match",
		annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.16.0.5",
		},
		podIPs: []string{"10.16.0.5", "fd00:10:16::5"},
	}, {
		name: "no pod ips",
		annotations: map[string]string{
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.16.0.5",
		},
	}, {
		name:   "no annotations",
		podIPs: []string{"10.16.0.5"},
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pod := mkTProxyPod("default", "pod", c.annotations, c.podIPs...)
			provider, found := getPodPrimaryNetworkProvider(pod)
			require.Equal(t, c.found, found)
			require.Equal(t, c.expected, provider)
		})
	}
}

func TestGetTProxyConditionPod(t *testing.T) {
	attachProvider := "macvlan.default.ovn"
	subnets := []*kubeovnv1.Subnet{{
		Name: "custom-subnet",
		Spec: kubeovnv1.SubnetSpec{Vpc: "custom-vpc"},
	}, {
		Name: "default-subnet",
		Spec: kubeovnv1.SubnetSpec{Vpc: util.DefaultVpc},
	}, {
		Name: "attach-subnet",
		Spec: kubeovnv1.SubnetSpec{Vpc: "custom-vpc"},
	}}

	kubeovnClient := kubeovnfake.NewSimpleClientset()
	informerFactory := kubeovninformerfactory.NewSharedInformerFactory(kubeovnClient, 0)
	subnetInformer := informerFactory.Kubeovn().V1().Subnets()
	for _, subnet := range subnets {
		require.NoError(t, subnetInformer.Informer().GetStore().Add(subnet))
	}

	c := &Controller{
		subnetsLister: subnetInformer.Lister(),
		config:        &Configuration{ClusterRouter: util.DefaultVpc},
	}

	primaryPod := mkTProxyPod("default", "primary", map[string]string{
		fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider):     "10.16.0.5",
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, util.OvnProvider): "custom-subnet",
	}, "10.16.0.5")
	defaultVpcPod := mkTProxyPod("default", "default-vpc", map[string]string{
		fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider):     "10.17.0.5",
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, util.OvnProvider): "default-subnet",
	}, "10.17.0.5")
	attachPod := mkTProxyPod("default", "attach", map[string]string{
		fmt.Sprintf(util.IPAddressAnnotationTemplate, attachProvider):     "172.17.0.5",
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, attachProvider): "attach-subnet",
	}, "172.17.0.5")
	// kube-ovn allocated an unused address for the default provider while another
	// CNI provides the primary network; tproxy must not intercept its probes
	secondaryPod := mkTProxyPod("default", "secondary", map[string]string{
		fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider):     "10.16.0.6",
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, util.OvnProvider): "custom-subnet",
	}, "192.168.0.6")
	staleSubnetPod := mkTProxyPod("default", "stale", map[string]string{
		fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider):     "10.18.0.5",
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, util.OvnProvider): "missing-subnet",
	}, "10.18.0.5")
	terminatingPod := mkTProxyPod("default", "terminating", map[string]string{
		fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider):     "10.16.0.7",
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, util.OvnProvider): "custom-subnet",
	}, "10.16.0.7")
	terminatingPod.DeletionTimestamp = new(metav1.Time)

	pods := []*corev1.Pod{secondaryPod, primaryPod, defaultVpcPod, attachPod, staleSubnetPod, terminatingPod}
	filtered, err := c.getTProxyConditionPod(pods, true)
	require.NoError(t, err)
	require.Len(t, filtered, 2)
	require.Equal(t, "attach", filtered[0].Name)
	require.Equal(t, "primary", filtered[1].Name)
}

func TestProviderExistsRequiresSubnetForNamedOvnProvider(t *testing.T) {
	subnets := []*kubeovnv1.Subnet{{
		Name: util.DefaultSubnet,
		Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider},
	}, {
		Name: "attach-subnet",
		Spec: kubeovnv1.SubnetSpec{Provider: "attachnet-a.default.ovn"},
	}}

	kubeovnClient := kubeovnfake.NewSimpleClientset()
	informerFactory := kubeovninformerfactory.NewSharedInformerFactory(kubeovnClient, 0)
	subnetInformer := informerFactory.Kubeovn().V1().Subnets()
	for _, subnet := range subnets {
		require.NoError(t, subnetInformer.Informer().GetStore().Add(subnet))
	}

	handler := cniServerHandler{Controller: &Controller{subnetsLister: subnetInformer.Lister()}}

	_, ok := handler.providerExists(util.OvnProvider, "", nil, false)
	require.True(t, ok)

	subnet, ok := handler.providerExists("attachnet-a.default.ovn", "", nil, false)
	require.True(t, ok)
	require.NotNil(t, subnet)
	require.Equal(t, "attach-subnet", subnet.Name)

	_, ok = handler.providerExists("attachnet-b.default.ovn", "", nil, false)
	require.False(t, ok)
}

func TestProviderExistsAllowsLegacyOvnSubnetSelectedByLogicalSwitch(t *testing.T) {
	const (
		provider   = "attachnet.default.ovn"
		subnetName = "legacy-attachment"
	)
	subnet := &kubeovnv1.Subnet{
		Name: subnetName,
		Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider},
	}
	subnetIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, subnetIndexer.Add(subnet))
	handler := cniServerHandler{Controller: &Controller{subnetsLister: kubeovnlister.NewSubnetLister(subnetIndexer)}}

	annotations := map[string]string{
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider): subnetName,
	}
	resolved, ok := handler.providerExists(provider, "", annotations, false)
	require.True(t, ok)
	require.Equal(t, subnetName, resolved.Name)

	interfaceAnnotations := map[string]string{
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider+".net1"): subnetName,
	}
	resolved, ok = handler.providerExists(provider, "net1", interfaceAnnotations, true)
	require.True(t, ok)
	require.Equal(t, subnetName, resolved.Name)
	resolved, ok = handler.providerExists(provider, "net1", interfaceAnnotations, false)
	require.True(t, ok)
	require.Equal(t, subnetName, resolved.Name)

	suffixedProvider := provider + ".net1"
	suffixedAnnotations := map[string]string{
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, suffixedProvider): subnetName,
	}
	resolved, ok = handler.providerExists(suffixedProvider, "net1", suffixedAnnotations, false)
	require.True(t, ok)
	require.Equal(t, subnetName, resolved.Name)

	emptyProviderSubnet := &kubeovnv1.Subnet{
		Name: "legacy-empty-provider",
	}
	require.NoError(t, subnetIndexer.Add(emptyProviderSubnet))
	emptyProviderAnnotations := map[string]string{
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "legacy.default.ovn"): emptyProviderSubnet.Name,
	}
	resolved, ok = handler.providerExists("legacy.default.ovn", "", emptyProviderAnnotations, false)
	require.True(t, ok)
	require.Equal(t, emptyProviderSubnet.Name, resolved.Name)
}

func TestAppendIfNameForAnnotations(t *testing.T) {
	provider := "attachnet.default.ovn"
	ifName := "net1"

	require.True(t, appendIfNameForAnnotations(map[string]string{
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider+"."+ifName): "subnet",
	}, provider, ifName))
	require.True(t, appendIfNameForAnnotations(map[string]string{
		fmt.Sprintf(util.IPAddressAnnotationTemplate, provider+"."+ifName): "10.0.0.2",
	}, provider, ifName))
	require.False(t, appendIfNameForAnnotations(map[string]string{
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider): "subnet",
	}, provider, ifName))
	require.False(t, appendIfNameForAnnotations(map[string]string{
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider+"."+ifName): "subnet",
	}, provider+"."+ifName, ifName))
}

func TestProviderExistsUsesEffectiveLogicalSwitchAnnotation(t *testing.T) {
	const provider = "attachnet.default.ovn"
	namedSubnet := &kubeovnv1.Subnet{
		Name: "named-attachment",
		Spec: kubeovnv1.SubnetSpec{Provider: provider},
	}
	legacySubnet := &kubeovnv1.Subnet{
		Name: "legacy-attachment",
		Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider},
	}
	subnetIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, subnetIndexer.Add(namedSubnet))
	require.NoError(t, subnetIndexer.Add(legacySubnet))
	handler := cniServerHandler{Controller: &Controller{subnetsLister: kubeovnlister.NewSubnetLister(subnetIndexer)}}

	annotations := map[string]string{
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider):         namedSubnet.Name,
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider+".net1"): legacySubnet.Name,
	}
	resolved, ok := handler.providerExists(provider, "net1", annotations, false)
	require.True(t, ok)
	require.Equal(t, namedSubnet.Name, resolved.Name)

	resolved, ok = handler.providerExists(provider, "net1", annotations, true)
	require.True(t, ok)
	require.Equal(t, legacySubnet.Name, resolved.Name)
}

func TestProviderExistsRejectsLogicalSwitchWithDifferentNamedProvider(t *testing.T) {
	const (
		provider   = "attachnet.default.ovn"
		subnetName = "other-attachment"
	)
	subnet := &kubeovnv1.Subnet{
		Name: subnetName,
		Spec: kubeovnv1.SubnetSpec{Provider: "other.default.ovn"},
	}
	subnetIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, subnetIndexer.Add(subnet))
	handler := cniServerHandler{Controller: &Controller{subnetsLister: kubeovnlister.NewSubnetLister(subnetIndexer)}}

	annotations := map[string]string{
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider): subnetName,
	}
	_, ok := handler.providerExists(provider, "", annotations, false)
	require.False(t, ok)
}

func TestProviderExistsAllowsOnSubnetListError(t *testing.T) {
	handler := cniServerHandler{Controller: &Controller{subnetsLister: errSubnetLister{}}}

	subnet, ok := handler.providerExists("attachnet-a.default.ovn", "", nil, false)
	require.True(t, ok)
	require.Nil(t, subnet)
}

func TestGetTunnelPort(t *testing.T) {
	cases := []struct {
		name        string
		networkType string
		expected    int
	}{
		{
			name:        "geneve network type",
			networkType: util.NetworkTypeGeneve,
			expected:    6081,
		},
		{
			name:        "vxlan network type",
			networkType: util.NetworkTypeVxlan,
			expected:    4789,
		},
		{
			name:        "vlan network type",
			networkType: util.NetworkTypeVlan,
			expected:    0,
		},
		{
			name:        "stt network type",
			networkType: util.NetworkTypeStt,
			expected:    0,
		},
		{
			name:        "empty network type defaults to 0",
			networkType: "",
			expected:    0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Controller{
				config: &Configuration{
					NetworkType:         tc.networkType,
					EnableTunnelNotrack: true,
				},
			}
			require.Equal(t, tc.expected, c.getTunnelPort())
		})
	}

	t.Run("disabled tunnel notrack returns 0", func(t *testing.T) {
		c := &Controller{
			config: &Configuration{
				NetworkType:         util.NetworkTypeGeneve,
				EnableTunnelNotrack: false,
			},
		}
		require.Equal(t, 0, c.getTunnelPort())
	})

	t.Run("nil config returns 0", func(t *testing.T) {
		c := &Controller{}
		require.Equal(t, 0, c.getTunnelPort())
	})
}

func TestGetOtherNodes_MultiEncap(t *testing.T) {
	node1 := &corev1.Node{
		Name: "node-1",
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "192.168.1.1"},
			},
		},
	}
	node2 := &corev1.Node{
		Name: "node-2",
		Annotations: map[string]string{
			util.NodeNetworksAnnotation: `{"storage": "10.0.1.2", "v6net": "fd00::2"}`,
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "192.168.1.2"},
			},
		},
	}
	node3 := &corev1.Node{
		Name: "node-3",
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "192.168.1.3"},
			},
		},
	}

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(node1))
	require.NoError(t, indexer.Add(node2))
	require.NoError(t, indexer.Add(node3))

	c := &Controller{
		config: &Configuration{
			NodeName: "node-1", // node-1 is self, should be excluded
		},
		nodesLister: listerv1.NewNodeLister(indexer),
	}

	v4Nodes, err := c.getOtherNodes(kubeovnv1.ProtocolIPv4)
	require.NoError(t, err)
	require.NotContains(t, v4Nodes, "192.168.1.1") // self excluded
	require.Contains(t, v4Nodes, "192.168.1.2")    // node-2 primary
	require.NotContains(t, v4Nodes, "10.0.1.2")    // node_networks not in other-nodes
	require.Contains(t, v4Nodes, "192.168.1.3")    // node-3 primary
	require.Len(t, v4Nodes, 2)

	v6Nodes, err := c.getOtherNodes(kubeovnv1.ProtocolIPv6)
	require.NoError(t, err)
	require.Empty(t, v6Nodes)
}

type tunnelPeerVswitch struct {
	ovs.Vswitch
	ports      []vswitch.Port
	interfaces []vswitch.Interface
	err        error
}

func (f *tunnelPeerVswitch) ListPort(filter func(*vswitch.Port) bool) ([]vswitch.Port, error) {
	var result []vswitch.Port
	for _, port := range f.ports {
		if filter == nil || filter(&port) {
			result = append(result, port)
		}
	}
	return result, f.err
}

func (f *tunnelPeerVswitch) ListInterface(filter func(*vswitch.Interface) bool) ([]vswitch.Interface, error) {
	var result []vswitch.Interface
	for _, iface := range f.interfaces {
		if filter == nil || filter(&iface) {
			result = append(result, iface)
		}
	}
	return result, f.err
}

func TestGetTunnelRemoteIPs_OVS(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(&corev1.Node{Name: "self"}))
	client := &tunnelPeerVswitch{
		ports: []vswitch.Port{{ExternalIDs: map[string]string{"ovn-chassis-id": "peer"}, Interfaces: []string{"tunnel", "v6", "flow"}}},
		interfaces: []vswitch.Interface{
			{UUID: "tunnel", Type: "geneve", Options: map[string]string{"remote_ip": "172.18.0.2"}},
			{UUID: "v6", Type: "geneve", Options: map[string]string{"remote_ip": "fd00::2"}},
			{UUID: "flow", Type: "geneve", Options: map[string]string{"remote_ip": "flow"}},
			{UUID: "foreign", Type: "geneve", Options: map[string]string{"remote_ip": "172.18.0.3"}},
		},
	}
	c := &Controller{config: &Configuration{NodeName: "self", NetworkType: "geneve", EnableTunnelNotrack: true}, nodesLister: listerv1.NewNodeLister(indexer), vswitchClient: client}
	ips, err := c.getTunnelRemoteIPs(kubeovnv1.ProtocolIPv4)
	require.NoError(t, err)
	require.Equal(t, []string{"172.18.0.2"}, ips)
	ips, err = c.getTunnelRemoteIPs(kubeovnv1.ProtocolIPv6)
	require.NoError(t, err)
	require.Equal(t, []string{"fd00::2"}, ips)
	client.interfaces = nil
	ips, err = c.getTunnelRemoteIPs(kubeovnv1.ProtocolIPv4)
	require.NoError(t, err)
	require.Empty(t, ips)

	// Repopulate cache with confirmed endpoints
	client.interfaces = []vswitch.Interface{
		{UUID: "tunnel", Type: "geneve", Options: map[string]string{"remote_ip": "172.18.0.2"}},
	}
	ips, err = c.getTunnelRemoteIPs(kubeovnv1.ProtocolIPv4)
	require.NoError(t, err)
	require.Equal(t, []string{"172.18.0.2"}, ips)

	// Transient error retains last confirmed endpoints
	client.err = errors.New("OVS unavailable")
	ips, err = c.getTunnelRemoteIPs(kubeovnv1.ProtocolIPv4)
	require.ErrorContains(t, err, "OVS unavailable")
	require.Equal(t, []string{"172.18.0.2"}, ips)
}

func TestPeerDiscoveryFailureDoesNotBlockReconciliation(t *testing.T) {
	nodeIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, nodeIndexer.Add(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "self",
			Annotations: map[string]string{
				util.IPAddressAnnotation: "10.244.0.1",
			},
		},
	}))

	svcIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, svcIndexer.Add(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test-svc", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.10",
			Ports:     []corev1.ServicePort{{Port: 80}},
		},
	}))

	subnetIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, subnetIndexer.Add(&kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
		Spec: kubeovnv1.SubnetSpec{
			Vpc:       "ovn-cluster",
			CIDRBlock: "10.244.0.0/16",
			Protocol:  kubeovnv1.ProtocolIPv4,
		},
	}))

	client := &tunnelPeerVswitch{
		ports: []vswitch.Port{{ExternalIDs: map[string]string{"ovn-chassis-id": "peer"}, Interfaces: []string{"tunnel"}}},
		interfaces: []vswitch.Interface{
			{UUID: "tunnel", Type: "geneve", Options: map[string]string{"remote_ip": "172.18.0.2"}},
		},
	}

	c := &Controller{
		protocol: kubeovnv1.ProtocolIPv4,
		config: &Configuration{
			NodeName:            "self",
			NetworkType:         "geneve",
			EnableTunnelNotrack: true,
			ClusterRouter:       "ovn-cluster",
		},
		nodesLister:    listerv1.NewNodeLister(nodeIndexer),
		servicesLister: listerv1.NewServiceLister(svcIndexer),
		subnetsLister:  kubeovnlister.NewSubnetLister(subnetIndexer),
		vswitchClient:  client,
	}

	// 1. Initial successful reconciliation populates cache
	snapshot, err := c.buildNFTSnapshot()
	require.NoError(t, err)
	require.NotEmpty(t, snapshot.Families)
	require.Equal(t, []string{"172.18.0.2"}, snapshot.Families[0].TunnelRemoteIPs)

	// 2. Simulate transient OVSDB peer lookup failure
	client.err = errors.New("OVSDB connection timeout")

	// Reconciliation must NOT be aborted by tunnel lookup failure
	snapshot, err = c.buildNFTSnapshot()
	require.NoError(t, err)
	require.NotEmpty(t, snapshot.Families)
	// Cached tunnel peer endpoints must be retained
	require.Equal(t, []string{"172.18.0.2"}, snapshot.Families[0].TunnelRemoteIPs)
	// Unrelated gateway services and subnets must still be reconciled
	require.NotEmpty(t, snapshot.Families[0].ClusterIPPorts)
	require.NotEmpty(t, snapshot.Families[0].Subnets)
}

func TestGetLocalEncapIPs(t *testing.T) {
	c := &Controller{config: &Configuration{
		DefaultEncapIP: "192.0.2.10",
		NodeNetworks:   map[string]string{"default": "192.0.2.10", "storage": "198.51.100.10", "v6": "2001:db8::10", "invalid": "not-an-ip"},
	}}
	require.Equal(t, []string{"192.0.2.10", "198.51.100.10", "2001:db8::10"}, c.getLocalEncapIPs())
	c.config.SetDefaultEncapIP("192.0.2.20")
	require.Contains(t, c.getLocalEncapIPs(), "192.0.2.20")
	require.Empty(t, (&Controller{}).getLocalEncapIPs())
}

func TestTunnelProtocolsIncludeUnderlayFamily(t *testing.T) {
	require.Equal(t, []string{kubeovnv1.ProtocolIPv4, kubeovnv1.ProtocolIPv6},
		getTunnelProtocols(kubeovnv1.ProtocolIPv6, []string{"192.0.2.10"}))
	require.Equal(t, []string{kubeovnv1.ProtocolIPv4, kubeovnv1.ProtocolIPv6},
		getTunnelProtocols(kubeovnv1.ProtocolIPv4, []string{"2001:db8::10"}))
	require.Equal(t, []string{kubeovnv1.ProtocolIPv4},
		getTunnelProtocols(kubeovnv1.ProtocolIPv4, []string{"192.0.2.10"}))
}
