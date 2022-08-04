package ovs

import (
	"fmt"
	"testing"

	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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

func (suite *OvnClientTestSuite) testCreateIngressACL() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test_create_ingress_acl_pg"
	asIngressName := "test.default.ingress.allow.ipv4"
	asExceptName := "test.default.ingress.except.ipv4"

	err := ovnClient.CreatePortGroup(pgName, nil)
	require.NoError(t, err)

	npp := mockNetworkPolicyPort()

	err = ovnClient.CreateIngressACL(pgName, asIngressName, asExceptName, kubeovnv1.ProtocolIPv4, npp)
	require.NoError(t, err)

	pg, err := ovnClient.GetPortGroup(pgName, false)
	require.NoError(t, err)
	require.Len(t, pg.ACLs, 3)

	match := fmt.Sprintf("outport==@%s && ip", pgName)
	defaultDropAcl, err := ovnClient.GetAcl(ovnnb.ACLDirectionToLport, util.IngressDefaultDrop, match, false)
	require.NoError(t, err)
	require.Equal(t, pgName, *defaultDropAcl.Name)
	require.Contains(t, pg.ACLs, defaultDropAcl.UUID)

	matches := newIngressAllowACLMatch(pgName, asIngressName, asExceptName, kubeovnv1.ProtocolIPv4, npp)
	for _, m := range matches {
		allowAcl, err := ovnClient.GetAcl(ovnnb.ACLDirectionToLport, util.IngressDefaultDrop, m, false)
		require.NoError(t, err)
		require.Contains(t, pg.ACLs, allowAcl.UUID)
	}
}

func (suite *OvnClientTestSuite) testGetAcl() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	priority := "1001"
	match := "outport==@ovn.sg.test_create_acl_pg && ip"

	err := ovnClient.CreateBareACL(ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated)
	require.NoError(t, err)

	t.Run("direction, priority and match are same", func(t *testing.T) {
		t.Parallel()
		acl, err := ovnClient.GetAcl(ovnnb.ACLDirectionToLport, priority, match, false)
		require.NoError(t, err)
		require.Equal(t, ovnnb.ACLDirectionToLport, acl.Direction)
		require.Equal(t, 1001, acl.Priority)
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

func (suite *OvnClientTestSuite) testCreateAclOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test-create-acl-op-pg"
	priority := "1001"
	match := "outport==@ovn.sg.test_create_acl_pg && ip"

	op, aclUUID, err := ovnClient.CreateAclOp(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated)
	require.NoError(t, err)
	require.Len(t, op, 1)

	require.Equal(t,
		ovsdb.Operation{
			Op:    "insert",
			Table: "ACL",
			Row: ovsdb.Row{
				"_uuid":     ovsdb.UUID{GoUUID: aclUUID},
				"direction": ovnnb.ACLDirectionToLport,
				"priority":  1001,
				"match":     match,
				"action":    ovnnb.ACLActionAllowRelated,
				"external_ids": ovsdb.OvsMap{GoMap: map[interface{}]interface{}{
					aclParentKey: pgName,
				}},
				"log": false,
			},
			UUIDName: aclUUID,
		}, op[0])
}

func (suite *OvnClientTestSuite) testnewAcl() {
	t := suite.T()
	t.Parallel()

	pgName := "test-new-acl-pg"
	priority := "1001"
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
		Priority:  1001,
		ExternalIDs: map[string]string{
			aclParentKey: pgName,
		},
		Log:      true,
		Severity: &ovnnb.ACLSeverityWarning,
	}

	acl := newAcl(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated, options)
	expect.UUID = acl.UUID
	require.Equal(t, expect, acl)
}

func (suite *OvnClientTestSuite) testnewIngressAllowACL() {
	t := suite.T()
	t.Parallel()

	pgName := "test-new-allow-acl-pg"
	asIngressName := "test.default.ingress.allow.ipv4"
	asExceptName := "test.default.ingress.except.ipv4"

	t.Run("has network policy port", func(t *testing.T) {
		t.Parallel()

		npp := mockNetworkPolicyPort()
		matches := newIngressAllowACLMatch(pgName, asIngressName, asExceptName, kubeovnv1.ProtocolIPv4, npp)
		require.Equal(t, []string{
			"ip4.src == $test.default.ingress.allow.ipv4 && ip4.src != $test.default.ingress.except.ipv4 && tcp.dst == 12345 && outport == @test-new-allow-acl-pg && ip",
			"ip4.src == $test.default.ingress.allow.ipv4 && ip4.src != $test.default.ingress.except.ipv4 && 12346 <= tcp.dst <= 20000 && outport == @test-new-allow-acl-pg && ip",
		}, matches)
	})

	t.Run("has network policy port but port is not set", func(t *testing.T) {
		t.Parallel()

		npp := mockNetworkPolicyPort()
		npp[1].Port = nil
		matches := newIngressAllowACLMatch(pgName, asIngressName, asExceptName, kubeovnv1.ProtocolIPv4, npp)
		require.Equal(t, []string{
			"ip4.src == $test.default.ingress.allow.ipv4 && ip4.src != $test.default.ingress.except.ipv4 && tcp.dst == 12345 && outport == @test-new-allow-acl-pg && ip",
			"ip4.src == $test.default.ingress.allow.ipv4 && ip4.src != $test.default.ingress.except.ipv4 && tcp && outport == @test-new-allow-acl-pg && ip",
		}, matches)
	})

	t.Run("has network policy port but endPort is not set", func(t *testing.T) {
		t.Parallel()

		npp := mockNetworkPolicyPort()
		npp[1].EndPort = nil
		matches := newIngressAllowACLMatch(pgName, asIngressName, asExceptName, kubeovnv1.ProtocolIPv4, npp)
		require.Equal(t, []string{
			"ip4.src == $test.default.ingress.allow.ipv4 && ip4.src != $test.default.ingress.except.ipv4 && tcp.dst == 12345 && outport == @test-new-allow-acl-pg && ip",
			"ip4.src == $test.default.ingress.allow.ipv4 && ip4.src != $test.default.ingress.except.ipv4 && tcp.dst == 12346 && outport == @test-new-allow-acl-pg && ip",
		}, matches)
	})

	t.Run("has network policy port is nil", func(t *testing.T) {
		t.Parallel()

		matches := newIngressAllowACLMatch(pgName, asIngressName, asExceptName, kubeovnv1.ProtocolIPv4, nil)
		require.Equal(t, []string{
			"ip4.src == $test.default.ingress.allow.ipv4 && ip4.src != $test.default.ingress.except.ipv4 && outport == @test-new-allow-acl-pg && ip",
		}, matches)
	})
}
