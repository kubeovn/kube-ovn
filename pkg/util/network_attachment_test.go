package util

import (
	"encoding/json"
	"reflect"
	"testing"

	cnitypes "github.com/containernetworking/cni/pkg/types"
	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
)

func TestParsePodNetworkObjectName(t *testing.T) {
	tests := []struct {
		name        string
		podNetwork  string
		netNsName   string
		networkName string
		netIfName   string
		err         string
	}{
		{
			name:        "controversy",
			podNetwork:  "",
			netNsName:   "",
			networkName: "",
			netIfName:   "",
			err:         "",
		},
		{
			name:        "base",
			podNetwork:  "kube-system/lb-svc-attachment",
			netNsName:   "kube-system",
			networkName: "lb-svc-attachment",
			netIfName:   "",
			err:         "",
		},
		{
			name:        "baseWithoutNS",
			podNetwork:  "lb-svc-attachment",
			netNsName:   "",
			networkName: "lb-svc-attachment",
			netIfName:   "",
			err:         "",
		},
		{
			name:        "baseWithIF",
			podNetwork:  "kube-system/lb-svc-attachment@eth0",
			netNsName:   "kube-system",
			networkName: "lb-svc-attachment",
			netIfName:   "eth0",
			err:         "",
		},
		{
			name:        "errFormat",
			podNetwork:  "kube-system/lb-svc-attachment/1",
			netNsName:   "",
			networkName: "lb-svc-attachment",
			netIfName:   "",
			err:         "Invalid network object",
		},
		{
			name:        "errFormatNS",
			podNetwork:  "mellanox.com/cx5_sriov_switchdev",
			netNsName:   "",
			networkName: "lb-svc-attachment",
			netIfName:   "",
			err:         "one or more items did not match comma-delimited format",
		},
		{
			name:        "errFormatIF",
			podNetwork:  "kube-system/lb-svc-attachment@eth0@1",
			netNsName:   "kube-system",
			networkName: "lb-svc-attachment",
			netIfName:   "eth0",
			err:         "Invalid network object",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			netNsName, networkName, netIfName, err := parsePodNetworkObjectName(c.podNetwork)
			if !ErrorContains(err, c.err) && (netNsName != c.netNsName || networkName != c.networkName || netIfName != c.netIfName) {
				t.Errorf("%v expected %v %v %v and err %v, but %v %v %v and err %v got",
					c.podNetwork, c.netNsName, c.networkName, c.netIfName, c.err, netNsName, networkName, netIfName, err)
			}
		})
	}
}

