package controller

import (
	"testing"

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFindServiceKey(t *testing.T) {
	tests := []struct {
		name          string
		endpointSlice *discoveryv1.EndpointSlice
		expectedKey   string
	}{
		{
			name: "valid endpoint slice with service name",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Labels: map[string]string{
						discoveryv1.LabelServiceName: "test-service",
					},
				},
			},
			expectedKey: "default/test-service",
		},
		{
			name: "endpoint slice with empty service name",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Labels: map[string]string{
						discoveryv1.LabelServiceName: "",
					},
				},
			},
			expectedKey: "",
		},
		{
			name: "endpoint slice with no labels",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
			},
			expectedKey: "",
		},
		{
			name:          "nil endpoint slice",
			endpointSlice: nil,
			expectedKey:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findServiceKey(tt.endpointSlice)
			if result != tt.expectedKey {
				t.Errorf("findServiceKey() = %q, want %q", result, tt.expectedKey)
			}
		})
	}
}

func TestEndpointReady(t *testing.T) {
	trueVal := true
	falseVal := false
	tests := []struct {
		name     string
		endpoint discoveryv1.Endpoint
		want     bool
	}{
		{
			name: "Ready is nil (should be ready)",
			endpoint: discoveryv1.Endpoint{
				Conditions: discoveryv1.EndpointConditions{
					Ready: nil,
				},
			},
			want: true,
		},
		{
			name: "Ready is true",
			endpoint: discoveryv1.Endpoint{
				Conditions: discoveryv1.EndpointConditions{
					Ready: &trueVal,
				},
			},
			want: true,
		},
		{
			name: "Ready is false",
			endpoint: discoveryv1.Endpoint{
				Conditions: discoveryv1.EndpointConditions{
					Ready: &falseVal,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := endpointReady(tt.endpoint)
			if got != tt.want {
				t.Errorf("endpointReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEndpointTargetLSP(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		namespace string
		provider  string
		expected  string
	}{
		{
			name:      "Endpoint empty",
			target:    "",
			namespace: "",
			provider:  "",
			expected:  "..",
		},
		{
			name:      "Endpoint empty with default provider",
			target:    "",
			namespace: "",
			provider:  "ovn",
			expected:  ".",
		},
		{
			name:      "Pod target with default provider",
			target:    "some-pod-1d8fn",
			namespace: "default",
			provider:  "ovn",
			expected:  "some-pod-1d8fn.default",
		},
		{
			name:      "Pod target with custom provider",
			target:    "some-pod-6xjd8",
			namespace: "default",
			provider:  "custom.provider",
			expected:  "some-pod-6xjd8.default.custom.provider",
		},
		{
			name:      "VM target with default provider",
			target:    "virt-launcher-some-vm-67jd3",
			namespace: "default",
			provider:  "ovn",
			expected:  "some-vm.default",
		},
		{
			name:      "VM target with custom provider",
			target:    "virt-launcher-some-vm-67jd3",
			namespace: "default",
			provider:  "custom.provider",
			expected:  "some-vm.default.custom.provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEndpointTargetLSP(tt.target, tt.namespace, tt.provider)
			if result != tt.expected {
				t.Errorf("getEndpointTargetLSP() = %q, want %q", result, tt.expected)
			}
		})
	}
}
