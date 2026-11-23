package cnp

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/network-policy-api/apis/v1beta1"
)

func TestProtocolPortAllowsProtocolOnlyMatches(t *testing.T) {
	tests := []struct {
		name      string
		protocol  v1beta1.ClusterNetworkPolicyProtocol
		transport string
	}{
		{
			name:      "tcp",
			protocol:  v1beta1.ClusterNetworkPolicyProtocol{TCP: &v1beta1.ClusterNetworkPolicyProtocolTCP{}},
			transport: "tcp",
		},
		{
			name:      "udp",
			protocol:  v1beta1.ClusterNetworkPolicyProtocol{UDP: &v1beta1.ClusterNetworkPolicyProtocolUDP{}},
			transport: "udp",
		},
		{
			name:      "sctp",
			protocol:  v1beta1.ClusterNetworkPolicyProtocol{SCTP: &v1beta1.ClusterNetworkPolicyProtocolSCTP{}},
			transport: "sctp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport, port, err := ProtocolPort(tt.protocol)
			require.NoError(t, err)
			require.Equal(t, tt.transport, transport)
			require.Nil(t, port)
		})
	}
}

func TestProtocolPortRejectsMultipleTransports(t *testing.T) {
	_, _, err := ProtocolPort(v1beta1.ClusterNetworkPolicyProtocol{
		TCP: &v1beta1.ClusterNetworkPolicyProtocolTCP{},
		UDP: &v1beta1.ClusterNetworkPolicyProtocolUDP{},
	})
	require.EqualError(t, err, "exactly one transport is required")
}

func TestValidateProtocolsAllowsDestinationNamedPort(t *testing.T) {
	policy := &v1beta1.ClusterNetworkPolicy{
		Spec: v1beta1.ClusterNetworkPolicySpec{
			Ingress: []v1beta1.ClusterNetworkPolicyIngressRule{{
				Protocols: []v1beta1.ClusterNetworkPolicyProtocol{{DestinationNamedPort: "http"}},
			}},
			Egress: []v1beta1.ClusterNetworkPolicyEgressRule{{
				Protocols: []v1beta1.ClusterNetworkPolicyProtocol{{DestinationNamedPort: "metrics"}},
			}},
		},
	}

	require.NoError(t, ValidateProtocols(policy))
}

func TestValidateProtocolsRejectsMixedNamedPortTransport(t *testing.T) {
	policy := &v1beta1.ClusterNetworkPolicy{
		Spec: v1beta1.ClusterNetworkPolicySpec{
			Ingress: []v1beta1.ClusterNetworkPolicyIngressRule{{Protocols: []v1beta1.ClusterNetworkPolicyProtocol{{
				DestinationNamedPort: "http",
				TCP:                  &v1beta1.ClusterNetworkPolicyProtocolTCP{},
			}}}},
		},
	}
	require.EqualError(t, ValidateProtocols(policy), "ingress[0]: destinationNamedPort cannot be combined with a transport")
}
