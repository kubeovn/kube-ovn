package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{util.EipAnnotation: "eip0"}},
				Spec:       v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer},
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
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{util.EipAnnotation: "eip0"}},
				Spec:       v1.ServiceSpec{Type: v1.ServiceTypeClusterIP},
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

func Test_buildDesiredNftableLbDnatRules(t *testing.T) {
	t.Parallel()

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web"},
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

	desired := buildDesiredNftableLbDnatRules(svc, "eip0", endpointSlices)

	// only 2 ready IPv4 backends on the tcp port; sctp port skipped
	require.Len(t, desired, 2)

	backends := make(map[string]*kubeovnv1.IptablesDnatRule)
	for _, rule := range desired {
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

func Test_buildDesiredNftableLbDnatRules_noMatchingPort(t *testing.T) {
	t.Parallel()

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web"},
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

	desired := buildDesiredNftableLbDnatRules(svc, "eip0", endpointSlices)
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
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web"},
		Spec: v1.ServiceSpec{
			Type:            v1.ServiceTypeLoadBalancer,
			Ports:           ports,
			SessionAffinity: v1.ServiceAffinityClientIP,
			SessionAffinityConfig: &v1.SessionAffinityConfig{
				ClientIP: &v1.ClientIPConfig{TimeoutSeconds: new(int32(600))},
			},
		},
	}
	desired := buildDesiredNftableLbDnatRules(svc, "eip0", endpointSlices)
	require.Len(t, desired, 1)
	for _, rule := range desired {
		require.Equal(t, kubeovnv1.DnatSessionAffinityClientIP, rule.Spec.SessionAffinity)
		require.Equal(t, int32(600), rule.Spec.SessionAffinityTimeoutSeconds)
	}

	// No affinity: fields stay empty (stateless numgen-random balancing)
	svcNone := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "web"},
		Spec: v1.ServiceSpec{
			Type:            v1.ServiceTypeLoadBalancer,
			Ports:           ports,
			SessionAffinity: v1.ServiceAffinityNone,
		},
	}
	desiredNone := buildDesiredNftableLbDnatRules(svcNone, "eip0", endpointSlices)
	require.Len(t, desiredNone, 1)
	for _, rule := range desiredNone {
		require.Equal(t, kubeovnv1.DnatSessionAffinityNone, rule.Spec.SessionAffinity)
		require.Zero(t, rule.Spec.SessionAffinityTimeoutSeconds)
	}
}

func Test_nftableLbSvcOwnerKey(t *testing.T) {
	t.Parallel()

	owned := &kubeovnv1.IptablesDnatRule{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
			util.NftableLbSvcNsLabel:   "default",
			util.NftableLbSvcNameLabel: "web",
		}},
	}
	require.Equal(t, "default/web", nftableLbSvcOwnerKey(owned))

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
