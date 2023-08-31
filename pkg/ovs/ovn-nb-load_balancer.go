package ovs

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// CreateLoadBalancer create loadbalancer
func (c *ovnNbClient) CreateLoadBalancer(lbName, protocol, selectFields string) error {
	exist, err := c.LoadBalancerExists(lbName)
	if err != nil {
		klog.Error(err)
		return err
	}

	// found, ignore
	if exist {
		return nil
	}

	lb := &ovnnb.LoadBalancer{
		UUID:     ovsclient.NamedUUID(),
		Name:     lbName,
		Protocol: &protocol,
	}

	if len(selectFields) != 0 {
		lb.SelectionFields = []string{selectFields}
	}

	op, err := c.ovsDbClient.Create(lb)
	if err != nil {
		return fmt.Errorf("generate operations for creating load balancer %s: %v", lbName, err)
	}

	if err := c.Transact("lb-add", op); err != nil {
		return fmt.Errorf("create load balancer %s: %v", lbName, err)
	}

	return nil
}

// UpdateLoadBalancer update load balancer
func (c *ovnNbClient) UpdateLoadBalancer(lb *ovnnb.LoadBalancer, fields ...interface{}) error {
	op, err := c.ovsDbClient.Where(lb).Update(lb, fields...)
	if err != nil {
		return fmt.Errorf("generate operations for updating load balancer %s: %v", lb.Name, err)
	}

	if err = c.Transact("lb-update", op); err != nil {
		return fmt.Errorf("update load balancer %s: %v", lb.Name, err)
	}

	return nil
}

// LoadBalancerAddVips adds or updates a vip
func (c *ovnNbClient) LoadBalancerAddVip(lbName, vip string, mappings map[string]string, backends ...string) error {
	var (
		lbhc *ovnnb.LoadBalancerHealthCheck
		ops  []ovsdb.Operation
		err  error
	)

	sort.Strings(backends)
	ops, err = c.LoadBalancerOp(
		lbName,
		func(lb *ovnnb.LoadBalancer) ([]model.Mutation, error) {
			var (
				mutations = make([]model.Mutation, 0, 2)
				value     = strings.Join(backends, ",")
			)

			if len(lb.Vips) != 0 {
				if lb.Vips[vip] == value {
					return nil, nil
				}
				mutations = append(mutations, model.Mutation{
					Field:   &lb.Vips,
					Value:   map[string]string{vip: lb.Vips[vip]},
					Mutator: ovsdb.MutateOperationDelete,
				})
			}
			mutations = append(
				mutations,
				model.Mutation{
					Field:   &lb.Vips,
					Value:   map[string]string{vip: value},
					Mutator: ovsdb.MutateOperationInsert,
				},
			)
			return mutations, nil
		},
	)

	if err != nil {
		return fmt.Errorf("failed to generate operations when adding vip %s with backends %v to load balancers %s: %v", vip, backends, lbName, err)
	}
	if err = c.Transact("lb-add", ops); err != nil {
		return fmt.Errorf("failed to add vip %s with backends %v to load balancers %s: %v", vip, backends, lbName, err)
	}
	// update ip port mapping
	if err = c.LoadBalancerUpdateIPPortMapping(lbName, vip, mappings); err != nil {
		return err
	}
	// add health check
	if err = c.CreateLoadBalancerHealthCheck(lbName, vip); err != nil {
		return err
	}
	if lbhc, err = c.GetLoadBalancerHealthCheck(lbName, vip, false); err != nil {
		return err
	}
	if err = c.LoadBalancerAddHealthCheck(lbName, lbhc.UUID); err != nil {
		return err
	}
	return nil
}

// LoadBalancerDeleteVip deletes load balancer vip
func (c *ovnNbClient) LoadBalancerDeleteVip(lbName string, vip string) error {
	var (
		lbhc *ovnnb.LoadBalancerHealthCheck
		ops  []ovsdb.Operation
		err  error
	)
	// delete ip port mapping
	if err = c.LoadBalancerDeleteIPPortMapping(lbName, vip, nil); err != nil {
		return err
	}
	// delete health check
	if lbhc, err = c.GetLoadBalancerHealthCheck(lbName, vip, true); err != nil {
		return err
	}
	if lbhc != nil {
		if err = c.LoadBalancerDeleteHealthCheck(lbName, lbhc.UUID); err != nil {
			return err
		}
	}
	ops, err = c.LoadBalancerOp(
		lbName,
		func(lb *ovnnb.LoadBalancer) ([]model.Mutation, error) {
			if len(lb.Vips) == 0 {
				return nil, nil
			}
			if _, ok := lb.Vips[vip]; !ok {
				return nil, nil
			}

			return []model.Mutation{
				{
					Field:   &lb.Vips,
					Value:   map[string]string{vip: lb.Vips[vip]},
					Mutator: ovsdb.MutateOperationDelete,
				},
			}, nil
		},
	)

	if err != nil {
		return fmt.Errorf("failed to generate operations when deleting vip %s from load balancers %s: %v", vip, lbName, err)
	}
	if len(ops) == 0 {
		return nil
	}
	if err = c.Transact("lb-add", ops); err != nil {
		return fmt.Errorf("failed to delete vip %s from load balancers %s: %v", vip, lbName, err)
	}
	return nil
}

