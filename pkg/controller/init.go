package controller

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/util"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func (c *Controller) InitDefaultVpc() error {
	vpc, err := c.vpcsLister.Get(util.DefaultVpc)
	if err != nil {
		vpc = &kubeovnv1.Vpc{}
		vpc.Name = util.DefaultVpc
		vpc, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Create(context.Background(), vpc, v1.CreateOptions{})
		if err != nil {
			klog.Errorf("init default vpc failed %v", err)
			return err
		}
	}

	vpc.Status.DefaultLogicalSwitch = c.config.DefaultLogicalSwitch
	vpc.Status.Router = c.config.ClusterRouter
	vpc.Status.TcpLoadBalancer = c.config.ClusterTcpLoadBalancer
	vpc.Status.TcpSessionLoadBalancer = c.config.ClusterTcpSessionLoadBalancer
	vpc.Status.UdpLoadBalancer = c.config.ClusterUdpLoadBalancer
	vpc.Status.UdpSessionLoadBalancer = c.config.ClusterUdpSessionLoadBalancer
	vpc.Status.Standby = true
	vpc.Status.Default = true

	bytes, err := vpc.Status.Bytes()
	if err != nil {
		return err
	}
	_, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(), vpc.Name, types.MergePatchType, bytes, v1.PatchOptions{}, "status")
	if err != nil {
		klog.Errorf("init default vpc failed %v", err)
		return err
	}
	return nil
}

// InitDefaultLogicalSwitch init the default logical switch for ovn network
func (c *Controller) initDefaultLogicalSwitch() error {
	subnet, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Get(context.Background(), c.config.DefaultLogicalSwitch, v1.GetOptions{})
	if err == nil {
		if subnet != nil && subnet.Spec.CIDRBlock != c.config.DefaultCIDR {
			// upgrade to dual-stack
			if util.CheckProtocol(c.config.DefaultCIDR) == kubeovnv1.ProtocolDual {
				subnet.Spec.CIDRBlock = c.config.DefaultCIDR
				if err := formatSubnet(subnet, c); err != nil {
					klog.Errorf("init format subnet %s failed %v", c.config.DefaultLogicalSwitch, err)
					return err
				}
			} else {
				return fmt.Errorf("can not modify subnet to single protocol")
			}
		}
		return nil
	}

	if !errors.IsNotFound(err) {
		klog.Errorf("get default subnet %s failed %v", c.config.DefaultLogicalSwitch, err)
		return err
	}

	defaultSubnet := kubeovnv1.Subnet{
		ObjectMeta: v1.ObjectMeta{Name: c.config.DefaultLogicalSwitch},
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
		if c.config.DefaultVlanID == 0 {
			defaultSubnet.Spec.UnderlayGateway = true
		}
	}

	_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Create(context.Background(), &defaultSubnet, v1.CreateOptions{})
	return err
}

