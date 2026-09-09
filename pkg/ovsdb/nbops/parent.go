package nbops

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

type rowTable interface {
	table.TableReader
	table.TableMutator
}

func listMatching[T any](ctx context.Context, t rowTable, pred func(*T) bool) ([]T, error) {
	rows := make([]T, 0)
	if err := t.Filter(ctx, pred, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func getNamed[T any](ctx context.Context, t rowTable, name, kind string, nameOf func(*T) string) (*T, error) {
	rows, err := listMatching(ctx, t, func(row *T) bool {
		return nameOf(row) == name
	})
	if err != nil {
		return nil, fmt.Errorf("get %s %s: %w", kind, name, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("get %s %s: %w", kind, name, table.ErrNotFound)
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("%s %s has multiple rows", kind, name)
	}
	return &rows[0], nil
}

// namedParentSpec is the shared single-parent UUID-set ownership flow used by
// Logical_Switch_Port and Logical_Router_Port. ACL stays out because it has
// two parent tables.
type namedParentSpec[Child, Parent any] struct {
	executor   table.Executor
	children   rowTable
	parents    rowTable
	method     string
	nilErr     string
	namesErr   string
	childKind  string
	parentKind string
	newChild   func(string) *Child
	uuidOf     func(*Child) string
	nameOf     func(*Parent) string
	field      func(*Parent) *[]string
}

func (s namedParentSpec[Child, Parent]) ensure(ctx context.Context, childName, parentName string) error {
	if s.children == nil || s.parents == nil || s.executor == nil {
		return errors.New(s.nilErr)
	}
	if childName == "" || parentName == "" {
		return errors.New(s.namesErr)
	}

	child := s.newChild(childName)
	if err := s.children.Get(ctx, child); err != nil {
		return fmt.Errorf("get %s %s: %w", s.childKind, childName, err)
	}
	uuid := s.uuidOf(child)

	target, err := getNamed(ctx, s.parents, parentName, s.parentKind, s.nameOf)
	if err != nil {
		return err
	}

	parents, err := listMatching(ctx, s.parents, func(row *Parent) bool {
		return slices.Contains(*s.field(row), uuid)
	})
	if err != nil {
		return fmt.Errorf("find parents for %s %s: %w", s.childKind, childName, err)
	}

	plan := table.NewTxPlan(s.method)
	hasTarget := false
	for i := range parents {
		parent := &parents[i]
		if s.nameOf(parent) == parentName {
			hasTarget = true
			continue
		}
		operations, err := s.parents.MutateOps(parent, model.Mutation{
			Field: s.field(parent), Value: []string{uuid}, Mutator: ovsdb.MutateOperationDelete,
		})
		if err != nil {
			return fmt.Errorf("detach %s %s from %s: %w", s.childKind, childName, s.nameOf(parent), err)
		}
		plan.Add(operations...)
	}
	if !hasTarget {
		operations, err := s.parents.MutateOps(target, model.Mutation{
			Field: s.field(target), Value: []string{uuid}, Mutator: ovsdb.MutateOperationInsert,
		})
		if err != nil {
			return fmt.Errorf("attach %s %s to %s: %w", s.childKind, childName, parentName, err)
		}
		plan.Add(operations...)
	}
	return s.executor.Execute(ctx, plan)
}
