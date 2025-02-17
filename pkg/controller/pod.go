package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	nadutils "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/utils"
	"github.com/scylladb/go-set/strset"
	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/logging"
	multustypes "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/request"
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

	restartableInitContainers := make([]v1.Container, 0, len(pod.Spec.InitContainers))
	for i := range pod.Spec.InitContainers {
		if pod.Spec.InitContainers[i].RestartPolicy != nil &&
			*pod.Spec.InitContainers[i].RestartPolicy == v1.ContainerRestartPolicyAlways {
			restartableInitContainers = append(restartableInitContainers, pod.Spec.InitContainers[i])
		}
	}

	containers := slices.Concat(restartableInitContainers, pod.Spec.Containers)
	if len(containers) == 0 {
		return
	}

	for _, container := range containers {
		if len(container.Ports) == 0 {
			continue
		}

		for _, port := range container.Ports {
			if port.Name == "" || port.ContainerPort == 0 {
				continue
			}

			if _, ok := n.namedPortMap[ns]; ok {
				if _, ok := n.namedPortMap[ns][port.Name]; ok {
					if n.namedPortMap[ns][port.Name].PortID == port.ContainerPort {
						n.namedPortMap[ns][port.Name].Pods.Add(podName)
					} else {
						klog.Warningf("named port %s has already be defined with portID %d",
							port.Name, n.namedPortMap[ns][port.Name].PortID)
					}
					continue
				}
			} else {
				n.namedPortMap[ns] = make(map[string]*util.NamedPortInfo)
			}
			n.namedPortMap[ns][port.Name] = &util.NamedPortInfo{
				PortID: port.ContainerPort,
				Pods:   strset.New(podName),
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

			if !n.namedPortMap[ns][port.Name].Pods.Has(podName) {
				continue
			}

			n.namedPortMap[ns][port.Name].Pods.Remove(podName)
			if n.namedPortMap[ns][port.Name].Pods.Size() == 0 {
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
			klog.Infof("namespace %s's namedPort portname is %s with info %v", namespace, portName, info)
		}
		return result
	}
	return nil
}

func isPodAlive(p *v1.Pod) bool {
	if !p.DeletionTimestamp.IsZero() && p.DeletionGracePeriodSeconds != nil {
		now := time.Now()
		deletionTime := p.DeletionTimestamp.Time
		gracePeriod := time.Duration(*p.DeletionGracePeriodSeconds) * time.Second
		if now.After(deletionTime.Add(gracePeriod)) {
			return false
		}
	}
	return isPodStatusPhaseAlive(p)
}

func isPodStatusPhaseAlive(p *v1.Pod) bool {
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
	p := obj.(*v1.Pod)
	if p.Spec.HostNetwork {
		return
	}

	// TODO: we need to find a way to reduce duplicated np added to the queue
	if c.config.EnableNP {
		c.namedPort.AddNamedPortByPod(p)
		if p.Status.PodIP != "" {
			for _, np := range c.podMatchNetworkPolicies(p) {
				klog.V(3).Infof("enqueue update network policy %s", np)
				c.updateNpQueue.Add(np)
			}
		}
	}

	key := cache.MetaObjectToName(p).String()
	if !isPodAlive(p) {
		isStateful, statefulSetName, statefulSetUID := isStatefulSetPod(p)
		isVMPod, vmName := isVMPod(p)
		if isStateful || (isVMPod && c.config.EnableKeepVMIP) {
			if isStateful && isStatefulSetPodToDel(c.config.KubeClient, p, statefulSetName, statefulSetUID) {
				klog.V(3).Infof("enqueue delete pod %s", key)
				c.deletingPodObjMap.Store(key, p)
				c.deletePodQueue.Add(key)
			}
			if isVMPod && c.isVMToDel(p, vmName) {
				klog.V(3).Infof("enqueue delete pod %s", key)
				c.deletingPodObjMap.Store(key, p)
				c.deletePodQueue.Add(key)
			}
		} else {
			klog.V(3).Infof("enqueue delete pod %s", key)
			c.deletingPodObjMap.Store(key, p)
			c.deletePodQueue.Add(key)
		}
		return
	}

	need, err := c.podNeedSync(p)
	if err != nil {
		klog.Errorf("invalid pod net: %v", err)
		return
	}
	if need {
		klog.Infof("enqueue add pod %s", key)
		c.addOrUpdatePodQueue.Add(key)
	}
}

func (c *Controller) enqueueDeletePod(obj interface{}) {
	p := obj.(*v1.Pod)
	if p.Spec.HostNetwork {
		return
	}

	if c.config.EnableNP {
		c.namedPort.DeleteNamedPortByPod(p)
		for _, np := range c.podMatchNetworkPolicies(p) {
			c.updateNpQueue.Add(np)
		}
	}

	if c.config.EnableANP {
		podNs, _ := c.namespacesLister.Get(obj.(*v1.Pod).Namespace)
		c.updateAnpsByLabelsMatch(podNs.Labels, obj.(*v1.Pod).Labels)
	}

	key := cache.MetaObjectToName(p).String()
	klog.Infof("enqueue delete pod %s", key)
	c.deletingPodObjMap.Store(key, p)
	c.deletePodQueue.Add(key)
}

func (c *Controller) enqueueUpdatePod(oldObj, newObj interface{}) {
	oldPod := oldObj.(*v1.Pod)
	newPod := newObj.(*v1.Pod)

	if oldPod.Annotations[util.AAPsAnnotation] != "" || newPod.Annotations[util.AAPsAnnotation] != "" {
		oldAAPs := strings.Split(oldPod.Annotations[util.AAPsAnnotation], ",")
		newAAPs := strings.Split(newPod.Annotations[util.AAPsAnnotation], ",")
		var vipNames []string
		for _, vipName := range oldAAPs {
			vipNames = append(vipNames, strings.TrimSpace(vipName))
		}
		for _, vipName := range newAAPs {
			vipName = strings.TrimSpace(vipName)
			if !slices.Contains(vipNames, vipName) {
				vipNames = append(vipNames, vipName)
			}
		}
		for _, vipName := range vipNames {
			if vip, err := c.virtualIpsLister.Get(vipName); err == nil {
				if vip.Spec.Namespace != newPod.Namespace {
					continue
				}
				klog.Infof("enqueue update virtual parents for %s", vipName)
				c.updateVirtualParentsQueue.Add(vipName)
			}
		}
	}

	if oldPod.ResourceVersion == newPod.ResourceVersion {
		return
	}
	if newPod.Spec.HostNetwork {
		return
	}

	podNets, err := c.getPodKubeovnNets(newPod)
	if err != nil {
		klog.Errorf("failed to get newPod nets %v", err)
		return
	}

	key := cache.MetaObjectToName(newPod).String()
	if c.config.EnableNP {
		c.namedPort.AddNamedPortByPod(newPod)
		newNp := c.podMatchNetworkPolicies(newPod)
		if !maps.Equal(oldPod.Labels, newPod.Labels) {
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

	if c.config.EnableANP {
		podNs, _ := c.namespacesLister.Get(newPod.Namespace)
		if !maps.Equal(oldPod.Labels, newPod.Labels) {
			c.updateAnpsByLabelsMatch(podNs.Labels, newPod.Labels)
		}

		for _, podNet := range podNets {
			oldAllocated := oldPod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)]
			newAllocated := newPod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)]
			if oldAllocated != newAllocated {
				c.updateAnpsByLabelsMatch(podNs.Labels, newPod.Labels)
				break
			}
		}
	}

	isStateful, statefulSetName, statefulSetUID := isStatefulSetPod(newPod)
	isVMPod, vmName := isVMPod(newPod)
	if !isPodStatusPhaseAlive(newPod) && !isStateful && !isVMPod {
		klog.V(3).Infof("enqueue delete pod %s", key)
		c.deletingPodObjMap.Store(key, newPod)
		c.deletePodQueue.Add(key)
		return
	}

	// enqueue delay
	var delay time.Duration
	if newPod.Spec.TerminationGracePeriodSeconds != nil {
		if !newPod.DeletionTimestamp.IsZero() {
			delay = time.Until(newPod.DeletionTimestamp.Add(time.Duration(*newPod.Spec.TerminationGracePeriodSeconds) * time.Second))
		} else {
			delay = time.Duration(*newPod.Spec.TerminationGracePeriodSeconds) * time.Second
		}
	}

	if !newPod.DeletionTimestamp.IsZero() && !isStateful && !isVMPod {
		go func() {
			// In case node get lost and pod can not be deleted,
			// the ip address will not be recycled
			klog.V(3).Infof("enqueue delete pod %s after %v", key, delay)
			c.deletingPodObjMap.Store(key, newPod)
			c.deletePodQueue.AddAfter(key, delay)
		}()
		return
	}

	// do not delete statefulset pod unless ownerReferences is deleted
	if isStateful && isStatefulSetPodToDel(c.config.KubeClient, newPod, statefulSetName, statefulSetUID) {
		go func() {
			klog.V(3).Infof("enqueue delete pod %s after %v", key, delay)
			c.deletingPodObjMap.Store(key, newPod)
			c.deletePodQueue.AddAfter(key, delay)
		}()
		return
	}
	if isVMPod && c.isVMToDel(newPod, vmName) {
		go func() {
			klog.V(3).Infof("enqueue delete pod %s after %v", key, delay)
			c.deletingPodObjMap.Store(key, newPod)
			c.deletePodQueue.AddAfter(key, delay)
		}()
		return
	}
	klog.Infof("enqueue update pod %s", key)
	c.addOrUpdatePodQueue.Add(key)

	// security policy changed
	for _, podNet := range podNets {
		oldSecurity := oldPod.Annotations[fmt.Sprintf(util.PortSecurityAnnotationTemplate, podNet.ProviderName)]
		newSecurity := newPod.Annotations[fmt.Sprintf(util.PortSecurityAnnotationTemplate, podNet.ProviderName)]
		oldSg := oldPod.Annotations[fmt.Sprintf(util.SecurityGroupAnnotationTemplate, podNet.ProviderName)]
		newSg := newPod.Annotations[fmt.Sprintf(util.SecurityGroupAnnotationTemplate, podNet.ProviderName)]
		oldVips := oldPod.Annotations[fmt.Sprintf(util.PortVipAnnotationTemplate, podNet.ProviderName)]
		newVips := newPod.Annotations[fmt.Sprintf(util.PortVipAnnotationTemplate, podNet.ProviderName)]
		oldAAPs := oldPod.Annotations[util.AAPsAnnotation]
		newAAPs := newPod.Annotations[util.AAPsAnnotation]
		if oldSecurity != newSecurity || oldSg != newSg || oldVips != newVips || oldAAPs != newAAPs {
			c.updatePodSecurityQueue.Add(key)
			break
		}
	}
}

