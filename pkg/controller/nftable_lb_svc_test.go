package controller

import (
	"context"
	"maps"
	"slices"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func Test_nftableLbSvcQualifies(t *testing.T) {
	t.Parallel()

	withAnnotations := func(svcType v1.ServiceType, anns map[string]string) *v1.Service {
		return &v1.Service{Annotations: anns, Spec: v1.ServiceSpec{Type: svcType}}
	}
	gateway := map[string]string{util.VpcNatGatewaySvcAnnotation: "gw0"}

	tests := []struct {
		name     string
		svc      *v1.Service
		expected bool
	}{
		{
			name:     "loadbalancer naming its gateway and an eip",
			svc:      withAnnotations(v1.ServiceTypeLoadBalancer, map[string]string{util.VpcNatGatewaySvcAnnotation: "gw0", util.EipAnnotation: "eip0"}),
			expected: true,
		},
		{
			// Without an EIP a LoadBalancer Service has no ingress IP to publish, so it is not
			// handled: the gateway serves it through the EIP's address.
			name:     "loadbalancer naming its gateway without an eip",
			svc:      withAnnotations(v1.ServiceTypeLoadBalancer, gateway),
			expected: false,
		},
		{
			name:     "clusterip naming its gateway",
			svc:      withAnnotations(v1.ServiceTypeClusterIP, gateway),
			expected: true,
		},
		{
			name:     "loadbalancer without a gateway",
			svc:      withAnnotations(v1.ServiceTypeLoadBalancer, map[string]string{util.EipAnnotation: "eip0"}),
			expected: false,
		},
		{
			name:     "clusterip without a gateway",
			svc:      withAnnotations(v1.ServiceTypeClusterIP, map[string]string{util.EipAnnotation: "eip0"}),
			expected: false,
		},
		{
			// Naming a vpc nat gateway wins over the per-Service forwarder's annotation: the two
			// halves of the exclusivity agree (the forwarder is removed instead of kept in sync),
			// so a Service carrying both is served by exactly one of them.
			name: "loadbalancer naming both its gateway and an attachment provider",
			svc: withAnnotations(v1.ServiceTypeLoadBalancer, map[string]string{
				util.VpcNatGatewaySvcAnnotation: "gw0",
				util.EipAnnotation:              "eip0",
				util.AttachmentProvider:         "lb-svc-attachment.kube-system",
			}),
			expected: true,
		},
		{
			name:     "nodeport is not handled",
			svc:      withAnnotations(v1.ServiceTypeNodePort, gateway),
			expected: false,
		},
		{
			name:     "externalname is not handled",
			svc:      withAnnotations(v1.ServiceTypeExternalName, gateway),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, nftableLbSvcQualifies(tt.svc))
		})
	}
}

func TestNftableLbEventHelpersRespectFeatureGate(t *testing.T) {
	t.Parallel()

	// Feature disabled: pod/EIP/NAT-GW event helpers must be no-ops even when their
	// listers/indexers are not initialized, so the shared pod/EIP/NAT-GW informer hot paths
	// do not pay for the nftable LB feature when it is off.
	c := &Controller{config: &Configuration{EnableOvnLB: true}}
	require.NotPanics(t, func() {
		c.enqueueNftableLbServicesForPod(&v1.Pod{Namespace: "ns", Name: "pod"})
		c.enqueueNftableLbServicesForEIP("eip0")
		c.enqueueNftableLbServicesForNatGw("gw")
	})
}

func TestEnqueueNftableLbServiceWithoutOvnLb(t *testing.T) {
	t.Parallel()

	// Gateway mode runs with the OVN mode disabled, so Service events must feed its own queue.
	queue := newTypedRateLimitingQueue[string]("nftable-lb-no-ovn-lb", nil)
	t.Cleanup(queue.ShutDown)
	c := &Controller{
		config:                       &Configuration{EnableOvnLB: false, EnableGwNftableLbSvc: true},
		addOrUpdateNftableLbSvcQueue: queue,
	}

	c.enqueueNftableLbService("default/web")
	require.Equal(t, 1, c.addOrUpdateNftableLbSvcQueue.Len())
	item, _ := c.addOrUpdateNftableLbSvcQueue.Get()
	require.Equal(t, "default/web", item)
	c.addOrUpdateNftableLbSvcQueue.Done(item)
}

func TestEnqueueNftableLbSvcOwnersFromRules(t *testing.T) {
	t.Parallel()

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, rule := range []*kubeovnv1.IptablesDnatRule{
		{Name: "owned-a", Labels: map[string]string{util.NftableLbSvcNsLabel: "ns1", util.NftableLbSvcNameLabel: "svc1"}},
		{Name: "owned-b", Labels: map[string]string{util.NftableLbSvcNsLabel: "ns1", util.NftableLbSvcNameLabel: "svc1"}},
		{Name: "owned-c", Labels: map[string]string{util.NftableLbSvcNsLabel: "ns2", util.NftableLbSvcNameLabel: "svc2"}},
		{Name: "manual", Spec: kubeovnv1.IptablesDnatRuleSpec{Type: kubeovnv1.DnatRuleTypeShare}},
	} {
		require.NoError(t, indexer.Add(rule))
	}

	c := &Controller{
		config:                       &Configuration{EnableGwNftableLbSvc: true},
		iptablesDnatRulesLister:      kubeovnlister.NewIptablesDnatRuleLister(indexer),
		addOrUpdateNftableLbSvcQueue: newTypedRateLimitingQueue[string]("AddOrUpdateNftableLbSvc", nil),
	}
	t.Cleanup(c.addOrUpdateNftableLbSvcQueue.ShutDown)

	require.NoError(t, c.enqueueNftableLbSvcOwnersFromRules())
	require.Equal(t, 2, c.addOrUpdateNftableLbSvcQueue.Len())
	owners := map[string]struct{}{}
	for c.addOrUpdateNftableLbSvcQueue.Len() > 0 {
		owner, _ := c.addOrUpdateNftableLbSvcQueue.Get()
		owners[owner] = struct{}{}
		c.addOrUpdateNftableLbSvcQueue.Done(owner)
	}
	require.Equal(t, map[string]struct{}{"ns1/svc1": {}, "ns2/svc2": {}}, owners)
}

func Test_nftableLbDnatRuleName(t *testing.T) {
	t.Parallel()

	// deterministic: same inputs produce the same name
	a := nftableLbDnatRuleName("ns", "svc", "tcp", "80", "10.0.0.1", "8080")
	b := nftableLbDnatRuleName("ns", "svc", "tcp", "80", "10.0.0.1", "8080")
	require.Equal(t, a, b)

	// different backend produces a different name
	c := nftableLbDnatRuleName("ns", "svc", "tcp", "80", "10.0.0.2", "8080")
	require.NotEqual(t, a, c)

	// name is DNS-1123 compliant and within the 63 char limit even for long svc names
	longName := "this-is-a-very-long-service-name-that-exceeds-the-kubernetes-limit-for-sure"
	got := nftableLbDnatRuleName("ns", longName, "tcp", "80", "10.0.0.1", "8080")
	require.LessOrEqual(t, len(got), 63)
	require.Empty(t, validation.IsDNS1123Subdomain(got))
}

func Test_nftableLbBackendResolver_missingTargetPod(t *testing.T) {
	t.Parallel()

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	c := &Controller{podsLister: corelisters.NewPodLister(indexer)}
	resolver := c.nftableLbBackendResolver(&v1.Service{Namespace: "ns"}, "gw-vpc")

	// EndpointSlice entry still references a Pod that no longer exists: the backend must be
	// skipped instead of falling back to the stale recorded IP (manual/selectorless slices
	// are not automatically pruned by the k8s endpointslice controller).
	ep := discoveryv1.Endpoint{
		Addresses: []string{"10.0.0.1"},
		TargetRef: &v1.ObjectReference{Kind: "Pod", Namespace: "ns", Name: "gone"},
	}
	ip, ok := resolver(ep)
	require.False(t, ok)
	require.Empty(t, ip)

	// Endpoints without a Pod target remain best-effort (e.g. externally maintained slices).
	ip, ok = resolver(discoveryv1.Endpoint{Addresses: []string{"10.0.0.2"}})
	require.True(t, ok)
	require.Equal(t, "10.0.0.2", ip)
}

// testNftableLbBackendIP resolves an endpoint to its first IPv4 address, matching the
// single-NIC (gateway-VPC-agnostic) behavior these table tests exercise.
func testNftableLbBackendIP(ep discoveryv1.Endpoint) (string, bool) {
	ip := firstIPv4(ep.Addresses)
	return ip, ip != ""
}

func Test_selectNftableLbBackendIPv4(t *testing.T) {
	t.Parallel()

	// single-NIC backend already in the gateway VPC: matches on its only NIC
	ip, ok := selectNftableLbBackendIPv4([]nftableLbNicCandidate{{ipv4: "10.0.0.1", vpc: "vpc1"}}, "vpc1")
	require.True(t, ok)
	require.Equal(t, "10.0.0.1", ip)

	// dual-NIC backend: primary in default VPC, secondary in the gateway VPC -> pick secondary
	ip, ok = selectNftableLbBackendIPv4([]nftableLbNicCandidate{
		{ipv4: "10.16.0.5", vpc: "ovn-cluster"},
		{ipv4: "192.168.0.5", vpc: "vpc1"},
	}, "vpc1")
	require.True(t, ok)
	require.Equal(t, "192.168.0.5", ip)

	// several NICs in the gateway VPC: deterministic lowest IPv4 (dual-NIC is the 99% case)
	ip, ok = selectNftableLbBackendIPv4([]nftableLbNicCandidate{
		{ipv4: "192.168.0.9", vpc: "vpc1"},
		{ipv4: "192.168.0.3", vpc: "vpc1"},
	}, "vpc1")
	require.True(t, ok)
	require.Equal(t, "192.168.0.3", ip)

	// has kube-ovn NICs but none in the gateway VPC -> no match (caller skips the backend)
	_, ok = selectNftableLbBackendIPv4([]nftableLbNicCandidate{{ipv4: "10.16.0.5", vpc: "ovn-cluster"}}, "vpc1")
	require.False(t, ok)

	// a NIC in the gateway VPC without an IPv4 is not selectable
	_, ok = selectNftableLbBackendIPv4([]nftableLbNicCandidate{{ipv4: "", vpc: "vpc1"}}, "vpc1")
	require.False(t, ok)

	// no candidates -> no match
	_, ok = selectNftableLbBackendIPv4(nil, "vpc1")
	require.False(t, ok)
}

func Test_firstIPv4(t *testing.T) {
	t.Parallel()

	require.Equal(t, "10.0.0.1", firstIPv4([]string{"fd00::1", "10.0.0.1"}))
	require.Equal(t, "10.0.0.1", firstIPv4([]string{"10.0.0.1"}))
	require.Empty(t, firstIPv4([]string{"fd00::1"}))
	require.Empty(t, firstIPv4(nil))
}

