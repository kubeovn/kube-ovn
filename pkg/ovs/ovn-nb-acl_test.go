package ovs

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func mockNetworkPolicyPort() []netv1.NetworkPolicyPort {
	protocolTCP := v1.ProtocolTCP
	var endPort int32 = 20000
	return []netv1.NetworkPolicyPort{
		{
			Port: &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: 12345,
			},
			Protocol: &protocolTCP,
		},
		{
			Port: &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: 12346,
			},
			EndPort:  &endPort,
			Protocol: &protocolTCP,
		},
	}
}

func newACL(parentName, direction, priority, match, action string, tier int, options ...func(acl *ovnnb.ACL)) *ovnnb.ACL {
	intPriority, _ := strconv.Atoi(priority)

	acl := &ovnnb.ACL{
		UUID:      ovsclient.NamedUUID(),
		Action:    action,
		Direction: direction,
		Match:     match,
		Priority:  intPriority,
		ExternalIDs: map[string]string{
			aclParentKey: parentName,
		},
		Tier: tier,
	}

	for _, option := range options {
		option(acl)
	}

	return acl
}

func (suite *OvnClientTestSuite) testUpdateIngressACLOps() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	expect := func(row ovsdb.Row, action, direction, match, priority string) {
		intPriority, err := strconv.Atoi(priority)
		require.NoError(t, err)
		require.Equal(t, action, row["action"])
		require.Equal(t, direction, row["direction"])
		require.Equal(t, match, row["match"])
		require.Equal(t, intPriority, row["priority"])
	}

	t.Run("ipv4 acl", func(t *testing.T) {
		t.Parallel()

		netpol := "ipv4 ingress"
		pgName := "test_create_v4_ingress_acl_pg"
		asIngressName := "test.default.ingress.allow.ipv4.all"
		asExceptName := "test.default.ingress.except.ipv4.all"
		protocol := kubeovnv1.ProtocolIPv4
		aclName := "test_create_v4_ingress_acl_pg"

		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		npp := mockNetworkPolicyPort()

		ops, err := nbClient.UpdateIngressACLOps(netpol, pgName, asIngressName, asExceptName, protocol, aclName, npp, true, nil, nil)
		require.NoError(t, err)
		require.Len(t, ops, 4)

		expect(ops[0].Row, "drop", ovnnb.ACLDirectionToLport, fmt.Sprintf("outport == @%s && ip", pgName), util.IngressDefaultDrop)

		matches := newNetworkPolicyACLMatch(pgName, asIngressName, asExceptName, protocol, ovnnb.ACLDirectionToLport, npp, nil)
		i := 1
		for _, m := range matches {
			require.Equal(t, m, ops[i].Row["match"])
			expect(ops[i].Row, ovnnb.ACLActionAllowRelated, ovnnb.ACLDirectionToLport, m, util.IngressAllowPriority)
			i++
		}
	})

	t.Run("ipv6 acl", func(t *testing.T) {
		t.Parallel()

		netpol := "ipv6 ingress"
		pgName := "test_create_v6_ingress_acl_pg"
		asIngressName := "test.default.ingress.allow.ipv6.all"
		asExceptName := "test.default.ingress.except.ipv6.all"
		protocol := kubeovnv1.ProtocolIPv6
		aclName := "test_create_v6_ingress_acl_pg"

		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		ops, err := nbClient.UpdateIngressACLOps(netpol, pgName, asIngressName, asExceptName, protocol, aclName, nil, true, nil, nil)
		require.NoError(t, err)
		require.Len(t, ops, 3)

		expect(ops[0].Row, "drop", ovnnb.ACLDirectionToLport, fmt.Sprintf("outport == @%s && ip", pgName), util.IngressDefaultDrop)

		matches := newNetworkPolicyACLMatch(pgName, asIngressName, asExceptName, protocol, ovnnb.ACLDirectionToLport, nil, nil)
		i := 1
		for _, m := range matches {
			require.Equal(t, m, ops[i].Row["match"])
			expect(ops[i].Row, ovnnb.ACLActionAllowRelated, ovnnb.ACLDirectionToLport, m, util.IngressAllowPriority)
			i++
		}
	})

	t.Run("test empty pgName", func(t *testing.T) {
		t.Parallel()

		netpol := "ingress with empty pg name"
		pgName := ""
		asIngressName := "test.default.ingress.allow.ipv4.all"
		asExceptName := "test.default.ingress.except.ipv4.all"
		protocol := kubeovnv1.ProtocolIPv4
		aclName := "test_create_v4_ingress_acl_pg"

		_, err := nbClient.UpdateIngressACLOps(netpol, pgName, asIngressName, asExceptName, protocol, aclName, nil, true, nil, nil)
		require.ErrorContains(t, err, "the port group name or logical switch name is required")
	})

	t.Run("test empty pgName without suffix", func(t *testing.T) {
		t.Parallel()

		netpol := "ingress with empty pg name and no suffix"
		pgName := ""
		asIngressName := "test.default.ingress.allow.ipv4"
		asExceptName := "test.default.ingress.except.ipv4"
		protocol := kubeovnv1.ProtocolIPv4
		aclName := "test_create_v4_ingress_acl_pg"

		_, err := nbClient.UpdateIngressACLOps(netpol, pgName, asIngressName, asExceptName, protocol, aclName, nil, true, nil, nil)
		require.ErrorContains(t, err, "the port group name or logical switch name is required")
	})
}

func (suite *OvnClientTestSuite) testUpdateEgressACLOps() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	expect := func(row ovsdb.Row, action, direction, match, priority string) {
		intPriority, err := strconv.Atoi(priority)
		require.NoError(t, err)
		require.Equal(t, action, row["action"])
		require.Equal(t, direction, row["direction"])
		require.Equal(t, match, row["match"])
		require.Equal(t, intPriority, row["priority"])
	}

	t.Run("ipv4 acl", func(t *testing.T) {
		t.Parallel()

		netpol := "ipv4 egress"
		pgName := "test_create_v4_egress_acl_pg"
		asEgressName := "test.default.egress.allow.ipv4.all"
		asExceptName := "test.default.egress.except.ipv4.all"
		protocol := kubeovnv1.ProtocolIPv4
		aclName := "test_create_v4_egress_acl_pg"

		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		npp := mockNetworkPolicyPort()

		ops, err := nbClient.UpdateEgressACLOps(netpol, pgName, asEgressName, asExceptName, protocol, aclName, npp, true, nil, nil)
		require.NoError(t, err)
		require.Len(t, ops, 4)

		expect(ops[0].Row, "drop", ovnnb.ACLDirectionFromLport, fmt.Sprintf("inport == @%s && ip", pgName), util.EgressDefaultDrop)

		matches := newNetworkPolicyACLMatch(pgName, asEgressName, asExceptName, protocol, ovnnb.ACLDirectionFromLport, npp, nil)
		i := 1
		for _, m := range matches {
			require.Equal(t, m, ops[i].Row["match"])
			expect(ops[i].Row, ovnnb.ACLActionAllowRelated, ovnnb.ACLDirectionFromLport, m, util.EgressAllowPriority)
			i++
		}
	})

	t.Run("ipv6 acl", func(t *testing.T) {
		t.Parallel()

		netpol := "ipv6 egress"
		pgName := "test_create_v6_egress_acl_pg"
		asEgressName := "test.default.egress.allow.ipv6.all"
		asExceptName := "test.default.egress.except.ipv6.all"
		protocol := kubeovnv1.ProtocolIPv6
		aclName := "test_create_v6_egress_acl_pg"

		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		ops, err := nbClient.UpdateEgressACLOps(netpol, pgName, asEgressName, asExceptName, protocol, aclName, nil, true, nil, nil)
		require.NoError(t, err)
		require.Len(t, ops, 3)

		expect(ops[0].Row, "drop", ovnnb.ACLDirectionFromLport, fmt.Sprintf("inport == @%s && ip", pgName), util.EgressDefaultDrop)

		matches := newNetworkPolicyACLMatch(pgName, asEgressName, asExceptName, protocol, ovnnb.ACLDirectionFromLport, nil, nil)
		i := 1
		for _, m := range matches {
			require.Equal(t, m, ops[i].Row["match"])
			expect(ops[i].Row, ovnnb.ACLActionAllowRelated, ovnnb.ACLDirectionFromLport, m, util.EgressAllowPriority)
			i++
		}
	})

	t.Run("test empty pgName", func(t *testing.T) {
		t.Parallel()

		netpol := "egress with empty pg name"
		pgName := ""
		asEgressName := "test.default.egress.allow.ipv4.all"
		asExceptName := "test.default.egress.except.ipv4.all"
		protocol := kubeovnv1.ProtocolIPv4
		aclName := "test_create_v4_egress_acl_pg"

		_, err := nbClient.UpdateEgressACLOps(netpol, pgName, asEgressName, asExceptName, protocol, aclName, nil, true, nil, nil)
		require.ErrorContains(t, err, "the port group name or logical switch name is required")
	})

	t.Run("test empty pgName without suffix", func(t *testing.T) {
		t.Parallel()

		netpol := "egress with empty pg name and no suffix"
		pgName := ""
		asEgressName := "test.default.egress.allow.ipv4"
		asExceptName := "test.default.egress.except.ipv4"
		protocol := kubeovnv1.ProtocolIPv4
		aclName := "test_create_v4_egress_acl_pg"

		_, err := nbClient.UpdateEgressACLOps(netpol, pgName, asEgressName, asExceptName, protocol, aclName, nil, true, nil, nil)
		require.ErrorContains(t, err, "the port group name or logical switch name is required")
	})
}

