package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/modelgen"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnicnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnicsb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
)

// LegacyClient is the legacy ovn client
type LegacyClient struct {
	OvnTimeout     int
	OvnICNbAddress string
	OvnICSbAddress string
}

type OVNNbClient struct {
	*table.Database
	aclSamplingMonitorMu sync.Mutex
	aclSamplingMonitored bool
}

type OVNSbClient struct {
	*table.Database
}

// OVNICNbClient is the generic IC northbound database client.
type OVNICNbClient struct {
	*table.Database
}

// OVNICSbClient is the generic IC southbound database client.
type OVNICSbClient struct {
	*table.Database
}

var (
	_ NbClient             = (*OVNNbClient)(nil)
	_ SbClient             = (*OVNSbClient)(nil)
	_ table.TableProvider = (*OVNNbClient)(nil)
	_ table.TableProvider = (*OVNSbClient)(nil)
	_ table.TableProvider = (*OVNICNbClient)(nil)
	_ table.TableProvider = (*OVNICSbClient)(nil)
)

type ovsTransactionObserver struct{}

func (ovsTransactionObserver) ObserveTransaction(event table.TransactionEvent) {
	elapsed := float64(event.Duration / time.Millisecond)
	code := "0"
	if event.Err != nil {
		code = "1"
		klog.Errorf("error occurred in transact with %s operations: %+v in %vms", event.Database, event.Operations, elapsed)
	} else if elapsed > 500 {
		klog.Warningf("%s operations took too long: %+v in %vms", event.Database, event.Operations, elapsed)
	}
	ovsClientRequestLatency.WithLabelValues(event.Database, event.Method, code).Observe(elapsed)
}

const (
	OVNIcNbCtl = "ovn-ic-nbctl"
	OVNIcSbCtl = "ovn-ic-sbctl"
	MayExist   = "--may-exist"
	IfExists   = "--if-exists"

	OVSDBWaitTimeout = 0

	ExternalIDVendor           = "vendor"
	ExternalIDVpcEgressGateway = "vpc-egress-gateway"
	ExternalIDVpcNatGateway    = "vpc-nat-gateway"
)

// NewLegacyClient init a legacy ovn client
func NewLegacyClient(timeout int) *LegacyClient {
	return &LegacyClient{
		OvnTimeout: timeout,
	}
}

func NewDynamicOvnNbClient(
	ovnNbAddr string,
	ovnNbTimeout, ovsDbConTimeout, ovsDbInactivityTimeout int,
	tables ...string,
) (*OVNNbClient, map[string]model.Model, error) {
	dbModel, err := model.NewClientDBModel(ovnnb.DatabaseName, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create client db model: %w", err)
	}

	nbClient, err := ovsclient.NewOvsDbClient(
		ovnnb.DatabaseName,
		ovnNbAddr,
		dbModel,
		nil,
		ovsDbConTimeout,
		ovsDbInactivityTimeout,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create initial ovsdb client to fetch schema: %w", err)
	}

	schemaTables := nbClient.Schema().Tables
	nbClient.Close()

	models := make(map[string]model.Model, len(tables))
	monitors := make([]table.MonitorOption, 0, len(tables))
	for name, schemaTable := range schemaTables {
		if len(tables) != 0 && !slices.Contains(tables, name) {
			continue
		}

		columns := maps.Clone(schemaTable.Columns)
		keys := slices.Collect(maps.Keys(columns))
		slices.Sort(keys)
		sortedColumns := slices.Insert(keys, 0, "_uuid")
		columns["_uuid"] = &ovsdb.UUIDColumn

		fields := make([]reflect.StructField, 0, len(columns))
		for column := range slices.Values(sortedColumns) {
			fields = append(fields, reflect.StructField{
				Name: modelgen.FieldName(column),
				Type: ovsdb.NativeType(columns[column]),
				Tag:  reflect.StructTag(strings.Trim(modelgen.Tag(column), "`")),
			})
		}

		model := reflect.New(reflect.StructOf(fields)).Interface().(model.Model)
		monitors = append(monitors, table.WithTable(model))
		models[name] = model
	}

	if dbModel, err = model.NewClientDBModel(ovnnb.DatabaseName, models); err != nil {
		return nil, nil, fmt.Errorf("failed to create dynamic client db model: %w", err)
	}

	if nbClient, err = ovsclient.NewOvsDbClient(
		ovnnb.DatabaseName,
		ovnNbAddr,
		dbModel,
		monitors,
		ovsDbConTimeout,
		ovsDbInactivityTimeout,
	); err != nil {
		return nil, nil, fmt.Errorf("failed to create dynamic ovsdb client: %w", err)
	}

	return &OVNNbClient{Database: newObservedDatabase(nbClient, ovnNbTimeout, "ovn-nb")}, models, nil
}

