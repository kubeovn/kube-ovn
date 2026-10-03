package ovs

import (
	"testing"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newTestCNIVswitchClient(t *testing.T) *VswitchClient {
	t.Helper()
	dbModel, err := vswitch.FullDatabaseModel()
	require.NoError(t, err)
	_, sock := newOVSDBServer(t, t.Name(), dbModel, vswitch.Schema())
	c, err := NewCNIVswitchClient("unix:" + sock)
	require.NoError(t, err)
	t.Cleanup(c.Close)
	t.Setenv("PATH", "/missing-host-tools")
	bridge := &vswitch.Bridge{UUID: "bridge", Name: "br-int"}
	root := &vswitch.OpenvSwitch{UUID: "root", Bridges: []string{"bridge"}}
	ops, err := c.Create(bridge)
	require.NoError(t, err)
	rootOps, err := c.Create(root)
	require.NoError(t, err)
	ops = append(ops, rootOps...)
	_, err = c.transactVswitchOperations(ops)
	require.NoError(t, err)
	return c
}

func addTestCNIPort(t *testing.T, c *VswitchClient, name, ifaceID string) *vswitch.Interface {
	t.Helper()
	iface := &vswitch.Interface{Name: name, ExternalIDs: map[string]string{"iface-id": ifaceID, "pod_name": "pod", "pod_namespace": "ns"}}
	require.NoError(t, c.AddCNIPort(iface))
	got, err := c.CNIInterface(name)
	require.NoError(t, err)
	require.NotNil(t, got)
	return got
}

func TestCNIVswitchPortLifecycleWithoutHostTools(t *testing.T) {
	c := newTestCNIVswitchClient(t)
	stale := addTestCNIPort(t, c, "old_h", "pod.ns")
	current := addTestCNIPort(t, c, "new_h", "pod.ns")
	require.NoError(t, c.CleanDuplicateCNIPort("pod.ns", "new_h"))
	got, err := c.CNIInterface(stale.Name)
	require.NoError(t, err)
	require.NotContains(t, got.ExternalIDs, "iface-id")
	require.NoError(t, c.patchCNIMap(vswitch.InterfaceTable, current.UUID, "external_ids", map[string]string{"ovn-installed": "true", "ip": "old"}, []string{"ip"}))
	require.NoError(t, c.AddCNIPort(&vswitch.Interface{Name: current.Name, ExternalIDs: map[string]string{"iface-id": "pod.ns", "ip": "new"}}))
	got, err = c.CNIInterface(current.Name)
	require.NoError(t, err)
	require.Equal(t, "true", got.ExternalIDs["ovn-installed"])
	require.Equal(t, "new", got.ExternalIDs["ip"])
	require.NoError(t, c.SetCNIInterfaceMTU(current.Name, 1400))
	got, err = c.CNIInterface(current.Name)
	require.NoError(t, err)
	require.Equal(t, new(1400), got.MTURequest)
	require.NoError(t, c.DeleteCNIPort(current.Name))
	require.NoError(t, c.DeleteCNIPort(current.Name))
	ports, err := readVswitch[vswitch.Port](c, vswitch.PortTable, nameWhere(current.Name))
	require.NoError(t, err)
	require.Empty(t, ports)
}

func TestCNIVswitchQoSAndMirrorWithoutHostTools(t *testing.T) {
	c := newTestCNIVswitchClient(t)
	iface := addTestCNIPort(t, c, "pod_h", "pod.ns")
	require.NoError(t, c.SetCNIBandwidth("pod", "ns", "pod.ns", "10", "20", "0", "0"))
	got, err := c.CNIInterface(iface.Name)
	require.NoError(t, err)
	require.Equal(t, 10000, got.IngressPolicingRate)
	require.Zero(t, got.IngressPolicingBurst)
	qos, queues, err := c.cniQoSState()
	require.NoError(t, err)
	require.Len(t, qos, 1)
	require.Len(t, queues, 1)
	require.Equal(t, "20000000", queues[0].OtherConfig["max-rate"])
	require.Equal(t, "0", queues[0].OtherConfig["burst"])
	require.NoError(t, c.patchCNIMap(vswitch.QueueTable, queues[0].UUID, "other_config", map[string]string{"priority": "7"}, nil))
	require.NoError(t, c.SetCNIBandwidth("pod", "ns", "pod.ns", "0", "0", "", ""))
	qos, queues, err = c.cniQoSState()
	require.NoError(t, err)
	require.Len(t, qos, 1)
	require.Equal(t, map[string]string{"priority": "7"}, queues[0].OtherConfig)
	// A used QoS/queue must survive DEL cleanup for another attachment.
	require.NoError(t, c.ClearCNIQoS("pod", "ns", ""))
	qos, queues, err = c.cniQoSState()
	require.NoError(t, err)
	require.Len(t, qos, 1)
	require.Len(t, queues, 1)
	require.NoError(t, c.patchCNIMap(vswitch.QueueTable, queues[0].UUID, "other_config", nil, []string{"priority"}))
	require.NoError(t, c.SetCNIBandwidth("pod", "ns", "pod.ns", "0", "0", "", ""))
	qos, queues, err = c.cniQoSState()
	require.NoError(t, err)
	require.Empty(t, qos)
	require.Empty(t, queues)
	require.NoError(t, c.SetCNINetem("pod", "ns", "pod.ns", "10", "20", "0.5", "2"))
	qos, _, err = c.cniQoSState()
	require.NoError(t, err)
	require.Len(t, qos, 1)
	require.Equal(t, util.NetemQos, qos[0].Type)
	require.Equal(t, map[string]string{"latency": "10000", "limit": "20", "loss": "0.5", "jitter": "2000"}, qos[0].OtherConfig)
	require.NoError(t, c.SetCNINetem("pod", "ns", "pod.ns", "5", "", "", ""))
	qos, _, err = c.cniQoSState()
	require.NoError(t, err)
	require.Equal(t, map[string]string{"latency": "5000"}, qos[0].OtherConfig)
	// HTB wins over a requested netem configuration.
	require.NoError(t, c.SetCNIBandwidth("pod", "ns", "pod.ns", "0", "5", "", ""))
	require.NoError(t, c.SetCNINetem("pod", "ns", "pod.ns", "1", "", "", ""))
	qos, _, err = c.cniQoSState()
	require.NoError(t, err)
	require.Equal(t, util.HtbQos, qos[0].Type)

	ports, err := readVswitch[vswitch.Port](c, vswitch.PortTable, nameWhere(iface.Name))
	require.NoError(t, err)
	mirrorID := "mirror"
	row := ovsdb.Row{"name": util.MirrorDefaultName, "select_dst_port": ovsdb.OvsSet{}}
	bridges, err := readVswitch[vswitch.Bridge](c, vswitch.BridgeTable, nameWhere("br-int"))
	require.NoError(t, err)
	_, err = c.transactVswitchOperations([]ovsdb.Operation{
		{Op: ovsdb.OperationInsert, Table: vswitch.MirrorTable, UUIDName: mirrorID, Row: row},
		{Op: ovsdb.OperationMutate, Table: vswitch.BridgeTable, Where: uuidWhere(bridges[0].UUID), Mutations: []ovsdb.Mutation{uuidSetMutation("mirrors", []string{mirrorID}, ovsdb.MutateOperationInsert)}},
	})
	require.NoError(t, err)
	require.NoError(t, c.ConfigureCNIMirror(false, "true", "pod.ns"))
	mirrors, err := readVswitch[cniMirror](c, vswitch.MirrorTable, nameWhere(util.MirrorDefaultName))
	require.NoError(t, err)
	require.Equal(t, []string{ports[0].UUID}, mirrors[0].SelectDstPort)
	require.NoError(t, c.ConfigureCNIMirror(false, "false", "pod.ns"))
	mirrors, err = readVswitch[cniMirror](c, vswitch.MirrorTable, nameWhere(util.MirrorDefaultName))
	require.NoError(t, err)
	require.Empty(t, mirrors[0].SelectDstPort)
	require.NoError(t, c.DeleteCNIPort(iface.Name))
	require.NoError(t, c.ClearCNIQoS("pod", "ns", ""))
	qos, queues, err = c.cniQoSState()
	require.NoError(t, err)
	require.Empty(t, qos)
	require.Empty(t, queues)
}

func TestCNIVswitchDPDKAndLegacySchema(t *testing.T) {
	schema := vswitch.Schema()
	delete(schema.Tables[vswitch.MirrorTable].Columns, "filter")
	delete(schema.Tables[vswitch.FlowSampleCollectorSetTable].Columns, "local_group_id")
	// The test server must use the same partial model as older installations.
	partialModel, err := model.NewClientDBModel(vswitch.DatabaseName, map[string]model.Model{
		vswitch.OpenvSwitchTable: &vswitch.OpenvSwitch{}, vswitch.BridgeTable: &vswitch.Bridge{},
		vswitch.PortTable: &vswitch.Port{}, vswitch.InterfaceTable: &vswitch.Interface{},
		vswitch.QoSTable: &vswitch.QoS{}, vswitch.QueueTable: &vswitch.Queue{}, vswitch.MirrorTable: &cniMirror{},
	})
	require.NoError(t, err)
	_, sock := newOVSDBServer(t, t.Name(), partialModel, schema)
	c, err := NewCNIVswitchClient("unix:" + sock)
	require.NoError(t, err)
	t.Cleanup(c.Close)
	t.Setenv("PATH", "/missing-host-tools")
	ops, err := c.Create(&vswitch.Bridge{UUID: "bridge", Name: "br-int", DatapathType: "netdev"})
	require.NoError(t, err)
	rootOps, err := c.Create(&vswitch.OpenvSwitch{Bridges: []string{"bridge"}})
	require.NoError(t, err)
	_, err = c.transactVswitchOperations(append(ops, rootOps...))
	require.NoError(t, err)
	iface := &vswitch.Interface{Name: "dpdk_h", Type: "dpdkvhostuserclient", Options: map[string]string{"vhost-server-path": "/var/pod/socket"}, ExternalIDs: map[string]string{"iface-id": "pod.ns"}}
	require.NoError(t, c.AddCNIPort(iface))
	got, err := c.CNIInterface(iface.Name)
	require.NoError(t, err)
	require.Equal(t, iface.Type, got.Type)
	require.Equal(t, iface.Options, got.Options)
	userspace, err := c.CNIUserspaceDataPath()
	require.NoError(t, err)
	require.True(t, userspace)
	// Concurrent metadata is retained when repeating an ADD.
	iface.Options["vhost-server-path"] = "/var/pod/new-socket"
	require.NoError(t, c.AddCNIPort(iface))
	got, err = c.CNIInterface(iface.Name)
	require.NoError(t, err)
	require.Equal(t, "/var/pod/new-socket", got.Options["vhost-server-path"])
	require.NoError(t, c.SetCNIBandwidth("pod", "ns", "pod.ns", "1", "2", "", ""))
	require.NoError(t, c.ConfigureCNIMirror(true, "", "pod.ns"))
	require.NoError(t, c.DeleteCNIPort(iface.Name))
	require.NoError(t, c.ClearCNIQoS("pod", "ns", ""))
}
