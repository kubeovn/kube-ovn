package ovs

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newTestCNIVswitchClient(t *testing.T) *VswitchClient {
	t.Helper()
	c, _ := newTestCNIVswitchClientWithBackend(t)
	return c
}

func newTestCNIVswitchClientWithBackend(t *testing.T) (*VswitchClient, client.Client) {
	t.Helper()
	dbModel, err := vswitch.FullDatabaseModel()
	require.NoError(t, err)
	sock := newCNITestOVSDBServer(t, dbModel, vswitch.Schema())
	backend, err := newCNIVswitchBackend("unix:" + sock)
	require.NoError(t, err)
	c := &VswitchClient{Database: table.NewDatabase(table.Wrap(backend), 30*time.Second, table.RetryPolicy{})}
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
	return c, backend
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

func TestCNIVswitchReconnect(t *testing.T) {
	c, backend := newTestCNIVswitchClientWithBackend(t)
	iface := addTestCNIPort(t, c, "pod_h", "pod.ns")
	require.NoError(t, c.patchCNIMap(vswitch.InterfaceTable, iface.UUID, "external_ids", map[string]string{
		"vendor": util.CniTypeName, "ip": "10.16.0.2", "pod_netns": "/var/run/netns/cni-pod",
	}, nil))
	backend.Disconnect()
	require.Eventually(t, c.Connected, 3*time.Second, 10*time.Millisecond)
	got, err := c.CNIInterface(iface.Name)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "10.16.0.2", got.ExternalIDs["ip"])
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
	require.NoError(t, c.patchCNIMap(vswitch.InterfaceTable, current.UUID, "external_ids", map[string]string{"vendor": util.CniTypeName, "ip": "10.16.0.2,fd00::2", "pod_netns": "/var/run/netns/cni-pod"}, []string{"vendor", "ip", "pod_netns"}))
	got, err = c.CNIInterface(current.Name)
	require.NoError(t, err)
	require.Equal(t, "10.16.0.2,fd00::2", got.ExternalIDs["ip"])
	require.Equal(t, "/var/run/netns/cni-pod", got.ExternalIDs["pod_netns"])
	require.Equal(t, util.CniTypeName, got.ExternalIDs["vendor"])
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
	sock := newCNITestOVSDBServer(t, partialModel, schema)
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

type cniTableBackend struct {
	*table.Database
}

func (b cniTableBackend) Transact(ctx context.Context, ops ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	return b.TransactResults(ctx, ops...)
}

type beforeCNIQoSWaitClient struct {
	table.Backend
	beforeWait func()
	waits      int
}

func (c *beforeCNIQoSWaitClient) Transact(ctx context.Context, ops ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	if len(ops) != 0 && ops[0].Op == ovsdb.OperationWait {
		c.waits++
		if c.beforeWait != nil {
			before := c.beforeWait
			c.beforeWait = nil
			before()
		}
	}
	return c.Backend.Transact(ctx, ops...)
}

func TestCNIQoSCleanupConcurrentBinding(t *testing.T) {
	c := newTestCNIVswitchClient(t)
	iface := addTestCNIPort(t, c, "pod_h", "pod.ns")
	require.NoError(t, c.SetCNIBandwidth("pod", "ns", "pod.ns", "0", "5", "", ""))
	qos, _, err := c.cniQoSState()
	require.NoError(t, err)
	require.Len(t, qos, 1)
	require.NoError(t, c.bindCNIQoS(iface.Name, nil))
	original := c.Database
	hook := &beforeCNIQoSWaitClient{Backend: cniTableBackend{Database: original}, beforeWait: func() {
		// Bind after the cleanup snapshot, but before its atomic guards.
		other := &VswitchClient{Database: original}
		require.NoError(t, other.bindCNIQoS(iface.Name, new(qos[0].UUID)))
	}}
	c.Database = table.NewDatabase(hook, c.Timeout, table.RetryPolicy{})
	require.NoError(t, c.ClearCNIQoS("pod", "ns", "pod.ns"))
	qos, queues, err := c.cniQoSState()
	require.NoError(t, err)
	require.Len(t, qos, 1)
	require.Len(t, queues, 1)
	require.Equal(t, 1, hook.waits)
}

func TestCNIQoSCleanupIgnoresConfigurationChanges(t *testing.T) {
	c := newTestCNIVswitchClient(t)
	iface := addTestCNIPort(t, c, "pod_h", "pod.ns")
	require.NoError(t, c.SetCNIBandwidth("pod", "ns", "pod.ns", "0", "5", "", ""))
	qos, queues, err := c.cniQoSState()
	require.NoError(t, err)
	require.NoError(t, c.bindCNIQoS(iface.Name, nil))
	original := c.Database
	hook := &beforeCNIQoSWaitClient{Backend: cniTableBackend{Database: original}, beforeWait: func() {
		other := &VswitchClient{Database: original}
		require.NoError(t, other.patchCNIMap(vswitch.QoSTable, qos[0].UUID, "other_config", map[string]string{"max-rate": "10"}, nil))
		require.NoError(t, other.patchCNIMap(vswitch.QueueTable, queues[0].UUID, "other_config", map[string]string{"priority": "7"}, nil))
	}}
	c.Database = table.NewDatabase(hook, c.Timeout, table.RetryPolicy{})
	require.NoError(t, c.ClearCNIQoS("pod", "ns", "pod.ns"))
	qos, queues, err = c.cniQoSState()
	require.NoError(t, err)
	require.Empty(t, qos)
	require.Empty(t, queues)
	require.Equal(t, 1, hook.waits)
}

// Set OVSDB_CNI_TEST_BIN_DIR to exercise the upstream server's transaction
// semantics as well as the in-process test server.
func newCNITestOVSDBServer(t *testing.T, dbModel model.ClientDBModel, schema ovsdb.DatabaseSchema) string {
	t.Helper()
	binDir := os.Getenv("OVSDB_CNI_TEST_BIN_DIR")
	if binDir == "" {
		_, socket := newOVSDBServer(t, t.Name(), dbModel, schema)
		return socket
	}
	dir, err := os.MkdirTemp("", "cni-ovsdb-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	schemaPath := filepath.Join(dir, "schema.json")
	data, err := json.Marshal(schema)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(schemaPath, data, 0o600))
	database := filepath.Join(dir, "database")
	output, err := exec.Command(filepath.Join(binDir, "ovsdb-tool"), "create", database, schemaPath).CombinedOutput()
	require.NoError(t, err, string(output))
	socket := filepath.Join(dir, "db.sock")
	server := exec.Command(filepath.Join(binDir, "ovsdb-server"), "--remote=punix:"+socket, "--unixctl="+filepath.Join(dir, "control.sock"), "--no-chdir", database)
	require.NoError(t, server.Start())
	t.Cleanup(func() { require.NoError(t, server.Process.Kill()); _ = server.Wait() })
	require.Eventually(t, func() bool {
		conn, err := net.Dial("unix", socket)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, time.Second, 10*time.Millisecond)
	return socket
}
