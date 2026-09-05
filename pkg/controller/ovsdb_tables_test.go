package controller

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
)

func TestMatchesExternalIDs(t *testing.T) {
	tests := []struct {
		name     string
		actual   map[string]string
		expected map[string]string
		want     bool
	}{
		{name: "empty selector", actual: map[string]string{"vendor": "kube-ovn"}, want: true},
		{name: "exact value", actual: map[string]string{"vendor": "kube-ovn"}, expected: map[string]string{"vendor": "kube-ovn"}, want: true},
		{name: "key only", actual: map[string]string{"node": "node-1"}, expected: map[string]string{"node": ""}, want: true},
		{name: "missing key", actual: map[string]string{}, expected: map[string]string{"node": ""}, want: false},
		{name: "different value", actual: map[string]string{"vendor": "other"}, expected: map[string]string{"vendor": "kube-ovn"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesExternalIDs(test.actual, test.expected); got != test.want {
				t.Fatalf("matchesExternalIDs(%v, %v) = %v, want %v", test.actual, test.expected, got, test.want)
			}
		})
	}
}

func TestControllerTableProviderReadsAssociatedRows(t *testing.T) {
	logicalRouter := &ovnnb.LogicalRouter{
		UUID:         "lr-1",
		Name:         "router-1",
		Policies:     []string{"policy-1"},
		Nat:          []string{"nat-1"},
		StaticRoutes: []string{"route-1"},
	}
	policy := &ovnnb.LogicalRouterPolicy{
		UUID:        "policy-1",
		Priority:    100,
		Match:       "ip4.src == 10.0.0.0/24",
		ExternalIDs: map[string]string{"vendor": "kube-ovn"},
	}
	route := &ovnnb.LogicalRouterStaticRoute{
		UUID:        "route-1",
		RouteTable:  "main",
		IPPrefix:    "10.1.0.0/16",
		Nexthop:     "10.0.0.1",
		ExternalIDs: map[string]string{"vendor": "kube-ovn"},
	}
	lb := &ovnnb.LoadBalancer{UUID: "lb-1", Name: "lb-1"}
	lbhc := &ovnnb.LoadBalancerHealthCheck{UUID: "lbhc-1", Vip: "10.0.0.1:80"}
	nat := &ovnnb.NAT{UUID: "nat-1", LogicalIP: "10.2.0.5", Type: ovnnb.NATTypeSNAT, ExternalIP: "192.0.2.5"}
	bfd := &ovnnb.BFD{UUID: "bfd-1", ExternalIDs: map[string]string{"owner": "egress"}}
	logicalSwitch := &ovnnb.LogicalSwitch{UUID: "ls-1", Name: "switch-1"}
	logicalSwitchPort := &ovnnb.LogicalSwitchPort{UUID: "lsp-1", Name: "port-1"}
	portGroup := &ovnnb.PortGroup{UUID: "pg-1", Name: "group-1"}
	chassis := &ovnsb.Chassis{
		UUID:        "chassis-1",
		Name:        "chassis-1",
		Hostname:    "node-1",
		ExternalIDs: map[string]string{"vendor": "kube-ovn"},
	}
	database := compat.NewDatabase(newTableBackend(logicalRouter, policy, route, lb, lbhc, nat, bfd, logicalSwitch, logicalSwitchPort, portGroup, chassis), time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database, OVNSbTables: database}

	policies, err := controller.listLogicalRouterPolicies("router-1", 100, map[string]string{"vendor": "kube-ovn"}, false)
	require.NoError(t, err)
	require.Len(t, policies, 1)
	require.Equal(t, "policy-1", policies[0].UUID)

	routes, err := controller.listLogicalRouterStaticRoutes("router-1", nil, nil, "", map[string]string{"vendor": "kube-ovn"})
	require.NoError(t, err)
	require.Len(t, routes, 1)
	require.Equal(t, "route-1", routes[0].UUID)

	loadBalancers, err := controller.listLoadBalancers(func(row *ovnnb.LoadBalancer) bool { return row.Name == "lb-1" })
	require.NoError(t, err)
	require.Len(t, loadBalancers, 1)
	foundLoadBalancer, err := controller.getLoadBalancer("lb-1", false)
	require.NoError(t, err)
	require.Equal(t, "lb-1", foundLoadBalancer.UUID)

	healthChecks, err := controller.listLoadBalancerHealthChecks(func(row *ovnnb.LoadBalancerHealthCheck) bool { return row.Vip == "10.0.0.1:80" })
	require.NoError(t, err)
	require.Len(t, healthChecks, 1)

	bfdRows, err := controller.findBFD(map[string]string{"owner": "egress"})
	require.NoError(t, err)
	require.Len(t, bfdRows, 1)

	exists, err := controller.natExists("router-1", ovnnb.NATTypeSNAT, "192.0.2.5", "10.2.0.5")
	require.NoError(t, err)
	require.True(t, exists)

	switchNames, err := controller.listLogicalSwitchNames(false, nil)
	require.NoError(t, err)
	require.Contains(t, switchNames, "switch-1")
	exists, err = controller.logicalSwitchPortExists("port-1")
	require.NoError(t, err)
	require.True(t, exists)
	exists, err = controller.portGroupExists("group-1")
	require.NoError(t, err)
	require.True(t, exists)

	foundChassis, err := controller.getChassisByHost("node-1")
	require.NoError(t, err)
	require.Equal(t, "chassis-1", foundChassis.Name)
	foundChassis, err = controller.getChassis("chassis-1", false)
	require.NoError(t, err)
	require.Equal(t, "node-1", foundChassis.Hostname)
	kubeOvnChassises, err := controller.listKubeOvnChassises()
	require.NoError(t, err)
	require.Len(t, kubeOvnChassises, 1)
}

