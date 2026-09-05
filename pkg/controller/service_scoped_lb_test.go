package controller

import (
	"context"
	"slices"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestServiceSessionAffinityTimeout(t *testing.T) {
	tests := []struct {
		name    string
		svc     *corev1.Service
		want    int
		wantErr bool
	}{
		{
			name: "default timeout",
			svc:  &corev1.Service{Namespace: "default", Name: "web", Spec: corev1.ServiceSpec{SessionAffinity: corev1.ServiceAffinityClientIP}},
			want: util.DefaultServiceSessionStickinessTimeout,
		},
		{
			name: "configured timeout",
			svc: &corev1.Service{Namespace: "default", Name: "web", Spec: corev1.ServiceSpec{
				SessionAffinity:       corev1.ServiceAffinityClientIP,
				SessionAffinityConfig: &corev1.SessionAffinityConfig{ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: new(int32(10))}},
			}},
			want: 10,
		},
		{
			name: "zero is rejected",
			svc: &corev1.Service{Namespace: "default", Name: "web", Spec: corev1.ServiceSpec{
				SessionAffinity:       corev1.ServiceAffinityClientIP,
				SessionAffinityConfig: &corev1.SessionAffinityConfig{ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: new(int32(0))}},
			}},
			wantErr: true,
		},
		{
			name: "value above OVN limit is rejected",
			svc: &corev1.Service{Namespace: "default", Name: "web", Spec: corev1.ServiceSpec{
				SessionAffinity:       corev1.ServiceAffinityClientIP,
				SessionAffinityConfig: &corev1.SessionAffinityConfig{ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: new(int32(maxOVNLBSessionTimeout + 1))}},
			}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := serviceSessionAffinityTimeout(tt.svc)
			if (err != nil) != tt.wantErr {
				t.Fatalf("serviceSessionAffinityTimeout() error = %v, wantErr %t", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("serviceSessionAffinityTimeout() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestServiceScopedLBIdentityAndPolicy(t *testing.T) {
	local := corev1.ServiceInternalTrafficPolicyLocal
	svc := &corev1.Service{
		Namespace: "default", Name: "web", UID: types.UID("uid-123"),
		Spec: corev1.ServiceSpec{
			SessionAffinity:       corev1.ServiceAffinityClientIP,
			InternalTrafficPolicy: &local,
			Ports:                 []corev1.ServicePort{{Protocol: corev1.ProtocolTCP}, {Protocol: corev1.ProtocolTCP}, {Protocol: corev1.ProtocolUDP}},
		},
	}

	if !serviceUsesScopedLB(svc) || !serviceUsesDistributedLB(svc) {
		t.Fatal("service with ClientIP and Local policy must use a distributed scoped LB")
	}
	if got := serviceScopedLBName(svc, corev1.ProtocolTCP); got == serviceScopedLBName(svc, corev1.ProtocolUDP) {
		t.Fatal("protocols must have distinct service-scoped LB names")
	}
	if got, want := serviceScopedLBName(svc, corev1.ProtocolTCP), "service:default/web:tcp:internal"; got != want {
		t.Fatalf("serviceScopedLBName() = %q, want %q", got, want)
	}
	other := svc.DeepCopy()
	other.UID = types.UID("different-uid")
	if serviceScopedLBName(other, corev1.ProtocolTCP) != serviceScopedLBName(svc, corev1.ProtocolTCP) {
		t.Fatal("service-scoped LB name must not depend on service UID")
	}
	if got := len(serviceScopedLBNames(svc)); got != 4 {
		t.Fatalf("serviceScopedLBNames() returned %d names, want 4", got)
	}
	if internal, external := serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, serviceLBInternalTraffic), serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, serviceLBExternalTraffic); internal == external {
		t.Fatalf("internal and external traffic classes must have distinct LB names: %q", internal)
	}
	ids := serviceScopedLBExternalIDs(svc, util.DefaultVpc, serviceLBInternalTraffic)
	if ids[serviceLBOwnerExternalID] != string(svc.UID) || ids[serviceLBVersionID] != serviceLBVersion {
		t.Fatalf("service LB ownership metadata = %v", ids)
	}
}

func TestRuleScopedLBIdentity(t *testing.T) {
	for _, tt := range []struct {
		kind string
		name string
		want string
	}{
		{kind: switchLBRuleLBOwnerKind, name: "slr1", want: "switchlbrule:ns1/slr1:tcp:external"},
		{kind: routerLBRuleLBOwnerKind, name: "rlr1", want: "routerlbrule:ns1/rlr1:tcp:external"},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			svc := &corev1.Service{Name: "generated", Namespace: "ns1", UID: types.UID("service-uid")}
			setServiceScopedLBOwner(svc, tt.kind, tt.name, "rule-uid")
			if got := serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, serviceLBExternalTraffic); got != tt.want {
				t.Fatalf("rule-scoped load balancer name = %q, want %q", got, tt.want)
			}
			ids := serviceScopedLBExternalIDs(svc, "vpc1", serviceLBExternalTraffic)
			if ids[serviceLBOwnerExternalID] != "rule-uid" || ids[serviceLBOwnerKindID] != tt.kind || ids[serviceLBVPCExternalID] != "vpc1" {
				t.Fatalf("rule-scoped load balancer metadata = %v", ids)
			}
		})
	}
}

