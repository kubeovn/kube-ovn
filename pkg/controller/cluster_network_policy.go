package controller

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"unicode"

	"k8s.io/client-go/tools/cache"

	"github.com/scylladb/go-set/strset"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"

	"sigs.k8s.io/network-policy-api/apis/v1alpha1"
	"sigs.k8s.io/network-policy-api/apis/v1alpha2"
)

// ClusterNetworkPolicyChangedDelta is used to determine what changed within a ClusterNetworkPolicy
type ClusterNetworkPolicyChangedDelta struct {
	key              string
	ruleNames        [util.CnpMaxRules]ChangedName
	field            ChangedField
	DNSReconcileDone bool
}

// enqueueAddCnp adds a new ClusterNetworkPolicy to the processing queue for creation
func (c *Controller) enqueueAddCnp(obj any) {
	key := cache.MetaObjectToName(obj.(*v1alpha2.ClusterNetworkPolicy)).String()
	klog.V(3).Infof("enqueue add cnp %s", key)
	c.addCnpQueue.Add(key)
}

// enqueueUpdateCnp adds an existing ClusterNetworkPolicy to the processing queue for updates
func (c *Controller) enqueueUpdateCnp(oldObj, newObj any) {
	oldCnp := oldObj.(*v1alpha2.ClusterNetworkPolicy)
	newCnp := newObj.(*v1alpha2.ClusterNetworkPolicy)

	// If the CNP was modified in a way that needs the ACLs to be re-created, we enqueue the CNP to be re-created
	// from scratch and skip the update logic entirely.
	if shouldRecreateCnpACLs(oldCnp, newCnp) {
		c.addCnpQueue.Add(newCnp.Name)
		return
	}

	klog.V(3).Infof("enqueue update cnp %s", newCnp.Name)

	// Check if the port group of the ACL needs to be re-created.
	if shouldUpdateCnpPortGroup(oldCnp, newCnp) {
		c.updateCnpQueue.Add(&ClusterNetworkPolicyChangedDelta{key: newCnp.Name, field: ChangedSubject})
	}

	// If the rule name or peer selector in ingress/egress rules has changed, the corresponding address-set need be updated
	changedIngressRuleNames, changedEgressRuleNames := getCnpAddressSetsToUpdate(oldCnp, newCnp)

	// Update the address-set of the ingress rules
	if !isCnpRulesArrayEmpty(changedIngressRuleNames) {
		c.updateCnpQueue.Add(&ClusterNetworkPolicyChangedDelta{
			key:       newCnp.Name,
			ruleNames: changedIngressRuleNames,
			field:     ChangedIngressRule,
		})
	}

	// Update the address-set of the egress rules
	if !isCnpRulesArrayEmpty(changedEgressRuleNames) {
		c.updateCnpQueue.Add(&ClusterNetworkPolicyChangedDelta{
			key:       newCnp.Name,
			ruleNames: changedEgressRuleNames,
			field:     ChangedEgressRule,
		})
	}
}

