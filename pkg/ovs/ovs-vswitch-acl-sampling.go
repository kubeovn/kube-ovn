package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

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
	collectorTable := c.OptionalTable(vswitch.FlowSampleCollectorSetTable, &vswitch.FlowSampleCollectorSet{})
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
		deleteOps, err := collectorTable.DeleteOps(collectorSet)
		if err != nil {
			return fmt.Errorf("build local ACL sampling collector set delete: %w", err)
		}
		operations = append(operations, deleteOps...)
	}

	localGroupID := int(config.LocalGroupID)
	if desired == nil {
		desired = &vswitch.FlowSampleCollectorSet{
			Bridge:       bridge.UUID,
			ExternalIDs:  desiredExternalIDs,
			ID:           int(config.SetID),
			LocalGroupID: &localGroupID,
		}
		createOps, err := collectorTable.CreateOps(desired)
		if err != nil {
			return fmt.Errorf("build local ACL sampling collector set: %w", err)
		}
		operations = append(operations, createOps...)
	} else if desired.IPFIX != nil || desired.LocalGroupID == nil || *desired.LocalGroupID != localGroupID ||
		!maps.Equal(desired.ExternalIDs, desiredExternalIDs) {
		desired.IPFIX = nil
		desired.LocalGroupID = &localGroupID
		desired.ExternalIDs = desiredExternalIDs
		updateOps, err := collectorTable.UpdateOps(desired, desired,
			&desired.IPFIX, &desired.LocalGroupID, &desired.ExternalIDs)
		if err != nil {
			return fmt.Errorf("build local ACL sampling collector set update: %w", err)
		}
		operations = append(operations, updateOps...)
	}

	if err := collectorTable.Transact(context.Background(), "acl-sampling-node-reconcile", operations...); err != nil {
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
	var bridges []vswitch.Bridge
	err := c.Table(&vswitch.Bridge{}).Filter(context.Background(), func(row *vswitch.Bridge) bool {
		return row.Name == aclSamplingIntegrationBridge
	}, &bridges)
	if err != nil {
		return nil, nil, fmt.Errorf("list local ACL sampling bridge: %w", err)
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

	var openVSwitchRows []vswitch.OpenvSwitch
	if err := c.Table(&vswitch.OpenvSwitch{}).List(context.Background(), &openVSwitchRows); err != nil {
		return nil, nil, fmt.Errorf("list Open_vSwitch rows: %w", err)
	}
	if len(openVSwitchRows) != 1 {
		return nil, nil, fmt.Errorf("%w: expected one Open_vSwitch row, found %d", ErrACLSamplingNodeUnsupported, len(openVSwitchRows))
	}
	datapathUUID := openVSwitchRows[0].Datapaths[datapathType]
	if datapathUUID == "" {
		return nil, nil, fmt.Errorf("%w: datapath type %s is not active", ErrACLSamplingNodeUnsupported, datapathType)
	}

	var datapaths []vswitch.Datapath
	if err := c.OptionalTable(vswitch.DatapathTable, &vswitch.Datapath{}).List(context.Background(), &datapaths); err != nil {
		return nil, nil, fmt.Errorf("list datapaths: %w", err)
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

	var collectorSets []vswitch.FlowSampleCollectorSet
	if err := c.OptionalTable(vswitch.FlowSampleCollectorSetTable, &vswitch.FlowSampleCollectorSet{}).List(context.Background(), &collectorSets); err != nil {
		return nil, nil, fmt.Errorf("list flow sample collector sets: %w", err)
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
	collectorTable := c.OptionalTable(vswitch.FlowSampleCollectorSetTable, &vswitch.FlowSampleCollectorSet{})
	var collectorSets []vswitch.FlowSampleCollectorSet
	if err := collectorTable.List(context.Background(), &collectorSets); err != nil {
		return fmt.Errorf("list local ACL sampling collector sets for cleanup: %w", err)
	}
	deleteOperations := make([]ovsdb.Operation, 0, len(collectorSets))
	for i := range collectorSets {
		if isOwnedACLSamplingObject(collectorSets[i].ExternalIDs) {
			deleteOps, err := collectorTable.DeleteOps(&collectorSets[i])
			if err != nil {
				return fmt.Errorf("build local ACL sampling collector set cleanup: %w", err)
			}
			deleteOperations = append(deleteOperations, deleteOps...)
		}
	}
	if err := collectorTable.Transact(context.Background(), "acl-sampling-node-cleanup", deleteOperations...); err != nil {
		return fmt.Errorf("clean up local ACL sampling collector sets: %w", err)
	}
	return nil
}
