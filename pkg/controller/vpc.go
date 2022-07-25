package controller

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddVpc(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add vpc %s", key)
	vpc := obj.(*kubeovnv1.Vpc)
	if _, ok := vpc.Labels[util.VpcExternalLabel]; !ok {
		c.addOrUpdateVpcQueue.Add(key)
	}
}

func (c *Controller) enqueueUpdateVpc(old, new interface{}) {
	if !c.isLeader() {
		return
	}
	oldVpc := old.(*kubeovnv1.Vpc)
	newVpc := new.(*kubeovnv1.Vpc)

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}

	_, oldOk := oldVpc.Labels[util.VpcExternalLabel]
	_, newOk := newVpc.Labels[util.VpcExternalLabel]
	if oldOk || newOk {
		return
	}

	if !newVpc.DeletionTimestamp.IsZero() ||
		!reflect.DeepEqual(oldVpc.Spec.Namespaces, newVpc.Spec.Namespaces) ||
		!reflect.DeepEqual(oldVpc.Spec.StaticRoutes, newVpc.Spec.StaticRoutes) ||
		!reflect.DeepEqual(oldVpc.Spec.PolicyRoutes, newVpc.Spec.PolicyRoutes) ||
		!reflect.DeepEqual(oldVpc.Spec.VpcPeerings, newVpc.Spec.VpcPeerings) ||
		!reflect.DeepEqual(oldVpc.Annotations, newVpc.Annotations) {
		klog.V(3).Infof("enqueue update vpc %s", key)
		c.addOrUpdateVpcQueue.Add(key)
	}
}

func (c *Controller) enqueueDelVpc(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	vpc := obj.(*kubeovnv1.Vpc)
	_, ok := vpc.Labels[util.VpcExternalLabel]
	if !vpc.Status.Default || !ok {
		klog.V(3).Infof("enqueue delete vpc %s", key)
		c.delVpcQueue.Add(obj)
	}
}

func (c *Controller) runAddVpcWorker() {
	for c.processNextAddVpcWorkItem() {
	}
}

func (c *Controller) runUpdateVpcStatusWorker() {
	for c.processNextUpdateStatusVpcWorkItem() {
	}
}

func (c *Controller) runDelVpcWorker() {
	for c.processNextDeleteVpcWorkItem() {
	}
}

func (c *Controller) handleDelVpc(vpc *kubeovnv1.Vpc) error {
	if err := c.deleteVpcLb(vpc); err != nil {
		return err
	}

	err := c.deleteVpcRouter(vpc.Status.Router)
	if err != nil {
		return err
	}
	return nil
}

func (c *Controller) handleUpdateVpcStatus(key string) error {
	orivpc, err := c.vpcsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	vpc := orivpc.DeepCopy()

	subnets, defaultSubnet, err := c.getVpcSubnets(vpc)
	if err != nil {
		return err
	}

	change := false
	if vpc.Status.DefaultLogicalSwitch != defaultSubnet {
		change = true
	}

	vpc.Status.DefaultLogicalSwitch = defaultSubnet
	vpc.Status.Subnets = subnets
	bytes, err := vpc.Status.Bytes()
	if err != nil {
		return err
	}

	vpc, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(), vpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
	if err != nil {
		return err
	}
	if change {
		for _, ns := range vpc.Spec.Namespaces {
			c.addNamespaceQueue.Add(ns)
		}
	}

	natGws, err := c.vpcNatGatewayLister.List(labels.Everything())
	if err != nil {
		return err
	}
	for _, gw := range natGws {
		if key == gw.Spec.Vpc {
			c.updateVpcSubnetQueue.Add(gw.Name)
		}
	}
	return nil
}

