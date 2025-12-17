package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddVpc(obj any) {
	vpc := obj.(*kubeovnv1.Vpc)
	key := cache.MetaObjectToName(vpc).String()
	if _, ok := vpc.Labels[util.VpcExternalLabel]; !ok {
		klog.V(3).Infof("enqueue add vpc %s", key)
		c.addOrUpdateVpcQueue.Add(key)
	}
}

func vpcBFDPortChanged(oldObj, newObj *kubeovnv1.BFDPort) bool {
	if oldObj == nil && newObj == nil {
		return false
	}
	if oldObj == nil || newObj == nil {
		return true
	}
	return oldObj.Enabled != newObj.Enabled || oldObj.IP != newObj.IP || !reflect.DeepEqual(oldObj.NodeSelector, newObj.NodeSelector)
}

func (c *Controller) enqueueUpdateVpc(oldObj, newObj any) {
	oldVpc := oldObj.(*kubeovnv1.Vpc)
	newVpc := newObj.(*kubeovnv1.Vpc)

	if newVpc.Labels != nil && newVpc.Labels[util.VpcExternalLabel] == "true" {
		return
	}

	if !newVpc.DeletionTimestamp.IsZero() ||
		!slices.Equal(oldVpc.Spec.Namespaces, newVpc.Spec.Namespaces) ||
		!reflect.DeepEqual(oldVpc.Spec.StaticRoutes, newVpc.Spec.StaticRoutes) ||
		!reflect.DeepEqual(oldVpc.Spec.PolicyRoutes, newVpc.Spec.PolicyRoutes) ||
		!reflect.DeepEqual(oldVpc.Spec.VpcPeerings, newVpc.Spec.VpcPeerings) ||
		kubeOvnAnnotationsChanged(oldVpc.Annotations, newVpc.Annotations) ||
		!slices.Equal(oldVpc.Spec.ExtraExternalSubnets, newVpc.Spec.ExtraExternalSubnets) ||
		oldVpc.Spec.EnableExternal != newVpc.Spec.EnableExternal ||
		oldVpc.Spec.EnableBfd != newVpc.Spec.EnableBfd ||
		vpcBFDPortChanged(oldVpc.Spec.BFDPort, newVpc.Spec.BFDPort) ||
		oldVpc.Labels[util.VpcExternalLabel] != newVpc.Labels[util.VpcExternalLabel] ||
		!slices.Equal(oldVpc.Status.Subnets, newVpc.Status.Subnets) {
		// TODO:// label VpcExternalLabel replace with spec enable external

		// recode last policies
		c.vpcLastPoliciesMap.Store(newVpc.Name, convertPolicies(oldVpc.Spec.PolicyRoutes))

		key := cache.MetaObjectToName(newVpc).String()
		klog.Infof("enqueue update vpc %s", key)
		c.addOrUpdateVpcQueue.Add(key)
	}
}