func Test_buildDesiredNftableLbDnatRules(t *testing.T) {
	t.Parallel()

	svc := &v1.Service{
		Namespace: "default", Name: "web",
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{
				{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
				{Name: "sctp", Port: 90, Protocol: v1.ProtocolSCTP},
			},
		},
	}

	endpointSlices := []*discoveryv1.EndpointSlice{
		{
			Ports: []discoveryv1.EndpointPort{
				{Name: new("http"), Port: new(int32(8080))},
				{Name: new("sctp"), Port: new(int32(9090))},
			},
			Endpoints: []discoveryv1.Endpoint{
				{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: new(true)}},
				{Addresses: []string{"10.0.0.2"}, Conditions: discoveryv1.EndpointConditions{Ready: new(true)}},
				// not ready -> skipped
				{Addresses: []string{"10.0.0.3"}, Conditions: discoveryv1.EndpointConditions{Ready: new(false)}},
				// IPv6 -> skipped (share DNAT is IPv4 only)
				{Addresses: []string{"fd00::1"}, Conditions: discoveryv1.EndpointConditions{Ready: new(true)}},
			},
		},
	}

	desired := buildDesiredNftableLbDnatRules(svc, "eip0", "gw0", endpointSlices, testNftableLbBackendIP)

	// 2 ready IPv4 backends on the tcp port, and the sctp port is skipped: share DNAT only supports
	// tcp/udp for now (see the TODO on util.ValidateProtocol)
	tcpRules := make(map[string]*kubeovnv1.IptablesDnatRule)
	for _, rule := range desired {
		require.Equal(t, "tcp", rule.Spec.Protocol)
		tcpRules[rule.Spec.InternalIP] = rule
	}
	require.Len(t, desired, 2)
	require.Len(t, tcpRules, 2)
	require.Equal(t, "80", tcpRules["10.0.0.1"].Spec.ExternalPort)
	require.Equal(t, "8080", tcpRules["10.0.0.1"].Spec.InternalPort)

	backends := make(map[string]*kubeovnv1.IptablesDnatRule)
	for _, rule := range tcpRules {
		require.Equal(t, "eip0", rule.Spec.EIP)
		require.Equal(t, "80", rule.Spec.ExternalPort)
		require.Equal(t, "8080", rule.Spec.InternalPort)
		require.Equal(t, "tcp", rule.Spec.Protocol)
		require.Equal(t, kubeovnv1.DnatRuleTypeShare, rule.Spec.Type)
		require.Equal(t, "default", rule.Labels[util.NftableLbSvcNsLabel])
		require.Equal(t, "web", rule.Labels[util.NftableLbSvcNameLabel])
		backends[rule.Spec.InternalIP] = rule
	}
	require.Contains(t, backends, "10.0.0.1")
	require.Contains(t, backends, "10.0.0.2")
	require.NotContains(t, backends, "10.0.0.3")
	require.NotContains(t, backends, "fd00::1")
}

func Test_buildDesiredNftableLbDnatRules_unnamedPort(t *testing.T) {
	t.Parallel()

	svc := &v1.Service{
		Namespace: "default", Name: "web",
		Spec: v1.ServiceSpec{
			Type:  v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{{Port: 80, Protocol: v1.ProtocolTCP}},
		},
	}
	endpointSlices := []*discoveryv1.EndpointSlice{
		{
			Ports:     []discoveryv1.EndpointPort{{Port: new(int32(8080))}},
			Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: new(true)}}},
		},
	}

	require.Len(t, buildDesiredNftableLbDnatRules(svc, "eip0", "gw0", endpointSlices, testNftableLbBackendIP), 1)
}

func Test_buildDesiredNftableLbDnatRules_noMatchingPort(t *testing.T) {
	t.Parallel()

	svc := &v1.Service{
		Namespace: "default", Name: "web",
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{
				{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
			},
		},
	}
	endpointSlices := []*discoveryv1.EndpointSlice{
		{
			Ports: []discoveryv1.EndpointPort{
				{Name: new("other"), Port: new(int32(8080))},
			},
			Endpoints: []discoveryv1.Endpoint{
				{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: new(true)}},
			},
		},
	}

	desired := buildDesiredNftableLbDnatRules(svc, "eip0", "gw0", endpointSlices, testNftableLbBackendIP)
	require.Empty(t, desired)
}

func Test_buildDesiredNftableLbDnatRules_sessionAffinity(t *testing.T) {
	t.Parallel()

	endpointSlices := []*discoveryv1.EndpointSlice{
		{
			Ports: []discoveryv1.EndpointPort{
				{Name: new("http"), Port: new(int32(8080))},
			},
			Endpoints: []discoveryv1.Endpoint{
				{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: new(true)}},
			},
		},
	}
	ports := []v1.ServicePort{{Name: "http", Port: 80, Protocol: v1.ProtocolTCP}}

	// ClientIP affinity with explicit timeout is propagated to the generated rules
	svc := &v1.Service{
		Namespace: "default", Name: "web",
		Spec: v1.ServiceSpec{
			Type:            v1.ServiceTypeLoadBalancer,
			Ports:           ports,
			SessionAffinity: v1.ServiceAffinityClientIP,
			SessionAffinityConfig: &v1.SessionAffinityConfig{
				ClientIP: &v1.ClientIPConfig{TimeoutSeconds: new(int32(600))},
			},
		},
	}
	desired := buildDesiredNftableLbDnatRules(svc, "eip0", "gw0", endpointSlices, testNftableLbBackendIP)
	require.Len(t, desired, 1)
	for _, rule := range desired {
		require.Equal(t, kubeovnv1.DnatSessionAffinityClientIP, rule.Spec.SessionAffinity)
		require.Equal(t, int32(600), rule.Spec.SessionAffinityTimeoutSeconds)
	}

	// No affinity: fields stay empty (stateless numgen-random balancing)
	svcNone := &v1.Service{
		Namespace: "default", Name: "web",
		Spec: v1.ServiceSpec{
			Type:            v1.ServiceTypeLoadBalancer,
			Ports:           ports,
			SessionAffinity: v1.ServiceAffinityNone,
		},
	}
	desiredNone := buildDesiredNftableLbDnatRules(svcNone, "eip0", "gw0", endpointSlices, testNftableLbBackendIP)
	require.Len(t, desiredNone, 1)
	for _, rule := range desiredNone {
		require.Equal(t, kubeovnv1.DnatSessionAffinityNone, rule.Spec.SessionAffinity)
		require.Zero(t, rule.Spec.SessionAffinityTimeoutSeconds)
	}
}

func Test_nftableLbSvcClusterIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		svc      v1.Service
		expected string
	}{
		{
			name:     "single stack",
			svc:      v1.Service{Spec: v1.ServiceSpec{ClusterIP: "10.96.1.5", ClusterIPs: []string{"10.96.1.5"}}},
			expected: "10.96.1.5",
		},
		{
			name:     "dual stack prefers IPv4",
			svc:      v1.Service{Spec: v1.ServiceSpec{ClusterIP: "10.96.1.5", ClusterIPs: []string{"10.96.1.5", "fd00:10:16::1"}}},
			expected: "10.96.1.5",
		},
		{
			name:     "legacy ClusterIP field only",
			svc:      v1.Service{Spec: v1.ServiceSpec{ClusterIP: "10.96.1.5"}},
			expected: "10.96.1.5",
		},
		{
			name: "headless",
			svc:  v1.Service{Spec: v1.ServiceSpec{ClusterIP: v1.ClusterIPNone}},
		},
		{
			name: "not allocated yet",
			svc:  v1.Service{Spec: v1.ServiceSpec{}},
		},
		{
			name: "IPv6 only (share DNAT is IPv4 only)",
			svc:  v1.Service{Spec: v1.ServiceSpec{ClusterIPs: []string{"fd00:10:16::1"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, nftableLbSvcClusterIP(&tt.svc))
		})
	}
}

func Test_buildDesiredNftableLbDnatRules_clusterIP(t *testing.T) {
	t.Parallel()

	svc := &v1.Service{
		Namespace: "default", Name: "web",
		Spec: v1.ServiceSpec{
			Type:       v1.ServiceTypeLoadBalancer,
			ClusterIP:  "10.96.1.5",
			ClusterIPs: []string{"10.96.1.5"},
			Ports:      []v1.ServicePort{{Name: "http", Port: 80, Protocol: v1.ProtocolTCP}},
		},
	}
	endpointSlices := []*discoveryv1.EndpointSlice{
		{
			Ports: []discoveryv1.EndpointPort{{Name: new("http"), Port: new(int32(8080))}},
			Endpoints: []discoveryv1.Endpoint{
				{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: new(true)}},
				{Addresses: []string{"10.0.0.2"}, Conditions: discoveryv1.EndpointConditions{Ready: new(true)}},
			},
		},
	}

	// the internal VIP is part of every generated rule: it is the durable record of the ClusterIP
	// identity the gateway programs next to the EIP, and it is what a ClusterIP Service has
	desired := buildDesiredNftableLbDnatRules(svc, "eip0", "gw0", endpointSlices, testNftableLbBackendIP)
	require.Len(t, desired, 2)
	for _, rule := range desired {
		require.Equal(t, "10.96.1.5", rule.Spec.ClusterIP)
	}

	// a headless Service has no internal VIP: the EIP identity is programmed alone
	svcHeadless := svc.DeepCopy()
	svcHeadless.Spec.ClusterIP = v1.ClusterIPNone
	svcHeadless.Spec.ClusterIPs = nil
	desired = buildDesiredNftableLbDnatRules(svcHeadless, "eip0", "gw0", endpointSlices, testNftableLbBackendIP)
	require.Len(t, desired, 2)
	for _, rule := range desired {
		require.Empty(t, rule.Spec.ClusterIP)
	}
}

func Test_nftableLbSvcOwnerKey(t *testing.T) {
	t.Parallel()

	owned := &kubeovnv1.IptablesDnatRule{
		Labels: map[string]string{
			util.NftableLbSvcNsLabel:   "default",
			util.NftableLbSvcNameLabel: "web",
		},
	}
	require.Equal(t, "default/web", nftableLbSvcOwnerKey(owned))

	for _, labels := range []map[string]string{
		{util.NftableLbSvcNsLabel: "default"},
		{util.NftableLbSvcNameLabel: "web"},
	} {
		require.Empty(t, nftableLbSvcOwnerKey(&kubeovnv1.IptablesDnatRule{Labels: labels}))
	}

	manual := &kubeovnv1.IptablesDnatRule{}
	require.Empty(t, nftableLbSvcOwnerKey(manual))
}

func Test_nftableLbDnatIdentity(t *testing.T) {
	t.Parallel()

	// protocol is normalized to lower case so TCP and tcp map to the same identity
	require.Equal(t, "eip0/80/tcp", nftableLbDnatIdentity("eip0", "80", "TCP"))
	require.Equal(t, nftableLbDnatIdentity("eip0", "80", "tcp"), nftableLbDnatIdentity("eip0", "80", "TCP"))
	require.NotEqual(t, nftableLbDnatIdentity("eip0", "80", "tcp"), nftableLbDnatIdentity("eip0", "443", "tcp"))
}

func Test_nftableLbSvcIdentities(t *testing.T) {
	t.Parallel()

	svc := &v1.Service{
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{Port: 80, Protocol: v1.ProtocolTCP},
				{Port: 53, Protocol: v1.ProtocolUDP},
				{Port: 90, Protocol: v1.ProtocolSCTP},
			},
		},
	}
	ids := nftableLbSvcIdentities(svc, "eip0")
	// identities must match what buildDesiredNftableLbDnatRules would program, including skipping
	// the ports it does not program: share DNAT only supports tcp/udp for now
	require.ElementsMatch(t, []string{"eip0/80/tcp", "eip0/53/udp"}, ids)
}