func (c *Controller) getPodKubeovnNets(pod *v1.Pod) ([]*kubeovnNet, error) {
	defaultSubnet, err := c.getPodDefaultSubnet(pod)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	attachmentNets, err := c.getPodAttachmentNet(pod)
	if err != nil {
		klog.Error(err)
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

func (c *Controller) handleAddOrUpdatePod(key string) (err error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.podKeyMutex.LockKey(key)
	defer func() { _ = c.podKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add/update pod %s", key)

	pod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
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

	// check and do hotnoplug nic
	if pod, err = c.syncKubeOvnNet(pod, podNets); err != nil {
		klog.Errorf("failed to sync pod nets %v", err)
		return err
	}
	if pod == nil {
		// pod has been deleted
		return nil
	}
	needAllocatePodNets := needAllocateSubnets(pod, podNets)
	if len(needAllocatePodNets) != 0 {
		if pod, err = c.reconcileAllocateSubnets(pod, needAllocatePodNets); err != nil {
			klog.Error(err)
			return err
		}
		if pod == nil {
			// pod has been deleted
			return nil
		}
	}

	// check if route subnet is need.
	return c.reconcileRouteSubnets(pod, needRouteSubnets(pod, podNets))
}

// do the same thing as add pod
func (c *Controller) reconcileAllocateSubnets(pod *v1.Pod, needAllocatePodNets []*kubeovnNet) (*v1.Pod, error) {
	namespace := pod.Namespace
	name := pod.Name
	klog.Infof("sync pod %s/%s allocated", namespace, name)

	vipsMap := c.getVirtualIPs(pod, needAllocatePodNets)
	isVMPod, vmName := isVMPod(pod)
	podType := getPodType(pod)
	podName := c.getNameByPod(pod)
	// todo: isVmPod, getPodType, getNameByPod has duplicated logic

	var err error
	var vmKey string
	if isVMPod && c.config.EnableKeepVMIP {
		vmKey = fmt.Sprintf("%s/%s", namespace, vmName)
	}
	// Avoid create lsp for already running pod in ovn-nb when controller restart
	patch := util.KVPatch{}
	for _, podNet := range needAllocatePodNets {
		// the subnet may changed when alloc static ip from the latter subnet after ns supports multi subnets
		v4IP, v6IP, mac, subnet, err := c.acquireAddress(pod, podNet)
		if err != nil {
			c.recorder.Eventf(pod, v1.EventTypeWarning, "AcquireAddressFailed", err.Error())
			klog.Error(err)
			return nil, err
		}
		ipStr := util.GetStringIP(v4IP, v6IP)
		patch[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)] = ipStr
		if mac == "" {
			patch[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)] = nil
		} else {
			patch[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)] = mac
		}
		patch[fmt.Sprintf(util.CidrAnnotationTemplate, podNet.ProviderName)] = subnet.Spec.CIDRBlock
		patch[fmt.Sprintf(util.GatewayAnnotationTemplate, podNet.ProviderName)] = subnet.Spec.Gateway
		if isOvnSubnet(podNet.Subnet) {
			patch[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, podNet.ProviderName)] = subnet.Name
			if pod.Annotations[fmt.Sprintf(util.PodNicAnnotationTemplate, podNet.ProviderName)] == "" {
				patch[fmt.Sprintf(util.PodNicAnnotationTemplate, podNet.ProviderName)] = c.config.PodNicType
			}
		} else {
			patch[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, podNet.ProviderName)] = nil
			patch[fmt.Sprintf(util.PodNicAnnotationTemplate, podNet.ProviderName)] = nil
		}
		patch[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] = "true"
		if vmKey != "" {
			patch[fmt.Sprintf(util.VMAnnotationTemplate, podNet.ProviderName)] = vmName
		}
		if err := util.ValidateNetworkBroadcast(podNet.Subnet.Spec.CIDRBlock, ipStr); err != nil {
			klog.Errorf("validate pod %s/%s failed: %v", namespace, name, err)
			c.recorder.Eventf(pod, v1.EventTypeWarning, "ValidatePodNetworkFailed", err.Error())
			return nil, err
		}

		if podNet.Type != providerTypeIPAM {
			if (subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway || subnet.Spec.U2OInterconnection) && subnet.Spec.Vpc != "" {
				patch[fmt.Sprintf(util.LogicalRouterAnnotationTemplate, podNet.ProviderName)] = subnet.Spec.Vpc
			}

			if subnet.Spec.Vlan != "" {
				vlan, err := c.vlansLister.Get(subnet.Spec.Vlan)
				if err != nil {
					klog.Error(err)
					c.recorder.Eventf(pod, v1.EventTypeWarning, "GetVlanInfoFailed", err.Error())
					return nil, err
				}
				patch[fmt.Sprintf(util.VlanIDAnnotationTemplate, podNet.ProviderName)] = strconv.Itoa(vlan.Spec.ID)
				patch[fmt.Sprintf(util.ProviderNetworkTemplate, podNet.ProviderName)] = vlan.Spec.Provider
			}

			portSecurity := false
			if pod.Annotations[fmt.Sprintf(util.PortSecurityAnnotationTemplate, podNet.ProviderName)] == "true" {
				portSecurity = true
			}

			vips := vipsMap[fmt.Sprintf("%s.%s", podNet.Subnet.Name, podNet.ProviderName)]
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

			var oldSgList []string
			if vmKey != "" {
				existingLsp, err := c.OVNNbClient.GetLogicalSwitchPort(portName, true)
				if err != nil {
					klog.Errorf("failed to get logical switch port %s: %v", portName, err)
					return nil, err
				}
				if existingLsp != nil {
					oldSgList, _ = c.getPortSg(existingLsp)
				}
			}

			securityGroupAnnotation := pod.Annotations[fmt.Sprintf(util.SecurityGroupAnnotationTemplate, podNet.ProviderName)]
			if err := c.OVNNbClient.CreateLogicalSwitchPort(subnet.Name, portName, ipStr, mac, podName, pod.Namespace,
				portSecurity, securityGroupAnnotation, vips, podNet.Subnet.Spec.EnableDHCP, dhcpOptions, subnet.Spec.Vpc); err != nil {
				c.recorder.Eventf(pod, v1.EventTypeWarning, "CreateOVNPortFailed", err.Error())
				klog.Errorf("%v", err)
				return nil, err
			}

			if pod.Annotations[fmt.Sprintf(util.Layer2ForwardAnnotationTemplate, podNet.ProviderName)] == "true" {
				if err := c.OVNNbClient.EnablePortLayer2forward(portName); err != nil {
					c.recorder.Eventf(pod, v1.EventTypeWarning, "SetOVNPortL2ForwardFailed", err.Error())
					klog.Errorf("%v", err)
					return nil, err
				}
			}

			if securityGroupAnnotation != "" || oldSgList != nil {
				securityGroups := strings.ReplaceAll(securityGroupAnnotation, " ", "")
				newSgList := strings.Split(securityGroups, ",")
				sgNames := util.UnionStringSlice(oldSgList, newSgList)
				for _, sgName := range sgNames {
					if sgName != "" {
						c.syncSgPortsQueue.Add(sgName)
					}
				}
			}

			if vips != "" {
				c.syncVirtualPortsQueue.Add(podNet.Subnet.Name)
			}
		}
		// CreatePort may fail, so put ip CR creation after CreatePort
		ipCRName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
		if err := c.createOrUpdateIPCR(ipCRName, podName, ipStr, mac, subnet.Name, pod.Namespace, pod.Spec.NodeName, podType); err != nil {
			err = fmt.Errorf("failed to create ips CR %s.%s: %w", podName, pod.Namespace, err)
			klog.Error(err)
			return nil, err
		}
	}
	if err = util.PatchAnnotations(c.config.KubeClient.CoreV1().Pods(namespace), name, patch); err != nil {
		if k8serrors.IsNotFound(err) {
			// Sometimes pod is deleted between kube-ovn configure ovn-nb and patch pod.
			// Then we need to recycle the resource again.
			key := strings.Join([]string{namespace, name}, "/")
			c.deletingPodObjMap.Store(key, pod)
			c.deletePodQueue.AddRateLimited(key)
			return nil, nil
		}
		klog.Errorf("failed to patch pod %s/%s: %v", namespace, name, err)
		return nil, err
	}

	if pod, err = c.config.KubeClient.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{}); err != nil {
		if k8serrors.IsNotFound(err) {
			key := strings.Join([]string{namespace, name}, "/")
			c.deletingPodObjMap.Store(key, pod)
			c.deletePodQueue.AddRateLimited(key)
			return nil, nil
		}
		klog.Errorf("failed to get pod %s/%s: %v", namespace, name, err)
		return nil, err
	}

	if vpcGwName, isVpcNatGw := pod.Annotations[util.VpcNatGatewayAnnotation]; isVpcNatGw {
		c.initVpcNatGatewayQueue.Add(vpcGwName)
	}
	return pod, nil
}

