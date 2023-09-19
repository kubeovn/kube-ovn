package controller

import (
	"context"
	"encoding/json"
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
	var (
		key string
		err error
	)

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

func (c *Controller) enqueueUpdateVpc(oldObj, newObj interface{}) {
	oldVpc := oldObj.(*kubeovnv1.Vpc)
	newVpc := newObj.(*kubeovnv1.Vpc)

	if newVpc.DeletionTimestamp.IsZero() ||
		!reflect.DeepEqual(oldVpc.Spec.Namespaces, newVpc.Spec.Namespaces) ||
		!reflect.DeepEqual(oldVpc.Spec.StaticRoutes, newVpc.Spec.StaticRoutes) ||
		!reflect.DeepEqual(oldVpc.Spec.PolicyRoutes, newVpc.Spec.PolicyRoutes) ||
		!reflect.DeepEqual(oldVpc.Spec.VpcPeerings, newVpc.Spec.VpcPeerings) ||
		!reflect.DeepEqual(oldVpc.Annotations, newVpc.Annotations) ||
		oldVpc.Labels[util.VpcExternalLabel] != newVpc.Labels[util.VpcExternalLabel] {
		// TODO:// label VpcExternalLabel replace with spec enable external
		var (
			key string
			err error
		)

		if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
			utilruntime.HandleError(err)
			return
		}
		klog.Infof("enqueue update vpc %s", key)

		if newVpc.Annotations == nil {
			newVpc.Annotations = make(map[string]string)
		}
		newVpc.Annotations["ovn.kubernetes.io/last_policies"] = convertPolicies(oldVpc.Spec.PolicyRoutes)

		c.addOrUpdateVpcQueue.Add(key)
	}
}

func (c *Controller) enqueueDelVpc(obj interface{}) {
	var (
		key string
		err error
	)

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
		klog.Error(err)
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
		klog.Error(err)
		return err
	}
	vpc := cachedVpc.DeepCopy()

	subnets, defaultSubnet, err := c.getVpcSubnets(vpc)
	if err != nil {
		klog.Error(err)
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
		klog.Error(err)
		return err
	}

	vpc, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(), vpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
	if err != nil {
		klog.Error(err)
		return err
	}
	if change {
		for _, ns := range vpc.Spec.Namespaces {
			c.addNamespaceQueue.Add(ns)
		}
	}

	natGws, err := c.vpcNatGatewayLister.List(labels.Everything())
	if err != nil {
		klog.Error(err)
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
	TCPLoadBalancer      string
	TCPSessLoadBalancer  string
	UDPLoadBalancer      string
	UDPSessLoadBalancer  string
	SctpLoadBalancer     string
	SctpSessLoadBalancer string
}

func (c *Controller) GenVpcLoadBalancer(vpcKey string) *VpcLoadBalancer {
	if vpcKey == c.config.ClusterRouter || vpcKey == "" {
		return &VpcLoadBalancer{
			TCPLoadBalancer:      c.config.ClusterTCPLoadBalancer,
			TCPSessLoadBalancer:  c.config.ClusterTCPSessionLoadBalancer,
			UDPLoadBalancer:      c.config.ClusterUDPLoadBalancer,
			UDPSessLoadBalancer:  c.config.ClusterUDPSessionLoadBalancer,
			SctpLoadBalancer:     c.config.ClusterSctpLoadBalancer,
			SctpSessLoadBalancer: c.config.ClusterSctpSessionLoadBalancer,
		}
	}
	return &VpcLoadBalancer{
		TCPLoadBalancer:      fmt.Sprintf("vpc-%s-tcp-load", vpcKey),
		TCPSessLoadBalancer:  fmt.Sprintf("vpc-%s-tcp-sess-load", vpcKey),
		UDPLoadBalancer:      fmt.Sprintf("vpc-%s-udp-load", vpcKey),
		UDPSessLoadBalancer:  fmt.Sprintf("vpc-%s-udp-sess-load", vpcKey),
		SctpLoadBalancer:     fmt.Sprintf("vpc-%s-sctp-load", vpcKey),
		SctpSessLoadBalancer: fmt.Sprintf("vpc-%s-sctp-sess-load", vpcKey),
	}
}

