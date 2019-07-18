package controller

import (
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/ovs"
	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/juju/errors"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

func (c *Controller) enqueueAddPod(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	p := obj.(*v1.Pod)
	if p.Annotations[util.IpPoolAnnotation] != "" && p.Annotations[util.IpAddressAnnotation] == "" {
		klog.V(3).Infof("enqueue add ip pool address pod %s", key)
		c.addIpPoolPodQueue.AddRateLimited(key)
		return
	}

	klog.V(3).Infof("enqueue add pod %s", key)
	c.addPodQueue.AddRateLimited(key)

	// TODO: we need to find a way to reduce duplicated np added to the queue
	if p.Status.PodIP != "" {
		for _, np := range c.podMatchNetworkPolicies(p) {
			c.updateNpQueue.AddRateLimited(np)
		}
	}
}

func (c *Controller) enqueueDeletePod(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue delete pod %s", key)
	c.deletePodQueue.AddRateLimited(key)

	p := obj.(*v1.Pod)
	for _, np := range c.podMatchNetworkPolicies(p) {
		c.updateNpQueue.AddRateLimited(np)
	}
}

func (c *Controller) enqueueUpdatePod(oldObj, newObj interface{}) {
	if !c.isLeader() {
		return
	}
	oldPod := oldObj.(*v1.Pod)
	newPod := newObj.(*v1.Pod)
	if oldPod.ResourceVersion == newPod.ResourceVersion {
		return
	}
	// pod assigned an ip
	if oldPod.Status.PodIP == "" && newPod.Status.PodIP != "" {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
			utilruntime.HandleError(err)
			return
		}
		klog.V(3).Infof("enqueue update pod %s", key)
		c.updatePodQueue.AddRateLimited(key)
	}

	if oldPod.Status.PodIP != newPod.Status.PodIP {
		for _, np := range c.podMatchNetworkPolicies(newPod) {
			c.updateNpQueue.AddRateLimited(np)
		}
		return
	}

	if !reflect.DeepEqual(oldPod.Labels, newPod.Labels) {
		oldNp := c.podMatchNetworkPolicies(oldPod)
		newNp := c.podMatchNetworkPolicies(newPod)
		for _, np := range util.DiffStringSlice(oldNp, newNp) {
			c.updateNpQueue.AddRateLimited(np)
		}
	}
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runAddPodWorker() {
	for c.processNextAddPodWorkItem() {
	}
}

func (c *Controller) runAddIpPoolPodWorker() {
	for c.processNextAddIpPoolPodWorkItem() {
	}
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runDeletePodWorker() {
	for c.processNextDeletePodWorkItem() {
	}
}

func (c *Controller) runUpdatePodWorker() {
	for c.processNextUpdatePodWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextAddPodWorkItem() bool {
	obj, shutdown := c.addPodQueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.addPodQueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.addPodQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Foo resource to be synced.
		if err := c.handleAddPod(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.addPodQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.addPodQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

func (c *Controller) processNextAddIpPoolPodWorkItem() bool {
	obj, shutdown := c.addIpPoolPodQueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.addIpPoolPodQueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.addIpPoolPodQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Foo resource to be synced.
		if err := c.handleAddIpPoolPod(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.addIpPoolPodQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.addIpPoolPodQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextDeletePodWorkItem() bool {
	obj, shutdown := c.deletePodQueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.deletePodQueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.deletePodQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Foo resource to be synced.
		if err := c.handleDeletePod(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.deletePodQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.deletePodQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

func (c *Controller) processNextUpdatePodWorkItem() bool {
	obj, shutdown := c.updatePodQueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.updatePodQueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.updatePodQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Foo resource to be synced.
		if err := c.handleUpdatePod(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.updatePodQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.updatePodQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

func (c *Controller) handleAddPod(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	pod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		// The Pod resource may no longer exist, in which case we stop
		// processing.
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	klog.Infof("add pod %s/%s", namespace, name)
	if pod.Spec.HostNetwork {
		klog.Infof("pod %s/%s in host network mode no need for ovn process", namespace, name)
		return nil
	}

	subnet, err := c.subnetsLister.Get(c.config.DefaultLogicalSwitch)
	if err != nil {
		klog.Errorf("failed to get default subnet %v", err)
		return err
	}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}

	for _, s := range subnets {
		for _, ns := range s.Spec.Namespaces {
			if ns == pod.Namespace {
				subnet = s
				break
			}
		}
	}

	if err := util.ValidatePodNetwork(pod.Annotations); err != nil {
		klog.Errorf("validate pod %s/%s failed, %v", namespace, name, err)
		c.recorder.Eventf(pod, v1.EventTypeWarning, "ValidatePodNetworkFailed", err.Error())
		return err
	}

	// pod address info may already exist in ovn
	ip := pod.Annotations[util.IpAddressAnnotation]
	mac := pod.Annotations[util.MacAddressAnnotation]

	nic, err := c.ovnClient.CreatePort(subnet.Name, ovs.PodNameToPortName(name, namespace), ip, mac)
	if err != nil {
		return err
	}

	op := "replace"
	if len(pod.Annotations) == 0 {
		op = "add"
	}
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[util.IpAddressAnnotation] = nic.IpAddress
	pod.Annotations[util.MacAddressAnnotation] = nic.MacAddress
	pod.Annotations[util.CidrAnnotation] = subnet.Spec.CIDRBlock
	pod.Annotations[util.GatewayAnnotation] = subnet.Spec.Gateway
	pod.Annotations[util.LogicalSwitchAnnotation] = subnet.Name

	patchPayloadTemplate :=
		`[{
        "op": "%s",
        "path": "/metadata/annotations",
        "value": %s
    }]`

	raw, _ := json.Marshal(pod.Annotations)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	_, err = c.config.KubeClient.CoreV1().Pods(namespace).Patch(name, types.JSONPatchType, []byte(patchPayload))
	if err != nil {
		klog.Errorf("patch pod %s/%s failed %v", name, namespace, err)
	}
	return err
}

func (c *Controller) handleAddIpPoolPod(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	pod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		// The Pod resource may no longer exist, in which case we stop
		// processing.
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	klog.Infof("add ip pool address pod %s/%s", namespace, name)
	if pod.Spec.HostNetwork {
		klog.Infof("pod %s/%s in host network mode no need for ovn process", namespace, name)
		return nil
	}

	subnet, err := c.subnetsLister.Get(c.config.DefaultLogicalSwitch)
	if err != nil {
		klog.Errorf("failed to get default subnet %v", err)
		return err
	}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}

	for _, s := range subnets {
		for _, ns := range s.Spec.Namespaces {
			if ns == pod.Namespace {
				subnet = s
				break
			}
		}
	}

	ipPoolAnnotation := pod.Annotations[util.IpPoolAnnotation]

	if ipPoolAnnotation != "" && pod.Annotations[util.IpAddressAnnotation] == "" {
		ipPool := strings.Split(pod.Annotations[util.IpPoolAnnotation], ",")

		if isStatefulSetPod(pod) {
			numIndex := len(strings.Split(pod.Name, "-")) - 1
			numStr := strings.Split(pod.Name, "-")[numIndex]
			index, _ := strconv.Atoi(numStr)
			if index < len(ipPool) {
				pod.Annotations[util.IpAddressAnnotation] = ipPool[index]
			}
		} else {
			for _, ip := range ipPool {
				if net.ParseIP(ip) == nil {
					continue
				}
				pods, err := c.config.KubeClient.CoreV1().Pods(v1.NamespaceAll).List(metav1.ListOptions{})
				if err != nil {
					klog.Errorf("failed to list pod %v", err)
					return err
				}
				used := false
				for _, existPod := range pods.Items {
					// use annotation to get exist ips, as podIp may not exist in this interval
					if strings.Split(existPod.Annotations[util.IpAddressAnnotation], "/")[0] == ip {
						used = true
						break
					}
				}
				if !used {
					pod.Annotations[util.IpAddressAnnotation] = ip
					break
				}
			}
		}
		if pod.Annotations[util.IpAddressAnnotation] == "" {
			klog.Errorf("no unused ip for pod %s", key)
			c.recorder.Event(pod, v1.EventTypeWarning, "FailedAllocateIP", "no unused ip")
			return fmt.Errorf("no unused ip for pod %s", key)
		}
	}

	// pod address info may already exist in ovn
	ip := pod.Annotations[util.IpAddressAnnotation]
	mac := pod.Annotations[util.MacAddressAnnotation]
	nic, err := c.ovnClient.CreatePort(subnet.Name, ovs.PodNameToPortName(name, namespace), ip, mac)
	if err != nil {
		return err
	}

	op := "replace"
	if len(pod.Annotations) == 0 {
		op = "add"
	}
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[util.IpAddressAnnotation] = nic.IpAddress
	pod.Annotations[util.MacAddressAnnotation] = nic.MacAddress
	pod.Annotations[util.CidrAnnotation] = subnet.Spec.CIDRBlock
	pod.Annotations[util.GatewayAnnotation] = subnet.Spec.Gateway
	pod.Annotations[util.LogicalSwitchAnnotation] = subnet.Name

	patchPayloadTemplate :=
		`[{
        "op": "%s",
        "path": "/metadata/annotations",
        "value": %s
    }]`

	raw, _ := json.Marshal(pod.Annotations)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	_, err = c.config.KubeClient.CoreV1().Pods(namespace).Patch(name, types.JSONPatchType, []byte(patchPayload))
	if err != nil {
		klog.Errorf("patch pod %s/%s failed %v", name, namespace, err)
	}
	return err
}

func (c *Controller) handleDeletePod(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	klog.Infof("delete pod %s/%s", namespace, name)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	portAddr, err := c.ovnClient.GetPortAddr(ovs.PodNameToPortName(name, namespace))
	if err != nil {
		if !strings.Contains(err.Error(), "no row") {
			return err
		}
	} else {
		if err := c.ovnClient.DeleteStaticRouter(portAddr[1], c.config.ClusterRouter); err != nil {
			return err
		}
	}
	pod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		// The Pod resource may no longer exist, in this case we stop
		// processing.
		if k8serrors.IsNotFound(err) {
			return c.ovnClient.DeletePort(ovs.PodNameToPortName(name, namespace))
		}
		return err
	}

	if pod.Spec.HostNetwork {
		klog.Infof("pod %s/%s in host network mode no need for ovn process", pod.Namespace, pod.Name)
		return nil
	}

	// for statefulset pod, names are same when updating, so double check to make sure the pod is to be deleted
	if pod.DeletionTimestamp != nil {
		return c.ovnClient.DeletePort(ovs.PodNameToPortName(name, namespace))
	}

	return nil
}

func (c *Controller) handleUpdatePod(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	pod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		// The Pod resource may no longer exist, in which case we stop
		// processing.
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	klog.Infof("update pod %s/%s", namespace, name)
	if pod.Spec.HostNetwork {
		klog.Infof("pod %s/%s in host network mode no need for ovn process", namespace, name)
		return nil
	}

	subnet, err := c.subnetsLister.Get(c.config.DefaultLogicalSwitch)
	if err != nil {
		klog.Errorf("failed to get default subnet %v", err)
		return err
	}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}

	for _, s := range subnets {
		for _, ns := range s.Spec.Namespaces {
			if ns == pod.Namespace {
				subnet = s
				break
			}
		}
	}

	switch subnet.Spec.GatewayType {
	case "", kubeovnv1.GWDistributedType:
		node, err := c.nodesLister.Get(pod.Spec.NodeName)
		if err != nil {
			klog.Errorf("get node %s failed %v", pod.Spec.NodeName, err)
			return err
		}
		nodeTunlIPAddr, err := getNodeTunlIP(node)
		if err != nil {
			return err
		}
		if err := c.ovnClient.AddStaticRouter(ovs.PolicySrcIP, pod.Status.PodIP, nodeTunlIPAddr.String(), c.config.ClusterRouter); err != nil {
			return errors.Annotate(err, "add static route failed")
		}
	case kubeovnv1.GWCentralizedType:
		node, err := c.nodesLister.Get(subnet.Spec.GatewayNode)
		if err != nil {
			klog.Errorf("get node %s failed %v", pod.Spec.NodeName, err)
			return err
		}
		nodeTunlIPAddr, err := getNodeTunlIP(node)
		if err != nil {
			return err
		}
		if err := c.ovnClient.AddStaticRouter(ovs.PolicySrcIP, pod.Status.PodIP, nodeTunlIPAddr.String(), c.config.ClusterRouter); err != nil {
			return errors.Annotate(err, "add static route failed")
		}
	}
	return nil
}

func isStatefulSetPod(pod *v1.Pod) bool {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "StatefulSet" {
			return true
		}
	}
	return false
}

func getNodeTunlIP(node *v1.Node) (net.IP, error) {
	nodeTunlIP := node.Annotations[util.IpAddressAnnotation]
	if nodeTunlIP == "" {
		return nil, errors.New("node has no tunl ip annotation")
	}
	nodeTunlIPAddr := net.ParseIP(nodeTunlIP)
	if nodeTunlIPAddr == nil {
		return nil, fmt.Errorf("failed to parse node tunl ip %s", nodeTunlIP)
	}
	return nodeTunlIPAddr, nil
}
