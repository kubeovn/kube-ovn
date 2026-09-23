package webhook

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// natReferenceCache holds the objects the NAT gateway and EIP hooks validate against: the two
// config maps, a vpc and a subnet, one healthy and one terminating gateway, and one healthy and
// one terminating qos policy.
func natReferenceCache() *mockCache {
	now := metav1.Now()
	objects := map[string]runtime.Object{
		"kube-system/ovn-vpc-nat-config": &corev1.ConfigMap{
			Name: util.VpcNatConfig, Namespace: metav1.NamespaceSystem,
			Data: map[string]string{"image": "test-image"},
		},
		"kube-system/ovn-vpc-nat-gw-config": &corev1.ConfigMap{
			Name: util.VpcNatGatewayConfig, Namespace: metav1.NamespaceSystem,
			Data: map[string]string{"enable-vpc-nat-gw": "true"},
		},
		"/test-vpc": &ovnv1.Vpc{Name: "test-vpc"},
		"/test-subnet": &ovnv1.Subnet{
			Name: "test-subnet",
			Spec: ovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24"},
		},
		"/healthy-gw": &ovnv1.VpcNatGateway{
			Name: "healthy-gw",
			Spec: ovnv1.VpcNatGatewaySpec{Vpc: "test-vpc", Subnet: "test-subnet"},
		},
		"/terminating-gw": &ovnv1.VpcNatGateway{
			Name: "terminating-gw", DeletionTimestamp: &now,
			Spec: ovnv1.VpcNatGatewaySpec{Vpc: "test-vpc", Subnet: "test-subnet"},
		},
		"/healthy-qos": &ovnv1.QoSPolicy{Name: "healthy-qos"},
		"/terminating-qos": &ovnv1.QoSPolicy{
			Name: "terminating-qos", DeletionTimestamp: &now,
		},
	}
	return &mockCache{objects: objects}
}

func newNatReferenceHook(t *testing.T) *ValidatingHook {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, ovnv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	return &ValidatingHook{decoder: admission.NewDecoder(scheme), cache: natReferenceCache()}
}

func updateRequest(t *testing.T, oldObj, newObj any) admission.Request {
	t.Helper()
	oldRaw, err := json.Marshal(oldObj)
	require.NoError(t, err)
	newRaw, err := json.Marshal(newObj)
	require.NoError(t, err)
	return admission.Request{
		Operation: admissionv1.Update,
		Object:    runtime.RawExtension{Raw: newRaw},
		OldObject: runtime.RawExtension{Raw: oldRaw},
	}
}

func createRequest(t *testing.T, obj any) admission.Request {
	t.Helper()
	raw, err := json.Marshal(obj)
	require.NoError(t, err)
	return admission.Request{Operation: admissionv1.Create, Object: runtime.RawExtension{Raw: raw}}
}

// TestNatGwQoSReferenceValidatedOnlyWhenIntroduced pins the reference contract of the NAT
// gateway hook: a terminating policy must not be referenced by a *new* binding, while a gateway
// that already references it keeps being updatable, so that its controller can still refresh
// labels and status and clean up the data plane while the policy terminates.
func TestNatGwQoSReferenceValidatedOnlyWhenIntroduced(t *testing.T) {
	hook := newNatReferenceHook(t)
	gateway := func(qos string, tolerations []corev1.Toleration) *ovnv1.VpcNatGateway {
		return &ovnv1.VpcNatGateway{
			Name: "test-gw",
			Spec: ovnv1.VpcNatGatewaySpec{
				Vpc: "test-vpc", Subnet: "test-subnet", Replicas: 1,
				QoSPolicy: qos, Tolerations: tolerations,
			},
		}
	}

	t.Run("create with a terminating policy is rejected", func(t *testing.T) {
		resp := hook.VpcNatGwCreateOrUpdateHook(context.Background(), createRequest(t, gateway("terminating-qos", nil)))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "is terminating")
	})

	t.Run("create with a healthy policy is allowed", func(t *testing.T) {
		resp := hook.VpcNatGwCreateOrUpdateHook(context.Background(), createRequest(t, gateway("healthy-qos", nil)))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})

	t.Run("label only update keeps working while the policy terminates", func(t *testing.T) {
		oldGw := gateway("terminating-qos", nil)
		newGw := gateway("terminating-qos", nil)
		newGw.Labels = map[string]string{util.QoSPolicyUIDLabel: "terminating-qos-uid"}
		resp := hook.VpcNatGwCreateOrUpdateHook(context.Background(), updateRequest(t, oldGw, newGw))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})

	t.Run("unrelated spec change keeps working while the policy terminates", func(t *testing.T) {
		oldGw := gateway("terminating-qos", nil)
		newGw := gateway("terminating-qos", []corev1.Toleration{{
			Key: "dedicated", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule,
		}})
		resp := hook.VpcNatGwCreateOrUpdateHook(context.Background(), updateRequest(t, oldGw, newGw))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})

	t.Run("switching to a terminating policy is rejected", func(t *testing.T) {
		resp := hook.VpcNatGwCreateOrUpdateHook(context.Background(),
			updateRequest(t, gateway("healthy-qos", nil), gateway("terminating-qos", nil)))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "is terminating")
	})

	t.Run("clearing the reference is allowed", func(t *testing.T) {
		resp := hook.VpcNatGwCreateOrUpdateHook(context.Background(),
			updateRequest(t, gateway("terminating-qos", nil), gateway("", nil)))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})
}

// TestIptablesEipReferenceValidatedOnlyWhenIntroduced is the eip counterpart: its gateway and
// policy references are validated when the request introduces them, not when the eip's specs
// change for another reason.
func TestIptablesEipReferenceValidatedOnlyWhenIntroduced(t *testing.T) {
	hook := newNatReferenceHook(t)
	eip := func(natGwDp, qos string) *ovnv1.IptablesEIP {
		return &ovnv1.IptablesEIP{
			Name: "test-eip",
			Spec: ovnv1.IptablesEIPSpec{
				NatGwDp: natGwDp, QoSPolicy: qos, ExternalSubnet: "test-subnet", V4ip: "10.0.0.5",
			},
		}
	}

	t.Run("create with a terminating gateway is rejected", func(t *testing.T) {
		resp := hook.iptablesEIPCreateHook(context.Background(), createRequest(t, eip("terminating-gw", "")))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "is terminating")
	})

	t.Run("create with a terminating policy is rejected", func(t *testing.T) {
		resp := hook.iptablesEIPCreateHook(context.Background(), createRequest(t, eip("healthy-gw", "terminating-qos")))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "is terminating")
	})

	t.Run("unrelated spec change keeps working while the gateway terminates", func(t *testing.T) {
		oldEip := eip("terminating-gw", "")
		newEip := eip("terminating-gw", "")
		newEip.Spec.MacAddress = "00:00:00:00:00:01"
		resp := hook.iptablesEIPUpdateHook(context.Background(), updateRequest(t, oldEip, newEip))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})

	t.Run("unrelated spec change keeps working while the policy terminates", func(t *testing.T) {
		oldEip := eip("healthy-gw", "terminating-qos")
		newEip := eip("healthy-gw", "terminating-qos")
		newEip.Spec.MacAddress = "00:00:00:00:00:01"
		resp := hook.iptablesEIPUpdateHook(context.Background(), updateRequest(t, oldEip, newEip))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})

	t.Run("switching to a terminating policy is rejected", func(t *testing.T) {
		resp := hook.iptablesEIPUpdateHook(context.Background(),
			updateRequest(t, eip("healthy-gw", ""), eip("healthy-gw", "terminating-qos")))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "is terminating")
	})
}
