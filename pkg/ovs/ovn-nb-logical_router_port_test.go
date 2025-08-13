package ovs

import (
	"fmt"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
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

func (suite *OvnClientTestSuite) testCreatePeerRouterPort() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient
	localLrName := "test-create-peer-lr-local"
	remoteLrName := "test-create-peer-lr-remote"
	localRouterPort := fmt.Sprintf("%s-%s", localLrName, remoteLrName)
	remoteRouterPort := fmt.Sprintf("%s-%s", remoteLrName, localLrName)

	err := nbClient.CreateLogicalRouter(localLrName)
	require.NoError(t, err)

	t.Run("create new port", func(t *testing.T) {
		err = nbClient.CreatePeerRouterPort(localLrName, remoteLrName, "192.168.230.1/24,192.168.231.1/24")
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(localRouterPort, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.Equal(t, localLrName, lrp.ExternalIDs[logicalRouterKey])
		require.ElementsMatch(t, []string{"192.168.230.1/24", "192.168.231.1/24"}, lrp.Networks)
		require.Equal(t, remoteRouterPort, *lrp.Peer)

		lr, err := nbClient.GetLogicalRouter(localLrName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{lrp.UUID}, lr.Ports)
	})

	t.Run("update port networks", func(t *testing.T) {
		err = nbClient.CreatePeerRouterPort(localLrName, remoteLrName, "192.168.230.1/24,192.168.241.1/24")
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(localRouterPort, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.ElementsMatch(t, []string{"192.168.230.1/24", "192.168.241.1/24"}, lrp.Networks)
		require.Equal(t, remoteRouterPort, *lrp.Peer)
	})

	t.Run("should log err when logical router does not exist", func(t *testing.T) {
		err = nbClient.CreatePeerRouterPort("test-nonexist-lr-local", "test-nonexist-lr-remote", "192.168.230.1/24,192.168.241.1/24")
		require.Error(t, err)
	})

	t.Run("fail nb client should log err", func(t *testing.T) {
		err = failedNbClient.CreatePeerRouterPort(localLrName, remoteLrName, "192.168.251.1/24,192.168.261.1/24")
		require.Error(t, err)
	})
}

func (suite *OvnClientTestSuite) testUpdateLogicalRouterPortRA() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient
	lrpName := "test-update-ra-lrp"
	lrName := "test-update-ra-lr"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"fd00::c0a8:1001/120"})
	require.NoError(t, err)

	t.Run("update ipv6 ra config when enableIPv6RA is true and ipv6RAConfigsStr is empty", func(t *testing.T) {
		err := nbClient.UpdateLogicalRouterPortRA(lrpName, "", true)
		require.NoError(t, err)

		out, err := nbClient.GetLogicalRouterPort(lrpName, false)
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
		err := nbClient.UpdateLogicalRouterPortRA(lrpName, "address_mode=dhcpv6_stateful,max_interval=30", true)
		require.NoError(t, err)

		out, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"120"}, out.Ipv6Prefix)
		require.Equal(t, map[string]string{
			"address_mode": "dhcpv6_stateful",
			"max_interval": "30",
		}, out.Ipv6RaConfigs)
	})

	t.Run("update ipv6 ra config when enableIPv6RA is false", func(t *testing.T) {
		err := nbClient.UpdateLogicalRouterPortRA(lrpName, "address_mode=dhcpv6_stateful,max_interval=30", false)
		require.NoError(t, err)

		out, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Empty(t, out.Ipv6Prefix)
		require.Empty(t, out.Ipv6RaConfigs)
	})

	t.Run("do nothing when enableIPv6RA is true and ipv6RAConfigsStr is invalid", func(t *testing.T) {
		err := nbClient.UpdateLogicalRouterPortRA(lrpName, "address_mode=,test", true)
		require.NoError(t, err)
	})

	t.Run("do nothing when enableIPv6RA is true and no ipv6 network", func(t *testing.T) {
		lrpName := "test-update-ra-lr-no-ipv6"
		err := nbClient.CreateLogicalRouterPort(lrName, lrpName, "", nil)
		require.NoError(t, err)

		err = nbClient.UpdateLogicalRouterPortRA(lrpName, "address_mode=dhcpv6_stateful,max_interval=30", true)
		require.NoError(t, err)
	})

	t.Run("should log err when logical router does not exist", func(t *testing.T) {
		err = nbClient.UpdateLogicalRouterPortRA("test-nonexist-lr", "address_mode=dhcpv6_stateful,max_interval=30", true)
		require.Error(t, err)
	})

	t.Run("fail nb client should log err", func(t *testing.T) {
		err = failedNbClient.UpdateLogicalRouterPortRA(lrpName, "address_mode=dhcpv6_stateful,max_interval=30", true)
		require.Error(t, err)
	})
}

