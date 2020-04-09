package controller

import (
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"strings"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

func (c *Controller) InitOVN() error {
	if err := c.initClusterRouter(); err != nil {
		klog.Errorf("init cluster router failed %v", err)
		return err
	}

	if err := c.initLoadBalancer(); err != nil {
		klog.Errorf("init load balancer failed %v", err)
		return err
	}

	if err := c.initDefaultVlan(); err != nil {
		klog.Errorf("init default vlan failed %v", err)
		return err
	}

	if err := c.initNetwork(); err != nil {
		klog.Errorf("init cluster network config failed %v", err)
		return err
	}

	if err := c.initNodeSwitch(); err != nil {
		klog.Errorf("init node switch failed %v", err)
		return err
	}

	if err := c.initDefaultLogicalSwitch(); err != nil {
		klog.Errorf("init default switch failed %v", err)
		return err
	}

	return nil
}

// InitDefaultLogicalSwitch int the default logical switch for ovn network
func (c *Controller) initDefaultLogicalSwitch() error {
	_, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Get(c.config.DefaultLogicalSwitch, v1.GetOptions{})
	if err == nil {
		return nil
	}

	if !errors.IsNotFound(err) {
		klog.Errorf("get default subnet %s failed %v", c.config.DefaultLogicalSwitch, err)
		return err
	}

	defaultSubnet := kubeovnv1.Subnet{
		ObjectMeta: v1.ObjectMeta{Name: c.config.DefaultLogicalSwitch},
		Spec: kubeovnv1.SubnetSpec{
			Default:     true,
			Provider:    util.OvnProvider,
			CIDRBlock:   c.config.DefaultCIDR,
			Gateway:     c.config.DefaultGateway,
			ExcludeIps:  strings.Split(c.config.DefaultExcludeIps, ","),
			NatOutgoing: true,
			GatewayType: kubeovnv1.GWDistributedType,
			Protocol:    util.CheckProtocol(c.config.DefaultCIDR),
			Vlan:        c.config.DefaultVlanName,
		},
	}
	if c.config.NetworkType == util.NetworkTypeVlan {
		defaultSubnet.Spec.Vlan = c.config.DefaultVlanName
	}

	_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Create(&defaultSubnet)
	return err
}

// InitNodeSwitch init node switch to connect host and pod
func (c *Controller) initNodeSwitch() error {
	_, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Get(c.config.NodeSwitch, v1.GetOptions{})
	if err == nil {
		return nil
	}

	if !errors.IsNotFound(err) {
		klog.Errorf("get node subnet %s failed %v", c.config.NodeSwitch, err)
		return err
	}

	nodeSubnet := kubeovnv1.Subnet{
		ObjectMeta: v1.ObjectMeta{Name: c.config.NodeSwitch},
		Spec: kubeovnv1.SubnetSpec{
			Default:    false,
			Provider:   util.OvnProvider,
			CIDRBlock:  c.config.NodeSwitchCIDR,
			Gateway:    c.config.NodeSwitchGateway,
			ExcludeIps: []string{c.config.NodeSwitchGateway},
			Protocol:   util.CheckProtocol(c.config.NodeSwitchCIDR),
			Vlan:       c.config.DefaultVlanName,
		},
	}
	if c.config.NetworkType == util.NetworkTypeVlan {
		nodeSubnet.Spec.Vlan = c.config.DefaultVlanName
	}

	_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Create(&nodeSubnet)
	return err
}

// InitClusterRouter init cluster router to connect different logical switches
func (c *Controller) initClusterRouter() error {
	lrs, err := c.ovnClient.ListLogicalRouter()
	if err != nil {
		return err
	}
	klog.Infof("exists routers %v", lrs)
	for _, r := range lrs {
		if c.config.ClusterRouter == r {
			return nil
		}
	}
	return c.ovnClient.CreateLogicalRouter(c.config.ClusterRouter)
}

// InitLoadBalancer init the default tcp and udp cluster loadbalancer
func (c *Controller) initLoadBalancer() error {
	tcpLb, err := c.ovnClient.FindLoadbalancer(c.config.ClusterTcpLoadBalancer)
	if err != nil {
		return fmt.Errorf("failed to find tcp lb %v", err)
	}
	if tcpLb == "" {
		klog.Infof("init cluster tcp load balancer %s", c.config.ClusterTcpLoadBalancer)
		err := c.ovnClient.CreateLoadBalancer(c.config.ClusterTcpLoadBalancer, util.ProtocolTCP)
		if err != nil {
			klog.Errorf("failed to crate cluster tcp load balancer %v", err)
			return err
		}
	} else {
		klog.Infof("tcp load balancer %s exists", tcpLb)
	}

	udpLb, err := c.ovnClient.FindLoadbalancer(c.config.ClusterUdpLoadBalancer)
	if err != nil {
		return fmt.Errorf("failed to find udp lb %v", err)
	}
	if udpLb == "" {
		klog.Infof("init cluster udp load balancer %s", c.config.ClusterUdpLoadBalancer)
		err := c.ovnClient.CreateLoadBalancer(c.config.ClusterUdpLoadBalancer, util.ProtocolUDP)
		if err != nil {
			klog.Errorf("failed to crate cluster udp load balancer %v", err)
			return err
		}
	} else {
		klog.Infof("udp load balancer %s exists", udpLb)
	}
	return nil
}

func (c *Controller) InitIPAM() error {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnet, %v", err)
		return err
	}
	for _, subnet := range subnets {
		if err := c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.ExcludeIps); err != nil {
			klog.Errorf("failed to init subnet %s, %v", subnet.Name, err)
		}
	}

	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list pods, %v", err)
		return err
	}
	for _, pod := range pods {
		if isPodAlive(pod) &&
			pod.Annotations[util.AllocatedAnnotation] == "true" &&
			pod.Annotations[util.LogicalSwitchAnnotation] != "" {
			_, _, err := c.ipam.GetStaticAddress(
				fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
				pod.Annotations[util.IpAddressAnnotation],
				pod.Annotations[util.MacAddressAnnotation],
				pod.Annotations[util.LogicalSwitchAnnotation])
			if err != nil {
				klog.Errorf("failed to init pod %s.%s address %s, %v", pod.Name, pod.Namespace, pod.Annotations[util.IpAddressAnnotation], err)
			}
		}
	}

	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes, %v", err)
		return err
	}
	for _, node := range nodes {
		if node.Annotations[util.AllocatedAnnotation] == "true" {
			portName := fmt.Sprintf("node-%s", node.Name)
			_, _, err := c.ipam.GetStaticAddress(portName, node.Annotations[util.IpAddressAnnotation],
				node.Annotations[util.MacAddressAnnotation],
				node.Annotations[util.LogicalSwitchAnnotation])
			if err != nil {
				klog.Errorf("failed to init node %s.%s address %s, %v", node.Name, node.Namespace, node.Annotations[util.IpAddressAnnotation], err)
			}
		}
	}

	return nil
}

