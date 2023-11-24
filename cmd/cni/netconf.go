//go:build !windows
// +build !windows

package main

import (
	"github.com/containernetworking/cni/pkg/types"

	"github.com/kubeovn/kube-ovn/pkg/request"
)

type netConf struct {
	types.NetConf
	ServerSocket string          `json:"server_socket"`
	Provider     string          `json:"provider"`
	Routes       []request.Route `json:"routes"`
	IPAM         *ipamConf       `json:"ipam"`
	// PciAddrs in case of using sriov
	DeviceID string `json:"deviceID"`
	VfDriver string `json:"vf_driver"`
	// for dpdk
	VhostUserSocketVolumeName  string `json:"vhost_user_socket_volume_name"`
	VhostUserSocketName        string `json:"vhost_user_socket_name"`
	VhostUserSocketConsumption string `json:"vhost_user_socket_consumption"`
}

func (n *netConf) postLoad() {
	// nothing to do on linux
}