func (suite *OvnClientTestSuite) testCreateGatewayACL() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	checkACL := func(parent any, direction, priority, match string, options map[string]string) {
		pg, isPg := parent.(*ovnnb.PortGroup)
		var name string
		var acls []string

		if isPg {
			name = pg.Name
			acls = pg.ACLs
		} else {
			ls := parent.(*ovnnb.LogicalSwitch)
			name = ls.Name
			acls = ls.ACLs
		}

		acl, err := nbClient.GetACL(name, direction, priority, match, false)
		require.NoError(t, err)
		expect := newACL(name, direction, priority, match, ovnnb.ACLActionAllowStateless, util.NetpolACLTier)
		expect.UUID = acl.UUID
		if len(options) != 0 {
			expect.Options = options
		}
		require.Equal(t, expect, acl)
		require.Contains(t, acls, acl.UUID)
	}

	expect := func(parent any, gateway string) {
		for gw := range strings.SplitSeq(gateway, ",") {
			protocol := util.CheckProtocol(gw)
			ipSuffix := "ip4"
			if protocol == kubeovnv1.ProtocolIPv6 {
				ipSuffix = "ip6"
			}

			match := fmt.Sprintf("%s.src == %s", ipSuffix, gw)
			checkACL(parent, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, match, nil)

			match = fmt.Sprintf("%s.dst == %s", ipSuffix, gw)
			checkACL(parent, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, match, map[string]string{
				"apply-after-lb": "true",
			})

			if ipSuffix == "ip6" {
				match = "nd || nd_ra || nd_rs"
				checkACL(parent, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, match, map[string]string{
					"apply-after-lb": "true",
				})
			}
		}
	}

	t.Run("add acl to pg", func(t *testing.T) {
		t.Parallel()

		t.Run("gateway's protocol is dual", func(t *testing.T) {
			t.Parallel()

			pgName := "test_create_gw_acl_pg_dual"
			gateway := "10.244.0.1,fc00::0af4:01"

			err := nbClient.CreatePortGroup(pgName, nil)
			require.NoError(t, err)

			err = nbClient.CreateGatewayACL("", pgName, gateway, "")
			require.NoError(t, err)

			pg, err := nbClient.GetPortGroup(pgName, false)
			require.NoError(t, err)
			require.Len(t, pg.ACLs, 5)

			expect(pg, gateway)
		})

		t.Run("gateway's protocol is dual with u2oInterconnectionIP", func(t *testing.T) {
			t.Parallel()

			pgName := "test_create_gw_acl_pg_dual_u2oInterconnectionIP"
			gateway := "10.244.0.1,fc00::0af4:01"
			u2oInterconnectionIP := "10.244.0.2,fc00::0af4:02"

			err := nbClient.CreatePortGroup(pgName, nil)
			require.NoError(t, err)

			err = nbClient.CreateGatewayACL("", pgName, gateway, u2oInterconnectionIP)
			require.NoError(t, err)

			pg, err := nbClient.GetPortGroup(pgName, false)
			require.NoError(t, err)
			require.Len(t, pg.ACLs, 9)

			expect(pg, gateway)
			expect(pg, u2oInterconnectionIP)
		})

		t.Run("gateway's protocol is ipv4", func(t *testing.T) {
			t.Parallel()

			pgName := "test_create_gw_acl_pg_v4"
			gateway := "10.244.0.1"

			err := nbClient.CreatePortGroup(pgName, nil)
			require.NoError(t, err)

			err = nbClient.CreateGatewayACL("", pgName, gateway, "")
			require.NoError(t, err)

			pg, err := nbClient.GetPortGroup(pgName, false)
			require.NoError(t, err)
			require.Len(t, pg.ACLs, 2)

			expect(pg, gateway)
		})

		t.Run("gateway's protocol is ipv4 with u2oInterconnectionIP", func(t *testing.T) {
			t.Parallel()

			pgName := "test_create_gw_acl_pg_v4_u2oInterconnectionIP"
			gateway := "10.244.0.1"
			u2oInterconnectionIP := "10.244.0.2"

			err := nbClient.CreatePortGroup(pgName, nil)
			require.NoError(t, err)

			err = nbClient.CreateGatewayACL("", pgName, gateway, u2oInterconnectionIP)
			require.NoError(t, err)

			pg, err := nbClient.GetPortGroup(pgName, false)
			require.NoError(t, err)
			require.Len(t, pg.ACLs, 4)

			expect(pg, gateway)
			expect(pg, u2oInterconnectionIP)
		})

		t.Run("gateway's protocol is ipv6", func(t *testing.T) {
			t.Parallel()

			pgName := "test_create_gw_acl_pg_v6"
			gateway := "fc00::0af4:01"

			err := nbClient.CreatePortGroup(pgName, nil)
			require.NoError(t, err)

			err = nbClient.CreateGatewayACL("", pgName, gateway, "")
			require.NoError(t, err)

			pg, err := nbClient.GetPortGroup(pgName, false)
			require.NoError(t, err)
			require.Len(t, pg.ACLs, 3)

			expect(pg, gateway)
		})

		t.Run("gateway's protocol is ipv6", func(t *testing.T) {
			t.Parallel()

			pgName := "test_create_gw_acl_pg_v6_u2oInterconnectionIP"
			gateway := "fc00::0af4:01"
			u2oInterconnectionIP := "fc00::0af4:02"

			err := nbClient.CreatePortGroup(pgName, nil)
			require.NoError(t, err)

			err = nbClient.CreateGatewayACL("", pgName, gateway, u2oInterconnectionIP)
			require.NoError(t, err)

			pg, err := nbClient.GetPortGroup(pgName, false)
			require.NoError(t, err)
			require.Len(t, pg.ACLs, 5)

			expect(pg, gateway)
			expect(pg, u2oInterconnectionIP)
		})
	})

	t.Run("add acl to ls", func(t *testing.T) {
		t.Parallel()

		t.Run("gateway's protocol is dual", func(t *testing.T) {
			t.Parallel()

			lsName := "test_create_gw_acl_ls_dual"
			gateway := "10.244.0.1,fc00::0af4:01"

			err := nbClient.CreateBareLogicalSwitch(lsName)
			require.NoError(t, err)

			err = nbClient.CreateGatewayACL(lsName, "", gateway, "")
			require.NoError(t, err)

			ls, err := nbClient.GetLogicalSwitch(lsName, false)
			require.NoError(t, err)
			require.Len(t, ls.ACLs, 5)

			expect(ls, gateway)
		})
	})

	t.Run("has no pg name and ls name", func(t *testing.T) {
		t.Parallel()
		err := nbClient.CreateGatewayACL("", "", "", "")
		require.EqualError(t, err, "one of port group name and logical switch name must be specified")
	})
}

func (suite *OvnClientTestSuite) testCreateNodeACL() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	checkACL := func(pg *ovnnb.PortGroup, direction, priority, match string, options map[string]string) {
		acl, err := nbClient.GetACL(pg.Name, direction, priority, match, false)
		require.NoError(t, err)
		expect := newACL(pg.Name, direction, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		expect.UUID = acl.UUID
		if len(options) != 0 {
			expect.Options = options
		}
		require.Equal(t, expect, acl)
		require.Contains(t, pg.ACLs, acl.UUID)
	}

	expect := func(pg *ovnnb.PortGroup, nodeIP, pgName string) {
		for ip := range strings.SplitSeq(nodeIP, ",") {
			protocol := util.CheckProtocol(ip)
			ipSuffix := "ip4"
			if protocol == kubeovnv1.ProtocolIPv6 {
				ipSuffix = "ip6"
			}

			pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)

			match := fmt.Sprintf("%s.src == %s && %s.dst == $%s", ipSuffix, ip, ipSuffix, pgAs)
			checkACL(pg, ovnnb.ACLDirectionToLport, util.NodeAllowPriority, match, nil)

			match = fmt.Sprintf("%s.dst == %s && %s.src == $%s", ipSuffix, ip, ipSuffix, pgAs)
			checkACL(pg, ovnnb.ACLDirectionFromLport, util.NodeAllowPriority, match, map[string]string{
				"apply-after-lb": "true",
			})
		}
	}

	t.Run("create node ACL with single stack nodeIP and dual stack joinIP", func(t *testing.T) {
		pgName := "test_create_node_acl_pg"
		nodeIP := "192.168.20.3"
		joinIP := "100.64.0.2,fd00:100:64::2"

		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		err = nbClient.CreateNodeACL(pgName, nodeIP, joinIP)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Len(t, pg.ACLs, 2)

		expect(pg, nodeIP, pgName)
	})

	t.Run("create node ACL with dual stack nodeIP and join IP", func(t *testing.T) {
		pgName := "test-pg-overlap"
		nodeIP := "192.168.20.4,fd00::4"
		joinIP := "100.64.0.3,fd00:100:64::3"

		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		err = nbClient.CreateNodeACL(pgName, nodeIP, joinIP)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Len(t, pg.ACLs, 4)

		expect(pg, nodeIP, pgName)
	})

	t.Run("create node ACL with nodeIP containing joinIP", func(t *testing.T) {
		pgName := "test-pg-nodeIP-contain-joinIP"
		nodeIP := "192.168.20.4,fd00::4"
		joinIP := "192.168.20.4,fd00:100:64::3"

		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		err = nbClient.CreateNodeACL(pgName, nodeIP, joinIP)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Len(t, pg.ACLs, 4)

		expect(pg, nodeIP, pgName)
	})

	t.Run("test empty pgName", func(t *testing.T) {
		pgName := ""
		nodeIP := "192.168.20.4,fd00::4"
		joinIP := "100.64.0.3,fd00:100:64::3"

		err := nbClient.CreateNodeACL(pgName, nodeIP, joinIP)
		require.ErrorContains(t, err, "the port group name or logical switch name is required")
	})
}

func (suite *OvnClientTestSuite) testCreateSgDenyAllACL() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient

	t.Run("normal create sg deny all acl", func(t *testing.T) {
		sgName := "test_create_deny_all_acl_pg"
		pgName := GetSgPortGroupName(sgName)

		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		err = nbClient.CreateSgDenyAllACL(sgName)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		// ingress acl
		match := fmt.Sprintf("outport == @%s && ip", pgName)
		ingressACL, err := nbClient.GetACL(pgName, ovnnb.ACLDirectionToLport, util.SecurityGroupDropPriority, match, false)
		require.NoError(t, err)
		expect := newACL(pgName, ovnnb.ACLDirectionToLport, util.SecurityGroupDropPriority, match, ovnnb.ACLActionDrop, util.NetpolACLTier)
		expect.UUID = ingressACL.UUID
		require.Equal(t, expect, ingressACL)
		require.Contains(t, pg.ACLs, ingressACL.UUID)

		// egress acl
		match = fmt.Sprintf("inport == @%s && ip", pgName)
		egressACL, err := nbClient.GetACL(pgName, ovnnb.ACLDirectionFromLport, util.SecurityGroupDropPriority, match, false)
		require.NoError(t, err)
		expect = newACL(pgName, ovnnb.ACLDirectionFromLport, util.SecurityGroupDropPriority, match, ovnnb.ACLActionDrop, util.NetpolACLTier)
		expect.UUID = egressACL.UUID
		require.Equal(t, expect, egressACL)
		require.Contains(t, pg.ACLs, egressACL.UUID)
	})

	t.Run("should print log err when sg name does not exist", func(t *testing.T) {
		sgName := "test_nonexist_pg"
		err := nbClient.CreateSgDenyAllACL(sgName)
		require.Error(t, err)
	})

	t.Run("should print log err when sg name is empty", func(t *testing.T) {
		err := nbClient.CreateSgDenyAllACL("")
		require.ErrorContains(t, err, "the port group name or logical switch name is required")
	})

	t.Run("fail nb client should log err", func(t *testing.T) {
		sgName := "test_failed_client"
		err := failedNbClient.CreateSgDenyAllACL(sgName)
		require.Error(t, err)
	})
}

