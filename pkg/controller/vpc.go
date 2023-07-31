package controller

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddVpc(obj interface{}) {

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
	c.vpcKeyMutex.LockKey(vpc.Name)
	defer func() { _ = c.vpcKeyMutex.UnlockKey(vpc.Name) }()
	klog.Infof("handle delete vpc %s", vpc.Name)

	if err := c.deleteVpcLb(vpc); err != nil {
		return err
	}

	if err := c.handleDelVpcExternal(vpc.Name); err != nil {
		klog.Errorf("failed to delete external connection for vpc %s, error %v", vpc.Name, err)
		return err
	}

	err := c.deleteVpcRouter(vpc.Status.Router)
	if err != nil {
		return err
	}
	return nil
}

func (c *Controller) handleUpdateVpcStatus(key string) error {
	c.vpcKeyMutex.LockKey(key)
	defer func() { _ = c.vpcKeyMutex.UnlockKey(key) }()
	klog.Infof("handle status update for vpc %s", key)

	cachedVpc, err := c.vpcsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	vpc := cachedVpc.DeepCopy()

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

type VpcLoadBalancer struct {
	TcpLoadBalancer      string
	TcpSessLoadBalancer  string
	UdpLoadBalancer      string
	UdpSessLoadBalancer  string
	SctpLoadBalancer     string
	SctpSessLoadBalancer string
}

func (c *Controller) GenVpcLoadBalancer(vpcKey string) *VpcLoadBalancer {
	if vpcKey == c.config.ClusterRouter || vpcKey == "" {
		return &VpcLoadBalancer{
			TcpLoadBalancer:      c.config.ClusterTcpLoadBalancer,
			TcpSessLoadBalancer:  c.config.ClusterTcpSessionLoadBalancer,
			UdpLoadBalancer:      c.config.ClusterUdpLoadBalancer,
			UdpSessLoadBalancer:  c.config.ClusterUdpSessionLoadBalancer,
			SctpLoadBalancer:     c.config.ClusterSctpLoadBalancer,
			SctpSessLoadBalancer: c.config.ClusterSctpSessionLoadBalancer,
		}
	} else {
		return &VpcLoadBalancer{
			TcpLoadBalancer:      fmt.Sprintf("vpc-%s-tcp-load", vpcKey),
			TcpSessLoadBalancer:  fmt.Sprintf("vpc-%s-tcp-sess-load", vpcKey),
			UdpLoadBalancer:      fmt.Sprintf("vpc-%s-udp-load", vpcKey),
			UdpSessLoadBalancer:  fmt.Sprintf("vpc-%s-udp-sess-load", vpcKey),
			SctpLoadBalancer:     fmt.Sprintf("vpc-%s-sctp-load", vpcKey),
			SctpSessLoadBalancer: fmt.Sprintf("vpc-%s-sctp-sess-load", vpcKey),
		}
	}
}

func (c *Controller) addLoadBalancer(vpc string) (*VpcLoadBalancer, error) {
	vpcLbConfig := c.GenVpcLoadBalancer(vpc)
	if err := c.initLB(vpcLbConfig.TcpLoadBalancer, string(v1.ProtocolTCP), false); err != nil {
		return nil, err
	}
	if err := c.initLB(vpcLbConfig.TcpSessLoadBalancer, string(v1.ProtocolTCP), true); err != nil {
		return nil, err
	}
	if err := c.initLB(vpcLbConfig.UdpLoadBalancer, string(v1.ProtocolUDP), false); err != nil {
		return nil, err
	}
	if err := c.initLB(vpcLbConfig.UdpSessLoadBalancer, string(v1.ProtocolUDP), true); err != nil {
		return nil, err
	}
	if err := c.initLB(vpcLbConfig.SctpLoadBalancer, string(v1.ProtocolSCTP), false); err != nil {
		return nil, err
	}
	if err := c.initLB(vpcLbConfig.SctpSessLoadBalancer, string(v1.ProtocolSCTP), true); err != nil {
		return nil, err
	}

	return vpcLbConfig, nil
}

func (c *Controller) handleAddOrUpdateVpc(key string) error {
	c.vpcKeyMutex.LockKey(key)
	defer func() { _ = c.vpcKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add/update vpc %s", key)

	// get latest vpc info
	cachedVpc, err := c.vpcsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	vpc := cachedVpc.DeepCopy()

	if err = formatVpc(vpc, c); err != nil {
		klog.Errorf("failed to format vpc %s: %v", key, err)
		return err
	}
	if err = c.createVpcRouter(key); err != nil {
		return err
	}

	var newPeers []string
	for _, peering := range vpc.Spec.VpcPeerings {
		if err = util.CheckCidrs(peering.LocalConnectIP); err != nil {
			klog.Errorf("invalid cidr %s", peering.LocalConnectIP)
			return err
		}

		newPeers = append(newPeers, peering.RemoteVpc)
		if err := c.ovnClient.CreatePeerRouterPort(vpc.Name, peering.RemoteVpc, peering.LocalConnectIP); err != nil {
			klog.Errorf("create peer router port for vpc %s, %v", vpc.Name, err)
			return err
		}
	}
	for _, oldPeer := range vpc.Status.VpcPeerings {
		if !util.ContainsString(newPeers, oldPeer) {
			if err = c.ovnClient.DeleteLogicalRouterPort(fmt.Sprintf("%s-%s", vpc.Name, oldPeer)); err != nil {
				klog.Errorf("delete peer router port for vpc %s, %v", vpc.Name, err)
				return err
			}
		}
	}

	// handle static route
	existRoute, err := c.ovnClient.ListLogicalRouterStaticRoutes(vpc.Name, nil, nil, "", nil)
	if err != nil {
		klog.Errorf("failed to get vpc %s static route list, %v", vpc.Name, err)
		return err
	}

	rtbs := c.getRouteTablesByVpc(vpc)
	targetRoutes := vpc.Spec.StaticRoutes
	if vpc.Name == c.config.ClusterRouter {
		if _, ok := rtbs[util.MainRouteTable]; !ok {
			rtbs[util.MainRouteTable] = nil
		}

		joinSubnet, err := c.subnetsLister.Get(c.config.NodeSwitch)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Error("failed to get node switch subnet %s: %v", c.config.NodeSwitch)
				return err
			}
		}
		gatewayV4, gatewayV6 := util.SplitStringIP(joinSubnet.Spec.Gateway)
		if gatewayV4 != "" {
			for rtb := range rtbs {
				targetRoutes = append(targetRoutes, &kubeovnv1.StaticRoute{
					Policy:     kubeovnv1.PolicyDst,
					CIDR:       "0.0.0.0/0",
					NextHopIP:  gatewayV4,
					RouteTable: rtb,
				})
			}
		}
		if gatewayV6 != "" {
			for rtb := range rtbs {
				targetRoutes = append(targetRoutes, &kubeovnv1.StaticRoute{
					Policy:     kubeovnv1.PolicyDst,
					CIDR:       "::/0",
					NextHopIP:  gatewayV6,
					RouteTable: rtb,
				})
			}
		}

		if c.config.EnableEipSnat {
			cm, err := c.configMapsLister.ConfigMaps(c.config.ExternalGatewayConfigNS).Get(util.ExternalGatewayConfig)
			if err == nil {
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
						return fmt.Errorf("no available gateway address")
					}
				}
				if strings.Contains(nextHop, "/") {
					nextHop = strings.Split(nextHop, "/")[0]
				}

				lr, err := c.ovnClient.GetLogicalRouter(vpc.Name, false)
				if err != nil {
					klog.Errorf("failed to get logical router %s: %v", vpc.Name, err)
					return err
				}

				for _, nat := range lr.Nat {
					info, err := c.ovnClient.GetNATByUUID(nat)
					if err != nil {
						klog.Errorf("failed to get nat ip info for vpc %s, %v", vpc.Name, err)
						return err
					}
					if info.LogicalIP != "" {
						for rtb := range rtbs {
							targetRoutes = append(targetRoutes, &kubeovnv1.StaticRoute{
								Policy:     kubeovnv1.PolicySrc,
								CIDR:       info.LogicalIP,
								NextHopIP:  nextHop,
								RouteTable: rtb,
							})
						}
					}
				}
			}
		}
	}

	routeNeedDel, routeNeedAdd, err := diffStaticRoute(existRoute, targetRoutes)
	if err != nil {
		klog.Errorf("failed to diff vpc %s static route, %v", vpc.Name, err)
		return err
	}
	for _, item := range routeNeedDel {
		klog.Infof("vpc %s del static route: %v", vpc.Name, item)
		policy := convertPolicy(item.Policy)
		if err = c.ovnClient.DeleteLogicalRouterStaticRoute(vpc.Name, &item.RouteTable, &policy, item.CIDR, item.NextHopIP); err != nil {
			klog.Errorf("del vpc %s static route failed, %v", vpc.Name, err)
			return err
		}
	}

	for _, item := range routeNeedAdd {
		klog.Infof("vpc %s add static route: %+v", vpc.Name, item)
		if item.BfdId != "" {
			if err = c.ovnClient.AddLogicalRouterStaticRoute(
				vpc.Name, item.RouteTable, convertPolicy(item.Policy), item.CIDR, &item.BfdId, item.NextHopIP,
			); err != nil {
				klog.Errorf("failed to add bfd static route to vpc %s , %v", vpc.Name, err)
				return err
			}
		} else {
			if err = c.ovnClient.AddLogicalRouterStaticRoute(
				vpc.Name, item.RouteTable, convertPolicy(item.Policy), item.CIDR, nil, item.NextHopIP,
			); err != nil {
				klog.Errorf("failed to add normal static route to vpc %s , %v", vpc.Name, err)
				return err
			}
		}
	}

	if vpc.Name != c.config.ClusterRouter && vpc.Spec.PolicyRoutes == nil {
		// do not clean default vpc policy routes
		if err = c.ovnClient.ClearLogicalRouterPolicy(vpc.Name); err != nil {
			klog.Errorf("clean all vpc %s policy route failed, %v", vpc.Name, err)
			return err
		}
	}

	if vpc.Spec.PolicyRoutes != nil {
		// diff update vpc policy route
		policyList, err := c.ovnClient.ListLogicalRouterPolicies(vpc.Name, -1, nil)
		if err != nil {
			klog.Errorf("failed to get vpc %s policy route list, %v", vpc.Name, err)
			return err
		}
		policyRouteNeedDel, policyRouteNeedAdd, err := diffPolicyRoute(policyList, vpc.Spec.PolicyRoutes)
		if err != nil {
			klog.Errorf("failed to diff vpc %s policy route, %v", vpc.Name, err)
			return err
		}
		for _, item := range policyRouteNeedDel {
			klog.Infof("delete policy route for router: %s, priority: %d, match %s", vpc.Name, item.Priority, item.Match)
			if err = c.ovnClient.DeleteLogicalRouterPolicy(vpc.Name, item.Priority, item.Match); err != nil {
				klog.Errorf("del vpc %s policy route failed, %v", vpc.Name, err)
				return err
			}
		}
		for _, item := range policyRouteNeedAdd {
			externalIDs := map[string]string{"vendor": util.CniTypeName}
			klog.Infof("add policy route for router: %s, match %s, action %s, nexthop %s, externalID %v", c.config.ClusterRouter, item.Match, string(item.Action), item.NextHopIP, externalIDs)
			if err = c.ovnClient.AddLogicalRouterPolicy(vpc.Name, item.Priority, item.Match, string(item.Action), []string{item.NextHopIP}, externalIDs); err != nil {
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
		vpc.Status.SctpLoadBalancer = vpcLb.SctpLoadBalancer
		vpc.Status.SctpSessionLoadBalancer = vpcLb.SctpSessLoadBalancer
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

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return err
	}

	for _, subnet := range subnets {
		if subnet.Spec.Vpc == key {
			c.addOrUpdateSubnetQueue.Add(subnet.Name)
		}
	}
	if vpc.Name != util.DefaultVpc {
		if cachedVpc.Spec.EnableExternal {
			if !cachedVpc.Status.EnableExternal {
				// connect vpc to external
				klog.Infof("connect external network with vpc %s", vpc.Name)
				if err := c.handleAddVpcExternal(key); err != nil {
					klog.Errorf("failed to add external connection for vpc %s, error %v", key, err)
					return err
				}
			}
			if vpc.Spec.EnableBfd {
				klog.Infof("remove normal static ecmp route for vpc %s", vpc.Name)
				// auto remove normal type static route, if using ecmp based bfd
				if err := c.reconcileCustomVpcDelNormalStaticRoute(vpc.Name); err != nil {
					klog.Errorf("failed to reconcile del vpc %q normal static route", vpc.Name)
					return err
				}
			}
			if !vpc.Spec.EnableBfd {
				// auto add normal type static route, if not use ecmp based bfd
				klog.Infof("add normal external static route for enable external vpc %s", vpc.Name)
				if err := c.reconcileCustomVpcAddNormalStaticRoute(vpc.Name); err != nil {
					klog.Errorf("failed to reconcile vpc %q bfd static route", vpc.Name)
					return err
				}
			}
		}

		if !cachedVpc.Spec.EnableBfd && cachedVpc.Status.EnableBfd {
			lrpEipName := fmt.Sprintf("%s-%s", key, c.config.ExternalGatewaySwitch)
			if err := c.ovnClient.DeleteBFD(lrpEipName, ""); err != nil {
				klog.Error(err)
				return err
			}
			if err := c.handleDeleteVpcStaticRoute(key); err != nil {
				klog.Errorf("failed to delete bfd route for vpc %s, error %v", key, err)
				return err
			}
		}

		if !cachedVpc.Spec.EnableExternal && cachedVpc.Status.EnableExternal {
			// disconnect vpc to external
			if err := c.handleDelVpcExternal(key); err != nil {
				klog.Errorf("failed to delete external connection for vpc %s, error %v", key, err)
				return err
			}
		}
	}

	return nil
}

func diffPolicyRoute(exist []*ovnnb.LogicalRouterPolicy, target []*kubeovnv1.PolicyRoute) (routeNeedDel, routeNeedAdd []*kubeovnv1.PolicyRoute, err error) {
	existV1 := make([]*kubeovnv1.PolicyRoute, 0, len(exist))
	for _, item := range exist {
		existV1 = append(existV1, &kubeovnv1.PolicyRoute{
			Priority: item.Priority,
			Match:    item.Match,
			Action:   kubeovnv1.PolicyRouteAction(item.Action),
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

func diffStaticRoute(exist []*ovnnb.LogicalRouterStaticRoute, target []*kubeovnv1.StaticRoute) (routeNeedDel, routeNeedAdd []*kubeovnv1.StaticRoute, err error) {
	existRouteMap := make(map[string]*kubeovnv1.StaticRoute, len(exist))
	for _, item := range exist {
		policy := kubeovnv1.PolicyDst
		if item.Policy != nil && *item.Policy == ovnnb.LogicalRouterStaticRoutePolicySrcIP {
			policy = kubeovnv1.PolicySrc
		}
		route := &kubeovnv1.StaticRoute{
			Policy:     policy,
			CIDR:       item.IPPrefix,
			NextHopIP:  item.Nexthop,
			RouteTable: item.RouteTable,
			ECMPMode:   util.StaticRouteBfdEcmp,
		}
		if item.BFD != nil {
			route.BfdId = *item.BFD
		}
		existRouteMap[getStaticRouteItemKey(route)] = route
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
		return fmt.Sprintf("%s:dst:%s=>%s", item.RouteTable, item.CIDR, item.NextHopIP)
	} else {
		return fmt.Sprintf("%s:src:%s=>%s", item.RouteTable, item.CIDR, item.NextHopIP)
	}
}

func formatVpc(vpc *kubeovnv1.Vpc, c *Controller) error {
	var changed bool
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
			return fmt.Errorf("invalid ip %s", item.CIDR)
		}
		// check next hop ip
		if ip := net.ParseIP(item.NextHopIP); ip == nil {
			return fmt.Errorf("invalid next hop ip %s", item.NextHopIP)
		}
	}

	for _, route := range vpc.Spec.PolicyRoutes {
		if route.Action != kubeovnv1.PolicyRouteActionReroute {
			if route.NextHopIP != "" {
				route.NextHopIP = ""
				changed = true
			}
		} else {
			// ecmp policy route may reroute to multiple next hop ips
			for _, ipStr := range strings.Split(route.NextHopIP, ",") {
				if ip := net.ParseIP(ipStr); ip == nil {
					err := fmt.Errorf("invalid next hop ips: %s", route.NextHopIP)
					klog.Error(err)
					return err
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
		return ovnnb.LogicalRouterStaticRoutePolicyDstIP
	}
	return ovnnb.LogicalRouterStaticRoutePolicySrcIP
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
	return c.ovnClient.CreateLogicalRouter(lr)
}

// deleteVpcRouter delete router to connect logical switches in vpc
func (c *Controller) deleteVpcRouter(lr string) error {
	return c.ovnClient.DeleteLogicalRouter(lr)
}

func (c *Controller) handleAddVpcExternal(key string) error {
	cachedSubnet, err := c.subnetsLister.Get(c.config.ExternalGatewaySwitch)
	if err != nil {
		return err
	}
	lrpEipName := fmt.Sprintf("%s-%s", key, c.config.ExternalGatewaySwitch)
	cachedEip, err := c.ovnEipsLister.Get(lrpEipName)
	var needCreateEip bool
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
		needCreateEip = true
	}
	var v4ip, v6ip, mac string
	klog.V(3).Infof("create vpc lrp eip %s", lrpEipName)
	if needCreateEip {
		if v4ip, v6ip, mac, err = c.acquireIpAddress(c.config.ExternalGatewaySwitch, lrpEipName, lrpEipName); err != nil {
			klog.Errorf("failed to acquire ip address for lrp eip %s, %v", lrpEipName, err)
			return err
		}
		if err := c.createOrUpdateCrdOvnEip(lrpEipName, c.config.ExternalGatewaySwitch, v4ip, v6ip, mac, util.LrpUsingEip); err != nil {
			klog.Errorf("failed to create ovn eip for lrp %s: %v", lrpEipName, err)
			return err
		}
	} else {
		v4ip = cachedEip.Spec.V4Ip
		mac = cachedEip.Spec.MacAddress
	}
	if v4ip == "" || mac == "" {
		err := fmt.Errorf("lrp '%s' ip or mac should not be empty", lrpEipName)
		klog.Error(err)
		return err
	}
	// init lrp gw chassis group
	cm, err := c.configMapsLister.ConfigMaps(c.config.ExternalGatewayConfigNS).Get(util.ExternalGatewayConfig)
	if err != nil {
		klog.Errorf("failed to get ovn-external-gw-config, %v", err)
		return err
	}
	if cm.Data["enable-external-gw"] == "false" {
		err := fmt.Errorf("gw chassis included in config map %s not exist", util.ExternalGatewayConfig)
		// TODO://
		// gw chassis controlled by a crd is a better way
		// gw chassis crd may generated by this cm, or by user manually, or by node ext gw type oeip
		klog.Error(err)
		return err
	}
	chassises, err := c.getGatewayChassis(cm.Data)
	if err != nil {
		klog.Errorf("failed to get gateway chassis, %v", err)
		return err
	}

	v4ipCidr := util.GetIpAddrWithMask(v4ip, cachedSubnet.Spec.CIDRBlock)
	lspName := fmt.Sprintf("%s-%s", c.config.ExternalGatewaySwitch, key)
	lrpName := fmt.Sprintf("%s-%s", key, c.config.ExternalGatewaySwitch)

	if err := c.ovnClient.CreateLogicalPatchPort(c.config.ExternalGatewaySwitch, key, lspName, lrpName, v4ipCidr, mac, chassises...); err != nil {
		klog.Errorf("failed to connect router '%s' to external: %v", key, err)
		return err
	}

	cachedVpc, err := c.vpcsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error("failed to get vpc %s, %v", key, err)
		return err
	}
	vpc := cachedVpc.DeepCopy()
	vpc.Status.EnableExternal = cachedVpc.Spec.EnableExternal
	bytes, err := vpc.Status.Bytes()
	if err != nil {
		klog.Errorf("failed to marshal vpc status: %v", err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(),
		vpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
		err := fmt.Errorf("failed to patch vpc %s status, %v", vpc.Name, err)
		klog.Error(err)
		return err
	}
	if _, err = c.ovnEipsLister.Get(lrpEipName); err != nil {
		err := fmt.Errorf("failed to get ovn eip %s, %v", lrpEipName, err)
		klog.Error(err)
		return err
	}
	return nil
}

func (c *Controller) handleDeleteVpcStaticRoute(key string) error {
	vpc, err := c.vpcsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get vpc %s, %v", key, err)
		return err
	}
	needUpdate := false
	newStaticRoutes := make([]*kubeovnv1.StaticRoute, 0, len(vpc.Spec.StaticRoutes))
	for _, route := range vpc.Spec.StaticRoutes {
		if route.ECMPMode != util.StaticRouteBfdEcmp {
			newStaticRoutes = append(newStaticRoutes, route)
			needUpdate = true
		}
	}
	// keep non ecmp bfd routes
	vpc.Spec.StaticRoutes = newStaticRoutes
	if needUpdate {
		if _, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Update(context.Background(), vpc, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update vpc spec static route %s, %v", vpc.Name, err)
			return err
		}
	}
	if err = c.patchVpcBfdStatus(vpc.Name); err != nil {
		klog.Errorf("failed to patch vpc %s, %v", vpc.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelVpcExternal(key string) error {
	lspName := fmt.Sprintf("%s-%s", c.config.ExternalGatewaySwitch, key)
	lrpName := fmt.Sprintf("%s-%s", key, c.config.ExternalGatewaySwitch)
	klog.V(3).Infof("delete vpc lrp %s", lrpName)
	if err := c.ovnClient.RemoveLogicalPatchPort(lspName, lrpName); err != nil {
		klog.Errorf("failed to disconnect router '%s' to external, %v", key, err)
		return err
	}

	if err := c.config.KubeOvnClient.KubeovnV1().OvnEips().Delete(context.Background(), lrpName, metav1.DeleteOptions{}); err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to delete ovn eip %s, %v", lrpName, err)
			return err
		}
	}
	if err := c.ovnClient.DeleteBFD(lrpName, ""); err != nil {
		klog.Error(err)
		return err
	}
	cachedVpc, err := c.vpcsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get vpc %s, %v", key, err)
		return err
	}
	vpc := cachedVpc.DeepCopy()
	vpc.Status.EnableExternal = cachedVpc.Spec.EnableExternal
	vpc.Status.EnableBfd = cachedVpc.Spec.EnableBfd
	bytes, err := vpc.Status.Bytes()
	if err != nil {
		klog.Errorf("failed to marshal vpc status: %v", err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(),
		vpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to patch vpc %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) patchVpcBfdStatus(key string) error {
	cachedVpc, err := c.vpcsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error("failed to get vpc %s, %v", key, err)
		return err
	}
	vpc := cachedVpc.DeepCopy()
	if vpc.Status.EnableBfd != vpc.Spec.EnableBfd {
		vpc.Status.EnableBfd = cachedVpc.Spec.EnableBfd
		bytes, err := vpc.Status.Bytes()
		if err != nil {
			klog.Errorf("failed to marshal vpc status: %v", err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(),
			vpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) getRouteTablesByVpc(vpc *kubeovnv1.Vpc) map[string][]*kubeovnv1.StaticRoute {
	rtbs := make(map[string][]*kubeovnv1.StaticRoute)
	for _, route := range vpc.Spec.StaticRoutes {
		rtbs[route.RouteTable] = append(rtbs[route.RouteTable], route)
	}
	return rtbs
}
