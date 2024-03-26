package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/scylladb/go-set/strset"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) InitOVN() error {
	var err error

	if err = c.initClusterRouter(); err != nil {
		klog.Errorf("init cluster router failed: %v", err)
		return err
	}

	if c.config.EnableLb {
		if err = c.initLoadBalancer(); err != nil {
			klog.Errorf("init load balancer failed: %v", err)
			return err
		}
	}

	if err = c.initDefaultVlan(); err != nil {
		klog.Errorf("init default vlan failed: %v", err)
		return err
	}

	if err = c.initNodeSwitch(); err != nil {
		klog.Errorf("init node switch failed: %v", err)
		return err
	}

	if err = c.initDefaultLogicalSwitch(); err != nil {
		klog.Errorf("init default switch failed: %v", err)
		return err
	}

	return nil
}

func (c *Controller) InitDefaultVpc() error {
	cachedVpc, err := c.vpcsLister.Get(c.config.ClusterRouter)
	if err != nil {
		cachedVpc = &kubeovnv1.Vpc{}
		cachedVpc.Name = c.config.ClusterRouter
		cachedVpc, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Create(context.Background(), cachedVpc, metav1.CreateOptions{})
		if err != nil {
			klog.Errorf("init default vpc failed: %v", err)
			return err
		}
	}
	vpc := cachedVpc.DeepCopy()
	vpc.Status.DefaultLogicalSwitch = c.config.DefaultLogicalSwitch
	vpc.Status.Router = c.config.ClusterRouter
	if c.config.EnableLb {
		vpc.Status.TCPLoadBalancer = c.config.ClusterTCPLoadBalancer
		vpc.Status.TCPSessionLoadBalancer = c.config.ClusterTCPSessionLoadBalancer
		vpc.Status.UDPLoadBalancer = c.config.ClusterUDPLoadBalancer
		vpc.Status.UDPSessionLoadBalancer = c.config.ClusterUDPSessionLoadBalancer
		vpc.Status.SctpLoadBalancer = c.config.ClusterSctpLoadBalancer
		vpc.Status.SctpSessionLoadBalancer = c.config.ClusterSctpSessionLoadBalancer
	}
	vpc.Status.Standby = true
	vpc.Status.Default = true

	bytes, err := vpc.Status.Bytes()
	if err != nil {
		klog.Error(err)
		return err
	}
	_, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(), vpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
	if err != nil {
		klog.Errorf("init default vpc failed: %v", err)
		return err
	}
	return nil
}

// InitDefaultLogicalSwitch init the default logical switch for ovn network
func (c *Controller) initDefaultLogicalSwitch() error {
	subnet, err := c.subnetsLister.Get(c.config.DefaultLogicalSwitch)
	if err == nil {
		if subnet != nil && util.CheckProtocol(c.config.DefaultCIDR) != util.CheckProtocol(subnet.Spec.CIDRBlock) {
			// single-stack upgrade to dual-stack
			if util.CheckProtocol(c.config.DefaultCIDR) == kubeovnv1.ProtocolDual {
				subnet := subnet.DeepCopy()
				subnet.Spec.CIDRBlock = c.config.DefaultCIDR
				if _, err = c.formatSubnet(subnet); err != nil {
					klog.Errorf("init format subnet %s failed: %v", c.config.DefaultLogicalSwitch, err)
					return err
				}
			}
		}
		return nil
	}

	if !k8serrors.IsNotFound(err) {
		klog.Errorf("get default subnet %s failed: %v", c.config.DefaultLogicalSwitch, err)
		return err
	}

	defaultSubnet := kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: c.config.DefaultLogicalSwitch},
		Spec: kubeovnv1.SubnetSpec{
			Vpc:                 c.config.ClusterRouter,
			Default:             true,
			Provider:            util.OvnProvider,
			CIDRBlock:           c.config.DefaultCIDR,
			Gateway:             c.config.DefaultGateway,
			GatewayNode:         "",
			DisableGatewayCheck: !c.config.DefaultGatewayCheck,
			ExcludeIps:          strings.Split(c.config.DefaultExcludeIps, ","),
			NatOutgoing:         true,
			GatewayType:         kubeovnv1.GWDistributedType,
			Protocol:            util.CheckProtocol(c.config.DefaultCIDR),
			EnableLb:            &c.config.EnableLb,
		},
	}
	if c.config.NetworkType == util.NetworkTypeVlan {
		defaultSubnet.Spec.Vlan = c.config.DefaultVlanName
		if c.config.DefaultLogicalGateway && c.config.DefaultU2OInterconnection {
			err = fmt.Errorf("logicalGateway and u2oInterconnection can't be opened at the same time")
			klog.Error(err)
			return err
		}
		defaultSubnet.Spec.LogicalGateway = c.config.DefaultLogicalGateway
		defaultSubnet.Spec.U2OInterconnection = c.config.DefaultU2OInterconnection
	}

	_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Create(context.Background(), &defaultSubnet, metav1.CreateOptions{})
	return err
}

