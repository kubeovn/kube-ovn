package daemon

import (
	"fmt"
	"net"
	"strings"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// ControllerRuntime represents runtime specific controller members
type ControllerRuntime struct{}

func (c *Controller) initRuntime() error {
	return nil
}

func (c *Controller) reconcileRouters(_ subnetEvent) error {
	klog.Info("reconcile routes")
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s %v", c.config.NodeName, err)
		return err
	}
	gateway, ok := node.Annotations[util.GatewayAnnotation]
	if !ok {
		err = fmt.Errorf("node %s does not have annotation %s", node.Name, util.GatewayAnnotation)
		klog.Error(err)
		return err
	}
	cidr, ok := node.Annotations[util.CidrAnnotation]
	if !ok {
		err = fmt.Errorf("node %s does not have annotation %s", node.Name, util.CidrAnnotation)
		klog.Error(err)
		return err
	}

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return err
	}

	gwIPv4, gwIPv6 := util.SplitStringIP(gateway)
	v4Cidrs, v6Cidrs := make([]string, 0, len(subnets)), make([]string, 0, len(subnets))
	for _, subnet := range subnets {
		if (subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) ||
			subnet.Spec.Vpc != util.DefaultVpc ||
			!subnet.Status.IsReady() {
			continue
		}

		for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
			if _, ipNet, err := net.ParseCIDR(cidrBlock); err != nil {
				klog.Errorf("%s is not a valid cidr block", cidrBlock)
			} else {
				_, bits := ipNet.Mask.Size()
				if bits == 32 {
					if gwIPv4 != "" && !util.CIDRContainIP(cidrBlock, gwIPv4) {
						v4Cidrs = append(v4Cidrs, ipNet.String())
					}
				} else {
					if gwIPv6 != "" && !util.CIDRContainIP(cidrBlock, gwIPv6) {
						v6Cidrs = append(v6Cidrs, ipNet.String())
					}
				}
			}
		}
	}

	adapter, err := util.GetNetAdapter(util.NodeNic, false)
	if err != nil {
		klog.Errorf("failed to get network adapter %s: %v", util.NodeNic, err)
		return err
	}
	routes, err := util.GetNetRoute(adapter.InterfaceIndex)
	if err != nil {
		klog.Errorf("failed to get NetIPRoute with index %d: %v", adapter.InterfaceIndex, err)
		return err
	}

	existingRoutes := make([]string, 0, len(routes))
	for _, route := range routes {
		if route.NextHop == "0.0.0.0" || route.NextHop == "::" {
			continue
		}
		existingRoutes = append(existingRoutes, route.DestinationPrefix)
	}

	toAddV4, toAddV6, toDel := routeDiff(existingRoutes, v4Cidrs, v6Cidrs)
	klog.Infof("routes to be added: %v", append(toAddV4, toAddV6...))
	klog.Infof("routes to be removed: %v", toDel)
	for _, r := range toAddV4 {
		if err = util.NewNetRoute(adapter.InterfaceIndex, r, gwIPv4); err != nil {
			klog.Errorf("failed to del route %s: %v", r, err)
		}
	}
	for _, r := range toAddV6 {
		if err = util.NewNetRoute(adapter.InterfaceIndex, r, gwIPv6); err != nil {
			klog.Errorf("failed to del route %s: %v", r, err)
		}
	}
	for _, r := range toDel {
		if err = util.RemoveNetRoute(adapter.InterfaceIndex, r); err != nil {
			klog.Errorf("failed to remove route %s: %v", r, err)
		}
	}

	if err = util.NewNetNat(util.NetNat, cidr, true); err != nil {
		klog.Error(err)
		return err
	}

	return nil
}

func routeDiff(existingRoutes, v4Cidrs, v6Cidrs []string) (toAddV4, toAddV6, toDel []string) {
	existing := make(map[string]struct{}, len(existingRoutes))
	expectedV4 := make(map[string]struct{}, len(v4Cidrs))
	expectedV6 := make(map[string]struct{}, len(v6Cidrs))
	for _, r := range existingRoutes {
		existing[r] = struct{}{}
	}

	var ok bool
	for _, r := range v4Cidrs {
		expectedV4[r] = struct{}{}
		if _, ok = existing[r]; !ok {
			toAddV4 = append(toAddV4, r)
		}
	}
	for _, r := range v6Cidrs {
		expectedV6[r] = struct{}{}
		if _, ok = existing[r]; !ok {
			toAddV6 = append(toAddV6, r)
		}
	}
	for _, r := range existingRoutes {
		if _, ok = expectedV4[r]; ok {
			continue
		}
		if _, ok = expectedV6[r]; ok {
			continue
		}
		toDel = append(toDel, r)
	}

	return
}

func (c *Controller) handlePod(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	klog.Infof("handle pod %s/%s", namespace, name)

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

	podName := pod.Name
	if pod.Annotations[fmt.Sprintf(util.VmTemplate, util.OvnProvider)] != "" {
		podName = pod.Annotations[fmt.Sprintf(util.VmTemplate, util.OvnProvider)]
	}

	// set default nic bandwidth
	ifaceID := ovs.PodNameToPortName(podName, pod.Namespace, util.OvnProvider)
	err = ovs.SetInterfaceBandwidth(podName, pod.Namespace, ifaceID, pod.Annotations[util.EgressRateAnnotation], pod.Annotations[util.IngressRateAnnotation])
	if err != nil {
		return err
	}
	err = ovs.ConfigInterfaceMirror(c.config.EnableMirror, pod.Annotations[util.MirrorControlAnnotation], ifaceID)
	if err != nil {
		return err
	}

	// set multus-nic bandwidth
	attachNets, err := util.ParsePodNetworkAnnotation(pod.Annotations[util.AttachmentNetworkAnnotation], pod.Namespace)
	if err != nil {
		return err
	}
	for _, multiNet := range attachNets {
		provider := fmt.Sprintf("%s.%s.ovn", multiNet.Name, multiNet.Namespace)
		if pod.Annotations[fmt.Sprintf(util.VmTemplate, provider)] != "" {
			podName = pod.Annotations[fmt.Sprintf(util.VmTemplate, provider)]
		}
		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)] == "true" {
			ifaceID = ovs.PodNameToPortName(podName, pod.Namespace, provider)
			err = ovs.SetInterfaceBandwidth(podName, pod.Namespace, ifaceID, pod.Annotations[fmt.Sprintf(util.EgressRateAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.IngressRateAnnotationTemplate, provider)])
			if err != nil {
				return err
			}
			err = ovs.ConfigInterfaceMirror(c.config.EnableMirror, pod.Annotations[fmt.Sprintf(util.MirrorControlAnnotationTemplate, provider)], ifaceID)
			if err != nil {
				return err
			}
			err = ovs.SetNetemQos(podName, pod.Namespace, ifaceID, pod.Annotations[fmt.Sprintf(util.NetemQosLatencyAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.NetemQosLimitAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.NetemQosLossAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.NetemQosJitterAnnotationTemplate, provider)])
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) loopEncapIpCheck() {
	// TODO
}

func rotateLog() {
	// TODO
}

func (c *Controller) ovnMetricsUpdate() {
	// TODO
}

func (c *Controller) operateMod() {
}
