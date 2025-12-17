package controller

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/scylladb/go-set/strset"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddBanp(obj any) {
	key := cache.MetaObjectToName(obj.(*v1alpha1.BaselineAdminNetworkPolicy)).String()
	klog.V(3).Infof("enqueue add banp %s", key)
	c.addBanpQueue.Add(key)
}

func (c *Controller) enqueueDeleteBanp(obj any) {
	var bnp *v1alpha1.BaselineAdminNetworkPolicy
	switch t := obj.(type) {
	case *v1alpha1.BaselineAdminNetworkPolicy:
		bnp = t
	case cache.DeletedFinalStateUnknown:
		b, ok := t.Obj.(*v1alpha1.BaselineAdminNetworkPolicy)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		bnp = b
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	klog.V(3).Infof("enqueue delete bnp %s", cache.MetaObjectToName(bnp).String())
	c.deleteBanpQueue.Add(bnp.DeepCopy())
}

func (c *Controller) enqueueUpdateBanp(oldObj, newObj any) {
	oldBanp := oldObj.(*v1alpha1.BaselineAdminNetworkPolicy)
	newBanp := newObj.(*v1alpha1.BaselineAdminNetworkPolicy)

	// All the acls should be recreated with the following situations
	if len(oldBanp.Spec.Ingress) != len(newBanp.Spec.Ingress) || len(oldBanp.Spec.Egress) != len(newBanp.Spec.Egress) {
		c.addBanpQueue.Add(newBanp.Name)
		return
	}

	// Acls should be updated when action or ports of ingress/egress rule has been changed
	for index, rule := range newBanp.Spec.Ingress {
		oldRule := oldBanp.Spec.Ingress[index]
		if oldRule.Action != rule.Action || !reflect.DeepEqual(oldRule.Ports, rule.Ports) {
			c.addBanpQueue.Add(newBanp.Name)
			return
		}
	}

	for index, rule := range newBanp.Spec.Egress {
		oldRule := oldBanp.Spec.Egress[index]
		if oldRule.Action != rule.Action || !reflect.DeepEqual(oldRule.Ports, rule.Ports) {
			c.addBanpQueue.Add(newBanp.Name)
			return
		}
	}

	if oldBanp.Annotations[util.ACLActionsLogAnnotation] != newBanp.Annotations[util.ACLActionsLogAnnotation] {
		c.addBanpQueue.Add(newBanp.Name)
		return
	}
	klog.V(3).Infof("enqueue update banp %s", newBanp.Name)

	// The remaining changes do not affect the acls. The port-group or address-set should be updated.
	// The port-group for anp should be updated
	if !reflect.DeepEqual(oldBanp.Spec.Subject, newBanp.Spec.Subject) {
		c.updateBanpQueue.Add(&AdminNetworkPolicyChangedDelta{key: newBanp.Name, field: ChangedSubject})
	}

	// Rule name or peer selector in ingress/egress rule has changed, the corresponding address-set need be updated
	ruleChanged := false
	var changedIngressRuleNames, changedEgressRuleNames [util.AnpMaxRules]ChangedName

	for index, rule := range newBanp.Spec.Ingress {
		oldRule := oldBanp.Spec.Ingress[index]
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
		c.updateBanpQueue.Add(&AdminNetworkPolicyChangedDelta{key: newBanp.Name, ruleNames: changedIngressRuleNames, field: ChangedIngressRule})
	}

	ruleChanged = false
	for index, rule := range newBanp.Spec.Egress {
		oldRule := oldBanp.Spec.Egress[index]
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
		c.updateBanpQueue.Add(&AdminNetworkPolicyChangedDelta{key: newBanp.Name, ruleNames: changedEgressRuleNames, field: ChangedEgressRule})
	}
}

func banpACLAction(action v1alpha1.BaselineAdminNetworkPolicyRuleAction) ovnnb.ACLAction {
	switch action {
	case v1alpha1.BaselineAdminNetworkPolicyRuleActionAllow:
		return ovnnb.ACLActionAllowRelated
	case v1alpha1.BaselineAdminNetworkPolicyRuleActionDeny:
		return ovnnb.ACLActionDrop
	}
	return ovnnb.ACLActionDrop
}

func (c *Controller) handleAddBanp(key string) (err error) {
	// Only one banp with default name can be created in cluster, no need to check
	c.banpKeyMutex.LockKey(key)
	defer func() { _ = c.banpKeyMutex.UnlockKey(key) }()

	cachedBanp, err := c.banpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	klog.Infof("handle add banp %s", cachedBanp.Name)
	banp := cachedBanp.DeepCopy()

	banpName := getAnpName(banp.Name)
	var logActions []string
	if banp.Annotations[util.ACLActionsLogAnnotation] != "" {
		logActions = strings.Split(banp.Annotations[util.ACLActionsLogAnnotation], ",")
	}

	// ovn portGroup/addressSet doesn't support name with '-', so we replace '-' by '.'.
	pgName := strings.ReplaceAll(banpName, "-", ".")
	if err = c.OVNNbClient.CreatePortGroup(pgName, map[string]string{baselineAdminNetworkPolicyKey: banpName}); err != nil {
		klog.Errorf("failed to create port group for banp %s: %v", key, err)
		return err
	}

	ports, err := c.fetchSelectedPods(&banp.Spec.Subject)
	if err != nil {
		klog.Errorf("failed to fetch ports belongs to banp %s: %v", key, err)
		return err
	}

	if err = c.OVNNbClient.PortGroupSetPorts(pgName, ports); err != nil {
		klog.Errorf("failed to set ports %v to port group %s: %v", ports, pgName, err)
		return err
	}

	ingressACLOps, err := c.OVNNbClient.DeleteAclsOps(pgName, portGroupKey, "to-lport", nil)
	if err != nil {
		klog.Errorf("failed to generate clear operations for banp %s ingress acls: %v", key, err)
		return err
	}

	curIngressAddrSet, curEgressAddrSet, err := c.getCurrentAddrSetByName(banpName, true)
	if err != nil {
		klog.Errorf("failed to list address sets for banp %s: %v", key, err)
		return err
	}
	desiredIngressAddrSet := strset.NewWithSize(len(banp.Spec.Ingress) * 2)
	desiredEgressAddrSet := strset.NewWithSize(len(banp.Spec.Egress) * 2)

	// create ingress acl
	for index, banpr := range banp.Spec.Ingress {
		// A single address set must contain addresses of the same type and the name must be unique within table, so IPv4 and IPv6 address set should be different
		ingressAsV4Name, ingressAsV6Name := getAnpAddressSetName(pgName, banpr.Name, index, true)
		desiredIngressAddrSet.Add(ingressAsV4Name, ingressAsV6Name)

		var v4Addrs, v4Addr, v6Addrs, v6Addr []string
		// This field must be defined and contain at least one item.
		for _, anprpeer := range banpr.From {
			if v4Addr, v6Addr, err = c.fetchIngressSelectedAddresses(&anprpeer); err != nil {
				klog.Errorf("failed to fetch admin network policy selected addresses, %v", err)
				return err
			}
			v4Addrs = append(v4Addrs, v4Addr...)
			v6Addrs = append(v6Addrs, v6Addr...)
		}
		klog.Infof("Banp Ingress Rule %s, selected v4 address %v, v6 address %v", banpr.Name, v4Addrs, v6Addrs)

		if err = c.createAsForAnpRule(banpName, banpr.Name, "ingress", ingressAsV4Name, v4Addrs, true); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.createAsForAnpRule(banpName, banpr.Name, "ingress", ingressAsV6Name, v6Addrs, true); err != nil {
			klog.Error(err)
			return err
		}

		// use 1700-1800 for banp acl priority
		aclPriority := util.BanpACLMaxPriority - index
		aclAction := banpACLAction(banpr.Action)
		rulePorts := []v1alpha1.AdminNetworkPolicyPort{}
		if banpr.Ports != nil {
			rulePorts = *banpr.Ports
		}

		if len(v4Addrs) != 0 {
			aclName := fmt.Sprintf("banp/%s/ingress/%s/%d", banpName, kubeovnv1.ProtocolIPv4, index)
			ops, err := c.OVNNbClient.UpdateAnpRuleACLOps(pgName, ingressAsV4Name, kubeovnv1.ProtocolIPv4, aclName, aclPriority, aclAction, logActions, rulePorts, true, true)
			if err != nil {
				klog.Errorf("failed to add v4 ingress acls for banp %s: %v", key, err)
				return err
			}
			ingressACLOps = append(ingressACLOps, ops...)
		}

		if len(v6Addrs) != 0 {
			aclName := fmt.Sprintf("banp/%s/ingress/%s/%d", banpName, kubeovnv1.ProtocolIPv6, index)
			ops, err := c.OVNNbClient.UpdateAnpRuleACLOps(pgName, ingressAsV6Name, kubeovnv1.ProtocolIPv6, aclName, aclPriority, aclAction, logActions, rulePorts, true, true)
			if err != nil {
				klog.Errorf("failed to add v6 ingress acls for banp %s: %v", key, err)
				return err
			}
			ingressACLOps = append(ingressACLOps, ops...)
		}
	}

	if err := c.OVNNbClient.Transact("add-ingress-acls", ingressACLOps); err != nil {
		return fmt.Errorf("failed to add ingress acls for banp %s: %w", key, err)
	}
	if err := c.deleteUnusedAddrSetForAnp(curIngressAddrSet, desiredIngressAddrSet); err != nil {
		return fmt.Errorf("failed to delete unused ingress address set for banp %s: %w", key, err)
	}

	egressACLOps, err := c.OVNNbClient.DeleteAclsOps(pgName, portGroupKey, "from-lport", nil)
	if err != nil {
		klog.Errorf("failed to generate clear operations for banp %s egress acls: %v", key, err)
		return err
	}
	// create egress acl
	for index, banpr := range banp.Spec.Egress {
		// A single address set must contain addresses of the same type and the name must be unique within table, so IPv4 and IPv6 address set should be different
		egressAsV4Name, egressAsV6Name := getAnpAddressSetName(pgName, banpr.Name, index, false)
		desiredEgressAddrSet.Add(egressAsV4Name, egressAsV6Name)

		var v4Addrs, v4Addr, v6Addrs, v6Addr []string
		// This field must be defined and contain at least one item.
		for _, anprpeer := range banpr.To {
			if v4Addr, v6Addr, err = c.fetchBaselineEgressSelectedAddresses(&anprpeer); err != nil {
				klog.Errorf("failed to fetch admin network policy selected addresses, %v", err)
				return err
			}
			v4Addrs = append(v4Addrs, v4Addr...)
			v6Addrs = append(v6Addrs, v6Addr...)
		}
		klog.Infof("Banp Egress Rule %s, selected v4 address %v, v6 address %v", banpr.Name, v4Addrs, v6Addrs)

		if err = c.createAsForAnpRule(banpName, banpr.Name, "egress", egressAsV4Name, v4Addrs, true); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.createAsForAnpRule(banpName, banpr.Name, "egress", egressAsV6Name, v6Addrs, true); err != nil {
			klog.Error(err)
			return err
		}

		aclPriority := util.BanpACLMaxPriority - index
		aclAction := banpACLAction(banpr.Action)
		rulePorts := []v1alpha1.AdminNetworkPolicyPort{}
		if banpr.Ports != nil {
			rulePorts = *banpr.Ports
		}

		if len(v4Addrs) != 0 {
			aclName := fmt.Sprintf("banp/%s/egress/%s/%d", banpName, kubeovnv1.ProtocolIPv4, index)
			ops, err := c.OVNNbClient.UpdateAnpRuleACLOps(pgName, egressAsV4Name, kubeovnv1.ProtocolIPv4, aclName, aclPriority, aclAction, logActions, rulePorts, false, true)
			if err != nil {
				klog.Errorf("failed to add v4 egress acls for banp %s: %v", key, err)
				return err
			}
			egressACLOps = append(egressACLOps, ops...)
		}

		if len(v6Addrs) != 0 {
			aclName := fmt.Sprintf("banp/%s/egress/%s/%d", banpName, kubeovnv1.ProtocolIPv6, index)
			ops, err := c.OVNNbClient.UpdateAnpRuleACLOps(pgName, egressAsV6Name, kubeovnv1.ProtocolIPv6, aclName, aclPriority, aclAction, logActions, rulePorts, false, true)
			if err != nil {
				klog.Errorf("failed to add v6 egress acls for banp %s: %v", key, err)
				return err
			}
			egressACLOps = append(egressACLOps, ops...)
		}
	}

	if err := c.OVNNbClient.Transact("add-egress-acls", egressACLOps); err != nil {
		return fmt.Errorf("failed to add egress acls for banp %s: %w", key, err)
	}
	if err := c.deleteUnusedAddrSetForAnp(curEgressAddrSet, desiredEgressAddrSet); err != nil {
		return fmt.Errorf("failed to delete unused egress address set for banp %s: %w", key, err)
	}

	return nil
}

func (c *Controller) handleDeleteBanp(banp *v1alpha1.BaselineAdminNetworkPolicy) error {
	c.banpKeyMutex.LockKey(banp.Name)
	defer func() { _ = c.banpKeyMutex.UnlockKey(banp.Name) }()

	klog.Infof("handle delete banp %s", banp.Name)
	banpName := getAnpName(banp.Name)

	// ACLs releated to port_group will be deleted automatically when port_group is deleted
	pgName := strings.ReplaceAll(banpName, "-", ".")
	if err := c.OVNNbClient.DeletePortGroup(pgName); err != nil {
		klog.Errorf("failed to delete port group for banp %s: %v", banpName, err)
	}

	if err := c.OVNNbClient.DeleteAddressSets(map[string]string{
		baselineAdminNetworkPolicyKey: fmt.Sprintf("%s/%s", banpName, "ingress"),
	}); err != nil {
		klog.Errorf("failed to delete ingress address set for banp %s: %v", banpName, err)
		return err
	}

	if err := c.OVNNbClient.DeleteAddressSets(map[string]string{
		baselineAdminNetworkPolicyKey: fmt.Sprintf("%s/%s", banpName, "egress"),
	}); err != nil {
		klog.Errorf("failed to delete egress address set for banp %s: %v", banpName, err)
		return err
	}

	return nil
}

func (c *Controller) handleUpdateBanp(changed *AdminNetworkPolicyChangedDelta) error {
	// Only handle updates that do not affect acls.
	c.banpKeyMutex.LockKey(changed.key)
	defer func() { _ = c.banpKeyMutex.UnlockKey(changed.key) }()

	cachedBanp, err := c.banpsLister.Get(changed.key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	desiredBanp := cachedBanp.DeepCopy()
	klog.Infof("handle update banp %s", desiredBanp.Name)

	banpName := getAnpName(desiredBanp.Name)
	pgName := strings.ReplaceAll(banpName, "-", ".")

	// The port-group for anp should be updated
	if changed.field == ChangedSubject {
		// The port-group must exist when update anp, this check should never be matched.
		if ok, err := c.OVNNbClient.PortGroupExists(pgName); !ok || err != nil {
			klog.Errorf("port-group for banp %s does not exist when update banp", desiredBanp.Name)
			return err
		}

		ports, err := c.fetchSelectedPods(&desiredBanp.Spec.Subject)
		if err != nil {
			klog.Errorf("failed to fetch ports belongs to banp %s: %v", desiredBanp.Name, err)
			return err
		}

		if err = c.OVNNbClient.PortGroupSetPorts(pgName, ports); err != nil {
			klog.Errorf("failed to set ports %v to port group %s: %v", ports, pgName, err)
			return err
		}
	}

	// Peer selector in ingress/egress rule has changed, so the corresponding address-set need be updated
	if changed.field == ChangedIngressRule {
		for index, rule := range desiredBanp.Spec.Ingress {
			// Make sure the rule is changed and go on update
			if rule.Name == changed.ruleNames[index].curRuleName {
				if err := c.setAddrSetForAnpRule(banpName, pgName, rule.Name, index, rule.From, []v1alpha1.AdminNetworkPolicyEgressPeer{}, true, true); err != nil {
					klog.Errorf("failed to set ingress address-set for anp rule %s/%s, %v", banpName, rule.Name, err)
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
		for index, rule := range desiredBanp.Spec.Egress {
			// Make sure the rule is changed and go on update
			if rule.Name == changed.ruleNames[index].curRuleName {
				if err := c.setAddrSetForBaselineAnpRule(banpName, pgName, rule.Name, index, []v1alpha1.AdminNetworkPolicyIngressPeer{}, rule.To, false, true); err != nil {
					klog.Errorf("failed to set egress address-set for banp rule %s/%s, %v", banpName, rule.Name, err)
					return err
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