// InitNodeSwitch init node switch to connect host and pod
func (c *Controller) initNodeSwitch() error {
	subnet, err := c.subnetsLister.Get(c.config.NodeSwitch)
	if err == nil {
		if util.CheckProtocol(c.config.NodeSwitchCIDR) == kubeovnv1.ProtocolDual && util.CheckProtocol(subnet.Spec.CIDRBlock) != kubeovnv1.ProtocolDual {
			// single-stack upgrade to dual-stack
			subnet := subnet.DeepCopy()
			subnet.Spec.CIDRBlock = c.config.NodeSwitchCIDR
			if _, err = c.formatSubnet(subnet); err != nil {
				klog.Errorf("init format subnet %s failed: %v", c.config.NodeSwitch, err)
				return err
			}
		} else {
			c.config.NodeSwitchCIDR = subnet.Spec.CIDRBlock
		}
		return nil
	}

	if !k8serrors.IsNotFound(err) {
		klog.Errorf("get node subnet %s failed: %v", c.config.NodeSwitch, err)
		return err
	}

	nodeSubnet := kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: c.config.NodeSwitch},
		Spec: kubeovnv1.SubnetSpec{
			Vpc:                    c.config.ClusterRouter,
			Default:                false,
			Provider:               util.OvnProvider,
			CIDRBlock:              c.config.NodeSwitchCIDR,
			Gateway:                c.config.NodeSwitchGateway,
			GatewayNode:            "",
			ExcludeIps:             strings.Split(c.config.NodeSwitchGateway, ","),
			Protocol:               util.CheckProtocol(c.config.NodeSwitchCIDR),
			DisableInterConnection: true,
		},
	}

	_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Create(context.Background(), &nodeSubnet, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to create subnet %s: %v", c.config.NodeSwitch, err)
		return err
	}
	return nil
}

// InitClusterRouter init cluster router to connect different logical switches
func (c *Controller) initClusterRouter() error {
	return c.OVNNbClient.CreateLogicalRouter(c.config.ClusterRouter)
}

func (c *Controller) initLB(name, protocol string, sessionAffinity bool) error {
	protocol = strings.ToLower(protocol)

	var (
		selectFields string
		err          error
	)

	if sessionAffinity {
		selectFields = ovnnb.LoadBalancerSelectionFieldsIPSrc
	}

	if err = c.OVNNbClient.CreateLoadBalancer(name, protocol, selectFields); err != nil {
		klog.Errorf("create load balancer %s: %v", name, err)
		return err
	}

	if sessionAffinity {
		if err = c.OVNNbClient.SetLoadBalancerAffinityTimeout(name, util.DefaultServiceSessionStickinessTimeout); err != nil {
			klog.Errorf("failed to set affinity timeout of %s load balancer %s: %v", protocol, name, err)
			return err
		}
	}

	return nil
}

