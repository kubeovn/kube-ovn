package controller

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestRecordSubnetError(t *testing.T) {
	recorder := record.NewFakeRecorder(1)
	c := &Controller{recorder: recorder}
	subnet := &kubeovnv1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "subnet-a"}}
	sourceErr := errors.New("boom")

	err := c.recordSubnetError(subnet, "ReconcileSubnetFailed", sourceErr)

	require.ErrorIs(t, err, sourceErr)
	require.Equal(t, "Warning ReconcileSubnetFailed boom", requireRecorderEvent(t, recorder))
}

func TestRecordIPPoolError(t *testing.T) {
	recorder := record.NewFakeRecorder(1)
	c := &Controller{recorder: recorder}
	ippool := &kubeovnv1.IPPool{ObjectMeta: metav1.ObjectMeta{Name: "pool-a"}}
	sourceErr := errors.New("boom")

	err := c.recordIPPoolError(ippool, "UpdateIPAMFailed", sourceErr)

	require.ErrorIs(t, err, sourceErr)
	require.Equal(t, "Warning UpdateIPAMFailed boom", requireRecorderEvent(t, recorder))
}

func TestResourceEventHelpersAllowMissingRecorderAndObject(t *testing.T) {
	c := &Controller{}

	require.NotPanics(t, func() {
		c.recordSubnetEvent(nil, corev1.EventTypeNormal, "ReconcileSuccess", "done")
		c.recordIPPoolEvent(nil, corev1.EventTypeNormal, "ReconcileSuccess", "done")
	})
}

func TestHandleAddOrUpdateSubnetRecordsFormatFailure(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "invalid-subnet"},
		Spec:       kubeovnv1.SubnetSpec{CIDRBlock: "invalid"},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)

	err = fc.fakeController.handleAddOrUpdateSubnet(subnet.Name)

	require.Error(t, err)
	require.Equal(t, "Warning FormatSubnetFailed failed to format subnet invalid-subnet, subnet invalid-subnet cidr invalid is invalid", requireRecorderEvent(t, fc.fakeController.recorder.(*record.FakeRecorder)))
}

func TestHandleAddOrUpdateSubnetRecordsStatusCalculationFailure(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "subnet-a"},
		Spec: kubeovnv1.SubnetSpec{
			Vpc:       util.DefaultVpc,
			CIDRBlock: "10.16.0.0/24",
			Gateway:   "10.16.0.1",
		},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	controller := fc.fakeController
	controller.ipIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	recorder := controller.recorder.(*record.FakeRecorder)

	err = controller.handleAddOrUpdateSubnet(subnet.Name)

	require.ErrorContains(t, err, "Index with name bySubnet does not exist")
	require.Equal(t, "Normal ValidateLogicalSwitchSuccess Subnet subnet-a status updated successfully", requireRecorderEvent(t, recorder))
	require.Equal(t, "Warning CalculateStatusFailed Index with name bySubnet does not exist", requireRecorderEvent(t, recorder))
}

func TestHandleAddOrUpdateIPPoolRecordsStatusPatchFailure(t *testing.T) {
	ippool := &kubeovnv1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pool-a",
			Finalizers: []string{util.KubeOVNControllerFinalizer},
		},
		Spec: kubeovnv1.IPPoolSpec{Subnet: "subnet-a"},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{IPPools: []*kubeovnv1.IPPool{ippool}})
	require.NoError(t, err)
	controller := fc.fakeController
	controller.ippoolKeyMutex = keymutex.NewHashed(0)
	reconcileErr := errors.New("delete address set failed")
	statusErr := errors.New("status patch failed")
	fc.mockOvnClient.EXPECT().DeleteAddressSet("pool.a").Return(reconcileErr)
	controller.config.KubeOvnClient.(*kubeovnfake.Clientset).PrependReactor("patch", "ippools", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "status" {
			return false, nil, nil
		}
		return true, nil, statusErr
	})
	recorder := controller.recorder.(*record.FakeRecorder)

	err = controller.handleAddOrUpdateIPPool(ippool.Name)

	require.ErrorIs(t, err, reconcileErr)
	require.ErrorIs(t, err, statusErr)
	require.Equal(t, "Warning ReconcileAddressSetFailed failed to delete address set pool.a: delete address set failed", requireRecorderEvent(t, recorder))
	require.Equal(t, "Warning UpdateStatusFailed status patch failed", requireRecorderEvent(t, recorder))
}