// do the same thing as update pod
func (c *Controller) reconcileRouteSubnets(pod *v1.Pod, needRoutePodNets []*kubeovnNet) error {
	// the lb-svc pod has dependencies on Running state, check it when pod state get updated
	if err := c.checkAndReInitLbSvcPod(pod); err != nil {
		klog.Errorf("failed to init iptable rules for load-balancer pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}

	if len(needRoutePodNets) == 0 {
		return nil
	}

	namespace := pod.Namespace
	name := pod.Name
	podName := c.getNameByPod(pod)

	klog.Infof("sync pod %s/%s routed", namespace, name)

	var podIP string
	var subnet *kubeovnv1.Subnet
	patch := util.KVPatch{}
	for _, podNet := range needRoutePodNets {
		// in case update handler overlap the annotation when cache is not in sync
		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "" {
			return fmt.Errorf("no address has been allocated to %s/%s", namespace, name)
		}

		podIP = pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)]
		subnet = podNet.Subnet

		// Check if pod uses nodeSwitch subnet
		if subnet.Name == c.config.NodeSwitch {
			return fmt.Errorf("NodeSwitch subnet %s is unavailable for pod", subnet.Name)
		}

		portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
		if (!c.config.EnableLb || !(subnet.Spec.EnableLb != nil && *subnet.Spec.EnableLb)) &&
			subnet.Spec.Vpc == c.config.ClusterRouter &&
			subnet.Spec.U2OInterconnection &&
			subnet.Spec.Vlan != "" &&
			!subnet.Spec.LogicalGateway {
			pgName := getOverlaySubnetsPortGroupName(subnet.Name, pod.Spec.NodeName)
			if err := c.OVNNbClient.PortGroupAddPorts(pgName, portName); err != nil {
				klog.Errorf("failed to add port to u2o port group %s: %v", pgName, err)
				return err
			}
		}

		if podIP != "" && (subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) && subnet.Spec.Vpc == c.config.ClusterRouter {
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
					externalSubnet, err := c.subnetsLister.Get(c.config.ExternalGatewaySwitch)
					if err != nil {
						klog.Errorf("failed to get subnet %s, %v", c.config.ExternalGatewaySwitch, err)
						return err
					}
					nextHop = externalSubnet.Spec.Gateway
					if nextHop == "" {
						klog.Errorf("no available gateway address")
						return errors.New("no available gateway address")
					}
				}
				if strings.Contains(nextHop, "/") {
					nextHop = strings.Split(nextHop, "/")[0]
				}

				if err := c.addPolicyRouteToVpc(
					subnet.Spec.Vpc,
					&kubeovnv1.PolicyRoute{
						Priority:  util.NorthGatewayRoutePolicyPriority,
						Match:     fmt.Sprintf("ip4.src == %s", podIP),
						Action:    kubeovnv1.PolicyRouteActionReroute,
						NextHopIP: nextHop,
					},
					map[string]string{
						"vendor": util.CniTypeName,
						"subnet": subnet.Name,
					},
				); err != nil {
					klog.Errorf("failed to add policy route, %v", err)
					return err
				}

				// remove lsp from port group to make EIP/SNAT work
				if err = c.OVNNbClient.PortGroupRemovePorts(pgName, portName); err != nil {
					klog.Error(err)
					return err
				}
			} else {
				if subnet.Spec.GatewayType == kubeovnv1.GWDistributedType && pod.Annotations[util.NorthGatewayAnnotation] == "" {
					nodeTunlIPAddr, err := getNodeTunlIP(node)
					if err != nil {
						klog.Error(err)
						return err
					}

					var added bool

					for _, nodeAddr := range nodeTunlIPAddr {
						for _, podAddr := range strings.Split(podIP, ",") {
							if util.CheckProtocol(nodeAddr.String()) != util.CheckProtocol(podAddr) {
								continue
							}

							if err := c.OVNNbClient.PortGroupAddPorts(pgName, portName); err != nil {
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

				if pod.Annotations[util.NorthGatewayAnnotation] != "" && pod.Annotations[util.IPAddressAnnotation] != "" {
					for _, podAddr := range strings.Split(pod.Annotations[util.IPAddressAnnotation], ",") {
						if util.CheckProtocol(podAddr) != util.CheckProtocol(pod.Annotations[util.NorthGatewayAnnotation]) {
							continue
						}
						ipSuffix := "ip4"
						if util.CheckProtocol(podAddr) == kubeovnv1.ProtocolIPv6 {
							ipSuffix = "ip6"
						}

						if err := c.addPolicyRouteToVpc(
							subnet.Spec.Vpc,
							&kubeovnv1.PolicyRoute{
								Priority:  util.NorthGatewayRoutePolicyPriority,
								Match:     fmt.Sprintf("%s.src == %s", ipSuffix, podAddr),
								Action:    kubeovnv1.PolicyRouteActionReroute,
								NextHopIP: pod.Annotations[util.NorthGatewayAnnotation],
							},
							map[string]string{
								"vendor": util.CniTypeName,
								"subnet": subnet.Name,
							},
						); err != nil {
							klog.Errorf("failed to add policy route, %v", err)
							return err
						}
					}
				} else if c.config.EnableEipSnat {
					if err = c.deleteStaticRouteFromVpc(
						c.config.ClusterRouter,
						subnet.Spec.RouteTable,
						podIP,
						"",
						kubeovnv1.PolicyDst,
					); err != nil {
						klog.Error(err)
						return err
					}
				}
			}

			if c.config.EnableEipSnat {
				for _, ipStr := range strings.Split(podIP, ",") {
					if eip := pod.Annotations[util.EipAnnotation]; eip == "" {
						if err = c.OVNNbClient.DeleteNats(c.config.ClusterRouter, ovnnb.NATTypeDNATAndSNAT, ipStr); err != nil {
							klog.Errorf("failed to delete nat rules: %v", err)
						}
					} else if util.CheckProtocol(eip) == util.CheckProtocol(ipStr) {
						if err = c.OVNNbClient.UpdateDnatAndSnat(c.config.ClusterRouter, eip, ipStr, fmt.Sprintf("%s.%s", podName, pod.Namespace), pod.Annotations[util.MacAddressAnnotation], c.ExternalGatewayType); err != nil {
							klog.Errorf("failed to add nat rules, %v", err)
							return err
						}
					}
					if eip := pod.Annotations[util.SnatAnnotation]; eip == "" {
						if err = c.OVNNbClient.DeleteNats(c.config.ClusterRouter, ovnnb.NATTypeSNAT, ipStr); err != nil {
							klog.Errorf("failed to delete nat rules: %v", err)
						}
					} else if util.CheckProtocol(eip) == util.CheckProtocol(ipStr) {
						if err = c.OVNNbClient.UpdateSnat(c.config.ClusterRouter, eip, ipStr); err != nil {
							klog.Errorf("failed to add nat rules, %v", err)
							return err
						}
					}
				}
			}
		}

		if pod.Annotations[fmt.Sprintf(util.ActivationStrategyTemplate, podNet.ProviderName)] != "" {
			if err := c.OVNNbClient.SetLogicalSwitchPortActivationStrategy(portName, pod.Spec.NodeName); err != nil {
				klog.Errorf("failed to set activation strategy for lsp %s: %v", portName, err)
				return err
			}
		}

		patch[fmt.Sprintf(util.RoutedAnnotationTemplate, podNet.ProviderName)] = "true"
	}
	if err := util.PatchAnnotations(c.config.KubeClient.CoreV1().Pods(namespace), name, patch); err != nil {
		if k8serrors.IsNotFound(err) {
			// Sometimes pod is deleted between kube-ovn configure ovn-nb and patch pod.
			// Then we need to recycle the resource again.
			key := strings.Join([]string{namespace, name}, "/")
			c.deletingPodObjMap.Store(key, pod)
			c.deletePodQueue.AddRateLimited(key)
			return nil
		}
		klog.Errorf("failed to patch pod %s/%s: %v", namespace, name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDeletePod(key string) (err error) {
	pod, ok := c.deletingPodObjMap.Load(key)
	if !ok {
		return nil
	}
	podName := c.getNameByPod(pod)
	c.podKeyMutex.LockKey(key)
	defer func() {
		_ = c.podKeyMutex.UnlockKey(key)
		if err == nil {
			c.deletingPodObjMap.Delete(key)
		}
	}()
	klog.Infof("handle delete pod %s", key)

	p, _ := c.podsLister.Pods(pod.Namespace).Get(pod.Name)
	if p != nil && p.UID != pod.UID {
		// Pod with same name exists, just return here
		return nil
	}

	if aaps := pod.Annotations[util.AAPsAnnotation]; aaps != "" {
		for _, vipName := range strings.Split(aaps, ",") {
			if vip, err := c.virtualIpsLister.Get(vipName); err == nil {
				if vip.Spec.Namespace != pod.Namespace {
					continue
				}
				klog.Infof("enqueue update virtual parents for %s", vipName)
				c.updateVirtualParentsQueue.Add(vipName)
			}
		}
	}

	podKey := fmt.Sprintf("%s/%s", pod.Namespace, podName)

	var keepIPCR bool
	if ok, stsName, stsUID := isStatefulSetPod(pod); ok {
		if pod.DeletionTimestamp != nil {
			klog.Infof("handle deletion of sts pod %s", podName)
			toDel := isStatefulSetPodToDel(c.config.KubeClient, pod, stsName, stsUID)
			if !toDel {
				klog.Infof("try keep ip for sts pod %s", podKey)
				keepIPCR = true
			}
		}
		if keepIPCR {
			isDelete, err := appendCheckPodToDel(c, pod, stsName, util.StatefulSet)
			if err != nil {
				klog.Error(err)
				return err
			}
			if isDelete {
				klog.Infof("not keep ip for sts pod %s", podKey)
				keepIPCR = false
			}
		}
	}
	isVMPod, vmName := isVMPod(pod)
	if isVMPod && c.config.EnableKeepVMIP {
		ports, err := c.OVNNbClient.ListNormalLogicalSwitchPorts(true, map[string]string{"pod": podKey})
		if err != nil {
			klog.Errorf("failed to list lsps of pod '%s', %v", pod.Name, err)
			return err
		}
		for _, port := range ports {
			if err := c.OVNNbClient.CleanLogicalSwitchPortMigrateOptions(port.Name); err != nil {
				err = fmt.Errorf("failed to clean migrate options for vm lsp %s, %w", port.Name, err)
				klog.Error(err)
				return err
			}
		}
		if pod.DeletionTimestamp != nil {
			klog.Infof("handle deletion of vm pod %s", podName)
			vmToBeDel := c.isVMToDel(pod, vmName)
			if !vmToBeDel {
				klog.Infof("try keep ip for vm pod %s", podKey)
				keepIPCR = true
			}
		}
		if keepIPCR {
			isDelete, err := appendCheckPodToDel(c, pod, vmName, util.VMInstance)
			if err != nil {
				klog.Error(err)
				return err
			}
			if isDelete {
				klog.Infof("not keep ip for vm pod %s", podKey)
				keepIPCR = false
			}
		}
	}

	podNets, err := c.getPodKubeovnNets(pod)
	if err != nil {
		klog.Errorf("failed to get pod nets %v", err)
	}
	if !keepIPCR {
		ports, err := c.OVNNbClient.ListNormalLogicalSwitchPorts(true, map[string]string{"pod": podKey})
		if err != nil {
			klog.Errorf("failed to list lsps of pod '%s', %v", pod.Name, err)
			return err
		}

		if len(ports) != 0 {
			addresses := c.ipam.GetPodAddress(podKey)
			for _, address := range addresses {
				if strings.TrimSpace(address.IP) == "" {
					continue
				}
				subnet, err := c.subnetsLister.Get(address.Subnet.Name)
				if k8serrors.IsNotFound(err) {
					continue
				} else if err != nil {
					klog.Error(err)
					return err
				}
				vpc, err := c.vpcsLister.Get(subnet.Spec.Vpc)
				if k8serrors.IsNotFound(err) {
					continue
				} else if err != nil {
					klog.Error(err)
					return err
				}

				ipSuffix := "ip4"
				if util.CheckProtocol(address.IP) == kubeovnv1.ProtocolIPv6 {
					ipSuffix = "ip6"
				}
				if err = c.deletePolicyRouteFromVpc(
					vpc.Name,
					util.NorthGatewayRoutePolicyPriority,
					fmt.Sprintf("%s.src == %s", ipSuffix, address.IP),
				); err != nil {
					klog.Errorf("failed to delete static route, %v", err)
					return err
				}

				if c.config.EnableEipSnat {
					if pod.Annotations[util.EipAnnotation] != "" {
						if err = c.OVNNbClient.DeleteNat(c.config.ClusterRouter, ovnnb.NATTypeDNATAndSNAT, pod.Annotations[util.EipAnnotation], address.IP); err != nil {
							klog.Errorf("failed to delete nat rules: %v", err)
						}
					}
					if pod.Annotations[util.SnatAnnotation] != "" {
						if err = c.OVNNbClient.DeleteNat(c.config.ClusterRouter, ovnnb.NATTypeSNAT, "", address.IP); err != nil {
							klog.Errorf("failed to delete nat rules: %v", err)
						}
					}
				}
			}
		}
		for _, port := range ports {
			// when lsp is deleted, the port of pod is deleted from any port-group automatically.
			klog.Infof("delete logical switch port %s", port.Name)
			if err := c.OVNNbClient.DeleteLogicalSwitchPort(port.Name); err != nil {
				klog.Errorf("failed to delete lsp %s, %v", port.Name, err)
				return err
			}
		}
		klog.Infof("try release all ip address for deleting pod %s", podKey)
		for _, podNet := range podNets {
			portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
			ipCR, err := c.ipsLister.Get(portName)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					continue
				}
				klog.Errorf("failed to get ip %s, %v", portName, err)
				return err
			}
			if ipCR.Labels[util.IPReservedLabel] != "true" {
				klog.Infof("delete ip CR %s", ipCR.Name)
				if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), ipCR.Name, metav1.DeleteOptions{}); err != nil {
					if !k8serrors.IsNotFound(err) {
						klog.Errorf("failed to delete ip %s, %v", ipCR.Name, err)
						return err
					}
				}
				// release ipam address after delete ip CR
				c.ipam.ReleaseAddressByPod(podKey, podNet.Subnet.Name)
			}
		}
		if pod.Annotations[util.VipAnnotation] != "" {
			if err = c.releaseVip(pod.Annotations[util.VipAnnotation]); err != nil {
				klog.Errorf("failed to clean label from vip %s, %v", pod.Annotations[util.VipAnnotation], err)
				return err
			}
		}
	}
	for _, podNet := range podNets {
		c.syncVirtualPortsQueue.Add(podNet.Subnet.Name)
		securityGroupAnnotation := pod.Annotations[fmt.Sprintf(util.SecurityGroupAnnotationTemplate, podNet.ProviderName)]
		if securityGroupAnnotation != "" {
			securityGroups := strings.ReplaceAll(securityGroupAnnotation, " ", "")
			for _, sgName := range strings.Split(securityGroups, ",") {
				if sgName != "" {
					c.syncSgPortsQueue.Add(sgName)
				}
			}
		}
	}
	return nil
}

func (c *Controller) handleUpdatePodSecurity(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.podKeyMutex.LockKey(key)
	defer func() { _ = c.podKeyMutex.UnlockKey(key) }()

	pod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	podName := c.getNameByPod(pod)

	klog.Infof("update pod %s/%s security", namespace, name)

	podNets, err := c.getPodKubeovnNets(pod)
	if err != nil {
		klog.Errorf("failed to pod nets %v", err)
		return err
	}

	vipsMap := c.getVirtualIPs(pod, podNets)

	// associated with security group
	for _, podNet := range podNets {
		portSecurity := false
		if pod.Annotations[fmt.Sprintf(util.PortSecurityAnnotationTemplate, podNet.ProviderName)] == "true" {
			portSecurity = true
		}

		mac := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)]
		ipStr := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)]
		vips := vipsMap[fmt.Sprintf("%s.%s", podNet.Subnet.Name, podNet.ProviderName)]

		if err = c.OVNNbClient.SetLogicalSwitchPortSecurity(portSecurity, ovs.PodNameToPortName(podName, namespace, podNet.ProviderName), mac, ipStr, vips); err != nil {
			klog.Errorf("set logical switch port security: %v", err)
			return err
		}

		c.syncVirtualPortsQueue.Add(podNet.Subnet.Name)
		securityGroupAnnotation := pod.Annotations[fmt.Sprintf(util.SecurityGroupAnnotationTemplate, podNet.ProviderName)]
		var securityGroups string
		if securityGroupAnnotation != "" {
			securityGroups = strings.ReplaceAll(securityGroupAnnotation, " ", "")
			for _, sgName := range strings.Split(securityGroups, ",") {
				if sgName != "" {
					c.syncSgPortsQueue.Add(sgName)
				}
			}
		}
		if err = c.reconcilePortSg(ovs.PodNameToPortName(podName, namespace, podNet.ProviderName), securityGroups); err != nil {
			klog.Errorf("reconcilePortSg failed. %v", err)
			return err
		}
	}
	return nil
}