// enqueueDeleteCnp adds an existing ClusterNetworkPolicy to the processing queue for deletion
func (c *Controller) enqueueDeleteCnp(obj any) {
	var cnp *v1alpha2.ClusterNetworkPolicy
	switch t := obj.(type) {
	case *v1alpha2.ClusterNetworkPolicy:
		cnp = t
	case cache.DeletedFinalStateUnknown:
		a, ok := t.Obj.(*v1alpha2.ClusterNetworkPolicy)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		cnp = a
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	klog.V(3).Infof("enqueue delete cnp %s", cache.MetaObjectToName(cnp).String())
	c.deleteCnpQueue.Add(cnp.DeepCopy())
}

func (c *Controller) handleAddCnp(key string) (err error) {
	c.cnpKeyMutex.LockKey(key)
	defer func() { _ = c.cnpKeyMutex.UnlockKey(key) }()

	cachedCnp, err := c.cnpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	klog.Infof("handle add cnp %s", cachedCnp.Name)
	cnp := cachedCnp.DeepCopy()

	// Validate the CNP is valid and can be configured
	if err := c.validateCnpConfig(cnp); err != nil {
		err := fmt.Errorf("failed to validate cnp %s: %w", cnp.Name, err)
		klog.Error(err)
		return err
	}

	// Update priority maps in case the priority/tier of the CNP has changed
	if err := c.updateCnpPriorityMapEntries(cnp); err != nil {
		err := fmt.Errorf("failed to update priority maps for cnp %s: %w", cnp.Name, err)
		klog.Error(err)
		return err
	}

	var logActions []string
	if cnp.Annotations[util.ACLActionsLogAnnotation] != "" {
		logActions = strings.Split(cnp.Annotations[util.ACLActionsLogAnnotation], ",")
	}

	// Setup port group for the CNP
	if err := c.setupCnpPortGroup(cnp); err != nil {
		klog.Errorf("failed to create port group for cnp %s: %v", cnp.Name, err)
		return err
	}

	cnpName := getCnpName(cnp.Name)
	pgName := getCnpPortGroupName(cnp)
	cnpACLTier := getCnpACLTier(cnp.Spec.Tier)

	curIngressAddrSet, curEgressAddrSet, err := c.getCnpCurrentAddrSetByName(cnpName)
	if err != nil {
		klog.Errorf("failed to list address sets for cnp %s: %v", cnp.Name, err)
		return err
	}

	// Multiplied by 2 to handle both IPv4 and IPv6 address sets
	desiredIngressAddrSet := strset.NewWithSize(len(cnp.Spec.Ingress) * 2)
	desiredEgressAddrSet := strset.NewWithSize(len(cnp.Spec.Egress) * 2)

	ingressACLOps, err := c.OVNNbClient.DeleteAclsOps(pgName, portGroupKey, "to-lport", nil)
	if err != nil {
		klog.Errorf("failed to generate clear operations for cnp %s ingress acls: %v", cnp.Name, err)
		return err
	}

	// Create ingress ACLs and address sets
	for index, rule := range cnp.Spec.Ingress {
		v4AddressSetName, as4len, v6AddressSetName, as6len, err := c.generateCnpIngressAddressSet(cnpName, pgName, rule, index)
		if err != nil {
			err := fmt.Errorf("failed to generate ingress address set for cnp %s: %w", cnp.Name, err)
			klog.Error(err)
			return err
		}

		desiredIngressAddrSet.Add(v4AddressSetName, v6AddressSetName)

		aclPriority := getCnpACLPriority(cnp, index)
		rulePorts := []v1alpha2.ClusterNetworkPolicyPort{}
		if rule.Ports != nil {
			rulePorts = *rule.Ports
		}

		if as4len != 0 {
			aclName := getCnpACLName(cnpName, kubeovnv1.ProtocolIPv4, "ingress", index)
			ops, err := c.OVNNbClient.UpdateCnpRuleACLOps(pgName, v4AddressSetName, kubeovnv1.ProtocolIPv4, aclName, aclPriority, getCnpACLAction(rule.Action), logActions, rulePorts, true, cnpACLTier)
			if err != nil {
				klog.Errorf("failed to add v4 ingress acls for cnp %s: %v", key, err)
				return err
			}
			ingressACLOps = append(ingressACLOps, ops...)
		}

		if as6len != 0 {
			aclName := getCnpACLName(cnpName, kubeovnv1.ProtocolIPv6, "ingress", index)
			ops, err := c.OVNNbClient.UpdateCnpRuleACLOps(pgName, v6AddressSetName, kubeovnv1.ProtocolIPv6, aclName, aclPriority, getCnpACLAction(rule.Action), logActions, rulePorts, true, cnpACLTier)
			if err != nil {
				klog.Errorf("failed to add v6 ingress acls for cnp %s: %v", cnp.Name, err)
				return err
			}
			ingressACLOps = append(ingressACLOps, ops...)
		}
	}

	if err := c.OVNNbClient.Transact("add-ingress-acls", ingressACLOps); err != nil {
		return fmt.Errorf("failed to add ingress acls for cnp %s: %w", cnp.Name, err)
	}
	if err := c.deleteUnusedAddrSetForAnp(curIngressAddrSet, desiredIngressAddrSet); err != nil {
		return fmt.Errorf("failed to delete unused ingress address set for cnp %s: %w", cnp.Name, err)
	}

	egressACLOps, err := c.OVNNbClient.DeleteAclsOps(pgName, portGroupKey, "from-lport", nil)
	if err != nil {
		klog.Errorf("failed to generate clear operations for cnp %s egress acls: %v", cnp.Name, err)
		return err
	}

	// Reconcile DNSNameResolvers for all collected domain names
	if c.config.EnableDNSNameResolver {
		if err := c.reconcileDNSNameResolversForANP(cnpName, getCnpDomainsNames(cnp)); err != nil {
			klog.Errorf("failed to reconcile DNSNameResolvers for CNP %s: %v", cnp.Name, err)
			return err
		}
	}

	// create egress acl
	for index, rule := range cnp.Spec.Egress {
		v4AddressSetName, as4len, v6AddressSetName, as6len, err := c.generateCnpEgressAddressSet(cnpName, pgName, rule, index)
		if err != nil {
			err := fmt.Errorf("failed to generate egress address set for cnp %s: %w", cnp.Name, err)
			klog.Error(err)
			return err
		}

		desiredEgressAddrSet.Add(v4AddressSetName, v6AddressSetName)

		hasDomainNames := hasCnpDomainNames(cnp)

		aclPriority := getCnpACLPriority(cnp, index)
		rulePorts := []v1alpha2.ClusterNetworkPolicyPort{}
		if rule.Ports != nil {
			rulePorts = *rule.Ports
		}

		// Create ACL rules if we have IP addresses OR domain names.
		// Domain names may not be resolved initially but will be updated later
		if as4len != 0 || hasDomainNames {
			aclName := getCnpACLName(cnpName, kubeovnv1.ProtocolIPv4, "egress", index)
			ops, err := c.OVNNbClient.UpdateCnpRuleACLOps(pgName, v4AddressSetName, kubeovnv1.ProtocolIPv4, aclName, aclPriority, getCnpACLAction(rule.Action), logActions, rulePorts, false, cnpACLTier)
			if err != nil {
				klog.Errorf("failed to add v4 egress acls for cnp %s: %v", key, err)
				return err
			}
			egressACLOps = append(egressACLOps, ops...)
		}

		if as6len != 0 || hasDomainNames {
			aclName := getCnpACLName(cnpName, kubeovnv1.ProtocolIPv6, "egress", index)
			ops, err := c.OVNNbClient.UpdateCnpRuleACLOps(pgName, v6AddressSetName, kubeovnv1.ProtocolIPv6, aclName, aclPriority, getCnpACLAction(rule.Action), logActions, rulePorts, false, cnpACLTier)
			if err != nil {
				klog.Errorf("failed to add v6 egress acls for cnp %s: %v", key, err)
				return err
			}
			egressACLOps = append(egressACLOps, ops...)
		}
	}

	if err := c.OVNNbClient.Transact("add-egress-acls", egressACLOps); err != nil {
		return fmt.Errorf("failed to add egress acls for cnp %s: %w", key, err)
	}
	if err := c.deleteUnusedAddrSetForAnp(curEgressAddrSet, desiredEgressAddrSet); err != nil {
		return fmt.Errorf("failed to delete unused egress address set for cnp %s: %w", key, err)
	}

	return nil
}

func (c *Controller) handleUpdateCnp(changed *ClusterNetworkPolicyChangedDelta) error {
	// Only handle updates that do not affect ACLs.
	c.cnpKeyMutex.LockKey(changed.key)
	defer func() { _ = c.cnpKeyMutex.UnlockKey(changed.key) }()

	klog.Infof("handleUpdateCnp: processing CNP %s, field=%s, DNSReconcileDone=%v",
		changed.key, changed.field, changed.DNSReconcileDone)

	cachedCnp, err := c.cnpsLister.Get(changed.key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	desiredCnp := cachedCnp.DeepCopy()
	klog.Infof("handle update cluster network policy %s", desiredCnp.Name)

	// Verify the CNP is correctly written
	if err := c.validateCnpConfig(desiredCnp); err != nil {
		klog.Errorf("failed to validate cnp %s: %v", desiredCnp.Name, err)
		return err
	}

	cnpName := getCnpName(desiredCnp.Name)
	pgName := getCnpPortGroupName(desiredCnp)

	// The port group of the CNP must be updated
	if changed.field == ChangedSubject {
		if err := c.setupCnpPortGroup(desiredCnp); err != nil {
			klog.Errorf("failed to create port group for cnp %s: %v", desiredCnp.Name, err)
			return err
		}
	}

	// Peer selector in ingress/egress rule has changed, so the corresponding address-set need be updated
	if changed.field == ChangedIngressRule {
		for index, rule := range desiredCnp.Spec.Ingress {
			// Make sure the rule is changed and go on update
			if rule.Name == changed.ruleNames[index].curRuleName {
				if err := c.setAddrSetForCnpRule(cnpName, pgName, rule.Name, index, rule.From, []v1alpha2.ClusterNetworkPolicyEgressPeer{}, true, false); err != nil {
					klog.Errorf("failed to set ingress address-set for cnp rule %s/%s, %v", cnpName, rule.Name, err)
					return err
				}

				if changed.ruleNames[index].oldRuleName != "" {
					oldRuleName := changed.ruleNames[index].oldRuleName
					oldAsV4Name, oldAsV6Name := getAnpAddressSetName(pgName, oldRuleName, index, true)

					if err := c.OVNNbClient.DeleteAddressSet(oldAsV4Name); err != nil {
						klog.Errorf("failed to delete address set %s, %v", oldAsV4Name, err)
						// just record error log
					}
					if err := c.OVNNbClient.DeleteAddressSet(oldAsV6Name); err != nil {
						klog.Errorf("failed to delete address set %s, %v", oldAsV6Name, err)
					}
				}
			}
		}
	}

	if changed.field == ChangedEgressRule {
		for index, rule := range desiredCnp.Spec.Egress {
			// Check if we need to update address sets (rule changed or DNS reconciliation needed)
			needAddrSetUpdate := rule.Name == changed.ruleNames[index].curRuleName || changed.DNSReconcileDone

			// Check if we need to reconcile DNS resolvers (DNS feature enabled and not already done)
			needDNSReconcile := c.config.EnableDNSNameResolver && !changed.DNSReconcileDone

			if needAddrSetUpdate {
				if err := c.setAddrSetForCnpRule(cnpName, pgName, rule.Name, index, []v1alpha2.ClusterNetworkPolicyIngressPeer{}, rule.To, false, false); err != nil {
					klog.Errorf("failed to set egress address-set for cnp rule %s/%s, %v", cnpName, rule.Name, err)
					return err
				}

				if needDNSReconcile {
					if err := c.reconcileDNSNameResolversForANP(cnpName, getCnpDomainsNames(desiredCnp)); err != nil {
						klog.Errorf("failed to reconcile DNSNameResolvers for egress rule %s/%s, %v", cnpName, rule.Name, err)
						return err
					}
				}

				if changed.ruleNames[index].oldRuleName != "" {
					oldRuleName := changed.ruleNames[index].oldRuleName
					oldAsV4Name, oldAsV6Name := getAnpAddressSetName(pgName, oldRuleName, index, false)

					if err := c.OVNNbClient.DeleteAddressSet(oldAsV4Name); err != nil {
						klog.Errorf("failed to delete address set %s, %v", oldAsV4Name, err)
						// just record error log
					}
					if err := c.OVNNbClient.DeleteAddressSet(oldAsV6Name); err != nil {
						klog.Errorf("failed to delete address set %s, %v", oldAsV6Name, err)
					}
				}
			}
		}
	}

	return nil
}

// handleDeleteCnp handles deletion of a ClusterNetworkPolicy
func (c *Controller) handleDeleteCnp(cnp *v1alpha2.ClusterNetworkPolicy) error {
	c.cnpKeyMutex.LockKey(cnp.Name)
	defer func() { _ = c.cnpKeyMutex.UnlockKey(cnp.Name) }()

	klog.Infof("handle delete cluster network policy %s", cnp.Name)

	// Delete the CNP from the priority mapping
	if err := c.deleteCnpPriorityMapEntries(cnp); err != nil {
		// Do not exit on errors, try to go as far as possible in the deletion
		klog.Errorf("failed to delete priorityMapEntries: %v", err)
	}

	cnpName := getCnpName(cnp.Name)

	// ACLs related to port_group will be deleted automatically when port_group is deleted
	pgName := getCnpPortGroupName(cnp)
	if err := c.OVNNbClient.DeletePortGroup(pgName); err != nil {
		// Do not exit on errors, try to go as far as possible in the deletion
		klog.Errorf("failed to delete port group for cnp %s: %v", cnp.Name, err)
	}

	// Delete all ingress address sets for this CNP
	if err := c.OVNNbClient.DeleteAddressSets(map[string]string{
		clusterNetworkPolicyKey: fmt.Sprintf("%s/%s", cnpName, "ingress"),
	}); err != nil {
		// Do not exit on errors, try to go as far as possible in the deletion
		klog.Errorf("failed to delete ingress address set for cnp %s: %v", cnp.Name, err)
	}

	// Delete all egress address sets for this CNP
	if err := c.OVNNbClient.DeleteAddressSets(map[string]string{
		clusterNetworkPolicyKey: fmt.Sprintf("%s/%s", cnpName, "egress"),
	}); err != nil {
		// Do not exit on errors, try to go as far as possible in the deletion
		klog.Errorf("failed to delete egress address set for cnp %s: %v", cnp.Name, err)
	}

	// Delete all DNSNameResolver CRs associated with this CNP
	if c.config.EnableDNSNameResolver {
		if err := c.reconcileDNSNameResolversForANP(cnpName, []string{}); err != nil {
			// Do not exit on errors, try to go as far as possible in the deletion
			klog.Errorf("failed to delete DNSNameResolver CRs for cnp %s: %v", cnpName, err)
		}
	}

	return nil
}

// getCnpCurrentAddrSetByName returns the address sets present in OVN databases for a given ClusterNetworkPolicy
func (c *Controller) getCnpCurrentAddrSetByName(cnpName string) (*strset.Set, *strset.Set, error) {
	curIngressAddrSet := strset.New()
	curEgressAddrSet := strset.New()

	operations := []string{"ingress", "egress"}
	for _, operation := range operations {
		addressSets, err := c.OVNNbClient.ListAddressSets(map[string]string{
			clusterNetworkPolicyKey: fmt.Sprintf("%s/%s", cnpName, operation),
		})
		if err != nil {
			klog.Errorf("failed to list %s address sets for cnp %s: %v", operation, cnpName, err)
			return nil, nil, err
		}

		for _, addressSet := range addressSets {
			if operation == "ingress" {
				curIngressAddrSet.Add(addressSet.Name)
				continue
			}

			curEgressAddrSet.Add(addressSet.Name)
		}
	}

	return curIngressAddrSet, curEgressAddrSet, nil
}

// setupCnpPortGroup setups the port group of a ClusterNetworkPolicy
func (c *Controller) setupCnpPortGroup(cnp *v1alpha2.ClusterNetworkPolicy) error {
	pgName := getCnpPortGroupName(cnp)

	// Create port group in OVN databases
	if err := c.OVNNbClient.CreatePortGroup(pgName, map[string]string{clusterNetworkPolicyKey: pgName}); err != nil {
		klog.Errorf("failed to create port group for cnp %s: %v", cnp.Name, err)
		return err
	}

	// Retrieve all the logical ports targeted by this CNP
	ports, err := c.getCnpPorts(&cnp.Spec.Subject)
	if err != nil {
		klog.Errorf("failed to fetch ports belongs to cnp %s: %v", cnp.Name, err)
		return err
	}

	// Assign the logical ports to the port group
	if err = c.OVNNbClient.PortGroupSetPorts(pgName, ports); err != nil {
		klog.Errorf("failed to set ports %v to port group %s: %v", ports, pgName, err)
		return err
	}

	return nil
}

// getCnpPorts returns the ports targeted by a ClusterNetworkPolicy
func (c *Controller) getCnpPorts(cnpSubject *v1alpha2.ClusterNetworkPolicySubject) ([]string, error) {
	var ports []string

	// Exactly one field must be set, either "namespaces", or "pods"
	if cnpSubject.Namespaces != nil {
		nsSelector, err := metav1.LabelSelectorAsSelector(cnpSubject.Namespaces)
		if err != nil {
			return nil, fmt.Errorf("error creating ns label selector, %w", err)
		}
		ports, _, _, err = c.fetchPods(nsSelector, labels.Everything())
		if err != nil {
			return nil, fmt.Errorf("failed to fetch pods, %w", err)
		}
	} else if cnpSubject.Pods != nil {
		nsSelector, err := metav1.LabelSelectorAsSelector(&cnpSubject.Pods.NamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("error creating ns label selector, %w", err)
		}
		podSelector, err := metav1.LabelSelectorAsSelector(&cnpSubject.Pods.PodSelector)
		if err != nil {
			return nil, fmt.Errorf("error creating pod label selector, %w", err)
		}
		ports, _, _, err = c.fetchPods(nsSelector, podSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch pods, %w", err)
		}
	}

	return ports, nil
}

// generateCnpIngressAddressSet generates the ingress address set for a rule of a ClusterNetworkPolicy
// The function returns the name of the address sets for both IPv6 and IPv4. The number of addresses
// contained in each address set is also returned.
func (c *Controller) generateCnpIngressAddressSet(cnpName, pgName string, rule v1alpha2.ClusterNetworkPolicyIngressRule, index int) (string, int, string, int, error) {
	ingressAsV4Name, ingressAsV6Name := getAnpAddressSetName(pgName, rule.Name, index, true)

	// Concatenate all the targeted addresses for the CNP
	var v4Addrs, v6Addrs []string
	var err error

	// For every peer in the rules, generate the targeted addresses
	for _, peer := range rule.From {
		var v4Addresses, v6Addresses []string
		if v4Addresses, v6Addresses, err = c.fetchIngressSelectedAddressesByCnp(&peer); err != nil {
			return "", 0, "", 0, err
		}
		v4Addrs = append(v4Addrs, v4Addresses...)
		v6Addrs = append(v6Addrs, v6Addresses...)
	}

	// Add IPv4 addresses to the address set
	if err = c.createCnpAddressSet(cnpName, rule.Name, "ingress", ingressAsV4Name, v4Addrs); err != nil {
		klog.Error(err)
		return "", 0, "", 0, err
	}

	// Add IPv6 addresses to the address set
	if err = c.createCnpAddressSet(cnpName, rule.Name, "ingress", ingressAsV6Name, v6Addrs); err != nil {
		klog.Error(err)
		return "", 0, "", 0, err
	}

	return ingressAsV4Name, len(v4Addrs), ingressAsV6Name, len(v6Addrs), nil
}

// generateCnpEgressAddressSet generates the egress address set for a rule of a ClusterNetworkPolicy
// The function returns the name of the address sets for both IPv6 and IPv4. The number of addresses
// contained in each address set is also returned.
func (c *Controller) generateCnpEgressAddressSet(cnpName, pgName string, rule v1alpha2.ClusterNetworkPolicyEgressRule, index int) (string, int, string, int, error) {
	egressAsV4Name, egressAsV6Name := getAnpAddressSetName(pgName, rule.Name, index, false)

	// Concatenate all the targeted addresses for the CNP
	var v4Addrs, v6Addrs []string
	var err error

	// For every peer in the rules, generate the targeted addresses
	for _, peer := range rule.To {
		var v4Addresses, v6Addresses []string
		if v4Addresses, v6Addresses, err = c.fetchEgressSelectedAddressesByCnp(&peer); err != nil {
			return "", 0, "", 0, err
		}
		v4Addrs = append(v4Addrs, v4Addresses...)
		v6Addrs = append(v6Addrs, v6Addresses...)
	}

	// Add IPv4 addresses to the address set
	if err = c.createCnpAddressSet(cnpName, rule.Name, "egress", egressAsV4Name, v4Addrs); err != nil {
		klog.Error(err)
		return "", 0, "", 0, err
	}

	// Add IPv6 addresses to the address set
	if err = c.createCnpAddressSet(cnpName, rule.Name, "egress", egressAsV6Name, v6Addrs); err != nil {
		klog.Error(err)
		return "", 0, "", 0, err
	}

	return egressAsV4Name, len(v4Addrs), egressAsV6Name, len(v6Addrs), nil
}

// createCnpAddressSet creates an address set in the OVN DBs for a particular rule
func (c *Controller) createCnpAddressSet(cnpName, ruleName, direction, asName string, addresses []string) error {
	if err := c.OVNNbClient.CreateAddressSet(asName, map[string]string{
		clusterNetworkPolicyKey: fmt.Sprintf("%s/%s", cnpName, direction),
	}); err != nil {
		klog.Errorf("failed to create ovn address set %s for cnp rule %s/%s: %v", asName, cnpName, ruleName, err)
		return err
	}

	if err := c.OVNNbClient.AddressSetUpdateAddress(asName, addresses...); err != nil {
		klog.Errorf("failed to set addresses %q to address set %s: %v", strings.Join(addresses, ","), asName, err)
		return err
	}

	return nil
}

func (c *Controller) fetchIngressSelectedAddressesByCnp(ingressPeer *v1alpha2.ClusterNetworkPolicyIngressPeer) ([]string, []string, error) {
	var v4Addresses, v6Addresses []string

	// Exactly one of the selector pointers must be set for a given peer
	if ingressPeer.Namespaces != nil {
		nsSelector, err := metav1.LabelSelectorAsSelector(ingressPeer.Namespaces)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating ns label selector, %w", err)
		}
		_, v4Addresses, v6Addresses, err = c.fetchPods(nsSelector, labels.Everything())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch ingress peer addresses, %w", err)
		}
	} else if ingressPeer.Pods != nil {
		nsSelector, err := metav1.LabelSelectorAsSelector(&ingressPeer.Pods.NamespaceSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating ns label selector, %w", err)
		}
		podSelector, err := metav1.LabelSelectorAsSelector(&ingressPeer.Pods.PodSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating pod label selector, %w", err)
		}
		_, v4Addresses, v6Addresses, err = c.fetchPods(nsSelector, podSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch ingress peer addresses, %w", err)
		}
	}

	return v4Addresses, v6Addresses, nil
}

