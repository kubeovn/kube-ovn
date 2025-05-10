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