func TestControllerTableProviderWrites(t *testing.T) {
	backend := newTableBackend()
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.createAddressSet("as-1", map[string]string{"owner": "test"}))
	require.NoError(t, controller.createPortGroup("pg-1", map[string]string{"owner": "test"}))
	require.NoError(t, controller.createLogicalRouter("lr-1"))
	require.NoError(t, controller.createLoadBalancer("lb-1", ovnnb.LoadBalancerProtocolTCP))

	require.Equal(t, 4, backend.createCalls)
	require.Equal(t, 4, backend.transactCalls)
}

func TestControllerTableProviderFieldUpdates(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.LoadBalancer{
			UUID:    "lb-1",
			Name:    "lb-1",
			Options: map[string]string{"owner": "test"},
		},
		&ovnnb.LogicalSwitchPort{
			UUID:        "lsp-1",
			Name:        "port-1",
			ExternalIDs: map[string]string{"owner": "test"},
		},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.setLoadBalancerAffinityTimeout("lb-1", 30))
	require.NoError(t, controller.setLoadBalancerPreferLocalBackend("lb-1", true))
	require.NoError(t, controller.setLoadBalancerCtFlush("lb-1", true))
	require.NoError(t, controller.setLogicalSwitchPortExternalIDs("port-1", map[string]string{"managed": "true"}))
	require.NoError(t, controller.setLogicalSwitchPortVlanTag("port-1", 100))
	require.Equal(t, 5, backend.updateCalls)
	require.Equal(t, 5, backend.transactCalls)

	require.Error(t, controller.setLogicalSwitchPortVlanTag("port-1", 4096))
	require.Equal(t, 5, backend.updateCalls)
	require.Equal(t, 5, backend.transactCalls)
}

func TestControllerTableProviderCollectionUpdates(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.AddressSet{UUID: "as-1", Name: "as-1", Addresses: []string{"10.0.0.0/24"}},
		&ovnnb.PortGroup{UUID: "pg-1", Name: "pg-1", Ports: []string{"lsp-old"}},
		&ovnnb.LogicalSwitchPort{UUID: "lsp-1", Name: "port-1"},
		&ovnnb.LogicalSwitchPort{UUID: "lsp-2", Name: "port-2"},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	// Equivalent CIDRs are normalized and do not result in a transaction.
	require.NoError(t, controller.updateAddressSetAddresses("as-1", "10.0.0.1/24", "10.0.0.0/24"))
	require.Equal(t, 0, backend.updateCalls)
	require.NoError(t, controller.updateAddressSetAddresses("as-1", "10.0.0.0/24", "10.0.1.0/24"))
	require.Equal(t, 1, backend.updateCalls)

	require.NoError(t, controller.setPortGroupPorts("pg-1", []string{"port-1"}))
	require.NoError(t, controller.updatePortGroupPorts("pg-1", ovsdb.MutateOperationInsert, "port-2"))
	require.Equal(t, 2, backend.mutateCalls)
	require.Equal(t, 3, backend.transactCalls)
}

