package ovs

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

func TestIPsecConfigurationPreservesOtherModules(t *testing.T) {
	c := newTestCNIVswitchClient(t)
	row, err := c.IPsecConfiguration()
	require.NoError(t, err)
	require.NoError(t, c.patchCNIMap(vswitch.OpenvSwitchTable, row.UUID, "other_config", map[string]string{"hw-offload": "true", "certificate": "old-cert", "private_key": "old-key", "ca_cert": "old-ca"}, nil))
	paths := map[string]string{"certificate": "new-cert", "private_key": "new-key", "ca_cert": "new-ca"}
	require.NoError(t, c.SetIPsecConfiguration(row.UUID, paths))
	got, err := c.IPsecConfiguration()
	require.NoError(t, err)
	require.Equal(t, "true", got.OtherConfig["hw-offload"])
	for key, value := range paths {
		require.Equal(t, value, got.OtherConfig[key])
	}
	c.Disconnect()
	require.Eventually(t, c.Connected, 3e9, 1e7)
	require.NoError(t, c.SetIPsecConfiguration(row.UUID, paths))
	require.Error(t, c.SetIPsecConfiguration("00000000-0000-0000-0000-000000000000", paths))
}

func TestIPsecDatapathPreflightUsesCurrentDatabase(t *testing.T) {
	c := newTestCNIVswitchClient(t)
	row, err := c.IPsecConfiguration()
	require.NoError(t, err)
	bridges, err := readVswitch[vswitch.Bridge](c, vswitch.BridgeTable, nameWhere("br-int"))
	require.NoError(t, err)
	addTestCNIPort(t, c, "fixture-port", "fixture.ns")
	ports, err := readVswitch[vswitch.Port](c, vswitch.PortTable, nameWhere("fixture-port"))
	require.NoError(t, err)
	paths := map[string]string{"certificate": "existing-cert", "private_key": "existing-key", "ca_cert": "existing-trust"}
	for _, scenario := range []struct {
		name         string
		externalIDs  map[string]string
		otherConfig  map[string]string
		portIDs      map[string]string
		datapathType string
		allowed      bool
	}{
		{name: "default-system", allowed: true},
		{name: "explicit-system", datapathType: "system", allowed: true},
		{name: "dual-encap", externalIDs: map[string]string{"ovn-encap-type": "geneve,vxlan"}, allowed: true},
		{name: "userspace-integration", datapathType: "netdev"},
		{name: "offload", otherConfig: map[string]string{"hw-offload": "true"}},
		{name: "unknown-offload", otherConfig: map[string]string{"hw-offload": "unknown"}},
		{name: "flow-based", externalIDs: map[string]string{"ovn-enable-flow-based-tunnels": "true"}},
		{name: "chassis-flow-based", externalIDs: map[string]string{"ovn-enable-flow-based-tunnels-chassis": "true"}},
		{name: "chassis-override-disables-flow", externalIDs: map[string]string{"ovn-enable-flow-based-tunnels": "true", "ovn-enable-flow-based-tunnels-chassis": "false"}, allowed: true},
		{name: "other-chassis-flow", externalIDs: map[string]string{"ovn-enable-flow-based-tunnels-other": "true"}, allowed: true},
		{name: "evpn", externalIDs: map[string]string{"ovn-evpn-vxlan-ports": "4789"}},
		{name: "chassis-evpn", externalIDs: map[string]string{"ovn-evpn-vxlan-ports-chassis": "4789"}},
		{name: "other-chassis-evpn", externalIDs: map[string]string{"ovn-evpn-vxlan-ports-other": "4789"}, allowed: true},
		{name: "unsupported-encap", externalIDs: map[string]string{"ovn-encap-type": "stt"}},
		{name: "leftover-flow-port", portIDs: map[string]string{"ovn-chassis-id": "flow"}},
		{name: "leftover-evpn-port", portIDs: map[string]string{"ovn-evpn-tunnel": "true"}},
		{name: "ordinary-ovn-port", portIDs: map[string]string{"ovn-chassis-id": "peer-chassis"}, allowed: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			row.ExternalIDs = map[string]string{"system-id": "chassis"}
			maps.Copy(row.ExternalIDs, scenario.externalIDs)
			row.OtherConfig = maps.Clone(paths)
			maps.Copy(row.OtherConfig, scenario.otherConfig)
			require.NoError(t, c.updateCNIModel(vswitch.OpenvSwitchTable, row.UUID, row, &row.ExternalIDs, &row.OtherConfig))
			bridge := bridges[0]
			bridge.DatapathType = scenario.datapathType
			require.NoError(t, c.updateCNIModel(vswitch.BridgeTable, bridge.UUID, &bridge, &bridge.DatapathType))
			require.NoError(t, c.patchCNIMap(vswitch.PortTable, ports[0].UUID, "external_ids", scenario.portIDs, []string{"ovn-chassis-id", "ovn-evpn-tunnel"}))
			got, err := c.IPsecDatapathConfiguration()
			if scenario.allowed {
				require.NoError(t, err)
				require.Equal(t, row.UUID, got.UUID)
			} else {
				require.Error(t, err)
				require.Nil(t, got)
			}
			unchanged, err := c.IPsecConfiguration()
			require.NoError(t, err)
			for key, value := range paths {
				require.Equal(t, value, unchanged.OtherConfig[key], "preflight must not change active identity paths")
			}
		})
	}
}
