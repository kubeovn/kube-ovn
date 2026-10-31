package controller

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	discoverylisters "k8s.io/client-go/listers/discovery/v1"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func Test_nftableLbSvcQualifies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		svcType         v1.ServiceType
		annotations     map[string]string
		enableEIP       bool
		enableClusterIP bool
		wantEIP         bool
		wantClusterIP   bool
	}{
		{
			name: "loadbalancer with both identities enabled", svcType: v1.ServiceTypeLoadBalancer,
			annotations: map[string]string{util.VpcNatGatewayAnnotation: "gw0", util.EipAnnotation: "eip0"},
			enableEIP:   true, enableClusterIP: true, wantEIP: true, wantClusterIP: true,
		},
		{
			name: "loadbalancer with only eip identity enabled", svcType: v1.ServiceTypeLoadBalancer,
			annotations: map[string]string{util.VpcNatGatewayAnnotation: "gw0", util.EipAnnotation: "eip0"},
			enableEIP:   true, wantEIP: true,
		},
		{
			name: "loadbalancer with only cluster ip identity enabled", svcType: v1.ServiceTypeLoadBalancer,
			annotations:     map[string]string{util.VpcNatGatewayAnnotation: "gw0", util.EipAnnotation: "eip0"},
			enableClusterIP: true, wantClusterIP: true,
		},
		{
			name: "loadbalancer with both identities disabled", svcType: v1.ServiceTypeLoadBalancer,
			annotations: map[string]string{util.VpcNatGatewayAnnotation: "gw0", util.EipAnnotation: "eip0"},
		},
		{
			name: "loadbalancer without eip can still serve cluster ip", svcType: v1.ServiceTypeLoadBalancer,
			annotations: map[string]string{util.VpcNatGatewayAnnotation: "gw0"},
			enableEIP:   true, enableClusterIP: true, wantClusterIP: true,
		},
		{
			name: "cluster ip service enabled", svcType: v1.ServiceTypeClusterIP,
			annotations:     map[string]string{util.VpcNatGatewayAnnotation: "gw0"},
			enableClusterIP: true, wantClusterIP: true,
		},
		{
			name: "service without gateway", svcType: v1.ServiceTypeLoadBalancer,
			annotations: map[string]string{util.EipAnnotation: "eip0"},
			enableEIP:   true, enableClusterIP: true,
		},
		{
			name: "nodeport is not handled", svcType: v1.ServiceTypeNodePort,
			annotations: map[string]string{util.VpcNatGatewayAnnotation: "gw0", util.EipAnnotation: "eip0"},
			enableEIP:   true, enableClusterIP: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &Controller{config: &Configuration{
				EnableGwNftableLbSvc: tt.enableEIP, EnableGwNftableSvcClusterIP: tt.enableClusterIP,
			}}
			svc := &v1.Service{Annotations: tt.annotations, Spec: v1.ServiceSpec{Type: tt.svcType}}
			gotEIP, gotClusterIP := c.nftableLbSvcQualifies(svc)
			require.Equal(t, tt.wantEIP, gotEIP)
			require.Equal(t, tt.wantClusterIP, gotClusterIP)
		})
	}
}

func TestEnqueueGwNftableLbServiceWithoutOvnLb(t *testing.T) {
	t.Parallel()

	// Gateway mode runs with the OVN mode disabled, so Service events must feed its own queue.
	queue := newTypedRateLimitingQueue[string]("gw-nftable-lb-no-ovn-lb", nil)
	t.Cleanup(queue.ShutDown)
	c := &Controller{
		config:                         &Configuration{EnableOvnLB: false, EnableGwNftableLbSvc: true},
		addOrUpdateGwNftableLbSvcQueue: queue,
	}

	c.enqueueGwNftableLbService("default/web")
	require.Equal(t, 1, c.addOrUpdateGwNftableLbSvcQueue.Len())
	item, _ := c.addOrUpdateGwNftableLbSvcQueue.Get()
	require.Equal(t, "default/web", item)
	c.addOrUpdateGwNftableLbSvcQueue.Done(item)
}

func TestPodAndEipEventsDoNotEnqueueGwNftableLbService(t *testing.T) {
	t.Parallel()

	serviceQueue := newTypedRateLimitingQueue[string]("GwNftableLbService", nil)
	addEipQueue := newTypedRateLimitingQueue[string]("AddIptablesEip", nil)
	updateEipQueue := newTypedRateLimitingQueue[string]("UpdateIptablesEip", nil)
	delEipQueue := newTypedRateLimitingQueue[*kubeovnv1.IptablesEIP]("DelIptablesEip", nil)
	defer serviceQueue.ShutDown()
	defer addEipQueue.ShutDown()
	defer updateEipQueue.ShutDown()
	defer delEipQueue.ShutDown()

	endpointIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	serviceIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	c := &Controller{
		config:                         &Configuration{EnableGwNftableLbSvc: true},
		addOrUpdateGwNftableLbSvcQueue: serviceQueue,
		addIptablesEipQueue:            addEipQueue,
		updateIptablesEipQueue:         updateEipQueue,
		delIptablesEipQueue:            delEipQueue,
		endpointSlicesLister:           discoverylisters.NewEndpointSliceLister(endpointIndexer),
		servicesLister:                 corelisters.NewServiceLister(serviceIndexer),
	}

	oldEip := &kubeovnv1.IptablesEIP{Name: "eip0"}
	newEip := oldEip.DeepCopy()
	newEip.ResourceVersion = "2"
	c.enqueueAddIptablesEip(oldEip)
	c.enqueueUpdateIptablesEip(oldEip, newEip)
	c.enqueueDelIptablesEip(oldEip)

	oldPod := &v1.Pod{Name: "pod", Namespace: "default", Spec: v1.PodSpec{HostNetwork: true}}
	newPod := oldPod.DeepCopy()
	newPod.ResourceVersion = "2"
	c.enqueueAddPod(oldPod)
	c.enqueueUpdatePod(oldPod, newPod)
	c.enqueueDeletePod(oldPod)

	require.Zero(t, serviceQueue.Len())
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

// buildDesiredNftableLbDnatRules builds the rules of a Service that is served through both of its
// addresses, which is what most of these tests exercise.
func buildDesiredNftableLbDnatRules(svc *v1.Service, eipName, gateway string, endpointSlices []*discoveryv1.EndpointSlice, backendIP func(discoveryv1.Endpoint) (string, bool)) map[string]*kubeovnv1.IptablesDnatRule {
	return buildDesiredNftableLbDnatRulesForIdentities(svc, eipName, gateway, true, true, endpointSlices, backendIP)
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
		IndexGwNftableLbServiceByEip: indexGwNftableLbServiceByEip,
	})
	port := int32(80)
	newService := func(namespace, name string) *v1.Service {
		return &v1.Service{
			Namespace: namespace,
			Name:      name,
			Annotations: map[string]string{
				util.EipAnnotation:           "eip0",
				util.VpcNatGatewayAnnotation: "gw0",
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
	queue := newTypedRateLimitingQueue[string]("gw-nftable-lb-conflict-test", nil)
	t.Cleanup(queue.ShutDown)
	controller := &Controller{
		svcIndexer:                     indexer,
		iptablesDnatRulesLister:        kubeovnlister.NewIptablesDnatRuleLister(ruleIndexer),
		recorder:                       record.NewFakeRecorder(2),
		addOrUpdateGwNftableLbSvcQueue: queue,
	}

	conflicted, err := controller.resolveNftableLbConflicts(loser, "ns/z-loser", map[string]*kubeovnv1.IptablesDnatRule{})
	require.NoError(t, err)
	require.True(t, conflicted, "a loser must be detected from Service ports even without ready backends")

	desired := map[string]*kubeovnv1.IptablesDnatRule{
		"backend": {
			Labels: map[string]string{util.EipV4IpLabel: "203.0.113.10", util.EipUIDLabel: "eip-uid"},
			Spec: kubeovnv1.IptablesDnatRuleSpec{
				EIP: "eip0", ClusterIP: "10.96.1.5", ExternalPort: "80", Protocol: "tcp",
			},
		},
	}
	conflicted, err = controller.resolveNftableLbConflicts(loser, "ns/z-loser", desired)
	require.NoError(t, err)
	require.True(t, conflicted)
	require.Empty(t, desired["backend"].Spec.EIP)
	require.Equal(t, "10.96.1.5", desired["backend"].Spec.ClusterIP)
	require.NotContains(t, desired["backend"].Labels, util.EipV4IpLabel)
	require.NotContains(t, desired["backend"].Labels, util.EipUIDLabel)
}

func TestResolveNftableLbConflictsIgnoresTerminatingServices(t *testing.T) {
	t.Parallel()

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		IndexGwNftableLbServiceByEip: indexGwNftableLbServiceByEip,
	})
	newService := func(name string, deleting bool) *v1.Service {
		svc := &v1.Service{
			Namespace: "ns",
			Name:      name,
			Annotations: map[string]string{
				util.EipAnnotation:           "eip0",
				util.VpcNatGatewayAnnotation: "gw0",
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
	queue := newTypedRateLimitingQueue[string]("gw-nftable-lb-terminating-test", nil)
	t.Cleanup(queue.ShutDown)
	controller := &Controller{
		svcIndexer:                     indexer,
		iptablesDnatRulesLister:        kubeovnlister.NewIptablesDnatRuleLister(ruleIndexer),
		recorder:                       record.NewFakeRecorder(1),
		addOrUpdateGwNftableLbSvcQueue: queue,
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

	// The lexicographically smallest Service key wins.
	require.Equal(t, "ns/a", chooseNftableLbOwner(map[string]struct{}{"ns/a": {}, "ns/b": {}}))
	require.Equal(t, "ns/a", chooseNftableLbOwner(map[string]struct{}{"ns/b": {}, "ns/a": {}}))

	// single owner wins trivially
	require.Equal(t, "ns/only", chooseNftableLbOwner(map[string]struct{}{"ns/only": {}}))
}

func Test_desiredNatGwVipStateForService(t *testing.T) {
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
				util.NftableLbSvcNsLabel:     "default",
				util.NftableLbSvcNameLabel:   "web",
				util.NftableLbSvcRecordLabel: "true",
				util.VpcNatGatewayNameLabel:  gwName,
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
	// a terminating rule must not keep a route alive
	terminating := owned("terminating", "eip0", "10.96.1.9")
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	terminating.Finalizers = []string{"keep"}
	require.NoError(t, ruleIndexer.Add(terminating))
	// an EIP that is not ready yet has no address to route. Its ClusterIP is still routed: the
	// internal VIP does not depend on the EIP.
	require.NoError(t, ruleIndexer.Add(owned("not-ready", "not-ready", "10.96.1.7")))

	vips, clusterIPs, err := controller.desiredNatGwVipStateForService(gwName, "", nil, "")
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
	vips, clusterIPs, err = controller.desiredNatGwVipStateForService(gwName, "", nil, "")
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
	vips, clusterIPs, err = controller.desiredNatGwVipStateForService(gwName, "", nil, "")
	require.NoError(t, err)
	require.Equal(t, "10.96.1.8", vips["ip4.dst == 10.96.1.8"], "the internal VIP of the rule is routed")
	require.Contains(t, clusterIPs, "10.96.1.8", "and held on lo")
	require.Equal(t, "203.0.113.10", vips["ip4.dst == 203.0.113.10"], "next to the ingress IP it aligns with")
}

// A Service that just created its accounting records is invisible to the informer for a moment.
// Another Service of the same gateway reconciling in that window computes the gateway-wide VIP set
// and overwrites the data plane with it, so it must keep the VIPs of the Service it cannot see:
// nothing would re-enqueue that Service to restore them (the informers run without resync).
func TestDesiredNatGwVipStateKeepsUnobservedServiceVips(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	controller := &Controller{
		iptablesDnatRulesLister: kubeovnlister.NewIptablesDnatRuleLister(indexer),
		iptablesEipsLister:      kubeovnlister.NewIptablesEIPLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
	}
	peerRecord := clusterIPServedRule("peer", gwName, "10.96.1.6")
	peerRecord.Labels[util.NftableLbSvcNameLabel] = "peer"
	const peer = "default/peer"
	peerVips := nftableLbSvcVips{clusterIPs: []string{"10.96.1.6"}}

	self := map[string]*kubeovnv1.IptablesDnatRule{"self": clusterIPServedRule("self", gwName, "10.96.1.5")}
	selfOwner := "default/web"

	// The peer programmed its VIP but its record has not reached the cache yet.
	controller.rememberNatGwVipState(gwName, peer, peerVips)
	vips, clusterIPs, err := controller.desiredNatGwVipStateForService(gwName, selfOwner, self, "")
	require.NoError(t, err)
	require.Equal(t, []string{"10.96.1.5", "10.96.1.6"}, clusterIPs)
	require.Equal(t, map[string]string{
		"ip4.dst == 10.96.1.5": "10.96.1.5",
		"ip4.dst == 10.96.1.6": "10.96.1.6",
	}, vips)

	// Once the record is observed the cache is authoritative; the floor adds nothing.
	require.NoError(t, indexer.Add(peerRecord))
	_, clusterIPs, err = controller.desiredNatGwVipStateForService(gwName, selfOwner, self, "")
	require.NoError(t, err)
	require.Equal(t, []string{"10.96.1.5", "10.96.1.6"}, clusterIPs)

	// A record deleted from outside stays terminating while its Service still programs the identity,
	// and nothing re-enqueues that Service (record events are ignored). Such a record must therefore
	// not make its owner look authoritative: the floor keeps speaking for it.
	now := metav1.Now()
	peerRecord.DeletionTimestamp = &now
	peerRecord.Finalizers = []string{"keep"}
	require.NoError(t, indexer.Update(peerRecord))
	_, clusterIPs, err = controller.desiredNatGwVipStateForService(gwName, selfOwner, self, "")
	require.NoError(t, err)
	require.Equal(t, []string{"10.96.1.5", "10.96.1.6"}, clusterIPs,
		"an externally deleted record must not drop the vips its owner still programs")

	// An owner that is really leaving drops its floor first (cleanupNftableLbSvcGateway calls
	// forgetNatGwVipState before it deletes the records), which is what releases its VIPs.
	controller.forgetNatGwVipState(gwName, peer)
	_, clusterIPs, err = controller.desiredNatGwVipStateForService(gwName, selfOwner, self, "")
	require.NoError(t, err)
	require.Equal(t, []string{"10.96.1.5"}, clusterIPs)

	// And the state stays released once the records are gone.
	require.NoError(t, indexer.Delete(peerRecord))
	require.Empty(t, controller.nftableLbVipSnapshot)
	_, clusterIPs, err = controller.desiredNatGwVipStateForService(gwName, selfOwner, self, "")
	require.NoError(t, err)
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
			util.NftableLbSvcNsLabel:     "default",
			util.NftableLbSvcNameLabel:   "web",
			util.NftableLbSvcRecordLabel: "true",
			util.VpcNatGatewayNameLabel:  "gw0",
		},
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			EIP: eip, ClusterIP: clusterIP, ExternalPort: "80", Protocol: "tcp",
			InternalIP: "10.0.7.2", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
		},
	}
	return rule
}

// vipRouteGw is the gateway every VIP route test reconciles, with one EIP and the external IDs
// its policies carry.
var (
	vipRouteGw  = &kubeovnv1.VpcNatGateway{Name: "gw0", Spec: kubeovnv1.VpcNatGatewaySpec{Vpc: "vpc0"}}
	vipRouteEip = &kubeovnv1.IptablesEIP{Name: "eip0", Status: kubeovnv1.IptablesEIPStatus{IP: "172.20.0.5"}}
)

// newVipRouteController builds a controller serving gw0 with the share DNAT feature on, which is
// the only state syncNatGwVipStateForService reads besides the pods and rules given here.
func newVipRouteController(t *testing.T, pods []*v1.Pod, rules ...*kubeovnv1.IptablesDnatRule) *fakeController {
	t.Helper()
	if len(rules) == 0 {
		rules = []*kubeovnv1.IptablesDnatRule{ownedShareRule("rule-a", "eip0", "10.96.1.5")}
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways:    []*kubeovnv1.VpcNatGateway{vipRouteGw},
		Pods:              pods,
		IptablesEips:      []*kubeovnv1.IptablesEIP{vipRouteEip},
		IptablesDnatRules: rules,
	})
	require.NoError(t, err)
	fc.fakeController.config.EnableGwNftableLbSvc = true
	return fc
}

