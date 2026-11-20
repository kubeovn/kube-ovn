package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8stesting "k8s.io/client-go/testing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// fakeGw returns a minimal VpcNatGateway CRD object for use in tests.
func fakeGw(name string) *kubeovnv1.VpcNatGateway {
	return &kubeovnv1.VpcNatGateway{
		Name: name,
		Spec: kubeovnv1.VpcNatGatewaySpec{},
	}
}

func TestSyncNatUIDLabels(t *testing.T) {
	t.Parallel()
	qos := &kubeovnv1.QoSPolicy{Name: "qos", UID: "qos-uid"}
	eip := &kubeovnv1.IptablesEIP{
		Name: "eip", UID: "eip-uid",
		Spec: kubeovnv1.IptablesEIPSpec{QoSPolicy: "qos"},
	}
	fip := &kubeovnv1.IptablesFIPRule{
		Name: "fip",
		Spec: kubeovnv1.IptablesFIPRuleSpec{EIP: "eip"},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		QoSPolicies:  []*kubeovnv1.QoSPolicy{qos},
		IptablesEips: []*kubeovnv1.IptablesEIP{eip},
	})
	require.NoError(t, err)
	_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Create(context.Background(), fip, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, fc.fakeController.syncNatUIDLabels())

	gotEip, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Get(context.Background(), "eip", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "qos-uid", gotEip.Labels[util.QoSPolicyUIDLabel])
	gotFip, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Get(context.Background(), "fip", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "eip-uid", gotFip.Labels[util.EipUIDLabel])
}

// runningNatGwPod returns a running instance of the gateway, which is what makes the data plane
// paths actually reach the Pod instead of being skipped as "no data plane left".
func runningNatGwPod(gwName string) *corev1.Pod {
	return &corev1.Pod{
		Name:      util.GenNatGwName(gwName) + "-0",
		Namespace: metav1.NamespaceSystem,
		Labels:    map[string]string{"app": util.GenNatGwName(gwName), util.VpcNatGatewayLabel: "true"},
		Status:    corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func TestFipRebindPreservesEipClaim(t *testing.T) {
	oldEnabled := vpcNatEnabled
	vpcNatEnabled = "true"
	t.Cleanup(func() { vpcNatEnabled = oldEnabled })

	newFip := func(statusGateway string) *kubeovnv1.IptablesFIPRule {
		return &kubeovnv1.IptablesFIPRule{
			Name: "fip", Labels: map[string]string{util.EipUIDLabel: "old-uid"},
			Spec:   kubeovnv1.IptablesFIPRuleSpec{EIP: "new-eip", InternalIP: "10.0.0.1"},
			Status: kubeovnv1.IptablesFIPRuleStatus{V4ip: "1.1.1.1", NatGwDp: statusGateway, InternalIP: "10.0.0.1", Ready: true},
		}
	}
	setup := func(t *testing.T, fip *kubeovnv1.IptablesFIPRule) *Controller {
		t.Helper()
		eip := &kubeovnv1.IptablesEIP{
			Name: "new-eip", UID: "new-uid",
			Spec:   kubeovnv1.IptablesEIPSpec{V4ip: "2.2.2.2", NatGwDp: "gw"},
			Status: kubeovnv1.IptablesEIPStatus{IP: "2.2.2.2"},
		}
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("gw")},
			IptablesEips:   []*kubeovnv1.IptablesEIP{eip},
			IptablesFips:   []*kubeovnv1.IptablesFIPRule{fip},
		})
		require.NoError(t, err)
		return fc.fakeController
	}
	getClaim := func(t *testing.T, c *Controller) string {
		t.Helper()
		fip, err := c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Get(context.Background(), "fip", metav1.GetOptions{})
		require.NoError(t, err)
		return fip.Labels[util.EipUIDLabel]
	}

	// The gateway of the first fixture still exists, so its rules must be removed before the claim
	// may move: the cleanup fails for want of a running instance and the old claim stays. The
	// second fixture's gateway is gone, so there is nothing to clean up and the claim moves to the
	// generation whose rules are about to be created.
	t.Run("old claim remains when old rule cleanup fails", func(t *testing.T) {
		c := setup(t, newFip("gw"))
		require.Error(t, c.handleUpdateIptablesFip("fip"))
		require.Equal(t, "old-uid", getClaim(t, c))
	})
	t.Run("new claim is written before new rule creation", func(t *testing.T) {
		c := setup(t, newFip("retired-gw"))
		require.Error(t, c.handleUpdateIptablesFip("fip"))
		require.Equal(t, "new-uid", getClaim(t, c))
	})
}