func TestControllerTableProviderRelationUpdates(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.LogicalSwitch{UUID: "ls-1", Name: "ls-1"},
		&ovnnb.LogicalRouter{UUID: "lr-1", Name: "lr-1"},
		&ovnnb.LoadBalancer{UUID: "lb-1", Name: "lb-1", ExternalIDs: map[string]string{"owner": "test"}},
		&ovnnb.LogicalSwitchPort{UUID: "lsp-1", Name: "port-1"},
		&ovnnb.PortGroup{UUID: "pg-1", Name: "pg-1", Ports: []string{"lsp-1"}},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.updateLogicalSwitchLoadBalancers("ls-1", ovsdb.MutateOperationInsert, "lb-1"))
	require.NoError(t, controller.updateLogicalRouterLoadBalancers("lr-1", ovsdb.MutateOperationInsert, "lb-1"))
	require.NoError(t, controller.updateLogicalSwitchOtherConfig("ls-1", ovsdb.MutateOperationInsert, map[string]string{"mcast_snoop": "true"}))
	require.NoError(t, controller.setLoadBalancerExternalTrafficLocal("lb-1", "10.0.0.10:80", "node-worker-1"))
	require.NoError(t, controller.removePortFromPortGroups("port-1"))

	require.Equal(t, 5, backend.mutateCalls)
	require.Equal(t, 5, backend.transactCalls)
}

func TestControllerTableProviderNBGlobalUpdates(t *testing.T) {
	backend := newTableBackend(&ovnnb.NBGlobal{
		UUID:    "nb-global-1",
		Options: map[string]string{"stale": "value"},
	})
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	legacyCalled := false
	legacy := func() error {
		legacyCalled = true
		return nil
	}
	require.NoError(t, controller.setNBGlobalOption("node_local_dns_ip", "10.96.0.10", true, legacy))
	require.NoError(t, controller.setNBGlobalOption("stale", "", false, legacy))
	require.NoError(t, controller.setNBGlobalIPSec(true, legacy))
	require.False(t, legacyCalled)
	require.Equal(t, 3, backend.updateCalls)
	require.Equal(t, 3, backend.transactCalls)
}

func TestControllerTableProviderParentReferenceDeletes(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.LogicalRouter{
			UUID:     "lr-1",
			Name:     "lr-1",
			Policies: []string{"policy-1", "policy-2"},
			Nat:      []string{"nat-1", "nat-2"},
		},
		&ovnnb.LogicalRouterPolicy{
			UUID:        "policy-1",
			Priority:    100,
			Nexthop:     new("10.0.0.1"),
			ExternalIDs: map[string]string{"owner": "test"},
		},
		&ovnnb.LogicalRouterPolicy{
			UUID:        "policy-2",
			Priority:    200,
			Nexthops:    []string{"10.0.0.2"},
			ExternalIDs: map[string]string{"owner": "test"},
		},
		&ovnnb.NAT{UUID: "nat-1", Type: ovnnb.NATTypeSNAT, ExternalIP: "192.0.2.1", LogicalIP: "10.0.0.1"},
		&ovnnb.NAT{UUID: "nat-2", Type: ovnnb.NATTypeSNAT, ExternalIP: "192.0.2.2", LogicalIP: "10.0.0.2"},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.deleteLogicalRouterPolicyByUUID("lr-1", "policy-1"))
	require.NoError(t, controller.deleteLogicalRouterPolicies("lr-1", 200, map[string]string{"owner": "test"}))
	require.NoError(t, controller.deleteLogicalRouterPolicyByNexthop("lr-1", 100, "10.0.0.1"))
	require.NoError(t, controller.deleteNats("lr-1", ovnnb.NATTypeSNAT, "10.0.0.1"))
	require.NoError(t, controller.deleteNat("lr-1", ovnnb.NATTypeSNAT, "192.0.2.2", "10.0.0.2"))

	require.Equal(t, 5, backend.mutateCalls)
	require.Equal(t, 5, backend.transactCalls)
}