func (c *Controller) syncKubeOvnNet(pod *v1.Pod, podNets []*kubeovnNet) (*v1.Pod, error) {
	podName := c.getNameByPod(pod)
	key := fmt.Sprintf("%s/%s", pod.Namespace, podName)
	targetPortNameList := strset.NewWithSize(len(podNets))
	portsNeedToDel := []string{}
	annotationsNeedToDel := []string{}
	annotationsNeedToAdd := make(map[string]string)
	subnetUsedByPort := make(map[string]string)

	for _, podNet := range podNets {
		portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
		targetPortNameList.Add(portName)
		if podNet.IPRequest != "" {
			klog.Infof("pod %s/%s use custom IP %s for provider %s", pod.Namespace, pod.Name, podNet.IPRequest, podNet.ProviderName)
			annotationsNeedToAdd[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)] = podNet.IPRequest
		}

		if podNet.MacRequest != "" {
			klog.Infof("pod %s/%s use custom MAC %s for provider %s", pod.Namespace, pod.Name, podNet.MacRequest, podNet.ProviderName)
			annotationsNeedToAdd[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)] = podNet.MacRequest
		}
	}

	ports, err := c.OVNNbClient.ListNormalLogicalSwitchPorts(true, map[string]string{"pod": key})
	if err != nil {
		klog.Errorf("failed to list lsps of pod '%s', %v", pod.Name, err)
		return nil, err
	}

	for _, port := range ports {
		if !targetPortNameList.Has(port.Name) {
			portsNeedToDel = append(portsNeedToDel, port.Name)
			subnetUsedByPort[port.Name] = port.ExternalIDs["ls"]
			portNameSlice := strings.Split(port.Name, ".")
			providerName := strings.Join(portNameSlice[2:], ".")
			if providerName == util.OvnProvider {
				continue
			}
			annotationsNeedToDel = append(annotationsNeedToDel, providerName)
		}
	}

	if len(portsNeedToDel) == 0 && len(annotationsNeedToAdd) == 0 {
		return pod, nil
	}

	for _, portNeedDel := range portsNeedToDel {
		klog.Infof("release port %s for pod %s", portNeedDel, podName)
		if subnet, ok := c.ipam.Subnets[subnetUsedByPort[portNeedDel]]; ok {
			subnet.ReleaseAddressWithNicName(podName, portNeedDel)
		}
		if err := c.OVNNbClient.DeleteLogicalSwitchPort(portNeedDel); err != nil {
			klog.Errorf("failed to delete lsp %s, %v", portNeedDel, err)
			return nil, err
		}
		if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), portNeedDel, metav1.DeleteOptions{}); err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to delete ip %s, %v", portNeedDel, err)
				return nil, err
			}
		}
	}

	patch := util.KVPatch{}
	for _, providerName := range annotationsNeedToDel {
		for key := range pod.Annotations {
			if strings.HasPrefix(key, providerName) {
				patch[key] = nil
			}
		}
	}

	for key, value := range annotationsNeedToAdd {
		patch[key] = value
	}

	if len(patch) == 0 {
		return pod, nil
	}

	if err = util.PatchAnnotations(c.config.KubeClient.CoreV1().Pods(pod.Namespace), pod.Name, patch); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		klog.Errorf("failed to clean annotations for pod %s/%s: %v", pod.Namespace, pod.Name, err)
		return nil, err
	}

	if pod, err = c.config.KubeClient.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		klog.Errorf("failed to get pod %s/%s: %v", pod.Namespace, pod.Name, err)
		return nil, err
	}

	return pod, nil
}

