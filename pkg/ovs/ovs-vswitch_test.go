package ovs

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/modelgen"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

func TestNewVswitchClientWithLegacySchema(t *testing.T) {
	schema := vswitch.Schema()
	delete(schema.Tables[vswitch.MirrorTable].Columns, "filter")
	delete(schema.Tables[vswitch.FlowSampleCollectorSetTable].Columns, "local_group_id")

	dbModel, err := newDynamicDBModelFromSchema(vswitch.DatabaseName, schema)
	require.NoError(t, err)

	_, sock := newOVSDBServer(t, "legacy-vswitch", dbModel, schema)
	client, err := NewVswitchClient("unix:"+sock, 1, 1)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	_, err = client.ListBridge(false, nil)
	require.NoError(t, err)
}

func newDynamicDBModelFromSchema(dbName string, schema ovsdb.DatabaseSchema) (model.ClientDBModel, error) {
	models := make(map[string]model.Model, len(schema.Tables))
	for tableName, table := range schema.Tables {
		models[tableName] = newDynamicTestTableModel(table.Columns)
	}
	return model.NewClientDBModel(dbName, models)
}

func newDynamicTestTableModel(columns map[string]*ovsdb.ColumnSchema) model.Model {
	sortedColumns := append([]string{"_uuid"}, slices.Sorted(maps.Keys(columns))...)
	fields := make([]reflect.StructField, 0, len(sortedColumns))
	for _, column := range sortedColumns {
		columnSchema := &ovsdb.UUIDColumn
		if column != "_uuid" {
			columnSchema = columns[column]
		}
		fields = append(fields, reflect.StructField{
			Name: modelgen.FieldName(column),
			Type: ovsdb.NativeType(columnSchema),
			Tag:  reflect.StructTag(strings.Trim(modelgen.Tag(column), "`")),
		})
	}

	return reflect.New(reflect.StructOf(fields)).Interface().(model.Model)
}