// InitLoadBalancer init the default tcp and udp cluster loadbalancer
func (c *Controller) initLoadBalancer() error {
	vpcs, err := c.vpcsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpc: %v", err)
		return err
	}

	for _, cachedVpc := range vpcs {
		vpc := cachedVpc.DeepCopy()
		vpcLb := c.GenVpcLoadBalancer(vpc.Name)
		if err = c.initLB(vpcLb.TCPLoadBalancer, string(v1.ProtocolTCP), false); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.initLB(vpcLb.TCPSessLoadBalancer, string(v1.ProtocolTCP), true); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.initLB(vpcLb.UDPLoadBalancer, string(v1.ProtocolUDP), false); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.initLB(vpcLb.UDPSessLoadBalancer, string(v1.ProtocolUDP), true); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.initLB(vpcLb.SctpLoadBalancer, string(v1.ProtocolSCTP), false); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.initLB(vpcLb.SctpSessLoadBalancer, string(v1.ProtocolSCTP), true); err != nil {
			klog.Error(err)
			return err
		}

		vpc.Status.TCPLoadBalancer = vpcLb.TCPLoadBalancer
		vpc.Status.TCPSessionLoadBalancer = vpcLb.TCPSessLoadBalancer
		vpc.Status.UDPLoadBalancer = vpcLb.UDPLoadBalancer
		vpc.Status.UDPSessionLoadBalancer = vpcLb.UDPSessLoadBalancer
		vpc.Status.SctpLoadBalancer = vpcLb.SctpLoadBalancer
		vpc.Status.SctpSessionLoadBalancer = vpcLb.SctpSessLoadBalancer
		bytes, err := vpc.Status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		}
		_, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(), vpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
		if err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) InitIPAM() error {
	start := time.Now()
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnet: %v", err)
		return err
	}
	for _, subnet := range subnets {
		if err := c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, subnet.Spec.ExcludeIps); err != nil {
			klog.Errorf("failed to init subnet %s: %v", subnet.Name, err)
		}

		u2oInterconnName := fmt.Sprintf(util.U2OInterconnName, subnet.Spec.Vpc, subnet.Name)
		u2oInterconnLrpName := fmt.Sprintf("%s-%s", subnet.Spec.Vpc, subnet.Name)
		if subnet.Status.U2OInterconnectionIP != "" {
			if _, _, _, err = c.ipam.GetStaticAddress(u2oInterconnName, u2oInterconnLrpName, subnet.Status.U2OInterconnectionIP, nil, subnet.Name, true); err != nil {
				klog.Errorf("failed to init subnet %q u2o interconnection ip to ipam %v", subnet.Name, err)
			}
		}
	}

	ippools, err := c.ippoolLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ippool: %v", err)
		return err
	}
	for _, ippool := range ippools {
		if err = c.ipam.AddOrUpdateIPPool(ippool.Spec.Subnet, ippool.Name, ippool.Spec.IPs); err != nil {
			klog.Errorf("failed to init ippool %s: %v", ippool.Name, err)
		}
	}

	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list pods: %v", err)
		return err
	}

	ips, err := c.ipsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list IPs: %v", err)
		return err
	}

	for _, ip := range ips {
		// recover sts and kubevirt vm ip, other ip recover in later pod loop
		if ip.Spec.PodType != util.StatefulSet && ip.Spec.PodType != util.VM {
			continue
		}

		var ipamKey string
		if ip.Spec.Namespace != "" {
			ipamKey = fmt.Sprintf("%s/%s", ip.Spec.Namespace, ip.Spec.PodName)
		} else {
			ipamKey = fmt.Sprintf("node-%s", ip.Spec.PodName)
		}
		if _, _, _, err = c.ipam.GetStaticAddress(ipamKey, ip.Name, ip.Spec.IPAddress, &ip.Spec.MacAddress, ip.Spec.Subnet, true); err != nil {
			klog.Errorf("failed to init IPAM from IP CR %s: %v", ip.Name, err)
		}
	}

	for _, pod := range pods {
		if pod.Spec.HostNetwork {
			continue
		}

		isAlive := isPodAlive(pod)
		isStsPod, _ := isStatefulSetPod(pod)
		if !isAlive && !isStsPod {
			continue
		}

		podNets, err := c.getPodKubeovnNets(pod)
		if err != nil {
			klog.Errorf("failed to get pod kubeovn nets %s.%s address %s: %v", pod.Name, pod.Namespace, pod.Annotations[util.IPAddressAnnotation], err)
			continue
		}

		podType := getPodType(pod)
		podName := c.getNameByPod(pod)
		key := fmt.Sprintf("%s/%s", pod.Namespace, podName)
		for _, podNet := range podNets {
			if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "true" {
				portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
				ip := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)]
				mac := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)]
				_, _, _, err := c.ipam.GetStaticAddress(key, portName, ip, &mac, podNet.Subnet.Name, true)
				if err != nil {
					klog.Errorf("failed to init pod %s.%s address %s: %v", podName, pod.Namespace, pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)], err)
				} else {
					err = c.createOrUpdateIPCR(portName, podName, ip, mac, podNet.Subnet.Name, pod.Namespace, pod.Spec.NodeName, podType)
					if err != nil {
						klog.Errorf("failed to create/update ips CR %s.%s with ip address %s: %v", podName, pod.Namespace, ip, err)
					}
				}

				// Append ExternalIds is added in v1.7, used for upgrading from v1.6.3. It should be deleted now since v1.7 is not used anymore.
			}
		}
	}

	vips, err := c.virtualIpsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vips: %v", err)
		return err
	}
	for _, vip := range vips {
		var ipamKey string
		if vip.Spec.Namespace != "" {
			ipamKey = fmt.Sprintf("%s/%s", vip.Spec.Namespace, vip.Name)
		} else {
			ipamKey = vip.Name
		}
		if _, _, _, err = c.ipam.GetStaticAddress(ipamKey, vip.Name, vip.Status.V4ip, &vip.Status.Mac, vip.Spec.Subnet, true); err != nil {
			klog.Errorf("failed to init ipam from vip cr %s: %v", vip.Name, err)
		}
	}

	eips, err := c.iptablesEipsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list EIPs: %v", err)
		return err
	}
	for _, eip := range eips {
		externalNetwork := util.GetExternalNetwork(eip.Spec.ExternalSubnet)
		if _, _, _, err = c.ipam.GetStaticAddress(eip.Name, eip.Name, eip.Status.IP, &eip.Spec.MacAddress, externalNetwork, true); err != nil {
			klog.Errorf("failed to init ipam from iptables eip cr %s: %v", eip.Name, err)
		}
	}

	oeips, err := c.ovnEipsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ovn eips: %v", err)
		return err
	}
	for _, oeip := range oeips {
		if _, _, _, err = c.ipam.GetStaticAddress(oeip.Name, oeip.Name, oeip.Status.V4Ip, &oeip.Status.MacAddress, oeip.Spec.ExternalSubnet, true); err != nil {
			klog.Errorf("failed to init ipam from ovn eip cr %s: %v", oeip.Name, err)
		}
	}

	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes: %v", err)
		return err
	}
	for _, node := range nodes {
		if node.Annotations[util.AllocatedAnnotation] == "true" {
			portName := fmt.Sprintf("node-%s", node.Name)
			mac := node.Annotations[util.MacAddressAnnotation]
			v4IP, v6IP, _, err := c.ipam.GetStaticAddress(portName, portName,
				node.Annotations[util.IPAddressAnnotation], &mac,
				node.Annotations[util.LogicalSwitchAnnotation], true)
			if err != nil {
				klog.Errorf("failed to init node %s.%s address %s: %v", node.Name, node.Namespace, node.Annotations[util.IPAddressAnnotation], err)
			}
			if v4IP != "" && v6IP != "" {
				node.Annotations[util.IPAddressAnnotation] = util.GetStringIP(v4IP, v6IP)
			}
		}
	}

	klog.Infof("take %.2f seconds to initialize IPAM", time.Since(start).Seconds())
	return nil
}

