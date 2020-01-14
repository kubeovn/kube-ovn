package controller

import (
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
	"strings"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/ovs"
	"github.com/alauda/kube-ovn/pkg/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

// InitDefaultLogicalSwitch int the default logical switch for ovn network
func InitDefaultLogicalSwitch(config *Configuration) error {
	_, err := config.KubeOvnClient.KubeovnV1().Subnets().Get(config.DefaultLogicalSwitch, v1.GetOptions{})
	if err == nil {
		return nil
	}

	if !errors.IsNotFound(err) {
		klog.Errorf("get default subnet %s failed %v", config.DefaultLogicalSwitch, err)
		return err
	}
	defaultSubnet := kubeovnv1.Subnet{
		ObjectMeta: v1.ObjectMeta{Name: config.DefaultLogicalSwitch},
		Spec: kubeovnv1.SubnetSpec{
			Default:     true,
			CIDRBlock:   config.DefaultCIDR,
			Gateway:     config.DefaultGateway,
			ExcludeIps:  strings.Split(config.DefaultExcludeIps, ","),
			NatOutgoing: true,
			GatewayType: kubeovnv1.GWDistributedType,
			Protocol:    util.CheckProtocol(config.DefaultCIDR),
		},
	}

	_, err = config.KubeOvnClient.KubeovnV1().Subnets().Create(&defaultSubnet)
	return err
}

// InitNodeSwitch init node switch to connect host and pod
func InitNodeSwitch(config *Configuration) error {
	_, err := config.KubeOvnClient.KubeovnV1().Subnets().Get(config.NodeSwitch, v1.GetOptions{})
	if err == nil {
		return nil
	}

	if !errors.IsNotFound(err) {
		klog.Errorf("get node subnet %s failed %v", config.NodeSwitch, err)
		return err
	}
	nodeSubnet := kubeovnv1.Subnet{
		ObjectMeta: v1.ObjectMeta{Name: config.NodeSwitch},
		Spec: kubeovnv1.SubnetSpec{
			Default:    false,
			CIDRBlock:  config.NodeSwitchCIDR,
			Gateway:    config.NodeSwitchGateway,
			ExcludeIps: []string{config.NodeSwitchGateway},
			Protocol:   util.CheckProtocol(config.NodeSwitchCIDR),
		},
	}
	_, err = config.KubeOvnClient.KubeovnV1().Subnets().Create(&nodeSubnet)

	return err
}

// InitClusterRouter init cluster router to connect different logical switches
func InitClusterRouter(config *Configuration) error {
	client := ovs.NewClient(config.OvnNbHost, config.OvnNbPort, config.OvnNbTimeout, "", 0, config.ClusterRouter, config.ClusterTcpLoadBalancer, config.ClusterUdpLoadBalancer, config.NodeSwitch, config.NodeSwitchCIDR)
	lrs, err := client.ListLogicalRouter()
	if err != nil {
		return err
	}
	klog.Infof("exists routers %v", lrs)
	for _, r := range lrs {
		if config.ClusterRouter == r {
			return nil
		}
	}
	return client.CreateLogicalRouter(config.ClusterRouter)
}

// InitLoadBalancer init the default tcp and udp cluster loadbalancer
func InitLoadBalancer(config *Configuration) error {
	client := ovs.NewClient(config.OvnNbHost, config.OvnNbPort, config.OvnNbTimeout, "", 0, config.ClusterRouter, config.ClusterTcpLoadBalancer, config.ClusterUdpLoadBalancer, config.NodeSwitch, config.NodeSwitchCIDR)
	tcpLb, err := client.FindLoadbalancer(config.ClusterTcpLoadBalancer)
	if err != nil {
		return fmt.Errorf("failed to find tcp lb %v", err)
	}
	if tcpLb == "" {
		klog.Infof("init cluster tcp load balancer %s", config.ClusterTcpLoadBalancer)
		err := client.CreateLoadBalancer(config.ClusterTcpLoadBalancer, util.ProtocolTCP)
		if err != nil {
			klog.Errorf("failed to crate cluster tcp load balancer %v", err)
			return err
		}
	} else {
		klog.Infof("tcp load balancer %s exists", tcpLb)
	}

	udpLb, err := client.FindLoadbalancer(config.ClusterUdpLoadBalancer)
	if err != nil {
		return fmt.Errorf("failed to find udp lb %v", err)
	}
	if udpLb == "" {
		klog.Infof("init cluster udp load balancer %s", config.ClusterUdpLoadBalancer)
		err := client.CreateLoadBalancer(config.ClusterUdpLoadBalancer, util.ProtocolUDP)
		if err != nil {
			klog.Errorf("failed to crate cluster udp load balancer %v", err)
			return err
		}
	} else {
		klog.Infof("udp load balancer %s exists", udpLb)
	}
	return nil
}