func (suite *OvnClientTestSuite) testUpdateLogicalRouterPortOptions() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrpName := "test-update-lrp-opt"
	lrName := "test-update-lrp-opt-lr"
	options := map[string]string{
		"k1": "v1",
		"k2": "v2",
	}

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"fd00::c0a8:1001/120"})
	require.NoError(t, err)

	t.Run("add logical router port options with nil", func(t *testing.T) {
		err := nbClient.UpdateLogicalRouterPortOptions(lrpName, map[string]string{})
		require.NoError(t, err)
	})

	t.Run("add logical router port options", func(t *testing.T) {
		err := nbClient.UpdateLogicalRouterPortOptions(lrpName, options)
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Equal(t, options, lrp.Options)
	})

	t.Run("remove logical router port options", func(t *testing.T) {
		err := nbClient.UpdateLogicalRouterPortOptions(lrpName, options)
		require.NoError(t, err)

		err = nbClient.UpdateLogicalRouterPortOptions(lrpName, map[string]string{"k2": ""})
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"k1": "v1"}, lrp.Options)
	})

	t.Run("update logical router port options", func(t *testing.T) {
		err := nbClient.UpdateLogicalRouterPortOptions(lrpName, options)
		require.NoError(t, err)

		err = nbClient.UpdateLogicalRouterPortOptions(lrpName, map[string]string{
			"k2": "",
			"k3": "v3",
		})
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"k1": "v1", "k3": "v3"}, lrp.Options)
	})

	t.Run("should log err when logical router does not exist", func(t *testing.T) {
		err := nbClient.UpdateLogicalRouterPortOptions("test-nonexist-lr", options)
		require.Error(t, err)
	})
}

func (suite *OvnClientTestSuite) testCreateLogicalRouterPort() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient
	lrName := "test-create-lrp-lr"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("create new logical router port with ipv4", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-create-lrp-ipv4"

		err := nbClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24"})
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.Equal(t, "00:11:22:37:af:62", lrp.MAC)
		require.ElementsMatch(t, []string{"192.168.123.1/24"}, lrp.Networks)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)

		err = nbClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24"})
		require.NoError(t, err)
	})

	t.Run("create new logical router port with ipv6", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-create-lrp-ipv6"

		err := nbClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"fd00::c0a8:7b01/120"})
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.Equal(t, "00:11:22:37:af:62", lrp.MAC)
		require.ElementsMatch(t, []string{"fd00::c0a8:7b01/120"}, lrp.Networks)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
	})

	t.Run("create new logical router port with dual", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-create-lrp-dual"
		err := nbClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24", "fd00::c0a8:7b01/120"})
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.Equal(t, "00:11:22:37:af:62", lrp.MAC)
		require.ElementsMatch(t, []string{"192.168.123.1/24", "fd00::c0a8:7b01/120"}, lrp.Networks)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
	})

	t.Run("fail nb client should log err", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-create-lrp-fail-clenit"
		err := failedNbClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24"})
		require.Error(t, err)
	})
}

