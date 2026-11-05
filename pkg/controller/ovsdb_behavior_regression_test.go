package controller

import (
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestControllerNATGatewayModeParity(t *testing.T) {
	for _, gatewayType := range []string{"centralized", "distributed"} {
		for _, provider := range []bool{false, true} {
			t.Run(gatewayType+"/provider-"+map[bool]string{false: "false", true: "true"}[provider], func(t *testing.T) {
				nb := newInMemoryOVNNbClient(t)
				c := &Controller{OVNNbClient: nb}
				if provider {
					c.OVNNbTables = nb
				}
				require.NoError(t, nb.CreateLogicalRouter("router"))
				for range 2 {
					require.NoError(t, c.updateDnatAndSnat("router", "192.0.2.10", "10.0.0.10", "pod.ns", "00:00:00:00:00:10", gatewayType))
					nat, err := nb.GetNat("router", ovnnb.NATTypeDNATAndSNAT, "192.0.2.10", "10.0.0.10", false)
					require.NoError(t, err)
					if gatewayType == "distributed" {
						require.Equal(t, new("pod.ns"), nat.LogicalPort)
						require.Equal(t, new("00:00:00:00:00:10"), nat.ExternalMAC)
						require.Equal(t, "true", nat.Options["stateless"])
					} else {
						require.Nil(t, nat.LogicalPort)
						require.Nil(t, nat.ExternalMAC)
						require.Empty(t, nat.Options)
					}
				}
			})
		}
	}
}

func TestControllerPerPortDHCPCleanupOwnershipParity(t *testing.T) {
	for _, provider := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "provider"}[provider], func(t *testing.T) {
			nb := newInMemoryOVNNbClient(t)
			c := &Controller{OVNNbClient: nb}
			if provider {
				c.OVNNbTables = nb
			}
			rows := []model.Model{
				&ovnnb.DHCPOptions{Cidr: "10.0.0.0/24", ExternalIDs: map[string]string{ovs.PortKey: "pod.ns", "vendor": util.CniTypeName}},
				&ovnnb.DHCPOptions{Cidr: "fd00::/64", ExternalIDs: map[string]string{ovs.PortKey: "pod.ns", "vendor": util.CniTypeName}},
				&ovnnb.DHCPOptions{Cidr: "10.1.0.0/24", ExternalIDs: map[string]string{ovs.PortKey: "pod.ns", "vendor": "other"}},
				&ovnnb.DHCPOptions{Cidr: "10.2.0.0/24", ExternalIDs: map[string]string{ovs.PortKey: "pod.ns"}},
				&ovnnb.DHCPOptions{Cidr: "10.3.0.0/24", ExternalIDs: map[string]string{ovs.PortKey: "other.ns", "vendor": util.CniTypeName}},
			}
			ops, err := nb.Create(rows...)
			require.NoError(t, err)
			require.NoError(t, nb.Transact("seed-dhcp-owners", ops))
			for range 2 {
				require.NoError(t, c.deleteDHCPOptionsForPort("pod.ns"))
				remaining, err := nb.ListDHCPOptions(false, nil)
				require.NoError(t, err)
				cidrs := make([]string, 0, len(remaining))
				for _, row := range remaining {
					cidrs = append(cidrs, row.Cidr)
				}
				require.ElementsMatch(t, []string{"10.1.0.0/24", "10.2.0.0/24", "10.3.0.0/24"}, cidrs)
			}
		})
	}
}

func TestControllerACLCleanupOwnershipParity(t *testing.T) {
	for _, parentType := range []string{"pg", "ls"} {
		for _, provider := range []bool{false, true} {
			t.Run(parentType+"/"+map[bool]string{false: "legacy", true: "provider"}[provider], func(t *testing.T) {
				nb := newInMemoryOVNNbClient(t)
				c := &Controller{OVNNbClient: nb}
				if provider {
					c.OVNNbTables = nb
				}
				const parentName = "policy"
				var parent model.Model
				var field *[]string
				if parentType == "pg" {
					require.NoError(t, nb.CreatePortGroup(parentName, nil))
					pg, err := nb.GetPortGroup(parentName, false)
					require.NoError(t, err)
					parent, field = pg, &pg.ACLs
				} else {
					require.NoError(t, nb.CreateBareLogicalSwitch(parentName))
					ls, err := nb.GetLogicalSwitch(parentName, false)
					require.NoError(t, err)
					parent, field = ls, &ls.ACLs
				}
				var rows []model.Model
				var ids []string
				for _, owner := range []string{parentName, "other", ""} {
					id := "acl_" + map[string]string{parentName: "owned", "other": "foreign", "": "unlabelled"}[owner]
					rows = append(rows, &ovnnb.ACL{
						UUID: id, Name: new(id), Direction: ovnnb.ACLDirectionToLport,
						Priority: 1000, Match: "ip4", Action: ovnnb.ACLActionAllow,
						ExternalIDs: map[string]string{"parent": owner},
					})
					ids = append(ids, id)
				}
				ops, err := nb.Create(rows...)
				require.NoError(t, err)
				attach, err := nb.Table(parent).MutateOps(parent, model.Mutation{Field: field, Value: ids, Mutator: ovsdb.MutateOperationInsert})
				require.NoError(t, err)
				require.NoError(t, nb.Transact("seed-acl-owners", append(ops, attach...)))
				for range 2 {
					switch {
					case !provider:
						require.NoError(t, nb.DeleteAcls(parentName, parentType, ovnnb.ACLDirectionToLport, nil))
					case parentType == "pg":
						require.NoError(t, c.deletePortGroupACLs(parentName, ovnnb.ACLDirectionToLport, nil))
					default:
						ls, err := c.getLogicalSwitch(parentName, false)
						require.NoError(t, err)
						cleanup, err := c.deleteLogicalSwitchACLOps(ls, ovnnb.ACLDirectionToLport, nil)
						require.NoError(t, err)
						require.NoError(t, c.transactNB("test-acl-cleanup", cleanup...))
					}
					require.Eventually(t, func() bool {
						var remaining []ovnnb.ACL
						if err := nb.Table(&ovnnb.ACL{}).List(t.Context(), &remaining); err != nil {
							return false
						}
						if len(remaining) != 2 {
							return false
						}
						for _, row := range remaining {
							if row.ExternalIDs["parent"] == parentName {
								return false
							}
						}
						return true
					}, time.Second, time.Millisecond)
				}
			})
		}
	}
}
