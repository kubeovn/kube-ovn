package webhook

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const testControllerUserName = "system:serviceaccount:kube-system:kube-ovn-controller"

func newVipUpdateRequest(t *testing.T, username string, vipOld, vipNew ovnv1.Vip) admission.Request {
	t.Helper()
	oldRaw, err := json.Marshal(vipOld)
	require.NoError(t, err)
	newRaw, err := json.Marshal(vipNew)
	require.NoError(t, err)

	return admission.Request{
		Operation: admissionv1.Update,
		OldObject: runtime.RawExtension{Raw: oldRaw},
		Object:    runtime.RawExtension{Raw: newRaw},
		UserInfo:  authenticationv1.UserInfo{Username: username},
	}
}

func TestVipUpdateHookLegacyMacRepair(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = ovnv1.AddToScheme(scheme)
	decoder := admission.NewDecoder(scheme)

	baseSpec := ovnv1.VipSpec{
		Namespace: "ns",
		Subnet:    "test-subnet",
		Type:      util.SwitchLBRuleVip,
		V4ip:      "10.0.0.10",
	}
	const gatewayMAC = "00:00:00:01:02:03"
	oldStatus := ovnv1.VipStatus{V4ip: "10.0.0.10", Mac: gatewayMAC}

	newVip := func(mutate func(spec *ovnv1.VipSpec)) ovnv1.Vip {
		spec := *baseSpec.DeepCopy()
		if mutate != nil {
			mutate(&spec)
		}
		return ovnv1.Vip{Spec: spec, Status: oldStatus}
	}

	vipOld := newVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = gatewayMAC })

	cacheWithGatewayMAC := func(mac string) *mockCache {
		return &mockCache{
			objects: map[string]runtime.Object{
				"/test-subnet": &ovnv1.Subnet{
					Name:   "test-subnet",
					Status: ovnv1.SubnetStatus{GatewayMAC: mac},
				},
			},
		}
	}

	t.Run("allowed when controller renews mac of a switch_lb_rule vip colliding with the gateway mac", func(t *testing.T) {
		vipNew := newVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = "00:00:00:aa:bb:cc" })

		v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC(gatewayMAC), controllerUserName: testControllerUserName}
		resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, testControllerUserName, vipOld, vipNew))
		require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
	})

	t.Run("rejected when requester is not the controller", func(t *testing.T) {
		vipNew := newVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = "00:00:00:aa:bb:cc" })

		v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC(gatewayMAC), controllerUserName: testControllerUserName}
		resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, "system:serviceaccount:default:attacker", vipOld, vipNew))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "does not support change")
	})

	t.Run("rejected when controllerUserName is not configured", func(t *testing.T) {
		vipNew := newVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = "00:00:00:aa:bb:cc" })

		v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC(gatewayMAC)}
		resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, "", vipOld, vipNew))
		require.False(t, resp.Allowed)
	})

	t.Run("rejected when vip is not a switch_lb_rule vip", func(t *testing.T) {
		vipOld := newVip(func(spec *ovnv1.VipSpec) { spec.Type = ""; spec.MacAddress = gatewayMAC })
		vipNew := newVip(func(spec *ovnv1.VipSpec) { spec.Type = ""; spec.MacAddress = "00:00:00:aa:bb:cc" })

		v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC(gatewayMAC), controllerUserName: testControllerUserName}
		resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, testControllerUserName, vipOld, vipNew))
		require.False(t, resp.Allowed)
	})

	t.Run("rejected when more than the mac changes", func(t *testing.T) {
		vipNew := newVip(func(spec *ovnv1.VipSpec) {
			spec.MacAddress = "00:00:00:aa:bb:cc"
			spec.V4ip = "10.0.0.20"
		})

		v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC(gatewayMAC), controllerUserName: testControllerUserName}
		resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, testControllerUserName, vipOld, vipNew))
		require.False(t, resp.Allowed)
	})

	t.Run("rejected when the old mac never collided with the subnet gateway mac", func(t *testing.T) {
		vipNew := newVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = "00:00:00:aa:bb:cc" })

		v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC("00:00:00:ff:ff:ff"), controllerUserName: testControllerUserName}
		resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, testControllerUserName, vipOld, vipNew))
		require.False(t, resp.Allowed)
	})

	t.Run("rejected on a second repair attempt, once the vip no longer collides", func(t *testing.T) {
		// after a successful repair, vipOld.Status.Mac is the renewed mac, which no
		// longer equals the subnet's gateway mac: the exemption self-expires.
		repairedVip := newVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = "00:00:00:aa:bb:cc" })
		repairedVip.Status.Mac = "00:00:00:aa:bb:cc"
		vipNew := newVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = "00:00:00:dd:ee:ff" })
		vipNew.Status.Mac = "00:00:00:aa:bb:cc"

		v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC(gatewayMAC), controllerUserName: testControllerUserName}
		resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, testControllerUserName, repairedVip, vipNew))
		require.False(t, resp.Allowed)
	})

	t.Run("normal immutability still rejects an ordinary mac change", func(t *testing.T) {
		vipNew := newVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = "00:00:00:aa:bb:cc" })

		v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC(gatewayMAC), controllerUserName: testControllerUserName}
		resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, "system:serviceaccount:default:some-user", vipOld, vipNew))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "does not support change")
	})
}