func (suite *OvnClientTestSuite) testCreateSgBaseACL() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	expect := func(pg *ovnnb.PortGroup, match, direction string) {
		arpACL, err := nbClient.GetACL(pg.Name, direction, util.SecurityGroupBasePriority, match, false)
		require.NoError(t, err)

		expect := newACL(pg.Name, direction, util.SecurityGroupBasePriority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, func(acl *ovnnb.ACL) {
			acl.UUID = arpACL.UUID
		})

		require.Equal(t, expect, arpACL)
		require.Contains(t, pg.ACLs, arpACL.UUID)
	}

	t.Run("create sg base ingress acl", func(t *testing.T) {
		t.Parallel()

		sgName := "test_create_sg_base_ingress_acl"
		pgName := GetSgPortGroupName(sgName)
		portDirection := "outport"

		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		// ingress
		err = nbClient.CreateSgBaseACL(sgName, ovnnb.ACLDirectionToLport)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Len(t, pg.ACLs, 5)

		// arp
		match := fmt.Sprintf("%s == @%s && arp", portDirection, pgName)
		expect(pg, match, ovnnb.ACLDirectionToLport)

		// icmpv6
		match = fmt.Sprintf("%s == @%s && icmp6.type == {130, 134, 135, 136} && icmp6.code == 0 && ip.ttl == 255", portDirection, pgName)
		expect(pg, match, ovnnb.ACLDirectionToLport)

		// dhcpv4
		match = fmt.Sprintf("%s == @%s && udp.src == 67 && udp.dst == 68 && ip4", portDirection, pgName)
		expect(pg, match, ovnnb.ACLDirectionToLport)

		// dhcpv6
		match = fmt.Sprintf("%s == @%s && udp.src == 547 && udp.dst == 546 && ip6", portDirection, pgName)
		expect(pg, match, ovnnb.ACLDirectionToLport)

		// vrrp
		match = fmt.Sprintf("%s == @%s && ip.proto == 112", portDirection, pgName)
		expect(pg, match, ovnnb.ACLDirectionToLport)
	})

	t.Run("create sg base egress acl", func(t *testing.T) {
		t.Parallel()

		sgName := "test_create_sg_base_egress_acl"
		pgName := GetSgPortGroupName(sgName)
		portDirection := "inport"

		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		// egress
		err = nbClient.CreateSgBaseACL(sgName, ovnnb.ACLDirectionFromLport)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Len(t, pg.ACLs, 5)

		// arp
		match := fmt.Sprintf("%s == @%s && arp", portDirection, pgName)
		expect(pg, match, ovnnb.ACLDirectionFromLport)

		// icmpv6
		match = fmt.Sprintf("%s == @%s && icmp6.type == {130, 133, 135, 136} && icmp6.code == 0 && ip.ttl == 255", portDirection, pgName)
		expect(pg, match, ovnnb.ACLDirectionFromLport)

		// dhcpv4
		match = fmt.Sprintf("%s == @%s && udp.src == 68 && udp.dst == 67 && ip4", portDirection, pgName)
		expect(pg, match, ovnnb.ACLDirectionFromLport)

		// dhcpv6
		match = fmt.Sprintf("%s == @%s && udp.src == 546 && udp.dst == 547 && ip6", portDirection, pgName)
		expect(pg, match, ovnnb.ACLDirectionFromLport)

		// vrrp
		match = fmt.Sprintf("%s == @%s && ip.proto == 112", portDirection, pgName)
		expect(pg, match, ovnnb.ACLDirectionFromLport)
	})

	t.Run("should return no err when sg name is empty", func(t *testing.T) {
		err := nbClient.CreateSgBaseACL("", ovnnb.ACLDirectionFromLport)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testUpdateSgACL() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	sgName := "test_update_sg_acl_pg"
	v4AsName := GetSgV4AssociatedName(sgName)
	v6AsName := GetSgV6AssociatedName(sgName)
	pgName := GetSgPortGroupName(sgName)

	sg := &kubeovnv1.SecurityGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: sgName,
		},
		Spec: kubeovnv1.SecurityGroupSpec{
			AllowSameGroupTraffic: true,
			IngressRules: []kubeovnv1.SecurityGroupRule{
				{
					IPVersion:     "ipv4",
					RemoteType:    kubeovnv1.SgRemoteTypeAddress,
					RemoteAddress: "0.0.0.0/0",
					Protocol:      "icmp",
					Priority:      12,
					Policy:        "allow",
				},
			},
			EgressRules: []kubeovnv1.SecurityGroupRule{
				{
					IPVersion:     "ipv4",
					RemoteType:    kubeovnv1.SgRemoteTypeAddress,
					RemoteAddress: "0.0.0.0/0",
					Protocol:      "all",
					Priority:      10,
					Policy:        "allow",
				},
			},
		},
	}

	err := nbClient.CreatePortGroup(pgName, nil)
	require.NoError(t, err)

	t.Run("update securityGroup ingress acl", func(t *testing.T) {
		err = nbClient.UpdateSgACL(sg, ovnnb.ACLDirectionToLport)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		// ipv4 acl
		match := fmt.Sprintf("outport == @%s && ip4 && ip4.src == $%s", pgName, v4AsName)
		v4Acl, err := nbClient.GetACL(pgName, ovnnb.ACLDirectionToLport, util.SecurityGroupAllowPriority, match, false)
		require.NoError(t, err)
		expect := newACL(pgName, ovnnb.ACLDirectionToLport, util.SecurityGroupAllowPriority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		expect.UUID = v4Acl.UUID
		require.Equal(t, expect, v4Acl)
		require.Contains(t, pg.ACLs, v4Acl.UUID)

		// ipv6 acl
		match = fmt.Sprintf("outport == @%s && ip6 && ip6.src == $%s", pgName, v6AsName)
		v6Acl, err := nbClient.GetACL(pgName, ovnnb.ACLDirectionToLport, util.SecurityGroupAllowPriority, match, false)
		require.NoError(t, err)
		expect = newACL(pgName, ovnnb.ACLDirectionToLport, util.SecurityGroupAllowPriority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		expect.UUID = v6Acl.UUID
		require.Equal(t, expect, v6Acl)
		require.Contains(t, pg.ACLs, v6Acl.UUID)

		// rule acl
		match = fmt.Sprintf("outport == @%s && ip4 && ip4.src == 0.0.0.0/0 && icmp4", pgName)
		rulACL, err := nbClient.GetACL(pgName, ovnnb.ACLDirectionToLport, "2288", match, false)
		require.NoError(t, err)
		expect = newACL(pgName, ovnnb.ACLDirectionToLport, "2288", match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		expect.UUID = rulACL.UUID
		require.Equal(t, expect, rulACL)
		require.Contains(t, pg.ACLs, rulACL.UUID)
	})

	t.Run("update securityGroup egress acl", func(t *testing.T) {
		err = nbClient.UpdateSgACL(sg, ovnnb.ACLDirectionFromLport)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		// ipv4 acl
		match := fmt.Sprintf("inport == @%s && ip4 && ip4.dst == $%s", pgName, v4AsName)
		v4Acl, err := nbClient.GetACL(pgName, ovnnb.ACLDirectionFromLport, util.SecurityGroupAllowPriority, match, false)
		require.NoError(t, err)
		expect := newACL(pgName, ovnnb.ACLDirectionFromLport, util.SecurityGroupAllowPriority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		expect.UUID = v4Acl.UUID
		require.Equal(t, expect, v4Acl)
		require.Contains(t, pg.ACLs, v4Acl.UUID)

		// ipv6 acl
		match = fmt.Sprintf("inport == @%s && ip6 && ip6.dst == $%s", pgName, v6AsName)
		v6Acl, err := nbClient.GetACL(pgName, ovnnb.ACLDirectionFromLport, util.SecurityGroupAllowPriority, match, false)
		require.NoError(t, err)
		expect = newACL(pgName, ovnnb.ACLDirectionFromLport, util.SecurityGroupAllowPriority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		expect.UUID = v6Acl.UUID
		require.Equal(t, expect, v6Acl)
		require.Contains(t, pg.ACLs, v6Acl.UUID)

		// rule acl
		match = fmt.Sprintf("inport == @%s && ip4 && ip4.dst == 0.0.0.0/0", pgName)
		rulACL, err := nbClient.GetACL(pgName, ovnnb.ACLDirectionFromLport, "2290", match, false)
		require.NoError(t, err)
		expect = newACL(pgName, ovnnb.ACLDirectionFromLport, "2290", match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		expect.UUID = rulACL.UUID
		require.Equal(t, expect, rulACL)
		require.Contains(t, pg.ACLs, rulACL.UUID)
	})

	t.Run("should print log err when sg name is empty", func(t *testing.T) {
		sg := &kubeovnv1.SecurityGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name: "",
			},
		}
		err = nbClient.UpdateSgACL(sg, ovnnb.ACLDirectionToLport)
		require.ErrorContains(t, err, "the port group name or logical switch name is required")
	})
}

func (suite *OvnClientTestSuite) testUpdateLogicalSwitchACL() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lsName := "test_update_acl_ls"
	cidrBlock := "192.168.2.0/24,2409:8720:4a00::0/64"

	subnetAcls := []kubeovnv1.ACL{
		{
			Direction: ovnnb.ACLDirectionToLport,
			Priority:  1111,
			Match:     "ip4.src == 192.168.111.5",
			Action:    ovnnb.ACLActionAllowRelated,
		},
		{
			Direction: ovnnb.ACLDirectionFromLport,
			Priority:  1111,
			Match:     "ip4.dst == 192.168.111.50",
			Action:    ovnnb.ACLActionDrop,
		},
	}

	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = nbClient.UpdateLogicalSwitchACL(lsName, cidrBlock, nil, true)
	require.NoError(t, err)

	err = nbClient.UpdateLogicalSwitchACL("", cidrBlock, subnetAcls, true)
	require.ErrorContains(t, err, "the port group name or logical switch name is required")

	err = nbClient.UpdateLogicalSwitchACL("", cidrBlock, subnetAcls, false)
	require.ErrorContains(t, err, "the port group name or logical switch name is required")

	err = nbClient.UpdateLogicalSwitchACL(lsName, cidrBlock, subnetAcls, true)
	require.NoError(t, err)

	ls, err := nbClient.GetLogicalSwitch(lsName, false)
	require.NoError(t, err)

	for cidr := range strings.SplitSeq(cidrBlock, ",") {
		protocol := util.CheckProtocol(cidr)

		match := "ip4.src == 192.168.2.0/24 && ip4.dst == 192.168.2.0/24"
		if protocol == kubeovnv1.ProtocolIPv6 {
			match = "ip6.src == 2409:8720:4a00::0/64 && ip6.dst == 2409:8720:4a00::0/64"
		}
		ingressACL, err := nbClient.GetACL(lsName, ovnnb.ACLDirectionToLport, util.AllowEWTrafficPriority, match, false)
		require.NoError(t, err)
		ingressExpect := newACL(lsName, ovnnb.ACLDirectionToLport, util.AllowEWTrafficPriority, match, ovnnb.ACLActionAllow, util.NetpolACLTier)
		ingressExpect.UUID = ingressACL.UUID
		ingressExpect.ExternalIDs["subnet"] = lsName
		require.Equal(t, ingressExpect, ingressACL)
		require.Contains(t, ls.ACLs, ingressACL.UUID)
		egressACL, err := nbClient.GetACL(lsName, ovnnb.ACLDirectionFromLport, util.AllowEWTrafficPriority, match, false)
		require.NoError(t, err)
		egressExpect := newACL(lsName, ovnnb.ACLDirectionFromLport, util.AllowEWTrafficPriority, match, ovnnb.ACLActionAllow, util.NetpolACLTier)
		egressExpect.UUID = egressACL.UUID
		egressExpect.ExternalIDs["subnet"] = lsName
		require.Equal(t, egressExpect, egressACL)
		require.Contains(t, ls.ACLs, egressACL.UUID)
	}

	for _, subnetACL := range subnetAcls {
		acl, err := nbClient.GetACL(lsName, subnetACL.Direction, strconv.Itoa(subnetACL.Priority), subnetACL.Match, false)
		require.NoError(t, err)
		expect := newACL(lsName, subnetACL.Direction, strconv.Itoa(subnetACL.Priority), subnetACL.Match, subnetACL.Action, util.NetpolACLTier)
		expect.UUID = acl.UUID
		expect.ExternalIDs["subnet"] = lsName
		require.Equal(t, expect, acl)
		require.Contains(t, ls.ACLs, acl.UUID)
	}
}

func (suite *OvnClientTestSuite) testSetACLLog() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := GetSgPortGroupName("test_set_acl_log")

	err := nbClient.CreatePortGroup(pgName, nil)
	require.NoError(t, err)

	t.Run("set ingress acl log to false", func(t *testing.T) {
		match := fmt.Sprintf("outport == @%s && ip", pgName)
		acl := newACL(pgName, ovnnb.ACLDirectionToLport, util.IngressDefaultDrop, match, ovnnb.ACLActionDrop, util.NetpolACLTier, func(acl *ovnnb.ACL) {
			acl.Name = &pgName
			acl.Log = true
			acl.Severity = ptr.To(ovnnb.ACLSeverityWarning)
		})

		err = nbClient.CreateAcls(pgName, portGroupKey, acl)
		require.NoError(t, err)

		err = nbClient.SetACLLog(pgName, false, true)
		require.NoError(t, err)

		acl, err = nbClient.GetACL(pgName, ovnnb.ACLDirectionToLport, util.IngressDefaultDrop, match, false)
		require.NoError(t, err)
		require.False(t, acl.Log)

		err = nbClient.SetACLLog(pgName, false, true)
		require.NoError(t, err)
	})

	t.Run("set egress acl log to false", func(t *testing.T) {
		match := fmt.Sprintf("inport == @%s && ip", pgName)
		acl := newACL(pgName, ovnnb.ACLDirectionFromLport, util.IngressDefaultDrop, match, ovnnb.ACLActionDrop, util.NetpolACLTier, func(acl *ovnnb.ACL) {
			acl.Name = &pgName
			acl.Log = false
			acl.Severity = ptr.To(ovnnb.ACLSeverityWarning)
		})

		err = nbClient.CreateAcls(pgName, portGroupKey, acl)
		require.NoError(t, err)

		err = nbClient.SetACLLog(pgName, true, false)
		require.NoError(t, err)

		acl, err = nbClient.GetACL(pgName, ovnnb.ACLDirectionFromLport, util.IngressDefaultDrop, match, false)
		require.NoError(t, err)
		require.True(t, acl.Log)

		err = nbClient.SetACLLog(pgName, true, false)
		require.NoError(t, err)
	})

	t.Run("set log for non-exist pgName", func(t *testing.T) {
		err := nbClient.SetACLLog("non-exist-pgName", true, false)
		require.NoError(t, err)
	})

	t.Run("test empty pgName", func(t *testing.T) {
		err := nbClient.SetACLLog("", true, false)
		require.ErrorContains(t, err, "the port group name or logical switch name is required")
	})
}

func (suite *OvnClientTestSuite) testSetLogicalSwitchPrivate() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	nodeSwitchCidrBlock := "100.64.0.0/16,fd00:100:64::/112"
	cidrBlock := "10.244.0.0/16,fc00::af4:0/112"
	allowSubnets := []string{
		"10.230.0.0/16",
		"10.240.0.0/16",
		"fc00::af9:0/112",
		"fc00::afa:0/112",
	}
	direction := ovnnb.ACLDirectionToLport

	t.Run("subnet protocol is dual", func(t *testing.T) {
		t.Parallel()

		lsName := "test_set_private_ls_dual"
		err := nbClient.CreateBareLogicalSwitch(lsName)
		require.NoError(t, err)

		err = nbClient.SetLogicalSwitchPrivate(lsName, cidrBlock, nodeSwitchCidrBlock, allowSubnets)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Len(t, ls.ACLs, 9)

		// default drop acl
		match := "ip"
		acl, err := nbClient.GetACL(lsName, direction, util.DefaultDropPriority, match, false)
		require.NoError(t, err)
		require.Contains(t, ls.ACLs, acl.UUID)

		// same subnet acl
		for cidr := range strings.SplitSeq(cidrBlock, ",") {
			protocol := util.CheckProtocol(cidr)

			match := fmt.Sprintf(`ip4.src == %s && ip4.dst == %s`, cidr, cidr)
			if protocol == kubeovnv1.ProtocolIPv6 {
				match = fmt.Sprintf(`ip6.src == %s && ip6.dst == %s`, cidr, cidr)
			}

			acl, err = nbClient.GetACL(lsName, direction, util.SubnetAllowPriority, match, false)
			require.NoError(t, err)
			require.Contains(t, ls.ACLs, acl.UUID)

			// allow subnet acl
			for _, subnet := range allowSubnets {
				protocol := util.CheckProtocol(cidr)

				allowProtocol := util.CheckProtocol(subnet)
				if allowProtocol != protocol {
					continue
				}

				match = fmt.Sprintf("(ip4.src == %s && ip4.dst == %s) || (ip4.src == %s && ip4.dst == %s)", cidr, subnet, subnet, cidr)
				if protocol == kubeovnv1.ProtocolIPv6 {
					match = fmt.Sprintf("(ip6.src == %s && ip6.dst == %s) || (ip6.src == %s && ip6.dst == %s)", cidr, subnet, subnet, cidr)
				}

				acl, err = nbClient.GetACL(lsName, direction, util.SubnetCrossAllowPriority, match, false)
				require.NoError(t, err)
				require.Contains(t, ls.ACLs, acl.UUID)
			}
		}

		// node subnet acl
		for cidr := range strings.SplitSeq(nodeSwitchCidrBlock, ",") {
			protocol := util.CheckProtocol(cidr)

			match := "ip4.src == " + cidr
			if protocol == kubeovnv1.ProtocolIPv6 {
				match = "ip6.src == " + cidr
			}

			acl, err = nbClient.GetACL(lsName, direction, util.NodeAllowPriority, match, false)
			require.NoError(t, err)
			require.Contains(t, ls.ACLs, acl.UUID)
		}
	})

	t.Run("subnet protocol is ipv4", func(t *testing.T) {
		t.Parallel()

		lsName := "test_set_private_ls_v4"
		err := nbClient.CreateBareLogicalSwitch(lsName)
		require.NoError(t, err)

		cidrBlock := "10.244.0.0/16"
		err = nbClient.SetLogicalSwitchPrivate(lsName, cidrBlock, nodeSwitchCidrBlock, allowSubnets)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Len(t, ls.ACLs, 5)

		// default drop acl
		match := "ip"
		acl, err := nbClient.GetACL(lsName, direction, util.DefaultDropPriority, match, false)
		require.NoError(t, err)
		require.Contains(t, ls.ACLs, acl.UUID)

		// same subnet acl
		for cidr := range strings.SplitSeq(cidrBlock, ",") {
			protocol := util.CheckProtocol(cidr)

			match := fmt.Sprintf(`ip4.src == %s && ip4.dst == %s`, cidr, cidr)
			if protocol == kubeovnv1.ProtocolIPv6 {
				match = fmt.Sprintf(`ip6.src == %s && ip6.dst == %s`, cidr, cidr)
			}

			acl, err = nbClient.GetACL(lsName, direction, util.SubnetAllowPriority, match, false)
			require.NoError(t, err)
			require.Contains(t, ls.ACLs, acl.UUID)

			// allow subnet acl
			for _, subnet := range allowSubnets {
				protocol := util.CheckProtocol(cidr)

				allowProtocol := util.CheckProtocol(subnet)
				if allowProtocol != protocol {
					continue
				}

				match = fmt.Sprintf("(ip4.src == %s && ip4.dst == %s) || (ip4.src == %s && ip4.dst == %s)", cidr, subnet, subnet, cidr)
				if protocol == kubeovnv1.ProtocolIPv6 {
					match = fmt.Sprintf("(ip6.src == %s && ip6.dst == %s) || (ip6.src == %s && ip6.dst == %s)", cidr, subnet, subnet, cidr)
				}

				acl, err = nbClient.GetACL(lsName, direction, util.SubnetCrossAllowPriority, match, false)
				require.NoError(t, err)
				require.Contains(t, ls.ACLs, acl.UUID)
			}
		}

		// node subnet acl
		for cidr := range strings.SplitSeq(nodeSwitchCidrBlock, ",") {
			protocol := util.CheckProtocol(cidr)

			match := "ip4.src == " + cidr
			if protocol == kubeovnv1.ProtocolIPv6 {
				match = "ip6.src == " + cidr
			}

			acl, err = nbClient.GetACL(lsName, direction, util.NodeAllowPriority, match, false)
			if protocol == kubeovnv1.ProtocolIPv4 {
				require.NoError(t, err)
				require.Contains(t, ls.ACLs, acl.UUID)
			} else {
				require.ErrorContains(t, err, "not found acl")
			}
		}
	})

	t.Run("should print log err when ls name is empty", func(t *testing.T) {
		err := nbClient.SetLogicalSwitchPrivate("", cidrBlock, nodeSwitchCidrBlock, allowSubnets)
		require.ErrorContains(t, err, "the port group name or logical switch name is required")
	})
}