func isStatefulSetPod(pod *v1.Pod) (bool, string, types.UID) {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == util.StatefulSet && strings.HasPrefix(owner.APIVersion, "apps/") {
			if strings.HasPrefix(pod.Name, owner.Name) {
				return true, owner.Name, owner.UID
			}
		}
	}
	return false, "", ""
}

func isStatefulSetPodToDel(c kubernetes.Interface, pod *v1.Pod, statefulSetName string, statefulSetUID types.UID) bool {
	// only delete statefulset pod lsp when statefulset deleted or down scaled
	sts, err := c.AppsV1().StatefulSets(pod.Namespace).Get(context.Background(), statefulSetName, metav1.GetOptions{})
	if err != nil {
		// statefulset is deleted
		if k8serrors.IsNotFound(err) {
			klog.Infof("statefulset %s is deleted", statefulSetName)
			return true
		}
		klog.Errorf("failed to get statefulset %v", err)
		return false
	}

	// statefulset is being deleted, or it's a newly created one
	if !sts.DeletionTimestamp.IsZero() || sts.UID != statefulSetUID {
		klog.Infof("statefulset %s is being deleted", statefulSetName)
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
	// down scaled
	var startOrdinal int64
	if sts.Spec.Ordinals != nil {
		startOrdinal = int64(sts.Spec.Ordinals.Start)
	}
	if index >= startOrdinal+int64(*sts.Spec.Replicas) {
		klog.Infof("statefulset %s is down scaled", statefulSetName)
		return true
	}
	return false
}

func getNodeTunlIP(node *v1.Node) ([]net.IP, error) {
	var nodeTunlIPAddr []net.IP
	nodeTunlIP := node.Annotations[util.IPAddressAnnotation]
	if nodeTunlIP == "" {
		return nil, errors.New("node has no tunnel ip annotation")
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
	// check if allocate from subnet is need.
	// allocate subnet when change subnet to hotplug nic
	// allocate subnet when migrate vm
	if !isPodAlive(pod) {
		return nil
	}

	if pod.Annotations == nil {
		return nets
	}

	migrate := false
	if job, ok := pod.Annotations[util.MigrationJobAnnotation]; ok {
		klog.Infof("pod %s/%s is in the migration job %s", pod.Namespace, pod.Name, job)
		migrate = true
	}

	result := make([]*kubeovnNet, 0, len(nets))
	for _, n := range nets {
		if migrate || pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, n.ProviderName)] != "true" {
			result = append(result, n)
		}
	}
	return result
}

