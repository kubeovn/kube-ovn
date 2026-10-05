package ko

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"
)

func TestCleanupIsReverseOrderedUIDScopedAndSurvivesCancellation(t *testing.T) {
	cs := fake.NewClientset()
	client := &Client{Kubernetes: cs, Namespace: "ovn-system"}
	run := &resourceRun{client: client, resources: []ownedResource{{kind: "pod", name: "a", uid: "pod-original"}, {kind: "service", name: "b", uid: "service-original"}, {kind: "daemonset", name: "c", uid: "ds-original"}}}
	var deleted []string
	cs.PrependReactor("delete", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		deletion := action.(ktesting.DeleteAction)
		require.NotNil(t, deletion.GetDeleteOptions().Preconditions)
		deleted = append(deleted, deletion.GetName()+":"+string(*deletion.GetDeleteOptions().Preconditions.UID))
		if deletion.GetName() == "b" {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "services"}, "b", errors.New("UID changed"))
		}
		return true, nil, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := run.cleanup(ctx)
	require.ErrorContains(t, err, "UID changed")
	require.Equal(t, []string{"c:ds-original", "b:service-original", "a:pod-original"}, deleted)
	require.ErrorContains(t, err, "service/b (UID service-original)")
}

func TestCleanupNeverDeletesAnUncreatedObject(t *testing.T) {
	cs := fake.NewClientset(&corev1.Pod{Name: "existing", Namespace: "ovn-system", UID: "other"})
	run := &resourceRun{client: &Client{Kubernetes: cs, Namespace: "ovn-system"}}
	_, err := run.createPod(t.Context(), &corev1.Pod{Name: "existing"})
	require.True(t, apierrors.IsAlreadyExists(err))
	require.NoError(t, run.cleanup(t.Context()))
	pod, err := cs.CoreV1().Pods("ovn-system").Get(t.Context(), "existing", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, types.UID("other"), pod.UID)
	for _, action := range cs.Actions() {
		require.NotEqual(t, "delete", action.GetVerb())
	}
}

func TestDaemonSetWaitDoesNotAcceptPreviousGeneration(t *testing.T) {
	ds := &appsv1.DaemonSet{Generation: 2, Status: appsv1.DaemonSetStatus{ObservedGeneration: 1, DesiredNumberScheduled: 1, CurrentNumberScheduled: 1, UpdatedNumberScheduled: 1, NumberReady: 1, NumberAvailable: 1}}
	require.False(t, daemonSetReady(ds))
	ds.Status.ObservedGeneration = 2
	require.True(t, daemonSetReady(ds))
	ds.Status.UpdatedNumberScheduled = 0
	require.False(t, daemonSetReady(ds))
}

func TestSubnetProbeAuthenticatesWithAutomountDisabledServiceAccount(t *testing.T) {
	account := &corev1.ServiceAccount{Name: "kube-ovn-app", Namespace: "ovn-system", AutomountServiceAccountToken: new(false)}
	pinger := &appsv1.DaemonSet{Name: "kube-ovn-pinger", Namespace: account.Namespace, Spec: appsv1.DaemonSetSpec{
		Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "pinger", Image: "probe-image"}}}},
	}}
	cs := fake.NewClientset(account, pinger)
	run := &resourceRun{client: &Client{Kubernetes: cs, Namespace: account.Namespace}, id: "authenticated"}
	cs.PrependReactor("create", "daemonsets", func(action ktesting.Action) (bool, runtime.Object, error) {
		ds := action.(ktesting.CreateAction).GetObject().(*appsv1.DaemonSet).DeepCopy()
		ds.Namespace = action.GetNamespace()
		spec := ds.Spec.Template.Spec
		require.Equal(t, account.Name, spec.ServiceAccountName)
		mount := account.AutomountServiceAccountToken
		if spec.AutomountServiceAccountToken != nil {
			mount = spec.AutomountServiceAccountToken
		}
		if !*mount {
			return true, nil, errors.New("probe cannot initialize an in-cluster client: token mount is disabled")
		}
		ds.UID = "authenticated-probe"
		ds.Status = appsv1.DaemonSetStatus{DesiredNumberScheduled: 1, CurrentNumberScheduled: 1, UpdatedNumberScheduled: 1, NumberReady: 1, NumberAvailable: 1}
		require.NoError(t, cs.Tracker().Add(ds))
		return true, ds, nil
	})
	require.NoError(t, run.subnetProbe(t.Context(), "subnet", diagnosticOptions{tcpPort: "8100", udpPort: "8101"}))
	require.Equal(t, []ownedResource{{kind: "daemonset", name: "ko-subnet-authenticated", uid: "authenticated-probe"}}, run.resources)
	stored, err := cs.CoreV1().ServiceAccounts(account.Namespace).Get(t.Context(), account.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, new(false), stored.AutomountServiceAccountToken)
}

