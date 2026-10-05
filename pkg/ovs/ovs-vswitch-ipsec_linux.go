package ovs

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

// IPsecConfiguration reads the singleton synchronously, including after a
// database reset, without depending on a stale monitor cache.
func (c *VswitchClient) IPsecConfiguration() (*vswitch.OpenvSwitch, error) {
	rows, err := readVswitch[vswitch.OpenvSwitch](c, vswitch.OpenvSwitchTable, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("expected one Open_vSwitch row, found %d", len(rows))
	}
	return &rows[0], nil
}

// IPsecDatapathConfiguration reads one database snapshot and rejects paths
// that cannot provide the per-peer kernel transport encryption used here.
// This is preflight validation, not a dataplane protection or SA readiness proof.
func (c *VswitchClient) IPsecDatapathConfiguration() (*vswitch.OpenvSwitch, error) {
	ops := []ovsdb.Operation{
		selectVswitch(vswitch.OpenvSwitchTable, nil),
		selectVswitch(vswitch.BridgeTable, nil),
		selectVswitch(vswitch.PortTable, nil),
	}
	ops[1].Columns = []string{"name", "datapath_type"}
	ops[2].Columns = []string{"name", "external_ids"}
	results, err := c.transactVswitchOperations(ops)
	if err != nil {
		return nil, err
	}
	rows, err := decodeVswitchRows[vswitch.OpenvSwitch](c.Schema(), vswitch.OpenvSwitchTable, results[0].Rows)
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("expected one Open_vSwitch row, found %d", len(rows))
	}
	bridges, err := decodeVswitchRows[vswitch.Bridge](c.Schema(), vswitch.BridgeTable, results[1].Rows)
	if err != nil {
		return nil, err
	}
	ports, err := decodeVswitchRows[vswitch.Port](c.Schema(), vswitch.PortTable, results[2].Rows)
	if err != nil {
		return nil, err
	}
	if err := validateIPsecDatapath(&rows[0], bridges, ports); err != nil {
		return nil, err
	}
	return &rows[0], nil
}

func validateIPsecDatapath(row *vswitch.OpenvSwitch, bridges []vswitch.Bridge, ports []vswitch.Port) error {
	if value := row.OtherConfig["hw-offload"]; value != "" && value != "false" {
		return errors.New("IPsec does not support hardware offload")
	}
	// Match OVN's chassis-specific option precedence, including explicit false.
	option := func(key, fallback string) string {
		if value, ok := row.ExternalIDs[key+"-"+row.ExternalIDs["system-id"]]; ok {
			return value
		}
		if value, ok := row.ExternalIDs[key]; ok {
			return value
		}
		return fallback
	}
	if value := option("ovn-enable-flow-based-tunnels", "false"); value != "false" {
		return errors.New("IPsec does not support flow-based tunnels")
	}
	if option("ovn-evpn-vxlan-ports", "") != "" {
		return errors.New("IPsec does not support EVPN tunnels")
	}
	for encap := range strings.SplitSeq(option("ovn-encap-type", "geneve"), ",") {
		if encap != "geneve" && encap != "vxlan" {
			return errors.New("IPsec supports only Geneve and VXLAN kernel tunnels")
		}
	}
	bridgeName := option("ovn-bridge", "br-int")
	for _, bridge := range bridges {
		if bridge.Name == bridgeName && bridge.DatapathType != "" && bridge.DatapathType != "system" {
			return errors.New("IPsec requires the OVN integration bridge to use the system datapath")
		}
	}
	// Leftover ports still count even after their feature switch was cleared.
	for _, port := range ports {
		if port.ExternalIDs["ovn-chassis-id"] == "flow" || port.ExternalIDs["ovn-evpn-tunnel"] == "true" {
			return errors.New("IPsec requires existing OVN flow-based/EVPN tunnel ports to be removed")
		}
	}
	return nil
}

// SetIPsecConfiguration replaces only the three IPsec paths in one transaction.
// Other node modules retain ownership of the rest of other_config.
func (c *VswitchClient) SetIPsecConfiguration(uuid string, paths map[string]string) error {
	op := cniMapPatch(vswitch.OpenvSwitchTable, uuid, "other_config", paths, []string{"certificate", "private_key", "ca_cert"})
	results, err := c.transactVswitchOperations([]ovsdb.Operation{op})
	if err != nil {
		return err
	}
	if results[0].Count != 1 {
		return fmt.Errorf("IPsec configuration changed %d Open_vSwitch rows", results[0].Count)
	}
	return nil
}