func TestRuleScopedLoadBalancerAttachments(t *testing.T) {
	t.Run("switch rule", func(t *testing.T) {
		fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{
			{Name: "subnet1"},
			{Name: "subnet2"},
		}})
		if err != nil {
			t.Fatal(err)
		}
		svc := &corev1.Service{Name: "slr-slr1", Namespace: "ns1"}
		setServiceScopedLBOwner(svc, switchLBRuleLBOwnerKind, "slr1", "slr-uid")
		fake.mockOvnClient.EXPECT().LogicalSwitchUpdateLoadBalancers("subnet1", ovsdb.MutateOperationInsert, "slr-lb").Return(nil)
		fake.mockOvnClient.EXPECT().LogicalSwitchUpdateLoadBalancers("subnet2", ovsdb.MutateOperationDelete, "slr-lb").Return(nil)
		if err := fake.fakeController.reconcileResourceScopedLoadBalancerAttachments(svc, "vpc1", "subnet1", "slr-lb"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("router rule", func(t *testing.T) {
		fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Vpcs: []*kubeovnv1.Vpc{
			{Name: "vpc1"},
			{Name: "vpc2"},
		}})
		if err != nil {
			t.Fatal(err)
		}
		svc := &corev1.Service{Name: "rlr-rlr1", Namespace: "ns1"}
		setServiceScopedLBOwner(svc, routerLBRuleLBOwnerKind, "rlr1", "rlr-uid")
		fake.mockOvnClient.EXPECT().LogicalRouterUpdateLoadBalancers("vpc1", ovsdb.MutateOperationInsert, "rlr-lb").Return(nil)
		fake.mockOvnClient.EXPECT().LogicalRouterUpdateLoadBalancers("vpc2", ovsdb.MutateOperationDelete, "rlr-lb").Return(nil)
		if err := fake.fakeController.reconcileResourceScopedLoadBalancerAttachments(svc, "vpc1", "", "rlr-lb"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestServiceScopedLBNamesTrafficDistributionDualStack(t *testing.T) {
	trafficDistribution := corev1.ServiceTrafficDistributionPreferSameZone
	svc := &corev1.Service{
		Namespace: "default", Name: "web", UID: types.UID("uid-dual-stack"),
		Spec: corev1.ServiceSpec{
			Type:                corev1.ServiceTypeClusterIP,
			ClusterIPs:          []string{"10.96.0.10", "fd00:10:96::10"},
			TrafficDistribution: &trafficDistribution,
			Ports:               []corev1.ServicePort{{Protocol: corev1.ProtocolTCP}},
		},
	}

	names := serviceScopedLBNames(svc)
	for _, family := range []string{"ipv4", "ipv6"} {
		want := serviceScopedLBNameForTrafficClassAndFamily(svc, corev1.ProtocolTCP, serviceLBInternalTraffic, family)
		if !slices.Contains(names, want) {
			t.Fatalf("serviceScopedLBNames() = %v, want %q", names, want)
		}
	}
	if slices.Contains(names, serviceScopedLBName(svc, corev1.ProtocolTCP)) {
		t.Fatalf("dual-stack template load balancers must be split by address family: %v", names)
	}
	if slices.Contains(names, serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, serviceLBExternalTraffic)) {
		t.Fatalf("traffic distribution must not create a scoped external load balancer: %v", names)
	}
}

func TestServiceUsesTrafficDistributionExcludesRuleServices(t *testing.T) {
	trafficDistribution := corev1.ServiceTrafficDistributionPreferSameZone
	for _, annotation := range []string{util.SwitchLBRuleVipsAnnotation, util.RouterLBRuleVipsAnnotation} {
		svc := &corev1.Service{
			Annotations: map[string]string{annotation: "10.0.0.10"},
			Spec:        corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, TrafficDistribution: &trafficDistribution},
		}
		if serviceUsesTrafficDistribution(svc) || serviceUsesTemplateLB(svc) || !serviceUsesScopedLB(svc) {
			t.Fatalf("service with %s must use a non-template resource-scoped load balancer", annotation)
		}
	}
}

func TestServiceUsesTrafficDistributionOnlyForClusterIPServices(t *testing.T) {
	trafficDistribution := corev1.ServiceTrafficDistributionPreferSameZone
	for _, serviceType := range []corev1.ServiceType{corev1.ServiceTypeNodePort, corev1.ServiceTypeLoadBalancer, corev1.ServiceTypeExternalName} {
		svc := &corev1.Service{Spec: corev1.ServiceSpec{Type: serviceType, TrafficDistribution: &trafficDistribution}}
		if serviceUsesTrafficDistribution(svc) || serviceUsesTemplateLB(svc) {
			t.Fatalf("trafficDistribution must not use scoped template LBs for service type %s", serviceType)
		}
	}

	clusterIP := &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, TrafficDistribution: &trafficDistribution}}
	if !serviceUsesTrafficDistribution(clusterIP) || !serviceUsesTemplateLB(clusterIP) {
		t.Fatal("trafficDistribution must use scoped template LBs for ClusterIP services")
	}
}