func (suite *OvnClientTestSuite) testUpdateLogicalRouterPort() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrpName := "test-update-lrp"
	lrName := "test-update-lrp-lr"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24"})
	require.NoError(t, err)

	t.Run("normal update", func(t *testing.T) {
		lrp := &ovnnb.LogicalRouterPort{
			Name:     lrpName,
			Networks: []string{"192.168.123.1/24", "192.168.125.1/24"},
		}
		err = nbClient.UpdateLogicalRouterPort(lrp)
		require.NoError(t, err)

		lrp, err = nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"192.168.123.1/24", "192.168.125.1/24"}, lrp.Networks)
	})

	t.Run("clear networks", func(t *testing.T) {
		lrp := &ovnnb.LogicalRouterPort{
			Name:     lrpName,
			Networks: nil,
		}
		err = nbClient.UpdateLogicalRouterPort(lrp, &lrp.Networks)
		require.NoError(t, err)

		lrp, err = nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Empty(t, lrp.Networks)
	})

	t.Run("update nil lsp", func(t *testing.T) {
		err = nbClient.UpdateLogicalRouterPort(nil)
		require.Error(t, err, "logical_router_port is nil")
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouterPorts() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	prefix := "test-del-ports-lrp"
	lrName := "test-del-ports-lr"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("normal delete logical router ports", func(t *testing.T) {
		for i := range 3 {
			lrpName := fmt.Sprintf("%s-%d", prefix, i)
			err = nbClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24"})
			require.NoError(t, err)
		}

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		for i := range 3 {
			lrpName := fmt.Sprintf("%s-%d", prefix, i)
			lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
			require.NoError(t, err)
			require.Contains(t, lr.Ports, lrp.UUID)
		}

		err = nbClient.DeleteLogicalRouterPorts(nil, func(lrp *ovnnb.LogicalRouterPort) bool {
			return len(lrp.ExternalIDs) != 0 && lrp.ExternalIDs[logicalRouterKey] == lrName
		})

		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.Ports)
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouterPort() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrpName := "test-delete-port-lrp"
	lrName := "test-delete-port-lr"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24"})
	require.NoError(t, err)

	t.Run("no err when delete existent logical router port", func(t *testing.T) {
		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)

		err = nbClient.DeleteLogicalRouterPort(lrpName)
		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.NotContains(t, lr.Ports, lrp.UUID)
	})

	t.Run("no err when delete non-existent logical router port", func(t *testing.T) {
		err := nbClient.DeleteLogicalRouterPort("test-delete-lrp-non-existent")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testGetLogicalRouterPort() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrpName := "test-get-port-lrp"
	lrName := "test-get-port-lr"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24"})
	require.NoError(t, err)

	t.Run("no err when get existent logical router port", func(t *testing.T) {
		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
	})

	t.Run("no err when get non-existent logical router port", func(t *testing.T) {
		lrp, err := nbClient.GetLogicalRouterPort("test-get-lrp-non-exist", true)
		require.NoError(t, err)
		require.Nil(t, lrp)
	})
}

func (suite *OvnClientTestSuite) testGetLogicalRouterPortByUUID() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrpName := "test-get-port-by-uuid-lrp"
	lrName := "test-get-port-by-uuid-lr"
	lrpuuid := "de097cfb-5f7c-46b8-add6-36254ce8f4f1"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	// err = nbClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24"})
	lrp := &ovnnb.LogicalRouterPort{
		UUID: lrpuuid,
		Name: lrpName,
		ExternalIDs: map[string]string{
			"pod": lrpName,
		},
	}

	ops, err := nbClient.CreateLogicalRouterPortOp(lrp, lrName)
	require.NoError(t, err)
	err = nbClient.Transact("lrp-add", ops)
	require.NoError(t, err)

	t.Run("no err when get existent logical router port by uuid", func(t *testing.T) {
		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		lrpc, err := nbClient.GetLogicalRouterPortByUUID(lrpuuid)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrpc.UUID)
	})

	t.Run("no err when get non-existent logical router port by uuid", func(t *testing.T) {
		lrp, err := nbClient.GetLogicalRouterPortByUUID("test-get-lrp-non-existent")
		require.Error(t, err)
		require.Nil(t, lrp)
	})
}

func (suite *OvnClientTestSuite) testCreateLogicalRouterPortOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient
	lrpName := "test-create-op-lrp"
	lrName := "test-create-op-lr"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("create without lrp", func(t *testing.T) {
		ops, err := nbClient.CreateLogicalRouterPortOp(nil, lrName)
		require.Error(t, err, "logical_router_port is nil")
		require.Nil(t, ops)
	})

	t.Run("merge ExternalIDs when exist ExternalIDs", func(t *testing.T) {
		lrp := &ovnnb.LogicalRouterPort{
			UUID: ovsclient.NamedUUID(),
			Name: lrpName,
			ExternalIDs: map[string]string{
				"pod": lrpName,
			},
		}

		ops, err := nbClient.CreateLogicalRouterPortOp(lrp, lrName)
		require.NoError(t, err)
		require.Len(t, ops, 2)
		require.Equal(t, ovsdb.OvsMap{
			GoMap: map[any]any{
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
					GoSet: []any{
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

		ops, err := nbClient.CreateLogicalRouterPortOp(lrp, lrName)
		require.NoError(t, err)

		require.Len(t, ops, 2)
		require.Equal(t, ovsdb.OvsMap{
			GoMap: map[any]any{
				logicalRouterKey: lrName,
				"vendor":         util.CniTypeName,
			},
		}, ops[0].Row["external_ids"])

		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []any{
						ovsdb.UUID{
							GoUUID: lrp.UUID,
						},
					},
				},
			},
		}, ops[1].Mutations)
	})

	t.Run("fail nb client should log err", func(t *testing.T) {
		lrp := &ovnnb.LogicalRouterPort{
			UUID: ovsclient.NamedUUID(),
			Name: lrpName,
			ExternalIDs: map[string]string{
				"pod": lrpName,
			},
		}

		_, err := failedNbClient.CreateLogicalRouterPortOp(lrp, lrName)
		require.Error(t, err)
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouterPortOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrpName := "test-del-op-lrp"
	lrName := "test-del-op-lr"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24"})
	require.NoError(t, err)

	lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
	require.NoError(t, err)

	ops, err := nbClient.DeleteLogicalRouterPortOp(lrpName)
	require.NoError(t, err)
	require.Len(t, ops, 1)

	require.Equal(t,
		[]ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationDelete,
				Value: ovsdb.OvsSet{
					GoSet: []any{
						ovsdb.UUID{
							GoUUID: lrp.UUID,
						},
					},
				},
			},
		}, ops[0].Mutations)
}

