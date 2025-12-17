package util

import (
	"reflect"
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
				"app":              "vpc-nat-gw-test-gateway",
				VpcNatGatewayLabel: "true",
			},
		},
		{
			name:   "Gateway label empty",
			gwName: "",
			expected: map[string]string{
				"app":              "vpc-nat-gw-",
				VpcNatGatewayLabel: "true",
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
		name                    string
		gw                      v1.VpcNatGateway
		externalNadNamespace    string
		externalNadName         string
		defaultVpcNadNamespace  string
		defaultVpcNadName       string
		expected                map[string]string
	}{
		{
			name: "All fields provided without default SNAT",
			gw: v1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gateway",
				},
				Spec: v1.VpcNatGatewaySpec{
					Subnet:            "internal-subnet",
					LanIP:             "10.20.30.40",
					EnableDefaultSnat: false,
				},
			},
			externalNadName:      "external-subnet",
			externalNadNamespace: "kube-system",
			defaultVpcNadName:    "",
			defaultVpcNadNamespace: "",
			expected: map[string]string{
				VpcNatGatewayAnnotation:      "test-gateway",
				nadv1.NetworkAttachmentAnnot: "kube-system/external-subnet",
				LogicalSwitchAnnotation:      "internal-subnet",
				IPAddressAnnotation:          "10.20.30.40",
			},
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
			externalNadNamespace: "kube-system",
			defaultVpcNadName:    "",
			defaultVpcNadNamespace: "",
			expected: map[string]string{
				VpcNatGatewayAnnotation:      "test-gateway",
				nadv1.NetworkAttachmentAnnot: "kube-system/external-subnet",
				LogicalSwitchAnnotation:      "internal-subnet",
				IPAddressAnnotation:          "",
			},
		},
		{
			name: "With default VPC SNAT enabled",
			gw: v1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gateway",
				},
				Spec: v1.VpcNatGatewaySpec{
					Subnet:            "internal-subnet",
					LanIP:             "10.20.30.40",
					EnableDefaultSnat: true,
					DefaultSnatSubnet: "ovn-default",
				},
			},
			externalNadName:        "external-subnet",
			externalNadNamespace:   "kube-system",
			defaultVpcNadName:      "ovn-default",
			defaultVpcNadNamespace: "kube-system",
			expected: map[string]string{
				VpcNatGatewayAnnotation:      "test-gateway",
				nadv1.NetworkAttachmentAnnot: "kube-system/external-subnet,kube-system/ovn-default",
				LogicalSwitchAnnotation:      "internal-subnet",
				IPAddressAnnotation:          "10.20.30.40",
			},
		},
		{
			name: "EnableDefaultSnat true but no NAD name provided",
			gw: v1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gateway",
				},
				Spec: v1.VpcNatGatewaySpec{
					Subnet:            "internal-subnet",
					LanIP:             "10.20.30.40",
					EnableDefaultSnat: true,
					DefaultSnatSubnet: "ovn-default",
				},
			},
			externalNadName:        "external-subnet",
			externalNadNamespace:   "kube-system",
			defaultVpcNadName:      "", // NAD name empty
			defaultVpcNadNamespace: "kube-system",
			expected: map[string]string{
				VpcNatGatewayAnnotation:      "test-gateway",
				nadv1.NetworkAttachmentAnnot: "kube-system/external-subnet", // Should not add net2
				LogicalSwitchAnnotation:      "internal-subnet",
				IPAddressAnnotation:          "10.20.30.40",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := GenNatGwPodAnnotations(&tc.gw, tc.externalNadNamespace, tc.externalNadName, tc.defaultVpcNadNamespace, tc.defaultVpcNadName)
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
			if firstEnv.Name != GatewayNameEnv || firstEnv.Value != tc.gatewayName {
				t.Errorf("gateway name env injection is faulty, got %v", firstEnv)
			}
		})
	}
}
