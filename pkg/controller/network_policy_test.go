package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"k8s.io/utils/set"

	"github.com/stretchr/testify/require"

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