func (suite *OvnClientTestSuite) testNewSgRuleACL() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	sgName := "test_create_sg_acl_pg"
	pgName := GetSgPortGroupName(sgName)
	highestPriority, _ := strconv.Atoi(util.SecurityGroupHighestPriority)

	t.Run("create securityGroup type sg acl", func(t *testing.T) {
		t.Parallel()

		sgRule := kubeovnv1.SecurityGroupRule{
			IPVersion:           "ipv4",
			RemoteType:          kubeovnv1.SgRemoteTypeSg,
			RemoteSecurityGroup: "ovn.sg.test_sg",
			Protocol:            "icmp",
			Priority:            12,
			Policy:              "allow",
		}
		priority := strconv.Itoa(highestPriority - sgRule.Priority)

		acl, err := nbClient.newSgRuleACL(sgName, ovnnb.ACLDirectionToLport, sgRule)
		require.NoError(t, err)

		match := fmt.Sprintf("outport == @%s && ip4 && ip4.src == $%s && icmp4", pgName, GetSgV4AssociatedName(sgRule.RemoteSecurityGroup))
		expect := newACL(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		expect.UUID = acl.UUID
		require.Equal(t, expect, acl)
	})

	t.Run("create address type sg acl", func(t *testing.T) {
		t.Parallel()

		sgRule := kubeovnv1.SecurityGroupRule{
			IPVersion:     "ipv4",
			RemoteType:    kubeovnv1.SgRemoteTypeAddress,
			RemoteAddress: "10.10.10.12/24",
			Protocol:      "icmp",
			Priority:      12,
			Policy:        "allow",
		}
		priority := strconv.Itoa(highestPriority - sgRule.Priority)

		acl, err := nbClient.newSgRuleACL(sgName, ovnnb.ACLDirectionToLport, sgRule)
		require.NoError(t, err)

		match := fmt.Sprintf("outport == @%s && ip4 && ip4.src == %s && icmp4", pgName, sgRule.RemoteAddress)
		expect := newACL(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		expect.UUID = acl.UUID
		require.Equal(t, expect, acl)
	})

	t.Run("create ipv6 acl", func(t *testing.T) {
		t.Parallel()

		sgRule := kubeovnv1.SecurityGroupRule{
			IPVersion:     "ipv6",
			RemoteType:    kubeovnv1.SgRemoteTypeAddress,
			RemoteAddress: "fe80::200:ff:fe04:2611/64",
			Protocol:      "icmp",
			Priority:      12,
			Policy:        "allow",
		}
		priority := strconv.Itoa(highestPriority - sgRule.Priority)

		acl, err := nbClient.newSgRuleACL(sgName, ovnnb.ACLDirectionToLport, sgRule)
		require.NoError(t, err)

		match := fmt.Sprintf("outport == @%s && ip6 && ip6.src == %s && icmp6", pgName, sgRule.RemoteAddress)
		expect := newACL(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		expect.UUID = acl.UUID
		require.Equal(t, expect, acl)
	})

	t.Run("create egress sg acl", func(t *testing.T) {
		t.Parallel()

		sgRule := kubeovnv1.SecurityGroupRule{
			IPVersion:     "ipv4",
			RemoteType:    kubeovnv1.SgRemoteTypeAddress,
			RemoteAddress: "10.10.10.12/24",
			Protocol:      "icmp",
			Priority:      12,
			Policy:        "allow",
		}
		priority := strconv.Itoa(highestPriority - sgRule.Priority)

		acl, err := nbClient.newSgRuleACL(sgName, ovnnb.ACLDirectionFromLport, sgRule)
		require.NoError(t, err)

		match := fmt.Sprintf("inport == @%s && ip4 && ip4.dst == %s && icmp4", pgName, sgRule.RemoteAddress)
		expect := newACL(pgName, ovnnb.ACLDirectionFromLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		expect.UUID = acl.UUID
		require.Equal(t, expect, acl)
	})

	t.Run("create drop sg acl", func(t *testing.T) {
		t.Parallel()

		sgRule := kubeovnv1.SecurityGroupRule{
			IPVersion:     "ipv4",
			RemoteType:    kubeovnv1.SgRemoteTypeAddress,
			RemoteAddress: "10.10.10.12/24",
			Protocol:      "icmp",
			Priority:      21,
			Policy:        "drop",
		}
		priority := strconv.Itoa(highestPriority - sgRule.Priority)

		acl, err := nbClient.newSgRuleACL(sgName, ovnnb.ACLDirectionToLport, sgRule)
		require.NoError(t, err)

		match := fmt.Sprintf("outport == @%s && ip4 && ip4.src == %s && icmp4", pgName, sgRule.RemoteAddress)
		expect := newACL(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionDrop, util.NetpolACLTier)
		expect.UUID = acl.UUID
		require.Equal(t, expect, acl)
	})

	t.Run("create tcp sg acl", func(t *testing.T) {
		t.Parallel()

		sgRule := kubeovnv1.SecurityGroupRule{
			IPVersion:     "ipv4",
			RemoteType:    kubeovnv1.SgRemoteTypeAddress,
			RemoteAddress: "10.10.10.12/24",
			Protocol:      "tcp",
			Priority:      12,
			Policy:        "allow",
			PortRangeMin:  12345,
			PortRangeMax:  12360,
		}
		priority := strconv.Itoa(highestPriority - sgRule.Priority)

		acl, err := nbClient.newSgRuleACL(sgName, ovnnb.ACLDirectionToLport, sgRule)
		require.NoError(t, err)

		match := fmt.Sprintf("outport == @%s && ip4 && ip4.src == %s && %d <= tcp.dst <= %d", pgName, sgRule.RemoteAddress, sgRule.PortRangeMin, sgRule.PortRangeMax)
		expect := newACL(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		expect.UUID = acl.UUID
		require.Equal(t, expect, acl)
	})
}

func (suite *OvnClientTestSuite) testCreateAcls() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test-create-acls-pg"
	priority := "5000"
	basePort := 12300
	matchPrefix := "outport == @ovn.sg.test_create_acl_pg && ip"
	acls := make([]*ovnnb.ACL, 0, 3)

	t.Run("add acl to port group", func(t *testing.T) {
		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		for i := range 3 {
			match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
			acl, err := nbClient.newACL(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			require.NoError(t, err)
			acls = append(acls, acl)
		}

		err = nbClient.CreateAcls(pgName, portGroupKey, append(acls, nil)...)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		for i := range 3 {
			match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
			acl, err := nbClient.GetACL(pgName, ovnnb.ACLDirectionToLport, priority, match, false)
			require.NoError(t, err)
			require.Equal(t, match, acl.Match)

			require.Contains(t, pg.ACLs, acl.UUID)
		}
	})

	t.Run("add acl to logical switch", func(t *testing.T) {
		lsName := "test-create-acls-ls"
		err := nbClient.CreateBareLogicalSwitch(lsName)
		require.NoError(t, err)

		for i := range 3 {
			match := fmt.Sprintf("%s && udp.dst == %d", matchPrefix, basePort+i)
			acl, err := nbClient.newACL(lsName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			require.NoError(t, err)
			acls = append(acls, acl)
		}

		err = nbClient.CreateAcls(lsName, LogicalSwitchKey, append(acls, nil)...)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)

		for i := range 3 {
			match := fmt.Sprintf("%s && udp.dst == %d", matchPrefix, basePort+i)
			acl, err := nbClient.GetACL(lsName, ovnnb.ACLDirectionToLport, priority, match, false)
			require.NoError(t, err)
			require.Equal(t, match, acl.Match)

			require.Contains(t, ls.ACLs, acl.UUID)
		}
	})

	t.Run("acl parent type is wrong", func(t *testing.T) {
		err := nbClient.CreateAcls(pgName, "", nil)
		require.ErrorContains(t, err, "acl parent type must be 'pg' or 'ls'")

		err = nbClient.CreateAcls(pgName, "wrong_key", nil)
		require.ErrorContains(t, err, "acl parent type must be 'pg' or 'ls'")
	})
}

func (suite *OvnClientTestSuite) testDeleteAcls() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient
	pgName := "test-del-acls-pg"
	lsName := "test-del-acls-ls"
	matchPrefix := "outport == @ovn.sg.test_del_acl_pg && ip"

	err := nbClient.CreatePortGroup(pgName, nil)
	require.NoError(t, err)

	err = nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("delete all direction acls from port group", func(t *testing.T) {
		priority := "5601"
		basePort := 5601
		acls := make([]*ovnnb.ACL, 0, 5)

		// to-lport
		for i := range 2 {
			match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
			acl, err := nbClient.newACL(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			require.NoError(t, err)
			acls = append(acls, acl)
		}

		// from-lport
		for i := range 3 {
			match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
			acl, err := nbClient.newACL(pgName, ovnnb.ACLDirectionFromLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			require.NoError(t, err)
			acls = append(acls, acl)
		}

		err = nbClient.CreateAcls(pgName, portGroupKey, acls...)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Len(t, pg.ACLs, 5)

		err = nbClient.DeleteAcls(pgName, portGroupKey, "", nil)
		require.NoError(t, err)

		pg, err = nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Empty(t, pg.ACLs)
	})

	t.Run("delete one-way acls from port group", func(t *testing.T) {
		priority := "5701"
		basePort := 5701
		acls := make([]*ovnnb.ACL, 0, 5)

		// to-lport
		for i := range 2 {
			match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
			acl, err := nbClient.newACL(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			require.NoError(t, err)
			acls = append(acls, acl)
		}

		// from-lport
		for i := range 3 {
			match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
			acl, err := nbClient.newACL(pgName, ovnnb.ACLDirectionFromLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			require.NoError(t, err)
			acls = append(acls, acl)
		}

		err = nbClient.CreateAcls(pgName, portGroupKey, acls...)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Len(t, pg.ACLs, 5)

		/* delete to-lport direction acl */
		err = nbClient.DeleteAcls(pgName, portGroupKey, ovnnb.ACLDirectionToLport, nil)
		require.NoError(t, err)

		pg, err = nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Len(t, pg.ACLs, 3)

		/* delete from-lport direction acl */
		err = nbClient.DeleteAcls(pgName, portGroupKey, ovnnb.ACLDirectionFromLport, nil)
		require.NoError(t, err)

		pg, err = nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Empty(t, pg.ACLs)
	})

	t.Run("delete all direction acls from logical switch", func(t *testing.T) {
		priority := "5601"
		basePort := 5601
		acls := make([]*ovnnb.ACL, 0, 5)

		// to-lport
		for i := range 2 {
			match := fmt.Sprintf("%s && udp.dst == %d", matchPrefix, basePort+i)
			acl, err := nbClient.newACL(lsName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			require.NoError(t, err)
			acls = append(acls, acl)
		}

		// from-lport
		for i := range 3 {
			match := fmt.Sprintf("%s && udp.dst == %d", matchPrefix, basePort+i)
			acl, err := nbClient.newACL(lsName, ovnnb.ACLDirectionFromLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			require.NoError(t, err)
			acls = append(acls, acl)
		}

		err = nbClient.CreateAcls(lsName, LogicalSwitchKey, acls...)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Len(t, ls.ACLs, 5)

		err = nbClient.DeleteAcls(lsName, LogicalSwitchKey, "", nil)
		require.NoError(t, err)

		ls, err = nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Empty(t, ls.ACLs)
	})

	t.Run("delete one-way acls from logical switch", func(t *testing.T) {
		priority := "5701"
		basePort := 5701
		acls := make([]*ovnnb.ACL, 0, 5)

		// to-lport
		for i := range 2 {
			match := fmt.Sprintf("%s && udp.dst == %d", matchPrefix, basePort+i)
			acl, err := nbClient.newACL(lsName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			require.NoError(t, err)
			acls = append(acls, acl)
		}

		// from-lport
		for i := range 3 {
			match := fmt.Sprintf("%s && udp.dst == %d", matchPrefix, basePort+i)
			acl, err := nbClient.newACL(lsName, ovnnb.ACLDirectionFromLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			require.NoError(t, err)
			acls = append(acls, acl)
		}

		err = nbClient.CreateAcls(lsName, LogicalSwitchKey, acls...)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Len(t, ls.ACLs, 5)

		/* delete to-lport direction acl */
		err = nbClient.DeleteAcls(lsName, LogicalSwitchKey, ovnnb.ACLDirectionToLport, nil)
		require.NoError(t, err)

		ls, err = nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Len(t, ls.ACLs, 3)

		/* delete from-lport direction acl */
		err = nbClient.DeleteAcls(lsName, LogicalSwitchKey, ovnnb.ACLDirectionFromLport, nil)
		require.NoError(t, err)

		ls, err = nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Empty(t, ls.ACLs)
	})

	t.Run("delete acls with additional external ids", func(t *testing.T) {
		priority := "5801"
		basePort := 5801
		acls := make([]*ovnnb.ACL, 0, 5)

		// to-lport

		match := fmt.Sprintf("%s && udp.dst == %d", matchPrefix, basePort)
		acl, err := nbClient.newACL(lsName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, func(acl *ovnnb.ACL) {
			if acl.ExternalIDs == nil {
				acl.ExternalIDs = make(map[string]string)
			}
			acl.ExternalIDs["subnet"] = lsName
		})
		require.NoError(t, err)
		acls = append(acls, acl)

		err = nbClient.CreateAcls(lsName, LogicalSwitchKey, acls...)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Len(t, ls.ACLs, 1)

		newACL := &ovnnb.ACL{UUID: ls.ACLs[0]}
		err = nbClient.GetEntityInfo(newACL)
		require.NoError(t, err)

		/* delete to-lport direction acl */
		err = nbClient.DeleteAcls(lsName, LogicalSwitchKey, ovnnb.ACLDirectionToLport, map[string]string{"subnet": lsName})
		require.NoError(t, err)

		ls, err = nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Empty(t, ls.ACLs)
	})

	t.Run("should no err when acls does not exist", func(t *testing.T) {
		err = nbClient.DeleteAcls("test-nonexist-ls", LogicalSwitchKey, ovnnb.ACLDirectionToLport, map[string]string{"subnet": "test-nonexist-ls"})
		require.NoError(t, err)
	})

	t.Run("fail nb client should log err", func(t *testing.T) {
		priority := "5805"
		basePort := 5805
		acls := make([]*ovnnb.ACL, 0, 5)

		match := fmt.Sprintf("%s && udp.dst == %d", matchPrefix, basePort)
		acl, err := failedNbClient.newACL(lsName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, func(acl *ovnnb.ACL) {
			if acl.ExternalIDs == nil {
				acl.ExternalIDs = make(map[string]string)
			}
			acl.ExternalIDs["subnet"] = lsName
		})
		require.NoError(t, err)
		acls = append(acls, acl)

		err = failedNbClient.CreateAcls(lsName, LogicalSwitchKey, acls...)
		require.Error(t, err)
		// TODO:// should err but not for now
		err = failedNbClient.DeleteAcls(lsName, LogicalSwitchKey, ovnnb.ACLDirectionToLport, map[string]string{"subnet": lsName})
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testDeleteACL() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test-del-acl-pg"
	lsName := "test-del-acl-ls"
	matchPrefix := "outport == @ovn.sg.test_del_acl_pg && ip"

	err := nbClient.CreatePortGroup(pgName, nil)
	require.NoError(t, err)

	err = nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("delete acl from port group", func(t *testing.T) {
		priority := "5601"
		basePort := 5601

		match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort)
		acl, err := nbClient.newACL(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		require.NoError(t, err)

		err = nbClient.CreateAcls(pgName, portGroupKey, acl)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Len(t, pg.ACLs, 1)

		err = nbClient.DeleteACL(pgName, portGroupKey, ovnnb.ACLDirectionToLport, priority, match)
		require.NoError(t, err)

		pg, err = nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Empty(t, pg.ACLs)
	})

	t.Run("delete all direction acls from logical switch", func(t *testing.T) {
		priority := "5601"
		basePort := 5601

		match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort)
		acl, err := nbClient.newACL(lsName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		require.NoError(t, err)

		err = nbClient.CreateAcls(lsName, LogicalSwitchKey, acl)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Len(t, ls.ACLs, 1)

		err = nbClient.DeleteACL(lsName, LogicalSwitchKey, ovnnb.ACLDirectionToLport, priority, match)
		require.NoError(t, err)

		ls, err = nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Empty(t, ls.ACLs)
	})

	t.Run("delete acl with empty pgName", func(t *testing.T) {
		priority := "5601"
		basePort := 5601
		match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort)

		err = nbClient.DeleteACL("", portGroupKey, ovnnb.ACLDirectionToLport, priority, match)
		require.ErrorContains(t, err, "the port group name or logical switch name is required")
	})
}

func (suite *OvnClientTestSuite) testGetACL() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test_get_acl_pg"
	priority := "2000"
	match := "ip4.dst == 100.64.0.0/16"

	err := nbClient.CreatePortGroup(pgName, nil)
	require.NoError(t, err)

	acl, err := nbClient.newACL(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
	require.NoError(t, err)

	err = nbClient.CreateAcls(pgName, portGroupKey, acl)
	require.NoError(t, err)

	t.Run("direction, priority and match are same", func(t *testing.T) {
		t.Parallel()
		acl, err := nbClient.GetACL(pgName, ovnnb.ACLDirectionToLport, priority, match, false)
		require.NoError(t, err)
		require.Equal(t, ovnnb.ACLDirectionToLport, acl.Direction)
		require.Equal(t, 2000, acl.Priority)
		require.Equal(t, match, acl.Match)
		require.Equal(t, ovnnb.ACLActionAllowRelated, acl.Action)
	})

	t.Run("direction, priority and match are not all same", func(t *testing.T) {
		t.Parallel()

		_, err := nbClient.GetACL(pgName, ovnnb.ACLDirectionFromLport, priority, match, false)
		require.ErrorContains(t, err, "not found acl")

		_, err = nbClient.GetACL(pgName, ovnnb.ACLDirectionToLport, "1010", match, false)
		require.ErrorContains(t, err, "not found acl")

		_, err = nbClient.GetACL(pgName, ovnnb.ACLDirectionToLport, priority, match+" && tcp", false)
		require.ErrorContains(t, err, "not found acl")
	})

	t.Run("should no err when direction, priority and match are not all same but ignoreNotFound=true", func(t *testing.T) {
		t.Parallel()

		_, err := nbClient.GetACL(pgName, ovnnb.ACLDirectionFromLport, priority, match, true)
		require.NoError(t, err)
	})

	t.Run("no acl belongs to parent exist", func(t *testing.T) {
		t.Parallel()

		_, err := nbClient.GetACL(pgName+"_1", ovnnb.ACLDirectionFromLport, priority, match, false)
		require.ErrorContains(t, err, "not found acl")
	})

	t.Run("more than one same acl", func(t *testing.T) {
		t.Parallel()
		newPgName := "test_get_acl_pg_1"

		err := nbClient.CreatePortGroup(newPgName, nil)
		require.NoError(t, err)

		acl1, err := nbClient.newACL(newPgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		require.NoError(t, err)
		acl2, err := nbClient.newACL(newPgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		require.NoError(t, err)

		err = nbClient.CreateAcls(newPgName, portGroupKey, acl1, acl2)
		require.NoError(t, err)

		_, err = nbClient.GetACL(newPgName, ovnnb.ACLDirectionToLport, priority, match, true)
		require.ErrorContains(t, err, "more than one acl with same")
	})
}

func (suite *OvnClientTestSuite) testListAcls() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test-list-acl-pg"
	basePort := 50000

	err := nbClient.CreatePortGroup(pgName, nil)
	require.NoError(t, err)

	matchPrefix := "outport == @ovn.sg.test_list_acl_pg && ip"
	// create two to-lport acl
	for i := range 2 {
		match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
		acl, err := nbClient.newACL(pgName, ovnnb.ACLDirectionToLport, "9999", match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		require.NoError(t, err)

		err = nbClient.CreateAcls(pgName, portGroupKey, acl)
		require.NoError(t, err)
	}

	// create two from-lport acl
	for i := range 3 {
		match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
		acl, err := nbClient.newACL(pgName, ovnnb.ACLDirectionFromLport, "9999", match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		require.NoError(t, err)

		err = nbClient.CreateAcls(pgName, portGroupKey, acl)
		require.NoError(t, err)
	}

	/* list all direction acl */
	out, err := nbClient.ListAcls("", nil)
	require.NoError(t, err)
	count := 0
	for _, v := range out {
		if strings.Contains(v.Match, matchPrefix) {
			count++
		}
	}
	require.Equal(t, count, 5)
}

func (suite *OvnClientTestSuite) testNewACL() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test-new-acl-pg"
	priority := "1000"
	match := "outport==@ovn.sg.test_create_acl_pg && ip"
	options := func(acl *ovnnb.ACL) {
		acl.Log = true
		acl.Severity = ptr.To(ovnnb.ACLSeverityWarning)
		acl.Name = &pgName
	}

	expect := &ovnnb.ACL{
		Name:      &pgName,
		Action:    ovnnb.ACLActionAllowRelated,
		Direction: ovnnb.ACLDirectionToLport,
		Match:     match,
		Priority:  1000,
		ExternalIDs: map[string]string{
			aclParentKey: pgName,
		},
		Log:      true,
		Severity: ptr.To(ovnnb.ACLSeverityWarning),
		Tier:     util.NetpolACLTier,
	}

	acl, err := nbClient.newACL(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, options)
	require.NoError(t, err)
	expect.UUID = acl.UUID
	require.Equal(t, expect, acl)

	err = nbClient.CreatePortGroup(pgName, nil)
	require.NoError(t, err)
	err = nbClient.CreateAcls(pgName, portGroupKey, acl)
	require.NoError(t, err)

	acl, err = nbClient.newACL(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, options)
	require.Nil(t, err)
	require.Nil(t, acl)
}

func (suite *OvnClientTestSuite) testNewACLWithoutCheck() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test-new-acl-without-check"

	t.Run("direction, priority, match or action is empty", func(t *testing.T) {
		t.Parallel()
		acl, err := nbClient.newACLWithoutCheck(pgName, ovnnb.ACLDirectionToLport, "", "", ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		require.ErrorContains(t, err, fmt.Sprintf("acl 'direction %s' and 'priority %s' and 'match %s' and 'action %s' is required", ovnnb.ACLDirectionToLport, "", "", ovnnb.ACLActionAllowRelated))
		require.Nil(t, acl)
	})
}

func (suite *OvnClientTestSuite) testnewNetworkPolicyACLMatch() {
	t := suite.T()
	t.Parallel()

	pgName := "test-new-acl-m-pg"
	asAllowName := "test.default.xx.allow.ipv4"
	asExceptName := "test.default.xx.except.ipv4"

	t.Run("has ingress network policy port", func(t *testing.T) {
		t.Parallel()

		npp := mockNetworkPolicyPort()
		matches := newNetworkPolicyACLMatch(pgName, asAllowName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionToLport, npp, nil)
		require.ElementsMatch(t, []string{
			fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s && tcp.dst == %d", pgName, asAllowName, asExceptName, npp[0].Port.IntVal),
			fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s && %d <= tcp.dst <= %d", pgName, asAllowName, asExceptName, npp[1].Port.IntVal, *npp[1].EndPort),
		}, matches)
	})

	t.Run("has egress network policy port", func(t *testing.T) {
		t.Parallel()

		npp := mockNetworkPolicyPort()

		matches := newNetworkPolicyACLMatch(pgName, asAllowName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionFromLport, npp, nil)
		require.ElementsMatch(t, []string{
			fmt.Sprintf("inport == @%s && ip && ip4.dst == $%s && ip4.dst != $%s && tcp.dst == %d", pgName, asAllowName, asExceptName, npp[0].Port.IntVal),
			fmt.Sprintf("inport == @%s && ip && ip4.dst == $%s && ip4.dst != $%s && %d <= tcp.dst <= %d", pgName, asAllowName, asExceptName, npp[1].Port.IntVal, *npp[1].EndPort),
		}, matches)
	})

	t.Run("network policy port is nil", func(t *testing.T) {
		t.Parallel()

		matches := newNetworkPolicyACLMatch(pgName, asAllowName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionToLport, nil, nil)
		require.ElementsMatch(t, []string{
			fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s", pgName, asAllowName, asExceptName),
		}, matches)
	})

	t.Run("has network policy port but port is not set", func(t *testing.T) {
		t.Parallel()

		npp := mockNetworkPolicyPort()
		npp[1].Port = nil

		matches := newNetworkPolicyACLMatch(pgName, asAllowName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionToLport, npp, nil)
		require.ElementsMatch(t, []string{
			fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s && tcp.dst == %d", pgName, asAllowName, asExceptName, npp[0].Port.IntVal),
			fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s && tcp", pgName, asAllowName, asExceptName),
		}, matches)
	})

	t.Run("has network policy port but endPort is not set", func(t *testing.T) {
		t.Parallel()

		t.Run("port type is Int", func(t *testing.T) {
			t.Parallel()
			npp := mockNetworkPolicyPort()
			npp[1].EndPort = nil

			matches := newNetworkPolicyACLMatch(pgName, asAllowName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionToLport, npp, nil)
			require.ElementsMatch(t, []string{
				fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s && tcp.dst == %d", pgName, asAllowName, asExceptName, npp[0].Port.IntVal),
				fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s && tcp.dst == %d", pgName, asAllowName, asExceptName, npp[1].Port.IntVal),
			}, matches)
		})

		t.Run("port type is String", func(t *testing.T) {
			t.Parallel()
			protocolTCP := v1.ProtocolTCP
			npp := []netv1.NetworkPolicyPort{
				{
					Port: &intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "test-pod-port",
					},
					Protocol: &protocolTCP,
				},
			}

			namedPortMap := map[string]*util.NamedPortInfo{
				"test-pod-port": {
					PortID: 13455,
				},
			}
			matches := newNetworkPolicyACLMatch(pgName, asAllowName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionToLport, npp, namedPortMap)
			require.ElementsMatch(t, []string{
				fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s && tcp.dst == %d", pgName, asAllowName, asExceptName, 13455),
			}, matches)
		})

		t.Run("port type is String and not find named port", func(t *testing.T) {
			t.Parallel()
			protocolTCP := v1.ProtocolTCP
			npp := []netv1.NetworkPolicyPort{
				{
					Port: &intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "test-pod-port-x",
					},
					Protocol: &protocolTCP,
				},
			}

			namedPortMap := map[string]*util.NamedPortInfo{
				"test-pod-port": {
					PortID: 13455,
				},
			}
			matches := newNetworkPolicyACLMatch(pgName, asAllowName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionToLport, npp, namedPortMap)
			require.ElementsMatch(t, []string{
				fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s && tcp.dst == %d", pgName, asAllowName, asExceptName, 0),
			}, matches)
		})
	})
}

func (suite *OvnClientTestSuite) testACLFilter() {
	t := suite.T()
	t.Parallel()

	pgName := "test-filter-acl-pg"

	acls := make([]*ovnnb.ACL, 0)

	t.Run("filter acl", func(t *testing.T) {
		t.Parallel()

		match := "outport == @ovn.sg.test_list_acl_pg && ip"
		// create two to-lport acl
		for range 2 {
			acl := newACL(pgName, ovnnb.ACLDirectionToLport, "9999", match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			acls = append(acls, acl)
		}

		// create two to-lport acl without acl parent key
		for range 2 {
			acl := newACL(pgName, ovnnb.ACLDirectionToLport, "9999", match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			acl.ExternalIDs = nil
			acls = append(acls, acl)
		}

		// create two from-lport acl
		for range 3 {
			acl := newACL(pgName, ovnnb.ACLDirectionFromLport, "9999", match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			acls = append(acls, acl)
		}

		// create four from-lport acl with other acl parent key
		for range 4 {
			acl := newACL(pgName, ovnnb.ACLDirectionFromLport, "9999", match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			acl.ExternalIDs[aclParentKey] = pgName + "-test"
			acls = append(acls, acl)
		}

		/* include all direction acl */
		filterFunc := aclFilter("", nil)
		count := 0
		for _, acl := range acls {
			if filterFunc(acl) {
				count++
			}
		}
		require.Equal(t, count, 11)

		/* include all direction acl with external ids */
		filterFunc = aclFilter("", map[string]string{aclParentKey: pgName})
		count = 0
		for _, acl := range acls {
			if filterFunc(acl) {
				count++
			}
		}
		require.Equal(t, count, 5)

		/* include to-lport acl */
		filterFunc = aclFilter(ovnnb.ACLDirectionToLport, nil)
		count = 0
		for _, acl := range acls {
			if filterFunc(acl) {
				count++
			}
		}
		require.Equal(t, count, 4)

		/* include to-lport acl with external ids */
		filterFunc = aclFilter(ovnnb.ACLDirectionToLport, map[string]string{aclParentKey: pgName})
		count = 0
		for _, acl := range acls {
			if filterFunc(acl) {
				count++
			}
		}
		require.Equal(t, count, 2)

		/* include from-lport acl */
		filterFunc = aclFilter(ovnnb.ACLDirectionFromLport, nil)
		count = 0
		for _, acl := range acls {
			if filterFunc(acl) {
				count++
			}
		}
		require.Equal(t, count, 7)

		/* include all from-lport acl with acl parent key*/
		filterFunc = aclFilter(ovnnb.ACLDirectionFromLport, map[string]string{aclParentKey: ""})
		count = 0
		for _, acl := range acls {
			if filterFunc(acl) {
				count++
			}
		}
		require.Equal(t, count, 7)
	})

	t.Run("result should exclude acl when externalIDs's length is not equal", func(t *testing.T) {
		t.Parallel()

		match := "outport == @ovn.sg.test_filter_acl_pg && ip"
		acl := newACL(pgName, ovnnb.ACLDirectionToLport, "9999", match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)

		filterFunc := aclFilter("", map[string]string{
			aclParentKey: pgName,
			"key":        "value",
		})

		require.False(t, filterFunc(acl))
	})
}

func (suite *OvnClientTestSuite) testCreateAclsOps() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test-create-acl-ops"

	t.Run("create acl ops without acl", func(t *testing.T) {
		t.Parallel()
		ops, err := nbClient.CreateAclsOps(pgName, portGroupKey)
		require.Nil(t, err)
		require.Nil(t, ops)
	})
}

func (suite *OvnClientTestSuite) testSgRuleNoACL() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	sgName := "test-sg"
	pgName := GetSgPortGroupName(sgName)

	err := nbClient.CreatePortGroup(pgName, nil)
	require.NoError(t, err)

	t.Run("ipv4 ingress rule", func(t *testing.T) {
		rule := kubeovnv1.SecurityGroupRule{
			IPVersion:     "ipv4",
			Protocol:      kubeovnv1.SgProtocolTCP,
			RemoteType:    kubeovnv1.SgRemoteTypeAddress,
			RemoteAddress: "192.168.1.0/24",
			PortRangeMin:  80,
			PortRangeMax:  80,
			Priority:      200,
		}
		noACL, err := nbClient.sgRuleNoACL(sgName, ovnnb.ACLDirectionToLport, rule)
		require.NoError(t, err)
		require.True(t, noACL)
	})

	t.Run("ipv6 egress rule", func(t *testing.T) {
		rule := kubeovnv1.SecurityGroupRule{
			IPVersion:           "ipv6",
			Protocol:            kubeovnv1.SgProtocolUDP,
			RemoteType:          kubeovnv1.SgRemoteTypeSg,
			RemoteSecurityGroup: "remote-sg",
			PortRangeMin:        53,
			PortRangeMax:        53,
			Priority:            199,
		}
		noACL, err := nbClient.sgRuleNoACL(sgName, ovnnb.ACLDirectionFromLport, rule)
		require.NoError(t, err)
		require.True(t, noACL)
	})

	t.Run("icmp rule", func(t *testing.T) {
		rule := kubeovnv1.SecurityGroupRule{
			IPVersion:     "ipv4",
			Protocol:      kubeovnv1.SgProtocolICMP,
			RemoteType:    kubeovnv1.SgRemoteTypeAddress,
			RemoteAddress: "10.0.0.0/8",
			Priority:      198,
		}
		noACL, err := nbClient.sgRuleNoACL(sgName, ovnnb.ACLDirectionToLport, rule)
		require.NoError(t, err)
		require.True(t, noACL)
	})

	t.Run("existing ACL", func(t *testing.T) {
		rule := kubeovnv1.SecurityGroupRule{
			IPVersion:     "ipv4",
			Protocol:      kubeovnv1.SgProtocolTCP,
			RemoteType:    kubeovnv1.SgRemoteTypeAddress,
			RemoteAddress: "172.16.0.0/16",
			PortRangeMin:  443,
			PortRangeMax:  443,
			Priority:      197,
		}

		match := fmt.Sprintf("inport == @%s && ip4 && ip4.dst == 172.16.0.0/16 && 443 <= tcp.dst <= 443", pgName)
		securityGroupHighestPriority, _ := strconv.Atoi(util.SecurityGroupHighestPriority)
		priority := securityGroupHighestPriority - rule.Priority
		acl, err := nbClient.newACL(pgName, ovnnb.ACLDirectionFromLport, strconv.Itoa(priority), match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		require.NoError(t, err)
		err = nbClient.CreateAcls(pgName, portGroupKey, acl)
		require.NoError(t, err)

		noACL, err := nbClient.sgRuleNoACL(sgName, ovnnb.ACLDirectionFromLport, rule)
		require.NoError(t, err)
		require.False(t, noACL)
	})
}

func (suite *OvnClientTestSuite) testSGLostACL() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	t.Run("no lost ACLs", func(t *testing.T) {
		t.Parallel()
		sg := &kubeovnv1.SecurityGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-sg-no-lost-acl",
			},
			Spec: kubeovnv1.SecurityGroupSpec{
				IngressRules: []kubeovnv1.SecurityGroupRule{
					{
						IPVersion:     "ipv4",
						Protocol:      "tcp",
						RemoteType:    kubeovnv1.SgRemoteTypeAddress,
						RemoteAddress: "192.168.0.0/24",
						PortRangeMin:  80,
						PortRangeMax:  80,
						Priority:      1,
						Policy:        "allow",
					},
				},
				EgressRules: []kubeovnv1.SecurityGroupRule{
					{
						IPVersion:     "ipv6",
						Protocol:      "udp",
						RemoteType:    kubeovnv1.SgRemoteTypeAddress,
						RemoteAddress: "fd00::/64",
						PortRangeMin:  53,
						PortRangeMax:  53,
						Priority:      1,
						Policy:        "allow",
					},
				},
			},
		}

		pgName := GetSgPortGroupName(sg.Name)
		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		ingressACL, err := nbClient.newACL(pgName, ovnnb.ACLDirectionToLport, "2299", "outport == @ovn.sg.test.sg.no.lost.acl && ip4 && ip4.src == 192.168.0.0/24 && 80 <= tcp.dst <= 80", ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		require.NoError(t, err)
		err = nbClient.CreateAcls(pgName, portGroupKey, ingressACL)
		require.NoError(t, err)

		egressACL, err := nbClient.newACL(pgName, ovnnb.ACLDirectionFromLport, "2299", "inport == @ovn.sg.test.sg.no.lost.acl && ip6 && ip6.dst == fd00::/64 && 53 <= udp.dst <= 53", ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		require.NoError(t, err)
		err = nbClient.CreateAcls(pgName, portGroupKey, egressACL)
		require.NoError(t, err)

		lost, err := nbClient.SGLostACL(sg)
		require.NoError(t, err)
		require.False(t, lost)
	})

	t.Run("lost ingress ACL", func(t *testing.T) {
		t.Parallel()
		sg := &kubeovnv1.SecurityGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-sg-lost-ingress-acl",
			},
			Spec: kubeovnv1.SecurityGroupSpec{
				IngressRules: []kubeovnv1.SecurityGroupRule{
					{
						IPVersion:     "ipv4",
						Protocol:      "tcp",
						RemoteType:    kubeovnv1.SgRemoteTypeAddress,
						RemoteAddress: "192.168.0.0/24",
						PortRangeMin:  80,
						PortRangeMax:  80,
						Priority:      1,
						Policy:        "allow",
					},
				},
				EgressRules: []kubeovnv1.SecurityGroupRule{
					{
						IPVersion:     "ipv6",
						Protocol:      "udp",
						RemoteType:    kubeovnv1.SgRemoteTypeAddress,
						RemoteAddress: "fd00::/64",
						PortRangeMin:  53,
						PortRangeMax:  53,
						Priority:      1,
						Policy:        "allow",
					},
				},
			},
		}

		pgName := GetSgPortGroupName(sg.Name)
		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		egressACL, err := nbClient.newACL(pgName, ovnnb.ACLDirectionFromLport, "2299", "inport == @ovn.sg.test.sg.lost.ingress.acl && ip6 && ip6.dst == fd00::/64 && 53 <= udp.dst <= 53", ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		require.NoError(t, err)
		err = nbClient.CreateAcls(pgName, portGroupKey, egressACL)
		require.NoError(t, err)

		lost, err := nbClient.SGLostACL(sg)
		require.NoError(t, err)
		require.True(t, lost)
	})

	t.Run("lost egress ACL", func(t *testing.T) {
		t.Parallel()
		sg := &kubeovnv1.SecurityGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-sg-lost-egress-acl",
			},
			Spec: kubeovnv1.SecurityGroupSpec{
				IngressRules: []kubeovnv1.SecurityGroupRule{
					{
						IPVersion:     "ipv4",
						Protocol:      "tcp",
						RemoteType:    kubeovnv1.SgRemoteTypeAddress,
						RemoteAddress: "192.168.0.0/24",
						PortRangeMin:  80,
						PortRangeMax:  80,
						Priority:      1,
						Policy:        "allow",
					},
				},
				EgressRules: []kubeovnv1.SecurityGroupRule{
					{
						IPVersion:     "ipv6",
						Protocol:      "udp",
						RemoteType:    kubeovnv1.SgRemoteTypeAddress,
						RemoteAddress: "fd00::/64",
						PortRangeMin:  53,
						PortRangeMax:  53,
						Priority:      1,
						Policy:        "allow",
					},
				},
			},
		}

		pgName := GetSgPortGroupName(sg.Name)
		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		ingressACL, err := nbClient.newACL(pgName, ovnnb.ACLDirectionToLport, "2299", "outport == @ovn.sg.test.sg.lost.egress.acl && ip4 && ip4.src == 192.168.0.0/24 && 80 <= tcp.dst <= 80", ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		require.NoError(t, err)
		err = nbClient.CreateAcls(pgName, portGroupKey, ingressACL)
		require.NoError(t, err)

		lost, err := nbClient.SGLostACL(sg)
		require.NoError(t, err)
		require.True(t, lost)
	})

	t.Run("empty security group", func(t *testing.T) {
		t.Parallel()
		sg := &kubeovnv1.SecurityGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-sg-empty",
			},
		}

		pgName := GetSgPortGroupName(sg.Name)
		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		lost, err := nbClient.SGLostACL(sg)
		require.NoError(t, err)
		require.False(t, lost)
	})
}

func (suite *OvnClientTestSuite) testNewAnpACLMatch() {
	t := suite.T()
	t.Parallel()

	testCases := []struct {
		name      string
		pgName    string
		asName    string
		protocol  string
		direction string
		rulePorts []v1alpha1.AdminNetworkPolicyPort
		expected  []string
	}{
		{
			name:      "IPv4 ingress no ports",
			pgName:    "pg1",
			asName:    "as1",
			protocol:  kubeovnv1.ProtocolIPv4,
			direction: ovnnb.ACLDirectionToLport,
			rulePorts: []v1alpha1.AdminNetworkPolicyPort{},
			expected:  []string{"outport == @pg1 && ip && ip4.src == $as1"},
		},
		{
			name:      "IPv6 egress no ports",
			pgName:    "pg2",
			asName:    "as2",
			protocol:  kubeovnv1.ProtocolIPv6,
			direction: ovnnb.ACLDirectionFromLport,
			rulePorts: []v1alpha1.AdminNetworkPolicyPort{},
			expected:  []string{"inport == @pg2 && ip && ip6.dst == $as2"},
		},
		{
			name:      "IPv4 ingress with port number",
			pgName:    "pg3",
			asName:    "as3",
			protocol:  kubeovnv1.ProtocolIPv4,
			direction: ovnnb.ACLDirectionToLport,
			rulePorts: []v1alpha1.AdminNetworkPolicyPort{
				{
					PortNumber: &v1alpha1.Port{
						Protocol: v1.ProtocolTCP,
						Port:     80,
					},
				},
			},
			expected: []string{"outport == @pg3 && ip && ip4.src == $as3 && tcp.dst == 80"},
		},
		{
			name:      "IPv6 egress with port range",
			pgName:    "pg4",
			asName:    "as4",
			protocol:  kubeovnv1.ProtocolIPv6,
			direction: ovnnb.ACLDirectionFromLport,
			rulePorts: []v1alpha1.AdminNetworkPolicyPort{
				{
					PortRange: &v1alpha1.PortRange{
						Protocol: v1.ProtocolUDP,
						Start:    1024,
						End:      2048,
					},
				},
			},
			expected: []string{"inport == @pg4 && ip && ip6.dst == $as4 && 1024 <= udp.dst <= 2048"},
		},
		{
			name:      "IPv4 ingress with multiple ports",
			pgName:    "pg5",
			asName:    "as5",
			protocol:  kubeovnv1.ProtocolIPv4,
			direction: ovnnb.ACLDirectionToLport,
			rulePorts: []v1alpha1.AdminNetworkPolicyPort{
				{
					PortNumber: &v1alpha1.Port{
						Protocol: v1.ProtocolTCP,
						Port:     80,
					},
				},
				{
					PortRange: &v1alpha1.PortRange{
						Protocol: v1.ProtocolUDP,
						Start:    1024,
						End:      2048,
					},
				},
			},
			expected: []string{
				"outport == @pg5 && ip && ip4.src == $as5 && tcp.dst == 80",
				"outport == @pg5 && ip && ip4.src == $as5 && 1024 <= udp.dst <= 2048",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := newAnpACLMatch(tc.pgName, tc.asName, tc.protocol, tc.direction, tc.rulePorts)
			require.Equal(t, tc.expected, result)
		})
	}
}

func (suite *OvnClientTestSuite) testCreateBareACL() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient

	t.Run("create bare ACL successfully", func(t *testing.T) {
		err := nbClient.CreateBareACL("test-parent", "from-lport", "1000", "ip4.src == 10.0.0.1", "allow")
		require.NoError(t, err)
	})

	t.Run("create bare ACL with empty match", func(t *testing.T) {
		err := nbClient.CreateBareACL("test-parent", "from-lport", "1000", "", "allow")
		require.Error(t, err)
		require.Contains(t, err.Error(), "new acl direction from-lport priority 1000 match")
	})

	t.Run("fail nb client should log err", func(t *testing.T) {
		err := failedNbClient.CreateBareACL("test-parent", "from-lport", "1000", "ip4.src == 10.0.0.1", "allow")
		require.Error(t, err)
	})
}

func (suite *OvnClientTestSuite) testUpdateAnpRuleACLOps() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	expect := func(row ovsdb.Row, action, direction, match, priority string) {
		intPriority, err := strconv.Atoi(priority)
		require.NoError(t, err)
		require.Equal(t, action, row["action"])
		require.Equal(t, direction, row["direction"])
		require.Equal(t, match, row["match"])
		require.Equal(t, intPriority, row["priority"])
	}

	t.Run("ingress ACL for ANP", func(t *testing.T) {
		pgName := "test-pg-ingress"
		asName := "test-as-ingress"
		protocol := "tcp"
		aclName := "test-acl"
		priority := 1000
		aclAction := ovnnb.ACLActionAllow
		logACLActions := []ovnnb.ACLAction{ovnnb.ACLActionAllow}
		rulePorts := []v1alpha1.AdminNetworkPolicyPort{}
		isIngress := true
		isBanp := false

		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)
		ops, err := nbClient.UpdateAnpRuleACLOps(pgName, asName, protocol, aclName, priority, aclAction, logACLActions, rulePorts, isIngress, isBanp)
		require.NoError(t, err)
		require.NotEmpty(t, ops)
		expect(ops[0].Row, ovnnb.ACLActionAllow, ovnnb.ACLDirectionToLport, fmt.Sprintf("outport == @%s && ip && ip4.src == $%s", pgName, asName), "1000")
	})

	t.Run("egress ACL for BANP", func(t *testing.T) {
		pgName := "test-pg-egress"
		asName := "test-as-egress"
		protocol := "udp"
		aclName := "test-acl"
		priority := 2000
		aclAction := ovnnb.ACLActionDrop
		logACLActions := []ovnnb.ACLAction{ovnnb.ACLActionDrop}
		rulePorts := []v1alpha1.AdminNetworkPolicyPort{}
		isIngress := false
		isBanp := true

		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)
		ops, err := nbClient.UpdateAnpRuleACLOps(pgName, asName, protocol, aclName, priority, aclAction, logACLActions, rulePorts, isIngress, isBanp)
		require.NoError(t, err)
		require.NotEmpty(t, ops)
		expect(ops[0].Row, ovnnb.ACLActionDrop, ovnnb.ACLDirectionFromLport, fmt.Sprintf("inport == @%s && ip && ip4.dst == $%s", pgName, asName), "2000")
	})
}

func (suite *OvnClientTestSuite) testUpdateACL() {
	t := suite.T()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient

	// nbClient := suite.ovnNBClient
	pgName := "test_update_acl_pg"
	priority := "2000"
	match := "ip4.dst == 100.64.0.0/16"

	err := nbClient.CreatePortGroup(pgName, nil)
	require.NoError(t, err)

	acl, err := nbClient.newACL(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
	require.NoError(t, err)

	err = nbClient.CreateAcls(pgName, portGroupKey, acl)
	require.NoError(t, err)

	acl, err = nbClient.GetACL(pgName, ovnnb.ACLDirectionToLport, priority, match, false)
	require.NoError(t, err)

	t.Run("update ACL with nil input", func(t *testing.T) {
		err := nbClient.UpdateACL(nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "address_set is nil")
	})

	t.Run("normal update ACL", func(t *testing.T) {
		acl.Priority = 1005
		err := nbClient.UpdateACL(acl)
		require.NoError(t, err)

		newACL, err := nbClient.GetACL(pgName, ovnnb.ACLDirectionToLport, "1005", match, false)
		require.NoError(t, err)
		fmt.Println(newACL.Priority)
	})

	t.Run("fail nb client should log err", func(t *testing.T) {
		failACL, err := failedNbClient.newACL(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		require.NoError(t, err)

		err = failedNbClient.CreateAcls(pgName, portGroupKey, failACL)
		require.Error(t, err)

		failACL.Priority = 1009
		err = failedNbClient.UpdateACL(failACL)
		// TODO:// should err but not for now
		require.NoError(t, err)
	})
}