func (c *Controller) initDefaultProviderNetwork() error {
	_, err := c.providerNetworksLister.Get(c.config.DefaultProviderName)
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get default provider network %s: %v", c.config.DefaultProviderName, err)
		return err
	}

	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get nodes: %v", err)
		return err
	}

	pn := kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name: c.config.DefaultProviderName,
		},
		Spec: kubeovnv1.ProviderNetworkSpec{
			DefaultInterface: c.config.DefaultHostInterface,
			ExchangeLinkName: c.config.DefaultExchangeLinkName,
		},
	}

	excludeAnno := fmt.Sprintf(util.ProviderNetworkExcludeTemplate, c.config.DefaultProviderName)
	interfaceAnno := fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, c.config.DefaultProviderName)
	newNodes := make([]*v1.Node, 0, len(nodes))
	for _, node := range nodes {
		if len(node.Annotations) == 0 {
			continue
		}

		var newNode *v1.Node
		if node.Annotations[excludeAnno] == "true" {
			pn.Spec.ExcludeNodes = append(pn.Spec.ExcludeNodes, node.Name)
			newNode = node.DeepCopy()
		} else if s := node.Annotations[interfaceAnno]; s != "" {
			var index *int
			for i := range pn.Spec.CustomInterfaces {
				if pn.Spec.CustomInterfaces[i].Interface == s {
					index = &i
					break
				}
			}
			if index != nil {
				pn.Spec.CustomInterfaces[*index].Nodes = append(pn.Spec.CustomInterfaces[*index].Nodes, node.Name)
			} else {
				ci := kubeovnv1.CustomInterface{Interface: s, Nodes: []string{node.Name}}
				pn.Spec.CustomInterfaces = append(pn.Spec.CustomInterfaces, ci)
			}
			newNode = node.DeepCopy()
		}
		if newNode != nil {
			delete(newNode.Annotations, excludeAnno)
			delete(newNode.Annotations, interfaceAnno)
			newNodes = append(newNodes, newNode)
		}
	}

	defer func() {
		if err == nil {
			return
		}

		// update nodes only when provider network has been created successfully
		for _, node := range newNodes {
			if _, err := c.config.KubeClient.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{}); err != nil {
				klog.Errorf("failed to update node %s: %v", node.Name, err)
			}
		}
	}()

	_, err = c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Create(context.Background(), &pn, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to create provider network %s: %v", c.config.DefaultProviderName, err)
		return err
	}
	return nil
}

