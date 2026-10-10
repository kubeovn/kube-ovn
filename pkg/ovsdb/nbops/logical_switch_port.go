// Package nbops contains typed, resource-oriented OVN northbound operations.
package nbops

import (
	"context"
	"errors"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

// LogicalSwitchPorts provides resource-level parent ownership operations.
// It hides model selectors, mutation fields, and detach-before-attach ordering
// from callers while retaining schema-generated models inside this package.
type LogicalSwitchPorts struct {
	ports    rowTable
	switches rowTable
	executor table.Executor
}

// NewLogicalSwitchPorts creates a typed facade over an NB table provider.
func NewLogicalSwitchPorts(provider table.Provider, executor table.Executor) *LogicalSwitchPorts {
	if provider == nil {
		return &LogicalSwitchPorts{executor: executor}
	}
	return &LogicalSwitchPorts{
		ports:    provider.Table(&ovnnb.LogicalSwitchPort{}),
		switches: provider.Table(&ovnnb.LogicalSwitch{}),
		executor: executor,
	}
}

// EnsureParent makes exactly one logical switch the owner of a port. Any
// stale parent references are removed before the desired parent is attached
// in one transaction.
func (p *LogicalSwitchPorts) EnsureParent(ctx context.Context, portName, switchName string) error {
	_, err := p.EnsureParentResult(ctx, portName, switchName)
	return err
}

// EnsureParentResult is the result-bearing form of EnsureParent. It exposes
// only commit metadata while keeping model mutations private to this facade.
func (p *LogicalSwitchPorts) EnsureParentResult(ctx context.Context, portName, switchName string) (table.CommitResult, error) {
	result := table.CommitResult{Method: "lsp-parent"}
	if p == nil {
		return result, errors.New("logical switch port facade is nil")
	}
	return namedParentSpec[ovnnb.LogicalSwitchPort, ovnnb.LogicalSwitch]{
		executor:   p.executor,
		children:   p.ports,
		parents:    p.switches,
		method:     "lsp-parent",
		nilErr:     "logical switch port facade is nil",
		namesErr:   "logical switch port and switch names are required",
		childKind:  "logical switch port",
		parentKind: "logical switch",
		newChild:   func(name string) *ovnnb.LogicalSwitchPort { return &ovnnb.LogicalSwitchPort{Name: name} },
		uuidOf:     func(port *ovnnb.LogicalSwitchPort) string { return port.UUID },
		nameOf:     func(ls *ovnnb.LogicalSwitch) string { return ls.Name },
		field:      func(ls *ovnnb.LogicalSwitch) *[]string { return &ls.Ports },
	}.ensure(ctx, portName, switchName)
}