// expectVipRoutesAdded records the matches and next hops of the policies the reconciler adds.
func expectVipRoutesAdded(fc *fakeController, times int) map[string][]string {
	added := map[string][]string{}
	fc.mockOvnClient.EXPECT().AddLogicalRouterPolicy("vpc0", util.NatGatewayVipPolicyPriority, gomock.Any(), string(kubeovnv1.PolicyRouteActionReroute), gomock.Any(), nil, natGwVipRouteExternalIDs("gw0")).
		DoAndReturn(func(_ string, _ int, match, _ string, nextHops, _ []string, _ map[string]string) error {
			added[match] = nextHops
			return nil
		}).Times(times)
	return added
}

func Test_syncNatGwVipStateForService(t *testing.T) {
	t.Parallel()

	externalIDs := natGwVipRouteExternalIDs("gw0")

	t.Run("creates a route per VIP", func(t *testing.T) {
		fc := newVipRouteController(t, gatewayPods("gw0", "10.0.7.254"))
		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).Return(nil, nil)
		added := expectVipRoutesAdded(fc, 2)

		require.NoError(t, fc.fakeController.syncNatGwVipStateForService("gw0", "", nil, ""))
		require.Equal(t, map[string][]string{
			"ip4.dst == 172.20.0.5": {"10.0.7.254"},
			"ip4.dst == 10.96.1.5":  {"10.0.7.254"},
		}, added)
	})

	t.Run("removes routes that no rule references and refreshes the next hops", func(t *testing.T) {
		fc := newVipRouteController(t, gatewayPods("gw0", "10.0.7.254"))
		stale := &ovnnb.LogicalRouterPolicy{UUID: "stale-uuid", Priority: util.NatGatewayVipPolicyPriority, Match: "ip4.dst == 10.96.1.9"}
		// Keep the expected next hop but drift the action: comparing nexthops alone would leave
		// this policy as "allow" forever and the VIP would bypass the gateway.
		drifted := &ovnnb.LogicalRouterPolicy{
			UUID: "drifted-uuid", Priority: util.NatGatewayVipPolicyPriority, Match: "ip4.dst == 10.96.1.5",
			Action: "allow", Nexthops: []string{"10.0.7.254"},
		}
		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).
			Return([]*ovnnb.LogicalRouterPolicy{stale, drifted}, nil)
		fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicyByUUID("vpc0", "stale-uuid").Return(nil)
		updated := map[string][]string{}
		fc.mockOvnClient.EXPECT().UpdateLogicalRouterPolicy(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(policy *ovnnb.LogicalRouterPolicy, _ ...any) error {
				require.Equal(t, string(kubeovnv1.PolicyRouteActionReroute), policy.Action)
				updated[policy.Match] = policy.Nexthops
				return nil
			}).Times(1)
		// the EIP's route did not exist yet
		fc.mockOvnClient.EXPECT().AddLogicalRouterPolicy("vpc0", util.NatGatewayVipPolicyPriority, "ip4.dst == 172.20.0.5",
			string(kubeovnv1.PolicyRouteActionReroute), []string{"10.0.7.254"}, nil, externalIDs).Return(nil)

		require.NoError(t, fc.fakeController.syncNatGwVipStateForService("gw0", "", nil, ""))
		require.Equal(t, 29210, util.NatGatewayVipPolicyPriority)
		require.Equal(t, map[string][]string{"ip4.dst == 10.96.1.5": {"10.0.7.254"}}, updated)
	})

	// This is what the gateway-move path in handleUpdateIptablesDnatRule relies on: once the rule's
	// label points at its new gateway, reconciling the one it left drops the routes it was the last
	// user of, instead of leaving them steering its addresses to a gateway that no longer serves it.
	t.Run("a rule that moved to another gateway leaves no route behind", func(t *testing.T) {
		moved := ownedShareRule("rule-a", "eip0", "10.96.1.5")
		moved.Labels[util.VpcNatGatewayNameLabel] = "gw1"
		fc := newVipRouteController(t, gatewayPods("gw0", "10.0.7.254"), moved)
		left := []*ovnnb.LogicalRouterPolicy{
			{UUID: "clusterip-uuid", Priority: util.NatGatewayVipPolicyPriority, Match: "ip4.dst == 10.96.1.5"},
			{UUID: "eip-uuid", Priority: util.NatGatewayVipPolicyPriority, Match: "ip4.dst == 172.20.0.5"},
		}
		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).Return(left, nil)
		var deleted []string
		fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicyByUUID("vpc0", gomock.Any()).
			DoAndReturn(func(_, uuid string) error {
				deleted = append(deleted, uuid)
				return nil
			}).Times(2)

		require.NoError(t, fc.fakeController.syncNatGwVipStateForService("gw0", "", nil, ""))
		require.ElementsMatch(t, []string{"clusterip-uuid", "eip-uuid"}, deleted)
	})
}

