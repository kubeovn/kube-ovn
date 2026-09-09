package ovs

import (
	"context"
	"fmt"
	"time"

	"github.com/ovn-kubernetes/libovsdb/ovsdb/serverdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
)

// ServerClient is a short-lived generic client for the libovsdb _Server
// database. The database schema is queried through OptionalTable because the
// server database is not monitored by the regular NB/SB clients.
type ServerClient struct {
	*compat.Database
}

// NewOvsdbServerClient creates a client for the libovsdb server database.
func NewOvsdbServerClient(address string, connTimeout, transactTimeout int) (*ServerClient, error) {
	dbModel, err := serverdb.FullDatabaseModel()
	if err != nil {
		return nil, fmt.Errorf("build ovsdb-server model: %w", err)
	}
	backend, err := ovsclient.NewOvsDbClient(
		serverdb.DatabaseName,
		address,
		dbModel,
		nil,
		connTimeout,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("connect to ovsdb-server database: %w", err)
	}
	return &ServerClient{Database: compat.NewDatabase(backend, time.Duration(transactTimeout)*time.Second, compat.RetryPolicy{}, compat.WithDatabaseName("ovsdb-server"))}, nil
}

// DatabaseLeader reads the leader flag for one served database.
func (c *ServerClient) DatabaseLeader(ctx context.Context, name string) (bool, error) {
	var rows []serverdb.Database
	if err := c.OptionalTable(serverdb.DatabaseTable, &serverdb.Database{}).List(ctx, &rows); err != nil {
		return false, fmt.Errorf("list ovsdb-server databases: %w", err)
	}
	for _, row := range rows {
		if row.Name == name {
			return row.Leader, nil
		}
	}
	return false, fmt.Errorf("ovsdb-server database %q was not found", name)
}
