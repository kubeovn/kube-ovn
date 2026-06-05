package controller

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
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
				t.Errorf("Expected familyPolicy %s, but got %s", tt.expectedFamilyPolicy, policy)
			}
		})
	}
}

func Test_handleDelSwitchLBRuleFallbackToInfoSubnet(t *testing.T) {
	t.Parallel()

	const (
		namespace  = "default"
		slrName    = "test-slr"
		subnetName = "test-subnet"
		vip        = "10.0.0.1:8080"
	)

	fc := newFakeController(t)
	_, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Create(
		context.Background(),
		&kubeovnv1.Vip{ObjectMeta: metav1.ObjectMeta{Name: subnetName}},
		metav1.CreateOptions{},
	)
	require.NoError(t, err)

	fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return([]ovnnb.LoadBalancerHealthCheck{}, nil)
	fc.mockOvnClient.EXPECT().DeleteLoadBalancerHealthChecks(gomock.Any()).Return(nil)
	fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return([]ovnnb.LoadBalancerHealthCheck{}, nil)

	err = fc.fakeController.handleDelSwitchLBRule(&SlrInfo{
		Name:      slrName,
		Namespace: namespace,
		Subnet:    subnetName,
		Vips:      []string{vip},
	})
	require.NoError(t, err)

	_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), subnetName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err), "VIP %s should have been deleted via SlrInfo.Subnet fallback", subnetName)
}
