package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func Test_upgradeNetworkPolicies(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	fakeinformers := fakeController.fakeInformers
	mockOvnClient := fakeController.mockOvnClient

	np := &netv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "np1",
			Namespace: "default",
		},
	}

	err := fakeinformers.npInformer.Informer().GetStore().Add(np)
	require.NoError(t, err)

	mockOvnClient.EXPECT().DeleteAcls(gomock.Any(), portGroupKey, "", nil, util.DefaultACLTier).Return(nil)

	err = ctrl.upgradeNetworkPolicies()
	require.NoError(t, err)
}
