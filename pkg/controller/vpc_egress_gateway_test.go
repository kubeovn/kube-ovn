package controller

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	stdruntime "runtime"
	"strings"
	"testing"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/keymutex"
	"k8s.io/utils/set"

	mockovs "github.com/kubeovn/kube-ovn/mocks/pkg/ovs"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func requireRecorderEvent(t *testing.T, recorder *record.FakeRecorder) string {
	t.Helper()
	select {
	case event := <-recorder.Events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return ""
	}
}

func TestRecordVpcEgressGatewayEvent(t *testing.T) {
	recorder := record.NewFakeRecorder(1)
	c := &Controller{recorder: recorder}
	gw := &kubeovnv1.VpcEgressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "egress-gw",
			Namespace: "default",
		},
	}

	c.recordVpcEgressGatewayEvent(gw, corev1.EventTypeWarning, "ReconcileWorkloadFailed", "boom")

	require.Equal(t, "Warning ReconcileWorkloadFailed boom", requireRecorderEvent(t, recorder))
}

func TestRecordVpcEgressGatewayError(t *testing.T) {
	tests := []struct {
		reason string
		err    error
	}{
		{reason: "UpdateStatusFailed", err: errors.New("status update failed")},
		{reason: "GetVpcFailed", err: errors.New("vpc lookup failed")},
		{reason: "GetPodSelectorFailed", err: errors.New("selector invalid")},
		{reason: "ListWorkloadPodsFailed", err: errors.New("pod list failed")},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			recorder := record.NewFakeRecorder(1)
			c := &Controller{recorder: recorder}
			gw := &kubeovnv1.VpcEgressGateway{}

			err := c.recordVpcEgressGatewayError(gw, tt.reason, tt.err)

			require.ErrorIs(t, err, tt.err)
			require.Equal(t, "Warning "+tt.reason+" "+tt.err.Error(), requireRecorderEvent(t, recorder))
		})
	}
}

func TestRecordVpcEgressGatewayKeyError(t *testing.T) {
	recorder := record.NewFakeRecorder(1)
	c := &Controller{recorder: recorder}
	sourceErr := errors.New("cache unavailable")

	err := c.recordVpcEgressGatewayKeyError("default", "egress-gw", "GetVpcEgressGatewayFailed", sourceErr)

	require.ErrorIs(t, err, sourceErr)
	require.Equal(t, "Warning GetVpcEgressGatewayFailed cache unavailable", requireRecorderEvent(t, recorder))
}

func TestVpcEgressGatewayReadyConditionChanged(t *testing.T) {
	gw := &kubeovnv1.VpcEgressGateway{}
	gw.Generation = 2
	gw.Status.Conditions.SetCondition(kubeovnv1.Ready, corev1.ConditionFalse, "Processing", "waiting", gw.Generation)

	require.False(t, vpcEgressGatewayReadyConditionChanged(gw, corev1.ConditionFalse, "Processing", "waiting"))
	require.True(t, vpcEgressGatewayReadyConditionChanged(gw, corev1.ConditionFalse, "Processing", "still waiting"))
	require.True(t, vpcEgressGatewayReadyConditionChanged(gw, corev1.ConditionTrue, "ReconcileSuccess", ""))
}

func TestSetVpcEgressGatewayNotReadyClearsStaleReady(t *testing.T) {
	gw := &kubeovnv1.VpcEgressGateway{}
	gw.Generation = 2
	gw.Status.Ready = true
	gw.Status.Phase = kubeovnv1.PhaseCompleted
	gw.Status.Conditions.SetReady("ReconcileSuccess", gw.Generation)

	changed := setVpcEgressGatewayNotReady(gw, "ReconcileOVNRoutesFailed", "route failed")

	require.True(t, changed)
	require.False(t, gw.Status.Ready)
	require.Equal(t, kubeovnv1.PhaseProcessing, gw.Status.Phase)
	condition := gw.Status.Conditions.GetCondition(kubeovnv1.Ready)
	require.NotNil(t, condition)
	require.Equal(t, corev1.ConditionFalse, condition.Status)
	require.Equal(t, "ReconcileOVNRoutesFailed", condition.Reason)
	require.Equal(t, "route failed", condition.Message)
}

