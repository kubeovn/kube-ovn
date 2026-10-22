package cnp

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/network-policy-api/apis/v1alpha2"
)

func TestProtocolPortAllowsProtocolOnlyMatches(t *testing.T) {
	tests := []struct {
		name      string
		protocol  v1alpha2.ClusterNetworkPolicyProtocol
		transport string
	}{
		{
			name:      "tcp",
			protocol:  v1alpha2.ClusterNetworkPolicyProtocol{TCP: &v1alpha2.ClusterNetworkPolicyProtocolTCP{}},
			transport: "tcp",
		},
		{
			name:      "udp",
			protocol:  v1alpha2.ClusterNetworkPolicyProtocol{UDP: &v1alpha2.ClusterNetworkPolicyProtocolUDP{}},
			transport: "udp",
		},
		{
			name:      "sctp",
			protocol:  v1alpha2.ClusterNetworkPolicyProtocol{SCTP: &v1alpha2.ClusterNetworkPolicyProtocolSCTP{}},
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
	_, _, err := ProtocolPort(v1alpha2.ClusterNetworkPolicyProtocol{
		TCP: &v1alpha2.ClusterNetworkPolicyProtocolTCP{},
		UDP: &v1alpha2.ClusterNetworkPolicyProtocolUDP{},
	})
	require.EqualError(t, err, "exactly one transport is required")
}
