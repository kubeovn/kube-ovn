package ko

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestLeaderRecoveryDisruptsCentralPodsAndPreservesAgent(t *testing.T) {
	for _, stalledRole := range []string{"", "nb", "sb", "northd"} {
		t.Run("stalled="+stalledRole, func(t *testing.T) {
			roles := []string{"nb", "sb", "northd"}
			objects := []runtime.Object{
				readyPod("agent", "node", "agent", map[string]string{"app": "kubectl-ko-node-agent"}),
				&appsv1.Deployment{
					Name: "ovn-central", Namespace: "ovn-system",
					Spec:   appsv1.DeploymentSpec{Replicas: new(int32(3))},
					Status: appsv1.DeploymentStatus{Replicas: 3, UpdatedReplicas: 3, ReadyReplicas: 3, AvailableReplicas: 3},
				},
			}
			for _, role := range roles {
				objects = append(objects, readyPod(role+"-leader", "node", "ovn-central", map[string]string{"ovn-" + role + "-leader": "true"}))
			}
			app, executor, out, _ := testApplication(t, objects...)
			client, err := app.newClient()
			require.NoError(t, err)
			client.ComponentFree = true
			kube := client.Kubernetes.(*fake.Clientset)
			var deleted []string
			kube.PrependReactor("delete", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
				deletion := action.(ktesting.DeleteAction)
				role := roles[len(deleted)]
				require.Equal(t, role+"-leader", deletion.GetName())
				require.Equal(t, "ovn-system", deletion.GetNamespace())
				require.Equal(t, "uid-"+role+"-leader", string(*deletion.GetDeleteOptions().Preconditions.UID))
				deleted = append(deleted, role)
				if role == stalledRole {
					// The API accepted deletion, but the old leader is still serving.
					return true, nil, nil
				}
				podResource := corev1.SchemeGroupVersion.WithResource("pods")
				require.NoError(t, kube.Tracker().Delete(podResource, "ovn-system", deletion.GetName()))
				require.NoError(t, kube.Tracker().Create(podResource,
					readyPod(role+"-replacement", "node", "ovn-central", map[string]string{"ovn-" + role + "-leader": "true"}), "ovn-system"))
				return true, nil, nil
			})
			ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
			defer cancel()
			err = app.leaderRecoveryPerformance(ctx, client)
			if stalledRole == "" {
				require.NoError(t, err)
				require.Equal(t, roles, deleted)
				for _, role := range roles {
					require.Contains(t, out.String(), role+" leader recovered")
				}
			} else {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Equal(t, stalledRole, deleted[len(deleted)-1])
				require.NotContains(t, out.String(), stalledRole+" leader recovered")
			}
			_, err = kube.CoreV1().Pods(client.Namespace).Get(t.Context(), "agent", metav1.GetOptions{})
			require.NoError(t, err)
			require.Empty(t, executor.calls)
		})
	}
}
