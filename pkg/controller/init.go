package controller

import (
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
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
	vpc, err := c.vpcsLister.Get(util.DefaultVpc)
	if err != nil {
		vpc = &kubeovnv1.Vpc{}
		vpc.Name = util.DefaultVpc
		vpc, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Create(context.Background(), vpc, metav1.CreateOptions{})
		if err != nil {
			klog.Errorf("init default vpc failed: %v", err)
			return err
		}
	}

	vpc.Status.DefaultLogicalSwitch = c.config.DefaultLogicalSwitch
	vpc.Status.Router = c.config.ClusterRouter
	if c.config.EnableLb {
		vpc.Status.TcpLoadBalancer = c.config.ClusterTcpLoadBalancer
		vpc.Status.TcpSessionLoadBalancer = c.config.ClusterTcpSessionLoadBalancer
		vpc.Status.UdpLoadBalancer = c.config.ClusterUdpLoadBalancer
		vpc.Status.UdpSessionLoadBalancer = c.config.ClusterUdpSessionLoadBalancer
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
			Vpc:         util.DefaultVpc,
			Default:     true,
			Provider:    util.OvnProvider,
			CIDRBlock:   c.config.DefaultCIDR,
			Gateway:     c.config.DefaultGateway,
			ExcludeIps:  strings.Split(c.config.DefaultExcludeIps, ","),
			NatOutgoing: true,
			GatewayType: kubeovnv1.GWDistributedType,
			Protocol:    util.CheckProtocol(c.config.DefaultCIDR),
		},
	}
	if c.config.NetworkType == util.NetworkTypeVlan {
		defaultSubnet.Spec.Vlan = c.config.DefaultVlanName
	}

	_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Create(context.Background(), &defaultSubnet, metav1.CreateOptions{})
	return err
}

// InitNodeSwitch init node switch to connect host and pod
func (c *Controller) initNodeSwitch() error {
	subnet, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Get(context.Background(), c.config.NodeSwitch, metav1.GetOptions{})
	if err == nil {
		if subnet != nil && util.CheckProtocol(c.config.NodeSwitchCIDR) != util.CheckProtocol(subnet.Spec.CIDRBlock) {
			// single-stack upgrade to dual-stack
			if util.CheckProtocol(c.config.NodeSwitchCIDR) == kubeovnv1.ProtocolDual {
				subnet.Spec.CIDRBlock = c.config.NodeSwitchCIDR
				if err := formatSubnet(subnet, c); err != nil {
					klog.Errorf("init format subnet %s failed: %v", c.config.NodeSwitch, err)
					return err
				}
			}
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
			ExcludeIps:             strings.Split(c.config.NodeSwitchGateway, ","),
			Protocol:               util.CheckProtocol(c.config.NodeSwitchCIDR),
			DisableInterConnection: true,
		},
	}

	_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Create(context.Background(), &nodeSubnet, metav1.CreateOptions{})
	return err
}

// InitClusterRouter init cluster router to connect different logical switches
func (c *Controller) initClusterRouter() error {
	lrs, err := c.ovnClient.ListLogicalRouter(c.config.EnableExternalVpc)
	if err != nil {
		return err
	}
	klog.Infof("exists routers: %v", lrs)
	for _, r := range lrs {
		if c.config.ClusterRouter == r {
			return nil
		}
	}
	return c.ovnClient.CreateLogicalRouter(c.config.ClusterRouter)
}

