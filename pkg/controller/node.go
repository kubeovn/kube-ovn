package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	goping "github.com/prometheus-community/pro-bing"
	"github.com/scylladb/go-set/strset"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type ErrChassisNotFound struct {
	Chassis string
	Node    string
}

func (e *ErrChassisNotFound) Error() string {
	return fmt.Sprintf("chassis %s not found for node %s", e.Chassis, e.Node)
}

func (c *Controller) enqueueAddNode(obj any) {
	key := cache.MetaObjectToName(obj.(*v1.Node)).String()
	klog.V(3).Infof("enqueue add node %s", key)
	c.addNodeQueue.Add(key)
}

func nodeReady(node *v1.Node) bool {
	var ready, networkUnavailable bool
	for _, c := range node.Status.Conditions {
		switch c.Type {
		case v1.NodeReady:
			ready = c.Status == v1.ConditionTrue
		case v1.NodeNetworkUnavailable:
			networkUnavailable = c.Status == v1.ConditionTrue
		}
	}
	return ready && !networkUnavailable
}

func kubeOvnAnnotationsChanged(oldAnnotations, newAnnotations map[string]string) bool {
	filterKubeOvnAnnotations := func(annotations map[string]string) map[string]string {
		filtered := make(map[string]string)
		for key, value := range annotations {
			if strings.Contains(key, ".kubernetes.io/") {
				filtered[key] = value
			}
		}
		return filtered
	}

	return !maps.Equal(filterKubeOvnAnnotations(oldAnnotations), filterKubeOvnAnnotations(newAnnotations))
}

func (c *Controller) enqueueUpdateNode(oldObj, newObj any) {
	oldNode := oldObj.(*v1.Node)
	newNode := newObj.(*v1.Node)

	if nodeReady(oldNode) != nodeReady(newNode) ||
		kubeOvnAnnotationsChanged(oldNode.Annotations, newNode.Annotations) ||
		!reflect.DeepEqual(oldNode.Labels, newNode.Labels) {
		key := cache.MetaObjectToName(newNode).String()
		if len(newNode.Annotations) == 0 || newNode.Annotations[util.AllocatedAnnotation] != "true" {
			klog.V(3).Infof("enqueue add node %s", key)
			c.addNodeQueue.Add(key)
		} else {
			klog.V(3).Infof("enqueue update node %s", key)
			c.updateNodeQueue.Add(key)
		}
	}
}

