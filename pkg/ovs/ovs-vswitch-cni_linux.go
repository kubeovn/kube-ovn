package ovs

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/cenkalti/backoff/v7"
	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// NewCNIVswitchClient uses short-lived, synchronous transactions. A CNI process
// does not need a monitor or a node-wide cache of interfaces and QoS records.
func NewCNIVswitchClient(addr string) (*VswitchClient, error) {
	dbModel, err := model.NewClientDBModel(vswitch.DatabaseName, map[string]model.Model{
		vswitch.BridgeTable:      &vswitch.Bridge{},
		vswitch.InterfaceTable:   &vswitch.Interface{},
		vswitch.OpenvSwitchTable: &vswitch.OpenvSwitch{},
		vswitch.PortTable:        &vswitch.Port{},
		vswitch.QoSTable:         &vswitch.QoS{},
		vswitch.QueueTable:       &vswitch.Queue{},
	})
	if err != nil {
		return nil, err
	}
	c, err := client.NewOVSDBClient(dbModel, client.WithEndpoint(addr),
		client.WithReconnect(30*time.Second, backoff.NewConstantBackOff(time.Second)))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return &VswitchClient{Client: c, Timeout: 30 * time.Second}, nil
}

func selectVswitch(table string, where []ovsdb.Condition) ovsdb.Operation {
	if where == nil {
		where = []ovsdb.Condition{}
	}
	return ovsdb.Operation{Op: ovsdb.OperationSelect, Table: table, Where: where}
}

func nameWhere(name string) []ovsdb.Condition {
	return []ovsdb.Condition{{Column: "name", Function: ovsdb.ConditionEqual, Value: name}}
}

func readVswitch[T any](c *VswitchClient, table string, where []ovsdb.Condition) ([]T, error) {
	results, err := c.transactVswitchOperations([]ovsdb.Operation{selectVswitch(table, where)})
	if err != nil {
		return nil, err
	}
	return decodeVswitchRows[T](c.Schema(), table, results[0].Rows)
}

func (c *VswitchClient) updateCNIModel(table, uuid string, value any, fields ...any) error {
	row, err := newVswitchRow(c.Schema(), table, value, fields...)
	if err != nil {
		return err
	}
	_, err = c.transactVswitchOperations([]ovsdb.Operation{{Op: ovsdb.OperationUpdate, Table: table, Where: uuidWhere(uuid), Row: row}})
	return err
}

func stringMapMutation(column string, values map[string]string, mutator ovsdb.Mutator) ovsdb.Mutation {
	m, _ := ovsdb.NewOvsMap(values)
	return ovsdb.Mutation{Column: column, Mutator: mutator, Value: m}
}

func uuidSetMutation(column string, uuids []string, mutator ovsdb.Mutator) ovsdb.Mutation {
	values := make([]ovsdb.UUID, len(uuids))
	for i, uuid := range uuids {
		values[i] = ovsdb.UUID{GoUUID: uuid}
	}
	s, _ := ovsdb.NewOvsSet(values)
	return ovsdb.Mutation{Column: column, Mutator: mutator, Value: s}
}

