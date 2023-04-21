package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/k8snetworkplumbingwg/multus-cni.v3/pkg/logging"
	multustypes "gopkg.in/k8snetworkplumbingwg/multus-cni.v3/pkg/types"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type NamedPort struct {
	mutex sync.RWMutex
	// first key is namespace, second key is portName
	namedPortMap map[string]map[string]*util.NamedPortInfo
}

func NewNamedPort() *NamedPort {
	return &NamedPort{
		mutex:        sync.RWMutex{},
		namedPortMap: map[string]map[string]*util.NamedPortInfo{},
	}
}

func (n *NamedPort) AddNamedPortByPod(pod *v1.Pod) {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	ns := pod.Namespace
	podName := pod.Name

	if pod.Spec.Containers == nil {
		return
	}

	for _, container := range pod.Spec.Containers {
		if container.Ports == nil {
			continue
		}

		for _, port := range container.Ports {
			if port.Name == "" || port.ContainerPort == 0 {
				continue
			}

			if _, ok := n.namedPortMap[ns]; ok {
				if _, ok := n.namedPortMap[ns][port.Name]; ok {
					if n.namedPortMap[ns][port.Name].PortId == port.ContainerPort {
						n.namedPortMap[ns][port.Name].Pods[podName] = struct{}{}
					} else {
						klog.Warningf("named port %s has already be defined with portID %d",
							port.Name, n.namedPortMap[ns][port.Name].PortId)
					}
					continue
				}
			} else {
				n.namedPortMap[ns] = make(map[string]*util.NamedPortInfo)
			}
			n.namedPortMap[ns][port.Name] = &util.NamedPortInfo{
				PortId: port.ContainerPort,
				Pods:   map[string]struct{}{podName: {}},
			}
		}
	}
}

func (n *NamedPort) DeleteNamedPortByPod(pod *v1.Pod) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	ns := pod.Namespace
	podName := pod.Name

	if pod.Spec.Containers == nil {
		return
	}

	for _, container := range pod.Spec.Containers {
		if container.Ports == nil {
			continue
		}

		for _, port := range container.Ports {
			if port.Name == "" {
				continue
			}

			if _, ok := n.namedPortMap[ns]; !ok {
				continue
			}

			if _, ok := n.namedPortMap[ns][port.Name]; !ok {
				continue
			}

			if _, ok := n.namedPortMap[ns][port.Name].Pods[podName]; !ok {
				continue
			}

			delete(n.namedPortMap[ns][port.Name].Pods, podName)
			if len(n.namedPortMap[ns][port.Name].Pods) == 0 {
				delete(n.namedPortMap[ns], port.Name)
				if len(n.namedPortMap[ns]) == 0 {
					delete(n.namedPortMap, ns)
				}
			}
		}
	}
}

func (n *NamedPort) GetNamedPortByNs(namespace string) map[string]*util.NamedPortInfo {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	if result, ok := n.namedPortMap[namespace]; ok {
		for portName, info := range result {
			klog.Infof("namespace %s's namedPort portname is %s with info %v ", namespace, portName, info)
		}
		return result
	}
	return nil
}

func isPodAlive(p *v1.Pod) bool {
	if p.DeletionTimestamp != nil && p.DeletionGracePeriodSeconds != nil {
		now := time.Now()
		deletionTime := p.DeletionTimestamp.Time
		gracePeriod := time.Duration(*p.DeletionGracePeriodSeconds) * time.Second
		if now.After(deletionTime.Add(gracePeriod)) {
			return false
		}
	}
	if p.Status.Phase == v1.PodSucceeded && p.Spec.RestartPolicy != v1.RestartPolicyAlways {
		return false
	}

	if p.Status.Phase == v1.PodFailed && p.Spec.RestartPolicy == v1.RestartPolicyNever {
		return false
	}

	if p.Status.Phase == v1.PodFailed && p.Status.Reason == "Evicted" {
		return false
	}
	return true
}

func (c *Controller) enqueueAddPod(obj interface{}) {

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	p := obj.(*v1.Pod)
	// TODO: we need to find a way to reduce duplicated np added to the queue
	if c.config.EnableNP {
		c.namedPort.AddNamedPortByPod(p)
		if p.Status.PodIP != "" {
			for _, np := range c.podMatchNetworkPolicies(p) {
				c.updateNpQueue.Add(np)
			}
		}
	}

	if p.Spec.HostNetwork {
		return
	}

	if !isPodAlive(p) {
		isStateful, statefulSetName := isStatefulSetPod(p)
		isVmPod, vmName := isVmPod(p)
		if isStateful || (isVmPod && c.config.EnableKeepVmIP) {
			if isStateful && isStatefulSetPodToDel(c.config.KubeClient, p, statefulSetName) {
				klog.V(3).Infof("enqueue delete pod %s", key)
				c.deletePodQueue.Add(obj)
			}
			if isVmPod && c.isVmPodToDel(p, vmName) {
				klog.V(3).Infof("enqueue delete pod %s", key)
				c.deletePodQueue.Add(obj)
			}
		} else {
			klog.V(3).Infof("enqueue delete pod %s", key)
			c.deletePodQueue.Add(obj)
		}
		return
	}

	klog.V(3).Infof("enqueue add pod %s", key)
	c.addOrUpdatePodQueue.Add(key)
}

func (c *Controller) enqueueDeletePod(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	p := obj.(*v1.Pod)
	if c.config.EnableNP {
		c.namedPort.DeleteNamedPortByPod(p)
		for _, np := range c.podMatchNetworkPolicies(p) {
			c.updateNpQueue.Add(np)
		}
	}

	if p.Spec.HostNetwork {
		return
	}

	klog.V(3).Infof("enqueue delete pod %s", key)
	c.deletePodQueue.Add(obj)
}