func TestHandleAddOrUpdateSubnetDoesNotRecordSuccessWhenStatusPatchFails(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "subnet-a"},
		Spec: kubeovnv1.SubnetSpec{
			Vpc:       util.DefaultVpc,
			CIDRBlock: "10.16.0.0/24",
			Gateway:   "10.16.0.1",
		},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	controller := fc.fakeController
	statusErr := errors.New("status patch failed")
	controller.config.KubeOvnClient.(*kubeovnfake.Clientset).PrependReactor("patch", "subnets", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "status" {
			return false, nil, nil
		}
		return true, nil, statusErr
	})
	recorder := controller.recorder.(*record.FakeRecorder)

	err = controller.handleAddOrUpdateSubnet(subnet.Name)

	require.ErrorIs(t, err, statusErr)
	require.Equal(t, "Warning UpdateStatusFailed status patch failed", requireRecorderEvent(t, recorder))
	select {
	case event := <-recorder.Events:
		t.Fatalf("unexpected event %q", event)
	default:
	}
}

func TestHandleAddOrUpdateIPPoolDoesNotRecordSuccessWhenStatusPatchFails(t *testing.T) {
	ippool := &kubeovnv1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pool-a",
			Finalizers: []string{util.KubeOVNControllerFinalizer},
		},
		Spec: kubeovnv1.IPPoolSpec{
			Subnet: "subnet-a",
			IPs:    []string{"10.16.0.10"},
		},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{IPPools: []*kubeovnv1.IPPool{ippool}})
	require.NoError(t, err)
	controller := fc.fakeController
	controller.ippoolKeyMutex = keymutex.NewHashed(0)
	require.NoError(t, controller.ipam.AddOrUpdateSubnet("subnet-a", "10.16.0.0/24", "10.16.0.1", nil))
	fc.mockOvnClient.EXPECT().DeleteAddressSet("pool.a").Return(nil)
	statusErr := errors.New("status patch failed")
	controller.config.KubeOvnClient.(*kubeovnfake.Clientset).PrependReactor("patch", "ippools", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "status" {
			return false, nil, nil
		}
		return true, nil, statusErr
	})
	recorder := controller.recorder.(*record.FakeRecorder)

	err = controller.handleAddOrUpdateIPPool(ippool.Name)

	require.ErrorIs(t, err, statusErr)
	require.Equal(t, "Warning UpdateStatusFailed status patch failed", requireRecorderEvent(t, recorder))
	select {
	case event := <-recorder.Events:
		t.Fatalf("unexpected event %q", event)
	default:
	}
}

func TestRecordResourceSuccessEvents(t *testing.T) {
	recorder := record.NewFakeRecorder(2)
	c := &Controller{recorder: recorder}
	subnet := &kubeovnv1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "subnet-a"}}
	ippool := &kubeovnv1.IPPool{ObjectMeta: metav1.ObjectMeta{Name: "pool-a"}}

	c.recordSubnetEvent(subnet, corev1.EventTypeNormal, "ReconcileSuccess", "Subnet subnet-a reconciled successfully")
	c.recordIPPoolEvent(ippool, corev1.EventTypeNormal, "DeleteSuccess", "IPPool pool-a deleted successfully")

	require.Equal(t, "Normal ReconcileSuccess Subnet subnet-a reconciled successfully", requireRecorderEvent(t, recorder))
	require.Equal(t, "Normal DeleteSuccess IPPool pool-a deleted successfully", requireRecorderEvent(t, recorder))
}

func TestHandleDeleteIPPoolRecordsOutcome(t *testing.T) {
	tests := []struct {
		name          string
		deleteErr     error
		expectedEvent string
	}{
		{
			name:          "failure",
			deleteErr:     errors.New("delete address set failed"),
			expectedEvent: "Warning DeleteFailed failed to delete ippool pool-a: delete address set failed",
		},
		{
			name:          "success",
			expectedEvent: "Normal DeleteSuccess IPPool pool-a deleted successfully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ippool := &kubeovnv1.IPPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "pool-a",
					Finalizers: []string{util.KubeOVNControllerFinalizer},
				},
				Spec: kubeovnv1.IPPoolSpec{Subnet: "subnet-a"},
			}
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{IPPools: []*kubeovnv1.IPPool{ippool}})
			require.NoError(t, err)
			controller := fc.fakeController
			controller.ippoolKeyMutex = keymutex.NewHashed(0)
			fc.mockOvnClient.EXPECT().DeleteAddressSet("pool.a").Return(tt.deleteErr)

			err = controller.handleDeleteIPPool(ippool)
			if tt.deleteErr != nil {
				require.ErrorIs(t, err, tt.deleteErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.expectedEvent, requireRecorderEvent(t, controller.recorder.(*record.FakeRecorder)))
		})
	}
}
