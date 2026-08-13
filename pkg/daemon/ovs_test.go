package daemon

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestClassifyProviderVlanPort(t *testing.T) {
	t.Parallel()

	bridge := "br-underlay"
	const vlanID = 500

	require.Equal(t, providerVlanPortCurrent, classifyProviderVlanPort("kv500-l2bvmi7mc", bridge, vlanID))
	require.Equal(t, providerVlanPortLegacy, classifyProviderVlanPort("br-underlay-vlan500", bridge, vlanID))
	require.Equal(t, providerVlanPortCurrent, classifyProviderVlanPort("br-e-vlan10", "br-e", 10))
	require.Equal(t, providerVlanPortUnrelated, classifyProviderVlanPort("enp16s0f0", bridge, vlanID))
}

func TestClassifyProviderVlanPortForBridge(t *testing.T) {
	t.Parallel()

	vlanInterfaces := map[string]int{"bond0.20": 20}

	kind, vlanID, vlanInterface := classifyProviderVlanPortForBridge(util.VlanInternalPortName("br-underlay", 20), "br-underlay", vlanInterfaces, true)
	require.Equal(t, providerVlanPortCurrent, kind)
	require.Equal(t, 20, vlanID)
	require.Equal(t, "bond0.20", vlanInterface)

	kind, vlanID, vlanInterface = classifyProviderVlanPortForBridge(util.VlanInternalPortName("br-underlay", 30), "br-underlay", vlanInterfaces, true)
	require.Equal(t, providerVlanPortStale, kind)
	require.Equal(t, 30, vlanID)
	require.Empty(t, vlanInterface)

	kind, vlanID, vlanInterface = classifyProviderVlanPortForBridge("br-underlay-vlan20", "br-underlay", vlanInterfaces, false)
	require.Equal(t, providerVlanPortForeign, kind)
	require.Equal(t, 20, vlanID)
	require.Empty(t, vlanInterface)
}

func TestProviderBridgePortCleanupAction(t *testing.T) {
	t.Parallel()

	const bridge = "br-underlay"
	require.Equal(t, providerBridgePortReject, providerBridgePortCleanupAction("br-underlay-vlan20", bridge, false))
	require.Equal(t, providerBridgePortRemoveVlan, providerBridgePortCleanupAction("br-underlay-vlan20", bridge, true))
	require.Equal(t, providerBridgePortRemoveNic, providerBridgePortCleanupAction("eth0", bridge, false))
}

func TestProviderBridgePorts(t *testing.T) {
	t.Parallel()

	require.Empty(t, providerBridgePorts(""))
	require.Equal(t, []string{"eth0", "br-underlay-vlan20"}, providerBridgePorts("eth0\nbr-underlay-vlan20"))
}
