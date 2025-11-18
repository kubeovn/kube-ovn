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

func TestGetNadInterfaceFromNetworkStatusAnnotation(t *testing.T) {
	tests := []struct {
		name           string
		statusJSON     string
		nadName        string
		expectedResult string
		expectError    bool
		description    string
	}{
		{
			name: "Valid network status with matching NAD name",
			statusJSON: `[{
				"name": "ovn-cluster/test-subnet",
				"interface": "net1",
				"ips": ["192.168.1.10"],
				"dns": {},
				"device-info": {
					"type": "kube-ovn",
					"version": "1.0.0"
				}
			}]`,
			nadName:        "ovn-cluster/test-subnet",
			expectedResult: "net1",
			expectError:    false,
			description:    "Should extract interface name for exact NAD name match",
		},
		{
			name: "Valid multiple interfaces, correct NAD name",
			statusJSON: `[
				{
					"name": "flannel",
					"interface": "eth0",
					"ips": ["10.244.0.10"],
					"device-info": {"type": "flannel"}
				},
				{
					"name": "ovn-cluster/test-subnet",
					"interface": "net1",
					"ips": ["192.168.1.10"],
					"device-info": {"type": "kube-ovn"}
				}
			]`,
			nadName:        "ovn-cluster/test-subnet",
			expectedResult: "net1",
			expectError:    false,
			description:    "Should find interface with correct NAD name from multiple entries",
		},
		{
			name: "Valid network status but non-matching NAD name",
			statusJSON: `[{
				"name": "ovn-cluster/test-subnet",
				"interface": "net1",
				"ips": ["192.168.1.10"],
				"device-info": {
					"type": "kube-ovn",
					"version": "1.0.0"
				}
			}]`,
			nadName:        "different-network",
			expectedResult: "",
			expectError:    true,
			description:    "Should return error when NAD name doesn't match any network",
		},
		{
			name: "Interface missing from network status",
			statusJSON: `[{
				"name": "ovn-cluster/test-subnet",
				"ips": ["192.168.1.10"],
				"device-info": {
					"type": "kube-ovn",
					"version": "1.0.0"
				}
			}]`,
			nadName:        "ovn-cluster/test-subnet",
			expectedResult: "",
			expectError:    true,
			description:    "Should return error when interface field is missing",
		},
		{
			name:           "Invalid input - empty status or malformed JSON",
			statusJSON:     `invalid json`,
			nadName:        "ovn-cluster/test-subnet",
			expectedResult: "",
			expectError:    true,
			description:    "Should return error for invalid JSON, empty status, or empty arrays",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetNadInterfaceFromNetworkStatusAnnotation(tt.statusJSON, tt.nadName)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none: %s", tt.description)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v - %s", err, tt.description)
				return
			}

			if result != tt.expectedResult {
				t.Errorf("Expected %q but got %q: %s", tt.expectedResult, result, tt.description)
			}
		})
	}

	// Test additional error scenarios in a single sub-test for efficiency
	t.Run("Additional error scenarios", func(t *testing.T) {
		errorCases := []struct {
			statusJSON string
			nadName    string
			scenario   string
		}{
			{"", "ovn-cluster/test-subnet", "empty status"},
			{`[]`, "ovn-cluster/test-subnet", "empty array"},
			{`malformed`, "ovn-cluster/test-subnet", "malformed JSON"},
		}

		for _, ec := range errorCases {
			_, err := GetNadInterfaceFromNetworkStatusAnnotation(ec.statusJSON, ec.nadName)
			if err == nil {
				t.Errorf("Expected error for %s but got none", ec.scenario)
			}
		}
	})
}
