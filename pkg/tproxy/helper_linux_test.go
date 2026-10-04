package tproxy

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/database/inmemory"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/ovn-kubernetes/libovsdb/server"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestNamespaceOVSOwnershipAndReconnect(t *testing.T) {
	fullModel, err := vswitch.FullDatabaseModel()
	require.NoError(t, err)
	dbModel, modelErrors := model.NewDatabaseModel(vswitch.Schema(), fullModel)
	require.Empty(t, modelErrors)
	db := inmemory.NewDatabase(map[string]model.ClientDBModel{vswitch.DatabaseName: fullModel}, nil)
	s, err := server.NewOvsdbServer(db, nil, dbModel)
	require.NoError(t, err)
	socket := filepath.Join(t.TempDir(), "ovs.sock")
	serverDone := make(chan error, 1)
	go func() { serverDone <- s.Serve("unix", socket) }()
	t.Cleanup(func() { s.Close(); require.NoError(t, <-serverDone) })
	require.Eventually(t, s.Ready, time.Second, 10*time.Millisecond)
	admin, err := client.NewOVSDBClient(fullModel, client.WithEndpoint("unix:"+socket))
	require.NoError(t, err)
	require.NoError(t, admin.Connect(t.Context()))
	t.Cleanup(admin.Close)
	iface := &vswitch.Interface{UUID: "iface", Name: "pod_h", ExternalIDs: map[string]string{
		"iface-id": "pod.ns", "vendor": util.CniTypeName, "pod_netns": "/var/run/netns/cni-pod", "ip": "10.16.0.2,fd00::2",
	}}
	var ops []ovsdb.Operation
	for _, item := range []model.Model{
		iface,
		&vswitch.Port{UUID: "port", Name: iface.Name, Interfaces: []string{"iface"}},
		&vswitch.Bridge{UUID: "bridge", Name: "br-int", Ports: []string{"port"}},
		&vswitch.OpenvSwitch{Bridges: []string{"bridge"}},
	} {
		created, err := admin.Create(item)
		require.NoError(t, err)
		ops = append(ops, created...)
	}
	results, err := admin.Transact(t.Context(), ops...)
	require.NoError(t, err)
	_, err = ovsdb.CheckOperationResults(results, ops)
	require.NoError(t, err)
	iface.UUID = results[0].UUID.GoUUID
	c, err := newNamespaceOVSClient("unix:" + socket)
	require.NoError(t, err)
	t.Cleanup(c.Close)
	t.Setenv("PATH", "/missing-host-tools")
	request := NamespaceRequest{NetNS: "/var/run/netns/cni-pod", PodIP: "10.16.0.2", Port: 8080}
	for _, ip := range []string{"10.16.0.2", "fd00::2"} {
		request.PodIP = ip
		ok, err := c.hasPodNetNS(request)
		require.NoError(t, err)
		require.True(t, ok)
	}
	request.PodIP = "10.16.0.3"
	ok, err := c.hasPodNetNS(request)
	require.NoError(t, err)
	require.False(t, ok)
	request.PodIP = "10.16.0.2"
	request.NetNS = "/var/run/netns/another-pod"
	ok, err = c.hasPodNetNS(request)
	require.NoError(t, err)
	require.False(t, ok)
	request.NetNS = "/var/run/netns/cni-pod"
	for _, change := range []struct{ key, value string }{{"vendor", "foreign"}, {"iface-id", ""}} {
		original := iface.ExternalIDs[change.key]
		iface.ExternalIDs[change.key] = change.value
		ops, err := admin.Where(iface).Update(iface, &iface.ExternalIDs)
		require.NoError(t, err)
		_, err = admin.Transact(t.Context(), ops...)
		require.NoError(t, err)
		ok, err = c.hasPodNetNS(request)
		require.NoError(t, err)
		require.False(t, ok)
		iface.ExternalIDs[change.key] = original
		ops, err = admin.Where(iface).Update(iface, &iface.ExternalIDs)
		require.NoError(t, err)
		_, err = admin.Transact(t.Context(), ops...)
		require.NoError(t, err)
	}
	c.Disconnect()
	require.Eventually(t, c.Connected, 3*time.Second, 10*time.Millisecond)
	ok, err = c.hasPodNetNS(request)
	require.NoError(t, err)
	require.True(t, ok)
}
