package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/mapper"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	ErrACLSamplingNodeUnsupported = errors.New("local ACL sampling is unsupported")
	ErrACLSamplingNodeConflict    = errors.New("local ACL sampling collector set conflicts with an unowned object")
)

const aclSamplingIntegrationBridge = "br-int"

type aclSamplingCollectorSetOwnership struct {
	UUID        string            `ovsdb:"_uuid"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// ReconcileACLSamplingCollectorSet idempotently configures local psample
// delivery for the integration bridge. Capability checks and all OVSDB
// changes stay best-effort and never affect the node networking setup.
func (c *VswitchClient) ReconcileACLSamplingCollectorSet(config aclsampling.NodeConfig) error {
	if !config.Enabled {
		return c.cleanupACLSamplingCollectorSets()
	}
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid node ACL sampling configuration: %w", err)
	}
	if err := validateNodeACLSamplingSchema(c.Schema()); err != nil {
		return err
	}

	bridge, collectorSets, err := c.readNodeACLSamplingState()
	if err != nil {
		return err
	}

	desiredExternalIDs := map[string]string{
		ExternalIDVendor:             util.CniTypeName,
		aclSamplingFeatureExternalID: aclSamplingFeature,
	}
	var desired *vswitch.FlowSampleCollectorSet
	operations := make([]ovsdb.Operation, 0, len(collectorSets)+1)
	for i := range collectorSets {
		collectorSet := &collectorSets[i]
		isDesiredKey := collectorSet.Bridge == bridge.UUID && collectorSet.ID == int(config.SetID)
		if isDesiredKey && !isOwnedACLSamplingObject(collectorSet.ExternalIDs) {
			return fmt.Errorf("%w: collector set ID %d on bridge %s", ErrACLSamplingNodeConflict, config.SetID, bridge.Name)
		}
		if !isOwnedACLSamplingObject(collectorSet.ExternalIDs) {
			continue
		}
		if isDesiredKey {
			desired = collectorSet
			continue
		}
		operations = append(operations, deleteVswitchRowOperation(vswitch.FlowSampleCollectorSetTable, collectorSet.UUID))
	}

	localGroupID := int(config.LocalGroupID)
	if desired == nil {
		desired = &vswitch.FlowSampleCollectorSet{
			Bridge:       bridge.UUID,
			ExternalIDs:  desiredExternalIDs,
			ID:           int(config.SetID),
			LocalGroupID: &localGroupID,
		}
		row, err := newVswitchRow(c.Schema(), vswitch.FlowSampleCollectorSetTable, desired)
		if err != nil {
			return fmt.Errorf("build local ACL sampling collector set: %w", err)
		}
		operations = append(operations, ovsdb.Operation{
			Op:    ovsdb.OperationInsert,
			Table: vswitch.FlowSampleCollectorSetTable,
			Row:   row,
		})
	} else if desired.IPFIX != nil || desired.LocalGroupID == nil || *desired.LocalGroupID != localGroupID ||
		!maps.Equal(desired.ExternalIDs, desiredExternalIDs) {
		desired.IPFIX = nil
		desired.LocalGroupID = &localGroupID
		desired.ExternalIDs = desiredExternalIDs
		row, err := newVswitchRow(c.Schema(), vswitch.FlowSampleCollectorSetTable, desired,
			&desired.IPFIX, &desired.LocalGroupID, &desired.ExternalIDs)
		if err != nil {
			return fmt.Errorf("build local ACL sampling collector set update: %w", err)
		}
		operations = append(operations, ovsdb.Operation{
			Op:    ovsdb.OperationUpdate,
			Table: vswitch.FlowSampleCollectorSetTable,
			Where: uuidWhere(desired.UUID),
			Row:   row,
		})
	}

	if err := c.Transact("acl-sampling-node-reconcile", operations); err != nil {
		return fmt.Errorf("reconcile local ACL sampling collector set: %w", err)
	}
	return nil
}

func validateNodeACLSamplingSchema(schema ovsdb.DatabaseSchema) error {
	required := map[string][]string{
		vswitch.BridgeTable:                 {"name", "datapath_type"},
		vswitch.DatapathTable:               {"capabilities"},
		vswitch.OpenvSwitchTable:            {"datapaths"},
		vswitch.FlowSampleCollectorSetTable: {"bridge", "external_ids", "id", "ipfix", "local_group_id"},
	}
	for tableName, columns := range required {
		table, ok := schema.Tables[tableName]
		if !ok {
			return fmt.Errorf("%w: table %s is missing", ErrACLSamplingNodeUnsupported, tableName)
		}
		for _, column := range columns {
			if _, ok := table.Columns[column]; !ok {
				return fmt.Errorf("%w: column %s.%s is missing", ErrACLSamplingNodeUnsupported, tableName, column)
			}
		}
	}
	return nil
}

func validateNodeACLSamplingCleanupSchema(schema ovsdb.DatabaseSchema) error {
	table, ok := schema.Tables[vswitch.FlowSampleCollectorSetTable]
	if !ok {
		return fmt.Errorf("%w: table %s is missing", ErrACLSamplingNodeUnsupported, vswitch.FlowSampleCollectorSetTable)
	}
	if _, ok := table.Columns["external_ids"]; !ok {
		return fmt.Errorf("%w: column %s.external_ids is missing", ErrACLSamplingNodeUnsupported, vswitch.FlowSampleCollectorSetTable)
	}
	return nil
}

func (c *VswitchClient) readNodeACLSamplingState() (*vswitch.Bridge, []vswitch.FlowSampleCollectorSet, error) {
	operations := []ovsdb.Operation{
		{
			Op:      ovsdb.OperationSelect,
			Table:   vswitch.BridgeTable,
			Where:   []ovsdb.Condition{{Column: "name", Function: ovsdb.ConditionEqual, Value: aclSamplingIntegrationBridge}},
			Columns: []string{"_uuid", "name", "datapath_type"},
		},
		{
			Op:      ovsdb.OperationSelect,
			Table:   vswitch.OpenvSwitchTable,
			Where:   []ovsdb.Condition{},
			Columns: []string{"_uuid", "datapaths"},
		},
		{
			Op:      ovsdb.OperationSelect,
			Table:   vswitch.DatapathTable,
			Where:   []ovsdb.Condition{},
			Columns: []string{"_uuid", "capabilities"},
		},
		{
			Op:      ovsdb.OperationSelect,
			Table:   vswitch.FlowSampleCollectorSetTable,
			Where:   []ovsdb.Condition{},
			Columns: []string{"_uuid", "bridge", "external_ids", "id", "ipfix", "local_group_id"},
		},
	}
	results, err := c.transactVswitchOperations(operations)
	if err != nil {
		return nil, nil, fmt.Errorf("read local ACL sampling state: %w", err)
	}

	bridges, err := decodeVswitchRows[vswitch.Bridge](c.Schema(), vswitch.BridgeTable, results[0].Rows)
	if err != nil {
		return nil, nil, err
	}
	if len(bridges) != 1 {
		return nil, nil, fmt.Errorf("%w: expected one %s bridge, found %d", ErrACLSamplingNodeUnsupported, aclSamplingIntegrationBridge, len(bridges))
	}
	bridge := &bridges[0]
	datapathType := bridge.DatapathType
	if datapathType == "" {
		datapathType = "system"
	}
	if datapathType != "system" {
		return nil, nil, fmt.Errorf("%w: bridge %s uses datapath type %s", ErrACLSamplingNodeUnsupported, bridge.Name, datapathType)
	}

	openVSwitchRows, err := decodeVswitchRows[vswitch.OpenvSwitch](c.Schema(), vswitch.OpenvSwitchTable, results[1].Rows)
	if err != nil {
		return nil, nil, err
	}
	if len(openVSwitchRows) != 1 {
		return nil, nil, fmt.Errorf("%w: expected one Open_vSwitch row, found %d", ErrACLSamplingNodeUnsupported, len(openVSwitchRows))
	}
	datapathUUID := openVSwitchRows[0].Datapaths[datapathType]
	if datapathUUID == "" {
		return nil, nil, fmt.Errorf("%w: datapath type %s is not active", ErrACLSamplingNodeUnsupported, datapathType)
	}

	datapaths, err := decodeVswitchRows[vswitch.Datapath](c.Schema(), vswitch.DatapathTable, results[2].Rows)
	if err != nil {
		return nil, nil, err
	}
	foundDatapath := false
	for i := range datapaths {
		if datapaths[i].UUID != datapathUUID {
			continue
		}
		foundDatapath = true
		if !strings.EqualFold(datapaths[i].Capabilities["psample"], "true") {
			return nil, nil, fmt.Errorf("%w: datapath type %s does not report psample=true", ErrACLSamplingNodeUnsupported, datapathType)
		}
		break
	}
	if !foundDatapath {
		return nil, nil, fmt.Errorf("%w: active datapath %s was not found", ErrACLSamplingNodeUnsupported, datapathUUID)
	}

	collectorSets, err := decodeVswitchRows[vswitch.FlowSampleCollectorSet](c.Schema(), vswitch.FlowSampleCollectorSetTable, results[3].Rows)
	if err != nil {
		return nil, nil, err
	}
	return bridge, collectorSets, nil
}

func (c *VswitchClient) cleanupACLSamplingCollectorSets() error {
	if err := validateNodeACLSamplingCleanupSchema(c.Schema()); err != nil {
		if errors.Is(err, ErrACLSamplingNodeUnsupported) {
			return nil
		}
		return err
	}
	operations := []ovsdb.Operation{{
		Op:      ovsdb.OperationSelect,
		Table:   vswitch.FlowSampleCollectorSetTable,
		Where:   []ovsdb.Condition{},
		Columns: []string{"_uuid", "external_ids"},
	}}
	results, err := c.transactVswitchOperations(operations)
	if err != nil {
		return fmt.Errorf("list local ACL sampling collector sets for cleanup: %w", err)
	}
	collectorSets, err := decodeVswitchRows[aclSamplingCollectorSetOwnership](c.Schema(), vswitch.FlowSampleCollectorSetTable, results[0].Rows)
	if err != nil {
		return err
	}
	deleteOperations := make([]ovsdb.Operation, 0, len(collectorSets))
	for i := range collectorSets {
		if isOwnedACLSamplingObject(collectorSets[i].ExternalIDs) {
			deleteOperations = append(deleteOperations,
				deleteVswitchRowOperation(vswitch.FlowSampleCollectorSetTable, collectorSets[i].UUID))
		}
	}
	if err := c.Transact("acl-sampling-node-cleanup", deleteOperations); err != nil {
		return fmt.Errorf("clean up local ACL sampling collector sets: %w", err)
	}
	return nil
}

func (c *VswitchClient) transactVswitchOperations(operations []ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	results, err := c.TransactResults(context.Background(), operations...)
	if err != nil {
		return nil, err
	}
	if len(results) != len(operations) {
		return nil, fmt.Errorf("expected %d OVSDB operation results, got %d", len(operations), len(results))
	}
	return results, nil
}

func decodeVswitchRows[T any](schema ovsdb.DatabaseSchema, tableName string, rows []ovsdb.Row) ([]T, error) {
	table := schema.Table(tableName)
	if table == nil {
		return nil, fmt.Errorf("OVSDB table %s is missing", tableName)
	}
	ovsMapper := mapper.NewMapper(schema)
	result := make([]T, len(rows))
	for i := range rows {
		info, err := mapper.NewInfo(tableName, table, &result[i])
		if err != nil {
			return nil, fmt.Errorf("build OVSDB mapper for table %s: %w", tableName, err)
		}
		if err := ovsMapper.GetRowDataWithUUID(&rows[i], info); err != nil {
			return nil, fmt.Errorf("decode OVSDB row from table %s: %w", tableName, err)
		}
	}
	return result, nil
}

func newVswitchRow(schema ovsdb.DatabaseSchema, tableName string, value any, fields ...any) (ovsdb.Row, error) {
	table := schema.Table(tableName)
	if table == nil {
		return nil, fmt.Errorf("OVSDB table %s is missing", tableName)
	}
	info, err := mapper.NewInfo(tableName, table, value)
	if err != nil {
		return nil, err
	}
	return mapper.NewMapper(schema).NewRow(info, fields...)
}

func uuidWhere(uuid string) []ovsdb.Condition {
	return []ovsdb.Condition{{Column: "_uuid", Function: ovsdb.ConditionEqual, Value: ovsdb.UUID{GoUUID: uuid}}}
}

func deleteVswitchRowOperation(tableName, uuid string) ovsdb.Operation {
	return ovsdb.Operation{Op: ovsdb.OperationDelete, Table: tableName, Where: uuidWhere(uuid)}
}
