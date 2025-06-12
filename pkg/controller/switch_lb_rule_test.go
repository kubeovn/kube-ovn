package controller

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
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
