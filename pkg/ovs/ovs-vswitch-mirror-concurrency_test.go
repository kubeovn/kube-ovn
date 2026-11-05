package ovs

import (
	"context"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type beforeMirrorWriteBackend struct {
	*table.Client
	beforeWrite func()
}

func (b *beforeMirrorWriteBackend) Transact(ctx context.Context, operations ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	for _, operation := range operations {
		if operation.Table == vswitch.MirrorTable && operation.Op != ovsdb.OperationSelect && b.beforeWrite != nil {
			before := b.beforeWrite
			b.beforeWrite = nil
			before()
			break
		}
	}
	return b.TransactResults(ctx, operations...)
}

func newMirrorConcurrencyClient(t *testing.T, selected bool) *VswitchClient {
	t.Helper()
	schema := vswitch.Schema()
	delete(schema.Tables[vswitch.MirrorTable].Columns, "filter")
	dbModel, err := model.NewClientDBModel(vswitch.DatabaseName, map[string]model.Model{
		vswitch.BridgeTable: &vswitch.Bridge{}, vswitch.InterfaceTable: &vswitch.Interface{},
		vswitch.MirrorTable: &mirrorTableRow{}, vswitch.OpenvSwitchTable: &vswitch.OpenvSwitch{}, vswitch.PortTable: &vswitch.Port{},
	})
	require.NoError(t, err)
	_, sock := newOVSDBServer(t, "mirror-concurrent", dbModel, schema)
	client, err := NewVswitchClient("unix:"+sock, 1, 1)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	mirror := &mirrorTableRow{UUID: "mirror", Name: util.MirrorDefaultName}
	if selected {
		mirror.SelectDstPort = []string{"port-a"}
	}
	rows := []model.Model{
		&vswitch.Interface{UUID: "iface-a", Name: "a", ExternalIDs: map[string]string{"iface-id": "pod-a"}},
		&vswitch.Interface{UUID: "iface-b", Name: "b", ExternalIDs: map[string]string{"iface-id": "pod-b"}},
		&vswitch.Port{UUID: "port-a", Name: "a", Interfaces: []string{"iface-a"}},
		&vswitch.Port{UUID: "port-b", Name: "b", Interfaces: []string{"iface-b"}},
		&vswitch.Bridge{UUID: "bridge", Name: "br-int", Ports: []string{"port-a", "port-b"}, Mirrors: []string{"mirror"}},
		&vswitch.OpenvSwitch{UUID: "root", Bridges: []string{"bridge"}},
	}
	operations, err := client.OptionalTable(vswitch.MirrorTable, &mirrorTableRow{}).CreateOps(mirror)
	require.NoError(t, err)
	for _, row := range rows {
		more, err := client.Table(row).CreateOps(row)
		require.NoError(t, err)
		operations = append(operations, more...)
	}
	require.NoError(t, client.Transact("seed-mirror", operations))
	require.Eventually(t, func() bool {
		var ports []vswitch.Port
		var interfaces []vswitch.Interface
		return client.Table(&vswitch.Port{}).List(t.Context(), &ports) == nil && len(ports) == 2 &&
			client.Table(&vswitch.Interface{}).List(t.Context(), &interfaces) == nil && len(interfaces) == 2
	}, time.Second, time.Millisecond)
	return client
}

func TestConfigVswitchInterfaceMirrorPreservesConcurrentPorts(t *testing.T) {
	for _, open := range []bool{true, false} {
		t.Run(map[bool]string{true: "enable", false: "disable"}[open], func(t *testing.T) {
			client := newMirrorConcurrencyClient(t, !open)
			backend := &beforeMirrorWriteBackend{Client: client.Client, beforeWrite: func() {
				require.NoError(t, ConfigVswitchInterfaceMirror(t.Context(), client, true, "pod-b"))
			}}
			provider := table.NewDatabase(backend, time.Second, table.RetryPolicy{})
			require.NoError(t, ConfigVswitchInterfaceMirror(t.Context(), provider, open, "pod-a"))
			require.Nil(t, backend.beforeWrite, "the concurrent writer must run before the outer write")
			var mirrors []mirrorTableRow
			require.NoError(t, client.OptionalTable(vswitch.MirrorTable, &mirrorTableRow{}).List(t.Context(), &mirrors))
			require.Len(t, mirrors, 1)
			portB, err := findVswitchPort(t.Context(), client, "b")
			require.NoError(t, err)
			expected := []string{portB.UUID}
			if open {
				portA, err := findVswitchPort(t.Context(), client, "a")
				require.NoError(t, err)
				expected = append(expected, portA.UUID)
			}
			require.ElementsMatch(t, expected, mirrors[0].SelectDstPort)
		})
	}
}