func TestSyncNatUIDLabelsKeepsTerminatingBindings(t *testing.T) {
	now := metav1.Now()
	qos := &kubeovnv1.QoSPolicy{Name: "qos", UID: "qos-uid", DeletionTimestamp: &now}
	eip := &kubeovnv1.IptablesEIP{
		Name: "eip", UID: "eip-uid", DeletionTimestamp: &now,
		Labels: map[string]string{util.QoSLabel: "qos"},
		Spec:   kubeovnv1.IptablesEIPSpec{QoSPolicy: "qos"},
	}
	fip := &kubeovnv1.IptablesFIPRule{
		Name: "fip", Labels: map[string]string{util.EipV4IpLabel: "10.0.0.2"},
		Spec: kubeovnv1.IptablesFIPRuleSpec{EIP: "eip"},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		QoSPolicies:  []*kubeovnv1.QoSPolicy{qos},
		IptablesEips: []*kubeovnv1.IptablesEIP{eip},
	})
	require.NoError(t, err)
	_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Create(context.Background(), fip, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, fc.fakeController.syncNatUIDLabels())

	gotEip, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Get(context.Background(), "eip", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "qos-uid", gotEip.Labels[util.QoSPolicyUIDLabel])
	gotFip, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Get(context.Background(), "fip", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "eip-uid", gotFip.Labels[util.EipUIDLabel])
}

func TestSyncNatUIDLabelsCorrectsStaleAndClearsDroppedQoS(t *testing.T) {
	qos := &kubeovnv1.QoSPolicy{Name: "qos", UID: "current-uid"}
	stale := &kubeovnv1.IptablesEIP{
		Name: "stale", Labels: map[string]string{util.QoSLabel: "qos", util.QoSPolicyUIDLabel: "old-uid"},
		Spec: kubeovnv1.IptablesEIPSpec{QoSPolicy: "qos"},
	}
	cleared := &kubeovnv1.IptablesEIP{
		Name: "cleared", Labels: map[string]string{util.QoSLabel: "qos", util.QoSPolicyUIDLabel: "old-uid"},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{QoSPolicies: []*kubeovnv1.QoSPolicy{qos}, IptablesEips: []*kubeovnv1.IptablesEIP{stale, cleared}})
	require.NoError(t, err)
	require.NoError(t, fc.fakeController.syncNatUIDLabels())

	got, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Get(context.Background(), "stale", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "current-uid", got.Labels[util.QoSPolicyUIDLabel])
	got, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Get(context.Background(), "cleared", metav1.GetOptions{})
	require.NoError(t, err)
	require.NotContains(t, got.Labels, util.QoSPolicyUIDLabel)
	require.NotContains(t, got.Labels, util.QoSLabel)
}

func TestQoSPolicyUID(t *testing.T) {
	t.Parallel()
	qos := &kubeovnv1.QoSPolicy{Name: "qos", UID: "qos-uid"}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{QoSPolicies: []*kubeovnv1.QoSPolicy{qos}})
	require.NoError(t, err)

	uid, err := fc.fakeController.qosPolicyUID(qos.Name)
	require.NoError(t, err)
	require.Equal(t, string(qos.UID), uid)

	uid, err = fc.fakeController.qosPolicyUID("")
	require.NoError(t, err)
	require.Empty(t, uid)

	_, err = fc.fakeController.qosPolicyUID("missing")
	require.Error(t, err)
}