func (c *Controller) enqueueUpdatePod(oldObj, newObj interface{}) {
	oldPod := oldObj.(*v1.Pod)
	newPod := newObj.(*v1.Pod)
	if oldPod.ResourceVersion == newPod.ResourceVersion {
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(newObj)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	podNets, err := c.getPodKubeovnNets(newPod)
	if err != nil {
		klog.Errorf("failed to get newPod nets %v", err)
		return
	}

	if c.config.EnableNP {
		c.namedPort.AddNamedPortByPod(newPod)
		newNp := c.podMatchNetworkPolicies(newPod)
		if !reflect.DeepEqual(oldPod.Labels, newPod.Labels) {
			oldNp := c.podMatchNetworkPolicies(oldPod)
			for _, np := range util.DiffStringSlice(oldNp, newNp) {
				c.updateNpQueue.Add(np)
			}
		}

		for _, podNet := range podNets {
			oldAllocated := oldPod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)]
			newAllocated := newPod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)]
			if oldAllocated != newAllocated {
				for _, np := range newNp {
					klog.V(3).Infof("enqueue update network policy %s for pod %s", np, key)
					c.updateNpQueue.Add(np)
				}
				break
			}
		}
	}

	if newPod.Spec.HostNetwork {
		return
	}

	isStateful, statefulSetName := isStatefulSetPod(newPod)
	isVmPod, vmName := isVmPod(newPod)
	if !isPodAlive(newPod) && !isStateful && !isVmPod {
		klog.V(3).Infof("enqueue delete pod %s", key)
		c.deletePodQueue.Add(newObj)
		return
	}

	if newPod.DeletionTimestamp != nil && !isStateful && !isVmPod {
		go func() {
			// In case node get lost and pod can not be deleted,
			// the ip address will not be recycled
			time.Sleep(time.Duration(*newPod.Spec.TerminationGracePeriodSeconds) * time.Second)
			c.deletePodQueue.Add(newObj)
		}()
		return
	}

	// do not delete statefulset pod unless ownerReferences is deleted
	if isStateful && isStatefulSetPodToDel(c.config.KubeClient, newPod, statefulSetName) {
		go func() {
			klog.V(3).Infof("enqueue delete pod %s", key)
			time.Sleep(time.Duration(*newPod.Spec.TerminationGracePeriodSeconds) * time.Second)
			c.deletePodQueue.Add(newObj)
		}()
		return
	}
	if isVmPod && c.isVmPodToDel(newPod, vmName) {
		go func() {
			klog.V(3).Infof("enqueue delete pod %s", key)
			time.Sleep(time.Duration(*newPod.Spec.TerminationGracePeriodSeconds) * time.Second)
			c.deletePodQueue.Add(newObj)
		}()
		return
	}

	c.addOrUpdatePodQueue.Add(key)

	// security policy changed
	for _, podNet := range podNets {
		oldSecurity := oldPod.Annotations[fmt.Sprintf(util.PortSecurityAnnotationTemplate, podNet.ProviderName)]
		newSecurity := newPod.Annotations[fmt.Sprintf(util.PortSecurityAnnotationTemplate, podNet.ProviderName)]
		oldSg := oldPod.Annotations[fmt.Sprintf(util.SecurityGroupAnnotationTemplate, podNet.ProviderName)]
		newSg := newPod.Annotations[fmt.Sprintf(util.SecurityGroupAnnotationTemplate, podNet.ProviderName)]
		oldVips := oldPod.Annotations[fmt.Sprintf(util.PortVipAnnotationTemplate, podNet.ProviderName)]
		newVips := newPod.Annotations[fmt.Sprintf(util.PortVipAnnotationTemplate, podNet.ProviderName)]
		if oldSecurity != newSecurity || oldSg != newSg || oldVips != newVips {
			c.updatePodSecurityQueue.Add(key)
			break
		}
	}
}

func (c *Controller) runAddOrUpdatePodWorker() {
	for c.processNextAddOrUpdatePodWorkItem() {
	}
}

func (c *Controller) runDeletePodWorker() {
	for c.processNextDeletePodWorkItem() {
	}
}

func (c *Controller) runUpdatePodSecurityWorker() {
	for c.processNextUpdatePodSecurityWorkItem() {
	}
}

