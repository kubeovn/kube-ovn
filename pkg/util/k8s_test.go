package util

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetNodeInternalIP(t *testing.T) {
	tests := []struct {
		name string
		node v1.Node
		exp4 string
		exp6 string
	}{
		{
			name: "correct",
			node: v1.Node{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       v1.NodeSpec{},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{
							Type:    "InternalIP",
							Address: "192.168.0.2",
						},
						{
							Type:    "ExternalIP",
							Address: "192.188.0.4",
						},
						{
							Type:    "InternalIP",
							Address: "ffff:ffff:ffff:ffff:ffff::23",
						},
					},
				},
			},
			exp4: "192.168.0.2",
			exp6: "ffff:ffff:ffff:ffff:ffff::23",
		},
		{
			name: "correctWithDiff",
			node: v1.Node{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       v1.NodeSpec{},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{
							Type:    "InternalIP",
							Address: "ffff:ffff:ffff:ffff:ffff::23",
						},
						{
							Type:    "ExternalIP",
							Address: "192.188.0.4",
						},
						{
							Type:    "InternalIP",
							Address: "192.188.0.43",
						},
					},
				},
			},
			exp4: "192.188.0.43",
			exp6: "ffff:ffff:ffff:ffff:ffff::23",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ret4, ret6 := GetNodeInternalIP(tt.node); ret4 != tt.exp4 || ret6 != tt.exp6 {
				t.Errorf("got %v, %v, want %v, %v", ret4, ret6, tt.exp4, tt.exp6)
			}
		})
	}
}