func TestResolveNftableLbConflictsWithNoReadyBackends(t *testing.T) {
	t.Parallel()

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		IndexServiceByNftableLbEip: indexServiceByNftableLbEip,
	})
	port := int32(80)
	newService := func(namespace, name string) *v1.Service {
		return &v1.Service{
			Namespace: namespace,
			Name:      name,
			Annotations: map[string]string{
				util.EipAnnotation:              "eip0",
				util.VpcNatGatewaySvcAnnotation: "gw0",
			},
			Spec: v1.ServiceSpec{
				Type:  v1.ServiceTypeLoadBalancer,
				Ports: []v1.ServicePort{{Port: port, Protocol: v1.ProtocolTCP}},
			},
		}
	}
	winner := newService("ns", "a-winner")
	loser := newService("ns", "z-loser")
	require.NoError(t, indexer.Add(winner))
	require.NoError(t, indexer.Add(loser))

	ruleIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	queue := newTypedRateLimitingQueue[string]("nftable-lb-conflict-test", nil)
	t.Cleanup(queue.ShutDown)
	controller := &Controller{
		svcIndexer:                   indexer,
		iptablesDnatRulesLister:      kubeovnlister.NewIptablesDnatRuleLister(ruleIndexer),
		recorder:                     record.NewFakeRecorder(1),
		addOrUpdateNftableLbSvcQueue: queue,
	}

	conflicted, err := controller.resolveNftableLbConflicts(loser, "ns/z-loser", map[string]*kubeovnv1.IptablesDnatRule{})
	require.NoError(t, err)
	require.True(t, conflicted, "a loser must be detected from Service ports even without ready backends")
}

func TestResolveNftableLbConflictsIgnoresTerminatingServices(t *testing.T) {
	t.Parallel()

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		IndexServiceByNftableLbEip: indexServiceByNftableLbEip,
	})
	newService := func(name string, deleting bool) *v1.Service {
		svc := &v1.Service{
			Namespace: "ns",
			Name:      name,
			Annotations: map[string]string{
				util.EipAnnotation:              "eip0",
				util.VpcNatGatewaySvcAnnotation: "gw0",
			},
			Spec: v1.ServiceSpec{
				Type:  v1.ServiceTypeLoadBalancer,
				Ports: []v1.ServicePort{{Port: 80, Protocol: v1.ProtocolTCP}},
			},
		}
		if deleting {
			now := metav1.Now()
			svc.DeletionTimestamp = &now
		}
		return svc
	}
	// The terminating Service sorts first, so it would own the identity and starve the live
	// Service until the informer cache dropped it.
	terminating := newService("a-terminating", true)
	loser := newService("z-loser", false)
	require.NoError(t, indexer.Add(terminating))
	require.NoError(t, indexer.Add(loser))

	ruleIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	queue := newTypedRateLimitingQueue[string]("nftable-lb-terminating-test", nil)
	t.Cleanup(queue.ShutDown)
	controller := &Controller{
		svcIndexer:                   indexer,
		iptablesDnatRulesLister:      kubeovnlister.NewIptablesDnatRuleLister(ruleIndexer),
		recorder:                     record.NewFakeRecorder(1),
		addOrUpdateNftableLbSvcQueue: queue,
	}

	conflicted, err := controller.resolveNftableLbConflicts(loser, "ns/z-loser", map[string]*kubeovnv1.IptablesDnatRule{})
	require.NoError(t, err)
	require.False(t, conflicted, "a terminating Service must release the identity to its successor")
}

func Test_nftableLbDnatSpecEqual(t *testing.T) {
	t.Parallel()

	base := kubeovnv1.IptablesDnatRuleSpec{
		EIP: "eip0", ExternalPort: "80", Protocol: "tcp",
		InternalIP: "10.0.0.1", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
	}
	same := base
	require.True(t, nftableLbDnatSpecEqual(&base, &same))

	// session-affinity drift is detected (drives rule recreate)
	affinity := base
	affinity.SessionAffinity = kubeovnv1.DnatSessionAffinityClientIP
	affinity.SessionAffinityTimeoutSeconds = 600
	require.False(t, nftableLbDnatSpecEqual(&base, &affinity))

	// EIP drift (annotation changed) is detected
	eip := base
	eip.EIP = "eip1"
	require.False(t, nftableLbDnatSpecEqual(&base, &eip))
}

func Test_chooseNftableLbOwner(t *testing.T) {
	t.Parallel()

	// a manually-created share rule (owner "") always wins
	require.Empty(t, chooseNftableLbOwner(map[string]struct{}{"": {}, "ns/a": {}, "ns/b": {}}))

	// otherwise the lexicographically smallest service key wins
	require.Equal(t, "ns/a", chooseNftableLbOwner(map[string]struct{}{"ns/a": {}, "ns/b": {}}))
	require.Equal(t, "ns/a", chooseNftableLbOwner(map[string]struct{}{"ns/b": {}, "ns/a": {}}))

	// single owner wins trivially
	require.Equal(t, "ns/only", chooseNftableLbOwner(map[string]struct{}{"ns/only": {}}))
}

func Test_desiredNatGwVipState(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	ruleIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	eipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	controller := &Controller{
		iptablesDnatRulesLister: kubeovnlister.NewIptablesDnatRuleLister(ruleIndexer),
		iptablesEipsLister:      kubeovnlister.NewIptablesEIPLister(eipIndexer),
	}

	owned := func(name, eip, clusterIP string) *kubeovnv1.IptablesDnatRule {
		rule := &kubeovnv1.IptablesDnatRule{
			Name: name,
			Labels: map[string]string{
				util.NftableLbSvcNsLabel:    "default",
				util.NftableLbSvcNameLabel:  "web",
				util.VpcNatGatewayNameLabel: gwName,
			},
			Spec: kubeovnv1.IptablesDnatRuleSpec{
				EIP: eip, ExternalPort: "80", Protocol: "tcp",
				InternalIP: "10.0.0.1", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
			},
		}
		rule.Spec.ClusterIP = clusterIP
		return rule
	}

	require.NoError(t, eipIndexer.Add(&kubeovnv1.IptablesEIP{
		Name:   "eip0",
		Status: kubeovnv1.IptablesEIPStatus{IP: "203.0.113.10"},
	}))
	require.NoError(t, eipIndexer.Add(&kubeovnv1.IptablesEIP{
		Name: "not-ready",
	}))

	// both VIPs of a generated rule are routed, once per VIP even with two backends
	require.NoError(t, ruleIndexer.Add(owned("a", "eip0", "10.96.1.5")))
	require.NoError(t, ruleIndexer.Add(owned("b", "eip0", "10.96.1.5")))
	// a hand-managed share rule contributes the address it serves, like any other rule: the rule
	// shape decides, not who created it. Its EIP is already routed by the rules above (same EIP),
	// which is why the expected set below does not grow.
	manual := owned("manual", "eip0", "")
	delete(manual.Labels, util.NftableLbSvcNameLabel)
	require.NoError(t, ruleIndexer.Add(manual))
	// a terminating rule must not keep a route alive
	terminating := owned("terminating", "eip0", "10.96.1.9")
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	terminating.Finalizers = []string{"keep"}
	require.NoError(t, ruleIndexer.Add(terminating))
	// an EIP that is not ready yet has no address to route. Its ClusterIP is still routed: the
	// internal VIP does not depend on the EIP.
	require.NoError(t, ruleIndexer.Add(owned("not-ready", "not-ready", "10.96.1.7")))

	vips, clusterIPs, err := controller.desiredNatGwVipState(gwName, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"ip4.dst == 203.0.113.10": "203.0.113.10",
		"ip4.dst == 10.96.1.5":    "10.96.1.5",
		"ip4.dst == 10.96.1.7":    "10.96.1.7",
	}, vips)
	// only the ClusterIPs are held on lo: the EIP already lives on the external interface, and a
	// terminating rule's ClusterIP is released
	require.Equal(t, []string{"10.96.1.5", "10.96.1.7"}, clusterIPs)

	// an IPv6-only ClusterIP has no IPv4 route (share DNAT is IPv4 only)
	require.NoError(t, ruleIndexer.Add(owned("v6", "eip0", "fd00::1")))
	vips, clusterIPs, err = controller.desiredNatGwVipState(gwName, nil)
	require.NoError(t, err)
	require.NotContains(t, vips, "ip4.dst == fd00::1")
	require.Len(t, vips, 3)
	require.NotContains(t, clusterIPs, "fd00::1")

	// A Service-driven rule carries its ClusterIP in the spec (the field replaces the annotation):
	// both of its addresses belong to the gateway state, the ingress IP and the internal VIP. The
	// rule is listed on its own so the counts above stay independent of this case.
	viaSpec := ownedShareRule("viaspec", "eip0", "")
	viaSpec.Spec.ClusterIP = "10.96.1.8"
	require.NoError(t, ruleIndexer.Add(viaSpec))
	vips, clusterIPs, err = controller.desiredNatGwVipState(gwName, nil)
	require.NoError(t, err)
	require.Equal(t, "10.96.1.8", vips["ip4.dst == 10.96.1.8"], "the internal VIP of the rule is routed")
	require.Contains(t, clusterIPs, "10.96.1.8", "and held on lo")
	require.Equal(t, "203.0.113.10", vips["ip4.dst == 203.0.113.10"], "next to the ingress IP it aligns with")
}

func TestDesiredNatGwVipStateConvergesAfterLabelUpdate(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	controller := &Controller{
		iptablesDnatRulesLister: kubeovnlister.NewIptablesDnatRuleLister(indexer),
		iptablesEipsLister:      kubeovnlister.NewIptablesEIPLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
	}
	first := clusterIPServedRule("first", gwName, "10.96.1.5")
	second := clusterIPServedRule("second", gwName, "10.96.1.6")

	// The second create reconcile can see only itself while the first rule's gateway label is still
	// absent from the informer cache, so its full-set sync temporarily narrows the desired set.
	delete(first.Labels, util.VpcNatGatewayNameLabel)
	require.NoError(t, indexer.Add(first))
	require.NoError(t, indexer.Add(second))
	_, clusterIPs, err := controller.desiredNatGwVipState(gwName, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"10.96.1.6"}, clusterIPs)

	// The label patch emits the update event that calls syncNatGwVipStateFromCache. Once visible,
	// deriving the set from cache restores both VIPs.
	first.Labels[util.VpcNatGatewayNameLabel] = gwName
	require.NoError(t, indexer.Update(first))
	vips, clusterIPs, err := controller.desiredNatGwVipState(gwName, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"10.96.1.5", "10.96.1.6"}, clusterIPs)
	require.Equal(t, map[string]string{
		"ip4.dst == 10.96.1.5": "10.96.1.5",
		"ip4.dst == 10.96.1.6": "10.96.1.6",
	}, vips)
}