func (c *Controller) initDefaultVlan() error {
	if c.config.NetworkType != util.NetworkTypeVlan {
		return nil
	}

	if err := c.initDefaultProviderNetwork(); err != nil {
		klog.Error(err)
		return err
	}

	_, err := c.vlansLister.Get(c.config.DefaultVlanName)
	if err == nil {
		return nil
	}

	if !k8serrors.IsNotFound(err) {
		klog.Errorf("get default vlan %s failed: %v", c.config.DefaultVlanName, err)
		return err
	}

	if c.config.DefaultVlanID < 0 || c.config.DefaultVlanID > 4095 {
		return fmt.Errorf("the default vlan id is not between 1-4095")
	}

	defaultVlan := kubeovnv1.Vlan{
		ObjectMeta: metav1.ObjectMeta{Name: c.config.DefaultVlanName},
		Spec: kubeovnv1.VlanSpec{
			ID:       c.config.DefaultVlanID,
			Provider: c.config.DefaultProviderName,
		},
	}

	_, err = c.config.KubeOvnClient.KubeovnV1().Vlans().Create(context.Background(), &defaultVlan, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to create vlan %s: %v", defaultVlan.Name, err)
		return err
	}
	return nil
}

func (c *Controller) syncIPCR() error {
	klog.Info("start to sync ips")
	ips, err := c.ipsLister.List(labels.Everything())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	ipMap := strset.New(c.getVMLsps()...)

	for _, ipCR := range ips {
		ip := ipCR.DeepCopy()
		if ip.DeletionTimestamp != nil && slices.Contains(ip.Finalizers, util.KubeOVNControllerFinalizer) {
			klog.Infof("enqueue update for deleting ip %s", ip.Name)
			c.updateIPQueue.Add(ip.Name)
		}
		changed := false
		if ipMap.Has(ip.Name) && ip.Spec.PodType == "" {
			ip.Spec.PodType = util.VM
			changed = true
		}

		v4IP, v6IP := util.SplitStringIP(ip.Spec.IPAddress)
		if ip.Spec.V4IPAddress == v4IP && ip.Spec.V6IPAddress == v6IP && !changed {
			continue
		}

		ip.Spec.V4IPAddress = v4IP
		ip.Spec.V6IPAddress = v6IP
		_, err := c.config.KubeOvnClient.KubeovnV1().IPs().Update(context.Background(), ip, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to sync crd ip %s: %v", ip.Spec.IPAddress, err)
			return err
		}
	}
	return nil
}

