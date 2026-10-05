package ovs

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

// IPsecProtection is the public portion of a node-local protection lease.
// Publishing it requires the caller to have armed and read back both guards.
// It does not confer ownership of any existing SA.
type IPsecProtection struct {
	NodeUID, Lease, Chassis string
	OVSUUID                 string
	Mark, Reqid             uint32
}

func (p IPsecProtection) externalIDs() map[string]string {
	return map[string]string{
		"ovn-ipsec-protection-node-uid": p.NodeUID,
		"ovn-ipsec-protection-lease":    p.Lease,
		"ovn-ipsec-protection-mark":     strconv.FormatUint(uint64(p.Mark), 10),
		"ovn-ipsec-protection-reqid":    strconv.FormatUint(uint64(p.Reqid), 10),
	}
}

type ipsecProtectionSnapshot struct {
	root       vswitch.OpenvSwitch
	ports      []vswitch.Port
	interfaces []vswitch.Interface
	guards     []ovsdb.Operation
}

func (c *VswitchClient) ipsecProtectionSnapshot() (*ipsecProtectionSnapshot, error) {
	tables := []string{vswitch.OpenvSwitchTable, vswitch.PortTable, vswitch.InterfaceTable, vswitch.BridgeTable}
	columns := [][]string{{"_uuid", "external_ids", "other_config", "bridges"}, {"_uuid", "interfaces", "external_ids"}, {"_uuid", "type", "options"}, {"_uuid", "ports"}}
	ops := make([]ovsdb.Operation, len(tables))
	for i, table := range tables {
		ops[i] = selectVswitch(table, nil)
		ops[i].Columns = columns[i]
	}
	results, err := c.transactVswitchOperations(ops)
	if err != nil {
		return nil, err
	}
	roots, err := decodeVswitchRows[vswitch.OpenvSwitch](c.Schema(), tables[0], results[0].Rows)
	if err != nil {
		return nil, err
	}
	if len(roots) != 1 {
		return nil, fmt.Errorf("expected one Open_vSwitch row, found %d", len(roots))
	}
	ports, err := decodeVswitchRows[vswitch.Port](c.Schema(), tables[1], results[1].Rows)
	if err != nil {
		return nil, err
	}
	interfaces, err := decodeVswitchRows[vswitch.Interface](c.Schema(), tables[2], results[2].Rows)
	if err != nil {
		return nil, err
	}
	snapshot := &ipsecProtectionSnapshot{root: roots[0], ports: ports, interfaces: interfaces}
	// Per-row comparisons work with both OVS and libovsdb servers. Guard bridge
	// membership too: a concurrently attached tunnel changes its bridge's ports,
	// or the root's bridges, and cannot miss initial output marking.
	for i, table := range tables {
		for _, row := range results[i].Rows {
			snapshot.guards = append(snapshot.guards, ovsdb.Operation{
				Op: ovsdb.OperationWait, Table: table,
				Where:   []ovsdb.Condition{{Column: "_uuid", Function: ovsdb.ConditionEqual, Value: row["_uuid"]}},
				Columns: columns[i], Until: string(ovsdb.WaitConditionEqual), Rows: []ovsdb.Row{row}, Timeout: new(0),
			})
		}
	}
	return snapshot, nil
}

