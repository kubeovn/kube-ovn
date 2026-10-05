package ko

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestNodePortProbeUsesAllocatedServiceFamilies(t *testing.T) {
	for _, tc := range []struct {
		name     string
		families []corev1.IPFamily
		want     string
	}{
		{"ipv4", []corev1.IPFamily{corev1.IPv4Protocol}, "tcp-192.0.2.1-30001"},
		{"ipv6", []corev1.IPFamily{corev1.IPv6Protocol}, "tcp-2001:db8::1-30001"},
		{"dual", []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol}, "tcp-192.0.2.1-30001,tcp-2001:db8::1-30001"},
		{"ipv6-primary", []corev1.IPFamily{corev1.IPv6Protocol, corev1.IPv4Protocol}, "tcp-192.0.2.1-30001,tcp-2001:db8::1-30001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs := fake.NewClientset(&corev1.Node{Name: "worker", Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeHostName, Address: "worker"},
				{Type: corev1.NodeInternalIP, Address: "192.0.2.1"},
				{Type: corev1.NodeInternalIP, Address: "2001:db8::1"},
			}}})
			cs.PrependReactor("create", "services", func(action ktesting.Action) (bool, runtime.Object, error) {
				service := action.(ktesting.CreateAction).GetObject().(*corev1.Service).DeepCopy()
				require.Equal(t, new(corev1.IPFamilyPolicyPreferDualStack), service.Spec.IPFamilyPolicy)
				service.Spec.IPFamilies = tc.families
				service.Spec.Ports[0].NodePort = 30001
				return true, service, nil
			})
			run := &resourceRun{client: &Client{Kubernetes: cs, Namespace: "ovn-system"}, id: "test"}
			targets, err := run.nodePortProbe(t.Context())
			require.NoError(t, err)
			require.Equal(t, tc.want, targets)
		})
	}
}

func TestNodePortProbeRejectsMissingFamilyTargets(t *testing.T) {
	for _, tc := range []struct {
		name     string
		families []corev1.IPFamily
		address  string
		wantErr  string
	}{
		{"unallocated", nil, "192.0.2.1", "no allocated IP families"},
		{"missing-ipv6", []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol}, "192.0.2.1", "family IPv6"},
		{"missing-ipv4", []corev1.IPFamily{corev1.IPv4Protocol}, "2001:db8::1", "family IPv4"},
		{"invalid-address", []corev1.IPFamily{corev1.IPv4Protocol}, "invalid", "invalid internal address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs := fake.NewClientset(&corev1.Node{Name: "worker", Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: tc.address}}}})
			cs.PrependReactor("create", "services", func(action ktesting.Action) (bool, runtime.Object, error) {
				service := action.(ktesting.CreateAction).GetObject().(*corev1.Service).DeepCopy()
				service.Spec.IPFamilies = tc.families
				service.Spec.Ports[0].NodePort = 30001
				return true, service, nil
			})
			run := &resourceRun{client: &Client{Kubernetes: cs, Namespace: "ovn-system"}, id: "test"}
			targets, err := run.nodePortProbe(t.Context())
			require.ErrorContains(t, err, tc.wantErr)
			require.Empty(t, targets)
		})
	}
}