func (c *Controller) syncSubnetCR() error {
	klog.Info("start to sync subnets")
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	for _, cachedSubnet := range subnets {
		subnet := cachedSubnet.DeepCopy()
		if util.CheckProtocol(subnet.Spec.CIDRBlock) == kubeovnv1.ProtocolDual {
			subnet, err = c.calcDualSubnetStatusIP(subnet)
		} else {
			subnet, err = c.calcSubnetStatusIP(subnet)
		}
		if err != nil {
			klog.Errorf("failed to calculate subnet %s used ip: %v", cachedSubnet.Name, err)
			return err
		}

		// only sync subnet spec enableEcmp when subnet.Spec.EnableEcmp is false and c.config.EnableEcmp is true
		if subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType && !subnet.Spec.EnableEcmp && subnet.Spec.EnableEcmp != c.config.EnableEcmp {
			subnet, err = c.subnetsLister.Get(subnet.Name)
			if err != nil {
				klog.Errorf("failed to get subnet %s: %v", subnet.Name, err)
				return err
			}

			subnet.Spec.EnableEcmp = c.config.EnableEcmp
			if _, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Update(context.Background(), subnet, metav1.UpdateOptions{}); err != nil {
				klog.Errorf("failed to sync subnet spec enableEcmp with kube-ovn-controller config enableEcmp %s: %v", subnet.Name, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) syncVpcNatGatewayCR() error {
	klog.Info("start to sync crd vpc nat gw")
	// get vpc nat gateway enable state
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcNatGatewayConfig)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get %s, %v", util.VpcNatGatewayConfig, err)
		return err
	}
	if k8serrors.IsNotFound(err) || cm.Data["enable-vpc-nat-gw"] == "false" {
		return nil
	}
	// get vpc nat gateway image
	cm, err = c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcNatConfig)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Errorf("configMap of %s not set, %v", util.VpcNatConfig, err)
			return err
		}
		klog.Errorf("failed to get %s, %v", util.VpcNatConfig, err)
		return err
	}

	if cm.Data["image"] == "" {
		err = errors.New("image of vpc-nat-gateway not set")
		klog.Error(err)
		return err
	}

	gws, err := c.vpcNatGatewayLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpc nat gateway, %v", err)
		return err
	}
	for _, gw := range gws {
		if err := c.updateCrdNatGwLabels(gw.Name, ""); err != nil {
			klog.Errorf("failed to update nat gw %s: %v", gw.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) syncVlanCR() error {
	klog.Info("start to sync vlans")
	vlans, err := c.vlansLister.List(labels.Everything())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	for _, vlan := range vlans {
		var needUpdate bool
		newVlan := vlan.DeepCopy()
		if newVlan.Spec.VlanID != 0 && newVlan.Spec.ID == 0 {
			newVlan.Spec.ID = newVlan.Spec.VlanID
			newVlan.Spec.VlanID = 0
			needUpdate = true
		}
		if newVlan.Spec.ProviderInterfaceName != "" && newVlan.Spec.Provider == "" {
			newVlan.Spec.Provider = newVlan.Spec.ProviderInterfaceName
			newVlan.Spec.ProviderInterfaceName = ""
			needUpdate = true
		}
		if needUpdate {
			if _, err = c.config.KubeOvnClient.KubeovnV1().Vlans().Update(context.Background(), newVlan, metav1.UpdateOptions{}); err != nil {
				klog.Errorf("failed to update spec of vlan %s: %v", newVlan.Name, err)
				return err
			}
		}
	}

	return nil
}

func (c *Controller) migrateNodeRoute(af int, node, ip, nexthop string) error {
	// default vpc use static route in old version, migrate to policy route
	var (
		match       = fmt.Sprintf("ip%d.dst == %s", af, ip)
		action      = kubeovnv1.PolicyRouteActionReroute
		externalIDs = map[string]string{
			"vendor": util.CniTypeName,
			"node":   node,
		}
	)
	klog.V(3).Infof("add policy route for router: %s, priority: %d, match %s, action %s, nexthop %s, externalID %v",
		c.config.ClusterRouter, util.NodeRouterPolicyPriority, match, action, nexthop, externalIDs)
	if err := c.addPolicyRouteToVpc(
		c.config.ClusterRouter,
		&kubeovnv1.PolicyRoute{
			Priority:  util.NodeRouterPolicyPriority,
			Match:     match,
			Action:    action,
			NextHopIP: nexthop,
		},
		externalIDs,
	); err != nil {
		klog.Errorf("failed to add logical router policy for node %s: %v", node, err)
		return err
	}

	if err := c.deleteStaticRouteFromVpc(
		c.config.ClusterRouter,
		util.MainRouteTable,
		ip,
		"",
		kubeovnv1.PolicyDst,
	); err != nil {
		klog.Errorf("failed to delete obsolete static route for node %s: %v", node, err)
		return err
	}

	asName := nodeUnderlayAddressSetName(node, af)
	obsoleteMatch := fmt.Sprintf("ip%d.dst == %s && ip%d.src != $%s", af, ip, af, asName)
	klog.V(3).Infof("delete policy route for router: %s, priority: %d, match %s", c.config.ClusterRouter, util.NodeRouterPolicyPriority, obsoleteMatch)
	if err := c.deletePolicyRouteFromVpc(c.config.ClusterRouter, util.NodeRouterPolicyPriority, obsoleteMatch); err != nil {
		klog.Errorf("failed to delete obsolete logical router policy for node %s: %v", node, err)
		return err
	}

	if err := c.OVNNbClient.DeleteAddressSet(asName); err != nil {
		klog.Errorf("delete obsolete address set %s for node %s: %v", asName, node, err)
		return err
	}

	return nil
}

func (c *Controller) syncNodeRoutes() error {
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes: %v", err)
		return err
	}
	for _, node := range nodes {
		if node.Annotations[util.AllocatedAnnotation] != "true" {
			continue
		}
		nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*node)
		joinAddrV4, joinAddrV6 := util.SplitStringIP(node.Annotations[util.IPAddressAnnotation])
		if nodeIPv4 != "" && joinAddrV4 != "" {
			if err = c.migrateNodeRoute(4, node.Name, nodeIPv4, joinAddrV4); err != nil {
				klog.Errorf("failed to migrate IPv4 route for node %s: %v", node.Name, err)
			}
		}
		if nodeIPv6 != "" && joinAddrV6 != "" {
			if err = c.migrateNodeRoute(6, node.Name, nodeIPv6, joinAddrV6); err != nil {
				klog.Errorf("failed to migrate IPv6 route for node %s: %v", node.Name, err)
			}
		}
	}

	if err := c.addNodeGatewayStaticRoute(); err != nil {
		klog.Errorf("failed to add static route for node gateway")
		return err
	}
	return nil
}