func TestFailVpcEgressGatewayReconcilePreservesErrors(t *testing.T) {
	reconcileErr := errors.New("route failed")
	statusErr := errors.New("status update failed")
	gw := &kubeovnv1.VpcEgressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "egress-gw",
			Namespace: "default",
		},
		Status: kubeovnv1.VpcEgressGatewayStatus{
			Ready: true,
			Phase: kubeovnv1.PhaseCompleted,
		},
	}
	gw.Status.Conditions.SetReady("ReconcileSuccess", gw.Generation)
	client := kubeovnfake.NewSimpleClientset(gw)
	client.PrependReactor("update", "vpc-egress-gateways", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, statusErr
	})
	c := &Controller{
		config:   &Configuration{KubeOvnClient: client},
		recorder: record.NewFakeRecorder(2),
	}

	err := c.failVpcEgressGatewayReconcile(gw, "ReconcileOVNRoutesFailed", reconcileErr)
	require.ErrorIs(t, err, reconcileErr)
	require.ErrorIs(t, err, statusErr)
	require.False(t, gw.Status.Ready)
	require.Equal(t, kubeovnv1.PhaseProcessing, gw.Status.Phase)
}

func TestFailVpcEgressGatewayReconcileRecordsRepeatedFailure(t *testing.T) {
	reconcileErr := errors.New("route failed")
	gw := &kubeovnv1.VpcEgressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "egress-gw",
			Namespace:  "default",
			Generation: 2,
		},
	}
	gw.Status.Conditions.SetCondition(kubeovnv1.Ready, corev1.ConditionFalse, "ReconcileOVNRoutesFailed", reconcileErr.Error(), gw.Generation)
	client := kubeovnfake.NewSimpleClientset(gw)
	client.PrependReactor("update", "vpc-egress-gateways", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, action.(ktesting.UpdateAction).GetObject(), nil
	})
	recorder := record.NewFakeRecorder(1)
	c := &Controller{
		config:   &Configuration{KubeOvnClient: client},
		recorder: recorder,
	}

	err := c.failVpcEgressGatewayReconcile(gw, "ReconcileOVNRoutesFailed", reconcileErr)
	require.ErrorIs(t, err, reconcileErr)
	require.Equal(t, "Warning ReconcileOVNRoutesFailed route failed", requireRecorderEvent(t, recorder))
}

func newVpcEgressGatewayDeleteController(t *testing.T, gw *kubeovnv1.VpcEgressGateway) (*Controller, *kubeovnfake.Clientset, *record.FakeRecorder) {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, indexer.Add(gw))
	kubeOvnClient := kubeovnfake.NewSimpleClientset(gw)
	// The generated client uses a hyphenated resource name that the object tracker cannot infer from the kind.
	kubeOvnClient.PrependReactor("update", "vpc-egress-gateways", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, action.(ktesting.UpdateAction).GetObject(), nil
	})

	mockCtrl := gomock.NewController(t)
	mockOvnClient := mockovs.NewMockNbClient(mockCtrl)
	mockOvnClient.EXPECT().FindBFD(gomock.Any()).Return(nil, nil)
	mockOvnClient.EXPECT().DeleteLogicalRouterPolicies(util.DefaultVpc, -1, gomock.Any()).Return(nil)
	mockOvnClient.EXPECT().DeletePortGroup(gomock.Any()).Return(nil)
	mockOvnClient.EXPECT().DeleteAddressSet(gomock.Any()).Return(nil).Times(2)
	recorder := record.NewFakeRecorder(2)

	c := &Controller{
		config: &Configuration{
			ClusterRouter: util.DefaultVpc,
			KubeOvnClient: kubeOvnClient,
		},
		OVNNbClient:              mockOvnClient,
		recorder:                 recorder,
		vpcEgressGatewayKeyMutex: keymutex.NewHashed(0),
		vpcEgressGatewayLister:   kubeovnlister.NewVpcEgressGatewayLister(indexer),
	}
	return c, kubeOvnClient, recorder
}

