package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

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
	goping "github.com/oilbeater/go-ping"
)

func (c *Controller) enqueueAddNode(obj interface{}) {
	if !c.isLeader() {
		return
	}
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
	if !c.isLeader() {
		return
	}

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
		klog.V(3).Infof("enqueue update node %s", key)
		c.updateNodeQueue.Add(key)
	}
}

func (c *Controller) enqueueDeleteNode(obj interface{}) {
	if !c.isLeader() {
		return
	}
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

func (c *Controller) handleAddNode(key string) error {
	orinode, err := c.nodesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	node := orinode.DeepCopy()

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return err
	}

	nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*node)
	for _, subnet := range subnets {
		if subnet.Spec.Vlan == "" && subnet.Spec.Vpc == util.DefaultVpc &&
			(util.CIDRContainIP(subnet.Spec.CIDRBlock, nodeIPv4) || util.CIDRContainIP(subnet.Spec.CIDRBlock, nodeIPv6)) {
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
		v4IP, v6IP, mac, err = c.ipam.GetStaticAddress(portName, node.Annotations[util.IpAddressAnnotation],
			node.Annotations[util.MacAddressAnnotation],
			node.Annotations[util.LogicalSwitchAnnotation])
		if err != nil {
			klog.Errorf("failed to alloc static ip addrs for node %v: %v", node.Name, err)
			return err
		}
	} else {
		v4IP, v6IP, mac, err = c.ipam.GetRandomAddress(portName, c.config.NodeSwitch, nil)
		if err != nil {
			klog.Errorf("failed to alloc random ip addrs for node %v: %v", node.Name, err)
			return err
		}
	}

	ipStr := util.GetStringIP(v4IP, v6IP)
	if err := c.ovnClient.CreatePort(c.config.NodeSwitch, portName, ipStr, subnet.Spec.CIDRBlock, mac, "", "", false, ""); err != nil {
		return err
	}

	for _, ip := range strings.Split(ipStr, ",") {
		if ip == "" {
			continue
		}

		nodeIP := nodeIPv4
		if util.CheckProtocol(ip) == kubeovnv1.ProtocolIPv6 {
			nodeIP = nodeIPv6
		}
		if nodeIP != "" {
			if err = c.ovnClient.AddStaticRoute("", nodeIP, ip, c.config.ClusterRouter, util.NormalRouteType); err != nil {
				klog.Errorf("failed to add static router from node to ovn0: %v", err)
				return err
			}
		}
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

	// ovn acl doesn't support address_set name with '-', so replace '-' by '.'
	pgName := strings.Replace(node.Annotations[util.PortNameAnnotation], "-", ".", -1)
	if err := c.ovnClient.CreateNpPortGroup(pgName, "node", key); err != nil {
		klog.Errorf("failed to create port group %v for node %s, %v", portName, key, err)
		return err
	}

	if err := c.RemoveRedundantChassis(node); err != nil {
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
	if err := c.ovnClient.DeleteLogicalSwitchPort(portName); err != nil {
		klog.Errorf("failed to delete node switch port node-%s: %v", key, err)
		return err
	}
	if err := c.ovnClient.DeleteChassis(key); err != nil {
		klog.Errorf("failed to delete chassis for node %s: %v", key, err)
		return err
	}

	if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), portName, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	// ovn acl doesn't support address_set name with '-', so replace '-' by '.'
	pgName := strings.Replace(portName, "-", ".", -1)
	if err := c.ovnClient.DeletePortGroup(pgName); err != nil {
		klog.Errorf("failed to delete port group %s for node, %v", portName, err)
		return err
	}

	addresses := c.ipam.GetPodAddress(portName)
	for _, addr := range addresses {
		if err := c.ovnClient.DeleteStaticRouteByNextHop(addr.Ip); err != nil {
			return err
		}
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

	if err := c.retryDelDupChassis(util.ChasRetryTime, util.ChasRetryIntev+2, c.checkChassisDupl, node); err != nil {
		return err
	}

	for _, orisubnet := range subnets {
		subnet := orisubnet.DeepCopy()
		if util.GatewayContains(subnet.Spec.GatewayNode, node.Name) {
			if err := c.reconcileGateway(subnet); err != nil {
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
		} else {
			newIpCr.Labels = map[string]string{
				util.SubnetNameLabel: subnetName,
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

func (c *Controller) CheckGatewayReady() {
	if err := c.checkGatewayReady(); err != nil {
		klog.Errorf("failed to check gateway ready %v", err)
	}
}

func (c *Controller) checkGatewayReady() error {
	if !c.config.EnableEcmp {
		return nil
	}
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
		if subnet.Spec.Vlan != "" || subnet.Spec.GatewayType != kubeovnv1.GWCentralizedType || subnet.Spec.GatewayNode == "" {
			continue
		}

		for _, node := range nodes {
			ipStr := node.Annotations[util.IpAddressAnnotation]
			for _, ip := range strings.Split(ipStr, ",") {
				var cidrBlock string
				for _, cidrBlock = range strings.Split(subnet.Spec.CIDRBlock, ",") {
					if util.CheckProtocol(cidrBlock) != util.CheckProtocol(ip) {
						continue
					}

					exist, err := c.checkNodeEcmpRouteExist(ip, cidrBlock)
					if err != nil {
						klog.Errorf("get ecmp static route for subnet %v, error %v", subnet.Name, err)
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
							klog.Warningf("failed to ping ovn0 %s or node %v is not ready", ip, node.Name)
							if exist {
								if err := c.ovnClient.DeleteMatchedStaticRoute(cidrBlock, ip, c.config.ClusterRouter); err != nil {
									klog.Errorf("failed to delete static route %s for node %s, %v", ip, node.Name, err)
									return err
								}
							}
						} else {
							klog.V(3).Infof("succeed to ping gw %s", ip)
							if !exist {
								if err := c.ovnClient.AddStaticRoute(ovs.PolicySrcIP, subnet.Spec.CIDRBlock, ip, c.config.ClusterRouter, util.EcmpRouteType); err != nil {
									klog.Errorf("failed to add static route for node %s, %v", node.Name, err)
									return err
								}
							}
						}
					} else {
						if exist {
							klog.Infof("subnet %v gatewayNode does not contains node %v, should delete ecmp route for node ip %s", subnet.Name, node.Name, ip)
							if err := c.ovnClient.DeleteMatchedStaticRoute(cidrBlock, ip, c.config.ClusterRouter); err != nil {
								klog.Errorf("failed to delete static route %s for node %s, %v", ip, node.Name, err)
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

func (c *Controller) checkNodeEcmpRouteExist(nodeIp, cidrBlock string) (bool, error) {
	routes, err := c.ovnClient.GetStaticRouteList(c.config.ClusterRouter)
	if err != nil {
		klog.Errorf("failed to list static route %v", err)
		return false, err
	}

	for _, route := range routes {
		if route.Policy != ovs.PolicySrcIP {
			continue
		}
		if route.CIDR == cidrBlock && route.NextHop == nodeIp {
			klog.V(3).Infof("src-ip static route exist for cidr %s, nexthop %v", cidrBlock, nodeIp)
			return true, nil
		}
	}
	return false, nil
}

func (c *Controller) checkChassisDupl(node *v1.Node) error {
	// notice that multiple chassises may arise and we are not prepared
	chassisAdd, err := c.ovnClient.GetChassis(node.Name)
	if err != nil {
		klog.Errorf("failed to get node %s chassisID, %v", node.Name, err)
		return err
	}
	chassisAnn := node.Annotations[util.ChassisAnnotation]
	if chassisAnn == chassisAdd || chassisAnn == "" {
		return nil
	}

	klog.Errorf("duplicate chassis for node %s and new chassis %s", node.Name, chassisAdd)
	if err := c.ovnClient.DeleteChassis(node.Name); err != nil {
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

func (c *Controller) fetchPodsOnNode(nodeName string) ([]string, error) {
	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list pods, %v", err)
		return nil, err
	}

	ports := make([]string, 0, len(pods))
	for _, pod := range pods {
		if !isPodAlive(pod) || pod.Spec.HostNetwork || pod.Spec.NodeName != nodeName || pod.Annotations[util.LogicalRouterAnnotation] != util.DefaultVpc {
			continue
		}
		podName := c.getNameByPod(pod)

		if pod.Annotations[util.AllocatedAnnotation] == "true" {
			ports = append(ports, fmt.Sprintf("%s.%s", podName, pod.Namespace))
		}
	}
	return ports, nil
}

func (c *Controller) checkPodsChangedOnNode(pgName string, ports []string) (bool, error) {
	pgPorts, err := c.ovnClient.ListPgPorts(pgName)
	if err != nil {
		klog.Errorf("failed to fetch ports for pg %v, %v", pgName, err)
		return false, err
	}

	nameIdMap, idNameMap, err := c.ovnClient.ListLspForNodePortgroup()
	if err != nil {
		klog.Errorf("failed to list lsp info, %v", err)
		return false, err
	}

	portIds := make([]string, 0, len(ports))
	for _, port := range ports {
		if portId, ok := nameIdMap[port]; ok {
			portIds = append(portIds, portId)
		}
	}

	for _, portId := range portIds {
		if !util.IsStringIn(portId, pgPorts) {
			klog.Infof("pod on node changed, new added port %v should add to node port group %v", idNameMap[portId], pgName)
			return true, nil
		}
	}

	for _, pgPort := range pgPorts {
		if !util.IsStringIn(pgPort, portIds) {
			klog.Infof("pod on node changed, can not find match pod for port %v in node port group %v", pgPort, pgName)
			return true, nil
		}
	}

	return false, nil
}

func (c *Controller) CheckNodePortGroup() {
	if err := c.checkAndUpdateNodePortGroup(); err != nil {
		klog.Errorf("failed to check node port-group status, %v", err)
	}
}

var lastNpExists = make(map[string]bool)

func (c *Controller) checkAndUpdateNodePortGroup() error {
	klog.V(3).Infoln("start to check node port-group status")
	np, _ := c.npsLister.List(labels.Everything())
	networkPolicyExists := len(np) != 0

	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes, %v", err)
		return err
	}

	for _, node := range nodes {
		// ovn acl doesn't support address_set name with '-', so replace '-' by '.'
		pgName := strings.Replace(node.Annotations[util.PortNameAnnotation], "-", ".", -1)
		nodeIP := node.Annotations[util.IpAddressAnnotation]
		if err := c.ovnClient.CreateNpPortGroup(pgName, "node", node.Name); err != nil {
			klog.Errorf("failed to create port group %v for node %s, %v", pgName, node.Name, err)
			return err
		}

		ports, err := c.fetchPodsOnNode(node.Name)
		if err != nil {
			klog.Errorf("failed to fetch pods for node %v, %v", node.Name, err)
			return err
		}

		changed, err := c.checkPodsChangedOnNode(pgName, ports)
		if err != nil {
			klog.Errorf("failed to check pod status for node %v, %v", node.Name, err)
			continue
		}

		if lastNpExists[node.Name] != networkPolicyExists {
			klog.Infof("networkpolicy num changed when check nodepg %v", pgName)
			changed = true
		}

		if !changed {
			klog.V(3).Infof("pods on node %v do not changed", node.Name)
			continue
		}
		lastNpExists[node.Name] = networkPolicyExists

		err = c.ovnClient.SetPortsToPortGroup(pgName, ports)
		if err != nil {
			klog.Errorf("failed to set port group for node %v, %v", node.Name, err)
			return err
		}

		if networkPolicyExists {
			if err := c.ovnClient.CreateACLForNodePg(pgName, nodeIP); err != nil {
				klog.Errorf("failed to create node acl for node pg %v, %v", pgName, err)
			}
		} else {
			if err := c.ovnClient.DeleteAclForNodePg(pgName); err != nil {
				klog.Errorf("failed to delete node acl for node pg %v, %v", pgName, err)
			}
		}
	}

	return nil
}

func (c *Controller) RemoveRedundantChassis(node *v1.Node) error {
	chassisAdd, err := c.ovnClient.GetChassis(node.Name)
	if err != nil {
		klog.Errorf("failed to get node %s chassisID, %v", node.Name, err)
		return err
	}

	if chassisAdd == "" && node.Annotations[util.ChassisAnnotation] != "" {
		chassises, err := c.ovnClient.GetALlChassisHostname()
		if err != nil {
			klog.Errorf("failed to get all chassis, %v", err)
		}
		nodes, err := c.nodesLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list nodes, %v", err)
			return err
		}
		for _, chassis := range chassises {
			matched := true
			for _, node := range nodes {
				if chassis == node.Name {
					matched = false
				}
			}
			if matched {
				if err := c.ovnClient.DeleteChassis(chassis); err != nil {
					klog.Errorf("failed to delete chassis for node %s %v", chassis, err)
					return err
				}
			}
		}
		return errors.New("chassis reset, reboot ovs-ovn on this node: " + node.Name)
	}
	return nil
}