//InitNetwork save the cluster default network
func (c *Controller) initNetwork() error {
	networkCrd, err := c.config.KubeOvnClient.KubeovnV1().Networks().Get("config", v1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		klog.Errorf("get network config failed %v", err)
		return err
	}

	networkSpec := kubeovnv1.NetworkSpec{
		NetworkType:   c.config.NetworkType,
		DefaultSubnet: c.config.DefaultLogicalSwitch,
		NodeSubnet:    c.config.NodeSwitch,
		PprofPort:     c.config.PprofPort,
		InterfaceName: c.config.DefaultHostInterface,
	}
	if c.config.NetworkType == util.NetworkTypeVlan {
		networkSpec.ProviderName = c.config.DefaultProviderName
		networkSpec.DefaultVlan = c.config.DefaultVlanName
		networkSpec.VlanRange = c.config.DefaultVlanRange
	}

	if errors.IsNotFound(err) {
		networkConfig := kubeovnv1.Network{
			ObjectMeta: v1.ObjectMeta{Name: "config"},
			Spec:       networkSpec,
		}
		_, err = c.config.KubeOvnClient.KubeovnV1().Networks().Create(&networkConfig)
		return err
	}

	networkCrd.Spec = networkSpec
	_, err = c.config.KubeOvnClient.KubeovnV1().Networks().Update(networkCrd)
	return err
}

//InitDefaultVlan init the default vlan when network type is vlan or xvlan
func (c *Controller) initDefaultVlan() error {
	if c.config.NetworkType != util.NetworkTypeVlan {
		return nil
	}

	_, err := c.config.KubeOvnClient.KubeovnV1().Vlans().Get(c.config.DefaultVlanName, v1.GetOptions{})
	if err == nil {
		return nil
	}

	if !errors.IsNotFound(err) {
		klog.Errorf("get default vlan %s failed %v", c.config.DefaultVlanName, err)
		return err
	}

	if c.config.DefaultVlanID < 0 || c.config.DefaultVlanID > 4095 {
		return fmt.Errorf("the default vlan id is not between 1-4095")
	}

	defaultVlan := kubeovnv1.Vlan{
		ObjectMeta: v1.ObjectMeta{Name: c.config.DefaultVlanName},
		Spec: kubeovnv1.VlanSpec{
			VlanId:                c.config.DefaultVlanID,
			ProviderInterfaceName: c.config.DefaultProviderName,
			LogicalInterfaceName:  c.config.DefaultHostInterface,
		},
	}

	_, err = c.config.KubeOvnClient.KubeovnV1().Vlans().Create(&defaultVlan)
	return err
}