func (c *Controller) enqueueDelVpc(obj any) {
	var vpc *kubeovnv1.Vpc
	switch t := obj.(type) {
	case *kubeovnv1.Vpc:
		vpc = t
	case cache.DeletedFinalStateUnknown:
		v, ok := t.Obj.(*kubeovnv1.Vpc)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		vpc = v
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	if _, ok := vpc.Labels[util.VpcExternalLabel]; !vpc.Status.Default || !ok {
		klog.V(3).Infof("enqueue delete vpc %s", vpc.Name)
		c.delVpcQueue.Add(vpc.DeepCopy())
	}
}

func (c *Controller) handleDelVpc(vpc *kubeovnv1.Vpc) error {
	c.vpcKeyMutex.LockKey(vpc.Name)
	defer func() { _ = c.vpcKeyMutex.UnlockKey(vpc.Name) }()
	klog.Infof("handle delete vpc %s", vpc.Name)

	// should delete vpc subnets first
	var err error
	for _, subnet := range vpc.Status.Subnets {
		if _, err = c.subnetsLister.Get(subnet); err != nil {
			if k8serrors.IsNotFound(err) {
				continue
			}
			err = fmt.Errorf("failed to get subnet %s for vpc %s: %w", subnet, vpc.Name, err)
		} else {
			err = fmt.Errorf("failed to delete vpc %s, please delete subnet %s first", vpc.Name, subnet)
		}
		klog.Error(err)
		return err
	}

	// clean up vpc last policies cached
	c.vpcLastPoliciesMap.Delete(vpc.Name)

	if err := c.deleteVpcLb(vpc); err != nil {
		klog.Error(err)
		return err
	}

	// Delete connection to default external network
	if err := c.deleteVpc2ExternalConnection(vpc.Name); err != nil {
		klog.Errorf("failed to delete default external connection for vpc %s: %v", vpc.Name, err)
		return err
	}

	for _, subnet := range vpc.Status.ExtraExternalSubnets {
		klog.Infof("disconnect external network %s to vpc %s", subnet, vpc.Name)
		if err := c.handleDelVpcRes2ExternalSubnet(vpc.Name, subnet); err != nil {
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

	change := vpc.Status.DefaultLogicalSwitch != defaultSubnet

	vpc.Status.DefaultLogicalSwitch = defaultSubnet
	vpc.Status.Subnets = subnets

	if !vpc.Spec.BFDPort.IsEnabled() && !vpc.Status.BFDPort.IsEmpty() {
		vpc.Status.BFDPort.Clear()
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

	if len(vpc.Status.Subnets) == 0 {
		klog.Infof("vpc %s has no subnets, add to queue", vpc.Name)
		c.addOrUpdateVpcQueue.AddAfter(vpc.Name, 5*time.Second)
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

	cachedVpc, err := c.vpcsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	vpc, err := c.formatVpc(cachedVpc.DeepCopy())
	if err != nil {
		klog.Errorf("failed to format vpc %s: %v", key, err)
		return err
	}

	learnFromARPRequest := vpc.Spec.EnableExternal
	if !learnFromARPRequest {
		for _, subnetName := range vpc.Status.Subnets {
			subnet, err := c.subnetsLister.Get(subnetName)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					continue
				}
				klog.Errorf("failed to get subnet %s for vpc %s: %v", subnetName, key, err)
				return err
			}
			if subnet.Spec.Vlan != "" && subnet.Spec.U2OInterconnection {
				learnFromARPRequest = true
				break
			}
		}
	}

	if err = c.createVpcRouter(key, learnFromARPRequest); err != nil {
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
		externalIDs         = map[string]string{"vendor": util.CniTypeName}
	)

	// only manage static routes which are kube-ovn managed, by filtering for vendor util.CniTypeName
	staticExistedRoutes, err = c.OVNNbClient.ListLogicalRouterStaticRoutes(vpc.Name, nil, nil, "", externalIDs)
	if err != nil {
		klog.Errorf("failed to get vpc %s static route list, %v", vpc.Name, err)
		return err
	}

	// Determine which external gateway switch to use
	// Logic: default subnet exists -> use default; default not exists + ConfigMap specified -> use ConfigMap
	externalGwSwitch, err := c.getConfigDefaultExternalSwitch()
	if err != nil {
		klog.Warningf("failed to get external gateway switch: %v", err)
		externalGwSwitch = c.config.ExternalGatewaySwitch // fallback to default
	}

	var externalSubnet *kubeovnv1.Subnet
	externalSubnetExist := false
	externalSubnetGW := ""
	if c.config.EnableEipSnat {
		externalSubnet, err = c.subnetsLister.Get(externalGwSwitch)
		if err != nil {
			klog.Warningf("enable-eip-snat need external subnet %s to exist: %v", externalGwSwitch, err)
		} else {
			if !externalSubnet.Spec.LogicalGateway {
				// logical gw external subnet can not access external
				externalSubnetExist = true
				externalSubnetGW = externalSubnet.Spec.Gateway
			} else {
				klog.Infof("external subnet %s using logical gw", externalGwSwitch)
			}
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
			c.addOrUpdateVpcQueue.Add(vpc.Name)
			return nil
		}
		gatewayV4, gatewayV6 := util.SplitStringIP(joinSubnet.Spec.Gateway)
		if gatewayV4 != "" {
			for table := range staticRouteMapping {
				staticTargetRoutes = append(
					staticTargetRoutes,
					&kubeovnv1.StaticRoute{
						Policy:     kubeovnv1.PolicyDst,
						CIDR:       "0.0.0.0/0",
						NextHopIP:  gatewayV4,
						RouteTable: table,
					},
				)
			}
		}
		if gatewayV6 != "" {
			for table := range staticRouteMapping {
				staticTargetRoutes = append(
					staticTargetRoutes,
					&kubeovnv1.StaticRoute{
						Policy:     kubeovnv1.PolicyDst,
						CIDR:       "::/0",
						NextHopIP:  gatewayV6,
						RouteTable: table,
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
						err = fmt.Errorf("failed to get external subnet %s", externalGwSwitch)
						klog.Error(err)
						return err
					}
					nextHop = externalSubnet.Spec.Gateway
					if nextHop == "" {
						err := fmt.Errorf("subnet %s has no gateway configuration", externalSubnet.Name)
						klog.Error(err)
						return err
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
				vpc.Name, item.RouteTable, convertPolicy(item.Policy), item.CIDR, &item.BfdID, externalIDs, item.NextHopIP,
			); err != nil {
				klog.Errorf("failed to add bfd static route to vpc %s , %v", vpc.Name, err)
				return err
			}
		} else {
			klog.Infof("vpc %s add static route: %+v", vpc.Name, item)
			if err = c.OVNNbClient.AddLogicalRouterStaticRoute(
				vpc.Name, item.RouteTable, convertPolicy(item.Policy), item.CIDR, nil, externalIDs, item.NextHopIP,
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
	)

	if vpc.Name == c.config.ClusterRouter {
		lastPolicies, _ := c.vpcLastPoliciesMap.Load(vpc.Name)
		policyRouteExisted = reversePolicies(lastPolicies)
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
		if err = c.OVNNbClient.AddLogicalRouterPolicy(vpc.Name, item.Priority, item.Match, string(item.Action), []string{item.NextHopIP}, nil, externalIDs); err != nil {
			klog.Errorf("add policy route to vpc %s failed, %v", vpc.Name, err)
			return err
		}
	}

	vpcSubnets, defaultSubnet, err := c.getVpcSubnets(vpc)
	if err != nil {
		klog.Error(err)
		return err
	}

	vpc.Status.Subnets = vpcSubnets
	vpc.Status.DefaultLogicalSwitch = defaultSubnet
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
	custVpcEnableExternalEcmp := false
	for _, subnet := range subnets {
		if subnet.Spec.Vpc == key {
			c.addOrUpdateSubnetQueue.Add(subnet.Name)
			if vpc.Name != util.DefaultVpc && vpc.Spec.EnableBfd && subnet.Spec.EnableEcmp {
				custVpcEnableExternalEcmp = true
			}
		}
	}

	if vpc.Spec.EnableExternal || vpc.Status.EnableExternal {
		if err = c.handleUpdateVpcExternal(cachedVpc, externalGwSwitch, custVpcEnableExternalEcmp, externalSubnetExist, externalSubnetGW); err != nil {
			klog.Errorf("failed to handle update external subnet for vpc %s, %v", key, err)
			return err
		}
	}

	bfdPortName, bfdPortNodes, err := c.reconcileVpcBfdLRP(vpc)
	if err != nil {
		klog.Error(err)
		return err
	}

	// Get the latest VPC object before updating status to avoid conflicts
	latestVpc, err := c.config.KubeOvnClient.KubeovnV1().Vpcs().Get(context.Background(), key, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get latest vpc %s: %v", key, err)
		return err
	}

	if vpc.Spec.BFDPort == nil || !vpc.Spec.BFDPort.Enabled {
		latestVpc.Status.BFDPort = kubeovnv1.BFDPortStatus{}
	} else {
		latestVpc.Status.BFDPort = kubeovnv1.BFDPortStatus{
			Name:  bfdPortName,
			IP:    vpc.Spec.BFDPort.IP,
			Nodes: bfdPortNodes,
		}
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().
		UpdateStatus(context.Background(), latestVpc, metav1.UpdateOptions{}); err != nil {
		klog.Error(err)
		return err
	}

	return nil
}

func (c *Controller) handleUpdateVpcExternal(vpc *kubeovnv1.Vpc, externalGwSwitch string, custVpcEnableExternalEcmp, defaultExternalSubnetExist bool, externalSubnetGW string) error {
	if c.config.EnableEipSnat && vpc.Name == util.DefaultVpc {
		klog.Infof("external_gw handle ovn default external gw %s", vpc.Name)
		return nil
	}

	if !vpc.Spec.EnableExternal && !vpc.Status.EnableExternal {
		// no need to handle external connection
		return nil
	}

	// handle any vpc external
	if vpc.Spec.EnableExternal && !defaultExternalSubnetExist && vpc.Spec.ExtraExternalSubnets == nil {
		// at least have a external subnet
		err := fmt.Errorf("failed to get external subnet for enable external vpc %s", vpc.Name)
		klog.Error(err)
		return err
	}

	if !vpc.Spec.EnableExternal && vpc.Status.EnableExternal {
		// just del all external subnets connection
		klog.Infof("disconnect default external subnet %s to vpc %s", externalGwSwitch, vpc.Name)
		if err := c.handleDelVpcRes2ExternalSubnet(vpc.Name, externalGwSwitch); err != nil {
			klog.Errorf("failed to delete external subnet %s connection for vpc %s, error %v", externalGwSwitch, vpc.Name, err)
			return err
		}
		for _, subnet := range vpc.Status.ExtraExternalSubnets {
			klog.Infof("disconnect external subnet %s to vpc %s", subnet, vpc.Name)
			if err := c.handleDelVpcRes2ExternalSubnet(vpc.Name, subnet); err != nil {
				klog.Errorf("failed to delete external subnet %s connection for vpc %s, error %v", subnet, vpc.Name, err)
				return err
			}
		}
	}

	if vpc.Spec.EnableExternal {
		if !vpc.Status.EnableExternal {
			// just add external connection
			if vpc.Spec.ExtraExternalSubnets == nil && defaultExternalSubnetExist {
				// only connect default external subnet
				klog.Infof("connect default external subnet %s with vpc %s", externalGwSwitch, vpc.Name)
				if err := c.handleAddVpcExternalSubnet(vpc.Name, externalGwSwitch); err != nil {
					klog.Errorf("failed to add external subnet %s connection for vpc %s, error %v", externalGwSwitch, vpc.Name, err)
					return err
				}
			}

			// only connect provider network vlan external subnet
			for _, subnet := range vpc.Spec.ExtraExternalSubnets {
				klog.Infof("connect external subnet %s with vpc %s", subnet, vpc.Name)
				if err := c.handleAddVpcExternalSubnet(vpc.Name, subnet); err != nil {
					klog.Errorf("failed to add external subnet %s connection for vpc %s, error %v", subnet, vpc.Name, err)
					return err
				}
			}
		}

		// diff to add
		for _, subnet := range vpc.Spec.ExtraExternalSubnets {
			if !slices.Contains(vpc.Status.ExtraExternalSubnets, subnet) {
				klog.Infof("connect external subnet %s with vpc %s", subnet, vpc.Name)
				if err := c.handleAddVpcExternalSubnet(vpc.Name, subnet); err != nil {
					klog.Errorf("failed to add external subnet %s connection for vpc %s, error %v", subnet, vpc.Name, err)
					return err
				}
			}
		}

		// diff to del
		for _, subnet := range vpc.Status.ExtraExternalSubnets {
			if !slices.Contains(vpc.Spec.ExtraExternalSubnets, subnet) {
				klog.Infof("disconnect external subnet %s to vpc %s", subnet, vpc.Name)
				if err := c.handleDelVpcRes2ExternalSubnet(vpc.Name, subnet); err != nil {
					klog.Errorf("failed to delete external subnet %s connection for vpc %s, error %v", subnet, vpc.Name, err)
					return err
				}
			}
		}
	}

	// custom vpc enable bfd
	if vpc.Spec.EnableBfd && vpc.Name != util.DefaultVpc && defaultExternalSubnetExist {
		// create bfd between lrp and physical switch gw
		// bfd status down means current lrp binding chassis node external nic lost external network connectivity
		// should switch lrp to another node
		lrpEipName := fmt.Sprintf("%s-%s", vpc.Name, externalGwSwitch)
		v4ExtGw, _ := util.SplitStringIP(externalSubnetGW)
		// TODO: dualstack
		if _, err := c.OVNNbClient.CreateBFD(lrpEipName, v4ExtGw, c.config.BfdMinRx, c.config.BfdMinTx, c.config.BfdDetectMult, nil); err != nil {
			klog.Error(err)
			return err
		}
		// TODO: support multi external nic
		if custVpcEnableExternalEcmp {
			klog.Infof("remove normal static ecmp route for vpc %s", vpc.Name)
			// auto remove normal type static route, if using ecmp based bfd
			if err := c.reconcileCustomVpcDelNormalStaticRoute(vpc.Name); err != nil {
				klog.Errorf("failed to reconcile del vpc %q normal static route", vpc.Name)
				return err
			}
		}
	}

	if !vpc.Spec.EnableBfd && vpc.Status.EnableBfd {
		lrpEipName := fmt.Sprintf("%s-%s", vpc.Name, externalGwSwitch)
		if err := c.OVNNbClient.DeleteBFDByDstIP(lrpEipName, ""); err != nil {
			klog.Error(err)
			return err
		}
		if err := c.handleDeleteVpcStaticRoute(vpc.Name); err != nil {
			klog.Errorf("failed to delete bfd route for vpc %s, error %v", vpc.Name, err)
			return err
		}
	}

	if err := c.updateVpcExternalStatus(vpc.Name, vpc.Spec.EnableExternal); err != nil {
		klog.Errorf("failed to update vpc external subnets status, %v", err)
		return err
	}
	return nil
}

func (c *Controller) reconcileVpcBfdLRP(vpc *kubeovnv1.Vpc) (string, []string, error) {
	portName := "bfd@" + vpc.Name
	if vpc.Spec.BFDPort == nil || !vpc.Spec.BFDPort.Enabled {
		if err := c.OVNNbClient.DeleteLogicalRouterPort(portName); err != nil {
			err = fmt.Errorf("failed to delete BFD LRP %s: %w", portName, err)
			klog.Error(err)
			return portName, nil, err
		}
		if err := c.OVNNbClient.DeleteHAChassisGroup(portName); err != nil {
			err = fmt.Errorf("failed to delete HA chassis group %s: %w", portName, err)
			klog.Error(err)
			return portName, nil, err
		}
		return portName, nil, nil
	}

	var err error
	chassisCount := 3
	selector := labels.Everything()
	if vpc.Spec.BFDPort.NodeSelector != nil {
		chassisCount = math.MaxInt
		if selector, err = metav1.LabelSelectorAsSelector(vpc.Spec.BFDPort.NodeSelector); err != nil {
			err = fmt.Errorf("failed to parse node selector %q: %w", vpc.Spec.BFDPort.NodeSelector.String(), err)
			klog.Error(err)
			return portName, nil, err
		}
	}

	nodes, err := c.nodesLister.List(selector)
	if err != nil {
		err = fmt.Errorf("failed to list nodes with selector %q: %w", vpc.Spec.BFDPort.NodeSelector, err)
		klog.Error(err)
		return portName, nil, err
	}
	if len(nodes) == 0 {
		err = fmt.Errorf("no nodes found by selector %q", selector.String())
		klog.Error(err)
		return portName, nil, err
	}

	nodeNames := make([]string, 0, len(nodes))
	chassisCount = min(chassisCount, len(nodes))
	chassisNames := make([]string, 0, chassisCount)
	for _, nodes := range nodes[:chassisCount] {
		chassis, err := c.OVNSbClient.GetChassisByHost(nodes.Name)
		if err != nil {
			err = fmt.Errorf("failed to get chassis of node %s: %w", nodes.Name, err)
			klog.Error(err)
			return portName, nil, err
		}
		chassisNames = append(chassisNames, chassis.Name)
		nodeNames = append(nodeNames, nodes.Name)
	}

	networks := strings.Split(vpc.Spec.BFDPort.IP, ",")
	if err = c.OVNNbClient.CreateLogicalRouterPort(vpc.Name, portName, "", networks); err != nil {
		klog.Error(err)
		return portName, nil, err
	}
	if err = c.OVNNbClient.UpdateLogicalRouterPortNetworks(portName, networks); err != nil {
		klog.Error(err)
		return portName, nil, err
	}
	if err = c.OVNNbClient.UpdateLogicalRouterPortOptions(portName, map[string]string{"bfd-only": "true"}); err != nil {
		klog.Error(err)
		return portName, nil, err
	}
	if err = c.OVNNbClient.CreateHAChassisGroup(portName, chassisNames, map[string]string{"lrp": portName}); err != nil {
		klog.Error(err)
		return portName, nil, err
	}
	if err = c.OVNNbClient.SetLogicalRouterPortHAChassisGroup(portName, portName); err != nil {
		klog.Error(err)
		return portName, nil, err
	}

	return portName, nodeNames, nil
}

func (c *Controller) addPolicyRouteToVpc(vpcName string, policy *kubeovnv1.PolicyRoute, externalIDs map[string]string) error {
	var (
		nextHops []string
		err      error
	)

	if policy.NextHopIP != "" {
		nextHops = strings.Split(policy.NextHopIP, ",")
	}

	if err = c.OVNNbClient.AddLogicalRouterPolicy(vpcName, policy.Priority, policy.Match, string(policy.Action), nextHops, nil, externalIDs); err != nil {
		klog.Errorf("add policy route to vpc %s failed, %v", vpcName, err)
		return err
	}
	return nil
}

func buildExternalIDsMapKey(match, action string, priority int) string {
	return fmt.Sprintf("%s-%s-%d", match, action, priority)
}

func (c *Controller) batchAddPolicyRouteToVpc(name string, policies []*kubeovnv1.PolicyRoute, externalIDs map[string]map[string]string) error {
	if len(policies) == 0 {
		return nil
	}
	start := time.Now()
	routerPolicies := make([]*ovnnb.LogicalRouterPolicy, 0, len(policies))
	for _, policy := range policies {
		var nextHops []string
		if policy.NextHopIP != "" {
			nextHops = strings.Split(policy.NextHopIP, ",")
		}
		routerPolicies = append(routerPolicies, &ovnnb.LogicalRouterPolicy{
			Priority:    policy.Priority,
			Nexthops:    nextHops,
			Action:      string(policy.Action),
			Match:       policy.Match,
			ExternalIDs: externalIDs[buildExternalIDsMapKey(policy.Match, string(policy.Action), policy.Priority)],
		})
	}

	if err := c.OVNNbClient.BatchAddLogicalRouterPolicy(name, routerPolicies...); err != nil {
		klog.Errorf("batch add policy route to vpc %s failed, %v", name, err)
		return err
	}
	klog.Infof("take to %v batch add policy route to vpc %s policies %d", time.Since(start), name, len(policies))
	return nil
}

func (c *Controller) deletePolicyRouteFromVpc(vpcName string, priority int, match string) error {
	var (
		vpc, cachedVpc *kubeovnv1.Vpc
		err            error
	)

	if err = c.OVNNbClient.DeleteLogicalRouterPolicy(vpcName, priority, match); err != nil {
		klog.Error(err)
		return err
	}

	cachedVpc, err = c.vpcsLister.Get(vpcName)
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

func (c *Controller) batchDeletePolicyRouteFromVpc(name string, policies []*kubeovnv1.PolicyRoute) error {
	var (
		vpc, cachedVpc *kubeovnv1.Vpc
		err            error
	)

	start := time.Now()
	routerPolicies := make([]*ovnnb.LogicalRouterPolicy, 0, len(policies))
	for _, policy := range policies {
		routerPolicies = append(routerPolicies, &ovnnb.LogicalRouterPolicy{
			Priority: policy.Priority,
			Match:    policy.Match,
		})
	}

	if err = c.OVNNbClient.BatchDeleteLogicalRouterPolicy(name, routerPolicies); err != nil {
		return err
	}
	klog.V(3).Infof("take to %v batch delete policy route from vpc %s policies %d", time.Since(start), name, len(policies))

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
	externalIDs := map[string]string{"vendor": util.CniTypeName}
	if route.BfdID != "" {
		klog.Infof("vpc %s add static ecmp route: %+v", name, route)
		if err := c.OVNNbClient.AddLogicalRouterStaticRoute(
			name, route.RouteTable, convertPolicy(route.Policy), route.CIDR, &route.BfdID, externalIDs, route.NextHopIP,
		); err != nil {
			klog.Errorf("failed to add bfd static route to vpc %s , %v", name, err)
			return err
		}
	} else {
		klog.Infof("vpc %s add static route: %+v", name, route)
		if err := c.OVNNbClient.AddLogicalRouterStaticRoute(
			name, route.RouteTable, convertPolicy(route.Policy), route.CIDR, nil, externalIDs, route.NextHopIP,
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

func (c *Controller) batchDeleteStaticRouteFromVpc(name string, staticRoutes []*kubeovnv1.StaticRoute) error {
	var (
		vpc, cachedVpc *kubeovnv1.Vpc
		err            error
	)
	start := time.Now()
	routeCount := len(staticRoutes)
	delRoutes := make([]*ovnnb.LogicalRouterStaticRoute, 0, routeCount)
	for _, sr := range staticRoutes {
		policyStr := convertPolicy(sr.Policy)
		newRoute := &ovnnb.LogicalRouterStaticRoute{
			RouteTable: sr.RouteTable,
			Nexthop:    sr.NextHopIP,
			Policy:     &policyStr,
			IPPrefix:   sr.CIDR,
		}
		delRoutes = append(delRoutes, newRoute)
	}
	if err = c.OVNNbClient.BatchDeleteLogicalRouterStaticRoute(name, delRoutes); err != nil {
		klog.Errorf("batch del vpc %s static route %d failed, %v", name, routeCount, err)
		return err
	}
	klog.V(3).Infof("take to %v batch delete static route from vpc %s static routes %d", time.Since(start), name, len(delRoutes))

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
	return routeNeedDel, routeNeedAdd, err
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
			return nil, fmt.Errorf("unknown policy type: %q", item.Policy)
		}
		// check cidr
		if strings.Contains(item.CIDR, "/") {
			if _, _, err := net.ParseCIDR(item.CIDR); err != nil {
				return nil, fmt.Errorf("invalid cidr %q: %w", item.CIDR, err)
			}
		} else if ip := net.ParseIP(item.CIDR); ip == nil {
			return nil, fmt.Errorf("invalid ip %q", item.CIDR)
		}
		// check next hop ip
		if ip := net.ParseIP(item.NextHopIP); ip == nil {
			return nil, fmt.Errorf("invalid next hop ip %q", item.NextHopIP)
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
			for ipStr := range strings.SplitSeq(route.NextHopIP, ",") {
				if ip := net.ParseIP(ipStr); ip == nil {
					err := fmt.Errorf("invalid next hop ips: %s", route.NextHopIP)
					klog.Error(err)
					return nil, err
				}
			}
		}
	}

	if vpc.DeletionTimestamp.IsZero() && !slices.Contains(vpc.GetFinalizers(), util.KubeOVNControllerFinalizer) {
		controllerutil.RemoveFinalizer(vpc, util.DepreciatedFinalizerName)
		controllerutil.AddFinalizer(vpc, util.KubeOVNControllerFinalizer)
		changed = true
	}

	if !vpc.DeletionTimestamp.IsZero() && len(vpc.Status.Subnets) == 0 {
		controllerutil.RemoveFinalizer(vpc, util.DepreciatedFinalizerName)
		controllerutil.RemoveFinalizer(vpc, util.KubeOVNControllerFinalizer)
		changed = true
	}

	if changed {
		newVpc, err := c.config.KubeOvnClient.KubeovnV1().Vpcs().Update(context.Background(), vpc, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update vpc %s: %v", vpc.Name, err)
			return nil, err
		}
		return newVpc, nil
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

		if vpc.Name != util.DefaultVpc && vpc.Spec.DefaultSubnet != "" && vpc.Spec.DefaultSubnet == subnet.Name {
			defaultSubnet = vpc.Spec.DefaultSubnet
		}
	}
	sort.Strings(subnets)
	return subnets, defaultSubnet, err
}

// createVpcRouter create router to connect logical switches in vpc
func (c *Controller) createVpcRouter(lr string, learnFromARPRequest bool) error {
	if err := c.OVNNbClient.CreateLogicalRouter(lr); err != nil {
		klog.Errorf("create logical router %s failed: %v", lr, err)
		return err
	}

	vpcRouter, err := c.OVNNbClient.GetLogicalRouter(lr, false)
	if err != nil {
		klog.Errorf("get logical router %s failed: %v", lr, err)
		return err
	}

	lrOptions := map[string]string{
		"mac_binding_age_threshold": "300",
		"dynamic_neigh_routers":     "true",
	}
	if !learnFromARPRequest {
		lrOptions["always_learn_from_arp_request"] = "false"
	}
	if !maps.Equal(vpcRouter.Options, lrOptions) {
		vpcRouter.Options = lrOptions
		if err = c.OVNNbClient.UpdateLogicalRouter(vpcRouter, &vpcRouter.Options); err != nil {
			klog.Errorf("failed to update options of logical router %s: %v", lr, err)
			return err
		}
	}

	return nil
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
	gwNodes, err := c.nodesLister.List(externalGatewayNodeSelector)
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
		err := errors.New("no external gw nodes")
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

func (c *Controller) handleDelVpcRes2ExternalSubnet(key, subnet string) error {
	lspName := fmt.Sprintf("%s-%s", subnet, key)
	lrpName := fmt.Sprintf("%s-%s", key, subnet)
	klog.Infof("delete vpc lrp %s", lrpName)
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
	if err := c.OVNNbClient.DeleteBFDByDstIP(lrpName, ""); err != nil {
		klog.Error(err)
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

func (c *Controller) updateVpcExternalStatus(key string, enableExternal bool) error {
	cachedVpc, err := c.vpcsLister.Get(key)
	if err != nil {
		klog.Errorf("failed to get vpc %s, %v", key, err)
		return err
	}
	vpc := cachedVpc.DeepCopy()
	vpc.Status.EnableExternal = vpc.Spec.EnableExternal
	vpc.Status.EnableBfd = vpc.Spec.EnableBfd

	if enableExternal {
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

// deleteVpc2ExternalConnection deletes VPC connections to external networks
// Deletes both ConfigMap-specified and default connections to ensure complete cleanup
// even if configuration changed during VPC lifecycle
func (c *Controller) deleteVpc2ExternalConnection(vpcName string) error {
	var anyErr error

	// Try to delete ConfigMap-specified connection if exists
	cm, err := c.configMapsLister.ConfigMaps(c.config.ExternalGatewayConfigNS).Get(util.ExternalGatewayConfig)
	if err == nil && cm.Data["external-gw-switch"] != "" {
		configSwitch := cm.Data["external-gw-switch"]
		if err := c.handleDelVpcRes2ExternalSubnet(vpcName, configSwitch); err != nil {
			klog.Errorf("failed to delete ConfigMap-specified connection %s for vpc %s: %v", configSwitch, vpcName, err)
			anyErr = err
		}
	}

	// Always try to delete default connection
	if err := c.handleDelVpcRes2ExternalSubnet(vpcName, c.config.ExternalGatewaySwitch); err != nil {
		klog.Errorf("failed to delete default connection %s for vpc %s: %v", c.config.ExternalGatewaySwitch, vpcName, err)
		if anyErr == nil {
			anyErr = err
		}
	}

	return anyErr
}
