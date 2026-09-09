package ovs

import (
	"fmt"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c *OVNNbClient) AddLoadBalancerHealthCheck(lbName, vipEndpoint string, externals map[string]string) error {
	lbhc, err := c.newLoadBalancerHealthCheck(lbName, vipEndpoint, externals)
	if err != nil {
		return logFmt("failed to new lb health check: %w", err)
	}

	return c.CreateLoadBalancerHealthCheck(lbName, vipEndpoint, lbhc)
}

// newLoadBalancerHealthCheck return hc with basic information
func (c *OVNNbClient) newLoadBalancerHealthCheck(lbName, vipEndpoint string, externals map[string]string) (*ovnnb.LoadBalancerHealthCheck, error) {
	var (
		exists bool
		err    error
	)

	if err := requireName(lbName, "the lb name is required"); err != nil {
		return nil, err
	}
	if err := requireName(vipEndpoint, "the vip endpoint is required"); err != nil {
		return nil, err
	}

	if exists, err = c.LoadBalancerHealthCheckExists(lbName, vipEndpoint); err != nil {
		return nil, logFmt("get lb health check %s: %w", vipEndpoint, err)
	}

	// found, ignore
	if exists {
		klog.Infof("already exists health check with vip %s for lb %s", vipEndpoint, lbName)
		return nil, nil
	}
	klog.Infof("create health check for vip endpoint %s in lb %s", vipEndpoint, lbName)

	return &ovnnb.LoadBalancerHealthCheck{
		UUID:        ovsclient.NamedUUID(),
		ExternalIDs: externals,
		Options: map[string]string{
			"timeout":       "20",
			"interval":      "5",
			"success_count": "3",
			"failure_count": "3",
		},
		Vip: vipEndpoint,
	}, nil
}

// CreateLoadBalancerHealthCheck create lb health check
func (c *OVNNbClient) CreateLoadBalancerHealthCheck(lbName, vipEndpoint string, lbhc *ovnnb.LoadBalancerHealthCheck) error {
	if lbhc == nil {
		return nil
	}

	if err := createAndAttach(c, "lbhc-add", &ovnnb.LoadBalancerHealthCheck{}, []*ovnnb.LoadBalancerHealthCheck{lbhc},
		func(row *ovnnb.LoadBalancerHealthCheck) string { return row.UUID },
		func(uuids []string) ([]ovsdb.Operation, error) {
			return c.LoadBalancerUpdateHealthCheckOp(lbName, uuids, ovsdb.MutateOperationInsert)
		}); err != nil {
		return logFmt("failed to create lb health check for lb %s vip %s: %w", lbName, vipEndpoint, err)
	}
	return nil
}

// UpdateLoadBalancerHealthCheck update lb
func (c *OVNNbClient) UpdateLoadBalancerHealthCheck(lbhc *ovnnb.LoadBalancerHealthCheck, fields ...any) error {
	return c.updateModelLogged("lbhc-update", lbhc, func(err error) error {
		return fmt.Errorf("update lb health check  %s: %w", lbhc.Vip, err)
	}, fields...)
}

// DeleteLoadBalancerHealthChecks delete several lb health checks once
func (c *OVNNbClient) DeleteLoadBalancerHealthChecks(filter func(lb *ovnnb.LoadBalancerHealthCheck) bool) error {
	return deleteWhereCache(
		c,
		&ovnnb.LoadBalancerHealthCheck{},
		"lbhc-del",
		filter,
		func(err error) error { return fmt.Errorf("generate operations for delete lb health checks: %w", err) },
		func(err error) error { return fmt.Errorf("delete lb health checks : %w", err) },
	)
}

// DeleteLoadBalancerHealthCheck delete lb health check
func (c *OVNNbClient) DeleteLoadBalancerHealthCheck(lbName, vip string) error {
	var (
		op  []ovsdb.Operation
		err error
	)

	op, err = c.DeleteLoadBalancerHealthCheckOp(lbName, vip)
	if err != nil {
		klog.Errorf("failed to delete lb health check: %v", err)
		return err
	}

	if err = transactOps(c, "lbhc-del", op); err != nil {
		return logWrap(err, wrapErr("delete lb %s: %w", lbName))
	}

	return nil
}

