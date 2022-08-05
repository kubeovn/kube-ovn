package ovs

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func mockNetworkPolicyPort() []netv1.NetworkPolicyPort {
	protocolTcp := v1.ProtocolTCP
	var endPort int32 = 20000
	return []netv1.NetworkPolicyPort{
		{
			Port: &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: 12345,
			},
			Protocol: &protocolTcp,
		},
		{
			Port: &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: 12346,
			},
			EndPort:  &endPort,
			Protocol: &protocolTcp,
		},
	}
}

func newAcl(parentName, direction, priority, match, action string, options ...func(acl *ovnnb.ACL)) *ovnnb.ACL {
	intPriority, _ := strconv.Atoi(priority)

	acl := &ovnnb.ACL{
		UUID:      ovsclient.UUID(),
		Action:    action,
		Direction: direction,
		Match:     match,
		Priority:  intPriority,
		ExternalIDs: map[string]string{
			aclParentKey: parentName,
		},
	}

	for _, option := range options {
		option(acl)
	}

	return acl
}

func (suite *OvnClientTestSuite) testCreateIngressAcl() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test_create_ingress_acl_pg"
	asIngressName := "test.default.ingress.allow.ipv4"
	asExceptName := "test.default.ingress.except.ipv4"

	err := ovnClient.CreatePortGroup(pgName, nil)
	require.NoError(t, err)

	npp := mockNetworkPolicyPort()

	err = ovnClient.CreateIngressAcl(pgName, asIngressName, asExceptName, kubeovnv1.ProtocolIPv4, npp)
	require.NoError(t, err)

	pg, err := ovnClient.GetPortGroup(pgName, false)
	require.NoError(t, err)
	require.Len(t, pg.ACLs, 3)

	match := fmt.Sprintf("outport == @%s && ip", pgName)
	defaultDropAcl, err := ovnClient.GetAcl(ovnnb.ACLDirectionToLport, util.IngressDefaultDrop, match, false)
	require.NoError(t, err)

	expect := newAcl(pgName, ovnnb.ACLDirectionToLport, util.IngressDefaultDrop, match, ovnnb.ACLActionDrop, func(acl *ovnnb.ACL) {
		acl.Name = &pgName
		acl.Log = true
		acl.Severity = &ovnnb.ACLSeverityWarning
		acl.UUID = defaultDropAcl.UUID
	})

	require.Equal(t, expect, defaultDropAcl)
	require.Contains(t, pg.ACLs, defaultDropAcl.UUID)

	matches := newAllowAclMatch(pgName, asIngressName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionToLport, npp)
	for _, m := range matches {
		allowAcl, err := ovnClient.GetAcl(ovnnb.ACLDirectionToLport, util.IngressAllowPriority, m, false)
		require.NoError(t, err)

		expect := newAcl(pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, m, ovnnb.ACLActionAllowRelated)
		expect.UUID = allowAcl.UUID
		require.Equal(t, expect, allowAcl)

		require.Contains(t, pg.ACLs, allowAcl.UUID)
	}
}

func (suite *OvnClientTestSuite) testCreateEgressAcl() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test_create_egress_acl_pg"
	asEgressName := "test.default.egress.allow.ipv4"
	asExceptName := "test.default.egress.except.ipv4"

	err := ovnClient.CreatePortGroup(pgName, nil)
	require.NoError(t, err)

	npp := mockNetworkPolicyPort()

	err = ovnClient.CreateEgressAcl(pgName, asEgressName, asExceptName, kubeovnv1.ProtocolIPv4, npp)
	require.NoError(t, err)

	pg, err := ovnClient.GetPortGroup(pgName, false)
	require.NoError(t, err)
	require.Len(t, pg.ACLs, 3)

	match := fmt.Sprintf("inport == @%s && ip", pgName)
	defaultDropAcl, err := ovnClient.GetAcl(ovnnb.ACLDirectionFromLport, util.EgressDefaultDrop, match, false)
	require.NoError(t, err)

	expect := newAcl(pgName, ovnnb.ACLDirectionFromLport, util.EgressDefaultDrop, match, ovnnb.ACLActionDrop, func(acl *ovnnb.ACL) {
		acl.Name = &pgName
		acl.Log = true
		acl.Severity = &ovnnb.ACLSeverityWarning
		acl.UUID = defaultDropAcl.UUID
	})

	require.Equal(t, expect, defaultDropAcl)
	require.Contains(t, pg.ACLs, defaultDropAcl.UUID)

	matches := newAllowAclMatch(pgName, asEgressName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionFromLport, npp)
	for _, m := range matches {
		allowAcl, err := ovnClient.GetAcl(ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, m, false)
		require.NoError(t, err)

		expect := newAcl(pgName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, m, ovnnb.ACLActionAllowRelated)
		expect.UUID = allowAcl.UUID
		require.Equal(t, expect, allowAcl)

		require.Contains(t, pg.ACLs, allowAcl.UUID)
	}
}

