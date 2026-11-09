package ovs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestExternalLBLocalPort(t *testing.T) {
	dbModel, err := ovnnb.FullDatabaseModel()
	require.NoError(t, err)
	_, sock := newOVSDBServer(t, "external-lb-nb", dbModel, ovnnb.Schema())
	client, err := newOvnNbClient(t, "unix:"+sock, 5)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	require.NoError(t, client.CreateBareLogicalSwitch("underlay"))
	name := util.ExternalLBLocalPortName("underlay")
	require.NoError(t, client.ReconcileExternalLBLocalPort("underlay", "uid", false))
	require.NoError(t, client.ReconcileExternalLBLocalPort("underlay", "uid", true))
	port, err := client.GetLogicalSwitchPort(name, false)
	require.NoError(t, err)
	uuid := port.UUID
	require.Equal(t, "localport", port.Type)
	require.Equal(t, []string{"unknown"}, port.Addresses)
	require.Empty(t, port.PortSecurity)
	require.Equal(t, "true", port.Options[util.ExternalLBARPOption])
	require.NoError(t, client.ReconcileExternalLBLocalPort("underlay", "uid", true))
	port, err = client.GetLogicalSwitchPort(name, false)
	require.NoError(t, err)
	require.Equal(t, uuid, port.UUID)
	port.Addresses = []string{"02:00:00:00:00:01"}
	port.PortSecurity = []string{"02:00:00:00:00:01"}
	port.Options = nil
	require.NoError(t, client.UpdateLogicalSwitchPort(port, &port.Addresses, &port.PortSecurity, &port.Options))
	require.NoError(t, client.ReconcileExternalLBLocalPort("underlay", "uid", true))
	port, err = client.GetLogicalSwitchPort(name, false)
	require.NoError(t, err)
	require.Equal(t, []string{"unknown"}, port.Addresses)
	require.Empty(t, port.PortSecurity)
	require.Equal(t, "true", port.Options[util.ExternalLBARPOption])
	require.NoError(t, client.ReconcileExternalLBLocalPort("underlay", "uid", false))
	port, err = client.GetLogicalSwitchPort(name, true)
	require.NoError(t, err)
	require.Nil(t, port)
	require.NoError(t, client.CreateBareLogicalSwitchPort("underlay", name, "", "02:00:00:00:00:02"))
	require.ErrorContains(t, client.ReconcileExternalLBLocalPort("underlay", "uid", true), "not owned")
	require.NoError(t, client.ReconcileExternalLBLocalPort("underlay", "uid", false))
}