func (c *Controller) processNextAddOrUpdatePodWorkItem() bool {
	obj, shutdown := c.addOrUpdatePodQueue.Get()
	if shutdown {
		return false
	}
	now := time.Now()

	err := func(obj interface{}) error {
		defer c.addOrUpdatePodQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addOrUpdatePodQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		klog.Infof("handle sync pod %s", key)
		if err := c.handleAddOrUpdatePod(key); err != nil {
			c.addOrUpdatePodQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		last := time.Since(now)
		klog.Infof("take %d ms to handle sync pod %s", last.Milliseconds(), key)
		c.addOrUpdatePodQueue.Forget(obj)
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
		var pod *v1.Pod
		var ok bool
		if pod, ok = obj.(*v1.Pod); !ok {
			c.deletePodQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected pod in workqueue but got %#v", obj))
			return nil
		}
		klog.Infof("handle delete pod %s/%s", pod.Namespace, pod.Name)
		if err := c.handleDeletePod(pod); err != nil {
			c.deletePodQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s': %s, requeuing", pod.Name, err.Error())
		}
		c.deletePodQueue.Forget(obj)
		last := time.Since(now)
		klog.Infof("take %d ms to handle delete pod %s/%s", last.Milliseconds(), pod.Namespace, pod.Name)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdatePodSecurityWorkItem() bool {
	obj, shutdown := c.updatePodSecurityQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updatePodSecurityQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updatePodSecurityQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdatePodSecurity(key); err != nil {
			c.updatePodSecurityQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updatePodSecurityQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) getPodKubeovnNets(pod *v1.Pod) ([]*kubeovnNet, error) {
	defaultSubnet, err := c.getPodDefaultSubnet(pod)
	if err != nil {
		return nil, err
	}

	attachmentNets, err := c.getPodAttachmentNet(pod)
	if err != nil {
		return nil, err
	}

	podNets := attachmentNets
	if _, hasOtherDefaultNet := pod.Annotations[util.DefaultNetworkAnnotation]; !hasOtherDefaultNet {
		podNets = append(attachmentNets, &kubeovnNet{
			Type:         providerTypeOriginal,
			ProviderName: util.OvnProvider,
			Subnet:       defaultSubnet,
			IsDefault:    true,
		})
	}

	return podNets, nil
}

func (c *Controller) changeVMSubnet(vmName, namespace, providerName, subnetName string, pod *v1.Pod) error {
	ipName := ovs.PodNameToPortName(vmName, namespace, providerName)
	ipCr, err := c.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), ipName, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			errMsg := fmt.Errorf("failed to get ip CR %s: %v", ipName, err)
			klog.Error(errMsg)
			return errMsg
		}
		// the returned pointer is not nil if the CR does not exist
		ipCr = nil
	}
	if ipCr != nil {
		if ipCr.Spec.Subnet != subnetName {
			key := fmt.Sprintf("%s/%s", pod.Namespace, vmName)
			ports, err := c.ovnClient.ListNormalLogicalSwitchPorts(true, map[string]string{"pod": key})
			if err != nil {
				klog.Errorf("failed to list lsps of pod '%s', %v", pod.Name, err)
				return err
			}
			for _, port := range ports {
				// when lsp is deleted, the port of pod is deleted from any port-group automatically.
				klog.Infof("gc logical switch port %s", port.Name)
				if err := c.ovnClient.DeleteLogicalSwitchPort(port.Name); err != nil {
					klog.Errorf("failed to delete lsp %s, %v", port.Name, err)
					return err
				}
			}
		}
	}
	return nil
}

func (c *Controller) handleAddOrUpdatePod(key string) (err error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.podKeyMutex.Lock(key)
	defer c.podKeyMutex.Unlock(key)

	cachedPod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	pod := cachedPod.DeepCopy()
	if err := util.ValidatePodNetwork(pod.Annotations); err != nil {
		klog.Errorf("validate pod %s/%s failed: %v", namespace, name, err)
		c.recorder.Eventf(pod, v1.EventTypeWarning, "ValidatePodNetworkFailed", err.Error())
		return err
	}

	podNets, err := c.getPodKubeovnNets(pod)
	if err != nil {
		klog.Errorf("failed to get pod nets %v", err)
		return err
	}
	if len(pod.Annotations) == 0 {
		pod.Annotations = map[string]string{}
	}

	// check and do hotnoplug nic
	if err = c.syncKubeOvnNet(pod, podNets); err != nil {
		klog.Errorf("failed to sync pod nets %v", err)
		return err
	}

	// check if allocate subnet is need. also allocate subnet when hotplug nic
	needAllocatePodNets := needAllocateSubnets(pod, podNets)
	if len(needAllocatePodNets) != 0 {
		if cachedPod, err = c.reconcileAllocateSubnets(cachedPod, pod, needAllocatePodNets); err != nil {
			return err
		}
		if cachedPod == nil {
			// pod has been deleted
			return nil
		}
	}

	// check if route subnet is need.
	pod = cachedPod.DeepCopy()
	return c.reconcileRouteSubnets(cachedPod, pod, needRouteSubnets(pod, podNets))
}

// do the same thing as add pod
func (c *Controller) reconcileAllocateSubnets(cachedPod, pod *v1.Pod, needAllocatePodNets []*kubeovnNet) (*v1.Pod, error) {
	namespace := pod.Namespace
	name := pod.Name
	isVmPod, vmName := isVmPod(pod)

	klog.Infof("sync pod %s/%s allocated", namespace, name)

	// Avoid create lsp for already running pod in ovn-nb when controller restart
	for _, podNet := range needAllocatePodNets {
		// the subnet may changed when alloc static ip from the latter subnet after ns supports multi subnets
		v4IP, v6IP, mac, subnet, err := c.acquireAddress(pod, podNet)
		if err != nil {
			c.recorder.Eventf(pod, v1.EventTypeWarning, "AcquireAddressFailed", err.Error())
			return nil, err
		}
		ipStr := util.GetStringIP(v4IP, v6IP)
		pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)] = ipStr
		pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)] = mac
		pod.Annotations[fmt.Sprintf(util.CidrAnnotationTemplate, podNet.ProviderName)] = subnet.Spec.CIDRBlock
		pod.Annotations[fmt.Sprintf(util.GatewayAnnotationTemplate, podNet.ProviderName)] = subnet.Spec.Gateway
		pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, podNet.ProviderName)] = subnet.Name
		pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] = "true"
		if pod.Annotations[fmt.Sprintf(util.PodNicAnnotationTemplate, podNet.ProviderName)] == "" {
			pod.Annotations[fmt.Sprintf(util.PodNicAnnotationTemplate, podNet.ProviderName)] = c.config.PodNicType
		}
		if isVmPod && c.config.EnableKeepVmIP {
			pod.Annotations[fmt.Sprintf(util.VmTemplate, podNet.ProviderName)] = vmName
			if err := c.changeVMSubnet(vmName, namespace, podNet.ProviderName, subnet.Name, pod); err != nil {
				klog.Errorf("change subnet of pod %s/%s to %s failed: %v", namespace, name, subnet.Name, err)
				return nil, err
			}
		}

		if err := util.ValidatePodCidr(podNet.Subnet.Spec.CIDRBlock, ipStr); err != nil {
			klog.Errorf("validate pod %s/%s failed: %v", namespace, name, err)
			c.recorder.Eventf(pod, v1.EventTypeWarning, "ValidatePodNetworkFailed", err.Error())
			return nil, err
		}

		podType := getPodType(pod)
		podName := c.getNameByPod(pod)
		if err := c.createOrUpdateCrdIPs(podName, ipStr, mac, subnet.Name, pod.Namespace, pod.Spec.NodeName, podNet.ProviderName, podType, nil); err != nil {
			err = fmt.Errorf("failed to create ips CR %s.%s: %v", podName, pod.Namespace, err)
			klog.Error(err)
			return nil, err
		}

		if podNet.Type != providerTypeIPAM {
			if (subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) && subnet.Spec.Vpc != "" {
				pod.Annotations[fmt.Sprintf(util.LogicalRouterAnnotationTemplate, podNet.ProviderName)] = subnet.Spec.Vpc
			}

			if subnet.Spec.Vlan != "" {
				vlan, err := c.vlansLister.Get(subnet.Spec.Vlan)
				if err != nil {
					c.recorder.Eventf(pod, v1.EventTypeWarning, "GetVlanInfoFailed", err.Error())
					return nil, err
				}
				pod.Annotations[fmt.Sprintf(util.VlanIdAnnotationTemplate, podNet.ProviderName)] = strconv.Itoa(vlan.Spec.ID)
				pod.Annotations[fmt.Sprintf(util.ProviderNetworkTemplate, podNet.ProviderName)] = vlan.Spec.Provider
			}

			portSecurity := false
			if pod.Annotations[fmt.Sprintf(util.PortSecurityAnnotationTemplate, podNet.ProviderName)] == "true" {
				portSecurity = true
			}

			securityGroupAnnotation := pod.Annotations[fmt.Sprintf(util.SecurityGroupAnnotationTemplate, podNet.ProviderName)]
			vips := pod.Annotations[fmt.Sprintf(util.PortVipAnnotationTemplate, podNet.ProviderName)]
			for _, ip := range strings.Split(vips, ",") {
				if ip != "" && net.ParseIP(ip) == nil {
					klog.Errorf("invalid vip address '%s' for pod %s", ip, name)
					vips = ""
					break
				}
			}

			portName := ovs.PodNameToPortName(podName, namespace, podNet.ProviderName)
			dhcpOptions := &ovs.DHCPOptionsUUIDs{
				DHCPv4OptionsUUID: subnet.Status.DHCPv4OptionsUUID,
				DHCPv6OptionsUUID: subnet.Status.DHCPv6OptionsUUID,
			}

			if err := c.ovnClient.CreateLogicalSwitchPort(subnet.Name, portName, ipStr, mac, podName, pod.Namespace, portSecurity, securityGroupAnnotation, vips, podNet.Subnet.Spec.EnableDHCP, dhcpOptions, subnet.Spec.Vpc); err != nil {
				c.recorder.Eventf(pod, v1.EventTypeWarning, "CreateOVNPortFailed", err.Error())
				klog.Errorf("%v", err)
				return nil, err
			}

			if portSecurity {
				sgNames := strings.Split(securityGroupAnnotation, ",")
				for _, sgName := range sgNames {
					if sgName == "" {
						continue
					}
					c.syncSgPortsQueue.Add(sgName)
				}
			}

			if vips != "" {
				c.syncVirtualPortsQueue.Add(podNet.Subnet.Name)
			}
		}
	}
	patch, err := util.GenerateMergePatchPayload(cachedPod, pod)
	if err != nil {
		klog.Errorf("failed to generate patch for pod %s/%s: %v", name, namespace, err)
		return nil, err
	}
	patchedPod, err := c.config.KubeClient.CoreV1().Pods(namespace).Patch(context.Background(), name,
		types.MergePatchType, patch, metav1.PatchOptions{}, "")
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Sometimes pod is deleted between kube-ovn configure ovn-nb and patch pod.
			// Then we need to recycle the resource again.
			c.deletePodQueue.AddRateLimited(pod)
			return nil, nil
		}
		klog.Errorf("patch pod %s/%s failed: %v", name, namespace, err)
		return nil, err
	}

	if vpcGwName, isVpcNatGw := pod.Annotations[util.VpcNatGatewayAnnotation]; isVpcNatGw {
		c.initVpcNatGatewayQueue.Add(vpcGwName)
	}
	return patchedPod.DeepCopy(), nil
}

