package ovn_ic_controller

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnicnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnicsb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

type icCapabilityProvider struct {
	gatewayCalls int
	patchCalls   int
}

func (p *icCapabilityProvider) Table(model.Model) compat.TableHandle { return nil }

func (p *icCapabilityProvider) ReconcileGatewayChassises(string, []string) error {
	p.gatewayCalls++
	return nil
}

func (p *icCapabilityProvider) CreateLogicalPatchPort(string, string, string, string, string, string, ...string) error {
	p.patchCalls++
	return nil
}

var _ compat.TableProvider = (*icCapabilityProvider)(nil)

func TestICOperationsUseTableProviderCapabilities(t *testing.T) {
	provider := &icCapabilityProvider{}
	controller := &Controller{OVNNbTables: provider}

	require.NoError(t, controller.reconcileICGatewayChassises("lrp", []string{"chassis"}))
	require.NoError(t, controller.createICLogicalPatchPort("ls", "lr", "lsp", "lrp", "10.0.0.1", "00:00:00:00:00:01"))
	require.Equal(t, 1, provider.gatewayCalls)
	require.Equal(t, 1, provider.patchCalls)
}

func TestICTransitSwitchReadsRequireTableProvider(t *testing.T) {
	controller := &Controller{}

	_, err := controller.listICTransitSwitches()
	require.EqualError(t, err, "IC NB table provider is nil")

	_, err = controller.getICTransitSwitchSubnet("ts-region1")
	require.EqualError(t, err, "IC NB table provider is nil")
}

