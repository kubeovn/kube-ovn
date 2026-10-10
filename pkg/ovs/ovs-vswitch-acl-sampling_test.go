package ovs

import (
	"fmt"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

func TestReconcileACLSamplingCollectorSetLifecycle(t *testing.T) {
	client := newACLSamplingVswitchTestClient(t, "", map[string]string{"psample": "true"})
	config := aclsampling.NodeConfig{Enabled: true, SetID: 142, LocalGroupID: 142}

	require.NoError(t, client.ReconcileACLSamplingCollectorSet(config))
	collectorSets := listACLSamplingCollectorSets(t, client)
	require.Len(t, collectorSets, 1)
	require.Equal(t, 142, collectorSets[0].ID)
	require.Equal(t, 142, *collectorSets[0].LocalGroupID)
	require.Nil(t, collectorSets[0].IPFIX)
	require.True(t, isOwnedACLSamplingObject(collectorSets[0].ExternalIDs))

	require.NoError(t, client.ReconcileACLSamplingCollectorSet(config))
	require.Len(t, listACLSamplingCollectorSets(t, client), 1)
	collectorSets = listACLSamplingCollectorSets(t, client)
	attachIPFIXToACLSamplingCollectorSet(t, client, &collectorSets[0])
	require.NoError(t, client.ReconcileACLSamplingCollectorSet(config))
	collectorSets = listACLSamplingCollectorSets(t, client)
	require.Nil(t, collectorSets[0].IPFIX)

	config.LocalGroupID = 143
	require.NoError(t, client.ReconcileACLSamplingCollectorSet(config))
	collectorSets = listACLSamplingCollectorSets(t, client)
	require.Len(t, collectorSets, 1)
	require.Equal(t, 143, *collectorSets[0].LocalGroupID)

	config.SetID = 144
	require.NoError(t, client.ReconcileACLSamplingCollectorSet(config))
	collectorSets = listACLSamplingCollectorSets(t, client)
	require.Len(t, collectorSets, 1)
	require.Equal(t, 144, collectorSets[0].ID)

	config.Enabled = false
	require.NoError(t, client.ReconcileACLSamplingCollectorSet(config))
	require.Empty(t, listACLSamplingCollectorSets(t, client))
}

func TestReconcileACLSamplingCollectorSetPreservesUnownedConflict(t *testing.T) {
	client := newACLSamplingVswitchTestClient(t, "system", map[string]string{"psample": "true"})
	bridge := getACLSamplingIntegrationBridge(t, client)
	localGroupID := 777
	seedACLSamplingCollectorSet(t, client, &vswitch.FlowSampleCollectorSet{
		Bridge:       bridge.UUID,
		ExternalIDs:  map[string]string{ExternalIDVendor: "another-application"},
		ID:           142,
		LocalGroupID: &localGroupID,
	})

	config := aclsampling.NodeConfig{Enabled: true, SetID: 142, LocalGroupID: 142}
	err := client.ReconcileACLSamplingCollectorSet(config)
	require.ErrorIs(t, err, ErrACLSamplingNodeConflict)
	collectorSets := listACLSamplingCollectorSets(t, client)
	require.Len(t, collectorSets, 1)
	require.Equal(t, 777, *collectorSets[0].LocalGroupID)
	require.Equal(t, "another-application", collectorSets[0].ExternalIDs[ExternalIDVendor])

	config.Enabled = false
	require.NoError(t, client.ReconcileACLSamplingCollectorSet(config))
	require.Len(t, listACLSamplingCollectorSets(t, client), 1)
}

func TestReconcileACLSamplingCollectorSetRejectsUnsupportedNode(t *testing.T) {
	tests := []struct {
		name         string
		datapathType string
		capabilities map[string]string
	}{
		{
			name:         "userspace datapath",
			datapathType: "netdev",
			capabilities: map[string]string{"psample": "true"},
		},
		{
			name:         "missing psample capability",
			datapathType: "system",
			capabilities: map[string]string{},
		},
		{
			name:         "disabled psample capability",
			datapathType: "system",
			capabilities: map[string]string{"psample": "false"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newACLSamplingVswitchTestClient(t, test.datapathType, test.capabilities)
			err := client.ReconcileACLSamplingCollectorSet(aclsampling.NodeConfig{
				Enabled: true, SetID: 142, LocalGroupID: 142,
			})
			require.ErrorIs(t, err, ErrACLSamplingNodeUnsupported)
			require.Empty(t, listACLSamplingCollectorSets(t, client))
		})
	}
}

func TestReconcileACLSamplingCollectorSetHandlesLegacySchema(t *testing.T) {
	schema := vswitch.Schema()
	delete(schema.Tables[vswitch.FlowSampleCollectorSetTable].Columns, "local_group_id")
	require.ErrorIs(t, validateNodeACLSamplingSchema(schema), ErrACLSamplingNodeUnsupported)

	delete(schema.Tables, vswitch.FlowSampleCollectorSetTable)
	dbModel, err := model.NewClientDBModel(vswitch.DatabaseName, map[string]model.Model{
		vswitch.BridgeTable:      &vswitch.Bridge{},
		vswitch.InterfaceTable:   &vswitch.Interface{},
		vswitch.OpenvSwitchTable: &vswitch.OpenvSwitch{},
		vswitch.PortTable:        &vswitch.Port{},
	})
	require.NoError(t, err)
	_, socket := newOVSDBServer(t, fmt.Sprintf("acl-sampling-legacy-%d", time.Now().UnixNano()), dbModel, schema)
	client, err := NewVswitchClient("unix:"+socket, 1, 1)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	require.NoError(t, client.ReconcileACLSamplingCollectorSet(aclsampling.NodeConfig{}))
}

func newACLSamplingVswitchTestClient(t *testing.T, datapathType string, capabilities map[string]string) *VswitchClient {
	t.Helper()
	dbModel, err := vswitch.FullDatabaseModel()
	require.NoError(t, err)
	_, socket := newOVSDBServer(t, fmt.Sprintf("acl-sampling-vswitch-%d", time.Now().UnixNano()), dbModel, vswitch.Schema())
	client, err := NewVswitchClient("unix:"+socket, 1, 1)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	activeDatapathType := datapathType
	if activeDatapathType == "" {
		activeDatapathType = "system"
	}
	datapath := &vswitch.Datapath{Capabilities: capabilities}
	bridge := &vswitch.Bridge{Name: aclSamplingIntegrationBridge, DatapathType: datapathType}
	openVSwitch := &vswitch.OpenvSwitch{
		Bridges:   []string{"acl-sampling-bridge"},
		Datapaths: map[string]string{activeDatapathType: "acl-sampling-datapath"},
	}
	datapathRow, err := newVswitchRow(client.Schema(), vswitch.DatapathTable, datapath)
	require.NoError(t, err)
	bridgeRow, err := newVswitchRow(client.Schema(), vswitch.BridgeTable, bridge)
	require.NoError(t, err)
	openVSwitchRow, err := newVswitchRow(client.Schema(), vswitch.OpenvSwitchTable, openVSwitch)
	require.NoError(t, err)
	require.NoError(t, client.Transact("seed-acl-sampling-vswitch", []ovsdb.Operation{
		{Op: ovsdb.OperationInsert, Table: vswitch.DatapathTable, Row: datapathRow, UUIDName: "acl-sampling-datapath"},
		{Op: ovsdb.OperationInsert, Table: vswitch.BridgeTable, Row: bridgeRow, UUIDName: "acl-sampling-bridge"},
		{Op: ovsdb.OperationInsert, Table: vswitch.OpenvSwitchTable, Row: openVSwitchRow},
	}))
	return client
}

func getACLSamplingIntegrationBridge(t *testing.T, client *VswitchClient) vswitch.Bridge {
	t.Helper()
	operations := []ovsdb.Operation{{
		Op:      ovsdb.OperationSelect,
		Table:   vswitch.BridgeTable,
		Where:   []ovsdb.Condition{{Column: "name", Function: ovsdb.ConditionEqual, Value: aclSamplingIntegrationBridge}},
		Columns: []string{"_uuid", "name", "datapath_type"},
	}}
	results, err := client.transactVswitchOperations(operations)
	require.NoError(t, err)
	bridges, err := decodeVswitchRows[vswitch.Bridge](client.Schema(), vswitch.BridgeTable, results[0].Rows)
	require.NoError(t, err)
	require.Len(t, bridges, 1)
	return bridges[0]
}

func seedACLSamplingCollectorSet(t *testing.T, client *VswitchClient, collectorSet *vswitch.FlowSampleCollectorSet) {
	t.Helper()
	row, err := newVswitchRow(client.Schema(), vswitch.FlowSampleCollectorSetTable, collectorSet)
	require.NoError(t, err)
	require.NoError(t, client.Transact("seed-acl-sampling-collector-set", []ovsdb.Operation{{
		Op: ovsdb.OperationInsert, Table: vswitch.FlowSampleCollectorSetTable, Row: row,
	}}))
}

func attachIPFIXToACLSamplingCollectorSet(t *testing.T, client *VswitchClient, collectorSet *vswitch.FlowSampleCollectorSet) {
	t.Helper()
	ipfix := &vswitch.IPFIX{Targets: []string{"127.0.0.1:4739"}}
	ipfixRow, err := newVswitchRow(client.Schema(), vswitch.IPFIXTable, ipfix)
	require.NoError(t, err)
	ipfixUUID := "acl-sampling-ipfix"
	collectorSet.IPFIX = &ipfixUUID
	collectorSetRow, err := newVswitchRow(client.Schema(), vswitch.FlowSampleCollectorSetTable, collectorSet, &collectorSet.IPFIX)
	require.NoError(t, err)
	require.NoError(t, client.Transact("attach-ipfix-to-acl-sampling-collector-set", []ovsdb.Operation{
		{Op: ovsdb.OperationInsert, Table: vswitch.IPFIXTable, Row: ipfixRow, UUIDName: ipfixUUID},
		{Op: ovsdb.OperationUpdate, Table: vswitch.FlowSampleCollectorSetTable, Where: uuidWhere(collectorSet.UUID), Row: collectorSetRow},
	}))
}

func listACLSamplingCollectorSets(t *testing.T, client *VswitchClient) []vswitch.FlowSampleCollectorSet {
	t.Helper()
	operations := []ovsdb.Operation{{
		Op:      ovsdb.OperationSelect,
		Table:   vswitch.FlowSampleCollectorSetTable,
		Where:   []ovsdb.Condition{},
		Columns: []string{"_uuid", "bridge", "external_ids", "id", "ipfix", "local_group_id"},
	}}
	results, err := client.transactVswitchOperations(operations)
	require.NoError(t, err)
	collectorSets, err := decodeVswitchRows[vswitch.FlowSampleCollectorSet](client.Schema(), vswitch.FlowSampleCollectorSetTable, results[0].Rows)
	require.NoError(t, err)
	return collectorSets
}
