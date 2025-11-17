package controller

import (
	"errors"
	"fmt"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/scylladb/go-set/strset"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"net"
	"reflect"
	"sigs.k8s.io/network-policy-api/apis/v1alpha1"
	"sigs.k8s.io/network-policy-api/apis/v1alpha2"
	"strings"
	"unicode"
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
	c.deleteCnpQueue.Add(cnp)
}

// enqueueUpdateCnp adds an existing ClusterNetworkPolicy to the processing queue for updates
func (c *Controller) enqueueUpdateCnp(oldObj, newObj any) {
	oldCnp := oldObj.(*v1alpha2.ClusterNetworkPolicy)
	newCnp := newObj.(*v1alpha2.ClusterNetworkPolicy)

	// All the ACLs should be recreated if:
	//   - the priority has changed
	//   - the count of ingress rules has changed
	//   - the count of egress rules has changed
	if oldCnp.Spec.Priority != newCnp.Spec.Priority || len(oldCnp.Spec.Ingress) != len(newCnp.Spec.Ingress) || len(oldCnp.Spec.Egress) != len(newCnp.Spec.Egress) {
		c.addCnpQueue.Add(newCnp.Name)
		return
	}

	// ACLs should be updated when action or ports of ingress/egress rule has been changed
	for index, rule := range newCnp.Spec.Ingress {
		oldRule := newCnp.Spec.Ingress[index]
		if oldRule.Action != rule.Action || !reflect.DeepEqual(oldRule.Ports, rule.Ports) {
			c.addCnpQueue.Add(newCnp.Name)
			return
		}
	}

	for index, rule := range newCnp.Spec.Egress {
		oldRule := oldCnp.Spec.Egress[index]
		if oldRule.Action != rule.Action || !reflect.DeepEqual(oldRule.Ports, rule.Ports) {
			c.addCnpQueue.Add(newCnp.Name)
			return
		}
	}

	// Re-create the ACLs if the logging has been enabled/disabled
	if oldCnp.Annotations[util.ACLActionsLogAnnotation] != newCnp.Annotations[util.ACLActionsLogAnnotation] {
		c.addCnpQueue.Add(newCnp.Name)
		return
	}

	klog.V(3).Infof("enqueue update cnp %s", newCnp.Name)

	// The remaining changes do not affect the ACLs. The port-group or address-set should be updated.
	// The port-group for CNP should be updated
	if !reflect.DeepEqual(oldCnp.Spec.Subject, newCnp.Spec.Subject) {
		c.updateCnpQueue.Add(&ClusterNetworkPolicyChangedDelta{key: newCnp.Name, field: ChangedSubject})
	}

	// Rule name or peer selector in ingress/egress rule has changed, the corresponding address-set need be updated
	ruleChanged := false
	var changedIngressRuleNames, changedEgressRuleNames [util.CnpMaxRules]ChangedName

	for index, rule := range newCnp.Spec.Ingress {
		oldRule := oldCnp.Spec.Ingress[index]
		if oldRule.Name != rule.Name {
			changedIngressRuleNames[index] = ChangedName{oldRuleName: oldRule.Name, curRuleName: rule.Name}
			ruleChanged = true
		}
		if !reflect.DeepEqual(oldRule.From, rule.From) {
			changedIngressRuleNames[index] = ChangedName{curRuleName: rule.Name}
			ruleChanged = true
		}
	}
	if ruleChanged {
		c.updateCnpQueue.Add(&ClusterNetworkPolicyChangedDelta{key: newCnp.Name, ruleNames: changedIngressRuleNames, field: ChangedIngressRule})
	}

	ruleChanged = false
	for index, rule := range newCnp.Spec.Egress {
		oldRule := oldCnp.Spec.Egress[index]
		if oldRule.Name != rule.Name {
			changedEgressRuleNames[index] = ChangedName{oldRuleName: oldRule.Name, curRuleName: rule.Name}
			ruleChanged = true
		}
		if !reflect.DeepEqual(oldRule.To, rule.To) {
			changedEgressRuleNames[index] = ChangedName{curRuleName: rule.Name}
			ruleChanged = true
		}
	}
	if ruleChanged {
		c.updateCnpQueue.Add(&ClusterNetworkPolicyChangedDelta{key: newCnp.Name, ruleNames: changedEgressRuleNames, field: ChangedEgressRule})
	}
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

	// Verify the CNP is correctly written
	priorityMap, _ := c.getCnpPriorityMaps(cnp.Spec.Tier)
	if priorityMap == nil {
		err := fmt.Errorf("unknown CNP tier: %s", cnp.Spec.Tier)
		klog.Error(err)
		return err
	}

	if err := validateCnpConfig(priorityMap, cnp); err != nil {
		klog.Errorf("failed to validate cnp %s: %v", cnp.Name, err)
		return err
	}

	priorityNameMap, namePriorityMap := c.getCnpPriorityMaps(cnp.Spec.Tier)
	if priorityNameMap == nil {
		err := fmt.Errorf("unknown CNP tier: %s", cnp.Spec.Tier)
		klog.Error(err)
		return err
	}

	if priority, exist := namePriorityMap[cnp.Name]; exist && priority != cnp.Spec.Priority {
		// anp spec's priority has been changed
		delete(priorityNameMap, priority)
	}

	// record new created anp after validation
	priorityNameMap[cnp.Spec.Priority] = cnp.Name
	namePriorityMap[cnp.Name] = cnp.Spec.Priority

	cnpName := getCnpName(cnp.Name)
	var logActions []string
	if cnp.Annotations[util.ACLActionsLogAnnotation] != "" {
		logActions = strings.Split(cnp.Annotations[util.ACLActionsLogAnnotation], ",")
	}

	// ovn portGroup/addressSet doesn't support name with '-', so we replace '-' by '.'.
	// This may cause conflict if two CNP with name test-anp and test.anp, maybe hash is a better solution, but we do not want to lose the readability now.
	// Make sure all create operations are reentrant.
	pgName := strings.ReplaceAll(cnpName, "-", ".")
	if err = c.OVNNbClient.CreatePortGroup(pgName, map[string]string{clusterNetworkPolicyKey: cnpName}); err != nil {
		klog.Errorf("failed to create port group for cnp %s: %v", key, err)
		return err
	}

	ports, err := c.fetchPodsSelectedByCnp(&cnp.Spec.Subject)
	if err != nil {
		klog.Errorf("failed to fetch ports belongs to cnp %s: %v", key, err)
		return err
	}

	if err = c.OVNNbClient.PortGroupSetPorts(pgName, ports); err != nil {
		klog.Errorf("failed to set ports %v to port group %s: %v", ports, pgName, err)
		return err
	}

	ingressACLOps, err := c.OVNNbClient.DeleteAclsOps(pgName, portGroupKey, "to-lport", nil)
	if err != nil {
		klog.Errorf("failed to generate clear operations for cnp %s ingress acls: %v", key, err)
		return err
	}

	curIngressAddrSet, curEgressAddrSet, err := c.getCurrentAddrSetByName(cnpName, false)
	if err != nil {
		klog.Errorf("failed to list address sets for cnp %s: %v", key, err)
		return err
	}
	desiredIngressAddrSet := strset.NewWithSize(len(cnp.Spec.Ingress) * 2)
	desiredEgressAddrSet := strset.NewWithSize(len(cnp.Spec.Egress) * 2)

	// create ingress acl
	for index, cnpr := range cnp.Spec.Ingress {
		// A single address set must contain addresses of the same type and the name must be unique within table, so IPv4 and IPv6 address set should be different
		ingressAsV4Name, ingressAsV6Name := getAnpAddressSetName(pgName, cnpr.Name, index, true)
		desiredIngressAddrSet.Add(ingressAsV4Name, ingressAsV6Name)

		var v4Addrs, v4Addr, v6Addrs, v6Addr []string
		// This field must be defined and contain at least one item.
		for _, cnprpeer := range cnpr.From {
			if v4Addr, v6Addr, err = c.fetchIngressSelectedAddressesByCnp(&cnprpeer); err != nil {
				klog.Errorf("failed to fetch cnp selected addresses, %v", err)
				return err
			}
			v4Addrs = append(v4Addrs, v4Addr...)
			v6Addrs = append(v6Addrs, v6Addr...)
		}
		klog.Infof("cnp %s, ingress rule %s, selected v4 address %v, v6 address %v", cnpName, cnpr.Name, v4Addrs, v6Addrs)

		if err = c.createAsForAnpRule(cnpName, cnpr.Name, "ingress", ingressAsV4Name, v4Addrs, false); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.createAsForAnpRule(cnpName, cnpr.Name, "ingress", ingressAsV6Name, v6Addrs, false); err != nil {
			klog.Error(err)
			return err
		}

		aclPriority := util.AnpACLMaxPriority - int(cnp.Spec.Priority*100) - index
		aclAction := getCnpACLAction(cnpr.Action)
		rulePorts := []v1alpha2.ClusterNetworkPolicyPort{}
		if cnpr.Ports != nil {
			rulePorts = *cnpr.Ports
		}

		if len(v4Addrs) != 0 {
			aclName := fmt.Sprintf("cnp/%s/ingress/%s/%d", cnpName, kubeovnv1.ProtocolIPv4, index)
			ops, err := c.OVNNbClient.UpdateCnpRuleACLOps(pgName, ingressAsV4Name, kubeovnv1.ProtocolIPv4, aclName, aclPriority, aclAction, logActions, rulePorts, true, false)
			if err != nil {
				klog.Errorf("failed to add v4 ingress acls for anp %s: %v", key, err)
				return err
			}
			ingressACLOps = append(ingressACLOps, ops...)
		}

		if len(v6Addrs) != 0 {
			aclName := fmt.Sprintf("cnp/%s/ingress/%s/%d", cnpName, kubeovnv1.ProtocolIPv6, index)
			ops, err := c.OVNNbClient.UpdateCnpRuleACLOps(pgName, ingressAsV6Name, kubeovnv1.ProtocolIPv6, aclName, aclPriority, aclAction, logActions, rulePorts, true, false)
			if err != nil {
				klog.Errorf("failed to add v6 ingress acls for anp %s: %v", key, err)
				return err
			}
			ingressACLOps = append(ingressACLOps, ops...)
		}
	}

	if err := c.OVNNbClient.Transact("add-ingress-acls", ingressACLOps); err != nil {
		return fmt.Errorf("failed to add ingress acls for anp %s: %w", key, err)
	}
	if err := c.deleteUnusedAddrSetForAnp(curIngressAddrSet, desiredIngressAddrSet); err != nil {
		return fmt.Errorf("failed to delete unused ingress address set for cnp %s: %w", key, err)
	}

	egressACLOps, err := c.OVNNbClient.DeleteAclsOps(pgName, portGroupKey, "from-lport", nil)
	if err != nil {
		klog.Errorf("failed to generate clear operations for cnp %s egress acls: %v", key, err)
		return err
	}

	// Reconcile DNSNameResolvers for all collected domain names
	if c.config.EnableDNSNameResolver {
		// Collect all domain names from egress rules
		var allDomainNames []string
		for _, anpr := range cnp.Spec.Egress {
			for _, anprpeer := range anpr.To {
				if len(anprpeer.DomainNames) != 0 {
					for _, domainName := range anprpeer.DomainNames {
						allDomainNames = append(allDomainNames, string(domainName))
					}
				}
			}
		}

		if err := c.reconcileDNSNameResolversForANP(cnpName, allDomainNames); err != nil {
			klog.Errorf("failed to reconcile DNSNameResolvers for CNP %s: %v", cnpName, err)
			return err
		}
	}

	// create egress acl
	for index, anpr := range cnp.Spec.Egress {
		// A single address set must contain addresses of the same type and the name must be unique within table, so IPv4 and IPv6 address set should be different
		egressAsV4Name, egressAsV6Name := getAnpAddressSetName(pgName, anpr.Name, index, false)
		desiredEgressAddrSet.Add(egressAsV4Name, egressAsV6Name)

		var v4Addrs, v4Addr, v6Addrs, v6Addr []string
		hasDomainNames := false
		// This field must be defined and contain at least one item.
		for _, anprpeer := range anpr.To {
			if v4Addr, v6Addr, err = c.fetchEgressSelectedAddressesByCnp(&anprpeer); err != nil {
				klog.Errorf("failed to fetch admin network policy selected addresses, %v", err)
				return err
			}
			v4Addrs = append(v4Addrs, v4Addr...)
			v6Addrs = append(v6Addrs, v6Addr...)

			// Check if this peer has domain names
			hasDomainNames = len(anprpeer.DomainNames) > 0
		}
		klog.Infof("cnp, %s, egress rule %s, selected v4 address %v, v6 address %v", cnpName, anpr.Name, v4Addrs, v6Addrs)

		if err = c.createAsForAnpRule(cnpName, anpr.Name, "egress", egressAsV4Name, v4Addrs, false); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.createAsForAnpRule(cnpName, anpr.Name, "egress", egressAsV6Name, v6Addrs, false); err != nil {
			klog.Error(err)
			return err
		}

		aclPriority := util.AnpACLMaxPriority - int(cnp.Spec.Priority*100) - index
		aclAction := getCnpACLAction(anpr.Action)
		rulePorts := []v1alpha2.ClusterNetworkPolicyPort{}
		if anpr.Ports != nil {
			rulePorts = *anpr.Ports
		}

		// Create ACL rules if we have IP addresses OR domain names.
		// Domain names may not be resolved initially but will be updated later
		if len(v4Addrs) != 0 || hasDomainNames {
			aclName := fmt.Sprintf("cnp/%s/egress/%s/%d", cnpName, kubeovnv1.ProtocolIPv4, index)
			ops, err := c.OVNNbClient.UpdateCnpRuleACLOps(pgName, egressAsV4Name, kubeovnv1.ProtocolIPv4, aclName, aclPriority, aclAction, logActions, rulePorts, false, false)
			if err != nil {
				klog.Errorf("failed to add v4 egress acls for cnp %s: %v", key, err)
				return err
			}
			egressACLOps = append(egressACLOps, ops...)
		}

		if len(v6Addrs) != 0 || hasDomainNames {
			aclName := fmt.Sprintf("cnp/%s/egress/%s/%d", cnpName, kubeovnv1.ProtocolIPv6, index)
			ops, err := c.OVNNbClient.UpdateCnpRuleACLOps(pgName, egressAsV6Name, kubeovnv1.ProtocolIPv6, aclName, aclPriority, aclAction, logActions, rulePorts, false, false)
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
		return fmt.Errorf("failed to delete unused egress address set for anp %s: %w", key, err)
	}

	return nil
}

func (c *Controller) handleUpdateCnp(changed *ClusterNetworkPolicyChangedDelta) error {
	// Only handle updates that do not affect ACLs.
	c.anpKeyMutex.LockKey(changed.key)
	defer func() { _ = c.anpKeyMutex.UnlockKey(changed.key) }()

	klog.Infof("handleUpdateAnp: processing CNP %s, field=%s, DNSReconcileDone=%v",
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

	cnpName := getCnpName(desiredCnp.Name)
	pgName := strings.ReplaceAll(cnpName, "-", ".")

	// The port-group for anp should be updated
	if changed.field == ChangedSubject {
		// The port-group must exist when update anp, this check should never be matched.
		if ok, err := c.OVNNbClient.PortGroupExists(pgName); !ok || err != nil {
			klog.Errorf("port-group for cnp %s does not exist when update anp", desiredCnp.Name)
			return err
		}

		ports, err := c.fetchPodsSelectedByCnp(&desiredCnp.Spec.Subject)
		if err != nil {
			klog.Errorf("failed to fetch ports belongs to cnp %s: %v", desiredCnp.Name, err)
			return err
		}

		if err = c.OVNNbClient.PortGroupSetPorts(pgName, ports); err != nil {
			klog.Errorf("failed to set ports %v to port group %s: %v", ports, pgName, err)
			return err
		}
	}

	// Peer selector in ingress/egress rule has changed, so the corresponding address-set need be updated
	if changed.field == ChangedIngressRule {
		for index, rule := range desiredCnp.Spec.Ingress {
			// Make sure the rule is changed and go on update
			if rule.Name == changed.ruleNames[index].curRuleName || changed.ruleNames[index].isMatch {
				if err := c.setAddrSetForAnpRuleForCnp(cnpName, pgName, rule.Name, index, rule.From, []v1alpha2.ClusterNetworkPolicyEgressPeer{}, true, false); err != nil {
					klog.Errorf("failed to set ingress address-set for cnp rule %s/%s, %v", cnpName, rule.Name, err)
					return err
				}

				if changed.ruleNames[index].oldRuleName != "" {
					oldRuleName := changed.ruleNames[index].oldRuleName
					// Normally the name can not be changed, but when the name really changes, the old address set should be deleted
					// There is no description in the Name comments that it cannot be changed
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
			needAddrSetUpdate := rule.Name == changed.ruleNames[index].curRuleName || changed.ruleNames[index].isMatch || changed.DNSReconcileDone

			// Check if we need to reconcile DNS resolvers (DNS feature enabled and not already done)
			needDNSReconcile := c.config.EnableDNSNameResolver && !changed.DNSReconcileDone

			if needAddrSetUpdate {
				if err := c.setAddrSetForAnpRuleForCnp(cnpName, pgName, rule.Name, index, []v1alpha2.ClusterNetworkPolicyIngressPeer{}, rule.To, false, false); err != nil {
					klog.Errorf("failed to set egress address-set for anp rule %s/%s, %v", cnpName, rule.Name, err)
					return err
				}

				if needDNSReconcile {
					var currentDomainNames []string
					for _, peer := range rule.To {
						for _, domainName := range peer.DomainNames {
							currentDomainNames = append(currentDomainNames, string(domainName))
						}
					}

					if err := c.reconcileDNSNameResolversForANP(cnpName, currentDomainNames); err != nil {
						klog.Errorf("failed to reconcile DNSNameResolvers for egress rule %s/%s, %v", cnpName, rule.Name, err)
						return err
					}
				}

				if changed.ruleNames[index].oldRuleName != "" {
					oldRuleName := changed.ruleNames[index].oldRuleName
					// Normally the name can not be changed, but when the name really changes, the old address set should be deleted
					// There is no description in the Name comments that it cannot be changed
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

func (c *Controller) handleDeleteCnp(cnp *v1alpha2.ClusterNetworkPolicy) error {
	c.cnpKeyMutex.LockKey(cnp.Name)
	defer func() { _ = c.cnpKeyMutex.UnlockKey(cnp.Name) }()

	klog.Infof("handle delete cluster network policy %s", cnp.Name)

	// TODO: priority for bnps
	delete(c.anpPrioNameMap, cnp.Spec.Priority)
	delete(c.anpNamePrioMap, cnp.Name)

	cnpName := getCnpName(cnp.Name)

	// ACLs related to port_group will be deleted automatically when port_group is deleted
	pgName := strings.ReplaceAll(cnpName, "-", ".")
	if err := c.OVNNbClient.DeletePortGroup(pgName); err != nil {
		klog.Errorf("failed to delete port group for cnp %s: %v", cnpName, err)
	}

	if err := c.OVNNbClient.DeleteAddressSets(map[string]string{
		clusterNetworkPolicyKey: fmt.Sprintf("%s/%s", cnpName, "ingress"),
	}); err != nil {
		klog.Errorf("failed to delete ingress address set for cnp %s: %v", cnpName, err)
		return err
	}

	if err := c.OVNNbClient.DeleteAddressSets(map[string]string{
		clusterNetworkPolicyKey: fmt.Sprintf("%s/%s", cnpName, "egress"),
	}); err != nil {
		klog.Errorf("failed to delete egress address set for cnp %s: %v", cnpName, err)
		return err
	}

	// Delete all DNSNameResolver CRs associated with this CNP
	if cnp.Spec.Tier == v1alpha2.AdminTier && c.config.EnableDNSNameResolver {
		if err := c.reconcileDNSNameResolversForANP(cnpName, []string{}); err != nil {
			klog.Errorf("failed to delete DNSNameResolver CRs for cnp %s: %v", cnpName, err)
			return err
		}
	}

	return nil
}

///////////////////////////
// REFACTORED
///////////////////////////

// getCnpPriorityMaps returns the maps linking CNPs in a specific tier with their priority
func (c *Controller) getCnpPriorityMaps(tier v1alpha2.Tier) (map[int32]string, map[string]int32) {
	switch tier {
	case v1alpha2.AdminTier:
		return c.anpPrioNameMap, c.anpNamePrioMap
	case v1alpha2.BaselineTier:
		return c.bnpPrioNameMap, c.bnpNamePrioMap
	default:
		return nil, nil
	}
}

func (c *Controller) fetchPodsSelectedByCnp(cnpSubject *v1alpha2.ClusterNetworkPolicySubject) ([]string, error) {
	var ports []string

	// Exactly one field must be set
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

	klog.Infof("get selected ports for subject, %v", ports)
	return ports, nil
}

func (c *Controller) fetchIngressSelectedAddressesByCnp(ingressPeer *v1alpha2.ClusterNetworkPolicyIngressPeer) ([]string, []string, error) {
	var v4Addresses, v6Addresses []string

	// Exactly one of the selector pointers must be set for a given peer.
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

func (c *Controller) setAddrSetForAnpRuleForCnp(anpName, pgName, ruleName string, index int, from []v1alpha2.ClusterNetworkPolicyIngressPeer, to []v1alpha2.ClusterNetworkPolicyEgressPeer, isIngress, isBanp bool) error {
	return c.setAddrSetForAnpRuleCommonForCnp(anpName, pgName, ruleName, index, from, to, nil, isIngress, isBanp)
}

func (c *Controller) setAddrSetForAnpRuleCommonForCnp(anpName, pgName, ruleName string, index int, from []v1alpha2.ClusterNetworkPolicyIngressPeer, to []v1alpha2.ClusterNetworkPolicyEgressPeer, baselineTo []v1alpha1.BaselineAdminNetworkPolicyEgressPeer, isIngress, isBanp bool) error {
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
			klog.Errorf("Failed to list DNSNameResolvers: %v", err)
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
			klog.V(3).Infof("No DNSNameResolver found for domain %s, skipping", domainName)
			continue
		}

		// Get resolved addresses from DNSNameResolver
		v4Addresses, v6Addresses, err := getResolvedAddressesFromDNSNameResolver(foundResolver)
		if err != nil {
			klog.Errorf("Failed to get resolved addresses from DNSNameResolver %s: %v", foundResolver.Name, err)
			continue
		}

		allV4Addresses = append(allV4Addresses, v4Addresses...)
		allV6Addresses = append(allV6Addresses, v6Addresses...)
	}

	return allV4Addresses, allV6Addresses, nil
}

// up to here, tested, refactored

// validateCnpConfig verifies a CNP is correctly written and doesn't conflict with any other
func validateCnpConfig(priorityNameMap map[int32]string, cnp *v1alpha2.ClusterNetworkPolicy) error {
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

	return nil
}

// checkCnpPriorities checks if a ClusterNetworkPolicy respects the priority rules defined by the standard:
//   - it must not collide with the priority of another CNP in the same tier
//   - the maximum priority must not be greater than the hardcoded limit
func checkCnpPriorities(priorityNameMap map[int32]string, cnp *v1alpha2.ClusterNetworkPolicy) error {
	// Sanitize the function input
	if priorityNameMap == nil || cnp == nil {
		err := fmt.Errorf("must provide a priorityMap and a CNP")
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
func getCnpName(name string) string {
	nameArray := []rune(name)

	// OVN will not handle the name if it doesn't start with a letter
	// We add a prefix to make it compliant (if it is necessary)
	if !unicode.IsLetter(nameArray[0]) {
		name = "cnp" + name
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
