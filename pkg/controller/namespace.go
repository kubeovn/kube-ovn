package controller

import (
	"fmt"
	"github.com/alauda/kube-ovn/pkg/util"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	"strings"
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

func (c *Controller) enqueueUpdateNamespace(old, new interface{}) {
	oldNs := old.(*v1.Namespace)
	newNs := new.(*v1.Namespace)
	if oldNs.Annotations[util.PrivateSwitchAnnotation] != newNs.Annotations[util.PrivateSwitchAnnotation] ||
		oldNs.Annotations[util.AllowAccessAnnotation] != newNs.Annotations[util.AllowAccessAnnotation] {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
			utilruntime.HandleError(err)
			return
		}
		c.updateNamespaceQueue.AddRateLimited(key)
	}
}

func (c *Controller) runAddNamespaceWorker() {
	for c.processNextAddNamespaceWorkItem() {
	}
}

func (c *Controller) runDeleteNamespaceWorker() {
	for c.processNextDeleteNamespaceWorkItem() {
	}
}

func (c *Controller) runUpdateNamespaceWorker() {
	for c.processNextUpdateNamespaceWorkItem() {
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

func (c *Controller) processNextUpdateNamespaceWorkItem() bool {
	obj, shutdown := c.updateNamespaceQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateNamespaceQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateNamespaceQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateNamespace(key); err != nil {
			c.updateNamespaceQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateNamespaceQueue.Forget(obj)
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
	private := ns.Annotations[util.PrivateSwitchAnnotation]
	allow := ns.Annotations[util.AllowAccessAnnotation]

	if ls == "" {
		klog.Infof("namespace %s use default logical switch %s", key, c.config.DefaultLogicalSwitch)
		return nil
	}
	if err != nil {
		return err
	}

	// skip creation if switch already exists
	exist := false
	ss, err := c.ovnClient.ListLogicalSwitch()
	if err != nil {
		return err
	}
	for _, s := range ss {
		if ls == s {
			exist = true
			break
		}
	}
	if !exist {
		if cidr == "" || gateway == "" {
			return fmt.Errorf("cidr and gateway are required for namespace %s", key)
		}
		if excludeIps == "" {
			excludeIps = gateway
		}
		// If multiple namespace use same ls name, only first one will success
		err = c.ovnClient.CreateLogicalSwitch(ls, cidr, gateway, excludeIps)
		if err != nil {
			return err
		}
	}

	if private == "true" {
		return c.ovnClient.SetPrivateLogicalSwitch(ls, strings.Split(allow, ","))
	} else {
		return c.ovnClient.CleanLogicalSwitchAcl(ls)
	}
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
			err = c.ovnClient.CleanLogicalSwitchAcl(ls)
			if err != nil {
				klog.Errorf("failed to delete acl of logical switch %s %v", ls, err)
				return err
			}
			err = c.ovnClient.DeleteLogicalSwitch(ls)
			if err != nil {
				klog.Errorf("failed to delete logical switch %s %v", ls, err)
				return err
			}
		}
	}

	return nil
}

func (c *Controller) handleUpdateNamespace(key string) error {
	ns, err := c.namespacesLister.Get(key)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	ls := ns.Annotations[util.LogicalSwitchAnnotation]
	if ls == "" {
		return nil
	}
	private := ns.Annotations[util.PrivateSwitchAnnotation]
	allow := ns.Annotations[util.AllowAccessAnnotation]
	if private != "true" {
		return c.ovnClient.CleanLogicalSwitchAcl(ls)
	} else {
		return c.ovnClient.SetPrivateLogicalSwitch(ls, strings.Split(allow, ","))
	}
}
