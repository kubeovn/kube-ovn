package ovs

import (
	"fmt"
	"testing"

	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

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
		require.Equal(t, []string{"192.168.230.1/24", "192.168.231.1/24"}, lrp.Networks)
		require.Equal(t, remoteRouterPort, *lrp.Peer)

		lr, err := ovnClient.GetLogicalRouter(localLrName, false)
		require.NoError(t, err)
		require.Equal(t, []string{lrp.UUID}, lr.Ports)
	})

	t.Run("update port networks", func(t *testing.T) {
		err = ovnClient.CreatePeerRouterPort(localLrName, remoteLrName, "192.168.230.1/24,192.168.241.1/24")
		require.NoError(t, err)

		lrp, err := ovnClient.GetLogicalRouterPort(localRouterPort, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.Equal(t, []string{"192.168.230.1/24", "192.168.241.1/24"}, lrp.Networks)
		require.Equal(t, remoteRouterPort, *lrp.Peer)
	})
}

func (suite *OvnClientTestSuite) testUpdateRouterPortIPv6RA() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrpName := "test-update-ra-lrp"
	lrName := "test-update-ra-lr"

	lrp := &ovnnb.LogicalRouterPort{
		UUID: ovsclient.UUID(),
		Name: lrpName,
		MAC:  "00:11:22:37:af:62",
		// Networks: []string{"192.168.33.1/24"},
		Networks: []string{"fd00::c0a8:1001/120"},
	}

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = ovnClient.CreateLogicalRouterPort(lrp, lrName)
	require.NoError(t, err)

	t.Run("update ipv6 ra config when enableIPv6RA is true and ipv6RAConfigsStr is empty", func(t *testing.T) {
		err := ovnClient.UpdateRouterPortIPv6RA(lrpName, "", true)
		require.NoError(t, err)

		out, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Equal(t, []string{"120"}, out.Ipv6Prefix)
		require.Equal(t, map[string]string{
			"address_mode":  "dhcpv6_stateful",
			"max_interval":  "30",
			"min_interval":  "5",
			"send_periodic": "true",
		}, out.Ipv6RaConfigs)
	})

	t.Run("update ipv6 ra config when enableIPv6RA is true and exist ipv6RAConfigsStr", func(t *testing.T) {
		err := ovnClient.UpdateRouterPortIPv6RA(lrpName, "address_mode=dhcpv6_stateful,max_interval=30", true)
		require.NoError(t, err)

		out, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Equal(t, []string{"120"}, out.Ipv6Prefix)
		require.Equal(t, map[string]string{
			"address_mode": "dhcpv6_stateful",
			"max_interval": "30",
		}, out.Ipv6RaConfigs)
	})

	t.Run("update ipv6 ra config when enableIPv6RA is false", func(t *testing.T) {
		err := ovnClient.UpdateRouterPortIPv6RA(lrpName, "address_mode=dhcpv6_stateful,max_interval=30", false)
		require.NoError(t, err)

		out, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Empty(t, out.Ipv6Prefix)
		require.Empty(t, out.Ipv6RaConfigs)

	})

	t.Run("do nothing when enableIPv6RA is true and ipv6RAConfigsStr is invalid", func(t *testing.T) {
		err := ovnClient.UpdateRouterPortIPv6RA(lrpName, "address_mode=,test ", true)
		require.NoError(t, err)
	})

	t.Run("do nothing when enableIPv6RA is true and no ipv6 network", func(t *testing.T) {
		name := "test-update-ra-lr-no-ipv6"

		lrp := &ovnnb.LogicalRouterPort{
			UUID:     ovsclient.UUID(),
			Name:     name,
			MAC:      "00:11:22:37:af:62",
			Networks: []string{"192.168.33.1/24"},
		}

		err := ovnClient.CreateLogicalRouterPort(lrp, lrName)
		require.NoError(t, err)

		err = ovnClient.UpdateRouterPortIPv6RA(name, "address_mode=dhcpv6_stateful,max_interval=30", true)
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

		name := "test-create-lrp-ipv4"

		lrp := &ovnnb.LogicalRouterPort{
			UUID:     ovsclient.UUID(),
			Name:     name,
			MAC:      "00:11:22:37:af:62",
			Networks: []string{"192.168.123.1/24"},
		}

		err := ovnClient.CreateLogicalRouterPort(lrp, LrName)
		require.NoError(t, err)

		lrp, err = ovnClient.GetLogicalRouterPort(name, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.Equal(t, "00:11:22:37:af:62", lrp.MAC)
		require.Equal(t, []string{"192.168.123.1/24"}, lrp.Networks)

		lr, err := ovnClient.GetLogicalRouter(LrName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
	})

	t.Run("create new logical router port with ipv6", func(t *testing.T) {
		t.Parallel()

		name := "test-create-lrp-ipv6"

		lrp := &ovnnb.LogicalRouterPort{
			UUID:     ovsclient.UUID(),
			Name:     name,
			MAC:      "00:11:22:37:af:62",
			Networks: []string{"fd00::c0a8:7b01/120"},
		}

		err := ovnClient.CreateLogicalRouterPort(lrp, LrName)
		require.NoError(t, err)

		lrp, err = ovnClient.GetLogicalRouterPort(name, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.Equal(t, "00:11:22:37:af:62", lrp.MAC)
		require.Equal(t, []string{"fd00::c0a8:7b01/120"}, lrp.Networks)

		lr, err := ovnClient.GetLogicalRouter(LrName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
	})

	t.Run("create new logical router port with dual", func(t *testing.T) {
		t.Parallel()

		name := "test-create-lrp-dual"

		lrp := &ovnnb.LogicalRouterPort{
			UUID:     ovsclient.UUID(),
			Name:     name,
			MAC:      "00:11:22:37:af:62",
			Networks: []string{"192.168.123.1/24", "fd00::c0a8:7b01/120"},
		}

		err := ovnClient.CreateLogicalRouterPort(lrp, LrName)
		require.NoError(t, err)

		lrp, err = ovnClient.GetLogicalRouterPort(name, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.Equal(t, "00:11:22:37:af:62", lrp.MAC)
		require.Equal(t, []string{"192.168.123.1/24", "fd00::c0a8:7b01/120"}, lrp.Networks)

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
	lrName := "test-update-lr"

	lrp := &ovnnb.LogicalRouterPort{
		UUID:     ovsclient.UUID(),
		Name:     lrpName,
		MAC:      "00:11:22:37:af:62",
		Networks: []string{"192.168.123.1/24"},
	}

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = ovnClient.CreateLogicalRouterPort(lrp, lrName)
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
		require.Equal(t, []string{"192.168.123.1/24", "192.168.125.1/24"}, lrp.Networks)
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

func (suite *OvnClientTestSuite) testDeleteLogicalRouterPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrpName := "test-delete-port-lrp"
	lrName := "test-delete-port-lr"

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	lrp := &ovnnb.LogicalRouterPort{
		UUID:     ovsclient.UUID(),
		Name:     lrpName,
		MAC:      "00:11:22:37:af:62",
		Networks: []string{"192.168.123.1/24"},
	}

	err = ovnClient.CreateLogicalRouterPort(lrp, lrName)
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

	t.Run("merget ExternalIDs when exist ExternalIDs", func(t *testing.T) {
		lrp := &ovnnb.LogicalRouterPort{
			UUID: ovsclient.UUID(),
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
		lrpName := "test-create-op-lrp-none-exid"

		lrp := &ovnnb.LogicalRouterPort{
			UUID: ovsclient.UUID(),
			Name: lrpName,
		}

		ops, err := ovnClient.CreateLogicalRouterPortOp(lrp, lrName)
		require.NoError(t, err)

		require.Len(t, ops, 2)
		require.Equal(t, ovsdb.OvsMap{
			GoMap: map[interface{}]interface{}{
				logicalRouterKey: lrName,
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

	lrp := &ovnnb.LogicalRouterPort{
		UUID:     ovsclient.UUID(),
		Name:     lrpName,
		MAC:      "00:11:22:37:af:62",
		Networks: []string{"192.168.123.1/24"},
		ExternalIDs: map[string]string{
			logicalRouterKey: lrName,
		},
	}

	ops, err := ovnClient.DeleteLogicalRouterPortOp(lrp)
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
