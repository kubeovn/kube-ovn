package controller

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestControllerSecurityGroupRulePaths(t *testing.T) {
	client := newInMemoryOVNNbClient(t)
	sgName := "rule-test"
	pgName := ovs.GetSgPortGroupName(sgName)
	require.NoError(t, client.CreatePortGroup(pgName, nil))
	tests := []struct {
		name      string
		direction string
		rule      kubeovnv1.SecurityGroupRule
		match     string
		action    string
	}{
		{
			name:      "ipv4 ingress tcp allow",
			direction: ovnnb.ACLDirectionToLport,
			rule:      kubeovnv1.SecurityGroupRule{IPVersion: "ipv4", RemoteType: kubeovnv1.SgRemoteTypeAddress, RemoteAddress: "192.0.2.0/24", LocalAddress: "10.0.0.0/24", Protocol: kubeovnv1.SgProtocolTCP, Policy: kubeovnv1.SgPolicyAllow, PortRangeMin: 80, PortRangeMax: 90, SourcePortRangeMin: 1000, SourcePortRangeMax: 2000},
			match:     "outport == @" + pgName + " && ip4 && ip4.src == 192.0.2.0/24 && ip4.dst == 10.0.0.0/24 && 80 <= tcp.dst <= 90 && 1000 <= tcp.src <= 2000",
			action:    ovnnb.ACLActionAllowRelated,
		},
		{
			name:      "ipv6 egress udp drop remote group",
			direction: ovnnb.ACLDirectionFromLport,
			rule:      kubeovnv1.SecurityGroupRule{IPVersion: "ipv6", RemoteType: kubeovnv1.SgRemoteTypeSg, RemoteSecurityGroup: "remote", LocalAddress: "2001:db8:1::/64", Protocol: kubeovnv1.SgProtocolUDP, Policy: kubeovnv1.SgPolicyDrop, PortRangeMin: 53, PortRangeMax: 54, SourcePortRangeMin: 3000, SourcePortRangeMax: 4000},
			match:     "inport == @" + pgName + " && ip6 && ip6.dst == $" + ovs.GetSgV6AssociatedName("remote") + " && ip6.src == 2001:db8:1::/64 && 53 <= udp.dst <= 54 && 3000 <= udp.src <= 4000",
			action:    ovnnb.ACLActionDrop,
		},
		{
			name:      "ipv6 ingress icmp pass",
			direction: ovnnb.ACLDirectionToLport,
			rule:      kubeovnv1.SecurityGroupRule{IPVersion: "ipv6", RemoteType: kubeovnv1.SgRemoteTypeAddress, RemoteAddress: "2001:db8::/64", Protocol: kubeovnv1.SgProtocolICMP, Policy: kubeovnv1.SgPolicyPass},
			match:     "outport == @" + pgName + " && ip6 && ip6.src == 2001:db8::/64 && icmp6",
			action:    ovnnb.ACLActionPass,
		},
		{
			name:      "ipv4 egress all drop remote group",
			direction: ovnnb.ACLDirectionFromLport,
			rule:      kubeovnv1.SecurityGroupRule{IPVersion: "ipv4", RemoteType: kubeovnv1.SgRemoteTypeSg, RemoteSecurityGroup: "remote", Protocol: kubeovnv1.SgProtocolALL, Policy: kubeovnv1.SgPolicyDrop},
			match:     "inport == @" + pgName + " && ip4 && ip4.dst == $" + ovs.GetSgV4AssociatedName("remote"),
			action:    ovnnb.ACLActionDrop,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.rule.Priority = 7
			sg := &kubeovnv1.SecurityGroup{Name: sgName, Spec: kubeovnv1.SecurityGroupSpec{Tier: 1, IngressRules: []kubeovnv1.SecurityGroupRule{tt.rule}, EgressRules: []kubeovnv1.SecurityGroupRule{tt.rule}}}
			var previous *ovnnb.ACL
			for _, provider := range []bool{false, true} {
				controller := &Controller{OVNNbClient: client}
				if provider {
					controller.OVNNbTables = client
					require.NoError(t, controller.updateSgACL(sg, tt.direction))
				} else {
					require.NoError(t, client.UpdateSgACL(sg, tt.direction))
				}
				acl, err := client.GetACL(pgName, tt.direction, "18477", tt.match, util.ConvertSGTierToOvnTier(1), false)
				require.NoError(t, err)
				require.Equal(t, tt.action, acl.Action)
				require.Equal(t, map[string]string{"parent": pgName, "vendor": util.CniTypeName}, acl.ExternalIDs)
				pg, err := client.GetPortGroup(pgName, false)
				require.NoError(t, err)
				require.Contains(t, pg.ACLs, acl.UUID)
				acl.UUID = ""
				if previous != nil {
					require.Equal(t, previous, acl)
				}
				previous = acl
			}
		})
	}
}

func TestControllerAddressSetInvalidCIDRFallback(t *testing.T) {
	client := newInMemoryOVNNbClient(t)
	require.NoError(t, client.CreateAddressSet("cidr_test", nil))
	addresses := []string{"192.0.2.1/24", "192.0.2.0/24", "2001:db8::1/64", "192.0.2.1/xx", "192.0.2.1/xx"}
	original := slices.Clone(addresses)
	for _, provider := range []bool{false, true} {
		require.NoError(t, client.AddressSetUpdateAddress("cidr_test"))
		controller := &Controller{OVNNbClient: client}
		if provider {
			controller.OVNNbTables = client
		}
		require.NoError(t, controller.updateAddressSetAddresses("cidr_test", addresses...))
		as, err := client.GetAddressSet("cidr_test", false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"192.0.2.0/24", "2001:db8::/64", "192.0.2.1/xx"}, as.Addresses)
		require.Equal(t, original, addresses)
	}
}