func TestSubnetProbeProvidesPrivatePingerLogDirectory(t *testing.T) {
	pinger := &appsv1.DaemonSet{Name: "kube-ovn-pinger", Namespace: "ovn-system", Spec: appsv1.DaemonSetSpec{
		Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "pinger", Image: "probe-image"}}}},
	}}
	cs := fake.NewClientset(pinger)
	run := &resourceRun{client: &Client{Kubernetes: cs, Namespace: pinger.Namespace}, id: "logs"}
	cs.PrependReactor("create", "daemonsets", func(action ktesting.Action) (bool, runtime.Object, error) {
		ds := action.(ktesting.CreateAction).GetObject().(*appsv1.DaemonSet).DeepCopy()
		ds.Namespace = action.GetNamespace()
		for _, mount := range ds.Spec.Template.Spec.Containers[0].VolumeMounts {
			if mount.MountPath != "/var/log/kube-ovn" || mount.ReadOnly {
				continue
			}
			for _, volume := range ds.Spec.Template.Spec.Volumes {
				if volume.Name == mount.Name && volume.EmptyDir != nil {
					ds.UID = "private-logs"
					ds.Status = appsv1.DaemonSetStatus{DesiredNumberScheduled: 1, CurrentNumberScheduled: 1, UpdatedNumberScheduled: 1, NumberReady: 1, NumberAvailable: 1}
					require.NoError(t, cs.Tracker().Add(ds))
					return true, ds, nil
				}
			}
		}
		return true, nil, errors.New("pinger startup cannot create /var/log/kube-ovn/kube-ovn-pinger.log without a private writable directory")
	})
	require.NoError(t, run.subnetProbe(t.Context(), "subnet", diagnosticOptions{tcpPort: "8100", udpPort: "8101"}))
}

func TestDaemonSetFailureReportsOwnedPodLogsAndPreservesError(t *testing.T) {
	ds := &appsv1.DaemonSet{Name: "ko-subnet-owned", Namespace: "ovn-system", UID: "owned-ds", Spec: appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"run": "owned"}},
	}}
	owned := corev1.Pod{Name: "owned-pod", Namespace: ds.Namespace, OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "DaemonSet", Name: ds.Name, UID: ds.UID, Controller: new(true)}}, Status: corev1.PodStatus{
		ContainerStatuses: []corev1.ContainerStatus{{Name: "probe", RestartCount: 2, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}},
	}}
	other := owned.DeepCopy()
	other.Name, other.OwnerReferences[0].UID = "unrelated-pod", "other-ds"
	var logRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var value any
		switch req.URL.Path {
		case "/apis/apps/v1/namespaces/ovn-system/daemonsets":
			value = ds
		case "/apis/apps/v1/namespaces/ovn-system/daemonsets/ko-subnet-owned":
			w.WriteHeader(http.StatusForbidden)
			value = &metav1.Status{Kind: "Status", APIVersion: "v1", Status: "Failure", Reason: metav1.StatusReasonForbidden, Code: 403, Message: "cannot observe readiness"}
		case "/api/v1/namespaces/ovn-system/pods":
			require.Equal(t, "run=owned", req.URL.Query().Get("labelSelector"))
			value = &corev1.PodList{Items: []corev1.Pod{*other, owned}}
		case "/api/v1/namespaces/ovn-system/pods/owned-pod/log":
			logRequests++
			require.Equal(t, "true", req.URL.Query().Get("previous"))
			require.Equal(t, "probe", req.URL.Query().Get("container"))
			require.Equal(t, "4096", req.URL.Query().Get("limitBytes"))
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "pinger startup failed\n", strings.Repeat("x", 10000))
			return
		default:
			t.Errorf("unexpected request %s", req.URL)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		data, err := json.Marshal(value)
		require.NoError(t, err)
		_, err = w.Write(data)
		require.NoError(t, err)
	}))
	defer server.Close()
	cs, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	require.NoError(t, err)
	run := &resourceRun{client: &Client{Kubernetes: cs, Namespace: ds.Namespace}}
	err = run.createDaemonSet(t.Context(), ds)
	require.True(t, apierrors.IsForbidden(err))
	require.ErrorContains(t, err, "cannot observe readiness")
	require.ErrorContains(t, err, "owned-pod/probe")
	require.ErrorContains(t, err, "CrashLoopBackOff")
	require.ErrorContains(t, err, "pinger startup failed")
	require.NotContains(t, err.Error(), "unrelated-pod")
	require.Less(t, len(err.Error()), 5000)
	require.Equal(t, 1, logRequests)
	require.Equal(t, []ownedResource{{kind: "daemonset", name: ds.Name, uid: ds.UID}}, run.resources)
}

func TestCleanupRecoversLostCreateResponseWithoutAdoptingOtherRuns(t *testing.T) {
	for _, owner := range []string{"ours", "other"} {
		t.Run(owner, func(t *testing.T) {
			cs := fake.NewClientset()
			run := &resourceRun{client: &Client{Kubernetes: cs, Namespace: "ovn-system"}, id: "ours"}
			cs.PrependReactor("create", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
				pod := action.(ktesting.CreateAction).GetObject().(*corev1.Pod).DeepCopy()
				pod.Namespace, pod.UID = "ovn-system", "server-uid"
				pod.Labels = map[string]string{"kubeovn.io/ko-run": owner}
				require.NoError(t, cs.Tracker().Add(pod))
				return true, nil, context.DeadlineExceeded
			})
			_, err := run.createPod(t.Context(), &corev1.Pod{Name: "probe"})
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.NoError(t, run.cleanup(t.Context()))
			_, err = cs.CoreV1().Pods("ovn-system").Get(t.Context(), "probe", metav1.GetOptions{})
			if owner == "ours" {
				require.True(t, apierrors.IsNotFound(err))
			} else {
				require.NoError(t, err, "another run's resource must survive")
			}
		})
	}
}
