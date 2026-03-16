package util

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestGenNatGwName(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Test case 1",
			input:    "test-nat-gw",
			expected: "vpc-nat-gw-test-nat-gw",
		},
		{
			name:     "Test case 2",
			input:    "my-nat-gateway",
			expected: "vpc-nat-gw-my-nat-gateway",
		},
		{
			name:     "Test case 3",
			input:    "",
			expected: "vpc-nat-gw-",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := GenNatGwName(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %s, but got %s", tc.expected, result)
			}
		})
	}
}

func TestGenNatGwNameWithCustomPrefix(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Test case 1",
			input:    "test-nat-gw",
			expected: "custom-prefix-test-nat-gw",
		},
		{
			name:     "Test case 2",
			input:    "my-nat-gateway",
			expected: "custom-prefix-my-nat-gateway",
		},
		{
			name:     "Test case 3",
			input:    "",
			expected: "custom-prefix-",
		},
	}

	// It is possible to override the default prefix appended to NAT GW statefulsets
	VpcNatGwNamePrefix = "custom-prefix"
	t.Cleanup(func() {
		VpcNatGwNamePrefix = VpcNatGwNameDefaultPrefix
	})

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := GenNatGwName(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %s, but got %s", tc.expected, result)
			}
		})
	}
}

func TestGenNatGwPodName(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Test case 1",
			input:    "test-nat-gw",
			expected: "vpc-nat-gw-test-nat-gw-0",
		},
		{
			name:     "Test case 2",
			input:    "my-nat-gateway",
			expected: "vpc-nat-gw-my-nat-gateway-0",
		},
		{
			name:     "Test case 3",
			input:    "",
			expected: "vpc-nat-gw--0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := GenNatGwPodName(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %s, but got %s", tc.expected, result)
			}
		})
	}
}

func TestGenNatGwPodNameWithCustomPrefix(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Test case 1",
			input:    "test-nat-gw",
			expected: "another-prefix-test-nat-gw-0",
		},
		{
			name:     "Test case 2",
			input:    "my-nat-gateway",
			expected: "another-prefix-my-nat-gateway-0",
		},
		{
			name:     "Test case 3",
			input:    "",
			expected: "another-prefix--0",
		},
	}

	// It is possible to override the default prefix appended to NAT GW pods
	VpcNatGwNamePrefix = "another-prefix"
	t.Cleanup(func() {
		VpcNatGwNamePrefix = VpcNatGwNameDefaultPrefix
	})

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := GenNatGwPodName(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %s, but got %s", tc.expected, result)
			}
		})
	}
}

func TestGetNatGwExternalNetwork(t *testing.T) {
	tests := []struct {
		name         string
		externalNets []string
		expected     string
	}{
		{
			name:         "External network specified",
			externalNets: []string{"custom-external-network"},
			expected:     "custom-external-network",
		},
		{
			name:         "External network not specified",
			externalNets: []string{},
			expected:     vpcExternalNet,
		},
		{
			name:         "Multiple external networks specified",
			externalNets: []string{"custom-external-network1", "custom-external-network2"},
			expected:     "custom-external-network1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := GetNatGwExternalNetwork(tc.externalNets)
			if result != tc.expected {
				t.Errorf("got %v, but want %v", result, tc.expected)
			}
		})
	}
}

func TestGenNatGwLabels(t *testing.T) {
	tests := []struct {
		name     string
		gwName   string
		expected map[string]string
	}{
		{
			name:   "Gateway name filled",
			gwName: "test-gateway",
			expected: map[string]string{
				"app":                  "vpc-nat-gw-test-gateway",
				VpcNatGatewayLabel:     "true",
				VpcNatGatewayNameLabel: "test-gateway",
			},
		},
		{
			name:   "Gateway label empty",
			gwName: "",
			expected: map[string]string{
				"app":                  "vpc-nat-gw-",
				VpcNatGatewayLabel:     "true",
				VpcNatGatewayNameLabel: "",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := GenNatGwLabels(tc.gwName)
			if !reflect.DeepEqual(tc.expected, result) {
				t.Errorf("got %v, but want %v", result, tc.expected)
			}
		})
	}
}

