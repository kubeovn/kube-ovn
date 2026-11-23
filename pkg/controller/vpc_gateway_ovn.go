package controller

import "github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"

// Gateway reconciliation depends on domain operations rather than a concrete
// client. Both the legacy client and the controller table adapter use the same
// BFD and policy algorithms.
type gatewayBFDClient interface {
	FindBFD(map[string]string) ([]ovnnb.BFD, error)
	CreateBFD(string, string, int, int, int, map[string]string) (*ovnnb.BFD, error)
	DeleteBFD(string) error
}

type gatewayPolicyClient interface {
	ListLogicalRouterPolicies(string, int, map[string]string, bool) ([]*ovnnb.LogicalRouterPolicy, error)
	UpdateLogicalRouterPolicy(*ovnnb.LogicalRouterPolicy, ...any) error
	DeleteLogicalRouterPolicyByUUID(string, string) error
	DeleteLogicalRouterPolicies(string, int, map[string]string) error
	AddLogicalRouterPolicy(string, int, string, string, []string, []string, map[string]string) error
}

type gatewayTableClient struct {
	controller *Controller
}

var (
	_ gatewayBFDClient    = gatewayTableClient{}
	_ gatewayPolicyClient = gatewayTableClient{}
)

func (c gatewayTableClient) FindBFD(externalIDs map[string]string) ([]ovnnb.BFD, error) {
	return c.controller.findBFD(externalIDs)
}

func (c gatewayTableClient) CreateBFD(port, destination string, minRx, minTx, multiplier int, externalIDs map[string]string) (*ovnnb.BFD, error) {
	return c.controller.createBFD(port, destination, minRx, minTx, multiplier, externalIDs)
}

func (c gatewayTableClient) DeleteBFD(uuid string) error {
	return c.controller.deleteBFD(uuid)
}

func (c gatewayTableClient) ListLogicalRouterPolicies(router string, priority int, externalIDs map[string]string, ignoreEmpty bool) ([]*ovnnb.LogicalRouterPolicy, error) {
	return c.controller.listLogicalRouterPolicies(router, priority, externalIDs, ignoreEmpty)
}

func (c gatewayTableClient) UpdateLogicalRouterPolicy(policy *ovnnb.LogicalRouterPolicy, fields ...any) error {
	return c.controller.updateLogicalRouterPolicy(policy, fields...)
}

func (c gatewayTableClient) DeleteLogicalRouterPolicyByUUID(router, uuid string) error {
	return c.controller.deleteLogicalRouterPolicyByUUID(router, uuid)
}

func (c gatewayTableClient) DeleteLogicalRouterPolicies(router string, priority int, externalIDs map[string]string) error {
	return c.controller.deleteLogicalRouterPolicies(router, priority, externalIDs)
}

func (c gatewayTableClient) AddLogicalRouterPolicy(router string, priority int, match, action string, nextHops, bfdSessions []string, externalIDs map[string]string) error {
	return c.controller.addLogicalRouterPolicy(router, priority, match, action, nextHops, bfdSessions, externalIDs)
}