// Test_natGwVipStateSerializedPerGateway pins that both halves of a gateway's shared VIP state take
// the gateway lock. Both are a read-modify-write of state derived from the gateway's rules, while
// the DNAT handlers lock by rule name and run add and update in separate workers, so two rules of
// one gateway reconcile concurrently: without the lock the call that lands last would win and drop
// an address or route the other one had just made desired.
//
// The lock is asserted by holding it: the call must not finish while another holder has the key,
// and must finish once it is released. No instance is passed, so nothing is executed in a Pod.
func Test_natGwVipStateSerializedPerGateway(t *testing.T) {
	t.Parallel()

	const gwName = "lock-gw"
	ruleIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	controller := &Controller{
		iptablesDnatRulesLister: kubeovnlister.NewIptablesDnatRuleLister(ruleIndexer),
		vpcNatGatewayLister:     kubeovnlister.NewVpcNatGatewayLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
		natGwVipKeyMutex:        keymutex.NewHashed(1),
		config:                  &Configuration{EnableGwNftableLbSvc: true},
	}

	for _, tc := range []struct {
		name string
		call func() error
	}{
		// syncNatGwVipState resolves the gateway first and returns once it is not found, so it
		// reaches its critical section with the lister empty.
		{"routes", func() error { return controller.syncNatGwVipState(gwName, nil) }},
		{"addresses", func() error { return controller.syncNatGwVipAddrs(nil, gwName, nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			controller.natGwVipKeyMutex.LockKey(gwName)
			done := make(chan error, 1)
			go func() { done <- tc.call() }()

			select {
			case <-done:
				require.Fail(t, "sync ran while another reconcile of the same gateway held its key")
			case <-time.After(100 * time.Millisecond):
			}

			require.NoError(t, controller.natGwVipKeyMutex.UnlockKey(gwName))
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(10 * time.Second):
				require.Fail(t, "sync did not proceed after the gateway key was released")
			}
		})
	}
}

// Test_desiredNatGwVipState_applying pins the override the DNAT handler relies on: it patches the
// gateway label and programs the rule in the same pass, so the informer cache regularly does not
// show the rule yet (and can still show a terminating one as live). The rule the caller is holding
// decides, not the cache, otherwise the first rule of a Service would be programmed in nftables
// with no VPC route and no address on lo, with nothing bounding the repair.
func Test_desiredNatGwVipState_applying(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	ruleIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	eipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	controller := &Controller{
		iptablesDnatRulesLister: kubeovnlister.NewIptablesDnatRuleLister(ruleIndexer),
		iptablesEipsLister:      kubeovnlister.NewIptablesEIPLister(eipIndexer),
	}
	require.NoError(t, eipIndexer.Add(&kubeovnv1.IptablesEIP{
		Name:   "eip0",
		Status: kubeovnv1.IptablesEIPStatus{IP: "203.0.113.10"},
	}))

	// The rule as the handler holds it: created with the owner labels, the gateway label patched
	// through the API but not observed here, so the lister is empty.
	applying := ownedShareRule("rule-a", "eip0", "10.96.1.5")
	delete(applying.Labels, util.VpcNatGatewayNameLabel)

	vips, clusterIPs, err := controller.desiredNatGwVipState(gwName, nil)
	require.NoError(t, err)
	require.Empty(t, vips, "the cache has not observed the rule yet")
	require.Empty(t, clusterIPs)

	vips, clusterIPs, err = controller.desiredNatGwVipState(gwName, applying)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"ip4.dst == 203.0.113.10": "203.0.113.10",
		"ip4.dst == 10.96.1.5":    "10.96.1.5",
	}, vips, "the rule being programmed must contribute its VIPs without waiting for the cache")
	require.Equal(t, []string{"10.96.1.5"}, clusterIPs)

	// The teardown direction: the cache still shows the rule as live, but the caller knows it is
	// terminating and its state has to go.
	live := ownedShareRule("rule-a", "eip0", "10.96.1.5")
	require.NoError(t, ruleIndexer.Add(live))
	terminating := live.DeepCopy()
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	terminating.Finalizers = []string{"keep"}

	vips, clusterIPs, err = controller.desiredNatGwVipState(gwName, terminating)
	require.NoError(t, err)
	require.Empty(t, vips, "a rule the caller is tearing down must not keep its route alive")
	require.Empty(t, clusterIPs)

	// It replaces that one rule only: a sibling of the same Service keeps the shared VIPs.
	require.NoError(t, ruleIndexer.Add(ownedShareRule("rule-b", "eip0", "10.96.1.5")))
	vips, clusterIPs, err = controller.desiredNatGwVipState(gwName, terminating)
	require.NoError(t, err)
	require.Len(t, vips, 2)
	require.Equal(t, []string{"10.96.1.5"}, clusterIPs)
}

// Test_natGwVipRouteNextHops_pendingInit pins that an instance which still needs initialization is
// not routed to. The vpc-nat-gw container has no readiness probe, so the kubelet reports it ready
// as soon as it runs, before the controller programmed its chains and identities.
func Test_natGwVipRouteNextHops_pendingInit(t *testing.T) {
	t.Parallel()

	gw := &kubeovnv1.VpcNatGateway{Name: "gw0", Spec: kubeovnv1.VpcNatGatewaySpec{Vpc: "vpc0", Subnet: "subnet0"}}
	pods := gatewayPodsOnNodes("gw0", map[string]string{"node0": "10.0.7.254", "node1": "10.0.7.253"})

	nextHops, err := natGwVipRouteNextHops(gw, pods)
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.7.253", "10.0.7.254"}, nextHops)

	// node0 sorts first, so pods[0] is its instance: it is running and ready, but the init mark of
	// its container instance is missing.
	delete(pods[0].Annotations, util.VpcNatGatewayInitAnnotation)
	nextHops, err = natGwVipRouteNextHops(gw, pods)
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.7.253"}, nextHops,
		"an instance that is not initialized yet has no share DNAT identity to serve the VIP")
}

// gatewayPods returns a ready, initialized NAT gateway instance as the VIP routes' next hop. The
// init annotation is what tells the controller the instance's chains and identities are programmed;
// without it the instance is not a next hop (see natGwVipRouteNextHops).
func gatewayPods(gwName, ip string) []*v1.Pod {
	return []*v1.Pod{{
		Namespace:   metav1.NamespaceSystem,
		Name:        util.GenNatGwPodName(gwName),
		Annotations: map[string]string{util.VpcNatGatewayInitAnnotation: "true"},
		Labels: map[string]string{
			"app":                       util.GenNatGwName(gwName),
			util.VpcNatGatewayLabel:     "true",
			util.VpcNatGatewayNameLabel: gwName,
		},
		Spec: v1.PodSpec{NodeName: "node0"},
		Status: v1.PodStatus{
			Phase:      v1.PodRunning,
			PodIPs:     []v1.PodIP{{IP: ip}},
			PodIP:      ip,
			Conditions: []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionTrue}},
		},
	}}
}

// gatewayPodsOnNodes returns one ready NAT gateway instance per node/LAN IP pair, so the VIP route
// next hop set covers more than one replica.
func gatewayPodsOnNodes(gwName string, nodeIPs map[string]string) []*v1.Pod {
	pods := make([]*v1.Pod, 0, len(nodeIPs))
	// Sorted, so a test that singles out one instance always picks the same one.
	for _, node := range slices.Sorted(maps.Keys(nodeIPs)) {
		ip := nodeIPs[node]
		pods = append(pods, &v1.Pod{
			Namespace:   metav1.NamespaceSystem,
			Name:        util.GenNatGwPodName(gwName) + "-" + node,
			Annotations: map[string]string{util.VpcNatGatewayInitAnnotation: "true"},
			Labels: map[string]string{
				"app":                       util.GenNatGwName(gwName),
				util.VpcNatGatewayLabel:     "true",
				util.VpcNatGatewayNameLabel: gwName,
			},
			Spec: v1.PodSpec{NodeName: node},
			Status: v1.PodStatus{
				Phase:      v1.PodRunning,
				PodIPs:     []v1.PodIP{{IP: ip}},
				PodIP:      ip,
				Conditions: []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionTrue}},
			},
		})
	}
	return pods
}

// ownedShareRule is a share DNAT rule generated by the nftable LB service feature for a Service
// whose ClusterIP is recorded on the rule.
func ownedShareRule(name, eip, clusterIP string) *kubeovnv1.IptablesDnatRule {
	rule := &kubeovnv1.IptablesDnatRule{
		Name: name,
		Labels: map[string]string{
			util.NftableLbSvcNsLabel:    "default",
			util.NftableLbSvcNameLabel:  "web",
			util.VpcNatGatewayNameLabel: "gw0",
		},
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			EIP: eip, ClusterIP: clusterIP, ExternalPort: "80", Protocol: "tcp",
			InternalIP: "10.0.7.2", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
		},
	}
	return rule
}