func TestIptablesEipDeleteHonorsUIDClaim(t *testing.T) {
	oldEnabled := vpcNatEnabled
	vpcNatEnabled = "false"
	t.Cleanup(func() { vpcNatEnabled = oldEnabled })
	newEip := func() *kubeovnv1.IptablesEIP {
		now := metav1.Now()
		return &kubeovnv1.IptablesEIP{
			Name: "eip-delete", UID: "eip-delete-uid", DeletionTimestamp: &now,
			Finalizers: []string{util.KubeOVNControllerFinalizer},
		}
	}

	t.Run("matching claim blocks finalizer removal", func(t *testing.T) {
		eip := newEip()
		fip := &kubeovnv1.IptablesFIPRule{
			Name: "fip", Labels: map[string]string{util.EipUIDLabel: string(eip.UID)},
		}
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			IptablesEips: []*kubeovnv1.IptablesEIP{eip}, IptablesFips: []*kubeovnv1.IptablesFIPRule{fip},
		})
		require.NoError(t, err)
		require.NoError(t, fc.fakeController.handleUpdateIptablesEip(eip.Name))
		got, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Get(context.Background(), eip.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.Contains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
	})

	t.Run("different UID does not block finalizer removal", func(t *testing.T) {
		eip := newEip()
		fip := &kubeovnv1.IptablesFIPRule{
			Name: "fip", Labels: map[string]string{util.EipUIDLabel: "other-uid", util.EipV4IpLabel: "same-address"},
		}
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			IptablesEips: []*kubeovnv1.IptablesEIP{eip}, IptablesFips: []*kubeovnv1.IptablesFIPRule{fip},
		})
		require.NoError(t, err)
		require.NoError(t, fc.fakeController.handleUpdateIptablesEip(eip.Name))
		got, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Get(context.Background(), eip.Name, metav1.GetOptions{})
		if err == nil {
			require.NotContains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
		} else {
			require.True(t, k8serrors.IsNotFound(err))
		}
	})
}

func TestGetIptablesEipNatUsesUID(t *testing.T) {
	eip := &kubeovnv1.IptablesEIP{Name: "eip", UID: "eip-uid"}
	makeLabels := func(uid string) map[string]string {
		return map[string]string{util.EipUIDLabel: uid}
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		IptablesEips: []*kubeovnv1.IptablesEIP{eip},
		IptablesFips: []*kubeovnv1.IptablesFIPRule{
			{Name: "same", Labels: makeLabels("eip-uid")},
			{Name: "other", Labels: makeLabels("other-uid")},
		},
		IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{
			{Name: "dnat", Labels: makeLabels("eip-uid")},
		},
	})
	require.NoError(t, err)
	got, err := fc.fakeController.getIptablesEipNat(eip)
	require.NoError(t, err)
	require.Equal(t, util.DnatUsingEip+","+util.FipUsingEip, got)
}

// TestEipQoSClaimIsWrittenBeforeApply pins the ordering of the claim and the data plane: the
// generation an eip is switching to must be claimed before its rules can be created, and the
// generation it is leaving must stay claimed until its rules are gone.
func TestEipQoSClaimIsWrittenBeforeApply(t *testing.T) {
	oldEnabled := vpcNatEnabled
	vpcNatEnabled = "true"
	t.Cleanup(func() { vpcNatEnabled = oldEnabled })

	// A rule is required so that both applying and deleting reach the pod and fail without one.
	rules := kubeovnv1.QoSPolicyBandwidthLimitRules{{Direction: kubeovnv1.QoSDirectionIngress}}
	policy := func(name, uid string) *kubeovnv1.QoSPolicy {
		return &kubeovnv1.QoSPolicy{
			Name: name, UID: types.UID(uid),
			Spec: kubeovnv1.QoSPolicySpec{
				BindingType: kubeovnv1.QoSBindingTypeEIP, BandwidthLimitRules: rules,
			},
			Status: kubeovnv1.QoSPolicyStatus{
				BindingType: kubeovnv1.QoSBindingTypeEIP, BandwidthLimitRules: rules,
			},
		}
	}
	eip := func(appliedQoS string) *kubeovnv1.IptablesEIP {
		return &kubeovnv1.IptablesEIP{
			Name: "eip", UID: "eip-uid",
			Labels: map[string]string{
				util.QoSLabel:          "old-qos",
				util.QoSPolicyUIDLabel: "old-qos-uid",
			},
			Spec: kubeovnv1.IptablesEIPSpec{
				V4ip: "2.2.2.2", NatGwDp: "gw", QoSPolicy: "new-qos", ExternalSubnet: "ext-net",
			},
			Status: kubeovnv1.IptablesEIPStatus{IP: "2.2.2.2", QoSPolicy: appliedQoS, Ready: true},
		}
	}
	setup := func(t *testing.T, cachedEip *kubeovnv1.IptablesEIP) *Controller {
		t.Helper()
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{{
				Name: "ext-net",
				Spec: kubeovnv1.SubnetSpec{CIDRBlock: "2.2.2.0/24"},
			}},
			VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("gw")},
			QoSPolicies:    []*kubeovnv1.QoSPolicy{policy("new-qos", "new-qos-uid"), policy("old-qos", "old-qos-uid")},
			IptablesEips:   []*kubeovnv1.IptablesEIP{cachedEip},
		})
		require.NoError(t, err)
		return fc.fakeController
	}
	claim := func(t *testing.T, c *Controller) string {
		t.Helper()
		got, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Get(context.Background(), "eip", metav1.GetOptions{})
		require.NoError(t, err)
		return got.Labels[util.QoSPolicyUIDLabel]
	}

	t.Run("new generation is claimed before its rules are applied", func(t *testing.T) {
		c := setup(t, eip(""))
		require.Error(t, c.handleUpdateIptablesEip("eip"))
		require.Equal(t, "new-qos-uid", claim(t, c))
	})

	t.Run("previous generation keeps its claim while its rules still exist", func(t *testing.T) {
		c := setup(t, eip("old-qos"))
		require.Error(t, c.handleUpdateIptablesEip("eip"))
		require.Equal(t, "old-qos-uid", claim(t, c))
	})
}