func TestEnsureServiceScopedLB(t *testing.T) {
	fake := newFakeController(t)
	ctrl := fake.fakeController
	ctrl.config.EnableOVNLBDistributed = true
	timeout := int32(42)
	svc := &corev1.Service{
		Namespace: "default", Name: "web", UID: types.UID("uid-ensure"),
		Spec: corev1.ServiceSpec{
			SessionAffinity:       corev1.ServiceAffinityClientIP,
			SessionAffinityConfig: &corev1.SessionAffinityConfig{ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: &timeout}},
		},
	}
	lbName := serviceScopedLBName(svc, corev1.ProtocolTCP)
	selectionFields := []string{ovnnb.LoadBalancerSelectionFieldsIPSrc, ovnnb.LoadBalancerSelectionFieldsIpv6Src}
	gomock.InOrder(
		fake.mockOvnClient.EXPECT().CreateLoadBalancer(lbName, "tcp", "ip_src", "ipv6_src").Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerSelectionFields(lbName, gomock.Eq(selectionFields)).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerExternalIDs(lbName, gomock.Eq(serviceScopedLBExternalIDs(svc, ctrl.config.ClusterRouter, serviceLBInternalTraffic))).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerAffinityTimeout(lbName, 42).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerDistributed(lbName, false).Return(nil),
	)

	got, err := ctrl.ensureServiceScopedLB(svc, corev1.ProtocolTCP, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != lbName {
		t.Fatalf("ensureServiceScopedLB() = %q, want %q", got, lbName)
	}
}

