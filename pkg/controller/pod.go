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
	"k8s.io/klog"

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
	if p.Status.PodIP != "" {
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
				c.deletePodQueue.Add(key)
			}
		} else {
			klog.V(3).Infof("enqueue delete pod %s", key)
			c.deletePodQueue.Add(key)
		}
		return
	}

	// In case update event might lost during leader election
	if p.Annotations != nil &&
		p.Annotations[util.AllocatedAnnotation] == "true" &&
		p.Annotations[util.RoutedAnnotation] != "true" &&
		p.Status.HostIP != "" && p.Status.PodIP != "" {
		c.updatePodQueue.Add(key)
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
	for _, np := range c.podMatchNetworkPolicies(p) {
		c.updateNpQueue.Add(np)
	}

	if p.Spec.HostNetwork {
		return
	}

	isStateful, statefulSetName := isStatefulSetPod(p)
	if isStateful {
		if isStatefulSetPodToDel(c.config.KubeClient, p, statefulSetName) {
			klog.V(3).Infof("enqueue delete pod %s", key)
			c.deletePodQueue.Add(key)
		}
	} else {
		klog.V(3).Infof("enqueue delete pod %s", key)
		c.deletePodQueue.Add(key)
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

	if !isPodAlive(newPod) {
		isStateful, statefulSetName := isStatefulSetPod(newPod)
		if isStateful {
			if isStatefulSetPodToDel(c.config.KubeClient, newPod, statefulSetName) {
				klog.V(3).Infof("enqueue delete pod %s", key)
				c.deletePodQueue.Add(key)
			}
		} else {
			klog.V(3).Infof("enqueue delete pod %s", key)
			c.deletePodQueue.Add(key)
		}
		return
	}

	if newPod.DeletionTimestamp != nil {
		go func() {
			// In case node get lost and pod can not be deleted,
			// the ipaddress will not be recycled
			time.Sleep(time.Duration(*newPod.Spec.TerminationGracePeriodSeconds) * time.Second)
			c.deletePodQueue.Add(key)
		}()
		return
	}

	// pod assigned an ip
	if newPod.Annotations[util.AllocatedAnnotation] == "true" &&
		newPod.Annotations[util.RoutedAnnotation] != "true" &&
		newPod.Spec.NodeName != "" {
		klog.V(3).Infof("enqueue update pod %s", key)
		c.updatePodQueue.Add(key)
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
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.deletePodQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		klog.Infof("handle delete pod %s", key)
		if err := c.handleDeletePod(key); err != nil {
			c.deletePodQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.deletePodQueue.Forget(obj)
		last := time.Since(now)
		klog.Infof("take %d ms to handle delete pod %s", last.Milliseconds(), key)
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

	for i, net1 := range podNets {
		if i >= len(podNets)-1 {
			break
		}
		for _, net2 := range podNets[i+1:] {
			if net1.Subnet.Name == net2.Subnet.Name {
				return nil, fmt.Errorf("subnet conflict, the same subnet should not be attached repeatedly")
			}
		}
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

	podNets, err := c.getPodKubeovnNets(pod)
	if err != nil {
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
			klog.Errorf("validate pod %s/%s failed, %v", namespace, name, err)
			c.recorder.Eventf(pod, v1.EventTypeWarning, "ValidatePodNetworkFailed", err.Error())
			return err
		}

		if podNet.Type != providerTypeIPAM {
			if subnet.Spec.Vlan != "" {
				vlan, err := c.vlansLister.Get(subnet.Spec.Vlan)
				if err != nil {
					c.recorder.Eventf(pod, v1.EventTypeWarning, "GetVlanInfoFailed", err.Error())
					return err
				}
				pod.Annotations[util.HostInterfaceName] = c.config.DefaultHostInterface
				pod.Annotations[fmt.Sprintf(util.VlanIdAnnotationTemplate, podNet.ProviderName)] = strconv.Itoa(vlan.Spec.VlanId)
				pod.Annotations[util.ProviderName] = c.config.DefaultProviderName
				pod.Annotations[fmt.Sprintf(util.VlanRangeAnnotationTemplate, podNet.ProviderName)] = c.config.DefaultVlanRange
			}

			tag, err := c.getSubnetVlanTag(subnet)
			if err != nil {
				return err
			}

			portSecurity := false
			if pod.Annotations[util.PortSecurityAnnotation] == "true" {
				portSecurity = true
			}

			if err := c.ovnClient.CreatePort(subnet.Name, ovs.PodNameToPortName(name, namespace, podNet.ProviderName), ipStr, subnet.Spec.CIDRBlock, mac, tag, pod.Name, pod.Namespace, portSecurity); err != nil {
				c.recorder.Eventf(pod, v1.EventTypeWarning, "CreateOVNPortFailed", err.Error())
				return err
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
		klog.Errorf("patch pod %s/%s failed %v", name, namespace, err)
		return err
	}

	if vpcGwName, isVpcNatGw := pod.Annotations[util.VpcNatGatewayAnnotation]; isVpcNatGw {
		c.initVpcNatGatewayQueue.Add(vpcGwName)
	}
	return nil
}

func (c *Controller) handleDeletePod(key string) error {
	c.podKeyMutex.Lock(key)
	defer c.podKeyMutex.Unlock(key)
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	pod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
	}
	if pod != nil && pod.DeletionTimestamp == nil && isPodAlive(pod) {
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

	ports, err := c.ovnClient.ListPodLogicalSwitchPorts(name, namespace)
	if err != nil {
		klog.Errorf("failed to list lsps of pod '%s', %v", name, err)
		return err
	}

	// Add additional default ports to compatible with previous versions
	ports = append(ports, ovs.PodNameToPortName(name, namespace, util.OvnProvider))
	for _, portName := range ports {
		if err := c.ovnClient.DeleteLogicalSwitchPort(portName); err != nil {
			klog.Errorf("failed to delete lsp %s, %v", portName, err)
			return err
		}
		if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), portName, metav1.DeleteOptions{}); err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to delete ip %s, %v", portName, err)
				return err
			}
		}
	}
	c.ipam.ReleaseAddressByPod(key)
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
	pod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
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
		// routing should be configured only if the OVN network is the default network
		if !podNet.IsDefault || util.OvnProvider != podNet.ProviderName {
			continue
		}
		podIP = pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)]
		subnet = podNet.Subnet
		break
	}

	if podIP != "" && subnet.Spec.Vpc == util.DefaultVpc && !subnet.Spec.UnderlayGateway {
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

	pod.Annotations[util.RoutedAnnotation] = "true"
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
	numIndex := len(strings.Split(pod.Name, "-")) - 1
	numStr := strings.Split(pod.Name, "-")[numIndex]
	index, err := strconv.ParseInt(numStr, 10, 0)
	if err != nil {
		klog.Errorf("failed to parse %s to int", numStr)
		return false
	}

	if index >= int64(*ss.Spec.Replicas) {
		return true
	}

	return false
}

