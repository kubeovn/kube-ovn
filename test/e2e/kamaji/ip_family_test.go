package kamaji

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestRequiredPodAddressFamilies(t *testing.T) {
	tests := []struct {
		name     string
		family   string
		wantIPv4 bool
		wantIPv6 bool
		wantErr  bool
	}{
		{name: "empty defaults to ipv4", wantIPv4: true},
		{name: "ipv4", family: "ipv4", wantIPv4: true},
		{name: "ipv6", family: "ipv6", wantIPv6: true},
		{name: "dual", family: "dual", wantIPv4: true, wantIPv6: true},
		{name: "reject invalid family", family: "bad", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIPv4, gotIPv6, err := requiredPodAddressFamilies(tt.family)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotIPv4 != tt.wantIPv4 || gotIPv6 != tt.wantIPv6 {
				t.Fatalf("requiredPodAddressFamilies(%q) = (%v, %v), want (%v, %v)",
					tt.family, gotIPv4, gotIPv6, tt.wantIPv4, tt.wantIPv6)
			}
		})
	}
}

func TestPodAddressFamilies(t *testing.T) {
	tests := []struct {
		name     string
		pod      corev1.Pod
		wantIPv4 bool
		wantIPv6 bool
	}{
		{
			name: "uses pod status addresses",
			pod: corev1.Pod{Status: corev1.PodStatus{PodIPs: []corev1.PodIP{
				{IP: "10.16.0.12"},
				{IP: "fd00:10:16::12"},
			}}},
			wantIPv4: true,
			wantIPv6: true,
		},
		{
			name: "falls back to kube-ovn allocation annotation",
			pod: corev1.Pod{ObjectMeta: metav1Object(map[string]string{
				util.IPAddressAnnotation: "fd00:10:16::13",
			})},
			wantIPv6: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIPv4, gotIPv6 := podAddressFamilies(&tt.pod)
			if gotIPv4 != tt.wantIPv4 || gotIPv6 != tt.wantIPv6 {
				t.Fatalf("podAddressFamilies() = (%v, %v), want (%v, %v)",
					gotIPv4, gotIPv6, tt.wantIPv4, tt.wantIPv6)
			}
		})
	}
}

func metav1Object(annotations map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Annotations: annotations}
}
