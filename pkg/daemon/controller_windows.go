package daemon

import (
	"strings"

	"k8s.io/client-go/informers"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovninformer "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Controller watch pod and namespace changes to update iptables, ipset and ovs qos
type Controller struct {
	*ControllerBase
}

// NewController init a daemon controller
func NewController(config *Configuration, podInformerFactory informers.SharedInformerFactory, nodeInformerFactory informers.SharedInformerFactory, kubeovnInformerFactory kubeovninformer.SharedInformerFactory) (*Controller, error) {
	base, err := newControllerBase(config, podInformerFactory, nodeInformerFactory, kubeovnInformerFactory)
	if err != nil {
		klog.Errorf("failed to initialize controller: %v", err)
		return nil, err
	}

	return &Controller{ControllerBase: base}, nil

}

func (c *ControllerBase) reconcileRouters(event subnetEvent) error {
	// subnets, err := c.subnetsLister.List(labels.Everything())
	// if err != nil {
	// 	klog.Errorf("failed to list subnets: %v", err)
	// 	return err
	// }

	// var ok bool
	// var oldSubnet, newSubnet *kubeovnv1.Subnet
	// if event.old != nil {
	// 	if oldSubnet, ok = event.old.(*kubeovnv1.Subnet); !ok {
	// 		klog.Errorf("expected old subnet in subnetEvent but got %#v", event.old)
	// 		return nil
	// 	}
	// }
	// if event.new != nil {
	// 	if newSubnet, ok = event.new.(*kubeovnv1.Subnet); !ok {
	// 		klog.Errorf("expected new subnet in subnetEvent but got %#v", event.new)
	// 		return nil
	// 	}
	// }

	// routesToAdd, routesToDel := c.diffRoutes(oldSubnet, newSubnet)
	// for _, r := range routesToDel {
	// 	if err = netlink.RouteAdd(&r); err != nil && !errors.Is(err, syscall.EEXIST) {
	// 		klog.Errorf("failed to add route for subnet %s: %v", newSubnet.Name, err)
	// 		return err
	// 	}
	// }

	// last, delete old network routes
	// for _, r := range routesToDel {
	// 	if err = netlink.RouteDel(&r); err != nil && !errors.Is(err, syscall.ENOENT) {
	// 		klog.Errorf("failed to delete route for subnet %s: %v", oldSubnet.Name, err)
	// 		return err
	// 	}
	// }

	// cidrs := make([]string, 0, len(subnets)*2)
	// for _, subnet := range subnets {
	// 	if subnet.Spec.Vlan != "" || subnet.Spec.Vpc != util.DefaultVpc || !subnet.Status.IsReady() {
	// 		continue
	// 	}

	// 	for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
	// 		if _, ipNet, err := net.ParseCIDR(cidrBlock); err != nil {
	// 			klog.Errorf("%s is not a valid cidr block", cidrBlock)
	// 		} else {
	// 			cidrs = append(cidrs, ipNet.String())
	// 		}
	// 	}
	// }

	// node, err := c.nodesLister.Get(c.config.NodeName)
	// if err != nil {
	// 	klog.Errorf("failed to get node %s %v", c.config.NodeName, err)
	// 	return err
	// }
	// gateway, ok := node.Annotations[util.GatewayAnnotation]
	// if !ok {
	// 	klog.Errorf("annotation for node %s ovn.kubernetes.io/gateway not exists", node.Name)
	// 	return fmt.Errorf("annotation for node ovn.kubernetes.io/gateway not exists")
	// }
	// nic, err := netlink.LinkByName(util.NodeNic)
	// if err != nil {
	// 	klog.Errorf("failed to get nic %s", util.NodeNic)
	// 	return fmt.Errorf("failed to get nic %s", util.NodeNic)
	// }

	// existRoutes, err := getNicExistRoutes(nic, gateway)
	// if err != nil {
	// 	return err
	// }

	// toAdd, toDel := routeDiff(existRoutes, cidrs)
	// for _, r := range toDel {
	// 	_, cidr, _ := net.ParseCIDR(r)
	// 	if err = netlink.RouteDel(&netlink.Route{Dst: cidr}); err != nil {
	// 		klog.Errorf("failed to del route %v", err)
	// 	}
	// }

	// for _, r := range toAdd {
	// 	_, cidr, _ := net.ParseCIDR(r)
	// 	for _, gw := range strings.Split(gateway, ",") {
	// 		if util.CheckProtocol(gw) != util.CheckProtocol(r) {
	// 			continue
	// 		}
	// 		if err = netlink.RouteReplace(&netlink.Route{Dst: cidr, LinkIndex: nic.Attrs().Index, Scope: netlink.SCOPE_UNIVERSE, Gw: net.ParseIP(gw)}); err != nil {
	// 			klog.Errorf("failed to add route %v", err)
	// 		}
	// 	}
	// }
	return nil
}

func (c *ControllerBase) diffRoutes(oldSubnet, newSubnet *kubeovnv1.Subnet) (toAdd, toDel []string) {
	oldRoutes, newRoutes := c.calcHostRoutes(oldSubnet), c.calcHostRoutes(newSubnet)
	return getRoutesToAdd(oldRoutes, newRoutes), getRoutesToAdd(newRoutes, oldRoutes)
}

func (c *ControllerBase) calcHostRoutes(subnet *kubeovnv1.Subnet) []string {
	if subnet == nil ||
		(subnet.Spec.Vlan != "" && subnet.Spec.LogicalGateway) ||
		subnet.Spec.Vpc != util.DefaultVpc {
		return nil
	}
	return strings.Split(subnet.Spec.CIDRBlock, ",")
}

func getRoutesToAdd(oldRoutes, newRoutes []string) []string {
	var result []string
	for _, route := range newRoutes {
		if !util.ContainsString(oldRoutes, route) {
			result = append(result, route)
		}
	}
	return result
}

func (c *Controller) handlePod(key string) error {
	// TODO
	return nil
}

func (c *Controller) loopEncapIpCheck() {
	return
}

func (c *Controller) loopCheckSubnetQosPriority() {
	// TODO
}

func (c *ControllerBase) clearQos(podName, podNamespace, ifaceID string) error {
	// TODO
	return nil
}

func rotateLog() {
}

func (c *Controller) operateMod() {
}

func recompute() {
	// TODO
}