func getNodeTunlIP(node *v1.Node) ([]net.IP, error) {
	var nodeTunlIPAddr []net.IP
	nodeTunlIP := node.Annotations[util.IpAddressAnnotation]
	if nodeTunlIP == "" {
		return nil, fmt.Errorf("node has no tunl ip annotation")
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
	if pod.Status.Phase == v1.PodRunning ||
		pod.Status.Phase == v1.PodSucceeded ||
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
			klog.Errorf("failed to get namespace %v", err)
			return nil, err
		}
		if ns.Annotations == nil {
			err = fmt.Errorf("namespace network annotations is nil")
			klog.Error(err)
			return nil, err
		}

		subnetName = ns.Annotations[util.LogicalSwitchAnnotation]
		if subnetName == "" {
			err = fmt.Errorf("namespace default logical switch is not found")
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
	Type         providerType
	ProviderName string
	Subnet       *kubeovnv1.Subnet
	IsDefault    bool
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
			isDefault := util.IsDefaultNet(pod.Annotations[util.DefaultNetworkAnnotation], attach)
			if isDefault {
				providerName = util.OvnProvider
			} else {
				providerName = fmt.Sprintf("%s.%s.ovn", attach.Name, attach.Namespace)
			}
			subnetName := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, providerName)]
			if subnetName == "" {
				subnetName = c.config.DefaultLogicalSwitch
			}
			subnet, err := c.subnetsLister.Get(subnetName)
			if err != nil {
				klog.Errorf("failed to get subnet %s, %v", subnetName, err)
				return nil, err
			}
			result = append(result, &kubeovnNet{
				Type:         providerTypeOriginal,
				ProviderName: providerName,
				Subnet:       subnet,
				IsDefault:    isDefault,
			})

		}

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
	return result, nil
}

func (c *Controller) acquireAddress(pod *v1.Pod, podNet *kubeovnNet) (string, string, string, error) {
	key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	macStr := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)]

	// Random allocate
	if pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)] == "" &&
		pod.Annotations[fmt.Sprintf(util.IpPoolAnnotationTemplate, podNet.ProviderName)] == "" {
		return c.ipam.GetRandomAddress(key, podNet.Subnet.Name)
	}

	// Static allocate
	if pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)] != "" {
		ipStr := pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)]
		return c.acquireStaticAddress(key, ipStr, macStr, podNet.Subnet.Name)
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
			if v4IP, v6IP, mac, err := c.acquireStaticAddress(key, staticIP, macStr, podNet.Subnet.Name); err == nil {
				return v4IP, v6IP, mac, nil
			} else {
				klog.Errorf("acquire address %s for %s failed, %v", staticIP, key, err)
			}
		}
	} else {
		numIndex := len(strings.Split(pod.Name, "-")) - 1
		numStr := strings.Split(pod.Name, "-")[numIndex]
		index, _ := strconv.Atoi(numStr)
		if index < len(ipPool) {
			return c.acquireStaticAddress(key, ipPool[index], macStr, podNet.Subnet.Name)
		}
	}
	klog.Errorf("alloc address for %s failed, return NoAvailableAddress", key)
	return "", "", "", ipam.NoAvailableError
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

func (c *Controller) acquireStaticAddress(key, ip, mac, subnet string) (string, string, string, error) {
	var v4IP, v6IP string
	var err error
	for _, ipStr := range strings.Split(ip, ",") {
		if net.ParseIP(ipStr) == nil {
			return "", "", "", fmt.Errorf("failed to parse IP %s", ipStr)
		}
	}

	if v4IP, v6IP, mac, err = c.ipam.GetStaticAddress(key, ip, mac, subnet); err != nil {
		klog.Errorf("failed to get static ip %v, mac %v, subnet %v, err %v", ip, mac, subnet, err)
		return "", "", "", err
	}
	return v4IP, v6IP, mac, nil
}