func TestEnsureServiceScopedLBExternalTrafficDoesNotDistribute(t *testing.T) {
	fake := newFakeController(t)
	ctrl := fake.fakeController
	ctrl.config.EnableOVNLBDistributed = true
	local := corev1.ServiceInternalTrafficPolicyLocal
	svc := &corev1.Service{
		Namespace: "default", Name: "web", UID: types.UID("uid-external"),
		Spec: corev1.ServiceSpec{
			SessionAffinity:       corev1.ServiceAffinityClientIP,
			InternalTrafficPolicy: &local,
		},
	}
	lbName := serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, serviceLBExternalTraffic)
	selectionFields := []string{ovnnb.LoadBalancerSelectionFieldsIPSrc, ovnnb.LoadBalancerSelectionFieldsIpv6Src}
	gomock.InOrder(
		fake.mockOvnClient.EXPECT().CreateLoadBalancer(lbName, "tcp", "ip_src", "ipv6_src").Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerSelectionFields(lbName, gomock.Eq(selectionFields)).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerExternalIDs(lbName, gomock.Eq(serviceScopedLBExternalIDs(svc, ctrl.config.ClusterRouter, serviceLBExternalTraffic))).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerAffinityTimeout(lbName, util.DefaultServiceSessionStickinessTimeout).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerDistributed(lbName, false).Return(nil),
	)
	if _, err := ctrl.ensureServiceScopedLBForTrafficClass(svc, corev1.ProtocolTCP, serviceLBExternalTraffic, ""); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureServiceScopedLBExternalTraffic(t *testing.T) {
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{
		{Name: "subnet-a", Spec: kubeovnv1.SubnetSpec{Vpc: util.DefaultVpc, EnableLb: new(true)}},
		{Name: "subnet-b", Spec: kubeovnv1.SubnetSpec{Vpc: util.DefaultVpc, EnableLb: new(true)}},
		{Name: "join", Spec: kubeovnv1.SubnetSpec{Vpc: util.DefaultVpc, EnableLb: new(true)}},
		{Name: "disabled", Spec: kubeovnv1.SubnetSpec{Vpc: util.DefaultVpc, EnableLb: new(false)}},
		{Name: "other-vpc", Spec: kubeovnv1.SubnetSpec{Vpc: "other-vpc", EnableLb: new(true)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctrl := fake.fakeController
	ctrl.config.EnableOVNLBDistributed = true
	local := corev1.ServiceInternalTrafficPolicyLocal
	svc := &corev1.Service{
		Namespace: "default", Name: "web", UID: types.UID("uid-external-attach"),
		Spec: corev1.ServiceSpec{
			InternalTrafficPolicy: &local,
			Ports:                 []corev1.ServicePort{{Protocol: corev1.ProtocolTCP}},
		},
	}
	lbName := serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, serviceLBExternalTraffic)
	gomock.InOrder(
		fake.mockOvnClient.EXPECT().CreateLoadBalancer(lbName, "tcp").Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerSelectionFields(lbName, []string(nil)).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerExternalIDs(lbName, gomock.Eq(serviceScopedLBExternalIDs(svc, ctrl.config.ClusterRouter, serviceLBExternalTraffic))).Return(nil),
		fake.mockOvnClient.EXPECT().DeleteLoadBalancerAffinityTimeout(lbName).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerDistributed(lbName, false).Return(nil),
		fake.mockOvnClient.EXPECT().LogicalSwitchUpdateLoadBalancers("subnet-a", ovsdb.MutateOperationInsert, lbName).Return(nil),
		fake.mockOvnClient.EXPECT().LogicalSwitchUpdateLoadBalancers("subnet-b", ovsdb.MutateOperationInsert, lbName).Return(nil),
		fake.mockOvnClient.EXPECT().LogicalSwitchUpdateLoadBalancers("disabled", ovsdb.MutateOperationDelete, lbName).Return(nil),
		fake.mockOvnClient.EXPECT().LogicalSwitchUpdateLoadBalancers("other-vpc", ovsdb.MutateOperationDelete, lbName).Return(nil),
	)
	if _, err := ctrl.ensureServiceScopedLBExternalTraffic(svc, corev1.ProtocolTCP); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.reconcileResourceScopedLoadBalancerAttachments(svc, util.DefaultVpc, "", lbName); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureServiceScopedLBClearsAffinityTimeout(t *testing.T) {
	fake := newFakeController(t)
	ctrl := fake.fakeController
	ctrl.config.EnableOVNLBDistributed = true
	local := corev1.ServiceInternalTrafficPolicyLocal
	svc := &corev1.Service{
		Namespace: "default", Name: "web", UID: types.UID("uid-clear-affinity"),
		Spec: corev1.ServiceSpec{InternalTrafficPolicy: &local},
	}
	lbName := serviceScopedLBName(svc, corev1.ProtocolTCP)
	gomock.InOrder(
		fake.mockOvnClient.EXPECT().CreateLoadBalancer(lbName, "tcp").Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerSelectionFields(lbName, []string(nil)).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerExternalIDs(lbName, gomock.Eq(serviceScopedLBExternalIDs(svc, ctrl.config.ClusterRouter, serviceLBInternalTraffic))).Return(nil),
		fake.mockOvnClient.EXPECT().DeleteLoadBalancerAffinityTimeout(lbName).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerDistributed(lbName, true).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerTemplate(lbName, false).Return(nil),
	)
	if _, err := ctrl.ensureServiceScopedLB(svc, corev1.ProtocolTCP, ""); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureServiceScopedLBTrafficDistributionAddressFamily(t *testing.T) {
	fake := newFakeController(t)
	ctrl := fake.fakeController
	trafficDistribution := corev1.ServiceTrafficDistributionPreferSameNode
	svc := &corev1.Service{
		Namespace: "default", Name: "web", UID: types.UID("uid-template-ipv6"),
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, TrafficDistribution: &trafficDistribution},
	}
	lbName := serviceScopedLBNameForTrafficClassAndFamily(svc, corev1.ProtocolTCP, serviceLBInternalTraffic, "ipv6")
	gomock.InOrder(
		fake.mockOvnClient.EXPECT().CreateLoadBalancer(lbName, "tcp").Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerSelectionFields(lbName, []string(nil)).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerExternalIDs(lbName, gomock.Eq(serviceScopedLBExternalIDs(svc, ctrl.config.ClusterRouter, serviceLBInternalTraffic))).Return(nil),
		fake.mockOvnClient.EXPECT().DeleteLoadBalancerAffinityTimeout(lbName).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerDistributed(lbName, false).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerTemplate(lbName, true).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerAddressFamily(lbName, "ipv6").Return(nil),
	)

	got, err := ctrl.ensureServiceScopedLB(svc, corev1.ProtocolTCP, "ipv6")
	if err != nil {
		t.Fatal(err)
	}
	if got != lbName {
		t.Fatalf("ensureServiceScopedLB() = %q, want %q", got, lbName)
	}
}

func trafficDistributionEndpoint(address, node, zone string) discoveryv1.Endpoint {
	return discoveryv1.Endpoint{
		Addresses: []string{address},
		Hints: &discoveryv1.EndpointHints{
			ForNodes: []discoveryv1.ForNode{{Name: node}},
			ForZones: []discoveryv1.ForZone{{Name: zone}},
		},
	}
}

func TestReconcileServiceTrafficDistribution(t *testing.T) {
	nodes := []*corev1.Node{
		{Name: "node-a", Labels: map[string]string{corev1.LabelTopologyZone: "zone-a"}},
		{Name: "node-x", Labels: map[string]string{corev1.LabelTopologyZone: "zone-a"}},
		{Name: "node-y", Labels: map[string]string{corev1.LabelTopologyZone: "zone-c"}},
	}
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Nodes: nodes})
	if err != nil {
		t.Fatal(err)
	}

	trafficDistribution := corev1.ServiceTrafficDistributionPreferSameNode
	svc := &corev1.Service{
		Namespace: "default", Name: "web", UID: types.UID("uid-template-reconcile"),
		Spec: corev1.ServiceSpec{
			Type:                corev1.ServiceTypeClusterIP,
			ClusterIPs:          []string{"10.96.0.10"},
			TrafficDistribution: &trafficDistribution,
			Ports:               []corev1.ServicePort{{Name: "http", Protocol: corev1.ProtocolTCP, Port: 80}},
		},
	}
	endpointSlices := []*discoveryv1.EndpointSlice{{
		Ports: []discoveryv1.EndpointPort{{Name: new("http"), Port: new(int32(8080))}},
		Endpoints: []discoveryv1.Endpoint{
			trafficDistributionEndpoint("10.0.0.2", "node-a", "zone-a"),
			trafficDistributionEndpoint("10.0.0.3", "node-b", "zone-a"),
			trafficDistributionEndpoint("10.0.0.4", "node-c", "zone-b"),
		},
	}}
	vpc := &kubeovnv1.Vpc{Status: kubeovnv1.VpcStatus{
		TCPLoadBalancer:        "tcp",
		TCPSessionLoadBalancer: "tcp-session",
	}}
	chassises := []ovnsb.Chassis{
		{Name: "chassis-a", Hostname: "node-a"},
		{Name: "chassis-x", Hostname: "node-x"},
		{Name: "chassis-y", Hostname: "node-y"},
	}
	prefix := serviceTrafficDistributionVariablePrefix(svc)
	base := prefix + "tcp_" + util.Sha256Hash([]byte("10.96.0.10:80"))[:8]
	vipVariable, backendVariable := base+"_vip", base+"_backends"
	templateVIP := "^" + vipVariable + ":80"
	lbName := serviceScopedLBNameForTrafficClassAndFamily(svc, corev1.ProtocolTCP, serviceLBInternalTraffic, "ipv4")
	staleVIP := "^" + prefix + "stale_vip:81"

	fake.mockOvnSbClient.EXPECT().ListChassis().Return(&chassises, nil)
	fake.mockOvnClient.EXPECT().LoadBalancerMigrateVIP(
		lbName,
		templateVIP,
		[]string{"^" + backendVariable},
		"10.96.0.10:80",
		serviceScopedLBName(svc, corev1.ProtocolTCP),
		lbName,
	).Return(nil)
	fake.mockOvnClient.EXPECT().ListLoadBalancers(gomock.Any()).Return([]ovnnb.LoadBalancer{{
		Name:        lbName,
		ExternalIDs: map[string]string{serviceLBOwnerExternalID: string(svc.UID)},
		Vips: map[string]string{
			templateVIP: "^" + backendVariable,
			staleVIP:    "^old_backends",
		},
	}}, nil)
	fake.mockOvnClient.EXPECT().LoadBalancerDeleteVip(lbName, staleVIP, true).Return(nil)
	fake.mockOvnClient.EXPECT().ReconcileChassisTemplateVariables("chassis-a", prefix, gomock.Eq(map[string]string{
		vipVariable:     "10.96.0.10",
		backendVariable: "10.0.0.2:8080",
	})).Return(nil)
	fake.mockOvnClient.EXPECT().ReconcileChassisTemplateVariables("chassis-x", prefix, gomock.Eq(map[string]string{
		vipVariable:     "10.96.0.10",
		backendVariable: "10.0.0.2:8080,10.0.0.3:8080",
	})).Return(nil)
	fake.mockOvnClient.EXPECT().ReconcileChassisTemplateVariables("chassis-y", prefix, gomock.Eq(map[string]string{
		vipVariable:     "10.96.0.10",
		backendVariable: "10.0.0.2:8080,10.0.0.3:8080,10.0.0.4:8080",
	})).Return(nil)

	if err := fake.fakeController.reconcileServiceTrafficDistribution(
		svc,
		endpointSlices,
		vpc,
		[]string{"10.96.0.10"},
		map[string]serviceLBTrafficClass{"10.96.0.10": serviceLBInternalTraffic},
	); err != nil {
		t.Fatal(err)
	}
}

func TestServiceLBTrafficClassForVIP(t *testing.T) {
	classes := map[string]serviceLBTrafficClass{"10.0.0.1": serviceLBExternalTraffic}
	if got := serviceLBTrafficClassForVIP(classes, "10.0.0.1"); got != serviceLBExternalTraffic {
		t.Fatalf("external VIP class = %q", got)
	}
	if got := serviceLBTrafficClassForVIP(classes, "10.0.0.2"); got != serviceLBInternalTraffic {
		t.Fatalf("default VIP class = %q", got)
	}
}

func TestServiceLBMigrationCandidates(t *testing.T) {
	fake := newFakeController(t)
	ctrl := fake.fakeController
	svc := &corev1.Service{Namespace: "default", Name: "web", UID: types.UID("uid-migrate"), Spec: corev1.ServiceSpec{SessionAffinity: corev1.ServiceAffinityClientIP}}
	const vpcName = "test-vpc"
	legacy := ctrl.GenVpcLoadBalancer(vpcName)
	vpc := &kubeovnv1.Vpc{Name: vpcName, Status: kubeovnv1.VpcStatus{TCPLoadBalancer: legacy.TCPLoadBalancer, TCPSessionLoadBalancer: legacy.TCPSessLoadBalancer}}
	for _, trafficClass := range []serviceLBTrafficClass{serviceLBInternalTraffic, serviceLBExternalTraffic} {
		candidates := ctrl.serviceLBMigrationCandidates(svc, corev1.ProtocolTCP, vpc, trafficClass)
		for _, want := range []string{legacy.TCPLoadBalancer, legacy.TCPSessLoadBalancer, serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, trafficClass)} {
			if !slices.Contains(candidates, want) {
				t.Fatalf("migration candidates %v do not contain %q", candidates, want)
			}
		}
		otherClass := serviceLBInternalTraffic
		if trafficClass == serviceLBInternalTraffic {
			otherClass = serviceLBExternalTraffic
		}
		if slices.Contains(candidates, serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, otherClass)) {
			t.Fatalf("%s migration candidates unexpectedly contain %s LB: %v", trafficClass, otherClass, candidates)
		}
	}
	nonLegacy := vpc.DeepCopy()
	nonLegacy.Status.TCPLoadBalancer = "svc-hash-tcp-external"
	for _, trafficClass := range []serviceLBTrafficClass{serviceLBInternalTraffic, serviceLBExternalTraffic} {
		if candidates := ctrl.serviceLBMigrationCandidates(svc, corev1.ProtocolTCP, nonLegacy, trafficClass); slices.Contains(candidates, nonLegacy.Status.TCPLoadBalancer) {
			t.Fatalf("resource-scoped load balancer %q must not be treated as a legacy VPC load balancer: %v", nonLegacy.Status.TCPLoadBalancer, candidates)
		}
	}

	trafficDistribution := corev1.ServiceTrafficDistributionPreferSameZone
	templateService := &corev1.Service{
		Namespace: "default", Name: "web", UID: types.UID("uid-template-migrate"),
		Spec: corev1.ServiceSpec{
			Type:                corev1.ServiceTypeClusterIP,
			ClusterIPs:          []string{"10.96.0.10", "fd00:10:96::10"},
			TrafficDistribution: &trafficDistribution,
		},
	}
	candidates := ctrl.serviceLBMigrationCandidates(templateService, corev1.ProtocolTCP, vpc, serviceLBInternalTraffic)
	for _, family := range []string{"ipv4", "ipv6"} {
		want := serviceScopedLBNameForTrafficClassAndFamily(templateService, corev1.ProtocolTCP, serviceLBInternalTraffic, family)
		if !slices.Contains(candidates, want) {
			t.Fatalf("template migration candidates %v do not contain %q", candidates, want)
		}
	}
}