func (suite *OvnClientTestSuite) testLogicalRouterPortOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-op-lrp-lr"
	lrpName := "test-op-lrp-lrp"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.CreateLogicalRouterPort(lrName, lrpName, util.GenerateMac(), []string{"172.177.19.1/24"})
	require.NoError(t, err)

	lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
	require.NoError(t, err)
	require.NotNil(t, lrp)
	require.ElementsMatch(t, lrp.Networks, []string{"172.177.19.1/24"})

	t.Run("normal create operations about logical router port", func(t *testing.T) {
		mutation := func(lrp *ovnnb.LogicalRouterPort) *model.Mutation {
			return &model.Mutation{
				Field:   &lrp.Networks,
				Value:   []string{"172.177.29.1/24", "172.177.39.1/24"},
				Mutator: ovsdb.MutateOperationInsert,
			}
		}

		ops, err := nbClient.LogicalRouterPortOp(lrpName, mutation)
		require.NoError(t, err)
		require.Len(t, ops, 1)
		require.Len(t, ops[0].Mutations, 1)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "networks",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []any{
						"172.177.29.1/24",
						"172.177.39.1/24",
					},
				},
			},
		}, ops[0].Mutations)

		ops, err = nbClient.LogicalRouterPortOp(lrpName)
		require.NoError(t, err)
		require.Nil(t, ops)
	})

	t.Run("should log err when logical router port does not exist", func(t *testing.T) {
		ops, err := nbClient.LogicalRouterPortOp("test-nonexist-lrp")
		require.Error(t, err)
		require.Nil(t, ops)
	})
}

func (suite *OvnClientTestSuite) testLogicalRouterPortFilter() {
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

	t.Run("filter out LRP with empty value in external IDs", func(t *testing.T) {
		lrp := newLogicalRouterPort(lrName, prefix+"-empty-value", util.GenerateMac(), networks)
		lrp.ExternalIDs["test-key"] = ""
		lrps = append(lrps, lrp)

		filterFunc := logicalRouterPortFilter(map[string]string{"test-key": ""}, nil)
		require.False(t, filterFunc(lrp))
	})
}

