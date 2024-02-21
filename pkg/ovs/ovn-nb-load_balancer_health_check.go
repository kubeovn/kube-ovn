package ovs

import (
	"context"
	"fmt"
	"slices"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c *OVNNbClient) AddLoadBalancerHealthCheck(lbName, vipEndpoint string, externals map[string]string) error {
	lbhc, err := c.newLoadBalancerHealthCheck(lbName, vipEndpoint, externals)
	if err != nil {
		err := fmt.Errorf("failed to new lb health check: %v", err)
		klog.Error(err)
		return err
	}

	return c.CreateLoadBalancerHealthCheck(lbName, vipEndpoint, lbhc)
}

// newLoadBalancerHealthCheck return hc with basic information
func (c *OVNNbClient) newLoadBalancerHealthCheck(lbName, vipEndpoint string, externals map[string]string) (*ovnnb.LoadBalancerHealthCheck, error) {
	var (
		exists bool
		err    error
	)

	if len(lbName) == 0 {
		err = fmt.Errorf("the lb name is required")
		klog.Error(err)
		return nil, err
	}
	if len(vipEndpoint) == 0 {
		err = fmt.Errorf("the vip endpoint is required")
		klog.Error(err)
		return nil, err
	}

	if exists, err = c.LoadBalancerHealthCheckExists(lbName, vipEndpoint); err != nil {
		err := fmt.Errorf("get lb health check %s: %v", vipEndpoint, err)
		klog.Error(err)
		return nil, err
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

	var (
		models                  = make([]model.Model, 0, 1)
		lbhcUUIDs               = make([]string, 0, 1)
		lbHcModel               = model.Model(lbhc)
		ops                     = make([]ovsdb.Operation, 0, 2)
		createLbhcOp, lbHcAddOp []ovsdb.Operation
		err                     error
	)

	models = append(models, lbHcModel)
	lbhcUUIDs = append(lbhcUUIDs, lbhc.UUID)

	if createLbhcOp, err = c.ovsDbClient.Create(models...); err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for creating lbhc: %v", err)
	}
	ops = append(ops, createLbhcOp...)

	if lbHcAddOp, err = c.LoadBalancerUpdateHealthCheckOp(lbName, lbhcUUIDs, ovsdb.MutateOperationInsert); err != nil {
		err = fmt.Errorf("generate operations for adding lbhc to lb %s: %v", lbName, err)
		klog.Error(err)
		return err
	}
	ops = append(ops, lbHcAddOp...)

	if err = c.Transact("lbhc-add", ops); err != nil {
		err = fmt.Errorf("failed to create lb health check for lb %s vip %s: %v", lbName, vipEndpoint, err)
		klog.Error(err)
		return err
	}

	return nil
}

// UpdateLoadBalancerHealthCheck update lb
func (c *OVNNbClient) UpdateLoadBalancerHealthCheck(lbhc *ovnnb.LoadBalancerHealthCheck, fields ...interface{}) error {
	var (
		op  []ovsdb.Operation
		err error
	)

	if op, err = c.ovsDbClient.Where(lbhc).Update(lbhc, fields...); err != nil {
		return fmt.Errorf("generate operations for updating lb health check %s: %v", lbhc.Vip, err)
	}

	if err = c.Transact("lbhc-update", op); err != nil {
		return fmt.Errorf("update lb health check  %s: %v", lbhc.Vip, err)
	}

	return nil
}

// DeleteLoadBalancerHealthChecks delete several lb health checks once
func (c *OVNNbClient) DeleteLoadBalancerHealthChecks(filter func(lb *ovnnb.LoadBalancerHealthCheck) bool) error {
	op, err := c.ovsDbClient.WhereCache(
		func(lbhc *ovnnb.LoadBalancerHealthCheck) bool {
			if filter != nil {
				return filter(lbhc)
			}
			return true
		},
	).Delete()
	if err != nil {
		return fmt.Errorf("generate operations for delete lb health checks: %v", err)
	}

	if err := c.Transact("lbhc-del", op); err != nil {
		return fmt.Errorf("delete lb health checks : %v", err)
	}

	return nil
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

	if err = c.Transact("lbhc-del", op); err != nil {
		return fmt.Errorf("delete lb %s: %v", lbName, err)
	}

	return nil
}

// GetLoadBalancerHealthCheck get lb health check by vip
func (c *OVNNbClient) GetLoadBalancerHealthCheck(lbName, vipEndpoint string, ignoreNotFound bool) (*ovnnb.LoadBalancer, *ovnnb.LoadBalancerHealthCheck, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lb, err := c.GetLoadBalancer(lbName, false)
	if err != nil {
		klog.Errorf("failed to get lb %s: %v", lbName, err)
		return nil, nil, err
	}

	if lb.HealthCheck == nil {
		if ignoreNotFound {
			return lb, nil, nil
		}
		err = fmt.Errorf("lb %s doesn't have health check", lbName)
		klog.Error(err)
		return nil, nil, err
	}

	healthCheckList := make([]ovnnb.LoadBalancerHealthCheck, 0)
	if err = c.ovsDbClient.WhereCache(
		func(healthCheck *ovnnb.LoadBalancerHealthCheck) bool {
			return slices.Contains(lb.HealthCheck, healthCheck.UUID) &&
				healthCheck.Vip == vipEndpoint
		},
	).List(ctx, &healthCheckList); err != nil {
		err = fmt.Errorf("failed to list lb health check lb health check by vip %q: %v", vipEndpoint, err)
		klog.Error(err)
		return nil, nil, err
	}

	if len(healthCheckList) > 1 {
		err = fmt.Errorf("lb %s has more than one health check with the same vip %s", lbName, vipEndpoint)
		klog.Error(err)
		return nil, nil, err
	}
	if len(healthCheckList) == 0 {
		if ignoreNotFound {
			return lb, nil, nil
		}
		err = fmt.Errorf("lb %s doesn't have health check with vip %s", lbName, vipEndpoint)
		klog.Error(err)
		return nil, nil, err
	}
	// #nosec G602
	return lb, &healthCheckList[0], nil
}

// ListLoadBalancerHealthChecks list all lb health checks
func (c *OVNNbClient) ListLoadBalancerHealthChecks(filter func(lbhc *ovnnb.LoadBalancerHealthCheck) bool) ([]ovnnb.LoadBalancerHealthCheck, error) {
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
		return nil, fmt.Errorf("list lb health check: %v", err)
	}

	return lbhcList, nil
}

// LoadBalancerHealthCheckExists get lb health check and return the result of existence
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

	mutateOps, err := c.Where(lb).Mutate(lb, model.Mutation{
		Field:   &lb.HealthCheck,
		Value:   []string{lbhc.UUID},
		Mutator: ovsdb.MutateOperationDelete,
	})
	if err != nil {
		klog.Errorf("failed to generate operations for deleting lb health check: %v", err)
		return nil, err
	}
	deleteOps, err := c.Where(lbhc).Delete()
	if err != nil {
		klog.Errorf("failed to generate operations for deleting lb health check: %v", err)
		return nil, err
	}

	return append(mutateOps, deleteOps...), nil
}
