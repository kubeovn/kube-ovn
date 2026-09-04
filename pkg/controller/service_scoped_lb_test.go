package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

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