func TestICTableProviderNBGlobalAndPortParentCleanup(t *testing.T) {
	backend := newICTableBackend(
		&ovnnb.NBGlobal{UUID: "global-1", Options: map[string]string{"stale": "value"}},
		&ovnnb.LogicalSwitch{UUID: "ls-1", Name: "ts-region1", Ports: []string{"lsp-1"}},
		&ovnnb.LogicalSwitchPort{UUID: "lsp-1", Name: "ts-region1-region2", ExternalIDs: map[string]string{"vendor": "kube-ovn"}},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{OVNNbTables: database}

	require.NoError(t, controller.setICAutoRouteTable(true, []string{"10.0.0.0/8"}))
	require.NoError(t, controller.deleteICLogicalSwitchPorts(func(row *ovnnb.LogicalSwitchPort) bool {
		return row.Name == "ts-region1-region2"
	}))
	require.Equal(t, 2, backend.transacts)
}

func TestICTableProviderICDatabaseOperations(t *testing.T) {
	backend := newICTableBackend(
		&ovnicnb.TransitSwitch{UUID: "ts-1", Name: "ts-region1", ExternalIDs: map[string]string{
			"vendor": "kube-ovn", "subnet": "10.1.0.0/16",
		}},
		&ovnicnb.TransitSwitch{UUID: "ts-2", Name: "other", ExternalIDs: map[string]string{"vendor": "other"}},
		&ovnicsb.AvailabilityZone{UUID: "az-1", Name: "region1"},
		&ovnicsb.Gateway{UUID: "gw-1", AvailabilityZone: "az-1"},
		&ovnicsb.Route{UUID: "route-1", AvailabilityZone: "az-1"},
		&ovnicsb.PortBinding{UUID: "pb-1", AvailabilityZone: "az-1"},
	)
	database := compat.NewDatabase(backend, time.Second, compat.RetryPolicy{})
	controller := &Controller{ICNbTables: database, ICSbTables: database}

	names, err := controller.listICTransitSwitches()
	require.NoError(t, err)
	require.Equal(t, []string{"ts-region1"}, names)

	subnet, err := controller.getICTransitSwitchSubnet("ts-region1")
	require.NoError(t, err)
	require.Equal(t, "10.1.0.0/16", subnet)

	require.NoError(t, controller.removeOldICChassisInSbDB("region1"))
	require.Equal(t, 1, backend.transacts)
}

type icTableBackend struct {
	rows        map[reflect.Type][]any
	conditional icConditional
	transacts   int
}

func newICTableBackend(rows ...any) *icTableBackend {
	backend := &icTableBackend{rows: make(map[reflect.Type][]any)}
	for _, row := range rows {
		typ := reflect.TypeOf(row)
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		backend.rows[typ] = append(backend.rows[typ], row)
	}
	backend.conditional.backend = backend
	return backend
}

func (b *icTableBackend) Get(_ context.Context, result model.Model) error {
	resultValue := reflect.ValueOf(result).Elem()
	for _, candidate := range b.rows[resultValue.Type()] {
		candidateValue := reflect.ValueOf(candidate)
		if candidateValue.Kind() == reflect.Pointer {
			candidateValue = candidateValue.Elem()
		}
		for _, field := range []string{"UUID", "Name"} {
			wanted, actual := resultValue.FieldByName(field), candidateValue.FieldByName(field)
			if wanted.IsValid() && actual.IsValid() && wanted.String() != "" && wanted.String() == actual.String() {
				resultValue.Set(candidateValue)
				return nil
			}
		}
	}
	return compat.ErrNotFound
}

func (b *icTableBackend) List(_ context.Context, result any) error {
	resultValue := reflect.ValueOf(result).Elem()
	rowType := resultValue.Type().Elem()
	if rowType.Kind() == reflect.Pointer {
		rowType = rowType.Elem()
	}
	for _, candidate := range b.rows[rowType] {
		candidateValue := reflect.ValueOf(candidate)
		if rowType.Kind() == reflect.Pointer {
			resultValue.Set(reflect.Append(resultValue, candidateValue))
			continue
		}
		if candidateValue.Kind() == reflect.Pointer {
			candidateValue = candidateValue.Elem()
		}
		resultValue.Set(reflect.Append(resultValue, candidateValue))
	}
	return nil
}

func (b *icTableBackend) WhereCache(predicate any) compat.ConditionalAPI {
	b.conditional.predicate = predicate
	return &b.conditional
}

func (b *icTableBackend) WhereCacheByUUIDs(any, ...string) compat.ConditionalAPI {
	return &b.conditional
}
func (b *icTableBackend) Where(...model.Model) compat.ConditionalAPI { return &b.conditional }
func (b *icTableBackend) WhereAny(model.Model, ...model.Condition) compat.ConditionalAPI {
	return &b.conditional
}

func (b *icTableBackend) WhereAll(model.Model, ...model.Condition) compat.ConditionalAPI {
	return &b.conditional
}

func (b *icTableBackend) Select(model.Model, ...any) ([]ovsdb.Operation, error) {
	return icTableOperation(), nil
}

func (b *icTableBackend) Create(...model.Model) ([]ovsdb.Operation, error) {
	return icTableOperation(), nil
}

func (b *icTableBackend) Transact(_ context.Context, operations ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	b.transacts++
	return make([]ovsdb.OperationResult, len(operations)), nil
}
func (b *icTableBackend) Cache() compat.Cache                                { return nil }
func (b *icTableBackend) Schema() ovsdb.DatabaseSchema                       { return ovsdb.DatabaseSchema{} }
func (b *icTableBackend) Connected() bool                                    { return true }
func (b *icTableBackend) NewMonitor(...compat.MonitorOption) *compat.Monitor { return nil }
func (b *icTableBackend) Monitor(context.Context, *compat.Monitor) (compat.MonitorCookie, error) {
	return compat.MonitorCookie{}, nil
}
func (b *icTableBackend) Echo(context.Context) error { return nil }
func (b *icTableBackend) Close()                     {}

type icConditional struct {
	backend   *icTableBackend
	predicate any
}

func (c *icConditional) List(_ context.Context, result any) error {
	resultValue := reflect.ValueOf(result).Elem()
	rowType := resultValue.Type().Elem()
	if rowType.Kind() == reflect.Pointer {
		rowType = rowType.Elem()
	}
	for _, candidate := range c.backend.rows[rowType] {
		if c.predicate != nil {
			candidateValue := reflect.ValueOf(candidate)
			if candidateValue.Kind() != reflect.Pointer {
				candidateValue = candidateValue.Addr()
			}
			matched := reflect.ValueOf(c.predicate).Call([]reflect.Value{candidateValue})[0].Bool()
			if !matched {
				continue
			}
		}
		candidateValue := reflect.ValueOf(candidate)
		if resultValue.Type().Elem().Kind() == reflect.Pointer {
			resultValue.Set(reflect.Append(resultValue, candidateValue))
		} else {
			if candidateValue.Kind() == reflect.Pointer {
				candidateValue = candidateValue.Elem()
			}
			resultValue.Set(reflect.Append(resultValue, candidateValue))
		}
	}
	return nil
}

func (c *icConditional) Mutate(model.Model, ...model.Mutation) ([]ovsdb.Operation, error) {
	return icTableOperation(), nil
}

func (c *icConditional) Update(model.Model, ...any) ([]ovsdb.Operation, error) {
	return icTableOperation(), nil
}
func (c *icConditional) Delete() ([]ovsdb.Operation, error) { return icTableOperation(), nil }
func (c *icConditional) Wait(ovsdb.WaitCondition, *int, model.Model, ...any) ([]ovsdb.Operation, error) {
	return icTableOperation(), nil
}

func (c *icConditional) Select(model.Model, ...any) ([]ovsdb.Operation, error) {
	return icTableOperation(), nil
}

func icTableOperation() []ovsdb.Operation {
	comment := "ic-table-test"
	return []ovsdb.Operation{{Op: ovsdb.OperationComment, Comment: &comment}}
}
