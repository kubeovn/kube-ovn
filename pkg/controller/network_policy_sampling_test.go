package controller

import (
	"errors"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

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
	}
	request := new(ovs.NetworkPolicySamplingRequest)
	controller.npSamplingRequests.Store("default/test", request)

	nbClient.EXPECT().ApplyNetworkPolicyACLSampling(config, request).Return(errors.New("injected sampling failure"))
	require.ErrorContains(t, controller.handleNetworkPolicyACLSampling("default/test"), "injected sampling failure")
	actual, ok := controller.npSamplingRequests.Load("default/test")
	require.True(t, ok)
	require.Same(t, request, actual)

	nbClient.EXPECT().ApplyNetworkPolicyACLSampling(config, request).Return(nil)
	require.NoError(t, controller.handleNetworkPolicyACLSampling("default/test"))
	_, ok = controller.npSamplingRequests.Load("default/test")
	require.False(t, ok)
}