// do the same thing as update pod
func (c *Controller) reconcileRouteSubnets(cachedPod, pod *v1.Pod, needRoutePodNets []*kubeovnNet) error {
	if len(needRoutePodNets) == 0 {
		return nil
	}

	namespace := pod.Namespace
	name := pod.Name
	podName := c.getNameByPod(pod)

	klog.Infof("sync pod %s/%s routed", namespace, name)

	var podIP string
	var subnet *kubeovnv1.Subnet

	for _, podNet := range needRoutePodNets {
		// in case update handler overlap the annotation when cache is not in sync
		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "" {
			return fmt.Errorf("no address has been allocated to %s/%s", namespace, name)
		}

		podIP = pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)]
		subnet = podNet.Subnet

		if podIP != "" && (subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) && subnet.Spec.Vpc == util.DefaultVpc {
			node, err := c.nodesLister.Get(pod.Spec.NodeName)
			if err != nil {
				klog.Errorf("failed to get node %s: %v", pod.Spec.NodeName, err)
				return err
			}

			pgName := getOverlaySubnetsPortGroupName(subnet.Name, node.Name)
			if c.config.EnableEipSnat && (pod.Annotations[util.EipAnnotation] != "" || pod.Annotations[util.SnatAnnotation] != "") {
				cm, err := c.configMapsLister.ConfigMaps(c.config.ExternalGatewayConfigNS).Get(util.ExternalGatewayConfig)
				if err != nil {
					klog.Errorf("failed to get ex-gateway config, %v", err)
					return err
				}
				nextHop := cm.Data["external-gw-addr"]
				if nextHop == "" {
					klog.Errorf("no available gateway nic address")
					return fmt.Errorf("no available gateway nic address")
				}
				if strings.Contains(nextHop, "/") {
					nextHop = strings.Split(nextHop, "/")[0]
				}

				if err := c.ovnLegacyClient.AddStaticRoute(ovs.PolicySrcIP, podIP, nextHop, "", "", c.config.ClusterRouter, util.NormalRouteType); err != nil {
					klog.Errorf("failed to add static route, %v", err)
					return err
				}

				// remove lsp from port group to make EIP/SNAT work
				portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
				if err = c.ovnClient.PortGroupRemovePorts(pgName, portName); err != nil {
					return err
				}

			} else {
				if subnet.Spec.GatewayType == kubeovnv1.GWDistributedType && pod.Annotations[util.NorthGatewayAnnotation] == "" {
					nodeTunlIPAddr, err := getNodeTunlIP(node)
					if err != nil {
						return err
					}

					var added bool
					for _, nodeAddr := range nodeTunlIPAddr {
						for _, podAddr := range strings.Split(podIP, ",") {
							if util.CheckProtocol(nodeAddr.String()) != util.CheckProtocol(podAddr) {
								continue
							}

							portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
							if err := c.ovnClient.PortGroupAddPorts(pgName, portName); err != nil {
								klog.Errorf("add port to port group %s: %v", pgName, err)
								return err
							}

							added = true
							break
						}
						if added {
							break
						}
					}
				}

				if pod.Annotations[util.NorthGatewayAnnotation] != "" {
					if err := c.ovnLegacyClient.AddStaticRoute(ovs.PolicySrcIP, podIP, pod.Annotations[util.NorthGatewayAnnotation],
						"", "", c.config.ClusterRouter, util.NormalRouteType); err != nil {
						klog.Errorf("failed to add static route, %v", err)
						return err
					}
				} else if c.config.EnableEipSnat {
					if err := c.ovnLegacyClient.DeleteStaticRoute(podIP, c.config.ClusterRouter); err != nil {
						return err
					}
				}
			}

			if c.config.EnableEipSnat {
				for _, ipStr := range strings.Split(podIP, ",") {
					if err := c.ovnLegacyClient.UpdateNatRule("dnat_and_snat", ipStr, pod.Annotations[util.EipAnnotation], c.config.ClusterRouter, pod.Annotations[util.MacAddressAnnotation], fmt.Sprintf("%s.%s", podName, pod.Namespace)); err != nil {
						klog.Errorf("failed to add nat rules, %v", err)
						return err
					}

					if err := c.ovnLegacyClient.UpdateNatRule("snat", ipStr, pod.Annotations[util.SnatAnnotation], c.config.ClusterRouter, "", ""); err != nil {
						klog.Errorf("failed to add nat rules, %v", err)
						return err
					}
				}
			}
		}

		pod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, podNet.ProviderName)] = "true"
	}
	patch, err := util.GenerateMergePatchPayload(cachedPod, pod)
	if err != nil {
		klog.Errorf("failed to generate patch for pod %s/%s: %v", name, namespace, err)
		return err
	}
	if _, err := c.config.KubeClient.CoreV1().Pods(namespace).Patch(context.Background(), name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			// Sometimes pod is deleted between kube-ovn configure ovn-nb and patch pod.
			// Then we need to recycle the resource again.
			c.deletePodQueue.AddRateLimited(pod)
			return nil
		}
		klog.Errorf("patch pod %s/%s failed %v", name, namespace, err)
		return err
	}
	return nil
}

func (c *Controller) handleDeletePod(pod *v1.Pod) error {
	var key string
	var err error

	podName := c.getNameByPod(pod)
	key = fmt.Sprintf("%s/%s", pod.Namespace, podName)
	c.podKeyMutex.Lock(key)
	defer c.podKeyMutex.Unlock(key)

	p, _ := c.podsLister.Pods(pod.Namespace).Get(pod.Name)
	if p != nil && p.UID != pod.UID {
		// Pod with same name exists, just return here
		return nil
	}

	ports, err := c.ovnClient.ListNormalLogicalSwitchPorts(true, map[string]string{"pod": key})
	if err != nil {
		klog.Errorf("failed to list lsps of pod '%s', %v", pod.Name, err)
		return err
	}

	if len(ports) != 0 {
		addresses := c.ipam.GetPodAddress(key)
		for _, address := range addresses {
			if strings.TrimSpace(address.Ip) == "" {
				continue
			}
			subnet, err := c.subnetsLister.Get(address.Subnet.Name)
			if k8serrors.IsNotFound(err) {
				continue
			} else if err != nil {
				return err
			}
			vpc, err := c.vpcsLister.Get(subnet.Spec.Vpc)
			if k8serrors.IsNotFound(err) {
				continue
			} else if err != nil {
				return err
			}
			// If pod has snat or eip, also need delete staticRoute when delete pod
			if vpc.Name == util.DefaultVpc {
				if err := c.ovnLegacyClient.DeleteStaticRoute(address.Ip, vpc.Name); err != nil {
					return err
				}
			}
			if exGwEnabled == "true" {
				if err := c.ovnLegacyClient.DeleteNatRule(address.Ip, vpc.Name); err != nil {
					return err
				}
			}
		}
	}

	var keepIpCR bool
	if ok, sts := isStatefulSetPod(pod); ok {
		toDel := isStatefulSetPodToDel(c.config.KubeClient, pod, sts)
		delete, err := appendCheckPodToDel(c, pod, sts, "StatefulSet")
		if pod.DeletionTimestamp != nil {
			// triggered by delete event
			if !(toDel || (delete && err == nil)) {
				return nil
			}
		}
		keepIpCR = !toDel && !delete && err == nil
	}
	isVmPod, vmName := isVmPod(pod)
	if isVmPod && c.config.EnableKeepVmIP {
		toDel := c.isVmPodToDel(pod, vmName)
		delete, err := appendCheckPodToDel(c, pod, vmName, util.VmInstance)
		if pod.DeletionTimestamp != nil {
			// triggered by delete event
			if !(toDel || (delete && err == nil)) {
				return nil
			}
			klog.Infof("delete vm pod %s", podName)
		}
	}

	for _, port := range ports {
		// when lsp is deleted, the port of pod is deleted from any port-group automatically.
		klog.Infof("gc logical switch port %s", port.Name)
		if err := c.ovnClient.DeleteLogicalSwitchPort(port.Name); err != nil {
			klog.Errorf("failed to delete lsp %s, %v", port.Name, err)
			return err
		}
	}
	podNets, err := c.getPodKubeovnNets(pod)
	if err != nil {
		klog.Errorf("failed to get pod nets %v", err)
	}
	if !keepIpCR {
		for _, podNet := range podNets {
			if err = c.deleteCrdIPs(podName, pod.Namespace, podNet.ProviderName); err != nil {
				klog.Errorf("failed to delete ip for pod %s, %v, please delete manually", pod.Name, err)
			}
		}
		if pod.Annotations[util.VipAnnotation] != "" {
			if err = c.releaseVip(pod.Annotations[util.VipAnnotation]); err != nil {
				klog.Errorf("failed to clean label from vip %s, %v", pod.Annotations[util.VipAnnotation], err)
				return err
			}
		}
	}
	c.ipam.ReleaseAddressByPod(key)
	for _, podNet := range podNets {
		c.syncVirtualPortsQueue.Add(podNet.Subnet.Name)
	}
	return nil
}

