package ipsec

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/database/inmemory"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb/serverdb"
	"github.com/ovn-kubernetes/libovsdb/server"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

func TestCleanupSkipsFreshHardwareOffloadNode(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "db.sock")
	dbModel, err := vswitch.FullDatabaseModel()
	require.NoError(t, err)
	serverModel, err := serverdb.FullDatabaseModel()
	require.NoError(t, err)
	schema, serverSchema := vswitch.Schema(), serverdb.Schema()
	db := inmemory.NewDatabase(map[string]model.ClientDBModel{schema.Name: dbModel, serverSchema.Name: serverModel}, nil)
	dbMod, errs := model.NewDatabaseModel(schema, dbModel)
	require.Empty(t, errs)
	svrMod, errs := model.NewDatabaseModel(serverSchema, serverModel)
	require.Empty(t, errs)
	ovsdbServer, err := server.NewOvsdbServer(db, nil, dbMod, svrMod)
	require.NoError(t, err)
	go func() { _ = ovsdbServer.Serve("unix", socket) }()
	t.Cleanup(ovsdbServer.Close)
	require.Eventually(t, ovsdbServer.Ready, 3e9, 1e7)

	seed, err := client.NewOVSDBClient(dbModel, client.WithEndpoint("unix:"+socket))
	require.NoError(t, err)
	require.NoError(t, seed.Connect(context.Background()))
	t.Cleanup(seed.Close)
	ops, err := seed.Create(&vswitch.OpenvSwitch{
		UUID:        "root",
		ExternalIDs: map[string]string{"system-id": "chassis"},
		OtherConfig: map[string]string{"hw-offload": "true"},
	})
	require.NoError(t, err)
	_, err = seed.Transact(context.Background(), ops...)
	require.NoError(t, err)

	node := &corev1.Node{Name: "node", UID: "node-uid"}
	newAgent := func(objects ...runtime.Object) *Agent {
		return &Agent{
			config: Configuration{NodeName: "node", Namespace: "kube-system", OVSSocket: socket, ProtectionDir: t.TempDir(), Kube: fake.NewClientset(objects...)},
			store:  store{dir: t.TempDir()},
		}
	}
	t.Run("fresh node", func(t *testing.T) {
		needed, err := newAgent(node).restoreCleanupProtection(context.Background())
		require.NoError(t, err)
		require.False(t, needed)
	})
	t.Run("frozen cleanup target", func(t *testing.T) {
		hash := strings.Repeat("0", 64)
		state := `{"version":1,"generation":"g","epoch":"e","phase":"` + DisablingPhase + `","daemonSetUID":"ds","templateHash":"` + hash + `","trustHash":"` + hash + `","targets":{"node":"node-uid"}}`
		cm := &corev1.ConfigMap{Name: CoordinationConfigMap, Namespace: "kube-system", Data: map[string]string{"state": state}}
		_, err := newAgent(node, cm).restoreCleanupProtection(context.Background())
		require.ErrorContains(t, err, "hardware offload")
	})
}