// TestUpdateEipAllocationPublishesQoSClaim verifies that the allocation records the policy generation.
func TestUpdateEipAllocationPublishesQoSClaim(t *testing.T) {
	qos := &kubeovnv1.QoSPolicy{Name: "qos", UID: "qos-uid"}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		QoSPolicies: []*kubeovnv1.QoSPolicy{qos},
		IptablesEips: []*kubeovnv1.IptablesEIP{{
			Name: "eip", UID: "eip-uid",
			Spec: kubeovnv1.IptablesEIPSpec{NatGwDp: "gw", QoSPolicy: "qos", ExternalSubnet: "ext-net"},
		}},
		Subnets: []*kubeovnv1.Subnet{{
			Name: "ext-net",
			Spec: kubeovnv1.SubnetSpec{CIDRBlock: "2.2.2.0/24"},
		}},
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("gw")},
	})
	require.NoError(t, err)

	require.NoError(t, fc.fakeController.updateEipAllocation(mustGetIptablesEip(t, fc.fakeController), "2.2.2.2", "", "", "ext-net", ""))

	got, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Get(
		context.Background(), "eip", metav1.GetOptions{},
	)
	require.NoError(t, err)
	require.Equal(t, "qos-uid", got.Labels[util.QoSPolicyUIDLabel])
	require.Equal(t, "qos", got.Labels[util.QoSLabel])
	require.Equal(t, "2.2.2.2", got.Status.IP)
	require.True(t, got.Status.Ready)
	require.Equal(t, "qos", got.Status.QoSPolicy)
}

