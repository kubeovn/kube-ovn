package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestResyncProviderNetworkStatusRecordsNodeNotReadyEvent(t *testing.T) {
	t.Parallel()

	const (
		providerNetworkName = "provider-network-1"
		nodeName            = "node-1"
	)

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{{
			ObjectMeta: metav1.ObjectMeta{Name: providerNetworkName},
		}},
		Nodes: []*corev1.Node{{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		}},
	})
	require.NoError(t, err)

	recorder := record.NewFakeRecorder(1)
	fc.fakeController.recorder = recorder
	fc.fakeController.resyncProviderNetworkStatus()

	require.Equal(t,
		"Warning InitOVSBridgeFailed Failed to initialize OVS bridge on node node-1: kube-ovn-cni pod on node node-1 not found",
		requireRecorderEvent(t, recorder),
	)

	pn, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Get(
		context.Background(), providerNetworkName, metav1.GetOptions{},
	)
	require.NoError(t, err)
	require.False(t, pn.Status.Ready)
	require.Equal(t, "InitOVSBridgeFailed", pn.Status.ConditionReason(nodeName, kubeovnv1.Ready))
}

func TestResyncProviderNetworkStatusRecordsNodeReadyEvent(t *testing.T) {
	t.Parallel()

	const (
		providerNetworkName = "provider-network-1"
		nodeName            = "node-1"
	)

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{{
			ObjectMeta: metav1.ObjectMeta{Name: providerNetworkName},
		}},
		Nodes: []*corev1.Node{{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
				Labels: map[string]string{
					fmt.Sprintf(util.ProviderNetworkReadyTemplate, providerNetworkName): "true",
				},
			},
		}},
	})
	require.NoError(t, err)

	recorder := record.NewFakeRecorder(1)
	fc.fakeController.recorder = recorder
	fc.fakeController.resyncProviderNetworkStatus()

	require.Equal(t,
		"Normal InitOVSBridgeSucceeded Initialized OVS bridge on node node-1",
		requireRecorderEvent(t, recorder),
	)

	pn, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Get(
		context.Background(), providerNetworkName, metav1.GetOptions{},
	)
	require.NoError(t, err)
	require.True(t, pn.Status.Ready)
	require.Equal(t, "InitOVSBridgeSucceeded", pn.Status.ConditionReason(nodeName, kubeovnv1.Ready))
}
