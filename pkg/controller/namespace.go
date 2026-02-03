package controller

import (
	"maps"
	"slices"
	"strings"

	"github.com/scylladb/go-set/strset"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddNamespace(obj any) {
	if c.config.EnableNP {
		for _, np := range c.namespaceMatchNetworkPolicies(obj.(*v1.Namespace)) {
			c.updateNpQueue.Add(np)
		}
	}

	key := cache.MetaObjectToName(obj.(*v1.Namespace)).String()
	c.addNamespaceQueue.Add(key)
}

func (c *Controller) enqueueDeleteNamespace(obj any) {
	var ns *v1.Namespace
	switch t := obj.(type) {
	case *v1.Namespace:
		ns = t
	case cache.DeletedFinalStateUnknown:
		n, ok := t.Obj.(*v1.Namespace)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		ns = n
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	if c.config.EnableNP {
		for _, np := range c.namespaceMatchNetworkPolicies(ns) {
			c.updateNpQueue.Add(np)
		}
	}
	if c.config.EnableANP {
		c.updateAnpsByLabelsMatch(ns.Labels, nil)
		c.updateCnpsByLabelsMatch(ns.Labels, nil)
	}
}

func (c *Controller) enqueueUpdateNamespace(oldObj, newObj any) {
	oldNs := oldObj.(*v1.Namespace)
	newNs := newObj.(*v1.Namespace)
	if oldNs.ResourceVersion == newNs.ResourceVersion {
		return
	}

	if !maps.Equal(oldNs.Labels, newNs.Labels) {
		if c.config.EnableNP {
			oldNp := c.namespaceMatchNetworkPolicies(oldNs)
			newNp := c.namespaceMatchNetworkPolicies(newNs)
			for _, np := range util.DiffStringSlice(oldNp, newNp) {
				c.updateNpQueue.Add(np)
			}
		}

		if c.config.EnableANP {
			c.updateAnpsByLabelsMatch(newObj.(*v1.Namespace).Labels, nil)
			c.updateCnpsByLabelsMatch(newObj.(*v1.Namespace).Labels, nil)
		}

		expectSubnets, err := c.getNsExpectSubnets(newNs)
		if err != nil {
			klog.Errorf("failed to list expected subnets for namespace %s, %v", newNs.Name, err)
			return
		}

		expectSubnetsSet := strset.New(expectSubnets...)
		existSubnetsSet := strset.New(strings.Split(newNs.Annotations[util.LogicalSwitchAnnotation], ",")...)
		if !expectSubnetsSet.IsEqual(existSubnetsSet) {
			c.addNamespaceQueue.Add(newNs.Name)
		}
	}

	// in case annotations are removed by other controllers
	if newNs.Annotations == nil || newNs.Annotations[util.LogicalSwitchAnnotation] == "" {
		klog.Warningf("no logical switch annotation for ns %s", newNs.Name)
		c.addNamespaceQueue.Add(newNs.Name)
	}
}

func (c *Controller) handleAddNamespace(key string) error {
	c.nsKeyMutex.LockKey(key)
	defer func() { _ = c.nsKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add/update namespace %s", key)

	cachedNs, err := c.namespacesLister.Get(key)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	namespace := cachedNs.DeepCopy()

	var ls string
	var lss, cidrs, excludeIps, ipPoolsAnnotation []string
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}
	ipPoolList, err := c.ippoolLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ippools: %v", err)
		return err
	}

	// check if subnet bind ns
	for _, s := range subnets {
		if slices.Contains(s.Spec.Namespaces, key) {
			lss = append(lss, s.Name)
			cidrs = append(cidrs, s.Spec.CIDRBlock)
			excludeIps = append(excludeIps, strings.Join(s.Spec.ExcludeIps, ","))
		}

		// bind subnet with namespaceLabelSelector which select the namespace
		for _, nsSelector := range s.Spec.NamespaceSelectors {
			matchSelector, err := metav1.LabelSelectorAsSelector(&nsSelector)
			if err != nil {
				klog.Errorf("failed to convert label selector, %v", err)
				return err
			}

			if matchSelector.Matches(labels.Set(namespace.Labels)) {
				if slices.Contains(lss, s.Name) {
					break
				}
				lss = append(lss, s.Name)
				cidrs = append(cidrs, s.Spec.CIDRBlock)
				excludeIps = append(excludeIps, strings.Join(s.Spec.ExcludeIps, ","))
				break
			}
		}

		// check if subnet is in custom vpc with configured defaultSubnet, then annotate the namespace with this subnet
		if s.Spec.Vpc != "" && s.Spec.Vpc != c.config.ClusterRouter {
			vpc, err := c.vpcsLister.Get(s.Spec.Vpc)
			if err != nil {
				if errors.IsNotFound(err) {
					// this subnet is broken (it references a non-existent VPC) - we just ignore it.
					klog.Errorf("vpc %q is not found. Ignoring subnet %q: %v", s.Spec.Vpc, s.Name, err)
					break
				}
				klog.Errorf("failed to get vpc %q: %v", s.Spec.Vpc, err)
				return err
			}
			if s.Name == vpc.Spec.DefaultSubnet {
				if slices.Contains(vpc.Spec.Namespaces, key) && key != metav1.NamespaceSystem {
					lss = append([]string{s.Name}, lss...)
				}
			}
		}
	}

	for _, ipPool := range ipPoolList {
		if slices.Contains(ipPool.Spec.Namespaces, key) {
			ipPoolsAnnotation = append(ipPoolsAnnotation, ipPool.Name)
		}
	}

	if lss == nil {
		// If NS does not belong to any custom VPC, then this NS belongs to the default VPC
		vpc, err := c.vpcsLister.Get(c.config.ClusterRouter)
		if err != nil {
			klog.Errorf("failed to get default vpc %v", err)
			return err
		}
		vpcs, err := c.vpcsLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list vpc %v", err)
			return err
		}
		for _, v := range vpcs {
			if slices.Contains(v.Spec.Namespaces, key) {
				vpc = v
				break
			}
		}

		if vpc.Status.DefaultLogicalSwitch != "" {
			ls = vpc.Status.DefaultLogicalSwitch
		} else {
			ls = c.config.DefaultLogicalSwitch
		}
		subnet, err := c.subnetsLister.Get(ls)
		if err != nil {
			klog.Errorf("failed to get default subnet %v", err)
			return err
		}
		lss = append(lss, subnet.Name)
		cidrs = append(cidrs, subnet.Spec.CIDRBlock)
		excludeIps = append(excludeIps, strings.Join(subnet.Spec.ExcludeIps, ","))
	}

	if namespace.Annotations[util.LogicalSwitchAnnotation] == strings.Join(lss, ",") &&
		namespace.Annotations[util.CidrAnnotation] == strings.Join(cidrs, ";") &&
		namespace.Annotations[util.ExcludeIpsAnnotation] == strings.Join(excludeIps, ";") &&
		namespace.Annotations[util.IPPoolAnnotation] == strings.Join(ipPoolsAnnotation, ",") {
		return nil
	}

	patch := util.KVPatch{
		util.LogicalSwitchAnnotation: strings.Join(lss, ","),
		util.CidrAnnotation:          strings.Join(cidrs, ";"),
		util.ExcludeIpsAnnotation:    strings.Join(excludeIps, ";"),
	}

	if len(ipPoolsAnnotation) == 0 {
		patch[util.IPPoolAnnotation] = nil
	} else {
		patch[util.IPPoolAnnotation] = strings.Join(ipPoolsAnnotation, ",")
	}

	if err = util.PatchAnnotations(c.config.KubeClient.CoreV1().Namespaces(), key, patch); err != nil {
		klog.Errorf("patch namespace %s failed %v", key, err)
	}
	return err
}

func (c *Controller) getNsExpectSubnets(newNs *v1.Namespace) ([]string, error) {
	var expectSubnets []string

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return expectSubnets, err
	}
	for _, subnet := range subnets {
		// ns labels match subnet's selector
		for _, nsSelector := range subnet.Spec.NamespaceSelectors {
			matchSelector, err := metav1.LabelSelectorAsSelector(&nsSelector)
			if err != nil {
				klog.Errorf("failed to convert label selector, %v", err)
				return expectSubnets, err
			}

			if matchSelector.Matches(labels.Set(newNs.Labels)) {
				expectSubnets = append(expectSubnets, subnet.Name)
				break
			}
		}

		// ns included in subnet's namespaces
		if slices.Contains(subnet.Spec.Namespaces, newNs.Name) && !slices.Contains(expectSubnets, subnet.Name) {
			expectSubnets = append(expectSubnets, subnet.Name)
		}
	}

	return expectSubnets, nil
}
