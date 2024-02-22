package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"slices"
	"sort"
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

	if !newVpc.DeletionTimestamp.IsZero() ||
		!reflect.DeepEqual(oldVpc.Spec.Namespaces, newVpc.Spec.Namespaces) ||
		!reflect.DeepEqual(oldVpc.Spec.StaticRoutes, newVpc.Spec.StaticRoutes) ||
		!reflect.DeepEqual(oldVpc.Spec.PolicyRoutes, newVpc.Spec.PolicyRoutes) ||
		!reflect.DeepEqual(oldVpc.Spec.VpcPeerings, newVpc.Spec.VpcPeerings) ||
		!reflect.DeepEqual(oldVpc.Annotations, newVpc.Annotations) ||
		!reflect.DeepEqual(oldVpc.Spec.ExtraExternalSubnets, newVpc.Spec.ExtraExternalSubnets) ||
		oldVpc.Spec.EnableExternal != newVpc.Spec.EnableExternal ||
		oldVpc.Spec.EnableBfd != newVpc.Spec.EnableBfd ||
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
		newVpc.Annotations[util.VpcLastPolicies] = convertPolicies(oldVpc.Spec.PolicyRoutes)

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
		klog.Error(err)
		return err
	}

	if err := c.handleDelVpcExternalSubnet(vpc.Name, c.config.ExternalGatewaySwitch); err != nil {
		klog.Errorf("failed to delete external connection for vpc %s, error %v", vpc.Name, err)
		return err
	}

	for _, subnet := range vpc.Status.ExtraExternalSubnets {
		klog.Infof("disconnect external network %s to vpc %s", subnet, vpc.Name)
		if err := c.handleDelVpcExternalSubnet(vpc.Name, subnet); err != nil {
			klog.Error(err)
			return err
		}
	}

	if err := c.deleteVpcRouter(vpc.Status.Router); err != nil {
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

	if vpc, err = c.formatVpc(vpc); err != nil {
		klog.Errorf("failed to format vpc %s: %v", key, err)
		return err
	}
	if err = c.createVpcRouter(key); err != nil {
		klog.Errorf("failed to create vpc router for vpc %s: %v", key, err)
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
		if !slices.Contains(newPeers, oldPeer) {
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

	var externalSubnet *kubeovnv1.Subnet
	externalSubnetExist := false
	if c.config.EnableEipSnat {
		externalSubnet, err = c.subnetsLister.Get(c.config.ExternalGatewaySwitch)
		if err != nil {
			klog.Warningf("enable-eip-snat need external subnet %s to be exist: %v", c.config.ExternalGatewaySwitch, err)
		} else {
			externalSubnetExist = true
		}
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
				klog.Errorf("failed to get node switch subnet %s: %v", c.config.NodeSwitch, err)
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
					if !externalSubnetExist {
						err = fmt.Errorf("failed to get external subnet %s", c.config.ExternalGatewaySwitch)
						klog.Error(err)
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
		policyRouteExisted = reversePolicies(vpc.Annotations[util.VpcLastPolicies])
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
			policyRouteLogical, err = c.OVNNbClient.ListLogicalRouterPolicies(vpc.Name, -1, nil, true)
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
			klog.Error(err)
			return err
		}
	} else if err = c.deleteVpcLb(vpc); err != nil {
		klog.Error(err)
		return err
	}

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Error(err)
		return err
	}
	custVpcEnableExternalMultiHopEcmp := false
	for _, subnet := range subnets {
		if subnet.Spec.Vpc == key {
			c.addOrUpdateSubnetQueue.Add(subnet.Name)
			if vpc.Name != util.DefaultVpc && vpc.Spec.EnableBfd && subnet.Spec.EnableEcmp {
				custVpcEnableExternalMultiHopEcmp = true
			}
		}
	}

	if vpc.Name != util.DefaultVpc {
		if cachedVpc.Spec.EnableExternal {
			if !externalSubnetExist {
				err = fmt.Errorf("failed to get external subnet %s", c.config.ExternalGatewaySwitch)
				klog.Error(err)
				return err
			}
			if externalSubnet.Spec.LogicalGateway {
				klog.Infof("no need to hanlde external connection for logical gw external subnet %s", c.config.ExternalGatewaySwitch)
				return nil
			}
			if !cachedVpc.Status.EnableExternal {
				// connect vpc to default external
				klog.Infof("connect external network with vpc %s", vpc.Name)
				if err := c.handleAddVpcExternalSubnet(key, c.config.ExternalGatewaySwitch); err != nil {
					klog.Errorf("failed to add default external connection for vpc %s, error %v", key, err)
					return err
				}
			}
			if vpc.Spec.EnableBfd {
				// create bfd between lrp and physical switch gw
				// bfd status down means current lrp binding chassis node external nic lost external network connectivity
				// should switch lrp to another node
				lrpEipName := fmt.Sprintf("%s-%s", key, c.config.ExternalGatewaySwitch)
				v4ExtGw, _ := util.SplitStringIP(externalSubnet.Spec.Gateway)
				// TODO: dualstack
				if _, err := c.OVNNbClient.CreateBFD(lrpEipName, v4ExtGw, c.config.BfdMinRx, c.config.BfdMinTx, c.config.BfdDetectMult); err != nil {
					klog.Error(err)
					return err
				}
				// TODO: support multi external nic
				if custVpcEnableExternalMultiHopEcmp {
					klog.Infof("remove normal static ecmp route for vpc %s", vpc.Name)
					// auto remove normal type static route, if using ecmp based bfd
					if err := c.reconcileCustomVpcDelNormalStaticRoute(vpc.Name); err != nil {
						klog.Errorf("failed to reconcile del vpc %q normal static route", vpc.Name)
						return err
					}
				}
			}
			if cachedVpc.Spec.ExtraExternalSubnets != nil {
				sort.Strings(vpc.Spec.ExtraExternalSubnets)
			}
			// add external subnets only in spec and delete external subnets only in status
			if !reflect.DeepEqual(vpc.Spec.ExtraExternalSubnets, vpc.Status.ExtraExternalSubnets) {
				for _, subnetStatus := range cachedVpc.Status.ExtraExternalSubnets {
					if !slices.Contains(cachedVpc.Spec.ExtraExternalSubnets, subnetStatus) {
						klog.Infof("delete external subnet %s connection for vpc %s", subnetStatus, vpc.Name)
						if err := c.handleDelVpcExternalSubnet(vpc.Name, subnetStatus); err != nil {
							klog.Errorf("failed to delete external subnet %s connection for vpc %s, error %v", subnetStatus, vpc.Name, err)
							return err
						}
					}
				}
				for _, subnetSpec := range cachedVpc.Spec.ExtraExternalSubnets {
					if !slices.Contains(cachedVpc.Status.ExtraExternalSubnets, subnetSpec) {
						klog.Infof("connect external subnet %s with vpc %s", subnetSpec, vpc.Name)
						if err := c.handleAddVpcExternalSubnet(key, subnetSpec); err != nil {
							klog.Errorf("failed to add external subnet %s connection for vpc %s, error %v", subnetSpec, key, err)
							return err
						}
					}
				}
				if err := c.updateVpcAddExternalStatus(key, true); err != nil {
					klog.Errorf("failed to update additional external subnets status, %v", err)
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
			// disconnect vpc to default external
			if err := c.handleDelVpcExternalSubnet(key, c.config.ExternalGatewaySwitch); err != nil {
				klog.Errorf("failed to delete external connection for vpc %s, error %v", key, err)
				return err
			}
		}

		if cachedVpc.Status.ExtraExternalSubnets != nil && !cachedVpc.Spec.EnableExternal {
			// disconnect vpc to extra external subnets
			for _, subnet := range cachedVpc.Status.ExtraExternalSubnets {
				klog.Infof("disconnect external network %s to vpc %s", subnet, vpc.Name)
				if err := c.handleDelVpcExternalSubnet(key, subnet); err != nil {
					klog.Error(err)
					return err
				}
			}
			if err := c.updateVpcAddExternalStatus(key, false); err != nil {
				klog.Errorf("failed to update additional external subnets status, %v", err)
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
		klog.Error(err)
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
		policyStr string
		err       error
	)

	policyStr = convertPolicy(policy)
	if err = c.OVNNbClient.DeleteLogicalRouterStaticRoute(name, &table, &policyStr, cidr, nextHop); err != nil {
		klog.Errorf("del vpc %s static route failed, %v", name, err)
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

func (c *Controller) formatVpc(vpc *kubeovnv1.Vpc) (*kubeovnv1.Vpc, error) {
	var changed bool
	for _, item := range vpc.Spec.StaticRoutes {
		// check policy
		if item.Policy == "" {
			item.Policy = kubeovnv1.PolicyDst
			changed = true
		}
		if item.Policy != kubeovnv1.PolicyDst && item.Policy != kubeovnv1.PolicySrc {
			return nil, fmt.Errorf("unknown policy type: %s", item.Policy)
		}
		// check cidr
		if strings.Contains(item.CIDR, "/") {
			if _, _, err := net.ParseCIDR(item.CIDR); err != nil {
				return nil, fmt.Errorf("invalid cidr %s: %w", item.CIDR, err)
			}
		} else if ip := net.ParseIP(item.CIDR); ip == nil {
			return nil, fmt.Errorf("invalid ip %s", item.CIDR)
		}
		// check next hop ip
		if ip := net.ParseIP(item.NextHopIP); ip == nil {
			return nil, fmt.Errorf("invalid next hop ip %s", item.NextHopIP)
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
					return nil, err
				}
			}
		}
	}

	if changed {
		newVpc, err := c.config.KubeOvnClient.KubeovnV1().Vpcs().Update(context.Background(), vpc, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update vpc %s: %v", vpc.Name, err)
			return nil, err
		}
		return newVpc, err
	}

	return vpc, nil
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

func (c *Controller) handleAddVpcExternalSubnet(key, subnet string) error {
	cachedSubnet, err := c.subnetsLister.Get(subnet)
	if err != nil {
		klog.Error(err)
		return err
	}
	lrpEipName := fmt.Sprintf("%s-%s", key, subnet)
	cachedEip, err := c.ovnEipsLister.Get(lrpEipName)
	var needCreateEip bool
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
		needCreateEip = true
	}
	var v4ip, v6ip, mac string
	klog.V(3).Infof("create vpc lrp eip %s", lrpEipName)
	if needCreateEip {
		if v4ip, v6ip, mac, err = c.acquireIPAddress(subnet, lrpEipName, lrpEipName); err != nil {
			klog.Errorf("failed to acquire ip address for lrp eip %s, %v", lrpEipName, err)
			return err
		}
		if err := c.createOrUpdateOvnEipCR(lrpEipName, subnet, v4ip, v6ip, mac, util.OvnEipTypeLRP); err != nil {
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
	chassises := []string{}
	sel, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: map[string]string{util.ExGatewayLabel: "true"}})
	gwNodes, err := c.nodesLister.List(sel)
	if err != nil {
		klog.Errorf("failed to list external gw nodes, %v", err)
		return err
	}
	for _, gwNode := range gwNodes {
		annoChassisName := gwNode.Annotations[util.ChassisAnnotation]
		if annoChassisName == "" {
			err := fmt.Errorf("node %s has no chassis annotation, kube-ovn-cni not ready", gwNode.Name)
			klog.Error(err)
			return err
		}
		klog.Infof("get node %s chassis: %s", gwNode.Name, annoChassisName)
		chassis, err := c.OVNSbClient.GetChassis(annoChassisName, false)
		if err != nil {
			klog.Errorf("failed to get node %s chassis: %s, %v", gwNode.Name, annoChassisName, err)
			return err
		}
		chassises = append(chassises, chassis.Name)
	}

	if len(chassises) == 0 {
		err := fmt.Errorf("no external gw nodes")
		klog.Error(err)
		return err
	}

	v4ipCidr, err := util.GetIPAddrWithMask(v4ip, cachedSubnet.Spec.CIDRBlock)
	if err != nil {
		klog.Error(err)
		return err
	}
	lspName := fmt.Sprintf("%s-%s", subnet, key)
	lrpName := fmt.Sprintf("%s-%s", key, subnet)

	if err := c.OVNNbClient.CreateLogicalPatchPort(subnet, key, lspName, lrpName, v4ipCidr, mac, chassises...); err != nil {
		klog.Errorf("failed to connect router '%s' to external: %v", key, err)
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
	if subnet == c.config.ExternalGatewaySwitch {
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

func (c *Controller) handleDelVpcExternalSubnet(key, subnet string) error {
	lspName := fmt.Sprintf("%s-%s", subnet, key)
	lrpName := fmt.Sprintf("%s-%s", key, subnet)
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
	if subnet == c.config.ExternalGatewaySwitch {
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
	}
	return nil
}

func (c *Controller) patchVpcBfdStatus(key string) error {
	cachedVpc, err := c.vpcsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get vpc %s, %v", key, err)
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

func (c *Controller) updateVpcAddExternalStatus(key string, addExternalStatus bool) error {
	cachedVpc, err := c.vpcsLister.Get(key)
	if err != nil {
		klog.Errorf("failed to get vpc %s, %v", key, err)
		return err
	}
	vpc := cachedVpc.DeepCopy()
	if addExternalStatus && vpc.Spec.ExtraExternalSubnets != nil {
		sort.Strings(vpc.Spec.ExtraExternalSubnets)
		vpc.Status.ExtraExternalSubnets = vpc.Spec.ExtraExternalSubnets
	} else {
		vpc.Status.ExtraExternalSubnets = nil
	}
	bytes, err := vpc.Status.Bytes()
	if err != nil {
		klog.Errorf("failed to get vpc bytes, %v", err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(),
		vpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
		klog.Errorf("failed to patch vpc %s, %v", key, err)
		return err
	}

	return nil
}