func (c *Controller) initNodeChassis() error {
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes: %v", err)
		return err
	}
	chassises, err := c.OVNSbClient.GetKubeOvnChassisses()
	if err != nil {
		klog.Errorf("failed to get chassis nodes: %v", err)
		return err
	}
	chassisNodes := make(map[string]string, len(*chassises))
	for _, chassis := range *chassises {
		chassisNodes[chassis.Name] = chassis.Hostname
	}
	for _, node := range nodes {
		if err := c.UpdateChassisTag(node); err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

func updateFinalizers(c client.Client, list client.ObjectList, getObjectItem func(int) (client.Object, client.Object)) error {
	if err := c.List(context.Background(), list); err != nil {
		klog.Errorf("failed to list objects: %v", err)
		return err
	}

	var i int
	var cachedObj, patchedObj client.Object
	for {
		if cachedObj, patchedObj = getObjectItem(i); cachedObj == nil {
			break
		}
		if !controllerutil.ContainsFinalizer(cachedObj, util.DepreciatedFinalizerName) {
			i++
			continue
		}

		controllerutil.RemoveFinalizer(patchedObj, util.DepreciatedFinalizerName)
		controllerutil.AddFinalizer(patchedObj, util.KubeOVNControllerFinalizer)
		if err := c.Patch(context.Background(), patchedObj, client.MergeFrom(cachedObj)); err != nil && !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to sync finalizers for %s %s: %v",
				patchedObj.GetObjectKind().GroupVersionKind().Kind,
				cache.MetaObjectToName(patchedObj), err)
			return err
		}
		i++
	}

	return nil
}

