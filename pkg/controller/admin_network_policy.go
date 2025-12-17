package controller

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"unicode"

	"github.com/scylladb/go-set/strset"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type ChangedField string

const (
	ChangedSubject     ChangedField = "Subject"
	ChangedIngressRule ChangedField = "IngressRule"
	ChangedEgressRule  ChangedField = "EgressRule"
)

type ChangedName struct {
	// the rule name can be omitted default, add isMatch to append check for rule update
	isMatch     bool
	oldRuleName string
	curRuleName string
}

type AdminNetworkPolicyChangedDelta struct {
	key              string
	ruleNames        [util.AnpMaxRules]ChangedName
	field            ChangedField
	DNSReconcileDone bool
}

func (c *Controller) enqueueAddAnp(obj any) {
	key := cache.MetaObjectToName(obj.(*v1alpha1.AdminNetworkPolicy)).String()
	klog.V(3).Infof("enqueue add anp %s", key)
	c.addAnpQueue.Add(key)
}

func (c *Controller) enqueueDeleteAnp(obj any) {
	var anp *v1alpha1.AdminNetworkPolicy
	switch t := obj.(type) {
	case *v1alpha1.AdminNetworkPolicy:
		anp = t
	case cache.DeletedFinalStateUnknown:
		a, ok := t.Obj.(*v1alpha1.AdminNetworkPolicy)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		anp = a
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	klog.V(3).Infof("enqueue delete anp %s", cache.MetaObjectToName(anp).String())
	c.deleteAnpQueue.Add(anp.DeepCopy())
}

func (c *Controller) enqueueUpdateAnp(oldObj, newObj any) {
	oldAnpObj := oldObj.(*v1alpha1.AdminNetworkPolicy)
	newAnpObj := newObj.(*v1alpha1.AdminNetworkPolicy)

	// All the acls should be recreated with the following situations
	if oldAnpObj.Spec.Priority != newAnpObj.Spec.Priority || len(oldAnpObj.Spec.Ingress) != len(newAnpObj.Spec.Ingress) || len(oldAnpObj.Spec.Egress) != len(newAnpObj.Spec.Egress) {
		c.addAnpQueue.Add(newAnpObj.Name)
		return
	}

	// Acls should be updated when action or ports of ingress/egress rule has been changed
	for index, rule := range newAnpObj.Spec.Ingress {
		oldRule := oldAnpObj.Spec.Ingress[index]
		if oldRule.Action != rule.Action || !reflect.DeepEqual(oldRule.Ports, rule.Ports) {
			// It's difficult to distinguish which rule has changed and update acls for that rule, so go through the anp add process to recreate acls.
			// If we want to get fine-grained changes over rule, maybe it's a better way to add a new queue to process the change
			c.addAnpQueue.Add(newAnpObj.Name)
			return
		}
	}

	for index, rule := range newAnpObj.Spec.Egress {
		oldRule := oldAnpObj.Spec.Egress[index]
		if oldRule.Action != rule.Action || !reflect.DeepEqual(oldRule.Ports, rule.Ports) {
			c.addAnpQueue.Add(newAnpObj.Name)
			return
		}
	}

	if oldAnpObj.Annotations[util.ACLActionsLogAnnotation] != newAnpObj.Annotations[util.ACLActionsLogAnnotation] {
		c.addAnpQueue.Add(newAnpObj.Name)
		return
	}
	klog.V(3).Infof("enqueue update anp %s", newAnpObj.Name)

	// The remaining changes do not affect the acls. The port-group or address-set should be updated.
	// The port-group for anp should be updated
	if !reflect.DeepEqual(oldAnpObj.Spec.Subject, newAnpObj.Spec.Subject) {
		c.updateAnpQueue.Add(&AdminNetworkPolicyChangedDelta{key: newAnpObj.Name, field: ChangedSubject})
	}

	// Rule name or peer selector in ingress/egress rule has changed, the corresponding address-set need be updated
	ruleChanged := false
	var changedIngressRuleNames, changedEgressRuleNames [util.AnpMaxRules]ChangedName
	for index, rule := range newAnpObj.Spec.Ingress {
		oldRule := oldAnpObj.Spec.Ingress[index]
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
		c.updateAnpQueue.Add(&AdminNetworkPolicyChangedDelta{key: newAnpObj.Name, ruleNames: changedIngressRuleNames, field: ChangedIngressRule})
	}

	ruleChanged = false
	for index, rule := range newAnpObj.Spec.Egress {
		oldRule := oldAnpObj.Spec.Egress[index]
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
		c.updateAnpQueue.Add(&AdminNetworkPolicyChangedDelta{key: newAnpObj.Name, ruleNames: changedEgressRuleNames, field: ChangedEgressRule})
	}
}

func (c *Controller) handleAddAnp(key string) (err error) {
	c.anpKeyMutex.LockKey(key)
	defer func() { _ = c.anpKeyMutex.UnlockKey(key) }()

	cachedAnp, err := c.anpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	klog.Infof("handle add anp %s", cachedAnp.Name)
	anp := cachedAnp.DeepCopy()

	if err := c.validateAnpConfig(anp); err != nil {
		klog.Errorf("failed to validate anp %s: %v", anp.Name, err)
		return err
	}
	if priority, exist := c.anpNamePrioMap[anp.Name]; exist && priority != anp.Spec.Priority {
		// anp spec's priority has been changed
		delete(c.anpPrioNameMap, priority)
	}
	// record new created anp after validation
	c.anpPrioNameMap[anp.Spec.Priority] = anp.Name
	c.anpNamePrioMap[anp.Name] = anp.Spec.Priority

	anpName := getAnpName(anp.Name)
	var logActions []string
	if anp.Annotations[util.ACLActionsLogAnnotation] != "" {
		logActions = strings.Split(anp.Annotations[util.ACLActionsLogAnnotation], ",")
	}

	// ovn portGroup/addressSet doesn't support name with '-', so we replace '-' by '.'.
	// This may cause conflict if two anp with name test-anp and test.anp, maybe hash is a better solution, but we do not want to lost the readability now.
	// Make sure all create operations are reentrant.
	pgName := strings.ReplaceAll(anpName, "-", ".")
	if err = c.OVNNbClient.CreatePortGroup(pgName, map[string]string{adminNetworkPolicyKey: anpName}); err != nil {
		klog.Errorf("failed to create port group for anp %s: %v", key, err)
		return err
	}

	ports, err := c.fetchSelectedPods(&anp.Spec.Subject)
	if err != nil {
		klog.Errorf("failed to fetch ports belongs to anp %s: %v", key, err)
		return err
	}

	if err = c.OVNNbClient.PortGroupSetPorts(pgName, ports); err != nil {
		klog.Errorf("failed to set ports %v to port group %s: %v", ports, pgName, err)
		return err
	}

	ingressACLOps, err := c.OVNNbClient.DeleteAclsOps(pgName, portGroupKey, "to-lport", nil)
	if err != nil {
		klog.Errorf("failed to generate clear operations for anp %s ingress acls: %v", key, err)
		return err
	}

	curIngressAddrSet, curEgressAddrSet, err := c.getCurrentAddrSetByName(anpName, false)
	if err != nil {
		klog.Errorf("failed to list address sets for anp %s: %v", key, err)
		return err
	}
	desiredIngressAddrSet := strset.NewWithSize(len(anp.Spec.Ingress) * 2)
	desiredEgressAddrSet := strset.NewWithSize(len(anp.Spec.Egress) * 2)

	// create ingress acl
	for index, anpr := range anp.Spec.Ingress {
		// A single address set must contain addresses of the same type and the name must be unique within table, so IPv4 and IPv6 address set should be different
		ingressAsV4Name, ingressAsV6Name := getAnpAddressSetName(pgName, anpr.Name, index, true)
		desiredIngressAddrSet.Add(ingressAsV4Name, ingressAsV6Name)

		var v4Addrs, v4Addr, v6Addrs, v6Addr []string
		// This field must be defined and contain at least one item.
		for _, anprpeer := range anpr.From {
			if v4Addr, v6Addr, err = c.fetchIngressSelectedAddresses(&anprpeer); err != nil {
				klog.Errorf("failed to fetch admin network policy selected addresses, %v", err)
				return err
			}
			v4Addrs = append(v4Addrs, v4Addr...)
			v6Addrs = append(v6Addrs, v6Addr...)
		}
		klog.Infof("anp %s, ingress rule %s, selected v4 address %v, v6 address %v", anpName, anpr.Name, v4Addrs, v6Addrs)

		if err = c.createAsForAnpRule(anpName, anpr.Name, "ingress", ingressAsV4Name, v4Addrs, false); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.createAsForAnpRule(anpName, anpr.Name, "ingress", ingressAsV6Name, v6Addrs, false); err != nil {
			klog.Error(err)
			return err
		}

		aclPriority := util.AnpACLMaxPriority - int(anp.Spec.Priority*100) - index
		aclAction := anpACLAction(anpr.Action)
		rulePorts := []v1alpha1.AdminNetworkPolicyPort{}
		if anpr.Ports != nil {
			rulePorts = *anpr.Ports
		}

		if len(v4Addrs) != 0 {
			aclName := fmt.Sprintf("anp/%s/ingress/%s/%d", anpName, kubeovnv1.ProtocolIPv4, index)
			ops, err := c.OVNNbClient.UpdateAnpRuleACLOps(pgName, ingressAsV4Name, kubeovnv1.ProtocolIPv4, aclName, aclPriority, aclAction, logActions, rulePorts, true, false)
			if err != nil {
				klog.Errorf("failed to add v4 ingress acls for anp %s: %v", key, err)
				return err
			}
			ingressACLOps = append(ingressACLOps, ops...)
		}

		if len(v6Addrs) != 0 {
			aclName := fmt.Sprintf("anp/%s/ingress/%s/%d", anpName, kubeovnv1.ProtocolIPv6, index)
			ops, err := c.OVNNbClient.UpdateAnpRuleACLOps(pgName, ingressAsV6Name, kubeovnv1.ProtocolIPv6, aclName, aclPriority, aclAction, logActions, rulePorts, true, false)
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
		return fmt.Errorf("failed to delete unused ingress address set for anp %s: %w", key, err)
	}

	egressACLOps, err := c.OVNNbClient.DeleteAclsOps(pgName, portGroupKey, "from-lport", nil)
	if err != nil {
		klog.Errorf("failed to generate clear operations for anp %s egress acls: %v", key, err)
		return err
	}

	// Reconcile DNSNameResolvers for all collected domain names
	if c.config.EnableDNSNameResolver {
		// Collect all domain names from egress rules
		var allDomainNames []string
		for _, anpr := range anp.Spec.Egress {
			for _, anprpeer := range anpr.To {
				if len(anprpeer.DomainNames) != 0 {
					for _, domainName := range anprpeer.DomainNames {
						allDomainNames = append(allDomainNames, string(domainName))
					}
				}
			}
		}

		if err := c.reconcileDNSNameResolversForANP(anpName, allDomainNames); err != nil {
			klog.Errorf("failed to reconcile DNSNameResolvers for ANP %s: %v", anpName, err)
			return err
		}
	}

	// create egress acl
	for index, anpr := range anp.Spec.Egress {
		// A single address set must contain addresses of the same type and the name must be unique within table, so IPv4 and IPv6 address set should be different
		egressAsV4Name, egressAsV6Name := getAnpAddressSetName(pgName, anpr.Name, index, false)
		desiredEgressAddrSet.Add(egressAsV4Name, egressAsV6Name)

		var v4Addrs, v4Addr, v6Addrs, v6Addr []string
		hasDomainNames := false
		// This field must be defined and contain at least one item.
		for _, anprpeer := range anpr.To {
			if v4Addr, v6Addr, err = c.fetchEgressSelectedAddresses(&anprpeer); err != nil {
				klog.Errorf("failed to fetch admin network policy selected addresses, %v", err)
				return err
			}
			v4Addrs = append(v4Addrs, v4Addr...)
			v6Addrs = append(v6Addrs, v6Addr...)

			// Check if this peer has domain names
			hasDomainNames = len(anprpeer.DomainNames) > 0
		}
		klog.Infof("anp %s, egress rule %s, selected v4 address %v, v6 address %v", anpName, anpr.Name, v4Addrs, v6Addrs)

		if err = c.createAsForAnpRule(anpName, anpr.Name, "egress", egressAsV4Name, v4Addrs, false); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.createAsForAnpRule(anpName, anpr.Name, "egress", egressAsV6Name, v6Addrs, false); err != nil {
			klog.Error(err)
			return err
		}

		aclPriority := util.AnpACLMaxPriority - int(anp.Spec.Priority*100) - index
		aclAction := anpACLAction(anpr.Action)
		rulePorts := []v1alpha1.AdminNetworkPolicyPort{}
		if anpr.Ports != nil {
			rulePorts = *anpr.Ports
		}

		// Create ACL rules if we have IP addresses OR domain names
		// Domain names may not be resolved initially but will be updated later
		if len(v4Addrs) != 0 || hasDomainNames {
			aclName := fmt.Sprintf("anp/%s/egress/%s/%d", anpName, kubeovnv1.ProtocolIPv4, index)
			ops, err := c.OVNNbClient.UpdateAnpRuleACLOps(pgName, egressAsV4Name, kubeovnv1.ProtocolIPv4, aclName, aclPriority, aclAction, logActions, rulePorts, false, false)
			if err != nil {
				klog.Errorf("failed to add v4 egress acls for anp %s: %v", key, err)
				return err
			}
			egressACLOps = append(egressACLOps, ops...)
		}

		if len(v6Addrs) != 0 || hasDomainNames {
			aclName := fmt.Sprintf("anp/%s/egress/%s/%d", anpName, kubeovnv1.ProtocolIPv6, index)
			ops, err := c.OVNNbClient.UpdateAnpRuleACLOps(pgName, egressAsV6Name, kubeovnv1.ProtocolIPv6, aclName, aclPriority, aclAction, logActions, rulePorts, false, false)
			if err != nil {
				klog.Errorf("failed to add v6 egress acls for anp %s: %v", key, err)
				return err
			}
			egressACLOps = append(egressACLOps, ops...)
		}
	}

	if err := c.OVNNbClient.Transact("add-egress-acls", egressACLOps); err != nil {
		return fmt.Errorf("failed to add egress acls for anp %s: %w", key, err)
	}
	if err := c.deleteUnusedAddrSetForAnp(curEgressAddrSet, desiredEgressAddrSet); err != nil {
		return fmt.Errorf("failed to delete unused egress address set for anp %s: %w", key, err)
	}

	return nil
}

func (c *Controller) handleDeleteAnp(anp *v1alpha1.AdminNetworkPolicy) error {
	c.anpKeyMutex.LockKey(anp.Name)
	defer func() { _ = c.anpKeyMutex.UnlockKey(anp.Name) }()

	klog.Infof("handle delete admin network policy %s", anp.Name)
	delete(c.anpPrioNameMap, anp.Spec.Priority)
	delete(c.anpNamePrioMap, anp.Name)

	anpName := getAnpName(anp.Name)

	// ACLs releated to port_group will be deleted automatically when port_group is deleted
	pgName := strings.ReplaceAll(anpName, "-", ".")
	if err := c.OVNNbClient.DeletePortGroup(pgName); err != nil {
		klog.Errorf("failed to delete port group for anp %s: %v", anpName, err)
	}

	if err := c.OVNNbClient.DeleteAddressSets(map[string]string{
		adminNetworkPolicyKey: fmt.Sprintf("%s/%s", anpName, "ingress"),
	}); err != nil {
		klog.Errorf("failed to delete ingress address set for anp %s: %v", anpName, err)
		return err
	}

	if err := c.OVNNbClient.DeleteAddressSets(map[string]string{
		adminNetworkPolicyKey: fmt.Sprintf("%s/%s", anpName, "egress"),
	}); err != nil {
		klog.Errorf("failed to delete egress address set for anp %s: %v", anpName, err)
		return err
	}

	// Delete all DNSNameResolver CRs associated with this ANP
	if c.config.EnableDNSNameResolver {
		if err := c.reconcileDNSNameResolversForANP(anpName, []string{}); err != nil {
			klog.Errorf("failed to delete DNSNameResolver CRs for anp %s: %v", anpName, err)
			return err
		}
	}

	return nil
}

func (c *Controller) handleUpdateAnp(changed *AdminNetworkPolicyChangedDelta) error {
	// Only handle updates that do not affect acls.
	c.anpKeyMutex.LockKey(changed.key)
	defer func() { _ = c.anpKeyMutex.UnlockKey(changed.key) }()

	klog.Infof("handleUpdateAnp: processing ANP %s, field=%s, DNSReconcileDone=%v",
		changed.key, changed.field, changed.DNSReconcileDone)

	cachedAnp, err := c.anpsLister.Get(changed.key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	desiredAnp := cachedAnp.DeepCopy()
	klog.Infof("handle update admin network policy %s", desiredAnp.Name)

	anpName := getAnpName(desiredAnp.Name)
	pgName := strings.ReplaceAll(anpName, "-", ".")

	// The port-group for anp should be updated
	if changed.field == ChangedSubject {
		// The port-group must exist when update anp, this check should never be matched.
		if ok, err := c.OVNNbClient.PortGroupExists(pgName); !ok || err != nil {
			klog.Errorf("port-group for anp %s does not exist when update anp", desiredAnp.Name)
			return err
		}

		ports, err := c.fetchSelectedPods(&desiredAnp.Spec.Subject)
		if err != nil {
			klog.Errorf("failed to fetch ports belongs to anp %s: %v", desiredAnp.Name, err)
			return err
		}

		if err = c.OVNNbClient.PortGroupSetPorts(pgName, ports); err != nil {
			klog.Errorf("failed to set ports %v to port group %s: %v", ports, pgName, err)
			return err
		}
	}

	// Peer selector in ingress/egress rule has changed, so the corresponding address-set need be updated
	if changed.field == ChangedIngressRule {
		for index, rule := range desiredAnp.Spec.Ingress {
			// Make sure the rule is changed and go on update
			if rule.Name == changed.ruleNames[index].curRuleName || changed.ruleNames[index].isMatch {
				if err := c.setAddrSetForAnpRule(anpName, pgName, rule.Name, index, rule.From, []v1alpha1.AdminNetworkPolicyEgressPeer{}, true, false); err != nil {
					klog.Errorf("failed to set ingress address-set for anp rule %s/%s, %v", anpName, rule.Name, err)
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
		for index, rule := range desiredAnp.Spec.Egress {
			// Check if we need to update address sets (rule changed or DNS reconciliation needed)
			needAddrSetUpdate := rule.Name == changed.ruleNames[index].curRuleName || changed.ruleNames[index].isMatch || changed.DNSReconcileDone

			// Check if we need to reconcile DNS resolvers (DNS feature enabled and not already done)
			needDNSReconcile := c.config.EnableDNSNameResolver && !changed.DNSReconcileDone

			if needAddrSetUpdate {
				if err := c.setAddrSetForAnpRule(anpName, pgName, rule.Name, index, []v1alpha1.AdminNetworkPolicyIngressPeer{}, rule.To, false, false); err != nil {
					klog.Errorf("failed to set egress address-set for anp rule %s/%s, %v", anpName, rule.Name, err)
					return err
				}

				if needDNSReconcile {
					var currentDomainNames []string
					for _, peer := range rule.To {
						for _, domainName := range peer.DomainNames {
							currentDomainNames = append(currentDomainNames, string(domainName))
						}
					}

					if err := c.reconcileDNSNameResolversForANP(anpName, currentDomainNames); err != nil {
						klog.Errorf("failed to reconcile DNSNameResolvers for egress rule %s/%s, %v", anpName, rule.Name, err)
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

func (c *Controller) validateAnpConfig(anp *v1alpha1.AdminNetworkPolicy) error {
	// The behavior is undefined if two ANP objects have same priority.
	if anpName, exist := c.anpPrioNameMap[anp.Spec.Priority]; exist && anpName != anp.Name {
		err := fmt.Errorf("can not create anp with same priority %d, exist one is %s, new created is %s", anp.Spec.Priority, anpName, anp.Name)
		klog.Error(err)
		return err
	}

	// We have noticed redhat's discussion about ACL priority in https://bugzilla.redhat.com/show_bug.cgi?id=2175752
	// After discussion, we decided to use the same range of priorities(20000-30000). Pay tribute to the developers of redhat.
	if anp.Spec.Priority > util.AnpMaxPriority {
		err := fmt.Errorf("the priority of anp %s is greater than max value %d", anp.Name, util.AnpMaxPriority)
		klog.Error(err)
		return err
	}

	if len(anp.Spec.Ingress) > util.AnpMaxRules || len(anp.Spec.Egress) > util.AnpMaxRules {
		err := fmt.Errorf("at most %d rules can be create in anp ingress/egress, ingress rules num %d and egress rules num %d in anp %s", util.AnpMaxRules, len(anp.Spec.Ingress), len(anp.Spec.Egress), anp.Name)
		klog.Error(err)
		return err
	}

	return nil
}

func (c *Controller) fetchSelectedPods(anpSubject *v1alpha1.AdminNetworkPolicySubject) ([]string, error) {
	var ports []string

	// Exactly one field must be set.
	if anpSubject.Namespaces != nil {
		nsSelector, err := metav1.LabelSelectorAsSelector(anpSubject.Namespaces)
		if err != nil {
			return nil, fmt.Errorf("error creating ns label selector, %w", err)
		}

		ports, _, _, err = c.fetchPods(nsSelector, labels.Everything())
		if err != nil {
			return nil, fmt.Errorf("failed to fetch pods, %w", err)
		}
	} else if anpSubject.Pods != nil {
		nsSelector, err := metav1.LabelSelectorAsSelector(&anpSubject.Pods.NamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("error creating ns label selector, %w", err)
		}
		podSelector, err := metav1.LabelSelectorAsSelector(&anpSubject.Pods.PodSelector)
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

func (c *Controller) fetchPods(nsSelector, podSelector labels.Selector) ([]string, []string, []string, error) {
	ports := make([]string, 0, util.AnpMaxRules)
	v4Addresses := make([]string, 0, util.AnpMaxRules)
	v6Addresses := make([]string, 0, util.AnpMaxRules)

	namespaces, err := c.namespacesLister.List(nsSelector)
	if err != nil {
		klog.Errorf("failed to list namespaces: %v", err)
		return nil, nil, nil, err
	}

	klog.V(3).Infof("fetch pod ports/addresses, namespace selector is %s, pod selector is %s", nsSelector.String(), podSelector.String())
	for _, namespace := range namespaces {
		pods, err := c.podsLister.Pods(namespace.Name).List(podSelector)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to list pods, %w", err)
		}

		for _, pod := range pods {
			if pod.Spec.HostNetwork {
				continue
			}
			podName := c.getNameByPod(pod)

			podNets, err := c.getPodKubeovnNets(pod)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to get pod networks, %w", err)
			}

			for _, podNet := range podNets {
				if !isOvnSubnet(podNet.Subnet) {
					continue
				}

				if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "true" {
					ports = append(ports, ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName))

					podIPAnnotation := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)]
					podIPs := strings.SplitSeq(podIPAnnotation, ",")
					for podIP := range podIPs {
						switch util.CheckProtocol(podIP) {
						case kubeovnv1.ProtocolIPv4:
							v4Addresses = append(v4Addresses, podIP)
						case kubeovnv1.ProtocolIPv6:
							v6Addresses = append(v6Addresses, podIP)
						}
					}
				}
			}
		}
	}

	return ports, v4Addresses, v6Addresses, nil
}

func (c *Controller) fetchIngressSelectedAddresses(ingressPeer *v1alpha1.AdminNetworkPolicyIngressPeer) ([]string, []string, error) {
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

func (c *Controller) fetchEgressSelectedAddresses(egressPeer *v1alpha1.AdminNetworkPolicyEgressPeer) ([]string, []string, error) {
	return c.fetchEgressSelectedAddressesCommon(egressPeer.Namespaces, egressPeer.Pods, egressPeer.Nodes, egressPeer.Networks, egressPeer.DomainNames)
}

func (c *Controller) fetchBaselineEgressSelectedAddresses(egressPeer *v1alpha1.BaselineAdminNetworkPolicyEgressPeer) ([]string, []string, error) {
	return c.fetchEgressSelectedAddressesCommon(egressPeer.Namespaces, egressPeer.Pods, egressPeer.Nodes, egressPeer.Networks, nil)
}

func (c *Controller) fetchEgressSelectedAddressesCommon(namespaces *metav1.LabelSelector, pods *v1alpha1.NamespacedPod, nodes *metav1.LabelSelector, networks []v1alpha1.CIDR, domainNames []v1alpha1.DomainName) ([]string, []string, error) {
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
		v4Addresses, v6Addresses = fetchCIDRAddrs(networks)
	case len(domainNames) != 0:
		// DomainNames field is present - resolve addresses from DNSNameResolver
		if !c.config.EnableDNSNameResolver {
			return nil, nil, fmt.Errorf("DNSNameResolver is disabled but domain names are specified: %v", domainNames)
		}
		klog.Infof("DomainNames detected in egress peer: %v", domainNames)
		var err error
		v4Addresses, v6Addresses, err = c.resolveDomainNames(domainNames)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to resolve domain names: %w", err)
		}
	default:
		return nil, nil, errors.New("at least one egressPeer must be specified")
	}

	return v4Addresses, v6Addresses, nil
}

// resolveDomainNames resolves domain names to IP addresses using DNSNameResolver lister
func (c *Controller) resolveDomainNames(domainNames []v1alpha1.DomainName) ([]string, []string, error) {
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

func (c *Controller) createAsForAnpRule(anpName, ruleName, direction, asName string, addresses []string, isBanp bool) error {
	var err error
	if isBanp {
		err = c.OVNNbClient.CreateAddressSet(asName, map[string]string{
			baselineAdminNetworkPolicyKey: fmt.Sprintf("%s/%s", anpName, direction),
		})
	} else {
		err = c.OVNNbClient.CreateAddressSet(asName, map[string]string{
			adminNetworkPolicyKey: fmt.Sprintf("%s/%s", anpName, direction),
		})
	}
	if err != nil {
		klog.Errorf("failed to create ovn address set %s for anp/banp rule %s/%s: %v", asName, anpName, ruleName, err)
		return err
	}

	if err := c.OVNNbClient.AddressSetUpdateAddress(asName, addresses...); err != nil {
		klog.Errorf("failed to set addresses %q to address set %s: %v", strings.Join(addresses, ","), asName, err)
		return err
	}

	return nil
}

func (c *Controller) getCurrentAddrSetByName(anpName string, isBanp bool) (*strset.Set, *strset.Set, error) {
	curIngressAddrSet := strset.New()
	curEgressAddrSet := strset.New()
	var ass []ovnnb.AddressSet
	var err error

	// anp and banp can use same name, so depends on the external_ids key field to distinguish
	if isBanp {
		ass, err = c.OVNNbClient.ListAddressSets(map[string]string{
			baselineAdminNetworkPolicyKey: fmt.Sprintf("%s/%s", anpName, "ingress"),
		})
	} else {
		ass, err = c.OVNNbClient.ListAddressSets(map[string]string{
			adminNetworkPolicyKey: fmt.Sprintf("%s/%s", anpName, "ingress"),
		})
	}
	if err != nil {
		klog.Errorf("failed to list ingress address sets for anp/banp %s: %v", anpName, err)
		return curIngressAddrSet, curEgressAddrSet, err
	}
	for _, as := range ass {
		curIngressAddrSet.Add(as.Name)
	}

	if isBanp {
		ass, err = c.OVNNbClient.ListAddressSets(map[string]string{
			baselineAdminNetworkPolicyKey: fmt.Sprintf("%s/%s", anpName, "egress"),
		})
	} else {
		ass, err = c.OVNNbClient.ListAddressSets(map[string]string{
			adminNetworkPolicyKey: fmt.Sprintf("%s/%s", anpName, "egress"),
		})
	}
	if err != nil {
		klog.Errorf("failed to list egress address sets for anp/banp %s: %v", anpName, err)
		return curIngressAddrSet, curEgressAddrSet, err
	}
	for _, as := range ass {
		curEgressAddrSet.Add(as.Name)
	}

	return curIngressAddrSet, curEgressAddrSet, nil
}

func (c *Controller) deleteUnusedAddrSetForAnp(curAddrSet, desiredAddrSet *strset.Set) error {
	toDel := strset.Difference(curAddrSet, desiredAddrSet).List()

	for _, asName := range toDel {
		if err := c.OVNNbClient.DeleteAddressSet(asName); err != nil {
			klog.Errorf("failed to delete address set %s, %v", asName, err)
			return err
		}
	}

	return nil
}

func (c *Controller) setAddrSetForAnpRule(anpName, pgName, ruleName string, index int, from []v1alpha1.AdminNetworkPolicyIngressPeer, to []v1alpha1.AdminNetworkPolicyEgressPeer, isIngress, isBanp bool) error {
	return c.setAddrSetForAnpRuleCommon(anpName, pgName, ruleName, index, from, to, nil, isIngress, isBanp)
}

func (c *Controller) setAddrSetForBaselineAnpRule(anpName, pgName, ruleName string, index int, from []v1alpha1.AdminNetworkPolicyIngressPeer, to []v1alpha1.BaselineAdminNetworkPolicyEgressPeer, isIngress, isBanp bool) error {
	return c.setAddrSetForAnpRuleCommon(anpName, pgName, ruleName, index, from, nil, to, isIngress, isBanp)
}

func (c *Controller) setAddrSetForAnpRuleCommon(anpName, pgName, ruleName string, index int, from []v1alpha1.AdminNetworkPolicyIngressPeer, to []v1alpha1.AdminNetworkPolicyEgressPeer, baselineTo []v1alpha1.BaselineAdminNetworkPolicyEgressPeer, isIngress, isBanp bool) error {
	// A single address set must contain addresses of the same type and the name must be unique within table, so IPv4 and IPv6 address set should be different

	var v4Addrs, v4Addr, v6Addrs, v6Addr []string
	var err error
	if isIngress {
		for _, anprpeer := range from {
			if v4Addr, v6Addr, err = c.fetchIngressSelectedAddresses(&anprpeer); err != nil {
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
				if v4Addr, v6Addr, err = c.fetchEgressSelectedAddresses(&anprpeer); err != nil {
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

func (c *Controller) updateAnpsByLabelsMatch(nsLabels, podLabels map[string]string) {
	anps, _ := c.anpsLister.List(labels.Everything())
	for _, anp := range anps {
		changed := &AdminNetworkPolicyChangedDelta{
			key: anp.Name,
		}

		if isLabelsMatch(anp.Spec.Subject.Namespaces, anp.Spec.Subject.Pods, nsLabels, podLabels) {
			klog.Infof("anp %s, labels matched for anp's subject, nsLabels %s, podLabels %s", anp.Name, labels.Set(nsLabels).String(), labels.Set(podLabels).String())
			changed.field = ChangedSubject
			c.updateAnpQueue.Add(changed)
		}

		ingressRuleNames, egressRuleNames := isLabelsMatchAnpRulePeers(anp.Spec.Ingress, anp.Spec.Egress, nsLabels, podLabels)
		if !isRulesArrayEmpty(ingressRuleNames) {
			klog.Infof("anp %s, labels matched for anp's ingress peer, nsLabels %s, podLabels %s", anp.Name, labels.Set(nsLabels).String(), labels.Set(podLabels).String())
			changed.ruleNames = ingressRuleNames
			changed.field = ChangedIngressRule
			c.updateAnpQueue.Add(changed)
		}

		if !isRulesArrayEmpty(egressRuleNames) {
			klog.Infof("anp %s, labels matched for anp's egress peer, nsLabels %s, podLabels %s", anp.Name, labels.Set(nsLabels).String(), labels.Set(podLabels).String())
			changed.ruleNames = egressRuleNames
			changed.field = ChangedEgressRule
			c.updateAnpQueue.Add(changed)
		}
	}

	banps, _ := c.banpsLister.List(labels.Everything())
	for _, banp := range banps {
		changed := &AdminNetworkPolicyChangedDelta{
			key: banp.Name,
		}

		if isLabelsMatch(banp.Spec.Subject.Namespaces, banp.Spec.Subject.Pods, nsLabels, podLabels) {
			klog.Infof("banp %s, labels matched for banp's subject, nsLabels %s, podLabels %s", banp.Name, labels.Set(nsLabels).String(), labels.Set(podLabels).String())
			changed.field = ChangedSubject
			c.updateBanpQueue.Add(changed)
		}

		ingressRuleNames, egressRuleNames := isLabelsMatchBanpRulePeers(banp.Spec.Ingress, banp.Spec.Egress, nsLabels, podLabels)
		if !isRulesArrayEmpty(ingressRuleNames) {
			klog.Infof("banp %s, labels matched for banp's ingress peer, nsLabels %s, podLabels %s", banp.Name, labels.Set(nsLabels).String(), labels.Set(podLabels).String())
			changed.ruleNames = ingressRuleNames
			changed.field = ChangedIngressRule
			c.updateBanpQueue.Add(changed)
		}

		if !isRulesArrayEmpty(egressRuleNames) {
			klog.Infof("banp %s, labels matched for banp's egress peer, nsLabels %s, podLabels %s", banp.Name, labels.Set(nsLabels).String(), labels.Set(podLabels).String())
			changed.ruleNames = egressRuleNames
			changed.field = ChangedEgressRule
			c.updateBanpQueue.Add(changed)
		}
	}
}

func isLabelsMatch(namespaces *metav1.LabelSelector, pods *v1alpha1.NamespacedPod, nsLabels, podLabels map[string]string) bool {
	// Exactly one field of namespaces/pods must be set.
	if namespaces != nil {
		nsSelector, _ := metav1.LabelSelectorAsSelector(namespaces)
		klog.V(3).Infof("namespaces is not nil, nsSelector %s", nsSelector.String())
		if nsSelector.Matches(labels.Set(nsLabels)) {
			return true
		}
	} else if pods != nil {
		nsSelector, _ := metav1.LabelSelectorAsSelector(&pods.NamespaceSelector)
		podSelector, _ := metav1.LabelSelectorAsSelector(&pods.PodSelector)
		klog.V(3).Infof("pods is not nil, nsSelector %s, podSelector %s", nsSelector.String(), podSelector.String())
		if nsSelector.Matches(labels.Set(nsLabels)) && podSelector.Matches(labels.Set(podLabels)) {
			return true
		}
	}

	return false
}

func isLabelsMatchRulePeers(from []v1alpha1.AdminNetworkPolicyIngressPeer, to []v1alpha1.AdminNetworkPolicyEgressPeer, nsLabels, podLabels map[string]string) bool {
	return isLabelsMatchRulePeersCommon(from, to, nil, nsLabels, podLabels)
}

func isLabelsMatchBaselineRulePeers(from []v1alpha1.AdminNetworkPolicyIngressPeer, to []v1alpha1.BaselineAdminNetworkPolicyEgressPeer, nsLabels, podLabels map[string]string) bool {
	return isLabelsMatchRulePeersCommon(from, nil, to, nsLabels, podLabels)
}

func isLabelsMatchRulePeersCommon(from []v1alpha1.AdminNetworkPolicyIngressPeer, to []v1alpha1.AdminNetworkPolicyEgressPeer, baselineTo []v1alpha1.BaselineAdminNetworkPolicyEgressPeer, nsLabels, podLabels map[string]string) bool {
	for _, ingressPeer := range from {
		if isLabelsMatch(ingressPeer.Namespaces, ingressPeer.Pods, nsLabels, podLabels) {
			return true
		}
	}

	if to != nil {
		for _, egressPeer := range to {
			if isLabelsMatch(egressPeer.Namespaces, egressPeer.Pods, nsLabels, podLabels) {
				return true
			}
		}
	} else {
		for _, egressPeer := range baselineTo {
			if isLabelsMatch(egressPeer.Namespaces, egressPeer.Pods, nsLabels, podLabels) {
				return true
			}
		}
	}

	return false
}

func isLabelsMatchAnpRulePeers(ingress []v1alpha1.AdminNetworkPolicyIngressRule, egress []v1alpha1.AdminNetworkPolicyEgressRule, nsLabels, podLabels map[string]string) ([util.AnpMaxRules]ChangedName, [util.AnpMaxRules]ChangedName) {
	return isLabelsMatchAnpRulePeersCommon(ingress, egress, nil, nsLabels, podLabels)
}

func isLabelsMatchBaselineAnpRulePeers(_ []v1alpha1.BaselineAdminNetworkPolicyIngressRule, egress []v1alpha1.BaselineAdminNetworkPolicyEgressRule, nsLabels, podLabels map[string]string) ([util.AnpMaxRules]ChangedName, [util.AnpMaxRules]ChangedName) {
	return isLabelsMatchAnpRulePeersCommon(nil, nil, egress, nsLabels, podLabels)
}

func isLabelsMatchAnpRulePeersCommon(ingress []v1alpha1.AdminNetworkPolicyIngressRule, egress []v1alpha1.AdminNetworkPolicyEgressRule, baselineEgress []v1alpha1.BaselineAdminNetworkPolicyEgressRule, nsLabels, podLabels map[string]string) ([util.AnpMaxRules]ChangedName, [util.AnpMaxRules]ChangedName) {
	var changedIngressRuleNames, changedEgressRuleNames [util.AnpMaxRules]ChangedName

	for index, anpr := range ingress {
		if isLabelsMatchRulePeers(anpr.From, []v1alpha1.AdminNetworkPolicyEgressPeer{}, nsLabels, podLabels) {
			changedIngressRuleNames[index].isMatch = true
			changedIngressRuleNames[index].curRuleName = anpr.Name
		}
	}

	if egress != nil {
		for index, anpr := range egress {
			if isLabelsMatchRulePeers([]v1alpha1.AdminNetworkPolicyIngressPeer{}, anpr.To, nsLabels, podLabels) {
				changedEgressRuleNames[index].isMatch = true
				changedEgressRuleNames[index].curRuleName = anpr.Name
			}
		}
	} else {
		for index, banpr := range baselineEgress {
			if isLabelsMatchBaselineRulePeers([]v1alpha1.AdminNetworkPolicyIngressPeer{}, banpr.To, nsLabels, podLabels) {
				changedEgressRuleNames[index].isMatch = true
				changedEgressRuleNames[index].curRuleName = banpr.Name
			}
		}
	}

	return changedIngressRuleNames, changedEgressRuleNames
}

func isLabelsMatchBanpRulePeers(ingress []v1alpha1.BaselineAdminNetworkPolicyIngressRule, egress []v1alpha1.BaselineAdminNetworkPolicyEgressRule, nsLabels, podLabels map[string]string) ([util.AnpMaxRules]ChangedName, [util.AnpMaxRules]ChangedName) {
	return isLabelsMatchBaselineAnpRulePeers(ingress, egress, nsLabels, podLabels)
}

func getAnpName(name string) string {
	anpName := name
	nameArray := []rune(name)
	if !unicode.IsLetter(nameArray[0]) {
		anpName = "anp" + name
	}
	return anpName
}

func getAnpAddressSetName(pgName, ruleName string, index int, isIngress bool) (string, string) {
	var asV4Name, asV6Name string
	if isIngress {
		// In case ruleName is omitted, add direction and index to distinguish address-set
		asV4Name = strings.ReplaceAll(fmt.Sprintf("%s.ingress.%d.%s.%s", pgName, index, ruleName, kubeovnv1.ProtocolIPv4), "-", ".")
		asV6Name = strings.ReplaceAll(fmt.Sprintf("%s.ingress.%d.%s.%s", pgName, index, ruleName, kubeovnv1.ProtocolIPv6), "-", ".")
	} else {
		asV4Name = strings.ReplaceAll(fmt.Sprintf("%s.egress.%d.%s.%s", pgName, index, ruleName, kubeovnv1.ProtocolIPv4), "-", ".")
		asV6Name = strings.ReplaceAll(fmt.Sprintf("%s.egress.%d.%s.%s", pgName, index, ruleName, kubeovnv1.ProtocolIPv6), "-", ".")
	}

	return asV4Name, asV6Name
}

func anpACLAction(action v1alpha1.AdminNetworkPolicyRuleAction) ovnnb.ACLAction {
	switch action {
	case v1alpha1.AdminNetworkPolicyRuleActionAllow:
		return ovnnb.ACLActionAllowRelated
	case v1alpha1.AdminNetworkPolicyRuleActionDeny:
		return ovnnb.ACLActionDrop
	case v1alpha1.AdminNetworkPolicyRuleActionPass:
		return ovnnb.ACLActionPass
	}
	return ovnnb.ACLActionDrop
}

func isRulesArrayEmpty(ruleNames [util.AnpMaxRules]ChangedName) bool {
	for _, ruleName := range ruleNames {
		// The ruleName can be omitted default
		if ruleName.curRuleName != "" || ruleName.isMatch {
			return false
		}
	}
	return true
}

func (c *Controller) fetchNodesAddrs(nodeSelector labels.Selector) ([]string, []string, error) {
	nodes, err := c.nodesLister.List(nodeSelector)
	if err != nil {
		klog.Errorf("failed to list nodes: %v", err)
		return nil, nil, err
	}
	v4Addresses := make([]string, 0, len(nodes))
	v6Addresses := make([]string, 0, len(nodes))

	klog.V(3).Infof("fetch nodes addresses, selector is %s", nodeSelector.String())
	for _, node := range nodes {
		nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*node)
		if nodeIPv4 != "" {
			v4Addresses = append(v4Addresses, nodeIPv4)
		}
		if nodeIPv6 != "" {
			v6Addresses = append(v6Addresses, nodeIPv6)
		}
	}

	return v4Addresses, v6Addresses, nil
}

func fetchCIDRAddrs(networks []v1alpha1.CIDR) ([]string, []string) {
	var v4Addresses, v6Addresses []string

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
