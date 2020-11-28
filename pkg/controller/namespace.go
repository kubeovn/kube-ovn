package controller

import (
	"fmt"
	"reflect"

	"github.com/alauda/kube-ovn/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

func (c *Controller) enqueueAddNamespace(obj interface{}) {
	if !c.isLeader() {
		return
	}
	ns := obj.(*v1.Namespace)
	for _, np := range c.namespaceMatchNetworkPolicies(ns) {
		c.updateNpQueue.Add(np)
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
	if !c.isLeader() {
		return
	}

	ns := obj.(*v1.Namespace)
	for _, np := range c.namespaceMatchNetworkPolicies(ns) {
		c.updateNpQueue.Add(np)
	}
}

func (c *Controller) enqueueUpdateNamespace(old, new interface{}) {
	if !c.isLeader() {
		return
	}
	oldNs := old.(*v1.Namespace)
	newNs := new.(*v1.Namespace)
	if oldNs.ResourceVersion == newNs.ResourceVersion {
		return
	}

	if !reflect.DeepEqual(oldNs.Labels, newNs.Labels) {
		oldNp := c.namespaceMatchNetworkPolicies(oldNs)
		newNp := c.namespaceMatchNetworkPolicies(newNs)
		for _, np := range util.DiffStringSlice(oldNp, newNp) {
			c.updateNpQueue.Add(np)
		}
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
	namespace, err := c.namespacesLister.Get(key)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	var ls, cidr string
	var excludeIps []string
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}
	// check if subnet bind ns
	for _, s := range subnets {
		for _, ns := range s.Spec.Namespaces {
			if ns == key {
				ls = s.Name
				cidr = s.Spec.CIDRBlock
				excludeIps = s.Spec.ExcludeIps
				break
			}
		}
		if ls != "" {
			break
		}
	}

	if ls == "" {
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
			if util.ContainsString(v.Spec.Namespaces, key) {
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
		ls = subnet.Name
		cidr = subnet.Spec.CIDRBlock
		excludeIps = subnet.Spec.ExcludeIps

	}

	op := "replace"
	if namespace.Annotations == nil || len(namespace.Annotations) == 0 {
		op = "add"
		namespace.Annotations = map[string]string{}
	} else {
		if namespace.Annotations[util.LogicalSwitchAnnotation] == ls &&
			namespace.Annotations[util.CidrAnnotation] == util.DualStackToString(subnet.Spec.CIDRBlock) &&
			namespace.Annotations[util.ExcludeIpsAnnotation] == util.DualStackListToString(subnet.Spec.ExcludeIps) {
			return nil
		}
	}

	namespace.Annotations[util.LogicalSwitchAnnotation] = ls
	namespace.Annotations[util.CidrAnnotation] = util.DualStackToString(subnet.Spec.CIDRBlock)
	namespace.Annotations[util.ExcludeIpsAnnotation] = util.DualStackListToString(subnet.Spec.ExcludeIps)

	if _, err = c.config.KubeClient.CoreV1().Namespaces().Patch(key, types.JSONPatchType, generatePatchPayload(namespace.Annotations, op)); err != nil {
		klog.Errorf("patch namespace %s failed %v", key, err)
	}
	return err
}
