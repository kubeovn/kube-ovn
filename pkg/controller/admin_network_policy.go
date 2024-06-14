package controller

import (
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/scylladb/go-set/strset"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
)

type AnpDelta struct {
	oldAnp v1alpha1.AdminNetworkPolicy
	newAnp v1alpha1.AdminNetworkPolicy
}

func (c *Controller) enqueueAddAnp(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add anp %s", key)
	c.addAnpQueue.Add(key)
}

func (c *Controller) enqueueDeleteAnp(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue delete anp %s", key)
	c.deleteAnpQueue.Add(obj)
}

func (c *Controller) enqueueUpdateAnp(oldObj, newObj interface{}) {
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
	klog.V(3).Infof("enqueue update anp %s", newAnpObj.Name)

	// The remaining changes do not affect the acls. The port-group or address-set should be updated.
	changed := AnpDelta{
		oldAnp: *oldAnpObj,
		newAnp: *newAnpObj,
	}
	// The port-group for anp should be updated
	if !reflect.DeepEqual(oldAnpObj.Spec.Subject, newAnpObj.Spec.Subject) {
		c.updateAnpQueue.Add(changed)
		return
	}

	// Peer selector in ingress/egress rule has changed, so the corresponding address-set need be updated
	for index, rule := range newAnpObj.Spec.Ingress {
		oldRule := oldAnpObj.Spec.Ingress[index]
		if !reflect.DeepEqual(oldRule.From, rule.From) {
			c.updateAnpQueue.Add(changed)
			return
		}
	}

	for index, rule := range newAnpObj.Spec.Egress {
		oldRule := oldAnpObj.Spec.Egress[index]
		if !reflect.DeepEqual(oldRule.To, rule.To) {
			c.updateAnpQueue.Add(changed)
			return
		}
	}
}

func (c *Controller) runAddAnpWorker() {
	for c.processNextAddAnpWorkItem() {
	}
}

func (c *Controller) runUpdateAnpWorker() {
	for c.processNextUpdateAnpWorkItem() {
	}
}

func (c *Controller) runDeleteAnpWorker() {
	for c.processNextDeleteAnpWorkItem() {
	}
}

