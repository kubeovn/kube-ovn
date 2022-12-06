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

	podNets, err := c.getPodKubeovnNets(p)
	if err != nil {
		klog.Errorf("pod not managed by ovn? failed to get pod nets %v", err)
		c.addPodQueue.Add(key)
		return
	}
	// In case update event might lost during leader election
	for _, podNet := range podNets {
		if !isOvnSubnet(podNet.Subnet) {
			continue
		}

		if p.Annotations != nil &&
			p.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "true" &&
			p.Status.HostIP != "" && p.Status.PodIP != "" {
			if p.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, podNet.ProviderName)] != "true" {
				c.updatePodQueue.Add(key)
				return
			}
		}

		if p.Annotations != nil && p.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "true" {
			return
		}
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
	isVmPod, vmName := isVmPod(p)
	if isStateful {
		if isStatefulSetPodToDel(c.config.KubeClient, p, statefulSetName) {
			klog.V(3).Infof("enqueue delete pod %s", key)
			c.deletePodQueue.Add(obj)
		}

		if delete, err := appendCheckPodToDel(c, p, statefulSetName, "StatefulSet"); delete && err == nil {
			klog.V(3).Infof("enqueue delete pod %s", key)
			c.deletePodQueue.Add(obj)
		}
	} else if isVmPod && c.config.EnableKeepVmIP {
		if c.isVmPodToDel(p, vmName) {
			klog.V(3).Infof("enqueue delete pod %s", key)
			c.deletePodQueue.Add(obj)
		}
		if delete, err := appendCheckPodToDel(c, p, vmName, util.VmInstance); delete && err == nil {
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
	isVmPod, vmName := isVmPod(newPod)
	if !isPodAlive(newPod) && !isStateful && !isVmPod {
		klog.V(3).Infof("enqueue delete pod %s", key)
		c.deletePodQueue.Add(newObj)
		return
	}

	if newPod.DeletionTimestamp != nil && !isStateful && !isVmPod {
		go func() {
			// In case node get lost and pod can not be deleted,
			// the ipaddress will not be recycled
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

	podNets, err := c.getPodKubeovnNets(newPod)
	if err != nil {
		klog.Errorf("failed to get pod nets %v", err)
		return
	}

	// pod assigned an ip
	for _, podNet := range podNets {
		if !isOvnSubnet(podNet.Subnet) {
			continue
		}

		if newPod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "true" && newPod.Spec.NodeName != "" {
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

func (c *Controller) processNextUpdatePodWorkItem() bool {
	obj, shutdown := c.updatePodQueue.Get()

	if shutdown {
		return false
	}

	now := time.Now()
	err := func(obj interface{}) error {
		defer c.updatePodQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updatePodQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		klog.Infof("handle update pod %s", key)
		if err := c.handleUpdatePod(key); err != nil {
			c.updatePodQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updatePodQueue.Forget(obj)
		last := time.Since(now)
		klog.Infof("take %d ms to handle update pod %s", last.Milliseconds(), key)
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
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.podKeyMutex.Lock(key)
	defer c.podKeyMutex.Unlock(key)

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

	oriPod := pod.DeepCopy()
	if len(pod.Annotations) == 0 {
		pod.Annotations = map[string]string{}
	}
	isVmPod, vmName := isVmPod(pod)

	// Avoid create lsp for already running pod in ovn-nb when controller restart
	for _, podNet := range needAllocateSubnets(pod, podNets) {
		// the subnet may changed when alloc static ip from the latter subnet after ns supports multi subnets
		v4IP, v6IP, mac, subnet, err := c.acquireAddress(pod, podNet)
		if err != nil {
			c.recorder.Eventf(pod, v1.EventTypeWarning, "AcquireAddressFailed", err.Error())
			return err
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
		}

		if err := util.ValidatePodCidr(podNet.Subnet.Spec.CIDRBlock, ipStr); err != nil {
			klog.Errorf("validate pod %s/%s failed: %v", namespace, name, err)
			c.recorder.Eventf(pod, v1.EventTypeWarning, "ValidatePodNetworkFailed", err.Error())
			return err
		}

		podType := getPodType(pod)
		podName := c.getNameByPod(pod)
		if err := c.createOrUpdateCrdIPs(podName, ipStr, mac, subnet.Name, pod.Namespace, pod.Spec.NodeName, podNet.ProviderName, podType, nil); err != nil {
			klog.Errorf("failed to create IP %s.%s: %v", podName, pod.Namespace, err)
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

			portName := ovs.PodNameToPortName(podName, namespace, podNet.ProviderName)
			dhcpOptions := &ovs.DHCPOptionsUUIDs{
				DHCPv4OptionsUUID: subnet.Status.DHCPv4OptionsUUID,
				DHCPv6OptionsUUID: subnet.Status.DHCPv6OptionsUUID,
			}

			hasUnknown := pod.Annotations[fmt.Sprintf(util.Layer2ForwardAnnotationTemplate, podNet.ProviderName)] == "true"
			if err := c.ovnLegacyClient.CreatePort(subnet.Name, portName, ipStr, mac, podName, pod.Namespace, portSecurity, securityGroupAnnotation, vips, podNet.AllowLiveMigration, podNet.Subnet.Spec.EnableDHCP, dhcpOptions, hasUnknown); err != nil {
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

	patch, err := util.GenerateStrategicMergePatchPayload(oriPod, pod)
	if err != nil {
		return err
	}
	if _, err := c.config.KubeClient.CoreV1().Pods(namespace).Patch(context.Background(), name,
		types.StrategicMergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			// Sometimes pod is deleted between kube-ovn configure ovn-nb and patch pod.
			// Then we need to recycle the resource again.
			c.deletePodQueue.AddRateLimited(pod)
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

	podName := c.getNameByPod(pod)
	key = fmt.Sprintf("%s/%s", pod.Namespace, podName)
	c.podKeyMutex.Lock(key)
	defer c.podKeyMutex.Unlock(key)

	p, _ := c.podsLister.Pods(pod.Namespace).Get(pod.Name)
	if p != nil && p.UID != pod.UID {
		// Pod with same name exists, just return here
		return nil
	}

	ports, err := c.ovnClient.ListPodLogicalSwitchPorts(key)
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
		delete, err := appendCheckPodToDel(c, pod, sts, "StatefulSet")
		keepIpCR = !isStatefulSetPodToDel(c.config.KubeClient, pod, sts) && !delete && err == nil
	}

	for _, port := range ports {
		sgs, err := c.getPortSg(&port)
		if err != nil {
			klog.Warningf("failed to get port '%s' sg, %v", port.Name, err)
		}
		// when lsp is deleted, the port of pod is deleted from any port-group automatically.
		if err := c.ovnLegacyClient.DeleteLogicalSwitchPort(port.Name); err != nil {
			klog.Errorf("failed to delete lsp %s, %v", port.Name, err)
			return err
		}
		for _, sg := range sgs {
			c.syncSgPortsQueue.Add(sg)
		}
	}
	podNets, err := c.getPodKubeovnNets(pod)
	if err != nil {
		klog.Errorf("failed to get pod nets %v", err)
	}
	if !keepIpCR {
		for _, podNet := range podNets {
			if err = c.deleteCrdIPs(pod.Name, pod.Namespace, podNet.ProviderName); err != nil {
				klog.Errorf("failed to delete ip for pod %s, %v, please delete manually", pod.Name, err)
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
		if err = c.ovnLegacyClient.SetPortSecurity(portSecurity, podNet.Subnet.Name, ovs.PodNameToPortName(podName, namespace, podNet.ProviderName), mac, ipStr, vips); err != nil {
			klog.Errorf("setPortSecurity failed. %v", err)
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

func (c *Controller) handleUpdatePod(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.podKeyMutex.Lock(key)
	defer c.podKeyMutex.Unlock(key)

	oriPod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	pod := oriPod.DeepCopy()
	podName := c.getNameByPod(pod)

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

				if err := c.ovnLegacyClient.AddStaticRoute(ovs.PolicySrcIP, podIP, nextHop, c.config.ClusterRouter, util.NormalRouteType); err != nil {
					klog.Errorf("failed to add static route, %v", err)
					return err
				}

				// remove lsp from port group to make EIP/SNAT work
				portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
				c.ovnPgKeyMutex.Lock(pgName)
				if err = c.ovnClient.PortGroupRemovePort(pgName, portName); err != nil {
					c.ovnPgKeyMutex.Unlock(pgName)
					return err
				}
				c.ovnPgKeyMutex.Unlock(pgName)

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
							c.ovnPgKeyMutex.Lock(pgName)
							if err = c.ovnClient.PortGroupAddPort(pgName, portName); err != nil {
								c.ovnPgKeyMutex.Unlock(pgName)
								return err
							}
							c.ovnPgKeyMutex.Unlock(pgName)

							added = true
							break
						}
						if added {
							break
						}
					}
				}

				if pod.Annotations[util.NorthGatewayAnnotation] != "" {
					if err := c.ovnLegacyClient.AddStaticRoute(ovs.PolicySrcIP, podIP, pod.Annotations[util.NorthGatewayAnnotation], c.config.ClusterRouter, util.NormalRouteType); err != nil {
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
	patch, err := util.GenerateStrategicMergePatchPayload(oriPod, pod)
	if err != nil {
		return err
	}
	if _, err := c.config.KubeClient.CoreV1().Pods(namespace).Patch(context.Background(), name,
		types.StrategicMergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
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
			nicName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)

			ipv4, ipv6, mac, err := c.ipam.GetRandomAddress(key, nicName, macStr, podNet.Subnet.Name, skippedAddrs, !podNet.AllowLiveMigration)
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

	nicName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)

	// The static ip can be assigned from any subnet after ns supports multi subnets
	nsNets, _ := c.getNsAvailableSubnets(pod)
	found := false
	for _, nsNet := range nsNets {
		if nsNet.Subnet.Name == podNet.Subnet.Name {
			found = true
			break
		}
	}
	if !found {
		nsNets = append(nsNets, podNet)
	}
	var v4IP, v6IP, mac string
	var err error

	// Static allocate
	if pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)] != "" {
		ipStr := pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)]

		for _, net := range nsNets {
			v4IP, v6IP, mac, err = c.acquireStaticAddress(key, nicName, ipStr, macStr, net.Subnet.Name, net.AllowLiveMigration)
			if err == nil {
				return v4IP, v6IP, mac, net.Subnet, nil
			}
		}
		return v4IP, v6IP, mac, podNet.Subnet, err
	}

	// IPPool allocate
	ipPool := strings.Split(pod.Annotations[fmt.Sprintf(util.IpPoolAnnotationTemplate, podNet.ProviderName)], ",")
	for i, ip := range ipPool {
		ipPool[i] = strings.TrimSpace(ip)
	}

	if ok, _ := isStatefulSetPod(pod); !ok {
		for _, net := range nsNets {
			for _, staticIP := range ipPool {
				if c.ipam.IsIPAssignedToPod(staticIP, net.Subnet.Name, key) {
					klog.Errorf("static address %s for %s has been assigned", staticIP, key)
					continue
				}

				v4IP, v6IP, mac, err = c.acquireStaticAddress(key, nicName, staticIP, macStr, net.Subnet.Name, net.AllowLiveMigration)
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
				v4IP, v6IP, mac, err = c.acquireStaticAddress(key, nicName, ipPool[index], macStr, net.Subnet.Name, net.AllowLiveMigration)
				if err == nil {
					return v4IP, v6IP, mac, net.Subnet, nil
				}
			}
			klog.Errorf("acquire address %s for %s failed, %v", ipPool[index], key, err)
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

// syncVmLiveMigrationPort set ip address to lsp after live migration
func (c *Controller) syncVmLiveMigrationPort() {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("list get subnet failed, %v", err)
		return
	}
	for _, subnet := range subnets {
		// lists pods with the 'liveMigration' flag
		ports, err := c.ovnLegacyClient.ListLogicalEntity("logical_switch_port",
			fmt.Sprintf("external_ids:ls=%s", subnet.Name),
			"external_ids:liveMigration=1")
		if err != nil {
			klog.Errorf("list logical_switch_port failed, %v", err)
			return
		}

		for _, port := range ports {
			addr, err := c.ipsLister.Get(port)
			if err != nil {
				klog.Errorf("get port ip failed, %v", err)
				return
			}
			// lists pods with the same IP address
			vmLsps, err := c.ovnLegacyClient.ListLogicalEntity("logical_switch_port",
				fmt.Sprintf("external_ids:ls=%s", subnet.Name),
				fmt.Sprintf("external_ids:ip=\"%s\"", strings.ReplaceAll(addr.Spec.IPAddress, ",", "/")))
			if err != nil {
				klog.Errorf("list logical_switch_port failed, %v", err)
				return
			}

			// reset addresses after live Migration
			if len(vmLsps) == 1 {
				if err = c.ovnLegacyClient.SetPortAddress(port, addr.Spec.MacAddress, addr.Spec.IPAddress); err != nil {
					klog.Errorf("set port addresses failed, %v", err)
					return
				}
				if err = c.ovnLegacyClient.SetPortExternalIds(port, "liveMigration", "0"); err != nil {
					klog.Errorf("set port externalIds failed, %v", err)
					return
				}
			}
		}
	}
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
		vmiAlived bool
		vmName    string
	)
	// The vmi is also deleted when pod is deleted, only left vm exists.
	vmi, err := c.config.KubevirtClient.VirtualMachineInstance(pod.Namespace).Get(vmiName, &metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			vmiAlived = false
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
			// deleteting ephemeral vmi
			return true
		}
		vmiAlived = (vmi.DeletionTimestamp == nil)
	}

	if vmiAlived {
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

func (c *Controller) getNsAvailableSubnets(pod *v1.Pod) ([]*kubeovnNet, error) {
	var result []*kubeovnNet

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
		if subnetName == "" {
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
