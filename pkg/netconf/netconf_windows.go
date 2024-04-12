package netconf

import (
	"github.com/containernetworking/plugins/pkg/hns"

	"github.com/kubeovn/kube-ovn/pkg/request"
)

type NetConf struct {
	hns.NetConf
	ServerSocket string          `json:"server_socket,omitempty"`
	Provider     string          `json:"provider,omitempty"`
	Routes       []request.Route `json:"routes,omitempty"`
	IPAM         *IPAMConf       `json:"ipam,omitempty"`
	// PciAddrs in case of using sriov
	DeviceID string `json:"deviceID,omitempty"`
	VfDriver string `json:"vf_driver,omitempty"`
	// for dpdk
	VhostUserSocketVolumeName  string `json:"vhost_user_socket_volume_name,omitempty"`
	VhostUserSocketName        string `json:"vhost_user_socket_name,omitempty"`
	VhostUserSocketConsumption string `json:"vhost_user_socket_consumption,omitempty"`
}

func (c *NetConf) PostLoad() {
	if len(c.DNS.Nameservers) == 0 {
		c.DNS.Nameservers = c.RuntimeConfig.DNS.Nameservers
	}
	if len(c.DNS.Search) == 0 {
		c.DNS.Search = c.RuntimeConfig.DNS.Search
	}
}
