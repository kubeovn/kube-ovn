package controller

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddQoSPolicy(obj any) {
	qos := obj.(*kubeovnv1.QoSPolicy)
	key := cache.MetaObjectToName(qos).String()
	// A policy already marked for deletion must go through the update reconcile so its finalizer
	// can be released; handleAddQoSPolicy does not process terminating policies. This also covers
	// controller restart, where the informer re-lists objects already in their final state.
	if enqueueUpdateIfTerminating(c.updateQoSPolicyQueue, key, "qos", qos.DeletionTimestamp) {
		return
	}
	klog.V(3).Infof("enqueue add qos policy %s", key)
	c.addQoSPolicyQueue.Add(key)
}

// enqueueQoSPolicyRelease re-enqueues policies no longer present in either the
// desired Spec reference or the applied Status credential. The event handler runs
// after the informer store update, so cleanup has converged before finalizer release.
func (c *Controller) enqueueQoSPolicyRelease(oldSpec, oldStatus, newSpec, newStatus string) {
	for _, qos := range []string{oldSpec, oldStatus} {
		if qos != "" && qos != newSpec && qos != newStatus {
			c.updateQoSPolicyQueue.Add(qos)
		}
	}
}

func compareQoSPolicyBandwidthLimitRules(oldObj, newObj kubeovnv1.QoSPolicyBandwidthLimitRules) bool {
	if len(oldObj) != len(newObj) {
		return false
	}

	// Sort both slices by Name for order-independent comparison
	// We need to sort copies to avoid mutating the original slices
	sortedOld := make(kubeovnv1.QoSPolicyBandwidthLimitRules, len(oldObj))
	sortedNew := make(kubeovnv1.QoSPolicyBandwidthLimitRules, len(newObj))
	copy(sortedOld, oldObj)
	copy(sortedNew, newObj)

	sort.Slice(sortedOld, func(i, j int) bool {
		return sortedOld[i].Name < sortedOld[j].Name
	})
	sort.Slice(sortedNew, func(i, j int) bool {
		return sortedNew[i].Name < sortedNew[j].Name
	})
	return reflect.DeepEqual(sortedOld, sortedNew)
}

func (c *Controller) enqueueUpdateQoSPolicy(oldObj, newObj any) {
	oldQos := oldObj.(*kubeovnv1.QoSPolicy)
	newQos := newObj.(*kubeovnv1.QoSPolicy)
	key := cache.MetaObjectToName(newQos).String()
	if oldQos.Status.BindingType == "" && qosPolicyStatusMatchesSpec(newQos) {
		c.enqueueQoSPolicyReferences(newQos)
	}
	if !newQos.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("enqueue update to clean qos %s", key)
		c.updateQoSPolicyQueue.Add(key)
		return
	}
	// Compare newQos.Status with newQos.Spec to check if reconciliation is needed
	// Using oldQos.Status would cause false positives when handleAddQoSPolicy patches status
	if newQos.Status.Shared != newQos.Spec.Shared ||
		newQos.Status.BindingType != newQos.Spec.BindingType ||
		!compareQoSPolicyBandwidthLimitRules(newQos.Status.BandwidthLimitRules,
			newQos.Spec.BandwidthLimitRules) {
		klog.V(3).Infof("enqueue update qos %s", key)
		c.updateQoSPolicyQueue.Add(key)
		return
	}
}

func qosPolicyStatusMatchesSpec(qos *kubeovnv1.QoSPolicy) bool {
	return qos.Status.Shared == qos.Spec.Shared && qos.Status.BindingType == qos.Spec.BindingType &&
		compareQoSPolicyBandwidthLimitRules(qos.Status.BandwidthLimitRules, qos.Spec.BandwidthLimitRules)
}

func iptablesEIPsUsingQoS(eips []*kubeovnv1.IptablesEIP, qos string) []*kubeovnv1.IptablesEIP {
	result := make([]*kubeovnv1.IptablesEIP, 0, len(eips))
	for _, eip := range eips {
		if eip.Spec.QoSPolicy == qos || eip.Status.QoSPolicy == qos {
			result = append(result, eip)
		}
	}
	return result
}