// AddCNIPort atomically creates the interface and its bridge membership, or
// updates an existing port on the same bridge. Map mutations retain OVS/OVN
// owned keys, including ovn-installed, across repeated ADD invocations.
func (c *VswitchClient) AddCNIPort(iface *vswitch.Interface) error {
	bridges, err := readVswitch[vswitch.Bridge](c, vswitch.BridgeTable, nameWhere("br-int"))
	if err != nil {
		return err
	}
	if len(bridges) != 1 {
		return fmt.Errorf("expected integration bridge br-int, found %d", len(bridges))
	}
	ports, err := readVswitch[vswitch.Port](c, vswitch.PortTable, nameWhere(iface.Name))
	if err != nil {
		return err
	}
	var ops []ovsdb.Operation
	if len(ports) == 0 {
		iface.UUID = ovsclient.NamedUUID()
		port := &vswitch.Port{UUID: ovsclient.NamedUUID(), Name: iface.Name, Interfaces: []string{iface.UUID}}
		ops, err = c.Create(iface)
		if err != nil {
			return err
		}
		createPort, err := c.Create(port)
		if err != nil {
			return err
		}
		ops = append(ops, createPort...)
		ops = append(ops, ovsdb.Operation{Op: ovsdb.OperationMutate, Table: vswitch.BridgeTable, Where: uuidWhere(bridges[0].UUID), Mutations: []ovsdb.Mutation{uuidSetMutation("ports", []string{port.UUID}, ovsdb.MutateOperationInsert)}})
	} else {
		if len(ports) != 1 || !slices.Contains(bridges[0].Ports, ports[0].UUID) {
			return fmt.Errorf("port %s already exists outside br-int", iface.Name)
		}
		existing, err := readVswitch[vswitch.Interface](c, vswitch.InterfaceTable, nameWhere(iface.Name))
		if err != nil {
			return err
		}
		if len(existing) != 1 || !slices.Contains(ports[0].Interfaces, existing[0].UUID) {
			return fmt.Errorf("port %s has incompatible interfaces", iface.Name)
		}
		iface.UUID = existing[0].UUID
		// Only the CNI-owned scalar fields are replaced.
		row, err := newVswitchRow(c.Schema(), vswitch.InterfaceTable, iface, &iface.Type, &iface.MTURequest)
		if err != nil {
			return err
		}
		ops = append(ops, ovsdb.Operation{Op: ovsdb.OperationUpdate, Table: vswitch.InterfaceTable, Where: uuidWhere(iface.UUID), Row: row})
		for column, values := range map[string]map[string]string{"external_ids": iface.ExternalIDs, "options": iface.Options} {
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			ops = append(ops, cniMapPatch(vswitch.InterfaceTable, iface.UUID, column, values, keys))
		}
	}
	_, err = c.transactVswitchOperations(ops)
	return err
}

func (c *VswitchClient) CleanDuplicateCNIPort(ifaceID, name string) error {
	ifaces, err := c.cniInterfaces(ifaceID)
	if err != nil {
		return err
	}
	var ops []ovsdb.Operation
	keys, _ := ovsdb.NewOvsSet([]string{"iface-id"})
	for _, iface := range ifaces {
		if iface.Name != name {
			ops = append(ops, ovsdb.Operation{Op: ovsdb.OperationMutate, Table: vswitch.InterfaceTable, Where: uuidWhere(iface.UUID), Mutations: []ovsdb.Mutation{{Column: "external_ids", Mutator: ovsdb.MutateOperationDelete, Value: keys}}})
		}
	}
	if len(ops) == 0 {
		return nil
	}
	_, err = c.transactVswitchOperations(ops)
	return err
}

func (c *VswitchClient) cniInterfaces(ifaceID string) ([]vswitch.Interface, error) {
	m, _ := ovsdb.NewOvsMap(map[string]string{"iface-id": ifaceID})
	return readVswitch[vswitch.Interface](c, vswitch.InterfaceTable, []ovsdb.Condition{{Column: "external_ids", Function: ovsdb.ConditionIncludes, Value: m}})
}

func (c *VswitchClient) CNIInterface(name string) (*vswitch.Interface, error) {
	ifaces, err := readVswitch[vswitch.Interface](c, vswitch.InterfaceTable, nameWhere(name))
	if err != nil {
		return nil, err
	}
	if len(ifaces) == 0 {
		return nil, nil
	}
	return &ifaces[0], nil
}

func (c *VswitchClient) SetCNIInterfaceMTU(name string, mtu int) error {
	iface, err := c.CNIInterface(name)
	if err != nil {
		return err
	}
	if iface == nil {
		return fmt.Errorf("OVS interface %s not found", name)
	}
	iface.MTURequest = new(mtu)
	return c.updateCNIModel(vswitch.InterfaceTable, iface.UUID, iface, &iface.MTURequest)
}

func (c *VswitchClient) CNIUserspaceDataPath() (bool, error) {
	bridges, err := readVswitch[vswitch.Bridge](c, vswitch.BridgeTable, nameWhere("br-int"))
	if err != nil {
		return false, err
	}
	return len(bridges) != 0 && bridges[0].DatapathType == "netdev", nil
}