func (c *Controller) fetchEgressSelectedAddressesByCnp(egressPeer *v1alpha2.ClusterNetworkPolicyEgressPeer) ([]string, []string, error) {
	return c.fetchEgressSelectedAddressesCommonByCnp(egressPeer.Namespaces, egressPeer.Pods, egressPeer.Nodes, egressPeer.Networks, egressPeer.DomainNames)
}

func (c *Controller) fetchEgressSelectedAddressesCommonByCnp(namespaces *metav1.LabelSelector, pods *v1alpha2.NamespacedPod, nodes *metav1.LabelSelector, networks []v1alpha2.CIDR, domainNames []v1alpha2.DomainName) ([]string, []string, error) {
	var v4Addresses, v6Addresses []string

	// Exactly one of the selector pointers must be set for a given peer.
	switch {
	case namespaces != nil:
		nsSelector, err := metav1.LabelSelectorAsSelector(namespaces)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating ns label selector, %w", err)
		}

		_, v4Addresses, v6Addresses, err = c.fetchPods(nsSelector, labels.Everything())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch egress peer addresses, %w", err)
		}
	case pods != nil:
		nsSelector, err := metav1.LabelSelectorAsSelector(&pods.NamespaceSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating ns label selector, %w", err)
		}
		podSelector, err := metav1.LabelSelectorAsSelector(&pods.PodSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating pod label selector, %w", err)
		}

		_, v4Addresses, v6Addresses, err = c.fetchPods(nsSelector, podSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch egress peer addresses, %w", err)
		}
	case nodes != nil:
		nodesSelector, err := metav1.LabelSelectorAsSelector(nodes)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating nodes label selector, %w", err)
		}
		v4Addresses, v6Addresses, err = c.fetchNodesAddrs(nodesSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch egress peer addresses, %w", err)
		}
	case len(networks) != 0:
		v4Addresses, v6Addresses = fetchCnpCIDRAddresses(networks)
	case len(domainNames) != 0:
		// DomainNames field is present - resolve addresses from DNSNameResolver
		if !c.config.EnableDNSNameResolver {
			return nil, nil, fmt.Errorf("DNSNameResolver is disabled but domain names are specified: %v", domainNames)
		}
		klog.Infof("DomainNames detected in egress peer: %v", domainNames)
		var err error
		v4Addresses, v6Addresses, err = c.resolveDomainNamesForCnp(domainNames)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to resolve domain names: %w", err)
		}
	default:
		return nil, nil, errors.New("at least one egressPeer must be specified")
	}

	return v4Addresses, v6Addresses, nil
}