func TestGenNatGwSelectors(t *testing.T) {
	tests := []struct {
		name      string
		selectors []string
		expected  map[string]string
	}{
		{
			name:      "One selector",
			selectors: []string{"kubernetes.io/hostname: kube-ovn-worker"},
			expected: map[string]string{
				"kubernetes.io/hostname": "kube-ovn-worker",
			},
		},
		{
			name:      "Empty selector",
			selectors: []string{},
			expected:  map[string]string{},
		},
		{
			name:      "Multiple selectors",
			selectors: []string{"kubernetes.io/hostname: kube-ovn-worker", "kubernetes.io/os: linux"},
			expected: map[string]string{
				"kubernetes.io/hostname": "kube-ovn-worker",
				"kubernetes.io/os":       "linux",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := GenNatGwSelectors(tc.selectors)
			if !reflect.DeepEqual(tc.expected, result) {
				t.Errorf("got %v, but want %v", result, tc.expected)
			}
		})
	}
}

func TestGenNatGwPodAnnotations(t *testing.T) {
	tests := []struct {
		name                 string
		gw                   v1.VpcNatGateway
		externalNadNamespace string
		externalNadName      string
		provider             string
		additionalNetworks   string
		expected             map[string]string
		expectError          bool
	}{
		{
			name: "Empty provider defaults to ovn",
			gw: v1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gateway",
				},
				Spec: v1.VpcNatGatewaySpec{
					Subnet: "internal-subnet",
					LanIP:  "10.20.30.40",
				},
			},
			externalNadName:      "external-subnet",
			externalNadNamespace: metav1.NamespaceSystem,
			provider:             "",
			additionalNetworks:   "",
			expected: map[string]string{
				VpcNatGatewayAnnotation:      "test-gateway",
				nadv1.NetworkAttachmentAnnot: "kube-system/external-subnet",
				LogicalSwitchAnnotation:      "internal-subnet",
				IPAddressAnnotation:          "10.20.30.40",
			},
			expectError: false,
		},
		{
			name: "All fields provided with ovn provider",
			gw: v1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gateway",
				},
				Spec: v1.VpcNatGatewaySpec{
					Subnet: "internal-subnet",
					LanIP:  "10.20.30.40",
				},
			},
			externalNadName:      "external-subnet",
			externalNadNamespace: metav1.NamespaceSystem,
			provider:             OvnProvider,
			additionalNetworks:   "",
			expected: map[string]string{
				VpcNatGatewayAnnotation:      "test-gateway",
				nadv1.NetworkAttachmentAnnot: "kube-system/external-subnet",
				LogicalSwitchAnnotation:      "internal-subnet",
				IPAddressAnnotation:          "10.20.30.40",
			},
			expectError: false,
		},
		{
			name: "All fields provided with NAD provider",
			gw: v1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gateway",
				},
				Spec: v1.VpcNatGatewaySpec{
					Subnet: "internal-subnet",
					LanIP:  "10.20.30.40",
				},
			},
			externalNadName:      "external-subnet",
			externalNadNamespace: metav1.NamespaceSystem,
			provider:             "subnet.namespace.ovn",
			additionalNetworks:   "",
			expected: map[string]string{
				VpcNatGatewayAnnotation:      "test-gateway",
				nadv1.NetworkAttachmentAnnot: "kube-system/external-subnet",
				fmt.Sprintf(LogicalSwitchAnnotationTemplate, "subnet.namespace.ovn"): "internal-subnet",
				fmt.Sprintf(IPAddressAnnotationTemplate, "subnet.namespace.ovn"):     "10.20.30.40",
				DefaultNetworkAnnotation: "namespace/subnet",
			},
			expectError: false,
		},
		{
			name: "With additional networks for secondary CNI",
			gw: v1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gateway",
				},
				Spec: v1.VpcNatGatewaySpec{
					Subnet: "internal-subnet",
					LanIP:  "10.20.30.40",
				},
			},
			externalNadName:      "external-subnet",
			externalNadNamespace: metav1.NamespaceSystem,
			provider:             "subnet.namespace.ovn",
			additionalNetworks:   "default/extra-net1, default/extra-net2",
			expected: map[string]string{
				VpcNatGatewayAnnotation:      "test-gateway",
				nadv1.NetworkAttachmentAnnot: "default/extra-net1, default/extra-net2, kube-system/external-subnet",
				fmt.Sprintf(LogicalSwitchAnnotationTemplate, "subnet.namespace.ovn"): "internal-subnet",
				fmt.Sprintf(IPAddressAnnotationTemplate, "subnet.namespace.ovn"):     "10.20.30.40",
				DefaultNetworkAnnotation: "namespace/subnet",
			},
			expectError: false,
		},
		{
			name: "No static LAN IP",
			gw: v1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gateway",
				},
				Spec: v1.VpcNatGatewaySpec{
					Subnet: "internal-subnet",
					LanIP:  "",
				},
			},
			externalNadName:      "external-subnet",
			externalNadNamespace: metav1.NamespaceSystem,
			provider:             OvnProvider,
			additionalNetworks:   "",
			expected: map[string]string{
				VpcNatGatewayAnnotation:      "test-gateway",
				nadv1.NetworkAttachmentAnnot: "kube-system/external-subnet",
				LogicalSwitchAnnotation:      "internal-subnet",
				IPAddressAnnotation:          "",
			},
			expectError: false,
		},
		{
			name: "Invalid provider syntax",
			gw: v1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gateway",
				},
				Spec: v1.VpcNatGatewaySpec{
					Subnet: "internal-subnet",
					LanIP:  "10.20.30.40",
				},
			},
			externalNadName:      "external-subnet",
			externalNadNamespace: metav1.NamespaceSystem,
			provider:             "invalid-provider",
			additionalNetworks:   "",
			expected:             nil,
			expectError:          true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := GenNatGwPodAnnotations(nil, &tc.gw, tc.externalNadNamespace, tc.externalNadName, tc.provider, tc.additionalNetworks)
			if (err != nil) != tc.expectError {
				t.Errorf("expected error: %v, but got: %v", tc.expectError, err)
			}
			if !reflect.DeepEqual(tc.expected, result) {
				t.Errorf("got %v, but want %v", result, tc.expected)
			}
		})
	}
}

