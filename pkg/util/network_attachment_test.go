package util

import (
	"testing"

	cnitypes "github.com/containernetworking/cni/pkg/types"
	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
)

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
		attach               *nadv1.NetworkSelectionElement
		expt                 bool
	}{
		{
			name:                 "base",
			defaultNetAnnotation: "nm",
			attach: &nadv1.NetworkSelectionElement{
				Name:      "nm",
				Namespace: "ns",
			},
			expt: true,
		},
		{
			name:                 "baseWithNS",
			defaultNetAnnotation: "ns/nm",
			attach: &nadv1.NetworkSelectionElement{
				Name:      "nm",
				Namespace: "ns",
			},
			expt: true,
		},
		{
			name:                 "errFormat",
			defaultNetAnnotation: "err",
			attach: &nadv1.NetworkSelectionElement{
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