// Test_syncNatGwVipStateNextHops pins which gateway instances a VIP route points at: every ready
// replica with an IPv4 address, and no route at all while there is none.
func Test_syncNatGwVipStateNextHops(t *testing.T) {
	t.Parallel()

	externalIDs := natGwVipRouteExternalIDs("gw0")

	t.Run("routes a VIP to every ready instance (ECMP)", func(t *testing.T) {
		// An HA gateway serves the VIP from any replica, so the route must carry all of them.
		fc := newVipRouteController(t, gatewayPodsOnNodes("gw0", map[string]string{
			"node0": "10.0.7.254",
			"node1": "10.0.7.253",
		}))
		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).Return(nil, nil)
		added := expectVipRoutesAdded(fc, 2)

		require.NoError(t, fc.fakeController.syncNatGwVipStateForService("gw0", "", nil, ""))
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
		fc := newVipRouteController(t, pods)
		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).Return(nil, nil)
		added := expectVipRoutesAdded(fc, 2)

		require.NoError(t, fc.fakeController.syncNatGwVipStateForService("gw0", "", nil, ""))
		ready := added["ip4.dst == 10.96.1.5"]
		require.Equal(t, []string{"10.0.7.254"}, ready, "a not ready replica must not be a next hop")
	})

	t.Run("drops every route while no instance has an IPv4 address", func(t *testing.T) {
		// a gateway whose VPC-side address is IPv6 only has no data plane for the IPv4 share DNAT,
		// so an ip4.dst route must not be left pointing at its IPv6 address
		v6Only := gatewayPods("gw0", "fd00::10")
		v6Only[0].Status.PodIPs = []v1.PodIP{{IP: "fd00::10"}}
		v6Only[0].Status.PodIP = "fd00::10"
		fc := newVipRouteController(t, v6Only)
		existing := &ovnnb.LogicalRouterPolicy{UUID: "u1", Priority: util.NatGatewayVipPolicyPriority, Match: "ip4.dst == 10.96.1.5"}
		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).
			Return([]*ovnnb.LogicalRouterPolicy{existing}, nil)
		fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicyByUUID("vpc0", "u1").Return(nil)

		require.NoError(t, fc.fakeController.syncNatGwVipStateForService("gw0", "", nil, ""))
	})

	t.Run("drops every route while no instance is reachable", func(t *testing.T) {
		notReady := gatewayPods("gw0", "10.0.7.254")
		notReady[0].Status.Conditions = []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse}}
		fc := newVipRouteController(t, notReady)
		existing := &ovnnb.LogicalRouterPolicy{UUID: "u1", Priority: util.NatGatewayVipPolicyPriority, Match: "ip4.dst == 10.96.1.5"}
		fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).
			Return([]*ovnnb.LogicalRouterPolicy{existing}, nil)
		fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicyByUUID("vpc0", "u1").Return(nil)

		require.NoError(t, fc.fakeController.syncNatGwVipStateForService("gw0", "", nil, ""))
	})
}

// Test_syncNatGwVipStateDisabled pins the feature gate: with --enable-gw-nftable-lb-svc off the
// feature owns no VIP, so the sync must not touch OVN at all (the mock client fails any call).
// Test_syncNatGwVipStateDisabled pins that the VIP routes are still reconciled with the feature
// disabled: the disabled-mode drain calls this path to remove the routes of leftovers, so the
// routes must converge instead of being skipped.
func Test_syncNatGwVipStateDisabled(t *testing.T) {
	t.Parallel()

	gw := &kubeovnv1.VpcNatGateway{Name: "gw0", Spec: kubeovnv1.VpcNatGatewaySpec{Vpc: "vpc0", Subnet: "subnet0"}}
	eip := &kubeovnv1.IptablesEIP{Name: "eip0", Status: kubeovnv1.IptablesEIPStatus{IP: "172.20.0.5"}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways:    []*kubeovnv1.VpcNatGateway{gw},
		Pods:              gatewayPods("gw0", "10.0.7.254"),
		IptablesEips:      []*kubeovnv1.IptablesEIP{eip},
		IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{ownedShareRule("rule-a", "eip0", "10.96.1.5")},
	})
	require.NoError(t, err)
	c := fc.fakeController
	c.config.EnableGwNftableLbSvc = false

	externalIDs := natGwVipRouteExternalIDs("gw0")
	fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies("vpc0", util.NatGatewayVipPolicyPriority, externalIDs, false).Return(nil, nil)
	fc.mockOvnClient.EXPECT().AddLogicalRouterPolicy("vpc0", util.NatGatewayVipPolicyPriority, gomock.Any(),
		string(kubeovnv1.PolicyRouteActionReroute), gomock.Any(), nil, externalIDs).Return(nil).Times(2)

	require.NoError(t, c.syncNatGwVipStateForService("gw0", "", nil, ""))
}

// TestHandleAddOrUpdateVpcNatGwSyncsVipRoutes pins the gateway side convergence of the share DNAT
// VIP routes: an HA scale down removes an instance without creating a new one, so no DNAT rule is
// rewritten and the only chance to drop the gone instance from the routes is the gateway reconcile.
// natGwInstancePod is a gateway instance Pod the controller accepts as ready and initialized.
func natGwInstancePod(name, namespace, gwName, ip string, labels map[string]string, stsUID types.UID) *v1.Pod {
	return &v1.Pod{
		Name: name, Namespace: namespace, Labels: labels,
		Annotations: map[string]string{
			util.IPAddressAnnotation:         ip,
			util.VpcNatGatewayInitAnnotation: "true",
			util.VpcNatGatewayAnnotation:     gwName,
		},
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(appsv1.SchemeGroupVersion.String(), util.KindStatefulSet, util.GenNatGwName(gwName), stsUID)},
		Spec:            v1.PodSpec{NodeName: "node-1"},
		Status: v1.PodStatus{
			Phase:      v1.PodRunning,
			PodIPs:     []v1.PodIP{{IP: ip}},
			Conditions: []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionTrue}},
			ContainerStatuses: []v1.ContainerStatus{{
				Name: "vpc-nat-gw", ContainerID: "containerd://sync-instance",
				State: v1.ContainerState{Running: &v1.ContainerStateRunning{}},
			}},
		},
	}
}

// natGwVipRouteSyncController builds a running gateway that serves one LoadBalancer Service
// through a share DNAT record, which is the state the gateway reconcile reads.
func natGwVipRouteSyncController(t *testing.T) (*Controller, string) {
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
	pod := natGwInstancePod(stsName+"-0", namespace, gwName, liveIP, gwLabels, sts.UID)
	svc := &v1.Service{
		Namespace: "default", Name: "sync-service",
		Annotations: map[string]string{util.VpcNatGatewayAnnotation: gwName, util.EipAnnotation: eipName},
		Spec:        v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer},
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
			EIP: eipName, ClusterIP: clusterIP,
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
		Services:          []*v1.Service{svc},
		IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{rule},
	})
	require.NoError(t, err)
	controller := fakeController.fakeController
	controller.config.EnableGwNftableLbSvc = true
	controller.config.EnableOvnLB = true
	controller.addOrUpdateGwNftableLbSvcQueue = newTypedRateLimitingQueue[string]("gw-nftable-lb-svc", nil)
	t.Cleanup(controller.addOrUpdateGwNftableLbSvcQueue.ShutDown)

	vpcNatEnabled = "true"
	t.Cleanup(func() { vpcNatEnabled = "unknown" })
	controller.serviceCIDRStore = util.NewServiceCIDRStore("10.96.0.0/12")
	return controller, gwName
}

func TestHandleAddOrUpdateVpcNatGwSyncsVipRoutes(t *testing.T) {
	controller, gwName := natGwVipRouteSyncController(t)

	// Gateway reconcile only re-enqueues the bound Service. The Service is the sole writer of
	// share-DNAT VIP routes, so no OVN mock is expected here.
	require.NoError(t, controller.handleUpdateVpcDnat(gwName))
	require.Equal(t, 1, controller.addOrUpdateGwNftableLbSvcQueue.Len())
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
			UID:       "owned-web-uid",
			Annotations: map[string]string{
				util.EipAnnotation:           eipName,
				util.VpcNatGatewayAnnotation: gwName,
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

func TestNftableLbServiceCreatedBeforeEip(t *testing.T) {
	for name, serveClusterIP := range map[string]bool{"EIP only": false, "EIP and ClusterIP": true} {
		t.Run(name, func(t *testing.T) {
			f := newNftableLbSvcOwnershipFixture()
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Vpcs:           []*kubeovnv1.Vpc{f.vpc},
				VpcNatGateways: []*kubeovnv1.VpcNatGateway{f.gw},
				Services:       []*v1.Service{f.svc},
			})
			require.NoError(t, err)
			c := fc.fakeController
			c.config.EnableGwNftableLbSvc = true
			c.config.EnableGwNftableSvcClusterIP = serveClusterIP
			eipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			c.iptablesEipsLister = kubeovnlister.NewIptablesEIPLister(eipIndexer)
			c.addOrUpdateGwNftableLbSvcQueue = newTypedRateLimitingQueue[string]("service-before-eip", nil)
			t.Cleanup(c.addOrUpdateGwNftableLbSvcQueue.ShutDown)
			key := f.namespace + "/" + f.svc.Name

			queue := c.addOrUpdateGwNftableLbSvcQueue
			queue.Add(key)
			require.True(t, processNextWorkItem("service-before-eip", queue, c.handleAddOrUpdateGwNftableLbService, getWorkItemKey))
			require.Zero(t, queue.NumRequeues(key), "cleanup succeeds without a rate-limited error retry")
			require.Eventually(t, func() bool { return queue.Len() == 1 },
				5*time.Second, 10*time.Millisecond, "worker Forget/Done must preserve the delayed retry")
			require.True(t, processNextWorkItem("service-before-eip", queue, c.handleAddOrUpdateGwNftableLbService, getWorkItemKey))
			require.Zero(t, queue.NumRequeues(key))
			require.Eventually(t, func() bool { return queue.Len() == 1 },
				5*time.Second, 10*time.Millisecond, "keep retrying while the EIP is still missing")

			// Make the EIP visible without delivering any EIP, Service or EndpointSlice event.
			require.NoError(t, eipIndexer.Add(f.eip))
			require.True(t, processNextWorkItem("service-before-eip", queue, c.handleAddOrUpdateGwNftableLbService, getWorkItemKey))
			require.Zero(t, queue.NumRequeues(key))
			updated, err := c.config.KubeClient.CoreV1().Services(f.namespace).Get(context.Background(), f.svc.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Contains(t, updated.Finalizers, util.KubeOVNControllerFinalizer, "the queued retry resumes programming")

			program, cleanup, err := c.resolveNftableLbSvcProgram(key, updated, true, serveClusterIP)
			require.NoError(t, err)
			require.False(t, cleanup)
			require.Equal(t, f.eip.Status.IP, program.eipIP)
			desired := buildDesiredNftableLbDnatRulesForIdentities(updated, program.eipName, program.gateway,
				program.serveEIP, program.serveClusterIP, []*discoveryv1.EndpointSlice{f.slice},
				func(endpoint discoveryv1.Endpoint) (string, bool) { return endpoint.Addresses[0], true })
			identities := buildNftableLbIdentities(desired, program.eipIP)
			require.Contains(t, identities, nftableLbDnatIdentity(f.eip.Status.IP, "80", "tcp"))
			if serveClusterIP {
				require.Contains(t, identities, nftableLbDnatIdentity(f.svc.Spec.ClusterIP, "80", "tcp"))
			} else {
				require.Len(t, identities, 1)
			}
		})
	}
}

func TestNftableLbServiceClaimsFinalizerBeforeProgramming(t *testing.T) {
	f := newNftableLbSvcOwnershipFixture()

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs:           []*kubeovnv1.Vpc{f.vpc},
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{f.gw},
		Subnets:        []*kubeovnv1.Subnet{f.subnet},
		Services:       []*v1.Service{f.svc},
		EndpointSlices: []*discoveryv1.EndpointSlice{f.slice},
		IptablesEips:   []*kubeovnv1.IptablesEIP{f.eip},
	})
	require.NoError(t, err)
	c := fc.fakeController
	c.config.EnableGwNftableLbSvc = true

	require.NoError(t, c.handleAddOrUpdateGwNftableLbService(f.namespace+"/"+f.svc.Name))
	updated, err := c.config.KubeClient.CoreV1().Services(f.namespace).Get(context.Background(), f.svc.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, updated.Status.LoadBalancer.Ingress)
	require.Contains(t, updated.Finalizers, util.KubeOVNControllerFinalizer)
}

