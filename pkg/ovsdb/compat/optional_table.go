package compat

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/ovn-kubernetes/libovsdb/mapper"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// OptionalTableProvider exposes tables that are not part of the fixed client
// model. It is intended for versioned or vendor-specific schema extensions;
// callers must check the schema before using the returned handle.
type OptionalTableProvider interface {
	TableProvider
	OptionalTable(string, model.Model) *OptionalTable
}

// OptionalTable is a schema-backed handle for a table that is not monitored by
// the regular libovsdb client model. Reads use select operations and writes use
// the same Database transaction policy as regular Table handles.
type OptionalTable struct {
	db        *Database
	tableName string
	prototype model.Model
}

var _ OptionalTableProvider = (*Database)(nil)

// OptionalTable returns a handle for a table that may be absent from a server
// schema. The handle reports a schema error when an operation is attempted.
func (d *Database) OptionalTable(tableName string, prototype model.Model) *OptionalTable {
	return &OptionalTable{db: d, tableName: tableName, prototype: prototype}
}

func (t *OptionalTable) ensure() (ovsdb.TableSchema, error) {
	if t == nil || t.db == nil || t.db.Client == nil {
		return ovsdb.TableSchema{}, errors.New("ovsdb optional table is nil")
	}
	if t.tableName == "" {
		return ovsdb.TableSchema{}, errors.New("ovsdb optional table name is empty")
	}
	if t.prototype == nil {
		return ovsdb.TableSchema{}, errors.New("ovsdb optional table prototype is nil")
	}
	table, ok := t.db.Schema().Tables[t.tableName]
	if !ok {
		return ovsdb.TableSchema{}, fmt.Errorf("ovsdb optional table %s is missing", t.tableName)
	}
	return table, nil
}

func (t *OptionalTable) mapperInfo(table ovsdb.TableSchema, value model.Model) (*mapper.Info, error) {
	if _, err := t.ensure(); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("ovsdb optional table model is nil")
	}
	info, err := mapper.NewInfo(t.tableName, &table, value)
	if err != nil {
		return nil, fmt.Errorf("build mapper for optional table %s: %w", t.tableName, err)
	}
	return info, nil
}

// List reads all rows with a select operation. Unlike Table.List, it does not
// require the table to be monitored in the client cache.
func (t *OptionalTable) List(ctx context.Context, result any) error {
	table, err := t.ensure()
	if err != nil {
		return err
	}
	if err := validateSliceResult(result); err != nil {
		return err
	}
	results, err := t.db.TransactResults(ctx, ovsdb.Operation{Op: ovsdb.OperationSelect, Table: t.tableName})
	if err != nil {
		return err
	}
	if len(results) != 1 {
		return fmt.Errorf("expected one select result for optional table %s, got %d", t.tableName, len(results))
	}
	return decodeOptionalRows(t.db.Schema(), t.tableName, table, results[0].Rows, result)
}

// CreateOps builds insert operations for the optional table.
func (t *OptionalTable) CreateOps(rows ...model.Model) ([]ovsdb.Operation, error) {
	table, err := t.ensure()
	if err != nil {
		return nil, err
	}
	mapperInstance := mapper.NewMapper(t.db.Schema())
	operations := make([]ovsdb.Operation, 0, len(rows))
	for _, row := range rows {
		info, err := t.mapperInfo(table, row)
		if err != nil {
			return nil, err
		}
		ovsRow, err := mapperInstance.NewRow(info)
		if err != nil {
			return nil, fmt.Errorf("build insert for optional table %s: %w", t.tableName, err)
		}
		operation := ovsdb.Operation{Op: ovsdb.OperationInsert, Table: t.tableName, Row: ovsRow}
		if uuid, err := info.FieldByColumn("_uuid"); err == nil {
			if namedUUID, ok := uuid.(string); ok && ovsdb.IsNamedUUID(namedUUID) {
				operation.UUIDName = namedUUID
			}
		}
		operations = append(operations, operation)
	}
	return operations, nil
}

