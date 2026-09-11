package controller

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8stesting "k8s.io/client-go/testing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestHandleAddOvnFip_RecordsNatStatusBeforeReady(t *testing.T) {
	t.Parallel()

	eip := &kubeovnv1.OvnEip{
		Name: "test-eip",
		Spec: kubeovnv1.OvnEipSpec{ExternalSubnet: "pubnet"},
	}
	eip.Status.V4Ip = "10.0.0.10"
	fip := &kubeovnv1.OvnFip{
		Name: "test-fip",
		Spec: kubeovnv1.OvnFipSpec{
			OvnEip: eip.Name,
			Vpc:    "test-vpc",
			V4Ip:   "10.0.1.5",
		},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		OvnEips:     []*kubeovnv1.OvnEip{eip},
		OvnFipRules: []*kubeovnv1.OvnFip{fip},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController

	fc.mockOvnClient.EXPECT().
		AddNat(fip.Spec.Vpc, ovnnb.NATTypeDNATAndSNAT, eip.Status.V4Ip, fip.Spec.V4Ip, gomock.Any(), fip.Spec.IPName, gomock.Any()).
		Return(nil)

	kubeovnClient, ok := ctrl.config.KubeOvnClient.(*kubeovnfake.Clientset)
	require.True(t, ok)
	kubeovnClient.PrependReactor("patch", "ovn-fips", func(action k8stesting.Action) (bool, runtime.Object, error) {
		patchAction, ok := action.(k8stesting.PatchAction)
		if ok && patchAction.GetPatchType() == types.JSONPatchType &&
			strings.Contains(string(patchAction.GetPatch()), "/metadata/annotations") {
			return true, nil, errors.New("injected failure after nat status recorded")
		}
		return false, nil, nil
	})

	err = ctrl.handleAddOvnFip(fip.Name)
	require.Error(t, err)

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().OvnFips().Get(t.Context(), fip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
	require.Equal(t, fip.Spec.Vpc, got.Status.Vpc)
	require.Equal(t, eip.Status.V4Ip, got.Status.V4Eip)
	require.Equal(t, fip.Spec.V4Ip, got.Status.V4Ip)
	require.False(t, got.Status.Ready)
}

func TestHandleAddOvnFip_RecordsV6NatStatus(t *testing.T) {
	t.Parallel()

	eip := &kubeovnv1.OvnEip{
		Name: "test-eip",
		Spec: kubeovnv1.OvnEipSpec{ExternalSubnet: "pubnet"},
	}
	eip.Status.V6Ip = "2001:db8::10"
	fip := &kubeovnv1.OvnFip{
		Name: "test-fip",
		Spec: kubeovnv1.OvnFipSpec{
			OvnEip: eip.Name,
			Vpc:    "test-vpc",
			V6Ip:   "2001:db8::5",
		},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		OvnEips:     []*kubeovnv1.OvnEip{eip},
		OvnFipRules: []*kubeovnv1.OvnFip{fip},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController

	fc.mockOvnClient.EXPECT().
		AddNat(fip.Spec.Vpc, ovnnb.NATTypeDNATAndSNAT, eip.Status.V6Ip, fip.Spec.V6Ip, gomock.Any(), fip.Spec.IPName, gomock.Any()).
		Return(nil)

	require.NoError(t, ctrl.handleAddOvnFip(fip.Name))

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().OvnFips().Get(t.Context(), fip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, eip.Status.V6Ip, got.Status.V6Eip)
	require.Equal(t, fip.Spec.V6Ip, got.Status.V6Ip)
}

func TestHandleUpdateOvnFip_DeleteRemovesNatAndFinalizer(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	fip := &kubeovnv1.OvnFip{
		Name:              "test-fip",
		Finalizers:        []string{util.KubeOVNControllerFinalizer},
		DeletionTimestamp: &now,
		Spec:              kubeovnv1.OvnFipSpec{OvnEip: "test-eip", Vpc: "test-vpc", V4Ip: "10.0.1.5"},
	}
	fip.Status = kubeovnv1.OvnFipStatus{
		Vpc:   "test-vpc",
		V4Eip: "10.0.0.10",
		V4Ip:  "10.0.1.5",
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{OvnFipRules: []*kubeovnv1.OvnFip{fip}})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.resetOvnEipQueue = newTypedRateLimitingQueue[string]("ResetOvnEip", nil)
	t.Cleanup(ctrl.resetOvnEipQueue.ShutDown)
	ctrl.updateOvnEipQueue = newTypedRateLimitingQueue[string]("UpdateOvnEip", nil)
	t.Cleanup(ctrl.updateOvnEipQueue.ShutDown)

	fc.mockOvnClient.EXPECT().
		NatExists(fip.Status.Vpc, ovnnb.NATTypeDNATAndSNAT, fip.Status.V4Eip, fip.Status.V4Ip).
		Return(true, nil)
	fc.mockOvnClient.EXPECT().
		DeleteNat(fip.Status.Vpc, ovnnb.NATTypeDNATAndSNAT, fip.Status.V4Eip, fip.Status.V4Ip).
		Return(nil)

	require.NoError(t, ctrl.handleUpdateOvnFip(fip.Name))

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().OvnFips().Get(t.Context(), fip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotContains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
}

func TestHandleUpdateOvnFip_DeleteToleratesMissingNat(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	fip := &kubeovnv1.OvnFip{
		Name:              "test-fip",
		Finalizers:        []string{util.KubeOVNControllerFinalizer},
		DeletionTimestamp: &now,
		Spec:              kubeovnv1.OvnFipSpec{OvnEip: "test-eip", Vpc: "test-vpc", V4Ip: "10.0.1.5"},
	}
	fip.Status = kubeovnv1.OvnFipStatus{
		Vpc:   "test-vpc",
		V4Eip: "10.0.0.10",
		V4Ip:  "10.0.1.5",
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{OvnFipRules: []*kubeovnv1.OvnFip{fip}})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.resetOvnEipQueue = newTypedRateLimitingQueue[string]("ResetOvnEip", nil)
	t.Cleanup(ctrl.resetOvnEipQueue.ShutDown)
	ctrl.updateOvnEipQueue = newTypedRateLimitingQueue[string]("UpdateOvnEip", nil)
	t.Cleanup(ctrl.updateOvnEipQueue.ShutDown)

	fc.mockOvnClient.EXPECT().
		NatExists(fip.Status.Vpc, ovnnb.NATTypeDNATAndSNAT, fip.Status.V4Eip, fip.Status.V4Ip).
		Return(false, nil)

	require.NoError(t, ctrl.handleUpdateOvnFip(fip.Name))

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().OvnFips().Get(t.Context(), fip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotContains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
}
