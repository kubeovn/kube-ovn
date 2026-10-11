package ovs

import (
	"maps"
	"os"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

func protectionFixture(t *testing.T) (*VswitchClient, IPsecProtection, *vswitch.Interface) {
	t.Helper()
	c := newTestCNIVswitchClient(t)
	root, err := c.IPsecConfiguration()
	require.NoError(t, err)
	require.NoError(t, c.patchCNIMap(vswitch.OpenvSwitchTable, root.UUID, "external_ids", map[string]string{"system-id": "chassis", "hostname": "node"}, nil))
	iface := addTestCNIPort(t, c, "ovn-peer", "")
	iface.Type = "geneve"
	iface.Options = map[string]string{"remote_ip": "192.0.2.2", "remote_name": "peer", "key": "flow", "csum": "true"}
	require.NoError(t, c.updateCNIModel(vswitch.InterfaceTable, iface.UUID, iface, &iface.Type, &iface.Options))
	ports, err := readVswitch[vswitch.Port](c, vswitch.PortTable, nameWhere(iface.Name))
	require.NoError(t, err)
	require.NoError(t, c.patchCNIMap(vswitch.PortTable, ports[0].UUID, "external_ids", map[string]string{"ovn-chassis-id": "peer"}, nil))
	return c, IPsecProtection{NodeUID: "node-uid", Chassis: "chassis", Lease: "lease", Mark: 759815, Reqid: 759815}, iface
}

func TestIPsecProtectionMarksOnlyOwnedTunnels(t *testing.T) {
	c, lease, owned := protectionFixture(t)
	foreign := addTestCNIPort(t, c, "foreign-ipsec", "foreign")
	foreign.Type = "geneve"
	foreign.Options = map[string]string{"remote_ip": "192.0.2.3", "remote_name": "foreign", "egress_pkt_mark": "99", "ipsec_reqid": "99"}
	require.NoError(t, c.updateCNIModel(vswitch.InterfaceTable, foreign.UUID, foreign, &foreign.Type, &foreign.Options))
	require.Error(t, c.VerifyIPsecProtection(lease))
	require.NoError(t, c.PublishIPsecProtection(lease))
	require.NoError(t, c.VerifyIPsecProtection(lease))
	stale := lease
	stale.OVSUUID = "00000000-0000-0000-0000-000000000000"
	require.Error(t, c.VerifyIPsecProtection(stale), "startup readback must bind the current database UUID")
	got, err := c.CNIInterface(owned.Name)
	require.NoError(t, err)
	for key, value := range owned.Options {
		require.Equal(t, value, got.Options[key], "existing tunnel configuration must be preserved")
	}
	require.Equal(t, "759815", got.Options["egress_pkt_mark"])
	require.Equal(t, "759815/0xffffffff", got.Options["ipsec_mark_out"])
	require.Equal(t, "759815", got.Options["ipsec_reqid"])
	untouched, err := c.CNIInterface(foreign.Name)
	require.NoError(t, err)
	require.Equal(t, foreign.Options, untouched.Options)
	root, err := c.IPsecConfiguration()
	require.NoError(t, err)
	require.Equal(t, "node", root.ExternalIDs["hostname"])
	require.NoError(t, c.PublishIPsecProtection(lease), "repeated publish must be idempotent")
	// A normal SB disable must retain the output mark without reintroducing
	// IKE options once OVN has removed the peer identity.
	require.NoError(t, c.patchCNIMap(vswitch.InterfaceTable, owned.UUID, "options", nil, []string{"remote_name", "ipsec_mark_out", "ipsec_reqid"}))
	require.NoError(t, c.PublishIPsecProtection(lease))
	got, err = c.CNIInterface(owned.Name)
	require.NoError(t, err)
	require.Equal(t, "759815", got.Options["egress_pkt_mark"])
	require.NotContains(t, got.Options, "ipsec_reqid")
}

func TestIPsecProtectionRejectsConflictsWithoutChangingDatabase(t *testing.T) {
	c, lease, iface := protectionFixture(t)
	for _, scenario := range []struct {
		name    string
		options map[string]string
		ids     map[string]string
	}{
		{name: "mark-conflict", options: map[string]string{"egress_pkt_mark": "99"}},
		{name: "reqid-conflict", options: map[string]string{"ipsec_reqid": "99"}},
		{name: "mark-out-conflict", options: map[string]string{"ipsec_mark_out": "99/0xffffffff"}},
		{name: "custom-udp", options: map[string]string{"dst_port": "1234"}},
		{name: "node-replaced", ids: map[string]string{"ovn-ipsec-protection-node-uid": "replacement"}},
		{name: "other-lease", ids: map[string]string{"ovn-ipsec-protection-lease": "foreign"}},
		{name: "chassis-replaced", ids: map[string]string{"system-id": "replacement"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root, err := c.IPsecConfiguration()
			require.NoError(t, err)
			root.ExternalIDs = map[string]string{"system-id": lease.Chassis}
			maps.Copy(root.ExternalIDs, scenario.ids)
			require.NoError(t, c.updateCNIModel(vswitch.OpenvSwitchTable, root.UUID, root, &root.ExternalIDs))
			iface.Options = map[string]string{"remote_name": "peer", "remote_ip": "192.0.2.2"}
			maps.Copy(iface.Options, scenario.options)
			require.NoError(t, c.updateCNIModel(vswitch.InterfaceTable, iface.UUID, iface, &iface.Options))
			require.Error(t, c.PublishIPsecProtection(lease))
			after, err := c.IPsecConfiguration()
			require.NoError(t, err)
			require.Equal(t, root.ExternalIDs, after.ExternalIDs)
			got, err := c.CNIInterface(iface.Name)
			require.NoError(t, err)
			require.Equal(t, iface.Options, got.Options)
		})
	}
}

func TestIPsecProtectionRejectsStaleTunnelSnapshot(t *testing.T) {
	c, lease, _ := protectionFixture(t)
	snapshot, err := c.ipsecProtectionSnapshot()
	require.NoError(t, err)
	addTestCNIPort(t, c, "concurrent-port", "pod")
	ops := append(snapshot.guards, cniMapPatch(vswitch.OpenvSwitchTable, snapshot.root.UUID, "external_ids", lease.externalIDs(), nil))
	_, err = c.transactVswitchOperations(ops)
	require.Error(t, err, "a new interface between inventory and publish must abort the transaction")
	root, err := c.IPsecConfiguration()
	require.NoError(t, err)
	require.NotContains(t, root.ExternalIDs, "ovn-ipsec-protection-mark")
	// A later retry must use a new snapshot, preserving the concurrent port.
	require.NoError(t, c.PublishIPsecProtection(lease))
	got, err := readVswitch[vswitch.Port](c, vswitch.PortTable, nameWhere("concurrent-port"))
	require.NoError(t, err)
	require.Len(t, got, 1)
	// OVSDB reset must invalidate a receipt, never synthesize one from a cache.
	_, err = c.transactVswitchOperations([]ovsdb.Operation{cniMapPatch(vswitch.OpenvSwitchTable, root.UUID, "external_ids", nil, []string{"ovn-ipsec-protection-mark"})})
	require.NoError(t, err)
	require.Error(t, c.VerifyIPsecProtection(lease))
}

func TestIPsecProtectionRejectsEmptyBridgeSnapshot(t *testing.T) {
	if os.Getenv("OVSDB_CNI_TEST_BIN_DIR") == "" {
		t.Skip("requires the real OVSDB server; libovsdb Wait ignores default-valued columns")
	}
	c := newTestCNIVswitchClient(t)
	root, err := c.IPsecConfiguration()
	require.NoError(t, err)
	require.NoError(t, c.patchCNIMap(vswitch.OpenvSwitchTable, root.UUID, "external_ids", map[string]string{"system-id": "chassis"}, nil))
	lease := IPsecProtection{NodeUID: "node", Chassis: "chassis", Lease: "lease", Mark: 759815, Reqid: 759815}
	snapshot, err := c.ipsecProtectionSnapshot()
	require.NoError(t, err)
	addTestCNIPort(t, c, "first-concurrent-port", "pod")
	_, err = c.transactVswitchOperations(append(snapshot.guards, cniMapPatch(vswitch.OpenvSwitchTable, root.UUID, "external_ids", lease.externalIDs(), nil)))
	require.Error(t, err, "attaching the first port to an empty bridge must invalidate the snapshot")
	root, err = c.IPsecConfiguration()
	require.NoError(t, err)
	require.NotContains(t, root.ExternalIDs, "ovn-ipsec-protection-mark")
	require.NoError(t, c.PublishIPsecProtection(lease))
}

func TestIPsecDisableTunnelConvergence(t *testing.T) {
	// The in-process server derives its Unix socket from t.Name. Create it at
	// the parent test: subtest names contain slashes and are not socket paths.
	c, lease, owned := protectionFixture(t)
	foreign := addTestCNIPort(t, c, "foreign-encrypted", "foreign")
	for _, tunnel := range []string{"geneve", "vxlan"} {
		t.Run(tunnel, func(t *testing.T) {
			owned.Type = tunnel
			owned.Options = map[string]string{"remote_ip": "192.0.2.2", "remote_name": "peer", "key": "flow", "csum": "true"}
			require.NoError(t, c.updateCNIModel(vswitch.InterfaceTable, owned.UUID, owned, &owned.Type, &owned.Options))
			foreign.Type, foreign.Options = tunnel, map[string]string{"remote_name": "foreign-peer", "remote_ip": "198.51.100.2", "ipsec_reqid": "99", "ipsec_mark_out": "99/0xffffffff"}
			require.NoError(t, c.updateCNIModel(vswitch.InterfaceTable, foreign.UUID, foreign, &foreign.Type, &foreign.Options))
			require.NoError(t, c.PublishIPsecProtection(lease))
			require.ErrorContains(t, c.VerifyIPsecTunnelQuiescence(lease), "local OVN tunnel convergence")
			// Observe OVN's updates independently: removing remote_name alone
			// must not hide stale transport options during asynchronous rollout.
			for _, key := range []string{"remote_name", "ipsec_reqid", "ipsec_mark_out"} {
				require.NoError(t, c.patchCNIMap(vswitch.InterfaceTable, owned.UUID, "options", nil, []string{key}))
				if key != "ipsec_mark_out" {
					require.ErrorContains(t, c.VerifyIPsecTunnelQuiescence(lease), "local OVN tunnel convergence")
				}
			}
			require.NoError(t, c.VerifyIPsecTunnelQuiescence(lease))
			require.NoError(t, c.VerifyIPsecProtection(lease), "local convergence must preserve the output mark and public lease")
			actual, err := c.CNIInterface(foreign.Name)
			require.NoError(t, err)
			require.Equal(t, foreign.Options, actual.Options, "foreign encrypted interfaces must be preserved")
			stale := lease
			stale.OVSUUID = "75980000-0000-0000-0000-000000000004"
			require.Error(t, c.VerifyIPsecTunnelQuiescence(stale))
			// A new peer appearing after a successful observation invalidates
			// the next preflight; a cached result cannot complete node cleanup.
			require.NoError(t, c.patchCNIMap(vswitch.InterfaceTable, owned.UUID, "options", map[string]string{"remote_name": "peer"}, nil))
			require.Error(t, c.VerifyIPsecTunnelQuiescence(lease))
		})
	}
}

func TestIPsecCleanupIdentityPreservesProtectionAndForeignFields(t *testing.T) {
	c, lease, iface := protectionFixture(t)
	require.NoError(t, c.PublishIPsecProtection(lease))
	paths := map[string]string{"certificate": "owned-cert", "private_key": "owned-key", "ca_cert": "owned-ca"}
	row, err := c.IPsecConfiguration()
	require.NoError(t, err)
	lease.OVSUUID = row.UUID
	require.NoError(t, c.SetIPsecConfiguration(row.UUID, paths))
	require.NoError(t, c.patchCNIMap(vswitch.OpenvSwitchTable, row.UUID, "other_config", map[string]string{"other-module": "preserve"}, nil))
	require.Error(t, c.ClearIPsecIdentity(lease, paths), "a live encrypted peer must block removal")
	require.NoError(t, c.patchCNIMap(vswitch.InterfaceTable, iface.UUID, "options", nil, []string{"remote_name", "ipsec_mark_out", "ipsec_reqid"}))
	wrong := lease
	wrong.Lease = "replacement"
	require.Error(t, c.ClearIPsecIdentity(wrong, paths))
	wrong = lease
	wrong.OVSUUID = "75980000-0000-0000-0000-000000000099"
	require.Error(t, c.ClearIPsecIdentity(wrong, paths))
	require.NoError(t, c.patchCNIMap(vswitch.OpenvSwitchTable, row.UUID, "other_config", map[string]string{"certificate": "foreign"}, []string{"certificate"}))
	require.ErrorContains(t, c.ClearIPsecIdentity(lease, paths), "identity changed")
	got, err := c.IPsecConfiguration()
	require.NoError(t, err)
	require.Equal(t, "foreign", got.OtherConfig["certificate"])
	require.NoError(t, c.SetIPsecConfiguration(row.UUID, paths))
	// An intervening writer must invalidate the same transaction snapshot that
	// protects tunnels; otherwise old evidence could erase a replacement path.
	snapshot, err := c.ipsecProtectionSnapshot()
	require.NoError(t, err)
	require.NoError(t, c.patchCNIMap(vswitch.OpenvSwitchTable, row.UUID, "other_config", map[string]string{"certificate": "concurrent"}, []string{"certificate"}))
	_, err = c.transactVswitchOperations(append(snapshot.guards, cniMapPatch(vswitch.OpenvSwitchTable, row.UUID, "other_config", nil, []string{"certificate", "private_key", "ca_cert"})))
	require.Error(t, err)
	got, err = c.IPsecConfiguration()
	require.NoError(t, err)
	require.Equal(t, "concurrent", got.OtherConfig["certificate"])
	require.NoError(t, c.SetIPsecConfiguration(row.UUID, paths))
	require.NoError(t, c.ClearIPsecIdentity(lease, paths))
	require.NoError(t, c.ClearIPsecIdentity(lease, nil), "recovery after commit must be idempotent")
	require.NoError(t, c.VerifyIPsecProtection(lease))
	got, err = c.IPsecConfiguration()
	require.NoError(t, err)
	require.Equal(t, "preserve", got.OtherConfig["other-module"])
	for key := range paths {
		require.NotContains(t, got.OtherConfig, key)
	}
	tunnel, err := c.CNIInterface(iface.Name)
	require.NoError(t, err)
	require.Equal(t, "759815", tunnel.Options["egress_pkt_mark"], "identity removal cannot withdraw protection")
}

func TestIPsecProtectionWithdrawalNeedsExactQuiescentLease(t *testing.T) {
	c, lease, iface := protectionFixture(t)
	foreign := addTestCNIPort(t, c, "foreign-protected", "foreign")
	foreign.Type, foreign.Options = "geneve", map[string]string{"remote_name": "foreign-peer", "egress_pkt_mark": "99", "ipsec_reqid": "99"}
	require.NoError(t, c.updateCNIModel(vswitch.InterfaceTable, foreign.UUID, foreign, &foreign.Type, &foreign.Options))
	require.NoError(t, c.PublishIPsecProtection(lease))
	row, err := c.IPsecConfiguration()
	require.NoError(t, err)
	lease.OVSUUID = row.UUID
	require.Error(t, c.WithdrawIPsecProtection(lease), "encrypted tunnels must block lease withdrawal")
	require.NoError(t, c.patchCNIMap(vswitch.InterfaceTable, iface.UUID, "options", nil, []string{"remote_name", "ipsec_mark_out", "ipsec_reqid"}))
	require.NoError(t, c.SetIPsecConfiguration(row.UUID, map[string]string{"certificate": "pending-owner-cleanup"}))
	require.ErrorContains(t, c.WithdrawIPsecProtection(lease), "identity cleanup")
	require.NoError(t, c.SetIPsecConfiguration(row.UUID, nil))
	wrong := lease
	wrong.Lease = "foreign"
	require.Error(t, c.WithdrawIPsecProtection(wrong))
	wrong = lease
	wrong.OVSUUID = "75980000-0000-0000-0000-000000000099"
	require.Error(t, c.WithdrawIPsecProtection(wrong))
	require.NoError(t, c.WithdrawIPsecProtection(lease))
	require.NoError(t, c.WithdrawIPsecProtection(lease), "only complete withdrawal is idempotent")
	got, err := c.IPsecConfiguration()
	require.NoError(t, err)
	require.Equal(t, lease.Chassis, got.ExternalIDs["system-id"])
	require.Equal(t, "node", got.ExternalIDs["hostname"])
	for key := range lease.externalIDs() {
		require.NotContains(t, got.ExternalIDs, key)
	}
	tunnel, err := c.CNIInterface(iface.Name)
	require.NoError(t, err)
	require.NotContains(t, tunnel.Options, "egress_pkt_mark")
	untouched, err := c.CNIInterface(foreign.Name)
	require.NoError(t, err)
	require.Equal(t, foreign.Options, untouched.Options)
	// A missing lease with any leftover protected output is an interruption,
	// never completion; restore/resolve it under the separate release journal.
	require.NoError(t, c.patchCNIMap(vswitch.InterfaceTable, iface.UUID, "options", map[string]string{"egress_pkt_mark": "759815"}, nil))
	require.Error(t, c.WithdrawIPsecProtection(lease))
	// Restore the public lease, then replace one field to emulate a foreign
	// writer. Withdrawal must leave that writer's value unchanged.
	require.NoError(t, c.PublishIPsecProtection(lease))
	require.NoError(t, c.patchCNIMap(vswitch.OpenvSwitchTable, row.UUID, "external_ids", map[string]string{"ovn-ipsec-protection-lease": "foreign"}, []string{"ovn-ipsec-protection-lease"}))
	require.Error(t, c.WithdrawIPsecProtection(lease))
	got, err = c.IPsecConfiguration()
	require.NoError(t, err)
	require.Equal(t, "foreign", got.ExternalIDs["ovn-ipsec-protection-lease"])
}