func (c *Controller) handleUpdatePodSecurity(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.podKeyMutex.Lock(key)
	defer c.podKeyMutex.Unlock(key)

	pod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	podName := c.getNameByPod(pod)

	klog.Infof("update pod %s/%s security", namespace, name)

	podNets, err := c.getPodKubeovnNets(pod)
	if err != nil {
		klog.Errorf("failed to pod nets %v", err)
		return err
	}

	// associated with security group
	for _, podNet := range podNets {
		portSecurity := false
		if pod.Annotations[fmt.Sprintf(util.PortSecurityAnnotationTemplate, podNet.ProviderName)] == "true" {
			portSecurity = true
		}

		mac := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)]
		ipStr := pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)]
		vips := pod.Annotations[fmt.Sprintf(util.PortVipAnnotationTemplate, podNet.ProviderName)]

		if err = c.ovnClient.SetLogicalSwitchPortSecurity(portSecurity, ovs.PodNameToPortName(podName, namespace, podNet.ProviderName), mac, ipStr, vips); err != nil {
			klog.Errorf("set logical switch port security: %v", err)
			return err
		}

		c.syncVirtualPortsQueue.Add(podNet.Subnet.Name)

		var securityGroups string
		if portSecurity {
			securityGroups = pod.Annotations[fmt.Sprintf(util.SecurityGroupAnnotationTemplate, podNet.ProviderName)]
			securityGroups = strings.ReplaceAll(securityGroups, " ", "")
		}
		if err = c.reconcilePortSg(ovs.PodNameToPortName(podName, namespace, podNet.ProviderName), securityGroups); err != nil {
			klog.Errorf("reconcilePortSg failed. %v", err)
			return err
		}
	}
	return nil
}
func (c *Controller) syncKubeOvnNet(pod *v1.Pod, podNets []*kubeovnNet) error {
	podName := c.getNameByPod(pod)
	key := fmt.Sprintf("%s/%s", pod.Namespace, podName)
	targetPortNameList := make(map[string]struct{})
	portsNeedToDel := []string{}
	subnetUsedByPort := make(map[string]string)

	for _, podNet := range podNets {
		portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
		targetPortNameList[portName] = struct{}{}
	}

	ports, err := c.ovnClient.ListNormalLogicalSwitchPorts(true, map[string]string{"pod": key})
	if err != nil {
		klog.Errorf("failed to list lsps of pod '%s', %v", pod.Name, err)
		return err
	}

	for _, port := range ports {
		if _, ok := targetPortNameList[port.Name]; !ok {
			portsNeedToDel = append(portsNeedToDel, port.Name)
			subnetUsedByPort[port.Name] = port.ExternalIDs["ls"]
		}
	}

	if len(portsNeedToDel) == 0 {
		return nil
	}

	for _, portNeedDel := range portsNeedToDel {

		if subnet, ok := c.ipam.Subnets[subnetUsedByPort[portNeedDel]]; ok {
			subnet.ReleaseAddressWithNicName(podName, portNeedDel)
		}

		if err := c.ovnClient.DeleteLogicalSwitchPort(portNeedDel); err != nil {
			klog.Errorf("failed to delete lsp %s, %v", portNeedDel, err)
			return err
		}
		if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), portNeedDel, metav1.DeleteOptions{}); err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to delete ip %s, %v", portNeedDel, err)
				return err
			}
		}
	}

	return nil
}

func isStatefulSetPod(pod *v1.Pod) (bool, string) {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "StatefulSet" && strings.HasPrefix(owner.APIVersion, "apps/") {
			if strings.HasPrefix(pod.Name, owner.Name) {
				return true, owner.Name
			}
		}
	}
	return false, ""
}

func isStatefulSetPodToDel(c kubernetes.Interface, pod *v1.Pod, statefulSetName string) bool {
	// only delete statefulset pod lsp when statefulset deleted or down scaled
	ss, err := c.AppsV1().StatefulSets(pod.Namespace).Get(context.Background(), statefulSetName, metav1.GetOptions{})
	if err != nil {
		// statefulset is deleted
		if k8serrors.IsNotFound(err) {
			return true
		} else {
			klog.Errorf("failed to get statefulset %v", err)
		}
		return false
	}

	// statefulset is deleting
	if ss.DeletionTimestamp != nil {
		return true
	}

	// down scale statefulset
	tempStrs := strings.Split(pod.Name, "-")
	numStr := tempStrs[len(tempStrs)-1]
	index, err := strconv.ParseInt(numStr, 10, 0)
	if err != nil {
		klog.Errorf("failed to parse %s to int", numStr)
		return false
	}

	return index >= int64(*ss.Spec.Replicas)
}

func getNodeTunlIP(node *v1.Node) ([]net.IP, error) {
	var nodeTunlIPAddr []net.IP
	nodeTunlIP := node.Annotations[util.IpAddressAnnotation]
	if nodeTunlIP == "" {
		return nil, fmt.Errorf("node has no tunnel ip annotation")
	}

	for _, ip := range strings.Split(nodeTunlIP, ",") {
		nodeTunlIPAddr = append(nodeTunlIPAddr, net.ParseIP(ip))
	}
	return nodeTunlIPAddr, nil
}

func getNextHopByTunnelIP(gw []net.IP) string {
	// validation check by caller
	nextHop := gw[0].String()
	if len(gw) == 2 {
		nextHop = gw[0].String() + "," + gw[1].String()
	}
	return nextHop
}

