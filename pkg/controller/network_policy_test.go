package controller

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	networkinglister "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"

	"k8s.io/utils/set"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestNetworkPolicyPortVariants(t *testing.T) {
	tcp := corev1.ProtocolTCP
	endPort := int32(90)
	numericPort := intstr.FromInt(80)
	namedPort := intstr.FromString("http")
	namedRange := intstr.FromString("https")
	numeric, named := networkPolicyPortVariants([]netv1.NetworkPolicyPort{
		{Port: &numericPort, Protocol: &tcp},
		{Port: &namedPort, Protocol: &tcp},
		{Port: &namedRange, EndPort: &endPort, Protocol: &tcp},
		{Protocol: &tcp},
	})
	if len(numeric) != 2 || numeric[0].Port.IntVal != 80 || numeric[1].Port != nil {
		t.Fatalf("unexpected numeric ports: %#v", numeric)
	}
	if len(named) != 1 || named[0].Port.StrVal != "http" {
		t.Fatalf("unexpected named ports: %#v", named)
	}
}

func TestNetworkPolicyNamedPortVariantsUseSelectedPodsAndProviders(t *testing.T) {
	port := int32(8080)
	pod := &corev1.Pod{
		Name:      "selected",
		Namespace: metav1.NamespaceDefault,
		Labels:    map[string]string{"app": "web"},
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation:                                    util.DefaultSubnet,
			fmt.Sprintf(util.AllocatedAnnotationTemplate, util.OvnProvider): "true",
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: port}}}}},
	}
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods: []*corev1.Pod{pod},
		Subnets: []*kubeovnv1.Subnet{{
			Name: util.DefaultSubnet,
			Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolIPv4},
		}},
	})
	require.NoError(t, err)
	controller := fake.fakeController
	controller.namedPort = NewNamedPort()
	controller.namedPort.AddNamedPortByPod(pod)
	protocol := corev1.ProtocolTCP
	named := intstr.FromString("http")
	np := &netv1.NetworkPolicy{
		Name:      "allow-http",
		Namespace: metav1.NamespaceDefault,
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			Ingress:     []netv1.NetworkPolicyIngressRule{{Ports: []netv1.NetworkPolicyPort{{Port: &named, Protocol: &protocol}}}},
		},
	}

	variants, err := controller.networkPolicyNamedPortVariants(np, set.New(util.OvnProvider))
	require.NoError(t, err)
	require.Len(t, variants, 1)
	require.Equal(t, "http", variants[0].name)
	require.Equal(t, port, variants[0].port)
	require.Equal(t, []string{ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)}, variants[0].ports)

	variants, err = controller.networkPolicyNamedPortVariants(np, set.New("other.default.ovn"))
	require.NoError(t, err)
	require.Empty(t, variants)

	rangePort := intstr.FromString("http")
	np.Spec.Ingress[0].Ports[0].EndPort = new(int32(9000))
	np.Spec.Ingress[0].Ports[0].Port = &rangePort
	variants, err = controller.networkPolicyNamedPortVariants(np, nil)
	require.NoError(t, err)
	require.Empty(t, variants)

	require.Equal(t, "http\x00TCP", networkPolicyNamedPortVariantKey("http", corev1.ProtocolTCP))
}

func TestNetworkPolicyNamedPortVariantsWithoutIndex(t *testing.T) {
	controller := &Controller{namedPort: NewNamedPort()}
	np := &netv1.NetworkPolicy{Namespace: metav1.NamespaceDefault}
	variants, err := controller.networkPolicyNamedPortVariants(np, nil)
	require.NoError(t, err)
	require.Empty(t, variants)
}

func TestNetworkPolicyNamedPortAddressSetNameSanitizesPrefix(t *testing.T) {
	name := networkPolicyNamedPortAddressSetName("egress-tcp.cnp.egress", kubeovnv1.ProtocolIPv4, 5, "web", namedPortKey{
		protocol: corev1.ProtocolTCP,
		port:     80,
	})
	require.Equal(t, "egress.tcp.cnp.egress.IPv4.5.named.db6b339eeb58", name)
}