func Test_syncNatGwVipState(t *testing.T) {
	t.Parallel()

	gw := &kubeovnv1.VpcNatGateway{
		Name: "gw0",
		Spec: kubeovnv1.VpcNatGatewaySpec{Vpc: "vpc0"},
	}
	eip := &kubeovnv1.IptablesEIP{
		Name:   "eip0",
		Status: kubeovnv1.IptablesEIPStatus{IP: "172.20.0.5"},
	}
	externalIDs := natGwVipRouteExternalIDs("gw0")

	t.Run("creates a route per VIP", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			VpcNatGateways:    []*kubeovnv1.VpcNatGateway{gw},
			Pods:              gatewayPods("gw0", "10.0.7.254"),
			IptablesEips:      []*kubeovnv1.IptablesEIP{eip},
			IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{ownedShareRule("rule-a", "eip0", "10.96.1.5")},
		})
		require.NoError(t, err)
		c := fc.fakeController
		c.config.EnableGwNftableLbSvc = true

		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).Return(nil, nil)
		added := map[string][]string{}
		fc.mockOvnClient.EXPECT().AddLogicalRouterPolicy("vpc0", util.NatGatewayVipPolicyPriority, gomock.Any(), string(kubeovnv1.PolicyRouteActionReroute), gomock.Any(), nil, externalIDs).
			DoAndReturn(func(_ string, _ int, match, _ string, nextHops, _ []string, _ map[string]string) error {
				added[match] = nextHops
				return nil
			}).Times(2)

		require.NoError(t, c.syncNatGwVipState("gw0", nil))
		require.Equal(t, map[string][]string{
			"ip4.dst == 172.20.0.5": {"10.0.7.254"},
			"ip4.dst == 10.96.1.5":  {"10.0.7.254"},
		}, added)
	})

	t.Run("removes routes that no rule references and refreshes the next hops", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			VpcNatGateways:    []*kubeovnv1.VpcNatGateway{gw},
			Pods:              gatewayPods("gw0", "10.0.7.254"),
			IptablesEips:      []*kubeovnv1.IptablesEIP{eip},
			IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{ownedShareRule("rule-a", "eip0", "10.96.1.5")},
		})
		require.NoError(t, err)
		c := fc.fakeController
		c.config.EnableGwNftableLbSvc = true

		stale := &ovnnb.LogicalRouterPolicy{UUID: "stale-uuid", Priority: util.NatGatewayVipPolicyPriority, Match: "ip4.dst == 10.96.1.9"}
		drifted := &ovnnb.LogicalRouterPolicy{
			UUID: "drifted-uuid", Priority: util.NatGatewayVipPolicyPriority, Match: "ip4.dst == 10.96.1.5",
			Action: string(kubeovnv1.PolicyRouteActionReroute), Nexthops: []string{"10.0.7.9"},
		}
		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).
			Return([]*ovnnb.LogicalRouterPolicy{stale, drifted}, nil)
		fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicyByUUID("vpc0", "stale-uuid").Return(nil)
		updated := map[string][]string{}
		fc.mockOvnClient.EXPECT().UpdateLogicalRouterPolicy(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(policy *ovnnb.LogicalRouterPolicy, _ ...any) error {
				updated[policy.Match] = policy.Nexthops
				return nil
			}).Times(1)
		// the EIP's route did not exist yet
		fc.mockOvnClient.EXPECT().AddLogicalRouterPolicy("vpc0", util.NatGatewayVipPolicyPriority, "ip4.dst == 172.20.0.5",
			string(kubeovnv1.PolicyRouteActionReroute), []string{"10.0.7.254"}, nil, externalIDs).Return(nil)

		require.NoError(t, c.syncNatGwVipState("gw0", nil))
		require.Equal(t, map[string][]string{"ip4.dst == 10.96.1.5": {"10.0.7.254"}}, updated)
	})

	t.Run("routes a VIP to every ready instance (ECMP)", func(t *testing.T) {
		// An HA gateway serves the VIP from any replica, so the route must carry all of them.
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
			Pods: gatewayPodsOnNodes("gw0", map[string]string{
				"node0": "10.0.7.254",
				"node1": "10.0.7.253",
			}),
			IptablesEips:      []*kubeovnv1.IptablesEIP{eip},
			IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{ownedShareRule("rule-a", "eip0", "10.96.1.5")},
		})
		require.NoError(t, err)
		c := fc.fakeController
		c.config.EnableGwNftableLbSvc = true

		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).Return(nil, nil)
		added := map[string][]string{}
		fc.mockOvnClient.EXPECT().AddLogicalRouterPolicy("vpc0", util.NatGatewayVipPolicyPriority, gomock.Any(), string(kubeovnv1.PolicyRouteActionReroute), gomock.Any(), nil, externalIDs).
			DoAndReturn(func(_ string, _ int, match, _ string, nextHops, _ []string, _ map[string]string) error {
				added[match] = nextHops
				return nil
			}).Times(2)

		require.NoError(t, c.syncNatGwVipState("gw0", nil))
		require.Equal(t, map[string][]string{
			"ip4.dst == 172.20.0.5": {"10.0.7.253", "10.0.7.254"},
			"ip4.dst == 10.96.1.5":  {"10.0.7.253", "10.0.7.254"},
		}, added)
	})

	t.Run("routes a VIP only to the ready instances", func(t *testing.T) {
		pods := gatewayPodsOnNodes("gw0", map[string]string{
			"node0": "10.0.7.254",
			"node1": "10.0.7.253",
		})
		pods[1].Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			VpcNatGateways:    []*kubeovnv1.VpcNatGateway{gw},
			Pods:              pods,
			IptablesEips:      []*kubeovnv1.IptablesEIP{eip},
			IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{ownedShareRule("rule-a", "eip0", "10.96.1.5")},
		})
		require.NoError(t, err)
		c := fc.fakeController
		c.config.EnableGwNftableLbSvc = true

		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).Return(nil, nil)
		added := map[string][]string{}
		fc.mockOvnClient.EXPECT().AddLogicalRouterPolicy("vpc0", util.NatGatewayVipPolicyPriority, gomock.Any(), string(kubeovnv1.PolicyRouteActionReroute), gomock.Any(), nil, externalIDs).
			DoAndReturn(func(_ string, _ int, match, _ string, nextHops, _ []string, _ map[string]string) error {
				added[match] = nextHops
				return nil
			}).Times(2)

		require.NoError(t, c.syncNatGwVipState("gw0", nil))
		ready := added["ip4.dst == 10.96.1.5"]
		require.Equal(t, []string{"10.0.7.254"}, ready, "a not ready replica must not be a next hop")
	})

	t.Run("drops every route while no instance has an IPv4 address", func(t *testing.T) {
		// a gateway whose VPC-side address is IPv6 only has no data plane for the IPv4 share DNAT,
		// so an ip4.dst route must not be left pointing at its IPv6 address
		v6Only := gatewayPods("gw0", "fd00::10")
		v6Only[0].Status.PodIPs = []v1.PodIP{{IP: "fd00::10"}}
		v6Only[0].Status.PodIP = "fd00::10"
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			VpcNatGateways:    []*kubeovnv1.VpcNatGateway{gw},
			Pods:              v6Only,
			IptablesEips:      []*kubeovnv1.IptablesEIP{eip},
			IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{ownedShareRule("rule-a", "eip0", "10.96.1.5")},
		})
		require.NoError(t, err)
		c := fc.fakeController
		c.config.EnableGwNftableLbSvc = true

		existing := &ovnnb.LogicalRouterPolicy{UUID: "u1", Priority: util.NatGatewayVipPolicyPriority, Match: "ip4.dst == 10.96.1.5"}
		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).
			Return([]*ovnnb.LogicalRouterPolicy{existing}, nil)
		fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicyByUUID("vpc0", "u1").Return(nil)

		require.NoError(t, c.syncNatGwVipState("gw0", nil))
	})

	t.Run("drops every route while no instance is reachable", func(t *testing.T) {
		notReady := gatewayPods("gw0", "10.0.7.254")
		notReady[0].Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			VpcNatGateways:    []*kubeovnv1.VpcNatGateway{gw},
			Pods:              notReady,
			IptablesEips:      []*kubeovnv1.IptablesEIP{eip},
			IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{ownedShareRule("rule-a", "eip0", "10.96.1.5")},
		})
		require.NoError(t, err)
		c := fc.fakeController
		c.config.EnableGwNftableLbSvc = true

		existing := &ovnnb.LogicalRouterPolicy{UUID: "u1", Priority: util.NatGatewayVipPolicyPriority, Match: "ip4.dst == 10.96.1.5"}
		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).
			Return([]*ovnnb.LogicalRouterPolicy{existing}, nil)
		fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicyByUUID("vpc0", "u1").Return(nil)

		require.NoError(t, c.syncNatGwVipState("gw0", nil))
	})
}

// Test_syncNatGwVipStateDisabled pins the feature gate: with --enable-gw-nftable-lb-svc off the
// feature owns no VIP, so the sync must not touch OVN at all (the mock client fails any call).
func Test_syncNatGwVipStateDisabled(t *testing.T) {
	t.Parallel()

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways:    []*kubeovnv1.VpcNatGateway{{Name: "gw0", Spec: kubeovnv1.VpcNatGatewaySpec{Vpc: "vpc0", Subnet: "subnet0"}}},
		Pods:              gatewayPods("gw0", "10.0.7.254"),
		IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{ownedShareRule("rule-a", "eip0", "10.96.1.5")},
	})
	require.NoError(t, err)
	fc.fakeController.config.EnableGwNftableLbSvc = false

	require.NoError(t, fc.fakeController.syncNatGwVipState("gw0", nil))
}

// TestHandleAddOrUpdateVpcNatGwSyncsVipRoutes pins the gateway side convergence of the share DNAT
// VIP routes: an HA scale down removes an instance without creating a new one, so no DNAT rule is
// rewritten and the only chance to drop the gone instance from the routes is the gateway reconcile.
func TestHandleAddOrUpdateVpcNatGwSyncsVipRoutes(t *testing.T) {
	const (
		gwName     = "sync-gw"
		subnetName = "nat-subnet"
		eipName    = "sync-eip"
		vpcName    = util.DefaultVpc
		liveIP     = "10.20.0.10"
		goneIP     = "10.20.0.11"
		clusterIP  = "10.96.1.5"
	)
	namespace := metav1.NamespaceSystem
	stsName := util.GenNatGwName(gwName)
	gwLabels := util.GenNatGwLabels(gwName)
	gw := &kubeovnv1.VpcNatGateway{
		Name: gwName, UID: "gw-uid",
		Spec: kubeovnv1.VpcNatGatewaySpec{Vpc: vpcName, Subnet: subnetName, Replicas: 1},
	}
	sts := &appsv1.StatefulSet{
		Name: stsName, Namespace: namespace, UID: "sts-uid",
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: gwLabels},
			Template: v1.PodTemplateSpec{Labels: gwLabels, Annotations: map[string]string{util.VpcNatGatewayAnnotation: gwName}},
		},
	}
	pod := &v1.Pod{
		Name: stsName + "-0", Namespace: namespace, Labels: gwLabels,
		Annotations: map[string]string{
			util.IPAddressAnnotation:         liveIP,
			util.VpcNatGatewayInitAnnotation: "true",
			util.VpcNatGatewayAnnotation:     gwName,
		},
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(appsv1.SchemeGroupVersion.String(), util.KindStatefulSet, stsName, sts.UID)},
		Spec:            v1.PodSpec{NodeName: "node-1"},
		Status: v1.PodStatus{
			Phase:      v1.PodRunning,
			PodIPs:     []v1.PodIP{{IP: liveIP}},
			Conditions: []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionTrue}},
		},
	}
	eip := &kubeovnv1.IptablesEIP{
		Name:   eipName,
		Spec:   kubeovnv1.IptablesEIPSpec{NatGwDp: gwName, V4ip: "172.20.0.5"},
		Status: kubeovnv1.IptablesEIPStatus{IP: "172.20.0.5"},
	}
	rule := &kubeovnv1.IptablesDnatRule{
		Name: "sync-rule",
		Labels: map[string]string{
			util.NftableLbSvcNsLabel:    "default",
			util.NftableLbSvcNameLabel:  "web",
			util.VpcNatGatewayNameLabel: gwName,
		},
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			EIP: eipName, ClusterIP: clusterIP, VpcNatGwDp: gwName,
			ExternalPort: "80", Protocol: "tcp",
			InternalIP: "10.0.7.2", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
		},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs:           []*kubeovnv1.Vpc{{Name: vpcName}},
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
		Subnets: []*kubeovnv1.Subnet{
			{
				Name: subnetName,
				Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolIPv4, Vpc: vpcName, CIDRBlock: "10.20.0.0/24", Gateway: "10.20.0.1"},
			},
			{
				Name: "ovn-vpc-external-network",
				Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolIPv4, Gateway: "192.168.0.1"},
			},
		},
		StatefulSets:      []*appsv1.StatefulSet{sts},
		Pods:              []*v1.Pod{pod},
		IptablesEips:      []*kubeovnv1.IptablesEIP{eip},
		IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{rule},
	})
	require.NoError(t, err)
	controller := fakeController.fakeController
	controller.config.EnableGwNftableLbSvc = true
	controller.config.EnableOvnLB = true

	vpcNatEnabled = "true"
	t.Cleanup(func() { vpcNatEnabled = "unknown" })
	controller.serviceCIDRStore = util.NewServiceCIDRStore("10.96.0.0/12")

	// The route exists from an earlier reconcile and still points at an instance that is gone.
	existing := &ovnnb.LogicalRouterPolicy{
		UUID: "vip-route-uid", Priority: util.NatGatewayVipPolicyPriority,
		Match:       "ip4.dst == " + clusterIP,
		Action:      string(kubeovnv1.PolicyRouteActionReroute),
		Nexthops:    []string{goneIP},
		ExternalIDs: natGwVipRouteExternalIDs(gwName),
	}
	fakeController.mockOvnClient.EXPECT().
		ListLogicalRouterPolicies(vpcName, util.NatGatewayVipPolicyPriority, natGwVipRouteExternalIDs(gwName), false).
		Return([]*ovnnb.LogicalRouterPolicy{existing}, nil).Times(1)
	var updatedNexthops [][]string
	fakeController.mockOvnClient.EXPECT().UpdateLogicalRouterPolicy(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(policy *ovnnb.LogicalRouterPolicy, _ ...any) error {
			updatedNexthops = append(updatedNexthops, policy.Nexthops)
			return nil
		}).Times(1)
	// The EIP of the same rule has no route yet: it is added with the same live next hop.
	fakeController.mockOvnClient.EXPECT().
		AddLogicalRouterPolicy(vpcName, util.NatGatewayVipPolicyPriority, "ip4.dst == 172.20.0.5",
			string(kubeovnv1.PolicyRouteActionReroute), []string{liveIP}, gomock.Any(), natGwVipRouteExternalIDs(gwName)).
		Return(nil).Times(1)

	require.NoError(t, controller.handleAddOrUpdateVpcNatGw(gwName))
	require.Equal(t, [][]string{{liveIP}}, updatedNexthops,
		"the gateway reconcile must drop an instance that is no longer running from the VIP route")
}

