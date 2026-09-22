package controller

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
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

func TestGenerateHeadlessServiceExplicitEndpointsHasNoSelector(t *testing.T) {
	slr := &kubeovnv1.SwitchLBRule{
		Name: "slr", Namespace: "default",
		Spec: kubeovnv1.SwitchLBRuleSpec{
			Vip:       "10.0.0.10",
			Endpoints: []string{"10.0.0.2"},
			Ports: []kubeovnv1.SwitchLBRulePort{{
				Name: "tcp", Port: 80, TargetPort: 80, Protocol: "TCP",
			}},
		},
	}

	service := generateHeadlessService(slr, nil)
	if service.Spec.Selector != nil {
		t.Fatalf("explicit endpoint service selector = %#v, want nil", service.Spec.Selector)
	}
	ref := metav1.GetControllerOf(service)
	if ref == nil || ref.Kind != util.KindSwitchLBRule || ref.Name != slr.Name {
		t.Fatalf("generated service controller = %#v", ref)
	}
}

func TestHandleAddOrUpdateSwitchLBRuleClearsSelectorBeforeEndpoints(t *testing.T) {
	service := &corev1.Service{
		Name: "slr-static", Namespace: metav1.NamespaceDefault,
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector:  map[string]string{"app": "stale"},
			Type:      corev1.ServiceTypeClusterIP,
		},
	}
	slr := &kubeovnv1.SwitchLBRule{
		Name: "static", UID: "slr-uid",
		Spec: kubeovnv1.SwitchLBRuleSpec{
			Namespace: metav1.NamespaceDefault,
			Vip:       "10.0.0.10",
			Selector:  []string{"app:stale"},
			Endpoints: []string{"10.0.0.2"},
			Ports:     []kubeovnv1.SwitchLBRulePort{{Name: "http", Port: 80, TargetPort: 80, Protocol: "TCP"}},
		},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Services:      []*corev1.Service{service},
		SwitchLBRules: []*kubeovnv1.SwitchLBRule{slr},
	})
	require.NoError(t, err)
	_, err = fc.kubeClient.DiscoveryV1().EndpointSlices(metav1.NamespaceDefault).Create(
		context.Background(), &discoveryv1.EndpointSlice{
			Name:      "stale-slice",
			Namespace: metav1.NamespaceDefault,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: service.Name,
				discoveryv1.LabelManagedBy:   "endpointslice-controller.k8s.io",
			},
		}, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	require.NoError(t, fc.fakeController.handleAddOrUpdateSwitchLBRule(slr.Name))
	cachedRule, err := fc.fakeController.switchLBRuleLister.Get(slr.Name)
	require.NoError(t, err)
	require.Equal(t, []string{"app:stale"}, cachedRule.Spec.Selector)

	updatedService, err := fc.fakeController.config.KubeClient.CoreV1().Services(metav1.NamespaceDefault).Get(
		context.Background(), service.Name, metav1.GetOptions{},
	)
	require.NoError(t, err)
	require.Nil(t, updatedService.Spec.Selector)

	serviceUpdateIndex, endpointsCreateIndex := -1, -1
	for i, action := range fc.kubeClient.Actions() {
		if action.GetVerb() == "update" && action.GetResource().Resource == "services" {
			serviceUpdateIndex = i
		}
		if action.GetVerb() == "create" && action.GetResource().Resource == "endpoints" {
			endpointsCreateIndex = i
		}
	}
	require.GreaterOrEqual(t, serviceUpdateIndex, 0)
	require.GreaterOrEqual(t, endpointsCreateIndex, 0)
	require.Less(t, serviceUpdateIndex, endpointsCreateIndex)
}

