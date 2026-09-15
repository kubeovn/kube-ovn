package controller

import (
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/set"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func Test_nftableLbSvcQualifies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		svc      *v1.Service
		expected bool
	}{
		{
			name: "loadbalancer with eip annotation",
			svc: &v1.Service{
				Annotations: map[string]string{util.EipAnnotation: "eip0"},
				Spec:        v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer},
			},
			expected: true,
		},
		{
			name: "loadbalancer without eip annotation",
			svc: &v1.Service{
				Spec: v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer},
			},
			expected: false,
		},
		{
			name: "clusterip with eip annotation",
			svc: &v1.Service{
				Annotations: map[string]string{util.EipAnnotation: "eip0"},
				Spec:        v1.ServiceSpec{Type: v1.ServiceTypeClusterIP},
			},
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
	c := &Controller{config: &Configuration{EnableLb: true}}
	require.NotPanics(t, func() {
		c.enqueueNftableLbServicesForPod(&v1.Pod{Namespace: "ns", Name: "pod"})
		c.enqueueNftableLbServicesForEIP("eip0")
		c.enqueueNftableLbServicesForNatGw("gw")
	})
}

func TestEnqueueNftableLbServiceWithoutOvnLb(t *testing.T) {
	t.Parallel()

	// The feature and the OVN load balancer are independent switches (they only never own the same
	// VIP), so it has to work with --enable-lb=false: the queue must be fed from the Service
	// informer alone.
	queue := newTypedRateLimitingQueue[string]("nftable-lb-no-ovn-lb", nil)
	t.Cleanup(queue.ShutDown)
	c := &Controller{
		config:                       &Configuration{EnableLb: false, EnableNftableLbSvc: true},
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
		config:                       &Configuration{EnableLb: true, EnableNftableLbSvc: true},
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

	desired := buildDesiredNftableLbDnatRules(svc, "eip0", endpointSlices, testNftableLbBackendIP)

	// 2 ready IPv4 backends on each of the two ports: tcp and sctp get the same treatment
	tcpRules := make(map[string]*kubeovnv1.IptablesDnatRule)
	sctpRules := make(map[string]*kubeovnv1.IptablesDnatRule)
	for _, rule := range desired {
		switch rule.Spec.Protocol {
		case "tcp":
			tcpRules[rule.Spec.InternalIP] = rule
		case "sctp":
			sctpRules[rule.Spec.InternalIP] = rule
		}
	}
	require.Len(t, desired, 4)
	require.Len(t, tcpRules, 2)
	require.Len(t, sctpRules, 2)
	require.Contains(t, sctpRules, "10.0.0.1")
	require.Contains(t, sctpRules, "10.0.0.2")
	require.Equal(t, "90", sctpRules["10.0.0.1"].Spec.ExternalPort)
	require.Equal(t, "9090", sctpRules["10.0.0.1"].Spec.InternalPort)

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

	require.Len(t, buildDesiredNftableLbDnatRules(svc, "eip0", endpointSlices, testNftableLbBackendIP), 1)
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

	desired := buildDesiredNftableLbDnatRules(svc, "eip0", endpointSlices, testNftableLbBackendIP)
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
	desired := buildDesiredNftableLbDnatRules(svc, "eip0", endpointSlices, testNftableLbBackendIP)
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
	desiredNone := buildDesiredNftableLbDnatRules(svcNone, "eip0", endpointSlices, testNftableLbBackendIP)
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

	// the internal VIP is recorded on every generated rule: it is the only durable record of the
	// ClusterIP identity the gateway programs next to the EIP
	desired := buildDesiredNftableLbDnatRules(svc, "eip0", endpointSlices, testNftableLbBackendIP)
	require.Len(t, desired, 2)
	for _, rule := range desired {
		require.Equal(t, "10.96.1.5", rule.Annotations[util.NftableLbSvcClusterIPAnnotation])
	}

	// a headless Service has no internal VIP: the EIP identity is programmed alone
	svcHeadless := svc.DeepCopy()
	svcHeadless.Spec.ClusterIP = v1.ClusterIPNone
	svcHeadless.Spec.ClusterIPs = nil
	desired = buildDesiredNftableLbDnatRules(svcHeadless, "eip0", endpointSlices, testNftableLbBackendIP)
	require.Len(t, desired, 2)
	for _, rule := range desired {
		require.Empty(t, rule.Annotations[util.NftableLbSvcClusterIPAnnotation])
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
	// identities must match what buildDesiredNftableLbDnatRules would program: every transport
	// protocol shares the same nftables treatment
	require.ElementsMatch(t, []string{"eip0/80/tcp", "eip0/53/udp", "eip0/90/sctp"}, ids)
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
				util.EipAnnotation: "eip0",
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
				util.EipAnnotation: "eip0",
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

func Test_nftableLbDnatRuleDrifted(t *testing.T) {
	t.Parallel()

	want := &kubeovnv1.IptablesDnatRule{
		Annotations: map[string]string{util.NftableLbSvcClusterIPAnnotation: "10.96.1.5"},
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			EIP: "eip0", ExternalPort: "80", Protocol: "tcp",
			InternalIP: "10.0.0.1", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
		},
	}

	same := want.DeepCopy()
	require.False(t, nftableLbDnatRuleDrifted(same, want))

	// spec drift (a new EIP) drives a recreate
	eip := want.DeepCopy()
	eip.Spec.EIP = "eip1"
	require.True(t, nftableLbDnatRuleDrifted(eip, want))

	// ClusterIP drift is annotation-only but must still drive a recreate: the rule names stay
	// identical when a Service is recreated with the same backends, so nothing else would notice
	clusterIP := want.DeepCopy()
	clusterIP.Annotations[util.NftableLbSvcClusterIPAnnotation] = "10.96.1.6"
	require.True(t, nftableLbDnatRuleDrifted(clusterIP, want))

	// losing the internal VIP (Service became headless) also rebuilds the rule
	gone := want.DeepCopy()
	delete(gone.Annotations, util.NftableLbSvcClusterIPAnnotation)
	require.True(t, nftableLbDnatRuleDrifted(gone, want))
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

func Test_desiredNatGwVipRouteVips(t *testing.T) {
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
		if clusterIP != "" {
			rule.Annotations = map[string]string{util.NftableLbSvcClusterIPAnnotation: clusterIP}
		}
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
	// a hand-managed share rule contributes nothing: the feature owns these routes
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

	vips, err := controller.desiredNatGwVipRouteVips(gwName)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"ip4.dst == 203.0.113.10": "203.0.113.10",
		"ip4.dst == 10.96.1.5":    "10.96.1.5",
		"ip4.dst == 10.96.1.7":    "10.96.1.7",
	}, vips)

	// an IPv6-only ClusterIP has no IPv4 route (share DNAT is IPv4 only)
	require.NoError(t, ruleIndexer.Add(owned("v6", "eip0", "fd00::1")))
	vips, err = controller.desiredNatGwVipRouteVips(gwName)
	require.NoError(t, err)
	require.NotContains(t, vips, "ip4.dst == fd00::1")
	require.Len(t, vips, 3)
}

// gatewayPods returns a ready NAT gateway instance as the VIP routes' next hop.
func gatewayPods(gwName, ip string) []*v1.Pod {
	return []*v1.Pod{{
		Namespace: metav1.NamespaceSystem,
		Name:      util.GenNatGwPodName(gwName),
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
	for node, ip := range nodeIPs {
		pods = append(pods, &v1.Pod{
			Namespace: metav1.NamespaceSystem,
			Name:      util.GenNatGwPodName(gwName) + "-" + node,
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
			EIP: eip, ExternalPort: "80", Protocol: "tcp",
			InternalIP: "10.0.7.2", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
		},
	}
	if clusterIP != "" {
		rule.Annotations = map[string]string{util.NftableLbSvcClusterIPAnnotation: clusterIP}
	}
	return rule
}

func Test_syncNatGwVipRoutes(t *testing.T) {
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

		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).Return(nil, nil)
		added := map[string][]string{}
		fc.mockOvnClient.EXPECT().AddLogicalRouterPolicy("vpc0", util.NatGatewayVipPolicyPriority, gomock.Any(), string(kubeovnv1.PolicyRouteActionReroute), gomock.Any(), nil, externalIDs).
			DoAndReturn(func(_ string, _ int, match, _ string, nextHops, _ []string, _ map[string]string) error {
				added[match] = nextHops
				return nil
			}).Times(2)

		require.NoError(t, c.syncNatGwVipRoutes("gw0"))
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

		require.NoError(t, c.syncNatGwVipRoutes("gw0"))
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

		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).Return(nil, nil)
		added := map[string][]string{}
		fc.mockOvnClient.EXPECT().AddLogicalRouterPolicy("vpc0", util.NatGatewayVipPolicyPriority, gomock.Any(), string(kubeovnv1.PolicyRouteActionReroute), gomock.Any(), nil, externalIDs).
			DoAndReturn(func(_ string, _ int, match, _ string, nextHops, _ []string, _ map[string]string) error {
				added[match] = nextHops
				return nil
			}).Times(2)

		require.NoError(t, c.syncNatGwVipRoutes("gw0"))
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

		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).Return(nil, nil)
		added := map[string][]string{}
		fc.mockOvnClient.EXPECT().AddLogicalRouterPolicy("vpc0", util.NatGatewayVipPolicyPriority, gomock.Any(), string(kubeovnv1.PolicyRouteActionReroute), gomock.Any(), nil, externalIDs).
			DoAndReturn(func(_ string, _ int, match, _ string, nextHops, _ []string, _ map[string]string) error {
				added[match] = nextHops
				return nil
			}).Times(2)

		require.NoError(t, c.syncNatGwVipRoutes("gw0"))
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

		existing := &ovnnb.LogicalRouterPolicy{UUID: "u1", Priority: util.NatGatewayVipPolicyPriority, Match: "ip4.dst == 10.96.1.5"}
		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).
			Return([]*ovnnb.LogicalRouterPolicy{existing}, nil)
		fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicyByUUID("vpc0", "u1").Return(nil)

		require.NoError(t, c.syncNatGwVipRoutes("gw0"))
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

		existing := &ovnnb.LogicalRouterPolicy{UUID: "u1", Priority: util.NatGatewayVipPolicyPriority, Match: "ip4.dst == 10.96.1.5"}
		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).
			Return([]*ovnnb.LogicalRouterPolicy{existing}, nil)
		fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicyByUUID("vpc0", "u1").Return(nil)

		require.NoError(t, c.syncNatGwVipRoutes("gw0"))
	})
}