func TestDeleteServiceLBMigrationVIPSkipsMissingLoadBalancers(t *testing.T) {
	fake := newFakeController(t)
	ctrl := fake.fakeController
	trafficDistribution := corev1.ServiceTrafficDistributionPreferSameZone
	svc := &corev1.Service{
		Namespace: "default", Name: "web", UID: types.UID("uid-template-delete"),
		Spec: corev1.ServiceSpec{
			Type:                corev1.ServiceTypeClusterIP,
			ClusterIPs:          []string{"10.96.0.10", "fd00:10:96::10"},
			TrafficDistribution: &trafficDistribution,
		},
	}
	const vpcName = "test-vpc"
	legacy := ctrl.GenVpcLoadBalancer(vpcName)
	vpc := &kubeovnv1.Vpc{Name: vpcName, Status: kubeovnv1.VpcStatus{
		TCPLoadBalancer:        legacy.TCPLoadBalancer,
		TCPSessionLoadBalancer: legacy.TCPSessLoadBalancer,
	}}
	current := serviceScopedLBNameForTrafficClassAndFamily(svc, corev1.ProtocolTCP, serviceLBInternalTraffic, "ipv4")
	base := serviceScopedLBNameForTrafficClass(svc, corev1.ProtocolTCP, serviceLBInternalTraffic)
	ipv6 := serviceScopedLBNameForTrafficClassAndFamily(svc, corev1.ProtocolTCP, serviceLBInternalTraffic, "ipv6")
	gomock.InOrder(
		fake.mockOvnClient.EXPECT().LoadBalancerExists(legacy.TCPLoadBalancer).Return(true, nil),
		fake.mockOvnClient.EXPECT().LoadBalancerDeleteVip(legacy.TCPLoadBalancer, "10.96.0.10:80", true).Return(nil),
		fake.mockOvnClient.EXPECT().LoadBalancerExists(legacy.TCPSessLoadBalancer).Return(false, nil),
		fake.mockOvnClient.EXPECT().LoadBalancerExists(base).Return(false, nil),
		fake.mockOvnClient.EXPECT().LoadBalancerExists(ipv6).Return(false, nil),
	)

	if err := ctrl.deleteServiceLBMigrationVIP(svc, corev1.ProtocolTCP, current, "10.96.0.10:80", vpc, serviceLBInternalTraffic); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupLegacyVpcLoadBalancersOnlyDeletesFixedEmptyLBs(t *testing.T) {
	const vpcName = "test-vpc"
	probe := newFakeController(t)
	legacy := probe.fakeController.GenVpcLoadBalancer(vpcName)
	vpc := &kubeovnv1.Vpc{
		Name: vpcName,
		Status: kubeovnv1.VpcStatus{
			TCPLoadBalancer: legacy.TCPLoadBalancer,
			UDPLoadBalancer: "svc-hash-tcp-external",
		},
	}
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Vpcs: []*kubeovnv1.Vpc{vpc}})
	if err != nil {
		t.Fatal(err)
	}
	fake.mockOvnClient.EXPECT().GetLoadBalancer(legacy.TCPLoadBalancer, true).Return(&ovnnb.LoadBalancer{Name: legacy.TCPLoadBalancer}, nil)
	fake.mockOvnClient.EXPECT().DeleteLoadBalancers(gomock.Any()).DoAndReturn(func(filter func(*ovnnb.LoadBalancer) bool) error {
		if !filter(&ovnnb.LoadBalancer{Name: legacy.TCPLoadBalancer}) {
			t.Fatal("legacy fixed load balancer was not selected for deletion")
		}
		if filter(&ovnnb.LoadBalancer{Name: "svc-hash-tcp-external"}) {
			t.Fatal("resource-scoped load balancer was selected as legacy")
		}
		return nil
	})

	if err := fake.fakeController.cleanupLegacyVpcLoadBalancers(vpc); err != nil {
		t.Fatal(err)
	}
	updated, err := fake.fakeController.config.KubeOvnClient.KubeovnV1().Vpcs().Get(context.Background(), vpcName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status.TCPLoadBalancer != "" {
		t.Fatalf("legacy TCP load balancer status = %q, want empty", updated.Status.TCPLoadBalancer)
	}
	if updated.Status.UDPLoadBalancer != "svc-hash-tcp-external" {
		t.Fatalf("non-legacy status field changed to %q", updated.Status.UDPLoadBalancer)
	}
}