// SetLoadBalancerAffinityTimeout sets the LB's affinity timeout in seconds
func (c *ovnNbClient) SetLoadBalancerAffinityTimeout(lbName string, timeout int) error {
	lb, err := c.GetLoadBalancer(lbName, false)
	if err != nil {
		klog.Error(err)
		return err
	}
	value := strconv.Itoa(timeout)
	if len(lb.Options) != 0 && lb.Options["affinity_timeout"] == value {
		return nil
	}

	options := make(map[string]string, len(lb.Options)+1)
	for k, v := range lb.Options {
		options[k] = v
	}
	options["affinity_timeout"] = value
	lb.Options = options
	if err := c.UpdateLoadBalancer(lb, &lb.Options); err != nil {
		return fmt.Errorf("failed to set affinity timeout of lb %s to %d: %v", lbName, timeout, err)
	}

	return nil
}

// DeleteLoadBalancers delete several loadbalancer once
func (c *ovnNbClient) DeleteLoadBalancers(filter func(lb *ovnnb.LoadBalancer) bool) error {
	op, err := c.ovsDbClient.WhereCache(func(lb *ovnnb.LoadBalancer) bool {
		if filter != nil {
			return filter(lb)
		}

		return true
	}).Delete()

	if err != nil {
		return fmt.Errorf("generate operations for delete load balancers: %v", err)
	}

	if err := c.Transact("lb-del", op); err != nil {
		return fmt.Errorf("delete load balancers : %v", err)
	}

	return nil
}

// DeleteLoadBalancer delete loadbalancer
func (c *ovnNbClient) DeleteLoadBalancer(lbName string) error {
	op, err := c.DeleteLoadBalancerOp(lbName)
	if err != nil {
		klog.Error(err)
		return err
	}

	if err := c.Transact("lb-del", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("delete load balancer %s: %v", lbName, err)
	}

	return nil
}

// GetLoadBalancer get load balancer by name,
// it is because of lack name index that does't use ovnNbClient.Get
func (c *ovnNbClient) GetLoadBalancer(lbName string, ignoreNotFound bool) (*ovnnb.LoadBalancer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lbList := make([]ovnnb.LoadBalancer, 0)
	if err := c.ovsDbClient.WhereCache(func(lb *ovnnb.LoadBalancer) bool {
		return lb.Name == lbName
	}).List(ctx, &lbList); err != nil {
		return nil, fmt.Errorf("list load balancer %q: %v", lbName, err)
	}

	// not found
	if len(lbList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found load balancer %q", lbName)
	}

	if len(lbList) > 1 {
		return nil, fmt.Errorf("more than one load balancer with same name %q", lbName)
	}

	// #nosec G602
	return &lbList[0], nil
}

func (c *ovnNbClient) LoadBalancerExists(lbName string) (bool, error) {
	lrp, err := c.GetLoadBalancer(lbName, true)
	return lrp != nil, err
}

// ListLoadBalancers list all load balancers
func (c *ovnNbClient) ListLoadBalancers(filter func(lb *ovnnb.LoadBalancer) bool) ([]ovnnb.LoadBalancer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lbList := make([]ovnnb.LoadBalancer, 0)
	if err := c.ovsDbClient.WhereCache(func(lb *ovnnb.LoadBalancer) bool {
		if filter != nil {
			return filter(lb)
		}

		return true
	}).List(ctx, &lbList); err != nil {
		return nil, fmt.Errorf("list load balancer: %v", err)
	}

	return lbList, nil
}