func TestHandleDelVpcEgressGatewayReturnsFinalizerUpdateError(t *testing.T) {
	updateErr := errors.New("update failed")
	gw := &kubeovnv1.VpcEgressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "egress-gw",
			Namespace:  "default",
			Finalizers: []string{util.KubeOVNControllerFinalizer},
		},
	}
	c, kubeOvnClient, recorder := newVpcEgressGatewayDeleteController(t, gw)
	kubeOvnClient.PrependReactor("update", "vpc-egress-gateways", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, updateErr
	})

	err := c.handleDelVpcEgressGateway("default/egress-gw")
	require.ErrorIs(t, err, updateErr)
	require.Equal(t, "Warning DeleteFailed failed to remove finalizer from vpc-egress-gateway default/egress-gw: update failed", requireRecorderEvent(t, recorder))
}

func TestHandleDelVpcEgressGatewayDoesNotRecordSuccessWithoutFinalizer(t *testing.T) {
	gw := &kubeovnv1.VpcEgressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "egress-gw",
			Namespace: "default",
		},
	}
	c, _, recorder := newVpcEgressGatewayDeleteController(t, gw)

	require.NoError(t, c.handleDelVpcEgressGateway("default/egress-gw"))
	select {
	case event := <-recorder.Events:
		t.Fatalf("unexpected event %q", event)
	default:
	}
}

func TestHandleDelVpcEgressGatewayRecordsSuccessAfterFinalizerUpdate(t *testing.T) {
	gw := &kubeovnv1.VpcEgressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "egress-gw",
			Namespace:  "default",
			Finalizers: []string{util.KubeOVNControllerFinalizer},
		},
	}
	c, _, recorder := newVpcEgressGatewayDeleteController(t, gw)

	require.NoError(t, c.handleDelVpcEgressGateway("default/egress-gw"))
	require.Equal(t, "Normal DeleteSuccess VpcEgressGateway default/egress-gw deleted successfully", requireRecorderEvent(t, recorder))
}

func TestVpcEgressGatewayContainerBFDDDefaultResources(t *testing.T) {
	container := genVpcEgressGatewayBFDDContainer("kube-ovn", "10.255.255.255", 100, 100, 5, false)

	require.Equal(t, "200m", container.Resources.Requests.Cpu().String())
	require.Equal(t, "300m", container.Resources.Limits.Cpu().String())
	require.Equal(t, "50Mi", container.Resources.Requests.Memory().String())
	require.Equal(t, "64Mi", container.Resources.Limits.Memory().String())
	ephemeralStorage := container.Resources.Limits[corev1.ResourceEphemeralStorage]
	require.Equal(t, "1Gi", ephemeralStorage.String())

	require.NotNil(t, container.StartupProbe)
	require.NotNil(t, container.LivenessProbe)
	require.NotNil(t, container.ReadinessProbe)
	require.Equal(t, []string{vegBFDDSupervisorBin, "run"}, container.Command)
	require.Equal(t, []string{vegBFDDSupervisorBin, "live"}, container.StartupProbe.Exec.Command)
	require.EqualValues(t, 30, container.StartupProbe.FailureThreshold)
	require.Equal(t, []string{vegBFDDSupervisorBin, "live"}, container.LivenessProbe.Exec.Command)
	require.EqualValues(t, 10, container.LivenessProbe.TimeoutSeconds)
	require.Equal(t, []string{vegBFDDSupervisorBin, "live"}, container.ReadinessProbe.Exec.Command)
	require.EqualValues(t, 10, container.ReadinessProbe.TimeoutSeconds)
	require.Contains(t, container.VolumeMounts, corev1.VolumeMount{Name: vegBFDDStateVolume, MountPath: vegBFDDStateDir})
	require.Contains(t, container.Ports, corev1.ContainerPort{Name: "metrics", ContainerPort: 10669, Protocol: corev1.ProtocolTCP})
}

