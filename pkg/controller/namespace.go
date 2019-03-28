package controller

import (
	"bitbucket.org/mathildetech/kube-ovn/pkg/util"
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

func (c *Controller) enqueueAddNamespace(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.addNamespaceQueue.AddRateLimited(key)
}

func (c *Controller) enqueueDeleteNamespace(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.deleteNamespaceQueue.AddRateLimited(key)
}

func (c *Controller) runAddNamespaceWorker() {
	for c.processNextAddNamespaceWorkItem() {
	}
}

func (c *Controller) runDeleteNamespaceWorker() {
	for c.processNextDeleteNamespaceWorkItem() {
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

func (c *Controller) processNextDeleteNamespaceWorkItem() bool {
	obj, shutdown := c.deleteNamespaceQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.deleteNamespaceQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.deleteNamespaceQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeleteNamespace(key); err != nil {
			c.deleteNamespaceQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.deleteNamespaceQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddNamespace(key string) error {
	ns, err := c.namespacesLister.Get(key)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	ls := ns.Annotations[util.LogicalSwitchAnnotation]
	cidr := ns.Annotations[util.CidrAnnotation]
	gateway := ns.Annotations[util.GatewayAnnotation]
	excludeIps := ns.Annotations[util.ExcludeIpsAnnotation]

	if ls == "" {
		klog.Infof("namespace %s use default logical switch %s", key, c.config.DefaultLogicalSwitch)
		return nil
	}
	if err != nil {
		return err
	}

	// return if switch already exists
	ss, err := c.ovnClient.ListLogicalSwitch()
	if err != nil {
		return err
	}
	for _, s := range ss {
		if ls == s {
			return nil
		}
	}

	if cidr == "" || gateway == "" {
		return fmt.Errorf("cidr and gateway are required for namespace %s", key)
	}
	if excludeIps == "" {
		excludeIps = gateway
	}
	// If multiple namespace use same ls name, only first one will success
	return c.ovnClient.CreateLogicalSwitch(ls, cidr, gateway, excludeIps)

}

func (c *Controller) handleDeleteNamespace(key string) error {
	switches, err := c.ovnClient.ListLogicalSwitch()
	if err != nil {
		return err
	}

	namespaces, err := c.namespacesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list namespace %v", err)
		return err
	}

	for _, ls := range switches {
		if ls == c.config.DefaultLogicalSwitch || ls == c.config.NodeSwitch || ls == "transit" || ls == "outside" {
			continue
		}
		found := false
		for _, ns := range namespaces {
			if ns.Annotations[util.LogicalSwitchAnnotation] == ls {
				found = true
				break
			}
		}
		if !found {
			klog.Infof("ls %s should be deleted", ls)
			err = c.ovnClient.DeleteLogicalSwitch(ls)
			if err != nil {
				klog.Errorf("failed to delete logical switch %s %v", ls, err)
				return err
			}
		}
	}

	return nil
}