// TestHandleUpdateEndpointSliceSkipsOwnedVips pins the OVN load balancer side of the ownership rule:
// for a Service the gateway serves, the handler releases the VIP instead of programming it.
// nftableLbSvcOwnershipFixture is the shared setup of the ownership tests: one Service handled by
// the nftable LB service feature, its EIP, the gateway serving it, and the VPC load balancer that
// must not program its VIPs while the gateway owns them.
type nftableLbSvcOwnershipFixture struct {
	svc       *v1.Service
	slice     *discoveryv1.EndpointSlice
	eip       *kubeovnv1.IptablesEIP
	gw        *kubeovnv1.VpcNatGateway
	vpc       *kubeovnv1.Vpc
	subnet    *kubeovnv1.Subnet
	namespace string
}

func newNftableLbSvcOwnershipFixture() *nftableLbSvcOwnershipFixture {
	const (
		gwName     = "owned-gw"
		subnetName = "owned-subnet"
		eipName    = "owned-eip"
		vpcName    = util.DefaultVpc
		svcName    = "owned-web"
		clusterIP  = "10.96.1.5"
	)
	namespace := metav1.NamespaceDefault
	return &nftableLbSvcOwnershipFixture{
		namespace: namespace,
		gw: &kubeovnv1.VpcNatGateway{
			Name: gwName, UID: "gw-uid",
			Spec: kubeovnv1.VpcNatGatewaySpec{Vpc: vpcName, Subnet: subnetName},
		},
		eip: &kubeovnv1.IptablesEIP{
			Name:   eipName,
			Spec:   kubeovnv1.IptablesEIPSpec{NatGwDp: gwName, V4ip: "172.20.0.5"},
			Status: kubeovnv1.IptablesEIPStatus{IP: "172.20.0.5"},
		},
		vpc: &kubeovnv1.Vpc{
			Name:   vpcName,
			Status: kubeovnv1.VpcStatus{TCPLoadBalancer: "vpc-tcp-load", TCPSessionLoadBalancer: "vpc-tcp-sess"},
		},
		subnet: &kubeovnv1.Subnet{
			Name: subnetName,
			Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolIPv4, Vpc: vpcName, CIDRBlock: "10.0.7.0/24", Gateway: "10.0.7.1"},
		},
		svc: &v1.Service{
			Namespace: namespace,
			Name:      svcName,
			Annotations: map[string]string{
				util.EipAnnotation:                 eipName,
				util.VpcNatGatewaySvcAnnotation:    gwName,
				util.NftableLbSvcManagedAnnotation: "true",
			},
			Spec: v1.ServiceSpec{
				Type:       v1.ServiceTypeLoadBalancer,
				ClusterIP:  clusterIP,
				ClusterIPs: []string{clusterIP},
				Ports:      []v1.ServicePort{{Name: "http", Port: 80, Protocol: v1.ProtocolTCP}},
			},
		},
		slice: &discoveryv1.EndpointSlice{
			Namespace: namespace, Name: svcName + "-abc",
			Labels:      map[string]string{discoveryv1.LabelServiceName: svcName},
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports:       []discoveryv1.EndpointPort{{Name: new("http"), Port: new(int32(8080)), Protocol: new(v1.ProtocolTCP)}},
			Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{"10.0.7.2"}, Conditions: discoveryv1.EndpointConditions{Ready: new(true)}}},
		},
	}
}

func Test_nftableLbDnatRulesReady(t *testing.T) {
	t.Parallel()

	want := ownedShareRule("rule-a", "eip0", "10.96.1.5")
	got := want.DeepCopy()
	got.Status.Ready = true
	require.True(t, nftableLbDnatRulesReady(map[string]*kubeovnv1.IptablesDnatRule{got.Name: want}, map[string]*kubeovnv1.IptablesDnatRule{got.Name: got}))
	got.Status.Ready = false
	require.False(t, nftableLbDnatRulesReady(map[string]*kubeovnv1.IptablesDnatRule{got.Name: want}, map[string]*kubeovnv1.IptablesDnatRule{got.Name: got}))
	require.False(t, nftableLbDnatRulesReady(nil, nil))
}

func TestNftableLbServiceWaitsForDnatReadyBeforeHandoff(t *testing.T) {
	f := newNftableLbSvcOwnershipFixture()
	delete(f.svc.Annotations, util.NftableLbSvcManagedAnnotation)
	desired := buildDesiredNftableLbDnatRules(f.svc, f.eip.Name, f.gw.Name, []*discoveryv1.EndpointSlice{f.slice}, testNftableLbBackendIP)
	require.Len(t, desired, 1)
	pending := slices.Collect(maps.Values(desired))[0]

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs:              []*kubeovnv1.Vpc{f.vpc},
		VpcNatGateways:    []*kubeovnv1.VpcNatGateway{f.gw},
		Subnets:           []*kubeovnv1.Subnet{f.subnet},
		Services:          []*v1.Service{f.svc},
		EndpointSlices:    []*discoveryv1.EndpointSlice{f.slice},
		IptablesEips:      []*kubeovnv1.IptablesEIP{f.eip},
		IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{pending},
	})
	require.NoError(t, err)
	c := fc.fakeController
	c.config.EnableGwNftableLbSvc = true
	fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies(gomock.Any(), util.NatGatewayVipPolicyPriority, gomock.Any(), false).Return(nil, nil)

	require.NoError(t, c.handleAddOrUpdateNftableLbService(f.namespace+"/"+f.svc.Name))
	updated, err := c.config.KubeClient.CoreV1().Services(f.namespace).Get(context.Background(), f.svc.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, updated.Status.LoadBalancer.Ingress)
	require.NotContains(t, updated.Annotations, util.NftableLbSvcManagedAnnotation)
}

func clusterIPServedRule(name, gateway, clusterIP string) *kubeovnv1.IptablesDnatRule {
	return &kubeovnv1.IptablesDnatRule{
		Name: name,
		Labels: map[string]string{
			util.VpcNatGatewayNameLabel: gateway,
			util.VpcDnatEPortLabel:      "80",
		},
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			ClusterIP: clusterIP, VpcNatGwDp: gateway,
			ExternalPort: "80", Protocol: "tcp",
			InternalIP: "10.0.7.2", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
		},
	}
}

// Test_desiredNatGwVipState_clusterIPServedRules pins that a rule serving a ClusterIP owns its
// address on its own: without an owner label it still contributes the VIP route and the address to
// hold on lo, otherwise the rule would program a share DNAT identity nothing routes to.
func Test_desiredNatGwVipState_clusterIPServedRules(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	ruleIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	eipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, eipIndexer.Add(&kubeovnv1.IptablesEIP{
		Name:   "eip0",
		Status: kubeovnv1.IptablesEIPStatus{IP: "203.0.113.10"},
	}))
	controller := &Controller{
		iptablesDnatRulesLister: kubeovnlister.NewIptablesDnatRuleLister(ruleIndexer),
		iptablesEipsLister:      kubeovnlister.NewIptablesEIPLister(eipIndexer),
	}
	require.NoError(t, ruleIndexer.Add(clusterIPServedRule("c1", gwName, "10.96.1.5")))
	require.NoError(t, ruleIndexer.Add(clusterIPServedRule("c2", gwName, "10.96.1.6")))

	vips, clusterIPs, err := controller.desiredNatGwVipState(gwName, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"ip4.dst == 10.96.1.5": "10.96.1.5",
		"ip4.dst == 10.96.1.6": "10.96.1.6",
	}, vips)
	require.Equal(t, []string{"10.96.1.5", "10.96.1.6"}, clusterIPs)

	// A terminating rule releases its address again.
	terminating := clusterIPServedRule("c1", gwName, "10.96.1.5")
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	terminating.Finalizers = []string{"keep"}
	vips, clusterIPs, err = controller.desiredNatGwVipState(gwName, terminating)
	require.NoError(t, err)
	require.NotContains(t, vips, "ip4.dst == 10.96.1.5")
	require.Equal(t, []string{"10.96.1.6"}, clusterIPs)

	// A hand-managed share rule contributes its own address too: with no ClusterIP it routes the
	// EIP's address alone, which is the same symmetry a rule serving only a ClusterIP has.
	manual := ownedShareRule("manual", "eip0", "")
	delete(manual.Labels, util.NftableLbSvcNameLabel)
	require.NoError(t, ruleIndexer.Add(manual))
	vips, clusterIPs, err = controller.desiredNatGwVipState(gwName, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"ip4.dst == 10.96.1.5":    "10.96.1.5",
		"ip4.dst == 10.96.1.6":    "10.96.1.6",
		"ip4.dst == 203.0.113.10": "203.0.113.10",
	}, vips, "the hand-managed EIP rule routes its own address, exactly like the ClusterIP ones")
	require.Equal(t, []string{"10.96.1.5", "10.96.1.6"}, clusterIPs,
		"only a rule that serves a ClusterIP holds one on lo")
	require.Equal(t, []string{"10.96.1.5", "10.96.1.6"}, clusterIPs)
}

// Test_sameDnatIdentity pins the identity comparison both flows share: the two address kinds can
// never be confused with each other, since a rule carries exactly one of them.
func Test_sameDnatIdentity(t *testing.T) {
	t.Parallel()

	eipA := &kubeovnv1.IptablesDnatRuleSpec{EIP: "eip-a"}
	eipB := &kubeovnv1.IptablesDnatRuleSpec{EIP: "eip-b"}
	// A Service-driven rule aligns the ingress IP and the ClusterIP on one identity.
	eipASvc := &kubeovnv1.IptablesDnatRuleSpec{EIP: "eip-a", ClusterIP: "10.96.1.5"}
	cipA := &kubeovnv1.IptablesDnatRuleSpec{ClusterIP: "10.96.1.5", VpcNatGwDp: "gw0"}
	cipA2 := &kubeovnv1.IptablesDnatRuleSpec{ClusterIP: "10.96.1.5", VpcNatGwDp: "gw1"}
	cipB := &kubeovnv1.IptablesDnatRuleSpec{ClusterIP: "10.96.1.6", VpcNatGwDp: "gw0"}

	require.True(t, sameDnatIdentity(eipA, eipA))
	require.False(t, sameDnatIdentity(eipA, eipB))
	require.True(t, sameDnatIdentity(eipA, eipASvc),
		"the EIP:port nft map is shared, so a rule that does not carry the ClusterIP still collides")
	require.False(t, sameDnatIdentity(eipA, cipA), "an EIP identity and a ClusterIP identity are distinct")
	require.True(t, sameDnatIdentity(cipA, cipA2), "a ClusterIP identity does not depend on the gateway")
	require.False(t, sameDnatIdentity(cipA, cipB))
}

