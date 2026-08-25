package webhook

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestValidateVpcNatGatewayAllowsDynamicLanIP(t *testing.T) {
	cache := &mockCache{objects: map[string]runtime.Object{
		"kube-system/ovn-vpc-nat-config": &corev1.ConfigMap{
			Name: "ovn-vpc-nat-config", Namespace: metav1.NamespaceSystem,
		},
		"/vpc":    &ovnv1.Vpc{Name: "vpc"},
		"/subnet": &ovnv1.Subnet{Name: "subnet", Spec: ovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24"}},
	}}
	hook := &ValidatingHook{cache: cache}
	gw := &ovnv1.VpcNatGateway{
		Name: "gw",
		Spec: ovnv1.VpcNatGatewaySpec{Vpc: "vpc", Subnet: "subnet", Replicas: 1},
	}
	require.NoError(t, hook.ValidateVpcNatGW(context.Background(), gw))

	// lanIp is ignored in HA mode to remain compatible with objects that may
	// still carry a stale value from before dynamic allocation was introduced.
	gw.Spec.Replicas = 2
	gw.Spec.LanIP = "10.0.0.10"
	require.NoError(t, hook.ValidateVpcNatGW(context.Background(), gw))
}

func TestValidateVpcNatGatewayLanIPUpdate(t *testing.T) {
	tests := []struct {
		name      string
		oldSpec   string
		oldStatus string
		replicas  int32
		newSpec   string
		wantError bool
	}{
		{name: "dynamic value is unchanged", oldSpec: "", oldStatus: "", newSpec: ""},
		{name: "observed status may be persisted", oldSpec: "", oldStatus: "10.0.0.10", newSpec: "10.0.0.10"},
		{name: "unobserved value is rejected", oldSpec: "", oldStatus: "", newSpec: "10.0.0.10", wantError: true},
		{name: "value different from status is rejected", oldSpec: "", oldStatus: "10.0.0.10", newSpec: "10.0.0.11", wantError: true},
		{name: "persisted value is immutable", oldSpec: "10.0.0.10", oldStatus: "10.0.0.10", newSpec: "10.0.0.11", wantError: true},
		{name: "persisted value may remain unchanged", oldSpec: "10.0.0.10", oldStatus: "10.0.0.10", newSpec: "10.0.0.10"},
		{name: "HA validation is handled separately", oldStatus: "10.0.0.10", replicas: 2, newSpec: "10.0.0.11"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldGw := &ovnv1.VpcNatGateway{
				Name:   "gw",
				Spec:   ovnv1.VpcNatGatewaySpec{LanIP: tt.oldSpec},
				Status: ovnv1.VpcNatGatewayStatus{LanIP: tt.oldStatus},
			}
			newGw := oldGw.DeepCopy()
			newGw.Spec.LanIP = tt.newSpec
			newGw.Spec.Replicas = tt.replicas
			err := validateVpcNatGatewayLanIPUpdate(oldGw, newGw)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
