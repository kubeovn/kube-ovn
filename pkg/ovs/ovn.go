package ovs

import (
	"errors"
	"fmt"
)

var (
	ErrNoAddr   = errors.New("no address")
	ErrNotFound = errors.New("not found")
)

type Client struct {
	OvnNbAddress           string
	OvnSbAddress           string
	ClusterRouter          string
	ClusterTcpLoadBalancer string
	ClusterUdpLoadBalancer string
	NodeSwitch             string
	NodeSwitchCIDR         string
}

const (
	OvnNbCtl    = "ovn-nbctl"
	OvnSbCtl    = "ovn-sbctl"
	MayExist    = "--may-exist"
	IfExists    = "--if-exists"
	WaitSb      = "--wait=sb"
	Policy      = "--policy"
	PolicyDstIP = "dst-ip"
	PolicySrcIP = "src-ip"
)

var GlobalDnsTable string

func NewClient(ovnNbHost string, ovnNbPort int, ovnSbHost string, ovnSbPort int, clusterRouter, clusterTcpLoadBalancer, clusterUdpLoadBalancer, nodeSwitch, nodeSwitchCIDR string) *Client {
	return &Client{
		OvnNbAddress:           fmt.Sprintf("tcp:%s:%d", ovnNbHost, ovnNbPort),
		OvnSbAddress:           fmt.Sprintf("tcp:%s:%d", ovnSbHost, ovnSbPort),
		ClusterRouter:          clusterRouter,
		ClusterTcpLoadBalancer: clusterTcpLoadBalancer,
		ClusterUdpLoadBalancer: clusterUdpLoadBalancer,
		NodeSwitch:             nodeSwitch,
		NodeSwitchCIDR:         nodeSwitchCIDR,
	}
}
