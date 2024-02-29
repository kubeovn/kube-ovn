package ovs

import (
	"context"
	"fmt"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

// CreateLoadBalancer create loadbalancer
func (c *OVNNbClient) CreateLoadBalancer(lbName, protocol, selectFields string) error {
	var (
		exist bool
		err   error
	)

	if exist, err = c.LoadBalancerExists(lbName); err != nil {
		klog.Errorf("failed to get lb: %v", err)
		return err
	}
	// found, ignore
	if exist {
		return nil
	}

	var (
		ops []ovsdb.Operation
		lb  *ovnnb.LoadBalancer
	)

	lb = &ovnnb.LoadBalancer{
		UUID:     ovsclient.NamedUUID(),
		Name:     lbName,
		Protocol: &protocol,
	}

	if len(selectFields) != 0 {
		lb.SelectionFields = []string{selectFields}
	}

	if ops, err = c.ovsDbClient.Create(lb); err != nil {
		return fmt.Errorf("generate operations for creating load balancer %s: %v", lbName, err)
	}

	if err = c.Transact("lb-add", ops); err != nil {
		return fmt.Errorf("create load balancer %s: %v", lbName, err)
	}
	return nil
}

// UpdateLoadBalancer update load balancer
func (c *OVNNbClient) UpdateLoadBalancer(lb *ovnnb.LoadBalancer, fields ...interface{}) error {
	var (
		ops []ovsdb.Operation
		err error
	)

	if ops, err = c.ovsDbClient.Where(lb).Update(lb, fields...); err != nil {
		return fmt.Errorf("generate operations for updating load balancer %s: %v", lb.Name, err)
	}

	if err = c.Transact("lb-update", ops); err != nil {
		return fmt.Errorf("update load balancer %s: %v", lb.Name, err)
	}
	return nil
}

// LoadBalancerAddVips adds or updates a vip
func (c *OVNNbClient) LoadBalancerAddVip(lbName, vip string, backends ...string) error {
	var (
		ops []ovsdb.Operation
		err error
	)

	if _, err = c.GetLoadBalancer(lbName, false); err != nil {
		klog.Errorf("failed to get lb health check: %v", err)
		return err
	}

	sort.Strings(backends)
	if ops, err = c.LoadBalancerOp(
		lbName,
		func(lb *ovnnb.LoadBalancer) []model.Mutation {
			var (
				mutations = make([]model.Mutation, 0, 2)
				value     = strings.Join(backends, ",")
			)

			if len(lb.Vips) != 0 {
				if lb.Vips[vip] == value {
					return nil
				}
				mutations = append(
					mutations,
					model.Mutation{
						Field:   &lb.Vips,
						Value:   map[string]string{vip: lb.Vips[vip]},
						Mutator: ovsdb.MutateOperationDelete,
					},
				)
			}
			mutations = append(
				mutations,
				model.Mutation{
					Field:   &lb.Vips,
					Value:   map[string]string{vip: value},
					Mutator: ovsdb.MutateOperationInsert,
				},
			)
			return mutations
		},
	); err != nil {
		return fmt.Errorf("failed to generate operations when adding vip %s with backends %v to load balancers %s: %v", vip, backends, lbName, err)
	}

	if ops != nil {
		if err = c.Transact("lb-add", ops); err != nil {
			return fmt.Errorf("failed to add vip %s with backends %v to load balancers %s: %v", vip, backends, lbName, err)
		}
	}
	return nil
}

// LoadBalancerDeleteVip deletes load balancer vip
func (c *OVNNbClient) LoadBalancerDeleteVip(lbName, vipEndpoint string, ignoreHealthCheck bool) error {
	var (
		ops  []ovsdb.Operation
		lb   *ovnnb.LoadBalancer
		lbhc *ovnnb.LoadBalancerHealthCheck
		err  error
	)

	lb, lbhc, err = c.GetLoadBalancerHealthCheck(lbName, vipEndpoint, true)
	if err != nil {
		klog.Errorf("failed to get lb health check: %v", err)
		return err
	}
	if !ignoreHealthCheck && lbhc != nil {
		klog.Infof("clean health check for lb %s with vip %s", lbName, vipEndpoint)
		// delete ip port mapping
		if err = c.LoadBalancerDeleteIPPortMapping(lbName, vipEndpoint); err != nil {
			klog.Errorf("failed to delete lb ip port mapping: %v", err)
			return err
		}
		if err = c.LoadBalancerDeleteHealthCheck(lbName, lbhc.UUID); err != nil {
			klog.Errorf("failed to delete lb health check: %v", err)
			return err
		}
	}
	if lb == nil || len(lb.Vips) == 0 {
		return nil
	}
	if _, ok := lb.Vips[vipEndpoint]; !ok {
		return nil
	}

	ops, err = c.LoadBalancerOp(
		lbName,
		func(lb *ovnnb.LoadBalancer) []model.Mutation {
			return []model.Mutation{
				{
					Field:   &lb.Vips,
					Value:   map[string]string{vipEndpoint: lb.Vips[vipEndpoint]},
					Mutator: ovsdb.MutateOperationDelete,
				},
			}
		},
	)
	if err != nil {
		return fmt.Errorf("failed to generate operations when deleting vip %s from load balancers %s: %v", vipEndpoint, lbName, err)
	}
	if len(ops) == 0 {
		return nil
	}

	if err = c.Transact("lb-add", ops); err != nil {
		return fmt.Errorf("failed to delete vip %s from load balancers %s: %v", vipEndpoint, lbName, err)
	}
	return nil
}

// SetLoadBalancerAffinityTimeout sets the LB's affinity timeout in seconds
func (c *OVNNbClient) SetLoadBalancerAffinityTimeout(lbName string, timeout int) error {
	var (
		options map[string]string
		lb      *ovnnb.LoadBalancer
		value   string
		err     error
	)

	if lb, err = c.GetLoadBalancer(lbName, false); err != nil {
		klog.Errorf("failed to get lb: %v", err)
		return err
	}

	if value = strconv.Itoa(timeout); len(lb.Options) != 0 && lb.Options["affinity_timeout"] == value {
		return nil
	}

	options = make(map[string]string, len(lb.Options)+1)
	for k, v := range lb.Options {
		options[k] = v
	}
	options["affinity_timeout"] = value

	lb.Options = options
	if err = c.UpdateLoadBalancer(lb, &lb.Options); err != nil {
		return fmt.Errorf("failed to set affinity timeout of lb %s to %d: %v", lbName, timeout, err)
	}
	return nil
}

// DeleteLoadBalancers delete several loadbalancer once
func (c *OVNNbClient) DeleteLoadBalancers(filter func(lb *ovnnb.LoadBalancer) bool) error {
	var (
		ops []ovsdb.Operation
		err error
	)

	if ops, err = c.ovsDbClient.WhereCache(
		func(lb *ovnnb.LoadBalancer) bool {
			if filter != nil {
				return filter(lb)
			}
			return true
		},
	).Delete(); err != nil {
		return fmt.Errorf("generate operations for delete load balancers: %v", err)
	}

	if err = c.Transact("lb-del", ops); err != nil {
		klog.Errorf("failed to del lbs: %v", err)
		return fmt.Errorf("delete load balancers : %v", err)
	}
	return nil
}

// DeleteLoadBalancer delete loadbalancer
func (c *OVNNbClient) DeleteLoadBalancer(lbName string) error {
	var (
		ops []ovsdb.Operation
		err error
	)

	ops, err = c.DeleteLoadBalancerOp(lbName)
	if err != nil {
		klog.Errorf("failed to get delete lb op: %v", err)
		return err
	}

	if err = c.Transact("lb-del", ops); err != nil {
		klog.Errorf("failed to del lb: %v", err)
		return fmt.Errorf("delete load balancer %s: %v", lbName, err)
	}
	return nil
}

// GetLoadBalancer get load balancer by name,
// it is because of lack name index that does't use OVNNbClient.Get
func (c *OVNNbClient) GetLoadBalancer(lbName string, ignoreNotFound bool) (*ovnnb.LoadBalancer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var (
		lbList []ovnnb.LoadBalancer
		err    error
	)

	lbList = make([]ovnnb.LoadBalancer, 0)
	if err = c.ovsDbClient.WhereCache(
		func(lb *ovnnb.LoadBalancer) bool {
			return lb.Name == lbName
		},
	).List(ctx, &lbList); err != nil {
		return nil, fmt.Errorf("failed to list load balancer %q: %v", lbName, err)
	}

	switch {
	// not found
	case len(lbList) == 0:
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found load balancer %q", lbName)
	case len(lbList) > 1:
		return nil, fmt.Errorf("more than one load balancer with same name %q", lbName)
	default:
		// #nosec G602
		return &lbList[0], nil
	}
}

func (c *OVNNbClient) LoadBalancerExists(lbName string) (bool, error) {
	lrp, err := c.GetLoadBalancer(lbName, true)
	return lrp != nil, err
}

// ListLoadBalancers list all load balancers
func (c *OVNNbClient) ListLoadBalancers(filter func(lb *ovnnb.LoadBalancer) bool) ([]ovnnb.LoadBalancer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var (
		lbList []ovnnb.LoadBalancer
		err    error
	)

	lbList = make([]ovnnb.LoadBalancer, 0)
	if err = c.ovsDbClient.WhereCache(
		func(lb *ovnnb.LoadBalancer) bool {
			if filter != nil {
				return filter(lb)
			}

			return true
		},
	).List(ctx, &lbList); err != nil {
		return nil, fmt.Errorf("failed to list load balancer: %v", err)
	}
	return lbList, nil
}

func (c *OVNNbClient) LoadBalancerOp(lbName string, mutationsFunc ...func(lb *ovnnb.LoadBalancer) []model.Mutation) ([]ovsdb.Operation, error) {
	var (
		mutations []model.Mutation
		ops       []ovsdb.Operation
		lb        *ovnnb.LoadBalancer
		err       error
	)

	if lb, err = c.GetLoadBalancer(lbName, false); err != nil {
		klog.Error(err)
		return nil, err
	}

	if len(mutationsFunc) == 0 {
		return nil, nil
	}

	mutations = make([]model.Mutation, 0, len(mutationsFunc))
	for _, f := range mutationsFunc {
		if mutation := f(lb); mutation != nil {
			mutations = append(mutations, mutation...)
		}
	}
	if len(mutations) == 0 {
		return nil, nil
	}

	if ops, err = c.ovsDbClient.Where(lb).Mutate(lb, mutations...); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("generate operations for mutating load balancer %s: %v", lb.Name, err)
	}
	return ops, nil
}

