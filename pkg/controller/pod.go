package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"k8s.io/client-go/kubernetes"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/alauda/kube-ovn/pkg/ipam"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/ovs"
	"github.com/alauda/kube-ovn/pkg/util"
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

	defaultSubnet, err := c.getPodDefaultSubnet(pod)
	if err != nil {
		return err
	}

	attachmentSubnets, err := c.getPodAttachmentSubnet(pod)
	if err != nil {
		return err
	}

	podSubnets := attachmentSubnets
	if _, hasOtherDefaultNet := pod.Annotations[util.DefaultNetworkAnnotation]; !hasOtherDefaultNet {
		podSubnets = append(attachmentSubnets, defaultSubnet)
	}

	op := "replace"
	if pod.Annotations == nil || len(pod.Annotations) == 0 {
		op = "add"
		pod.Annotations = map[string]string{}
	}

	// Avoid create lsp for already running pod in ovn-nb when controller restart
	for _, subnet := range needAllocateSubnets(pod, podSubnets) {
		v4IP, v6IP, mac, err := c.acquireAddress(pod, subnet)
		if err != nil {
			c.recorder.Eventf(pod, v1.EventTypeWarning, "AcquireAddressFailed", err.Error())
			return err
		}
		ipStr := util.GetStringIP(v4IP, v6IP)

		if subnet.Spec.Vlan != "" {
			pod.Annotations[util.NetworkType] = util.NetworkTypeVlan
		} else {
			pod.Annotations[util.NetworkType] = util.NetworkTypeGeneve
		}
		pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, subnet.Spec.Provider)] = ipStr
		pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, subnet.Spec.Provider)] = mac
		pod.Annotations[fmt.Sprintf(util.CidrAnnotationTemplate, subnet.Spec.Provider)] = subnet.Spec.CIDRBlock
		pod.Annotations[fmt.Sprintf(util.GatewayAnnotationTemplate, subnet.Spec.Provider)] = subnet.Spec.Gateway
		pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, subnet.Spec.Provider)] = subnet.Name
		pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, subnet.Spec.Provider)] = "true"

		if err := util.ValidatePodCidr(subnet.Spec.CIDRBlock, ipStr); err != nil {
			klog.Errorf("validate pod %s/%s failed, %v", namespace, name, err)
			c.recorder.Eventf(pod, v1.EventTypeWarning, "ValidatePodNetworkFailed", err.Error())
			return err
		}

		if isOvnSubnet(subnet) {
			if subnet.Spec.Vlan != "" {
				vlan, err := c.vlansLister.Get(subnet.Spec.Vlan)
				if err != nil {
					c.recorder.Eventf(pod, v1.EventTypeWarning, "GetVlanInfoFailed", err.Error())
					return err
				}
				pod.Annotations[util.HostInterfaceName] = c.config.DefaultHostInterface
				pod.Annotations[util.VlanIdAnnotation] = strconv.Itoa(vlan.Spec.VlanId)
				pod.Annotations[util.ProviderInterfaceName] = c.config.DefaultProviderName
				pod.Annotations[util.VlanRangeAnnotation] = c.config.DefaultVlanRange
			}

			tag, err := c.getSubnetVlanTag(subnet)
			if err != nil {
				return err
			}

			portSecurity := false
			if pod.Annotations[util.PortSecurityAnnotation] == "true" {
				portSecurity = true
			}

			ip := pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, subnet.Spec.Provider)]
			if err := c.ovnClient.CreatePort(subnet.Name, ovs.PodNameToPortName(name, namespace), ip, subnet.Spec.CIDRBlock, mac, tag, portSecurity); err != nil {
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
		return nil
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

	if err := c.ovnClient.DeleteLogicalSwitchPort(ovs.PodNameToPortName(name, namespace)); err != nil {
		klog.Errorf("failed to delete lsp %s, %v", ovs.PodNameToPortName(name, namespace), err)
		return err
	}

	if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), ovs.PodNameToPortName(name, namespace), metav1.DeleteOptions{}); err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to delete ip %s, %v", ovs.PodNameToPortName(name, namespace), err)
			return err
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

	if _, hasOtherDefaultNet := pod.Annotations[util.DefaultNetworkAnnotation]; hasOtherDefaultNet {
		return nil
	}

	klog.Infof("update pod %s/%s", namespace, name)
	podIP := pod.Annotations[util.IpAddressAnnotation]

	subnet, err := c.getPodDefaultSubnet(pod)
	if err != nil {
		klog.Errorf("failed to get subnet %v", err)
		return err
	}

	vpc, err := c.vpcsLister.Get(subnet.Spec.Vpc)
	if err != nil {
		klog.Errorf("failed to get vpc %v", err)
		return err
	}

	if !subnet.Spec.UnderlayGateway {
		if vpc.Status.Default {
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
				nextHop = strings.Split(nextHop, "/")[0]

				if err := c.ovnClient.AddStaticRoute(ovs.PolicySrcIP, podIP, nextHop, c.config.ClusterRouter); err != nil {
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
							if err := c.ovnClient.AddStaticRoute(ovs.PolicySrcIP, podAddr, nodeAddr.String(), c.config.ClusterRouter); err != nil {
								klog.Errorf("failed to add static route, %v", err)
								return err
							}
						}
					}
				}

				if pod.Annotations[util.NorthGatewayAnnotation] != "" {
					if err := c.ovnClient.AddStaticRoute(ovs.PolicySrcIP, podIP, pod.Annotations[util.NorthGatewayAnnotation], vpc.Status.Router); err != nil {
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

func needAllocateSubnets(pod *v1.Pod, subnets []*kubeovnv1.Subnet) []*kubeovnv1.Subnet {
	if pod.Status.Phase == v1.PodRunning ||
		pod.Status.Phase == v1.PodSucceeded ||
		pod.Status.Phase == v1.PodFailed {
		return nil
	}

	if pod.Annotations == nil {
		return subnets
	}

	result := make([]*kubeovnv1.Subnet, 0, len(subnets))
	for _, subnet := range subnets {
		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, subnet.Spec.Provider)] != "true" {
			result = append(result, subnet)
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

func (c *Controller) getPodAttachmentSubnet(pod *v1.Pod) ([]*kubeovnv1.Subnet, error) {
	attachments, err := util.ParsePodNetworkAnnotation(pod.Annotations[util.AttachmentNetworkAnnotation], pod.Namespace)
	if err != nil {
		return nil, err
	}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	result := make([]*kubeovnv1.Subnet, 0, len(attachments))
	for _, attach := range attachments {
		provider := fmt.Sprintf("%s.%s", attach.Name, attach.Namespace)
		for _, subnet := range subnets {
			if subnet.Spec.Provider == provider {
				result = append(result, subnet)
				break
			}
		}
	}
	return result, nil
}

func (c *Controller) acquireAddress(pod *v1.Pod, subnet *kubeovnv1.Subnet) (string, string, string, error) {
	key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	macStr := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, subnet.Spec.Provider)]

	// Random allocate
	if pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, subnet.Spec.Provider)] == "" &&
		pod.Annotations[fmt.Sprintf(util.IpPoolAnnotationTemplate, subnet.Spec.Provider)] == "" {
		return c.ipam.GetRandomAddress(key, subnet.Name)
	}

	// Static allocate
	if pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, subnet.Spec.Provider)] != "" {
		ipStr := pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, subnet.Spec.Provider)]
		return c.acquireStaticAddress(key, ipStr, macStr, subnet.Name)
	}

	// IPPool allocate
	ipPool := strings.Split(pod.Annotations[fmt.Sprintf(util.IpPoolAnnotationTemplate, subnet.Spec.Provider)], ",")
	for i, ip := range ipPool {
		ipPool[i] = strings.TrimSpace(ip)
	}

	if ok, _ := isStatefulSetPod(pod); !ok {
		for _, staticIP := range ipPool {
			if c.ipam.IsIPAssignedToPod(staticIP, subnet.Name) {
				continue
			}
			if v4IP, v6IP, mac, err := c.acquireStaticAddress(key, staticIP, macStr, subnet.Name); err == nil {
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
			return c.acquireStaticAddress(key, ipPool[index], macStr, subnet.Name)
		}
	}
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
		klog.Errorf("failed to get static ip %v, mac %v, err %v", ip, mac, err)
		return "", "", "", err
	}
	return v4IP, v6IP, mac, nil
}
