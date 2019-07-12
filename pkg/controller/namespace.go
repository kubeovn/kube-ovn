package controller

import (
	"github.com/alauda/kube-ovn/pkg/util"
	v1 "k8s.io/api/core/v1"
	"reflect"
)

func (c *Controller) enqueueAddNamespace(obj interface{}) {
	if !c.isLeader() {
		return
	}
	ns := obj.(*v1.Namespace)
	for _, np := range c.namespaceMatchNetworkPolicies(ns) {
		c.updateNpQueue.AddRateLimited(np)
	}
}

func (c *Controller) enqueueDeleteNamespace(obj interface{}) {
	if !c.isLeader() {
		return
	}

	ns := obj.(*v1.Namespace)
	for _, np := range c.namespaceMatchNetworkPolicies(ns) {
		c.updateNpQueue.AddRateLimited(np)
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
			c.updateNpQueue.AddRateLimited(np)
		}
	}
}