func (s *ipsecProtectionSnapshot) tunnelOptions(p IPsecProtection, requireApplied bool) (map[string]map[string]string, error) {
	if p.NodeUID == "" || p.Lease == "" || p.Chassis == "" || p.Mark == 0 || p.Reqid == 0 || p.Reqid > 1<<31-1 || s.root.ExternalIDs["system-id"] != p.Chassis || p.OVSUUID != "" && s.root.UUID != p.OVSUUID {
		return nil, errors.New("invalid IPsec protection lease or changed OVS chassis")
	}
	for key, value := range p.externalIDs() {
		actual := s.root.ExternalIDs[key]
		if actual != value && (requireApplied || actual != "") {
			return nil, errors.New("OVS IPsec protection lease is missing or conflicts with the node owner")
		}
	}
	interfaces := make(map[string]vswitch.Interface, len(s.interfaces))
	for _, iface := range s.interfaces {
		interfaces[iface.UUID] = iface
	}
	options := make(map[string]map[string]string)
	for _, port := range s.ports {
		peer := port.ExternalIDs["ovn-chassis-id"]
		if peer == "" && port.ExternalIDs["ovn-evpn-tunnel"] != "true" {
			continue
		}
		if peer == "flow" || port.ExternalIDs["ovn-evpn-tunnel"] == "true" || len(port.Interfaces) != 1 {
			return nil, errors.New("IPsec protection found an unsupported OVN tunnel port")
		}
		iface, ok := interfaces[port.Interfaces[0]]
		if !ok || iface.Type != "geneve" && iface.Type != "vxlan" || iface.Options["remote_ip"] == "flow" {
			return nil, errors.New("IPsec protection requires ordinary Geneve/VXLAN OVN tunnel interfaces")
		}
		if value := iface.Options["dst_port"]; value != "" && (iface.Type == "geneve" && value != "6081" || iface.Type == "vxlan" && value != "4789") {
			return nil, errors.New("IPsec protection does not support custom tunnel UDP ports")
		}
		values := map[string]string{"egress_pkt_mark": strconv.FormatUint(uint64(p.Mark), 10)}
		if iface.Options["remote_name"] != "" {
			values["ipsec_mark_out"] = values["egress_pkt_mark"] + "/0xffffffff"
			values["ipsec_reqid"] = strconv.FormatUint(uint64(p.Reqid), 10)
		}
		for key, value := range values {
			actual := iface.Options[key]
			if actual != value && (requireApplied || actual != "") {
				return nil, errors.New("OVN tunnel output protection is missing or conflicts with the node owner")
			}
		}
		options[iface.UUID] = values
	}
	return options, nil
}

// PublishIPsecProtection atomically publishes a lease and marks existing OVN
// tunnels. Future tunnels use the production ovn-controller extension. Only
// protection options are patched; unrelated maps and interfaces are preserved.
func (c *VswitchClient) PublishIPsecProtection(p IPsecProtection) error {
	snapshot, err := c.ipsecProtectionSnapshot()
	if err != nil {
		return err
	}
	options, err := snapshot.tunnelOptions(p, false)
	if err != nil {
		return err
	}
	ops := snapshot.guards
	ops = append(ops, cniMapPatch(vswitch.OpenvSwitchTable, snapshot.root.UUID, "external_ids", p.externalIDs(), []string{
		"ovn-ipsec-protection-node-uid", "ovn-ipsec-protection-lease", "ovn-ipsec-protection-mark", "ovn-ipsec-protection-reqid",
	}))
	for uuid, values := range options {
		keys := []string{"egress_pkt_mark"}
		if values["ipsec_reqid"] != "" {
			keys = append(keys, "ipsec_mark_out", "ipsec_reqid")
		}
		ops = append(ops, cniMapPatch(vswitch.InterfaceTable, uuid, "options", values, keys))
	}
	_, err = c.transactVswitchOperations(ops)
	return err
}

// VerifyIPsecProtection reads live OVSDB state instead of a prior receipt.
func (c *VswitchClient) VerifyIPsecProtection(p IPsecProtection) error {
	snapshot, err := c.ipsecProtectionSnapshot()
	if err != nil {
		return err
	}
	_, err = snapshot.tunnelOptions(p, true)
	return err
}

// VerifyIPsecTunnelQuiescence confirms the local part of a coordinated disable
// while the output lease is still published. Global NB/SB false alone cannot
// prove that ovn-controller has removed every local encrypted peer. This is a
// read-only preflight, not authorization to withdraw the lease or kernel guards.
func (c *VswitchClient) VerifyIPsecTunnelQuiescence(p IPsecProtection) error {
	snapshot, err := c.ipsecProtectionSnapshot()
	if err != nil {
		return err
	}
	return snapshot.verifyTunnelQuiescence(p)
}

func (s *ipsecProtectionSnapshot) verifyTunnelQuiescence(p IPsecProtection) error {
	owned, err := s.tunnelOptions(p, true)
	if err != nil {
		return err
	}
	for _, iface := range s.interfaces {
		if _, ok := owned[iface.UUID]; !ok {
			continue
		}
		for _, key := range []string{"remote_name", "ipsec_mark_out", "ipsec_reqid"} {
			if iface.Options[key] != "" {
				return errors.New("IPsec disable is waiting for local OVN tunnel convergence")
			}
		}
	}
	return nil
}

