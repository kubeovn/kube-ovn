package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"

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
		Name: "dnat-v6",
		Labels: map[string]string{
			util.EipV4IpLabel: "",
			util.EipV6IpLabel: util.IPv6ToLabelValue("fc00:1::a"),
		},
	}
	ipv6Snat := &kubeovnv1.OvnSnatRule{
		Name: "snat-v6",
		Labels: map[string]string{
			util.EipV4IpLabel: "",
			util.EipV6IpLabel: util.IPv6ToLabelValue("fc00:1::a"),
		},
	}
	ipv4Dnat := &kubeovnv1.OvnDnatRule{
		Name: "dnat-v4",
		Labels: map[string]string{
			util.EipV4IpLabel: "192.168.0.5",
			util.EipV6IpLabel: "",
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

// assertEnqueueAddRoutingWithFinalizer checks the OVN add-path routing: a live object goes to the
// add queue, a terminating object with a finalizer goes to the update queue (deletion cleanup), and
// a terminating object whose finalizer is already gone is skipped (cleanup done, awaiting deletion).
func assertEnqueueAddRoutingWithFinalizer(
	t *testing.T,
	addQueue, updateQueue workqueue.TypedRateLimitingInterface[string],
	enqueue func(any),
	live, terminatingWithFinalizer, terminatingNoFinalizer any,
) {
	t.Helper()
	enqueue(live)
	require.Equal(t, 1, addQueue.Len(), "live object should go to the add queue")
	require.Equal(t, 0, updateQueue.Len(), "live object must not go to the update queue")

	enqueue(terminatingWithFinalizer)
	require.Equal(t, 1, addQueue.Len(), "terminating object must not go to the add queue")
	require.Equal(t, 1, updateQueue.Len(), "terminating object with finalizer should go to the update queue")

	enqueue(terminatingNoFinalizer)
	require.Equal(t, 1, addQueue.Len(), "terminating object without finalizer must not go to the add queue")
	require.Equal(t, 1, updateQueue.Len(), "terminating object without finalizer must not be re-enqueued")
}

func TestEnqueueAddOvnEip(t *testing.T) {
	t.Parallel()
	c := &Controller{
		config:            &Configuration{},
		addOvnEipQueue:    newTypedRateLimitingQueue[string]("AddOvnEip", nil),
		updateOvnEipQueue: newTypedRateLimitingQueue[string]("UpdateOvnEip", nil),
	}
	t.Cleanup(c.addOvnEipQueue.ShutDown)
	t.Cleanup(c.updateOvnEipQueue.ShutDown)
	now := metav1.Now()
	fin := []string{util.KubeOVNControllerFinalizer}
	assertEnqueueAddRoutingWithFinalizer(t, c.addOvnEipQueue, c.updateOvnEipQueue, c.enqueueAddOvnEip,
		&kubeovnv1.OvnEip{Name: "live-eip"},
		&kubeovnv1.OvnEip{Name: "terminating-eip", DeletionTimestamp: &now, Finalizers: fin},
		&kubeovnv1.OvnEip{Name: "terminating-eip-no-finalizer", DeletionTimestamp: &now},
	)
}

func TestEnqueueAddOvnFip(t *testing.T) {
	t.Parallel()
	c := &Controller{
		addOvnFipQueue:    newTypedRateLimitingQueue[string]("AddOvnFip", nil),
		updateOvnFipQueue: newTypedRateLimitingQueue[string]("UpdateOvnFip", nil),
	}
	t.Cleanup(c.addOvnFipQueue.ShutDown)
	t.Cleanup(c.updateOvnFipQueue.ShutDown)
	now := metav1.Now()
	fin := []string{util.KubeOVNControllerFinalizer}
	assertEnqueueAddRoutingWithFinalizer(t, c.addOvnFipQueue, c.updateOvnFipQueue, c.enqueueAddOvnFip,
		&kubeovnv1.OvnFip{Name: "live-fip"},
		&kubeovnv1.OvnFip{Name: "terminating-fip", DeletionTimestamp: &now, Finalizers: fin},
		&kubeovnv1.OvnFip{Name: "terminating-fip-no-finalizer", DeletionTimestamp: &now},
	)
}

func TestEnqueueAddOvnDnatRule(t *testing.T) {
	t.Parallel()
	c := &Controller{
		addOvnDnatRuleQueue:    newTypedRateLimitingQueue[string]("AddOvnDnat", nil),
		updateOvnDnatRuleQueue: newTypedRateLimitingQueue[string]("UpdateOvnDnat", nil),
	}
	t.Cleanup(c.addOvnDnatRuleQueue.ShutDown)
	t.Cleanup(c.updateOvnDnatRuleQueue.ShutDown)
	now := metav1.Now()
	fin := []string{util.KubeOVNControllerFinalizer}
	assertEnqueueAddRoutingWithFinalizer(t, c.addOvnDnatRuleQueue, c.updateOvnDnatRuleQueue, c.enqueueAddOvnDnatRule,
		&kubeovnv1.OvnDnatRule{Name: "live-dnat"},
		&kubeovnv1.OvnDnatRule{Name: "terminating-dnat", DeletionTimestamp: &now, Finalizers: fin},
		&kubeovnv1.OvnDnatRule{Name: "terminating-dnat-no-finalizer", DeletionTimestamp: &now},
	)
}

func TestEnqueueAddOvnSnatRule(t *testing.T) {
	t.Parallel()
	c := &Controller{
		addOvnSnatRuleQueue:    newTypedRateLimitingQueue[string]("AddOvnSnat", nil),
		updateOvnSnatRuleQueue: newTypedRateLimitingQueue[string]("UpdateOvnSnat", nil),
	}
	t.Cleanup(c.addOvnSnatRuleQueue.ShutDown)
	t.Cleanup(c.updateOvnSnatRuleQueue.ShutDown)
	now := metav1.Now()
	fin := []string{util.KubeOVNControllerFinalizer}
	assertEnqueueAddRoutingWithFinalizer(t, c.addOvnSnatRuleQueue, c.updateOvnSnatRuleQueue, c.enqueueAddOvnSnatRule,
		&kubeovnv1.OvnSnatRule{Name: "live-snat"},
		&kubeovnv1.OvnSnatRule{Name: "terminating-snat", DeletionTimestamp: &now, Finalizers: fin},
		&kubeovnv1.OvnSnatRule{Name: "terminating-snat-no-finalizer", DeletionTimestamp: &now},
	)
}

// TestEnqueueAddOvnEipRequeuesRouterLBRules verifies that enqueueAddOvnEip re-queues an EIP's
// RouterLBRules even when the EIP is terminating (the side effect must run before the terminating
// route returns, so LB rules react to the EIP going away).
func TestEnqueueAddOvnEipRequeuesRouterLBRules(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		RouterLBRules: []*kubeovnv1.RouterLBRule{{
			Name: "rlr1",
			Spec: kubeovnv1.RouterLBRuleSpec{OvnEip: "eip1"},
		}},
	})
	require.NoError(t, err)
	c := fc.fakeController
	c.config.EnableLb = true
	c.addRouterLBRuleQueue = newTypedRateLimitingQueue[string]("AddRouterLBRule", nil)
	c.addOvnEipQueue = newTypedRateLimitingQueue[string]("AddOvnEip", nil)
	c.updateOvnEipQueue = newTypedRateLimitingQueue[string]("UpdateOvnEip", nil)
	t.Cleanup(c.addRouterLBRuleQueue.ShutDown)
	t.Cleanup(c.addOvnEipQueue.ShutDown)
	t.Cleanup(c.updateOvnEipQueue.ShutDown)

	now := metav1.Now()
	c.enqueueAddOvnEip(&kubeovnv1.OvnEip{
		Name: "eip1", DeletionTimestamp: &now, Finalizers: []string{util.KubeOVNControllerFinalizer},
	})

	require.Equal(t, 1, c.updateOvnEipQueue.Len(), "terminating eip with finalizer goes to the update queue")
	require.Equal(t, 0, c.addOvnEipQueue.Len(), "terminating eip must not go to the add queue")
	require.Equal(t, 1, c.addRouterLBRuleQueue.Len(), "router lb rules must be re-queued even for a terminating eip")
}