func (suite *OvnClientTestSuite) testAddLogicalRouterPort() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient
	lrName := "test-add-lrp-lr"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("add new logical router port with wrong lr", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-add-lrp-with-wrong-lr"

		err := nbClient.AddLogicalRouterPort("test-add-lrp-wrong-lr", lrpName, "", "192.168.124.1/24")
		require.NotNil(t, err)
	})

	t.Run("add new logical router port without mac", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-add-lrp-without-mac"

		err := nbClient.AddLogicalRouterPort(lrName, lrpName, "", "192.168.124.1/24")
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.NotEmpty(t, lrp.MAC)
		require.ElementsMatch(t, []string{"192.168.124.1/24"}, lrp.Networks)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
	})

	t.Run("add new logical router port with ipv4", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-add-lrp-ipv4"

		err := nbClient.AddLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", "192.168.123.1/24")
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.Equal(t, "00:11:22:37:af:62", lrp.MAC)
		require.ElementsMatch(t, []string{"192.168.123.1/24"}, lrp.Networks)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
	})

	t.Run("add new logical router port with ipv6", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-add-lrp-ipv6"

		err := nbClient.AddLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", "fd00::c0a8:7b01/120")
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.Equal(t, "00:11:22:37:af:62", lrp.MAC)
		require.ElementsMatch(t, []string{"fd00::c0a8:7b01/120"}, lrp.Networks)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
	})

	t.Run("create new logical router port with dual", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-add-lrp-dual"
		err := nbClient.AddLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", "192.168.123.1/24,fd00::c0a8:7b01/120")
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.NotEmpty(t, lrp.UUID)
		require.Equal(t, "00:11:22:37:af:62", lrp.MAC)
		require.ElementsMatch(t, []string{"192.168.123.1/24", "fd00::c0a8:7b01/120"}, lrp.Networks)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
	})

	t.Run("fail nb client should log err", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-add-lrp-ipv4-fail-client"

		err := failedNbClient.AddLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", "192.168.123.1/24")
		require.Error(t, err)
	})
}

func (suite *OvnClientTestSuite) testListLogicalRouterPorts() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	prefix := "test-list-ports-lrp"
	lrName := "test-list-ports-lr"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	for i := range 3 {
		lrpName := fmt.Sprintf("%s-%d", prefix, i)
		err = nbClient.CreateLogicalRouterPort(lrName, lrpName, "00:11:22:37:af:62", []string{"192.168.123.1/24"})
		require.NoError(t, err)
	}

	lrps, err := nbClient.ListLogicalRouterPorts(nil, func(lrp *ovnnb.LogicalRouterPort) bool {
		return len(lrp.ExternalIDs) != 0 && lrp.ExternalIDs[logicalRouterKey] == lrName
	})
	require.NoError(t, err)
	require.Len(t, lrps, 3)

	err = nbClient.DeleteLogicalRouterPorts(nil, func(lrp *ovnnb.LogicalRouterPort) bool {
		return len(lrp.ExternalIDs) != 0 && lrp.ExternalIDs[logicalRouterKey] == lrName
	})

	require.NoError(t, err)

	lrps, err = nbClient.ListLogicalRouterPorts(nil, func(lrp *ovnnb.LogicalRouterPort) bool {
		return len(lrp.ExternalIDs) != 0 && lrp.ExternalIDs[logicalRouterKey] == lrName
	})
	require.NoError(t, err)
	require.Len(t, lrps, 0)
}

func (suite *OvnClientTestSuite) testLogicalRouterPortUpdateGatewayChassisOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-name-update-gateway-chassis-op-lr"
	lrpName := "test-name-update-gateway-chassis-op-lrp"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("upadate gateway chassis op with nil uuids", func(t *testing.T) {
		_, err := nbClient.LogicalRouterPortUpdateGatewayChassisOp(lrpName, nil, "op")
		require.NoError(t, err)
	})
}