func Test_nftableLbSvcOwnsServiceVipsInVpc(t *testing.T) {
	t.Parallel()

	eipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, eipIndexer.Add(&kubeovnv1.IptablesEIP{
		Name:   "eip0",
		Spec:   kubeovnv1.IptablesEIPSpec{NatGwDp: "gw0"},
		Status: kubeovnv1.IptablesEIPStatus{IP: "172.20.0.5"},
	}))
	gwIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, gwIndexer.Add(&kubeovnv1.VpcNatGateway{
		Name: "gw0",
		Spec: kubeovnv1.VpcNatGatewaySpec{Vpc: "vpc0"},
	}))

	newController := func(config *Configuration) *Controller {
		return &Controller{
			config:              config,
			iptablesEipsLister:  kubeovnlister.NewIptablesEIPLister(eipIndexer),
			vpcNatGatewayLister: kubeovnlister.NewVpcNatGatewayLister(gwIndexer),
		}
	}
	svc := &v1.Service{
		Namespace: "default", Name: "web", Annotations: map[string]string{util.EipAnnotation: "eip0"},
		Spec: v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer},
	}

	c := newController(&Configuration{EnableNftableLbSvc: true})
	require.True(t, c.nftableLbSvcOwnsServiceVipsInVpc(svc, "vpc0"), "the gateway's own VPC hands its VIPs to the gateway")
	require.False(t, c.nftableLbSvcOwnsServiceVipsInVpc(svc, "vpc1"), "another VPC keeps load balancing the VIPs")

	// the feature gate decides: without it kube-ovn keeps loading balancing the VIPs
	require.False(t, newController(&Configuration{}).nftableLbSvcOwnsServiceVipsInVpc(svc, "vpc0"))

	// work with and without the OVN load balancer: the flags are independent
	require.True(t, newController(&Configuration{EnableLb: true, EnableNftableLbSvc: true}).nftableLbSvcOwnsServiceVipsInVpc(svc, "vpc0"))
	require.True(t, newController(&Configuration{EnableLb: false, EnableNftableLbSvc: true}).nftableLbSvcOwnsServiceVipsInVpc(svc, "vpc0"))

	// only Services that opt in (EIP annotation) hand their VIPs over
	plain := svc.DeepCopy()
	plain.Annotations = nil
	require.False(t, c.nftableLbSvcOwnsServiceVipsInVpc(plain, "vpc0"))

	// an unknown EIP or gateway cannot own anything yet
	unknownEip := svc.DeepCopy()
	unknownEip.Annotations[util.EipAnnotation] = "missing"
	require.False(t, c.nftableLbSvcOwnsServiceVipsInVpc(unknownEip, "vpc0"))

	// share DNAT is IPv4 only: an EIP without an IPv4 address has no gateway data plane, so its VIPs
	// stay with the OVN load balancer
	require.NoError(t, eipIndexer.Add(&kubeovnv1.IptablesEIP{
		Name:   "eip6",
		Spec:   kubeovnv1.IptablesEIPSpec{NatGwDp: "gw0", V6ip: "fd00::5"},
		Status: kubeovnv1.IptablesEIPStatus{IP: "fd00::5"},
	}))
	v6Eip := svc.DeepCopy()
	v6Eip.Annotations[util.EipAnnotation] = "eip6"
	require.False(t, c.nftableLbSvcOwnsServiceVipsInVpc(v6Eip, "vpc0"))

	// a SwitchLBRule/RouterLBRule VIP stays under the control of the VIP feature
	for _, annotation := range []string{util.SwitchLBRuleVipsAnnotation, util.RouterLBRuleVipsAnnotation} {
		mixed := svc.DeepCopy()
		mixed.Annotations[annotation] = "10.96.0.100"
		require.False(t, c.nftableLbSvcOwnsServiceVipsInVpc(mixed, "vpc0"))
	}
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
		Annotations: map[string]string{util.NftableLbSvcClusterIPAnnotation: clusterIP},
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			EIP: eipName, ExternalPort: "80", Protocol: "tcp",
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
	controller.config.EnableNftableLbSvc = true
	controller.config.EnableLb = true

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
func TestHandleUpdateEndpointSliceSkipsOwnedVips(t *testing.T) {
	const (
		gwName     = "owned-gw"
		subnetName = "owned-subnet"
		eipName    = "owned-eip"
		vpcName    = util.DefaultVpc
		svcName    = "owned-web"
		clusterIP  = "10.96.1.5"
	)
	namespace := metav1.NamespaceDefault
	gw := &kubeovnv1.VpcNatGateway{
		Name: gwName, UID: "gw-uid",
		Spec: kubeovnv1.VpcNatGatewaySpec{Vpc: vpcName, Subnet: subnetName},
	}
	eip := &kubeovnv1.IptablesEIP{
		Name:   eipName,
		Spec:   kubeovnv1.IptablesEIPSpec{NatGwDp: gwName, V4ip: "172.20.0.5"},
		Status: kubeovnv1.IptablesEIPStatus{IP: "172.20.0.5"},
	}
	svc := &v1.Service{
		Namespace:   namespace,
		Name:        svcName,
		Annotations: map[string]string{util.EipAnnotation: eipName},
		Spec: v1.ServiceSpec{
			Type:       v1.ServiceTypeLoadBalancer,
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
		Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{"10.0.7.2"}, Conditions: discoveryv1.EndpointConditions{Ready: new(true)}}},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs: []*kubeovnv1.Vpc{{
			Name:   vpcName,
			Status: kubeovnv1.VpcStatus{TCPLoadBalancer: "vpc-tcp-load"},
		}},
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
		Subnets: []*kubeovnv1.Subnet{{
			Name: subnetName,
			Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolIPv4, Vpc: vpcName, CIDRBlock: "10.0.7.0/24", Gateway: "10.0.7.1"},
		}},
		Services:       []*v1.Service{svc},
		EndpointSlices: []*discoveryv1.EndpointSlice{slice},
		IptablesEips:   []*kubeovnv1.IptablesEIP{eip},
	})
	require.NoError(t, err)
	controller := fakeController.fakeController
	controller.config.EnableNftableLbSvc = true
	controller.config.EnableLb = true

	// The gateway serves this Service in this VPC, so the VIP must be released from the load
	// balancer and never programmed (no LoadBalancerAddVip expectation: an unexpected call fails).
	fakeController.mockOvnClient.EXPECT().LoadBalancerDeleteIPPortMapping("vpc-tcp-load", clusterIP+":80").Return(nil).Times(1)
	fakeController.mockOvnClient.EXPECT().LoadBalancerDeleteVip("vpc-tcp-load", clusterIP+":80", true).Return(nil).Times(1)

	require.NoError(t, controller.handleUpdateEndpointSlice(namespace+"/"+svcName))
}

