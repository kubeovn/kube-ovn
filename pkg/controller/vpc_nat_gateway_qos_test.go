package controller

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestNatGwRedoTokenUsesContainerInstancesPerGateway(t *testing.T) {
	created := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	started := metav1.NewTime(created.Add(time.Minute))
	gateway := func(name string) *kubeovnv1.VpcNatGateway {
		return &kubeovnv1.VpcNatGateway{Name: name}
	}
	pod := func(gateway, containerID string) *corev1.Pod {
		return &corev1.Pod{
			Name: gateway + "-0", Namespace: metav1.NamespaceSystem, UID: types.UID(gateway + "-uid"),
			Labels:            map[string]string{"app": "vpc-nat-gw-" + gateway, "ovn.kubernetes.io/vpc-nat-gw": "true"},
			CreationTimestamp: created,
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{
					Name: "vpc-nat-gw", ContainerID: containerID,
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: started}},
				}},
			},
		}
	}
	fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gateway("gw1"), gateway("gw2")},
		Pods:           []*corev1.Pod{pod("gw1", "containerd://one"), pod("gw2", "containerd://two")},
	})
	require.NoError(t, err)

	token1, err := fakeCtrl.fakeController.natGwRedoToken("gw1")
	require.NoError(t, err)
	token2, err := fakeCtrl.fakeController.natGwRedoToken("gw2")
	require.NoError(t, err)
	require.Len(t, token1, sha256.Size*2)
	require.Len(t, token2, sha256.Size*2)
	require.NotEqual(t, token1, token2)

	restarted := pod("gw1", "containerd://restarted")
	_, err = fakeCtrl.fakeController.config.KubeClient.CoreV1().Pods(metav1.NamespaceSystem).Update(
		context.Background(), restarted, metav1.UpdateOptions{},
	)
	require.NoError(t, err)
	restartedToken, err := fakeCtrl.fakeController.natGwRedoToken("gw1")
	require.NoError(t, err)
	require.Len(t, restartedToken, sha256.Size*2)
	require.NotEqual(t, token1, restartedToken)

	// A replica whose container is not running carries no instance to identify and
	// must not stop the other replicas from being restored.
	waiting := pod("gw1", "")
	waiting.Name = "gw1-1"
	waiting.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name: "vpc-nat-gw",
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
			Reason: "CrashLoopBackOff",
		}},
	}}
	_, err = fakeCtrl.fakeController.config.KubeClient.CoreV1().Pods(metav1.NamespaceSystem).Create(
		context.Background(), waiting, metav1.CreateOptions{},
	)
	require.NoError(t, err)
	withoutWaitingReplica, err := fakeCtrl.fakeController.natGwRedoToken("gw1")
	require.NoError(t, err)
	require.Equal(t, restartedToken, withoutWaitingReplica)

	// Starting that container adds its instance to the token, which asks for a redo.
	waiting.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name: "vpc-nat-gw", ContainerID: "containerd://second-replica",
		State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: started}},
	}}
	_, err = fakeCtrl.fakeController.config.KubeClient.CoreV1().Pods(metav1.NamespaceSystem).Update(
		context.Background(), waiting, metav1.UpdateOptions{},
	)
	require.NoError(t, err)
	withWaitingReplica, err := fakeCtrl.fakeController.natGwRedoToken("gw1")
	require.NoError(t, err)
	require.NotEqual(t, withoutWaitingReplica, withWaitingReplica)
}

func TestNatGwInitTargets(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	live := &corev1.Pod{Name: "gw-0"}
	terminating := &corev1.Pod{Name: "gw-old", DeletionTimestamp: &now}

	// A terminating Pod cannot be initialized, so its replacement has to be waited for
	// instead of reporting a successful initialization of the gateway.
	require.Equal(t, []*corev1.Pod{live}, natGwInitTargets([]*corev1.Pod{terminating, live}))
	require.Empty(t, natGwInitTargets([]*corev1.Pod{terminating}))
	require.Empty(t, natGwInitTargets(nil))

	// Only a gateway that still exists waits for its replacement Pod: a gateway already
	// marked for deletion has nothing left to initialize, and a gateway with a Pod has a
	// target to run the init command on.
	gateway := &kubeovnv1.VpcNatGateway{Name: "gw"}
	require.True(t, natGwInitPending(gateway, nil))
	require.False(t, natGwInitPending(gateway, []*corev1.Pod{live}))
	terminatingGateway := gateway.DeepCopy()
	terminatingGateway.DeletionTimestamp = &now
	require.False(t, natGwInitPending(terminatingGateway, nil))
}
