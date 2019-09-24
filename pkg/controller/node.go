package controller

import (
	"encoding/json"
	"fmt"
	"strings"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

func (c *Controller) enqueueAddNode(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add node %s", key)
	c.addNodeQueue.AddRateLimited(key)
}

func nodeReady(node *v1.Node) bool {
	for _, con := range node.Status.Conditions {
		if con.Type == v1.NodeReady && con.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

func (c *Controller) enqueueUpdateNode(oldObj, newObj interface{}) {
	if !c.isLeader() {
		return
	}

	oldNode := oldObj.(*v1.Node)
	newNode := newObj.(*v1.Node)

	if nodeReady(oldNode) != nodeReady(newNode) {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
			utilruntime.HandleError(err)
			return
		}
		klog.V(3).Infof("enqueue update node %s", key)
		c.updateNodeQueue.AddRateLimited(key)
	}
}

func (c *Controller) enqueueDeleteNode(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue delete node %s", key)
	c.deleteNodeQueue.AddRateLimited(key)
}

func (c *Controller) runAddNodeWorker() {
	for c.processNextAddNodeWorkItem() {
	}
}

func (c *Controller) runUpdateNodeWorker() {
	for c.processNextUpdateNodeWorkItem() {
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

func (c *Controller) processNextUpdateNodeWorkItem() bool {
	obj, shutdown := c.updateNodeQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateNodeQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateNodeQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateNode(key); err != nil {
			c.updateNodeQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateNodeQueue.Forget(obj)
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

	nic, err := c.ovnClient.CreatePort(
		c.config.NodeSwitch, fmt.Sprintf("node-%s", key),
		node.Annotations[util.IpAddressAnnotation],
		node.Annotations[util.CidrAnnotation],
		node.Annotations[util.MacAddressAnnotation])
	if err != nil {
		return err
	}

	nodeAddr := getNodeInternalIP(node)
	if util.CheckProtocol(nodeAddr) == util.CheckProtocol(nic.IpAddress) {
		err = c.ovnClient.AddStaticRouter("", nodeAddr, strings.Split(nic.IpAddress, "/")[0], c.config.ClusterRouter)
		if err != nil {
			klog.Errorf("failed to add static router from node to ovn0 %v", err)
			return err
		}
	}

	subnet, err := c.subnetsLister.Get(c.config.NodeSwitch)
	if err != nil {
		klog.Errorf("failed to get node subnet %v", err)
		return err
	}

	patchPayloadTemplate :=
		`[{
        "op": "%s",
        "path": "/metadata/annotations",
        "value": %s
    }]`
	payload := map[string]string{
		util.IpAddressAnnotation:     nic.IpAddress,
		util.MacAddressAnnotation:    nic.MacAddress,
		util.CidrAnnotation:          subnet.Spec.CIDRBlock,
		util.GatewayAnnotation:       subnet.Spec.Gateway,
		util.LogicalSwitchAnnotation: c.config.NodeSwitch,
		util.PortNameAnnotation:      fmt.Sprintf("node-%s", key),
	}
	raw, _ := json.Marshal(payload)
	op := "replace"
	if len(node.Annotations) == 0 {
		op = "add"
	}
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	_, err = c.config.KubeClient.CoreV1().Nodes().Patch(key, types.JSONPatchType, []byte(patchPayload))
	if err != nil {
		klog.Errorf("patch node %s failed %v", key, err)
		return err
	}

	ipCr, err := c.config.KubeOvnClient.KubeovnV1().IPs().Get(key, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err := c.config.KubeOvnClient.KubeovnV1().IPs().Create(&kubeovnv1.IP{
				ObjectMeta: metav1.ObjectMeta{
					Name: key,
					Labels: map[string]string{
						util.SubnetNameLabel: c.config.NodeSwitch,
					},
				},
				Spec: kubeovnv1.IPSpec{
					PodName:    key,
					Subnet:     c.config.NodeSwitch,
					NodeName:   key,
					IPAddress:  nic.IpAddress,
					MacAddress: nic.MacAddress,
				},
			})
			if err != nil {
				errMsg := fmt.Errorf("failed to create ip crd for %s, %v", nic.IpAddress, err)
				klog.Error(errMsg)
				return errMsg
			}
		} else {
			errMsg := fmt.Errorf("failed to get ip crd for %s, %v", nic.IpAddress, err)
			klog.Error(errMsg)
			return errMsg
		}
	} else {
		if ipCr.Labels != nil {
			ipCr.Labels[util.SubnetNameLabel] = c.config.NodeSwitch
		} else {
			ipCr.Labels = map[string]string{
				util.SubnetNameLabel: c.config.NodeSwitch,
			}
		}
		ipCr.Spec.PodName = key
		ipCr.Spec.Namespace = ""
		ipCr.Spec.Subnet = c.config.NodeSwitch
		ipCr.Spec.NodeName = key
		ipCr.Spec.IPAddress = nic.IpAddress
		ipCr.Spec.MacAddress = nic.MacAddress
		ipCr.Spec.ContainerID = ""
		_, err := c.config.KubeOvnClient.KubeovnV1().IPs().Update(ipCr)
		if err != nil {
			errMsg := fmt.Errorf("failed to create ip crd for %s, %v", nic.IpAddress, err)
			klog.Error(errMsg)
			return errMsg
		}
	}

	return err
}

func (c *Controller) handleDeleteNode(key string) error {
	err := c.ovnClient.DeletePort(fmt.Sprintf("node-%s", key))
	if err != nil {
		klog.Infof("failed to delete node switch port node-%s %v", key, err)
		return err
	}

	ipCr, err := c.config.KubeOvnClient.KubeovnV1().IPs().Get(key, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if err := c.ovnClient.DeleteStaticRouter(ipCr.Spec.IPAddress, c.config.ClusterRouter); err != nil {
		return err
	}

	err = c.config.KubeOvnClient.KubeovnV1().IPs().Delete(key, &metav1.DeleteOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func (c *Controller) handleUpdateNode(key string) error {
	node, err := c.nodesLister.Get(key)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get subnets %v", err)
		return err
	}

	if nodeReady(node) {
		for _, subnet := range subnets {
			if subnet.Status.ActivateGateway == "" && gatewayContains(subnet.Spec.GatewayNode, node.Name) {
				if err := c.reconcileCentralizedGateway(subnet); err != nil {
					return err
				}
			}
		}
	} else {
		for _, subnet := range subnets {
			if subnet.Status.ActivateGateway == node.Name {
				if err := c.reconcileCentralizedGateway(subnet); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func gatewayContains(gatewayNodeStr, gateway string) bool {
	for _, gw := range strings.Split(gatewayNodeStr, ",") {
		gw = strings.TrimSpace(gw)
		if gw == gateway {
			return true
		}
	}
	return false
}

func getNodeInternalIP(node *v1.Node) string {
	var nodeAddr string
	for _, addr := range node.Status.Addresses {
		if addr.Type == v1.NodeInternalIP {
			nodeAddr = addr.Address
			break
		}
	}
	return nodeAddr
}
