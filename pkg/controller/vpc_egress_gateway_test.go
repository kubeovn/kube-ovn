package controller

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/set"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestVpcEgressGatewayContainerBFDDDefaultResources(t *testing.T) {
	container := vpcEgressGatewayContainerBFDD("kube-ovn", "10.255.255.255", 100, 100, 5)

	require.Equal(t, "200m", container.Resources.Requests.Cpu().String())
	require.Equal(t, "200m", container.Resources.Limits.Cpu().String())
	require.Equal(t, "50Mi", container.Resources.Requests.Memory().String())
	require.Equal(t, "50Mi", container.Resources.Limits.Memory().String())
	ephemeralStorage := container.Resources.Limits[corev1.ResourceEphemeralStorage]
	require.Equal(t, "1Gi", ephemeralStorage.String())

	require.NotNil(t, container.StartupProbe)
	require.NotNil(t, container.LivenessProbe)
	require.NotNil(t, container.ReadinessProbe)
	require.Equal(t, []string{"bash", "/kube-ovn/bfdd-prestart.sh"}, container.StartupProbe.Exec.Command)
	require.Equal(t, []string{"bash", "/kube-ovn/bfdd-healthcheck.sh"}, container.LivenessProbe.Exec.Command)
	require.EqualValues(t, 3, container.LivenessProbe.TimeoutSeconds)
	require.Equal(t, []string{"bfdd-control", "status"}, container.ReadinessProbe.Exec.Command)
}

func TestBFDDHealthcheck(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is required to test the BFD health check")
	}

	_, filename, _, ok := runtime.Caller(0)
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