func NewOvnNbClient(ovnNbAddr string, ovnNbTimeout, ovsDbConTimeout, ovsDbInactivityTimeout, maxRetry int) (*OVNNbClient, error) {
	dbModel, err := ovnnb.FullDatabaseModel()
	if err != nil {
		return nil, logErr(err)
	}

	dbModel.SetIndexes(map[string][]model.ClientIndex{
		ovnnb.LogicalRouterPolicyTable: {
			{Columns: []model.ColumnKey{{Column: "match"}, {Column: "priority"}}},
			{Columns: []model.ColumnKey{{Column: "priority"}}},
			{Columns: []model.ColumnKey{{Column: "match"}}},
		},
	})
	klog.Infof("ovn nb table %s client index %#v", ovnnb.LogicalRouterPolicyTable, dbModel.Indexes(ovnnb.LogicalRouterPolicyTable))

	monitors := []table.MonitorOption{
		table.WithTable(&ovnnb.ACL{}),
		table.WithTable(&ovnnb.AddressSet{}),
		table.WithTable(&ovnnb.BFD{}),
		table.WithTable(&ovnnb.DHCPOptions{}),
		table.WithTable(&ovnnb.GatewayChassis{}),
		table.WithTable(&ovnnb.HAChassis{}),
		table.WithTable(&ovnnb.HAChassisGroup{}),
		table.WithTable(&ovnnb.LoadBalancer{}),
		table.WithTable(&ovnnb.LoadBalancerHealthCheck{}),
		table.WithTable(&ovnnb.LogicalRouterPolicy{}),
		table.WithTable(&ovnnb.LogicalRouterPort{}),
		table.WithTable(&ovnnb.LogicalRouterStaticRoute{}),
		table.WithTable(&ovnnb.LogicalRouter{}),
		table.WithTable(&ovnnb.LogicalSwitchPort{}),
		table.WithTable(&ovnnb.LogicalSwitch{}),
		table.WithTable(&ovnnb.NAT{}),
		table.WithTable(&ovnnb.NBGlobal{}),
		table.WithTable(&ovnnb.PortGroup{}),
		table.WithTable(&ovnnb.Meter{}),
		table.WithTable(&ovnnb.MeterBand{}),
	}

	nbClient, err := connectOvsdb(ovnnb.DatabaseName, ovnNbAddr, dbModel, monitors, ovsDbConTimeout, ovsDbInactivityTimeout, maxRetry, "OVN NB")
	if err != nil {
		return nil, err
	}
	return &OVNNbClient{Database: newObservedDatabase(nbClient, ovnNbTimeout, "ovn-nb")}, nil
}

func NewOvnSbClient(ovnSbAddr string, ovnSbTimeout, ovsDbConTimeout, ovsDbInactivityTimeout, maxRetry int) (*OVNSbClient, error) {
	dbModel, err := ovnsb.FullDatabaseModel()
	if err != nil {
		return nil, logErr(err)
	}

	monitors := []table.MonitorOption{
		table.WithTable(&ovnsb.Chassis{}),
		table.WithTable(&ovnsb.PortBinding{}),
	}
	sbClient, err := connectOvsdb(ovnsb.DatabaseName, ovnSbAddr, dbModel, monitors, ovsDbConTimeout, ovsDbInactivityTimeout, maxRetry, "OVN SB")
	if err != nil {
		return nil, err
	}
	return &OVNSbClient{Database: newObservedDatabase(sbClient, ovnSbTimeout, "ovn-sb")}, nil
}

func NewOvnICNbClient(ovnICNbAddr string, timeout, ovsDbConTimeout, ovsDbInactivityTimeout, maxRetry int) (*OVNICNbClient, error) {
	dbModel, err := ovnicnb.FullDatabaseModel()
	if err != nil {
		return nil, err
	}
	monitors := []table.MonitorOption{
		table.WithTable(&ovnicnb.TransitSwitch{}),
	}
	backend, err := connectOvsdb(ovnicnb.DatabaseName, ovnICNbAddr, dbModel, monitors, ovsDbConTimeout, ovsDbInactivityTimeout, maxRetry, "OVN IC NB")
	if err != nil {
		return nil, err
	}
	return &OVNICNbClient{Database: newObservedDatabase(backend, timeout, "ovn-ic-nb")}, nil
}

func NewOvnICSbClient(ovnICSbAddr string, timeout, ovsDbConTimeout, ovsDbInactivityTimeout, maxRetry int) (*OVNICSbClient, error) {
	dbModel, err := ovnicsb.FullDatabaseModel()
	if err != nil {
		return nil, err
	}
	monitors := []table.MonitorOption{
		table.WithTable(&ovnicsb.AvailabilityZone{}),
		table.WithTable(&ovnicsb.Gateway{}),
		table.WithTable(&ovnicsb.Route{}),
		table.WithTable(&ovnicsb.PortBinding{}),
	}
	backend, err := connectOvsdb(ovnicsb.DatabaseName, ovnICSbAddr, dbModel, monitors, ovsDbConTimeout, ovsDbInactivityTimeout, maxRetry, "OVN IC SB")
	if err != nil {
		return nil, err
	}
	return &OVNICSbClient{Database: newObservedDatabase(backend, timeout, "ovn-ic-sb")}, nil
}

func ConstructWaitForNameNotExistsOperation(name, table string) ovsdb.Operation {
	return ConstructWaitForUniqueOperation(table, "name", name)
}

func ConstructWaitForUniqueOperation(table, column string, value any) ovsdb.Operation {
	timeout := OVSDBWaitTimeout
	return ovsdb.Operation{
		Op:      ovsdb.OperationWait,
		Table:   table,
		Timeout: &timeout,
		Where:   []ovsdb.Condition{{Column: column, Function: ovsdb.ConditionEqual, Value: value}},
		Columns: []string{column},
		Until:   string(ovsdb.WaitConditionNotEqual),
		Rows:    []ovsdb.Row{{column: value}},
	}
}

// ListDynamic lists rows using the runtime model returned by the dynamic NB
// client. It is intended for schema-aware tooling, not regular resource code.
func (c *OVNNbClient) ListDynamic(ctx context.Context, result any) error {
	if c.Database == nil {
		return errors.New("ovsdb database is nil")
	}
	return c.List(ctx, result)
}
