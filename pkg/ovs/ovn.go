package ovs

import (
	"github.com/ovn-org/libovsdb/client"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
)

// Client is the ovn client
type Client struct {
	OvnNbAddress                  string
	OvnTimeout                    int
	OvnSbAddress                  string
	OVNIcNBAddress                string
	ClusterRouter                 string
	ClusterTcpLoadBalancer        string
	ClusterUdpLoadBalancer        string
	ClusterTcpSessionLoadBalancer string
	ClusterUdpSessionLoadBalancer string
	NodeSwitch                    string
	NodeSwitchCIDR                string
	ExternalGatewayType           string

	nbClient client.Client
}

const (
	OvnNbCtl    = "ovn-nbctl"
	OvnSbCtl    = "ovn-sbctl"
	OVNIcNbCtl  = "ovn-ic-nbctl"
	OvsVsCtl    = "ovs-vsctl"
	MayExist    = "--may-exist"
	IfExists    = "--if-exists"
	Policy      = "--policy"
	PolicyDstIP = "dst-ip"
	PolicySrcIP = "src-ip"
)

// NewClient init an ovn client
func NewClient(ovnNbAddr string, ovnNbTimeout int, ovnSbAddr, clusterRouter, clusterTcpLoadBalancer, clusterUdpLoadBalancer, clusterTcpSessionLoadBalancer, clusterUdpSessionLoadBalancer, nodeSwitch, nodeSwitchCIDR string) (*Client, error) {
	nbClient, err := ovsclient.NewNbClient(ovnNbAddr, ovnNbTimeout)
	if err != nil {
		klog.Errorf("failed to create OVN NB client: %v", err)
		return nil, err
	}

	c := &Client{
		OvnNbAddress:                  ovnNbAddr,
		OvnSbAddress:                  ovnSbAddr,
		OvnTimeout:                    ovnNbTimeout,
		ClusterRouter:                 clusterRouter,
		ClusterTcpLoadBalancer:        clusterTcpLoadBalancer,
		ClusterUdpLoadBalancer:        clusterUdpLoadBalancer,
		ClusterTcpSessionLoadBalancer: clusterTcpSessionLoadBalancer,
		ClusterUdpSessionLoadBalancer: clusterUdpSessionLoadBalancer,
		NodeSwitch:                    nodeSwitch,
		NodeSwitchCIDR:                nodeSwitchCIDR,
		nbClient:                      nbClient,
	}
	return c, nil
}
