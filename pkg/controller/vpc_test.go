package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestReconcileVpcBfdLRPClearsHAChassisGroupWhenSelectorMatchesNoReadyNodes(t *testing.T) {
	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient

	require.NoError(t, fakeController.fakeInformers.nodeInformer.Informer().GetStore().Add(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a", Labels: map[string]string{"egress": "true"}},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{
			Type: corev1.NodeReady, Status: corev1.ConditionFalse,
		}}},
	}))

	vpc := &kubeovnv1.Vpc{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpc-bfd"},
		Spec: kubeovnv1.VpcSpec{
			BFDPort: &kubeovnv1.BFDPort{
				Enabled: true,
				IP:      "169.254.0.1/32",
				NodeSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"egress": "true"},
				},
			},
		},
	}

	portName := "bfd@test-vpc-bfd"
	networks := []string{"169.254.0.1/32"}
	mockOvnClient.EXPECT().CreateLogicalRouterPort(vpc.Name, portName, "", networks).Return(nil)
	mockOvnClient.EXPECT().UpdateLogicalRouterPortNetworks(portName, networks).Return(nil)
	mockOvnClient.EXPECT().UpdateLogicalRouterPortOptions(portName, map[string]string{"bfd-only": "true"}).Return(nil)
	mockOvnClient.EXPECT().CreateHAChassisGroup(portName, []string{}, map[string]string{"lrp": portName}).Return(nil)
	mockOvnClient.EXPECT().SetLogicalRouterPortHAChassisGroup(portName, portName).Return(nil)

	name, nodes, err := ctrl.reconcileVpcBfdLRP(vpc)
	require.NoError(t, err)
	require.Equal(t, portName, name)
	require.Empty(t, nodes)
}