func TestUpdateEipAllocationPreservesLiveReplayStatus(t *testing.T) {
	oldStatus := kubeovnv1.IptablesEIPStatus{
		IP: "2.2.2.2", Ready: true, Nat: "old-nat", Redo: "old-replay",
	}
	eip := &kubeovnv1.IptablesEIP{
		Name: "eip", UID: "eip-uid",
		Spec:   kubeovnv1.IptablesEIPSpec{V4ip: "2.2.2.2", NatGwDp: "gw", ExternalSubnet: "ext-net"},
		Status: oldStatus,
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		IptablesEips:   []*kubeovnv1.IptablesEIP{eip},
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("gw")},
	})
	require.NoError(t, err)
	c := fc.fakeController

	client := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs()
	live, err := client.Get(context.Background(), "eip", metav1.GetOptions{})
	require.NoError(t, err)
	live.Status = kubeovnv1.IptablesEIPStatus{
		IP: "2.2.2.2", Ready: true, Nat: "new-nat", Redo: "new-replay",
	}
	_, err = client.UpdateStatus(context.Background(), live, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		cached, getErr := c.iptablesEipsLister.Get("eip")
		return getErr == nil && cached.Status.Redo == "new-replay"
	}, time.Second, time.Millisecond)

	// Reintroduce the informer lag that triggered the bug. The API object retains the new
	// replay status while the lister returns the old one.
	stale := eip.DeepCopy()
	stale.Status = oldStatus
	require.NoError(t, fc.fakeInformers.iptablesEipInformer.Informer().GetIndexer().Update(stale))
	require.Equal(t, "old-replay", mustGetIptablesEip(t, c).Status.Redo)

	require.NoError(t, c.updateEipAllocation(mustGetIptablesEip(t, c), "2.2.2.2", "", "", "ext-net", ""))

	got, err := client.Get(context.Background(), "eip", metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, got.Status.Ready)
	require.Equal(t, "new-nat", got.Status.Nat)
	require.Equal(t, "new-replay", got.Status.Redo)
}

func TestUpdateEipAllocationPreservesPendingReplay(t *testing.T) {
	eip := &kubeovnv1.IptablesEIP{
		Name: "eip", UID: "eip-uid",
		Spec: kubeovnv1.IptablesEIPSpec{V4ip: "2.2.2.2", NatGwDp: "gw", ExternalSubnet: "ext-net"},
		Status: kubeovnv1.IptablesEIPStatus{
			IP: "2.2.2.2", Nat: "new-nat", Redo: "new-replay",
		},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		IptablesEips:   []*kubeovnv1.IptablesEIP{eip},
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("gw")},
	})
	require.NoError(t, err)

	client := fc.fakeController.config.KubeOvnClient.KubeovnV1().IptablesEIPs()
	gotBefore, err := client.Get(context.Background(), eip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, gotBefore.Status.Ready)

	require.NoError(t, fc.fakeController.updateEipAllocation(
		eip, "2.2.2.2", "", "", "ext-net", "",
	))

	got, err := client.Get(context.Background(), eip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, got.Status.Ready)
	require.Equal(t, "new-replay", got.Status.Redo)
	require.Equal(t, "new-nat", got.Status.Nat)
}

func mustGetIptablesEip(t *testing.T, c *Controller) *kubeovnv1.IptablesEIP {
	t.Helper()
	eip, err := c.iptablesEipsLister.Get("eip")
	require.NoError(t, err)
	return eip
}

// TestSyncNatUIDLabelsIsIdempotentAndBestEffort pins the migration contract: objects whose
// claim is already correct are left untouched, and an object that cannot be migrated is
// reported without stopping the migration of the others.
func TestSyncNatUIDLabelsIsIdempotentAndBestEffort(t *testing.T) {
	qos := &kubeovnv1.QoSPolicy{Name: "qos", UID: "qos-uid"}
	upToDate := &kubeovnv1.IptablesEIP{
		Name: "up-to-date", UID: "eip-1",
		Labels: map[string]string{util.QoSLabel: "qos", util.QoSPolicyUIDLabel: "qos-uid"},
		Spec:   kubeovnv1.IptablesEIPSpec{QoSPolicy: "qos"},
	}
	toBeMigrated := &kubeovnv1.IptablesEIP{
		Name: "to-migrate", UID: "eip-2",
		Spec: kubeovnv1.IptablesEIPSpec{QoSPolicy: "qos"},
	}
	gateway := &kubeovnv1.VpcNatGateway{
		Name: "gw",
		Spec: kubeovnv1.VpcNatGatewaySpec{QoSPolicy: "qos"},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		QoSPolicies:    []*kubeovnv1.QoSPolicy{qos},
		IptablesEips:   []*kubeovnv1.IptablesEIP{upToDate, toBeMigrated},
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gateway},
	})
	require.NoError(t, err)

	client := fc.fakeController.config.KubeOvnClient.(*kubeovnfake.Clientset)
	client.PrependReactor("update", "vpc-nat-gateways", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("admission webhook rejected the update")
	})

	err = fc.fakeController.syncNatUIDLabels()
	require.ErrorContains(t, err, "gw")

	got, err := client.KubeovnV1().IptablesEIPs().Get(context.Background(), "to-migrate", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "qos-uid", got.Labels[util.QoSPolicyUIDLabel])

	// The eip that already carried the claim must not be written again.
	updates := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" && action.GetResource().Resource == "iptables-eips" {
			updates++
		}
	}
	require.Equal(t, 1, updates)
}