// DeleteLoadBalancerOp create operation which delete load balancer
func (c *OVNNbClient) DeleteLoadBalancerOp(lbName string) ([]ovsdb.Operation, error) {
	var (
		ops []ovsdb.Operation
		lb  *ovnnb.LoadBalancer
		err error
	)

	if lb, err = c.GetLoadBalancer(lbName, true); err != nil {
		klog.Error(err)
		return nil, err
	}
	// not found, skip
	if lb == nil {
		return nil, nil
	}

	if ops, err = c.Where(lb).Delete(); err != nil {
		klog.Error(err)
		return nil, err
	}
	return ops, nil
}

// LoadBalancerAddIPPortMapping add load balancer ip port mapping
func (c *OVNNbClient) LoadBalancerAddIPPortMapping(lbName, vipEndpoint string, mappings map[string]string) error {
	if len(mappings) == 0 {
		return nil
	}

	var (
		ops []ovsdb.Operation
		err error
	)

	if ops, err = c.LoadBalancerOp(
		lbName,
		func(lb *ovnnb.LoadBalancer) []model.Mutation {
			return []model.Mutation{
				{
					Field:   &lb.IPPortMappings,
					Value:   mappings,
					Mutator: ovsdb.MutateOperationInsert,
				},
			}
		},
	); err != nil {
		return fmt.Errorf("failed to generate operations when adding ip port mapping with vip %v to load balancers %s: %v", vipEndpoint, lbName, err)
	}

	if err = c.Transact("lb-add", ops); err != nil {
		return fmt.Errorf("failed to add ip port mapping with vip %v to load balancers %s: %v", vipEndpoint, lbName, err)
	}
	return nil
}

