package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/intel/multus-cni/logging"
	multustypes "github.com/intel/multus-cni/types"
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
	if c.config.EnableNP && p.Status.PodIP != "" {
		for _, np := range c.podMatchNetworkPolicies(p) {
			c.updateNpQueue.Add(np)
		}
	}

	if p.Spec.HostNetwork {
		return
	}

	if !isPodAlive(p) {
		isStateful, statefulSetName := isStatefulSetPod(p)
		if isStateful {
			if isStatefulSetPodToDel(c.config.KubeClient, p, statefulSetName) {
				klog.V(3).Infof("enqueue delete pod %s", key)
				c.deletePodQueue.Add(obj)
			}
		} else {
			klog.V(3).Infof("enqueue delete pod %s", key)
			c.deletePodQueue.Add(obj)
		}
		return
	}

	podNets, err := c.getPodKubeovnNets(p)
	if err != nil {
		klog.Errorf("failed to get pod nets %v", err)
		return
	}
	// In case update event might lost during leader election
	if p.Annotations != nil &&
		p.Annotations[util.AllocatedAnnotation] == "true" &&
		p.Status.HostIP != "" && p.Status.PodIP != "" {
		for _, podNet := range podNets {
			if !isOvnSubnet(podNet.Subnet) {
				continue
			}

			if p.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, podNet.ProviderName)] != "true" {
				c.updatePodQueue.Add(key)
				break
			}
		}
		return
	}

	if p.Annotations != nil && p.Annotations[util.AllocatedAnnotation] == "true" {
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
	if c.config.EnableNP {
		for _, np := range c.podMatchNetworkPolicies(p) {
			c.updateNpQueue.Add(np)
		}
	}

	if p.Spec.HostNetwork {
		return
	}

	isStateful, statefulSetName := isStatefulSetPod(p)
	if isStateful {
		if isStatefulSetPodToDel(c.config.KubeClient, p, statefulSetName) {
			klog.V(3).Infof("enqueue delete pod %s", key)
			c.deletePodQueue.Add(obj)
		}
	} else {
		klog.V(3).Infof("enqueue delete pod %s", key)
		c.deletePodQueue.Add(obj)
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

	if c.config.EnableNP {
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

	isStateful, statefulSetName := isStatefulSetPod(newPod)
	if !isPodAlive(newPod) && !isStateful {
		klog.V(3).Infof("enqueue delete pod %s", key)
		c.deletePodQueue.Add(newObj)
		return
	}

	if newPod.DeletionTimestamp != nil && !isStateful {
		go func() {
			// In case node get lost and pod can not be deleted,
			// the ipaddress will not be recycled
			time.Sleep(time.Duration(*newPod.Spec.TerminationGracePeriodSeconds) * time.Second)
			c.deletePodQueue.Add(newObj)
		}()
		return
	}

	// do not delete statefulset pod unless ownerReferences is deleted
	if isStateful {
		if isStatefulSetPodToDel(c.config.KubeClient, newPod, statefulSetName) {
			klog.V(3).Infof("enqueue delete pod %s", key)
			c.deletePodQueue.Add(newObj)
			return
		}
	}

	podNets, err := c.getPodKubeovnNets(newPod)
	if err != nil {
		klog.Errorf("failed to get pod nets %v", err)
		return
	}

	// pod assigned an ip
	if newPod.Annotations[util.AllocatedAnnotation] == "true" &&
		newPod.Spec.NodeName != "" {
		for _, podNet := range podNets {
			if !isOvnSubnet(podNet.Subnet) {
				continue
			}

			if newPod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, podNet.ProviderName)] != "true" {
				klog.V(3).Infof("enqueue update pod %s", key)
				c.updatePodQueue.Add(key)
				break
			}
		}
	}

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

func (c *Controller) runAddPodWorker() {
	for c.processNextAddPodWorkItem() {
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

func (c *Controller) runUpdatePodSecurityWorker() {
	for c.processNextUpdatePodSecurityWorkItem() {
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
		klog.Infof("handle add pod %s", key)
		if err := c.handleAddPod(key); err != nil {
			c.addPodQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		last := time.Since(now)
		klog.Infof("take %d ms to handle add pod %s", last.Milliseconds(), key)
		c.addPodQueue.Forget(obj)
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
		klog.Infof("handle delete pod %s", pod.Name)
		if err := c.handleDeletePod(pod); err != nil {
			c.deletePodQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s': %s, requeuing", pod.Name, err.Error())
		}
		c.deletePodQueue.Forget(obj)
		last := time.Since(now)
		klog.Infof("take %d ms to handle delete pod %s", last.Milliseconds(), pod.Name)
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

func (c *Controller) handleAddPod(key string) error {
	c.podKeyMutex.Lock(key)
	defer c.podKeyMutex.Unlock(key)
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	oripod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	pod := oripod.DeepCopy()
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

	op := "replace"
	if pod.Annotations == nil || len(pod.Annotations) == 0 {
		op = "add"
		pod.Annotations = map[string]string{}
	}

	// Avoid create lsp for already running pod in ovn-nb when controller restart
	for _, podNet := range needAllocateSubnets(pod, podNets) {
		subnet := podNet.Subnet
		v4IP, v6IP, mac, err := c.acquireAddress(pod, podNet)
		if err != nil {
			c.recorder.Eventf(pod, v1.EventTypeWarning, "AcquireAddressFailed", err.Error())
			return err
		}
		ipStr := util.GetStringIP(v4IP, v6IP)
		if subnet.Spec.Vlan != "" {
			pod.Annotations[fmt.Sprintf(util.NetworkTypeTemplate, podNet.ProviderName)] = util.NetworkTypeVlan
		} else {
			pod.Annotations[fmt.Sprintf(util.NetworkTypeTemplate, podNet.ProviderName)] = util.NetworkTypeGeneve
		}
		pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)] = ipStr
		pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)] = mac
		pod.Annotations[fmt.Sprintf(util.CidrAnnotationTemplate, podNet.ProviderName)] = subnet.Spec.CIDRBlock
		pod.Annotations[fmt.Sprintf(util.GatewayAnnotationTemplate, podNet.ProviderName)] = subnet.Spec.Gateway
		pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, podNet.ProviderName)] = subnet.Name
		pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] = "true"
		if pod.Annotations[util.PodNicAnnotation] == "" {
			pod.Annotations[util.PodNicAnnotation] = c.config.PodNicType
		}

		if err := util.ValidatePodCidr(podNet.Subnet.Spec.CIDRBlock, ipStr); err != nil {
			klog.Errorf("validate pod %s/%s failed: %v", namespace, name, err)
			c.recorder.Eventf(pod, v1.EventTypeWarning, "ValidatePodNetworkFailed", err.Error())
			return err
		}

		if podNet.Type != providerTypeIPAM {
			if (subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) && subnet.Spec.Vpc != "" {
				pod.Annotations[fmt.Sprintf(util.LogicalRouterAnnotationTemplate, podNet.ProviderName)] = subnet.Spec.Vpc
			}

			if subnet.Spec.Vlan != "" {
				vlan, err := c.vlansLister.Get(subnet.Spec.Vlan)
				if err != nil {
					c.recorder.Eventf(pod, v1.EventTypeWarning, "GetVlanInfoFailed", err.Error())
					return err
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
			portName := ovs.PodNameToPortName(name, namespace, podNet.ProviderName)
			if err := c.ovnClient.CreatePort(subnet.Name, portName, ipStr, mac, pod.Name, pod.Namespace, portSecurity, securityGroupAnnotation, vips); err != nil {
				c.recorder.Eventf(pod, v1.EventTypeWarning, "CreateOVNPortFailed", err.Error())
				return err
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
		}
	}

	if _, err := c.config.KubeClient.CoreV1().Pods(namespace).Patch(context.Background(), name, types.JSONPatchType, generatePatchPayload(pod.Annotations, op), metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			// Sometimes pod is deleted between kube-ovn configure ovn-nb and patch pod.
			// Then we need to recycle the resource again.
			c.deletePodQueue.AddRateLimited(key)
			return nil
		}
		klog.Errorf("patch pod %s/%s failed: %v", name, namespace, err)
		return err
	}

	if vpcGwName, isVpcNatGw := pod.Annotations[util.VpcNatGatewayAnnotation]; isVpcNatGw {
		c.initVpcNatGatewayQueue.Add(vpcGwName)
	}
	return nil
}

func (c *Controller) handleDeletePod(pod *v1.Pod) error {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(pod); err != nil {
		return err
	}
	c.podKeyMutex.Lock(key)
	defer c.podKeyMutex.Unlock(key)

	p, _ := c.podsLister.Pods(pod.Namespace).Get(pod.Name)
	if p != nil && p.UID != pod.UID {
		// Pod with same name exists, just return here
		return nil
	}

	addresses := c.ipam.GetPodAddress(key)
	for _, address := range addresses {
		if strings.TrimSpace(address.Ip) == "" {
			continue
		}
		subnet, err := c.subnetsLister.Get(address.Subnet.Name)
		if err != nil {
			return err
		}
		vpc, err := c.vpcsLister.Get(subnet.Spec.Vpc)
		if err != nil {
			return err
		}
		if err := c.ovnClient.DeleteStaticRoute(address.Ip, vpc.Status.Router); err != nil {
			return err
		}
		if err := c.ovnClient.DeleteNatRule(address.Ip, vpc.Status.Router); err != nil {
			return err
		}
	}

	ports, err := c.ovnClient.ListPodLogicalSwitchPorts(pod.Name, pod.Namespace)
	if err != nil {
		klog.Errorf("failed to list lsps of pod '%s', %v", pod.Name, err)
		return err
	}

	var keepIpCR bool
	if ok, sts := isStatefulSetPod(pod); ok {
		keepIpCR = !isStatefulSetPodToDel(c.config.KubeClient, pod, sts)
	}

	// Add additional default ports to compatible with previous versions
	ports = append(ports, ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider))
	for _, portName := range ports {
		sgs, err := c.getPortSg(portName)
		if err != nil {
			klog.Warningf("filed to get port '%s' sg, %v", portName, err)
		}
		if err := c.ovnClient.DeleteLogicalSwitchPort(portName); err != nil {
			klog.Errorf("failed to delete lsp %s, %v", portName, err)
			return err
		}
		if !keepIpCR {
			if err = c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), portName, metav1.DeleteOptions{}); err != nil {
				if !k8serrors.IsNotFound(err) {
					klog.Errorf("failed to delete ip %s, %v", portName, err)
					return err
				}
			}
		}
		for _, sg := range sgs {
			c.syncSgPortsQueue.Add(sg)
		}
	}
	if !keepIpCR {
		if err = c.deleteAttachmentNetWorkIP(pod); err != nil {
			klog.Errorf("failed to delete attach ip for pod %v, %v, please delete attach ip manually", pod.Name, err)
		}
	}
	c.ipam.ReleaseAddressByPod(key)
	return nil
}