func (c *Controller) setAddrSetForCnpRule(anpName, pgName, ruleName string, index int, from []v1alpha2.ClusterNetworkPolicyIngressPeer, to []v1alpha2.ClusterNetworkPolicyEgressPeer, isIngress, isBanp bool) error {
	return c.setAddrSetForCnpRuleCommon(anpName, pgName, ruleName, index, from, to, nil, isIngress, isBanp)
}

func (c *Controller) setAddrSetForCnpRuleCommon(anpName, pgName, ruleName string, index int, from []v1alpha2.ClusterNetworkPolicyIngressPeer, to []v1alpha2.ClusterNetworkPolicyEgressPeer, baselineTo []v1alpha1.BaselineAdminNetworkPolicyEgressPeer, isIngress, isBanp bool) error {
	// A single address set must contain addresses of the same type and the name must be unique within table, so IPv4 and IPv6 address set should be different

	var v4Addrs, v4Addr, v6Addrs, v6Addr []string
	var err error
	if isIngress {
		for _, anprpeer := range from {
			if v4Addr, v6Addr, err = c.fetchIngressSelectedAddressesByCnp(&anprpeer); err != nil {
				klog.Errorf("failed to fetch anp/banp ingress selected addresses, %v", err)
				return err
			}
			v4Addrs = append(v4Addrs, v4Addr...)
			v6Addrs = append(v6Addrs, v6Addr...)
		}
		klog.Infof("update anp/banp ingress rule %s, selected v4 address %v, v6 address %v", ruleName, v4Addrs, v6Addrs)

		gressAsV4Name, gressAsV6Name := getAnpAddressSetName(pgName, ruleName, index, true)
		if err = c.createAsForAnpRule(anpName, ruleName, "ingress", gressAsV4Name, v4Addrs, isBanp); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.createAsForAnpRule(anpName, ruleName, "ingress", gressAsV6Name, v6Addrs, isBanp); err != nil {
			klog.Error(err)
			return err
		}
	} else {
		if to != nil {
			for _, anprpeer := range to {
				if v4Addr, v6Addr, err = c.fetchEgressSelectedAddressesByCnp(&anprpeer); err != nil {
					klog.Errorf("failed to fetch anp/banp egress selected addresses, %v", err)
					return err
				}
				v4Addrs = append(v4Addrs, v4Addr...)
				v6Addrs = append(v6Addrs, v6Addr...)
			}
		} else {
			for _, anprpeer := range baselineTo {
				if v4Addr, v6Addr, err = c.fetchBaselineEgressSelectedAddresses(&anprpeer); err != nil {
					klog.Errorf("failed to fetch baseline anp/banp egress selected addresses, %v", err)
					return err
				}
				v4Addrs = append(v4Addrs, v4Addr...)
				v6Addrs = append(v6Addrs, v6Addr...)
			}
		}
		klog.Infof("update anp/banp egress rule %s, selected v4 address %v, v6 address %v", ruleName, v4Addrs, v6Addrs)

		gressAsV4Name, gressAsV6Name := getAnpAddressSetName(pgName, ruleName, index, false)
		if err = c.createAsForAnpRule(anpName, ruleName, "egress", gressAsV4Name, v4Addrs, isBanp); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.createAsForAnpRule(anpName, ruleName, "egress", gressAsV6Name, v6Addrs, isBanp); err != nil {
			klog.Error(err)
			return err
		}
	}

	return nil
}