func (c *Controller) enqueueDeleteNode(obj any) {
	var node *v1.Node
	switch t := obj.(type) {
	case *v1.Node:
		node = t
	case cache.DeletedFinalStateUnknown:
		n, ok := t.Obj.(*v1.Node)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		node = n
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	key := cache.MetaObjectToName(node).String()
	klog.V(3).Infof("enqueue delete node %s", key)
	c.deletingNodeObjMap.Store(key, node)
	c.deleteNodeQueue.Add(key)
}

func nodeUnderlayAddressSetName(node string, af int) string {
	return fmt.Sprintf("node_%s_underlay_v%d", strings.ReplaceAll(node, "-", "_"), af)
}

func (c *Controller) handleAddNode(key string) error {
	c.nodeKeyMutex.LockKey(key)
	defer func() { _ = c.nodeKeyMutex.UnlockKey(key) }()

	cachedNode, err := c.nodesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get node %s: %v", key, err)
		return err
	}
	node := cachedNode.DeepCopy()
	klog.Infof("handle add node %s", node.Name)

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return err
	}

	nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*node)
	for _, subnet := range subnets {
		if subnet.Spec.Vpc != c.config.ClusterRouter {
			continue
		}

		v4, v6 := util.SplitStringIP(subnet.Spec.CIDRBlock)
		if subnet.Spec.Vlan == "" && (util.CIDRContainIP(v4, nodeIPv4) || util.CIDRContainIP(v6, nodeIPv6)) {
			msg := fmt.Sprintf("internal IP address of node %s is in CIDR of subnet %s, this may result in network issues", node.Name, subnet.Name)
			klog.Warning(msg)
			c.recorder.Eventf(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: node.Name, UID: types.UID(node.Name)}}, v1.EventTypeWarning, "NodeAddressConflictWithSubnet", msg)
			break
		}
	}

	if err = c.handleNodeAnnotationsForProviderNetworks(node); err != nil {
		klog.Errorf("failed to handle annotations of node %s for provider networks: %v", node.Name, err)
		return err
	}

	subnet, err := c.subnetsLister.Get(c.config.NodeSwitch)
	if err != nil {
		klog.Errorf("failed to get node subnet: %v", err)
		return err
	}

	var v4IP, v6IP, mac string

	portName := util.NodeLspName(key)
	if node.Annotations[util.AllocatedAnnotation] == "true" && node.Annotations[util.IPAddressAnnotation] != "" && node.Annotations[util.MacAddressAnnotation] != "" {
		macStr := node.Annotations[util.MacAddressAnnotation]
		v4IP, v6IP, mac, err = c.ipam.GetStaticAddress(portName, portName, node.Annotations[util.IPAddressAnnotation],
			&macStr, node.Annotations[util.LogicalSwitchAnnotation], true)
		if err != nil {
			klog.Errorf("failed to alloc static ip addrs for node %v: %v", node.Name, err)
			return err
		}
	} else {
		v4IP, v6IP, mac, err = c.ipam.GetRandomAddress(portName, portName, nil, c.config.NodeSwitch, "", nil, true)
		if err != nil {
			klog.Errorf("failed to alloc random ip addrs for node %v: %v", node.Name, err)
			return err
		}

		// Clean up potentially existing logical switch ports to avoid leftover issues from previous failed configurations
		if err := c.OVNNbClient.DeleteLogicalSwitchPort(portName); err != nil {
			klog.Errorf("failed to delete stale logical switch port %s: %v", portName, err)
			return err
		}
		klog.Infof("deleted stale logical switch port %s", portName)
	}

	ipStr := util.GetStringIP(v4IP, v6IP)
	if err := c.OVNNbClient.CreateBareLogicalSwitchPort(c.config.NodeSwitch, portName, ipStr, mac); err != nil {
		klog.Errorf("failed to create logical switch port %s: %v", portName, err)
		return err
	}

	for ip := range strings.SplitSeq(ipStr, ",") {
		if ip == "" {
			continue
		}

		nodeIP, af := nodeIPv4, 4
		protocol := util.CheckProtocol(ip)
		if protocol == kubeovnv1.ProtocolIPv6 {
			nodeIP, af = nodeIPv6, 6
		}
		if nodeIP != "" {
			var (
				match       = fmt.Sprintf("ip%d.dst == %s", af, nodeIP)
				action      = kubeovnv1.PolicyRouteActionReroute
				externalIDs = map[string]string{
					"vendor":         util.CniTypeName,
					"node":           node.Name,
					"address-family": strconv.Itoa(af),
				}
			)
			klog.Infof("add policy route for router: %s, match %s, action %s, nexthop %s, externalID %v", c.config.ClusterRouter, match, action, ip, externalIDs)
			if err = c.addPolicyRouteToVpc(
				c.config.ClusterRouter,
				&kubeovnv1.PolicyRoute{
					Priority:  util.NodeRouterPolicyPriority,
					Match:     match,
					Action:    action,
					NextHopIP: ip,
				},
				externalIDs,
			); err != nil {
				klog.Errorf("failed to add logical router policy for node %s: %v", node.Name, err)
				return err
			}

			dnsIPs := make([]string, 0, len(c.config.NodeLocalDNSIPs))
			for _, ip := range c.config.NodeLocalDNSIPs {
				if util.CheckProtocol(ip) == protocol {
					dnsIPs = append(dnsIPs, ip)
				}
			}

			if err = c.addPolicyRouteForLocalDNSCacheOnNode(dnsIPs, portName, ip, node.Name, af); err != nil {
				klog.Errorf("failed to add policy route for node %s: %v", node.Name, err)
				return err
			}
		}
	}

	if err := c.addNodeGatewayStaticRoute(); err != nil {
		klog.Errorf("failed to add static route for node gw: %v", err)
		return err
	}

	patch := util.KVPatch{
		util.IPAddressAnnotation:     ipStr,
		util.MacAddressAnnotation:    mac,
		util.CidrAnnotation:          subnet.Spec.CIDRBlock,
		util.GatewayAnnotation:       subnet.Spec.Gateway,
		util.LogicalSwitchAnnotation: c.config.NodeSwitch,
		util.AllocatedAnnotation:     "true",
		util.PortNameAnnotation:      portName,
	}
	if err = util.PatchAnnotations(c.config.KubeClient.CoreV1().Nodes(), node.Name, patch); err != nil {
		klog.Errorf("failed to update annotations of node %s: %v", node.Name, err)
		return err
	}

	if err := c.createOrUpdateIPCR("", "", ipStr, mac, c.config.NodeSwitch, "", node.Name, ""); err != nil {
		klog.Errorf("failed to create or update IPs %s: %v", portName, err)
		return err
	}

	for _, subnet := range subnets {
		if (subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) || subnet.Spec.Vpc != c.config.ClusterRouter || subnet.Name == c.config.NodeSwitch || subnet.Spec.GatewayType != kubeovnv1.GWDistributedType {
			continue
		}
		if err = c.createPortGroupForDistributedSubnet(node, subnet); err != nil {
			klog.Errorf("failed to create port group for node %s and subnet %s: %v", node.Name, subnet.Name, err)
			return err
		}
	}
	c.distributedSubnetNeedSync.Store(true)

	// ovn acl doesn't support address_set name with '-', so replace '-' by '.'
	pgName := strings.ReplaceAll(portName, "-", ".")
	if err = c.OVNNbClient.CreatePortGroup(pgName, map[string]string{"node": node.Name, networkPolicyKey: "node" + "/" + key}); err != nil {
		klog.Errorf("create port group %s for node %s: %v", pgName, key, err)
		return err
	}

	if err := c.addPolicyRouteForCentralizedSubnetOnNode(node, ipStr); err != nil {
		klog.Errorf("failed to add policy route for node %s, %v", key, err)
		return err
	}

	if err := c.UpdateChassisTag(node); err != nil {
		klog.Errorf("failed to update chassis tag for node %s: %v", node.Name, err)
		return err
	}

	if err := c.retryDelDupChassis(util.ChassisRetryMaxTimes, util.ChassisControllerRetryInterval, c.cleanDuplicatedChassis, node); err != nil {
		klog.Errorf("failed to clean duplicated chassis for node %s: %v", node.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleNodeAnnotationsForProviderNetworks(node *v1.Node) error {
	providerNetworks, err := c.providerNetworksLister.List(labels.Everything())
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to list provider networks: %v", err)
		return err
	}

	for _, pn := range providerNetworks {
		excludeAnno := fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name)
		interfaceAnno := fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name)

		var newPn *kubeovnv1.ProviderNetwork
		excluded, err := util.IsNodeExcludedFromProviderNetwork(node, pn)
		if err != nil {
			klog.Error(err)
			return err
		}

		// Handle node annotation for exclusion (only when nodeSelector is not set)
		if !excluded && pn.Spec.NodeSelector == nil && len(node.Annotations) != 0 && node.Annotations[excludeAnno] == "true" {
			newPn = pn.DeepCopy()
			newPn.Spec.ExcludeNodes = append(newPn.Spec.ExcludeNodes, node.Name)
			excluded = true
		}

		var customInterface string
		for _, v := range pn.Spec.CustomInterfaces {
			if slices.Contains(v.Nodes, node.Name) {
				customInterface = v.Interface
				break
			}
		}
		if customInterface == "" && len(node.Annotations) != 0 {
			if customInterface = node.Annotations[interfaceAnno]; customInterface != "" {
				if newPn == nil {
					newPn = pn.DeepCopy()
				}
				var index *int
				for i := range newPn.Spec.CustomInterfaces {
					if newPn.Spec.CustomInterfaces[i].Interface == customInterface {
						index = &i
						break
					}
				}
				if index != nil {
					newPn.Spec.CustomInterfaces[*index].Nodes = append(newPn.Spec.CustomInterfaces[*index].Nodes, node.Name)
				} else {
					ci := kubeovnv1.CustomInterface{Interface: customInterface, Nodes: []string{node.Name}}
					newPn.Spec.CustomInterfaces = append(newPn.Spec.CustomInterfaces, ci)
				}
			}
		}

		if newPn != nil {
			if newPn, err = c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Update(context.Background(), newPn, metav1.UpdateOptions{}); err != nil {
				klog.Errorf("failed to update provider network %s: %v", pn.Name, err)
				return err
			}
		}

		if len(node.Annotations) != 0 {
			patch := util.KVPatch{excludeAnno: nil, interfaceAnno: nil}
			if err = util.PatchAnnotations(c.config.KubeClient.CoreV1().Nodes(), node.Name, patch); err != nil {
				klog.Errorf("failed to patch node %s: %v", node.Name, err)
				return err
			}
		}

		if excluded {
			if newPn == nil {
				newPn = pn.DeepCopy()
			} else {
				newPn = newPn.DeepCopy()
			}

			if newPn.Status.EnsureNodeStandardConditions(node.Name) {
				_, err = c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().UpdateStatus(context.Background(), newPn, metav1.UpdateOptions{})
				if err != nil {
					klog.Errorf("failed to update status of provider network %s: %v", pn.Name, err)
					return err
				}
			}
		}
	}

	return nil
}

