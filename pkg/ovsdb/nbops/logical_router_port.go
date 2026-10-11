package nbops

import (
	"context"
	"errors"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

// LogicalRouterPorts provides resource-level parent ownership operations.
type LogicalRouterPorts struct {
	ports     rowTable
	routers   rowTable
	committer table.Committer
}

// NewLogicalRouterPorts creates a typed facade over an NB table provider.
func NewLogicalRouterPorts(provider table.Provider, committer table.Committer) *LogicalRouterPorts {
	if provider == nil {
		return &LogicalRouterPorts{committer: committer}
	}
	return &LogicalRouterPorts{
		ports:     provider.Table(&ovnnb.LogicalRouterPort{}),
		routers:   provider.Table(&ovnnb.LogicalRouter{}),
		committer: committer,
	}
}

// EnsureParent makes exactly one logical router the owner of a port. Any
// stale parent references are removed before the desired parent is attached
// in one transaction.
func (p *LogicalRouterPorts) EnsureParent(ctx context.Context, portName, routerName string) error {
	_, err := p.EnsureParentResult(ctx, portName, routerName)
	return err
}

// EnsureParentResult is the result-bearing form of EnsureParent. It exposes
// only commit metadata while keeping model mutations private to this facade.
func (p *LogicalRouterPorts) EnsureParentResult(ctx context.Context, portName, routerName string) (table.CommitResult, error) {
	result := table.CommitResult{Method: "lrp-parent"}
	if p == nil {
		return result, errors.New("logical router port facade is nil")
	}
	return namedParentSpec[ovnnb.LogicalRouterPort, ovnnb.LogicalRouter]{
		committer:  p.committer,
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