func TestDeleteServiceScopedLoadBalancers(t *testing.T) {
	fake := newFakeController(t)
	svc := &corev1.Service{Namespace: "default", Name: "web", UID: types.UID("uid-delete")}
	fake.mockOvnClient.EXPECT().DeleteLoadBalancers(gomock.Any()).Return(nil)
	if err := fake.fakeController.deleteServiceScopedLoadBalancers(svc); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteServiceScopedLBTrafficClass(t *testing.T) {
	fake := newFakeController(t)
	svc := &corev1.Service{Namespace: "default", Name: "web", UID: types.UID("uid-delete-class")}
	fake.mockOvnClient.EXPECT().DeleteLoadBalancers(gomock.Any()).Return(nil)
	if err := fake.fakeController.deleteServiceScopedLBTrafficClass(svc, corev1.ProtocolTCP, serviceLBExternalTraffic); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteServiceScopedLBExternalTraffic(t *testing.T) {
	fake := newFakeController(t)
	svc := &corev1.Service{
		Namespace: "default", Name: "web", UID: types.UID("uid-delete-external"),
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
			{Protocol: corev1.ProtocolTCP},
			{Protocol: corev1.ProtocolUDP},
			{Protocol: corev1.ProtocolTCP},
		}},
	}
	fake.mockOvnClient.EXPECT().DeleteLoadBalancers(gomock.Any()).Times(2).Return(nil)
	if err := fake.fakeController.deleteServiceScopedLBExternalTraffic(svc); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupServiceScopedLBVIPs(t *testing.T) {
	fake := newFakeController(t)
	svc := &corev1.Service{Namespace: "default", Name: "web", UID: types.UID("uid-clean-vips")}
	lbName := serviceScopedLBName(svc, corev1.ProtocolTCP)
	templateVIP := "^" + serviceTrafficDistributionVariablePrefix(svc) + "tcp_vip:80"
	fake.mockOvnClient.EXPECT().ListLoadBalancers(gomock.Any()).Return([]ovnnb.LoadBalancer{{
		Name: lbName,
		ExternalIDs: map[string]string{
			serviceLBOwnerExternalID: string(svc.UID),
			serviceLBVersionID:       serviceLBVersion,
		},
		Vips: map[string]string{
			"10.96.0.10:80": "10.0.0.2:8080",
			"10.96.0.10:81": "10.0.0.2:8081",
			templateVIP:     "^backends",
		},
	}}, nil)
	fake.mockOvnClient.EXPECT().LoadBalancerDeleteVip(lbName, "10.96.0.10:81", true).Return(nil)
	fake.mockOvnClient.EXPECT().LoadBalancerDeleteIPPortMapping(lbName, "10.96.0.10:81").Return(nil)
	desired := map[string]map[string]struct{}{lbName: {"10.96.0.10:80": {}}}
	if err := fake.fakeController.cleanupServiceScopedLBVIPs(svc, desired); err != nil {
		t.Fatal(err)
	}
}

func TestGCServiceTrafficDistributionVariables(t *testing.T) {
	fake := newFakeController(t)
	trafficDistribution := corev1.ServiceTrafficDistributionPreferSameZone
	svc := &corev1.Service{UID: types.UID("uid-active"), Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, TrafficDistribution: &trafficDistribution}}
	activePrefix := serviceTrafficDistributionVariablePrefix(svc)
	fake.mockOvnClient.EXPECT().DeleteChassisTemplateVariables(gomock.Any()).DoAndReturn(func(filter func(string) bool) error {
		if filter(activePrefix + "tcp_vip") {
			t.Fatal("active service variable selected for garbage collection")
		}
		if !filter(serviceTemplateVarRoot + "orphan_tcp_vip") {
			t.Fatal("orphan service variable not selected for garbage collection")
		}
		if filter("other_controller_variable") {
			t.Fatal("unmanaged variable selected for garbage collection")
		}
		return nil
	})
	if err := fake.fakeController.gcServiceTrafficDistributionVariables([]*corev1.Service{svc}); err != nil {
		t.Fatal(err)
	}
}

