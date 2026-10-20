package client

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	libovsdbclient "github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/database/inmemory"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/ovn-kubernetes/libovsdb/ovsdb/serverdb"
	"github.com/ovn-kubernetes/libovsdb/server"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

func TestSingleEndpointDatabaseModel(t *testing.T) {
	for _, tt := range []struct {
		name    string
		model   string
		leader  bool
		wantErr bool
	}{
		{name: "standalone", model: serverdb.DatabaseModelStandalone, leader: true},
		{name: "relay", model: serverdb.DatabaseModelRelay},
		{name: "clustered leader", model: serverdb.DatabaseModelClustered, leader: true},
		{name: "clustered follower", model: serverdb.DatabaseModelClustered, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dbModel, err := vswitch.FullDatabaseModel()
			require.NoError(t, err)
			serverModel, err := serverdb.FullDatabaseModel()
			require.NoError(t, err)
			logger := logr.Discard()
			db := inmemory.NewDatabase(map[string]model.ClientDBModel{
				vswitch.DatabaseName: dbModel, "_Server": serverModel,
			}, &logger)
			primary, errs := model.NewDatabaseModel(vswitch.Schema(), dbModel)
			require.Empty(t, errs)
			metadata, errs := model.NewDatabaseModel(serverdb.Schema(), serverModel)
			require.Empty(t, errs)
			s, err := server.NewOvsdbServer(db, &logger, primary, metadata)
			require.NoError(t, err)
			socket := filepath.Join(t.TempDir(), "db.sock")
			go func() {
				if err := s.Serve("unix", socket); err != nil {
					t.Errorf("serve OVSDB: %v", err)
				}
			}()
			t.Cleanup(s.Close)
			require.Eventually(t, s.Ready, time.Second, time.Millisecond)
			endpoint := "unix:" + socket
			admin, err := libovsdbclient.NewOVSDBClient(serverModel, libovsdbclient.WithEndpoint(endpoint))
			require.NoError(t, err)
			t.Cleanup(admin.Close)
			require.NoError(t, admin.Connect(t.Context()))
			row := &serverdb.Database{
				Name: vswitch.DatabaseName, Model: tt.model, Leader: tt.leader,
				Connected: true, Sid: new("00000000-0000-4000-8000-000000000001"),
			}
			ops, err := admin.Create(row)
			require.NoError(t, err)
			results, err := admin.Transact(t.Context(), ops...)
			require.NoError(t, err)
			_, err = ovsdb.CheckOperationResults(results, ops)
			require.NoError(t, err)

			c, err := NewOvsDbClient(vswitch.DatabaseName, endpoint, dbModel, nil, 1, 0)
			if tt.wantErr {
				require.ErrorContains(t, err, "endpoint is not leader")
				return
			}
			require.NoError(t, err)
			t.Cleanup(c.Close)
			require.NoError(t, c.Echo(t.Context()))
		})
	}
}
