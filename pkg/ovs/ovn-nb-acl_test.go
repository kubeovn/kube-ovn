package ovs

import (
	"fmt"

	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (suite *OvnClientTestSuite) testCreateAclOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test-create-acl-op-pg"
	priority := "1001"
	match := "outport==@ovn.sg.test_create_acl_pg && ip"
	aclName := fmt.Sprintf("%s.%s.%s", pgName, ovnnb.ACLDirectionToLport, priority)

	acl := ovnClient.newAcl(pgName, match, ovnnb.ACLDirectionToLport, ovnnb.ACLActionAllowRelated, priority)

	op, err := ovnClient.CreateAclOp(acl)
	require.NoError(t, err)
	require.Len(t, op, 1)

	require.Equal(t,
		ovsdb.Operation{
			Op:    "insert",
			Table: "ACL",
			Row: ovsdb.Row{
				"action":    ovnnb.ACLActionAllowRelated,
				"direction": ovnnb.ACLDirectionToLport,
				"external_ids": ovsdb.OvsMap{GoMap: map[interface{}]interface{}{
					portGroupKey: pgName,
				}},
				"match": match,
				"log":   false,
				"name": ovsdb.OvsSet{
					GoSet: []interface{}{
						aclName,
					}},
				"priority": 1001,
			}}, op[0])
}

func (suite *OvnClientTestSuite) testnewAcl() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test-new-acl-pg"
	priority := "1001"
	match := "outport==@ovn.sg.test_create_acl_pg && ip"
	aclName := fmt.Sprintf("%s.%s.%s", pgName, ovnnb.ACLDirectionToLport, priority)

	options := func(acl *ovnnb.ACL) {
		acl.Log = true
		acl.Severity = &ovnnb.ACLSeverityWarning
	}

	expect := &ovnnb.ACL{
		Name:      &aclName,
		Action:    ovnnb.ACLActionAllowRelated,
		Direction: ovnnb.ACLDirectionToLport,
		Match:     match,
		Priority:  1001,
		ExternalIDs: map[string]string{
			portGroupKey: pgName,
		},
		Log:      true,
		Severity: &ovnnb.ACLSeverityWarning,
	}

	acl := ovnClient.newAcl(pgName, match, ovnnb.ACLDirectionToLport, ovnnb.ACLActionAllowRelated, priority, options)
	require.Equal(t, expect, acl)
}
