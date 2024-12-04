package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func Test_upgradeNodes(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	fakeinformers := fakeController.fakeInformers
	mockOvnClient := fakeController.mockOvnClient

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
			Annotations: map[string]string{
				util.PortNameAnnotation: "node-1",
			},
		},
	}

	err := fakeinformers.nodeInformer.Informer().GetStore().Add(node)
	require.NoError(t, err)

	mockOvnClient.EXPECT().DeleteAcls(gomock.Any(), portGroupKey, "", nil, util.DefaultACLTier).Return(nil).Times(1)

	err = ctrl.upgradeNodes()
	require.NoError(t, err)
}