func clusterIPServedRule(name, gateway, clusterIP string) *kubeovnv1.IptablesDnatRule {
	return &kubeovnv1.IptablesDnatRule{
		Name: name,
		Labels: map[string]string{
			util.NftableLbSvcNsLabel:     "default",
			util.NftableLbSvcNameLabel:   "web",
			util.NftableLbSvcRecordLabel: "true",
			util.VpcNatGatewayNameLabel:  gateway,
			util.VpcDnatEPortLabel:       "80",
		},
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			ClusterIP:    clusterIP,
			ExternalPort: "80", Protocol: "tcp",
			InternalIP: "10.0.7.2", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
		},
	}
}

// Test_desiredNatGwVipStateForService_clusterIPServedRules pins that a rule serving a ClusterIP owns its
// address on its own: without an owner label it still contributes the VIP route and the address to
// hold on lo, otherwise the rule would program a share DNAT identity nothing routes to.
func Test_desiredNatGwVipStateForService_clusterIPServedRules(t *testing.T) {
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

	vips, clusterIPs, err := controller.desiredNatGwVipStateForService(gwName, "", nil, "")
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
	require.NoError(t, ruleIndexer.Update(terminating))
	vips, clusterIPs, err = controller.desiredNatGwVipStateForService(gwName, "", nil, "")
	require.NoError(t, err)
	require.NotContains(t, vips, "ip4.dst == 10.96.1.5")
	require.Equal(t, []string{"10.96.1.6"}, clusterIPs)
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
				util.VpcNatGatewayAnnotation: "gw0",
				util.EipAnnotation:           eip,
			},
			Spec: v1.ServiceSpec{
				Type:       svcType,
				ClusterIP:  "10.96.1.5",
				ClusterIPs: []string{"10.96.1.5"},
				Ports:      []v1.ServicePort{{Name: "http", Port: 80, Protocol: v1.ProtocolTCP}},
			},
		}
	}

	// ClusterIP Service: no EIP at all, so its record carries the internal VIP.
	desired := buildDesiredNftableLbDnatRules(newSvc(v1.ServiceTypeClusterIP, ""), "", "gw0", endpointSlices, testNftableLbBackendIP)
	require.Len(t, desired, 1)
	for _, rule := range desired {
		require.Empty(t, rule.Spec.EIP, "a ClusterIP Service has no public address")
		require.Equal(t, "10.96.1.5", rule.Spec.ClusterIP)
		require.Equal(t, kubeovnv1.DnatRuleTypeShare, rule.Spec.Type)
	}

	// LoadBalancer Service: both addresses on the same rule, so the two VIPs of one Service port
	// stay aligned and are programmed and released together.
	desired = buildDesiredNftableLbDnatRules(newSvc(v1.ServiceTypeLoadBalancer, "eip0"), "eip0", "gw0", endpointSlices, testNftableLbBackendIP)
	require.Len(t, desired, 1)
	for _, rule := range desired {
		require.Equal(t, "eip0", rule.Spec.EIP)
		require.Equal(t, "10.96.1.5", rule.Spec.ClusterIP)
	}
}

func Test_buildDesiredNftableLbDnatRules_featureGates(t *testing.T) {
	t.Parallel()

	svc := &v1.Service{
		Namespace: "default", Name: "web",
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer, ClusterIP: "10.96.1.5", ClusterIPs: []string{"10.96.1.5"},
			Ports: []v1.ServicePort{{Name: "http", Port: 80, Protocol: v1.ProtocolTCP}},
		},
	}
	endpointSlices := []*discoveryv1.EndpointSlice{{
		Ports: []discoveryv1.EndpointPort{{Name: new("http"), Port: new(int32(8080))}},
		Endpoints: []discoveryv1.Endpoint{{
			Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: new(true)},
		}},
	}}
	tests := []struct {
		name                          string
		enableEIP, enableClusterIP    bool
		wantEIP, wantClusterIP        string
		wantIdentities, wantRuleCount int
	}{
		{name: "both disabled"},
		{name: "eip only", enableEIP: true, wantEIP: "eip0", wantIdentities: 1, wantRuleCount: 1},
		{name: "cluster ip only", enableClusterIP: true, wantClusterIP: "10.96.1.5", wantIdentities: 1, wantRuleCount: 1},
		{name: "both enabled", enableEIP: true, enableClusterIP: true, wantEIP: "eip0", wantClusterIP: "10.96.1.5", wantIdentities: 2, wantRuleCount: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := buildDesiredNftableLbDnatRulesForIdentities(svc, "eip0", "gw0", tt.enableEIP, tt.enableClusterIP, endpointSlices, testNftableLbBackendIP)
			require.Len(t, desired, tt.wantRuleCount)
			require.Len(t, buildNftableLbIdentities(desired, "203.0.113.10"), tt.wantIdentities)
			for _, rule := range desired {
				require.Equal(t, tt.wantEIP, rule.Spec.EIP)
				require.Equal(t, tt.wantClusterIP, rule.Spec.ClusterIP)
			}
		})
	}
}

// Test_handleAddOrUpdateGwNftableLbService_clusterIPService pins the ClusterIP Service reconcile:
// the gateway comes from the annotation, no EIP is looked up, and the generated rule is the one
// that serves the internal VIP.
func Test_handleAddOrUpdateGwNftableLbService_clusterIPService(t *testing.T) {
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
		Annotations: map[string]string{util.VpcNatGatewayAnnotation: gwName},
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
	c.config.EnableGwNftableSvcClusterIP = true
	c.addOrUpdateGwNftableLbSvcQueue = newTypedRateLimitingQueue[string]("test-gw-nftable-lb-svc", nil)
	t.Cleanup(c.addOrUpdateGwNftableLbSvcQueue.ShutDown)

	require.NoError(t, c.handleAddOrUpdateGwNftableLbService(namespace+"/"+svcName))
	require.Eventually(t, func() bool { return c.addOrUpdateGwNftableLbSvcQueue.Len() == 1 },
		2*time.Second, 50*time.Millisecond, "finalizer claim schedules the programming pass")

	updated, err := c.config.KubeClient.CoreV1().Services(namespace).Get(context.Background(), svcName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, updated.Finalizers, util.KubeOVNControllerFinalizer)

	// No accounting record exists before the gateway was programmed.
	rules, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().List(context.Background(),
		metav1.ListOptions{LabelSelector: util.NftableLbSvcNameLabel + "=" + svcName})
	require.NoError(t, err)
	require.Empty(t, rules.Items)
}

func TestClearNftableLbSvcIngressIP(t *testing.T) {
	t.Parallel()

	svc := &v1.Service{
		Namespace:  metav1.NamespaceDefault,
		Name:       "web",
		Finalizers: []string{util.KubeOVNControllerFinalizer},
		Status: v1.ServiceStatus{LoadBalancer: v1.LoadBalancerStatus{
			Ingress: []v1.LoadBalancerIngress{{IP: "203.0.113.10"}},
		}},
	}
	client := k8sfake.NewSimpleClientset(svc)
	c := &Controller{config: &Configuration{KubeClient: client}}

	require.NoError(t, c.clearNftableLbSvcIngressIP(svc))
	actions := client.Actions()
	require.Len(t, actions, 1)
	require.Equal(t, "status", actions[0].GetSubresource())
	updated, err := client.CoreV1().Services(svc.Namespace).Get(context.Background(), svc.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, updated.Status.LoadBalancer.Ingress)
	require.Contains(t, updated.Finalizers, util.KubeOVNControllerFinalizer)
}

