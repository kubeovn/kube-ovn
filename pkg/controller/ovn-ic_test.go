package controller

import (
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func Test_listRemoteLogicalSwitchPortAddress(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient

	mockLsp := func(addresses []string) ovnnb.LogicalSwitchPort {
		return ovnnb.LogicalSwitchPort{
			Addresses: addresses,
		}
	}

	lsps := []ovnnb.LogicalSwitchPort{
		mockLsp([]string{"00:00:00:53:21:6F 10.244.0.17 fc00::af4:11"}),
		mockLsp([]string{"00:00:00:53:21:6F 10.244.0.18"}),
		mockLsp([]string{"00:00:00:53:21:6E 10.244.0.10"}),
		mockLsp([]string{"00:00:00:53:21:6F"}),
		mockLsp([]string{""}),
		mockLsp([]string{}),
	}

	mockOvnClient.EXPECT().ListLogicalSwitchPorts(gomock.Any(), gomock.Any(), gomock.Any()).Return(lsps, nil)

	addresses, err := ctrl.listRemoteLogicalSwitchPortAddress()
	require.NoError(t, err)
	require.Equal(t, map[string]struct{}{
		"10.244.0.10": {},
		"10.244.0.18": {},
	}, addresses)
}