func needAllocateSubnets(pod *v1.Pod, nets []*kubeovnNet) []*kubeovnNet {
	if !isPodAlive(pod) {
		return nil
	}

	if pod.Annotations == nil {
		return nets
	}

	result := make([]*kubeovnNet, 0, len(nets))
	for _, n := range nets {
		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, n.ProviderName)] != "true" {
			result = append(result, n)
		}
	}
	return result
}

func needRouteSubnets(pod *v1.Pod, nets []*kubeovnNet) []*kubeovnNet {
	if !isPodAlive(pod) {
		return nil
	}

	if pod.Annotations == nil {
		return nets
	}

	result := make([]*kubeovnNet, 0, len(nets))
	for _, n := range nets {
		if !isOvnSubnet(n.Subnet) {
			continue
		}

		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, n.ProviderName)] == "true" && pod.Spec.NodeName != "" {
			if pod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, n.ProviderName)] != "true" {
				result = append(result, n)
			}
		}
	}
	return result
}

func (c *Controller) getPodDefaultSubnet(pod *v1.Pod) (*kubeovnv1.Subnet, error) {
	var subnet *kubeovnv1.Subnet
	var err error
	// 1. check annotation subnet
	lsName, lsExist := pod.Annotations[util.LogicalSwitchAnnotation]
	if lsExist {
		subnet, err = c.subnetsLister.Get(lsName)
		if err != nil {
			klog.Errorf("failed to get subnet %v", err)
			return nil, err
		}
	} else {
		ns, err := c.namespacesLister.Get(pod.Namespace)
		if err != nil {
			klog.Errorf("failed to get namespace %s, %v", pod.Namespace, err)
			return nil, err
		}
		if ns.Annotations == nil {
			err = fmt.Errorf("namespace %s network annotations is nil", pod.Namespace)
			klog.Error(err)
			return nil, err
		}

		subnetNames := ns.Annotations[util.LogicalSwitchAnnotation]
		for _, subnetName := range strings.Split(subnetNames, ",") {
			if subnetName == "" {
				err = fmt.Errorf("namespace %s default logical switch is not found", pod.Namespace)
				klog.Error(err)
				return nil, err
			}
			subnet, err = c.subnetsLister.Get(subnetName)
			if err != nil {
				klog.Errorf("failed to get subnet %v", err)
				return nil, err
			}

			switch subnet.Spec.Protocol {
			case kubeovnv1.ProtocolIPv4:
				fallthrough
			case kubeovnv1.ProtocolDual:
				if subnet.Status.V4AvailableIPs == 0 {
					klog.V(3).Infof("there's no available ips for subnet %v, try next subnet", subnet.Name)
					continue
				}
			case kubeovnv1.ProtocolIPv6:
				if subnet.Status.V6AvailableIPs == 0 {
					klog.Infof("there's no available ips for subnet %v, try next subnet", subnet.Name)
					continue
				}
			}
			break
		}
	}
	return subnet, nil
}

func loadNetConf(bytes []byte) (*multustypes.DelegateNetConf, error) {
	delegateConf := &multustypes.DelegateNetConf{}
	if err := json.Unmarshal(bytes, &delegateConf.Conf); err != nil {
		return nil, logging.Errorf("LoadDelegateNetConf: error unmarshalling delegate config: %v", err)
	}

	if delegateConf.Conf.Type == "" {
		if err := multustypes.LoadDelegateNetConfList(bytes, delegateConf); err != nil {
			return nil, logging.Errorf("LoadDelegateNetConf: failed with: %v", err)
		}
	}
	return delegateConf, nil
}

type providerType int

const (
	providerTypeIPAM providerType = iota
	providerTypeOriginal
)

type kubeovnNet struct {
	Type               providerType
	ProviderName       string
	Subnet             *kubeovnv1.Subnet
	IsDefault          bool
	AllowLiveMigration bool
}

func (c *Controller) getPodAttachmentNet(pod *v1.Pod) ([]*kubeovnNet, error) {
	var multusNets []*multustypes.NetworkSelectionElement
	defaultAttachNetworks := pod.Annotations[util.DefaultNetworkAnnotation]
	if defaultAttachNetworks != "" {
		attachments, err := util.ParsePodNetworkAnnotation(defaultAttachNetworks, pod.Namespace)
		if err != nil {
			klog.Errorf("failed to parse default attach net for pod '%s', %v", pod.Name, err)
			return nil, err
		}
		multusNets = attachments
	}

	attachNetworks := pod.Annotations[util.AttachmentNetworkAnnotation]
	if attachNetworks != "" {
		attachments, err := util.ParsePodNetworkAnnotation(attachNetworks, pod.Namespace)
		if err != nil {
			klog.Errorf("failed to parse attach net for pod '%s', %v", pod.Name, err)
			return nil, err
		}
		multusNets = append(multusNets, attachments...)
	}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	result := make([]*kubeovnNet, 0, len(multusNets))
	for _, attach := range multusNets {
		networkClient := c.config.AttachNetClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(attach.Namespace)
		network, err := networkClient.Get(context.Background(), attach.Name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get net-attach-def %s, %v", attach.Name, err)
			return nil, err
		}

		netCfg, err := loadNetConf([]byte(network.Spec.Config))
		if err != nil {
			klog.Errorf("failed to load config of net-attach-def %s, %v", attach.Name, err)
			return nil, err
		}

		// allocate kubeovn network
		var providerName string
		if util.IsOvnNetwork(netCfg) {
			allowLiveMigration := false
			isDefault := util.IsDefaultNet(pod.Annotations[util.DefaultNetworkAnnotation], attach)

			providerName = fmt.Sprintf("%s.%s.ovn", attach.Name, attach.Namespace)
			if pod.Annotations[fmt.Sprintf(util.LiveMigrationAnnotationTemplate, providerName)] == "true" {
				allowLiveMigration = true
			}

			subnetName := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, providerName)]
			if subnetName == "" {
				for _, subnet := range subnets {
					if subnet.Spec.Provider == providerName {
						subnetName = subnet.Name
						break
					}
				}
			}
			var subnet *kubeovnv1.Subnet
			if subnetName == "" {
				subnet, err = c.getPodDefaultSubnet(pod)
				if err != nil {
					klog.Errorf("failed to pod default subnet, %v", err)
					return nil, err
				}
			} else {
				subnet, err = c.subnetsLister.Get(subnetName)
				if err != nil {
					klog.Errorf("failed to get subnet %s, %v", subnetName, err)
					return nil, err
				}
			}
			result = append(result, &kubeovnNet{
				Type:               providerTypeOriginal,
				ProviderName:       providerName,
				Subnet:             subnet,
				IsDefault:          isDefault,
				AllowLiveMigration: allowLiveMigration,
			})
		} else {
			providerName = fmt.Sprintf("%s.%s", attach.Name, attach.Namespace)
			for _, subnet := range subnets {
				if subnet.Spec.Provider == providerName {
					result = append(result, &kubeovnNet{
						Type:         providerTypeIPAM,
						ProviderName: providerName,
						Subnet:       subnet,
					})
					break
				}
			}
		}
	}
	return result, nil
}

