package controller

import (
	"fmt"
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestKubeOvnAnnotationsChanged(t *testing.T) {
	tests := []struct {
		name           string
		oldAnnotations map[string]string
		newAnnotations map[string]string
		expected       bool
	}{
		{
			name:           "no annotations",
			oldAnnotations: map[string]string{},
			newAnnotations: map[string]string{},
			expected:       false,
		},
		{
			name:           "kube-ovn annotation added",
			oldAnnotations: map[string]string{},
			newAnnotations: map[string]string{
				util.AllocatedAnnotation: "true",
			},
			expected: true,
		},
		{
			name: "kube-ovn annotation removed",
			oldAnnotations: map[string]string{
				util.AllocatedAnnotation: "true",
			},
			newAnnotations: map[string]string{},
			expected:       true,
		},
		{
			name: "kube-ovn annotation value changed",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
			},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.2",
			},
			expected: true,
		},
		{
			name: "kube-ovn annotation unchanged",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
			},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
			},
			expected: false,
		},
		{
			name: "non-kube-ovn annotation changed",
			oldAnnotations: map[string]string{
				"other.io/annotation": "value1",
			},
			newAnnotations: map[string]string{
				"other.io/annotation": "value2",
			},
			expected: false,
		},
		{
			name: "mixed annotations, only non-kube-ovn changed",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
				"other.io/annotation":    "value1",
			},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
				"other.io/annotation":    "value2",
			},
			expected: false,
		},
		{
			name: "mixed annotations, kube-ovn changed",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
				"other.io/annotation":    "value1",
			},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.2",
				"other.io/annotation":    "value2",
			},
			expected: true,
		},
		{
			name: "multiple kube-ovn annotations unchanged",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation:  "10.0.0.1",
				util.MacAddressAnnotation: "00:11:22:33:44:55",
				util.AllocatedAnnotation:  "true",
			},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation:  "10.0.0.1",
				util.MacAddressAnnotation: "00:11:22:33:44:55",
				util.AllocatedAnnotation:  "true",
			},
			expected: false,
		},
		{
			name: "multiple kube-ovn annotations, one changed",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation:  "10.0.0.1",
				util.MacAddressAnnotation: "00:11:22:33:44:55",
			},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation:  "10.0.0.1",
				util.MacAddressAnnotation: "00:11:22:33:44:56",
			},
			expected: true,
		},
		{
			name: "provider network annotation changed",
			oldAnnotations: map[string]string{
				fmt.Sprintf(util.ProviderNetworkTemplate, "net1"): "provider1",
			},
			newAnnotations: map[string]string{
				fmt.Sprintf(util.ProviderNetworkTemplate, "net1"): "provider2",
			},
			expected: true,
		},
		{
			name: "annotation with kubernetes.io in value not key",
			oldAnnotations: map[string]string{
				"some.annotation": "value.kubernetes.io",
			},
			newAnnotations: map[string]string{
				"some.annotation": "changed.kubernetes.io",
			},
			expected: false,
		},
		{
			name:           "empty to kube-ovn annotations",
			oldAnnotations: map[string]string{},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation:  "10.0.0.1",
				util.MacAddressAnnotation: "00:11:22:33:44:55",
				util.ChassisAnnotation:    "node1",
			},
			expected: true,
		},
		{
			name: "kube-ovn annotations to empty",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation:  "10.0.0.1",
				util.MacAddressAnnotation: "00:11:22:33:44:55",
			},
			newAnnotations: map[string]string{},
			expected:       true,
		},
		{
			name: "non-kube-ovn added and removed",
			oldAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
				"old.annotation":         "value",
			},
			newAnnotations: map[string]string{
				util.IPAddressAnnotation: "10.0.0.1",
				"new.annotation":         "value",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := kubeOvnAnnotationsChanged(tt.oldAnnotations, tt.newAnnotations)
			if result != tt.expected {
				t.Errorf("kubeOvnAnnotationsChanged() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
