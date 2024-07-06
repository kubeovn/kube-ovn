package framework

import (
	"encoding/json"

	"github.com/containernetworking/cni/pkg/types"
	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"k8s.io/kubernetes/test/e2e/framework"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/netconf"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const CNIVersion = "0.3.1"

// https://github.com/containernetworking/plugins/blob/main/plugins/main/macvlan/macvlan.go#L37
type MacvlanNetConf struct {
	netconf.NetConf
	Master     string `json:"master"`
	Mode       string `json:"mode"`
	MTU        int    `json:"mtu"`
	Mac        string `json:"mac,omitempty"`
	LinkContNs bool   `json:"linkInContainer,omitempty"`

	RuntimeConfig struct {
		Mac string `json:"mac,omitempty"`
	} `json:"runtimeConfig,omitempty"`
}

func MakeMacvlanNetworkAttachmentDefinition(name, namespace, master, mode, provider string, routes []request.Route) *nadv1.NetworkAttachmentDefinition {
	ginkgo.GinkgoHelper()

	config := &MacvlanNetConf{
		NetConf: netconf.NetConf{
			NetConf: types.NetConf{
				CNIVersion: CNIVersion,
				Type:       "macvlan",
			},
			IPAM: &netconf.IPAMConf{
				Type:         util.CniTypeName,
				ServerSocket: "/run/openvswitch/kube-ovn-daemon.sock",
				Provider:     provider,
				Routes:       routes,
			},
		},
		Master:     master,
		Mode:       mode,
		LinkContNs: true,
	}
	buf, err := json.MarshalIndent(config, "", "  ")
	framework.ExpectNoError(err)

	return MakeNetworkAttachmentDefinition(name, namespace, string(buf))
}

func MakeOVNNetworkAttachmentDefinition(name, namespace, provider string, routes []request.Route) *nadv1.NetworkAttachmentDefinition {
	ginkgo.GinkgoHelper()

	config := &netconf.NetConf{
		NetConf: types.NetConf{
			CNIVersion: CNIVersion,
			Type:       util.CniTypeName,
		},
		ServerSocket: "/run/openvswitch/kube-ovn-daemon.sock",
		Provider:     provider,
		Routes:       routes,
	}
	buf, err := json.MarshalIndent(config, "", "  ")
	framework.ExpectNoError(err)

	return MakeNetworkAttachmentDefinition(name, namespace, string(buf))
}