// Test_buildDesiredNftableLbDnatRules_clusterIPService pins the rule shape of the two Service
// kinds: a ClusterIP Service is served through its internal VIP alone, a LoadBalancer Service
// through the EIP and that same VIP aligned on one rule.
func Test_buildDesiredNftableLbDnatRules_clusterIPService(t *testing.T) {
	t.Parallel()

	endpointSlices := []*discoveryv1.EndpointSlice{
		{
			Ports: []discoveryv1.EndpointPort{{Name: new("http"), Port: new(int32(8080))}},
			Endpoints: []discoveryv1.Endpoint{
				{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: new(true)}},
			},
		},
	}
	newSvc := func(svcType v1.ServiceType, eip string) *v1.Service {
		return &v1.Service{
			Namespace: "default", Name: "web",
			Annotations: map[string]string{
				util.VpcNatGatewaySvcAnnotation: "gw0",
				util.EipAnnotation:              eip,
			},
			Spec: v1.ServiceSpec{
				Type:       svcType,
				ClusterIP:  "10.96.1.5",
				ClusterIPs: []string{"10.96.1.5"},
				Ports:      []v1.ServicePort{{Name: "http", Port: 80, Protocol: v1.ProtocolTCP}},
			},
		}
	}

	// ClusterIP Service: no EIP at all, so the rule carries the internal VIP and the gateway.
	desired := buildDesiredNftableLbDnatRules(newSvc(v1.ServiceTypeClusterIP, ""), "", "gw0", endpointSlices, testNftableLbBackendIP)
	require.Len(t, desired, 1)
	for _, rule := range desired {
		require.Empty(t, rule.Spec.EIP, "a ClusterIP Service has no public address")
		require.Equal(t, "10.96.1.5", rule.Spec.ClusterIP)
		require.Equal(t, "gw0", rule.Spec.VpcNatGwDp, "without an EIP the gateway cannot be derived")
		require.Equal(t, kubeovnv1.DnatRuleTypeShare, rule.Spec.Type)
	}

	// LoadBalancer Service: both addresses on the same rule, so the two VIPs of one Service port
	// stay aligned and are programmed and released together.
	desired = buildDesiredNftableLbDnatRules(newSvc(v1.ServiceTypeLoadBalancer, "eip0"), "eip0", "gw0", endpointSlices, testNftableLbBackendIP)
	require.Len(t, desired, 1)
	for _, rule := range desired {
		require.Equal(t, "eip0", rule.Spec.EIP)
		require.Equal(t, "10.96.1.5", rule.Spec.ClusterIP)
		require.Equal(t, "gw0", rule.Spec.VpcNatGwDp)
	}
}

// Test_handleAddOrUpdateNftableLbService_clusterIPService pins the ClusterIP Service reconcile:
// the gateway comes from the annotation, no EIP is looked up, and the generated rule is the one
// that serves the internal VIP.
func Test_handleAddOrUpdateNftableLbService_clusterIPService(t *testing.T) {
	const (
		gwName     = "cip-gw"
		subnetName = "cip-subnet"
		vpcName    = util.DefaultVpc
		svcName    = "cip-web"
		clusterIP  = "10.96.1.9"
	)
	namespace := metav1.NamespaceDefault
	gw := &kubeovnv1.VpcNatGateway{
		Name: gwName, UID: "gw-uid",
		Spec: kubeovnv1.VpcNatGatewaySpec{Vpc: vpcName, Subnet: subnetName},
	}
	svc := &v1.Service{
		Namespace: namespace, Name: svcName,
		Annotations: map[string]string{util.VpcNatGatewaySvcAnnotation: gwName},
		Spec: v1.ServiceSpec{
			Type:       v1.ServiceTypeClusterIP,
			ClusterIP:  clusterIP,
			ClusterIPs: []string{clusterIP},
			Ports:      []v1.ServicePort{{Name: "http", Port: 80, Protocol: v1.ProtocolTCP}},
		},
	}
	slice := &discoveryv1.EndpointSlice{
		Namespace: namespace, Name: svcName + "-abc",
		Labels:      map[string]string{discoveryv1.LabelServiceName: svcName},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports:       []discoveryv1.EndpointPort{{Name: new("http"), Port: new(int32(8080)), Protocol: new(v1.ProtocolTCP)}},
		Endpoints: []discoveryv1.Endpoint{{
			Addresses:  []string{"10.0.0.7"},
			Conditions: discoveryv1.EndpointConditions{Ready: new(true)},
		}},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs:           []*kubeovnv1.Vpc{{Name: vpcName, Status: kubeovnv1.VpcStatus{TCPLoadBalancer: "vpc-tcp-load"}}},
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
		Subnets: []*kubeovnv1.Subnet{{
			Name: subnetName,
			Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolIPv4, Vpc: vpcName, CIDRBlock: "10.0.0.0/24", Gateway: "10.0.0.1"},
		}},
		Services:       []*v1.Service{svc},
		EndpointSlices: []*discoveryv1.EndpointSlice{slice},
	})
	require.NoError(t, err)
	c := fc.fakeController
	c.config.EnableGwNftableLbSvc = true

	// The reconcile ends by syncing the VIP state of the gateway: with no running instance there is
	// no next hop, so no route is programmed (the address on lo is synced on the rule paths).
	fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies(gomock.Any(), util.NatGatewayVipPolicyPriority, gomock.Any(), false).
		Return(nil, nil).Times(1)

	require.NoError(t, c.handleAddOrUpdateNftableLbService(namespace+"/"+svcName))

	// Read the rules back through the API: the lister is fed by an informer, so it may not have
	// observed the creation yet.
	rules, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().List(context.Background(),
		metav1.ListOptions{LabelSelector: util.NftableLbSvcNameLabel + "=" + svcName})
	require.NoError(t, err)
	require.Len(t, rules.Items, 1, "one share DNAT rule per servicePort and ready backend")
	rule := rules.Items[0]
	require.Equal(t, clusterIP, rule.Spec.ClusterIP, "the internal VIP the gateway has to serve")
	require.Equal(t, gwName, rule.Spec.VpcNatGwDp, "the gateway named by the Service")
	require.Empty(t, rule.Spec.EIP, "a ClusterIP Service has no ingress IP to publish")
	require.Equal(t, "10.0.0.7:8080", rule.Spec.InternalIP+":"+rule.Spec.InternalPort)
	require.Equal(t, "80", rule.Spec.ExternalPort)
	require.Equal(t, "tcp", rule.Spec.Protocol)

	// No ingress IP is published for a ClusterIP Service: it has no external address, and a stale
	// one from an earlier incarnation has to be released.
	updated, err := fc.fakeController.config.KubeClient.CoreV1().Services(namespace).Get(context.Background(), svcName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, updated.Status.LoadBalancer.Ingress)
}

// Test_shareDnatHairpinRules pins the hairpin arguments of the two VIP shapes: a LoadBalancer
// Service port has two addresses (its ingress IP and its ClusterIP) and needs a rule for each,
// while a ClusterIP Service passes the same address twice and must still get one.
func Test_shareDnatHairpinRules(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		[]string{"203.0.113.10,80,tcp", "10.96.1.5,80,tcp"},
		shareDnatHairpinRules("tcp", "80", "203.0.113.10", "10.96.1.5"))

	require.Equal(t,
		[]string{"10.96.1.5,80,tcp"},
		shareDnatHairpinRules("tcp", "80", "10.96.1.5", "10.96.1.5"),
		"a ClusterIP-only rule serves one address, not the same rule twice")

	require.Empty(t, shareDnatHairpinRules("tcp", "80", "", ""))
}

// Test_shareDnatIdentityCoverage pins what applyShareDnatIdentity and deleteShareDnatIdentity have
// to program for each rule shape. The internal VIP of a rule that only serves a ClusterIP is also
// that rule's own address, which must not make the function skip its hairpin rule and the address
// set: without them the VIP is programmed but nothing holds or routes it.
func Test_shareDnatIdentityCoverage(t *testing.T) {
	t.Parallel()

	// The addresses the VIP state is derived from, per rule shape.
	clusterIPServed := clusterIPServedRule("c1", "gw0", "10.96.1.5")
	lbServed := ownedShareRule("lb1", "eip0", "10.96.1.5")

	// A rule that serves only a ClusterIP has one identity: it must not be passed twice.
	require.Equal(t, "10.96.1.5", clusterIPServed.Spec.ClusterIP)
	require.Equal(t, []string{"10.96.1.5,80,tcp"},
		shareDnatHairpinRules("tcp", "80", clusterIPServed.Spec.ClusterIP, clusterIPServed.Spec.ClusterIP),
		"the single identity of a ClusterIP-only rule still gets its hairpin rule")

	// A LoadBalancer rule has two: the ingress IP it is programmed with and the internal VIP.
	require.Equal(t, "10.96.1.5", lbServed.Spec.ClusterIP)
	require.Equal(t, []string{"203.0.113.10,80,tcp", "10.96.1.5,80,tcp"},
		shareDnatHairpinRules("tcp", "80", "203.0.113.10", lbServed.Spec.ClusterIP),
		"a LoadBalancer rule has two addresses and gets a rule for each")

	// A hand-managed rule that serves an EIP only has none.
	manual := ownedShareRule("manual", "eip0", "")
	require.Empty(t, manual.Spec.ClusterIP)
}

// Test_syncNatGwVipAddrsGate pins that the feature switch stops the feature-owned VIP state before
// it reaches the gateway: with --enable-gw-nftable-lb-svc off, a pass over a Pod must not try to
// execute anything there (a disabled feature must not need a gateway to answer). The identity of a
// rule is not gated this way: that is the share DNAT data plane itself, which also serves
// hand-managed rules (see applyShareDnatIdentity, whose identity part runs before this call).
//
// Test_applyShareDnatVipStateGate pins the same for the other half of that state, the per-identity
// hairpin SNAT rule, which the delete path gates identically: programming one and not releasing it
// would leave a rule in the gateway that no rule accounts for any more.
func Test_syncNatGwVipAddrsGate(t *testing.T) {
	t.Parallel()

	ruleIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, ruleIndexer.Add(clusterIPServedRule("c1", "gw0", "10.96.1.5")))
	pod := &v1.Pod{Namespace: "kube-system", Name: "gw0-0"}
	controller := &Controller{
		config:                  &Configuration{EnableGwNftableLbSvc: false},
		iptablesDnatRulesLister: kubeovnlister.NewIptablesDnatRuleLister(ruleIndexer),
		iptablesEipsLister:      kubeovnlister.NewIptablesEIPLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
		natGwVipKeyMutex:        keymutex.NewHashed(1),
	}

	// No kube client and no exec are wired: reaching the gateway here would panic, so a clean
	// return is what proves the gate.
	require.NoError(t, controller.syncNatGwVipAddrs([]*v1.Pod{pod}, "gw0", nil))

	controller.config.EnableGwNftableLbSvc = true
	require.True(t, controller.gwNftableLbSvcEnabled())
	require.Panics(t, func() { _ = controller.syncNatGwVipAddrs([]*v1.Pod{pod}, "gw0", nil) },
		"with the feature on the same call does reach the data plane")
}

