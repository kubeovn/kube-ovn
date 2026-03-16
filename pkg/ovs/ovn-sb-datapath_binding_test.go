package ovs

import (
	"testing"

	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
)

func newDatapathBinding(name string, tunnelKey int) *ovnsb.DatapathBinding {
	return &ovnsb.DatapathBinding{
		UUID:        ovsclient.NamedUUID(),
		ExternalIDs: map[string]string{"name": name},
		TunnelKey:   tunnelKey,
	}
}

func (suite *OvnClientTestSuite) testGetLogicalSwitchTunnelKey() {
	t := suite.T()

	sbClient := suite.ovnSBClient
	nbClient := suite.ovnNBClient
	lsName := "test-ls-tunnel-key"

	// Create logical switch in NB to trigger datapath binding creation in SB
	// Note: In real OVN, northd syncs NB to SB. In test, we simulate by creating datapath binding directly.
	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	// In mock test environment, we need to create datapath binding manually
	// since there's no northd to sync NB to SB
	tunnelKey := 100
	dp := newDatapathBinding(lsName, tunnelKey)
	ops, err := sbClient.Create(dp)
	require.NoError(t, err)
	err = sbClient.Transact("datapath-binding-add", ops)
	require.NoError(t, err)

	t.Run("should return tunnel key for existing ls", func(t *testing.T) {
		key, err := sbClient.GetLogicalSwitchTunnelKey(lsName)
		require.NoError(t, err)
		require.Equal(t, tunnelKey, key)
	})

	t.Run("should fail with empty ls name", func(t *testing.T) {
		key, err := sbClient.GetLogicalSwitchTunnelKey("")
		require.Equal(t, 0, key)
		require.ErrorContains(t, err, "logical switch name is empty")
	})

	t.Run("should fail for non-existent ls", func(t *testing.T) {
		key, err := sbClient.GetLogicalSwitchTunnelKey("non-existent-ls")
		require.Equal(t, 0, key)
		require.ErrorContains(t, err, "datapath binding not found")
	})
}