func TestControllerTableProviderPortDeletes(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.LogicalSwitch{UUID: "ls-1", Name: "ls-1", Ports: []string{"lsp-1"}},
		&ovnnb.LogicalSwitchPort{
			UUID:        "lsp-1",
			Name:        "lsp-1",
			ExternalIDs: map[string]string{"ls": "ls-1"},
		},
		&ovnnb.DHCPOptions{
			UUID:        "dhcp-1",
			ExternalIDs: map[string]string{"port": "lsp-1"},
		},
		&ovnnb.LogicalRouter{UUID: "lr-1", Name: "lr-1", Ports: []string{"lrp-1"}},
		&ovnnb.LogicalRouterPort{
			UUID:        "lrp-1",
			Name:        "lrp-1",
			ExternalIDs: map[string]string{"lr": "lr-1"},
		},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.deleteLogicalSwitchPort("lsp-1"))
	require.NoError(t, controller.deleteLogicalRouterPort("lrp-1"))
	require.Equal(t, 2, backend.mutateCalls)
	require.Equal(t, 2, backend.transactCalls)
}

func TestControllerTableProviderHAChassisGroup(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.HAChassisGroup{
			UUID:        "group-1",
			Name:        "bfd-vpc-1",
			HaChassis:   []string{"ha-1"},
			ExternalIDs: map[string]string{"vendor": "kube-ovn"},
		},
		&ovnnb.HAChassis{UUID: "ha-1", ChassisName: "node-old", Priority: 100},
		&ovnnb.LogicalRouterPort{UUID: "lrp-1", Name: "bfd-vpc-1"},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.createHAChassisGroup("bfd-vpc-1", []string{"node-new"}, map[string]string{"lrp": "bfd-vpc-1"}))
	require.NoError(t, controller.setLogicalRouterPortHAChassisGroup("bfd-vpc-1", "bfd-vpc-1"))
	require.NoError(t, controller.deleteHAChassisGroup("bfd-vpc-1"))

	require.Equal(t, 1, backend.createCalls)
	require.Equal(t, 2, backend.updateCalls)
	require.Equal(t, 2, backend.mutateCalls)
	require.Equal(t, 2, backend.transactCalls)
}

func TestControllerTableProviderMeterDelete(t *testing.T) {
	backend := newTableBackend(
		&ovnnb.Meter{UUID: "meter-1", Name: "meter-1", Bands: []string{"band-1", "band-2"}},
		&ovnnb.MeterBand{UUID: "band-1"},
		&ovnnb.MeterBand{UUID: "band-2"},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.deleteMeter("meter-1"))
	require.NoError(t, controller.deleteMeter("missing-meter"))
}

type tableBackend struct {
	rows          map[reflect.Type][]any
	createCalls   int
	mutateCalls   int
	updateCalls   int
	transactCalls int
}

func newTableBackend(rows ...any) *tableBackend {
	backend := &tableBackend{rows: make(map[reflect.Type][]any)}
	for _, row := range rows {
		value := reflect.ValueOf(row)
		typ := value.Type()
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		backend.rows[typ] = append(backend.rows[typ], row)
	}
	return backend
}

func (b *tableBackend) Get(_ context.Context, result model.Model) error {
	resultValue := reflect.ValueOf(result)
	rows := b.rows[resultValue.Elem().Type()]
	for _, candidate := range rows {
		candidateValue := reflect.ValueOf(candidate)
		if candidateValue.Kind() == reflect.Pointer {
			candidateValue = candidateValue.Elem()
		}
		for _, field := range []string{"UUID", "Name"} {
			wanted := resultValue.Elem().FieldByName(field)
			actual := candidateValue.FieldByName(field)
			if wanted.IsValid() && actual.IsValid() && wanted.Kind() == reflect.String && wanted.String() != "" && wanted.String() == actual.String() {
				resultValue.Elem().Set(candidateValue)
				return nil
			}
		}
	}
	return compat.ErrNotFound
}

func (b *tableBackend) List(_ context.Context, result any) error {
	resultValue := reflect.ValueOf(result).Elem()
	for _, candidate := range b.rows[resultValue.Type().Elem()] {
		appendTableRow(resultValue, reflect.ValueOf(candidate))
	}
	return nil
}

