package ko

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func recoverySpec(container string) corev1.PodSpec {
	return corev1.PodSpec{Containers: []corev1.Container{{Name: container, VolumeMounts: []corev1.VolumeMount{{Name: "db", MountPath: "/etc/ovn"}}, Env: []corev1.EnvVar{{Name: "NODE_IPS", Value: "10.0.0.1,10.0.0.2"}}}}, Volumes: []corev1.Volume{{Name: "db", HostPath: &corev1.HostPathVolumeSource{Path: "/etc/ovn"}}}}
}

func recoveryApplication(t *testing.T) (*Application, *Client, *recordingExecutor, *appsv1.Deployment) {
	t.Helper()
	deployment := &appsv1.Deployment{Name: "ovn-central", Namespace: "ovn-system", Spec: appsv1.DeploymentSpec{Replicas: new(int32(2)), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "ovn-central"}}, Template: corev1.PodTemplateSpec{Spec: recoverySpec("ovn-central")}}, Status: appsv1.DeploymentStatus{Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2}}
	objects := []runtime.Object{deployment, &appsv1.DaemonSet{Name: "ovs-ovn", Namespace: "ovn-system", Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 2, CurrentNumberScheduled: 2, UpdatedNumberScheduled: 2, NumberReady: 2, NumberAvailable: 2}}}
	for i := 1; i <= 2; i++ {
		name := fmt.Sprintf("node-%d", i)
		node := &corev1.Node{Name: name, Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: fmt.Sprintf("10.0.0.%d", i)}}}}
		pod := readyPod("ovs-"+name, name, "openvswitch", map[string]string{"app": "ovs"})
		pod.Spec = recoverySpec("openvswitch")
		pod.Spec.NodeName = name
		objects = append(objects, node, pod)
	}
	app, executor, _, _ := testApplication(t, objects...)
	client, err := app.newClient()
	require.NoError(t, err)
	executor.run = func(_ context.Context, _ Target, argv []string, s Streams) error {
		if len(argv) > 1 && argv[1] == "db-name" {
			_, err := io.WriteString(s.Out, "OVN_Northbound\n")
			return err
		}
		if len(argv) > 3 && argv[3] == "ovsdb-server/get-db-storage-status" {
			_, err := io.WriteString(s.Out, "status: ok\n")
			return err
		}
		return nil
	}
	return app, client, executor, deployment
}

func TestRecoveryPlanRejectsNonBootstrapSource(t *testing.T) {
	_, client, executor, _ := recoveryApplication(t)
	_, _, err := client.planRecovery(t.Context(), "node-2")
	require.ErrorContains(t, err, "first NODE_IPS member")
	require.Empty(t, executor.calls)
	_, record, err := client.planRecovery(t.Context(), "node-1")
	require.NoError(t, err)
	require.Len(t, record.Targets, 2)
}

func TestRecoveryBackupDirectorySurvivesDatabaseStartupPermissions(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("database startup permissions apply to Linux cluster filesystems")
	}
	_, client, _, _ := recoveryApplication(t)
	_, record, err := client.planRecovery(t.Context(), "node-1")
	require.NoError(t, err)
	directory := t.TempDir()
	backup := filepath.Join(directory, filepath.Base(record.Directory))
	require.NoError(t, os.Mkdir(backup, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(backup, "ovnnb_db.original"), []byte("backup"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "ovnnb_db.db"), []byte("database"), 0o600))
	// start-db.sh applies this glob after restarting the recovered database.
	output, err := exec.CommandContext(t.Context(), "bash", "-c", `chmod 600 "$1"/*`, "start-db", directory).CombinedOutput()
	require.NoError(t, err, "%s", output)
	info, err := os.Stat(backup)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm(), "retained backups must remain traversable after central restarts")
}

