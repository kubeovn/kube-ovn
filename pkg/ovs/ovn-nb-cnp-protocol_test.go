package ovs

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/network-policy-api/apis/v1alpha2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func TestCnpProtocolMatch(t *testing.T) {
	protocols := []v1alpha2.ClusterNetworkPolicyProtocol{
		{TCP: &v1alpha2.ClusterNetworkPolicyProtocolTCP{DestinationPort: &v1alpha2.Port{Number: 80}}},
		{UDP: &v1alpha2.ClusterNetworkPolicyProtocolUDP{DestinationPort: &v1alpha2.Port{Range: &v1alpha2.PortRange{Start: 53, End: 54}}}},
		{SCTP: &v1alpha2.ClusterNetworkPolicyProtocolSCTP{DestinationPort: &v1alpha2.Port{Number: 9999}}},
	}
	for _, direction := range []string{ovnnb.ACLDirectionToLport, ovnnb.ACLDirectionFromLport} {
		for _, family := range []string{"IPv4", "IPv6"} {
			matches, err := newCnpACLMatch("pg", "as", family, direction, protocols)
			if err != nil || len(matches) != 3 {
				t.Fatalf("protocol matches: %v", err)
			}
			for i, fragment := range []string{"tcp.dst == 80", "53 <= udp.dst <= 54", "sctp.dst == 9999"} {
				if !strings.Contains(matches[i], fragment) {
					t.Fatalf("unexpected match: %s", matches[i])
				}
			}
		}
	}
	if _, err := newCnpACLMatch("pg", "as", "IPv4", ovnnb.ACLDirectionToLport, []v1alpha2.ClusterNetworkPolicyProtocol{{DestinationNamedPort: "http"}}); err == nil {
		t.Fatal("unsupported named port must not silently produce an ACL")
	}
}

func TestCnpProtocolMatchWithoutDestinationPort(t *testing.T) {
	protocols := []struct {
		protocol v1alpha2.ClusterNetworkPolicyProtocol
		match    string
	}{
		{protocol: v1alpha2.ClusterNetworkPolicyProtocol{TCP: &v1alpha2.ClusterNetworkPolicyProtocolTCP{}}, match: "tcp"},
		{protocol: v1alpha2.ClusterNetworkPolicyProtocol{UDP: &v1alpha2.ClusterNetworkPolicyProtocolUDP{}}, match: "udp"},
		{protocol: v1alpha2.ClusterNetworkPolicyProtocol{SCTP: &v1alpha2.ClusterNetworkPolicyProtocolSCTP{}}, match: "sctp"},
	}
	for _, family := range []struct {
		name string
		key  string
	}{
		{name: "IPv4", key: "ip4.src"},
		{name: "IPv6", key: "ip6.src"},
	} {
		for _, tt := range protocols {
			matches, err := newCnpACLMatch("pg", "as", family.name, ovnnb.ACLDirectionToLport, []v1alpha2.ClusterNetworkPolicyProtocol{tt.protocol})
			require.NoError(t, err)
			require.Len(t, matches, 1)
			require.Equal(t, "outport == @pg && ip && "+family.key+" == $as && "+tt.match, matches[0])
		}
	}
}
