package controller

import (
	"encoding/json"
	"fmt"
	"github.com/alauda/kube-ovn/pkg/ipam"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

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
	// TODO: we need to find a way to reduce duplicated np added to the queue
	if p.Status.PodIP != "" {
		for _, np := range c.podMatchNetworkPolicies(p) {
			c.updateNpQueue.Add(np)
		}
	}

	if p.Spec.HostNetwork {
		return
	}
	if p.Status.Phase == v1.PodFailed && p.Status.Reason == "Evicted" {
		c.deletePodQueue.Add(key)
		return
	}

	if p.Annotations[util.IpPoolAnnotation] != "" && p.Annotations[util.IpAddressAnnotation] == "" {
		klog.V(3).Infof("enqueue add ip pool address pod %s", key)
		c.addIpPoolPodQueue.Add(key)
		return
	}

	klog.V(3).Infof("enqueue add pod %s", key)
	c.addPodQueue.Add(key)
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

	p := obj.(*v1.Pod)
	for _, np := range c.podMatchNetworkPolicies(p) {
		c.updateNpQueue.Add(np)
	}
	isStateful, statefulSetName := isStatefulSetPod(p)
	if p.Spec.HostNetwork {
		return
	}
	if !isStateful {
		klog.V(3).Infof("enqueue delete pod %s", key)
		c.deletePodQueue.Add(key)
	} else {
		// only delete statefulset pod lsp when statefulset deleted or down scaled
		ss, err := c.config.KubeClient.AppsV1().StatefulSets(p.Namespace).Get(statefulSetName, metav1.GetOptions{})
		if err != nil {
			// statefulset is deleted
			if k8serrors.IsNotFound(err) {
				c.deletePodQueue.Add(key)
			} else {
				klog.Errorf("failed to get statefulset %v", err)
			}
			return
		}

		// statefulset is deleting
		if ss.DeletionTimestamp != nil {
			c.deletePodQueue.Add(key)
			return
		}

		// down scale statefulset
		numIndex := len(strings.Split(p.Name, "-")) - 1
		numStr := strings.Split(p.Name, "-")[numIndex]
		index, _ := strconv.Atoi(numStr)
		if int32(index) >= *ss.Spec.Replicas {
			c.deletePodQueue.Add(key)
			return
		}
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

	if !reflect.DeepEqual(oldPod.Labels, newPod.Labels) {
		oldNp := c.podMatchNetworkPolicies(oldPod)
		newNp := c.podMatchNetworkPolicies(newPod)
		for _, np := range util.DiffStringSlice(oldNp, newNp) {
			c.updateNpQueue.Add(np)
		}
	}

	if oldPod.Status.PodIP != newPod.Status.PodIP {
		for _, np := range c.podMatchNetworkPolicies(newPod) {
			c.updateNpQueue.Add(np)
		}
	}

	if newPod.Spec.HostNetwork {
		return
	}

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	if newPod.Status.Phase == v1.PodFailed && newPod.Status.Reason == "Evicted" {
		c.deletePodQueue.Add(key)
		return
	}
	// pod assigned an ip
	if oldPod.Status.PodIP != newPod.Status.PodIP {
		klog.V(3).Infof("enqueue update pod %s", key)
		c.updatePodQueue.Add(key)
	}
}

func (c *Controller) runAddPodWorker() {
	for c.processNextAddPodWorkItem() {
	}
}

func (c *Controller) runAddIpPoolPodWorker() {
	for c.processNextAddIpPoolPodWorkItem() {
	}
}

func (c *Controller) runDeletePodWorker() {
	for c.processNextDeletePodWorkItem() {
	}
}

func (c *Controller) runUpdatePodWorker() {
	for c.processNextUpdatePodWorkItem() {
	}
}

func (c *Controller) processNextAddPodWorkItem() bool {
	obj, shutdown := c.addPodQueue.Get()

	if shutdown {
		return false
	}
	now := time.Now()

	err := func(obj interface{}) error {
		defer c.addPodQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addPodQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddPod(key); err != nil {
			c.addPodQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addPodQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	last := time.Since(now)
	klog.Infof("take %d ms to deal with add pod", last.Milliseconds())
	return true
}

func (c *Controller) processNextAddIpPoolPodWorkItem() bool {
	obj, shutdown := c.addIpPoolPodQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addIpPoolPodQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addIpPoolPodQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddPod(key); err != nil {
			c.addIpPoolPodQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addIpPoolPodQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

func (c *Controller) processNextDeletePodWorkItem() bool {
	obj, shutdown := c.deletePodQueue.Get()

	if shutdown {
		return false
	}

	now := time.Now()
	err := func(obj interface{}) error {
		defer c.deletePodQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.deletePodQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeletePod(key); err != nil {
			c.deletePodQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.deletePodQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	last := time.Since(now)
	klog.Infof("take %d ms to deal with delete pod", last.Milliseconds())
	return true
}

func (c *Controller) processNextUpdatePodWorkItem() bool {
	obj, shutdown := c.updatePodQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updatePodQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updatePodQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdatePod(key); err != nil {
			c.updatePodQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
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
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	pod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := util.ValidatePodNetwork(pod.Annotations); err != nil {
		klog.Errorf("validate pod %s/%s failed, %v", namespace, name, err)
		c.recorder.Eventf(pod, v1.EventTypeWarning, "ValidatePodNetworkFailed", err.Error())
		return err
	}

	// Avoid create lsp for already running pod in ovn-nb when controller restart
	if needCreateLogicalSwitchPort(pod) {
		subnet, err := c.getPodSubnet(pod)
		if err != nil {
			return err
		}

		op := "replace"
		if pod.Annotations == nil || len(pod.Annotations) == 0 {
			op = "add"
			pod.Annotations = map[string]string{}
		}

		ip, mac, err := c.acquireAddress(pod)
		if err != nil {
			c.recorder.Eventf(pod, v1.EventTypeWarning, "AcquireAddressFailed", err.Error())
			return err
		}

		if err := c.ovnClient.CreatePort(subnet.Name, ovs.PodNameToPortName(name, namespace), ip, subnet.Spec.CIDRBlock, mac); err != nil {
			c.recorder.Eventf(pod, v1.EventTypeWarning, "CreateOVNPortFailed", err.Error())
			return err
		}

		pod.Annotations[util.IpAddressAnnotation] = ip
		pod.Annotations[util.MacAddressAnnotation] = mac
		pod.Annotations[util.CidrAnnotation] = subnet.Spec.CIDRBlock
		pod.Annotations[util.GatewayAnnotation] = subnet.Spec.Gateway
		pod.Annotations[util.LogicalSwitchAnnotation] = subnet.Name
		pod.Annotations[util.AllocatedAnnotation] = "true"
		if _, err := c.config.KubeClient.CoreV1().Pods(namespace).Patch(name, types.JSONPatchType, generatePatchPayload(pod.Annotations, op)); err != nil {
			klog.Errorf("patch pod %s/%s failed %v", name, namespace, err)
			return err
		}
	}

	// In case update event might lost during leader election
	if pod.Spec.NodeName != "" && pod.Status.PodIP != "" {
		return c.handleUpdatePod(key)
	}
	return nil
}

func (c *Controller) handleDeletePod(key string) error {
	ip, _, exist := c.ipam.GetPodAddress(key)
	if !exist {
		return nil
	}

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	klog.Infof("delete pod %s/%s", namespace, name)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	if err = c.ovnClient.DeleteStaticRoute(ip, c.config.ClusterRouter); err != nil {
		return err
	}

	if err = c.ovnClient.DeletePort(ovs.PodNameToPortName(name, namespace)); err != nil {
		klog.Errorf("failed to delete lsp %s, %v", ovs.PodNameToPortName(name, namespace), err)
		return err
	}

	err = c.config.KubeOvnClient.KubeovnV1().IPs().Delete(ovs.PodNameToPortName(name, namespace), &metav1.DeleteOptions{})
	if err == nil || k8serrors.IsNotFound(err) {
		c.ipam.ReleaseAddressByPod(key)
		return nil
	}

	klog.Errorf("failed to delete ip %s, %v", ovs.PodNameToPortName(name, namespace), err)
	return err
}

func (c *Controller) handleUpdatePod(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	pod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	klog.Infof("update pod %s/%s", namespace, name)
	podIP := pod.Annotations[util.IpAddressAnnotation]

	subnet, err := c.getPodSubnet(pod)
	if err != nil {
		klog.Errorf("failed to get subnet %v", err)
		return err
	}

	if subnet.Spec.GatewayType == kubeovnv1.GWDistributedType {
		node, err := c.nodesLister.Get(pod.Spec.NodeName)
		if err != nil {
			klog.Errorf("get node %s failed %v", pod.Spec.NodeName, err)
			return err
		}
		nodeTunlIPAddr, err := getNodeTunlIP(node)
		if err != nil {
			return err
		}

		if err := c.ovnClient.AddStaticRoute(ovs.PolicySrcIP, podIP, nodeTunlIPAddr.String(), c.config.ClusterRouter); err != nil {
			return errors.Annotate(err, "add static route failed")
		}
	}
	return nil
}

func isStatefulSetPod(pod *v1.Pod) (bool, string) {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "StatefulSet" {
			return true, owner.Name
		}
	}
	return false, ""
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

func needCreateLogicalSwitchPort(pod *v1.Pod) bool {
	if pod.Status.Phase == v1.PodRunning ||
		pod.Status.Phase == v1.PodSucceeded ||
		pod.Status.Phase == v1.PodFailed ||
		pod.Annotations != nil && pod.Annotations[util.AllocatedAnnotation] == "true" {
		return false
	}
	return true
}

func (c *Controller) getPodSubnet(pod *v1.Pod) (*kubeovnv1.Subnet, error) {
	subnet, err := c.subnetsLister.Get(c.config.DefaultLogicalSwitch)
	if err != nil {
		klog.Errorf("failed to get default subnet %v", err)
		return nil, err
	}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return nil, err
	}

	for _, s := range subnets {
		for _, ns := range s.Spec.Namespaces {
			if ns == pod.Namespace {
				subnet = s
				break
			}
		}
	}
	return subnet, nil
}

func (c *Controller) acquireAddress(pod *v1.Pod) (string, string, error) {
	subnet, err := c.getPodSubnet(pod)
	if err != nil {
		return "", "", err
	}
	key := fmt.Sprintf("%s/%s", pod.Name, pod.Namespace)

	// Random allocate
	if pod.Annotations[util.IpAddressAnnotation] == "" && pod.Annotations[util.IpPoolAnnotation] == "" {
		return c.ipam.GetRandomAddress(key, subnet.Name)
	}

	// Static allocate
	if pod.Annotations[util.IpAddressAnnotation] != "" {
		return c.ipam.GetStaticAddress(key, pod.Annotations[util.IpAddressAnnotation], pod.Annotations[util.MacAddressAnnotation], subnet.Name)
	}

	// IPPool allocate
	ipPool := strings.Split(pod.Annotations[util.IpPoolAnnotation], ",")
	for i, ip := range ipPool {
		ipPool[i] = strings.TrimSpace(ip)
	}

	if ok, _ := isStatefulSetPod(pod); !ok {
		for _, staticIP := range ipPool {
			if ip, mac, err := c.ipam.GetStaticAddress(key, staticIP, pod.Annotations[util.MacAddressAnnotation], subnet.Name); err == nil {
				return ip, mac, nil
			}
		}
		return "", "", ipam.NoAvailableError
	} else {
		numIndex := len(strings.Split(pod.Name, "-")) - 1
		numStr := strings.Split(pod.Name, "-")[numIndex]
		index, _ := strconv.Atoi(numStr)
		if index < len(ipPool) {
			return c.ipam.GetStaticAddress(key, ipPool[index], pod.Annotations[util.MacAddressAnnotation], subnet.Name)
		}
	}
	return "", "", ipam.NoAvailableError
}

func generatePatchPayload(annotations map[string]string, op string) []byte {
	patchPayloadTemplate :=
		`[{
        "op": "%s",
        "path": "/metadata/annotations",
        "value": %s
          }]`

	raw, _ := json.Marshal(annotations)
	return []byte(fmt.Sprintf(patchPayloadTemplate, op, raw))
}