func (c *Controller) validatePodIP(podName, subnetName, ipv4, ipv6 string) (bool, bool, error) {
	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		klog.Errorf("failed to get subnet %s: %v", subnetName, err)
		return false, false, err
	}

	if subnet.Spec.Vlan == "" && subnet.Spec.Vpc == util.DefaultVpc {
		nodes, err := c.nodesLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list nodes: %v", err)
			return false, false, err
		}

		for _, node := range nodes {
			nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*node)
			if ipv4 != "" && ipv4 == nodeIPv4 {
				klog.Errorf("IP address (%s) assigned to pod %s is the same with internal IP address of node %s, reallocating...", ipv4, podName, node.Name)
				return false, true, nil
			}
			if ipv6 != "" && ipv6 == nodeIPv6 {
				klog.Errorf("IP address (%s) assigned to pod %s is the same with internal IP address of node %s, reallocating...", ipv6, podName, node.Name)
				return true, false, nil
			}
		}
	}

	return true, true, nil
}

func (c *Controller) acquireAddress(pod *v1.Pod, podNet *kubeovnNet) (string, string, string, *kubeovnv1.Subnet, error) {
	podName := c.getNameByPod(pod)
	key := fmt.Sprintf("%s/%s", pod.Namespace, podName)

	isStsPod, _ := isStatefulSetPod(pod)
	// if pod has static vip
	vipName := pod.Annotations[util.VipAnnotation]
	if vipName != "" {
		vip, err := c.virtualIpsLister.Get(vipName)
		if err != nil {
			klog.Errorf("failed to get static vip '%s', %v", vipName, err)
			return "", "", "", podNet.Subnet, err
		}
		portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
		if err = c.podReuseVip(vipName, portName, isStsPod); err != nil {
			return "", "", "", podNet.Subnet, err
		}
		return vip.Status.V4ip, vip.Status.V6ip, vip.Status.Mac, podNet.Subnet, nil
	}

	macStr := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)]
	if macStr != "" {
		if _, err := net.ParseMAC(macStr); err != nil {
			return "", "", "", podNet.Subnet, err
		}
	}

	// Random allocate
	if pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)] == "" &&
		pod.Annotations[fmt.Sprintf(util.IpPoolAnnotationTemplate, podNet.ProviderName)] == "" {
		var skippedAddrs []string
		for {
			portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)

			ipv4, ipv6, mac, err := c.ipam.GetRandomAddress(key, portName, macStr, podNet.Subnet.Name, skippedAddrs, !podNet.AllowLiveMigration)
			if err != nil {
				return "", "", "", podNet.Subnet, err
			}
			ipv4OK, ipv6OK, err := c.validatePodIP(pod.Name, podNet.Subnet.Name, ipv4, ipv6)
			if err != nil {
				return "", "", "", podNet.Subnet, err
			}
			if ipv4OK && ipv6OK {
				return ipv4, ipv6, mac, podNet.Subnet, nil
			}

			if !ipv4OK {
				skippedAddrs = append(skippedAddrs, ipv4)
			}
			if !ipv6OK {
				skippedAddrs = append(skippedAddrs, ipv6)
			}
		}
	}

	portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)

	// The static ip can be assigned from any subnet after ns supports multi subnets
	nsNets, _ := c.getNsAvailableSubnets(pod, podNet)
	var v4IP, v6IP, mac string
	var err error

	// Static allocate
	if pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)] != "" {
		ipStr := pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)]

		for _, net := range nsNets {
			v4IP, v6IP, mac, err = c.acquireStaticAddress(key, portName, ipStr, macStr, net.Subnet.Name, net.AllowLiveMigration)
			if err == nil {
				return v4IP, v6IP, mac, net.Subnet, nil
			}
		}
		return v4IP, v6IP, mac, podNet.Subnet, err
	}

	// IPPool allocate
	if pod.Annotations[fmt.Sprintf(util.IpPoolAnnotationTemplate, podNet.ProviderName)] != "" {
		var ipPool []string
		if strings.Contains(pod.Annotations[fmt.Sprintf(util.IpPoolAnnotationTemplate, podNet.ProviderName)], ";") {
			ipPool = strings.Split(pod.Annotations[fmt.Sprintf(util.IpPoolAnnotationTemplate, podNet.ProviderName)], ";")
		} else {
			ipPool = strings.Split(pod.Annotations[fmt.Sprintf(util.IpPoolAnnotationTemplate, podNet.ProviderName)], ",")
		}
		for i, ip := range ipPool {
			ipPool[i] = strings.TrimSpace(ip)
		}

		if !isStsPod {
			for _, net := range nsNets {
				for _, staticIP := range ipPool {
					var checkIP string
					ipProtocol := util.CheckProtocol(staticIP)
					if ipProtocol == kubeovnv1.ProtocolDual {
						checkIP = strings.Split(staticIP, ",")[0]
					} else {
						checkIP = staticIP
					}

					if assignedPod, ok := c.ipam.IsIPAssignedToOtherPod(checkIP, net.Subnet.Name, key); ok {
						klog.Errorf("static address %s for %s has been assigned to %s", staticIP, key, assignedPod)
						continue
					}

					v4IP, v6IP, mac, err = c.acquireStaticAddress(key, portName, staticIP, macStr, net.Subnet.Name, net.AllowLiveMigration)
					if err == nil {
						return v4IP, v6IP, mac, net.Subnet, nil
					}
				}
			}
			klog.Errorf("acquire address %s for %s failed, %v", pod.Annotations[fmt.Sprintf(util.IpPoolAnnotationTemplate, podNet.ProviderName)], key, err)
		} else {
			tempStrs := strings.Split(pod.Name, "-")
			numStr := tempStrs[len(tempStrs)-1]
			index, _ := strconv.Atoi(numStr)

			if index < len(ipPool) {
				for _, net := range nsNets {
					v4IP, v6IP, mac, err = c.acquireStaticAddress(key, portName, ipPool[index], macStr, net.Subnet.Name, net.AllowLiveMigration)
					if err == nil {
						return v4IP, v6IP, mac, net.Subnet, nil
					}
				}
				klog.Errorf("acquire address %s for %s failed, %v", ipPool[index], key, err)
			}
		}
	}
	klog.Errorf("alloc address for %s failed, return NoAvailableAddress", key)
	return "", "", "", podNet.Subnet, ipam.ErrNoAvailable
}

func (c *Controller) acquireStaticAddress(key, nicName, ip, mac, subnet string, liveMigration bool) (string, string, string, error) {
	var v4IP, v6IP string
	var err error
	for _, ipStr := range strings.Split(ip, ",") {
		if net.ParseIP(ipStr) == nil {
			return "", "", "", fmt.Errorf("failed to parse IP %s", ipStr)
		}
	}

	if v4IP, v6IP, mac, err = c.ipam.GetStaticAddress(key, nicName, ip, mac, subnet, !liveMigration); err != nil {
		klog.Errorf("failed to get static ip %v, mac %v, subnet %v, err %v", ip, mac, subnet, err)
		return "", "", "", err
	}
	return v4IP, v6IP, mac, nil
}

