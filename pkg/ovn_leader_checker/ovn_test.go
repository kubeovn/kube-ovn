package ovn_leader_checker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func mockPod(namespace, name string, labels map[string]string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
		},
	}
}

func Test_patchPodLabels(t *testing.T) {
	t.Parallel()
	t.Run("patch new labels", func(t *testing.T) {
		t.Parallel()
		podName := "ovn-central-123"
		podNamespace := "default"
		pod := mockPod(podName, podNamespace, map[string]string{
			"app": "nginx",
		})
		clientset := fake.NewSimpleClientset(pod)

		cfg := &Configuration{
			KubeClient: clientset,
		}

		err := patchPodLabels(cfg, pod, map[string]string{
			"app":               "nginx",
			"ovn-nb-leader":     "true",
			"ovn-sb-leader":     "true",
			"ovn-northd-leader": "true",
		})

		require.NoError(t, err)

		newPod, err := clientset.CoreV1().Pods(podName).Get(context.Background(), podNamespace, metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, map[string]string{
			"ovn-nb-leader":     "true",
			"ovn-sb-leader":     "true",
			"ovn-northd-leader": "true",
			"app":               "nginx",
		}, newPod.Labels)
	})

	t.Run("delete some labels", func(t *testing.T) {
		t.Parallel()
		podName := "ovn-central-123"
		podNamespace := "default"
		pod := mockPod(podName, podNamespace, map[string]string{
			"app":               "nginx",
			"ovn-nb-leader":     "true",
			"ovn-sb-leader":     "true",
			"ovn-northd-leader": "true",
		})

		clientset := fake.NewSimpleClientset(pod)

		cfg := &Configuration{
			KubeClient: clientset,
		}

		err := patchPodLabels(cfg, pod, map[string]string{
			"ovn-northd-leader": "true",
			"app":               "nginx",
		})

		require.NoError(t, err)

		newPod, err := clientset.CoreV1().Pods(podName).Get(context.Background(), podNamespace, metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, map[string]string{
			"ovn-northd-leader": "true",
			"app":               "nginx",
		}, newPod.Labels)
	})

	t.Run("pod's labels is empty", func(t *testing.T) {
		t.Parallel()
		podName := "ovn-central-123"
		podNamespace := "default"
		pod := mockPod(podName, podNamespace, nil)

		clientset := fake.NewSimpleClientset(pod)

		cfg := &Configuration{
			KubeClient: clientset,
		}

		err := patchPodLabels(cfg, pod, map[string]string{
			"ovn-northd-leader": "true",
			"app":               "nginx",
		})
		require.NoError(t, err)

		newPod, err := clientset.CoreV1().Pods(podName).Get(context.Background(), podNamespace, metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, map[string]string{
			"ovn-northd-leader": "true",
			"app":               "nginx",
		}, newPod.Labels)
	})
}
