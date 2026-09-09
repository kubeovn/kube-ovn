package nbops

import (
	"context"
	"errors"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

// LogicalRouterPorts provides resource-level parent ownership operations.
type LogicalRouterPorts struct {
	ports    rowTable
	routers  rowTable
	executor table.Executor
}

// NewLogicalRouterPorts creates a typed facade over an NB table provider.
func NewLogicalRouterPorts(provider table.TableProvider, executor table.Executor) *LogicalRouterPorts {
	if provider == nil {
		return &LogicalRouterPorts{executor: executor}
	}
	return &LogicalRouterPorts{
		ports:    provider.Table(&ovnnb.LogicalRouterPort{}),
		routers:  provider.Table(&ovnnb.LogicalRouter{}),
		executor: executor,
	}
}

// EnsureParent makes exactly one logical router the owner of a port. Any
// stale parent references are removed before the desired parent is attached
// in one transaction.
func (p *LogicalRouterPorts) EnsureParent(ctx context.Context, portName, routerName string) error {
	if p == nil {
		return errors.New("logical router port facade is nil")
	}
	return namedParentSpec[ovnnb.LogicalRouterPort, ovnnb.LogicalRouter]{
		executor:   p.executor,
		children:   p.ports,
		parents:    p.routers,
		method:     "lrp-parent",
		nilErr:     "logical router port facade is nil",
		namesErr:   "logical router port and router names are required",
		childKind:  "logical router port",
		parentKind: "logical router",
		newChild:   func(name string) *ovnnb.LogicalRouterPort { return &ovnnb.LogicalRouterPort{Name: name} },
		uuidOf:     func(port *ovnnb.LogicalRouterPort) string { return port.UUID },
		nameOf:     func(lr *ovnnb.LogicalRouter) string { return lr.Name },
		field:      func(lr *ovnnb.LogicalRouter) *[]string { return &lr.Ports },
	}.ensure(ctx, portName, routerName)
}