func TestVpcEgressGatewayBFDDRuntimeProbeTransport(t *testing.T) {
	t.Run("default VPC uses HTTP", func(t *testing.T) {
		container := genVpcEgressGatewayBFDDContainer("kube-ovn", "10.255.255.255", 100, 100, 5, true)

		require.Equal(t, []string{vegBFDDSupervisorBin, "live"}, container.StartupProbe.Exec.Command)
		for _, probe := range []*corev1.Probe{container.LivenessProbe, container.ReadinessProbe} {
			require.Nil(t, probe.Exec)
			require.Equal(t, "/livez", probe.HTTPGet.Path)
			require.Equal(t, "metrics", probe.HTTPGet.Port.StrVal)
		}
	})

	t.Run("custom VPC falls back to exec", func(t *testing.T) {
		container := genVpcEgressGatewayBFDDContainer("kube-ovn", "10.255.255.255", 100, 100, 5, false)

		for _, probe := range []*corev1.Probe{container.LivenessProbe, container.ReadinessProbe} {
			require.Nil(t, probe.HTTPGet)
			require.Equal(t, []string{vegBFDDSupervisorBin, "live"}, probe.Exec.Command)
		}
	})
}

func TestConfigureVpcEgressGatewayBFDWorkload(t *testing.T) {
	deploy := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{Name: "sidecar"}, {Name: "gateway"}},
		InitContainers: []corev1.Container{
			{Name: "other", Command: []string{"bash", "-c", "unchanged"}},
			{Name: "init", Command: []string{"bash", "-c", "old"}},
		},
	}}}}
	container := genVpcEgressGatewayBFDDContainer("kube-ovn", "10.255.255.255", 100, 100, 3, false)

	require.NoError(t, configureVpcEgressGatewayBFDWorkload(deploy, container))

	require.Equal(t, "sidecar", deploy.Spec.Template.Spec.Containers[0].Name)
	require.Equal(t, "bfdd", deploy.Spec.Template.Spec.Containers[1].Name)
	require.Equal(t, "unchanged", deploy.Spec.Template.Spec.InitContainers[0].Command[2])
	require.Equal(t, vegBFDInitCommand, deploy.Spec.Template.Spec.InitContainers[1].Command[2])
	require.Contains(t, deploy.Spec.Template.Spec.InitContainers[1].VolumeMounts,
		corev1.VolumeMount{Name: vegBFDDStateVolume, MountPath: vegBFDDStateDir})
	require.Contains(t, deploy.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: vegBFDDStateVolume,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})
	require.EqualValues(t, 30, *deploy.Spec.Template.Spec.TerminationGracePeriodSeconds)
	resources := corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}}
	require.NoError(t, setVpcEgressGatewayWorkloadResources(deploy, resources))
	require.Empty(t, deploy.Spec.Template.Spec.Containers[0].Resources)
	require.Equal(t, resources, deploy.Spec.Template.Spec.Containers[1].Resources)

	require.Error(t, configureVpcEgressGatewayBFDWorkload(&appsv1.Deployment{}, container))
	require.Error(t, setVpcEgressGatewayWorkloadResources(&appsv1.Deployment{}, resources))
}