func (c *Controller) syncFinalizers() error {
	cl, err := client.New(config.GetConfigOrDie(), client.Options{})
	if err != nil {
		klog.Errorf("failed to create client: %v", err)
		return err
	}

	// migrate depreciated finalizer to new finalizer
	klog.Info("start to sync finalizers")
	if err := c.syncIPFinalizer(cl); err != nil {
		klog.Errorf("failed to sync ip finalizer: %v", err)
		return err
	}
	if err := c.syncOvnDnatFinalizer(cl); err != nil {
		klog.Errorf("failed to sync ovn dnat finalizer: %v", err)
		return err
	}
	if err := c.syncOvnEipFinalizer(cl); err != nil {
		klog.Errorf("failed to sync ovn eip finalizer: %v", err)
		return err
	}
	if err := c.syncOvnFipFinalizer(cl); err != nil {
		klog.Errorf("failed to sync ovn fip finalizer: %v", err)
		return err
	}
	if err := c.syncOvnSnatFinalizer(cl); err != nil {
		klog.Errorf("failed to sync ovn snat finalizer: %v", err)
		return err
	}
	if err := c.syncQoSPolicyFinalizer(cl); err != nil {
		klog.Errorf("failed to sync qos policy finalizer: %v", err)
		return err
	}
	if err := c.syncSubnetFinalizer(cl); err != nil {
		klog.Errorf("failed to sync subnet finalizer: %v", err)
		return err
	}
	if err := c.syncVipFinalizer(cl); err != nil {
		klog.Errorf("failed to sync vip finalizer: %v", err)
		return err
	}
	if err := c.syncIptablesEipFinalizer(cl); err != nil {
		klog.Errorf("failed to sync iptables eip finalizer: %v", err)
		return err
	}
	if err := c.syncIptablesFipFinalizer(cl); err != nil {
		klog.Errorf("failed to sync iptables fip finalizer: %v", err)
		return err
	}
	if err := c.syncIptablesDnatFinalizer(cl); err != nil {
		klog.Errorf("failed to sync iptables dnat finalizer: %v", err)
		return err
	}
	if err := c.syncIptablesSnatFinalizer(cl); err != nil {
		klog.Errorf("failed to sync iptables snat finalizer: %v", err)
		return err
	}
	klog.Info("sync finalizers done")
	return nil
}

func (c *Controller) ReplaceFinalizer(cachedObj client.Object) ([]byte, error) {
	if controllerutil.ContainsFinalizer(cachedObj, util.DepreciatedFinalizerName) {
		newObj := cachedObj.DeepCopyObject().(client.Object)
		controllerutil.RemoveFinalizer(newObj, util.DepreciatedFinalizerName)
		controllerutil.AddFinalizer(newObj, util.KubeOVNControllerFinalizer)
		patch, err := util.GenerateMergePatchPayload(cachedObj, newObj)
		if err != nil {
			klog.Errorf("failed to generate patch payload for %s, %v", newObj.GetName(), err)
			return nil, err
		}
		return patch, nil
	}
	return nil, nil
}
