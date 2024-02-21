package controller

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

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
}

func (c *Controller) enqueueUpdateNamespace(oldObj, newObj interface{}) {
	oldNs := oldObj.(*v1.Namespace)
	newNs := newObj.(*v1.Namespace)
	if oldNs.ResourceVersion == newNs.ResourceVersion {
		return
	}

	if c.config.EnableNP && !reflect.DeepEqual(oldNs.Labels, newNs.Labels) {
		oldNp := c.namespaceMatchNetworkPolicies(oldNs)
		newNp := c.namespaceMatchNetworkPolicies(newNs)
		for _, np := range util.DiffStringSlice(oldNp, newNp) {
			c.updateNpQueue.Add(np)
		}
	}

	// in case annotations are removed by other controllers
	if newNs.Annotations == nil || newNs.Annotations[util.LogicalSwitchAnnotation] == "" {
		klog.Warningf("no logical switch annotation for ns %s", newNs.Name)
		c.addNamespaceQueue.Add(newNs.Name)
	}

	if newNs.Annotations != nil && newNs.Annotations[util.LogicalSwitchAnnotation] != "" && !reflect.DeepEqual(oldNs.Annotations, newNs.Annotations) {
		c.addNamespaceQueue.Add(newNs.Name)
	}
}

func (c *Controller) runAddNamespaceWorker() {
	for c.processNextAddNamespaceWorkItem() {
	}
}

func (c *Controller) processNextAddNamespaceWorkItem() bool {
	obj, shutdown := c.addNamespaceQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addNamespaceQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addNamespaceQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddNamespace(key); err != nil {
			c.addNamespaceQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addNamespaceQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
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

	if namespace.Annotations == nil || len(namespace.Annotations) == 0 {
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