// enqueueQoSPolicyReferences notifies the resources referencing a policy once it
// first becomes usable. Each referrer reconciles the policy from its own queue, so
// a referrer that cannot apply the policy cannot hold up an unrelated one.
func (c *Controller) enqueueQoSPolicyReferences(qos *kubeovnv1.QoSPolicy) {
	switch qos.Status.BindingType {
	case kubeovnv1.QoSBindingTypeEIP:
		eips, err := c.iptablesEipsLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list eips referencing QoS policy %s: %v", qos.Name, err)
			return
		}
		for _, eip := range eips {
			if eip.Spec.QoSPolicy == qos.Name {
				klog.V(3).Infof("enqueue eip %s for ready QoS policy %s", eip.Name, qos.Name)
				c.updateIptablesEipQueue.Add(eip.Name)
			}
		}
	case kubeovnv1.QoSBindingTypeNatGw:
		items, err := c.vpcNatGatewayLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list nat gateways referencing QoS policy %s: %v", qos.Name, err)
			return
		}
		for _, gateway := range items {
			if gateway.Spec.QoSPolicy == qos.Name {
				klog.V(3).Infof("enqueue vpc nat gateway %s for ready QoS policy %s", gateway.Name, qos.Name)
				c.addOrUpdateVpcNatGatewayQueue.Add(gateway.Name)
			}
		}
	}
}

func (c *Controller) enqueueDelQoSPolicy(obj any) {
	var qos *kubeovnv1.QoSPolicy
	switch t := obj.(type) {
	case *kubeovnv1.QoSPolicy:
		qos = t
	case cache.DeletedFinalStateUnknown:
		q, ok := t.Obj.(*kubeovnv1.QoSPolicy)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		qos = q
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	key := cache.MetaObjectToName(qos).String()
	klog.V(3).Infof("enqueue delete qos policy %s", key)
	c.delQoSPolicyQueue.Add(key)
}

func (c *Controller) handleAddQoSPolicy(key string) error {
	cachedQoS, err := c.qosPoliciesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add QoS policy %s", key)

	// Add the finalizer as soon as the policy exists so that deletion always goes through
	// the reference check, even for policies whose spec is never updated after creation.
	if err = c.handleAddQoSPolicyFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for qos %s, %v", key, err)
		return err
	}

	sortedNewRules := slices.Clone(cachedQoS.Spec.BandwidthLimitRules)
	sort.Slice(sortedNewRules, func(i, j int) bool {
		return sortedNewRules[i].Name < sortedNewRules[j].Name
	})

	if reflect.DeepEqual(cachedQoS.Status.BandwidthLimitRules,
		sortedNewRules) &&
		cachedQoS.Status.Shared == cachedQoS.Spec.Shared &&
		cachedQoS.Status.BindingType == cachedQoS.Spec.BindingType {
		// already ok
		return nil
	}
	klog.V(3).Infof("handle add qos %s", key)

	if err := c.validateQosPolicy(cachedQoS); err != nil {
		klog.Errorf("failed to validate qos %s, %v", key, err)
		return err
	}

	if err = c.patchQoSStatus(key, cachedQoS.Spec.Shared, cachedQoS.Spec.BindingType, sortedNewRules); err != nil {
		klog.Errorf("failed to patch status for qos %s, %v", key, err)
		return err
	}

	return nil
}

