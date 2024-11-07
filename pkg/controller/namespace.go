package controller

import (
	"context"
	"reflect"
	"slices"
	"strings"

	"github.com/scylladb/go-set/strset"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddNamespace(obj interface{}) {
	if c.config.EnableNP {
		for _, np := range c.namespaceMatchNetworkPolicies(obj.(*v1.Namespace)) {
			c.updateNpQueue.Add(np)
		}
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.addNamespaceQueue.Add(key)
}

func (c *Controller) enqueueDeleteNamespace(obj interface{}) {
	if c.config.EnableNP {
		for _, np := range c.namespaceMatchNetworkPolicies(obj.(*v1.Namespace)) {
			c.updateNpQueue.Add(np)
		}
	}
	if c.config.EnableANP {
		c.updateAnpsByLabelsMatch(obj.(*v1.Namespace).Labels, nil)
	}
}

func (c *Controller) enqueueUpdateNamespace(oldObj, newObj interface{}) {
	oldNs := oldObj.(*v1.Namespace)
	newNs := newObj.(*v1.Namespace)
	if oldNs.ResourceVersion == newNs.ResourceVersion {
		return
	}

	if !reflect.DeepEqual(oldNs.Labels, newNs.Labels) {
		if c.config.EnableNP {
			oldNp := c.namespaceMatchNetworkPolicies(oldNs)
			newNp := c.namespaceMatchNetworkPolicies(newNs)
			for _, np := range util.DiffStringSlice(oldNp, newNp) {
				c.updateNpQueue.Add(np)
			}
		}

		if c.config.EnableANP {
			c.updateAnpsByLabelsMatch(newObj.(*v1.Namespace).Labels, nil)
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

	var ls, ippool string
	var lss, cidrs, excludeIps []string
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}
	ippools, err := c.ippoolLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ippools: %v", err)
		return err
	}

	// check if subnet bind ns
	for _, s := range subnets {
		for _, ns := range s.Spec.Namespaces {
			if ns == key {
				lss = append(lss, s.Name)
				cidrs = append(cidrs, s.Spec.CIDRBlock)
				excludeIps = append(excludeIps, strings.Join(s.Spec.ExcludeIps, ","))
				break
			}
		}

		// bind subnet with namespaceLabelSeletcor which select the namespace
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
				klog.Errorf("failed to get custom vpc %v", err)
				return err
			}
			if s.Name == vpc.Spec.DefaultSubnet {
				lss = []string{s.Name}
			}
		}
	}

	for _, p := range ippools {
		if slices.Contains(p.Spec.Namespaces, key) {
			ippool = p.Name
			break
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

	if len(namespace.Annotations) == 0 {
		namespace.Annotations = map[string]string{}
	} else if namespace.Annotations[util.LogicalSwitchAnnotation] == strings.Join(lss, ",") &&
		namespace.Annotations[util.CidrAnnotation] == strings.Join(cidrs, ";") &&
		namespace.Annotations[util.ExcludeIpsAnnotation] == strings.Join(excludeIps, ";") &&
		namespace.Annotations[util.IPPoolAnnotation] == ippool {
		return nil
	}

	namespace.Annotations[util.LogicalSwitchAnnotation] = strings.Join(lss, ",")
	namespace.Annotations[util.CidrAnnotation] = strings.Join(cidrs, ";")
	namespace.Annotations[util.ExcludeIpsAnnotation] = strings.Join(excludeIps, ";")

	if ippool == "" {
		delete(namespace.Annotations, util.IPPoolAnnotation)
	} else {
		namespace.Annotations[util.IPPoolAnnotation] = ippool
	}

	patch, err := util.GenerateStrategicMergePatchPayload(cachedNs, namespace)
	if err != nil {
		klog.Error(err)
		return err
	}
	if _, err = c.config.KubeClient.CoreV1().Namespaces().Patch(context.Background(), key,
		types.StrategicMergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
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
