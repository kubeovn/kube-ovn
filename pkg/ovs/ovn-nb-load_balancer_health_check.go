package ovs

import (
	"context"
	"fmt"

	"golang.org/x/exp/slices"

	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

// CreateLoadBalancerHealthCheck create load balancer health check
func (c *ovnNbClient) CreateLoadBalancerHealthCheck(lbName, vipEndpoint string) error {
	lb, lbhc, err := c.GetLoadBalancerHealthCheck(lbName, vipEndpoint)
	if err != nil {
		klog.Errorf("failed to get lb health check: %v", err)
		return err
	}

	if lbhc != nil {
		klog.Infof("lb health check %s %s already exists", lbName, vipEndpoint)
		return nil
	}

	if lb.IPPortMappings == nil {
		err := fmt.Errorf("lb %s should has ip port mapping before setting health check", lbName)
		klog.Error(err)
		return err
	}
	ops := make([]ovsdb.Operation, 0, 2)
	uuid := ovsclient.NamedUUID()
	lbhc = &ovnnb.LoadBalancerHealthCheck{
		UUID: uuid,
		Options: map[string]string{
			"timeout":       "20",
			"interval":      "5",
			"success_count": "3",
			"failure_count": "3",
		},
		Vip: vipEndpoint,
	}
	hcOp, err := c.ovsDbClient.Create(lbhc)
	if err != nil {
		return fmt.Errorf("generate operations for creating load balancer health check %s %s: %v", lbName, vipEndpoint, err)
	}
	ops = append(ops, hcOp...)
	lb.HealthCheck = append(lb.HealthCheck, uuid)
	lbOp, err := c.ovsDbClient.Where(lb).Update(lb)
	if err != nil {
		err := fmt.Errorf("generate operations for updating load balancer %s: %v", lbName, err)
		klog.Error(err)
		return err
	}
	ops = append(ops, lbOp...)
	if err = c.Transact("lbhc-add", ops); err != nil {
		err = fmt.Errorf("failed to create load balancer health check for lb %s vip %s: %v", lbName, vipEndpoint, err)
		klog.Error(err)
		return err
	}
	return nil
}

// UpdateLoadBalancerHealthCheck update load balancer
func (c *ovnNbClient) UpdateLoadBalancerHealthCheck(lbhc *ovnnb.LoadBalancerHealthCheck, fields ...interface{}) error {
	var (
		op  []ovsdb.Operation
		err error
	)

	op, err = c.ovsDbClient.Where(lbhc).Update(lbhc, fields...)
	if err != nil {
		return fmt.Errorf("generate operations for updating load balancer health check %s: %v", lbhc.Vip, err)
	}

	if err = c.Transact("lbhc-update", op); err != nil {
		return fmt.Errorf("update load balancer health check  %s: %v", lbhc.Vip, err)
	}

	return nil
}

// DeleteLoadBalancerHealthChecks delete several load balancer health checks once
func (c *ovnNbClient) DeleteLoadBalancerHealthChecks(filter func(lb *ovnnb.LoadBalancerHealthCheck) bool) error {
	var (
		op  []ovsdb.Operation
		err error
	)

	op, err = c.ovsDbClient.WhereCache(
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
func (c *ovnNbClient) DeleteLoadBalancerHealthCheck(lbName, vip string) error {
	var (
		op  []ovsdb.Operation
		err error
	)

	op, err = c.DeleteLoadBalancerHealthCheckOp(lbName, vip)
	if err != nil {
		klog.Errorf("failed to delete lb health check: %v", err)
		return err
	}

	if err = c.Transact("lbhc-del", op); err != nil {
		return fmt.Errorf("delete load balancer %s: %v", lbName, err)
	}

	return nil
}

// GetLoadBalancerHealthCheck get load balancer health check by vip
func (c *ovnNbClient) GetLoadBalancerHealthCheck(lbName, vipEndpoint string) (*ovnnb.LoadBalancer, *ovnnb.LoadBalancerHealthCheck, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	var lb *ovnnb.LoadBalancer
	var err error
	if lb, err = c.GetLoadBalancer(lbName, false); err != nil {
		klog.Errorf("failed to get lb %s: %v", lbName, err)
		return nil, nil, err
	}

	healthCheckList := make([]ovnnb.LoadBalancerHealthCheck, 0)
	if err := c.ovsDbClient.WhereCache(func(healthCheck *ovnnb.LoadBalancerHealthCheck) bool {
		return slices.Contains(lb.HealthCheck, healthCheck.UUID)
	}).List(ctx, &healthCheckList); err != nil {
		err := fmt.Errorf("failed to list load balancer health check lb health check by vip %q: %v", vipEndpoint, err)
		klog.Error(err)
		return nil, nil, err
	}

	if len(healthCheckList) > 1 {
		err := fmt.Errorf("lb %s has more than one health check with the same vip %s", lbName, vipEndpoint)
		klog.Error(err)
		return nil, nil, err
	}
	if len(healthCheckList) == 1 {
		// #nosec G602
		return lb, &healthCheckList[0], nil
	}
	return lb, nil, nil
}

// ListLoadBalancerHealthChecks list all load balancer health checks
func (c *ovnNbClient) ListLoadBalancerHealthChecks(filter func(lbhc *ovnnb.LoadBalancerHealthCheck) bool) ([]ovnnb.LoadBalancerHealthCheck, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var (
		lbhcList []ovnnb.LoadBalancerHealthCheck
		err      error
	)
	lbhcList = make([]ovnnb.LoadBalancerHealthCheck, 0)

	if err = c.ovsDbClient.WhereCache(
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
func (c *ovnNbClient) LoadBalancerHealthCheckExists(lbName, vip string) (bool, error) {
	_, lbhc, err := c.GetLoadBalancerHealthCheck(lbName, vip)
	if err != nil {
		klog.Errorf("failed to get lb health check: %v", err)
		return false, err
	}
	return lbhc != nil, err
}

// DeleteLoadBalancerHealthCheckOp delete operation which delete load balancer health check
func (c *ovnNbClient) DeleteLoadBalancerHealthCheckOp(lbName, vip string) ([]ovsdb.Operation, error) {
	var (
		lbhc *ovnnb.LoadBalancerHealthCheck
		err  error
	)

	_, lbhc, err = c.GetLoadBalancerHealthCheck(lbName, vip)
	if err != nil {
		klog.Errorf("failed to get lb health check: %v", err)
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
		klog.Errorf("failed to generate operations for deleting lb health check: %v", err)
		return nil, err
	}

	return op, nil
}