// InitNodeSwitch init node switch to connect host and pod
func (c *Controller) initNodeSwitch() error {
	subnet, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Get(context.Background(), c.config.NodeSwitch, v1.GetOptions{})
	if err == nil {
		if subnet != nil && subnet.Spec.CIDRBlock != c.config.NodeSwitchCIDR {
			// upgrade to dual-stack
			if util.CheckProtocol(c.config.NodeSwitchCIDR) == kubeovnv1.ProtocolDual {
				subnet.Spec.CIDRBlock = c.config.NodeSwitchCIDR
				if err := formatSubnet(subnet, c); err != nil {
					klog.Errorf("init format subnet %s failed %v", c.config.NodeSwitch, err)
					return err
				}
			} else {
				return fmt.Errorf("can not modify subnet to single protocol")
			}
		}
		return nil
	}

	if !errors.IsNotFound(err) {
		klog.Errorf("get node subnet %s failed %v", c.config.NodeSwitch, err)
		return err
	}

	nodeSubnet := kubeovnv1.Subnet{
		ObjectMeta: v1.ObjectMeta{Name: c.config.NodeSwitch},
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

	_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Create(context.Background(), &nodeSubnet, v1.CreateOptions{})
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
		err := c.ovnClient.CreateLoadBalancer(c.config.ClusterTcpLoadBalancer, util.ProtocolTCP, "")
		if err != nil {
			klog.Errorf("failed to crate cluster tcp load balancer %v", err)
			return err
		}
	} else {
		klog.Infof("tcp load balancer %s exists", tcpLb)
	}

	tcpSessionLb, err := c.ovnClient.FindLoadbalancer(c.config.ClusterTcpSessionLoadBalancer)
	if err != nil {
		return fmt.Errorf("failed to find tcp session lb %v", err)
	}
	if tcpSessionLb == "" {
		klog.Infof("init cluster tcp session load balancer %s", c.config.ClusterTcpSessionLoadBalancer)
		err := c.ovnClient.CreateLoadBalancer(c.config.ClusterTcpSessionLoadBalancer, util.ProtocolTCP, "ip_src")
		if err != nil {
			klog.Errorf("failed to crate cluster tcp session load balancer %v", err)
			return err
		}
	} else {
		klog.Infof("tcp session load balancer %s exists", tcpSessionLb)
	}

	udpLb, err := c.ovnClient.FindLoadbalancer(c.config.ClusterUdpLoadBalancer)
	if err != nil {
		return fmt.Errorf("failed to find udp lb %v", err)
	}
	if udpLb == "" {
		klog.Infof("init cluster udp load balancer %s", c.config.ClusterUdpLoadBalancer)
		err := c.ovnClient.CreateLoadBalancer(c.config.ClusterUdpLoadBalancer, util.ProtocolUDP, "")
		if err != nil {
			klog.Errorf("failed to crate cluster udp load balancer %v", err)
			return err
		}
	} else {
		klog.Infof("udp load balancer %s exists", udpLb)
	}

	udpSessionLb, err := c.ovnClient.FindLoadbalancer(c.config.ClusterUdpSessionLoadBalancer)
	if err != nil {
		return fmt.Errorf("failed to find udp session lb %v", err)
	}
	if udpSessionLb == "" {
		klog.Infof("init cluster udp session load balancer %s", c.config.ClusterUdpSessionLoadBalancer)
		err := c.ovnClient.CreateLoadBalancer(c.config.ClusterUdpSessionLoadBalancer, util.ProtocolUDP, "ip_src")
		if err != nil {
			klog.Errorf("failed to crate cluster udp session load balancer %v", err)
			return err
		}
	} else {
		klog.Infof("udp session load balancer %s exists", udpSessionLb)
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
			_, _, _, err := c.ipam.GetStaticAddress(
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
			_, _, _, err := c.ipam.GetStaticAddress(portName, node.Annotations[util.IpAddressAnnotation],
				node.Annotations[util.MacAddressAnnotation],
				node.Annotations[util.LogicalSwitchAnnotation])
			if err != nil {
				klog.Errorf("failed to init node %s.%s address %s, %v", node.Name, node.Namespace, node.Annotations[util.IpAddressAnnotation], err)
			}
		}
	}

	return nil
}

//InitDefaultVlan init the default vlan when network type is vlan or vxlan
func (c *Controller) initDefaultVlan() error {
	if !util.IsNetworkVlan(c.config.NetworkType) {
		return nil
	}

	_, err := c.config.KubeOvnClient.KubeovnV1().Vlans().Get(context.Background(), c.config.DefaultVlanName, v1.GetOptions{})
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

	_, err = c.config.KubeOvnClient.KubeovnV1().Vlans().Create(context.Background(), &defaultVlan, v1.CreateOptions{})
	return err
}

func (c *Controller) initSyncCrdIPs() error {
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
			klog.Errorf("failed to sync crd ip %s, %v", ip.Spec.IPAddress, err)
			return err
		}
	}
	return nil
}

func (c *Controller) initSyncCrdSubnets() error {
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
			klog.Errorf("failed to calculate subnet %s used ip, %v", subnet.Name, err)
			return err
		}
	}
	return nil
}