func Test_nftableLbSvcVips(t *testing.T) {
	t.Parallel()

	// share DNAT is IPv4 only: an IPv6 ClusterIP or ingress address has no gateway data plane and
	// must stay with the OVN load balancer
	svc := &v1.Service{Spec: v1.ServiceSpec{ClusterIPs: []string{"10.96.1.5", "fd00:10:16::1"}}}
	require.Equal(t, []string{"10.96.1.5"}, nftableLbSvcVips(svc))

	svc.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{{IP: "172.20.0.5"}, {IP: "fd00::5"}, {IP: ""}}
	require.Equal(t, []string{"10.96.1.5", "172.20.0.5"}, nftableLbSvcVips(svc))

	v6Only := &v1.Service{Spec: v1.ServiceSpec{ClusterIPs: []string{"fd00:10:16::1"}}}
	require.Empty(t, nftableLbSvcVips(v6Only))

	headless := &v1.Service{Spec: v1.ServiceSpec{ClusterIP: v1.ClusterIPNone}}
	require.Empty(t, nftableLbSvcVips(headless))
}

func Test_nftableLbSvcVipPorts(t *testing.T) {
	t.Parallel()

	// only the identities the gateway serves: IPv4 VIPs on every servicePort. The IPv6 ClusterIP
	// keeps its OVN load balancing.
	svc := &v1.Service{
		Spec: v1.ServiceSpec{
			ClusterIPs: []string{"10.96.1.5", "fd00:10:16::1"},
			Ports: []v1.ServicePort{
				{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
				{Name: "dns", Port: 53, Protocol: v1.ProtocolUDP},
				{Name: "sctp", Port: 132, Protocol: v1.ProtocolSCTP},
			},
		},
		Status: v1.ServiceStatus{LoadBalancer: v1.LoadBalancerStatus{
			Ingress: []v1.LoadBalancerIngress{{IP: "172.20.0.5"}},
		}},
	}
	require.Equal(t, []string{
		"10.96.1.5:80", "10.96.1.5:53", "10.96.1.5:132",
		"172.20.0.5:80", "172.20.0.5:53", "172.20.0.5:132",
	}, nftableLbSvcVipPorts(svc))

	// an IPv6-only Service owns nothing
	v6Only := svc.DeepCopy()
	v6Only.Spec.ClusterIPs = []string{"fd00:10:16::1"}
	v6Only.Status.LoadBalancer.Ingress = nil
	require.Empty(t, nftableLbSvcVipPorts(v6Only))
}

func Test_clearNftableLbSvcVipsFromLBs(t *testing.T) {
	t.Parallel()

	svc := &v1.Service{
		Spec: v1.ServiceSpec{
			ClusterIPs: []string{"10.96.1.5", "fd00:10:16::1"},
			Ports: []v1.ServicePort{
				{Port: 80, Protocol: v1.ProtocolTCP},
				{Port: 443, Protocol: v1.ProtocolUDP},
				{Port: 132, Protocol: v1.ProtocolSCTP},
			},
		},
		Status: v1.ServiceStatus{LoadBalancer: v1.LoadBalancerStatus{
			Ingress: []v1.LoadBalancerIngress{{IP: "172.20.0.5"}, {IP: ""}},
		}},
	}
	// every IPv4 VIP on every servicePort, on every load balancer that is named; the empty name (a
	// protocol with no load balancer) is skipped, deleting an absent VIP must be a no-op, and the
	// IPv6 ClusterIP is not part of the owned set.
	want := set.New[string]()
	for _, lb := range []string{"vpc-tcp-load", "vpc-udp-load", "vpc-tcp-sess-load"} {
		for _, vip := range []string{"10.96.1.5", "172.20.0.5"} {
			for _, port := range []int32{80, 443, 132} {
				want.Insert(lb + " " + util.JoinHostPort(vip, port))
			}
		}
	}

	fc, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	c := fc.fakeController

	got := set.New[string]()
	fc.mockOvnClient.EXPECT().LoadBalancerDeleteIPPortMapping(gomock.Any(), gomock.Any()).
		DoAndReturn(func(lb, vip string) error { got.Insert(lb + " " + vip); return nil }).Times(want.Len())
	fc.mockOvnClient.EXPECT().LoadBalancerDeleteVip(gomock.Any(), gomock.Any(), true).
		DoAndReturn(func(lb, vip string, _ bool) error { got.Insert(lb + " " + vip); return nil }).Times(want.Len())

	require.NoError(t, c.clearNftableLbSvcVipsFromLBs(svc, "vpc-tcp-load", "", "vpc-udp-load", "vpc-tcp-sess-load"))
	require.Equal(t, want.SortedList(), got.SortedList())
}

func Test_clearNftableLbSvcVipsFromLBs_noVips(t *testing.T) {
	t.Parallel()

	// A headless Service without a published address owns nothing: no load balancer call at all.
	fc, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	svc := &v1.Service{Spec: v1.ServiceSpec{Ports: []v1.ServicePort{{Port: 80, Protocol: v1.ProtocolTCP}}}}
	require.NoError(t, fc.fakeController.clearNftableLbSvcVipsFromLBs(svc, "vpc-tcp-load"))
}
