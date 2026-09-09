package controller

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8stesting "k8s.io/client-go/testing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestHandleAddOvnSnatRule_RecordsNatStatusBeforeReady(t *testing.T) {
	t.Parallel()

	eip := &kubeovnv1.OvnEip{
		Name: "test-eip",
		Spec: kubeovnv1.OvnEipSpec{ExternalSubnet: "pubnet"},
	}
	eip.Status.V4Ip = "10.0.0.10"
	snat := &kubeovnv1.OvnSnatRule{
		Name: "test-snat",
		Spec: kubeovnv1.OvnSnatRuleSpec{
			OvnEip:   eip.Name,
			Vpc:      "test-vpc",
			V4IpCidr: "10.0.1.0/24",
		},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		OvnEips:      []*kubeovnv1.OvnEip{eip},
		OvnSnatRules: []*kubeovnv1.OvnSnatRule{snat},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController

	fc.mockOvnClient.EXPECT().
		AddNat(snat.Spec.Vpc, ovnnb.NATTypeSNAT, eip.Status.V4Ip, snat.Spec.V4IpCidr, "", "", nil).
		Return(nil)

	kubeovnClient, ok := ctrl.config.KubeOvnClient.(*kubeovnfake.Clientset)
	require.True(t, ok)
	kubeovnClient.PrependReactor("patch", "ovn-snat-rules", func(action k8stesting.Action) (bool, runtime.Object, error) {
		patchAction, ok := action.(k8stesting.PatchAction)
		if ok && patchAction.GetPatchType() == types.JSONPatchType &&
			strings.Contains(string(patchAction.GetPatch()), "/metadata/annotations") {
			return true, nil, errors.New("injected failure after nat status recorded")
		}
		return false, nil, nil
	})

	err = ctrl.handleAddOvnSnatRule(snat.Name)
	require.Error(t, err)

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().OvnSnatRules().Get(t.Context(), snat.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
	require.Equal(t, snat.Spec.Vpc, got.Status.Vpc)
	require.Equal(t, eip.Status.V4Ip, got.Status.V4Eip)
	require.Equal(t, snat.Spec.V4IpCidr, got.Status.V4IpCidr)
	require.False(t, got.Status.Ready)
}

func TestHandleUpdateOvnSnatRule_DeleteRemovesNatAndFinalizer(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	snat := &kubeovnv1.OvnSnatRule{
		Name:              "test-snat",
		Finalizers:        []string{util.KubeOVNControllerFinalizer},
		DeletionTimestamp: &now,
		Spec:              kubeovnv1.OvnSnatRuleSpec{OvnEip: "test-eip", Vpc: "test-vpc", V4IpCidr: "10.0.1.0/24"},
	}
	snat.Status = kubeovnv1.OvnSnatRuleStatus{
		Vpc:      "test-vpc",
		V4Eip:    "10.0.0.10",
		V4IpCidr: "10.0.1.0/24",
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{OvnSnatRules: []*kubeovnv1.OvnSnatRule{snat}})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.resetOvnEipQueue = newTypedRateLimitingQueue[string]("ResetOvnEip", nil)
	t.Cleanup(ctrl.resetOvnEipQueue.ShutDown)
	ctrl.updateOvnEipQueue = newTypedRateLimitingQueue[string]("UpdateOvnEip", nil)
	t.Cleanup(ctrl.updateOvnEipQueue.ShutDown)

	fc.mockOvnClient.EXPECT().
		NatExists(snat.Status.Vpc, ovnnb.NATTypeSNAT, snat.Status.V4Eip, snat.Status.V4IpCidr).
		Return(true, nil)
	fc.mockOvnClient.EXPECT().
		DeleteNat(snat.Status.Vpc, ovnnb.NATTypeSNAT, snat.Status.V4Eip, snat.Status.V4IpCidr).
		Return(nil)

	require.NoError(t, ctrl.handleUpdateOvnSnatRule(snat.Name))

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().OvnSnatRules().Get(t.Context(), snat.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotContains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
}

func TestHandleUpdateOvnSnatRule_DeleteToleratesMissingNat(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	snat := &kubeovnv1.OvnSnatRule{
		Name:              "test-snat",
		Finalizers:        []string{util.KubeOVNControllerFinalizer},
		DeletionTimestamp: &now,
		Spec:              kubeovnv1.OvnSnatRuleSpec{OvnEip: "test-eip", Vpc: "test-vpc", V4IpCidr: "10.0.1.0/24"},
	}
	snat.Status = kubeovnv1.OvnSnatRuleStatus{
		Vpc:      "test-vpc",
		V4Eip:    "10.0.0.10",
		V4IpCidr: "10.0.1.0/24",
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{OvnSnatRules: []*kubeovnv1.OvnSnatRule{snat}})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.resetOvnEipQueue = newTypedRateLimitingQueue[string]("ResetOvnEip", nil)
	t.Cleanup(ctrl.resetOvnEipQueue.ShutDown)
	ctrl.updateOvnEipQueue = newTypedRateLimitingQueue[string]("UpdateOvnEip", nil)
	t.Cleanup(ctrl.updateOvnEipQueue.ShutDown)

	fc.mockOvnClient.EXPECT().
		NatExists(snat.Status.Vpc, ovnnb.NATTypeSNAT, snat.Status.V4Eip, snat.Status.V4IpCidr).
		Return(false, nil)

	require.NoError(t, ctrl.handleUpdateOvnSnatRule(snat.Name))

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().OvnSnatRules().Get(t.Context(), snat.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotContains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
}