// TestCleanupNftableLbServiceIgnoresPredecessorRules pins what a record of a previous incarnation
// of the Service may decide: it is deleted like any other record of this namespace/name, but it is
// not evidence that this Service published an ingress IP, so the cleanup must not clear it.
func TestCleanupNftableLbServiceIgnoresPredecessorRules(t *testing.T) {
	f := newNftableLbSvcOwnershipFixture()
	f.svc.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{{IP: "172.20.0.9"}}

	stale := buildDesiredNftableLbDnatRules(f.svc, f.eip.Name, f.gw.Name, []*discoveryv1.EndpointSlice{f.slice}, testNftableLbBackendIP)
	require.Len(t, stale, 1)
	staleRule := slices.Collect(maps.Values(stale))[0]
	staleRule.Labels[util.NftableLbSvcUIDLabel] = "a-previous-incarnation"

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs:              []*kubeovnv1.Vpc{f.vpc},
		VpcNatGateways:    []*kubeovnv1.VpcNatGateway{f.gw},
		Subnets:           []*kubeovnv1.Subnet{f.subnet},
		Services:          []*v1.Service{f.svc},
		IptablesEips:      []*kubeovnv1.IptablesEIP{f.eip},
		IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{staleRule},
	})
	require.NoError(t, err)
	c := fc.fakeController
	c.config.EnableGwNftableLbSvc = true

	require.NoError(t, c.cleanupNftableLbService(f.svc, f.namespace, f.svc.Name))

	updated, err := c.config.KubeClient.CoreV1().Services(f.namespace).Get(context.Background(), f.svc.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, []v1.LoadBalancerIngress{{IP: "172.20.0.9"}}, updated.Status.LoadBalancer.Ingress,
		"a predecessor's rule must not authorize clearing this Service's ingress IP")

	_, err = c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Get(context.Background(), staleRule.Name, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err), "the predecessor's rule must still be deleted")
}

// Test_handleAddOrUpdateGwNftableLbService_disabledDrainsFinalizers pins the disabled-mode drain:
// with both gateway gates off, a Service that still carries the controller finalizer and its
// accounting records must still be released, otherwise deleting it leaves it terminating forever.
func Test_handleAddOrUpdateGwNftableLbService_disabledDrainsFinalizers(t *testing.T) {
	f := newNftableLbSvcOwnershipFixture()
	f.svc.Finalizers = []string{util.KubeOVNControllerFinalizer}
	f.svc.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{{IP: f.eip.Status.IP}}

	desired := buildDesiredNftableLbDnatRules(f.svc, f.eip.Name, f.gw.Name, []*discoveryv1.EndpointSlice{f.slice}, testNftableLbBackendIP)
	require.Len(t, desired, 1)
	rule := slices.Collect(maps.Values(desired))[0]
	rule.Finalizers = []string{util.NftableLbSvcRecordFinalizer}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs:              []*kubeovnv1.Vpc{f.vpc},
		VpcNatGateways:    []*kubeovnv1.VpcNatGateway{f.gw},
		Subnets:           []*kubeovnv1.Subnet{f.subnet},
		Services:          []*v1.Service{f.svc},
		IptablesEips:      []*kubeovnv1.IptablesEIP{f.eip},
		IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{rule},
	})
	require.NoError(t, err)
	c := fc.fakeController
	// Both gateway gates stay off, which is what makes this the drain path.

	require.NoError(t, c.handleAddOrUpdateGwNftableLbService(f.namespace+"/"+f.svc.Name))

	updated, err := c.config.KubeClient.CoreV1().Services(f.namespace).Get(context.Background(), f.svc.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotContains(t, updated.Finalizers, util.KubeOVNControllerFinalizer,
		"the disabled drain must remove the controller finalizer")
	require.Empty(t, updated.Status.LoadBalancer.Ingress, "the disabled drain must clear the published ingress IP")

	_, err = c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Get(context.Background(), rule.Name, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err), "the disabled drain must delete the accounting record")
}

// Test_handleAddOrUpdateGwNftableLbService_disabledIgnoresUnmanaged pins that the drain never
// derives identities from a Service the feature did not program: without the finalizer and without
// records there is nothing to release, and the Service must be left untouched.
func Test_handleAddOrUpdateGwNftableLbService_disabledIgnoresUnmanaged(t *testing.T) {
	f := newNftableLbSvcOwnershipFixture()

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs:           []*kubeovnv1.Vpc{f.vpc},
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{f.gw},
		Subnets:        []*kubeovnv1.Subnet{f.subnet},
		Services:       []*v1.Service{f.svc},
		IptablesEips:   []*kubeovnv1.IptablesEIP{f.eip},
	})
	require.NoError(t, err)
	c := fc.fakeController

	require.NoError(t, c.handleAddOrUpdateGwNftableLbService(f.namespace+"/"+f.svc.Name))

	updated, err := c.config.KubeClient.CoreV1().Services(f.namespace).Get(context.Background(), f.svc.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, updated.Finalizers, "a Service the feature never managed must not gain a finalizer")
}

// Test_enqueueGwNftableLbSvcRecordOwners pins the startup replay that covers an orphaned record
// whose Service is already gone, which no share-record event would ever trigger.
func Test_enqueueGwNftableLbSvcRecordOwners(t *testing.T) {
	f := newNftableLbSvcOwnershipFixture()
	desired := buildDesiredNftableLbDnatRules(f.svc, f.eip.Name, f.gw.Name, []*discoveryv1.EndpointSlice{f.slice}, testNftableLbBackendIP)
	require.Len(t, desired, 1)
	rule := slices.Collect(maps.Values(desired))[0]
	rule.Finalizers = []string{util.NftableLbSvcRecordFinalizer}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{rule},
	})
	require.NoError(t, err)
	c := fc.fakeController
	c.addOrUpdateGwNftableLbSvcQueue = newTypedRateLimitingQueue[string]("test", nil)

	require.NoError(t, c.enqueueGwNftableLbSvcRecordOwners())
	require.Equal(t, 1, c.addOrUpdateGwNftableLbSvcQueue.Len())
	item, shutdown := c.addOrUpdateGwNftableLbSvcQueue.Get()
	require.False(t, shutdown)
	require.Equal(t, f.namespace+"/"+f.svc.Name, item)
}

// Test_enqueueGwNftableLbSvcRecordOwners_legacy pins the upgrade path: a record written by an
// earlier version carries only the owner labels and the generic controller finalizer. It must be
// replayed too, otherwise an orphan from before the upgrade keeps its identity and its EIP
// reference with nothing left that would ever reclaim them.
func TestEnqueueGwNftableLbSvcRecordOwnersLegacy(t *testing.T) {
	f := newNftableLbSvcOwnershipFixture()
	legacy := &kubeovnv1.IptablesDnatRule{
		Name: "lb-web-legacy",
		Labels: map[string]string{
			util.NftableLbSvcNsLabel:   f.namespace,
			util.NftableLbSvcNameLabel: f.svc.Name,
		},
		Finalizers: []string{util.KubeOVNControllerFinalizer},
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			Type: kubeovnv1.DnatRuleTypeShare, EIP: f.eip.Name,
			ExternalPort: "80", Protocol: "tcp", InternalIP: "10.0.7.2", InternalPort: "8080",
		},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{legacy},
	})
	require.NoError(t, err)
	c := fc.fakeController
	c.addOrUpdateGwNftableLbSvcQueue = newTypedRateLimitingQueue[string]("test", nil)

	require.NoError(t, c.enqueueGwNftableLbSvcRecordOwners())
	require.Equal(t, 1, c.addOrUpdateGwNftableLbSvcQueue.Len())
	item, _ := c.addOrUpdateGwNftableLbSvcQueue.Get()
	require.Equal(t, f.namespace+"/"+f.svc.Name, item)
}

// TestDeleteNftableLbRecordDropsLegacyFinalizer pins the other half of the upgrade path: nothing
// removes the generic controller finalizer from a share object any more (the DNAT workers skip
// them), so the record cleanup has to, or the record stays Terminating and its EIP stays in use.
func TestDeleteNftableLbRecordDropsLegacyFinalizer(t *testing.T) {
	legacy := &kubeovnv1.IptablesDnatRule{
		Name:       "lb-web-legacy",
		Finalizers: []string{util.KubeOVNControllerFinalizer, "third-party/keep"},
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			Type: kubeovnv1.DnatRuleTypeShare, EIP: "eip0",
			ExternalPort: "80", Protocol: "tcp", InternalIP: "10.0.7.2", InternalPort: "8080",
		},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{legacy},
	})
	require.NoError(t, err)
	c := fc.fakeController
	c.resetIptablesEipQueue = newTypedRateLimitingQueue[string]("test-eip", nil)
	t.Cleanup(c.resetIptablesEipQueue.ShutDown)

	require.NoError(t, c.deleteNftableLbRecord(legacy))

	// The fake client does not honour finalizers, so the Delete alone proves nothing: assert that
	// the update preceding it stripped the legacy finalizer and kept the third party one.
	var updated *kubeovnv1.IptablesDnatRule
	for _, action := range c.config.KubeOvnClient.(*kubeovnfake.Clientset).Actions() {
		if update, ok := action.(k8stesting.UpdateAction); ok && action.GetResource().Resource == "iptables-dnat-rules" {
			updated, _ = update.GetObject().(*kubeovnv1.IptablesDnatRule)
		}
	}
	require.NotNil(t, updated, "the record finalizers must be updated before the delete")
	require.Equal(t, []string{"third-party/keep"}, updated.Finalizers)
}

func TestBuildNftableLbIdentities(t *testing.T) {
	t.Parallel()

	records := map[string]*kubeovnv1.IptablesDnatRule{
		"a": {
			Spec: kubeovnv1.IptablesDnatRuleSpec{
				EIP: "eip0", ClusterIP: "10.96.1.5", ExternalPort: "80", Protocol: "tcp",
				InternalIP: "10.0.0.2", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
			},
		},
		"b": {
			Spec: kubeovnv1.IptablesDnatRuleSpec{
				EIP: "eip0", ClusterIP: "10.96.1.5", ExternalPort: "80", Protocol: "tcp",
				InternalIP: "10.0.0.3", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
			},
		},
	}

	got := buildNftableLbIdentities(records, "203.0.113.10")
	require.Len(t, got, 2)
	require.ElementsMatch(t, []string{"10.0.0.2:8080", "10.0.0.3:8080"}, got["203.0.113.10/80/tcp"].backends)
	require.ElementsMatch(t, []string{"10.0.0.2:8080", "10.0.0.3:8080"}, got["10.96.1.5/80/tcp"].backends)
}

// TestNftableLbHairpinRule pins the argument the gateway script gets for an identity: the same
// (vip, port, protocol) triple that names the nft map, for both addresses of a LoadBalancer record.
func TestNftableLbHairpinRule(t *testing.T) {
	t.Parallel()

	records := map[string]*kubeovnv1.IptablesDnatRule{
		"a": {Spec: kubeovnv1.IptablesDnatRuleSpec{
			EIP: "eip0", ClusterIP: "10.96.1.5", ExternalPort: "80", Protocol: "TCP",
			InternalIP: "10.0.0.2", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
		}},
	}
	identities := buildNftableLbIdentities(records, "203.0.113.10")
	require.Len(t, identities, 2)
	rules := make([]string, 0, len(identities))
	for _, key := range slices.Sorted(maps.Keys(identities)) {
		rules = append(rules, nftableLbHairpinRule(identities[key]))
		require.Equal(t, []string{"10.0.0.2:8080"}, identities[key].backends)
	}
	require.Equal(t, []string{"10.96.1.5,80,tcp", "203.0.113.10,80,tcp"}, rules)
}