// resolveDomainNames resolves domain names to IP addresses using DNSNameResolver lister
func (c *Controller) resolveDomainNamesForCnp(domainNames []v1alpha2.DomainName) ([]string, []string, error) {
	var allV4Addresses, allV6Addresses []string

	for _, domainName := range domainNames {
		// Find DNSNameResolver for this domain name
		dnsNameResolvers, err := c.dnsNameResolversLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list DNSNameResolvers: %v", err)
			continue
		}

		var foundResolver *kubeovnv1.DNSNameResolver
		for _, resolver := range dnsNameResolvers {
			if string(resolver.Spec.Name) == string(domainName) {
				foundResolver = resolver
				break
			}
		}

		if foundResolver == nil {
			klog.V(3).Infof("no DNSNameResolver found for domain %s, skipping", domainName)
			continue
		}

		// Get resolved addresses from DNSNameResolver
		v4Addresses, v6Addresses, err := getResolvedAddressesFromDNSNameResolver(foundResolver)
		if err != nil {
			klog.Errorf("failed to get resolved addresses from DNSNameResolver %s: %v", foundResolver.Name, err)
			continue
		}

		allV4Addresses = append(allV4Addresses, v4Addresses...)
		allV6Addresses = append(allV6Addresses, v6Addresses...)
	}

	return allV4Addresses, allV6Addresses, nil
}

