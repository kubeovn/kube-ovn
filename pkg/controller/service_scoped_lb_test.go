package controller

import (
	"testing"

	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
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
	if got := len(serviceScopedLBNames(svc)); got != 2 {
		t.Fatalf("serviceScopedLBNames() returned %d names, want 2", got)
	}
	ids := serviceScopedLBExternalIDs(svc)
	if ids[serviceLBOwnerExternalID] != string(svc.UID) || ids[serviceLBVersionID] != serviceLBVersion {
		t.Fatalf("service LB ownership metadata = %v", ids)
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
		fake.mockOvnClient.EXPECT().SetLoadBalancerExternalIDs(lbName, gomock.Eq(serviceScopedLBExternalIDs(svc))).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerAffinityTimeout(lbName, 42).Return(nil),
		fake.mockOvnClient.EXPECT().SetLoadBalancerDistributed(lbName, false).Return(nil),
	)

	got, err := ctrl.ensureServiceScopedLB(svc, corev1.ProtocolTCP)
	if err != nil {
		t.Fatal(err)
	}
	if got != lbName {
		t.Fatalf("ensureServiceScopedLB() = %q, want %q", got, lbName)
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
	endpoint := discoveryv1.Endpoint{Addresses: []string{"10.0.0.2"}, TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "default", Name: "backend"}}
	portName := ovs.PodNameToPortName("backend", "default", util.OvnProvider)
	fake.mockOvnClient.EXPECT().GetLogicalSwitchPort(portName, true).Return(&ovnnb.LogicalSwitchPort{Name: portName}, nil)
	mapping, err := ctrl.getDistributedIPPortMapping([]*discoveryv1.EndpointSlice{{Endpoints: []discoveryv1.Endpoint{endpoint}}}, service)
	if err != nil {
		t.Fatal(err)
	}
	if mapping["10.0.0.2"] != portName {
		t.Fatalf("distributed mapping = %v, want backend mapped to %q", mapping, portName)
	}

	endpoint.TargetRef = nil
	if _, err := ctrl.getDistributedIPPortMapping([]*discoveryv1.EndpointSlice{{Endpoints: []discoveryv1.Endpoint{endpoint}}}, service); err == nil {
		t.Fatal("expected ready endpoint without target to fail")
	}
}