func TestNftableLbExistingIdentities(t *testing.T) {
	t.Parallel()

	record := &kubeovnv1.IptablesDnatRule{
		Labels: map[string]string{util.EipV4IpLabel: "203.0.113.10"},
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			EIP: "eip0", ClusterIP: "10.96.1.5", ExternalPort: "80", Protocol: "TCP",
		},
	}
	got := nftableLbExistingIdentities([]*kubeovnv1.IptablesDnatRule{record})
	require.Contains(t, got, "203.0.113.10/80/tcp")
	require.Contains(t, got, "10.96.1.5/80/tcp")
}

// TestSyncNftableLbRecordsProtectsLedger pins that the accounting records cannot be emptied under a
// live Service: the controller finalizer holds a deletion request, the ledger survives while the
// identity is still desired, and the controller removes it only when the identity becomes stale.
func TestSyncNftableLbRecordsProtectsLedger(t *testing.T) {
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{})
	require.NoError(t, err)
	c := fc.fakeController

	svc := &v1.Service{Namespace: metav1.NamespaceDefault, Name: "web"}
	ctx := context.Background()
	records := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules()

	newDesired := func() map[string]*kubeovnv1.IptablesDnatRule {
		record := &kubeovnv1.IptablesDnatRule{
			Name: "lb-web-abc",
			Labels: map[string]string{
				util.NftableLbSvcNsLabel:     svc.Namespace,
				util.NftableLbSvcNameLabel:   svc.Name,
				util.NftableLbSvcRecordLabel: "true",
				util.VpcNatGatewayNameLabel:  "gw0",
			},
			Spec: kubeovnv1.IptablesDnatRuleSpec{
				EIP: "eip0", ClusterIP: "10.96.1.5", ExternalPort: "80", Protocol: "tcp",
				InternalIP: "10.0.0.2", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
			},
		}
		return map[string]*kubeovnv1.IptablesDnatRule{record.Name: record}
	}

	require.NoError(t, c.syncNftableLbRecords(svc, "203.0.113.10", "gw0", newDesired(), nil))
	created, err := records.Get(ctx, "lb-web-abc", metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, created.Finalizers, util.NftableLbSvcRecordFinalizer,
		"a programmed record must be protected by the controller finalizer")

	// Reconciling an already-programmed record keeps exactly the controller finalizer.
	require.NoError(t, c.syncNftableLbRecords(svc, "203.0.113.10", "gw0", newDesired(), []*kubeovnv1.IptablesDnatRule{created}))
	kept, err := records.Get(ctx, "lb-web-abc", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, []string{util.NftableLbSvcRecordFinalizer}, kept.Finalizers)

	// Once the identity is no longer desired the controller drops the finalizer and the record.
	require.NoError(t, c.syncNftableLbRecords(svc, "203.0.113.10", "gw0", map[string]*kubeovnv1.IptablesDnatRule{}, []*kubeovnv1.IptablesDnatRule{kept}))
	_, err = records.Get(ctx, "lb-web-abc", metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err), "a stale record must be removed for real")
}

// Test_nftableLbForeignEipIdentities pins the ownership guard the stale-identity teardown and the
// Service cleanup rely on: an identity another live Service owns must never be torn down, while an
// identity nobody owns any more still is.
func Test_nftableLbForeignEipIdentities(t *testing.T) {
	t.Parallel()

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		IndexGwNftableLbServiceByEip: indexGwNftableLbServiceByEip,
	})
	newService := func(name string, port int32) *v1.Service {
		return &v1.Service{
			Namespace: "ns", Name: name,
			Annotations: map[string]string{
				util.EipAnnotation:           "eip0",
				util.VpcNatGatewayAnnotation: "gw0",
			},
			Spec: v1.ServiceSpec{
				Type:  v1.ServiceTypeLoadBalancer,
				Ports: []v1.ServicePort{{Port: port, Protocol: v1.ProtocolTCP}},
			},
		}
	}
	winner := newService("a-winner", 80)
	require.NoError(t, indexer.Add(winner))
	require.NoError(t, indexer.Add(newService("z-loser", 80)))
	require.NoError(t, indexer.Add(newService("z-other", 81)))

	controller := &Controller{svcIndexer: indexer}
	declared := func(port string) map[string]struct{} {
		return map[string]struct{}{nftableLbDnatIdentity("eip0", port, "tcp"): {}}
	}

	// The loser still holds a stale record for the contested EIP:port.
	refs := []nftableLbEipIdentityRef{{
		eipName: "eip0", eipIP: "203.0.113.10", port: "80", protocol: "tcp",
	}}
	foreign, err := controller.nftableLbForeignEipIdentities("ns/z-loser", refs, declared("80"))
	require.NoError(t, err)
	require.Contains(t, foreign, "203.0.113.10/80/tcp", "the winner's program must be protected")

	// A Service that no longer declares an identity must not seed itself as its owner: the live
	// Service that still declares it owns it, even though it sorts after the caller.
	staleRemovedPort := []nftableLbEipIdentityRef{{
		eipName: "eip0", eipIP: "203.0.113.10", port: "81", protocol: "tcp",
	}}
	foreign, err = controller.nftableLbForeignEipIdentities("ns/a-winner", staleRemovedPort, nil)
	require.NoError(t, err)
	require.Contains(t, foreign, "203.0.113.10/81/tcp",
		"the live declarer owns the identity the caller no longer declares")

	// Once the last owner is gone, the identity is free to tear down.
	require.NoError(t, indexer.Delete(winner))
	foreign, err = controller.nftableLbForeignEipIdentities("ns/z-loser", refs, declared("80"))
	require.NoError(t, err)
	require.Empty(t, foreign, "self is the last owner and may reclaim its own identity")
}

// Test_nftableLbCleanupGateways pins the durable cleanup path: the Service names its gateway, so the
// gateway is cleaned even when every accounting record has already been deleted.
func Test_nftableLbCleanupGateways(t *testing.T) {
	t.Parallel()

	svc := &v1.Service{
		Namespace:   "ns",
		Name:        "web",
		Annotations: map[string]string{util.VpcNatGatewayAnnotation: "gw0"},
	}
	require.Equal(t, []string{"gw0"}, nftableLbCleanupGateways(svc, nil),
		"an empty ledger must still clean the Service's gateway")

	rule := &kubeovnv1.IptablesDnatRule{Labels: map[string]string{util.VpcNatGatewayNameLabel: "gw1"}}
	require.Equal(t, []string{"gw0", "gw1"}, nftableLbCleanupGateways(svc, []*kubeovnv1.IptablesDnatRule{rule}))

	// A Service already gone still cleans the gateways its records name.
	require.Equal(t, []string{"gw1"}, nftableLbCleanupGateways(nil, []*kubeovnv1.IptablesDnatRule{rule}))
}

func Test_nftableLbSvcSpecIdentities(t *testing.T) {
	t.Parallel()

	eipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, eipIndexer.Add(&kubeovnv1.IptablesEIP{
		Name:   "eip0",
		Status: kubeovnv1.IptablesEIPStatus{IP: "203.0.113.10"},
	}))
	controller := &Controller{iptablesEipsLister: kubeovnlister.NewIptablesEIPLister(eipIndexer)}

	svc := &v1.Service{
		Namespace: "ns", Name: "web",
		Annotations: map[string]string{
			util.EipAnnotation:           "eip0",
			util.VpcNatGatewayAnnotation: "gw0",
		},
		Spec: v1.ServiceSpec{
			Type:       v1.ServiceTypeLoadBalancer,
			ClusterIP:  "10.96.1.5",
			ClusterIPs: []string{"10.96.1.5"},
			Ports: []v1.ServicePort{
				{Name: "http", Port: 80, Protocol: v1.ProtocolTCP},
				{Name: "dns", Port: 53, Protocol: v1.ProtocolUDP},
				{Name: "sctp", Port: 9999, Protocol: v1.ProtocolSCTP},
			},
		},
	}
	eipRefs, clusterIPs := controller.nftableLbSvcSpecIdentities(svc)
	require.Equal(t, []string{"10.96.1.5/80/tcp", "10.96.1.5/53/udp"}, []string{
		nftableLbIdentityKey(clusterIPs[0]), nftableLbIdentityKey(clusterIPs[1]),
	}, "SCTP is not programmed by the share DNAT generation")
	require.Len(t, eipRefs, 2)
	require.Equal(t, "eip0", eipRefs[0].eipName)
	require.Equal(t, "203.0.113.10/80/tcp", nftableLbDnatIdentity(eipRefs[0].eipIP, eipRefs[0].port, eipRefs[0].protocol))

	// No EIP annotation: only the ClusterIP identities remain.
	svc.Annotations = map[string]string{util.VpcNatGatewayAnnotation: "gw0"}
	eipRefs, clusterIPs = controller.nftableLbSvcSpecIdentities(svc)
	require.Empty(t, eipRefs)
	require.Len(t, clusterIPs, 2)

	// An EIP that is already gone is not fatal: cleanup still reclaims the ClusterIP identity.
	svc.Annotations = map[string]string{util.EipAnnotation: "gone", util.VpcNatGatewayAnnotation: "gw0"}
	eipRefs, clusterIPs = controller.nftableLbSvcSpecIdentities(svc)
	require.Empty(t, eipRefs)
	require.Len(t, clusterIPs, 2)
}

// Test_nftableLbDeclaredEipIdentities pins that only the current EIP and the ports the generation
// programs are declared: a stale record of a removed port must not make the Service look like that
// identity's owner.
func Test_nftableLbDeclaredEipIdentities(t *testing.T) {
	t.Parallel()

	svc := &v1.Service{
		Annotations: map[string]string{util.EipAnnotation: "eip0"},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{Port: 80, Protocol: v1.ProtocolTCP},
				{Port: 53, Protocol: v1.ProtocolUDP},
				{Port: 9999, Protocol: v1.ProtocolSCTP},
			},
		},
	}
	require.Equal(t, map[string]struct{}{
		nftableLbDnatIdentity("eip0", "80", "tcp"): {},
		nftableLbDnatIdentity("eip0", "53", "udp"): {},
	}, nftableLbDeclaredEipIdentities(svc), "only the current EIP and tcp/udp ports are declared")

	svc.Annotations = nil
	require.Empty(t, nftableLbDeclaredEipIdentities(svc), "a Service without an EIP declares no EIP identity")
}

