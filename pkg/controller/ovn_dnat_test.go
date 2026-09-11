package controller

import (
	"errors"
	"strings"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8stesting "k8s.io/client-go/testing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestHandleAddOvnDnatRule_RecordsNatStatusBeforeReady(t *testing.T) {
	t.Parallel()

	eip := &kubeovnv1.OvnEip{
		Name: "test-eip",
		Spec: kubeovnv1.OvnEipSpec{ExternalSubnet: "pubnet"},
	}
	eip.Status.V4Ip = "10.0.0.10"
	dnat := &kubeovnv1.OvnDnatRule{
		Name: "test-dnat",
		Spec: kubeovnv1.OvnDnatRuleSpec{
			OvnEip:       eip.Name,
			Vpc:          "test-vpc",
			V4Ip:         "10.0.1.5",
			Protocol:     "tcp",
			ExternalPort: "2222",
			InternalPort: "22",
		},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		OvnEips:      []*kubeovnv1.OvnEip{eip},
		OvnDnatRules: []*kubeovnv1.OvnDnatRule{dnat},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController

	fc.mockOvnClient.EXPECT().CreateLoadBalancer(dnat.Name, dnat.Spec.Protocol).Return(nil)
	fc.mockOvnClient.EXPECT().LoadBalancerAddVip(dnat.Name, eip.Status.V4Ip+":"+dnat.Spec.ExternalPort, dnat.Spec.V4Ip+":"+dnat.Spec.InternalPort).Return(nil)
	fc.mockOvnClient.EXPECT().LogicalRouterUpdateLoadBalancers(dnat.Spec.Vpc, ovsdb.MutateOperationInsert, dnat.Name).Return(nil)

	kubeovnClient, ok := ctrl.config.KubeOvnClient.(*kubeovnfake.Clientset)
	require.True(t, ok)
	kubeovnClient.PrependReactor("patch", "ovn-dnat-rules", func(action k8stesting.Action) (bool, runtime.Object, error) {
		patchAction, ok := action.(k8stesting.PatchAction)
		if ok && patchAction.GetPatchType() == types.JSONPatchType &&
			strings.Contains(string(patchAction.GetPatch()), "/metadata/annotations") {
			return true, nil, errors.New("injected failure after nat status recorded")
		}
		return false, nil, nil
	})

	err = ctrl.handleAddOvnDnatRule(dnat.Name)
	require.Error(t, err)

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().OvnDnatRules().Get(t.Context(), dnat.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
	require.Equal(t, dnat.Spec.Vpc, got.Status.Vpc)
	require.Equal(t, eip.Status.V4Ip, got.Status.V4Eip)
	require.Equal(t, dnat.Spec.ExternalPort, got.Status.ExternalPort)
	require.False(t, got.Status.Ready)
}

func TestHandleUpdateOvnDnatRule_DeleteRemovesLBAndFinalizer(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	dnat := &kubeovnv1.OvnDnatRule{
		Name:              "test-dnat",
		Finalizers:        []string{util.KubeOVNControllerFinalizer},
		DeletionTimestamp: &now,
		Spec:              kubeovnv1.OvnDnatRuleSpec{OvnEip: "test-eip", Vpc: "test-vpc"},
	}
	dnat.Status = kubeovnv1.OvnDnatRuleStatus{
		Vpc:          "test-vpc",
		V4Eip:        "10.0.0.10",
		ExternalPort: "2222",
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{OvnDnatRules: []*kubeovnv1.OvnDnatRule{dnat}})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.resetOvnEipQueue = newTypedRateLimitingQueue[string]("ResetOvnEip", nil)
	t.Cleanup(ctrl.resetOvnEipQueue.ShutDown)
	ctrl.updateOvnEipQueue = newTypedRateLimitingQueue[string]("UpdateOvnEip", nil)
	t.Cleanup(ctrl.updateOvnEipQueue.ShutDown)

	fc.mockOvnClient.EXPECT().
		LoadBalancerDeleteVip(dnat.Name, dnat.Status.V4Eip+":"+dnat.Status.ExternalPort, true).
		Return(nil)
	fc.mockOvnClient.EXPECT().
		LogicalRouterUpdateLoadBalancers(dnat.Status.Vpc, ovsdb.MutateOperationDelete, dnat.Name).
		Return(nil)

	require.NoError(t, ctrl.handleUpdateOvnDnatRule(dnat.Name))

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().OvnDnatRules().Get(t.Context(), dnat.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotContains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
}