// DeleteCNIPort also supports --with-iface semantics, and is idempotent when
// the sandbox or OVS port has already disappeared.
func (c *VswitchClient) DeleteCNIPort(name string) error {
	results, err := c.transactVswitchOperations([]ovsdb.Operation{
		selectVswitch(vswitch.PortTable, nil), selectVswitch(vswitch.InterfaceTable, nameWhere(name)), selectVswitch(vswitch.BridgeTable, nil),
	})
	if err != nil {
		return err
	}
	ports, err := decodeVswitchRows[vswitch.Port](c.Schema(), vswitch.PortTable, results[0].Rows)
	if err != nil {
		return err
	}
	ifaces, err := decodeVswitchRows[vswitch.Interface](c.Schema(), vswitch.InterfaceTable, results[1].Rows)
	if err != nil {
		return err
	}
	bridges, err := decodeVswitchRows[vswitch.Bridge](c.Schema(), vswitch.BridgeTable, results[2].Rows)
	if err != nil {
		return err
	}
	var ops []ovsdb.Operation
	for _, port := range ports {
		match := port.Name == name
		for _, iface := range ifaces {
			match = match || slices.Contains(port.Interfaces, iface.UUID)
		}
		if !match {
			continue
		}
		for _, bridge := range bridges {
			if !slices.Contains(bridge.Ports, port.UUID) {
				continue
			}
			if bridge.Name != "br-int" {
				return fmt.Errorf("CNI port %s belongs to bridge %s", name, bridge.Name)
			}
			ops = append(ops, ovsdb.Operation{Op: ovsdb.OperationMutate, Table: vswitch.BridgeTable, Where: uuidWhere(bridge.UUID), Mutations: []ovsdb.Mutation{uuidSetMutation("ports", []string{port.UUID}, ovsdb.MutateOperationDelete)}})
		}
		ops = append(ops, deleteVswitchRowOperation(vswitch.PortTable, port.UUID))
	}
	if len(ops) == 0 {
		return nil
	}
	_, err = c.transactVswitchOperations(ops)
	return err
}

// Use only the mirror columns available on all supported OVS versions.
type cniMirror struct {
	UUID          string   `ovsdb:"_uuid"`
	Name          string   `ovsdb:"name"`
	SelectDstPort []string `ovsdb:"select_dst_port"`
}

func (c *VswitchClient) ConfigureCNIMirror(global bool, open, ifaceID string) error {
	if global {
		return nil
	}
	ifaces, err := c.cniInterfaces(ifaceID)
	if err != nil {
		return err
	}
	for _, iface := range ifaces {
		ports, err := readVswitch[vswitch.Port](c, vswitch.PortTable, nameWhere(iface.Name))
		if err != nil {
			return err
		}
		if len(ports) != 1 {
			return fmt.Errorf("expected OVS port %s", iface.Name)
		}
		mirrors, err := readVswitch[cniMirror](c, vswitch.MirrorTable, nameWhere(util.MirrorDefaultName))
		if err != nil {
			return err
		}
		if len(mirrors) != 1 {
			return fmt.Errorf("expected mirror %s", util.MirrorDefaultName)
		}
		mutator := ovsdb.MutateOperationDelete
		if open == "true" {
			mutator = ovsdb.MutateOperationInsert
		}
		_, err = c.transactVswitchOperations([]ovsdb.Operation{{Op: ovsdb.OperationMutate, Table: vswitch.MirrorTable, Where: uuidWhere(mirrors[0].UUID), Mutations: []ovsdb.Mutation{uuidSetMutation("select_dst_port", []string{ports[0].UUID}, mutator)}}})
		if err != nil {
			return err
		}
	}
	return nil
}

func cniExternalIDs(podName, podNamespace, ifaceID string) map[string]string {
	ids := map[string]string{"iface-id": ifaceID}
	if podName != "" && podNamespace != "" {
		ids["pod"] = podNamespace + "/" + podName
	}
	return ids
}

func cniOwned(ids map[string]string, podName, podNamespace, ifaceID string) bool {
	if ifaceID != "" {
		return ids["iface-id"] == ifaceID
	}
	return ids["pod"] == podNamespace+"/"+podName
}