func (c *Controller) addLoadBalancer(vpc string) (*VpcLoadBalancer, error) {
	vpcLbConfig := c.GenVpcLoadBalancer(vpc)
	if err := c.initLB(vpcLbConfig.TCPLoadBalancer, string(v1.ProtocolTCP), false); err != nil {
		return nil, err
	}
	if err := c.initLB(vpcLbConfig.TCPSessLoadBalancer, string(v1.ProtocolTCP), true); err != nil {
		return nil, err
	}
	if err := c.initLB(vpcLbConfig.UDPLoadBalancer, string(v1.ProtocolUDP), false); err != nil {
		return nil, err
	}
	if err := c.initLB(vpcLbConfig.UDPSessLoadBalancer, string(v1.ProtocolUDP), true); err != nil {
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
	var (
		vpc, cachedVpc *kubeovnv1.Vpc
		err            error
	)

	cachedVpc, err = c.vpcsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	vpc = cachedVpc.DeepCopy()

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
		if err := c.OVNNbClient.CreatePeerRouterPort(vpc.Name, peering.RemoteVpc, peering.LocalConnectIP); err != nil {
			klog.Errorf("create peer router port for vpc %s, %v", vpc.Name, err)
			return err
		}
	}
	for _, oldPeer := range vpc.Status.VpcPeerings {
		if !util.ContainsString(newPeers, oldPeer) {
			if err = c.OVNNbClient.DeleteLogicalRouterPort(fmt.Sprintf("%s-%s", vpc.Name, oldPeer)); err != nil {
				klog.Errorf("delete peer router port for vpc %s, %v", vpc.Name, err)
				return err
			}
		}
	}

	// handle static route
	var (
		staticExistedRoutes []*ovnnb.LogicalRouterStaticRoute
		staticTargetRoutes  []*kubeovnv1.StaticRoute
		staticRouteMapping  map[string][]*kubeovnv1.StaticRoute
	)

	staticExistedRoutes, err = c.OVNNbClient.ListLogicalRouterStaticRoutes(vpc.Name, nil, nil, "", nil)
	if err != nil {
		klog.Errorf("failed to get vpc %s static route list, %v", vpc.Name, err)
		return err
	}

	staticRouteMapping = c.getRouteTablesByVpc(vpc)
	staticTargetRoutes = vpc.Spec.StaticRoutes

	if vpc.Name == c.config.ClusterRouter {
		if _, ok := staticRouteMapping[util.MainRouteTable]; !ok {
			staticRouteMapping[util.MainRouteTable] = nil
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
			for tabele := range staticRouteMapping {
				staticTargetRoutes = append(
					staticTargetRoutes,
					&kubeovnv1.StaticRoute{
						Policy:     kubeovnv1.PolicyDst,
						CIDR:       "0.0.0.0/0",
						NextHopIP:  gatewayV4,
						RouteTable: tabele,
					},
				)
			}
		}
		if gatewayV6 != "" {
			for tabele := range staticRouteMapping {
				staticTargetRoutes = append(
					staticTargetRoutes,
					&kubeovnv1.StaticRoute{
						Policy:     kubeovnv1.PolicyDst,
						CIDR:       "::/0",
						NextHopIP:  gatewayV6,
						RouteTable: tabele,
					},
				)
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

				lr, err := c.OVNNbClient.GetLogicalRouter(vpc.Name, false)
				if err != nil {
					klog.Errorf("failed to get logical router %s: %v", vpc.Name, err)
					return err
				}

				for _, nat := range lr.Nat {
					info, err := c.OVNNbClient.GetNATByUUID(nat)
					if err != nil {
						klog.Errorf("failed to get nat ip info for vpc %s, %v", vpc.Name, err)
						return err
					}
					if info.LogicalIP != "" {
						for table := range staticRouteMapping {
							staticTargetRoutes = append(
								staticTargetRoutes,
								&kubeovnv1.StaticRoute{
									Policy:     kubeovnv1.PolicySrc,
									CIDR:       info.LogicalIP,
									NextHopIP:  nextHop,
									RouteTable: table,
								},
							)
						}
					}
				}
			}
		}
	}

	routeNeedDel, routeNeedAdd, err := diffStaticRoute(staticExistedRoutes, staticTargetRoutes)
	if err != nil {
		klog.Errorf("failed to diff vpc %s static route, %v", vpc.Name, err)
		return err
	}

	for _, item := range routeNeedDel {
		klog.Infof("vpc %s del static route: %+v", vpc.Name, item)
		policy := convertPolicy(item.Policy)
		if err = c.OVNNbClient.DeleteLogicalRouterStaticRoute(vpc.Name, &item.RouteTable, &policy, item.CIDR, item.NextHopIP); err != nil {
			klog.Errorf("del vpc %s static route failed, %v", vpc.Name, err)
			return err
		}
	}

	for _, item := range routeNeedAdd {
		if item.BfdID != "" {
			klog.Infof("vpc %s add static ecmp route: %+v", vpc.Name, item)
			if err = c.OVNNbClient.AddLogicalRouterStaticRoute(
				vpc.Name, item.RouteTable, convertPolicy(item.Policy), item.CIDR, &item.BfdID, item.NextHopIP,
			); err != nil {
				klog.Errorf("failed to add bfd static route to vpc %s , %v", vpc.Name, err)
				return err
			}
		} else {
			klog.Infof("vpc %s add static route: %+v", vpc.Name, item)
			if err = c.OVNNbClient.AddLogicalRouterStaticRoute(
				vpc.Name, item.RouteTable, convertPolicy(item.Policy), item.CIDR, nil, item.NextHopIP,
			); err != nil {
				klog.Errorf("failed to add normal static route to vpc %s , %v", vpc.Name, err)
				return err
			}
		}
	}

	// handle policy route
	var (
		policyRouteExisted, policyRouteNeedDel, policyRouteNeedAdd []*kubeovnv1.PolicyRoute
		policyRouteLogical                                         []*ovnnb.LogicalRouterPolicy
		externalIDs                                                = map[string]string{"vendor": util.CniTypeName}
	)

	if vpc.Name == c.config.ClusterRouter {
		policyRouteExisted = reversePolicies(vpc.Annotations["ovn.kubernetes.io/last_policies"])
		// diff list
		policyRouteNeedDel, policyRouteNeedAdd = diffPolicyRouteWithExisted(policyRouteExisted, vpc.Spec.PolicyRoutes)
	} else {
		if vpc.Spec.PolicyRoutes == nil {
			// do not clean default vpc policy routes
			if err = c.OVNNbClient.ClearLogicalRouterPolicy(vpc.Name); err != nil {
				klog.Errorf("clean all vpc %s policy route failed, %v", vpc.Name, err)
				return err
			}
		} else {
			policyRouteLogical, err = c.OVNNbClient.ListLogicalRouterPolicies(vpc.Name, -1, nil)
			if err != nil {
				klog.Errorf("failed to get vpc %s policy route list, %v", vpc.Name, err)
				return err
			}
			// diff vpc policy route
			policyRouteNeedDel, policyRouteNeedAdd = diffPolicyRouteWithLogical(policyRouteLogical, vpc.Spec.PolicyRoutes)
		}
	}
	// delete policies non-exist
	for _, item := range policyRouteNeedDel {
		klog.Infof("delete policy route for router: %s, priority: %d, match %s", vpc.Name, item.Priority, item.Match)
		if err = c.OVNNbClient.DeleteLogicalRouterPolicy(vpc.Name, item.Priority, item.Match); err != nil {
			klog.Errorf("del vpc %s policy route failed, %v", vpc.Name, err)
			return err
		}
	}
	// add new policies
	for _, item := range policyRouteNeedAdd {
		klog.Infof("add policy route for router: %s, match %s, action %s, nexthop %s, externalID %v", c.config.ClusterRouter, item.Match, string(item.Action), item.NextHopIP, externalIDs)
		if err = c.OVNNbClient.AddLogicalRouterPolicy(vpc.Name, item.Priority, item.Match, string(item.Action), []string{item.NextHopIP}, externalIDs); err != nil {
			klog.Errorf("add policy route to vpc %s failed, %v", vpc.Name, err)
			return err
		}
	}

	vpc.Status.Router = key
	vpc.Status.Standby = true
	vpc.Status.VpcPeerings = newPeers
	if c.config.EnableLb {
		vpcLb, err := c.addLoadBalancer(key)
		if err != nil {
			klog.Error(err)
			return err
		}
		vpc.Status.TCPLoadBalancer = vpcLb.TCPLoadBalancer
		vpc.Status.TCPSessionLoadBalancer = vpcLb.TCPSessLoadBalancer
		vpc.Status.UDPLoadBalancer = vpcLb.UDPLoadBalancer
		vpc.Status.UDPSessionLoadBalancer = vpcLb.UDPSessLoadBalancer
		vpc.Status.SctpLoadBalancer = vpcLb.SctpLoadBalancer
		vpc.Status.SctpSessionLoadBalancer = vpcLb.SctpSessLoadBalancer
	}
	bytes, err := vpc.Status.Bytes()
	if err != nil {
		klog.Error(err)
		return err
	}
	vpc, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(), vpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
	if err != nil {
		klog.Error(err)
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
		klog.Error(err)
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
			if err := c.OVNNbClient.DeleteBFD(lrpEipName, ""); err != nil {
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

func (c *Controller) addPolicyRouteToVpc(name string, policy *kubeovnv1.PolicyRoute, externalIDs map[string]string) error {
	var (
		nextHops []string
		err      error
	)

	if policy.NextHopIP != "" {
		nextHops = strings.Split(policy.NextHopIP, ",")
	}

	if err = c.OVNNbClient.AddLogicalRouterPolicy(name, policy.Priority, policy.Match, string(policy.Action), nextHops, externalIDs); err != nil {
		klog.Errorf("add policy route to vpc %s failed, %v", name, err)
		return err
	}
	return nil
}

func (c *Controller) deletePolicyRouteFromVpc(name string, priority int, match string) error {
	var (
		vpc, cachedVpc *kubeovnv1.Vpc
		err            error
	)

	if err = c.OVNNbClient.DeleteLogicalRouterPolicy(name, priority, match); err != nil {
		return err
	}

	cachedVpc, err = c.vpcsLister.Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	vpc = cachedVpc.DeepCopy()
	// make sure custom policies not be deleted
	_, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Update(context.Background(), vpc, metav1.UpdateOptions{})
	if err != nil {
		klog.Error(err)
		return err
	}
	return nil
}

func (c *Controller) addStaticRouteToVpc(name string, route *kubeovnv1.StaticRoute) error {
	if route.BfdID != "" {
		klog.Infof("vpc %s add static ecmp route: %+v", name, route)
		if err := c.OVNNbClient.AddLogicalRouterStaticRoute(
			name, route.RouteTable, convertPolicy(route.Policy), route.CIDR, &route.BfdID, route.NextHopIP,
		); err != nil {
			klog.Errorf("failed to add bfd static route to vpc %s , %v", name, err)
			return err
		}
	} else {
		klog.Infof("vpc %s add static route: %+v", name, route)
		if err := c.OVNNbClient.AddLogicalRouterStaticRoute(
			name, route.RouteTable, convertPolicy(route.Policy), route.CIDR, nil, route.NextHopIP,
		); err != nil {
			klog.Errorf("failed to add normal static route to vpc %s , %v", name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) deleteStaticRouteFromVpc(name, table, cidr, nextHop string, policy kubeovnv1.RoutePolicy) error {
	var (
		vpc, cachedVpc *kubeovnv1.Vpc
		policyStr      string
		err            error
	)

	policyStr = convertPolicy(policy)
	if err = c.OVNNbClient.DeleteLogicalRouterStaticRoute(name, &table, &policyStr, cidr, nextHop); err != nil {
		klog.Errorf("del vpc %s static route failed, %v", name, err)
		return err
	}

	cachedVpc, err = c.vpcsLister.Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	vpc = cachedVpc.DeepCopy()
	// make sure custom policies not be deleted
	_, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Update(context.Background(), vpc, metav1.UpdateOptions{})
	if err != nil {
		klog.Error(err)
		return err
	}
	return nil
}

func diffPolicyRouteWithExisted(exists, target []*kubeovnv1.PolicyRoute) ([]*kubeovnv1.PolicyRoute, []*kubeovnv1.PolicyRoute) {
	var (
		dels, adds []*kubeovnv1.PolicyRoute
		existsMap  map[string]*kubeovnv1.PolicyRoute
		key        string
		ok         bool
	)

	existsMap = make(map[string]*kubeovnv1.PolicyRoute, len(exists))
	for _, item := range exists {
		existsMap[getPolicyRouteItemKey(item)] = item
	}
	// load policies to add
	for _, item := range target {
		key = getPolicyRouteItemKey(item)

		if _, ok = existsMap[key]; ok {
			delete(existsMap, key)
		} else {
			adds = append(adds, item)
		}
	}
	// load policies to delete
	for _, item := range existsMap {
		dels = append(dels, item)
	}
	return dels, adds
}

func diffPolicyRouteWithLogical(exists []*ovnnb.LogicalRouterPolicy, target []*kubeovnv1.PolicyRoute) ([]*kubeovnv1.PolicyRoute, []*kubeovnv1.PolicyRoute) {
	var (
		dels, adds []*kubeovnv1.PolicyRoute
		existsMap  map[string]*kubeovnv1.PolicyRoute
		key        string
		ok         bool
	)
	existsMap = make(map[string]*kubeovnv1.PolicyRoute, len(exists))

	for _, item := range exists {
		policy := &kubeovnv1.PolicyRoute{
			Priority: item.Priority,
			Match:    item.Match,
			Action:   kubeovnv1.PolicyRouteAction(item.Action),
		}
		existsMap[getPolicyRouteItemKey(policy)] = policy
	}

	for _, item := range target {
		key = getPolicyRouteItemKey(item)

		if _, ok = existsMap[key]; ok {
			delete(existsMap, key)
		} else {
			adds = append(adds, item)
		}
	}

	for _, item := range existsMap {
		dels = append(dels, item)
	}
	return dels, adds
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
			route.BfdID = *item.BFD
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

func getStaticRouteItemKey(item *kubeovnv1.StaticRoute) string {
	var key string
	if item.Policy == kubeovnv1.PolicyDst {
		key = fmt.Sprintf("%s:dst:%s=>%s", item.RouteTable, item.CIDR, item.NextHopIP)
	} else {
		key = fmt.Sprintf("%s:src:%s=>%s", item.RouteTable, item.CIDR, item.NextHopIP)
	}
	return key
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

func convertPolicies(list []*kubeovnv1.PolicyRoute) string {
	if list == nil {
		return ""
	}

	var (
		res []byte
		err error
	)

	if res, err = json.Marshal(list); err != nil {
		klog.Errorf("failed to serialize policy routes %v , reason : %v", list, err)
		return ""
	}
	return string(res)
}

func reversePolicies(origin string) []*kubeovnv1.PolicyRoute {
	if origin == "" {
		return nil
	}

	var (
		list []*kubeovnv1.PolicyRoute
		err  error
	)

	if err = json.Unmarshal([]byte(origin), &list); err != nil {
		klog.Errorf("failed to deserialize policy routes %v , reason : %v", list, err)
		return nil
	}
	return list
}

func convertPolicy(origin kubeovnv1.RoutePolicy) string {
	if origin == kubeovnv1.PolicyDst {
		return ovnnb.LogicalRouterStaticRoutePolicyDstIP
	}
	return ovnnb.LogicalRouterStaticRoutePolicySrcIP
}

func reversePolicy(origin ovnnb.LogicalRouterStaticRoutePolicy) kubeovnv1.RoutePolicy {
	if origin == ovnnb.LogicalRouterStaticRoutePolicyDstIP {
		return kubeovnv1.PolicyDst
	}
	return kubeovnv1.PolicySrc
}

func (c *Controller) processNextUpdateStatusVpcWorkItem() bool {
	obj, shutdown := c.updateVpcStatusQueue.Get()
	if shutdown {
		return false
	}

	if err := func(obj interface{}) error {
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
	}(obj); err != nil {
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

	if err := func(obj interface{}) error {
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
	}(obj); err != nil {
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

	if err := func(obj interface{}) error {
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
	}(obj); err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) getVpcSubnets(vpc *kubeovnv1.Vpc) (subnets []string, defaultSubnet string, err error) {
	subnets = []string{}
	allSubnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Error(err)
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
	return c.OVNNbClient.CreateLogicalRouter(lr)
}

// deleteVpcRouter delete router to connect logical switches in vpc
func (c *Controller) deleteVpcRouter(lr string) error {
	return c.OVNNbClient.DeleteLogicalRouter(lr)
}

func (c *Controller) handleAddVpcExternal(key string) error {
	cachedSubnet, err := c.subnetsLister.Get(c.config.ExternalGatewaySwitch)
	if err != nil {
		klog.Error(err)
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
		if v4ip, v6ip, mac, err = c.acquireIPAddress(c.config.ExternalGatewaySwitch, lrpEipName, lrpEipName); err != nil {
			klog.Errorf("failed to acquire ip address for lrp eip %s, %v", lrpEipName, err)
			return err
		}
		if err := c.createOrUpdateCrdOvnEip(lrpEipName, c.config.ExternalGatewaySwitch, v4ip, v6ip, mac, util.Lrp); err != nil {
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

	v4ipCidr := util.GetIPAddrWithMask(v4ip, cachedSubnet.Spec.CIDRBlock)
	lspName := fmt.Sprintf("%s-%s", c.config.ExternalGatewaySwitch, key)
	lrpName := fmt.Sprintf("%s-%s", key, c.config.ExternalGatewaySwitch)

	if err := c.OVNNbClient.CreateLogicalPatchPort(c.config.ExternalGatewaySwitch, key, lspName, lrpName, v4ipCidr, mac, chassises...); err != nil {
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
	if err := c.OVNNbClient.RemoveLogicalPatchPort(lspName, lrpName); err != nil {
		klog.Errorf("failed to disconnect router '%s' to external, %v", key, err)
		return err
	}

	if err := c.config.KubeOvnClient.KubeovnV1().OvnEips().Delete(context.Background(), lrpName, metav1.DeleteOptions{}); err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to delete ovn eip %s, %v", lrpName, err)
			return err
		}
	}
	if err := c.OVNNbClient.DeleteBFD(lrpName, ""); err != nil {
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

	if cachedVpc.Status.EnableBfd != cachedVpc.Spec.EnableBfd {
		status := cachedVpc.Status.DeepCopy()
		status.EnableExternal = cachedVpc.Spec.EnableExternal
		status.EnableBfd = cachedVpc.Spec.EnableBfd
		bytes, err := status.Bytes()
		if err != nil {
			klog.Errorf("failed to marshal vpc status: %v", err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(),
			cachedVpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
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
