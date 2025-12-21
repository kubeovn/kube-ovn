package netconf

import (
	"encoding/json"

	"github.com/containernetworking/cni/pkg/types"

	"github.com/kubeovn/kube-ovn/pkg/request"
)

type NetConf struct {
	types.NetConf
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

func (c *NetConf) MarshalJSON() ([]byte, error) {
	// use type alias to escape recursion for json.Marshal() to MarshalJSON()
	type fixObjType = NetConf

	bytes, err := json.Marshal(fixObjType(*c)) //nolint:all
	if err != nil {
		return nil, err
	}

	fixupObj := make(map[string]any)
	if err := json.Unmarshal(bytes, &fixupObj); err != nil {
		return nil, err
	}

	if c.IPAM != nil {
		if bytes, err = json.Marshal(c.IPAM); err != nil {
			return nil, err
		}
		ipamObj := make(map[string]any)
		if err := json.Unmarshal(bytes, &ipamObj); err != nil {
			return nil, err
		}
		fixupObj["ipam"] = ipamObj
	}

	return json.Marshal(fixupObj)
}

func (c *NetConf) PostLoad() {
	// nothing to do on linux
}
