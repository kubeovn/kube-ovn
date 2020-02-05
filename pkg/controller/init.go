package controller

import (
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
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
			CIDRBlock:   c.config.DefaultCIDR,
			Gateway:     c.config.DefaultGateway,
			ExcludeIps:  strings.Split(c.config.DefaultExcludeIps, ","),
			NatOutgoing: true,
			GatewayType: kubeovnv1.GWDistributedType,
			Protocol:    util.CheckProtocol(c.config.DefaultCIDR),
		},
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
			CIDRBlock:  c.config.NodeSwitchCIDR,
			Gateway:    c.config.NodeSwitchGateway,
			ExcludeIps: []string{c.config.NodeSwitchGateway},
			Protocol:   util.CheckProtocol(c.config.NodeSwitchCIDR),
		},
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
