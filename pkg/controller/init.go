package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) InitOVN() error {
	if err := c.initClusterRouter(); err != nil {
		klog.Errorf("init cluster router failed: %v", err)
		return err
	}

	if c.config.EnableLb {
		if err := c.initLoadBalancer(); err != nil {
			klog.Errorf("init load balancer failed: %v", err)
			return err
		}
		v4Svc, _ := util.SplitStringIP(c.config.ServiceClusterIPRange)
		if v4Svc != "" {
			if err := c.ovnClient.SetLBCIDR(v4Svc); err != nil {
				klog.Errorf("init load balancer svc cidr failed: %v", err)
				return err
			}
		}
	}

	if err := c.initDefaultVlan(); err != nil {
		klog.Errorf("init default vlan failed: %v", err)
		return err
	}

	if err := c.initNodeSwitch(); err != nil {
		klog.Errorf("init node switch failed: %v", err)
		return err
	}

	if err := c.initDefaultLogicalSwitch(); err != nil {
		klog.Errorf("init default switch failed: %v", err)
		return err
	}

	return nil
}

func (c *Controller) InitDefaultVpc() error {
	cachedVpc, err := c.vpcsLister.Get(util.DefaultVpc)
	if err != nil {
		cachedVpc = &kubeovnv1.Vpc{}
		cachedVpc.Name = util.DefaultVpc
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
		vpc.Status.TcpLoadBalancer = c.config.ClusterTcpLoadBalancer
		vpc.Status.TcpSessionLoadBalancer = c.config.ClusterTcpSessionLoadBalancer
		vpc.Status.UdpLoadBalancer = c.config.ClusterUdpLoadBalancer
		vpc.Status.UdpSessionLoadBalancer = c.config.ClusterUdpSessionLoadBalancer
		vpc.Status.SctpLoadBalancer = c.config.ClusterSctpLoadBalancer
		vpc.Status.SctpSessionLoadBalancer = c.config.ClusterSctpSessionLoadBalancer
	}
	vpc.Status.Standby = true
	vpc.Status.Default = true

	bytes, err := vpc.Status.Bytes()
	if err != nil {
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
	subnet, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Get(context.Background(), c.config.DefaultLogicalSwitch, metav1.GetOptions{})
	if err == nil {
		if subnet != nil && util.CheckProtocol(c.config.DefaultCIDR) != util.CheckProtocol(subnet.Spec.CIDRBlock) {
			// single-stack upgrade to dual-stack
			if util.CheckProtocol(c.config.DefaultCIDR) == kubeovnv1.ProtocolDual {
				subnet := subnet.DeepCopy()
				subnet.Spec.CIDRBlock = c.config.DefaultCIDR
				if err := formatSubnet(subnet, c); err != nil {
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
			Vpc:                 util.DefaultVpc,
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
	subnet, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Get(context.Background(), c.config.NodeSwitch, metav1.GetOptions{})
	if err == nil {
		if util.CheckProtocol(c.config.NodeSwitchCIDR) == kubeovnv1.ProtocolDual && util.CheckProtocol(subnet.Spec.CIDRBlock) != kubeovnv1.ProtocolDual {
			// single-stack upgrade to dual-stack
			subnet := subnet.DeepCopy()
			subnet.Spec.CIDRBlock = c.config.NodeSwitchCIDR
			if err := formatSubnet(subnet, c); err != nil {
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
			Vpc:                    util.DefaultVpc,
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
	return c.ovnClient.CreateLogicalRouter(c.config.ClusterRouter)
}

func (c *Controller) initLB(name, protocol string, sessionAffinity bool) error {
	protocol = strings.ToLower(protocol)

	var selectFields string
	if sessionAffinity {
		selectFields = string(ovnnb.LoadBalancerSelectionFieldsIPSrc)
	}

	if err := c.ovnClient.CreateLoadBalancer(name, protocol, selectFields); err != nil {
		klog.Errorf("create load balancer %s: %v", name, err)
		return err
	}

	if sessionAffinity {
		if err := c.ovnClient.SetLoadBalancerAffinityTimeout(name, util.DefaultServiceSessionStickinessTimeout); err != nil {
			klog.Errorf("failed to set affinity timeout of %s load balancer %s: %v", protocol, name, err)
			return err
		}
	}

	return nil
}

// InitLoadBalancer init the default tcp and udp cluster loadbalancer
func (c *Controller) initLoadBalancer() error {
	vpcs, err := c.config.KubeOvnClient.KubeovnV1().Vpcs().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list vpc: %v", err)
		return err
	}

	for _, cachedVpc := range vpcs.Items {
		vpc := cachedVpc.DeepCopy()
		vpcLb := c.GenVpcLoadBalancer(vpc.Name)
		if err = c.initLB(vpcLb.TcpLoadBalancer, string(v1.ProtocolTCP), false); err != nil {
			return err
		}
		if err = c.initLB(vpcLb.TcpSessLoadBalancer, string(v1.ProtocolTCP), true); err != nil {
			return err
		}
		if err = c.initLB(vpcLb.UdpLoadBalancer, string(v1.ProtocolUDP), false); err != nil {
			return err
		}
		if err = c.initLB(vpcLb.UdpSessLoadBalancer, string(v1.ProtocolUDP), true); err != nil {
			return err
		}
		if err = c.initLB(vpcLb.SctpLoadBalancer, string(v1.ProtocolSCTP), false); err != nil {
			return err
		}
		if err = c.initLB(vpcLb.SctpSessLoadBalancer, string(v1.ProtocolSCTP), true); err != nil {
			return err
		}

		vpc.Status.TcpLoadBalancer = vpcLb.TcpLoadBalancer
		vpc.Status.TcpSessionLoadBalancer = vpcLb.TcpSessLoadBalancer
		vpc.Status.UdpLoadBalancer = vpcLb.UdpLoadBalancer
		vpc.Status.UdpSessionLoadBalancer = vpcLb.UdpSessLoadBalancer
		vpc.Status.SctpLoadBalancer = vpcLb.SctpLoadBalancer
		vpc.Status.SctpSessionLoadBalancer = vpcLb.SctpSessLoadBalancer
		bytes, err := vpc.Status.Bytes()
		if err != nil {
			return err
		}
		_, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(), vpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
		if err != nil {
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
			if _, _, _, err = c.ipam.GetStaticAddress(u2oInterconnName, u2oInterconnLrpName, subnet.Status.U2OInterconnectionIP, "", subnet.Name, true); err != nil {
				klog.Errorf("failed to init subnet u2o interonnection ip to ipam %v", subnet.Name, err)
			}
		}
	}

	lsList, err := c.ovnClient.ListLogicalSwitch(false, nil)
	if err != nil {
		klog.Errorf("failed to list LS: %v", err)
		return err
	}
	lsPortsMap := make(map[string]map[string]struct{}, len(lsList))
	for _, ls := range lsList {
		lsPortsMap[ls.Name] = make(map[string]struct{}, len(ls.Ports))
		for _, port := range ls.Ports {
			lsPortsMap[ls.Name][port] = struct{}{}
		}
	}

	lspList, err := c.ovnClient.ListLogicalSwitchPortsWithLegacyExternalIDs()
	if err != nil {
		klog.Errorf("failed to list LSP: %v", err)
		return err
	}
	lspWithoutVendor := make(map[string]struct{}, len(lspList))
	lspWithoutLS := make(map[string]string, len(lspList))
	for _, lsp := range lspList {
		if len(lsp.ExternalIDs) == 0 || lsp.ExternalIDs["vendor"] == "" {
			lspWithoutVendor[lsp.Name] = struct{}{}
		}
		if len(lsp.ExternalIDs) == 0 || lsp.ExternalIDs[logicalSwitchKey] == "" {
			lspWithoutLS[lsp.Name] = lsp.UUID
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

	ipsMap := make(map[string]*kubeovnv1.IP, len(ips))
	for _, ip := range ips {
		ipsMap[ip.Name] = ip
		// recover sts and kubevirt vm ip, other ip recover in later pod loop
		if ip.Spec.PodType != "StatefulSet" && ip.Spec.PodType != util.Vm {
			continue
		}

		var ipamKey string
		if ip.Spec.Namespace != "" {
			ipamKey = fmt.Sprintf("%s/%s", ip.Spec.Namespace, ip.Spec.PodName)
		} else {
			ipamKey = fmt.Sprintf("node-%s", ip.Spec.PodName)
		}
		if _, _, _, err = c.ipam.GetStaticAddress(ipamKey, ip.Name, ip.Spec.IPAddress, ip.Spec.MacAddress, ip.Spec.Subnet, true); err != nil {
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
			klog.Errorf("failed to get pod kubeovn nets %s.%s address %s: %v", pod.Name, pod.Namespace, pod.Annotations[util.IpAddressAnnotation], err)
			continue
		}

		podType := getPodType(pod)
		podName := c.getNameByPod(pod)
		key := fmt.Sprintf("%s/%s", pod.Namespace, podName)
		for _, podNet := range podNets {
			if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "true" {
				portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
				ip := pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)]
				mac := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)]
				subnet := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, podNet.ProviderName)]
				_, _, _, err := c.ipam.GetStaticAddress(key, portName, ip, mac, subnet, true)
				if err != nil {
					klog.Errorf("failed to init pod %s.%s address %s: %v", podName, pod.Namespace, pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)], err)
				} else {
					ipCR := ipsMap[portName]
					err = c.createOrUpdateCrdIPs(podName, ip, mac, subnet, pod.Namespace, pod.Spec.NodeName, podNet.ProviderName, podType, &ipCR)
					if err != nil {
						klog.Errorf("failed to create/update ips CR %s.%s with ip address %s: %v", podName, pod.Namespace, ip, err)
					}
				}
				if podNet.ProviderName == util.OvnProvider || strings.HasSuffix(podNet.ProviderName, util.OvnProvider) {
					externalIDs := make(map[string]string, 3)
					if _, ok := lspWithoutVendor[portName]; ok {
						externalIDs["vendor"] = util.CniTypeName
						externalIDs["pod"] = fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
					}
					if uuid := lspWithoutLS[portName]; uuid != "" {
						for ls, ports := range lsPortsMap {
							if _, ok := ports[uuid]; ok {
								externalIDs[logicalSwitchKey] = ls
								break
							}
						}
					}

					if err = c.initAppendLspExternalIds(portName, externalIDs); err != nil {
						klog.Errorf("failed to append external-ids for logical switch port %s: %v", portName, err)
					}
				}
			}
		}
	}

	vips, err := c.virtualIpsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list VIPs: %v", err)
		return err
	}
	for _, vip := range vips {
		var ipamKey string
		if vip.Spec.Namespace != "" {
			ipamKey = fmt.Sprintf("%s/%s", vip.Spec.Namespace, vip.Name)
		} else {
			ipamKey = vip.Name
		}
		if _, _, _, err = c.ipam.GetStaticAddress(ipamKey, vip.Name, vip.Status.V4ip, vip.Status.Mac, vip.Spec.Subnet, true); err != nil {
			klog.Errorf("failed to init ipam from vip cr %s: %v", vip.Name, err)
		}
	}

	eips, err := c.iptablesEipsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list EIPs: %v", err)
		return err
	}
	for _, eip := range eips {
		if _, _, _, err = c.ipam.GetStaticAddress(eip.Name, eip.Name, eip.Status.IP, eip.Spec.MacAddress, util.VpcExternalNet, true); err != nil {
			klog.Errorf("failed to init ipam from iptables eip cr %s: %v", eip.Name, err)
		}
	}
	oeips, err := c.ovnEipsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ovn eips: %v", err)
		return err
	}
	for _, oeip := range oeips {
		if _, _, _, err = c.ipam.GetStaticAddress(oeip.Name, oeip.Name, oeip.Status.V4Ip, oeip.Status.MacAddress, oeip.Spec.ExternalSubnet, true); err != nil {
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
			v4IP, v6IP, _, err := c.ipam.GetStaticAddress(portName, portName,
				node.Annotations[util.IpAddressAnnotation],
				node.Annotations[util.MacAddressAnnotation],
				node.Annotations[util.LogicalSwitchAnnotation], true)
			if err != nil {
				klog.Errorf("failed to init node %s.%s address %s: %v", node.Name, node.Namespace, node.Annotations[util.IpAddressAnnotation], err)
			}
			if v4IP != "" && v6IP != "" {
				node.Annotations[util.IpAddressAnnotation] = util.GetStringIP(v4IP, v6IP)
			}

			externalIDs := make(map[string]string, 2)
			if _, ok := lspWithoutVendor[portName]; ok {
				externalIDs["vendor"] = util.CniTypeName
			}
			if uuid := lspWithoutLS[portName]; uuid != "" {
				for ls, ports := range lsPortsMap {
					if _, ok := ports[uuid]; ok {
						externalIDs[logicalSwitchKey] = ls
						break
					}
				}
			}

			if err = c.initAppendLspExternalIds(portName, externalIDs); err != nil {
				klog.Errorf("failed to append external-ids for logical switch port %s: %v", portName, err)
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
		klog.Errorf("failed to create vlan %s: %v", defaultVlan, err)
		return err
	}
	return nil
}

func (c *Controller) initSyncCrdIPs() error {
	klog.Info("start to sync ips")
	ips, err := c.config.KubeOvnClient.KubeovnV1().IPs().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	vmLsps := c.getVmLsps()
	ipMap := make(map[string]struct{}, len(vmLsps))
	for _, vmLsp := range vmLsps {
		ipMap[vmLsp] = struct{}{}
	}

	for _, ipCr := range ips.Items {
		ip := ipCr.DeepCopy()
		changed := false
		if _, ok := ipMap[ip.Name]; ok && ip.Spec.PodType == "" {
			ip.Spec.PodType = util.Vm
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

func (c *Controller) initSyncCrdSubnets() error {
	klog.Info("start to sync subnets")
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	for _, orisubnet := range subnets {
		subnet := orisubnet.DeepCopy()
		if util.CheckProtocol(subnet.Spec.CIDRBlock) == kubeovnv1.ProtocolDual {
			err = calcDualSubnetStatusIP(subnet, c)
		} else {
			err = calcSubnetStatusIP(subnet, c)
		}
		if err != nil {
			klog.Errorf("failed to calculate subnet %s used ip: %v", subnet.Name, err)
			return err
		}

		// only sync subnet spec enableEcmp when subnet.Spec.EnableEcmp is false and c.config.EnableEcmp is true
		if subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType && !subnet.Spec.EnableEcmp && subnet.Spec.EnableEcmp != c.config.EnableEcmp {
			subnet, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Get(context.Background(), subnet.Name, metav1.GetOptions{})
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

func (c *Controller) initSyncCrdVpcNatGw() error {
	klog.Info("start to sync crd vpc nat gw")
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcNatGatewayConfig)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get ovn-vpc-nat-gw-config, %v", err)
		return err
	}
	if k8serrors.IsNotFound(err) || cm.Data["enable-vpc-nat-gw"] == "false" || cm.Data["image"] == "" {
		return nil
	}
	gws, err := c.vpcNatGatewayLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpc nat gateway, %v", err)
		return err
	}
	for _, gw := range gws {
		if err := c.updateCrdNatGw(gw.Name); err != nil {
			klog.Errorf("failed to update nat gw: %v", gw.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) initSyncCrdVlans() error {
	klog.Info("start to sync vlans")
	vlans, err := c.vlansLister.List(labels.Everything())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	for _, vlan := range vlans {
		var needUpdate bool
		newVlan := vlan.DeepCopy()
		if newVlan.Spec.VlanId != 0 && newVlan.Spec.ID == 0 {
			newVlan.Spec.ID = newVlan.Spec.VlanId
			newVlan.Spec.VlanId = 0
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
	// migrate from old version static route to policy route
	match := fmt.Sprintf("ip%d.dst == %s", af, ip)
	consistent, err := c.ovnLegacyClient.CheckPolicyRouteNexthopConsistent(match, nexthop, util.NodeRouterPolicyPriority)
	if err != nil {
		return err
	}
	if consistent {
		klog.V(3).Infof("node policy route migrated")
		return nil
	}
	if err := c.ovnLegacyClient.DeleteStaticRoute(ip, c.config.ClusterRouter); err != nil {
		klog.Errorf("failed to delete obsolete static route for node %s: %v", node, err)
		return err
	}

	asName := nodeUnderlayAddressSetName(node, af)
	obsoleteMatch := fmt.Sprintf("ip%d.dst == %s && ip%d.src != $%s", af, ip, af, asName)
	klog.Infof("delete policy route for router: %s, priority: %d, match %s", c.config.ClusterRouter, util.NodeRouterPolicyPriority, obsoleteMatch)
	if err := c.ovnLegacyClient.DeletePolicyRoute(c.config.ClusterRouter, util.NodeRouterPolicyPriority, obsoleteMatch); err != nil {
		klog.Errorf("failed to delete obsolete logical router policy for node %s: %v", node, err)
		return err
	}

	if err := c.ovnLegacyClient.DeleteAddressSet(asName); err != nil {
		klog.Errorf("failed to delete obsolete address set %s for node %s: %v", asName, node, err)
		return err
	}

	externalIDs := map[string]string{
		"vendor": util.CniTypeName,
		"node":   node,
	}
	klog.Infof("add policy route for router: %s, priority: %d, match %s, action %s, nexthop %s, extrenalID %v",
		c.config.ClusterRouter, util.NodeRouterPolicyPriority, match, "reroute", nexthop, externalIDs)
	if err := c.ovnLegacyClient.AddPolicyRoute(c.config.ClusterRouter, util.NodeRouterPolicyPriority, match, "reroute", nexthop, externalIDs); err != nil {
		klog.Errorf("failed to add logical router policy for node %s: %v", node, err)
		return err
	}

	return nil
}

func (c *Controller) initNodeRoutes() error {
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
		joinAddrV4, joinAddrV6 := util.SplitStringIP(node.Annotations[util.IpAddressAnnotation])
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

	return nil
}

func (c *Controller) initAppendLspExternalIds(portName string, externalIDs map[string]string) error {
	if err := c.ovnClient.SetLogicalSwitchPortExternalIds(portName, externalIDs); err != nil {
		klog.Errorf("set lsp external_ids for logical switch port %s: %v", portName, err)
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

	for _, node := range nodes {
		chassisName := node.Annotations[util.ChassisAnnotation]
		if chassisName != "" {
			exist, err := c.ovnLegacyClient.ChassisExist(chassisName)
			if err != nil {
				klog.Errorf("failed to check chassis exist: %v", err)
				return err
			}
			if exist {
				err = c.ovnLegacyClient.InitChassisNodeTag(chassisName, node.Name)
				if err != nil {
					klog.Errorf("failed to set chassis nodeTag: %v", err)
					return err
				}
			}
		}
	}
	return nil
}
