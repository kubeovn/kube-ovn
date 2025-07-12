package controller

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeovn/kube-ovn/pkg/util"
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

func TestGetEndpointTargetLSPNameFromProvider(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		provider string
		expected string
	}{
		{
			name: "No provider, faulty pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "",
					Namespace: "",
				},
			},
			provider: "",
			expected: ".",
		},
		{
			name: "Endpoint empty with default provider and faulty pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "",
					Namespace: "",
				},
			},
			provider: util.OvnProvider,
			expected: ".",
		},
		{
			name: "Pod target with no provider",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-pod-1d8fn",
					Namespace: "default",
				},
			},
			provider: "",
			expected: "some-pod-1d8fn.default",
		},
		{
			name: "Pod target with default provider",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-pod-1d8fn",
					Namespace: "default",
				},
			},
			provider: util.OvnProvider,
			expected: "some-pod-1d8fn.default",
		},
		{
			name: "Pod target with custom provider",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-pod-1d8fn",
					Namespace: "default",
				},
			},
			provider: "custom.provider",
			expected: "some-pod-1d8fn.default.custom.provider",
		},
		{
			name: "VM target with no provider",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "virt-launcher-some-vm-67jd3",
					Namespace: "default",
					Annotations: map[string]string{
						fmt.Sprintf(util.VMAnnotationTemplate, util.OvnProvider): "some-vm",
					},
				},
			},
			provider: "",
			expected: "some-vm.default",
		},
		{
			name: "VM target with default provider",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "virt-launcher-some-vm-67jd3",
					Namespace: "default",
					Annotations: map[string]string{
						fmt.Sprintf(util.VMAnnotationTemplate, util.OvnProvider): "some-vm",
					},
				},
			},
			provider: util.OvnProvider,
			expected: "some-vm.default",
		},
		{
			name: "VM target with custom provider",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "virt-launcher-some-vm-67jd3",
					Namespace: "default",
					Annotations: map[string]string{
						fmt.Sprintf(util.VMAnnotationTemplate, "custom.provider"): "some-vm",
					},
				},
			},
			provider: "custom.provider",
			expected: "some-vm.default.custom.provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEndpointTargetLSPNameFromProvider(tt.pod, tt.provider)
			if result != tt.expected {
				t.Errorf("getEndpointTargetLSPName() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetMatchingProviderForAddress(t *testing.T) {
	tests := []struct {
		name      string
		pod       *corev1.Pod
		providers []string
		address   string
		expected  string
	}{
		{
			name: "IP is on default provider and single stack",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "1.1.1.1",
					},
				},
			},
			providers: []string{util.OvnProvider},
			address:   "1.1.1.1",
			expected:  util.OvnProvider,
		},
		{
			name: "IP is on default provider and dual stack",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "1.1.1.1,fd00::a",
					},
				},
			},
			providers: []string{util.OvnProvider},
			address:   "fd00::a",
			expected:  util.OvnProvider,
		},
		{
			name: "IP is on custom provider and dual stack",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fmt.Sprintf(util.IPAddressAnnotationTemplate, "custom.provider"): "1.1.1.1,fd00::a",
					},
				},
			},
			providers: []string{"custom.provider"},
			address:   "fd00::a",
			expected:  "custom.provider",
		},
		{
			name: "IP is on custom provider with multiple other providers present on the pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider):  "10.0.0.1,fd10:0:0::1",
						fmt.Sprintf(util.IPAddressAnnotationTemplate, "first.provider"):  "1.1.1.1,fd00::a",
						fmt.Sprintf(util.IPAddressAnnotationTemplate, "second.provider"): "2.2.2.2,fd00::b",
						fmt.Sprintf(util.IPAddressAnnotationTemplate, "third.provider"):  "3.3.3.3,fd00::c",
					},
				},
			},
			providers: []string{util.OvnProvider, "first.provider", "second.provider", "third.provider"},
			address:   "fd00::b",
			expected:  "second.provider",
		},
		{
			name: "No provider is matching",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.0.0.1,fd10:0:0::1",
					},
				},
			},
			providers: []string{util.OvnProvider},
			address:   "fd00::b",
			expected:  "",
		},
		{
			name: "No annotation is present",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: nil,
				},
			},
			providers: []string{},
			address:   "fd00::b",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getMatchingProviderForAddress(tt.pod, tt.providers, tt.address)
			if result != tt.expected {
				t.Errorf("getMatchingProviderForAddress() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestServiceHasSelector(t *testing.T) {
	tests := []struct {
		name     string
		service  *corev1.Service
		expected bool
	}{
		{
			name: "Service has no selector",
			service: &corev1.Service{Spec: corev1.ServiceSpec{
				Selector: nil,
			}},
			expected: false,
		},
		{
			name: "Service has empty selectors",
			service: &corev1.Service{Spec: corev1.ServiceSpec{
				Selector: map[string]string{},
			}},
			expected: false,
		},
		{
			name: "Service has one selector",
			service: &corev1.Service{Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"a": "b",
				},
			}},
			expected: true,
		},
		{
			name: "Service has multiple selectors",
			service: &corev1.Service{Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"a": "b",
					"c": "d",
				},
			}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := serviceHasSelector(tt.service)
			if result != tt.expected {
				t.Errorf("serviceHasSelector() = %t, want %t", result, tt.expected)
			}
		})
	}
}