func (c *Controller) handleDeleteNode(key string) (err error) {
	c.nodeKeyMutex.LockKey(key)
	defer func() {
		_ = c.nodeKeyMutex.UnlockKey(key)
		if err == nil {
			c.deletingNodeObjMap.Delete(key)
		}
	}()
	klog.Infof("handle delete node %s", key)

	node, ok := c.deletingNodeObjMap.Load(key)
	if !ok {
		return nil
	}
	n, _ := c.nodesLister.Get(key)
	if n != nil && n.UID != node.UID {
		klog.Warningf("Node %s is adding, skip the node delete handler, but it may leave some gc resources behind", key)
		return nil
	}
	return c.deleteNode(key)
}

func (c *Controller) deleteNode(key string) error {
	portName := util.NodeLspName(key)
	klog.Infof("delete logical switch port %s", portName)
	if err := c.OVNNbClient.DeleteLogicalSwitchPort(portName); err != nil {
		klog.Errorf("failed to delete node switch port %s: %v", portName, err)
		return err
	}
	if err := c.OVNSbClient.DeleteChassisByHost(key); err != nil {
		klog.Errorf("failed to delete chassis for node %s: %v", key, err)
		return err
	}

	for _, af := range [...]int{4, 6} {
		if err := c.deletePolicyRouteForLocalDNSCacheOnNode(key, af); err != nil {
			klog.Error(err)
			return err
		}
	}

	// ovn acl doesn't support address_set name with '-', so replace '-' by '.'
	pgName := strings.ReplaceAll(portName, "-", ".")
	if err := c.OVNNbClient.DeletePortGroup(pgName); err != nil {
		klog.Errorf("delete port group %s for node: %v", portName, err)
		return err
	}

	if err := c.deletePolicyRouteForNode(key, portName); err != nil {
		klog.Errorf("failed to delete policy route for node %s: %v", key, err)
		return err
	}

	if err := c.OVNNbClient.DeleteAddressSet(nodeUnderlayAddressSetName(key, 4)); err != nil {
		klog.Errorf("failed to delete address set for node %s: %v", key, err)
		return err
	}
	if err := c.OVNNbClient.DeleteAddressSet(nodeUnderlayAddressSetName(key, 6)); err != nil {
		klog.Errorf("failed to delete address set for node %s: %v", key, err)
		return err
	}

	klog.Infof("release node port %s", portName)
	c.ipam.ReleaseAddressByPod(portName, c.config.NodeSwitch)

	providerNetworks, err := c.providerNetworksLister.List(labels.Everything())
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to list provider networks: %v", err)
		return err
	}

	for _, pn := range providerNetworks {
		if err = c.updateProviderNetworkForNodeDeletion(pn, key); err != nil {
			klog.Error(err)
			return err
		}
	}
	klog.Infof("delete node ip %s", portName)
	if err = c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), portName, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	return nil
}