func TestOpenBFDDControlHardeningPatch(t *testing.T) {
	_, filename, _, ok := stdruntime.Caller(0)
	if !ok {
		t.Fatal("failed to get current filename")
	}
	imagesDir := filepath.Join(filepath.Dir(filename), "..", "..", "dist", "images")

	startScript, err := os.ReadFile(filepath.Join(imagesDir, "start-bfdd.sh"))
	require.NoError(t, err)
	require.NotContains(t, string(startScript), "trap '' PIPE", "SIGPIPE handling must be limited to OpenBFDD control replies")

	patchName := "OpenBFDD-control-hardening.patch"
	patchContent, err := os.ReadFile(filepath.Join(imagesDir, patchName))
	require.NoError(t, err)
	require.Contains(t, string(patchContent), "ControlCommandTimeoutMs = 500")
	require.Contains(t, string(patchContent), "ControlRequestTimeoutMs = 5000")
	require.Contains(t, string(patchContent), "ControlOperationTimeoutMs = 4000")
	require.Contains(t, string(patchContent), "connectedSocket.SetBlocking(false)")
	require.Contains(t, string(patchContent), "m_replySocket.SendStream")
	require.Contains(t, string(patchContent), "MSG_DONTWAIT | MSG_NOSIGNAL")
	require.Contains(t, string(patchContent), "m_replyFailed")
	require.Contains(t, string(patchContent), "m_operations.pop_back()")
	require.Contains(t, string(patchContent), "waitCondition(NULL)")
	require.Contains(t, string(patchContent), "TimeSpec::MonoNow() >= m_requestDeadline")
	require.Contains(t, string(patchContent), "TimeSpec::MonoNow() >= deadline")
	require.Contains(t, string(patchContent), "struct StatusOperation")
	require.Contains(t, string(patchContent), "references(2)")
	require.Contains(t, string(patchContent), "condition(true, true)")
	require.Contains(t, string(patchContent), "pthread_condattr_setclock(&attributes, CLOCK_MONOTONIC)")
	require.Contains(t, string(patchContent), "runStatusOperation")
	require.Contains(t, string(patchContent), `failurePrefix = "Unable to complete "`)
	require.Contains(t, string(patchContent), "operation_timeout")
	require.Contains(t, string(patchContent), "without a reply")
	require.Contains(t, string(patchContent), "Slow or failed control request")

	dockerfile, err := os.ReadFile(filepath.Join(imagesDir, "Dockerfile.base"))
	require.NoError(t, err)
	require.Contains(t, string(dockerfile), "git apply /usr/src/OpenBFDD-compile.patch")
	require.NotContains(t, string(dockerfile), "git apply --no-apply /usr/src/OpenBFDD-compile.patch")
	require.Contains(t, string(dockerfile), "ADD "+patchName+" /usr/src/")
	require.Contains(t, string(dockerfile), "git apply --unidiff-zero /usr/src/"+patchName)
	require.NotContains(t, string(dockerfile), "git apply --no-apply /usr/src/"+patchName)
}

func TestBFDDHealthcheck(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is required to test the BFD health check")
	}

	_, filename, _, ok := stdruntime.Caller(0)
	if !ok {
		t.Fatal("failed to get current filename")
	}
	healthcheckPath := filepath.Join(filepath.Dir(filename), "..", "..", "dist", "images", "bfdd-healthcheck.sh")

	tests := []struct {
		name           string
		statusOutput   string
		statusExitCode string
		wantHealthy    bool
		wantAllowed    []string
	}{
		{
			name:         "existing session is healthy",
			statusOutput: "There are 1 sessions:",
			wantHealthy:  true,
		},
		{
			name:         "zero sessions fails without mutating peer configuration",
			statusOutput: "There are 0 sessions:",
		},
		{
			name:           "status failure fails without mutating peer configuration",
			statusOutput:   "control socket unavailable",
			statusExitCode: "2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := t.TempDir()
			binDir := filepath.Join(testDir, "bin")
			require.NoError(t, os.Mkdir(binDir, 0o755))

			allowLog := filepath.Join(testDir, "allow.log")
			mockControl := filepath.Join(binDir, "bfdd-control")
			mockScript := `#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  status)
    printf '%s\n' "${STATUS_OUTPUT:-}"
    exit "${STATUS_EXIT_CODE:-0}"
    ;;
  allow)
    printf '%s\n' "${2:-}" >> "${ALLOW_LOG}"
    ;;
  *)
    exit 1
    ;;
esac
`
			require.NoError(t, os.WriteFile(mockControl, []byte(mockScript), 0o755))

			cmd := exec.Command(bash, healthcheckPath) // #nosec G204 -- path is derived from the test source location
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+":"+os.Getenv("PATH"),
				"STATUS_OUTPUT="+tt.statusOutput,
				"STATUS_EXIT_CODE="+tt.statusExitCode,
				"ALLOW_LOG="+allowLog,
				"BFD_PEER_IPS=10.0.0.1,fd00::1",
			)
			output, err := cmd.CombinedOutput()
			if tt.wantHealthy {
				require.NoError(t, err, "health check output: %s", output)
			} else {
				require.Error(t, err, "health check output: %s", output)
			}

			var allowed []string
			content, err := os.ReadFile(allowLog)
			switch {
			case err == nil:
				allowed = strings.Fields(string(content))
			case os.IsNotExist(err):
			default:
				t.Fatalf("failed to read allowed peer log: %v", err)
			}
			require.Equal(t, tt.wantAllowed, allowed)
		})
	}
}