func (c *Controller) reconcileRouterPorts(vpc *kubeovnv1.Vpc) error {
	subnets, _, err := c.getVpcSubnets(vpc)
	if err != nil {
		klog.ErrorS(err, "unable to get related subnets", "vpc", vpc.Name)
		return err
	}

	router := vpc.Name
	for _, subnetName := range subnets {
		routerPortName := ovs.LogicalRouterPortName(router, subnetName)
		exists, err := c.ovnClient.LogicalRouterPortExists(routerPortName)
		if err != nil {
			return err
		}

		if !exists {
			subnet, err := c.subnetsLister.Get(subnetName)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					continue
				}
				klog.ErrorS(err, "unable to get subnet", "subnet", subnetName)
				return err
			}

			klog.V(1).InfoS("router port not exists, trying to create", "vpc", vpc.Name, "subnet", subnetName)

			networks := util.GetIpAddrWithMask(subnet.Spec.Gateway, subnet.Spec.CIDRBlock)
			if err := c.ovnClient.AddLogicalRouterPort(router, routerPortName, "", networks); err != nil {
				klog.ErrorS(err, "unable to create router port", "vpc", vpc.Name, "subnet", subnetName)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) reconcileRouterPortBySubnet(vpc *kubeovnv1.Vpc, subnet *kubeovnv1.Subnet) error {
	router := vpc.Name
	routerPortName := ovs.LogicalRouterPortName(router, subnet.Name)
	exists, err := c.ovnClient.LogicalRouterPortExists(routerPortName)
	if err != nil {
		return err
	}

	if !exists {
		subnet, err := c.subnetsLister.Get(subnet.Name)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Errorf("failed to get subnet %s, err %v", subnet.Name, err)
			return err
		}

		networks := util.GetIpAddrWithMask(subnet.Spec.Gateway, subnet.Spec.CIDRBlock)
		klog.Infof("router port does not exist, trying to create %s with ip %s", routerPortName, networks)

		if err := c.ovnClient.AddLogicalRouterPort(router, routerPortName, "", networks); err != nil {
			klog.Errorf("failed to create router port %s, err %v", routerPortName, err)
			return err
		}
	}
	return nil
}

type VpcLoadBalancer struct {
	TcpLoadBalancer     string
	TcpSessLoadBalancer string
	UdpLoadBalancer     string
	UdpSessLoadBalancer string
}

func (c *Controller) GenVpcLoadBalancer(vpcKey string) *VpcLoadBalancer {
	if vpcKey == util.DefaultVpc || vpcKey == "" {
		return &VpcLoadBalancer{
			TcpLoadBalancer:     c.config.ClusterTcpLoadBalancer,
			TcpSessLoadBalancer: c.config.ClusterTcpSessionLoadBalancer,
			UdpLoadBalancer:     c.config.ClusterUdpLoadBalancer,
			UdpSessLoadBalancer: c.config.ClusterUdpSessionLoadBalancer,
		}
	} else {
		return &VpcLoadBalancer{
			TcpLoadBalancer:     fmt.Sprintf("vpc-%s-tcp-load", vpcKey),
			TcpSessLoadBalancer: fmt.Sprintf("vpc-%s-tcp-sess-load", vpcKey),
			UdpLoadBalancer:     fmt.Sprintf("vpc-%s-udp-load", vpcKey),
			UdpSessLoadBalancer: fmt.Sprintf("vpc-%s-udp-sess-load", vpcKey),
		}
	}
}

func (c *Controller) addLoadBalancer(vpc string) (*VpcLoadBalancer, error) {
	vpcLbConfig := c.GenVpcLoadBalancer(vpc)

	tcpLb, err := c.ovnLegacyClient.FindLoadbalancer(vpcLbConfig.TcpLoadBalancer)
	if err != nil {
		return nil, fmt.Errorf("failed to find tcp lb %v", err)
	}
	if tcpLb == "" {
		klog.Infof("init cluster tcp load balancer %s", vpcLbConfig.TcpLoadBalancer)
		err := c.ovnLegacyClient.CreateLoadBalancer(vpcLbConfig.TcpLoadBalancer, util.ProtocolTCP, "")
		if err != nil {
			klog.Errorf("failed to crate cluster tcp load balancer %v", err)
			return nil, err
		}
	} else {
		klog.Infof("tcp load balancer %s exists", tcpLb)
	}

	tcpSessionLb, err := c.ovnLegacyClient.FindLoadbalancer(vpcLbConfig.TcpSessLoadBalancer)
	if err != nil {
		return nil, fmt.Errorf("failed to find tcp session lb %v", err)
	}
	if tcpSessionLb == "" {
		klog.Infof("init cluster tcp session load balancer %s", vpcLbConfig.TcpSessLoadBalancer)
		err := c.ovnLegacyClient.CreateLoadBalancer(vpcLbConfig.TcpSessLoadBalancer, util.ProtocolTCP, "ip_src")
		if err != nil {
			klog.Errorf("failed to crate cluster tcp session load balancer %v", err)
			return nil, err
		}
	} else {
		klog.Infof("tcp session load balancer %s exists", tcpSessionLb)
	}

	udpLb, err := c.ovnLegacyClient.FindLoadbalancer(vpcLbConfig.UdpLoadBalancer)
	if err != nil {
		return nil, fmt.Errorf("failed to find udp lb %v", err)
	}
	if udpLb == "" {
		klog.Infof("init cluster udp load balancer %s", vpcLbConfig.UdpLoadBalancer)
		err := c.ovnLegacyClient.CreateLoadBalancer(vpcLbConfig.UdpLoadBalancer, util.ProtocolUDP, "")
		if err != nil {
			klog.Errorf("failed to crate cluster udp load balancer %v", err)
			return nil, err
		}
	} else {
		klog.Infof("udp load balancer %s exists", udpLb)
	}

	udpSessionLb, err := c.ovnLegacyClient.FindLoadbalancer(vpcLbConfig.UdpSessLoadBalancer)
	if err != nil {
		return nil, fmt.Errorf("failed to find udp session lb %v", err)
	}
	if udpSessionLb == "" {
		klog.Infof("init cluster udp session load balancer %s", vpcLbConfig.UdpSessLoadBalancer)
		err := c.ovnLegacyClient.CreateLoadBalancer(vpcLbConfig.UdpSessLoadBalancer, util.ProtocolUDP, "ip_src")
		if err != nil {
			klog.Errorf("failed to crate cluster udp session load balancer %v", err)
			return nil, err
		}
	} else {
		klog.Infof("udp session load balancer %s exists", udpSessionLb)
	}

	return vpcLbConfig, nil
}

