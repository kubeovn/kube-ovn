package ovs

import (
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

const localExternalVIPKeyPrefix = "kube-ovn.io/local-external-vip/"

// CreateLoadBalancer create loadbalancer
func (c *OVNNbClient) CreateLoadBalancer(lbName, protocol string, selectFields ...string) error {
	lb := &ovnnb.LoadBalancer{
		UUID:     ovsclient.NamedUUID(),
		Name:     lbName,
		Protocol: &protocol,
		ExternalIDs: map[string]string{
			"vendor": util.CniTypeName,
		},
	}

	if len(selectFields) != 0 {
		lb.SelectionFields = selectFields
	}

	return logErr(c.loadBalancerTable().createIfAbsent(lbName, "lb-add", lb))
}

// UpdateLoadBalancer update load balancer
func (c *OVNNbClient) UpdateLoadBalancer(lb *ovnnb.LoadBalancer, fields ...any) error {
	return c.updateModelLogged("lb-update", lb, func(err error) error {
		return fmt.Errorf("update load balancer %s: %w", lb.Name, err)
	}, fields...)
}

// LoadBalancerAddVips adds or updates a vip
func (c *OVNNbClient) LoadBalancerAddVip(lbName, vip string, backends ...string) error {
	if _, err := c.GetLoadBalancer(lbName, false); err != nil {
		klog.Errorf("failed to get lb health check: %v", err)
		return err
	}

	sort.Strings(backends)
	value := strings.Join(backends, ",")
	if err := c.transactLB(lbName, "lb-add", func(lb *ovnnb.LoadBalancer) []model.Mutation {
		mutations := make([]model.Mutation, 0, 2)
		if len(lb.Vips) != 0 {
			if lb.Vips[vip] == value {
				return nil
			}
			mutations = append(mutations, mapDeleteMutation(&lb.Vips, vip, lb.Vips[vip]))
		}
		mutations = append(mutations, mapInsertMutation(&lb.Vips, vip, value))
		return mutations
	}); err != nil {
		return logWrap(err, wrapErr("failed to add vip %s with backends %v to load balancers %s: %w", vip, backends, lbName))
	}
	return nil
}

// LoadBalancerDeleteVip deletes load balancer vip
func (c *OVNNbClient) LoadBalancerDeleteVip(lbName, vipEndpoint string, ignoreHealthCheck bool) error {
	lb, lbhc, err := c.GetLoadBalancerHealthCheck(lbName, vipEndpoint, true)
	if err != nil {
		klog.Errorf("failed to get lb health check: %v", err)
		return err
	}
	if len(lb.IPPortMappings) != 0 {
		ignoreHealthCheck = false
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

	if err := c.transactLB(lbName, "lb-add", func(lb *ovnnb.LoadBalancer) []model.Mutation {
		mutations := []model.Mutation{
			mapDeleteMutation(&lb.Vips, vipEndpoint, lb.Vips[vipEndpoint]),
		}
		key := localExternalVIPKeyPrefix + vipEndpoint
		if value, ok := lb.ExternalIDs[key]; ok {
			mutations = append(mutations, mapDeleteMutation(&lb.ExternalIDs, key, value))
		}
		return mutations
	}); err != nil {
		return logWrap(err, wrapErr("failed to delete vip %s from load balancers %s: %w", vipEndpoint, lbName))
	}
	return nil
}

// SetLoadBalancerVIPExternalTrafficLocal records the node LSP of the chassis
// announcing an external VIP of a LoadBalancer Service with
// externalTrafficPolicy=Local.
func (c *OVNNbClient) SetLoadBalancerVIPExternalTrafficLocal(lbName, vip, vipNodeLSP string) error {
	key := localExternalVIPKeyPrefix + vip
	if err := c.transactLB(lbName, "lb-update-external-vip", func(lb *ovnnb.LoadBalancer) []model.Mutation {
		return mapReplaceMutations(&lb.ExternalIDs, lb.ExternalIDs, key, vipNodeLSP)
	}); err != nil {
		return fmt.Errorf("failed to update external traffic local marker for vip %s on load balancer %s: %w", vip, lbName, err)
	}
	return nil
}

func (c *OVNNbClient) transactLB(lbName, method string, mutations func(*ovnnb.LoadBalancer) []model.Mutation) error {
	ops, err := c.LoadBalancerOp(lbName, mutations)
	if err != nil {
		return err
	}
	return transactOps(c, method, ops)
}

func (c *OVNNbClient) setLoadBalancerOption(lbName, key, value string) error {
	lb, err := c.GetLoadBalancer(lbName, false)
	if err != nil {
		klog.Errorf("failed to get lb: %v", err)
		return err
	}
	if len(lb.Options) != 0 && lb.Options[key] == value {
		return nil
	}
	options := make(map[string]string, len(lb.Options)+1)
	maps.Copy(options, lb.Options)
	options[key] = value
	lb.Options = options
	return c.UpdateLoadBalancer(lb, &lb.Options)
}

func (c *OVNNbClient) setLoadBalancerOptionLogged(lbName, key, value string, wrap func(error) error) error {
	if err := c.setLoadBalancerOption(lbName, key, value); err != nil {
		klog.Error(err)
		return wrap(err)
	}
	return nil
}

func (c *OVNNbClient) SetLoadBalancerAffinityTimeout(lbName string, timeout int) error {
	return c.setLoadBalancerOptionLogged(lbName, "affinity_timeout", strconv.Itoa(timeout),
		wrapErr("failed to set affinity timeout of lb %s to %d: %w", lbName, timeout))
}

func (c *OVNNbClient) SetLoadBalancerPreferLocalBackend(lbName string, preferLocalBackend bool) error {
	value := strconv.FormatBool(preferLocalBackend)
	return c.setLoadBalancerOptionLogged(lbName, "prefer_local_backend", value,
		wrapErr("failed to set prefer local backend of lb %s to %s: %w", lbName, value))
}

func (c *OVNNbClient) SetLoadBalancerCtFlush(lbName string, ctFlush bool) error {
	value := strconv.FormatBool(ctFlush)
	return c.setLoadBalancerOptionLogged(lbName, "ct_flush", value,
		wrapErr("failed to set ct_flush of lb %s to %s: %w", lbName, value))
}

func (c *OVNNbClient) DeleteLoadBalancers(filter func(lb *ovnnb.LoadBalancer) bool) error {
	return deleteWhereCache(
		c,
		&ovnnb.LoadBalancer{},
		"lb-del",
		filter,
		func(err error) error { return fmt.Errorf("generate operations for delete load balancers: %w", err) },
		func(err error) error { return fmt.Errorf("delete load balancers : %w", err) },
	)
}

// DeleteLoadBalancer delete loadbalancer
func (c *OVNNbClient) DeleteLoadBalancer(lbName string) error {
	ops, err := c.DeleteLoadBalancerOp(lbName)
	if err != nil {
		klog.Errorf("failed to get delete lb op: %v", err)
		return err
	}
	if err := transactOps(c, "lb-del", ops); err != nil {
		klog.Errorf("failed to del lb: %v", err)
		return fmt.Errorf("delete load balancer %s: %w", lbName, err)
	}
	return nil
}

// GetLoadBalancer get load balancer by name,
// it is because of lack name index that doesn't use OVNNbClient.Get
func (c *OVNNbClient) GetLoadBalancer(lbName string, ignoreNotFound bool) (*ovnnb.LoadBalancer, error) {
	return logRet(c.loadBalancerTable().get(lbName, ignoreNotFound))
}

func (c *OVNNbClient) LoadBalancerExists(lbName string) (bool, error) {
	return existsByGet(c.GetLoadBalancer, lbName)
}

// ListLoadBalancers list all load balancers
func (c *OVNNbClient) ListLoadBalancers(filter func(lb *ovnnb.LoadBalancer) bool) ([]ovnnb.LoadBalancer, error) {
	return logRet(c.loadBalancerTable().list(false, filter))
}

func (c *OVNNbClient) LoadBalancerOp(lbName string, mutationsFunc ...func(lb *ovnnb.LoadBalancer) []model.Mutation) ([]ovsdb.Operation, error) {
	return c.loadBalancerTable().mutateNamedAll(lbName, mutationsFunc...)
}

// DeleteLoadBalancerOp create operation which delete load balancer
func (c *OVNNbClient) DeleteLoadBalancerOp(lbName string) ([]ovsdb.Operation, error) {
	table := c.loadBalancerTable()
	lb, err := table.get(lbName, true)
	if err != nil {
		return nil, logErr(err)
	}
	if lb == nil {
		return nil, nil
	}

	ops, err := table.delete(lb)
	if err != nil {
		return nil, logErr(err)
	}
	return ops, nil
}

// LoadBalancerAddIPPortMapping add load balancer ip port mapping
func (c *OVNNbClient) LoadBalancerAddIPPortMapping(lbName, vipEndpoint string, mappings map[string]string) error {
	if len(mappings) == 0 {
		return nil
	}
	if err := c.transactLB(lbName, "lb-add", func(lb *ovnnb.LoadBalancer) []model.Mutation {
		return []model.Mutation{
			{
				Field:   &lb.IPPortMappings,
				Value:   mappings,
				Mutator: ovsdb.MutateOperationInsert,
			},
		}
	}); err != nil {
		return logWrap(err, wrapErr("failed to add ip port mapping with vip %v to load balancers %s: %w", vipEndpoint, lbName))
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

	if err := c.transactLB(lbName, "lb-del", func(lb *ovnnb.LoadBalancer) []model.Mutation {
		return []model.Mutation{
			{
				Field:   &lb.IPPortMappings,
				Value:   unusedBackendIPs,
				Mutator: ovsdb.MutateOperationDelete,
			},
		}
	}); err != nil {
		return logWrap(err, wrapErr("failed to delete IP port mappings for VIP %s from load balancer %s: %w", vipEndpoint, lbName))
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

// LoadBalancerUpdateIPPortMapping update the ip_port_mapping of a loadbalancer.
// The ipPortMapping received can contain a partial update of the port mapping.
// You must only pass the port mapping of vipEndpoint, not of every VIP on the LB.
// Existing port mappings will be overwritten if the LSP changed for a particular IP.
// The orphaned port mappings (for IPs that are not contained in any backend for any VIP) are deleted on update.
func (c *OVNNbClient) LoadBalancerUpdateIPPortMapping(lbName, vipEndpoint string, ipPortMappings map[string]string) error {
	if err := c.transactLB(lbName, "lb-update", func(lb *ovnnb.LoadBalancer) []model.Mutation {
		toDelete := make(map[string]string)
		toInsert := make(map[string]string)

		// Build set of new backend IPs
		newBackendIPs := make(map[string]bool, len(ipPortMappings))
		for ip := range ipPortMappings {
			cleanIP := strings.Trim(ip, "[]")
			newBackendIPs[cleanIP] = true
		}

		// Find orphaned mappings: IPs in current mappings that are not in new mappings
		// AND not used by any other VIP in the load balancer
		for mappingKey, mappingValue := range lb.IPPortMappings {
			cleanKey := strings.Trim(mappingKey, "[]")

			// If this IP is not in the new mappings, check if it should be deleted
			if !newBackendIPs[cleanKey] {
				// Use the existing isBackendIPStillUsed helper to check all VIPs
				if !c.isBackendIPStillUsed(lb, vipEndpoint, cleanKey) {
					toDelete[mappingKey] = mappingValue
				}
			}
		}

		// For each IP in the new mappings, check if it already exists with a different value
		for ip, newLSP := range ipPortMappings {
			if len(lb.IPPortMappings) != 0 {
				// Normalize the IP key for comparison (strip brackets for IPv6)
				cleanIP := strings.Trim(ip, "[]")

				// Check if an existing mapping exists for this IP (in any key format)
				var existingKey string
				var existingLSP string
				var found bool

				// First try exact match
				if lsp, exists := lb.IPPortMappings[ip]; exists {
					existingKey = ip
					existingLSP = lsp
					found = true
				} else {
					// Try to find the IP with normalized key (check both bracketed/unbracketed forms)
					for mappingKey, mappingValue := range lb.IPPortMappings {
						if strings.Trim(mappingKey, "[]") == cleanIP {
							existingKey = mappingKey
							existingLSP = mappingValue
							found = true
							break
						}
					}
				}

				if found {
					if existingLSP == newLSP {
						// Mapping is already correct, skip
						continue
					}
					// Different value, need to delete old and insert new
					toDelete[existingKey] = existingLSP
					toInsert[ip] = newLSP
				} else {
					// New mapping
					toInsert[ip] = newLSP
				}
			} else {
				// No existing mappings, just insert
				toInsert[ip] = newLSP
			}
		}

		mutations := make([]model.Mutation, 0, 2)

		// Create delete mutation if needed
		if len(toDelete) > 0 {
			mutations = append(
				mutations,
				model.Mutation{
					Field:   &lb.IPPortMappings,
					Value:   toDelete,
					Mutator: ovsdb.MutateOperationDelete,
				},
			)
		}

		// Create insert mutation if needed
		if len(toInsert) > 0 {
			mutations = append(
				mutations,
				model.Mutation{
					Field:   &lb.IPPortMappings,
					Value:   toInsert,
					Mutator: ovsdb.MutateOperationInsert,
				},
			)
		}

		return mutations
	}); err != nil {
		return fmt.Errorf("failed to update ip port mapping with vip %v to load balancers %s: %w", vipEndpoint, lbName, err)
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
	lb, err := c.GetLoadBalancer(lbName, false)
	if err != nil {
		klog.Errorf("failed to get lb: %v", err)
		return err
	}

	if slices.Contains(lb.HealthCheck, uuid) {
		if err := c.transactLB(lbName, "lb-hc-del", func(lb *ovnnb.LoadBalancer) []model.Mutation {
			return []model.Mutation{
				{
					Field:   &lb.HealthCheck,
					Value:   []string{uuid},
					Mutator: ovsdb.MutateOperationDelete,
				},
			}
		}); err != nil {
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

func (c *OVNNbClient) loadBalancerTable() namedTable[ovnnb.LoadBalancer] {
	table := newNamedTable(c.Database, &ovnnb.LoadBalancer{}, "load balancer",
		func(lb *ovnnb.LoadBalancer) string { return lb.Name },
		func(lb *ovnnb.LoadBalancer) map[string]string { return lb.ExternalIDs })
	table.listErrorVerb = "failed to list"
	return table
}
