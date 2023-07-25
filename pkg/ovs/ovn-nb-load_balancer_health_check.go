package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

// CreateLoadBalancerHealthCheck create load balancer health check
func (c *ovnClient) CreateLoadBalancerHealthCheck(lbName, vip string) error {
	var (
		lbhc *ovnnb.LoadBalancerHealthCheck
		err  error
	)

	lbhc, err = c.GetLoadBalancerHealthCheck(lbName, vip, true)
	if err != nil {
		return err
	}

	// found, ignore
	if lbhc != nil {
		return nil
	}

	lbhc = &ovnnb.LoadBalancerHealthCheck{
		UUID:        ovsclient.NamedUUID(),
		ExternalIDs: map[string]string{},
		Options: map[string]string{
			"timeout":       "20",
			"interval":      "5",
			"success_count": "3",
			"failure_count": "3",
		},
		Vip: vip,
	}

	var (
		op []ovsdb.Operation
	)

	if op, err = c.ovnNbClient.Create(lbhc); err != nil {
		return fmt.Errorf("generate operations for creating load balancer health check %s %s: %v", lbName, vip, err)
	}

	if err = c.Transact("lbhc-add", op); err != nil {
		return fmt.Errorf("create load balancer health check %s %s: %v", lbName, vip, err)
	}

	return nil
}

// UpdateLoadBalancerHealthCheck update load balancer
func (c *ovnClient) UpdateLoadBalancerHealthCheck(lbhc *ovnnb.LoadBalancerHealthCheck, fields ...interface{}) error {
	var (
		op  []ovsdb.Operation
		err error
	)

	op, err = c.ovnNbClient.Where(lbhc).Update(lbhc, fields...)
	if err != nil {
		return fmt.Errorf("generate operations for updating load balancer health check %s: %v", lbhc.Vip, err)
	}

	if err = c.Transact("lbhc-update", op); err != nil {
		return fmt.Errorf("update load balancer health check  %s: %v", lbhc.Vip, err)
	}

	return nil
}

// DeleteLoadBalancerHealthChecks delete several load balancer health checks once
func (c *ovnClient) DeleteLoadBalancerHealthChecks(filter func(lb *ovnnb.LoadBalancerHealthCheck) bool) error {
	var (
		op  []ovsdb.Operation
		err error
	)

	op, err = c.ovnNbClient.WhereCache(
		func(lbhc *ovnnb.LoadBalancerHealthCheck) bool {
			if filter != nil {
				return filter(lbhc)
			}
			return true
		},
	).Delete()

	if err != nil {
		return fmt.Errorf("generate operations for delete load balancer health checks: %v", err)
	}

	if err := c.Transact("lbhc-del", op); err != nil {
		return fmt.Errorf("delete load balancer health checks : %v", err)
	}

	return nil
}

// DeleteLoadBalancerHealthCheck delete load balancer health check
func (c *ovnClient) DeleteLoadBalancerHealthCheck(lbName, vip string) error {
	var (
		op  []ovsdb.Operation
		err error
	)

	op, err = c.DeleteLoadBalancerHealthCheckOp(lbName, vip)
	if err != nil {
		return nil
	}

	if err = c.Transact("lbhc-del", op); err != nil {
		return fmt.Errorf("delete load balancer %s: %v", lbName, err)
	}

	return nil
}

// GetLoadBalancerHealthCheck get load balancer health check by vip
func (c *ovnClient) GetLoadBalancerHealthCheck(lbName, vip string, ignoreNotFound bool) (*ovnnb.LoadBalancerHealthCheck, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var (
		lbhcList []ovnnb.LoadBalancerHealthCheck
		err      error
	)
	lbhcList = make([]ovnnb.LoadBalancerHealthCheck, 0)

	if err = c.ovnNbClient.WhereCache(
		func(lbhc *ovnnb.LoadBalancerHealthCheck) bool {
			return lbhc.Vip == vip
		},
	).List(ctx, &lbhcList); err != nil {
		return nil, fmt.Errorf("list load balancer health check %q: %v", vip, err)
	}

	// not found
	if len(lbhcList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found load balancer health check %q", vip)
	}

	if len(lbhcList) > 1 {
		return nil, fmt.Errorf("more than one load balancer health check with same vip %q", vip)
	}

	return &lbhcList[0], nil
}

// ListLoadBalancerHealthChecks list all load balancer health checks
func (c *ovnClient) ListLoadBalancerHealthChecks(filter func(lbhc *ovnnb.LoadBalancerHealthCheck) bool) ([]ovnnb.LoadBalancerHealthCheck, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var (
		lbhcList []ovnnb.LoadBalancerHealthCheck
		err      error
	)
	lbhcList = make([]ovnnb.LoadBalancerHealthCheck, 0)

	if err = c.ovnNbClient.WhereCache(
		func(lbhc *ovnnb.LoadBalancerHealthCheck) bool {
			if filter != nil {
				return filter(lbhc)
			}
			return true
		},
	).List(ctx, &lbhcList); err != nil {
		return nil, fmt.Errorf("list load balancer health check: %v", err)
	}

	return lbhcList, nil
}

// LoadBalancerHealthCheckExists get load balancer health check and return the result of existence
func (c *ovnClient) LoadBalancerHealthCheckExists(lbName, vip string) (bool, error) {
	lbhc, err := c.GetLoadBalancerHealthCheck(lbName, vip, true)
	return lbhc != nil, err
}

// DeleteLoadBalancerHealthCheckOp create operation which delete load balancer health check
func (c *ovnClient) DeleteLoadBalancerHealthCheckOp(lbName, vip string) ([]ovsdb.Operation, error) {
	var (
		lbhc *ovnnb.LoadBalancerHealthCheck
		err  error
	)

	lbhc, err = c.GetLoadBalancerHealthCheck(lbName, vip, true)
	if err != nil {
		return nil, err
	}
	// not found, skip
	if lbhc == nil {
		return nil, nil
	}

	var (
		op []ovsdb.Operation
	)

	op, err = c.Where(lbhc).Delete()
	if err != nil {
		return nil, err
	}

	return op, nil
}