func (c *Controller) handleUpdatePodSecurity(key string) error {
	c.podKeyMutex.Lock(key)
	defer c.podKeyMutex.Unlock(key)
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
		if portSecurity {
			securityGroupAnnotation := pod.Annotations[fmt.Sprintf(util.SecurityGroupAnnotationTemplate, podNet.ProviderName)]
			portName := ovs.PodNameToPortName(name, namespace, podNet.ProviderName)
			if err = c.reconcilePortSg(portName, securityGroupAnnotation); err != nil {
				klog.Errorf("reconcilePortSg failed. %v", err)
				return err
			}
		}

		mac := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)]
		ipStr := pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)]
		vips := pod.Annotations[fmt.Sprintf(util.PortVipAnnotationTemplate, podNet.ProviderName)]
		if err = c.ovnClient.SetPortSecurity(portSecurity, ovs.PodNameToPortName(name, namespace, podNet.ProviderName), mac, ipStr, vips); err != nil {
			klog.Errorf("setPortSecurity failed. %v", err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleUpdatePod(key string) error {
	c.podKeyMutex.Lock(key)
	defer c.podKeyMutex.Unlock(key)
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	oripod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	pod := oripod.DeepCopy()

	// in case update handler overlap the annotation when cache is not in sync
	if pod.Annotations[util.AllocatedAnnotation] == "" {
		return fmt.Errorf("no address has been allocated to %s/%s", namespace, name)
	}

	klog.Infof("update pod %s/%s", namespace, name)

	var podIP string
	var subnet *kubeovnv1.Subnet
	podNets, err := c.getPodKubeovnNets(pod)
	if err != nil {
		klog.Errorf("failed to pod nets %v", err)
		return err
	}

	for _, podNet := range podNets {
		if !isOvnSubnet(podNet.Subnet) {
			continue
		}
		podIP = pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)]
		subnet = podNet.Subnet

		if podIP != "" && subnet.Spec.Vlan == "" && subnet.Spec.Vpc == util.DefaultVpc {
			if pod.Annotations[util.EipAnnotation] != "" || pod.Annotations[util.SnatAnnotation] != "" {
				cm, err := c.configMapsLister.ConfigMaps("kube-system").Get(util.ExternalGatewayConfig)
				if err != nil {
					klog.Errorf("failed to get ex-gateway config, %v", err)
					return err
				}
				nextHop := cm.Data["nic-ip"]
				if nextHop == "" {
					klog.Errorf("no available gateway nic address")
					return fmt.Errorf("no available gateway nic address")
				}
				if !strings.Contains(nextHop, "/") {
					klog.Errorf("gateway nic address's format is invalid")
					return fmt.Errorf("gateway nic address's format is invalid")
				}
				nextHop = strings.Split(nextHop, "/")[0]
				if addr := cm.Data["external-gw-addr"]; addr != "" {
					nextHop = addr
				}

				if err := c.ovnClient.AddStaticRoute(ovs.PolicySrcIP, podIP, nextHop, c.config.ClusterRouter, util.NormalRouteType); err != nil {
					klog.Errorf("failed to add static route, %v", err)
					return err
				}
			} else {
				if subnet.Spec.GatewayType == kubeovnv1.GWDistributedType && pod.Annotations[util.NorthGatewayAnnotation] == "" {
					node, err := c.nodesLister.Get(pod.Spec.NodeName)
					if err != nil {
						klog.Errorf("get node %s failed %v", pod.Spec.NodeName, err)
						return err
					}
					nodeTunlIPAddr, err := getNodeTunlIP(node)
					if err != nil {
						return err
					}

					for _, nodeAddr := range nodeTunlIPAddr {
						for _, podAddr := range strings.Split(podIP, ",") {
							if util.CheckProtocol(nodeAddr.String()) != util.CheckProtocol(podAddr) {
								continue
							}
							if err := c.ovnClient.AddStaticRoute(ovs.PolicySrcIP, podAddr, nodeAddr.String(), c.config.ClusterRouter, util.NormalRouteType); err != nil {
								klog.Errorf("failed to add static route, %v", err)
								return err
							}
						}
					}
				}

				if pod.Annotations[util.NorthGatewayAnnotation] != "" {
					if err := c.ovnClient.AddStaticRoute(ovs.PolicySrcIP, podIP, pod.Annotations[util.NorthGatewayAnnotation], c.config.ClusterRouter, util.NormalRouteType); err != nil {
						klog.Errorf("failed to add static route, %v", err)
						return err
					}
				}
			}

			for _, ipStr := range strings.Split(podIP, ",") {
				if err := c.ovnClient.UpdateNatRule("dnat_and_snat", ipStr, pod.Annotations[util.EipAnnotation], c.config.ClusterRouter, pod.Annotations[util.MacAddressAnnotation], fmt.Sprintf("%s.%s", pod.Name, pod.Namespace)); err != nil {
					klog.Errorf("failed to add nat rules, %v", err)
					return err
				}

				if err := c.ovnClient.UpdateNatRule("snat", ipStr, pod.Annotations[util.SnatAnnotation], c.config.ClusterRouter, "", ""); err != nil {
					klog.Errorf("failed to add nat rules, %v", err)
					return err
				}
			}
		}

		pod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, podNet.ProviderName)] = "true"
	}
	if _, err := c.config.KubeClient.CoreV1().Pods(namespace).Patch(context.Background(), name, types.JSONPatchType, generatePatchPayload(pod.Annotations, "replace"), metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			// Sometimes pod is deleted between kube-ovn configure ovn-nb and patch pod.
			// Then we need to recycle the resource again.
			c.deletePodQueue.AddRateLimited(key)
			return nil
		}
		klog.Errorf("patch pod %s/%s failed %v", name, namespace, err)
		return err
	}
	return nil
}