func (c *Controller) podNeedSync(pod *v1.Pod) (bool, error) {
	// 1. check annotations
	if pod.Annotations == nil {
		return true, nil
	}
	// 2. check annotation ovn subnet
	if pod.Annotations[util.RoutedAnnotation] != "true" {
		return true, nil
	}
	// 3. check multus subnet
	attachmentNets, err := c.getPodAttachmentNet(pod)
	if err != nil {
		klog.Error(err)
		return false, err
	}
	for _, n := range attachmentNets {
		if pod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, n.ProviderName)] != "true" {
			return true, nil
		}
		ipName := ovs.PodNameToPortName(pod.Name, pod.Namespace, n.ProviderName)
		if _, err = c.ipsLister.Get(ipName); err != nil {
			err = fmt.Errorf("pod has no ip %s: %w", ipName, err)
			// need to sync to create ip
			klog.Error(err)
			return true, nil
		}
	}
	return false, nil
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
	// check pod annotations
	if lsName := pod.Annotations[util.LogicalSwitchAnnotation]; lsName != "" {
		subnet, err := c.subnetsLister.Get(lsName)
		if err != nil {
			klog.Errorf("failed to get subnet %s: %v", lsName, err)
			return nil, err
		}
		return subnet, nil
	}

	ns, err := c.namespacesLister.Get(pod.Namespace)
	if err != nil {
		klog.Errorf("failed to get namespace %s: %v", pod.Namespace, err)
		return nil, err
	}
	if len(ns.Annotations) == 0 {
		err = fmt.Errorf("namespace %s network annotations is empty", ns.Name)
		klog.Error(err)
		return nil, err
	}

	subnetNames := ns.Annotations[util.LogicalSwitchAnnotation]
	for _, subnetName := range strings.Split(subnetNames, ",") {
		if subnetName == "" {
			err = fmt.Errorf("namespace %s default logical switch is not found", ns.Name)
			klog.Error(err)
			return nil, err
		}
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			klog.Errorf("failed to get subnet %s: %v", subnetName, err)
			return nil, err
		}

		switch subnet.Spec.Protocol {
		case kubeovnv1.ProtocolDual:
			if subnet.Status.V6AvailableIPs == 0 {
				klog.Infof("there's no available ipv6 address in subnet %s, try next one", subnet.Name)
				continue
			}
			fallthrough
		case kubeovnv1.ProtocolIPv4:
			if subnet.Status.V4AvailableIPs == 0 {
				klog.Infof("there's no available ipv4 address in subnet %s, try next one", subnet.Name)
				continue
			}
		case kubeovnv1.ProtocolIPv6:
			if subnet.Status.V6AvailableIPs == 0 {
				klog.Infof("there's no available ipv6 address in subnet %s, try next one", subnet.Name)
				continue
			}
		}
		return subnet, nil
	}
	return nil, ipam.ErrNoAvailable
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
	IPRequest          string
	MacRequest         string
}