func (c *Controller) handleAddOrUpdateVpc(key string) error {
	// get latest vpc info
	orivpc, err := c.config.KubeOvnClient.KubeovnV1().Vpcs().Get(context.Background(), key, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	vpc := orivpc.DeepCopy()

	if err = formatVpc(vpc, c); err != nil {
		klog.Errorf("failed to format vpc: %v", err)
		return err
	}
	if err = c.createVpcRouter(key); err != nil {
		return err
	}

	if err := c.reconcileRouterPorts(vpc); err != nil {
		klog.ErrorS(err, "unable to reconcileRouterPorts")
		return err
	}

	var newPeers []string
	for _, peering := range vpc.Spec.VpcPeerings {
		if err = util.CheckCidrs(peering.LocalConnectIP); err != nil {
			klog.Errorf("invalid cidr %s", peering.LocalConnectIP)
			return err
		}
		newPeers = append(newPeers, peering.RemoteVpc)
		if err := c.ovnLegacyClient.CreatePeerRouterPort(vpc.Name, peering.RemoteVpc, peering.LocalConnectIP); err != nil {
			klog.Errorf("failed to create peer router port for vpc %s, %v", vpc.Name, err)
			return err
		}
	}
	for _, oldPeer := range vpc.Status.VpcPeerings {
		if !util.ContainsString(newPeers, oldPeer) {
			if err = c.ovnLegacyClient.DeleteLogicalRouterPort(fmt.Sprintf("%s-%s", vpc.Name, oldPeer)); err != nil {
				klog.Errorf("failed to delete peer router port for vpc %s, %v", vpc.Name, err)
				return err
			}
		}
	}

	if vpc.Name != util.DefaultVpc {
		// handle static route
		existRoute, err := c.ovnLegacyClient.GetStaticRouteList(vpc.Name)
		if err != nil {
			klog.Errorf("failed to get vpc %s static route list, %v", vpc.Name, err)
			return err
		}

		routeNeedDel, routeNeedAdd, err := diffStaticRoute(existRoute, vpc.Spec.StaticRoutes)
		if err != nil {
			klog.Errorf("failed to diff vpc %s static route, %v", vpc.Name, err)
			return err
		}
		for _, item := range routeNeedDel {
			if err = c.ovnLegacyClient.DeleteStaticRoute(item.CIDR, vpc.Name); err != nil {
				klog.Errorf("del vpc %s static route failed, %v", vpc.Name, err)
				return err
			}
		}

		for _, item := range routeNeedAdd {
			if err = c.ovnLegacyClient.AddStaticRoute(convertPolicy(item.Policy), item.CIDR, item.NextHopIP, vpc.Name, util.NormalRouteType); err != nil {
				klog.Errorf("add static route to vpc %s failed, %v", vpc.Name, err)
				return err
			}
		}
		// handle policy route
		existPolicyRoute, err := c.ovnLegacyClient.GetPolicyRouteList(vpc.Name)
		if err != nil {
			klog.Errorf("failed to get vpc %s policy route list, %v", vpc.Name, err)
			return err
		}

		policyRouteNeedDel, policyRouteNeedAdd, err := diffPolicyRoute(existPolicyRoute, vpc.Spec.PolicyRoutes)
		if err != nil {
			klog.Errorf("failed to diff vpc %s policy route, %v", vpc.Name, err)
			return err
		}
		for _, item := range policyRouteNeedDel {
			if err = c.ovnLegacyClient.DeletePolicyRoute(vpc.Name, item.Priority, item.Match); err != nil {
				klog.Errorf("del vpc %s policy route failed, %v", vpc.Name, err)
				return err
			}
		}
		for _, item := range policyRouteNeedAdd {
			externalIDs := map[string]string{"vendor": util.CniTypeName}
			if err = c.ovnLegacyClient.AddPolicyRoute(vpc.Name, item.Priority, item.Match, string(item.Action), item.NextHopIP, externalIDs); err != nil {
				klog.Errorf("add policy route to vpc %s failed, %v", vpc.Name, err)
				return err
			}
		}
	}

	vpc.Status.Router = key
	vpc.Status.Standby = true
	vpc.Status.VpcPeerings = newPeers
	if c.config.EnableLb {
		vpcLb, err := c.addLoadBalancer(key)
		if err != nil {
			return err
		}
		vpc.Status.TcpLoadBalancer = vpcLb.TcpLoadBalancer
		vpc.Status.TcpSessionLoadBalancer = vpcLb.TcpSessLoadBalancer
		vpc.Status.UdpLoadBalancer = vpcLb.UdpLoadBalancer
		vpc.Status.UdpSessionLoadBalancer = vpcLb.UdpSessLoadBalancer
	}
	bytes, err := vpc.Status.Bytes()
	if err != nil {
		return err
	}
	vpc, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(), vpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
	if err != nil {
		return err
	}

	if len(vpc.Annotations) != 0 && strings.ToLower(vpc.Annotations[util.VpcLbAnnotation]) == "on" {
		if err = c.createVpcLb(vpc); err != nil {
			return err
		}
	} else if err = c.deleteVpcLb(vpc); err != nil {
		return err
	}

	return nil
}

func diffPolicyRoute(exist []*ovs.PolicyRoute, target []*kubeovnv1.PolicyRoute) (routeNeedDel []*kubeovnv1.PolicyRoute, routeNeedAdd []*kubeovnv1.PolicyRoute, err error) {
	existV1 := make([]*kubeovnv1.PolicyRoute, 0, len(exist))
	for _, item := range exist {
		existV1 = append(existV1, &kubeovnv1.PolicyRoute{
			Priority:  item.Priority,
			Match:     item.Match,
			Action:    kubeovnv1.PolicyRouteAction(item.Action),
			NextHopIP: item.NextHopIP,
		})
	}

	existRouteMap := make(map[string]*kubeovnv1.PolicyRoute, len(exist))
	for _, item := range existV1 {
		existRouteMap[getPolicyRouteItemKey(item)] = item
	}

	for _, item := range target {
		key := getPolicyRouteItemKey(item)
		if _, ok := existRouteMap[key]; ok {
			delete(existRouteMap, key)
		} else {
			routeNeedAdd = append(routeNeedAdd, item)
		}
	}
	for _, item := range existRouteMap {
		routeNeedDel = append(routeNeedDel, item)
	}
	return routeNeedDel, routeNeedAdd, nil
}

func getPolicyRouteItemKey(item *kubeovnv1.PolicyRoute) (key string) {
	return fmt.Sprintf("%d:%s:%s:%s", item.Priority, item.Match, item.Action, item.NextHopIP)
}

func diffStaticRoute(exist []*ovs.StaticRoute, target []*kubeovnv1.StaticRoute) (routeNeedDel []*kubeovnv1.StaticRoute, routeNeedAdd []*kubeovnv1.StaticRoute, err error) {
	existV1 := make([]*kubeovnv1.StaticRoute, 0, len(exist))
	for _, item := range exist {
		policy := kubeovnv1.PolicyDst
		if item.Policy == ovs.PolicySrcIP {
			policy = kubeovnv1.PolicySrc
		}
		existV1 = append(existV1, &kubeovnv1.StaticRoute{
			Policy:    policy,
			CIDR:      item.CIDR,
			NextHopIP: item.NextHop,
		})
	}

	existRouteMap := make(map[string]*kubeovnv1.StaticRoute, len(exist))
	for _, item := range existV1 {
		existRouteMap[getStaticRouteItemKey(item)] = item
	}

	for _, item := range target {
		key := getStaticRouteItemKey(item)
		if _, ok := existRouteMap[key]; ok {
			delete(existRouteMap, key)
		} else {
			routeNeedAdd = append(routeNeedAdd, item)
		}
	}
	for _, item := range existRouteMap {
		routeNeedDel = append(routeNeedDel, item)
	}
	return
}

func getStaticRouteItemKey(item *kubeovnv1.StaticRoute) (key string) {
	if item.Policy == kubeovnv1.PolicyDst {
		return fmt.Sprintf("dst:%s=>%s", item.CIDR, item.NextHopIP)
	} else {
		return fmt.Sprintf("src:%s=>%s", item.CIDR, item.NextHopIP)
	}
}

func formatVpc(vpc *kubeovnv1.Vpc, c *Controller) error {
	var changed bool

	// default vpc does not support custom route
	if vpc.Status.Default {
		if len(vpc.Spec.StaticRoutes) > 0 {
			changed = true
			vpc.Spec.StaticRoutes = nil
		}
	} else {
		for _, item := range vpc.Spec.StaticRoutes {
			// check policy
			if item.Policy == "" {
				item.Policy = kubeovnv1.PolicyDst
				changed = true
			}
			if item.Policy != kubeovnv1.PolicyDst && item.Policy != kubeovnv1.PolicySrc {
				return fmt.Errorf("unknown policy type: %s", item.Policy)
			}
			// check cidr
			if strings.Contains(item.CIDR, "/") {
				if _, _, err := net.ParseCIDR(item.CIDR); err != nil {
					return fmt.Errorf("invalid cidr %s: %w", item.CIDR, err)
				}
			} else if ip := net.ParseIP(item.CIDR); ip == nil {
				return fmt.Errorf("invalid IP %s", item.CIDR)
			}
			// check next hop ip
			if ip := net.ParseIP(item.NextHopIP); ip == nil {
				return fmt.Errorf("invalid next hop IP %s", item.NextHopIP)
			}
		}

		for _, route := range vpc.Spec.PolicyRoutes {
			if route.Action != kubeovnv1.PolicyRouteActionReroute {
				if route.NextHopIP != "" {
					route.NextHopIP = ""
					changed = true
				}
			} else {
				if ip := net.ParseIP(route.NextHopIP); ip == nil {
					return fmt.Errorf("bad next hop ip: %s", route.NextHopIP)
				}
			}
		}
	}

	if changed {
		if _, err := c.config.KubeOvnClient.KubeovnV1().Vpcs().Update(context.Background(), vpc, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update vpc %s: %v", vpc.Name, err)
			return err
		}
	}

	return nil
}

func convertPolicy(origin kubeovnv1.RoutePolicy) string {
	if origin == kubeovnv1.PolicyDst {
		return ovs.PolicyDstIP
	} else {
		return ovs.PolicySrcIP
	}
}

func (c *Controller) processNextUpdateStatusVpcWorkItem() bool {
	obj, shutdown := c.updateVpcStatusQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateVpcStatusQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateVpcStatusQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateVpcStatus(key); err != nil {
			c.updateVpcStatusQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateVpcStatusQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextAddVpcWorkItem() bool {
	obj, shutdown := c.addOrUpdateVpcQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addOrUpdateVpcQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addOrUpdateVpcQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddOrUpdateVpc(key); err != nil {
			// c.addOrUpdateVpcQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addOrUpdateVpcQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		c.addOrUpdateVpcQueue.AddRateLimited(obj)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteVpcWorkItem() bool {
	obj, shutdown := c.delVpcQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.delVpcQueue.Done(obj)
		var vpc *kubeovnv1.Vpc
		var ok bool
		if vpc, ok = obj.(*kubeovnv1.Vpc); !ok {
			c.delVpcQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelVpc(vpc); err != nil {
			c.delVpcQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s': %s, requeuing", vpc.Name, err.Error())
		}
		c.delVpcQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) getVpcSubnets(vpc *kubeovnv1.Vpc) (subnets []string, defaultSubnet string, err error) {
	subnets = []string{}
	allSubnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return nil, "", err
	}

	for _, subnet := range allSubnets {
		if subnet.Spec.Vpc != vpc.Name || !subnet.DeletionTimestamp.IsZero() || !isOvnSubnet(subnet) {
			continue
		}

		subnets = append(subnets, subnet.Name)
		if subnet.Spec.Default {
			defaultSubnet = subnet.Name
		}
	}
	return
}

// createVpcRouter create router to connect logical switches in vpc
func (c *Controller) createVpcRouter(lr string) error {
	lrs, err := c.ovnLegacyClient.ListLogicalRouter(c.config.EnableExternalVpc)
	if err != nil {
		return err
	}
	klog.Infof("exists routers %v", lrs)
	for _, r := range lrs {
		if lr == r {
			return nil
		}
	}
	return c.ovnLegacyClient.CreateLogicalRouter(lr)
}

// deleteVpcRouter delete router to connect logical switches in vpc
func (c *Controller) deleteVpcRouter(lr string) error {
	return c.ovnLegacyClient.DeleteLogicalRouter(lr)
}