func (c *Controller) updateCnpsByLabelsMatch(nsLabels, podLabels map[string]string) {
	cnps, _ := c.cnpsLister.List(labels.Everything())
	for _, cnp := range cnps {
		changed := &ClusterNetworkPolicyChangedDelta{
			key: cnp.Name,
		}

		// Pod/namespace that has been updated is the subject of a CNP, update that CNP
		if doCnpLabelsMatch(cnp.Spec.Subject.Namespaces, cnp.Spec.Subject.Pods, nsLabels, podLabels) {
			klog.Infof("cnp %s, labels matched for cnp's subject, nsLabels %s, podLabels %s", cnp.Name, labels.Set(nsLabels).String(), labels.Set(podLabels).String())
			changed.field = ChangedSubject
			c.updateCnpQueue.Add(changed)
		}

		ingressRuleNames, egressRuleNames := getAffectedCnpRules(cnp, nsLabels, podLabels)
		if !isCnpRulesArrayEmpty(ingressRuleNames) {
			klog.Infof("cnp %s, labels matched for cnp's ingress peer, nsLabels %s, podLabels %s", cnp.Name, labels.Set(nsLabels).String(), labels.Set(podLabels).String())
			changed.ruleNames = ingressRuleNames
			changed.field = ChangedIngressRule
			c.updateCnpQueue.Add(changed)
		}

		if !isCnpRulesArrayEmpty(egressRuleNames) {
			klog.Infof("cnp %s, labels matched for cnp's egress peer, nsLabels %s, podLabels %s", cnp.Name, labels.Set(nsLabels).String(), labels.Set(podLabels).String())
			changed.ruleNames = egressRuleNames
			changed.field = ChangedEgressRule
			c.updateCnpQueue.Add(changed)
		}
	}
}

// getAffectedCnpRules returns the rules affected by a namespace/pod update by looking at the selectors within its peers.
func getAffectedCnpRules(cnp *v1alpha2.ClusterNetworkPolicy, nsLabels, podLabels map[string]string) ([util.CnpMaxRules]ChangedName, [util.CnpMaxRules]ChangedName) {
	var changedIngressRuleNames, changedEgressRuleNames [util.CnpMaxRules]ChangedName

	for index, rule := range cnp.Spec.Ingress {
		for _, from := range rule.From {
			if doCnpLabelsMatch(from.Namespaces, from.Pods, nsLabels, podLabels) {
				changedIngressRuleNames[index].curRuleName = rule.Name
			}
		}
	}

	for index, rule := range cnp.Spec.Egress {
		for _, to := range rule.To {
			if doCnpLabelsMatch(to.Namespaces, to.Pods, nsLabels, podLabels) {
				changedEgressRuleNames[index].curRuleName = rule.Name
			}
		}
	}

	return changedIngressRuleNames, changedEgressRuleNames
}

// isCnpRulesArrayEmpty returns whether an array of changed ClusterNetworkPolicy rules is empty or not
func isCnpRulesArrayEmpty(rules [util.CnpMaxRules]ChangedName) bool {
	for _, rule := range rules {
		if rule.curRuleName != "" {
			return false
		}
	}
	return true
}

// getCnpPortGroupName returns the normalized name for the port group of a ClusterNetworkPolicy
func getCnpPortGroupName(cnp *v1alpha2.ClusterNetworkPolicy) string {
	// OVN port groups do not support name with '-', so we replace '-' by '.'
	// This may cause conflict if two CNP with name test-cnp and test.cnp
	// Maybe using hash is a better solution, but we do not want to lose the readability for now
	return strings.ReplaceAll(getCnpName(cnp.Name), "-", ".")
}

// shouldUpdateCnpPortGroup determines if the port group of a ClusterNetworkPolicy needs to be updated
func shouldUpdateCnpPortGroup(oldCnp, newCnp *v1alpha2.ClusterNetworkPolicy) bool {
	return !reflect.DeepEqual(oldCnp.Spec.Subject, newCnp.Spec.Subject)
}

// getCnpAddressSetsToUpdate returns the ingress/egress address sets that need to be updated following a ClusterNetworkPolicy update
func getCnpAddressSetsToUpdate(oldCnp, newCnp *v1alpha2.ClusterNetworkPolicy) (ingress, egress [util.CnpMaxRules]ChangedName) {
	// Search through every ingress rule for changed names or changed selectors
	for index, rule := range newCnp.Spec.Ingress {
		oldRule := oldCnp.Spec.Ingress[index]
		change := ChangedName{}

		if !reflect.DeepEqual(oldRule.From, rule.From) || oldRule.Name != rule.Name {
			change.curRuleName = rule.Name
		}
		if oldRule.Name != rule.Name {
			change.oldRuleName = oldRule.Name
		}

		ingress[index] = change
	}

	// Search through every egress rule for changed names or changed selectors
	for index, rule := range newCnp.Spec.Egress {
		oldRule := oldCnp.Spec.Egress[index]
		change := ChangedName{}

		if !reflect.DeepEqual(oldRule.To, rule.To) || oldRule.Name != rule.Name {
			change.curRuleName = rule.Name
		}
		if oldRule.Name != rule.Name {
			change.oldRuleName = oldRule.Name
		}

		egress[index] = change
	}

	return ingress, egress
}

