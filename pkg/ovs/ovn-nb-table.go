package ovs

import (
	"context"
	"errors"
	"fmt"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

// namedTable centralizes the cache-backed operations shared by named OVN NB
// tables. The model-specific methods remain in their logical resource files,
// while this type owns the repeated lookup, list, create, and mutation flow.
type namedStore interface {
	table.TableProvider
	table.Executor
}

type namedTable[T any] struct {
	store         *table.Database
	prototype     model.Model
	kind          string
	listErrorVerb string
	nameOf        func(*T) string
	externalIDsOf func(*T) map[string]string
	newByName     func(string) *T
}

func (t namedTable[T]) table() table.TableHandle {
	return t.store.Table(t.prototype)
}

func (t namedTable[T]) listError(format string, args ...any) error {
	verb := t.listErrorVerb
	if verb == "" {
		verb = "list"
	}
	return fmt.Errorf(verb+" "+format, args...)
}

func (t namedTable[T]) get(name string, ignoreNotFound bool) (*T, error) {
	rows, err := t.store.Filter(context.Background(), t.prototype, func(row *T) bool {
		return t.nameOf(row) == name
	})
	if err != nil {
		return nil, t.listError("%s %q: %w", t.kind, name, err)
	}
	return table.UniqueByName(rows, name, t.kind, ignoreNotFound)
}

func (t namedTable[T]) list(needVendorFilter bool, filter func(*T) bool) ([]T, error) {
	rows, err := t.store.Filter(context.Background(), t.prototype, func(row *T) bool {
		if needVendorFilter && !hasVendor(t.externalIDsOf(row)) {
			return false
		}
		return filter == nil || filter(row)
	})
	if err != nil {
		return nil, t.listError("%s: %w", t.kind, err)
	}
	return rows, nil
}

func (t namedTable[T]) names(rows []T) []string {
	return fieldSlice(rows, t.nameOf)
}

func (t namedTable[T]) mutate(row *T, mutationFuncs ...func(*T) *model.Mutation) ([]ovsdb.Operation, error) {
	if len(mutationFuncs) == 0 {
		return nil, nil
	}

	mutations := make([]model.Mutation, 0, len(mutationFuncs))
	for _, mutationFunc := range mutationFuncs {
		mutation := mutationFunc(row)
		if mutation != nil {
			mutations = append(mutations, *mutation)
		}
	}

	return t.table().MutateOps(row, mutations...)
}

func (t namedTable[T]) mutateAll(row *T, mutationFuncs ...func(*T) []model.Mutation) ([]ovsdb.Operation, error) {
	if len(mutationFuncs) == 0 {
		return nil, nil
	}

	mutations := make([]model.Mutation, 0, len(mutationFuncs))
	for _, mutationFunc := range mutationFuncs {
		if mutation := mutationFunc(row); mutation != nil {
			mutations = append(mutations, mutation...)
		}
	}
	if len(mutations) == 0 {
		return nil, nil
	}

	return t.table().MutateOps(row, mutations...)
}

func (t namedTable[T]) createIfAbsent(name, method string, row *T) error {
	existing, err := t.get(name, true)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	operations, err := t.table().CreateOps(row)
	if err != nil {
		return fmt.Errorf("generate operations for creating %s %s: %w", t.kind, name, err)
	}
	plan := table.NewTxPlan(method)
	plan.Add(operations...)
	if err := t.store.Execute(context.Background(), plan); err != nil {
		return fmt.Errorf("create %s %s: %w", t.kind, name, err)
	}
	return nil
}

func (t namedTable[T]) delete(row *T) ([]ovsdb.Operation, error) {
	return t.table().DeleteOps(row)
}

func newNamedTable[T any](store *table.Database, prototype model.Model, kind string, nameOf func(*T) string, externalIDsOf func(*T) map[string]string) namedTable[T] {
	return namedTable[T]{
		store:         store,
		prototype:     prototype,
		kind:          kind,
		nameOf:        nameOf,
		externalIDsOf: externalIDsOf,
	}
}

func (t namedTable[T]) lookup(name string, ignoreNotFound bool) (*T, error) {
	if t.newByName == nil {
		return t.get(name, ignoreNotFound)
	}
	row := t.newByName(name)
	if err := t.table().Get(context.Background(), row); err != nil {
		if ignoreNotFound && errors.Is(err, table.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return row, nil
}

func (t namedTable[T]) mutateNamed(name string, mutationsFunc ...func(*T) *model.Mutation) ([]ovsdb.Operation, error) {
	row, err := t.lookup(name, false)
	if err != nil {
		return nil, logWrap(err, wrapErr("get %s %s: %w", t.kind, name))
	}
	ops, err := t.mutate(row, mutationsFunc...)
	if err != nil {
		return nil, logWrap(err, wrapErr("generate operations for mutating %s %s: %w", t.kind, name))
	}
	return ops, nil
}

func (t namedTable[T]) mutateNamedAll(name string, mutationsFunc ...func(*T) []model.Mutation) ([]ovsdb.Operation, error) {
	row, err := t.lookup(name, false)
	if err != nil {
		return nil, logErr(err)
	}
	ops, err := t.mutateAll(row, mutationsFunc...)
	if err != nil {
		return nil, logWrap(err, wrapErr("generate operations for mutating %s %s: %w", t.kind, name))
	}
	return ops, nil
}

func (t namedTable[T]) mutateUUIDs(name string, field func(*T) *[]string, uuids []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(uuids) == 0 {
		return nil, nil
	}
	return t.mutateNamed(name, func(row *T) *model.Mutation {
		return &model.Mutation{Field: field(row), Value: uuids, Mutator: op}
	})
}

func (t namedTable[T]) mutateOwnedUUID(parentName, uuid string, op ovsdb.Mutator, contains func(*T, string) bool, field func(*T) *[]string, child, parent string) ([]ovsdb.Operation, error) {
	if len(uuid) == 0 {
		return nil, nil
	}
	if parentName == "" && op == ovsdb.MutateOperationDelete {
		var err error
		parentName, err = t.ownerName(uuid, contains, child, parent)
		if err != nil {
			return nil, err
		}
	}
	return t.mutateUUIDs(parentName, field, []string{uuid}, op)
}

func (t namedTable[T]) mutateMap(name string, field func(*T) *map[string]string, values map[string]string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(values) == 0 {
		return nil, nil
	}
	return t.mutateNamed(name, func(row *T) *model.Mutation {
		return &model.Mutation{Field: field(row), Value: values, Mutator: op}
	})
}

func (t namedTable[T]) ownerName(uuid string, contains func(*T, string) bool, child, parent string) (string, error) {
	rows, err := t.list(false, func(row *T) bool {
		return contains(row, uuid)
	})
	if err != nil {
		klog.Error(err)
		return "", fmt.Errorf("failed to list %s by %s UUID %s: %w", parent, child, uuid, err)
	}
	name, err := uniqueOwnerName(rows, uuid, child, parent, t.nameOf)
	if err != nil {
		klog.Error(err)
		return "", err
	}
	return name, nil
}