func TestLocalGatewayPolicyBFDSessionsSkipsEmptySession(t *testing.T) {
	require.Empty(t, localGatewayPolicyBFDSessions(map[string]string{"10.244.10.4": ""}, "10.244.10.4"))
	require.Equal(t, set.New("bfd-1"), localGatewayPolicyBFDSessions(map[string]string{"10.244.10.4": "bfd-1"}, "10.244.10.4"))
}

func newVegWorkloadPod(name, node, podIP, attachment string) *corev1.Pod {
	annotations := map[string]string{}
	if attachment != "" {
		annotations[nadv1.NetworkStatusAnnot] = attachment
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			NodeName: node,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIPs: []corev1.PodIP{{
				IP: podIP,
			}},
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
}

func TestCollectVpcEgressGatewayWorkloadStatus(t *testing.T) {
	attachmentNetwork := "default/eth1"
	readyAttachment := `[{"name":"default/eth1","ips":["172.17.1.10"]}]`

	tests := []struct {
		name              string
		pods              []*corev1.Pod
		wantInternalIPs   []string
		wantExternalIPs   []string
		wantNodes         []string
		wantNotReadyCount int
	}{
		{
			name: "all workload pods have attachment network",
			pods: []*corev1.Pod{
				newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment),
				newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"default/eth1","ips":["172.17.1.11"]}]`),
			},
			wantInternalIPs: []string{"10.16.1.10", "10.16.1.11"},
			wantExternalIPs: []string{"172.17.1.10", "172.17.1.11"},
			wantNodes:       []string{"node-1", "node-2"},
		},
		{
			name: "one workload pod misses attachment network",
			pods: []*corev1.Pod{
				newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment),
				newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"kube-ovn","ips":["10.16.1.11"]}]`),
			},
			wantInternalIPs:   []string{"10.16.1.10"},
			wantExternalIPs:   []string{"172.17.1.10"},
			wantNodes:         []string{"node-1"},
			wantNotReadyCount: 2,
		},
		{
			name: "one workload pod has attachment network without ip",
			pods: []*corev1.Pod{
				newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment),
				newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"default/eth1","ips":[]}]`),
			},
			wantInternalIPs:   []string{"10.16.1.10"},
			wantExternalIPs:   []string{"172.17.1.10"},
			wantNodes:         []string{"node-1"},
			wantNotReadyCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &kubeovnv1.VpcEgressGateway{
				Spec: kubeovnv1.VpcEgressGatewaySpec{
					Replicas: 2,
				},
			}

			_, _, messages := collectVpcEgressGatewayWorkloadStatus(gw, tt.pods, attachmentNetwork)

			require.Equal(t, tt.wantInternalIPs, gw.Status.InternalIPs)
			require.Equal(t, tt.wantExternalIPs, gw.Status.ExternalIPs)
			require.Equal(t, tt.wantNodes, gw.Status.Workload.Nodes)
			require.Len(t, messages, tt.wantNotReadyCount)
		})
	}
}

func TestCollectVpcEgressGatewayWorkloadStatusRetainsNetworkedNotReadyPod(t *testing.T) {
	const attachmentNetwork = "default/eth1"
	readyPod := newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", `[{"name":"default/eth1","ips":["172.17.1.10"]}]`)
	notReadyPod := newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"default/eth1","ips":["172.17.1.11"]}]`)
	notReadyPod.Status.Conditions[0].Status = corev1.ConditionFalse
	gw := &kubeovnv1.VpcEgressGateway{Spec: kubeovnv1.VpcEgressGatewaySpec{Replicas: 2}}

	ipv4, ipv6, messages := collectVpcEgressGatewayWorkloadStatus(gw, []*corev1.Pod{readyPod, notReadyPod}, attachmentNetwork)

	require.Equal(t, []string{"10.16.1.10", "10.16.1.11"}, gw.Status.InternalIPs)
	require.Equal(t, []string{"172.17.1.10", "172.17.1.11"}, gw.Status.ExternalIPs)
	require.Equal(t, []string{"node-1", "node-2"}, gw.Status.Workload.Nodes)
	require.Equal(t, map[string]string{"node-1": "10.16.1.10", "node-2": "10.16.1.11"}, ipv4)
	require.Empty(t, ipv6)
	require.Len(t, messages, 1)
}