func TestGenNatGwBgpSpeakerContainer(t *testing.T) {
	tests := []struct {
		name          string
		speakerImage  string
		gatewayName   string
		speakerParams v1.VpcBgpSpeaker
		mustError     bool
	}{
		{
			name:          "Speaker with missing params",
			speakerImage:  "kubeovn.io/fake/image:latest",
			gatewayName:   "test-gateway",
			speakerParams: v1.VpcBgpSpeaker{},
			mustError:     true,
		},
		{
			name:         "Speaker dualstack neighbors",
			speakerImage: "kubeovn.io/fake/image:latest",
			gatewayName:  "working-fw",
			speakerParams: v1.VpcBgpSpeaker{
				ASN:       123456,
				RemoteASN: 213219,
				Neighbors: []string{"10.10.10.10", "fd00::a", "1.2.3.4", "fd00:c00f:ee::"},
			},
			mustError: false,
		},
		{
			name:         "Only v6 neighbors",
			speakerImage: "kubeovn.io/fake/image:latest",
			gatewayName:  "working-fw",
			speakerParams: v1.VpcBgpSpeaker{
				ASN:       123456,
				RemoteASN: 213219,
				Neighbors: []string{"fd00::a", "fd00:c00f:ee::"},
			},
			mustError: false,
		},
		{
			name:         "Only v4 neighbors",
			speakerImage: "kubeovn.io/fake/image:latest",
			gatewayName:  "working-fw",
			speakerParams: v1.VpcBgpSpeaker{
				ASN:       123456,
				RemoteASN: 213219,
				Neighbors: []string{"10.10.10.10", "1.2.3.4"},
			},
			mustError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := GenNatGwBgpSpeakerContainer(tc.speakerParams, tc.speakerImage, tc.gatewayName)
			if !tc.mustError && err != nil {
				t.Errorf("generation returned error: %s", err.Error())
			} else if tc.mustError && err != nil {
				return
			}

			if result.Name != "vpc-nat-gw-speaker" {
				t.Errorf("speaker container doesn't have the right name, expected vpc-nat-gw-speaker, got %s", result.Name)
			}

			if result.Image != tc.speakerImage {
				t.Errorf("speaker container has wrong image, expected %s, got %s", tc.speakerImage, result.Image)
			}

			// Speaker MUST be in NAT GW mode
			if result.Args[0] != "--nat-gw-mode" {
				t.Errorf("speaker not running in NAT gateway mode")
			}

			// Check we inject the gateway name correctly, used by the speaker to retrieve EIPs by ownership
			firstEnv := result.Env[0]
			if firstEnv.Name != EnvGatewayName || firstEnv.Value != tc.gatewayName {
				t.Errorf("gateway name env injection is faulty, got %v", firstEnv)
			}
		})
	}
}