func TestGetDistributedIPPortMapping(t *testing.T) {
	pod := &corev1.Pod{
		Namespace: "default", Name: "backend",
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation: util.DefaultSubnet,
		},
	}
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods:    []*corev1.Pod{pod},
		Subnets: []*kubeovnv1.Subnet{{Name: util.DefaultSubnet}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctrl := fake.fakeController
	service := &corev1.Service{Namespace: "default", Name: "web", Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "web"}}}
	servicePort := corev1.ServicePort{Port: 80}
	endpoint := discoveryv1.Endpoint{Addresses: []string{"10.0.0.2"}, TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "default", Name: "backend"}}
	endpointSlice := &discoveryv1.EndpointSlice{
		Ports:     []discoveryv1.EndpointPort{{Port: new(int32(8080))}},
		Endpoints: []discoveryv1.Endpoint{endpoint},
	}
	portName := ovs.PodNameToPortName("backend", "default", util.OvnProvider)
	fake.mockOvnClient.EXPECT().GetLogicalSwitchPort(portName, true).Return(&ovnnb.LogicalSwitchPort{Name: portName}, nil)
	mapping, err := ctrl.getDistributedIPPortMapping([]*discoveryv1.EndpointSlice{endpointSlice}, service, servicePort, "10.96.0.10")
	if err != nil {
		t.Fatal(err)
	}
	if mapping["10.0.0.2"] != portName {
		t.Fatalf("distributed mapping = %v, want backend mapped to %q", mapping, portName)
	}

	endpoint.TargetRef = nil
	endpointSlice.Endpoints = []discoveryv1.Endpoint{endpoint}
	if _, err := ctrl.getDistributedIPPortMapping([]*discoveryv1.EndpointSlice{endpointSlice}, service, servicePort, "10.96.0.10"); err == nil {
		t.Fatal("expected ready endpoint without target to fail")
	}
}