func TestRecoveryRejectsUnsharedDatabaseVolumes(t *testing.T) {
	_, client, _, deployment := recoveryApplication(t)
	deployment.Spec.Template.Spec.Volumes[0].HostPath = nil
	_, err := client.Kubernetes.AppsV1().Deployments(client.Namespace).Update(t.Context(), deployment, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, _, err = client.planRecovery(t.Context(), "node-1")
	require.ErrorContains(t, err, "writable hostPath")
}

func TestRecoveryPlansHelpersWhenOVSDoesNotMountDatabases(t *testing.T) {
	_, client, executor, _ := recoveryApplication(t)
	for _, name := range []string{"ovs-node-1", "ovs-node-2"} {
		pod, err := client.Kubernetes.CoreV1().Pods(client.Namespace).Get(t.Context(), name, metav1.GetOptions{})
		require.NoError(t, err)
		pod.Spec.Containers[0].VolumeMounts = nil
		pod.Spec.Volumes = nil
		_, err = client.Kubernetes.CoreV1().Pods(client.Namespace).Update(t.Context(), pod, metav1.UpdateOptions{})
		require.NoError(t, err)
	}
	_, record, err := client.planRecovery(t.Context(), "node-1")
	require.NoError(t, err)
	require.Len(t, record.Targets, 2)
	for _, target := range record.Targets {
		require.Equal(t, "recovery", target.Container)
		require.True(t, strings.HasPrefix(target.Pod, "ko-recovery-"))
	}
	require.Empty(t, executor.calls, "dry-run must not access an unrelated OVS filesystem")
	pods, err := client.Kubernetes.CoreV1().Pods(client.Namespace).List(t.Context(), metav1.ListOptions{LabelSelector: "app=kubectl-ko-probe"})
	require.NoError(t, err)
	require.Empty(t, pods.Items, "planning must not create helpers")
}

func TestRecoveryHelperFailureCleansUpBeforeStoppingCentral(t *testing.T) {
	t.Chdir(t.TempDir())
	app, client, executor, deployment := recoveryApplication(t)
	deployment.Spec.Template.Spec.Containers[0].Image = "registry.example/kube-ovn:test"
	deployment.Spec.Template.Spec.Volumes[0].HostPath.Path = "/custom/ovn"
	record := &recoveryRecord{ID: "helper-test", Namespace: client.Namespace, SourceNode: "node-1", Replicas: 2, Directory: "/etc/ovn/recovery", Targets: []Target{{Namespace: client.Namespace, Pod: "ko-recovery-helper-test-0", Container: "recovery", Node: "node-1"}}}
	cs := client.Kubernetes.(*fake.Clientset)
	scales := installRecoveryScaleReactor(t, client)
	cs.PrependReactor("create", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		pod := action.(ktesting.CreateAction).GetObject().(*corev1.Pod).DeepCopy()
		require.Equal(t, "node-1", pod.Spec.NodeName)
		require.True(t, pod.Spec.HostNetwork, "recovery must not depend on the broken OVN Pod network")
		require.Equal(t, "registry.example/kube-ovn:test", pod.Spec.Containers[0].Image)
		require.Equal(t, "/custom/ovn", pod.Spec.Volumes[0].HostPath.Path)
		require.Equal(t, corev1.HostPathDirectory, *pod.Spec.Volumes[0].HostPath.Type)
		require.Equal(t, "/etc/ovn", pod.Spec.Containers[0].VolumeMounts[0].MountPath)
		require.False(t, *pod.Spec.AutomountServiceAccountToken)
		pod.UID = types.UID("helper-uid")
		pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		require.NoError(t, cs.Tracker().Add(pod))
		return true, pod, nil
	})
	executor.run = func(_ context.Context, target Target, argv []string, _ Streams) error {
		require.Equal(t, "recovery", target.Container)
		require.Equal(t, []string{"test", "-f", "/etc/ovn/ovnnb_db.db"}, argv)
		return errors.New("database file missing")
	}
	require.ErrorContains(t, app.restore(t.Context(), client, deployment, record), "recovery stopped at planned")
	require.Empty(t, *scales, "helper preflight must finish before central stops")
	var deleted bool
	for _, action := range cs.Actions() {
		if action.Matches("delete", "pods") {
			options := action.(ktesting.DeleteAction).GetDeleteOptions()
			require.Equal(t, types.UID("helper-uid"), *options.Preconditions.UID)
			deleted = true
		}
	}
	require.True(t, deleted, "failed helpers must be cleaned up by UID")
}