// LoadBalancerDeleteIPPortMapping delete load balancer ip port mapping
func (c *OVNNbClient) LoadBalancerDeleteIPPortMapping(lbName, vipEndpoint string) error {
	var (
		ops      []ovsdb.Operation
		lb       *ovnnb.LoadBalancer
		mappings map[string]string
		vip      string
		err      error
	)

	if lb, err = c.GetLoadBalancer(lbName, true); err != nil {
		klog.Errorf("failed to get lb health check: %v", err)
		return err
	}

	if lb == nil {
		klog.Infof("lb %s already deleted", lbName)
		return nil
	}
	if len(lb.IPPortMappings) == 0 {
		klog.Infof("lb %s has no ip port mapping", lbName)
		return nil
	}

	if vip, _, err = net.SplitHostPort(vipEndpoint); err != nil {
		err := fmt.Errorf("failed to split host port: %v", err)
		klog.Error(err)
		return err
	}

	mappings = lb.IPPortMappings
	for portIP, portMapVip := range lb.IPPortMappings {
		splits := strings.Split(portMapVip, ":")
		if len(splits) == 2 && splits[1] == vip {
			delete(mappings, portIP)
		}
	}

	if ops, err = c.LoadBalancerOp(
		lbName,
		func(lb *ovnnb.LoadBalancer) []model.Mutation {
			return []model.Mutation{
				{
					Field:   &lb.IPPortMappings,
					Value:   mappings,
					Mutator: ovsdb.MutateOperationDelete,
				},
			}
		},
	); err != nil {
		return fmt.Errorf("failed to generate operations when deleting ip port mapping %s from load balancers %s: %v", vipEndpoint, lbName, err)
	}
	if len(ops) == 0 {
		return nil
	}
	if err = c.Transact("lb-del", ops); err != nil {
		return fmt.Errorf("failed to delete ip port mappings %s from load balancer %s: %v", vipEndpoint, lbName, err)
	}
	return nil
}