func isStatefulSetPod(pod *v1.Pod) (bool, string) {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "StatefulSet" && strings.HasPrefix(owner.APIVersion, "apps/") {
			return true, owner.Name
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

	return index >= int64(*ss.Spec.Replicas) && index >= int64(ss.Status.Replicas)
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
	if pod.Status.Phase == v1.PodSucceeded ||
		pod.Status.Phase == v1.PodFailed {
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

func (c *Controller) getPodDefaultSubnet(pod *v1.Pod) (*kubeovnv1.Subnet, error) {
	var subnetName string
	// 1. check annotation subnet
	lsName, lsExist := pod.Annotations[util.LogicalSwitchAnnotation]
	if lsExist {
		subnetName = lsName
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

		subnetName = ns.Annotations[util.LogicalSwitchAnnotation]
		if subnetName == "" {
			err = fmt.Errorf("namespace %s default logical switch is not found", pod.Namespace)
			klog.Error(err)
			return nil, err
		}
	}

	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		klog.Errorf("failed to get subnet %v", err)
		return nil, err
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
			if isDefault {
				providerName = util.OvnProvider
			} else {
				providerName = fmt.Sprintf("%s.%s.ovn", attach.Name, attach.Namespace)
				if pod.Annotations[fmt.Sprintf(util.LiveMigrationAnnotationTemplate, providerName)] == "true" {
					allowLiveMigration = true
				}
			}
			subnetName := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, providerName)]
			if !isDefault && subnetName == "" {
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

func (c *Controller) acquireAddress(pod *v1.Pod, podNet *kubeovnNet) (string, string, string, error) {
	key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	macStr := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)]

	// Random allocate
	if pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)] == "" &&
		pod.Annotations[fmt.Sprintf(util.IpPoolAnnotationTemplate, podNet.ProviderName)] == "" {
		var skippedAddrs []string
		for {
			nicName := ovs.PodNameToPortName(pod.Name, pod.Namespace, podNet.ProviderName)
			ipv4, ipv6, mac, err := c.ipam.GetRandomAddress(key, nicName, podNet.Subnet.Name, skippedAddrs)
			if err != nil {
				return "", "", "", err
			}

			ipv4OK, ipv6OK, err := c.validatePodIP(pod.Name, podNet.Subnet.Name, ipv4, ipv6)
			if err != nil {
				return "", "", "", err
			}
			if ipv4OK && ipv6OK {
				return ipv4, ipv6, mac, nil
			}

			if !ipv4OK {
				skippedAddrs = append(skippedAddrs, ipv4)
			}
			if !ipv6OK {
				skippedAddrs = append(skippedAddrs, ipv6)
			}
		}
	}

	nicName := ovs.PodNameToPortName(pod.Name, pod.Namespace, podNet.ProviderName)
	// Static allocate
	if pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)] != "" {
		ipStr := pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)]
		return c.acquireStaticAddress(key, nicName, ipStr, macStr, podNet.Subnet.Name, podNet.AllowLiveMigration)
	}

	// IPPool allocate
	ipPool := strings.Split(pod.Annotations[fmt.Sprintf(util.IpPoolAnnotationTemplate, podNet.ProviderName)], ",")
	for i, ip := range ipPool {
		ipPool[i] = strings.TrimSpace(ip)
	}

	if ok, _ := isStatefulSetPod(pod); !ok {
		for _, staticIP := range ipPool {
			if c.ipam.IsIPAssignedToPod(staticIP, podNet.Subnet.Name) {
				klog.Errorf("static address %s for %s has been assigned", staticIP, key)
				continue
			}
			if v4IP, v6IP, mac, err := c.acquireStaticAddress(key, nicName, staticIP, macStr, podNet.Subnet.Name, podNet.AllowLiveMigration); err == nil {
				return v4IP, v6IP, mac, nil
			} else {
				klog.Errorf("acquire address %s for %s failed, %v", staticIP, key, err)
			}
		}
	} else {
		tempStrs := strings.Split(pod.Name, "-")
		numStr := tempStrs[len(tempStrs)-1]
		index, _ := strconv.Atoi(numStr)
		if index < len(ipPool) {
			return c.acquireStaticAddress(key, nicName, ipPool[index], macStr, podNet.Subnet.Name, podNet.AllowLiveMigration)
		}
	}
	klog.Errorf("alloc address for %s failed, return NoAvailableAddress", key)
	return "", "", "", ipam.ErrNoAvailable
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

func (c *Controller) deleteAttachmentNetWorkIP(pod *v1.Pod) error {
	var providers []string
	for k, v := range pod.Annotations {
		if !strings.Contains(k, util.AllocatedAnnotationSuffix) || v != "true" {
			continue
		}
		providerName := strings.ReplaceAll(k, util.AllocatedAnnotationSuffix, "")
		if providerName == util.OvnProvider {
			continue
		} else {
			providers = append(providers, providerName)
		}
	}
	if len(providers) == 0 {
		return nil
	}
	klog.Infof("providers are %v for pod %v", providers, pod.Name)

	for _, providerName := range providers {
		portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, providerName)
		if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), portName, metav1.DeleteOptions{}); err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to delete ip %s, %v", portName, err)
				return err
			}
		}
	}
	return nil
}
