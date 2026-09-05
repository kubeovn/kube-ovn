package ovs

import (
	"context"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
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

func TestSetInterfaceBandwidthUsesTableProvider(t *testing.T) {
	dbModel, err := model.NewClientDBModel(vswitch.DatabaseName, map[string]model.Model{
		vswitch.OpenvSwitchTable: &vswitch.OpenvSwitch{},
		vswitch.BridgeTable:      &vswitch.Bridge{},
		vswitch.InterfaceTable:   &vswitch.Interface{},
		vswitch.PortTable:        &vswitch.Port{},
		vswitch.QoSTable:         &vswitch.QoS{},
		vswitch.QueueTable:       &vswitch.Queue{},
	})
	require.NoError(t, err)

	_, sock := newOVSDBServer(t, "table-qos", dbModel, vswitch.Schema())
	client, err := NewVswitchClient("unix:"+sock, 1, 1)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	iface := &vswitch.Interface{
		UUID:        "interface",
		Name:        "veth-qos",
		ExternalIDs: map[string]string{"iface-id": "pod.ns.eth0"},
	}
	port := &vswitch.Port{UUID: "port", Name: "veth-qos", Interfaces: []string{"interface"}}
	bridge := &vswitch.Bridge{UUID: "bridge", Name: "br-int", Ports: []string{"port"}}
	open := &vswitch.OpenvSwitch{UUID: "open", Bridges: []string{"bridge"}}
	ifaceOps, err := client.Table(&vswitch.Interface{}).CreateOps(iface)
	require.NoError(t, err)
	portOps, err := client.Table(&vswitch.Port{}).CreateOps(port)
	require.NoError(t, err)
	bridgeOps, err := client.Table(&vswitch.Bridge{}).CreateOps(bridge)
	require.NoError(t, err)
	openOps, err := client.Table(&vswitch.OpenvSwitch{}).CreateOps(open)
	require.NoError(t, err)
	seedOps := append(ifaceOps, portOps...)
	seedOps = append(seedOps, bridgeOps...)
	seedOps = append(seedOps, openOps...)
	require.NoError(t, client.Table(&vswitch.Interface{}).Transact(context.Background(), "seed-table-qos", seedOps...))
	require.Eventually(t, func() bool {
		var rows []vswitch.Interface
		return client.Table(&vswitch.Interface{}).List(context.Background(), &rows) == nil && len(rows) == 1
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, SetInterfaceBandwidth("pod", "ns", "pod.ns.eth0", "2", "3", "", "", client))

	var interfaces []vswitch.Interface
	var ports []vswitch.Port
	var qosRows []vswitch.QoS
	var queues []vswitch.Queue
	require.Eventually(t, func() bool {
		if client.Table(&vswitch.Interface{}).List(context.Background(), &interfaces) != nil ||
			client.Table(&vswitch.Port{}).List(context.Background(), &ports) != nil ||
			client.Table(&vswitch.QoS{}).List(context.Background(), &qosRows) != nil ||
			client.Table(&vswitch.Queue{}).List(context.Background(), &queues) != nil {
			return false
		}
		return len(interfaces) == 1 && len(ports) == 1 && len(qosRows) == 1 && len(queues) == 1
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, 2000, interfaces[0].IngressPolicingRate)
	require.Equal(t, 1600, interfaces[0].IngressPolicingBurst)
	require.Equal(t, "pod.ns.eth0", queues[0].ExternalIDs["iface-id"])
	require.Equal(t, "3000000", queues[0].OtherConfig["max-rate"])
	require.Equal(t, "300000", queues[0].OtherConfig["burst"])
	require.Equal(t, util.HtbQos, qosRows[0].Type)
	require.NotNil(t, ports[0].QOS)
	require.Equal(t, qosRows[0].UUID, *ports[0].QOS)

	require.NoError(t, SetInterfaceBandwidth("pod", "ns", "pod.ns.eth0", "2", "", "", "", client))
	require.Eventually(t, func() bool {
		var currentPorts []vswitch.Port
		var currentQoS []vswitch.QoS
		var currentQueues []vswitch.Queue
		if client.Table(&vswitch.Port{}).List(context.Background(), &currentPorts) != nil ||
			client.Table(&vswitch.QoS{}).List(context.Background(), &currentQoS) != nil ||
			client.Table(&vswitch.Queue{}).List(context.Background(), &currentQueues) != nil {
			return false
		}
		return len(currentPorts) == 1 && currentPorts[0].QOS == nil && len(currentQoS) == 0 && len(currentQueues) == 0
	}, time.Second, 10*time.Millisecond)
}

func TestSetNetemQosUsesTableProvider(t *testing.T) {
	dbModel, err := model.NewClientDBModel(vswitch.DatabaseName, map[string]model.Model{
		vswitch.OpenvSwitchTable: &vswitch.OpenvSwitch{},
		vswitch.BridgeTable:      &vswitch.Bridge{},
		vswitch.InterfaceTable:   &vswitch.Interface{},
		vswitch.PortTable:        &vswitch.Port{},
		vswitch.QoSTable:         &vswitch.QoS{},
		vswitch.QueueTable:       &vswitch.Queue{},
	})
	require.NoError(t, err)

	_, sock := newOVSDBServer(t, "table-netem-qos", dbModel, vswitch.Schema())
	client, err := NewVswitchClient("unix:"+sock, 1, 1)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	iface := &vswitch.Interface{
		UUID:        "interface",
		Name:        "veth-netem",
		ExternalIDs: map[string]string{"iface-id": "pod.ns.eth0"},
	}
	port := &vswitch.Port{UUID: "port", Name: "veth-netem", Interfaces: []string{"interface"}}
	bridge := &vswitch.Bridge{UUID: "bridge", Name: "br-int", Ports: []string{"port"}}
	open := &vswitch.OpenvSwitch{UUID: "open", Bridges: []string{"bridge"}}
	ifaceOps, err := client.Table(&vswitch.Interface{}).CreateOps(iface)
	require.NoError(t, err)
	portOps, err := client.Table(&vswitch.Port{}).CreateOps(port)
	require.NoError(t, err)
	bridgeOps, err := client.Table(&vswitch.Bridge{}).CreateOps(bridge)
	require.NoError(t, err)
	openOps, err := client.Table(&vswitch.OpenvSwitch{}).CreateOps(open)
	require.NoError(t, err)
	seedOps := append(ifaceOps, portOps...)
	seedOps = append(seedOps, bridgeOps...)
	seedOps = append(seedOps, openOps...)
	require.NoError(t, client.Table(&vswitch.Interface{}).Transact(context.Background(), "seed-table-netem-qos", seedOps...))
	require.Eventually(t, func() bool {
		var rows []vswitch.Interface
		return client.Table(&vswitch.Interface{}).List(context.Background(), &rows) == nil && len(rows) == 1
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, SetNetemQos("pod", "ns", "pod.ns.eth0", "10", "20", "1.5", "3", client))
	var qosRows []vswitch.QoS
	var ports []vswitch.Port
	require.Eventually(t, func() bool {
		if client.Table(&vswitch.QoS{}).List(context.Background(), &qosRows) != nil ||
			client.Table(&vswitch.Port{}).List(context.Background(), &ports) != nil {
			return false
		}
		return len(qosRows) == 1 && len(ports) == 1 && ports[0].QOS != nil
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, util.NetemQos, qosRows[0].Type)
	require.Equal(t, map[string]string{
		"latency": "10000",
		"limit":   "20",
		"loss":    "1.5",
		"jitter":  "3000",
	}, qosRows[0].OtherConfig)
	require.Equal(t, qosRows[0].UUID, *ports[0].QOS)

	require.NoError(t, SetNetemQos("pod", "ns", "pod.ns.eth0", "0", "0", "0", "0", client))
	require.Eventually(t, func() bool {
		var currentQoS []vswitch.QoS
		var currentPorts []vswitch.Port
		if client.Table(&vswitch.QoS{}).List(context.Background(), &currentQoS) != nil ||
			client.Table(&vswitch.Port{}).List(context.Background(), &currentPorts) != nil {
			return false
		}
		return len(currentQoS) == 0 && len(currentPorts) == 1 && currentPorts[0].QOS == nil
	}, time.Second, 10*time.Millisecond)
}
