package ovs

import (
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/ovn-org/libovsdb/ovsdb"
)

type NbGlobal interface {
	UpdateNbGlobal(nbGlobal *ovnnb.NBGlobal, fields ...interface{}) error
	GetNbGlobal() (*ovnnb.NBGlobal, error)
}

type LogicalRouter interface {
	GetLogicalRouter(lrName string, ignoreNotFound bool) (*ovnnb.LogicalRouter, error)
}

type LogicalRouterPort interface {
	AddLogicalRouterPort(lr, name, mac, networks string) error
	LogicalRouterPortExists(lrpName string) (bool, error)
}

type LogicalSwitchPort interface {
	ListLogicalSwitchPorts(needVendorFilter bool, externalIDs map[string]string) ([]ovnnb.LogicalSwitchPort, error)
	GetLogicalSwitchPort(lspName string, ignoreNotFound bool) (*ovnnb.LogicalSwitchPort, error)
}

type PortGroup interface {
	PortGroupUpdatePorts(pgName string, op ovsdb.Mutator, lspNames ...string) error
	PortGroupExists(pgName string) (bool, error)
}

type LogicalRouterStaticRoute interface {
	GetLogicalRouterRouteByOpts(key, value string) ([]ovnnb.LogicalRouterStaticRoute, error)
	ListLogicalRouterStaticRoutes(externalIDs map[string]string) ([]ovnnb.LogicalRouterStaticRoute, error)
}

type LogicalRouterPolicy interface {
	AddLogicalRouterPolicy(lrName string, priority int, match, action string, nextHops []string, externalIDs map[string]string) error
	DeleteLogicalRouterPolicy(lrName string, priority int, match string) error
	DeleteRouterPolicy(lr *ovnnb.LogicalRouter, uuid string) error
	ListLogicalRouterPolicies(externalIDs map[string]string) ([]ovnnb.LogicalRouterPolicy, error)
}

type OvnClient interface {
	LogicalRouter
	LogicalRouterPort
	NbGlobal
	LogicalSwitchPort
	PortGroup
	LogicalRouterStaticRoute
	LogicalRouterPolicy
}