// GetLoadBalancerHealthCheck get lb health check by vip
func (c *OVNNbClient) GetLoadBalancerHealthCheck(lbName, vipEndpoint string, ignoreNotFound bool) (*ovnnb.LoadBalancer, *ovnnb.LoadBalancerHealthCheck, error) {
	lb, err := c.GetLoadBalancer(lbName, false)
	if err != nil {
		klog.Errorf("failed to get lb %s: %v", lbName, err)
		return nil, nil, err
	}

	if lb.HealthCheck == nil {
		if ignoreNotFound {
			return lb, nil, nil
		}
		return nil, nil, logFmt("lb %s doesn't have health check", lbName)
	}

	healthCheckList, err := filterTimeout(c.Database, &ovnnb.LoadBalancerHealthCheck{}, func(healthCheck *ovnnb.LoadBalancerHealthCheck) bool {
		return slices.Contains(lb.HealthCheck, healthCheck.UUID) && healthCheck.Vip == vipEndpoint
	})
	if err != nil {
		return nil, nil, logFmt("failed to list lb health check lb health check by vip %q: %w", vipEndpoint, err)
	}

	hc, err := compat.Unique(healthCheckList, ignoreNotFound,
		fmt.Errorf("lb %s doesn't have health check with vip %s", lbName, vipEndpoint),
		fmt.Errorf("lb %s has more than one health check with the same vip %s", lbName, vipEndpoint),
	)
	if err != nil {
		return nil, nil, logErr(err)
	}
	return lb, hc, nil
}

// ListLoadBalancerHealthChecks list all lb health checks
func (c *OVNNbClient) ListLoadBalancerHealthChecks(filter func(lbhc *ovnnb.LoadBalancerHealthCheck) bool) ([]ovnnb.LoadBalancerHealthCheck, error) {
	return filterWrap(c.Database, &ovnnb.LoadBalancerHealthCheck{}, func(lbhc *ovnnb.LoadBalancerHealthCheck) bool {
		return filter == nil || filter(lbhc)
	}, func(err error) error {
		return fmt.Errorf("list lb health check: %w", err)
	})
}

func (c *OVNNbClient) LoadBalancerHealthCheckExists(lbName, vipEndpoint string) (bool, error) {
	_, lbhc, err := c.GetLoadBalancerHealthCheck(lbName, vipEndpoint, true)
	if err != nil {
		klog.Errorf("failed to get lb %s health check by vip endpoint %s: %v", lbName, vipEndpoint, err)
		return false, err
	}
	return lbhc != nil, err
}

// DeleteLoadBalancerHealthCheckOp delete operation which delete lb health check
func (c *OVNNbClient) DeleteLoadBalancerHealthCheckOp(lbName, vip string) ([]ovsdb.Operation, error) {
	lb, lbhc, err := c.GetLoadBalancerHealthCheck(lbName, vip, true)
	if err != nil {
		klog.Errorf("failed to get lb health check: %v", err)
		return nil, err
	}
	// not found, skip
	if lbhc == nil {
		return nil, nil
	}

	mutateOps, err := c.Database.Table(&ovnnb.LoadBalancer{}).MutateOps(lb, model.Mutation{
		Field:   &lb.HealthCheck,
		Value:   []string{lbhc.UUID},
		Mutator: ovsdb.MutateOperationDelete,
	})
	if err != nil {
		klog.Errorf("failed to generate operations for deleting lb health check: %v", err)
		return nil, err
	}
	deleteOps, err := c.Database.Table(&ovnnb.LoadBalancerHealthCheck{}).DeleteOps(lbhc)
	if err != nil {
		klog.Errorf("failed to generate operations for deleting lb health check: %v", err)
		return nil, err
	}

	return append(mutateOps, deleteOps...), nil
}