// nftableLbPeerService builds one of several co-existing Services of the same gateway.
func nftableLbPeerService(name, eip string, port int32) *v1.Service {
	return &v1.Service{
		Namespace: "ns", Name: name,
		Annotations: map[string]string{
			util.EipAnnotation:           eip,
			util.VpcNatGatewayAnnotation: "gw0",
		},
		Spec: v1.ServiceSpec{
			Type:  v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{{Port: port, Protocol: v1.ProtocolTCP}},
		},
	}
}

// nftableLbPeerRecord builds the accounting record a peer Service of the same gateway owns.
func nftableLbPeerRecord(name, owner, eip, eipIP, clusterIP, port string) *kubeovnv1.IptablesDnatRule {
	return &kubeovnv1.IptablesDnatRule{
		Name: name,
		Labels: map[string]string{
			util.NftableLbSvcNsLabel:     "ns",
			util.NftableLbSvcNameLabel:   owner,
			util.NftableLbSvcRecordLabel: "true",
			util.VpcNatGatewayNameLabel:  "gw0",
			util.VpcDnatEPortLabel:       port,
			util.EipV4IpLabel:            eipIP,
		},
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			EIP: eip, ClusterIP: clusterIP, ExternalPort: port, Protocol: "tcp",
			InternalIP: "10.0.7.2", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
		},
	}
}

// Test_resolveNftableLbConflicts_coexistingServices pins that several LoadBalancer Services of one
// gateway co-exist: only the exact (eip, port, protocol) identity is exclusive, so the same EIP on
// another port, another protocol, or another EIP entirely is not a conflict and every Service keeps
// programming its own identity.
func Test_resolveNftableLbConflicts_coexistingServices(t *testing.T) {
	t.Parallel()

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		IndexGwNftableLbServiceByEip: indexGwNftableLbServiceByEip,
	})
	sameEipPort80 := nftableLbPeerService("a-port80", "eip0", 80)
	sameEipPort81 := nftableLbPeerService("b-port81", "eip0", 81)
	otherEip := nftableLbPeerService("c-other-eip", "eip1", 80)
	udpPort80 := nftableLbPeerService("d-udp80", "eip0", 80)
	udpPort80.Spec.Ports[0].Protocol = v1.ProtocolUDP
	for _, svc := range []*v1.Service{sameEipPort80, sameEipPort81, otherEip, udpPort80} {
		require.NoError(t, indexer.Add(svc))
	}

	queue := newTypedRateLimitingQueue[string]("gw-nftable-lb-coexist-test", nil)
	t.Cleanup(queue.ShutDown)
	controller := &Controller{
		svcIndexer:                     indexer,
		recorder:                       record.NewFakeRecorder(4),
		addOrUpdateGwNftableLbSvcQueue: queue,
	}

	for _, svc := range []*v1.Service{sameEipPort80, sameEipPort81, otherEip, udpPort80} {
		protocol := strings.ToLower(string(svc.Spec.Ports[0].Protocol))
		desired := map[string]*kubeovnv1.IptablesDnatRule{
			"backend": {
				Labels: map[string]string{util.EipV4IpLabel: "203.0.113.10"},
				Spec: kubeovnv1.IptablesDnatRuleSpec{
					EIP:          svc.Annotations[util.EipAnnotation],
					ClusterIP:    "10.96.1.5",
					ExternalPort: strconv.Itoa(int(svc.Spec.Ports[0].Port)),
					Protocol:     protocol,
				},
			},
		}
		conflicted, err := controller.resolveNftableLbConflicts(svc, "ns/"+svc.Name, desired)
		require.NoError(t, err)
		require.False(t, conflicted, "service %s declares an identity no peer declares", svc.Name)
		require.Equal(t, svc.Annotations[util.EipAnnotation], desired["backend"].Spec.EIP,
			"service %s must keep programming its own eip identity", svc.Name)
		require.Zero(t, queue.Len(), "a service that owns its identity is not retried")
	}
}

// Test_nftableLbStaleIdentities pins the teardown scope that makes co-existing Services of one
// gateway independent: a reconcile removes only the identities its own records name and its desired
// state dropped. Another Service's identity, whether it shares the gateway, the EIP or neither, is
// not in that set, and an identity another Service owns now is skipped even when this Service still
// holds a record for it.
func Test_nftableLbStaleIdentities(t *testing.T) {
	t.Parallel()

	const eipIP = "203.0.113.10"
	// This Service serves 80 and 81 of the shared EIP plus its own ClusterIP.
	own := []*kubeovnv1.IptablesDnatRule{
		nftableLbPeerRecord("own-80", "web", "eip0", eipIP, "10.96.1.5", "80"),
		nftableLbPeerRecord("own-81", "web", "eip0", eipIP, "10.96.1.5", "81"),
	}
	wanted := map[string]struct{}{
		nftableLbDnatIdentity(eipIP, "80", "tcp"):       {},
		nftableLbDnatIdentity("10.96.1.5", "80", "tcp"): {},
	}

	// Both addresses of the dropped port are stale, and nothing else is: the Service keeps serving
	// port 80 through the same EIP and the same ClusterIP.
	stale := nftableLbStaleIdentities(wanted, own, nil)
	staleKeys := make([]string, 0, len(stale))
	for _, identity := range stale {
		staleKeys = append(staleKeys, nftableLbIdentityKey(identity))
	}
	require.Equal(t, []string{
		nftableLbDnatIdentity("10.96.1.5", "81", "tcp"),
		nftableLbDnatIdentity(eipIP, "81", "tcp"),
	}, staleKeys)

	// A peer's record of the same EIP, the same gateway or another EIP is never part of the input,
	// so no reconcile of this Service can reach it.
	peers := []*kubeovnv1.IptablesDnatRule{
		nftableLbPeerRecord("peer-82", "peer", "eip0", eipIP, "10.96.1.6", "82"),
		nftableLbPeerRecord("peer-other-eip", "peer2", "eip1", "203.0.113.11", "10.96.1.7", "80"),
	}
	for _, identity := range nftableLbStaleIdentities(wanted, own, nil) {
		for _, peer := range nftableLbExistingIdentities(peers) {
			require.NotEqual(t, nftableLbIdentityKey(peer), nftableLbIdentityKey(identity))
		}
	}

	// The contested case: this Service still holds the record, but the identity belongs to the
	// Service that won it.
	protected := map[string]struct{}{nftableLbDnatIdentity(eipIP, "81", "tcp"): {}}
	remaining := nftableLbStaleIdentities(wanted, own, protected)
	require.Len(t, remaining, 1)
	require.Equal(t, nftableLbDnatIdentity("10.96.1.5", "81", "tcp"), nftableLbIdentityKey(remaining[0]),
		"an eip identity another live service owns must survive this service's teardown, its own ClusterIP identity must not")
}

// TestSyncNftableLbRecordsIsScopedToItsService pins the ledger half of the same independence: the
// record sync of one Service only ever writes or deletes its own records, so a peer of the same
// gateway and EIP keeps its ledger (and with it its claim on the EIP) across any update.
func TestSyncNftableLbRecordsIsScopedToItsService(t *testing.T) {
	const eipIP = "203.0.113.10"
	peer := nftableLbPeerRecord("peer-81", "peer", "eip0", eipIP, "10.96.1.6", "81")
	peer.Finalizers = []string{util.NftableLbSvcRecordFinalizer}
	own := nftableLbPeerRecord("own-80", "web", "eip0", eipIP, "10.96.1.5", "80")
	own.Finalizers = []string{util.NftableLbSvcRecordFinalizer}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{peer, own},
	})
	require.NoError(t, err)
	c := fc.fakeController
	records := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules()
	ctx := context.Background()

	// The Service drops its only port: its record goes, the peer's record is untouched.
	svc := &v1.Service{Namespace: "ns", Name: "web"}
	require.NoError(t, c.syncNftableLbRecords(svc, eipIP, "gw0",
		map[string]*kubeovnv1.IptablesDnatRule{}, []*kubeovnv1.IptablesDnatRule{own}))

	_, err = records.Get(ctx, own.Name, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err), "the service's own stale record must be removed")
	kept, err := records.Get(ctx, peer.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, peer.Spec, kept.Spec, "a peer's record must not be rewritten")
	require.Equal(t, []string{util.NftableLbSvcRecordFinalizer}, kept.Finalizers,
		"a peer keeps its finalizer, and with it its claim on the shared eip")
}

// TestDesiredNatGwVipStateIsTheUnionOfCoexistingServices pins the gateway-wide half: the VIP
// addresses and routes of one gateway are the union of what every Service on it asks for, so a
// reconcile of one Service publishes its own VIP without dropping a peer's, whether the peer shares
// the EIP or uses another one.
func TestDesiredNatGwVipStateIsTheUnionOfCoexistingServices(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	ruleIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	eipIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for name, ip := range map[string]string{"eip0": "203.0.113.10", "eip1": "203.0.113.11"} {
		require.NoError(t, eipIndexer.Add(&kubeovnv1.IptablesEIP{
			Name: name, Status: kubeovnv1.IptablesEIPStatus{IP: ip},
		}))
	}
	controller := &Controller{
		iptablesDnatRulesLister: kubeovnlister.NewIptablesDnatRuleLister(ruleIndexer),
		iptablesEipsLister:      kubeovnlister.NewIptablesEIPLister(eipIndexer),
	}
	// Two peers of the same gateway: one shares the EIP on another port, one uses another EIP.
	require.NoError(t, ruleIndexer.Add(nftableLbPeerRecord("peer-81", "peer", "eip0", "203.0.113.10", "10.96.1.6", "81")))
	require.NoError(t, ruleIndexer.Add(nftableLbPeerRecord("peer2-80", "peer2", "eip1", "203.0.113.11", "10.96.1.7", "80")))

	own := map[string]*kubeovnv1.IptablesDnatRule{
		"own-80": nftableLbPeerRecord("own-80", "web", "eip0", "203.0.113.10", "10.96.1.5", "80"),
	}
	vips, clusterIPs, err := controller.desiredNatGwVipStateForService(gwName, "ns/web", own, "203.0.113.10")
	require.NoError(t, err)
	require.Equal(t, []string{"10.96.1.5", "10.96.1.6", "10.96.1.7"}, clusterIPs,
		"every service of the gateway keeps its internal VIP on lo")
	require.Equal(t, map[string]string{
		"ip4.dst == 203.0.113.10": "203.0.113.10",
		"ip4.dst == 203.0.113.11": "203.0.113.11",
		"ip4.dst == 10.96.1.5":    "10.96.1.5",
		"ip4.dst == 10.96.1.6":    "10.96.1.6",
		"ip4.dst == 10.96.1.7":    "10.96.1.7",
	}, vips)

	// The same Service releasing everything only removes its own VIP; the peers keep theirs,
	// including the EIP they share with it.
	vips, clusterIPs, err = controller.desiredNatGwVipStateForService(gwName, "ns/web", nil, "")
	require.NoError(t, err)
	require.Equal(t, []string{"10.96.1.6", "10.96.1.7"}, clusterIPs)
	require.Contains(t, vips, "ip4.dst == 203.0.113.10", "the peer still serves the shared eip")
	require.NotContains(t, vips, "ip4.dst == 10.96.1.5")
}