func TestFilterServiceEndpointSlicesIgnoresStaleSelectorSlices(t *testing.T) {
	service := &corev1.Service{Spec: corev1.ServiceSpec{Selector: nil}}
	selectorSlice := &discoveryv1.EndpointSlice{Labels: map[string]string{
		discoveryv1.LabelManagedBy:   "endpointslice-controller.k8s.io",
		discoveryv1.LabelServiceName: "svc",
	}}
	mirrorSlice := &discoveryv1.EndpointSlice{Labels: map[string]string{
		discoveryv1.LabelManagedBy:   "endpointslicemirror-controller.k8s.io",
		discoveryv1.LabelServiceName: "svc",
	}}

	filtered := filterServiceEndpointSlices(service, []*discoveryv1.EndpointSlice{selectorSlice, mirrorSlice})
	require.Equal(t, []*discoveryv1.EndpointSlice{mirrorSlice}, filtered)

	service.Spec.Selector = map[string]string{"app": "backend"}
	require.Equal(t, []*discoveryv1.EndpointSlice{selectorSlice, mirrorSlice}, filterServiceEndpointSlices(service, []*discoveryv1.EndpointSlice{selectorSlice, mirrorSlice}))
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
				Annotations: map[string]string{
					util.LogicalRouterAnnotation: "test",
				},
			},
			result: &corev1.Service{
				Annotations: map[string]string{
					util.LogicalRouterAnnotation: "test",
					util.VpcAnnotation:           "test",
				},
			},
		},
		{
			name:    "Propagate Subnet",
			service: &corev1.Service{},
			slr: &kubeovnv1.SwitchLBRule{
				Annotations: map[string]string{
					util.LogicalSwitchAnnotation: "test",
				},
			},
			result: &corev1.Service{
				Annotations: map[string]string{
					util.LogicalSwitchAnnotation: "test",
				},
			},
		},
		{
			name:    "Propagate VPC/Subnet",
			service: &corev1.Service{},
			slr: &kubeovnv1.SwitchLBRule{
				Annotations: map[string]string{
					util.LogicalRouterAnnotation: "test1",
					util.LogicalSwitchAnnotation: "test2",
				},
			},
			result: &corev1.Service{
				Annotations: map[string]string{
					util.LogicalRouterAnnotation: "test1",
					util.LogicalSwitchAnnotation: "test2",
					util.VpcAnnotation:           "test1",
				},
			},
		},
		{
			name: "Remove stale VPC/subnet",
			service: &corev1.Service{Annotations: map[string]string{
				util.LogicalRouterAnnotation: "old-vpc",
				util.LogicalSwitchAnnotation: "old-subnet",
				util.VpcAnnotation:           "old-vpc",
			}},
			slr:    &kubeovnv1.SwitchLBRule{},
			result: &corev1.Service{Annotations: map[string]string{}},
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
		Name: vpcName,
		Status: kubeovnv1.VpcStatus{
			TCPLoadBalancer: tcpLBName,
		},
	}
	_, err := ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().Create(context.Background(), vpc, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, fc.fakeInformers.vpcInformer.Informer().GetStore().Add(vpc))

	svcName := generateSvcName(slrName)
	svc := &corev1.Service{
		Name:      svcName,
		Namespace: namespace,
		UID:       "slr-owner-uid",
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
			util.VpcAnnotation:           vpcName,
		},
	}
	setServiceScopedLBOwner(svc, switchLBRuleLBOwnerKind, slrName, "slr-owner-uid")
	_, err = ctrl.config.KubeClient.CoreV1().Services(namespace).Create(context.Background(), svc, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, fc.fakeInformers.serviceInformer.Informer().GetStore().Add(svc))
	fc.mockOvnClient.EXPECT().DeleteLoadBalancers(gomock.Any()).Return(nil)

	vip := &kubeovnv1.Vip{
		Name: subnetName,
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
		ownerUID   = "slr-owner-uid"
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

		info := &SwitchLBRuleInfo{Name: slrName, Namespace: namespace, UID: ownerUID, Vips: []string{vip1}}
		err := fc.fakeController.handleDelSwitchLBRule(info)
		require.NoError(t, err)

		// VIP should have been deleted
		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), subnetName, metav1.GetOptions{})
		require.True(t, k8serrors.IsNotFound(err), "VIP %s should have been deleted", subnetName)
	})

	t.Run("LBHC referenced by owner LB should be cleaned up", func(t *testing.T) {
		fc := setupHandleDelSLRTest(t, vpcName, subnetName, slrName, namespace, tcpLBName)

		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return(
			[]ovnnb.LoadBalancerHealthCheck{{
				UUID:        lbhcUUID1,
				Vip:         vip1,
				ExternalIDs: map[string]string{util.SwitchLBRuleSubnet: subnetName},
			}}, nil,
		)
		// The resource-scoped LB owned by this SLR references this LBHC.
		fc.mockOvnClient.EXPECT().ListLoadBalancers(gomock.Any()).Return(
			[]ovnnb.LoadBalancer{{
				Name:        tcpLBName,
				HealthCheck: []string{lbhcUUID1},
				ExternalIDs: map[string]string{
					serviceLBOwnerExternalID: "slr-owner-uid",
					serviceLBOwnerKindID:     switchLBRuleLBOwnerKind,
					serviceLBNamespaceID:     namespace,
					serviceLBNameExternalID:  slrName,
					serviceLBVersionID:       serviceLBVersion,
				},
			}}, nil,
		)
		fc.mockOvnClient.EXPECT().LoadBalancerDeleteHealthCheck(tcpLBName, lbhcUUID1).Return(nil)
		fc.mockOvnClient.EXPECT().LoadBalancerDeleteIPPortMapping(tcpLBName, vip1).Return(nil)
		fc.mockOvnClient.EXPECT().DeleteLoadBalancerHealthChecks(gomock.Any()).Return(nil)
		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return([]ovnnb.LoadBalancerHealthCheck{}, nil)

		info := &SwitchLBRuleInfo{Name: slrName, Namespace: namespace, UID: ownerUID, Vips: []string{vip1}}
		err := fc.fakeController.handleDelSwitchLBRule(info)
		require.NoError(t, err)

		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), subnetName, metav1.GetOptions{})
		require.True(t, k8serrors.IsNotFound(err), "VIP %s should have been deleted", subnetName)
	})

	t.Run("forged live service owner uid still cleans owner LB", func(t *testing.T) {
		fc := setupHandleDelSLRTest(t, vpcName, subnetName, slrName, namespace, tcpLBName)

		svc, err := fc.fakeController.servicesLister.Services(namespace).Get(generateSvcName(slrName))
		require.NoError(t, err)
		tampered := svc.DeepCopy()
		ref := metav1.GetControllerOf(tampered)
		require.NotNil(t, ref)
		ref.UID = "other-rule-uid"
		require.NoError(t, fc.fakeInformers.serviceInformer.Informer().GetStore().Update(tampered))

		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return(
			[]ovnnb.LoadBalancerHealthCheck{{
				UUID:        lbhcUUID1,
				Vip:         vip1,
				ExternalIDs: map[string]string{util.SwitchLBRuleSubnet: subnetName},
			}}, nil,
		)
		fc.mockOvnClient.EXPECT().ListLoadBalancers(gomock.Any()).Return(
			[]ovnnb.LoadBalancer{{
				Name:        tcpLBName,
				HealthCheck: []string{lbhcUUID1},
				ExternalIDs: map[string]string{
					serviceLBOwnerExternalID: ownerUID,
					serviceLBOwnerKindID:     switchLBRuleLBOwnerKind,
					serviceLBNamespaceID:     namespace,
					serviceLBNameExternalID:  slrName,
					serviceLBVersionID:       serviceLBVersion,
				},
			}}, nil,
		)
		fc.mockOvnClient.EXPECT().LoadBalancerDeleteHealthCheck(tcpLBName, lbhcUUID1).Return(nil)
		fc.mockOvnClient.EXPECT().LoadBalancerDeleteIPPortMapping(tcpLBName, vip1).Return(nil)
		fc.mockOvnClient.EXPECT().DeleteLoadBalancerHealthChecks(gomock.Any()).Return(nil)
		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return([]ovnnb.LoadBalancerHealthCheck{}, nil)

		info := &SwitchLBRuleInfo{Name: slrName, Namespace: namespace, UID: ownerUID, Vips: []string{vip1}}
		require.NoError(t, fc.fakeController.handleDelSwitchLBRule(info))
	})

	t.Run("LBHC referenced by another owner LB should not be cleaned up", func(t *testing.T) {
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

		info := &SwitchLBRuleInfo{Name: slrName, Namespace: namespace, UID: ownerUID, Vips: []string{vip1}}
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

		info := &SwitchLBRuleInfo{Name: slrName, Namespace: namespace, UID: ownerUID, Vips: []string{vip1}}
		err := fc.fakeController.handleDelSwitchLBRule(info)
		require.NoError(t, err)

		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), subnetName, metav1.GetOptions{})
		require.True(t, k8serrors.IsNotFound(err), "VIP %s should have been deleted via fallback path", subnetName)
	})

	t.Run("service missing from lister falls back to info.Subnet", func(t *testing.T) {
		fc := setupHandleDelSLRTest(t, vpcName, subnetName, slrName, namespace, tcpLBName)

		// Simulate a racing service-delete event that removed the backing service
		// from both the informer cache and the API before the SLR delete worker ran.
		svcName := generateSvcName(slrName)
		svc, err := fc.fakeController.config.KubeClient.CoreV1().Services(namespace).Get(context.Background(), svcName, metav1.GetOptions{})
		require.NoError(t, err)
		require.NoError(t, fc.fakeInformers.serviceInformer.Informer().GetStore().Delete(svc))
		require.NoError(t, fc.fakeController.config.KubeClient.CoreV1().Services(namespace).Delete(context.Background(), svcName, metav1.DeleteOptions{}))

		// With no LBHC matching info.Vips and the service gone, the handler must
		// still clean up the VIP via the subnet recorded on info.Subnet.
		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return([]ovnnb.LoadBalancerHealthCheck{}, nil)
		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return([]ovnnb.LoadBalancerHealthCheck{}, nil)

		info := &SwitchLBRuleInfo{
			Name:      slrName,
			Namespace: namespace,
			UID:       ownerUID,
			Subnet:    subnetName,
			Vips:      []string{vip1},
		}
		err = fc.fakeController.handleDelSwitchLBRule(info)
		require.NoError(t, err)

		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), subnetName, metav1.GetOptions{})
		require.True(t, k8serrors.IsNotFound(err), "VIP %s should have been deleted via info.Subnet fallback", subnetName)
	})

	t.Run("service missing from lister still honours info.VPC scoping", func(t *testing.T) {
		fc := setupHandleDelSLRTest(t, vpcName, subnetName, slrName, namespace, tcpLBName)

		svcName := generateSvcName(slrName)
		svc, err := fc.fakeController.config.KubeClient.CoreV1().Services(namespace).Get(context.Background(), svcName, metav1.GetOptions{})
		require.NoError(t, err)
		require.NoError(t, fc.fakeInformers.serviceInformer.Informer().GetStore().Delete(svc))
		require.NoError(t, fc.fakeController.config.KubeClient.CoreV1().Services(namespace).Delete(context.Background(), svcName, metav1.DeleteOptions{}))

		// Matching LBHC is referenced by a DIFFERENT VPC's LB; without info.VPC the
		// handler would fall back to unscoped deletion and remove a health check
		// that still belongs to another VPC.  With info.VPC set we must skip it.
		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return(
			[]ovnnb.LoadBalancerHealthCheck{{
				UUID:        lbhcUUID1,
				Vip:         vip1,
				ExternalIDs: map[string]string{util.SwitchLBRuleSubnet: subnetName},
			}}, nil,
		)
		fc.mockOvnClient.EXPECT().ListLoadBalancers(gomock.Any()).Return(
			[]ovnnb.LoadBalancer{{
				Name:        "vpc-other-tcp-load",
				HealthCheck: []string{lbhcUUID1},
			}}, nil,
		)
		// Fallback VIP-by-subnet lookup still finds the foreign LBHC, so the VIP
		// must stay.
		fc.mockOvnClient.EXPECT().ListLoadBalancerHealthChecks(gomock.Any()).Return(
			[]ovnnb.LoadBalancerHealthCheck{{
				UUID:        lbhcUUID1,
				Vip:         vip1,
				ExternalIDs: map[string]string{util.SwitchLBRuleSubnet: subnetName},
			}}, nil,
		)

		info := &SwitchLBRuleInfo{
			Name:      slrName,
			Namespace: namespace,
			UID:       ownerUID,
			Subnet:    subnetName,
			VPC:       vpcName,
			Vips:      []string{vip1},
		}
		err = fc.fakeController.handleDelSwitchLBRule(info)
		require.NoError(t, err)

		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), subnetName, metav1.GetOptions{})
		require.NoError(t, err, "VIP %s should still exist (other VPC owns the LBHC)", subnetName)
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

		info := &SwitchLBRuleInfo{Name: slrName, Namespace: namespace, UID: ownerUID, Vips: []string{vip1, vip2}}
		err := fc.fakeController.handleDelSwitchLBRule(info)
		require.NoError(t, err)

		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), subnetName, metav1.GetOptions{})
		require.True(t, k8serrors.IsNotFound(err), "VIP %s should have been deleted", subnetName)
	})
}