func (suite *OvnClientTestSuite) testGetAcl() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test_get_acl_pg"
	priority := "2000"
	match := "outport==@ovn.sg.test_create_acl_pg && ip"

	err := ovnClient.CreateBareAcl(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated)
	require.NoError(t, err)

	t.Run("direction, priority and match are same", func(t *testing.T) {
		t.Parallel()
		acl, err := ovnClient.GetAcl(ovnnb.ACLDirectionToLport, priority, match, false)
		require.NoError(t, err)
		require.Equal(t, ovnnb.ACLDirectionToLport, acl.Direction)
		require.Equal(t, 2000, acl.Priority)
		require.Equal(t, match, acl.Match)
		require.Equal(t, ovnnb.ACLActionAllowRelated, acl.Action)
	})

	t.Run("direction, priority and match are not all same", func(t *testing.T) {
		t.Parallel()

		_, err := ovnClient.GetAcl(ovnnb.ACLDirectionFromLport, priority, match, false)
		require.ErrorContains(t, err, "not found acl")

		_, err = ovnClient.GetAcl(ovnnb.ACLDirectionToLport, "1010", match, false)
		require.ErrorContains(t, err, "not found acl")

		_, err = ovnClient.GetAcl(ovnnb.ACLDirectionToLport, priority, match+" && tcp", false)
		require.ErrorContains(t, err, "not found acl")
	})

	t.Run("should no err when direction, priority and match are not all same but ignoreNotFound=true", func(t *testing.T) {
		t.Parallel()

		_, err := ovnClient.GetAcl(ovnnb.ACLDirectionFromLport, priority, match, true)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testListAcls() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test-list-acl-pg"
	basePort := 50000

	t.Run("list acl by direction", func(t *testing.T) {
		matchPrefix := "outport == @ovn.sg.test_list_acl_pg && ip"
		// create two to-lport acl
		for i := 0; i < 2; i++ {
			match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
			err := ovnClient.CreateBareAcl(pgName, ovnnb.ACLDirectionToLport, "9999", match, ovnnb.ACLActionAllowRelated)
			require.NoError(t, err)
		}

		// create two from-lport acl
		for i := 0; i < 3; i++ {
			match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
			err := ovnClient.CreateBareAcl(pgName, ovnnb.ACLDirectionFromLport, "9999", match, ovnnb.ACLActionAllowRelated)
			require.NoError(t, err)
		}

		/* list all direction acl */
		out, err := ovnClient.ListAcls("", nil)
		require.NoError(t, err)
		count := 0
		for _, v := range out {
			if strings.Contains(v.Match, matchPrefix) {
				count++
			}
		}
		require.Equal(t, count, 5)

		/* list to-lport acl */
		out, err = ovnClient.ListAcls(ovnnb.ACLDirectionToLport, nil)
		require.NoError(t, err)
		count = 0
		for _, v := range out {
			if strings.Contains(v.Match, matchPrefix) && v.Direction == ovnnb.ACLDirectionToLport {
				count++
			}
		}
		require.Equal(t, count, 2)

		/* list from-lport acl */
		out, err = ovnClient.ListAcls(ovnnb.ACLDirectionFromLport, nil)
		require.NoError(t, err)
		count = 0
		for _, v := range out {
			if strings.Contains(v.Match, matchPrefix) && v.Direction == ovnnb.ACLDirectionFromLport {
				count++
			}
		}
		require.Equal(t, count, 3)
	})

	t.Run("result should exclude acl when externalIDs's length is not equal", func(t *testing.T) {
		match := "outport == @ovn.sg.test_list_acl_pg && ip"
		err := ovnClient.CreateBareAcl(pgName, ovnnb.ACLDirectionToLport, "9999", match, ovnnb.ACLActionAllowRelated)
		require.NoError(t, err)

		out, err := ovnClient.ListAcls("", map[string]string{
			aclParentKey: pgName,
			"key":        "value",
		})
		require.NoError(t, err)
		require.Empty(t, out)
	})

	t.Run("result should include acl when key exists in acl column: external_ids", func(t *testing.T) {
		matchPrefix := "outport == @ovn.sg.test_list_acl_pg && ip"
		pgName := "test-list-acl-with-kv-pg"
		// create two to-lport acl
		for i := 0; i < 3; i++ {
			match := fmt.Sprintf("%s && udp.dst == %d", matchPrefix, basePort+i)
			err := ovnClient.CreateBareAcl(pgName, ovnnb.ACLDirectionToLport, "9999", match, ovnnb.ACLActionAllowRelated)
			require.NoError(t, err)
		}

		// create two from-lport acl
		for i := 0; i < 2; i++ {
			match := fmt.Sprintf("%s && udp.dst == %d", matchPrefix, basePort+i)
			err := ovnClient.CreateBareAcl(pgName, ovnnb.ACLDirectionFromLport, "9999", match, ovnnb.ACLActionAllowRelated)
			require.NoError(t, err)
		}

		out, err := ovnClient.ListAcls("", map[string]string{aclParentKey: pgName})
		require.NoError(t, err)
		require.Len(t, out, 5)

		/* list to-lport acl */
		out, err = ovnClient.ListAcls(ovnnb.ACLDirectionToLport, map[string]string{aclParentKey: pgName})
		require.NoError(t, err)
		count := 0
		for _, v := range out {
			if strings.Contains(v.Match, matchPrefix) && v.Direction == ovnnb.ACLDirectionToLport {
				count++
			}
		}
		require.Equal(t, count, 3)
	})

	t.Run("result should include acl which externalIDs[key] is ''", func(t *testing.T) {
		matchPrefix := "outport == @ovn.sg.test_list_acl_pg && ip"
		pgName := "test-list-acl-with-key-pg"
		// create two to-lport acl
		for i := 0; i < 3; i++ {
			match := fmt.Sprintf("%s && udp.dst == %d", matchPrefix, basePort+i)
			err := ovnClient.CreateBareAcl(pgName, ovnnb.ACLDirectionToLport, "1897", match, ovnnb.ACLActionAllowRelated)
			require.NoError(t, err)
		}

		// create two from-lport acl
		for i := 0; i < 2; i++ {
			match := fmt.Sprintf("%s && udp.dst == %d", matchPrefix, basePort+i)
			err := ovnClient.CreateBareAcl(pgName, ovnnb.ACLDirectionFromLport, "1897", match, ovnnb.ACLActionAllowRelated)
			require.NoError(t, err)
		}

		out, err := ovnClient.ListAcls("", map[string]string{aclParentKey: ""})
		require.NoError(t, err)

		count := 0
		for _, v := range out {
			if strings.Contains(v.ExternalIDs[aclParentKey], pgName) {
				count++
			}
		}
		require.Equal(t, count, 5)
	})
}

func (suite *OvnClientTestSuite) testCreateAcls() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test-create-acls-pg"
	priority := "5000"
	basePort := 12300
	matchPrefix := "outport == @ovn.sg.test_create_acl_pg && ip"
	acls := make([]*ovnnb.ACL, 0, 3)

	err := ovnClient.CreatePortGroup(pgName, nil)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
		acl, err := ovnClient.newAcl(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated)
		require.NoError(t, err)
		acls = append(acls, acl)
	}

	err = ovnClient.CreateAcls(pgName, acls...)
	require.NoError(t, err)

	pg, err := ovnClient.GetPortGroup(pgName, false)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
		acl, err := ovnClient.GetAcl(ovnnb.ACLDirectionToLport, priority, match, false)
		require.NoError(t, err)
		require.Equal(t, match, acl.Match)

		require.Contains(t, pg.ACLs, acl.UUID)
	}
}