func (c *ovnNbClient) LoadBalancerOp(lbName string, mutationsFunc ...func(lb *ovnnb.LoadBalancer) ([]model.Mutation, error)) ([]ovsdb.Operation, error) {
	lb, err := c.GetLoadBalancer(lbName, false)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	if len(mutationsFunc) == 0 {
		return nil, nil
	}

	mutations := make([]model.Mutation, 0, len(mutationsFunc))
	for _, f := range mutationsFunc {
		m, e := f(lb)
		if e != nil {
			return nil, e
		}
		if len(m) != 0 {
			mutations = append(mutations, m...)
		}
	}
	if len(mutations) == 0 {
		return nil, nil
	}

	ops, err := c.ovsDbClient.Where(lb).Mutate(lb, mutations...)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("generate operations for mutating load balancer %s: %v", lb.Name, err)
	}

	return ops, nil
}

// DeleteLoadBalancerOp create operation which delete load balancer
func (c *ovnNbClient) DeleteLoadBalancerOp(lbName string) ([]ovsdb.Operation, error) {
	lb, err := c.GetLoadBalancer(lbName, true)

	if err != nil {
		klog.Error(err)
		return nil, err
	}

	// not found, skip
	if lb == nil {
		return nil, nil
	}

	op, err := c.Where(lb).Delete()
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	return op, nil
}

// LoadBalancerAddIPPortMapping add load balancer ip port mapping
func (c *ovnNbClient) LoadBalancerAddIPPortMapping(lbName, vip string, mappings map[string]string) error {
	if len(mappings) == 0 {
		return nil
	}

	var (
		ops []ovsdb.Operation
		err error
	)

	ops, err = c.LoadBalancerOp(
		lbName,
		func(lb *ovnnb.LoadBalancer) ([]model.Mutation, error) {
			var (
				mutations = make([]model.Mutation, 0, 1)
			)

			mutations = append(
				mutations,
				model.Mutation{
					Field:   &lb.IPPortMappings,
					Value:   mappings,
					Mutator: ovsdb.MutateOperationInsert,
				},
			)
			return mutations, nil
		},
	)

	if err != nil {
		return fmt.Errorf("failed to generate operations when adding ip port mapping with vip %v to load balancers %s: %v", vip, lbName, err)
	}
	if err = c.Transact("lb-add", ops); err != nil {
		return fmt.Errorf("failed to add ip port mapping with vip %v to load balancers %s: %v", vip, lbName, err)
	}
	return nil
}

// LoadBalancerDeleteIPPortMapping delete load balancer ip port mapping
func (c *ovnNbClient) LoadBalancerDeleteIPPortMapping(lbName, vip string, mappings map[string]string) error {
	var (
		ops []ovsdb.Operation
		err error
	)

	ops, err = c.LoadBalancerOp(
		lbName,
		func(lb *ovnnb.LoadBalancer) ([]model.Mutation, error) {
			if len(lb.IPPortMappings) == 0 {
				return nil, nil
			}

			var (
				host string
				err  error
			)

			if len(mappings) == 0 {
				backends, ok := lb.Vips[vip]
				if !ok {
					return nil, nil
				}
				mappings = make(map[string]string)

				for _, backend := range strings.Split(backends, ",") {
					if strings.Compare(backend, "") != 0 {
						if host, _, err = net.SplitHostPort(backend); err != nil {
							return nil, err
						}

						if mp, ex := lb.IPPortMappings[host]; ex {
							mappings[host] = mp
						}
					}
				}
			}

			if len(mappings) != 0 {
				for ip, backends := range lb.Vips {
					if strings.Compare(ip, vip) != 0 &&
						strings.Compare(backends, "") != 0 {
						for _, backend := range strings.Split(backends, ",") {
							if strings.Compare(backend, "") != 0 {
								if host, _, err = net.SplitHostPort(backend); err != nil {
									return nil, err
								}
								// backend used by other vip
								delete(mappings, host)
							}
						}
					}
				}
			}

			if len(mappings) == 0 {
				return nil, nil
			}

			return []model.Mutation{
				{
					Field:   &lb.IPPortMappings,
					Value:   mappings,
					Mutator: ovsdb.MutateOperationDelete,
				},
			}, nil
		},
	)

	if err != nil {
		return fmt.Errorf("failed to generate operations when deleting ip port mapping %s from load balancers %s: %v", vip, lbName, err)
	}
	if len(ops) == 0 {
		return nil
	}
	if err = c.Transact("lb-del", ops); err != nil {
		return fmt.Errorf("failed to delete ip port mappings %s from load balancer %s: %v", vip, lbName, err)
	}
	return nil
}