// UpdateOps builds an update operation selected by the model UUID. Optional
// tables intentionally require UUID selectors because they do not have client
// indexes available to the dynamic handle.
func (t *OptionalTable) UpdateOps(selector, update model.Model, fields ...any) ([]ovsdb.Operation, error) {
	table, err := t.ensure()
	if err != nil {
		return nil, err
	}
	selectorInfo, err := t.mapperInfo(table, selector)
	if err != nil {
		return nil, err
	}
	updateInfo, err := t.mapperInfo(table, update)
	if err != nil {
		return nil, err
	}
	uuid, err := selectorInfo.FieldByColumn("_uuid")
	if err != nil {
		return nil, fmt.Errorf("optional table %s selector has no UUID: %w", t.tableName, err)
	}
	uuidString, ok := uuid.(string)
	if !ok || uuidString == "" {
		return nil, fmt.Errorf("optional table %s update requires a UUID selector", t.tableName)
	}
	row, err := mapper.NewMapper(t.db.Schema()).NewRow(updateInfo, fields...)
	if err != nil {
		return nil, fmt.Errorf("build update for optional table %s: %w", t.tableName, err)
	}
	return []ovsdb.Operation{{
		Op:    ovsdb.OperationUpdate,
		Table: t.tableName,
		Where: []ovsdb.Condition{{Column: "_uuid", Function: ovsdb.ConditionEqual, Value: ovsdb.UUID{GoUUID: uuidString}}},
		Row:   row,
	}}, nil
}

// DeleteOps builds delete operations selected by model UUIDs.
func (t *OptionalTable) DeleteOps(selectors ...model.Model) ([]ovsdb.Operation, error) {
	table, err := t.ensure()
	if err != nil {
		return nil, err
	}
	operations := make([]ovsdb.Operation, 0, len(selectors))
	for _, selector := range selectors {
		info, err := t.mapperInfo(table, selector)
		if err != nil {
			return nil, err
		}
		uuid, err := info.FieldByColumn("_uuid")
		if err != nil {
			return nil, fmt.Errorf("optional table %s selector has no UUID: %w", t.tableName, err)
		}
		uuidString, ok := uuid.(string)
		if !ok || uuidString == "" {
			return nil, fmt.Errorf("optional table %s delete requires a UUID selector", t.tableName)
		}
		operations = append(operations, ovsdb.Operation{
			Op:    ovsdb.OperationDelete,
			Table: t.tableName,
			Where: []ovsdb.Condition{{Column: "_uuid", Function: ovsdb.ConditionEqual, Value: ovsdb.UUID{GoUUID: uuidString}}},
		})
	}
	return operations, nil
}

// Transact submits operations through the enclosing Database policy layer.
func (t *OptionalTable) Transact(ctx context.Context, method string, operations ...ovsdb.Operation) error {
	if _, err := t.ensure(); err != nil {
		return err
	}
	_, err := t.db.transact(ctx, method, operations...)
	return err
}

func validateSliceResult(result any) error {
	value := reflect.ValueOf(result)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Slice {
		return errors.New("ovsdb optional table result must be a non-nil pointer to a slice")
	}
	return nil
}

func decodeOptionalRows(schema ovsdb.DatabaseSchema, tableName string, table ovsdb.TableSchema, rows []ovsdb.Row, result any) error {
	value := reflect.ValueOf(result).Elem()
	elementType := value.Type().Elem()
	mapperInstance := mapper.NewMapper(schema)
	for _, row := range rows {
		var modelValue reflect.Value
		if elementType.Kind() == reflect.Pointer {
			modelValue = reflect.New(elementType.Elem())
		} else {
			modelValue = reflect.New(elementType)
		}
		modelRow, ok := modelValue.Interface().(model.Model)
		if !ok {
			return fmt.Errorf("optional table %s result element is not a struct model", tableName)
		}
		info, err := mapper.NewInfo(tableName, &table, modelRow)
		if err != nil {
			return err
		}
		if err := mapperInstance.GetRowDataWithUUID(&row, info); err != nil {
			return fmt.Errorf("decode optional table %s row: %w", tableName, err)
		}
		if elementType.Kind() == reflect.Pointer {
			value.Set(reflect.Append(value, modelValue))
		} else {
			value.Set(reflect.Append(value, modelValue.Elem()))
		}
	}
	return nil
}