// Test_applyShareDnatVipStateGate pins that the hairpin SNAT rule of an identity is gated by the
// feature switch exactly like the address set (and like its deletion, see deleteShareDnatIdentity):
// a disabled feature must not add a rule that the delete path would refuse to remove.
func Test_applyShareDnatVipStateGate(t *testing.T) {
	t.Parallel()

	rule := clusterIPServedRule("c1", "gw0", "10.96.1.5")
	pod := &v1.Pod{Namespace: "kube-system", Name: "gw0-0"}
	controller := &Controller{
		config:           &Configuration{EnableGwNftableLbSvc: false},
		natGwVipKeyMutex: keymutex.NewHashed(1),
	}

	// No kube client is wired: reaching the gateway here would panic, so a clean return proves the
	// gate covers the hairpin rule too.
	require.NoError(t, controller.applyShareDnatVipState([]*v1.Pod{pod}, rule, "gw0", "tcp", "10.96.1.5", "80", "10.96.1.5"))

	controller.config.EnableGwNftableLbSvc = true
	require.Panics(t, func() {
		_ = controller.applyShareDnatVipState([]*v1.Pod{pod}, rule, "gw0", "tcp", "10.96.1.5", "80", "10.96.1.5")
	},
		"with the feature on the same call does reach the data plane")
}

// Test_desiredNatGwVipStateGatewayArgument pins the mechanism behind the resolved gateway having to
// be carried into the VIP state: the address set belongs to a gateway, so an EIP-only rule (which
// does not spell its gateway out, it gets it from the EIP) still has to be aggregated under that
// gateway together with its siblings. Looking the set up under the empty name the rule spells out
// returns nothing, and an empty set is not a no-op: vip-addr-sync would drop every labeled address
// the real gateway holds, taking the ClusterIPs of other Services with it.
//
// The call site that passes the resolved gateway (applyShareDnatIdentity -> applyShareDnatVipState
// -> syncNatGwVipAddrs) is not asserted here: the address set is executed in the gateway, whose exec
// boundary is not fakeable in this harness. It is covered end to end instead, by the ClusterIP
// scenario creating a hand-managed EIP-only share rule on the same gateway and asserting the
// internal VIP is still held afterwards.
func Test_desiredNatGwVipStateGatewayArgument(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	ruleIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	eipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, eipIndexer.Add(&kubeovnv1.IptablesEIP{
		Name:   "eip0",
		Spec:   kubeovnv1.IptablesEIPSpec{NatGwDp: gwName, V4ip: "203.0.113.10"},
		Status: kubeovnv1.IptablesEIPStatus{IP: "203.0.113.10"},
	}))
	controller := &Controller{
		iptablesDnatRulesLister: kubeovnlister.NewIptablesDnatRuleLister(ruleIndexer),
		iptablesEipsLister:      kubeovnlister.NewIptablesEIPLister(eipIndexer),
	}
	// a Service rule already served by the gateway, and a traditional EIP-only share rule: it
	// references its EIP but leaves vpcNatGwDp empty, which is what the webhook allows for an EIP
	// rule (the gateway comes from the EIP).
	require.NoError(t, ruleIndexer.Add(clusterIPServedRule("svc-rule", gwName, "10.96.1.5")))
	eipOnly := &kubeovnv1.IptablesDnatRule{
		Name: "eip-only-rule",
		Labels: map[string]string{
			util.VpcNatGatewayNameLabel: gwName,
			util.VpcDnatEPortLabel:      "8080",
		},
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			EIP: "eip0", ExternalPort: "8080", Protocol: "tcp",
			InternalIP: "10.0.7.2", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
		},
	}
	require.Empty(t, eipOnly.Spec.VpcNatGwDp, "an EIP rule may leave its gateway to the EIP")
	require.Empty(t, eipOnly.Spec.ClusterIP)
	require.NoError(t, ruleIndexer.Add(eipOnly))

	// The gateway derived from the EIP is the one whose set has to be reconciled: it holds the
	// sibling ClusterIP and it is where the rule itself is programmed.
	vips, clusterIPs, err := controller.desiredNatGwVipState(gwName, eipOnly)
	require.NoError(t, err)
	require.Equal(t, []string{"10.96.1.5"}, clusterIPs,
		"the set of the resolved gateway covers its other rules")
	require.Equal(t, "203.0.113.10", vips["ip4.dst == 203.0.113.10"],
		"the rule contributes the address of the EIP it references")

	_, clusterIPs, err = controller.desiredNatGwVipState(eipOnly.Spec.VpcNatGwDp, eipOnly)
	require.NoError(t, err)
	require.Empty(t, clusterIPs,
		"looking the set up under the empty name the rule spells out finds nothing, which is the bug")
}

// TestClearNftableLbSvcIngressIP clears status before dropping the managed marker. The informer
// therefore observes an ingress change while it can still identify this controller as its writer.
func TestClearNftableLbSvcIngressIP(t *testing.T) {
	t.Parallel()

	svc := &v1.Service{
		Namespace: metav1.NamespaceDefault,
		Name:      "web",
		Annotations: map[string]string{
			util.NftableLbSvcManagedAnnotation: "true",
		},
		Status: v1.ServiceStatus{LoadBalancer: v1.LoadBalancerStatus{
			Ingress: []v1.LoadBalancerIngress{{IP: "203.0.113.10"}},
		}},
	}
	client := k8sfake.NewSimpleClientset(svc)
	c := &Controller{config: &Configuration{KubeClient: client}}

	require.NoError(t, c.clearNftableLbSvcIngressIP(svc))
	actions := client.Actions()
	require.Len(t, actions, 2)
	require.Equal(t, "status", actions[0].GetSubresource())
	require.Empty(t, actions[1].GetSubresource())
	updated, err := client.CoreV1().Services(svc.Namespace).Get(context.Background(), svc.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, updated.Status.LoadBalancer.Ingress)
	require.NotContains(t, updated.Annotations, util.NftableLbSvcManagedAnnotation)
}

// Test_shareClusterIPBackends pins the decision the teardown of one share rule makes for the internal
// VIP it serves: the siblings of the ClusterIP identity are the live rules of the gateway serving
// that same address, which is not the same set as the siblings of the EIP identity (sameDnatIdentity
// compares the EIP whenever either rule has one). A rule that is the only one serving its ClusterIP
// therefore gets an empty result, which is what makes the caller drop the address instead of
// rebuilding it with the backends of a rule that serves another one.
func Test_shareClusterIPBackends(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	handManaged := func(name, eip, clusterIP, backend string) *kubeovnv1.IptablesDnatRule {
		return &kubeovnv1.IptablesDnatRule{
			Name: name,
			Labels: map[string]string{
				util.VpcNatGatewayNameLabel: gwName,
				util.VpcDnatEPortLabel:      "80",
			},
			Spec: kubeovnv1.IptablesDnatRuleSpec{
				EIP: eip, ClusterIP: clusterIP, ExternalPort: "80", Protocol: "tcp",
				InternalIP: backend, InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
			},
		}
	}
	// Two hand-managed rules sharing one EIP: each serves its own internal VIP.
	a := handManaged("a", "eip0", "10.96.1.5", "10.0.7.2")
	b := handManaged("b", "eip0", "10.96.1.6", "10.0.7.3")
	// Two rules of one Service port: both serve the same internal VIP.
	svcRule := handManaged("svc-1", "eip0", "10.96.1.7", "10.0.7.4")
	svcRule2 := handManaged("svc-2", "eip0", "10.96.1.7", "10.0.7.5")

	c := dnatListerController(t, a, b, svcRule, svcRule2)

	// The EIP identity aggregates both hand-managed rules, which is why the ClusterIP one cannot.
	eipBackends, _, _, err := c.getShareBackends(gwName, a, "80", "tcp")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"10.0.7.3:8080", "10.0.7.5:8080", "10.0.7.4:8080"}, eipBackends)

	// Its own internal VIP is served by nobody else, so the address has to go.
	backends, _, _, err := c.shareClusterIPBackends(gwName, a, "80", "tcp", "10.96.1.5")
	require.NoError(t, err)
	require.Empty(t, backends, "no other rule serves the internal VIP of the deleted rule")

	// The internal VIP of a Service port keeps the surviving backend of that Service.
	backends, affinity, _, err := c.shareClusterIPBackends(gwName, svcRule, "80", "tcp", "10.96.1.7")
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.7.5:8080"}, backends)
	require.Empty(t, affinity)
}

// Test_clusterIPIdentityBackends pins what the internal VIP of one rule is programmed with: the live
// rules serving that same address, plus the rule itself. It must not be the siblings of the EIP
// identity (sameDnatIdentity compares the EIP whenever either rule has one), because two hand-managed
// rules can share an EIP:port while serving different internal VIPs -- reusing that aggregation would
// repoint one address at the other's backends, and the teardown would then disagree with it.
func Test_clusterIPIdentityBackends(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	rule := func(name, eip, clusterIP, backend string) *kubeovnv1.IptablesDnatRule {
		return &kubeovnv1.IptablesDnatRule{
			Name: name,
			Labels: map[string]string{
				util.VpcNatGatewayNameLabel: gwName,
				util.VpcDnatEPortLabel:      "80",
			},
			Spec: kubeovnv1.IptablesDnatRuleSpec{
				EIP: eip, ClusterIP: clusterIP, ExternalPort: "80", Protocol: "tcp",
				InternalIP: backend, InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
			},
		}
	}
	// Two hand-managed rules sharing one EIP: each serves its own internal VIP.
	a := rule("a", "eip0", "10.96.1.5", "10.0.7.2")
	b := rule("b", "eip0", "10.96.1.6", "10.0.7.3")
	// Two rules of one Service port: both serve the same internal VIP.
	svc1 := rule("svc-1", "eip0", "10.96.1.7", "10.0.7.4")
	svc2 := rule("svc-2", "eip0", "10.96.1.7", "10.0.7.5")
	c := dnatListerController(t, a, b, svc1, svc2)

	backends, _, _, err := c.clusterIPIdentityBackends(gwName, a, "tcp", "80", "10.96.1.5")
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.7.2:8080"}, backends,
		"the internal VIP of one rule must not carry the backends of a rule serving another one")

	backends, _, _, err = c.clusterIPIdentityBackends(gwName, svc1, "tcp", "80", "10.96.1.7")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"10.0.7.4:8080", "10.0.7.5:8080"}, backends,
		"a Service port keeps every rule serving its internal VIP")
}

// Test_shareBackendsSeparatesOwners pins the last line of defence behind admission: a rule must never
// aggregate the backends of a rule with a different owner, whatever identity they appear to share.
// Two Service rules carry one owner key and a hand-managed rule carries none, so a hand-managed rule
// that happens to serve the same address cannot inject its backend into a Service's nft map (the
// webhook rejects such a pair, and this covers the window where its cache had not seen it yet).
func Test_shareBackendsSeparatesOwners(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	svcRule := ownedShareRule("svc-rule", "eip0", "10.96.1.5")
	svcRule.Spec.EIP = "eip0"
	manual := clusterIPServedRule("manual-rule", gwName, "10.96.1.5")
	otherSvc := ownedShareRule("other-svc-rule", "eip0", "10.96.1.6")
	otherSvc.Labels[util.NftableLbSvcNameLabel] = "other"
	c := dnatListerController(t, svcRule, manual, otherSvc)

	backends, _, _, err := c.shareClusterIPBackends(gwName, svcRule, "80", "tcp", "10.96.1.5")
	require.NoError(t, err)
	require.Empty(t, backends, "a hand-managed rule of another owner must not join a Service identity")

	manualBackends, _, _, err := c.shareClusterIPBackends(gwName, manual, "80", "tcp", "10.96.1.5")
	require.NoError(t, err)
	require.Empty(t, manualBackends, "and the Service rule must not join the hand-managed identity either")
}
