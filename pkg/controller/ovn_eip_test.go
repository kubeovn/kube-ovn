package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func Test_enqueueUpdateOvnEip(t *testing.T) {
	newTestController := func(t *testing.T) *Controller {
		ctrl := &Controller{
			updateOvnEipQueue: newTypedRateLimitingQueue[string]("UpdateOvnEip", nil),
			resetOvnEipQueue:  newTypedRateLimitingQueue[string]("ResetOvnEip", nil),
		}
		t.Cleanup(func() {
			ctrl.updateOvnEipQueue.ShutDown()
			ctrl.resetOvnEipQueue.ShutDown()
		})
		return ctrl
	}

	t.Run("v6 ip change is rejected via resetOvnEipQueue, not updateOvnEipQueue", func(t *testing.T) {
		ctrl := newTestController(t)
		oldEip := &kubeovnv1.OvnEip{
			ObjectMeta: metav1.ObjectMeta{Name: "eip-v6"},
			Spec:       kubeovnv1.OvnEipSpec{V6Ip: "fc00:1::a"},
		}
		newEip := oldEip.DeepCopy()
		newEip.Spec.V6Ip = "fc00:1::b"

		ctrl.enqueueUpdateOvnEip(oldEip, newEip)

		require.Equal(t, 1, ctrl.resetOvnEipQueue.Len())
		require.Equal(t, 0, ctrl.updateOvnEipQueue.Len())
		key, _ := ctrl.resetOvnEipQueue.Get()
		require.Equal(t, "eip-v6", key)
	})

	t.Run("v4 ip change is rejected via resetOvnEipQueue", func(t *testing.T) {
		ctrl := newTestController(t)
		oldEip := &kubeovnv1.OvnEip{
			ObjectMeta: metav1.ObjectMeta{Name: "eip-v4"},
			Spec:       kubeovnv1.OvnEipSpec{V4Ip: "192.168.0.5"},
		}
		newEip := oldEip.DeepCopy()
		newEip.Spec.V4Ip = "192.168.0.6"

		ctrl.enqueueUpdateOvnEip(oldEip, newEip)

		require.Equal(t, 1, ctrl.resetOvnEipQueue.Len())
		require.Equal(t, 0, ctrl.updateOvnEipQueue.Len())
	})

	t.Run("v4 ip assigned from empty is queued via updateOvnEipQueue", func(t *testing.T) {
		ctrl := newTestController(t)
		oldEip := &kubeovnv1.OvnEip{
			ObjectMeta: metav1.ObjectMeta{Name: "eip-assign"},
			Spec:       kubeovnv1.OvnEipSpec{V4Ip: "", V6Ip: "fc00:1::a"},
		}
		newEip := oldEip.DeepCopy()
		newEip.Spec.V4Ip = "192.168.0.5"

		ctrl.enqueueUpdateOvnEip(oldEip, newEip)

		require.Equal(t, 0, ctrl.resetOvnEipQueue.Len())
		require.Equal(t, 1, ctrl.updateOvnEipQueue.Len())
	})

	t.Run("v6 ip assigned from empty is queued via updateOvnEipQueue", func(t *testing.T) {
		ctrl := newTestController(t)
		oldEip := &kubeovnv1.OvnEip{
			ObjectMeta: metav1.ObjectMeta{Name: "eip-assign-v6"},
			Spec:       kubeovnv1.OvnEipSpec{V4Ip: "192.168.0.5", V6Ip: ""},
		}
		newEip := oldEip.DeepCopy()
		newEip.Spec.V6Ip = "fc00:1::a"

		ctrl.enqueueUpdateOvnEip(oldEip, newEip)

		require.Equal(t, 0, ctrl.resetOvnEipQueue.Len())
		require.Equal(t, 1, ctrl.updateOvnEipQueue.Len())
	})
}

func Test_getOvnEipNat(t *testing.T) {
	// NAT rules always carry an eip_v4_ip label, so a pure-IPv6 rule still has
	// eip_v4_ip="" together with its own eip_v6_ip label.
	ipv6Dnat := &kubeovnv1.OvnDnatRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dnat-v6",
			Labels: map[string]string{
				util.EipV4IpLabel: "",
				util.EipV6IpLabel: util.IPv6ToLabelValue("fc00:1::a"),
			},
		},
	}
	ipv6Snat := &kubeovnv1.OvnSnatRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: "snat-v6",
			Labels: map[string]string{
				util.EipV4IpLabel: "",
				util.EipV6IpLabel: util.IPv6ToLabelValue("fc00:1::a"),
			},
		},
	}
	ipv4Dnat := &kubeovnv1.OvnDnatRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dnat-v4",
			Labels: map[string]string{
				util.EipV4IpLabel: "192.168.0.5",
				util.EipV6IpLabel: "",
			},
		},
	}

	// Regression: a pure-IPv6 EIP (V4Ip="") must not be considered "in use" by
	// an unrelated IPv6 NAT rule just because both carry an empty eip_v4_ip
	// label. Previously getOvnEipNat queried with {eip_v4_ip: ""}, matching
	// every IPv6-only NAT rule and blocking the EIP from ever being deleted.
	t.Run("pure IPv6 EIP does not match unrelated IPv6 NAT via empty v4 label", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			OvnDnatRules: []*kubeovnv1.OvnDnatRule{ipv6Dnat},
			OvnSnatRules: []*kubeovnv1.OvnSnatRule{ipv6Snat},
		})
		require.NoError(t, err)
		nat, err := fc.fakeController.getOvnEipNat("", "fc00:1::7")
		require.NoError(t, err)
		require.Empty(t, nat)
	})

	t.Run("pure IPv6 EIP matches NAT rules that actually use its v6 ip", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			OvnDnatRules: []*kubeovnv1.OvnDnatRule{ipv6Dnat},
			OvnSnatRules: []*kubeovnv1.OvnSnatRule{ipv6Snat},
		})
		require.NoError(t, err)
		nat, err := fc.fakeController.getOvnEipNat("", "fc00:1::a")
		require.NoError(t, err)
		require.Equal(t, util.DnatUsingEip+","+util.SnatUsingEip, nat)
	})

	t.Run("IPv4 EIP matches a NAT rule that uses its v4 ip", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			OvnDnatRules: []*kubeovnv1.OvnDnatRule{ipv4Dnat},
		})
		require.NoError(t, err)
		nat, err := fc.fakeController.getOvnEipNat("192.168.0.5", "")
		require.NoError(t, err)
		require.Equal(t, util.DnatUsingEip, nat)
	})

	t.Run("IPv4 EIP does not match a different v4 NAT rule", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			OvnDnatRules: []*kubeovnv1.OvnDnatRule{ipv4Dnat},
		})
		require.NoError(t, err)
		nat, err := fc.fakeController.getOvnEipNat("192.168.0.99", "")
		require.NoError(t, err)
		require.Empty(t, nat)
	})

	t.Run("EIP with no ip queries nothing", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			OvnDnatRules: []*kubeovnv1.OvnDnatRule{ipv6Dnat},
		})
		require.NoError(t, err)
		nat, err := fc.fakeController.getOvnEipNat("", "")
		require.NoError(t, err)
		require.Empty(t, nat)
	})
}
