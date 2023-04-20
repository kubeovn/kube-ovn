package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	goping "github.com/oilbeater/go-ping"
	v1 "k8s.io/api/core/v1"
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

func (c *Controller) enqueueAddNode(obj interface{}) {

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add node %s", key)
	c.addNodeQueue.Add(key)
}

func nodeReady(node *v1.Node) bool {
	for _, con := range node.Status.Conditions {
		if con.Type == v1.NodeReady && con.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

func (c *Controller) enqueueUpdateNode(oldObj, newObj interface{}) {

	oldNode := oldObj.(*v1.Node)
	newNode := newObj.(*v1.Node)

	if nodeReady(oldNode) != nodeReady(newNode) ||
		!reflect.DeepEqual(oldNode.Annotations, newNode.Annotations) {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
			utilruntime.HandleError(err)
			return
		}
		if len(newNode.Annotations) == 0 || newNode.Annotations[util.AllocatedAnnotation] != "true" {
			klog.V(3).Infof("enqueue add node %s", key)
			c.addNodeQueue.Add(key)
		} else {
			klog.V(3).Infof("enqueue update node %s", key)
			c.updateNodeQueue.Add(key)
		}
	}
}

func (c *Controller) enqueueDeleteNode(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue delete node %s", key)
	c.deleteNodeQueue.Add(key)
}

func (c *Controller) runAddNodeWorker() {
	for c.processNextAddNodeWorkItem() {
	}
}

func (c *Controller) runUpdateNodeWorker() {
	for c.processNextUpdateNodeWorkItem() {
	}
}

func (c *Controller) runDeleteNodeWorker() {
	for c.processNextDeleteNodeWorkItem() {
	}
}

func (c *Controller) processNextAddNodeWorkItem() bool {
	obj, shutdown := c.addNodeQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addNodeQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addNodeQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddNode(key); err != nil {
			c.addNodeQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addNodeQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateNodeWorkItem() bool {
	obj, shutdown := c.updateNodeQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateNodeQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateNodeQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateNode(key); err != nil {
			c.updateNodeQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateNodeQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteNodeWorkItem() bool {
	obj, shutdown := c.deleteNodeQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.deleteNodeQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.deleteNodeQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeleteNode(key); err != nil {
			c.deleteNodeQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.deleteNodeQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func nodeUnderlayAddressSetName(node string, af int) string {
	return fmt.Sprintf("node_%s_underlay_v%d", strings.ReplaceAll(node, "-", "_"), af)
}

func (c *Controller) handleAddNode(key string) error {
	cachedNode, err := c.nodesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
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
		if subnet.Spec.Vpc != util.DefaultVpc {
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
	portName := fmt.Sprintf("node-%s", key)
	if node.Annotations[util.AllocatedAnnotation] == "true" && node.Annotations[util.IpAddressAnnotation] != "" && node.Annotations[util.MacAddressAnnotation] != "" {
		v4IP, v6IP, mac, err = c.ipam.GetStaticAddress(portName, portName, node.Annotations[util.IpAddressAnnotation],
			node.Annotations[util.MacAddressAnnotation],
			node.Annotations[util.LogicalSwitchAnnotation], true)
		if err != nil {
			klog.Errorf("failed to alloc static ip addrs for node %v: %v", node.Name, err)
			return err
		}
	} else {
		v4IP, v6IP, mac, err = c.ipam.GetRandomAddress(portName, portName, "", c.config.NodeSwitch, nil, true)
		if err != nil {
			klog.Errorf("failed to alloc random ip addrs for node %v: %v", node.Name, err)
			return err
		}
	}

	ipStr := util.GetStringIP(v4IP, v6IP)
	if err := c.ovnClient.CreateBareLogicalSwitchPort(c.config.NodeSwitch, portName, ipStr, mac); err != nil {
		return err
	}

	for _, ip := range strings.Split(ipStr, ",") {
		if ip == "" {
			continue
		}

		nodeIP, af := nodeIPv4, 4
		if util.CheckProtocol(ip) == kubeovnv1.ProtocolIPv6 {
			nodeIP, af = nodeIPv6, 6
		}
		if nodeIP != "" {
			match := fmt.Sprintf("ip%d.dst == %s", af, nodeIP)
			externalIDs := map[string]string{
				"vendor":         util.CniTypeName,
				"node":           node.Name,
				"address-family": strconv.Itoa(af),
			}
			klog.Infof("add policy route for router: %s, match %s, action %s, nexthop %s, extrenalID %v", c.config.ClusterRouter, match, "reroute", ip, externalIDs)
			if err = c.ovnLegacyClient.AddPolicyRoute(c.config.ClusterRouter, util.NodeRouterPolicyPriority, match, "reroute", ip, externalIDs); err != nil {
				klog.Errorf("failed to add logical router policy for node %s: %v", node.Name, err)
				return err
			}
		}
	}

	if err := c.addNodeGwStaticRoute(); err != nil {
		klog.Errorf("failed to add static route for node gw: %v", err)
		return err
	}

	patchPayloadTemplate :=
		`[{
        "op": "%s",
        "path": "/metadata/annotations",
        "value": %s
    }]`
	op := "replace"
	if len(node.Annotations) == 0 {
		node.Annotations = map[string]string{}
		op = "add"
	}

	node.Annotations[util.IpAddressAnnotation] = ipStr
	node.Annotations[util.MacAddressAnnotation] = mac
	node.Annotations[util.CidrAnnotation] = subnet.Spec.CIDRBlock
	node.Annotations[util.GatewayAnnotation] = subnet.Spec.Gateway
	node.Annotations[util.LogicalSwitchAnnotation] = c.config.NodeSwitch
	node.Annotations[util.AllocatedAnnotation] = "true"
	node.Annotations[util.PortNameAnnotation] = fmt.Sprintf("node-%s", key)
	raw, _ := json.Marshal(node.Annotations)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	_, err = c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), key, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}, "")
	if err != nil {
		klog.Errorf("patch node %s failed: %v", key, err)
		return err
	}

	if err := c.createOrUpdateCrdIPs("", ipStr, mac, c.config.NodeSwitch, "", node.Name, "", "", nil); err != nil {
		klog.Errorf("failed to create or update IPs node-%s: %v", key, err)
		return err
	}

	for _, subnet := range subnets {
		if (subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) || subnet.Spec.Vpc != util.DefaultVpc || subnet.Name == c.config.NodeSwitch || subnet.Spec.GatewayType != kubeovnv1.GWDistributedType {
			continue
		}
		if err = c.createPortGroupForDistributedSubnet(node, subnet); err != nil {
			klog.Errorf("failed to create port group for node %s and subnet %s: %v", node.Name, subnet.Name, err)
			return err
		}
		if err = c.addPolicyRouteForDistributedSubnet(subnet, node.Name, v4IP, v6IP); err != nil {
			klog.Errorf("failed to add policy router for node %s and subnet %s: %v", node.Name, subnet.Name, err)
			return err
		}
		// policy route for overlay distributed subnet should be reconciled when node ip changed
		c.addOrUpdateSubnetQueue.Add(subnet.Name)
	}

	// ovn acl doesn't support address_set name with '-', so replace '-' by '.'
	pgName := strings.Replace(node.Annotations[util.PortNameAnnotation], "-", ".", -1)
	if err = c.ovnClient.CreatePortGroup(pgName, map[string]string{networkPolicyKey: "node" + "/" + key}); err != nil {
		klog.Errorf("create port group %s for node %s: %v", pgName, key, err)
		return err
	}

	if err := c.addPolicyRouteForCentralizedSubnetOnNode(node.Name, ipStr); err != nil {
		klog.Errorf("failed to add policy route for node %s, %v", key, err)
		return err
	}

	if err := c.validateChassis(node); err != nil {
		return err
	}

	if err := c.retryDelDupChassis(util.ChasRetryTime, util.ChasRetryIntev+2, c.checkChassisDupl, node); err != nil {
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
		excluded := util.ContainsString(pn.Spec.ExcludeNodes, node.Name)
		if !excluded && len(node.Annotations) != 0 && node.Annotations[excludeAnno] == "true" {
			newPn = pn.DeepCopy()
			newPn.Spec.ExcludeNodes = append(newPn.Spec.ExcludeNodes, node.Name)
			excluded = true
		}

		var customInterface string
		for _, v := range pn.Spec.CustomInterfaces {
			if util.ContainsString(v.Nodes, node.Name) {
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
			newNode := node.DeepCopy()
			delete(newNode.Annotations, excludeAnno)
			delete(newNode.Annotations, interfaceAnno)
			if len(newNode.Annotations) != len(node.Annotations) {
				if _, err = c.config.KubeClient.CoreV1().Nodes().Update(context.Background(), newNode, metav1.UpdateOptions{}); err != nil {
					klog.Errorf("failed to update node %s: %v", node.Name, err)
					return err
				}
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

func (c *Controller) handleDeleteNode(key string) error {
	portName := fmt.Sprintf("node-%s", key)
	klog.Infof("delete logical switch port %s", portName)
	if err := c.ovnClient.DeleteLogicalSwitchPort(portName); err != nil {
		klog.Errorf("failed to delete node switch port node-%s: %v", key, err)
		return err
	}
	if err := c.ovnLegacyClient.DeleteChassisByNode(key); err != nil {
		klog.Errorf("failed to delete chassis for node %s: %v", key, err)
		return err
	}

	if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), portName, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	// ovn acl doesn't support address_set name with '-', so replace '-' by '.'
	pgName := strings.Replace(portName, "-", ".", -1)
	if err := c.ovnClient.DeletePortGroup(pgName); err != nil {
		klog.Errorf("delete port group %s for node: %v", portName, err)
		return err
	}

	if err := c.deletePolicyRouteForNode(key); err != nil {
		klog.Errorf("failed to delete policy route for node %s: %v", key, err)
		return err
	}

	addresses := c.ipam.GetPodAddress(portName)
	for _, addr := range addresses {
		if addr.Ip == "" {
			continue
		}
		if err := c.ovnLegacyClient.DeletePolicyRouteByNexthop(c.config.ClusterRouter, util.NodeRouterPolicyPriority, addr.Ip); err != nil {
			klog.Errorf("failed to delete router policy for node %s: %v", key, err)
			return err
		}
	}
	if err := c.ovnLegacyClient.DeleteAddressSet(nodeUnderlayAddressSetName(key, 4)); err != nil {
		klog.Errorf("failed to delete address set for node %s: %v", key, err)
		return err
	}
	if err := c.ovnLegacyClient.DeleteAddressSet(nodeUnderlayAddressSetName(key, 6)); err != nil {
		klog.Errorf("failed to delete address set for node %s: %v", key, err)
		return err
	}

	c.ipam.ReleaseAddressByPod(portName)

	providerNetworks, err := c.providerNetworksLister.List(labels.Everything())
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to list provider networks: %v", err)
		return err
	}

	for _, pn := range providerNetworks {
		if err = c.updateProviderNetworkForNodeDeletion(pn, key); err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) updateProviderNetworkForNodeDeletion(pn *kubeovnv1.ProviderNetwork, node string) error {
	// update provider network status
	var needUpdate bool
	newPn := pn.DeepCopy()
	if util.ContainsString(newPn.Status.ReadyNodes, node) {
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
		newPn := pn.DeepCopy()
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
		if newPn == nil {
			newPn = pn.DeepCopy()
		}
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
	node, err := c.nodesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
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

	if node.Annotations[util.ChassisAnnotation] != "" {
		if err = c.ovnLegacyClient.InitChassisNodeTag(node.Annotations[util.ChassisAnnotation], node.Name); err != nil {
			klog.Errorf("failed to set chassis nodeTag for node '%s', %v", node.Name, err)
			return err
		}
	}
	if err := c.retryDelDupChassis(util.ChasRetryTime, util.ChasRetryIntev+2, c.checkChassisDupl, node); err != nil {
		return err
	}

	for _, cachedSubnet := range subnets {
		subnet := cachedSubnet.DeepCopy()
		if util.GatewayContains(subnet.Spec.GatewayNode, node.Name) {
			if err := c.reconcileOvnDefaultVpcRoute(subnet); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Controller) createOrUpdateCrdIPs(podName, ip, mac, subnetName, ns, nodeName, providerName, podType string, existingCR **kubeovnv1.IP) error {
	var key, ipName string
	if subnetName == c.config.NodeSwitch {
		key = nodeName
		ipName = fmt.Sprintf("node-%s", nodeName)
	} else if strings.HasPrefix(podName, util.U2OInterconnName[0:19]) {
		key = podName
		ipName = podName
	} else {
		key = podName
		ipName = ovs.PodNameToPortName(podName, ns, providerName)
	}

	var err error
	var ipCr *kubeovnv1.IP
	if existingCR != nil {
		ipCr = *existingCR
	} else {
		ipCr, err = c.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), ipName, metav1.GetOptions{})
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				errMsg := fmt.Errorf("failed to get ip CR %s: %v", ipName, err)
				klog.Error(errMsg)
				return errMsg
			}
			// the returned pointer is not nil if the CR does not exist
			ipCr = nil
		}
	}

	v4IP, v6IP := util.SplitStringIP(ip)
	if ipCr == nil {
		_, err = c.config.KubeOvnClient.KubeovnV1().IPs().Create(context.Background(), &kubeovnv1.IP{
			ObjectMeta: metav1.ObjectMeta{
				Name: ipName,
				Labels: map[string]string{
					util.SubnetNameLabel: subnetName,
					util.NodeNameLabel:   nodeName,
					subnetName:           "",
				},
			},
			Spec: kubeovnv1.IPSpec{
				PodName:       key,
				Subnet:        subnetName,
				NodeName:      nodeName,
				Namespace:     ns,
				IPAddress:     ip,
				V4IPAddress:   v4IP,
				V6IPAddress:   v6IP,
				MacAddress:    mac,
				AttachIPs:     []string{},
				AttachMacs:    []string{},
				AttachSubnets: []string{},
				PodType:       podType,
			},
		}, metav1.CreateOptions{})
		if err != nil {
			errMsg := fmt.Errorf("failed to create ip CR %s: %v", ipName, err)
			klog.Error(errMsg)
			return errMsg
		}
	} else {
		newIpCr := ipCr.DeepCopy()
		if newIpCr.Labels != nil {
			newIpCr.Labels[util.SubnetNameLabel] = subnetName
			newIpCr.Labels[util.NodeNameLabel] = nodeName
		} else {
			newIpCr.Labels = map[string]string{
				util.SubnetNameLabel: subnetName,
				util.NodeNameLabel:   nodeName,
			}
		}
		newIpCr.Spec.PodName = key
		newIpCr.Spec.Namespace = ns
		newIpCr.Spec.Subnet = subnetName
		newIpCr.Spec.NodeName = nodeName
		newIpCr.Spec.IPAddress = ip
		newIpCr.Spec.V4IPAddress = v4IP
		newIpCr.Spec.V6IPAddress = v6IP
		newIpCr.Spec.MacAddress = mac
		newIpCr.Spec.AttachIPs = []string{}
		newIpCr.Spec.AttachMacs = []string{}
		newIpCr.Spec.AttachSubnets = []string{}
		newIpCr.Spec.PodType = podType
		if reflect.DeepEqual(newIpCr.Labels, ipCr.Labels) && reflect.DeepEqual(newIpCr.Spec, ipCr.Spec) {
			return nil
		}

		_, err := c.config.KubeOvnClient.KubeovnV1().IPs().Update(context.Background(), newIpCr, metav1.UpdateOptions{})
		if err != nil {
			errMsg := fmt.Errorf("failed to update ip CR %s: %v", newIpCr.Name, err)
			klog.Error(errMsg)
			return errMsg
		}
	}

	return nil
}

func (c *Controller) deleteCrdIPs(podName, ns, providerName string) error {
	portName := ovs.PodNameToPortName(podName, ns, providerName)
	klog.Infof("delete cr ip '%s' for pod %s/%s", portName, ns, podName)
	if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), portName, metav1.DeleteOptions{}); err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to delete ip %s, %v", portName, err)
			return err
		}
	}
	return nil
}

func (c *Controller) CheckGatewayReady() {
	if err := c.checkGatewayReady(); err != nil {
		klog.Errorf("failed to check gateway ready %v", err)
	}
}

func (c *Controller) checkGatewayReady() error {
	klog.V(3).Infoln("start to check gateway status")
	subnetList, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes, %v", err)
		return err
	}

	for _, subnet := range subnetList {
		if (subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) ||
			subnet.Spec.GatewayNode == "" ||
			subnet.Spec.GatewayType != kubeovnv1.GWCentralizedType ||
			!subnet.Spec.EnableEcmp {
			continue
		}

		for _, node := range nodes {
			ipStr := node.Annotations[util.IpAddressAnnotation]
			for _, ip := range strings.Split(ipStr, ",") {
				for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
					if util.CheckProtocol(cidrBlock) != util.CheckProtocol(ip) {
						continue
					}

					exist, err := c.checkPolicyRouteExistForNode(node.Name, cidrBlock, ip, util.GatewayRouterPolicyPriority)
					if err != nil {
						klog.Errorf("check ecmp policy route exist for subnet %v, error %v", subnet.Name, err)
						break
					}
					nextHops, nameIpMap, err := c.getPolicyRouteParas(cidrBlock, util.GatewayRouterPolicyPriority)
					if err != nil {
						klog.Errorf("get ecmp policy route paras for subnet %v, error %v", subnet.Name, err)
						break
					}

					if util.GatewayContains(subnet.Spec.GatewayNode, node.Name) {
						pinger, err := goping.NewPinger(ip)
						if err != nil {
							return fmt.Errorf("failed to init pinger, %v", err)
						}
						pinger.SetPrivileged(true)

						count := 5
						pinger.Count = count
						pinger.Timeout = time.Duration(count) * time.Second
						pinger.Interval = 1 * time.Second

						success := false

						pinger.OnRecv = func(p *goping.Packet) {
							success = true
							pinger.Stop()
						}
						pinger.Run()

						if !nodeReady(node) {
							success = false
						}

						if !success {
							if exist {
								klog.Warningf("failed to ping ovn0 %s or node %s is not ready, delete ecmp policy route for node", ip, node.Name)
								nextHops = util.RemoveString(nextHops, ip)
								delete(nameIpMap, node.Name)
								klog.Infof("update policy route for centralized subnet %s, nextHops %s", subnet.Name, nextHops)
								if err = c.updatePolicyRouteForCentralizedSubnet(subnet.Name, cidrBlock, nextHops, nameIpMap); err != nil {
									klog.Errorf("failed to delete ecmp policy route for subnet %s on node %s, %v", subnet.Name, node.Name, err)
									return err
								}
							}
						} else {
							klog.V(3).Infof("succeed to ping gw %s", ip)
							if !exist {
								nextHops = append(nextHops, ip)
								if nameIpMap == nil {
									nameIpMap = make(map[string]string, 1)
								}
								nameIpMap[node.Name] = ip
								klog.Infof("update policy route for centralized subnet %s, nextHops %s", subnet.Name, nextHops)
								if err = c.updatePolicyRouteForCentralizedSubnet(subnet.Name, cidrBlock, nextHops, nameIpMap); err != nil {
									klog.Errorf("failed to add ecmp policy route for subnet %s on node %s, %v", subnet.Name, node.Name, err)
									return err
								}
							}
						}
					} else {
						if exist {
							klog.Infof("subnet %s gatewayNode does not contains node %v, delete policy route for node ip %s", subnet.Name, node.Name, ip)
							nextHops = util.RemoveString(nextHops, ip)
							delete(nameIpMap, node.Name)
							klog.Infof("update policy route for centralized subnet %s, nextHops %s", subnet.Name, nextHops)
							if err = c.updatePolicyRouteForCentralizedSubnet(subnet.Name, cidrBlock, nextHops, nameIpMap); err != nil {
								klog.Errorf("failed to delete ecmp policy route for subnet %s on node %s, %v", subnet.Name, node.Name, err)
								return err
							}
						}
					}
				}
			}
		}
	}
	return nil
}

func (c *Controller) checkRouteExist(nextHop, cidrBlock, routePolicy string) (bool, error) {
	routes, err := c.ovnLegacyClient.GetStaticRouteList(c.config.ClusterRouter)
	if err != nil {
		klog.Errorf("failed to list static route %v", err)
		return false, err
	}

	for _, route := range routes {
		if route.Policy != routePolicy {
			continue
		}

		if route.CIDR == cidrBlock && route.NextHop == nextHop {
			klog.V(3).Infof("static route exists for cidr %s, nexthop %v", cidrBlock, nextHop)
			return true, nil
		}
	}
	return false, nil
}

func (c *Controller) checkChassisDupl(node *v1.Node) error {
	// notice that multiple chassises may arise and we are not prepared
	chassisAdd, err := c.ovnLegacyClient.GetChassis(node.Name)
	if err != nil {
		klog.Errorf("failed to get node %s chassisID, %v", node.Name, err)
		return err
	}
	if chassisAdd == "" {
		klog.Errorf("chassis of node %s is empty", node.Name)
		return err
	}
	chassisAnn := node.Annotations[util.ChassisAnnotation]
	if chassisAnn == chassisAdd || chassisAnn == "" {
		return nil
	}

	klog.Errorf("duplicate chassis for node %s and new chassis %s", node.Name, chassisAdd)
	if err := c.ovnLegacyClient.DeleteChassisByNode(node.Name); err != nil {
		klog.Errorf("failed to delete chassis for node %s %v", node.Name, err)
		return err
	}
	return errors.New("deleting dismatch chassis id")
}

func (c *Controller) retryDelDupChassis(attempts int, sleep int, f func(node *v1.Node) error, node *v1.Node) (err error) {
	i := 0
	for ; ; i++ {
		err = f(node)
		if err == nil {
			return
		}
		if i >= (attempts - 1) {
			break
		}
		time.Sleep(time.Duration(sleep) * time.Second)
	}
	if i >= (attempts - 1) {
		errMsg := fmt.Errorf("exhausting all attempts")
		klog.Error(errMsg)
		return errMsg
	}
	klog.V(3).Infof("finish check chassis")
	return nil
}

func (c *Controller) fetchPodsOnNode(nodeName string, pods []*v1.Pod) ([]string, error) {
	ports := make([]string, 0, len(pods))
	for _, pod := range pods {
		if !isPodAlive(pod) || pod.Spec.HostNetwork || pod.Spec.NodeName != nodeName || pod.Annotations[util.LogicalRouterAnnotation] != util.DefaultVpc {
			continue
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
	np, _ := c.npsLister.List(labels.Everything())
	networkPolicyExists := len(np) != 0

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
		pgName := strings.Replace(node.Annotations[util.PortNameAnnotation], "-", ".", -1)

		// use join IP only when no internal IP exists
		nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*node)
		joinIP := node.Annotations[util.IpAddressAnnotation]
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

		if err = c.ovnClient.PortGroupSetPorts(pgName, nodePorts); err != nil {
			klog.Errorf("add ports to port group %s: %v", pgName, err)
			return err
		}

		if networkPolicyExists {
			if err := c.ovnLegacyClient.CreateACLForNodePg(pgName, nodeIP, joinIP); err != nil {
				klog.Errorf("failed to create node acl for node pg %v, %v", pgName, err)
			}
		} else {
			if err := c.ovnLegacyClient.DeleteAclForNodePg(pgName); err != nil {
				klog.Errorf("failed to delete node acl for node pg %v, %v", pgName, err)
			}
		}
	}

	return nil
}

func (c *Controller) validateChassis(node *v1.Node) error {
	if node.Annotations[util.ChassisAnnotation] == "" {
		return nil
	}
	chassisAdd, err := c.ovnLegacyClient.GetChassis(node.Name)
	if err != nil {
		klog.Errorf("failed to get node %s chassisID, %v", node.Name, err)
		return err
	}
	if node.Annotations[util.ChassisAnnotation] == chassisAdd {
		if err = c.ovnLegacyClient.InitChassisNodeTag(chassisAdd, node.Name); err != nil {
			return fmt.Errorf("failed to init chassis tag, %v", err)
		}
		return nil
	}
	if chassisAdd == "" {
		// If no chassisID for this node is obtained, we need to perform GC in order for the chassis to be re-registered
		if err = c.gcChassis(); err != nil {
			return fmt.Errorf("failed to gc chassis, %v", err)
		}
	} else {
		// When the ids are obtained but are inconsistent, it is usually because there are duplicate chassis records.
		// All chassis related to Node need to be deleted so that the correct chassis can be registered again
		if err := c.ovnLegacyClient.DeleteChassisByNode(node.Name); err != nil {
			klog.Errorf("failed to delete chassis for node %s %v", node.Name, err)
			return err
		}
	}
	return errors.New("chassis reset, reboot ovs-ovn on this node: " + node.Name)
}

func (c *Controller) addNodeGwStaticRoute() error {
	// If user not manage static route for default vpc, just add route about ovn-default to join
	if vpc, err := c.vpcsLister.Get(util.DefaultVpc); err != nil || vpc.Spec.StaticRoutes != nil {
		existRoute, err := c.ovnLegacyClient.GetStaticRouteList(c.config.ClusterRouter)
		if err != nil {
			klog.Errorf("failed to get vpc %s static route list, %v", c.config.ClusterRouter, err)
		}
		if len(existRoute) != 0 {
			klog.Infof("skip add static route for node gw")
			return nil
		}
	}
	dstCidr := "0.0.0.0/0,::/0"
	for _, cidrBlock := range strings.Split(dstCidr, ",") {
		for _, nextHop := range strings.Split(c.config.NodeSwitchGateway, ",") {
			if util.CheckProtocol(cidrBlock) != util.CheckProtocol(nextHop) {
				continue
			}
			exist, err := c.checkRouteExist(nextHop, cidrBlock, ovs.PolicyDstIP)
			if err != nil {
				klog.Errorf("get static route for node gw error %v", err)
				return err
			}

			if !exist {
				klog.Infof("add static route for node gw")
				if err := c.ovnLegacyClient.AddStaticRoute("", cidrBlock, nextHop, "", "", c.config.ClusterRouter, util.NormalRouteType); err != nil {
					klog.Errorf("failed to add static route for node gw: %v", err)
					return err
				}
			}
		}
	}
	return nil
}

func (c *Controller) getPolicyRouteParas(cidr string, priority int32) ([]string, map[string]string, error) {
	// TODO:// support get policy route by vpc
	ipSuffix := "ip4"
	if util.CheckProtocol(cidr) == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}
	match := fmt.Sprintf("%s.src == %s", ipSuffix, cidr)
	nextHops, nameIpMap, err := c.ovnLegacyClient.GetPolicyRouteParas(priority, match)
	if err != nil {
		klog.Errorf("failed to get policy route paras, %v", err)
		return nextHops, nameIpMap, err
	}
	return nextHops, nameIpMap, nil
}

func (c *Controller) checkPolicyRouteExistForNode(nodeName, cidr, nexthop string, priority int32) (bool, error) {
	_, nameIpMap, err := c.getPolicyRouteParas(cidr, priority)
	if err != nil {
		klog.Errorf("failed to get policy route paras, %v", err)
		return false, err
	}
	if nodeIp, ok := nameIpMap[nodeName]; ok && nodeIp == nexthop {
		return true, nil
	}
	return false, nil
}

func (c *Controller) deletePolicyRouteForNode(nodeName string) error {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("get subnets: %v", err)
		return err
	}

	for _, subnet := range subnets {
		if (subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) || subnet.Spec.Vpc != util.DefaultVpc || subnet.Name == c.config.NodeSwitch {
			continue
		}

		if subnet.Spec.GatewayType == kubeovnv1.GWDistributedType {
			pgName := getOverlaySubnetsPortGroupName(subnet.Name, nodeName)
			if err = c.ovnClient.DeletePortGroup(pgName); err != nil {
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
				for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
					nextHops, nameIpMap, err := c.getPolicyRouteParas(cidrBlock, util.GatewayRouterPolicyPriority)
					if err != nil {
						klog.Errorf("get ecmp policy route paras for subnet %v, error %v", subnet.Name, err)
						continue
					}

					exist := false
					if _, ok := nameIpMap[nodeName]; ok {
						exist = true
					}

					if exist {
						nextHops = util.RemoveString(nextHops, nameIpMap[nodeName])
						delete(nameIpMap, nodeName)

						if len(nextHops) == 0 {
							klog.Infof("delete policy route for centralized subnet %s, nextHops %s", subnet.Name, nextHops)
							if err := c.deletePolicyRouteForCentralizedSubnet(subnet); err != nil {
								klog.Errorf("failed to delete policy route for centralized subnet %s, %v", subnet.Name, err)
								return err
							}
						} else {
							klog.Infof("update policy route for centralized subnet %s, nextHops %s", subnet.Name, nextHops)
							if err = c.updatePolicyRouteForCentralizedSubnet(subnet.Name, cidrBlock, nextHops, nameIpMap); err != nil {
								klog.Errorf("failed to update policy route for subnet %s on node %s, %v", subnet.Name, nodeName, err)
								return err
							}
						}
					}
				}
			} else {
				klog.Infof("delete policy route for centralized subnet %s", subnet.Name)
				if err := c.deletePolicyRouteForCentralizedSubnet(subnet); err != nil {
					klog.Errorf("failed to delete policy route for centralized subnet %s, %v", subnet.Name, err)
					return err
				}
			}
		}
	}
	return nil
}

