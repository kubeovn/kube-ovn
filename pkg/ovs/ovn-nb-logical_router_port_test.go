package ovs

import (
	"fmt"
	"testing"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newLogicalRouterPort(lrName, lrpName, mac string, networks []string) *ovnnb.LogicalRouterPort {
	return &ovnnb.LogicalRouterPort{
		UUID:     ovsclient.NamedUUID(),
		Name:     lrpName,
		MAC:      mac,
		Networks: networks,
		ExternalIDs: map[string]string{
			logicalRouterKey: lrName,
		},
	}
}

func createLogicalRouterPort(c *ovnClient, lrp *ovnnb.LogicalRouterPort) error {
	op, err := c.Create(lrp)
	if err != nil {
		return fmt.Errorf("generate operations for creating logical router port %s: %v", lrp.Name, err)
	}

	return c.Transact("lrp-create", op)
}

func (suite *OvnClientTestSuite) testCreatePeerRouterPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	localLrName := "test-create-peer-lr-local"
	remoteLrName := "test-create-peer-lr-remote"
	localRouterPort := fmt.Sprintf("%s-%s", localLrName, remoteLrName)
	remoteRouterPort := fmt.Sprintf("%s-%s", remoteLrName, localLrName)

	err := ovnClient.CreateLogicalRouter(localLrName)
	require.NoError(t, err)

	t.Run("create new port", func(t *testing.T) {
		err = ovnClient.CreatePeerRouterPort(localLrName, remoteLrName, "192.168.230.1/24,192.168.231.1/24")
		require.NoError(t, err)

		lrp, err := ovnClient.GetLogicalRouterPort(localRouterPort, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.Equal(t, localLrName, lrp.ExternalIDs[logicalRouterKey])
		require.ElementsMatch(t, []string{"192.168.230.1/24", "192.168.231.1/24"}, lrp.Networks)
		require.Equal(t, remoteRouterPort, *lrp.Peer)

		lr, err := ovnClient.GetLogicalRouter(localLrName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{lrp.UUID}, lr.Ports)
	})

	t.Run("update port networks", func(t *testing.T) {
		err = ovnClient.CreatePeerRouterPort(localLrName, remoteLrName, "192.168.230.1/24,192.168.241.1/24")
		require.NoError(t, err)

		lrp, err := ovnClient.GetLogicalRouterPort(localRouterPort, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.ElementsMatch(t, []string{"192.168.230.1/24", "192.168.241.1/24"}, lrp.Networks)
		require.Equal(t, remoteRouterPort, *lrp.Peer)
	})
}

func (suite *OvnClientTestSuite) testUpdateLogicalRouterPortRA() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrpName := "test-update-ra-lrp"
	lrName := "test-update-ra-lr"

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = ovnClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"fd00::c0a8:1001/120"})
	require.NoError(t, err)

	t.Run("update ipv6 ra config when enableIPv6RA is true and ipv6RAConfigsStr is empty", func(t *testing.T) {
		err := ovnClient.UpdateLogicalRouterPortRA(lrpName, "", true)
		require.NoError(t, err)

		out, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"120"}, out.Ipv6Prefix)
		require.Equal(t, map[string]string{
			"address_mode":  "dhcpv6_stateful",
			"max_interval":  "30",
			"min_interval":  "5",
			"send_periodic": "true",
		}, out.Ipv6RaConfigs)
	})

	t.Run("update ipv6 ra config when enableIPv6RA is true and exist ipv6RAConfigsStr", func(t *testing.T) {
		err := ovnClient.UpdateLogicalRouterPortRA(lrpName, "address_mode=dhcpv6_stateful,max_interval=30", true)
		require.NoError(t, err)

		out, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"120"}, out.Ipv6Prefix)
		require.Equal(t, map[string]string{
			"address_mode": "dhcpv6_stateful",
			"max_interval": "30",
		}, out.Ipv6RaConfigs)
	})

	t.Run("update ipv6 ra config when enableIPv6RA is false", func(t *testing.T) {
		err := ovnClient.UpdateLogicalRouterPortRA(lrpName, "address_mode=dhcpv6_stateful,max_interval=30", false)
		require.NoError(t, err)

		out, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Empty(t, out.Ipv6Prefix)
		require.Empty(t, out.Ipv6RaConfigs)

	})

	t.Run("do nothing when enableIPv6RA is true and ipv6RAConfigsStr is invalid", func(t *testing.T) {
		err := ovnClient.UpdateLogicalRouterPortRA(lrpName, "address_mode=,test ", true)
		require.NoError(t, err)
	})

	t.Run("do nothing when enableIPv6RA is true and no ipv6 network", func(t *testing.T) {
		lrpName := "test-update-ra-lr-no-ipv6"
		err := ovnClient.CreateLogicalRouterPort(lrName, lrpName, "", nil)
		require.NoError(t, err)

		err = ovnClient.UpdateLogicalRouterPortRA(lrpName, "address_mode=dhcpv6_stateful,max_interval=30", true)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testCreateLogicalRouterPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	LrName := "test-create-lrp-lr"

	err := ovnClient.CreateLogicalRouter(LrName)
	require.NoError(t, err)

	t.Run("create new logical router port with ipv4", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-create-lrp-ipv4"

		err := ovnClient.CreateLogicalRouterPort(LrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24"})
		require.NoError(t, err)

		lrp, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.Equal(t, "00:11:22:37:af:62", lrp.MAC)
		require.ElementsMatch(t, []string{"192.168.123.1/24"}, lrp.Networks)

		lr, err := ovnClient.GetLogicalRouter(LrName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
	})

	t.Run("create new logical router port with ipv6", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-create-lrp-ipv6"

		err := ovnClient.CreateLogicalRouterPort(LrName, lrpName, "00:11:22:37:af:62", []string{"fd00::c0a8:7b01/120"})
		require.NoError(t, err)

		lrp, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.Equal(t, "00:11:22:37:af:62", lrp.MAC)
		require.ElementsMatch(t, []string{"fd00::c0a8:7b01/120"}, lrp.Networks)

		lr, err := ovnClient.GetLogicalRouter(LrName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
	})

	t.Run("create new logical router port with dual", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-create-lrp-dual"
		err := ovnClient.CreateLogicalRouterPort(LrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24", "fd00::c0a8:7b01/120"})
		require.NoError(t, err)

		lrp, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.Equal(t, "00:11:22:37:af:62", lrp.MAC)
		require.ElementsMatch(t, []string{"192.168.123.1/24", "fd00::c0a8:7b01/120"}, lrp.Networks)

		lr, err := ovnClient.GetLogicalRouter(LrName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
	})
}

func (suite *OvnClientTestSuite) testUpdateLogicalRouterPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrpName := "test-update-lrp"
	lrName := "test-update-lrp-lr"

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = ovnClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24"})
	require.NoError(t, err)

	t.Run("normal update", func(t *testing.T) {
		lrp := &ovnnb.LogicalRouterPort{
			Name:     lrpName,
			Networks: []string{"192.168.123.1/24", "192.168.125.1/24"},
		}
		err = ovnClient.UpdateLogicalRouterPort(lrp)
		require.NoError(t, err)

		lrp, err = ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"192.168.123.1/24", "192.168.125.1/24"}, lrp.Networks)
	})

	t.Run("clear networks", func(t *testing.T) {
		lrp := &ovnnb.LogicalRouterPort{
			Name:     lrpName,
			Networks: nil,
		}
		err = ovnClient.UpdateLogicalRouterPort(lrp, &lrp.Networks)
		require.NoError(t, err)

		lrp, err = ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Empty(t, lrp.Networks)
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouterPorts() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	prefix := "test-del-ports-lrp"
	lrName := "test-del-ports-lr"

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		lrpName := fmt.Sprintf("%s-%d", prefix, i)
		err = ovnClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24"})
		require.NoError(t, err)
	}

	lr, err := ovnClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		lrpName := fmt.Sprintf("%s-%d", prefix, i)
		lrp, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
	}

	err = ovnClient.DeleteLogicalRouterPorts(nil, func(lrp *ovnnb.LogicalRouterPort) bool {
		return len(lrp.ExternalIDs) != 0 && lrp.ExternalIDs[logicalRouterKey] == lrName
	})

	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		lrpName := fmt.Sprintf("%s-%d", prefix, i)
		_, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.ErrorContains(t, err, "object not found")
	}

	lr, err = ovnClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)
	require.Empty(t, lr.Ports)
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouterPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrpName := "test-delete-port-lrp"
	lrName := "test-delete-port-lr"

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = ovnClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24"})
	require.NoError(t, err)

	t.Run("no err when delete existent logical router port", func(t *testing.T) {
		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		lrp, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)

		err = ovnClient.DeleteLogicalRouterPort(lrpName)
		require.NoError(t, err)

		_, err = ovnClient.GetLogicalRouterPort(lrpName, false)
		require.ErrorContains(t, err, "object not found")

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.NotContains(t, lr.Ports, lrp.UUID)
	})

	t.Run("no err when delete non-existent logical router port", func(t *testing.T) {
		err := ovnClient.DeleteLogicalRouterPort("test-delete-lrp-non-existent")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testCreateLogicalRouterPortOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrpName := "test-create-op-lrp"
	lrName := "test-create-op-lr"

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("merge ExternalIDs when exist ExternalIDs", func(t *testing.T) {
		lrp := &ovnnb.LogicalRouterPort{
			UUID: ovsclient.NamedUUID(),
			Name: lrpName,
			ExternalIDs: map[string]string{
				"pod": lrpName,
			},
		}

		ops, err := ovnClient.CreateLogicalRouterPortOp(lrp, lrName)
		require.NoError(t, err)
		require.Len(t, ops, 2)
		require.Equal(t, ovsdb.OvsMap{
			GoMap: map[interface{}]interface{}{
				logicalRouterKey: lrName,
				"vendor":         util.CniTypeName,
				"pod":            lrpName,
			},
		}, ops[0].Row["external_ids"])

		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: lrp.UUID,
						},
					},
				},
			},
		}, ops[1].Mutations)
	})

	t.Run("attach ExternalIDs when does't exist ExternalIDs", func(t *testing.T) {
		lrpName := "test-create-op-lrp-none-ext-id"

		lrp := &ovnnb.LogicalRouterPort{
			UUID: ovsclient.NamedUUID(),
			Name: lrpName,
		}

		ops, err := ovnClient.CreateLogicalRouterPortOp(lrp, lrName)
		require.NoError(t, err)

		require.Len(t, ops, 2)
		require.Equal(t, ovsdb.OvsMap{
			GoMap: map[interface{}]interface{}{
				logicalRouterKey: lrName,
				"vendor":         util.CniTypeName,
			},
		}, ops[0].Row["external_ids"])

		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: lrp.UUID,
						},
					},
				},
			},
		}, ops[1].Mutations)
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouterPortOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrpName := "test-del-op-lrp"
	lrName := "test-del-op-lr"

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = ovnClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24"})
	require.NoError(t, err)

	lrp, err := ovnClient.GetLogicalRouterPort(lrpName, false)
	require.NoError(t, err)

	ops, err := ovnClient.DeleteLogicalRouterPortOp(lrpName)
	require.NoError(t, err)
	require.Len(t, ops, 2)

	require.Equal(t,
		[]ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationDelete,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: lrp.UUID,
						},
					},
				},
			},
		}, ops[0].Mutations)

	require.Equal(t,
		ovsdb.Operation{
			Op:    "delete",
			Table: "Logical_Router_Port",
			Where: []ovsdb.Condition{
				{
					Column:   "_uuid",
					Function: "==",
					Value: ovsdb.UUID{
						GoUUID: lrp.UUID,
					},
				},
			},
		}, ops[1])
}