func (b *tableBackend) WhereCache(predicate any) compat.ConditionalAPI {
	return tableConditional{backend: b, predicate: predicate}
}

func (b *tableBackend) WhereCacheByUUIDs(predicate any, _ ...string) compat.ConditionalAPI {
	return b.WhereCache(predicate)
}

func (b *tableBackend) Where(...model.Model) compat.ConditionalAPI {
	return tableConditional{backend: b}
}

func (b *tableBackend) WhereAny(model.Model, ...model.Condition) compat.ConditionalAPI {
	return tableConditional{backend: b}
}

func (b *tableBackend) WhereAll(model.Model, ...model.Condition) compat.ConditionalAPI {
	return tableConditional{backend: b}
}

func (*tableBackend) Select(model.Model, ...any) ([]ovsdb.Operation, error) {
	return nil, nil
}

func (b *tableBackend) Create(...model.Model) ([]ovsdb.Operation, error) {
	b.createCalls++
	return []ovsdb.Operation{{Op: ovsdb.OperationComment, Comment: new("table")}}, nil
}

func (b *tableBackend) Transact(_ context.Context, operations ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	b.transactCalls++
	return make([]ovsdb.OperationResult, len(operations)), nil
}

func (*tableBackend) Cache() compat.Cache { return tableCache{} }

func (*tableBackend) Schema() ovsdb.DatabaseSchema { return ovsdb.DatabaseSchema{} }

func (*tableBackend) Connected() bool { return true }

func (*tableBackend) NewMonitor(...compat.MonitorOption) *compat.Monitor { return nil }

func (*tableBackend) Monitor(context.Context, *compat.Monitor) (compat.MonitorCookie, error) {
	return compat.MonitorCookie{}, nil
}

func (*tableBackend) Echo(context.Context) error { return nil }

func (*tableBackend) Close() {}

type tableCache struct{}

func (tableCache) AddEventHandler(compat.EventHandler) {}

type tableConditional struct {
	backend   *tableBackend
	predicate any
}

func (c tableConditional) List(_ context.Context, result any) error {
	resultValue := reflect.ValueOf(result).Elem()
	var rows []any
	if c.predicate != nil {
		predicateType := reflect.TypeOf(c.predicate)
		rows = c.backend.rows[predicateType.In(0).Elem()]
	} else {
		rows = c.backend.rows[resultValue.Type().Elem()]
	}
	for _, candidate := range rows {
		candidateValue := reflect.ValueOf(candidate)
		if c.predicate != nil && !reflect.ValueOf(c.predicate).Call([]reflect.Value{candidateValue})[0].Bool() {
			continue
		}
		appendTableRow(resultValue, candidateValue)
	}
	return nil
}

func appendTableRow(destination, candidate reflect.Value) {
	if candidate.Type().AssignableTo(destination.Type().Elem()) {
		destination.Set(reflect.Append(destination, candidate))
		return
	}
	if candidate.Kind() == reflect.Pointer && candidate.Elem().Type().AssignableTo(destination.Type().Elem()) {
		destination.Set(reflect.Append(destination, candidate.Elem()))
		return
	}
	if destination.Type().Elem().Kind() == reflect.Pointer && candidate.Type().AssignableTo(destination.Type().Elem()) {
		destination.Set(reflect.Append(destination, candidate))
	}
}

func (c tableConditional) Mutate(model.Model, ...model.Mutation) ([]ovsdb.Operation, error) {
	c.backend.mutateCalls++
	return []ovsdb.Operation{{Op: ovsdb.OperationComment, Comment: new("table")}}, nil
}

func (c tableConditional) Update(model.Model, ...any) ([]ovsdb.Operation, error) {
	c.backend.updateCalls++
	return []ovsdb.Operation{{Op: ovsdb.OperationComment, Comment: new("table")}}, nil
}

func (tableConditional) Delete() ([]ovsdb.Operation, error) { return nil, nil }

func (tableConditional) Wait(ovsdb.WaitCondition, *int, model.Model, ...any) ([]ovsdb.Operation, error) {
	return nil, nil
}

func (tableConditional) Select(model.Model, ...any) ([]ovsdb.Operation, error) {
	return nil, nil
}
