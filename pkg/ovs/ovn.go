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

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/modelgen"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
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
	ovsDbClient
	aclSamplingMonitorMu sync.Mutex
	aclSamplingMonitored bool
}

type OVNSbClient struct {
	ovsDbClient
}

type ovsDbClient struct {
	client.Client
	Timeout time.Duration
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
	monitors := make([]client.MonitorOption, 0, len(tables))
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
		monitors = append(monitors, client.WithTable(model))
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
		Client:  nbClient,
		Timeout: time.Duration(ovnNbTimeout) * time.Second,
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

	monitors := []client.MonitorOption{
		client.WithTable(&ovnnb.ACL{}),
		client.WithTable(&ovnnb.AddressSet{}),
		client.WithTable(&ovnnb.BFD{}),
		client.WithTable(&ovnnb.DHCPOptions{}),
		client.WithTable(&ovnnb.GatewayChassis{}),
		client.WithTable(&ovnnb.HAChassis{}),
		client.WithTable(&ovnnb.HAChassisGroup{}),
		client.WithTable(&ovnnb.LoadBalancer{}),
		client.WithTable(&ovnnb.ChassisTemplateVar{}),
		client.WithTable(&ovnnb.LoadBalancerHealthCheck{}),
		client.WithTable(&ovnnb.LogicalRouterPolicy{}),
		client.WithTable(&ovnnb.LogicalRouterPort{}),
		client.WithTable(&ovnnb.LogicalRouterStaticRoute{}),
		client.WithTable(&ovnnb.LogicalRouter{}),
		client.WithTable(&ovnnb.LogicalSwitchPort{}),
		client.WithTable(&ovnnb.LogicalSwitch{}),
		client.WithTable(&ovnnb.NAT{}),
		client.WithTable(&ovnnb.NBGlobal{}),
		client.WithTable(&ovnnb.PortGroup{}),
		client.WithTable(&ovnnb.Meter{}),
		client.WithTable(&ovnnb.MeterBand{}),
	}

	try := 0
	var nbClient client.Client
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
		Client:  nbClient,
		Timeout: time.Duration(ovnNbTimeout) * time.Second,
	}
	return c, nil
}

func NewOvnSbClient(ovnSbAddr string, ovnSbTimeout, ovsDbConTimeout, ovsDbInactivityTimeout, maxRetry int) (*OVNSbClient, error) {
	dbModel, err := ovnsb.FullDatabaseModel()
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	monitors := []client.MonitorOption{
		client.WithTable(&ovnsb.Chassis{}),
	}
	try := 0
	var sbClient client.Client
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
		Client:  sbClient,
		Timeout: time.Duration(ovnSbTimeout) * time.Second,
	}
	return c, nil
}

// TODO: support ic-nb ic-sb client

func ConstructWaitForNameNotExistsOperation(name, table string) ovsdb.Operation {
	return ConstructWaitForUniqueOperation(table, "name", name)
}

// ConstructWaitForUniqueOperation builds a Wait that blocks when any row with
// the given column value exists.  The condition uses Until:"!=" with
// Rows:[{column:value}], so it passes immediately when zero rows match (good
// for insert) and times out when one or more matches are found.
//
// Production OVSDB servers project the matched rows onto the specified columns
// and deduplicate, so any positive number of same-name rows compares equal to
// the single-element Rows and the Wait correctly times out.
//
// NOTE: The libovsdb in-memory test server does not deduplicate projected rows
// the same way.  When two or more duplicate rows exist in the test server the
// result set has multiple elements, which differs from the single-element Rows
// and causes the Wait to pass.  Callers use a cache-level existence check
// before the transaction so that pre-existing rows (including duplicates) are
// handled without reaching the Wait.
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

func (c *ovsDbClient) Transact(method string, operations []ovsdb.Operation) error {
	_, _, err := c.transact(method, operations)
	return err
}

// TransactConditional executes a transaction that may include a Wait operation
// for conditional insert. It returns (true, nil) on success, (false, nil) when
// a Wait condition fails (indicating the row already exists), and (false, err)
// on real errors.
func (c *ovsDbClient) TransactConditional(method string, operations []ovsdb.Operation) (bool, error) {
	created, opErrs, err := c.transact(method, operations)
	if err != nil {
		for _, opErr := range opErrs {
			if _, ok := errors.AsType[*ovsdb.TimedOut](opErr); ok {
				klog.V(5).Infof("wait condition not met in %s transaction (row already exists), treating as idempotent success", method)
				return false, nil
			}
		}
		return false, err
	}
	return created, nil
}

// transact is the shared transaction implementation. It returns (true, nil, nil)
// on success, or (false, operationErrors, err) on failure.
func (c *ovsDbClient) transact(method string, operations []ovsdb.Operation) (bool, []ovsdb.OperationError, error) {
	if len(operations) == 0 {
		klog.V(6).Info("operations should not be empty")
		return true, nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	start := time.Now()
	results, err := c.Client.Transact(ctx, operations...)
	elapsed := float64(time.Since(start) / time.Millisecond)

	var dbType string
	switch c.Schema().Name {
	case ovnnb.DatabaseName:
		dbType = "ovn-nb"
	case ovnsb.DatabaseName:
		dbType = "ovn-sb"
	}

	code := "0"
	defer func() {
		ovsClientRequestLatency.WithLabelValues(dbType, method, code).Observe(elapsed)
	}()

	if err != nil {
		code = "1"
		klog.Errorf("error occurred in transact with %s operations: %+v in %vms", dbType, operations, elapsed)
		return false, nil, err
	}

	if elapsed > 500 {
		klog.Warningf("%s operations took too long: %+v in %vms", dbType, operations, elapsed)
	}

	opErrs, err := ovsdb.CheckOperationResults(results, operations)
	if err != nil {
		klog.Errorf("error occurred in transact with operations %+v with operation errors %+v: %v", operations, opErrs, err)
		return false, opErrs, err
	}

	return true, nil, nil
}

// GetEntityInfo get entity info by column which is the index,
// reference to ovn-nb.ovsschema(ovsdb-client get-schema unix:/var/run/ovn/ovnnb_db.sock OVN_Northbound) for more information,
// UUID is index
func (c *ovsDbClient) GetEntityInfo(entity any) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	entityPtr := reflect.ValueOf(entity)
	if entityPtr.Kind() != reflect.Pointer {
		return errors.New("entity must be pointer")
	}

	err := c.Get(ctx, entity)
	if err != nil {
		klog.Error(err)
		return err
	}

	return nil
}