func TestGenMacvlanIfaceName(t *testing.T) {
	tests := []struct {
		name     string
		master   string
		expected string
	}{
		{
			name:     "short interface name",
			master:   "eth0",
			expected: "maceth0",
		},
		{
			name:     "bond interface",
			master:   "bond0",
			expected: "macbond0",
		},
		{
			name:     "longer interface name",
			master:   "enp0s25",
			expected: "macenp0s25",
		},
		{
			name:     "max length interface name (12 chars)",
			master:   "eth0.1234567",
			expected: "maceth0.1234567",
		},
		{
			name:   "long interface name (exceeds 12 chars, uses hash)",
			master: "very-long-interface-name",
			// Name will be "mac" + 8 hex chars (FNV-1a hash)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GenMacvlanIfaceName(tt.master)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result) > 15 {
				t.Errorf("interface name should not exceed 15 chars, got %d", len(result))
			}
			if !strings.HasPrefix(result, MacvlanLinkPrefix) {
				t.Errorf("should have '%s' prefix, got %s", MacvlanLinkPrefix, result)
			}
			if tt.expected != "" && result != tt.expected {
				t.Errorf("got %s, want %s", result, tt.expected)
			}
			// Verify consistent output
			result2, _ := GenMacvlanIfaceName(tt.master)
			if result != result2 {
				t.Errorf("same input should produce same output: %s vs %s", result, result2)
			}
		})
	}

	// Verify uniqueness for different master interfaces
	t.Run("different masters produce different outputs", func(t *testing.T) {
		names := make(map[string]string)
		inputs := []string{
			"eth0",
			"eth1",
			"bond0",
			"bond1",
			"enp0s25",
			"very-long-interface-name",
			"another-long-interface",
		}
		for _, input := range inputs {
			result, err := GenMacvlanIfaceName(input)
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", input, err)
			}
			if existing, ok := names[result]; ok {
				t.Errorf("collision detected: %q and %q both produce %q", existing, input, result)
			}
			names[result] = input
		}
	})

	// Verify that long interface names are properly hashed
	t.Run("long names are hashed to fit 15 char limit", func(t *testing.T) {
		longName := "this-is-a-very-long-interface-name"
		result, err := GenMacvlanIfaceName(longName)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) > 15 {
			t.Errorf("should not exceed 15 chars, got %d", len(result))
		}
		if !strings.HasPrefix(result, MacvlanLinkPrefix) {
			t.Errorf("should have '%s' prefix", MacvlanLinkPrefix)
		}
		// Should be "mac" + 8 hex chars = 11 chars
		if len(result) != 11 {
			t.Errorf("expected 11 chars for hashed name, got %d", len(result))
		}
	})

	t.Run("empty master returns error", func(t *testing.T) {
		_, err := GenMacvlanIfaceName("")
		if err == nil {
			t.Error("expected error for empty master")
		}
		if !strings.Contains(err.Error(), "empty") {
			t.Errorf("error should mention 'empty', got: %v", err)
		}
	})
}