func (suite *OvnClientTestSuite) testnewAcl() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test-new-acl-pg"
	priority := "1000"
	match := "outport==@ovn.sg.test_create_acl_pg && ip"
	options := func(acl *ovnnb.ACL) {
		acl.Log = true
		acl.Severity = &ovnnb.ACLSeverityWarning
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
		Severity: &ovnnb.ACLSeverityWarning,
	}

	acl, err := ovnClient.newAcl(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, options)
	require.NoError(t, err)
	expect.UUID = acl.UUID
	require.Equal(t, expect, acl)
}

func (suite *OvnClientTestSuite) testnewAllowAclMatch() {
	t := suite.T()
	t.Parallel()

	pgName := "test-new-acl-m-pg"
	asAllowName := "test.default.xx.allow.ipv4"
	asExceptName := "test.default.xx.except.ipv4"

	t.Run("has ingress network policy port", func(t *testing.T) {
		t.Parallel()

		npp := mockNetworkPolicyPort()
		// matches := newIngressAllowACLMatch(pgName, asIngressName, asExceptName, kubeovnv1.ProtocolIPv4, npp)
		matches := newAllowAclMatch(pgName, asAllowName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionToLport, npp)
		require.Equal(t, []string{
			fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s && tcp.dst == %d", pgName, asAllowName, asExceptName, npp[0].Port.IntVal),
			fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s && %d <= tcp.dst <= %d", pgName, asAllowName, asExceptName, npp[1].Port.IntVal, *npp[1].EndPort),
		}, matches)
	})

	t.Run("has egress network policy port", func(t *testing.T) {
		t.Parallel()

		npp := mockNetworkPolicyPort()

		matches := newAllowAclMatch(pgName, asAllowName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionFromLport, npp)
		require.Equal(t, []string{
			fmt.Sprintf("inport == @%s && ip && ip4.dst == $%s && ip4.dst != $%s && tcp.dst == %d", pgName, asAllowName, asExceptName, npp[0].Port.IntVal),
			fmt.Sprintf("inport == @%s && ip && ip4.dst == $%s && ip4.dst != $%s && %d <= tcp.dst <= %d", pgName, asAllowName, asExceptName, npp[1].Port.IntVal, *npp[1].EndPort),
		}, matches)
	})

	t.Run("network policy port is nil", func(t *testing.T) {
		t.Parallel()

		matches := newAllowAclMatch(pgName, asAllowName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionToLport, nil)
		require.Equal(t, []string{
			fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s", pgName, asAllowName, asExceptName),
		}, matches)
	})

	t.Run("has network policy port but port is not set", func(t *testing.T) {
		t.Parallel()

		npp := mockNetworkPolicyPort()
		npp[1].Port = nil

		matches := newAllowAclMatch(pgName, asAllowName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionToLport, npp)
		require.Equal(t, []string{
			fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s && tcp.dst == %d", pgName, asAllowName, asExceptName, npp[0].Port.IntVal),
			fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s && tcp", pgName, asAllowName, asExceptName),
		}, matches)
	})

	t.Run("has network policy port but endPort is not set", func(t *testing.T) {
		t.Parallel()

		npp := mockNetworkPolicyPort()
		npp[1].EndPort = nil

		matches := newAllowAclMatch(pgName, asAllowName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionToLport, npp)
		require.Equal(t, []string{
			fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s && tcp.dst == %d", pgName, asAllowName, asExceptName, npp[0].Port.IntVal),
			fmt.Sprintf("outport == @%s && ip && ip4.src == $%s && ip4.src != $%s && tcp.dst == %d", pgName, asAllowName, asExceptName, npp[1].Port.IntVal),
		}, matches)
	})
}
