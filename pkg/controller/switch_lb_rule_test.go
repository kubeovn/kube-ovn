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
	"github.com/kubeovn/kube-ovn/pkg/util"
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
				t.Errorf("Expected familyPolicy %s, but got %s", tt.expectedFamilyPolicy, policy)
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

// setupHandleDelSLRTest creates a fakeController with a VPC (with LB names in Status)
// and a Service (with subnet/VPC annotations) pre-populated in both the informer cache
// and the fake API client.  It also creates a VIP CR so we can verify deletion.
func setupHandleDelSLRTest(t *testing.T, vpcName, subnetName, slrName, namespace, tcpLBName string) *fakeController {
	t.Helper()
	fc := newFakeController(t)
	ctrl := fc.fakeController

	vpc := &kubeovnv1.Vpc{
		ObjectMeta: metav1.ObjectMeta{Name: vpcName},
		Status: kubeovnv1.VpcStatus{
			TCPLoadBalancer: tcpLBName,
		},
	}
	_, err := ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().Create(context.Background(), vpc, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, fc.fakeInformers.vpcInformer.Informer().GetStore().Add(vpc))

	svcName := generateSvcName(slrName)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: namespace,
			Annotations: map[string]string{
				util.LogicalSwitchAnnotation: subnetName,
				util.VpcAnnotation:           vpcName,
			},
		},
	}
	_, err = ctrl.config.KubeClient.CoreV1().Services(namespace).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, fc.fakeInformers.serviceInformer.Informer().GetStore().Add(svc))

	vip := &kubeovnv1.Vip{
		ObjectMeta: metav1.ObjectMeta{Name: subnetName},
	}
	_, err = ctrl.config.KubeOvnClient.KubeovnV1().Vips().Create(context.Background(), vip, metav1.CreateOptions{})
	require.NoError(t, err)

	return fc
}