func TestNamedOnlyPolicyDoesNotAllowOtherPorts(t *testing.T) {
	pod := &corev1.Pod{
		Name: "server", Namespace: metav1.NamespaceDefault,
		Labels: map[string]string{"app": "server"},
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation:                                    util.DefaultSubnet,
			fmt.Sprintf(util.AllocatedAnnotationTemplate, util.OvnProvider): "true",
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.16.0.2",
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}}}}},
	}
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods: []*corev1.Pod{pod},
		Subnets: []*kubeovnv1.Subnet{{Name: util.DefaultSubnet, Spec: kubeovnv1.SubnetSpec{
			Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolIPv4,
		}}},
	})
	require.NoError(t, err)
	c := fake.fakeController
	nb := newInMemoryOVNNbClient(t)
	c.OVNNbClient = nb
	c.npKeyMutex = keymutex.NewHashed(0)
	c.namedPort = NewNamedPort()
	c.namedPort.AddNamedPortByPod(pod)
	require.NoError(t, c.OVNNbClient.CreateBareLogicalSwitch(util.DefaultSubnet))
	require.NoError(t, c.OVNNbClient.CreateBareLogicalSwitchPort(util.DefaultSubnet, ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider), "10.16.0.2", "00:00:00:00:00:02"))
	policy := &netv1.NetworkPolicy{
		Name: "allow-http", Namespace: metav1.NamespaceDefault,
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "server"}},
			Ingress:     []netv1.NetworkPolicyIngressRule{{Ports: []netv1.NetworkPolicyPort{{Port: new(intstr.FromString("http"))}}}},
		},
	}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, indexer.Add(policy))
	c.npsLister = networkinglister.NewNetworkPolicyLister(indexer)
	require.NoError(t, c.handleUpdateNp("default/allow-http"))
	acls, err := nb.ListAcls("to-lport", nil)
	require.NoError(t, err)
	allowCount := 0
	for _, acl := range acls {
		if acl.Action == "allow-related" && acl.Priority == 2001 {
			allowCount++
			require.Contains(t, acl.Match, "tcp.dst == 8080")
		}
	}
	require.Positive(t, allowCount)

	ipBlock := netv1.NetworkPolicyPeer{IPBlock: &netv1.IPBlock{CIDR: "10.0.0.0/8"}}
	ingressIPBlock := policy.DeepCopy()
	ingressIPBlock.Spec.Ingress[0].From = []netv1.NetworkPolicyPeer{ipBlock}
	require.NoError(t, indexer.Update(ingressIPBlock))
	require.NoError(t, c.handleUpdateNp("default/allow-http"))
	acls, err = nb.ListAcls("to-lport", nil)
	require.NoError(t, err)
	for _, acl := range acls {
		if acl.Action == "allow-related" && acl.Priority == 2001 {
			require.Contains(t, acl.Match, "tcp.dst == 8080")
		}
	}

	egressIPBlock := ingressIPBlock.DeepCopy()
	egressIPBlock.Spec.Ingress = nil
	egressIPBlock.Spec.Egress = []netv1.NetworkPolicyEgressRule{{
		To:    []netv1.NetworkPolicyPeer{ipBlock},
		Ports: []netv1.NetworkPolicyPort{{Port: new(intstr.FromString("http"))}},
	}}
	egressIPBlock.Spec.PolicyTypes = []netv1.PolicyType{netv1.PolicyTypeEgress}
	require.NoError(t, indexer.Update(egressIPBlock))
	require.NoError(t, c.handleUpdateNp("default/allow-http"))
	acls, err = nb.ListAcls("from-lport", nil)
	require.NoError(t, err)
	for _, acl := range acls {
		require.NotContains(t, acl.Match, "10.0.0.0/8")
	}

	egressAll := egressIPBlock.DeepCopy()
	egressAll.Spec.Egress[0].To = nil
	require.NoError(t, indexer.Update(egressAll))
	require.NoError(t, c.handleUpdateNp("default/allow-http"))
	acls, err = nb.ListAcls("from-lport", nil)
	require.NoError(t, err)
	allowCount = 0
	for _, acl := range acls {
		if acl.Action == "allow-related" && acl.Priority == 2001 && strings.Contains(acl.Match, "tcp.dst == 8080") {
			allowCount++
		}
	}
	require.Positive(t, allowCount)
}

func TestParsePolicyFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		annotation    *string
		wantProviders set.Set[string]
	}{
		{
			name:          "annotation omitted",
			annotation:    nil,
			wantProviders: nil,
		},
		{
			name:       "ovn only",
			annotation: new("ovn"),
			wantProviders: set.New(
				util.OvnProvider,
			),
		},
		{
			name:       "duplicate ovn",
			annotation: new("ovn, ovn"),
			wantProviders: set.New(
				util.OvnProvider,
			),
		},
		{
			name:       "secondary only",
			annotation: new("ns1/net1"),
			wantProviders: set.New(
				"net1.ns1." + util.OvnProvider,
			),
		},
		{
			name:       "ovn and secondary",
			annotation: new(" ovn , ns1/net1 "),
			wantProviders: set.New(
				util.OvnProvider,
				"net1.ns1."+util.OvnProvider,
			),
		},
		{
			name:       "ovn and invalid",
			annotation: new("ovn, foo"),
			wantProviders: set.New(
				util.OvnProvider,
			),
		},
		{
			name:          "invalid all",
			annotation:    new("all"),
			wantProviders: set.New[string](),
		},
		{
			name:          "invalid default",
			annotation:    new("default"),
			wantProviders: set.New[string](),
		},
		{
			name:          "invalid no entries",
			annotation:    new(","),
			wantProviders: set.New[string](),
		},
		{
			name:          "invalid token",
			annotation:    new("foo"),
			wantProviders: set.New[string](),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			np := &netv1.NetworkPolicy{
				Name:      "np",
				Namespace: "default",
			}
			if tt.annotation != nil {
				np.Annotations = map[string]string{
					util.NetworkPolicyForAnnotation: *tt.annotation,
				}
			}

			providers := parsePolicyFor(np)
			if tt.wantProviders == nil {
				require.Nil(t, providers)
				return
			}
			require.Equal(t, tt.wantProviders, providers)
		})
	}
}

func TestNetpolAppliesToProvider(t *testing.T) {
	t.Parallel()
	providers := set.New("ovn", "net1.ns1.ovn")
	require.True(t, netpolAppliesToProvider("ovn", providers))
	require.False(t, netpolAppliesToProvider("net2.ns2.ovn", providers))
	require.True(t, netpolAppliesToProvider("ovn", nil))
	require.False(t, netpolAppliesToProvider("ovn", set.New[string]()))
}