func (c *Controller) getPodAttachmentNet(pod *v1.Pod) ([]*kubeovnNet, error) {
	var multusNets []*nadv1.NetworkSelectionElement
	defaultAttachNetworks := pod.Annotations[util.DefaultNetworkAnnotation]
	if defaultAttachNetworks != "" {
		attachments, err := nadutils.ParseNetworkAnnotation(defaultAttachNetworks, pod.Namespace)
		if err != nil {
			klog.Errorf("failed to parse default attach net for pod '%s', %v", pod.Name, err)
			return nil, err
		}
		multusNets = attachments
	}

	attachNetworks := pod.Annotations[nadv1.NetworkAttachmentAnnot]
	if attachNetworks != "" {
		attachments, err := nadutils.ParseNetworkAnnotation(attachNetworks, pod.Namespace)
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

			providerName = fmt.Sprintf("%s.%s.%s", attach.Name, attach.Namespace, util.OvnProvider)
			if pod.Annotations[util.MigrationJobAnnotation] != "" {
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

			ret := &kubeovnNet{
				Type:               providerTypeOriginal,
				ProviderName:       providerName,
				Subnet:             subnet,
				IsDefault:          isDefault,
				AllowLiveMigration: allowLiveMigration,
				MacRequest:         attach.MacRequest,
				IPRequest:          strings.Join(attach.IPRequest, ","),
			}
			result = append(result, ret)
		} else {
			providerName = fmt.Sprintf("%s.%s", attach.Name, attach.Namespace)
			for _, subnet := range subnets {
				if subnet.Spec.Provider == providerName {
					result = append(result, &kubeovnNet{
						Type:         providerTypeIPAM,
						ProviderName: providerName,
						Subnet:       subnet,
						MacRequest:   attach.MacRequest,
						IPRequest:    strings.Join(attach.IPRequest, ","),
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

	if subnet.Spec.Vlan == "" && subnet.Spec.Vpc == c.config.ClusterRouter {
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

	var checkVMPod bool
	isStsPod, _, _ := isStatefulSetPod(pod)
	// if pod has static vip
	vipName := pod.Annotations[util.VipAnnotation]
	if vipName != "" {
		vip, err := c.virtualIpsLister.Get(vipName)
		if err != nil {
			klog.Errorf("failed to get static vip '%s', %v", vipName, err)
			return "", "", "", podNet.Subnet, err
		}
		portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
		if c.config.EnableKeepVMIP {
			checkVMPod, _ = isVMPod(pod)
		}
		if err = c.podReuseVip(vipName, portName, isStsPod || checkVMPod); err != nil {
			return "", "", "", podNet.Subnet, err
		}
		return vip.Status.V4ip, vip.Status.V6ip, vip.Status.Mac, podNet.Subnet, nil
	}

	var macPointer *string
	if isOvnSubnet(podNet.Subnet) {
		annoMAC := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)]
		if annoMAC != "" {
			if _, err := net.ParseMAC(annoMAC); err != nil {
				return "", "", "", podNet.Subnet, err
			}
			macPointer = &annoMAC
		}
	} else {
		macPointer = ptr.To("")
	}

	ippoolStr := pod.Annotations[fmt.Sprintf(util.IPPoolAnnotationTemplate, podNet.ProviderName)]
	if ippoolStr == "" {
		ns, err := c.namespacesLister.Get(pod.Namespace)
		if err != nil {
			klog.Errorf("failed to get namespace %s: %v", pod.Namespace, err)
			return "", "", "", podNet.Subnet, err
		}

		if len(ns.Annotations) != 0 {
			if ipPoolList, ok := ns.Annotations[util.IPPoolAnnotation]; ok {
				for _, ipPoolName := range strings.Split(ipPoolList, ",") {
					ippool, err := c.ippoolLister.Get(ipPoolName)
					if err != nil {
						klog.Errorf("failed to get ippool %s: %v", ipPoolName, err)
						return "", "", "", podNet.Subnet, err
					}

					switch podNet.Subnet.Spec.Protocol {
					case kubeovnv1.ProtocolDual:
						if ippool.Status.V4AvailableIPs.Int64() == 0 || ippool.Status.V6AvailableIPs.Int64() == 0 {
							continue
						}
					case kubeovnv1.ProtocolIPv4:
						if ippool.Status.V4AvailableIPs.Int64() == 0 {
							continue
						}

					default:
						if ippool.Status.V6AvailableIPs.Int64() == 0 {
							continue
						}
					}

					if ippool.Spec.Subnet == podNet.Subnet.Name {
						ippoolStr = ippool.Name
						break
					}
				}
			}
		}
	}

	// Random allocate
	if pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)] == "" &&
		ippoolStr == "" {
		var skippedAddrs []string
		for {
			portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)

			ipv4, ipv6, mac, err := c.ipam.GetRandomAddress(key, portName, macPointer, podNet.Subnet.Name, "", skippedAddrs, !podNet.AllowLiveMigration)
			if err != nil {
				klog.Error(err)
				return "", "", "", podNet.Subnet, err
			}
			ipv4OK, ipv6OK, err := c.validatePodIP(pod.Name, podNet.Subnet.Name, ipv4, ipv6)
			if err != nil {
				klog.Error(err)
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
	if pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)] != "" {
		ipStr := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)]

		for _, net := range nsNets {
			v4IP, v6IP, mac, err = c.acquireStaticAddress(key, portName, ipStr, macPointer, net.Subnet.Name, net.AllowLiveMigration)
			if err == nil {
				return v4IP, v6IP, mac, net.Subnet, nil
			}
		}
		return v4IP, v6IP, mac, podNet.Subnet, err
	}

	// IPPool allocate
	if ippoolStr != "" {
		var ipPool []string
		if strings.ContainsRune(ippoolStr, ';') {
			ipPool = strings.Split(ippoolStr, ";")
		} else {
			ipPool = strings.Split(ippoolStr, ",")
			if len(ipPool) == 2 && util.CheckProtocol(ipPool[0]) != util.CheckProtocol(ipPool[1]) {
				ipPool = []string{ippoolStr}
			}
		}
		for i, ip := range ipPool {
			ipPool[i] = strings.TrimSpace(ip)
		}

		if len(ipPool) == 1 && (!strings.ContainsRune(ipPool[0], ',') && net.ParseIP(ipPool[0]) == nil) {
			var skippedAddrs []string
			for {
				portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
				ipv4, ipv6, mac, err := c.ipam.GetRandomAddress(key, portName, macPointer, podNet.Subnet.Name, ipPool[0], skippedAddrs, !podNet.AllowLiveMigration)
				if err != nil {
					klog.Error(err)
					return "", "", "", podNet.Subnet, err
				}
				ipv4OK, ipv6OK, err := c.validatePodIP(pod.Name, podNet.Subnet.Name, ipv4, ipv6)
				if err != nil {
					klog.Error(err)
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

					v4IP, v6IP, mac, err = c.acquireStaticAddress(key, portName, staticIP, macPointer, net.Subnet.Name, net.AllowLiveMigration)
					if err == nil {
						return v4IP, v6IP, mac, net.Subnet, nil
					}
				}
			}
			klog.Errorf("acquire address from ippool %s for %s failed, %v", ippoolStr, key, err)
		} else {
			tempStrs := strings.Split(pod.Name, "-")
			numStr := tempStrs[len(tempStrs)-1]
			index, _ := strconv.Atoi(numStr)

			if index < len(ipPool) {
				for _, net := range nsNets {
					v4IP, v6IP, mac, err = c.acquireStaticAddress(key, portName, ipPool[index], macPointer, net.Subnet.Name, net.AllowLiveMigration)
					if err == nil {
						return v4IP, v6IP, mac, net.Subnet, nil
					}
				}
				klog.Errorf("acquire address %s for %s failed, %v", ipPool[index], key, err)
			}
		}
	}
	klog.Errorf("allocate address for %s failed, return NoAvailableAddress", key)
	return "", "", "", podNet.Subnet, ipam.ErrNoAvailable
}

func (c *Controller) acquireStaticAddress(key, nicName, ip string, mac *string, subnet string, liveMigration bool) (string, string, string, error) {
	var v4IP, v6IP, macStr string
	var err error
	for _, ipStr := range strings.Split(ip, ",") {
		if net.ParseIP(ipStr) == nil {
			return "", "", "", fmt.Errorf("failed to parse IP %s", ipStr)
		}
	}

	if v4IP, v6IP, macStr, err = c.ipam.GetStaticAddress(key, nicName, ip, mac, subnet, !liveMigration); err != nil {
		klog.Errorf("failed to get static ip %v, mac %v, subnet %v, err %v", ip, mac, subnet, err)
		return "", "", "", err
	}
	return v4IP, v6IP, macStr, nil
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
	case util.StatefulSet:
		ss, err := c.config.KubeClient.AppsV1().StatefulSets(pod.Namespace).Get(context.Background(), ownerRefName, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				klog.Infof("Statefulset %s is not found", ownerRefName)
				return true, nil
			}
			klog.Errorf("failed to get StatefulSet %s, %v", ownerRefName, err)
		}
		if ss.Spec.Template.ObjectMeta.Annotations[util.LogicalSwitchAnnotation] != "" {
			ownerRefSubnetExist = true
			ownerRefSubnet = ss.Spec.Template.ObjectMeta.Annotations[util.LogicalSwitchAnnotation]
		}

	case util.VMInstance:
		vm, err := c.config.KubevirtClient.VirtualMachine(pod.Namespace).Get(context.Background(), ownerRefName, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				klog.Infof("VirtualMachine %s is not found", ownerRefName)
				return true, nil
			}
			klog.Errorf("failed to get VirtualMachine %s, %v", ownerRefName, err)
		}
		if vm != nil &&
			vm.Spec.Template != nil &&
			vm.Spec.Template.ObjectMeta.Annotations != nil &&
			vm.Spec.Template.ObjectMeta.Annotations[util.LogicalSwitchAnnotation] != "" {
			ownerRefSubnetExist = true
			ownerRefSubnet = vm.Spec.Template.ObjectMeta.Annotations[util.LogicalSwitchAnnotation]
		}
	}
	podSwitch := strings.TrimSpace(pod.Annotations[util.LogicalSwitchAnnotation])
	if !ownerRefSubnetExist {
		nsSubnetNames := podNs.Annotations[util.LogicalSwitchAnnotation]
		// check if pod use the subnet of its ns
		if nsSubnetNames != "" && podSwitch != "" && !slices.Contains(strings.Split(nsSubnetNames, ","), podSwitch) {
			klog.Infof("ns %s annotation subnet is %s, which is inconstant with subnet for pod %s, delete pod", pod.Namespace, nsSubnetNames, pod.Name)
			return true, nil
		}
	}

	// subnet cidr has been changed, and statefulset pod's ip is not in the range of subnet's cidr anymore
	podSubnet, err := c.subnetsLister.Get(podSwitch)
	if err != nil {
		klog.Errorf("failed to get subnet %s, %v, not auto clean ip", podSwitch, err)
		return false, err
	}
	if podSubnet == nil {
		// TODO: remove: CRD get interface will retrun a nil subnet ?
		klog.Errorf("pod %s/%s subnet %s is nil, not auto clean ip", pod.Namespace, pod.Name, podSwitch)
		return false, nil
	}
	podIP := pod.Annotations[util.IPAddressAnnotation]
	if podIP == "" {
		// delete pod just after it created < 1ms
		klog.Infof("pod %s/%s annotaions has no ip address, not auto clean ip", pod.Namespace, pod.Name)
		return false, nil
	}
	podSubnetCidr := podSubnet.Spec.CIDRBlock
	if podSubnetCidr != "" {
		// subnet spec cidr changed by user
		klog.Errorf("invalid pod subnet %s empty cidr %s, not auto clean ip", podSwitch, podSubnetCidr)
		return false, nil
	}
	if !util.CIDRContainIP(podSubnetCidr, podIP) {
		klog.Infof("pod's ip %s is not in the range of subnet %s, delete pod", pod.Annotations[util.IPAddressAnnotation], podSubnet.Name)
		return true, nil
	}
	// subnet of ownerReference(sts/vm) has been changed, it needs to handle delete pod and create port on the new logical switch
	if ownerRefSubnet != "" && podSubnet.Name != ownerRefSubnet {
		klog.Infof("Subnet of owner %s has been changed from %s to %s, delete pod %s/%s", ownerRefName, podSubnet.Name, ownerRefSubnet, pod.Namespace, pod.Name)
		return true, nil
	}

	return false, nil
}

