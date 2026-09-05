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
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
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
	*compat.Database
	aclSamplingMonitorMu sync.Mutex
	aclSamplingMonitored bool
}

type OVNSbClient struct {
	*compat.Database
}

var (
	_ NbClient = (*OVNNbClient)(nil)
	_ SbClient = (*OVNSbClient)(nil)
)

type ovsTransactionObserver struct{}

func (ovsTransactionObserver) ObserveTransaction(event compat.TransactionEvent) {
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
	OvsVsCtl   = "ovs-vsctl"
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
	monitors := make([]compat.MonitorOption, 0, len(tables))
	for name, table := range schemaTables {
		if len(tables) != 0 && !slices.Contains(tables, name) {
			continue
		}

		columns := maps.Clone(table.Columns)
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
		monitors = append(monitors, compat.WithTable(model))
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

	c := &OVNNbClient{
		Database: compat.NewDatabase(nbClient, time.Duration(ovnNbTimeout)*time.Second, compat.RetryPolicy{},
			compat.WithDatabaseName("ovn-nb"), compat.WithTransactionObserver(ovsTransactionObserver{})),
	}
	return c, models, nil
}

func NewOvnNbClient(ovnNbAddr string, ovnNbTimeout, ovsDbConTimeout, ovsDbInactivityTimeout, maxRetry int) (*OVNNbClient, error) {
	dbModel, err := ovnnb.FullDatabaseModel()
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	dbModel.SetIndexes(map[string][]model.ClientIndex{
		ovnnb.LogicalRouterPolicyTable: {
			{Columns: []model.ColumnKey{{Column: "match"}, {Column: "priority"}}},
			{Columns: []model.ColumnKey{{Column: "priority"}}},
			{Columns: []model.ColumnKey{{Column: "match"}}},
		},
	})
	klog.Infof("ovn nb table %s client index %#v", ovnnb.LogicalRouterPolicyTable, dbModel.Indexes(ovnnb.LogicalRouterPolicyTable))

	monitors := []compat.MonitorOption{
		compat.WithTable(&ovnnb.ACL{}),
		compat.WithTable(&ovnnb.AddressSet{}),
		compat.WithTable(&ovnnb.BFD{}),
		compat.WithTable(&ovnnb.DHCPOptions{}),
		compat.WithTable(&ovnnb.GatewayChassis{}),
		compat.WithTable(&ovnnb.HAChassis{}),
		compat.WithTable(&ovnnb.HAChassisGroup{}),
		compat.WithTable(&ovnnb.LoadBalancer{}),
		compat.WithTable(&ovnnb.LoadBalancerHealthCheck{}),
		compat.WithTable(&ovnnb.LogicalRouterPolicy{}),
		compat.WithTable(&ovnnb.LogicalRouterPort{}),
		compat.WithTable(&ovnnb.LogicalRouterStaticRoute{}),
		compat.WithTable(&ovnnb.LogicalRouter{}),
		compat.WithTable(&ovnnb.LogicalSwitchPort{}),
		compat.WithTable(&ovnnb.LogicalSwitch{}),
		compat.WithTable(&ovnnb.NAT{}),
		compat.WithTable(&ovnnb.NBGlobal{}),
		compat.WithTable(&ovnnb.PortGroup{}),
		compat.WithTable(&ovnnb.Meter{}),
		compat.WithTable(&ovnnb.MeterBand{}),
	}

	try := 0
	var nbClient compat.Backend
	for {
		nbClient, err = ovsclient.NewOvsDbClient(
			ovnnb.DatabaseName,
			ovnNbAddr,
			dbModel,
			monitors,
			ovsDbConTimeout,
			ovsDbInactivityTimeout,
		)
		if err != nil {
			klog.Errorf("failed to create OVN NB client: %v", err)
		} else {
			break
		}
		if try >= maxRetry {
			return nil, err
		}
		time.Sleep(2 * time.Second)
		try++
	}

	c := &OVNNbClient{
		Database: compat.NewDatabase(nbClient, time.Duration(ovnNbTimeout)*time.Second, compat.RetryPolicy{},
			compat.WithDatabaseName("ovn-nb"), compat.WithTransactionObserver(ovsTransactionObserver{})),
	}
	return c, nil
}

func NewOvnSbClient(ovnSbAddr string, ovnSbTimeout, ovsDbConTimeout, ovsDbInactivityTimeout, maxRetry int) (*OVNSbClient, error) {
	dbModel, err := ovnsb.FullDatabaseModel()
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	monitors := []compat.MonitorOption{
		compat.WithTable(&ovnsb.Chassis{}),
	}
	try := 0
	var sbClient compat.Backend
	for {
		sbClient, err = ovsclient.NewOvsDbClient(
			ovnsb.DatabaseName,
			ovnSbAddr,
			dbModel,
			monitors,
			ovsDbConTimeout,
			ovsDbInactivityTimeout,
		)
		if err != nil {
			klog.Errorf("failed to create OVN SB client: %v", err)
		} else {
			break
		}
		if try >= maxRetry {
			return nil, err
		}
		time.Sleep(2 * time.Second)
		try++
	}

	c := &OVNSbClient{
		Database: compat.NewDatabase(sbClient, time.Duration(ovnSbTimeout)*time.Second, compat.RetryPolicy{},
			compat.WithDatabaseName("ovn-sb"), compat.WithTransactionObserver(ovsTransactionObserver{})),
	}
	return c, nil
}

// TODO: support ic-nb ic-sb client

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
