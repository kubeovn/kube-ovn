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
			input:    "gateway123",
			expected: "vpc-nat-gw-gateway123",
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
			input:    "gateway123",
			expected: "vpc-nat-gw-gateway123-0",
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

func TestGetNatGwExternalNetwork(t *testing.T) {
	testCases := []struct {
		name            string
		externalSubnets []string
		expectedName    string
	}{
		{
			name:            "External network specified",
			externalSubnets: []string{"external-network"},
			expectedName:    "external-network",
		},
		{
			name:            "External network not specified",
			externalSubnets: []string{},
			expectedName:    "external",
		},
		{
			name:            "Multiple external networks specified",
			externalSubnets: []string{"network1", "network2"},
			expectedName:    "network1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			name := GetNatGwExternalNetwork(tc.externalSubnets)
			if name != tc.expectedName {
				t.Errorf("Expected name %s, but got %s", tc.expectedName, name)
			}
		})
	}
}

func TestGenNatGwLabels(t *testing.T) {
	testCases := []struct {
		name           string
		gatewayName    string
		expectedLabels map[string]string
	}{
		{
			name:        "Gateway name filled",
			gatewayName: "test-gateway",
			expectedLabels: map[string]string{
				"app":              "vpc-nat-gw-test-gateway",
				VpcNatGatewayLabel: "true",
			},
		},
		{
			name:        "Gateway label empty",
			gatewayName: "",
			expectedLabels: map[string]string{
				"app":              "vpc-nat-gw-",
				VpcNatGatewayLabel: "true",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			labels := GenNatGwLabels(tc.gatewayName)
			if !reflect.DeepEqual(labels, tc.expectedLabels) {
				t.Errorf("Expected labels %v, but got %v", tc.expectedLabels, labels)
			}
		})
	}
}

func TestGenNatGwSelectors(t *testing.T) {
	testCases := []struct {
		name              string
		selectors         []string
		expectedSelectors map[string]string
	}{
		{
			name:      "One selector",
			selectors: []string{"kubernetes.io/os: linux"},
			expectedSelectors: map[string]string{
				"kubernetes.io/os": "linux",
			},
		},
		{
			name:              "Empty selector",
			selectors:         []string{},
			expectedSelectors: map[string]string{},
		},
		{
			name:      "Multiple selectors",
			selectors: []string{"kubernetes.io/os: linux", "kubernetes.io/arch: amd64"},
			expectedSelectors: map[string]string{
				"kubernetes.io/os":   "linux",
				"kubernetes.io/arch": "amd64",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			selectors := GenNatGwSelectors(tc.selectors)
			if !reflect.DeepEqual(selectors, tc.expectedSelectors) {
				t.Errorf("Expected selectors %v, but got %v", tc.expectedSelectors, selectors)
			}
		})
	}
}

func TestGenNatGwPodAnnotations(t *testing.T) {
	testCases := []struct {
		name                 string
		gw                   *v1.VpcNatGateway
		externalNadNamespace string
		externalNadName      string
		expectedAnnotations  map[string]string
	}{
		{
			name: "All fields provided",
			gw: &v1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gw",
				},
				Spec: v1.VpcNatGatewaySpec{
					Subnet: "test-subnet",
					LanIP:  "10.0.0.1",
				},
			},
			externalNadNamespace: "default",
			externalNadName:      "external-net",
			expectedAnnotations: map[string]string{
				VpcNatGatewayAnnotation:  "test-gw",
				nadv1.NetworkAttachmentAnnot: "default/external-net",
				LogicalSwitchAnnotation:  "test-subnet",
				IPAddressAnnotation:      "10.0.0.1",
			},
		},
		{
			name: "No static LAN IP",
			gw: &v1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gw",
				},
				Spec: v1.VpcNatGatewaySpec{
					Subnet: "test-subnet",
					LanIP:  "",
				},
			},
			externalNadNamespace: "default",
			externalNadName:      "external-net",
			expectedAnnotations: map[string]string{
				VpcNatGatewayAnnotation:  "test-gw",
				nadv1.NetworkAttachmentAnnot: "default/external-net",
				LogicalSwitchAnnotation:  "test-subnet",
				IPAddressAnnotation:      "",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			annotations := GenNatGwPodAnnotations(tc.gw, tc.externalNadNamespace, tc.externalNadName)
			if !reflect.DeepEqual(annotations, tc.expectedAnnotations) {
				t.Errorf("Expected annotations %v, but got %v", tc.expectedAnnotations, annotations)
			}
		})
	}
}

func TestGenNatGwBgpSpeakerContainer(t *testing.T) {
	testCases := []struct {
		name        string
		speaker     v1.VpcBgpSpeaker
		image       string
		gatewayName string
		expectErr   bool
	}{
		{
			name: "Speaker with missing params",
			speaker: v1.VpcBgpSpeaker{
				Enabled: true,
			},
			image:       "speaker-image:latest",
			gatewayName: "nat-gw",
			expectErr:   true,
		},
		{
			name: "Speaker dualstack neighbors",
			speaker: v1.VpcBgpSpeaker{
				Enabled:   true,
				ASN:       64512,
				RemoteASN: 64513,
				Neighbors: []string{"192.168.1.1", "fd00::1"},
			},
			image:       "speaker-image:latest",
			gatewayName: "nat-gw",
			expectErr:   false,
		},
		{
			name: "Only v6 neighbors",
			speaker: v1.VpcBgpSpeaker{
				Enabled:   true,
				ASN:       64512,
				RemoteASN: 64513,
				Neighbors: []string{"fd00::1", "fd00::2"},
			},
			image:       "speaker-image:latest",
			gatewayName: "nat-gw",
			expectErr:   false,
		},
		{
			name: "Only v4 neighbors",
			speaker: v1.VpcBgpSpeaker{
				Enabled:   true,
				ASN:       64512,
				RemoteASN: 64513,
				Neighbors: []string{"192.168.1.1", "192.168.1.2"},
			},
			image:       "speaker-image:latest",
			gatewayName: "nat-gw",
			expectErr:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := GenNatGwBgpSpeakerContainer(tc.speaker, tc.image, tc.gatewayName)
			if tc.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Errorf("Expected a BGP speaker container but got nil")
				return
			}

			if result.Name != "vpc-nat-gw-speaker" {
				t.Errorf("Expected container name 'vpc-nat-gw-speaker' but got %s", result.Name)
			}

			// Check that gateway name environment variable is set correctly
			if len(result.Env) == 0 {
				t.Errorf("Expected environment variables to be set")
				return
			}

			firstEnv := result.Env[0]
			if firstEnv.Name != EnvGatewayName || firstEnv.Value != tc.gatewayName {
				t.Errorf("Expected gateway name env %s=%s, but got %s=%s", EnvGatewayName, tc.gatewayName, firstEnv.Name, firstEnv.Value)
			}
		})
	}
}
