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

type tableBackend struct {
	rows map[reflect.Type][]any
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

func (*tableBackend) Create(...model.Model) ([]ovsdb.Operation, error) { return nil, nil }

func (*tableBackend) Transact(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	return nil, nil
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

func (tableConditional) Mutate(model.Model, ...model.Mutation) ([]ovsdb.Operation, error) {
	return nil, nil
}

func (tableConditional) Update(model.Model, ...any) ([]ovsdb.Operation, error) {
	return nil, nil
}

func (tableConditional) Delete() ([]ovsdb.Operation, error) { return nil, nil }

func (tableConditional) Wait(ovsdb.WaitCondition, *int, model.Model, ...any) ([]ovsdb.Operation, error) {
	return nil, nil
}

func (tableConditional) Select(model.Model, ...any) ([]ovsdb.Operation, error) {
	return nil, nil
}