// shouldRecreateCnpACLs determines if the ACLs for a ClusterNetworkPolicy should be re-created following an update
func shouldRecreateCnpACLs(oldCnp, newCnp *v1alpha2.ClusterNetworkPolicy) bool {
	// ACLs must be re-created if:
	//   - the tier of the CNP has changed
	//   - the priority of the CNP has changed
	//   - logging configuration of the CNP has changed
	//   - the count of ingress rules has changed
	//   - the count of egress rules has changed
	tierChanged := oldCnp.Spec.Tier != newCnp.Spec.Tier
	priorityChanged := oldCnp.Spec.Priority != newCnp.Spec.Priority
	ingressCountChanged := len(oldCnp.Spec.Ingress) != len(newCnp.Spec.Ingress)
	egressCountChanged := len(oldCnp.Spec.Egress) != len(newCnp.Spec.Egress)
	logChanged := oldCnp.Annotations[util.ACLActionsLogAnnotation] != newCnp.Annotations[util.ACLActionsLogAnnotation]

	if tierChanged || priorityChanged || ingressCountChanged || egressCountChanged || logChanged {
		return true
	}

	// ACLs must be re-created if ingress rules action or ports have changed
	for index, rule := range newCnp.Spec.Ingress {
		oldRule := oldCnp.Spec.Ingress[index]
		if oldRule.Action != rule.Action || !reflect.DeepEqual(oldRule.Ports, rule.Ports) {
			return true
		}
	}

	// ACLs must be re-created if egress rules action or ports have changed
	for index, rule := range newCnp.Spec.Egress {
		oldRule := oldCnp.Spec.Egress[index]
		if oldRule.Action != rule.Action || !reflect.DeepEqual(oldRule.Ports, rule.Ports) {
			return true
		}
	}

	return false
}

// getCnpPriorityMaps returns the maps linking CNPs in a specific tier with their priority
func (c *Controller) getCnpPriorityMaps(tier v1alpha2.Tier) (map[int32]string, map[string]int32, error) {
	switch tier {
	case v1alpha2.AdminTier:
		return c.anpPrioNameMap, c.anpNamePrioMap, nil
	case v1alpha2.BaselineTier:
		return c.bnpPrioNameMap, c.bnpNamePrioMap, nil
	default:
		return nil, nil, fmt.Errorf("unknown cnp tier %s", tier)
	}
}

// updateCnpPriorityMapEntries updates the entries of a ClusterNetworkPolicy in the priority maps of all tiers
func (c *Controller) updateCnpPriorityMapEntries(cnp *v1alpha2.ClusterNetworkPolicy) error {
	// Wipe the CNP from all the priority maps (this handles both tier change and priority change)
	if err := c.wipeCnpPriorityMapEntries(cnp); err != nil {
		return fmt.Errorf("failed to handle tier change for cnp %s: %w", cnp.Name, err)
	}

	// Handle priority changes within the (possibly changed) CNP tier
	priorityNameMap, namePriorityMap, err := c.getCnpPriorityMaps(cnp.Spec.Tier)
	if err != nil {
		return fmt.Errorf("failed to get priority maps for cnp %s: %w", cnp.Name, err)
	}

	// Update map entries for the CNP
	priorityNameMap[cnp.Spec.Priority] = cnp.Name
	namePriorityMap[cnp.Name] = cnp.Spec.Priority

	return nil
}

// deleteCnpPriorityMapEntries deletes entries of a ClusterNetworkPolicy in the priority maps
func (c *Controller) deleteCnpPriorityMapEntries(cnp *v1alpha2.ClusterNetworkPolicy) error {
	priorityNameMap, namePriorityMap, err := c.getCnpPriorityMaps(cnp.Spec.Tier)
	if err != nil {
		return fmt.Errorf("failed to get priority maps for cnp %s: %w", cnp.Name, err)
	}

	delete(priorityNameMap, cnp.Spec.Priority)
	delete(namePriorityMap, cnp.Name)

	return nil
}

// wipeCnpPriorityMapEntries removes a ClusterNetworkPolicy from every priority map in all tiers
func (c *Controller) wipeCnpPriorityMapEntries(cnp *v1alpha2.ClusterNetworkPolicy) error {
	tiers := []v1alpha2.Tier{v1alpha2.AdminTier, v1alpha2.BaselineTier}

	// For each exiting CNP tier, we wipe the CNP from the associated priority maps
	for _, tier := range tiers {
		priorityNameMap, namePriorityMap, err := c.getCnpPriorityMaps(tier)
		if err != nil {
			return fmt.Errorf("failed to get priority maps for cnp %s: %w", cnp.Name, err)
		}

		delete(priorityNameMap, namePriorityMap[cnp.Name])
		delete(namePriorityMap, cnp.Name)
	}

	return nil
}

// validateCnpConfig verifies a CNP is correctly written and doesn't conflict with any other
func (c *Controller) validateCnpConfig(cnp *v1alpha2.ClusterNetworkPolicy) error {
	// Get the priority map of the CNP
	priorityNameMap, _, err := c.getCnpPriorityMaps(cnp.Spec.Tier)
	if err != nil {
		err := fmt.Errorf("failed to get priority maps for cnp %s: %w", cnp.Name, err)
		klog.Error(err)
		return err
	}

	// Check the CNP respects priority rules
	if err := checkCnpPriorities(priorityNameMap, cnp); err != nil {
		return err
	}

	// Check the number of ingress and egress rule doesn't exceed the limit
	if len(cnp.Spec.Ingress) > util.CnpMaxRules || len(cnp.Spec.Egress) > util.CnpMaxRules {
		err := fmt.Errorf("at most %d rules allowed by ingress/egress section for cnp %s, got %d ingress rules and %d egress rules", util.CnpMaxRules, cnp.Name, len(cnp.Spec.Ingress), len(cnp.Spec.Egress))
		klog.Error(err)
		return err
	}

	// Check domain and network rules are respected for peers
	if err := checkNetworkAndDomainRules(cnp); err != nil {
		return err
	}

	return nil
}