// ClearIPsecIdentity removes exactly one proven identity triple while retaining
// the output lease and guards. A changed database, tunnel or path aborts the
// transaction. Callers must stop their runtime and establish live kernel drain
// first; an identity path alone does not authorize disable or guard withdrawal.
func (c *VswitchClient) ClearIPsecIdentity(p IPsecProtection, paths map[string]string) error {
	if !ovsdb.IsValidUUID(p.OVSUUID) {
		return errors.New("IPsec cleanup requires the current OVS database identity")
	}
	snapshot, err := c.ipsecProtectionSnapshot()
	if err != nil {
		return err
	}
	if err := snapshot.verifyTunnelQuiescence(p); err != nil {
		return err
	}
	keys := []string{"certificate", "private_key", "ca_cert"}
	populated := 0
	for _, key := range keys {
		if paths[key] != "" {
			populated++
		}
		if snapshot.root.OtherConfig[key] != paths[key] {
			return errors.New("IPsec cleanup identity changed before its guarded removal")
		}
	}
	if populated == 0 {
		return nil // Repeat after an already committed removal.
	}
	if populated != len(keys) {
		return errors.New("IPsec cleanup requires a complete identity triple")
	}
	ops := append(snapshot.guards, cniMapPatch(vswitch.OpenvSwitchTable, snapshot.root.UUID, "other_config", nil, keys))
	_, err = c.transactVswitchOperations(ops)
	return err
}

// WithdrawIPsecProtection is the final database step of an explicitly
// authorized disable. It is not called by the guarded Cleanup preflight.
// The caller must retain kernel guards and the durable startup requirement
// until its release protocol confirms completion. This transaction removes
// only the current owner's lease and output marks; all other resources remain.
func (c *VswitchClient) WithdrawIPsecProtection(p IPsecProtection) error {
	if !ovsdb.IsValidUUID(p.OVSUUID) {
		return errors.New("IPsec protection withdrawal requires the current OVS identity")
	}
	snapshot, err := c.ipsecProtectionSnapshot()
	if err != nil {
		return err
	}
	for _, key := range []string{"certificate", "private_key", "ca_cert"} {
		if snapshot.root.OtherConfig[key] != "" {
			return errors.New("IPsec protection withdrawal is waiting for identity cleanup")
		}
	}
	missing := true
	for key := range p.externalIDs() {
		missing = missing && snapshot.root.ExternalIDs[key] == ""
	}
	if missing {
		// Verify the same current chassis/database and every owned tunnel on
		// recovery too; an absent lease alone is not evidence of completion.
		owned, err := snapshot.tunnelOptions(p, false)
		if err != nil {
			return err
		}
		for _, iface := range snapshot.interfaces {
			if _, ok := owned[iface.UUID]; !ok {
				continue
			}
			for _, key := range []string{"remote_name", "ipsec_mark_out", "ipsec_reqid", "egress_pkt_mark"} {
				if iface.Options[key] != "" {
					return errors.New("IPsec protection withdrawal has incomplete tunnel evidence")
				}
			}
		}
		return nil
	}
	for key, value := range p.externalIDs() {
		if snapshot.root.ExternalIDs[key] != value {
			return errors.New("IPsec protection withdrawal lease changed before its guarded removal")
		}
	}
	if err := snapshot.verifyTunnelQuiescence(p); err != nil {
		return err
	}
	owned, err := snapshot.tunnelOptions(p, true)
	if err != nil {
		return err
	}
	ops := append(snapshot.guards, cniMapPatch(vswitch.OpenvSwitchTable, snapshot.root.UUID, "external_ids", nil, []string{
		"ovn-ipsec-protection-node-uid", "ovn-ipsec-protection-lease", "ovn-ipsec-protection-mark", "ovn-ipsec-protection-reqid",
	}))
	for id := range owned {
		ops = append(ops, cniMapPatch(vswitch.InterfaceTable, id, "options", nil, []string{"egress_pkt_mark"}))
	}
	_, err = c.transactVswitchOperations(ops)
	return err
}
