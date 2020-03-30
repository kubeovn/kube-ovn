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
	c.addNodeQueue.Add(key)
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
		c.updateNodeQueue.Add(key)
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
	c.deleteNodeQueue.Add(key)
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

	subnet, err := c.subnetsLister.Get(c.config.NodeSwitch)
	if err != nil {
		klog.Errorf("failed to get node subnet %v", err)
		return err
	}

	var ip, mac string
	portName := fmt.Sprintf("node-%s", key)
	if node.Annotations[util.AllocatedAnnotation] == "true" {
		return nil
	} else {
		ip, mac, err = c.ipam.GetRandomAddress(portName, c.config.NodeSwitch)
		if err != nil {
			return err
		}
	}
	if err := c.ovnClient.CreatePort(c.config.NodeSwitch, portName, ip, subnet.Spec.CIDRBlock, mac); err != nil {
		return err
	}

	nodeAddr := getNodeInternalIP(node)
	if util.CheckProtocol(nodeAddr) == util.CheckProtocol(ip) {
		err = c.ovnClient.AddStaticRoute("", nodeAddr, strings.Split(ip, "/")[0], c.config.ClusterRouter)
		if err != nil {
			klog.Errorf("failed to add static router from node to ovn0 %v", err)
			return err
		}
	}

	//set node port tag and set address
	if c.config.NetworkType != util.NetworkTypeVlan && subnet.Spec.Vlan != "" {
		if err := c.addPortVlan(portName, ip, mac, subnet.Spec.Vlan); err != nil {
			return err
		}
	}

	patchPayloadTemplate :=
		`[{
        "op": "%s",
        "path": "/metadata/annotations",
        "value": %s
    }]`
	payload := map[string]string{
		util.IpAddressAnnotation:     ip,
		util.MacAddressAnnotation:    mac,
		util.CidrAnnotation:          subnet.Spec.CIDRBlock,
		util.GatewayAnnotation:       subnet.Spec.Gateway,
		util.LogicalSwitchAnnotation: c.config.NodeSwitch,
		util.AllocatedAnnotation:     "true",
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

	ipCr, err := c.config.KubeOvnClient.KubeovnV1().IPs().Get(fmt.Sprintf("node-%s", key), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err := c.config.KubeOvnClient.KubeovnV1().IPs().Create(&kubeovnv1.IP{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("node-%s", key),
					Labels: map[string]string{
						util.SubnetNameLabel: c.config.NodeSwitch,
						c.config.NodeSwitch:  "",
					},
				},
				Spec: kubeovnv1.IPSpec{
					PodName:    key,
					Subnet:     c.config.NodeSwitch,
					NodeName:   key,
					IPAddress:  ip,
					MacAddress: mac,
				},
			})
			if err != nil {
				errMsg := fmt.Errorf("failed to create ip crd for %s, %v", ip, err)
				klog.Error(errMsg)
				return errMsg
			}
		} else {
			errMsg := fmt.Errorf("failed to get ip crd for %s, %v", ip, err)
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
		ipCr.Spec.IPAddress = ip
		ipCr.Spec.MacAddress = mac
		ipCr.Spec.ContainerID = ""
		_, err := c.config.KubeOvnClient.KubeovnV1().IPs().Update(ipCr)
		if err != nil {
			errMsg := fmt.Errorf("failed to create ip crd for %s, %v", ip, err)
			klog.Error(errMsg)
			return errMsg
		}
	}

	return nil
}

func (c *Controller) handleDeleteNode(key string) error {
	portName := fmt.Sprintf("node-%s", key)
	if err := c.ovnClient.DeletePort(portName); err != nil {
		klog.Errorf("failed to delete node switch port node-%s %v", key, err)
		return err
	}
	if err := c.ovnClient.DeleteChassis(key); err != nil {
		klog.Errorf("failed to delete chassis for node %s %v", key, err)
		return err
	}

	if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(portName, &metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	ips, _ := c.ipam.GetPodAddress(portName)
	if len(ips) > 0 {
		if err := c.ovnClient.DeleteStaticRouteByNextHop(ips[0]); err != nil {
			return err
		}
	}

	c.ipam.ReleaseAddressByPod(portName)
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