// checkCnpPriorities checks if a ClusterNetworkPolicy respects the priority rules defined by the standard:
//   - it must not collide with the priority of another CNP in the same tier
//   - the maximum priority must not be greater than the limit
func checkCnpPriorities(priorityNameMap map[int32]string, cnp *v1alpha2.ClusterNetworkPolicy) error {
	// Sanitize the function input
	if priorityNameMap == nil || cnp == nil {
		err := errors.New("must provide a priorityMap and a CNP")
		klog.Error(err)
		return err
	}

	// The behavior is undefined if two CNP objects of the same tier have the same priority
	if cnpName, exist := priorityNameMap[cnp.Spec.Priority]; exist && cnpName != cnp.Name {
		err := fmt.Errorf("can not create cnp %s with priority %d, cnp %s already exists with the same priority", cnp.Name, cnp.Spec.Priority, cnpName)
		klog.Error(err)
		return err
	}

	// We have noticed RedHat's discussion about ACL priority in https://bugzilla.redhat.com/show_bug.cgi?id=2175752
	// After discussion, we decided to use the same range of priorities (20000-30000). Pay tribute to the developers of RedHat.
	// This is a deviation from the standard of the API (max priority should be 1000).
	if cnp.Spec.Priority > util.CnpMaxPriority || cnp.Spec.Priority < 0 {
		err := fmt.Errorf("priority of cnp %s is not within bounds 0 to %d", cnp.Name, util.CnpMaxPriority)
		klog.Error(err)
		return err
	}

	return nil
}

// checkNetworkAndDomainRules checks if a clusterNetworkPolicy respects the following rules:
//   - number of domains per peer egress is not greater than the limit
//   - number of networks per peer egress is not greater than the limit
func checkNetworkAndDomainRules(cnp *v1alpha2.ClusterNetworkPolicy) error {
	for _, egressRule := range cnp.Spec.Egress {
		for _, peer := range egressRule.To {
			if len(peer.DomainNames) > util.CnpMaxDomains {
				return fmt.Errorf("cnp egress peers can have a maximum of %d domains, got %d", util.CnpMaxDomains, len(peer.DomainNames))
			}

			if len(peer.Networks) > util.CnpMaxNetworks {
				return fmt.Errorf("cnp egress peers can have a maximum of %d domains, got %d", util.CnpMaxNetworks, len(peer.Networks))
			}
		}
	}

	return nil
}

// fetchCnpCIDRAddresses returns the IPv4 and IPv6 addresses within a CNP CIDR
func fetchCnpCIDRAddresses(networks []v1alpha2.CIDR) ([]string, []string) {
	var v4Addresses, v6Addresses []string

	// Sort IPv4 and IPv6 networks by protocol
	for _, network := range networks {
		if _, _, err := net.ParseCIDR(string(network)); err != nil {
			klog.Errorf("invalid cidr %s", string(network))
			continue
		}
		switch util.CheckProtocol(string(network)) {
		case kubeovnv1.ProtocolIPv4:
			v4Addresses = append(v4Addresses, string(network))
		case kubeovnv1.ProtocolIPv6:
			v6Addresses = append(v6Addresses, string(network))
		}
	}

	return v4Addresses, v6Addresses
}

// getCnpName returns a normalized name for ClusterNetworkPolicies for insertion in OVN databases
// TODO: normalize prefix in any case?
func getCnpName(name string) string {
	nameArray := []rune(name)

	// OVN will not handle the name if it doesn't start with a letter
	// We add a prefix to make it compliant (if it is necessary)
	if !unicode.IsLetter(nameArray[0]) {
		name = clusterNetworkPolicyKey + name
	}

	return name
}

// getCnpACLAction returns the OVN ACL action associated with a CNP rule action
func getCnpACLAction(action v1alpha2.ClusterNetworkPolicyRuleAction) ovnnb.ACLAction {
	switch action {
	case v1alpha2.ClusterNetworkPolicyRuleActionAccept:
		return ovnnb.ACLActionAllowRelated
	case v1alpha2.ClusterNetworkPolicyRuleActionDeny:
		return ovnnb.ACLActionDrop
	case v1alpha2.ClusterNetworkPolicyRuleActionPass:
		return ovnnb.ACLActionPass
	default:
		return ovnnb.ACLActionDrop
	}
}

// getCnpACLTier returns the OVN ACL tier for a given CNP tier
func getCnpACLTier(tier v1alpha2.Tier) int {
	switch tier {
	case v1alpha2.AdminTier:
		return util.AnpACLTier
	case v1alpha2.BaselineTier:
		return util.BanpACLTier
	default:
		return util.BanpACLTier
	}
}

// getCnpDomainsNames returns all the domain names in rules contained within a ClusterNetworkPolicy
func getCnpDomainsNames(cnp *v1alpha2.ClusterNetworkPolicy) (domainNames []string) {
	for _, rule := range cnp.Spec.Egress {
		for _, to := range rule.To {
			for _, domainName := range to.DomainNames {
				domainNames = append(domainNames, string(domainName))
			}
		}
	}

	return domainNames
}

// hasCnpDomainNames returns whether a ClusterNetworkPolicy has domain names defined
func hasCnpDomainNames(cnp *v1alpha2.ClusterNetworkPolicy) bool {
	for _, rule := range cnp.Spec.Egress {
		for _, to := range rule.To {
			if len(to.DomainNames) > 0 {
				return true
			}
		}
	}

	return false
}

// getCnpACLPriority returns the ACL priority of a ClusterNetworkPolicy for a given priority and rule index
func getCnpACLPriority(cnp *v1alpha2.ClusterNetworkPolicy, index int) int {
	return util.CnpACLMaxPriority - int(cnp.Spec.Priority*util.CnpMaxRules) - index
}

// getCnpACLName returns the name of an ACL for a given ClusterNetworkPolicy, protocol, direction and rule index
func getCnpACLName(cnpName, protocol, direction string, index int) string {
	return fmt.Sprintf("%s/%s/%s/%s/%d", clusterNetworkPolicyKey, cnpName, direction, protocol, index)
}

// doCnpLabelsMatch returns whether namespace/pod selectors on a ClusterNetworkPolicy match the labels of some pods/namespaces
// This is used to determine if the "subject" or "rule peers" of a CNP match pods/namespaces
func doCnpLabelsMatch(namespaces *metav1.LabelSelector, pods *v1alpha2.NamespacedPod, nsLabels, podLabels map[string]string) bool {
	// Exactly one field of namespaces/pods must be set
	if namespaces != nil {
		nsSelector, _ := metav1.LabelSelectorAsSelector(namespaces)
		if nsSelector.Matches(labels.Set(nsLabels)) {
			return true
		}
	} else if pods != nil {
		nsSelector, _ := metav1.LabelSelectorAsSelector(&pods.NamespaceSelector)
		podSelector, _ := metav1.LabelSelectorAsSelector(&pods.PodSelector)
		if nsSelector.Matches(labels.Set(nsLabels)) && podSelector.Matches(labels.Set(podLabels)) {
			return true
		}
	}

	return false
}
