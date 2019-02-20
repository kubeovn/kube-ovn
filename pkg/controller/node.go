package controller

import (
	"encoding/json"
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

func (c *Controller) enqueueAddNode(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.addNodeQueue.AddRateLimited(key)
}

func (c *Controller) enqueueDeleteNode(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.deleteNodeQueue.AddRateLimited(key)
}

func (c *Controller) runAddNodeWorker() {
	for c.processNextAddNodeWorkItem() {
	}
}

func (c *Controller) runDeleteNodeWorker() {
	for c.processNextDeleteNodeWorkItem() {
	}
}

func (c *Controller) processNextAddNodeWorkItem() bool {
	obj, shutdown := c.addNodeQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addNodeQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addNodeQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddNode(key); err != nil {
			c.addNodeQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addNodeQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteNodeWorkItem() bool {
	obj, shutdown := c.deleteNodeQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.deleteNodeQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.deleteNodeQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeleteNode(key); err != nil {
			c.deleteNodeQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.deleteNodeQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddNode(key string) error {
	node, err := c.nodesLister.Get(key)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	nic, err := c.ovnClient.CreatePort(c.config.NodeSwitch, fmt.Sprintf("node-%s", key), "", "")
	if err != nil {
		return err
	}

	patchPayloadTemplate :=
		`[{
        "op": "%s",
        "path": "/metadata/annotations",
        "value": %s
    }]`
	payload := map[string]string{
		"ovn.kubernetes.io/ip_address":     nic.IpAddress,
		"ovn.kubernetes.io/mac_address":    nic.MacAddress,
		"ovn.kubernetes.io/cidr":           nic.CIDR,
		"ovn.kubernetes.io/gateway":        nic.Gateway,
		"ovn.kubernetes.io/logical_switch": c.config.NodeSwitch,
		"ovn.kubernetes.io/port_name":      fmt.Sprintf("node-%s", key),
	}
	raw, _ := json.Marshal(payload)
	op := "replace"
	if len(node.GetAnnotations()) == 0 {
		op = "add"
	}
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	_, err = c.kubeclientset.CoreV1().Nodes().Patch(key, types.JSONPatchType, []byte(patchPayload))
	if err != nil {
		klog.Errorf("patch node % failed %v", key, err)
	}
	return err
}

func (c *Controller) handleDeleteNode(key string) error {
	err := c.ovnClient.DeletePort(fmt.Sprintf("node-%s", key))
	if err != nil {
		klog.Infof("failed to delete node switch port node-%s %v", key, err)
		return err
	}
	return nil
}