func Test_handleDelSwitchLBRule(t *testing.T) {
	t.Parallel()

	const (
		vpcName    = "test-vpc"
		subnetName = "test-subnet"
		slrName    = "test-slr"
		namespace  = "default"
		lbhcUUID1  = "lbhc-uuid-1"
		lbhcUUID2  = "lbhc-uuid-2"
		vip1       = "10.0.0.1:8080"
		vip2       = "10.0.0.2:8082"
		tcpLBName  = "vpc-test-tcp-load"
	)

	t.Run("orphaned LBHC with no LB references should be cleaned up", func(t *testing.T) {
		fc := setupHandleDelSLRTest(t, vpcName, subnetName, slrName, namespace, tcpLBName)

		// First call: find LBHC matching the VIP
		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return(
			[]ovnnb.LoadBalancerHealthCheck{{
				UUID:        lbhcUUID1,
				Vip:         vip1,
				ExternalIDs: map[string]string{util.SwitchLBRuleSubnet: subnetName},
			}}, nil,
		)
		// No LB references this LBHC (orphaned)
		fc.mockOvnClient.EXPECT().ListLoadBalancers(gomock.Any()).Return([]ovnnb.LoadBalancer{}, nil)
		// Expect LBHC to be deleted
		fc.mockOvnClient.EXPECT().DeleteLoadBalancerHealthChecks(gomock.Any()).Return(nil)
		// Second call: after LBHC deletion, no more LBHCs for this subnet
		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return([]ovnnb.LoadBalancerHealthCheck{}, nil)

		info := &SwitchLBRuleInfo{Name: slrName, Namespace: namespace, Vips: []string{vip1}}
		err := fc.fakeController.handleDelSwitchLBRule(info)
		require.NoError(t, err)

		// VIP should have been deleted
		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), subnetName, metav1.GetOptions{})
		require.True(t, k8serrors.IsNotFound(err), "VIP %s should have been deleted", subnetName)
	})

	t.Run("LBHC referenced by same VPC LB should be cleaned up", func(t *testing.T) {
		fc := setupHandleDelSLRTest(t, vpcName, subnetName, slrName, namespace, tcpLBName)

		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return(
			[]ovnnb.LoadBalancerHealthCheck{{
				UUID:        lbhcUUID1,
				Vip:         vip1,
				ExternalIDs: map[string]string{util.SwitchLBRuleSubnet: subnetName},
			}}, nil,
		)
		// LB in same VPC references this LBHC
		fc.mockOvnClient.EXPECT().ListLoadBalancers(gomock.Any()).Return(
			[]ovnnb.LoadBalancer{{
				Name:        tcpLBName,
				HealthCheck: []string{lbhcUUID1},
			}}, nil,
		)
		fc.mockOvnClient.EXPECT().LoadBalancerDeleteHealthCheck(tcpLBName, lbhcUUID1).Return(nil)
		fc.mockOvnClient.EXPECT().LoadBalancerDeleteIPPortMapping(tcpLBName, vip1).Return(nil)
		fc.mockOvnClient.EXPECT().DeleteLoadBalancerHealthChecks(gomock.Any()).Return(nil)
		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return([]ovnnb.LoadBalancerHealthCheck{}, nil)

		info := &SwitchLBRuleInfo{Name: slrName, Namespace: namespace, Vips: []string{vip1}}
		err := fc.fakeController.handleDelSwitchLBRule(info)
		require.NoError(t, err)

		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), subnetName, metav1.GetOptions{})
		require.True(t, k8serrors.IsNotFound(err), "VIP %s should have been deleted", subnetName)
	})

	t.Run("LBHC referenced by other VPC LB should not be cleaned up", func(t *testing.T) {
		fc := setupHandleDelSLRTest(t, vpcName, subnetName, slrName, namespace, tcpLBName)

		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return(
			[]ovnnb.LoadBalancerHealthCheck{{
				UUID:        lbhcUUID1,
				Vip:         vip1,
				ExternalIDs: map[string]string{util.SwitchLBRuleSubnet: subnetName},
			}}, nil,
		)
		// LB from a DIFFERENT VPC references this LBHC
		fc.mockOvnClient.EXPECT().ListLoadBalancers(gomock.Any()).Return(
			[]ovnnb.LoadBalancer{{
				Name:        "vpc-other-tcp-load",
				HealthCheck: []string{lbhcUUID1},
			}}, nil,
		)
		// No DeleteLoadBalancerHealthChecks expected (LBHC belongs to other VPC)
		// Fallback path: vips empty → uses service annotation subnet, then checks remaining LBHCs
		// The LBHC still exists for this subnet, so VIP is not deleted
		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return(
			[]ovnnb.LoadBalancerHealthCheck{{
				UUID:        lbhcUUID1,
				Vip:         vip1,
				ExternalIDs: map[string]string{util.SwitchLBRuleSubnet: subnetName},
			}}, nil,
		)

		info := &SwitchLBRuleInfo{Name: slrName, Namespace: namespace, Vips: []string{vip1}}
		err := fc.fakeController.handleDelSwitchLBRule(info)
		require.NoError(t, err)

		// VIP should still exist
		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), subnetName, metav1.GetOptions{})
		require.NoError(t, err, "VIP %s should still exist (other VPC owns the LBHC)", subnetName)
	})

	t.Run("no LBHC found should fallback to service annotation subnet", func(t *testing.T) {
		fc := setupHandleDelSLRTest(t, vpcName, subnetName, slrName, namespace, tcpLBName)

		// No LBHCs found at all
		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return([]ovnnb.LoadBalancerHealthCheck{}, nil)
		// Fallback: no LBHC for subnet after deletion check
		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return([]ovnnb.LoadBalancerHealthCheck{}, nil)

		info := &SwitchLBRuleInfo{Name: slrName, Namespace: namespace, Vips: []string{vip1}}
		err := fc.fakeController.handleDelSwitchLBRule(info)
		require.NoError(t, err)

		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), subnetName, metav1.GetOptions{})
		require.True(t, k8serrors.IsNotFound(err), "VIP %s should have been deleted via fallback path", subnetName)
	})

	t.Run("multiple orphaned LBHCs for same subnet should all be cleaned up", func(t *testing.T) {
		fc := setupHandleDelSLRTest(t, vpcName, subnetName, slrName, namespace, tcpLBName)

		// Two orphaned LBHCs for the same subnet
		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return(
			[]ovnnb.LoadBalancerHealthCheck{
				{UUID: lbhcUUID1, Vip: vip1, ExternalIDs: map[string]string{util.SwitchLBRuleSubnet: subnetName}},
				{UUID: lbhcUUID2, Vip: vip2, ExternalIDs: map[string]string{util.SwitchLBRuleSubnet: subnetName}},
			}, nil,
		)
		// Both have no LB references
		fc.mockOvnClient.EXPECT().ListLoadBalancers(gomock.Any()).Return([]ovnnb.LoadBalancer{}, nil)
		fc.mockOvnClient.EXPECT().ListLoadBalancers(gomock.Any()).Return([]ovnnb.LoadBalancer{}, nil)
		// Both should be deleted
		fc.mockOvnClient.EXPECT().DeleteLoadBalancerHealthChecks(gomock.Any()).Return(nil)
		// After deletion, no more LBHCs for this subnet
		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return([]ovnnb.LoadBalancerHealthCheck{}, nil)

		info := &SwitchLBRuleInfo{Name: slrName, Namespace: namespace, Vips: []string{vip1, vip2}}
		err := fc.fakeController.handleDelSwitchLBRule(info)
		require.NoError(t, err)

		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), subnetName, metav1.GetOptions{})
		require.True(t, k8serrors.IsNotFound(err), "VIP %s should have been deleted", subnetName)
	})
}