func TestParsePodNetworkAnnotation(t *testing.T) {
	correctJSON0, _ := json.Marshal([]types.NetworkSelectionElement{
		{
			Name:                       "lb-svc-attachment",
			Namespace:                  "kube-system",
			InterfaceRequest:           "eth0",
			MacRequest:                 "00:0c:29:9a:96:74",
			DeprecatedInterfaceRequest: "eth0",
			IPRequest:                  []string{"192.168.50.6"},
		},
	})
	correctJSON0IP, _ := json.Marshal([]types.NetworkSelectionElement{
		{
			Name:                       "lb-svc-attachment",
			Namespace:                  "kube-system",
			InterfaceRequest:           "eth0",
			MacRequest:                 "00:0c:29:9a:96:74",
			DeprecatedInterfaceRequest: "eth0",
			IPRequest:                  []string{"192.168.50.6/20"},
		},
	})
	errJSON0, _ := json.Marshal(types.NetworkSelectionElement{
		Name:                       "lb-svc-attachment",
		Namespace:                  "kube-system",
		InterfaceRequest:           "eth0",
		MacRequest:                 "00:0c:29:9a:96:74",
		DeprecatedInterfaceRequest: "eth0",
		IPRequest:                  []string{"192.168.50.6"},
	})
	errJSONMac, _ := json.Marshal([]types.NetworkSelectionElement{
		{
			Name:                       "lb-svc-attachment",
			Namespace:                  "kube-system",
			InterfaceRequest:           "eth0",
			MacRequest:                 "123",
			DeprecatedInterfaceRequest: "eth0",
			IPRequest:                  []string{"192.168.50.6"},
		},
	})
	errJSON0IP1, _ := json.Marshal([]types.NetworkSelectionElement{
		{
			Name:                       "lb-svc-attachment",
			Namespace:                  "kube-system",
			InterfaceRequest:           "eth0",
			MacRequest:                 "00:0c:29:9a:96:74",
			DeprecatedInterfaceRequest: "eth0",
			IPRequest:                  []string{"192.168.6"},
		},
	})
	errJSON0IP2, _ := json.Marshal([]types.NetworkSelectionElement{
		{
			Name:                       "lb-svc-attachment",
			Namespace:                  "kube-system",
			InterfaceRequest:           "eth0",
			MacRequest:                 "00:0c:29:9a:96:74",
			DeprecatedInterfaceRequest: "eth0",
			IPRequest:                  []string{"192.168.6/20"},
		},
	})
	correctJSON0IfReq, _ := json.Marshal([]types.NetworkSelectionElement{
		{
			Name:                       "lb-svc-attachment",
			Namespace:                  "kube-system",
			InterfaceRequest:           "",
			MacRequest:                 "00:0c:29:9a:96:74",
			DeprecatedInterfaceRequest: "eth0",
			IPRequest:                  []string{"192.168.50.6"},
		},
	})

	tests := []struct {
		name             string
		podNetworks      string
		defaultNamespace string
		exp              []*types.NetworkSelectionElement
		err              string
	}{
		{
			name:             "base",
			podNetworks:      "kube-system/lb-svc-attachment@eth0",
			defaultNamespace: "kube-system",
			exp: []*types.NetworkSelectionElement{
				{
					Name:             "lb-svc-attachment",
					Namespace:        "kube-system",
					InterfaceRequest: "eth0",
				},
			},
			err: "",
		},
		{
			name:             "baseWithIF",
			podNetworks:      "kube-system/lb-svc-attachment",
			defaultNamespace: "kube-system",
			exp: []*types.NetworkSelectionElement{
				{
					Name:             "lb-svc-attachment",
					Namespace:        "kube-system",
					InterfaceRequest: "",
				},
			},
			err: "",
		},
		{
			name:             "baseWithoutNS",
			podNetworks:      "lb-svc-attachment",
			defaultNamespace: "kube-system",
			exp: []*types.NetworkSelectionElement{
				{
					Name:             "lb-svc-attachment",
					Namespace:        "kube-system",
					InterfaceRequest: "",
				},
			},
			err: "",
		},
		{
			name:             "baseWithoutNS",
			podNetworks:      "lb-svc-attachment",
			defaultNamespace: "kube-system",
			exp: []*types.NetworkSelectionElement{
				{
					Name:             "lb-svc-attachment",
					Namespace:        "kube-system",
					InterfaceRequest: "",
				},
			},
			err: "",
		},
		{
			name:             "baseWithIFandNS",
			podNetworks:      "kube-system/lb-svc-attachment@eth0",
			defaultNamespace: "kube-system",
			exp: []*types.NetworkSelectionElement{
				{
					Name:             "lb-svc-attachment",
					Namespace:        "kube-system",
					InterfaceRequest: "eth0",
				},
			},
			err: "",
		},
		{
			name:             "correctJson",
			podNetworks:      string(correctJSON0),
			defaultNamespace: "kube-system",
			exp: []*types.NetworkSelectionElement{
				{
					Name:                       "lb-svc-attachment",
					Namespace:                  "kube-system",
					InterfaceRequest:           "eth0",
					MacRequest:                 "00:0c:29:9a:96:74",
					DeprecatedInterfaceRequest: "eth0",
					IPRequest:                  []string{"192.168.50.6"},
				},
			},
			err: "",
		},
		{
			name:             "correctJsonIP",
			podNetworks:      string(correctJSON0IP),
			defaultNamespace: "kube-system",
			exp: []*types.NetworkSelectionElement{
				{
					Name:                       "lb-svc-attachment",
					Namespace:                  "kube-system",
					InterfaceRequest:           "eth0",
					MacRequest:                 "00:0c:29:9a:96:74",
					DeprecatedInterfaceRequest: "eth0",
					IPRequest:                  []string{"192.168.50.6/20"},
				},
			},
			err: "",
		},
		{
			name:             "correctJsonIfReq",
			podNetworks:      string(correctJSON0IfReq),
			defaultNamespace: "kube-system",
			exp: []*types.NetworkSelectionElement{
				{
					Name:                       "lb-svc-attachment",
					Namespace:                  "kube-system",
					InterfaceRequest:           "eth0",
					MacRequest:                 "00:0c:29:9a:96:74",
					DeprecatedInterfaceRequest: "eth0",
					IPRequest:                  []string{"192.168.50.6"},
				},
			},
			err: "",
		},
		{
			name:             "errJson",
			podNetworks:      string(errJSON0),
			defaultNamespace: "kube-system",
			exp:              nil,
			err:              "json: cannot unmarshal object into Go value",
		},
		{
			name:             "errJSONMac",
			podNetworks:      string(errJSONMac),
			defaultNamespace: "kube-system",
			exp:              nil,
			err:              "invalid MAC address",
		},
		{
			name:             "errJsonIP1",
			podNetworks:      string(errJSON0IP1),
			defaultNamespace: "kube-system",
			exp:              nil,
			err:              "failed to parse IP address",
		},
		{
			name:             "errJsonIP2",
			podNetworks:      string(errJSON0IP2),
			defaultNamespace: "kube-system",
			exp:              nil,
			err:              "invalid CIDR address",
		},
		{
			name:             "errFormat",
			podNetworks:      "kube-system/lb-svc-attachment@eth0@1",
			defaultNamespace: "kube-system",
			exp:              nil,
			err:              "Invalid network object",
		},
		{
			name:             "errNull",
			podNetworks:      "",
			defaultNamespace: "kube-system",
			exp:              nil,
			err:              "",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ele, err := ParsePodNetworkAnnotation(c.podNetworks, c.defaultNamespace)
			if !ErrorContains(err, c.err) || !reflect.DeepEqual(ele, c.exp) {
				t.Errorf("%v, %v expected %v and err %v, but %v and err %v got",
					c.podNetworks, c.defaultNamespace, c.exp[0], c.err, ele[0], err)
			}
		})
	}
}