func TestNatGwNoDataPlane(t *testing.T) {
	t.Parallel()

	t.Run("a running instance holds a data plane", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("test-gw")},
			Pods:           []*corev1.Pod{runningNatGwPod("test-gw")},
		})
		require.NoError(t, err)
		gone, err := fc.fakeController.natGwNoDataPlane("test-gw")
		require.NoError(t, err)
		require.False(t, gone)
	})

	// The rules live in the gateway container's writable layer, so cleanup must not wait for an
	// instance that is not there: a replacement starts empty and is programmed from live CRs.
	t.Run("a gateway without a running instance holds none", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("test-gw")},
		})
		require.NoError(t, err)
		gone, err := fc.fakeController.natGwNoDataPlane("test-gw")
		require.NoError(t, err)
		require.True(t, gone)
	})

	t.Run("a terminating gateway holds none", func(t *testing.T) {
		gw := fakeGw("test-gw")
		now := metav1.Now()
		gw.DeletionTimestamp = &now
		gw.Finalizers = []string{util.KubeOVNControllerFinalizer}
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
			Pods:           []*corev1.Pod{runningNatGwPod("test-gw")},
		})
		require.NoError(t, err)
		gone, err := fc.fakeController.natGwNoDataPlane("test-gw")
		require.NoError(t, err)
		require.True(t, gone)
	})

	t.Run("a missing gateway holds none", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, nil)
		require.NoError(t, err)
		gone, err := fc.fakeController.natGwNoDataPlane("missing-gw")
		require.NoError(t, err)
		require.True(t, gone)
	})
}

// TestDeleteEipInPod_NatGwGone verifies that deleteEipInPod returns nil (skips
// cleanup) when the VpcNatGateway CRD no longer exists, allowing the EIP to
// be finalized without an infinite retry loop.
func TestDeleteEipInPod_NatGwGone(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, nil) // no VpcNatGateway
	require.NoError(t, err)
	err = fc.fakeController.deleteEipInPod("missing-gw", "10.0.0.1/24", "kube-system")
	require.NoError(t, err, "should skip cleanup when gateway CRD is gone")
}

// TestDeleteEipInPod_NatGwNoRunningInstance verifies that deleteEipInPod retries while the gateway
// exists but has no running instance: the instance that replaces it must not come back with the
// rule still programmed.
func TestDeleteEipInPod_NatGwNoRunningInstance(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("test-gw")},
	})
	require.NoError(t, err)
	err = fc.fakeController.deleteEipInPod("test-gw", "10.0.0.1/24", "kube-system")
	require.Error(t, err, "should retry while the gateway exists but has no running instance")
}

// TestDelEipQoSInPod_NatGwGone verifies cleanup is skipped when gateway is gone.
func TestDelEipQoSInPod_NatGwGone(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	err = fc.fakeController.delEipQoSInPod("missing-gw", "10.0.0.1", "kube-system", kubeovnv1.QoSDirectionIngress)
	require.NoError(t, err, "should skip cleanup when gateway CRD is gone")
}

// TestDelEipQoSInPod_NatGwNoRunningInstance verifies that the cleanup is retried while the gateway
// exists but has no running instance.
func TestDelEipQoSInPod_NatGwNoRunningInstance(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("test-gw")},
	})
	require.NoError(t, err)
	err = fc.fakeController.delEipQoSInPod("test-gw", "10.0.0.1", "kube-system", kubeovnv1.QoSDirectionEgress)
	require.Error(t, err, "should retry while the gateway exists but has no running instance")
}

