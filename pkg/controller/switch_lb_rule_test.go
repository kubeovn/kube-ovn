package controller

import (
	"reflect"
	"testing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_getIPFamilies(t *testing.T) {
	tests := []struct {
		name                 string
		vip                  string
		expectedFamilies     []corev1.IPFamily
		expectedFamilyPolicy corev1.IPFamilyPolicy
	}{
		{
			name:                 "IPv4 VIP",
			vip:                  "10.0.0.0",
			expectedFamilies:     []corev1.IPFamily{corev1.IPv4Protocol},
			expectedFamilyPolicy: corev1.IPFamilyPolicySingleStack,
		},
		{
			name:                 "IPv6 VIP",
			vip:                  "fd00::1",
			expectedFamilies:     []corev1.IPFamily{corev1.IPv6Protocol},
			expectedFamilyPolicy: corev1.IPFamilyPolicySingleStack,
		},
		{
			name:                 "IPv6/v4 VIP",
			vip:                  "fd00::1,10.0.0.0",
			expectedFamilies:     []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			expectedFamilyPolicy: corev1.IPFamilyPolicyPreferDualStack,
		},
		{
			name:                 "IPv4/v6 VIP",
			vip:                  "10.0.0.0,fd00::1",
			expectedFamilies:     []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			expectedFamilyPolicy: corev1.IPFamilyPolicyPreferDualStack,
		},
		{
			name:                 "Many v4 VIP",
			vip:                  "10.0.0.0,10.0.0.1,10.0.0.2",
			expectedFamilies:     []corev1.IPFamily{corev1.IPv4Protocol},
			expectedFamilyPolicy: corev1.IPFamilyPolicySingleStack,
		},
		{
			name:                 "Many v6 VIP",
			vip:                  "fd00::1,fd00::2,fd00::3",
			expectedFamilies:     []corev1.IPFamily{corev1.IPv6Protocol},
			expectedFamilyPolicy: corev1.IPFamilyPolicySingleStack,
		},
		{
			name:                 "Many v6/v4 VIP",
			vip:                  "fd00::1,fd00::2,fd00::3,10.0.0.0,10.0.0.1,10.0.0.2",
			expectedFamilies:     []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			expectedFamilyPolicy: corev1.IPFamilyPolicyPreferDualStack,
		},
		{
			name:                 "Invalid",
			vip:                  "invalid",
			expectedFamilies:     []corev1.IPFamily{},
			expectedFamilyPolicy: corev1.IPFamilyPolicySingleStack,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			families, policy := getIPFamilies(tt.vip)

			if !reflect.DeepEqual(families, tt.expectedFamilies) {
				t.Errorf("Expected families %v, but got %v", tt.expectedFamilies, families)
			}

			if policy != tt.expectedFamilyPolicy {
				t.Errorf("Expected familiyPolicy %s, but got %s", tt.expectedFamilyPolicy, policy)
			}
		})
	}
}

func Test_setUserDefinedNetwork(t *testing.T) {
	tests := []struct {
		name    string
		service *corev1.Service
		slr     *kubeovnv1.SwitchLBRule
		result  *corev1.Service
	}{
		{
			name:    "Propagate VPC",
			service: &corev1.Service{},
			slr: &kubeovnv1.SwitchLBRule{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.LogicalRouterAnnotation: "test",
					},
				},
			},
			result: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.LogicalRouterAnnotation: "test",
					},
				},
			},
		},
		{
			name:    "Propagate Subnet",
			service: &corev1.Service{},
			slr: &kubeovnv1.SwitchLBRule{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.LogicalSwitchAnnotation: "test",
					},
				},
			},
			result: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.LogicalSwitchAnnotation: "test",
					},
				},
			},
		},
		{
			name:    "Propagate VPC/Subnet",
			service: &corev1.Service{},
			slr: &kubeovnv1.SwitchLBRule{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.LogicalRouterAnnotation: "test1",
						util.LogicalSwitchAnnotation: "test2",
					},
				},
			},
			result: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						util.LogicalRouterAnnotation: "test1",
						util.LogicalSwitchAnnotation: "test2",
					},
				},
			},
		},
		{
			name:    "Propagate nothing",
			service: &corev1.Service{},
			slr:     &kubeovnv1.SwitchLBRule{},
			result:  &corev1.Service{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setUserDefinedNetwork(tt.service, tt.slr)

			if !reflect.DeepEqual(*tt.service, *tt.result) {
				t.Errorf("Expected service %v, but got %v", *tt.service, *tt.result)
			}
		})
	}
}