func (c *Controller) updateProviderNetworkForNodeDeletion(pn *kubeovnv1.ProviderNetwork, node string) error {
	// update provider network status
	var needUpdate bool
	newPn := pn.DeepCopy()
	if slices.Contains(newPn.Status.ReadyNodes, node) {
		newPn.Status.ReadyNodes = util.RemoveString(newPn.Status.ReadyNodes, node)
		needUpdate = true
	}
	if newPn.Status.RemoveNodeConditions(node) {
		needUpdate = true
	}
	if needUpdate {
		var err error
		newPn, err = c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().UpdateStatus(context.Background(), newPn, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update status of provider network %s: %v", pn.Name, err)
			return err
		}
	}

	// update provider network spec
	pn, newPn = newPn, nil
	if excludeNodes := util.RemoveString(pn.Spec.ExcludeNodes, node); len(excludeNodes) != len(pn.Spec.ExcludeNodes) {
		newPn = pn.DeepCopy()
		newPn.Spec.ExcludeNodes = excludeNodes
	}

	var changed bool
	customInterfaces := make([]kubeovnv1.CustomInterface, 0, len(pn.Spec.CustomInterfaces))
	for _, ci := range pn.Spec.CustomInterfaces {
		nodes := util.RemoveString(ci.Nodes, node)
		if !changed {
			changed = len(nodes) == 0 || len(nodes) != len(ci.Nodes)
		}
		if len(nodes) != 0 {
			customInterfaces = append(customInterfaces, kubeovnv1.CustomInterface{Interface: ci.Interface, Nodes: nodes})
		}
	}
	if changed {
		newPn = pn.DeepCopy()
		newPn.Spec.CustomInterfaces = customInterfaces
	}
	if newPn != nil {
		if _, err := c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Update(context.Background(), newPn, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update provider network %s: %v", pn.Name, err)
			return err
		}
	}

	return nil
}

func (c *Controller) handleUpdateNode(key string) error {
	c.nodeKeyMutex.LockKey(key)
	defer func() { _ = c.nodeKeyMutex.UnlockKey(key) }()
	klog.Infof("handle update node %s", key)

	node, err := c.nodesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get node %s: %v", key, err)
		return err
	}

	if err = c.handleNodeAnnotationsForProviderNetworks(node); err != nil {
		klog.Errorf("failed to handle annotations of node %s for provider networks: %v", node.Name, err)
		return err
	}

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get subnets %v", err)
		return err
	}

	if err := c.UpdateChassisTag(node); err != nil {
		klog.Errorf("failed to update chassis tag for node %s: %v", node.Name, err)
		return err
	}
	if err := c.retryDelDupChassis(util.ChassisRetryMaxTimes, util.ChassisControllerRetryInterval, c.cleanDuplicatedChassis, node); err != nil {
		klog.Errorf("failed to clean duplicated chassis for node %s: %v", node.Name, err)
		return err
	}

	c.distributedSubnetNeedSync.Store(true)

	for _, cachedSubnet := range subnets {
		if cachedSubnet.Spec.GatewayType != kubeovnv1.GWCentralizedType {
			continue
		}

		// For subnets using GatewayNodeSelectors, always trigger reconciliation
		// when node labels change, since the node might have been added or removed
		// from the gateway list
		if cachedSubnet.Spec.GatewayNode == "" && len(cachedSubnet.Spec.GatewayNodeSelectors) > 0 {
			c.addOrUpdateSubnetQueue.Add(cachedSubnet.Name)
			continue
		}

		if util.GatewayContains(cachedSubnet.Spec.GatewayNode, node.Name) {
			if err := c.reconcileOvnDefaultVpcRoute(cachedSubnet); err != nil {
				klog.Error(err)
				return err
			}
		}
	}

	return nil
}

func (c *Controller) syncDistributedSubnetRoutes() {
	if !c.distributedSubnetNeedSync.Swap(false) {
		return
	}

	klog.V(3).Infoln("start to sync distributed subnet routes")
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		c.distributedSubnetNeedSync.Store(true)
		return
	}

	for _, subnet := range subnets {
		if subnet.Spec.Vpc != c.config.ClusterRouter ||
			subnet.Name == c.config.NodeSwitch ||
			subnet.Spec.GatewayType != kubeovnv1.GWDistributedType ||
			(subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) {
			continue
		}
		if err := c.reconcileDistributedSubnetRouteInDefaultVpc(subnet); err != nil {
			klog.Errorf("failed to reconcile distributed subnet %s route: %v", subnet.Name, err)
			c.distributedSubnetNeedSync.Store(true)
		}
	}
}

func (c *Controller) checkSubnetGateway() {
	if err := c.checkSubnetGatewayNode(); err != nil {
		klog.Errorf("failed to check subnet gateway node: %v", err)
	}
}