func Test_handleAddOrUpdateSwitchLBRule(t *testing.T) {
	t.Parallel()

	const (
		slrName   = "test-slr"
		namespace = "default"
		vip       = "10.96.0.100"
		newIP     = "10.244.0.26"
	)
	newSLR := func() *kubeovnv1.SwitchLBRule {
		return &kubeovnv1.SwitchLBRule{
			ObjectMeta: metav1.ObjectMeta{Name: slrName},
			Spec: kubeovnv1.SwitchLBRuleSpec{
				Vip:       vip,
				Namespace: namespace,
				Endpoints: []string{newIP},
				Ports:     []kubeovnv1.SwitchLBRulePort{{Name: "api", Port: 6443, TargetPort: 6443, Protocol: "TCP"}},
			},
		}
	}
	verify := func(t *testing.T, ctrl *Controller) {
		t.Helper()
		svcName := generateSvcName(slrName)

		eps, err := ctrl.config.KubeClient.CoreV1().Endpoints(namespace).Get(context.Background(), svcName, metav1.GetOptions{})
		require.NoError(t, err)
		require.Len(t, eps.Subsets, 1)
		require.Len(t, eps.Subsets[0].Addresses, 1)
		require.Equal(t, newIP, eps.Subsets[0].Addresses[0].IP)

		_, err = ctrl.config.KubeClient.CoreV1().Services(namespace).Get(context.Background(), svcName, metav1.GetOptions{})
		require.NoError(t, err)

		slr, err := ctrl.config.KubeOvnClient.KubeovnV1().SwitchLBRules().Get(context.Background(), slrName, metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, namespace+"/"+svcName, slr.Status.Service)
	}

	t.Run("service and endpoints are created when neither exists", func(t *testing.T) {
		t.Parallel()
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{SwitchLBRules: []*kubeovnv1.SwitchLBRule{newSLR()}})
		require.NoError(t, err)

		require.NoError(t, fc.fakeController.handleAddOrUpdateSwitchLBRule(slrName))
		verify(t, fc.fakeController)
	})

	t.Run("endpoints left behind without their service are reused", func(t *testing.T) {
		t.Parallel()
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{SwitchLBRules: []*kubeovnv1.SwitchLBRule{newSLR()}})
		require.NoError(t, err)
		ctrl := fc.fakeController

		// The generated Endpoints exist but the generated Service does not, e.g.
		// an earlier attempt created the Endpoints and then failed to create the
		// Service. The rule must still converge instead of failing forever on an
		// Endpoints create.
		leftover := &corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{Name: generateSvcName(slrName), Namespace: namespace},
			Subsets:    []corev1.EndpointSubset{{Addresses: []corev1.EndpointAddress{{IP: "10.244.0.20"}}}},
		}
		_, err = ctrl.config.KubeClient.CoreV1().Endpoints(namespace).Create(context.Background(), leftover, metav1.CreateOptions{})
		require.NoError(t, err)

		require.NoError(t, ctrl.handleAddOrUpdateSwitchLBRule(slrName))
		verify(t, ctrl)
	})
}
