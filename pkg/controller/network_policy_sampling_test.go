package controller

import (
	"errors"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/keymutex"

	mockovs "github.com/kubeovn/kube-ovn/mocks/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

func TestHandleNetworkPolicyACLSamplingRetriesOnlySampling(t *testing.T) {
	controller, nbClient, config := newNetworkPolicySamplingTestController(t)
	request := new(ovs.NetworkPolicySamplingRequest)
	controller.npSamplingStates.Store("default/test", &networkPolicySamplingState{request: request, ready: true})

	nbClient.EXPECT().ApplyNetworkPolicyACLSampling(config, request).Return(errors.New("injected sampling failure"))
	require.ErrorContains(t, controller.handleNetworkPolicyACLSampling("default/test"), "injected sampling failure")
	actual, ok := controller.npSamplingStates.Load("default/test")
	require.True(t, ok)
	require.Same(t, request, actual.request)

	nbClient.EXPECT().ApplyNetworkPolicyACLSampling(config, request).Return(nil)
	require.NoError(t, controller.handleNetworkPolicyACLSampling("default/test"))
	_, ok = controller.npSamplingStates.Load("default/test")
	require.False(t, ok)
}

func TestHandleNetworkPolicyACLSamplingUsesRequestStoredByConcurrentEnforcement(t *testing.T) {
	controller, nbClient, config := newNetworkPolicySamplingTestController(t)
	const key = "default/test"
	oldRequest := new(ovs.NetworkPolicySamplingRequest)
	newRequest := new(ovs.NetworkPolicySamplingRequest)
	controller.npSamplingStates.Store(key, &networkPolicySamplingState{request: oldRequest, ready: true})

	controller.npKeyMutex.LockKey(key)
	done := make(chan error, 1)
	go func() {
		done <- controller.handleNetworkPolicyACLSampling(key)
	}()
	controller.npSamplingStates.Store(key, &networkPolicySamplingState{request: newRequest, ready: true})
	nbClient.EXPECT().ApplyNetworkPolicyACLSampling(config, newRequest).Return(nil)
	require.NoError(t, controller.npKeyMutex.UnlockKey(key))

	require.NoError(t, <-done)
	_, ok := controller.npSamplingStates.Load(key)
	require.False(t, ok)
}

func TestPrepareNetworkPolicyACLSamplingRejectsIncompleteSnapshot(t *testing.T) {
	controller, nbClient, _ := newNetworkPolicySamplingTestController(t)
	request := new(ovs.NetworkPolicySamplingRequest)
	nbClient.EXPECT().PrepareNetworkPolicyACLSampling("pg", "default", "test", "uid").
		Return(request, errors.New("injected snapshot failure"))

	require.Nil(t, controller.prepareNetworkPolicyACLSampling("default/test", "pg", networkPolicySamplingTestPolicy()))
	_, ok := controller.npSamplingStates.Load("default/test")
	require.False(t, ok)
}

func TestPrepareNetworkPolicyACLSamplingRetainsSnapshotAcrossEnforcementRetries(t *testing.T) {
	controller, _, _ := newNetworkPolicySamplingTestController(t)
	const key = "default/test"
	state := &networkPolicySamplingState{request: new(ovs.NetworkPolicySamplingRequest), policyUID: types.UID("uid"), ready: true}
	controller.npSamplingStates.Store(key, state)

	actual := controller.prepareNetworkPolicyACLSampling(key, "pg", networkPolicySamplingTestPolicy())
	require.Same(t, state, actual)
	require.False(t, actual.ready)
}

func TestPrepareNetworkPolicyACLSamplingReplacesSnapshotForNewPolicyUID(t *testing.T) {
	controller, nbClient, _ := newNetworkPolicySamplingTestController(t)
	const key = "default/test"
	oldState := &networkPolicySamplingState{request: new(ovs.NetworkPolicySamplingRequest), policyUID: types.UID("old-uid"), ready: true}
	controller.npSamplingStates.Store(key, oldState)
	newRequest := new(ovs.NetworkPolicySamplingRequest)
	nbClient.EXPECT().PrepareNetworkPolicyACLSampling("pg", "default", "test", "uid").Return(newRequest, nil)

	actual := controller.prepareNetworkPolicyACLSampling(key, "pg", networkPolicySamplingTestPolicy())
	require.NotSame(t, oldState, actual)
	require.Same(t, newRequest, actual.request)
	require.Equal(t, types.UID("uid"), actual.policyUID)
}

func TestDeleteNetworkPolicySamplingStateMatchesPolicyUID(t *testing.T) {
	controller, _, _ := newNetworkPolicySamplingTestController(t)
	const key = "default/test"
	state := &networkPolicySamplingState{request: new(ovs.NetworkPolicySamplingRequest), policyUID: types.UID("new-uid")}
	controller.npSamplingStates.Store(key, state)

	controller.deleteNetworkPolicySamplingState(key, types.UID("old-uid"))
	actual, ok := controller.npSamplingStates.Load(key)
	require.True(t, ok)
	require.Same(t, state, actual)

	controller.deleteNetworkPolicySamplingState(key, types.UID("new-uid"))
	_, ok = controller.npSamplingStates.Load(key)
	require.False(t, ok)
}

func TestHandleNetworkPolicyACLSamplingWaitsForSuccessfulEnforcement(t *testing.T) {
	controller, nbClient, config := newNetworkPolicySamplingTestController(t)
	const key = "default/test"
	request := new(ovs.NetworkPolicySamplingRequest)
	state := &networkPolicySamplingState{request: request}
	controller.npSamplingStates.Store(key, state)

	require.NoError(t, controller.handleNetworkPolicyACLSampling(key))
	actual, ok := controller.npSamplingStates.Load(key)
	require.True(t, ok)
	require.Same(t, state, actual)

	controller.queueNetworkPolicyACLSampling(key, state)
	require.True(t, state.ready)
	nbClient.EXPECT().ApplyNetworkPolicyACLSampling(config, request).Return(nil)
	require.NoError(t, controller.handleNetworkPolicyACLSampling(key))
	_, ok = controller.npSamplingStates.Load(key)
	require.False(t, ok)
}

func TestSetNetworkPolicyACLLogReportsSamplingReadiness(t *testing.T) {
	controller, nbClient, _ := newNetworkPolicySamplingTestController(t)
	nbClient.EXPECT().SetNetPolACLLog("pg", true, true).Return(nil)
	require.True(t, controller.setNetworkPolicyACLLog("pg", "default/test", true, true))
	nbClient.EXPECT().SetNetPolACLLog("pg", true, false).Return(errors.New("injected logging failure"))
	require.False(t, controller.setNetworkPolicyACLLog("pg", "default/test", true, false))
}

func newNetworkPolicySamplingTestController(t *testing.T) (*Controller, *mockovs.MockNbClient, aclsampling.ControllerConfig) {
	t.Helper()
	config := aclsampling.ControllerConfig{Enabled: true}
	nbClient := mockovs.NewMockNbClient(gomock.NewController(t))
	controller := &Controller{
		config:           &Configuration{ACLSampling: config},
		OVNNbClient:      nbClient,
		npSamplingQueue:  newTypedRateLimitingQueue[string]("TestNetworkPolicyACLSampling", nil),
		npSamplingStates: xsync.NewMap[string, *networkPolicySamplingState](),
		npKeyMutex:       keymutex.NewHashed(1),
	}
	t.Cleanup(controller.npSamplingQueue.ShutDown)
	return controller, nbClient, config
}

func networkPolicySamplingTestPolicy() *netv1.NetworkPolicy {
	return &netv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{
		Namespace: "default",
		Name:      "test",
		UID:       types.UID("uid"),
	}}
}