func (c *Controller) checkSubnetGatewayNode() error {
	klog.V(3).Infoln("start to check subnet gateway node")
	subnetList, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return err
	}
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes: %v", err)
		return err
	}

	for _, subnet := range subnetList {
		if (subnet.Spec.Vlan != "" && (subnet.Spec.U2OInterconnection || !subnet.Spec.LogicalGateway)) ||
			subnet.Spec.Vpc != c.config.ClusterRouter ||
			subnet.Name == c.config.NodeSwitch ||
			subnet.Spec.GatewayNode == "" ||
			subnet.Spec.GatewayType != kubeovnv1.GWCentralizedType ||
			!subnet.Spec.EnableEcmp {
			continue
		}
		gwNodes := strings.Split(subnet.Spec.GatewayNode, ",")
		if len(gwNodes) < 2 {
			continue
		}

		for cidrBlock := range strings.SplitSeq(subnet.Spec.CIDRBlock, ",") {
			nextHops, nameIPMap, err := c.getPolicyRouteParas(cidrBlock, util.GatewayRouterPolicyPriority)
			if err != nil {
				klog.Errorf("failed to get ecmp policy route paras for subnet %s: %v", subnet.Name, err)
				continue
			}
			for _, node := range nodes {
				ipStr := node.Annotations[util.IPAddressAnnotation]
				for ip := range strings.SplitSeq(ipStr, ",") {
					if util.CheckProtocol(cidrBlock) != util.CheckProtocol(ip) {
						continue
					}

					exist := nameIPMap[node.Name] == ip
					if util.GatewayContains(subnet.Spec.GatewayNode, node.Name) {
						pinger, err := goping.NewPinger(ip)
						if err != nil {
							return fmt.Errorf("failed to init pinger, %w", err)
						}
						pinger.SetPrivileged(true)

						count := 5
						pinger.Count = count
						pinger.Timeout = time.Duration(count) * time.Second
						pinger.Interval = 1 * time.Second

						var pingSucceeded bool
						pinger.OnRecv = func(_ *goping.Packet) {
							pingSucceeded = true
							pinger.Stop()
						}
						if err = pinger.Run(); err != nil {
							klog.Errorf("failed to run pinger for destination %s: %v", ip, err)
							return err
						}

						nodeIsReady := nodeReady(node)
						if !pingSucceeded || !nodeIsReady {
							if exist {
								if !pingSucceeded {
									klog.Warningf("failed to ping %s ip %s on node %s", util.NodeNic, ip, node.Name)
								}
								if !nodeIsReady {
									klog.Warningf("node %s is not ready", node.Name)
								}
								klog.Warningf("delete ecmp policy route for node %s ip %s", node.Name, ip)
								nextHops.Remove(ip)
								delete(nameIPMap, node.Name)
								klog.Infof("update policy route for centralized subnet %s, nextHops %s", subnet.Name, nextHops)
								if err = c.updatePolicyRouteForCentralizedSubnet(subnet.Name, cidrBlock, nextHops.List(), nameIPMap); err != nil {
									klog.Errorf("failed to delete ecmp policy route for subnet %s on node %s, %v", subnet.Name, node.Name, err)
									return err
								}
							}
						} else {
							klog.V(3).Infof("succeeded to ping %s ip %s on node %s", util.NodeNic, ip, node.Name)
							if !exist {
								nextHops.Add(ip)
								if nameIPMap == nil {
									nameIPMap = make(map[string]string, 1)
								}
								nameIPMap[node.Name] = ip
								klog.Infof("update policy route for centralized subnet %s, nextHops %s", subnet.Name, nextHops)
								if err = c.updatePolicyRouteForCentralizedSubnet(subnet.Name, cidrBlock, nextHops.List(), nameIPMap); err != nil {
									klog.Errorf("failed to add ecmp policy route for subnet %s on node %s, %v", subnet.Name, node.Name, err)
									return err
								}
							}
						}
					} else if exist {
						klog.Infof("subnet %s gateway nodes does not contain node %s, delete policy route for node ip %s", subnet.Name, node.Name, ip)
						nextHops.Remove(ip)
						delete(nameIPMap, node.Name)
						klog.Infof("update policy route for centralized subnet %s, nextHops %s", subnet.Name, nextHops)
						if err = c.updatePolicyRouteForCentralizedSubnet(subnet.Name, cidrBlock, nextHops.List(), nameIPMap); err != nil {
							klog.Errorf("failed to delete ecmp policy route for subnet %s on node %s, %v", subnet.Name, node.Name, err)
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

func (c *Controller) cleanDuplicatedChassis(node *v1.Node) error {
	// if multi chassis has the same node name, delete all of them
	var err error
	if _, err := c.OVNSbClient.GetChassisByHost(node.Name); err == nil {
		return nil
	}
	klog.Errorf("failed to get chassis for node %s: %v", node.Name, err)
	if errors.Is(err, ovs.ErrOneNodeMultiChassis) {
		klog.Warningf("node %s has multiple chassis", node.Name)
		if err := c.OVNSbClient.DeleteChassisByHost(node.Name); err != nil {
			klog.Errorf("failed to delete chassis for node %s: %v", node.Name, err)
			return err
		}
	}
	return err
}

func (c *Controller) retryDelDupChassis(attempts, sleep int, f func(node *v1.Node) error, node *v1.Node) (err error) {
	i := 0
	for ; ; i++ {
		err = f(node)
		if err == nil {
			return err
		}
		klog.Errorf("failed to delete duplicated chassis for node %s: %v", node.Name, err)
		if i >= (attempts - 1) {
			break
		}
		time.Sleep(time.Duration(sleep) * time.Second)
	}
	if i >= (attempts - 1) {
		errMsg := errors.New("exhausting all attempts")
		klog.Error(errMsg)
		return errMsg
	}
	klog.V(3).Infof("finish check chassis")
	return nil
}

func (c *Controller) fetchPodsOnNode(nodeName string, pods []*v1.Pod) ([]string, error) {
	ports := make([]string, 0, len(pods))
	for _, pod := range pods {
		if !isPodAlive(pod) || pod.Spec.HostNetwork || pod.Spec.NodeName != nodeName {
			continue
		}

		if pod.Annotations[util.LogicalRouterAnnotation] != c.config.ClusterRouter {
			subnetName := pod.Annotations[util.LogicalSwitchAnnotation]
			if subnetName == "" {
				klog.V(4).Infof("Pod %s/%s is not on cluster router and has no logical switch annotation, skipping for VLAN check.", pod.Namespace, pod.Name)
				continue
			}

			subnet, err := c.subnetsLister.Get(subnetName)
			if err != nil {
				klog.Errorf("failed to get subnet %s: %v", subnetName, err)
				return nil, err
			}

			if subnet.Spec.Vlan == "" {
				continue
			}
		}

		podName := c.getNameByPod(pod)

		podNets, err := c.getPodKubeovnNets(pod)
		if err != nil {
			klog.Errorf("failed to get pod nets %v", err)
			return nil, err
		}

		for _, podNet := range podNets {
			if !isOvnSubnet(podNet.Subnet) {
				continue
			}

			if pod.Annotations != nil && pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "true" {
				ports = append(ports, ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName))
			}
		}
	}
	return ports, nil
}

func (c *Controller) CheckNodePortGroup() {
	if err := c.checkAndUpdateNodePortGroup(); err != nil {
		klog.Errorf("check node port group status: %v", err)
	}
}

func (c *Controller) checkAndUpdateNodePortGroup() error {
	klog.V(3).Infoln("start to check node port-group status")
	var networkPolicyExists bool
	if c.config.EnableNP {
		np, _ := c.npsLister.List(labels.Everything())
		networkPolicyExists = len(np) != 0
	}

	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("list nodes: %v", err)
		return err
	}

	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("list pods, %v", err)
		return err
	}

	for _, node := range nodes {
		// The port-group should already created when add node
		pgName := strings.ReplaceAll(node.Annotations[util.PortNameAnnotation], "-", ".")

		// use join IP only when no internal IP exists
		nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*node)
		joinIP := node.Annotations[util.IPAddressAnnotation]
		joinIPv4, joinIPv6 := util.SplitStringIP(joinIP)
		if nodeIPv4 == "" {
			nodeIPv4 = joinIPv4
		}
		if nodeIPv6 == "" {
			nodeIPv6 = joinIPv6
		}
		nodeIP := strings.Trim(fmt.Sprintf("%s,%s", nodeIPv4, nodeIPv6), ",")

		nodePorts, err := c.fetchPodsOnNode(node.Name, pods)
		if err != nil {
			klog.Errorf("fetch pods for node %v: %v", node.Name, err)
			return err
		}

		if err = c.OVNNbClient.PortGroupSetPorts(pgName, nodePorts); err != nil {
			klog.Errorf("failed to set ports of port group %s: %v", pgName, err)
			return err
		}

		if networkPolicyExists {
			if err := c.OVNNbClient.CreateNodeACL(pgName, nodeIP, joinIP); err != nil {
				klog.Errorf("create node acl for node pg %s: %v", pgName, err)
			}
		} else {
			// clear all acl
			if err = c.OVNNbClient.DeleteAcls(pgName, portGroupKey, "", nil); err != nil {
				klog.Errorf("delete node acl for node pg %s: %v", pgName, err)
			}
		}
	}

	return nil
}

func (c *Controller) UpdateChassisTag(node *v1.Node) error {
	annoChassisName := node.Annotations[util.ChassisAnnotation]
	if annoChassisName == "" {
		// kube-ovn-cni not ready to set chassis
		return nil
	}
	chassis, err := c.OVNSbClient.GetChassis(annoChassisName, true)
	if err != nil {
		klog.Errorf("failed to get chassis %s for node %s: %v", annoChassisName, node.Name, err)
		return err
	}
	if chassis == nil {
		klog.Infof("chassis %q not registered for node %s, do chassis gc once", annoChassisName, node.Name)
		// chassis name conflict, do GC
		if err = c.gcChassis(); err != nil {
			klog.Errorf("failed to gc chassis: %v", err)
			return err
		}
		err = &ErrChassisNotFound{Chassis: annoChassisName, Node: node.Name}
		klog.Error(err)
		return err
	}

	if chassis.ExternalIDs == nil || chassis.ExternalIDs["vendor"] != util.CniTypeName {
		klog.Infof("init tag %s for node %s chassis %s", util.CniTypeName, node.Name, chassis.Name)
		if err = c.OVNSbClient.UpdateChassisTag(chassis.Name, node.Name); err != nil {
			err := fmt.Errorf("failed to init chassis tag, %w", err)
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) addNodeGatewayStaticRoute() error {
	// If user not manage static route for default vpc, just add route about ovn-default to join
	if vpc, err := c.vpcsLister.Get(c.config.ClusterRouter); err != nil || vpc.Spec.StaticRoutes != nil {
		existRoute, err := c.OVNNbClient.ListLogicalRouterStaticRoutes(c.config.ClusterRouter, nil, nil, "", nil)
		if err != nil {
			klog.Errorf("failed to get vpc %s static route list, %v", c.config.ClusterRouter, err)
		}
		if len(existRoute) != 0 {
			klog.Infof("skip add static route for node gw")
			return nil
		}
	}
	dstCidr := "0.0.0.0/0,::/0"
	for cidrBlock := range strings.SplitSeq(dstCidr, ",") {
		for nextHop := range strings.SplitSeq(c.config.NodeSwitchGateway, ",") {
			if util.CheckProtocol(cidrBlock) != util.CheckProtocol(nextHop) {
				continue
			}

			if err := c.addStaticRouteToVpc(
				c.config.ClusterRouter,
				&kubeovnv1.StaticRoute{
					Policy:     kubeovnv1.PolicyDst,
					CIDR:       cidrBlock,
					NextHopIP:  nextHop,
					RouteTable: util.MainRouteTable,
				},
			); err != nil {
				klog.Errorf("failed to add static route for node gw: %v", err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) getPolicyRouteParas(cidr string, priority int) (*strset.Set, map[string]string, error) {
	ipSuffix := "ip4"
	if util.CheckProtocol(cidr) == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}
	match := fmt.Sprintf("%s.src == %s", ipSuffix, cidr)
	policyList, err := c.OVNNbClient.GetLogicalRouterPolicy(c.config.ClusterRouter, priority, match, true)
	if err != nil {
		klog.Errorf("failed to get logical router policy: %v", err)
		return nil, nil, err
	}
	if len(policyList) == 0 {
		return strset.New(), map[string]string{}, nil
	}
	return strset.New(policyList[0].Nexthops...), policyList[0].ExternalIDs, nil
}

func (c *Controller) deletePolicyRouteForNode(nodeName, portName string) error {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("get subnets: %v", err)
		return err
	}

	addresses := c.ipam.GetPodAddress(portName)
	for _, addr := range addresses {
		if addr.IP == "" {
			continue
		}
		klog.Infof("deleting logical router policy with nexthop %q from %s for node %s", addr.IP, c.config.ClusterRouter, nodeName)
		if err = c.OVNNbClient.DeleteLogicalRouterPolicyByNexthop(c.config.ClusterRouter, util.NodeRouterPolicyPriority, addr.IP); err != nil {
			klog.Errorf("failed to delete logical router policy with nexthop %q from %s for node %s: %v", addr.IP, c.config.ClusterRouter, nodeName, err)
			return err
		}
	}

	for _, subnet := range subnets {
		if (subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) || subnet.Spec.Vpc != c.config.ClusterRouter || subnet.Name == c.config.NodeSwitch {
			continue
		}

		if subnet.Spec.GatewayType == kubeovnv1.GWDistributedType {
			pgName := getOverlaySubnetsPortGroupName(subnet.Name, nodeName)
			if err = c.OVNNbClient.DeletePortGroup(pgName); err != nil {
				klog.Errorf("delete port group for subnet %s and node %s: %v", subnet.Name, nodeName, err)
				return err
			}

			klog.Infof("delete policy route for distributed subnet %s, node %s", subnet.Name, nodeName)
			if err = c.deletePolicyRouteForDistributedSubnet(subnet, nodeName); err != nil {
				klog.Errorf("delete policy route for subnet %s and node %s: %v", subnet.Name, nodeName, err)
				return err
			}
		}

		if subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType {
			if subnet.Spec.EnableEcmp {
				for cidrBlock := range strings.SplitSeq(subnet.Spec.CIDRBlock, ",") {
					nextHops, nameIPMap, err := c.getPolicyRouteParas(cidrBlock, util.GatewayRouterPolicyPriority)
					if err != nil {
						klog.Errorf("get ecmp policy route paras for subnet %v, error %v", subnet.Name, err)
						continue
					}

					exist := false
					if _, ok := nameIPMap[nodeName]; ok {
						exist = true
					}

					if exist {
						nextHops.Remove(nameIPMap[nodeName])
						delete(nameIPMap, nodeName)

						if nextHops.Size() == 0 {
							klog.Infof("delete policy route for centralized subnet %s, nextHops %s", subnet.Name, nextHops)
							if err := c.deletePolicyRouteForCentralizedSubnet(subnet); err != nil {
								klog.Errorf("failed to delete policy route for centralized subnet %s, %v", subnet.Name, err)
								return err
							}
						} else {
							klog.Infof("update policy route for centralized subnet %s, nextHops %s", subnet.Name, nextHops)
							if err = c.updatePolicyRouteForCentralizedSubnet(subnet.Name, cidrBlock, nextHops.List(), nameIPMap); err != nil {
								klog.Errorf("failed to update policy route for subnet %s on node %s, %v", subnet.Name, nodeName, err)
								return err
							}
						}
					}
				}
			} else {
				klog.Infof("reconcile policy route for centralized subnet %s", subnet.Name)
				if err := c.reconcileDefaultCentralizedSubnetRouteInDefaultVpc(subnet); err != nil {
					klog.Errorf("failed to delete policy route for centralized subnet %s, %v", subnet.Name, err)
					return err
				}
			}
		}
	}
	return nil
}

func (c *Controller) addPolicyRouteForCentralizedSubnetOnNode(node *v1.Node, nodeIP string) error {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get subnets %v", err)
		return err
	}

	for _, subnet := range subnets {
		if (subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) || subnet.Spec.Vpc != c.config.ClusterRouter || subnet.Name == c.config.NodeSwitch || subnet.Spec.GatewayType != kubeovnv1.GWCentralizedType {
			continue
		}
		nodeName := node.Name
		if subnet.Spec.EnableEcmp {
			if !util.GatewayContains(subnet.Spec.GatewayNode, nodeName) &&
				(subnet.Spec.GatewayNode != "" || !util.MatchLabelSelectors(subnet.Spec.GatewayNodeSelectors, node.Labels)) {
				continue
			}

			for nextHop := range strings.SplitSeq(nodeIP, ",") {
				for cidrBlock := range strings.SplitSeq(subnet.Spec.CIDRBlock, ",") {
					if util.CheckProtocol(cidrBlock) != util.CheckProtocol(nextHop) {
						continue
					}

					nextHops, nameIPMap, err := c.getPolicyRouteParas(cidrBlock, util.GatewayRouterPolicyPriority)
					if err != nil {
						klog.Errorf("get ecmp policy route paras for subnet %v, error %v", subnet.Name, err)
						continue
					}
					if nameIPMap[nodeName] == nextHop {
						continue
					}

					nextHops.Add(nextHop)
					if nameIPMap == nil {
						nameIPMap = make(map[string]string, 1)
					}
					nameIPMap[nodeName] = nextHop
					klog.Infof("update policy route for centralized subnet %s, nextHops %s", subnet.Name, nextHops)
					if err = c.updatePolicyRouteForCentralizedSubnet(subnet.Name, cidrBlock, nextHops.List(), nameIPMap); err != nil {
						klog.Errorf("failed to update policy route for subnet %s on node %s, %v", subnet.Name, nodeName, err)
						return err
					}
				}
			}
		} else {
			if subnet.Status.ActivateGateway != nodeName {
				continue
			}
			klog.Infof("add policy route for centralized subnet %s, on node %s, ip %s", subnet.Name, nodeName, nodeIP)
			if err = c.addPolicyRouteForCentralizedSubnet(subnet, nodeName, nil, strings.Split(nodeIP, ",")); err != nil {
				klog.Errorf("failed to add active-backup policy route for centralized subnet %s: %v", subnet.Name, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) addPolicyRouteForLocalDNSCacheOnNode(dnsIPs []string, nodePortName, nodeIP, nodeName string, af int) error {
	if len(dnsIPs) == 0 {
		return c.deletePolicyRouteForLocalDNSCacheOnNode(nodeName, af)
	}

	var (
		externalIDs = map[string]string{
			"vendor":          util.CniTypeName,
			"node":            nodeName,
			"address-family":  strconv.Itoa(af),
			"isLocalDnsCache": "true",
		}
		pgAs     = strings.ReplaceAll(fmt.Sprintf("%s_ip%d", nodePortName, af), "-", ".")
		action   = kubeovnv1.PolicyRouteActionReroute
		nextHops = []string{nodeIP}
	)
	matches := strset.NewWithSize(len(dnsIPs))
	for _, ip := range dnsIPs {
		matches.Add(fmt.Sprintf("ip%d.src == $%s && ip%d.dst == %s", af, pgAs, af, ip))
	}

	policies, err := c.OVNNbClient.GetLogicalRouterPoliciesByExtID(c.config.ClusterRouter, "node", nodeName)
	if err != nil {
		klog.Errorf("failed to list logical router policies with external-ids:node = %q: %v", nodeName, err)
		return err
	}

	for _, policy := range policies {
		if len(policy.ExternalIDs) == 0 || policy.ExternalIDs["vendor"] != util.CniTypeName || policy.ExternalIDs["isLocalDnsCache"] != "true" {
			continue
		}
		if policy.Priority == util.NodeRouterPolicyPriority && policy.Action == string(action) && slices.Equal(policy.Nexthops, nextHops) && matches.Has(policy.Match) {
			matches.Remove(policy.Match)
			continue
		}
		// delete unused policy router policy
		klog.Infof("deleting logical router policy by UUID %s", policy.UUID)
		if err = c.OVNNbClient.DeleteLogicalRouterPolicyByUUID(c.config.ClusterRouter, policy.UUID); err != nil {
			klog.Errorf("failed to delete logical router policy by UUID %s: %v", policy.UUID, err)
			return err
		}
	}

	for _, match := range matches.List() {
		klog.Infof("add node local dns cache policy route for router %s: match %q, action %q, nexthop %q, externalID %v", c.config.ClusterRouter, match, action, nodeIP, externalIDs)
		if err := c.addPolicyRouteToVpc(
			c.config.ClusterRouter,
			&kubeovnv1.PolicyRoute{
				Priority:  util.NodeRouterPolicyPriority,
				Match:     match,
				Action:    action,
				NextHopIP: nodeIP,
			},
			externalIDs,
		); err != nil {
			klog.Errorf("failed to add logical router policy for node %s: %v", nodeName, err)
			return err
		}
	}

	return nil
}

func (c *Controller) deletePolicyRouteForLocalDNSCacheOnNode(nodeName string, af int) error {
	policies, err := c.OVNNbClient.ListLogicalRouterPolicies(c.config.ClusterRouter, -1, map[string]string{
		"vendor":          util.CniTypeName,
		"node":            nodeName,
		"address-family":  strconv.Itoa(af),
		"isLocalDnsCache": "true",
	}, true)
	if err != nil {
		klog.Errorf("failed to list logical router policies: %v", err)
		return err
	}
	if len(policies) == 0 {
		return nil
	}

	for _, policy := range policies {
		klog.Infof("delete node local dns cache policy route for router %s with match %s", c.config.ClusterRouter, policy.Match)

		if err := c.OVNNbClient.DeleteLogicalRouterPolicyByUUID(c.config.ClusterRouter, policy.UUID); err != nil {
			klog.Errorf("failed to delete policy route for node local dns in router %s with match %s: %v", c.config.ClusterRouter, policy.Match, err)
			return err
		}
	}
	return nil
}
