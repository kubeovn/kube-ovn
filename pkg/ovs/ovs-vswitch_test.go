package ovs

import (
	"context"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

func TestNewVswitchClientWithLegacySchema(t *testing.T) {
	schema := vswitch.Schema()
	delete(schema.Tables[vswitch.MirrorTable].Columns, "filter")
	delete(schema.Tables[vswitch.FlowSampleCollectorSetTable].Columns, "local_group_id")

	dbModel, err := model.NewClientDBModel(vswitch.DatabaseName, map[string]model.Model{
		vswitch.BridgeTable:      &vswitch.Bridge{},
		vswitch.InterfaceTable:   &vswitch.Interface{},
		vswitch.OpenvSwitchTable: &vswitch.OpenvSwitch{},
		vswitch.PortTable:        &vswitch.Port{},
	})
	require.NoError(t, err)

	_, sock := newOVSDBServer(t, "legacy-vswitch", dbModel, schema)
	client, err := NewVswitchClient("unix:"+sock, 1, 1)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	_, err = client.ListBridge(false, nil)
	require.NoError(t, err)
}

func TestVswitchClientCleanInterface(t *testing.T) {
	dbModel, err := model.NewClientDBModel(vswitch.DatabaseName, map[string]model.Model{
		vswitch.BridgeTable:      &vswitch.Bridge{},
		vswitch.InterfaceTable:   &vswitch.Interface{},
		vswitch.OpenvSwitchTable: &vswitch.OpenvSwitch{},
		vswitch.PortTable:        &vswitch.Port{},
		vswitch.QoSTable:         &vswitch.QoS{},
	})
	require.NoError(t, err)

	_, sock := newOVSDBServer(t, "clean-interface", dbModel, vswitch.Schema())
	client, err := NewVswitchClient("unix:"+sock, 1, 1)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	qos := &vswitch.QoS{UUID: "qos", Type: "linux-htb"}
	iface := &vswitch.Interface{UUID: "interface", Name: "veth-orphan"}
	port := &vswitch.Port{UUID: "port", Name: "veth-orphan", Interfaces: []string{"interface"}, QOS: new("qos")}
	bridge := &vswitch.Bridge{UUID: "bridge", Name: "br-int", Ports: []string{"port"}}
	open := &vswitch.OpenvSwitch{UUID: "open", Bridges: []string{"bridge"}}
	ops, err := client.Table(&vswitch.QoS{}).CreateOps(qos)
	require.NoError(t, err)
	moreOps, err := client.Table(&vswitch.Interface{}).CreateOps(iface)
	require.NoError(t, err)
	ops = append(ops, moreOps...)
	moreOps, err = client.Table(&vswitch.Port{}).CreateOps(port)
	require.NoError(t, err)
	ops = append(ops, moreOps...)
	moreOps, err = client.Table(&vswitch.Bridge{}).CreateOps(bridge)
	require.NoError(t, err)
	ops = append(ops, moreOps...)
	moreOps, err = client.Table(&vswitch.OpenvSwitch{}).CreateOps(open)
	require.NoError(t, err)
	ops = append(ops, moreOps...)
	require.NoError(t, client.Table(&vswitch.Port{}).Transact(context.Background(), "seed-clean-interface", ops...))

	require.Eventually(t, func() bool {
		var ports []vswitch.Port
		return client.Table(&vswitch.Port{}).List(context.Background(), &ports) == nil && len(ports) == 1
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, client.CleanInterface("veth-orphan"))

	require.Eventually(t, func() bool {
		var ports []vswitch.Port
		var interfaces []vswitch.Interface
		var qosRows []vswitch.QoS
		if err := client.Table(&vswitch.Port{}).List(context.Background(), &ports); err != nil {
			return false
		}
		if err := client.Table(&vswitch.Interface{}).List(context.Background(), &interfaces); err != nil {
			return false
		}
		if err := client.Table(&vswitch.QoS{}).List(context.Background(), &qosRows); err != nil {
			return false
		}
		return len(ports) == 0 && len(interfaces) == 0 && len(qosRows) == 0
	}, time.Second, 10*time.Millisecond)

	var bridges []vswitch.Bridge
	require.NoError(t, client.Table(&vswitch.Bridge{}).List(context.Background(), &bridges))
	require.Len(t, bridges, 1)
	require.Empty(t, bridges[0].Ports)
}