func isVMPod(pod *v1.Pod) (bool, string) {
	for _, owner := range pod.OwnerReferences {
		// The name of vmi is consistent with vm's name.
		if owner.Kind == util.VMInstance && strings.HasPrefix(owner.APIVersion, "kubevirt.io") {
			return true, owner.Name
		}
	}
	return false, ""
}

func isOwnsByTheVM(vmi metav1.Object) (bool, string) {
	for _, owner := range vmi.GetOwnerReferences() {
		if owner.Kind == util.VM && strings.HasPrefix(owner.APIVersion, "kubevirt.io") {
			return true, owner.Name
		}
	}
	return false, ""
}

func (c *Controller) isVMToDel(pod *v1.Pod, vmiName string) bool {
	var (
		vmiAlive bool
		vmName   string
	)
	// The vmi is also deleted when pod is deleted, only left vm exists.
	vmi, err := c.config.KubevirtClient.VirtualMachineInstance(pod.Namespace).Get(context.Background(), vmiName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			vmiAlive = false
			// The name of vmi is consistent with vm's name.
			vmName = vmiName
			klog.ErrorS(err, "failed to get vmi, will try to get the vm directly", "name", vmiName)
		} else {
			klog.ErrorS(err, "failed to get vmi", "name", vmiName)
			return false
		}
	} else {
		var ownsByVM bool
		ownsByVM, vmName = isOwnsByTheVM(vmi)
		if !ownsByVM && !vmi.DeletionTimestamp.IsZero() {
			klog.Infof("ephemeral vmi %s is deleting", vmiName)
			return true
		}
		vmiAlive = vmi.DeletionTimestamp.IsZero()
	}

	if vmiAlive {
		return false
	}

	vm, err := c.config.KubevirtClient.VirtualMachine(pod.Namespace).Get(context.Background(), vmName, metav1.GetOptions{})
	if err != nil {
		// the vm has gone
		if k8serrors.IsNotFound(err) {
			klog.ErrorS(err, "failed to get vm", "name", vmName)
			return true
		}
		klog.ErrorS(err, "failed to get vm", "name", vmName)
		return false
	}

	if !vm.DeletionTimestamp.IsZero() {
		klog.Infof("vm %s is deleting", vmName)
		return true
	}
	return false
}

func (c *Controller) getNameByPod(pod *v1.Pod) string {
	if c.config.EnableKeepVMIP {
		if isVMPod, vmName := isVMPod(pod); isVMPod {
			return vmName
		}
	}
	return pod.Name
}

// When subnet's v4availableIPs is 0 but still there's available ip in exclude-ips, the static ip in exclude-ips can be allocated normal.
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

		result = append(result, &kubeovnNet{
			Type:         providerTypeOriginal,
			ProviderName: subnet.Spec.Provider,
			Subnet:       subnet,
		})
	}

	return result, nil
}

func getPodType(pod *v1.Pod) string {
	if ok, _, _ := isStatefulSetPod(pod); ok {
		return util.StatefulSet
	}

	if isVMPod, _ := isVMPod(pod); isVMPod {
		return util.VM
	}
	return ""
}

func (c *Controller) getVirtualIPs(pod *v1.Pod, podNets []*kubeovnNet) map[string]string {
	vipsListMap := make(map[string][]string)
	var vipNamesList []string
	for _, vipName := range strings.Split(strings.TrimSpace(pod.Annotations[util.AAPsAnnotation]), ",") {
		if vipName = strings.TrimSpace(vipName); vipName == "" {
			continue
		}
		if !slices.Contains(vipNamesList, vipName) {
			vipNamesList = append(vipNamesList, vipName)
		} else {
			continue
		}
		vip, err := c.virtualIpsLister.Get(vipName)
		if err != nil {
			klog.Errorf("failed to get vip %s, %v", vipName, err)
			continue
		}
		if vip.Spec.Namespace != pod.Namespace || (vip.Status.V4ip == "" && vip.Status.V6ip == "") {
			continue
		}
		for _, podNet := range podNets {
			if podNet.Subnet.Name == vip.Spec.Subnet {
				key := fmt.Sprintf("%s.%s", podNet.Subnet.Name, podNet.ProviderName)
				vipsList := vipsListMap[key]
				if vipsList == nil {
					vipsList = []string{}
				}
				// ipam will ensure the uniqueness of VIP
				if util.IsValidIP(vip.Status.V4ip) {
					vipsList = append(vipsList, vip.Status.V4ip)
				}
				if util.IsValidIP(vip.Status.V6ip) {
					vipsList = append(vipsList, vip.Status.V6ip)
				}

				vipsListMap[key] = vipsList
			}
		}
	}

	for _, podNet := range podNets {
		vipStr := pod.Annotations[fmt.Sprintf(util.PortVipAnnotationTemplate, podNet.ProviderName)]
		if vipStr == "" {
			continue
		}
		key := fmt.Sprintf("%s.%s", podNet.Subnet.Name, podNet.ProviderName)
		vipsList := vipsListMap[key]
		if vipsList == nil {
			vipsList = []string{}
		}

		for _, vip := range strings.Split(vipStr, ",") {
			if util.IsValidIP(vip) && !slices.Contains(vipsList, vip) {
				vipsList = append(vipsList, vip)
			}
		}

		vipsListMap[key] = vipsList
	}

	vipsMap := make(map[string]string)
	for key, vipsList := range vipsListMap {
		vipsMap[key] = strings.Join(vipsList, ",")
	}
	return vipsMap
}

func setPodRoutesAnnotation(annotations map[string]string, provider string, routes []request.Route) error {
	key := fmt.Sprintf(util.RoutesAnnotationTemplate, provider)
	if len(routes) == 0 {
		delete(annotations, key)
		return nil
	}

	buf, err := json.Marshal(routes)
	if err != nil {
		err = fmt.Errorf("failed to marshal routes %+v: %w", routes, err)
		klog.Error(err)
		return err
	}
	annotations[key] = string(buf)

	return nil
}
