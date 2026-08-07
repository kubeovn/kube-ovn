package controller

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"k8s.io/utils/keymutex"

	mockovs "github.com/kubeovn/kube-ovn/mocks/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

func TestHandleNetworkPolicyACLSamplingRetriesOnlySampling(t *testing.T) {
	mockController := gomock.NewController(t)
	nbClient := mockovs.NewMockNbClient(mockController)
	config := aclsampling.ControllerConfig{Enabled: true}
	controller := &Controller{
		config:             &Configuration{ACLSampling: config},
		OVNNbClient:        nbClient,
		npSamplingRequests: xsync.NewMap[string, *ovs.NetworkPolicySamplingRequest](),
		npKeyMutex:         keymutex.NewHashed(1),
	}
	request := new(ovs.NetworkPolicySamplingRequest)
	controller.npSamplingRequests.Store("default/test", request)
	failuresBefore := testutil.ToFloat64(metricACLSamplingControllerFailures.WithLabelValues(aclSamplingOperationAttach))

	nbClient.EXPECT().ApplyNetworkPolicyACLSampling(config, request).Return(errors.New("injected sampling failure"))
	require.ErrorContains(t, controller.handleNetworkPolicyACLSampling("default/test"), "injected sampling failure")
	require.Equal(t, failuresBefore+1, testutil.ToFloat64(metricACLSamplingControllerFailures.WithLabelValues(aclSamplingOperationAttach)))
	require.Equal(t, float64(0), testutil.ToFloat64(metricACLSamplingControllerAvailable))
	actual, ok := controller.npSamplingRequests.Load("default/test")
	require.True(t, ok)
	require.Same(t, request, actual)

	nbClient.EXPECT().ApplyNetworkPolicyACLSampling(config, request).Return(nil)
	require.NoError(t, controller.handleNetworkPolicyACLSampling("default/test"))
	require.Equal(t, float64(1), testutil.ToFloat64(metricACLSamplingControllerAvailable))
	_, ok = controller.npSamplingRequests.Load("default/test")
	require.False(t, ok)
}

func TestHandleNetworkPolicyACLSamplingUsesRequestStoredByConcurrentEnforcement(t *testing.T) {
	mockController := gomock.NewController(t)
	nbClient := mockovs.NewMockNbClient(mockController)
	config := aclsampling.ControllerConfig{Enabled: true}
	controller := &Controller{
		config:             &Configuration{ACLSampling: config},
		OVNNbClient:        nbClient,
		npSamplingRequests: xsync.NewMap[string, *ovs.NetworkPolicySamplingRequest](),
		npKeyMutex:         keymutex.NewHashed(1),
	}
	const key = "default/test"
	oldRequest := new(ovs.NetworkPolicySamplingRequest)
	newRequest := new(ovs.NetworkPolicySamplingRequest)
	controller.npSamplingRequests.Store(key, oldRequest)

	controller.npKeyMutex.LockKey(key)
	done := make(chan error, 1)
	go func() {
		done <- controller.handleNetworkPolicyACLSampling(key)
	}()
	controller.npSamplingRequests.Store(key, newRequest)
	nbClient.EXPECT().ApplyNetworkPolicyACLSampling(config, newRequest).Return(nil)
	require.NoError(t, controller.npKeyMutex.UnlockKey(key))

	require.NoError(t, <-done)
	_, ok := controller.npSamplingRequests.Load(key)
	require.False(t, ok)
}

func TestReconcileACLSamplingRecordsAvailability(t *testing.T) {
	mockController := gomock.NewController(t)
	nbClient := mockovs.NewMockNbClient(mockController)
	config := aclsampling.ControllerConfig{Enabled: true}
	controller := &Controller{
		config:      &Configuration{ACLSampling: config},
		OVNNbClient: nbClient,
	}

	failuresBefore := testutil.ToFloat64(metricACLSamplingControllerFailures.WithLabelValues(aclSamplingOperationReconcile))
	nbClient.EXPECT().ReconcileACLSampling(config).Return(errors.New("injected schema conflict"))
	controller.reconcileACLSampling()
	require.Equal(t, failuresBefore+1, testutil.ToFloat64(metricACLSamplingControllerFailures.WithLabelValues(aclSamplingOperationReconcile)))
	require.Equal(t, float64(0), testutil.ToFloat64(metricACLSamplingControllerAvailable))

	nbClient.EXPECT().ReconcileACLSampling(config).Return(nil)
	controller.reconcileACLSampling()
	require.Equal(t, float64(1), testutil.ToFloat64(metricACLSamplingControllerAvailable))

	config.Enabled = false
	controller.config.ACLSampling = config
	nbClient.EXPECT().ReconcileACLSampling(config).Return(nil)
	controller.reconcileACLSampling()
	require.Equal(t, float64(0), testutil.ToFloat64(metricACLSamplingControllerAvailable))
}
