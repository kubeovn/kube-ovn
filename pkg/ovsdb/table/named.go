package table

import (
	"context"
	"errors"
	"fmt"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// CreateIfAbsentByName atomically inserts a named row. Existing rows, including
// pre-existing duplicates, are left unchanged. The server-side Wait closes the
// race between the cache check and insertion by a concurrent creator.
func CreateIfAbsentByName[T any](ctx context.Context, provider Provider, row *T, tableName, name, method string, nameOf func(*T) string) error {
	if name == "" {
		return errors.New("empty row name")
	}
	rows, err := Filter[T](ctx, provider, row, func(existing *T) bool { return nameOf(existing) == name })
	if err != nil {
		return fmt.Errorf("check existence of %s %q: %w", tableName, name, err)
	}
	if len(rows) != 0 {
		return nil
	}
	handle, err := tableFor(provider, row)
	if err != nil {
		return err
	}
	operations, err := handle.CreateOps(row)
	if err != nil {
		return fmt.Errorf("generate operations for creating %s %q: %w", tableName, name, err)
	}
	wait := ovsdb.Operation{
		Op:      ovsdb.OperationWait,
		Table:   tableName,
		Timeout: new(0),
		Where:   []ovsdb.Condition{{Column: "name", Function: ovsdb.ConditionEqual, Value: name}},
		Columns: []string{"name"},
		Until:   string(ovsdb.WaitConditionNotEqual),
		Rows:    []ovsdb.Row{{"name": name}},
	}
	operations = append([]ovsdb.Operation{wait}, operations...)
	if err := handle.Transact(ctx, method, operations...); err != nil {
		if _, timedOut := errors.AsType[*ovsdb.TimedOut](err); timedOut {
			return nil
		}
		return fmt.Errorf("create %s %q: %w", tableName, name, err)
	}
	return nil
}