// TestEnqueueAddIptablesEip verifies that on the add path a terminating EIP is routed to the
// update queue (which runs deletion cleanup) instead of the add queue. This is what lets a
// stuck-terminating EIP be finalized after a controller restart, where the informer re-lists
// existing objects and fires only AddFunc.
func TestEnqueueAddIptablesEip(t *testing.T) {
	t.Parallel()
	c := &Controller{
		addIptablesEipQueue:    newTypedRateLimitingQueue[string]("AddIptablesEip", nil),
		updateIptablesEipQueue: newTypedRateLimitingQueue[string]("UpdateIptablesEip", nil),
	}
	t.Cleanup(c.addIptablesEipQueue.ShutDown)
	t.Cleanup(c.updateIptablesEipQueue.ShutDown)
	now := metav1.Now()
	assertEnqueueAddRouting(t, c.addIptablesEipQueue, c.updateIptablesEipQueue, c.enqueueAddIptablesEip,
		&kubeovnv1.IptablesEIP{Name: "live-eip"},
		&kubeovnv1.IptablesEIP{Name: "terminating-eip", DeletionTimestamp: &now},
	)

	// Informer relist uses AddFunc after a controller restart. Resume a QoS update
	// that had not yet published its applied credential instead of taking the add
	// handler's allocated-EIP fast path.
	c.enqueueAddIptablesEip(&kubeovnv1.IptablesEIP{
		Name: "pending-qos-eip",
		Spec: kubeovnv1.IptablesEIPSpec{QoSPolicy: "new-qos"},
		Status: kubeovnv1.IptablesEIPStatus{
			Ready: true, IP: "192.0.2.10", QoSPolicy: "old-qos",
		},
	})
	require.Equal(t, 1, c.addIptablesEipQueue.Len(), "allocated EIP must not re-enter add")
	require.Equal(t, 2, c.updateIptablesEipQueue.Len(), "pending QoS credential must resume update")
}

func TestEnqueueIptablesEipReplayWhenAllocated(t *testing.T) {
	t.Parallel()
	eip := &kubeovnv1.IptablesEIP{
		Name: "eip", Status: kubeovnv1.IptablesEIPStatus{Redo: "pending-replay"},
	}
	clientset := kubeovnfake.NewSimpleClientset()
	client := clientset.KubeovnV1().IptablesEIPs()
	_, err := client.Create(t.Context(), eip, metav1.CreateOptions{})
	require.NoError(t, err)
	c := &Controller{
		config:                 &Configuration{KubeOvnClient: clientset},
		addIptablesEipQueue:    newTypedRateLimitingQueue[string]("AddIptablesEip", nil),
		updateIptablesEipQueue: newTypedRateLimitingQueue[string]("UpdateIptablesEip", nil),
	}
	t.Cleanup(c.addIptablesEipQueue.ShutDown)
	t.Cleanup(c.updateIptablesEipQueue.ShutDown)

	require.NoError(t, c.recordEipAllocation(eip, "192.0.2.10", ""))
	allocated, err := client.Get(t.Context(), eip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "192.0.2.10", allocated.Status.IP)
	require.Equal(t, eip.Status.Redo, allocated.Status.Redo)
	require.False(t, allocated.Status.Ready)

	for _, event := range []struct {
		name    string
		enqueue func()
	}{
		{name: "first status IP", enqueue: func() { c.enqueueUpdateIptablesEip(eip, allocated) }},
		{name: "informer relist", enqueue: func() { c.enqueueAddIptablesEip(allocated) }},
	} {
		t.Run(event.name, func(t *testing.T) {
			event.enqueue()
			require.Zero(t, c.addIptablesEipQueue.Len())
			require.Equal(t, 1, c.updateIptablesEipQueue.Len())
			key, shutdown := c.updateIptablesEipQueue.Get()
			require.False(t, shutdown)
			require.Equal(t, eip.Name, key)
			c.updateIptablesEipQueue.Done(key)
		})
	}

	c.enqueueUpdateIptablesEip(allocated, allocated.DeepCopy())
	require.Zero(t, c.updateIptablesEipQueue.Len(), "already published IP leaves the queue alone")
}