func (c *Controller) processNextAddAnpWorkItem() bool {
	obj, shutdown := c.addAnpQueue.Get()
	if shutdown {
		return false
	}
	now := time.Now()

	err := func(obj interface{}) error {
		defer c.addAnpQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addAnpQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddAnp(key); err != nil {
			c.addAnpQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		last := time.Since(now)
		klog.Infof("take %d ms to handle add anp %s", last.Milliseconds(), key)
		c.addAnpQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateAnpWorkItem() bool {
	obj, shutdown := c.updateAnpQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateAnpQueue.Done(obj)
		var key AnpDelta
		var ok bool
		if key, ok = obj.(AnpDelta); !ok {
			c.updateAnpQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected AnpDelta in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateAnp(key); err != nil {
			c.updateAnpQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing admin network policy %s: %v, requeuing", key.newAnp.Name, err)
		}
		c.updateAnpQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteAnpWorkItem() bool {
	obj, shutdown := c.deleteAnpQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.deleteAnpQueue.Done(obj)
		var anp *v1alpha1.AdminNetworkPolicy
		var ok bool
		if anp, ok = obj.(*v1alpha1.AdminNetworkPolicy); !ok {
			c.deleteAnpQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected anp object in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeleteAnp(anp); err != nil {
			c.deleteAnpQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing anp '%s': %s, requeuing", anp.Name, err.Error())
		}
		c.deleteAnpQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
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

	// ovn portGroup/addressSet doesn't support name with '-', so we replace '-' by '.'.
	// This may cause conflict if two anp with name test-anp and test.anp, maybe hash is a better solution, but we do not want to lost the readability now.
	// Make sure all create operations are reentrant.
	pgName := strings.ReplaceAll(fmt.Sprintf("%s", anpName), "-", ".")
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
	for idx, anpr := range anp.Spec.Ingress {
		// A single address set must contain addresses of the same type and the name must be unique within table, so IPv4 and IPv6 address set should be different
		ingressAsV4Name := strings.ReplaceAll(fmt.Sprintf("%s.%s.%s", pgName, anpr.Name, kubeovnv1.ProtocolIPv4), "-", ".")
		ingressAsV6Name := strings.ReplaceAll(fmt.Sprintf("%s.%s.%s", pgName, anpr.Name, kubeovnv1.ProtocolIPv6), "-", ".")
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
		klog.Infof("Anp Ingress Rule %s, selected v4 address %v, v6 address %v", anpr.Name, v4Addrs, v6Addrs)

		if err = c.createAsForAnpRule(anpName, anpr.Name, "ingress", ingressAsV4Name, v4Addrs, false); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.createAsForAnpRule(anpName, anpr.Name, "ingress", ingressAsV6Name, v6Addrs, false); err != nil {
			klog.Error(err)
			return err
		}

		// We have noticed redhat's discussion about ACL priority in https://bugzilla.redhat.com/show_bug.cgi?id=2175752
		// After discussion, we decided to use the same range of priorities. Pay tribute to the developers of redhat.
		aclPriority := util.AnpAclMaxPriority - int(anp.Spec.Priority*100) - idx
		aclAction := convertAction(anpr.Action, "")
		if len(v4Addrs) != 0 {
			ops, err := c.OVNNbClient.UpdateAnpRuleACLOps(pgName, ingressAsV4Name, kubeovnv1.ProtocolIPv4, aclPriority, aclAction, *anpr.Ports, true)
			if err != nil {
				klog.Errorf("failed to add v4 ingress acls for anp %s: %v", key, err)
				return err
			}
			ingressACLOps = append(ingressACLOps, ops...)
		}

		if len(v6Addrs) != 0 {
			ops, err := c.OVNNbClient.UpdateAnpRuleACLOps(pgName, ingressAsV6Name, kubeovnv1.ProtocolIPv6, aclPriority, aclAction, *anpr.Ports, true)
			if err != nil {
				klog.Errorf("failed to add v6 ingress acls for anp %s: %v", key, err)
				return err
			}
			ingressACLOps = append(ingressACLOps, ops...)
		}
	}

	if err := c.OVNNbClient.Transact("add-ingress-acls", ingressACLOps); err != nil {
		return fmt.Errorf("failed to add ingress acls for anp %s: %v", key, err)
	}
	if err := c.deleteUnusedAddrSetForAnp(curIngressAddrSet, desiredIngressAddrSet); err != nil {
		return fmt.Errorf("failed to delete unused ingress address set for anp %s: %v", key, err)
	}

	egressACLOps, err := c.OVNNbClient.DeleteAclsOps(pgName, portGroupKey, "from-lport", nil)
	if err != nil {
		klog.Errorf("failed to generate clear operations for anp %s egress acls: %v", key, err)
		return err
	}
	// create egress acl
	for idx, anpr := range anp.Spec.Egress {
		// A single address set must contain addresses of the same type and the name must be unique within table, so IPv4 and IPv6 address set should be different
		egressAsV4Name := strings.ReplaceAll(fmt.Sprintf("%s.%s.%s", pgName, anpr.Name, kubeovnv1.ProtocolIPv4), "-", ".")
		egressAsV6Name := strings.ReplaceAll(fmt.Sprintf("%s.%s.%s", pgName, anpr.Name, kubeovnv1.ProtocolIPv6), "-", ".")
		desiredEgressAddrSet.Add(egressAsV4Name, egressAsV6Name)

		var v4Addrs, v4Addr, v6Addrs, v6Addr []string
		// This field must be defined and contain at least one item.
		for _, anprpeer := range anpr.To {
			if v4Addr, v6Addr, err = c.fetchEgressSelectedAddresses(&anprpeer); err != nil {
				klog.Errorf("failed to fetch admin network policy selected addresses, %v", err)
				return err
			}
			v4Addrs = append(v4Addrs, v4Addr...)
			v6Addrs = append(v6Addrs, v6Addr...)
		}
		klog.Infof("Anp Egress Rule %s, selected v4 address %v, v6 address %v", anpr.Name, v4Addrs, v6Addrs)

		if err = c.createAsForAnpRule(anpName, anpr.Name, "egress", egressAsV4Name, v4Addrs, false); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.createAsForAnpRule(anpName, anpr.Name, "egress", egressAsV6Name, v6Addrs, false); err != nil {
			klog.Error(err)
			return err
		}

		aclPriority := util.AnpAclMaxPriority - int(anp.Spec.Priority*100) - idx
		aclAction := convertAction(anpr.Action, "")
		if len(v4Addrs) != 0 {
			ops, err := c.OVNNbClient.UpdateAnpRuleACLOps(pgName, egressAsV4Name, kubeovnv1.ProtocolIPv4, aclPriority, aclAction, *anpr.Ports, false)
			if err != nil {
				klog.Errorf("failed to add v4 egress acls for anp %s: %v", key, err)
				return err
			}
			egressACLOps = append(egressACLOps, ops...)
		}

		if len(v6Addrs) != 0 {
			ops, err := c.OVNNbClient.UpdateAnpRuleACLOps(pgName, egressAsV6Name, kubeovnv1.ProtocolIPv6, aclPriority, aclAction, *anpr.Ports, false)
			if err != nil {
				klog.Errorf("failed to add v6 egress acls for anp %s: %v", key, err)
				return err
			}
			egressACLOps = append(egressACLOps, ops...)
		}
	}

	if err := c.OVNNbClient.Transact("add-egress-acls", egressACLOps); err != nil {
		return fmt.Errorf("failed to add egress acls for anp %s: %v", key, err)
	}
	if err := c.deleteUnusedAddrSetForAnp(curEgressAddrSet, desiredEgressAddrSet); err != nil {
		return fmt.Errorf("failed to delete unused egress address set for anp %s: %v", key, err)
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
	pgName := strings.ReplaceAll(fmt.Sprintf("%s", anpName), "-", ".")
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

	return nil
}

func (c *Controller) handleUpdateAnp(changed AnpDelta) error {
	// Only handle updates that do not affect acls.
	c.anpKeyMutex.LockKey(changed.newAnp.Name)
	defer func() { _ = c.anpKeyMutex.UnlockKey(changed.newAnp.Name) }()

	curAnp := changed.oldAnp
	desiredAnp := changed.newAnp
	klog.Infof("handle update admin network policy %s", desiredAnp.Name)

	anpName := getAnpName(desiredAnp.Name)
	pgName := strings.ReplaceAll(fmt.Sprintf("%s", anpName), "-", ".")

	// The port-group for anp should be updated
	if !reflect.DeepEqual(curAnp.Spec.Subject, desiredAnp.Spec.Subject) {
		// The port-group must exist when update anp, this check should never be matched.
		if ok, err := c.OVNNbClient.PortGroupExists(pgName); !ok || err != nil {
			klog.Error("port-group for anp %s does not exist when update anp", desiredAnp.Name)
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
	for index, rule := range desiredAnp.Spec.Ingress {
		oldRule := curAnp.Spec.Ingress[index]
		if !reflect.DeepEqual(oldRule.From, rule.From) {
			if err := c.setAddrSetForAnpRule(anpName, pgName, rule.Name, rule.From, []v1alpha1.AdminNetworkPolicyEgressPeer{}, true, false); err != nil {
				klog.Errorf("failed to set ingress address-set for anp rule %s/%s, %v", anpName, rule.Name, err)
				return err
			}

			var oldAsV4Name, oldAsV6Name string
			// Normally the name can not be changed, but just in case, when the name changes, the old address set should be deleted
			// There is no description in the Name comments that it cannot be changed
			if oldRule.Name != rule.Name {
				oldAsV4Name = strings.ReplaceAll(fmt.Sprintf("%s.%s.%s", pgName, oldRule.Name, kubeovnv1.ProtocolIPv4), "-", ".")
				oldAsV6Name = strings.ReplaceAll(fmt.Sprintf("%s.%s.%s", pgName, oldRule.Name, kubeovnv1.ProtocolIPv6), "-", ".")
			}

			if oldAsV4Name != "" {
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

	for index, rule := range desiredAnp.Spec.Egress {
		oldRule := curAnp.Spec.Egress[index]
		if !reflect.DeepEqual(oldRule.To, rule.To) {
			if err := c.setAddrSetForAnpRule(anpName, pgName, rule.Name, []v1alpha1.AdminNetworkPolicyIngressPeer{}, rule.To, false, false); err != nil {
				klog.Errorf("failed to set egress address-set for anp rule %s/%s, %v", anpName, rule.Name, err)
				return err
			}

			var oldAsV4Name, oldAsV6Name string
			// Normally the name can not be changed, but just in case, when the name changes, the old address set should be deleted
			// There is no description in the Name comments that it cannot be changed
			if oldRule.Name != rule.Name {
				oldAsV4Name = strings.ReplaceAll(fmt.Sprintf("%s.%s.%s", pgName, oldRule.Name, kubeovnv1.ProtocolIPv4), "-", ".")
				oldAsV6Name = strings.ReplaceAll(fmt.Sprintf("%s.%s.%s", pgName, oldRule.Name, kubeovnv1.ProtocolIPv6), "-", ".")
			}

			if oldAsV4Name != "" {
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

	return nil
}

func (c *Controller) validateAnpConfig(anp *v1alpha1.AdminNetworkPolicy) error {
	// The behavior is undefined if two ANP objects have same priority.
	if anpName, exist := c.anpPrioNameMap[anp.Spec.Priority]; exist && anpName != anp.Name {
		err := fmt.Errorf("can not create anp with same priority %d, exist one is %s, new created is %s", anp.Spec.Priority, anpName, anp.Name)
		klog.Error(err)
		return err
	}

	if len(anp.Spec.Ingress) > util.AnpMaxRules || len(anp.Spec.Egress) > util.AnpMaxRules {
		err := fmt.Errorf("At most %d rules can be create in anp ingress/egress, ingress rules num %d and egress rules num %d in anp %s", util.AnpMaxRules, len(anp.Spec.Ingress), len(anp.Spec.Egress), anp.Name)
		klog.Error(err)
		return err
	}

	if len(anp.Spec.Ingress) == 0 && len(anp.Spec.Egress) == 0 {
		err := fmt.Errorf("one of ingress/egress rules must be set, both ingress/egress are empty for anp %s", anp.Name)
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
			return nil, fmt.Errorf("error creating ns label selector, %v", err)
		}

		ports, _, _, err = c.fetchPods(nsSelector, labels.Everything())
		if err != nil {
			return nil, fmt.Errorf("failed to fetch pods, %v", err)
		}
	} else {
		nsSelector, err := metav1.LabelSelectorAsSelector(&anpSubject.Pods.NamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("error creating ns label selector, %v", err)
		}
		podSelector, err := metav1.LabelSelectorAsSelector(&anpSubject.Pods.PodSelector)
		if err != nil {
			return nil, fmt.Errorf("error creating pod label selector, %v", err)
		}

		ports, _, _, err = c.fetchPods(nsSelector, podSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch pods, %v", err)
		}
	}

	return ports, nil
}

func (c *Controller) fetchPods(nsSelector, podSelector labels.Selector) ([]string, []string, []string, error) {
	ports := make([]string, 0, 1000)
	v4Addresses := make([]string, 0, 1000)
	v6Addresses := make([]string, 0, 1000)

	namespaces, err := c.namespacesLister.List(nsSelector)
	if err != nil {
		klog.Errorf("failed to list namespaces: %v", err)
		return nil, nil, nil, err
	}

	for _, namespace := range namespaces {
		pods, err := c.podsLister.Pods(namespace.Name).List(podSelector)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to list pods, %v", err)
		}

		for _, pod := range pods {
			if pod.Spec.HostNetwork {
				continue
			}
			podName := c.getNameByPod(pod)

			podNets, err := c.getPodKubeovnNets(pod)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to get pod networks, %v", err)
			}

			for _, podNet := range podNets {
				if !isOvnSubnet(podNet.Subnet) {
					continue
				}

				if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "true" {
					ports = append(ports, ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName))

					podIPAnnotation := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)]
					podIPs := strings.Split(podIPAnnotation, ",")
					for _, podIP := range podIPs {
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
			return nil, nil, fmt.Errorf("error creating ns label selector, %v", err)
		}

		_, v4Addresses, v6Addresses, err = c.fetchPods(nsSelector, labels.Everything())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch ingress peer addresses, %v", err)
		}
	} else if ingressPeer.Pods != nil {
		nsSelector, err := metav1.LabelSelectorAsSelector(&ingressPeer.Pods.NamespaceSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating ns label selector, %v", err)
		}
		podSelector, err := metav1.LabelSelectorAsSelector(&ingressPeer.Pods.PodSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating pod label selector, %v", err)
		}

		_, v4Addresses, v6Addresses, err = c.fetchPods(nsSelector, podSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch ingress peer addresses, %v", err)
		}
	} else {
		return nil, nil, fmt.Errorf("no pods or namespaces selector is set for ingress peer")
	}

	return v4Addresses, v6Addresses, nil
}

func (c *Controller) fetchEgressSelectedAddresses(egressPeer *v1alpha1.AdminNetworkPolicyEgressPeer) ([]string, []string, error) {
	var v4Addresses, v6Addresses []string

	// Exactly one of the selector pointers must be set for a given peer.
	if egressPeer.Namespaces != nil {
		nsSelector, err := metav1.LabelSelectorAsSelector(egressPeer.Namespaces)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating ns label selector, %v", err)
		}

		_, v4Addresses, v6Addresses, err = c.fetchPods(nsSelector, labels.Everything())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch egress peer addresses, %v", err)
		}
	} else if egressPeer.Pods != nil {
		nsSelector, err := metav1.LabelSelectorAsSelector(&egressPeer.Pods.NamespaceSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating ns label selector, %v", err)
		}
		podSelector, err := metav1.LabelSelectorAsSelector(&egressPeer.Pods.PodSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating pod label selector, %v", err)
		}

		_, v4Addresses, v6Addresses, err = c.fetchPods(nsSelector, podSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch egress peer addresses, %v", err)
		}
	} else if egressPeer.Nodes != nil {
		// TODO: add support for node selector
	} else if len(egressPeer.Networks) != 0 {
		// TODO: add support for cidr
	} else {
		return nil, nil, fmt.Errorf("no pods or namespaces selector is set for egress peer")
	}

	return v4Addresses, v6Addresses, nil
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

func (c *Controller) deleteUnusedAddrSetForAnp(curAddrSet *strset.Set, desiredAddrSet *strset.Set) error {
	toDel := strset.Difference(curAddrSet, desiredAddrSet).List()

	for _, asName := range toDel {
		if err := c.OVNNbClient.DeleteAddressSet(asName); err != nil {
			klog.Errorf("failed to delete address set %s, %v", asName, err)
			return err
		}
	}

	return nil
}

func (c *Controller) setAddrSetForAnpRule(anpName, pgName, ruleName string, from []v1alpha1.AdminNetworkPolicyIngressPeer, to []v1alpha1.AdminNetworkPolicyEgressPeer, isIngress, isBanp bool) error {
	// A single address set must contain addresses of the same type and the name must be unique within table, so IPv4 and IPv6 address set should be different
	gressAsV4Name := strings.ReplaceAll(fmt.Sprintf("%s.%s.%s", pgName, ruleName, kubeovnv1.ProtocolIPv4), "-", ".")
	gressAsV6Name := strings.ReplaceAll(fmt.Sprintf("%s.%s.%s", pgName, ruleName, kubeovnv1.ProtocolIPv6), "-", ".")

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
		klog.Infof("Update Anp/Banp Ingress Rule %s, selected v4 address %v, v6 address %v", ruleName, v4Addrs, v6Addrs)

		if err = c.createAsForAnpRule(anpName, ruleName, "ingress", gressAsV4Name, v4Addrs, isBanp); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.createAsForAnpRule(anpName, ruleName, "ingress", gressAsV6Name, v6Addrs, isBanp); err != nil {
			klog.Error(err)
			return err
		}
	} else {
		for _, anprpeer := range to {
			if v4Addr, v6Addr, err = c.fetchEgressSelectedAddresses(&anprpeer); err != nil {
				klog.Errorf("failed to fetch anp/banp egress selected addresses, %v", err)
				return err
			}
			v4Addrs = append(v4Addrs, v4Addr...)
			v6Addrs = append(v6Addrs, v6Addr...)
		}
		klog.Infof("Update Anp/Banp Egress Rule %s, selected v4 address %v, v6 address %v", ruleName, v4Addrs, v6Addrs)

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

func (c *Controller) checkLabelsMatchForAnps(selectLabels map[string]string) {
	anps, _ := c.anpsLister.List(labels.Everything())
	for _, anp := range anps {
		changed := AnpDelta{
			oldAnp: v1alpha1.AdminNetworkPolicy{},
			newAnp: *anp,
		}

		if isLabelsMatchAnpSubject(anp.Spec.Subject, selectLabels) {
			c.updateAnpQueue.Add(changed)
		}

		if isLabelsMatchAnpRulePeers(anp.Spec.Ingress, anp.Spec.Egress, selectLabels) {
			c.updateAnpQueue.Add(changed)
		}
	}

	banps, _ := c.banpsLister.List(labels.Everything())
	for _, banp := range banps {
		changed := BanpDelta{
			oldBanp: v1alpha1.BaselineAdminNetworkPolicy{},
			newBanp: *banp,
		}

		if isLabelsMatchAnpSubject(banp.Spec.Subject, selectLabels) {
			c.updateBanpQueue.Add(changed)
		}

		if isLabelsMatchBanpRulePeers(banp.Spec.Ingress, banp.Spec.Egress, selectLabels) {
			c.updateBanpQueue.Add(changed)
		}
	}

	return
}

func isLabelsMatchAnpSubject(subject v1alpha1.AdminNetworkPolicySubject, selectLabels map[string]string) bool {
	var anpNsSelector labels.Selector

	if subject.Namespaces != nil {
		anpNsSelector, _ = metav1.LabelSelectorAsSelector(subject.Namespaces)
	} else {
		anpNsSelector, _ = metav1.LabelSelectorAsSelector(&subject.Pods.NamespaceSelector)
	}
	if anpNsSelector.Matches(labels.Set(selectLabels)) {
		return true
	}

	return false
}

func isLabelsMatchRulePeers(from []v1alpha1.AdminNetworkPolicyIngressPeer, to []v1alpha1.AdminNetworkPolicyEgressPeer, selectLabels map[string]string) bool {
	var anpNsSelector labels.Selector

	for _, ingressPeer := range from {
		if ingressPeer.Namespaces != nil {
			anpNsSelector, _ = metav1.LabelSelectorAsSelector(ingressPeer.Namespaces)
		} else {
			anpNsSelector, _ = metav1.LabelSelectorAsSelector(&ingressPeer.Pods.NamespaceSelector)
		}
		if anpNsSelector.Matches(labels.Set(selectLabels)) {
			return true
		}
	}

	for _, egressPeer := range to {
		if egressPeer.Namespaces != nil {
			anpNsSelector, _ = metav1.LabelSelectorAsSelector(egressPeer.Namespaces)
		} else {
			anpNsSelector, _ = metav1.LabelSelectorAsSelector(&egressPeer.Pods.NamespaceSelector)
		}
		if anpNsSelector.Matches(labels.Set(selectLabels)) {
			return true
		}
	}

	return false
}

func isLabelsMatchAnpRulePeers(ingress []v1alpha1.AdminNetworkPolicyIngressRule, egress []v1alpha1.AdminNetworkPolicyEgressRule, selectLabels map[string]string) bool {
	for _, anpr := range ingress {
		if isLabelsMatchRulePeers(anpr.From, []v1alpha1.AdminNetworkPolicyEgressPeer{}, selectLabels) {
			return true
		}
	}
	for _, anpr := range egress {
		if isLabelsMatchRulePeers([]v1alpha1.AdminNetworkPolicyIngressPeer{}, anpr.To, selectLabels) {
			return true
		}
	}

	return false
}

func isLabelsMatchBanpRulePeers(ingress []v1alpha1.BaselineAdminNetworkPolicyIngressRule, egress []v1alpha1.BaselineAdminNetworkPolicyEgressRule, selectLabels map[string]string) bool {
	for _, banpr := range ingress {
		if isLabelsMatchRulePeers(banpr.From, []v1alpha1.AdminNetworkPolicyEgressPeer{}, selectLabels) {
			return true
		}
	}
	for _, banpr := range egress {
		if isLabelsMatchRulePeers([]v1alpha1.AdminNetworkPolicyIngressPeer{}, banpr.To, selectLabels) {
			return true
		}
	}

	return false
}

func getAnpName(name string) string {
	anpName := name
	nameArray := []rune(name)
	if !unicode.IsLetter(nameArray[0]) {
		anpName = "anp" + name
	}
	return anpName
}

func convertAction(anpRuleAction v1alpha1.AdminNetworkPolicyRuleAction, banpRuleAction v1alpha1.BaselineAdminNetworkPolicyRuleAction) (aclAction ovnnb.ACLAction) {
	switch anpRuleAction {
	case v1alpha1.AdminNetworkPolicyRuleActionAllow:
		aclAction = ovnnb.ACLActionAllow
	case v1alpha1.AdminNetworkPolicyRuleActionDeny:
		aclAction = ovnnb.ACLActionDrop
	case v1alpha1.AdminNetworkPolicyRuleActionPass:
		aclAction = ovnnb.ACLActionPass
	}

	switch banpRuleAction {
	case v1alpha1.BaselineAdminNetworkPolicyRuleActionAllow:
		aclAction = ovnnb.ACLActionAllow
	case v1alpha1.BaselineAdminNetworkPolicyRuleActionDeny:
		aclAction = ovnnb.ACLActionDrop
	}
	return
}
