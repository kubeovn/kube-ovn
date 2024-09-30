package ovs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// createLogicalRouter delete logical router in ovn
func createLogicalRouter(c *OVNNbClient, lr *ovnnb.LogicalRouter) error {
	op, err := c.ovsDbClient.Create(lr)
	if err != nil {
		klog.Error(err)
		return err
	}

	return c.Transact("lr-add", op)
}

func (suite *OvnClientTestSuite) testCreateLogicalRouter() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	name := "test-create-lr"

	err := nbClient.CreateLogicalRouter(name)
	require.NoError(t, err)

	lr, err := nbClient.GetLogicalRouter(name, false)
	require.NoError(t, err)
	require.Equal(t, name, lr.Name)
	require.NotEmpty(t, lr.UUID)
	require.Equal(t, util.CniTypeName, lr.ExternalIDs["vendor"])
}

func (suite *OvnClientTestSuite) testUpdateLogicalRouter() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-update-lr"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	lr, err := nbClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)

	t.Run("update external-ids", func(t *testing.T) {
		lr.ExternalIDs = map[string]string{"foo": "bar"}
		err = nbClient.UpdateLogicalRouter(lr)
		require.NoError(t, err)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"foo": "bar"}, lr.ExternalIDs)
	})

	t.Run("clear external-ids", func(t *testing.T) {
		lr.ExternalIDs = nil

		err = nbClient.UpdateLogicalRouter(lr, &lr.ExternalIDs)
		require.NoError(t, err)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.ExternalIDs)
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouter() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	name := "test-delete-lr"

	t.Run("no err when delete existent logical router", func(t *testing.T) {
		t.Parallel()
		err := nbClient.CreateLogicalRouter(name)
		require.NoError(t, err)

		err = nbClient.DeleteLogicalRouter(name)
		require.NoError(t, err)

		_, err = nbClient.GetLogicalRouter(name, false)
		require.ErrorContains(t, err, "not found logical router")
	})

	t.Run("no err when delete non-existent logical router", func(t *testing.T) {
		t.Parallel()
		err := nbClient.DeleteLogicalRouter("test-delete-lr-non-existent")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testGetLogicalRouter() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	name := "test-get-lr"

	err := nbClient.CreateLogicalRouter(name)
	require.NoError(t, err)

	t.Run("should return no err when found logical router", func(t *testing.T) {
		t.Parallel()
		lr, err := nbClient.GetLogicalRouter(name, false)
		require.NoError(t, err)
		require.Equal(t, name, lr.Name)
		require.NotEmpty(t, lr.UUID)
	})

	t.Run("should return err when not found logical router", func(t *testing.T) {
		t.Parallel()
		_, err := nbClient.GetLogicalRouter("test-get-lr-non-existent", false)
		require.ErrorContains(t, err, "not found logical router")
	})

	t.Run("no err when not found logical router and ignoreNotFound is true", func(t *testing.T) {
		t.Parallel()
		_, err := nbClient.GetLogicalRouter("test-get-lr-non-existent", true)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testListLogicalRouter() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	namePrefix := "test-list-lr"

	i := 0
	// create three logical router
	for ; i < 3; i++ {
		name := fmt.Sprintf("%s-%d", namePrefix, i)
		err := nbClient.CreateLogicalRouter(name)
		require.NoError(t, err)
	}

	// create two logical router which vendor is others
	for ; i < 5; i++ {
		name := fmt.Sprintf("%s-%d", namePrefix, i)
		lr := &ovnnb.LogicalRouter{
			Name:        name,
			ExternalIDs: map[string]string{"vendor": "test-vendor"},
		}

		err := createLogicalRouter(nbClient, lr)
		require.NoError(t, err)
	}

	// create two logical router without vendor
	for ; i < 7; i++ {
		name := fmt.Sprintf("%s-%d", namePrefix, i)
		lr := &ovnnb.LogicalRouter{
			Name: name,
		}

		err := createLogicalRouter(nbClient, lr)
		require.NoError(t, err)
	}

	t.Run("return all logical router which vendor is kube-ovn", func(t *testing.T) {
		t.Parallel()
		lrs, err := nbClient.ListLogicalRouter(true, nil)
		require.NoError(t, err)

		count := 0
		for _, lr := range lrs {
			if strings.Contains(lr.Name, namePrefix) {
				count++
			}
		}
		require.Equal(t, count, 3)
	})

	t.Run("has custom filter", func(t *testing.T) {
		t.Parallel()
		lrs, err := nbClient.ListLogicalRouter(false, func(lr *ovnnb.LogicalRouter) bool {
			return len(lr.ExternalIDs) == 0 || lr.ExternalIDs["vendor"] != util.CniTypeName
		})

		require.NoError(t, err)

		count := 0
		for _, lr := range lrs {
			if strings.Contains(lr.Name, namePrefix) {
				count++
			}
		}
		require.Equal(t, count, 4)
	})
}

func (suite *OvnClientTestSuite) testLogicalRouterUpdateLoadBalancers() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-add-lb-to-lr"
	prefix := "test-add-lr-lb"
	lbNames := make([]string, 0, 3)

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	for i := 1; i <= 3; i++ {
		lbName := fmt.Sprintf("%s-%d", prefix, i)
		lbNames = append(lbNames, lbName)
		err := nbClient.CreateLoadBalancer(lbName, "tcp", "")
		require.NoError(t, err)
	}

	t.Run("add lbs to logical router", func(t *testing.T) {
		err = nbClient.LogicalRouterUpdateLoadBalancers(lrName, ovsdb.MutateOperationInsert, lbNames...)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		for _, lbName := range lbNames {
			lb, err := nbClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)
			require.Contains(t, ls.LoadBalancer, lb.UUID)
		}
	})

	t.Run("should no err when add non-existent lbs to logical router", func(t *testing.T) {
		// add a non-existent lb
		err = nbClient.LogicalSwitchUpdateLoadBalancers(lrName, ovsdb.MutateOperationInsert, "test-add-lb-non-existent")
		require.NoError(t, err)
	})

	t.Run("del lbs from logical router", func(t *testing.T) {
		// delete the first two lbs from logical switch
		err = nbClient.LogicalRouterUpdateLoadBalancers(lrName, ovsdb.MutateOperationDelete, lbNames[0:2]...)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		for i, lbName := range lbNames {
			lb, err := nbClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)

			// logical switch contains the last lb
			if i == 2 {
				require.Contains(t, ls.LoadBalancer, lb.UUID)
				continue
			}
			require.NotContains(t, ls.LoadBalancer, lb.UUID)
		}
	})

	t.Run("del non-existent lbs from logical router", func(t *testing.T) {
		err = nbClient.LogicalRouterUpdateLoadBalancers(lrName, ovsdb.MutateOperationDelete, []string{"test-del-lb-non-existent", "test-del-lb-non-existent-1"}...)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testLogicalRouterUpdatePortOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-update-port-op-lr"
	uuid := ovsclient.NamedUUID()

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("add new port to logical router", func(t *testing.T) {
		t.Parallel()
		ops, err := nbClient.LogicalRouterUpdatePortOp(lrName, uuid, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: uuid,
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("del port from logical router", func(t *testing.T) {
		t.Parallel()
		ops, err := nbClient.LogicalRouterUpdatePortOp(lrName, uuid, ovsdb.MutateOperationDelete)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationDelete,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: uuid,
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("should return err when logical router does not exist", func(t *testing.T) {
		t.Parallel()
		_, err := nbClient.LogicalRouterUpdatePortOp("test-update-port-op-lr-non-existent", uuid, ovsdb.MutateOperationInsert)
		require.ErrorContains(t, err, "not found logical router")
	})
}

func (suite *OvnClientTestSuite) testLogicalRouterUpdatePolicyOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-update-policy-op-lr"
	uuid := ovsclient.NamedUUID()

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("add new policy to logical router", func(t *testing.T) {
		t.Parallel()
		ops, err := nbClient.LogicalRouterUpdatePolicyOp(lrName, []string{uuid}, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "policies",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: uuid,
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("del policy from logical router", func(t *testing.T) {
		t.Parallel()
		ops, err := nbClient.LogicalRouterUpdatePolicyOp(lrName, []string{uuid}, ovsdb.MutateOperationDelete)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "policies",
				Mutator: ovsdb.MutateOperationDelete,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: uuid,
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("should return err when logical router does not exist", func(t *testing.T) {
		t.Parallel()
		_, err := nbClient.LogicalRouterUpdatePolicyOp("test-update-policy-op-lr-non-existent", []string{uuid}, ovsdb.MutateOperationInsert)
		require.ErrorContains(t, err, "not found logical router")
	})
}

func (suite *OvnClientTestSuite) testLogicalRouterUpdateNatOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-update-nat-op-lr"
	uuid := ovsclient.NamedUUID()

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("add new nat to logical router", func(t *testing.T) {
		t.Parallel()
		ops, err := nbClient.LogicalRouterUpdateNatOp(lrName, []string{uuid}, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "nat",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: uuid,
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("del nat from logical router", func(t *testing.T) {
		t.Parallel()
		ops, err := nbClient.LogicalRouterUpdateNatOp(lrName, []string{uuid}, ovsdb.MutateOperationDelete)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "nat",
				Mutator: ovsdb.MutateOperationDelete,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: uuid,
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("should return err when logical router does not exist", func(t *testing.T) {
		t.Parallel()
		_, err := nbClient.LogicalRouterUpdateNatOp("test-update-nat-op-lr-non-existent", []string{uuid}, ovsdb.MutateOperationInsert)
		require.ErrorContains(t, err, "not found logical router")
	})
}

func (suite *OvnClientTestSuite) testLogicalRouterUpdateStaticRouteOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-update-route-op-lr"
	uuid := ovsclient.NamedUUID()

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("add new static route to logical router", func(t *testing.T) {
		t.Parallel()
		ops, err := nbClient.LogicalRouterUpdateStaticRouteOp(lrName, []string{uuid}, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "static_routes",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: uuid,
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("del static route from logical router", func(t *testing.T) {
		t.Parallel()
		ops, err := nbClient.LogicalRouterUpdateStaticRouteOp(lrName, []string{uuid}, ovsdb.MutateOperationDelete)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "static_routes",
				Mutator: ovsdb.MutateOperationDelete,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: uuid,
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("should return err when logical router does not exist", func(t *testing.T) {
		t.Parallel()
		_, err := nbClient.LogicalRouterUpdateStaticRouteOp("test-update-route-op-lr-non-existent", []string{uuid}, ovsdb.MutateOperationInsert)
		require.ErrorContains(t, err, "not found logical router")
	})
}

func (suite *OvnClientTestSuite) testLogicalRouterOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-op-lr"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	lrpUUID := ovsclient.NamedUUID()
	lrpMutation := func(lr *ovnnb.LogicalRouter) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &lr.Ports,
			Value:   []string{lrpUUID},
			Mutator: ovsdb.MutateOperationInsert,
		}

		return mutation
	}

	policyUUID := ovsclient.NamedUUID()
	policyMutation := func(lr *ovnnb.LogicalRouter) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &lr.Policies,
			Value:   []string{policyUUID},
			Mutator: ovsdb.MutateOperationInsert,
		}

		return mutation
	}

	ops, err := nbClient.LogicalRouterOp(lrName, lrpMutation, policyMutation)
	require.NoError(t, err)

	require.Len(t, ops[0].Mutations, 2)
	require.Equal(t, []ovsdb.Mutation{
		{
			Column:  "ports",
			Mutator: ovsdb.MutateOperationInsert,
			Value: ovsdb.OvsSet{
				GoSet: []interface{}{
					ovsdb.UUID{
						GoUUID: lrpUUID,
					},
				},
			},
		},
		{
			Column:  "policies",
			Mutator: ovsdb.MutateOperationInsert,
			Value: ovsdb.OvsSet{
				GoSet: []interface{}{
					ovsdb.UUID{
						GoUUID: policyUUID,
					},
				},
			},
		},
	}, ops[0].Mutations)
}