// TestTerminatingEipKeepsConvergingWhileInUse pins that a terminating EIP does not depend on an
// external event to notice that the last NAT rule referencing it is gone: the rule controllers
// notify it through a delaying queue that keeps only the earliest deadline for an item, so a
// notification scheduled while the rule was being programmed can swallow the one scheduled when it
// was deleted. The EIP therefore re-queues itself while it is still in use.
func TestTerminatingEipKeepsConvergingWhileInUse(t *testing.T) {
	oldEnabled := vpcNatEnabled
	vpcNatEnabled = "true"
	t.Cleanup(func() { vpcNatEnabled = oldEnabled })

	now := metav1.Now()
	eip := &kubeovnv1.IptablesEIP{
		Name: "eip", UID: "eip-uid", DeletionTimestamp: &now, Finalizers: []string{util.KubeOVNControllerFinalizer},
		Labels: map[string]string{util.VpcNatGatewayNameLabel: "gw", util.EipV4IpLabel: "2.2.2.2"},
		Spec:   kubeovnv1.IptablesEIPSpec{V4ip: "2.2.2.2", NatGwDp: "gw", ExternalSubnet: "ext-net"},
		Status: kubeovnv1.IptablesEIPStatus{IP: "2.2.2.2", Ready: true},
	}
	// One record still references the EIP, which is what keeps it in use.
	record := &kubeovnv1.IptablesDnatRule{
		Name: "lb-web-abc",
		Labels: map[string]string{
			util.VpcNatGatewayNameLabel:  "gw",
			util.EipV4IpLabel:            "2.2.2.2",
			util.EipUIDLabel:             "eip-uid",
			util.NftableLbSvcNsLabel:     "default",
			util.NftableLbSvcNameLabel:   "web",
			util.NftableLbSvcRecordLabel: "true",
		},
		Spec: kubeovnv1.IptablesDnatRuleSpec{
			EIP: "eip", ClusterIP: "10.96.1.5", ExternalPort: "80", Protocol: "tcp",
			InternalIP: "10.0.7.2", InternalPort: "8080", Type: kubeovnv1.DnatRuleTypeShare,
		},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets:           []*kubeovnv1.Subnet{{Name: "ext-net", Spec: kubeovnv1.SubnetSpec{CIDRBlock: "2.2.2.0/24"}}},
		VpcNatGateways:    []*kubeovnv1.VpcNatGateway{fakeGw("gw")},
		IptablesEips:      []*kubeovnv1.IptablesEIP{eip},
		IptablesDnatRules: []*kubeovnv1.IptablesDnatRule{record},
	})
	require.NoError(t, err)
	c := fc.fakeController

	require.NoError(t, c.handleUpdateIptablesEip("eip"), "an eip in use is not an error, it is not done yet")
	require.Eventually(t, func() bool { return c.updateIptablesEipQueue.Len() == 1 },
		5*time.Second, 50*time.Millisecond, "the eip must come back to check whether the rule is gone")

	kept, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Get(context.Background(), "eip", metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, kept.Finalizers, util.KubeOVNControllerFinalizer,
		"the finalizer must be held while a rule still references the eip")
}

func TestEipAllocationRejectsChangedObject(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*kubeovnv1.IptablesEIP)
	}{
		{"changed spec", func(eip *kubeovnv1.IptablesEIP) { eip.Spec.V4ip = "192.0.2.20" }},
		{"replacement object", func(eip *kubeovnv1.IptablesEIP) { eip.UID = "replacement" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			expected := &kubeovnv1.IptablesEIP{
				Name: "eip", UID: "original",
				Spec: kubeovnv1.IptablesEIPSpec{V4ip: "192.0.2.10", NatGwDp: "gw"},
			}
			live := expected.DeepCopy()
			change.apply(live)
			clientset := kubeovnfake.NewSimpleClientset()
			client := clientset.KubeovnV1().IptablesEIPs()
			_, err := client.Create(t.Context(), live, metav1.CreateOptions{})
			require.NoError(t, err)
			c := &Controller{config: &Configuration{KubeOvnClient: clientset}}

			require.ErrorContains(t, c.updateEipAllocation(expected, "192.0.2.10", "", "", "ext-net", ""), "changed during allocation")
			require.ErrorContains(t, c.recordEipAllocation(expected, "192.0.2.10", ""), "changed before allocation status")
			got, err := client.Get(t.Context(), live.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, live.UID, got.UID)
			require.Equal(t, live.Spec, got.Spec)
			require.Equal(t, live.Status, got.Status)
		})
	}
}