// InitLoadBalancer init the default tcp and udp cluster loadbalancer
func (c *Controller) initLoadBalancer() error {
	vpcs, err := c.vpcsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpc: %v", err)
		return err
	}

	for _, vpc := range vpcs {
		vpcLb := c.GenVpcLoadBalancer(vpc.Name)

		tcpLb, err := c.ovnClient.FindLoadbalancer(vpcLb.TcpLoadBalancer)
		if err != nil {
			return fmt.Errorf("failed to find tcp lb: %v", err)
		}
		if tcpLb == "" {
			klog.Infof("init cluster tcp load balancer %s", vpcLb.TcpLoadBalancer)
			err := c.ovnClient.CreateLoadBalancer(vpcLb.TcpLoadBalancer, util.ProtocolTCP, "")
			if err != nil {
				klog.Errorf("failed to crate cluster tcp load balancer: %v", err)
				return err
			}
		} else {
			klog.Infof("tcp load balancer %s exists", tcpLb)
		}

		tcpSessionLb, err := c.ovnClient.FindLoadbalancer(vpcLb.TcpSessLoadBalancer)
		if err != nil {
			return fmt.Errorf("failed to find tcp session lb: %v", err)
		}
		if tcpSessionLb == "" {
			klog.Infof("init cluster tcp session load balancer %s", vpcLb.TcpSessLoadBalancer)
			err := c.ovnClient.CreateLoadBalancer(vpcLb.TcpSessLoadBalancer, util.ProtocolTCP, "ip_src")
			if err != nil {
				klog.Errorf("failed to crate cluster tcp session load balancer: %v", err)
				return err
			}
		} else {
			klog.Infof("tcp session load balancer %s exists", vpcLb.TcpSessLoadBalancer)
		}

		udpLb, err := c.ovnClient.FindLoadbalancer(vpcLb.UdpLoadBalancer)
		if err != nil {
			return fmt.Errorf("failed to find udp lb: %v", err)
		}
		if udpLb == "" {
			klog.Infof("init cluster udp load balancer %s", vpcLb.UdpLoadBalancer)
			err := c.ovnClient.CreateLoadBalancer(vpcLb.UdpLoadBalancer, util.ProtocolUDP, "")
			if err != nil {
				klog.Errorf("failed to crate cluster udp load balancer: %v", err)
				return err
			}
		} else {
			klog.Infof("udp load balancer %s exists", udpLb)
		}

		udpSessionLb, err := c.ovnClient.FindLoadbalancer(vpcLb.UdpSessLoadBalancer)
		if err != nil {
			return fmt.Errorf("failed to find udp session lb: %v", err)
		}
		if udpSessionLb == "" {
			klog.Infof("init cluster udp session load balancer %s", vpcLb.UdpSessLoadBalancer)
			err := c.ovnClient.CreateLoadBalancer(vpcLb.UdpSessLoadBalancer, util.ProtocolUDP, "ip_src")
			if err != nil {
				klog.Errorf("failed to crate cluster udp session load balancer: %v", err)
				return err
			}
		} else {
			klog.Infof("udp session load balancer %s exists", vpcLb.UdpSessLoadBalancer)
		}

		vpc.Status.TcpLoadBalancer = vpcLb.TcpLoadBalancer
		vpc.Status.TcpSessionLoadBalancer = vpcLb.TcpSessLoadBalancer
		vpc.Status.UdpLoadBalancer = vpcLb.UdpLoadBalancer
		vpc.Status.UdpSessionLoadBalancer = vpcLb.UdpSessLoadBalancer
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
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnet: %v", err)
		return err
	}
	for _, subnet := range subnets {
		if err := c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.ExcludeIps); err != nil {
			klog.Errorf("failed to init subnet %s: %v", subnet.Name, err)
		}
	}

	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list pods: %v", err)
		return err
	}
	for _, pod := range pods {
		if isPodAlive(pod) && pod.Annotations[util.AllocatedAnnotation] == "true" {
			podName := c.getNameByPod(pod)
			if pod.Annotations[util.LogicalSwitchAnnotation] != "" {
				_, _, _, err := c.ipam.GetStaticAddress(
					fmt.Sprintf("%s/%s", pod.Namespace, podName),
					pod.Annotations[util.IpAddressAnnotation],
					pod.Annotations[util.MacAddressAnnotation],
					pod.Annotations[util.LogicalSwitchAnnotation])
				if err != nil {
					klog.Errorf("failed to init pod %s.%s address %s: %v", podName, pod.Namespace, pod.Annotations[util.IpAddressAnnotation], err)
				}
			}
			attachNetworks := pod.Annotations[util.AttachmentNetworkAnnotation]
			if attachNetworks != "" {
				attachments, err := util.ParsePodNetworkAnnotation(attachNetworks, pod.Namespace)
				if err != nil {
					klog.Errorf("failed to parse attach net for pod '%s', %v", podName, err)
					continue
				}
				for _, attach := range attachments {
					var builder strings.Builder
					builder.WriteString(attach.Name)
					builder.WriteString(".")
					if attach.Namespace == "" {
						builder.WriteString("default")
					} else {
						builder.WriteString(attach.Namespace)
					}

					_, _, _, err := c.ipam.GetStaticAddress(
						fmt.Sprintf("%s/%s", pod.Namespace, podName),
						pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, builder.String())],
						pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, builder.String())],
						pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, builder.String())])
					if err != nil {
						klog.Errorf("failed to init pod %s.%s address %s: %v", podName, pod.Namespace, pod.Annotations[util.IpAddressAnnotation], err)
					}
				}
			}
			if err = c.initAppendPodExternalIds(pod); err != nil {
				klog.Errorf("failed to init append pod %s.%s externalIds: %v", podName, pod.Namespace, err)
			}
		}
	}

	ips, err := c.ipsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list IPs: %v", err)
		return err
	}
	for _, ip := range ips {
		var ipamKey string
		if ip.Spec.Namespace != "" {
			ipamKey = fmt.Sprintf("%s/%s", ip.Spec.Namespace, ip.Spec.PodName)
		} else {
			ipamKey = fmt.Sprintf("node-%s", ip.Spec.PodName)
		}
		if _, _, _, err = c.ipam.GetStaticAddress(ipamKey, ip.Spec.IPAddress, ip.Spec.MacAddress, ip.Spec.Subnet); err != nil {
			klog.Errorf("failed to init IPAM from IP CR %s: %v", ip.Name, err)
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
			v4IP, v6IP, _, err := c.ipam.GetStaticAddress(portName, node.Annotations[util.IpAddressAnnotation],
				node.Annotations[util.MacAddressAnnotation],
				node.Annotations[util.LogicalSwitchAnnotation])
			if err != nil {
				klog.Errorf("failed to init node %s.%s address %s: %v", node.Name, node.Namespace, node.Annotations[util.IpAddressAnnotation], err)
			}
			if v4IP != "" && v6IP != "" {
				node.Annotations[util.IpAddressAnnotation] = util.GetStringIP(v4IP, v6IP)
			}

			if err = c.initAppendNodeExternalIds(portName, node.Name); err != nil {
				klog.Errorf("failed to init append node %s externalIds: %v", node.Name, err)
			}
		}
	}

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
			var index int
			for index = range pn.Spec.CustomInterfaces {
				if pn.Spec.CustomInterfaces[index].Interface == s {
					break
				}
			}
			if index != len(pn.Spec.CustomInterfaces) {
				pn.Spec.CustomInterfaces[index].Nodes = append(pn.Spec.CustomInterfaces[index].Nodes, node.Name)
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
	return err
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
	return err
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

	for _, ipCr := range ips.Items {
		ip := ipCr
		v4IP, v6IP := util.SplitStringIP(ip.Spec.IPAddress)
		if ip.Spec.V4IPAddress == v4IP && ip.Spec.V6IPAddress == v6IP {
			continue
		}
		ip.Spec.V4IPAddress = v4IP
		ip.Spec.V6IPAddress = v6IP

		_, err := c.config.KubeOvnClient.KubeovnV1().IPs().Update(context.Background(), &ip, metav1.UpdateOptions{})
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
	for _, subnet := range subnets {
		if util.CheckProtocol(subnet.Spec.CIDRBlock) == kubeovnv1.ProtocolDual {
			err = calcDualSubnetStatusIP(subnet, c)
		} else {
			err = calcSubnetStatusIP(subnet, c)
		}
		if err != nil {
			klog.Errorf("failed to calculate subnet %s used ip: %v", subnet.Name, err)
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

func (c *Controller) initAppendPodExternalIds(pod *v1.Pod) error {
	if !isPodAlive(pod) {
		return nil
	}

	podNets, err := c.getPodKubeovnNets(pod)
	if err != nil {
		klog.Errorf("failed to get pod nets %v", err)
		return err
	}

	podName := c.getNameByPod(pod)
	for _, podNet := range podNets {
		if !strings.HasSuffix(podNet.ProviderName, util.OvnProvider) {
			continue
		}
		portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
		externalIds, err := c.ovnClient.OvnGet("logical_switch_port", portName, "external_ids", "")
		if err != nil {
			klog.Errorf("failed to get lsp external_ids for pod %s/%s, %v", pod.Namespace, podName, err)
			return err
		}
		if strings.Contains(externalIds, "pod") || strings.Contains(externalIds, "vendor") {
			continue
		}

		ovnCommand := []string{"set", "logical_switch_port", portName, fmt.Sprintf("external_ids:pod=%s/%s", pod.Namespace, podName), fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName)}
		if err = c.ovnClient.SetLspExternalIds(ovnCommand); err != nil {
			klog.Errorf("failed to set lsp external_ids for pod %s/%s, %v", pod.Namespace, podName, err)
			return err
		}
	}
	return nil
}

func (c *Controller) initAppendNodeExternalIds(portName, nodeName string) error {
	externalIds, err := c.ovnClient.OvnGet("logical_switch_port", portName, "external_ids", "")
	if err != nil {
		klog.Errorf("failed to get lsp external_ids for node %s, %v", nodeName, err)
		return err
	}
	if strings.Contains(externalIds, "vendor") {
		return nil
	}

	ovnCommand := []string{"set", "logical_switch_port", portName, fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName)}
	if err = c.ovnClient.SetLspExternalIds(ovnCommand); err != nil {
		klog.Errorf("failed to set lsp external_ids for node %s, %v", nodeName, err)
		return err
	}
	return nil
}