func appendCheckPodToDel(c *Controller, pod *v1.Pod, ownerRefName, ownerRefKind string) (bool, error) {
	// subnet for ns has been changed, and statefulset pod's ip is not in the range of subnet's cidr anymore
	podNs, err := c.namespacesLister.Get(pod.Namespace)
	if err != nil {
		klog.Errorf("failed to get namespace %s, %v", pod.Namespace, err)
		return false, err
	}

	// check if subnet exist in OwnerReference
	var ownerRefSubnetExist bool
	var ownerRefSubnet string
	switch ownerRefKind {
	case "StatefulSet":
		ss, err := c.config.KubeClient.AppsV1().StatefulSets(pod.Namespace).Get(context.Background(), ownerRefName, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return true, nil
			} else {
				klog.Errorf("failed to get StatefulSet %s, %v", ownerRefName, err)
			}
		}
		if ss.Spec.Template.ObjectMeta.Annotations[util.LogicalSwitchAnnotation] != "" {
			ownerRefSubnetExist = true
			ownerRefSubnet = ss.Spec.Template.ObjectMeta.Annotations[util.LogicalSwitchAnnotation]
		}

	case util.VmInstance:
		vm, err := c.config.KubevirtClient.VirtualMachine(pod.Namespace).Get(ownerRefName, &metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return true, nil
			} else {
				klog.Errorf("failed to get VirtualMachine %s, %v", ownerRefName, err)
			}
		}
		if vm != nil &&
			vm.Spec.Template != nil &&
			vm.Spec.Template.ObjectMeta.Annotations != nil &&
			vm.Spec.Template.ObjectMeta.Annotations[util.LogicalSwitchAnnotation] != "" {
			ownerRefSubnetExist = true
			ownerRefSubnet = vm.Spec.Template.ObjectMeta.Annotations[util.LogicalSwitchAnnotation]
		}
	}

	if !ownerRefSubnetExist {
		subnetNames := podNs.Annotations[util.LogicalSwitchAnnotation]
		if subnetNames != "" && pod.Annotations[util.LogicalSwitchAnnotation] != "" && !util.ContainsString(strings.Split(subnetNames, ","), strings.TrimSpace(pod.Annotations[util.LogicalSwitchAnnotation])) {
			klog.Infof("ns %s annotation subnet is %s, which is inconstant with subnet for pod %s, delete pod", pod.Namespace, podNs.Annotations[util.LogicalSwitchAnnotation], pod.Name)
			return true, nil
		}
	}

	// subnet cidr has been changed, and statefulset pod's ip is not in the range of subnet's cidr anymore
	podSubnet, err := c.subnetsLister.Get(strings.TrimSpace(pod.Annotations[util.LogicalSwitchAnnotation]))
	if err != nil {
		klog.Errorf("failed to get subnet %s, %v", pod.Annotations[util.LogicalSwitchAnnotation], err)
		return false, err
	}
	if podSubnet != nil && !util.CIDRContainIP(podSubnet.Spec.CIDRBlock, pod.Annotations[util.IpAddressAnnotation]) {
		klog.Infof("pod's ip %s is not in the range of subnet %s, delete pod", pod.Annotations[util.IpAddressAnnotation], podSubnet.Name)
		return true, nil
	}
	// subnet of ownerReference(sts/vm) has been changed, it needs to handle delete pod and create port on the new logical switch
	if podSubnet != nil && ownerRefSubnet != "" && podSubnet.Name != ownerRefSubnet {
		klog.Infof("Subnet of owner %s has been changed from %s to %s, delete pod %s/%s", ownerRefName, podSubnet.Name, ownerRefSubnet, pod.Namespace, pod.Name)
		return true, nil
	}

	return false, nil
}

func isVmPod(pod *v1.Pod) (bool, string) {
	for _, owner := range pod.OwnerReferences {
		// The name of vmi is consistent with vm's name.
		if owner.Kind == util.VmInstance && strings.HasPrefix(owner.APIVersion, "kubevirt.io") {
			return true, owner.Name
		}
	}
	return false, ""
}

func isOwnsByTheVM(vmi metav1.Object) (bool, string) {
	for _, owner := range vmi.GetOwnerReferences() {
		if owner.Kind == util.Vm && strings.HasPrefix(owner.APIVersion, "kubevirt.io") {
			return true, owner.Name
		}
	}
	return false, ""
}

func (c *Controller) isVmPodToDel(pod *v1.Pod, vmiName string) bool {
	var (
		vmiAlive bool
		vmName   string
	)
	// The vmi is also deleted when pod is deleted, only left vm exists.
	vmi, err := c.config.KubevirtClient.VirtualMachineInstance(pod.Namespace).Get(vmiName, &metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			vmiAlive = false
			// The name of vmi is consistent with vm's name.
			vmName = vmiName
			klog.V(4).ErrorS(err, "failed to get vmi, will try to get the vm directly", "name", vmiName)
		} else {
			klog.ErrorS(err, "failed to get vmi", "name", vmiName)
			return false
		}
	} else {
		var ownsByVM bool
		ownsByVM, vmName = isOwnsByTheVM(vmi)
		if !ownsByVM && vmi.DeletionTimestamp != nil {
			// deleting ephemeral vmi
			return true
		}
		vmiAlive = (vmi.DeletionTimestamp == nil)
	}

	if vmiAlive {
		return false
	}

	vm, err := c.config.KubevirtClient.VirtualMachine(pod.Namespace).Get(vmName, &metav1.GetOptions{})
	if err != nil {
		// the vm has gone
		if k8serrors.IsNotFound(err) {
			klog.V(4).ErrorS(err, "failed to get vm", "name", vmName)
			return true
		} else {
			klog.ErrorS(err, "failed to get vm", "name", vmName)
		}
		return false
	}

	// vm is deleting
	if vm.DeletionTimestamp != nil {
		return true
	}

	return false
}

func (c *Controller) getNameByPod(pod *v1.Pod) string {
	if c.config.EnableKeepVmIP {
		if isVmPod, vmName := isVmPod(pod); isVmPod {
			return vmName
		}
	}
	return pod.Name
}

func (c *Controller) getNsAvailableSubnets(pod *v1.Pod, podNet *kubeovnNet) ([]*kubeovnNet, error) {
	var result []*kubeovnNet
	// keep the annotation subnet of the pod in first position
	result = append(result, podNet)

	ns, err := c.namespacesLister.Get(pod.Namespace)
	if err != nil {
		klog.Errorf("failed to get namespace %s, %v", pod.Namespace, err)
		return nil, err
	}
	if ns.Annotations == nil {
		return nil, nil
	}

	subnetNames := ns.Annotations[util.LogicalSwitchAnnotation]
	for _, subnetName := range strings.Split(subnetNames, ",") {
		if subnetName == "" || subnetName == podNet.Subnet.Name {
			continue
		}
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			klog.Errorf("failed to get subnet %v", err)
			return nil, err
		}

		switch subnet.Spec.Protocol {
		case kubeovnv1.ProtocolIPv4:
			fallthrough
		case kubeovnv1.ProtocolDual:
			if subnet.Status.V4AvailableIPs == 0 {
				continue
			}
		case kubeovnv1.ProtocolIPv6:
			if subnet.Status.V6AvailableIPs == 0 {
				continue
			}
		}

		result = append(result, &kubeovnNet{
			Type:         providerTypeOriginal,
			ProviderName: subnet.Spec.Provider,
			Subnet:       subnet,
		})
	}

	return result, nil
}

func getPodType(pod *v1.Pod) string {
	if ok, _ := isStatefulSetPod(pod); ok {
		return "StatefulSet"
	}

	if isVmPod, _ := isVmPod(pod); isVmPod {
		return util.Vm
	}
	return ""
}