func (c *Controller) addPolicyRouteForCentralizedSubnetOnNode(nodeName, nodeIP string) error {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get subnets %v", err)
		return err
	}

	for _, subnet := range subnets {
		if (subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) || subnet.Spec.Vpc != util.DefaultVpc || subnet.Name == c.config.NodeSwitch || subnet.Spec.GatewayType != kubeovnv1.GWCentralizedType {
			continue
		}

		if subnet.Spec.EnableEcmp {
			if !util.GatewayContains(subnet.Spec.GatewayNode, nodeName) {
				continue
			}

			for _, nextHop := range strings.Split(nodeIP, ",") {
				for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
					if util.CheckProtocol(cidrBlock) != util.CheckProtocol(nextHop) {
						continue
					}
					exist, err := c.checkPolicyRouteExistForNode(nodeName, cidrBlock, nextHop, util.GatewayRouterPolicyPriority)
					if err != nil {
						klog.Errorf("check ecmp policy route exist for subnet %v, error %v", subnet.Name, err)
						continue
					}
					if exist {
						continue
					}

					nextHops, nameIpMap, err := c.getPolicyRouteParas(cidrBlock, util.GatewayRouterPolicyPriority)
					if err != nil {
						klog.Errorf("get ecmp policy route paras for subnet %v, error %v", subnet.Name, err)
						continue
					}
					nextHops = append(nextHops, nextHop)
					if nameIpMap == nil {
						nameIpMap = make(map[string]string, 1)
					}
					nameIpMap[nodeName] = nextHop
					klog.Infof("update policy route for centralized subnet %s, nextHops %s", subnet.Name, nextHops)
					if err = c.updatePolicyRouteForCentralizedSubnet(subnet.Name, cidrBlock, nextHops, nameIpMap); err != nil {
						klog.Errorf("failed to update policy route for subnet %s on node %s, %v", subnet.Name, nodeName, err)
						return err
					}
				}
			}
		} else {
			if subnet.Status.ActivateGateway != nodeName {
				continue
			}
			klog.Infof("add policy route for centrailized subnet %s, on node %s, ip %s", subnet.Name, nodeName, nodeIP)
			if err = c.addPolicyRouteForCentralizedSubnet(subnet, nodeName, nil, strings.Split(nodeIP, ",")); err != nil {
				klog.Errorf("failed to add active-backup policy route for centralized subnet %s: %v", subnet.Name, err)
				return err
			}
		}
	}
	return nil
}