func installRecoveryScaleReactor(t *testing.T, client *Client) *[]int32 {
	t.Helper()
	cs := client.Kubernetes.(*fake.Clientset)
	scales := new([]int32)
	cs.PrependReactor("get", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}
		return true, &autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: 2}}, nil
	})
	cs.PrependReactor("update", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}
		scale := action.(ktesting.UpdateAction).GetObject().(*autoscalingv1.Scale)
		*scales = append(*scales, scale.Spec.Replicas)
		if scale.Spec.Replicas == 2 {
			pod := readyPod("central-new", "node-1", "ovn-central", map[string]string{"app": "ovn-central", "ovn-nb-leader": "true", "ovn-sb-leader": "true", "ovn-northd-leader": "true"})
			if err := cs.Tracker().Add(pod); err != nil {
				return true, nil, err
			}
		}
		return true, scale, nil
	})
	return scales
}

func TestRecoveryStagesStopWithoutRestartingOnFailure(t *testing.T) {
	for _, helpers := range []bool{false, true} {
		for _, failCommand := range []string{"mkdir", "cp", "cluster-to-standalone", "mv", "none"} {
			t.Run(fmt.Sprintf("%s/helpers=%t", failCommand, helpers), func(t *testing.T) {
				t.Chdir(t.TempDir())
				app, client, executor, deployment := recoveryApplication(t)
				_, record, err := client.planRecovery(t.Context(), "node-1")
				require.NoError(t, err)
				created := make(map[string]types.UID)
				cs := client.Kubernetes.(*fake.Clientset)
				if helpers {
					deployment.Spec.Template.Spec.Containers[0].Image = "registry.example/kube-ovn:test"
					for i := range record.Targets {
						record.Targets[i].Container = "recovery"
						record.Targets[i].Pod = fmt.Sprintf("ko-recovery-%s-%d", record.ID, i)
					}
					cs.PrependReactor("create", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
						pod := action.(ktesting.CreateAction).GetObject().(*corev1.Pod).DeepCopy()
						require.True(t, pod.Spec.HostNetwork)
						pod.UID = types.UID("uid-" + pod.Name)
						created[pod.Name] = pod.UID
						pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
						require.NoError(t, cs.Tracker().Add(pod))
						return true, pod, nil
					})
				}
				scales := installRecoveryScaleReactor(t, client)
				executor.calls = nil
				original := executor.run
				failed := false
				executor.run = func(ctx context.Context, target Target, argv []string, s Streams) error {
					require.False(t, failed, "execution continued after an injected failure")
					if argv[0] == failCommand || len(argv) > 1 && argv[1] == failCommand {
						failed = true
						return errors.New("injected failure")
					}
					return original(ctx, target, argv, s)
				}
				err = app.restore(t.Context(), client, deployment, record)
				for _, action := range cs.Actions() {
					if action.Matches("delete", "pods") {
						deletion := action.(ktesting.DeleteAction)
						require.Equal(t, created[deletion.GetName()], *deletion.GetDeleteOptions().Preconditions.UID)
						delete(created, deletion.GetName())
					}
				}
				require.Empty(t, created, "all helpers must be removed on success or failure")
				if failCommand == "none" {
					require.NoError(t, err)
					require.Equal(t, []int32{0, 2}, *scales)
					require.Equal(t, "completed", record.Stage)
				} else {
					require.ErrorContains(t, err, "injected failure")
					require.ErrorContains(t, err, "preserve kubectl-ko-recovery-")
					require.Equal(t, []int32{0}, *scales, "a failed restore must not restart partially replaced databases")
				}
				data, readErr := os.ReadFile("kubectl-ko-recovery-" + record.ID + ".json")
				require.NoError(t, readErr)
				require.Contains(t, string(data), record.Stage)
				if failCommand == "none" {
					var backups, moves int
					for _, call := range executor.calls {
						if call.argv[0] == "cp" && strings.Contains(call.argv[len(call.argv)-1], ".original") {
							backups++
						}
						if call.argv[0] == "mv" {
							require.Equal(t, 4, backups, "all originals must be copied before any member is replaced")
							moves++
						}
					}
					require.Equal(t, 8, moves, "both databases and their RAFT headers must be archived on each node")
				}
			})
		}
	}
}
