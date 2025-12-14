package ovs

import (
	"context"
	"fmt"
	"maps"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
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
		ExternalIDs: map[string]string{
			"vendor": util.CniTypeName,
		},
	}

	if len(selectFields) != 0 {
		lb.SelectionFields = []string{selectFields}
	}

	if ops, err = c.Create(lb); err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for creating load balancer %s: %w", lbName, err)
	}

	if err = c.Transact("lb-add", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("create load balancer %s: %w", lbName, err)
	}
	return nil
}

// UpdateLoadBalancer update load balancer
func (c *OVNNbClient) UpdateLoadBalancer(lb *ovnnb.LoadBalancer, fields ...any) error {
	var (
		ops []ovsdb.Operation
		err error
	)

	if ops, err = c.ovsDbClient.Where(lb).Update(lb, fields...); err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for updating load balancer %s: %w", lb.Name, err)
	}

	if err = c.Transact("lb-update", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("update load balancer %s: %w", lb.Name, err)
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
		return fmt.Errorf("failed to generate operations when adding vip %s with backends %v to load balancers %s: %w", vip, backends, lbName, err)
	}

	if ops != nil {
		if err = c.Transact("lb-add", ops); err != nil {
			klog.Error(err)
			return fmt.Errorf("failed to add vip %s with backends %v to load balancers %s: %w", vip, backends, lbName, err)
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
		klog.Error(err)
		return fmt.Errorf("failed to generate operations when deleting vip %s from load balancers %s: %w", vipEndpoint, lbName, err)
	}
	if len(ops) == 0 {
		return nil
	}

	if err = c.Transact("lb-add", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to delete vip %s from load balancers %s: %w", vipEndpoint, lbName, err)
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
	maps.Copy(options, lb.Options)
	options["affinity_timeout"] = value

	lb.Options = options
	if err = c.UpdateLoadBalancer(lb, &lb.Options); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to set affinity timeout of lb %s to %d: %w", lbName, timeout, err)
	}
	return nil
}

// SetLoadBalancerPreferLocalBackend sets the LB's affinity timeout in seconds
func (c *OVNNbClient) SetLoadBalancerPreferLocalBackend(lbName string, preferLocalBackend bool) error {
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

	if preferLocalBackend {
		value = "true"
	} else {
		value = "false"
	}
	if len(lb.Options) != 0 && lb.Options["prefer_local_backend"] == value {
		return nil
	}

	options = make(map[string]string, len(lb.Options)+1)
	maps.Copy(options, lb.Options)
	options["prefer_local_backend"] = value

	lb.Options = options
	if err = c.UpdateLoadBalancer(lb, &lb.Options); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to set prefer local backend of lb %s to %s: %w", lbName, value, err)
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
		klog.Error(err)
		return fmt.Errorf("generate operations for delete load balancers: %w", err)
	}

	if err = c.Transact("lb-del", ops); err != nil {
		klog.Errorf("failed to del lbs: %v", err)
		return fmt.Errorf("delete load balancers : %w", err)
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
		return fmt.Errorf("delete load balancer %s: %w", lbName, err)
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
		klog.Error(err)
		return nil, fmt.Errorf("failed to list load balancer %q: %w", lbName, err)
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
		klog.Error(err)
		return nil, fmt.Errorf("failed to list load balancer: %w", err)
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
		return nil, fmt.Errorf("generate operations for mutating load balancer %s: %w", lb.Name, err)
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
		klog.Error(err)
		return fmt.Errorf("failed to generate operations when adding ip port mapping with vip %v to load balancers %s: %w", vipEndpoint, lbName, err)
	}

	if err = c.Transact("lb-add", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to add ip port mapping with vip %v to load balancers %s: %w", vipEndpoint, lbName, err)
	}
	return nil
}

// LoadBalancerDeleteIPPortMapping deletes IP port mappings for a specific VIP from a load balancer.
// This function ensures that only backend IPs that are no longer referenced by any VIP are removed.
func (c *OVNNbClient) LoadBalancerDeleteIPPortMapping(lbName, vipEndpoint string) error {
	lb, err := c.getLoadBalancerForDeletion(lbName)
	if err != nil {
		klog.Errorf("failed to get load balancer for deletion: %v", err)
		return err
	}
	if lb == nil {
		return nil
	}

	targetBackendIPs, err := c.extractBackendIPsFromVIP(lb, vipEndpoint)
	if err != nil {
		klog.Errorf("failed to extract backend IPs from VIP: %v", err)
		return err
	}
	if len(targetBackendIPs) == 0 {
		return nil
	}

	unusedBackendIPs := c.findUnusedBackendIPs(lb, vipEndpoint, targetBackendIPs)
	return c.deleteUnusedIPPortMappings(lbName, vipEndpoint, unusedBackendIPs)
}

// getLoadBalancerForDeletion retrieves the load balancer and performs initial validation
func (c *OVNNbClient) getLoadBalancerForDeletion(lbName string) (*ovnnb.LoadBalancer, error) {
	lb, err := c.GetLoadBalancer(lbName, true)
	if err != nil {
		klog.Errorf("failed to get load balancer %s: %v", lbName, err)
		return nil, err
	}

	if lb == nil {
		klog.Infof("load balancer %s already deleted", lbName)
		return nil, nil
	}

	if len(lb.IPPortMappings) == 0 {
		klog.Infof("load balancer %s has no IP port mappings", lbName)
		return nil, nil
	}

	return lb, nil
}