func TestServiceEndpointCandidatesTerminatingFallback(t *testing.T) {
	ready, notReady := true, false
	terminating := true
	httpName, otherName := "http", "other"
	servicePort := corev1.ServicePort{Name: httpName, Port: 80}
	endpointSlices := []*discoveryv1.EndpointSlice{
		{
			Ports: []discoveryv1.EndpointPort{{Name: &httpName, Port: new(int32(8080))}},
			Endpoints: []discoveryv1.Endpoint{
				{Addresses: []string{"10.0.0.2", "fd00::2"}, Conditions: discoveryv1.EndpointConditions{Ready: &notReady, Terminating: &terminating}},
				{Addresses: []string{"10.0.0.3"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}},
			},
		},
		{
			Ports:     []discoveryv1.EndpointPort{{Name: &otherName, Port: new(int32(9090))}},
			Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.4"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}}},
		},
	}

	if got := (&Controller{}).getEndpointBackend(endpointSlices, servicePort, "10.96.0.10", true); !slices.Equal(got, []string{"10.0.0.3:8080"}) {
		t.Fatalf("backends with a ready endpoint = %v, want ready endpoint only", got)
	}

	endpointSlices[0].Endpoints[1].Conditions.Ready = &notReady
	if got := (&Controller{}).getEndpointBackend(endpointSlices, servicePort, "10.96.0.10", true); !slices.Equal(got, []string{"10.0.0.2:8080"}) {
		t.Fatalf("backends without a ready endpoint = %v, want serving and terminating fallback", got)
	}
	if got := (&Controller{}).getEndpointBackend(endpointSlices, servicePort, "10.96.0.10", false); len(got) != 0 {
		t.Fatalf("non-Local backends = %v, want no terminating fallback", got)
	}
}

func TestUpdateSubnetLoadBalancersIncludesScopedLoadBalancers(t *testing.T) {
	svc := &corev1.Service{
		Namespace: "default", Name: "web", UID: types.UID("uid-new-subnet"),
		Spec: corev1.ServiceSpec{
			SessionAffinity: corev1.ServiceAffinityClientIP,
			Ports:           []corev1.ServicePort{{Protocol: corev1.ProtocolTCP}},
		},
	}
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Services: []*corev1.Service{svc}})
	if err != nil {
		t.Fatal(err)
	}
	fake.fakeController.config.EnableLb = true
	vpc := &kubeovnv1.Vpc{
		Name: util.DefaultVpc,
		Status: kubeovnv1.VpcStatus{
			TCPLoadBalancer:         "tcp",
			TCPSessionLoadBalancer:  "tcp-session",
			UDPLoadBalancer:         "udp",
			UDPSessionLoadBalancer:  "udp-session",
			SctpLoadBalancer:        "sctp",
			SctpSessionLoadBalancer: "sctp-session",
		},
	}
	subnet := &kubeovnv1.Subnet{Name: "new-subnet", Spec: kubeovnv1.SubnetSpec{EnableLb: new(true)}}
	fake.mockOvnClient.EXPECT().LogicalSwitchUpdateLoadBalancers(
		subnet.Name,
		ovsdb.MutateOperationInsert,
		serviceScopedLBName(svc, corev1.ProtocolTCP),
	).Return(nil)
	if err := fake.fakeController.updateSubnetLoadBalancers(subnet, vpc); err != nil {
		t.Fatal(err)
	}
}

func TestDecorateDistributedIPPortMapping(t *testing.T) {
	mapping := IPPortMapping{"10.0.0.2": "pod-backend"}
	decorateDistributedIPPortMapping(mapping, "10.0.0.10")
	if mapping["10.0.0.2"] != "pod-backend:10.0.0.10" {
		t.Fatalf("decorated mapping = %v", mapping)
	}
	decorateDistributedIPPortMapping(mapping, "")
	if mapping["10.0.0.2"] != "pod-backend:10.0.0.10" {
		t.Fatalf("empty source IP changed mapping = %v", mapping)
	}
}