func (c *Controller) patchQoSStatus(
	key string, shared bool, qosType kubeovnv1.QoSPolicyBindingType, bandwidthRules kubeovnv1.QoSPolicyBandwidthLimitRules,
) error {
	oriQoS, err := c.qosPoliciesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	qos := oriQoS.DeepCopy()
	qos.Status.Shared = shared
	qos.Status.BindingType = qosType
	qos.Status.BandwidthLimitRules = bandwidthRules
	bytes, err := qos.Status.Bytes()
	if err != nil {
		klog.Error(err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().QoSPolicies().Patch(context.Background(), qos.Name,
		types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to patch qos %s, %v", qos.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelQoSPoliciesFinalizer(key string) error {
	cachedQoSPolicies, err := c.qosPoliciesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if len(cachedQoSPolicies.GetFinalizers()) == 0 {
		return nil
	}
	newQoSPolicies := cachedQoSPolicies.DeepCopy()
	controllerutil.RemoveFinalizer(newQoSPolicies, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedQoSPolicies, newQoSPolicies)
	if err != nil {
		klog.Errorf("failed to generate patch payload for qos '%s', %v", cachedQoSPolicies.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().QoSPolicies().Patch(context.Background(), cachedQoSPolicies.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from qos '%s', %v", cachedQoSPolicies.Name, err)
		return err
	}
	return nil
}

func (c *Controller) syncQoSPolicyFinalizer(cl client.Client) error {
	// migrate deprecated finalizer to new finalizer
	polices := &kubeovnv1.QoSPolicyList{}
	return migrateFinalizers(cl, polices, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(polices.Items) {
			return nil, nil
		}
		return polices.Items[i].DeepCopy(), polices.Items[i].DeepCopy()
	})
}

func diffQoSPolicyBandwidthLimitRules(oldList, newList kubeovnv1.QoSPolicyBandwidthLimitRules) (added, deleted, updated kubeovnv1.QoSPolicyBandwidthLimitRules) {
	added = kubeovnv1.QoSPolicyBandwidthLimitRules{}
	deleted = kubeovnv1.QoSPolicyBandwidthLimitRules{}
	updated = kubeovnv1.QoSPolicyBandwidthLimitRules{}

	// Create a map of old rules indexed by name for efficient lookup
	// Store values (not pointers) to ensure correct reflect.DeepEqual comparison
	oldMap := make(map[string]kubeovnv1.QoSPolicyBandwidthLimitRule)
	for _, s := range oldList {
		oldMap[s.Name] = s
	}

	// Loop through new rules and compare with old rules
	for _, s := range newList {
		old, ok := oldMap[s.Name]
		switch {
		case !ok:
			added = append(added, s)
		case reflect.DeepEqual(old, s):
		case old.Direction == s.Direction && old.Interface == s.Interface && old.Priority == s.Priority &&
			old.MatchType == s.MatchType && old.MatchValue == s.MatchValue:
			updated = append(updated, s)
		default:
			deleted = append(deleted, old)
			added = append(added, s)
		}
		delete(oldMap, s.Name)
	}

	// Remaining rules in oldMap are deleted
	for _, s := range oldMap {
		deleted = append(deleted, s)
	}

	return added, deleted, updated
}

func (c *Controller) reconcileEIPBandwidthLimitRulesLocked(
	eip *kubeovnv1.IptablesEIP,
	added kubeovnv1.QoSPolicyBandwidthLimitRules,
	deleted kubeovnv1.QoSPolicyBandwidthLimitRules,
	updated kubeovnv1.QoSPolicyBandwidthLimitRules,
) error {
	if eip.Spec.NatGwDp == "" || eip.Status.IP == "" {
		return nil
	}
	// Rules whose identity changed are deleted first, then the new rules are
	// added, so that the gateway never carries stale tc classes from the old rule.
	if len(deleted) > 0 {
		if err := c.delEIPBandwidthLimitRulesLocked(eip, eip.Status.IP, deleted); err != nil {
			return fmt.Errorf("failed to delete eip %s bandwidth limit rules: %w", eip.Name, err)
		}
	}
	if len(added) > 0 {
		if err := c.addOrUpdateEIPBandwidthLimitRulesLocked(eip, eip.Status.IP, added); err != nil {
			return fmt.Errorf("failed to add eip %s bandwidth limit rules: %w", eip.Name, err)
		}
	}
	if len(updated) > 0 {
		if err := c.addOrUpdateEIPBandwidthLimitRulesLocked(eip, eip.Status.IP, updated); err != nil {
			return fmt.Errorf("failed to update eip %s bandwidth limit rules: %w", eip.Name, err)
		}
	}
	return nil
}

func validateIPMatchValue(matchValue string) bool {
	parts := strings.Split(matchValue, " ")
	if len(parts) != 2 || parts[0] != "src" && parts[0] != "dst" {
		return false
	}
	prefix, err := netip.ParsePrefix(parts[1])
	return err == nil && prefix.Addr().Is4() && prefix == prefix.Masked()
}

// numericRatePattern validates that rate/burst values are numeric (integer or decimal)
// Supports decimal values like "0.5" for sub-Mbps rates (0.5 Mbps = 500 Kbps)
// This prevents command injection when values are passed to shell scripts
// Defense in depth: CRD schema validation may be bypassed by direct API access
var numericRatePattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// interfaceNamePattern validates network interface names
// Linux interface names: alphanumeric, underscore, hyphen, max 15 chars (IFNAMSIZ-1)
// Examples: eth0, net1, veth-abc, bond_0
// This prevents command injection when interface names are passed to shell scripts
var interfaceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,15}$`)

func validateRateValue(value, fieldName string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", fieldName)
	}
	if !numericRatePattern.MatchString(value) {
		return fmt.Errorf("invalid %s value %q: must be a positive number (e.g., 100 or 0.5)", fieldName, value)
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || n <= 0 {
		return fmt.Errorf("invalid %s value %q: must be greater than zero", fieldName, value)
	}
	if fieldName == "rateMax" && n < 0.000008 {
		return fmt.Errorf("invalid rateMax value %q: tc cannot represent less than 0.000008 Mbps", value)
	}
	return nil
}

// validateInterfaceName validates network interface name to prevent command injection
// Linux interface names must be 1-15 characters, alphanumeric with underscore/hyphen
func validateInterfaceName(iface string) error {
	if iface == "" {
		return nil // empty is allowed (omitempty in CRD)
	}
	if !interfaceNamePattern.MatchString(iface) {
		return fmt.Errorf("invalid interface name %q: must be 1-15 alphanumeric characters, underscores, or hyphens", iface)
	}
	return nil
}

// validateDirection validates QoS rule direction to prevent command injection
// Only "ingress" and "egress" are valid values
func validateDirection(direction kubeovnv1.QoSPolicyRuleDirection) error {
	if direction != kubeovnv1.QoSDirectionIngress && direction != kubeovnv1.QoSDirectionEgress {
		return fmt.Errorf("invalid direction %q: must be 'ingress' or 'egress'", direction)
	}
	return nil
}

func qosRuleInterface(rule kubeovnv1.QoSPolicyBandwidthLimitRule) string {
	if rule.Interface != "" {
		return rule.Interface
	}
	if rule.Direction == kubeovnv1.QoSDirectionIngress {
		return "eth0"
	}
	return "net1"
}

func (c *Controller) validateQosPolicy(qosPolicy *kubeovnv1.QoSPolicy) error {
	if qosPolicy.Spec.BindingType != kubeovnv1.QoSBindingTypeEIP &&
		qosPolicy.Spec.BindingType != kubeovnv1.QoSBindingTypeNatGw {
		return fmt.Errorf("invalid binding type %q: must be EIP or NATGW", qosPolicy.Spec.BindingType)
	}
	names := make(map[string]struct{}, len(qosPolicy.Spec.BandwidthLimitRules))
	directions := make(map[kubeovnv1.QoSPolicyRuleDirection]struct{}, 2)
	identities := make(map[struct {
		direction  kubeovnv1.QoSPolicyRuleDirection
		iface      string
		priority   int
		matchType  kubeovnv1.QoSPolicyRuleMatchType
		matchValue string
	}]struct{}, len(qosPolicy.Spec.BandwidthLimitRules))
	for _, rule := range qosPolicy.Spec.BandwidthLimitRules {
		if _, ok := names[rule.Name]; ok {
			return fmt.Errorf("duplicate bandwidth rule name %q", rule.Name)
		}
		names[rule.Name] = struct{}{}
		if rule.Priority < 0 || rule.Priority > 65535 {
			return fmt.Errorf("invalid priority %d: must be between 0 and 65535", rule.Priority)
		}
		if qosPolicy.Spec.BindingType == kubeovnv1.QoSBindingTypeNatGw &&
			rule.MatchType == kubeovnv1.QoSMatchTypeIP && rule.Priority == 0 {
			return errors.New("priority must be greater than zero for NATGW ip match rules")
		}
		if err := validateRateValue(rule.RateMax, "rateMax"); err != nil {
			return err
		}
		if err := validateRateValue(rule.BurstMax, "burstMax"); err != nil {
			return err
		}
		if err := validateInterfaceName(rule.Interface); err != nil {
			return err
		}
		if err := validateDirection(rule.Direction); err != nil {
			return err
		}
		switch rule.MatchType {
		case "":
			if rule.MatchValue != "" {
				return errors.New("matchValue must be empty when matchType is empty")
			}
		case kubeovnv1.QoSMatchTypeIP:
			if !validateIPMatchValue(rule.MatchValue) {
				return fmt.Errorf("invalid ip MatchValue %s", rule.MatchValue)
			}
		default:
			return fmt.Errorf("invalid match type %q", rule.MatchType)
		}

		if qosPolicy.Spec.BindingType == kubeovnv1.QoSBindingTypeNatGw {
			matchValue := rule.MatchValue
			priority := rule.Priority
			if rule.MatchType == "" {
				matchValue = ""
				priority %= 255
			}
			identity := struct {
				direction  kubeovnv1.QoSPolicyRuleDirection
				iface      string
				priority   int
				matchType  kubeovnv1.QoSPolicyRuleMatchType
				matchValue string
			}{rule.Direction, qosRuleInterface(rule), priority, rule.MatchType, matchValue}
			if _, ok := identities[identity]; ok {
				return fmt.Errorf("bandwidth rule %q duplicates an existing rule identity", rule.Name)
			}
			identities[identity] = struct{}{}
		} else {
			if rule.Interface != "" || rule.MatchType != "" || rule.MatchValue != "" {
				return fmt.Errorf("bandwidth rule %q: interface and match fields are not supported for EIP binding", rule.Name)
			}
			if _, ok := directions[rule.Direction]; ok {
				return fmt.Errorf("bandwidth rule %q duplicates direction %q", rule.Name, rule.Direction)
			}
			directions[rule.Direction] = struct{}{}
		}
	}
	if !qosPolicy.Spec.Shared && qosPolicy.Spec.BindingType == kubeovnv1.QoSBindingTypeNatGw {
		return fmt.Errorf("qos policy %s is not shared, but binding to nat gateway", qosPolicy.Name)
	}
	return nil
}

func (c *Controller) handleUpdateQoSPolicy(key string) error {
	cachedQos, err := c.qosPoliciesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle update QoS policy %s", key)

	// should delete
	if !cachedQos.DeletionTimestamp.IsZero() {
		// Check both supported reference types. BindingType changes are rejected by
		// reconciliation but not by API validation, so deletion cannot trust Spec alone.
		eips, err := c.iptablesEipsLister.List(labels.Everything())
		if err != nil {
			return fmt.Errorf("failed to list eips: %w", err)
		}
		inUse := slices.ContainsFunc(eips, func(eip *kubeovnv1.IptablesEIP) bool {
			return eip.Spec.QoSPolicy == key || eip.Status.QoSPolicy == key
		})
		if !inUse {
			gateways, err := c.vpcNatGatewayLister.List(labels.Everything())
			if err != nil {
				return fmt.Errorf("failed to list nat gateways: %w", err)
			}
			inUse = slices.ContainsFunc(gateways, func(gateway *kubeovnv1.VpcNatGateway) bool {
				return gateway.Spec.QoSPolicy == key || gateway.Status.QoSPolicy == key
			})
		}

		if inUse {
			// QoS policy is being deleted but still in use.
			// Return nil instead of error to avoid infinite retry loop.
			// The EIP/NatGw controller will remove the reference, which triggers
			// another reconciliation that will eventually delete the finalizer.
			klog.V(3).Infof("qos policy %s is marked for deletion but still in use, waiting for references to be removed", key)
			return nil
		}

		if err = c.handleDelQoSPoliciesFinalizer(key); err != nil {
			klog.Errorf("failed to handle del finalizer for qos %s, %v", key, err)
			return err
		}
		return nil
	}
	if err = c.handleAddQoSPolicyFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for qos, %v", err)
		return err
	}

	if cachedQos.Status.Shared != cachedQos.Spec.Shared ||
		cachedQos.Status.BindingType != cachedQos.Spec.BindingType {
		err := fmt.Errorf("not support qos %s change shared", key)
		klog.Error(err)
		return err
	}

	if err := c.validateQosPolicy(cachedQos); err != nil {
		klog.Errorf("failed to validate qos %s, %v", key, err)
		return err
	}

	added, deleted, updated := diffQoSPolicyBandwidthLimitRules(cachedQos.Status.BandwidthLimitRules, cachedQos.Spec.BandwidthLimitRules)
	bandwidthRulesChanged := len(added) > 0 || len(deleted) > 0 || len(updated) > 0

	if bandwidthRulesChanged {
		klog.V(3).Infof(
			"bandwidth limit rules is changed for qos %s, added: %s, deleted: %s, updated: %s",
			key, added.Strings(), deleted.Strings(), updated.Strings(),
		)
		if cachedQos.Status.Shared {
			err := fmt.Errorf("not support shared qos %s change rule", key)
			klog.Error(err)
			return err
		}

		sortedNewRules := slices.Clone(cachedQos.Spec.BandwidthLimitRules)
		sort.Slice(sortedNewRules, func(i, j int) bool {
			return sortedNewRules[i].Name < sortedNewRules[j].Name
		})

		if cachedQos.Status.BindingType == kubeovnv1.QoSBindingTypeEIP {
			eips, err := c.iptablesEipsLister.List(labels.Everything())
			if err != nil {
				return fmt.Errorf("failed to list eips for QoS policy %s: %w", key, err)
			}
			eips = iptablesEIPsUsingQoS(eips, key)
			switch len(eips) {
			case 0:
			case 1:
				eip := eips[0]
				return c.withNatGwQoSLock(eip.Spec.NatGwDp, func() error {
					if err := c.reconcileEIPBandwidthLimitRulesLocked(eip, added, deleted, updated); err != nil {
						return err
					}
					return c.patchQoSStatus(key, cachedQos.Status.Shared, cachedQos.Status.BindingType, sortedNewRules)
				})
			default:
				return fmt.Errorf("not support qos %s change rule, related eip more than one", key)
			}
		}

		// .Status.Shared and .Status.BindingType are not supported to change.
		if err = c.patchQoSStatus(key, cachedQos.Status.Shared, cachedQos.Status.BindingType, sortedNewRules); err != nil {
			return fmt.Errorf("failed to patch status for qos %s: %w", key, err)
		}
	}
	return nil
}

func (c *Controller) handleDelQoSPolicy(key string) error {
	klog.V(3).Infof("deleted qos policy %s", key)
	return nil
}

func (c *Controller) handleAddQoSPolicyFinalizer(key string) error {
	cachedQoSPolicy, err := c.qosPoliciesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if !cachedQoSPolicy.DeletionTimestamp.IsZero() || controllerutil.ContainsFinalizer(cachedQoSPolicy, util.KubeOVNControllerFinalizer) {
		return nil
	}
	newQoSPolicy := cachedQoSPolicy.DeepCopy()
	controllerutil.AddFinalizer(newQoSPolicy, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedQoSPolicy, newQoSPolicy)
	if err != nil {
		klog.Errorf("failed to generate patch payload for qos '%s', %v", cachedQoSPolicy.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().QoSPolicies().Patch(context.Background(), cachedQoSPolicy.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for qos '%s', %v", cachedQoSPolicy.Name, err)
		return err
	}
	return nil
}