// LoadBalancerUpdateIPPortMapping update load balancer ip port mapping
func (c *ovnNbClient) LoadBalancerUpdateIPPortMapping(lbName, vip string, mappings map[string]string) error {
	var (
		ops []ovsdb.Operation
		err error
	)

	ops, err = c.LoadBalancerOp(
		lbName,
		func(lb *ovnnb.LoadBalancer) ([]model.Mutation, error) {
			var (
				mutations = make([]model.Mutation, 0, 2)
				exists    = make(map[string]string)
				host      string
			)

			if backends, exist := lb.Vips[vip]; exist {
				for _, backend := range strings.Split(backends, ",") {
					if backend != "" {
						if host, _, err := net.SplitHostPort(backend); err != nil {
							return nil, err
						} else {
							if m, ex := lb.IPPortMappings[host]; ex {
								exists[host] = m
							}
						}
					}
				}
			}

			if len(exists) != 0 {
				for ip, backends := range lb.Vips {
					if strings.Compare(ip, vip) != 0 &&
						strings.Compare(backends, "") != 0 {
						for _, backend := range strings.Split(backends, ",") {
							if strings.Compare(backend, "") != 0 {
								if host, _, err = net.SplitHostPort(backend); err != nil {
									return nil, err
								}
								// backend used by other vip
								delete(exists, host)
							}
						}
					}
				}
			}

			mutations = append(
				mutations,
				model.Mutation{
					Field:   &lb.IPPortMappings,
					Value:   exists,
					Mutator: ovsdb.MutateOperationDelete,
				},
			)

			if len(mappings) != 0 {
				mutations = append(
					mutations,
					model.Mutation{
						Field:   &lb.IPPortMappings,
						Value:   mappings,
						Mutator: ovsdb.MutateOperationInsert,
					},
				)
			}

			return mutations, nil
		},
	)

	if err != nil {
		return fmt.Errorf("failed to generate operations when adding ip port mapping with vip %v to load balancers %s: %v", vip, lbName, err)
	}
	if err = c.Transact("lb-add", ops); err != nil {
		return fmt.Errorf("failed to add ip port mapping with vip %v to load balancers %s: %v", vip, lbName, err)
	}
	return nil
}

// LoadBalancerAddHealthCheck add load balancer health check
func (c *ovnNbClient) LoadBalancerAddHealthCheck(lbName, uuid string) error {
	var (
		ops []ovsdb.Operation
		lb  *ovnnb.LoadBalancer
		err error
	)

	if lb, err = c.GetLoadBalancer(lbName, false); err != nil {
		return err
	}

	if !util.ContainsString(lb.HealthCheck, uuid) {
		ops, err = c.LoadBalancerOp(
			lbName,
			func(lb *ovnnb.LoadBalancer) ([]model.Mutation, error) {
				var (
					mutations = make([]model.Mutation, 0, 1)
				)

				mutations = append(
					mutations,
					model.Mutation{
						Field:   &lb.HealthCheck,
						Value:   append(lb.HealthCheck, uuid),
						Mutator: ovsdb.MutateOperationInsert,
					},
				)
				return mutations, nil
			},
		)

		if err != nil {
			return fmt.Errorf("failed to generate operations when adding health check with uuid %v to load balancers %s: %v", uuid, lbName, err)
		}
		if err = c.Transact("lb-add", ops); err != nil {
			return fmt.Errorf("failed to add health check with uuid %v to load balancers %s: %v", uuid, lbName, err)
		}
	}

	return nil
}

// LoadBalancerDeleteHealthCheck delete load balancer health check
func (c *ovnNbClient) LoadBalancerDeleteHealthCheck(lbName, uuid string) error {
	var (
		ops []ovsdb.Operation
		lb  *ovnnb.LoadBalancer
		err error
	)

	if lb, err = c.GetLoadBalancer(lbName, false); err != nil {
		return err
	}

	if util.ContainsString(lb.HealthCheck, uuid) {
		ops, err = c.LoadBalancerOp(
			lbName,
			func(lb *ovnnb.LoadBalancer) ([]model.Mutation, error) {
				if len(lb.HealthCheck) == 0 {
					return nil, nil
				}

				return []model.Mutation{
					{
						Field:   &lb.HealthCheck,
						Value:   []string{uuid},
						Mutator: ovsdb.MutateOperationDelete,
					},
				}, nil
			},
		)

		if err != nil {
			return fmt.Errorf("failed to generate operations when deleting health check %s from load balancers %s: %v", uuid, lbName, err)
		}
		if len(ops) == 0 {
			return nil
		}
		if err = c.Transact("lb-del", ops); err != nil {
			return fmt.Errorf("failed to delete health check %s from load balancers %s: %v", uuid, lbName, err)
		}
	}

	return nil
}