// extractBackendIPsFromVIP extracts the backend IPs that are used by a specific VIP
func (c *OVNNbClient) extractBackendIPsFromVIP(lb *ovnnb.LoadBalancer, vipEndpoint string) (map[string]bool, error) {
	vipBackends, exists := lb.Vips[vipEndpoint]
	if !exists {
		klog.Infof("VIP %s not found in load balancer", vipEndpoint)
		return nil, nil
	}

	backendIPs := make(map[string]bool)

	for backend := range strings.SplitSeq(vipBackends, ",") {
		if backendIP, _, err := net.SplitHostPort(backend); err == nil {
			backendIPs[backendIP] = true
		}
	}

	klog.V(4).Infof("VIP %s uses %d backend IPs: %v", vipEndpoint, len(backendIPs), getMapKeys(backendIPs))
	return backendIPs, nil
}

// findUnusedBackendIPs identifies which backend IPs are no longer used by any other VIP
func (c *OVNNbClient) findUnusedBackendIPs(lb *ovnnb.LoadBalancer, targetVIP string, targetBackendIPs map[string]bool) map[string]string {
	unusedBackendIPs := make(map[string]string)

	for backendIP := range targetBackendIPs {
		if !c.isBackendIPStillUsed(lb, targetVIP, backendIP) {
			if portMapping, exists := lb.IPPortMappings[backendIP]; exists {
				unusedBackendIPs[backendIP] = portMapping
			}
		}
	}

	klog.V(4).Infof("Found %d unused backend IPs for VIP %s", len(unusedBackendIPs), targetVIP)
	return unusedBackendIPs
}

// isBackendIPStillUsed checks if a backend IP is still referenced by any other VIP
func (c *OVNNbClient) isBackendIPStillUsed(lb *ovnnb.LoadBalancer, targetVIP, backendIP string) bool {
	for otherVIP, otherBackends := range lb.Vips {
		if otherVIP == targetVIP {
			continue
		}

		for otherBackend := range strings.SplitSeq(otherBackends, ",") {
			if otherBackendIP, _, err := net.SplitHostPort(otherBackend); err == nil && otherBackendIP == backendIP {
				klog.V(5).Infof("Backend IP %s is still used by VIP %s", backendIP, otherVIP)
				return true
			}
		}
	}

	klog.V(5).Infof("Backend IP %s is no longer used by any other VIP", backendIP)
	return false
}

// deleteUnusedIPPortMappings performs the actual deletion of unused IP port mappings
func (c *OVNNbClient) deleteUnusedIPPortMappings(lbName, vipEndpoint string, unusedBackendIPs map[string]string) error {
	if len(unusedBackendIPs) == 0 {
		klog.Infof("no unused backend IPs to delete for VIP %s in load balancer %s", vipEndpoint, lbName)
		return nil
	}

	ops, err := c.LoadBalancerOp(
		lbName,
		func(lb *ovnnb.LoadBalancer) []model.Mutation {
			return []model.Mutation{
				{
					Field:   &lb.IPPortMappings,
					Value:   unusedBackendIPs,
					Mutator: ovsdb.MutateOperationDelete,
				},
			}
		},
	)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to generate operations when deleting IP port mappings for VIP %s from load balancer %s: %w", vipEndpoint, lbName, err)
	}

	if len(ops) == 0 {
		return nil
	}

	if err = c.Transact("lb-del", ops); err != nil {
		return fmt.Errorf("failed to delete IP port mappings for VIP %s from load balancer %s: %w", vipEndpoint, lbName, err)
	}

	klog.Infof("successfully deleted %d unused backend IPs for VIP %s from load balancer %s",
		len(unusedBackendIPs), vipEndpoint, lbName)
	return nil
}

// getMapKeys returns the keys of a map as a slice
func getMapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// LoadBalancerUpdateIPPortMapping update load balancer ip port mapping
func (c *OVNNbClient) LoadBalancerUpdateIPPortMapping(lbName, vipEndpoint string, ipPortMappings map[string]string) error {
	ops, err := c.LoadBalancerOp(
		lbName,
		func(lb *ovnnb.LoadBalancer) []model.Mutation {
			// Delete from the IPPortMappings any outdated mapping
			mappingToDelete := make(map[string]string)
			for portIP, portMapVip := range lb.IPPortMappings {
				if _, ok := ipPortMappings[portIP]; !ok {
					mappingToDelete[portIP] = portMapVip
				}
			}

			if len(mappingToDelete) > 0 {
				klog.Infof("deleting outdated entry from ipportmapping %v", mappingToDelete)
			}

			return []model.Mutation{
				{
					Field:   &lb.IPPortMappings,
					Value:   mappingToDelete,
					Mutator: ovsdb.MutateOperationDelete,
				},
				{
					Field:   &lb.IPPortMappings,
					Value:   ipPortMappings,
					Mutator: ovsdb.MutateOperationInsert,
				},
			}
		},
	)
	if err != nil {
		return fmt.Errorf("failed to generate operations when adding ip port mapping with vip %v to load balancers %s: %w", vipEndpoint, lbName, err)
	}
	if err = c.Transact("lb-add", ops); err != nil {
		return fmt.Errorf("failed to add ip port mapping with vip %v to load balancers %s: %w", vipEndpoint, lbName, err)
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
			return fmt.Errorf("failed to generate operations when deleting health check %s from load balancers %s: %w", uuid, lbName, err)
		}
		if len(ops) == 0 {
			return nil
		}
		if err = c.Transact("lb-hc-del", ops); err != nil {
			return fmt.Errorf("failed to delete health check %s from load balancers %s: %w", uuid, lbName, err)
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