// TestVpcNatGwTerminationNotifiesServices pins the gateway half of the reference contract: when
// the gateway a Service is bound to starts terminating or disappears, that Service is enqueued so
// it can release its records, its published ingress IP and its finalizer. Nothing else would wake
// it, because this controller is only triggered by Service and EndpointSlice events. The
// notification only enqueues; the Service itself decides what to converge.
func TestVpcNatGwTerminationNotifiesServices(t *testing.T) {
	t.Parallel()

	const gwName = "gw0"
	now := metav1.Now()
	gw := &kubeovnv1.VpcNatGateway{Name: gwName, Spec: kubeovnv1.VpcNatGatewaySpec{Vpc: util.DefaultVpc}}
	terminating := gw.DeepCopy()
	terminating.DeletionTimestamp = &now
	terminating.Finalizers = []string{util.KubeOVNControllerFinalizer}

	newController := func(t *testing.T, svc *v1.Service, config *Configuration) (*Controller, workqueue.TypedRateLimitingInterface[string]) {
		t.Helper()
		indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
			IndexGwNftableLbServiceByGateway: indexGwNftableLbServiceByGateway,
		})
		require.NoError(t, indexer.Add(svc))
		queue := newTypedRateLimitingQueue[string]("GwNftableLbService", nil)
		delQueue := newTypedRateLimitingQueue[string]("DelVpcNatGateway", nil)
		addQueue := newTypedRateLimitingQueue[string]("AddOrUpdateVpcNatGateway", nil)
		qosQueue := newTypedRateLimitingQueue[string]("UpdateQoSPolicy", nil)
		t.Cleanup(func() {
			queue.ShutDown()
			delQueue.ShutDown()
			addQueue.ShutDown()
			qosQueue.ShutDown()
		})
		return &Controller{
			config:                         config,
			svcIndexer:                     indexer,
			addOrUpdateGwNftableLbSvcQueue: queue,
			delVpcNatGatewayQueue:          delQueue,
			addOrUpdateVpcNatGatewayQueue:  addQueue,
			updateQoSPolicyQueue:           qosQueue,
		}, queue
	}

	bound := func(mutate func(*v1.Service)) *v1.Service {
		svc := &v1.Service{
			Namespace: "ns", Name: "web",
			Annotations: map[string]string{
				util.VpcNatGatewayAnnotation: gwName,
				util.EipAnnotation:           "eip0",
			},
			Spec: v1.ServiceSpec{
				Type:  v1.ServiceTypeLoadBalancer,
				Ports: []v1.ServicePort{{Port: 80, Protocol: v1.ProtocolTCP}},
			},
		}
		if mutate != nil {
			mutate(svc)
		}
		return svc
	}
	enabled := &Configuration{EnableGwNftableLbSvc: true, PodNamespace: metav1.NamespaceSystem}

	t.Run("the delete event notifies the bound service", func(t *testing.T) {
		c, queue := newController(t, bound(nil), enabled)
		c.enqueueDeleteVpcNatGw(gw)
		require.Equal(t, 1, queue.Len())
	})

	t.Run("a terminating gateway notifies on add and update", func(t *testing.T) {
		c, queue := newController(t, bound(nil), enabled)
		c.enqueueAddVpcNatGw(terminating)
		require.Equal(t, 1, queue.Len())
		c.enqueueUpdateVpcNatGw(gw, terminating)
		require.Equal(t, 1, queue.Len(), "the same service is deduplicated by the queue")
	})

	t.Run("a live gateway update does not notify", func(t *testing.T) {
		c, queue := newController(t, bound(nil), enabled)
		updated := gw.DeepCopy()
		updated.Spec.Replicas = 2
		c.enqueueUpdateVpcNatGw(gw, updated)
		require.Zero(t, queue.Len(), "instance replacement is replayed through the dnat redo path")
	})

	t.Run("a service of another gateway is not notified", func(t *testing.T) {
		c, queue := newController(t, bound(func(svc *v1.Service) {
			svc.Annotations[util.VpcNatGatewayAnnotation] = "other-gw"
		}), enabled)
		c.enqueueDeleteVpcNatGw(gw)
		require.Zero(t, queue.Len())
	})

	// With the gates off the notification still has to reach a Service the feature holds, so the
	// disabled-mode drain can release it; a Service it never programmed stays untouched.
	t.Run("with the gates off only a leftover is notified", func(t *testing.T) {
		disabled := &Configuration{PodNamespace: metav1.NamespaceSystem}
		c, queue := newController(t, bound(nil), disabled)
		c.enqueueDeleteVpcNatGw(gw)
		require.Zero(t, queue.Len())

		c, queue = newController(t, bound(func(svc *v1.Service) {
			svc.Finalizers = []string{util.KubeOVNControllerFinalizer}
		}), disabled)
		c.enqueueDeleteVpcNatGw(gw)
		require.Equal(t, 1, queue.Len())
	})
}

// TestGwNftableLbServiceReleasesWhenGatewayIsGone pins the other half of the gateway reference
// contract: once notified, a Service whose gateway no longer exists releases everything it holds,
// its accounting records (and with them the EIP), the ingress IP it published and its finalizer.
// The gateway data plane went away with the gateway, so nothing is executed in a Pod.
func TestGwNftableLbServiceReleasesWhenGatewayIsGone(t *testing.T) {
	f := newNftableLbSvcOwnershipFixture()
	f.svc.Finalizers = []string{util.KubeOVNControllerFinalizer}
	f.svc.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{{IP: f.eip.Status.IP}}

	records := buildDesiredNftableLbDnatRules(f.svc, f.eip.Name, f.gw.Name, []*discoveryv1.EndpointSlice{f.slice}, testNftableLbBackendIP)
	require.Len(t, records, 1)
	record := slices.Collect(maps.Values(records))[0]
	record.Finalizers = []string{util.NftableLbSvcRecordFinalizer}

	// Everything the Service needs is there except the gateway it is bound to.
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs:              []*kubeovnv1.Vpc{f.vpc},
		Subnets:           []*kubeovnv1.Subnet{f.subnet},
		Services:          []*v1.Service{f.svc},
		EndpointSlices:    []*discoveryv1.EndpointSlice{f.slice},
		IptablesEips:      []*kubeovnv1.IptablesEIP{f.eip},
		IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{record},
	})
	require.NoError(t, err)
	c := fc.fakeController
	c.config.EnableGwNftableLbSvc = true
	c.nftableLbVipSnapshot = map[string]map[string]nftableLbSvcVips{
		f.gw.Name: {f.namespace + "/" + f.svc.Name: {eipIP: f.eip.Status.IP}},
	}

	require.NoError(t, c.handleAddOrUpdateGwNftableLbService(f.namespace+"/"+f.svc.Name))

	_, err = c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Get(context.Background(), record.Name, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err), "the record must be deleted so the eip stops being in use")

	updated, err := c.config.KubeClient.CoreV1().Services(f.namespace).Get(context.Background(), f.svc.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, updated.Status.LoadBalancer.Ingress, "the ingress ip of a gone gateway must not stay published")
	require.NotContains(t, updated.Finalizers, util.KubeOVNControllerFinalizer, "the service must be released")
	require.Empty(t, c.nftableLbVipSnapshot, "the gateway floor must be dropped with the service")
}

// TestNftableLbIdentityKeyIsSingleSourced pins the invariant the desired/existing comparison rests
// on: the key of an identity map entry, the key derived from the identity it holds, and the key a
// caller builds from (vip, port, protocol) are one and the same string, for the desired side and
// the existing side alike. Protocol case and both addresses of a LoadBalancer record are covered.
func TestNftableLbIdentityKeyIsSingleSourced(t *testing.T) {
	t.Parallel()

	const eipIP = "203.0.113.10"
	rule := &kubeovnv1.IptablesDnatRule{
		Name: "lb-web-abc",
		Labels: map[string]string{
			util.NftableLbSvcNsLabel: "ns", util.NftableLbSvcNameLabel: "web",
			util.NftableLbSvcRecordLabel: "true", util.EipV4IpLabel: eipIP,
		},
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			EIP: "eip0", ClusterIP: "10.96.1.5", ExternalPort: "80", Protocol: "TCP",
			InternalIP: "10.0.7.2", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
		},
	}

	desired := buildNftableLbIdentities(map[string]*kubeovnv1.IptablesDnatRule{rule.Name: rule}, eipIP)
	existing := nftableLbExistingIdentities([]*kubeovnv1.IptablesDnatRule{rule})
	want := []string{
		nftableLbDnatIdentity("10.96.1.5", "80", "tcp"),
		nftableLbDnatIdentity(eipIP, "80", "TCP"),
	}

	require.Equal(t, want, slices.Sorted(maps.Keys(desired)))
	require.Equal(t, want, slices.Sorted(maps.Keys(existing)),
		"the existing side must key its identities exactly like the desired side")
	for key, identity := range desired {
		require.Equal(t, key, nftableLbIdentityKey(identity), "a map key is the key of the identity it holds")
	}
	for key, identity := range existing {
		require.Equal(t, key, nftableLbIdentityKey(identity))
	}
}

// TestHandleAddOrUpdateVpcNatGwSyncsVipRoutesOnInstanceChange pins the convergence point of the
// instance set: a replica that goes away produces only a workload event, which lands in the
// gateway reconcile. The share-DNAT VIP routes are written by the bound Services alone, so that
// reconcile has to wake them or a removed instance stays a next hop.
func TestHandleAddOrUpdateVpcNatGwSyncsVipRoutesOnInstanceChange(t *testing.T) {
	controller, gwName := natGwVipRouteSyncController(t)

	require.NoError(t, controller.handleAddOrUpdateVpcNatGw(gwName))
	require.Equal(t, 1, controller.addOrUpdateGwNftableLbSvcQueue.Len())
}
