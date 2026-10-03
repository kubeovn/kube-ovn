package ovs

import (
	"strings"
	"testing"

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
