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

func (n *NetConf) PostLoad() {
	if len(n.DNS.Nameservers) == 0 {
		n.DNS.Nameservers = n.RuntimeConfig.DNS.Nameservers
	}
	if len(n.DNS.Search) == 0 {
		n.DNS.Search = n.RuntimeConfig.DNS.Search
	}
}
