package ovs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

// IPsecGlobalState is a live database readback, not a monitor-cache snapshot.
// It establishes the global switch only; local tunnel/SA cleanup still needs
// its own convergence and ownership checks before protection can be removed.
type IPsecGlobalState struct {
	UUID    string
	Enabled bool
}

func (c *OVNNbClient) GetIPsecGlobal(ctx context.Context) (*IPsecGlobalState, error) {
	return readIPsecGlobal(ctx, c.Database, c.Timeout, ovnnb.NBGlobalTable)
}

func (c *OVNSbClient) GetIPsecGlobal(ctx context.Context) (*IPsecGlobalState, error) {
	// Do not add a second SB monitor: reconnecting multiple monitors can purge
	// the existing Chassis cache. Select only the public switch and row UUID.
	return readIPsecGlobal(ctx, c.Database, c.Timeout, ovnsb.SBGlobalTable)
}

func readIPsecGlobal(ctx context.Context, db *table.Database, timeout time.Duration, tableName string) (*IPsecGlobalState, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ops := []ovsdb.Operation{{Op: ovsdb.OperationSelect, Table: tableName, Where: []ovsdb.Condition{}, Columns: []string{"_uuid", "ipsec"}}}
	results, err := db.TransactResults(ctx, ops...)
	if err != nil {
		return nil, fmt.Errorf("read live IPsec switch from %s: %w", tableName, err)
	}
	if opErrors, err := ovsdb.CheckOperationResults(results, ops); err != nil {
		failures := []error{err}
		for _, opErr := range opErrors {
			failures = append(failures, opErr)
		}
		return nil, errors.Join(failures...)
	}
	if len(results) != 1 || len(results[0].Rows) != 1 {
		return nil, fmt.Errorf("live IPsec read requires exactly one %s row", tableName)
	}
	row := results[0].Rows[0]
	uuid, uuidOK := row["_uuid"].(ovsdb.UUID)
	enabled, switchOK := row["ipsec"].(bool)
	if !uuidOK || !ovsdb.IsValidUUID(uuid.GoUUID) || !switchOK {
		return nil, fmt.Errorf("live IPsec read from %s lacks a valid UUID or boolean switch", tableName)
	}
	return &IPsecGlobalState{UUID: uuid.GoUUID, Enabled: enabled}, nil
}
