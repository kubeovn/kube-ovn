package controller

import (
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/mocks/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func Test_setAzName(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	nbGlobal := &ovnnb.NBGlobal{
		Connections: []string{"c7744628-6399-4852-8ac0-06e4e436c146"},
		NbCfg:       100,
		Options: map[string]string{
			"mac_prefix": "11:22:33",
			"max_tunid":  "16711680",
		},
	}

	mockOvnClient := ovs.NewMockOvnClient(mockCtrl)
	mockOvnClient.EXPECT().GetNbGlobal().Return(nbGlobal, nil)
	mockOvnClient.EXPECT().UpdateNbGlobal(nbGlobal, gomock.Any()).Return(nil)

	controller := &Controller{
		ovnClient: mockOvnClient,
	}

	err := controller.setAzName("test-az")
	require.NoError(t, err)
}