// LoadBalancerUpdateIPPortMapping update load balancer ip port mapping
func (c *OVNNbClient) LoadBalancerUpdateIPPortMapping(lbName, vipEndpoint string, ipPortMappings map[string]string) error {
	if len(ipPortMappings) != 0 {
		ops, err := c.LoadBalancerOp(
			lbName,
			func(lb *ovnnb.LoadBalancer) []model.Mutation {
				return []model.Mutation{
					{
						Field:   &lb.IPPortMappings,
						Value:   ipPortMappings,
						Mutator: ovsdb.MutateOperationInsert,
					},
				}
			},
		)
		if err != nil {
			return fmt.Errorf("failed to generate operations when adding ip port mapping with vip %v to load balancers %s: %v", vipEndpoint, lbName, err)
		}
		if err = c.Transact("lb-add", ops); err != nil {
			return fmt.Errorf("failed to add ip port mapping with vip %v to load balancers %s: %v", vipEndpoint, lbName, err)
		}
	}
	return nil
}

// LoadBalancerAddHealthCheck adds health check
func (c *OVNNbClient) LoadBalancerAddHealthCheck(lbName, vipEndpoint string, ignoreHealthCheck bool, ipPortMapping, externals map[string]string) error {
	klog.Infof("lb %s health check use ip port mapping %v", lbName, ipPortMapping)
	if err := c.LoadBalancerUpdateIPPortMapping(lbName, vipEndpoint, ipPortMapping); err != nil {
		klog.Errorf("failed to update lb ip port mapping: %v", err)
		return err
	}
	if !ignoreHealthCheck {
		klog.Infof("add health check for lb %s with vip %s and health check vip maps %v", lbName, vipEndpoint, ipPortMapping)
		if err := c.AddLoadBalancerHealthCheck(lbName, vipEndpoint, externals); err != nil {
			klog.Errorf("failed to create lb health check: %v", err)
			return err
		}
	}
	return nil
}

// LoadBalancerDeleteHealthCheck delete load balancer health check
func (c *OVNNbClient) LoadBalancerDeleteHealthCheck(lbName, uuid string) error {
	var (
		ops []ovsdb.Operation
		lb  *ovnnb.LoadBalancer
		err error
	)

	if lb, err = c.GetLoadBalancer(lbName, false); err != nil {
		klog.Errorf("failed to get lb: %v", err)
		return err
	}

	if slices.Contains(lb.HealthCheck, uuid) {
		ops, err = c.LoadBalancerOp(
			lbName,
			func(lb *ovnnb.LoadBalancer) []model.Mutation {
				return []model.Mutation{
					{
						Field:   &lb.HealthCheck,
						Value:   []string{uuid},
						Mutator: ovsdb.MutateOperationDelete,
					},
				}
			},
		)
		if err != nil {
			return fmt.Errorf("failed to generate operations when deleting health check %s from load balancers %s: %v", uuid, lbName, err)
		}
		if len(ops) == 0 {
			return nil
		}
		if err = c.Transact("lb-hc-del", ops); err != nil {
			return fmt.Errorf("failed to delete health check %s from load balancers %s: %v", uuid, lbName, err)
		}
	}

	return nil
}

// LoadBalancerUpdateHealthCheckOp create operations add to or delete health check from it
func (c *OVNNbClient) LoadBalancerUpdateHealthCheckOp(lbName string, lbhcUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(lbhcUUIDs) == 0 {
		return nil, nil
	}

	return c.LoadBalancerOp(
		lbName,
		func(lb *ovnnb.LoadBalancer) []model.Mutation {
			return []model.Mutation{
				{
					Field:   &lb.HealthCheck,
					Value:   lbhcUUIDs,
					Mutator: op,
				},
			}
		},
	)
}
