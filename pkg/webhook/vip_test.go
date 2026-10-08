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

const (
	testControllerUserName = "system:serviceaccount:kube-system:kube-ovn-controller"
	testGatewayMAC         = "00:00:00:01:02:03"
)

var testVipBaseSpec = ovnv1.VipSpec{
	Namespace: "ns",
	Subnet:    "test-subnet",
	Type:      util.SwitchLBRuleVip,
	V4ip:      "10.0.0.10",
}

func newRepairTestVip(mutate func(spec *ovnv1.VipSpec)) ovnv1.Vip {
	spec := *testVipBaseSpec.DeepCopy()
	if mutate != nil {
		mutate(&spec)
	}
	return ovnv1.Vip{Spec: spec, Status: ovnv1.VipStatus{V4ip: "10.0.0.10", Mac: testGatewayMAC}}
}

func cacheWithGatewayMAC(mac string) *mockCache {
	return &mockCache{
		objects: map[string]runtime.Object{
			"/test-subnet": &ovnv1.Subnet{
				Name:   "test-subnet",
				Status: ovnv1.SubnetStatus{GatewayMAC: mac},
			},
		},
	}
}

func newRepairDecoder(t *testing.T) admission.Decoder {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, ovnv1.AddToScheme(scheme))
	return admission.NewDecoder(scheme)
}

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

func TestVipUpdateHookLegacyMacRepairAllowed(t *testing.T) {
	decoder := newRepairDecoder(t)
	vipOld := newRepairTestVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = testGatewayMAC })
	vipNew := newRepairTestVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = "00:00:00:aa:bb:cc" })

	v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC(testGatewayMAC), controllerUserName: testControllerUserName}
	resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, testControllerUserName, vipOld, vipNew))
	require.True(t, resp.Allowed, "expected allowed, got: %+v", resp.Result)
}

func TestVipUpdateHookLegacyMacRepairRejectsWrongIdentity(t *testing.T) {
	decoder := newRepairDecoder(t)
	vipOld := newRepairTestVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = testGatewayMAC })
	vipNew := newRepairTestVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = "00:00:00:aa:bb:cc" })

	t.Run("requester is not the controller", func(t *testing.T) {
		v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC(testGatewayMAC), controllerUserName: testControllerUserName}
		resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, "system:serviceaccount:default:attacker", vipOld, vipNew))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "does not support change")
	})

	t.Run("controllerUserName is not configured", func(t *testing.T) {
		v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC(testGatewayMAC)}
		resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, "", vipOld, vipNew))
		require.False(t, resp.Allowed)
	})

	t.Run("normal immutability still rejects an ordinary mac change", func(t *testing.T) {
		v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC(testGatewayMAC), controllerUserName: testControllerUserName}
		resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, "system:serviceaccount:default:some-user", vipOld, vipNew))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "does not support change")
	})
}

func TestVipUpdateHookLegacyMacRepairRejectsWrongShape(t *testing.T) {
	decoder := newRepairDecoder(t)

	t.Run("vip is not a switch_lb_rule vip", func(t *testing.T) {
		vipOld := newRepairTestVip(func(spec *ovnv1.VipSpec) { spec.Type = ""; spec.MacAddress = testGatewayMAC })
		vipNew := newRepairTestVip(func(spec *ovnv1.VipSpec) { spec.Type = ""; spec.MacAddress = "00:00:00:aa:bb:cc" })

		v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC(testGatewayMAC), controllerUserName: testControllerUserName}
		resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, testControllerUserName, vipOld, vipNew))
		require.False(t, resp.Allowed)
	})

	t.Run("more than the mac changes", func(t *testing.T) {
		vipOld := newRepairTestVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = testGatewayMAC })
		vipNew := newRepairTestVip(func(spec *ovnv1.VipSpec) {
			spec.MacAddress = "00:00:00:aa:bb:cc"
			spec.V4ip = "10.0.0.20"
		})

		v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC(testGatewayMAC), controllerUserName: testControllerUserName}
		resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, testControllerUserName, vipOld, vipNew))
		require.False(t, resp.Allowed)
	})
}

func TestVipUpdateHookLegacyMacRepairRejectsWithoutGenuineCollision(t *testing.T) {
	decoder := newRepairDecoder(t)

	t.Run("old mac never collided with the subnet gateway mac", func(t *testing.T) {
		vipOld := newRepairTestVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = testGatewayMAC })
		vipNew := newRepairTestVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = "00:00:00:aa:bb:cc" })

		v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC("00:00:00:ff:ff:ff"), controllerUserName: testControllerUserName}
		resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, testControllerUserName, vipOld, vipNew))
		require.False(t, resp.Allowed)
	})

	t.Run("second repair attempt, once the vip no longer collides", func(t *testing.T) {
		// vipOld.Status.Mac is now the renewed mac, which no longer matches the
		// subnet's gateway mac: the exemption self-expires.
		repairedVip := newRepairTestVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = "00:00:00:aa:bb:cc" })
		repairedVip.Status.Mac = "00:00:00:aa:bb:cc"
		vipNew := newRepairTestVip(func(spec *ovnv1.VipSpec) { spec.MacAddress = "00:00:00:dd:ee:ff" })
		vipNew.Status.Mac = "00:00:00:aa:bb:cc"

		v := &ValidatingHook{decoder: decoder, cache: cacheWithGatewayMAC(testGatewayMAC), controllerUserName: testControllerUserName}
		resp := v.VipUpdateHook(context.Background(), newVipUpdateRequest(t, testControllerUserName, repairedVip, vipNew))
		require.False(t, resp.Allowed)
	})
}