func TestIsOvnNetwork(t *testing.T) {
	tests := []struct {
		name   string
		netCfg *types.DelegateNetConf
		expt   bool
	}{
		{
			name: "base",
			netCfg: &types.DelegateNetConf{
				Conf: cnitypes.NetConf{
					Type: CniTypeName,
				},
			},
			expt: true,
		},
		{
			name: "basewithPlugins",
			netCfg: &types.DelegateNetConf{
				ConfList: cnitypes.NetConfList{
					Plugins: []*cnitypes.NetConf{
						{Type: CniTypeName},
					},
				},
			},
			expt: true,
		},
		{
			name: "baseWithErr",
			netCfg: &types.DelegateNetConf{
				Conf: cnitypes.NetConf{
					Type: "err",
				},
			},
			expt: false,
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			rslt := IsOvnNetwork(c.netCfg)
			if rslt != c.expt {
				t.Errorf("%v expected %v, but %v got",
					c.netCfg, c.expt, rslt)
			}
		})
	}
}

func TestIsDefaultNet(t *testing.T) {
	tests := []struct {
		name                 string
		defaultNetAnnotation string
		attach               *types.NetworkSelectionElement
		expt                 bool
	}{
		{
			name:                 "base",
			defaultNetAnnotation: "nm",
			attach: &types.NetworkSelectionElement{
				Name:      "nm",
				Namespace: "ns",
			},
			expt: true,
		},
		{
			name:                 "baseWithNS",
			defaultNetAnnotation: "ns/nm",
			attach: &types.NetworkSelectionElement{
				Name:      "nm",
				Namespace: "ns",
			},
			expt: true,
		},
		{
			name:                 "errFormat",
			defaultNetAnnotation: "err",
			attach: &types.NetworkSelectionElement{
				Name:      "nm",
				Namespace: "ns",
			},
			expt: false,
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			rslt := IsDefaultNet(c.defaultNetAnnotation, c.attach)
			if rslt != c.expt {
				t.Errorf("%v %v expected %v, but %v got",
					c.defaultNetAnnotation, c.attach, c.expt, rslt)
			}
		})
	}
}