func (suite *OvnClientTestSuite) testLogicalRouterPortOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrpName := "test-op-lrp"

	lrp := &ovnnb.LogicalRouterPort{
		UUID: ovsclient.NamedUUID(),
		Name: lrpName,
		ExternalIDs: map[string]string{
			"vendor": util.CniTypeName,
		},
	}

	err := createLogicalRouterPort(ovnClient, lrp)
	require.NoError(t, err)

	gwChassisUUID := ovsclient.NamedUUID()

	mutation := func(lrp *ovnnb.LogicalRouterPort) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &lrp.GatewayChassis,
			Value:   []string{gwChassisUUID},
			Mutator: ovsdb.MutateOperationInsert,
		}

		return mutation
	}

	ops, err := ovnClient.LogicalRouterPortOp(lrpName, mutation)
	require.NoError(t, err)

	require.Len(t, ops[0].Mutations, 1)
	require.Equal(t, []ovsdb.Mutation{
		{
			Column:  "gateway_chassis",
			Mutator: ovsdb.MutateOperationInsert,
			Value: ovsdb.OvsSet{
				GoSet: []interface{}{
					ovsdb.UUID{
						GoUUID: gwChassisUUID,
					},
				},
			},
		},
	}, ops[0].Mutations)
}

func (suite *OvnClientTestSuite) testlogicalRouterPortFilter() {
	t := suite.T()
	t.Parallel()

	lrName := "test-filter-lrp-lr"
	prefix := "test-filter-lrp"
	networks := []string{"192.168.200.1/24"}
	lrps := make([]*ovnnb.LogicalRouterPort, 0)

	i := 0
	// create three normal lrp
	for ; i < 3; i++ {
		lrpName := fmt.Sprintf("%s-%d", prefix, i)
		lrp := newLogicalRouterPort(lrName, lrpName, util.GenerateMac(), networks)
		lrps = append(lrps, lrp)
	}

	// create two peer lrp
	for ; i < 5; i++ {
		lrpName := fmt.Sprintf("%s-%d", prefix, i)
		lrp := newLogicalRouterPort(lrName, lrpName, util.GenerateMac(), networks)
		peer := lrpName + "-peer"
		lrp.Peer = &peer
		lrps = append(lrps, lrp)
	}

	// create two normal lrp with different logical router name
	for ; i < 6; i++ {
		lrpName := fmt.Sprintf("%s-%d", prefix, i)
		lrp := newLogicalRouterPort(lrName, lrpName, util.GenerateMac(), networks)
		lrp.ExternalIDs[logicalRouterKey] = lrName + "-test"
		lrps = append(lrps, lrp)
	}

	t.Run("include all lrp", func(t *testing.T) {
		filterFunc := logicalRouterPortFilter(nil, nil)
		count := 0
		for _, lrp := range lrps {
			if filterFunc(lrp) {
				count++
			}
		}
		require.Equal(t, count, 6)
	})

	t.Run("include all lrp with external ids", func(t *testing.T) {
		filterFunc := logicalRouterPortFilter(map[string]string{logicalRouterKey: lrName}, nil)
		count := 0
		for _, lrp := range lrps {
			if filterFunc(lrp) {
				count++
			}
		}
		require.Equal(t, count, 5)
	})

	t.Run("include all logicalRouterKey lrp with external ids key's value is empty", func(t *testing.T) {
		filterFunc := logicalRouterPortFilter(map[string]string{logicalRouterKey: ""}, nil)
		count := 0
		for _, lrp := range lrps {
			if filterFunc(lrp) {
				count++
			}
		}
		require.Equal(t, count, 6)
	})

	t.Run("meet custom filter func", func(t *testing.T) {
		filterFunc := logicalRouterPortFilter(nil, func(lrp *ovnnb.LogicalRouterPort) bool {
			return lrp.Peer != nil && len(*lrp.Peer) != 0
		})
		count := 0
		for _, lrp := range lrps {
			if filterFunc(lrp) {
				count++
			}
		}
		require.Equal(t, count, 2)
	})

	t.Run("externalIDs's length is not equal", func(t *testing.T) {
		t.Parallel()

		lrp := newLogicalRouterPort(lrName, prefix+"-test", util.GenerateMac(), networks)
		filterFunc := logicalRouterPortFilter(map[string]string{
			logicalRouterKey: lrName,
			"key":            "value",
		}, nil)

		require.False(t, filterFunc(lrp))
	})
}
