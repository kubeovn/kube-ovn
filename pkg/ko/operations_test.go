package ko

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	utilexec "k8s.io/client-go/util/exec"
)

func TestDatabaseKickUsesTheRequestedLeaderAndPreservesFailure(t *testing.T) {
	for _, role := range []string{"nb", "sb"} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dryRun=%t", role, dryRun), func(t *testing.T) {
				app, executor, out, _ := testApplication(t,
					readyPod("nb-leader", "a", "ovn-central", map[string]string{"ovn-nb-leader": "true"}),
					readyPod("sb-leader", "b", "ovn-central", map[string]string{"ovn-sb-leader": "true"}),
				)
				failure := utilexec.CodeExitError{Err: errors.New("member removal failed"), Code: 42}
				executor.run = func(_ context.Context, _ Target, _ []string, _ Streams) error { return failure }
				args := []string{"db", role, "kick", "ffffffff"}
				if dryRun {
					args = append(args, "--dry-run")
				}
				err := app.Execute(t.Context(), args)
				database := "OVN_Northbound"
				if role == "sb" {
					database = "OVN_Southbound"
				}
				argv := []string{"ovn-appctl", "-t", "/var/run/ovn/ovn" + role + "_db.ctl", "cluster/kick", database, "ffffffff"}
				if dryRun {
					require.NoError(t, err)
					require.Empty(t, executor.calls, "dry-run must not execute member removal")
					require.Equal(t, fmt.Sprintf("ovn-system/%s-leader: %q\n", role, argv), out.String())
					return
				}
				require.ErrorIs(t, err, failure)
				require.Equal(t, 42, ExitCode(err))
				require.Len(t, executor.calls, 1, "failed member removal must not be replayed")
				require.Equal(t, role+"-leader", executor.calls[0].target.Pod)
				require.Equal(t, "ovn-central", executor.calls[0].target.Container)
				require.Equal(t, argv, executor.calls[0].argv)
			})
		}
	}
}

func TestComponentFreeDatabaseKickDryRunDoesNotRequireAgent(t *testing.T) {
	for _, role := range []string{"nb", "sb"} {
		t.Run(role, func(t *testing.T) {
			app, executor, out, _ := testApplication(t,
				readyPod("nb-leader", "a", "ovn-central", map[string]string{"ovn-nb-leader": "true"}),
				readyPod("sb-leader", "b", "ovn-central", map[string]string{"ovn-sb-leader": "true"}),
			)
			client, err := app.newClient()
			require.NoError(t, err)
			client.ComponentFree = true
			require.NoError(t, app.Execute(t.Context(), []string{"db", role, "kick", "ffffffff", "--dry-run"}))
			database := "OVN_Northbound"
			if role == "sb" {
				database = "OVN_Southbound"
			}
			argv := []string{"ovn-appctl", "-t", "/var/run/ovn/ovn" + role + "_db.ctl", "cluster/kick", database, "ffffffff"}
			require.Equal(t, fmt.Sprintf("ovn-system/%s-leader: %q\n", role, argv), out.String())
			require.Empty(t, executor.calls, "planning must not execute member removal")
		})
	}
}

func TestComponentFreeDatabaseKickExecutesOnLeaderNodeAgent(t *testing.T) {
	for _, role := range []string{"nb", "sb"} {
		t.Run(role, func(t *testing.T) {
			app, executor, _, _ := testApplication(t,
				readyPod("nb-leader", "a", "ovn-central", map[string]string{"ovn-nb-leader": "true"}),
				readyPod("sb-leader", "b", "ovn-central", map[string]string{"ovn-sb-leader": "true"}),
				readyPod("agent-a", "a", "agent", map[string]string{"app": "kubectl-ko-node-agent"}),
				readyPod("agent-b", "b", "agent", map[string]string{"app": "kubectl-ko-node-agent"}),
			)
			client, err := app.newClient()
			require.NoError(t, err)
			client.ComponentFree = true
			failure := utilexec.CodeExitError{Err: errors.New("member removal failed"), Code: 42}
			executor.run = func(_ context.Context, _ Target, _ []string, _ Streams) error { return failure }
			err = app.Execute(t.Context(), []string{"db", role, "kick", "ffffffff"})
			require.ErrorIs(t, err, failure)
			require.Equal(t, 42, ExitCode(err))
			require.Len(t, executor.calls, 1, "failed member removal must not be replayed")
			node, database := "a", "OVN_Northbound"
			if role == "sb" {
				node, database = "b", "OVN_Southbound"
			}
			require.Equal(t, Target{Namespace: "ovn-system", Pod: "agent-" + node, Container: "agent", Node: node}, executor.calls[0].target)
			require.Equal(t, []string{"ovn-appctl", "-t", "/var/run/ovn/ovn" + role + "_db.ctl", "cluster/kick", database, "ffffffff"}, executor.calls[0].argv)
		})
	}
}

func TestEnvironmentChecksAllRunningCNIsAndPreservesFailures(t *testing.T) {
	pending := readyPod("pending", "c", "cni-server", map[string]string{"app": "kube-ovn-cni"})
	pending.Status.Phase = corev1.PodPending
	terminating := readyPod("terminating", "d", "cni-server", map[string]string{"app": "kube-ovn-cni"})
	terminating.DeletionTimestamp = new(metav1.Now())
	app, executor, out, _ := testApplication(t,
		readyPod("cni-a", "a", "cni-server", map[string]string{"app": "kube-ovn-cni"}),
		readyPod("cni-b", "b", "cni-server", map[string]string{"app": "kube-ovn-cni"}),
		readyPod("other", "e", "other", map[string]string{"app": "kube-ovn-cni"}),
		pending, terminating,
	)
	failures := map[string]error{"a": errors.New("checker unavailable"), "b": errors.New("checker failed")}
	executor.run = func(_ context.Context, target Target, argv []string, _ Streams) error {
		require.Equal(t, "cni-server", target.Container)
		require.Equal(t, []string{"bash", "/kube-ovn/env-check.sh"}, argv)
		return failures[target.Node]
	}
	err := app.Execute(t.Context(), []string{"diagnose", "environment"})
	for node, failure := range failures {
		require.ErrorIs(t, err, failure, "one failed checker must not prevent other nodes from being checked")
		require.Contains(t, out.String(), "Environment check on "+node+"\n")
	}
	require.Len(t, executor.calls, 2)
}

func TestEnvironmentChecksLinuxNodesWithoutCNI(t *testing.T) {
	app, executor, out, _ := testApplication(t,
		&corev1.Node{Name: "a", Labels: map[string]string{corev1.LabelOSStable: "linux"}},
		&corev1.Node{Name: "b", Labels: map[string]string{corev1.LabelOSStable: "linux"}},
		&corev1.Node{Name: "missing", Labels: map[string]string{corev1.LabelOSStable: "linux"}},
		&corev1.Node{Name: "duplicate", Labels: map[string]string{corev1.LabelOSStable: "linux"}},
		&corev1.Node{Name: "windows", Labels: map[string]string{corev1.LabelOSStable: "windows"}},
		readyPod("agent-a", "a", "agent", map[string]string{"app": "kubectl-ko-node-agent"}),
		readyPod("agent-b", "b", "agent", map[string]string{"app": "kubectl-ko-node-agent"}),
		readyPod("agent-duplicate-1", "duplicate", "agent", map[string]string{"app": "kubectl-ko-node-agent"}),
		readyPod("agent-duplicate-2", "duplicate", "agent", map[string]string{"app": "kubectl-ko-node-agent"}),
	)
	client, err := app.newClient()
	require.NoError(t, err)
	client.ComponentFree = true
	failure := errors.New("checker failed")
	executor.run = func(_ context.Context, target Target, argv []string, _ Streams) error {
		require.Equal(t, "agent", target.Container)
		require.Equal(t, []string{"bash", "/kube-ovn/env-check.sh"}, argv)
		if target.Node == "a" {
			return failure
		}
		return nil
	}
	err = app.Execute(t.Context(), []string{"diagnose", "environment"})
	require.ErrorIs(t, err, failure)
	require.ErrorContains(t, err, `node "missing"`)
	require.ErrorContains(t, err, `node "duplicate"`)
	require.Len(t, executor.calls, 2)
	for _, node := range []string{"a", "b"} {
		require.Contains(t, out.String(), "Environment check on "+node+"\n")
	}
	require.NotContains(t, out.String(), "windows")
}

func TestEmbeddedKubeProxyChecksAgentsWithoutCNI(t *testing.T) {
	agent := readyPod("agent", "worker", "agent", map[string]string{"app": "kubectl-ko-node-agent"})
	agent.Spec.HostNetwork = true
	agent.Status.PodIP = "2001:db8::1"
	app, executor, _, _ := testApplication(t, agent,
		&corev1.Node{Name: "worker", Labels: map[string]string{corev1.LabelOSStable: "linux"}},
	)
	client, err := app.newClient()
	require.NoError(t, err)
	client.ComponentFree = true
	require.NoError(t, client.checkKubeProxy(t.Context()))
	require.Len(t, executor.calls, 1)
	require.Equal(t, "agent", executor.calls[0].target.Container)
	require.Equal(t, []string{"curl", "--globoff", "--fail", "--silent", "--show-error", "--max-time", "3", "http://[2001:db8::1]:10256/healthz"}, executor.calls[0].argv)
}

func TestDiagnosticProbeReportsConnectivityFailures(t *testing.T) {
	podA := readyPod("subnet-a", "a", "probe", nil)
	podA.Status.PodIPs = []corev1.PodIP{{IP: "192.0.2.2"}, {IP: "2001:db8::2"}}
	podB := readyPod("subnet-b", "b", "probe", nil)
	podB.Status.PodIPs = []corev1.PodIP{{IP: "192.0.2.3"}, {IP: "2001:db8::3"}}
	app, executor, _, _ := testApplication(t, podA, podB)
	client, err := app.newClient()
	require.NoError(t, err)
	executor.run = func(_ context.Context, _ Target, argv []string, _ Streams) error {
		require.Contains(t, argv, "--exit-code=1")
		require.NotContains(t, argv, "--enable-verbose-conn-check=true", "node TCP/UDP listeners are optional")
		require.Contains(t, argv, "--network-mode=diagnostic")
		require.Contains(t, argv, "--target-ip-ports=tcp-192.0.2.1-1,tcp-192.0.2.2-8100,udp-192.0.2.2-8101,tcp-2001:db8::2-8100,udp-2001:db8::2-8101,tcp-192.0.2.3-8100,udp-192.0.2.3-8101,tcp-2001:db8::3-8100,udp-2001:db8::3-8101")
		return utilexec.CodeExitError{Err: errors.New("connectivity failure"), Code: 1}
	}
	err = app.runDiagnosticProbes(t.Context(), client, []Target{{Namespace: podA.Namespace, Pod: podA.Name, Node: "a"}, {Namespace: podB.Namespace, Pod: podB.Name, Node: "b"}}, "subnet", "tcp-192.0.2.1-1", diagnosticOptions{tcpPort: "8100", udpPort: "8101"})
	require.ErrorContains(t, err, "probe on a")
	require.ErrorContains(t, err, "probe on b")
	require.Len(t, executor.calls, 2, "one failed node must not hide other nodes")
}

func TestComponentFreeDiagnosticsUseIndependentProbeInPingerNetNS(t *testing.T) {
	for _, mode := range []string{"all", "node", "IPPorts"} {
		for _, hostNetwork := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/hostNetwork=%t", mode, hostNetwork), func(t *testing.T) {
				pod := readyPod("pinger", "worker", "pinger", map[string]string{"app": "kube-ovn-pinger"})
				pod.UID = "pinger-uid"
				pod.Spec.HostNetwork = hostNetwork
				pod.Status.PodIP = "192.0.2.2"
				pod.Status.HostIP = "192.0.2.1"
				pod.Spec.ServiceAccountName = "kube-ovn-app"
				pod.Spec.Containers[0].Image = "registry.example/kube-ovn:test"
				pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "log", MountPath: "/var/log/kube-ovn"}}
				pod.Spec.Volumes = []corev1.Volume{{Name: "log", HostPath: &corev1.HostPathVolumeSource{Path: "/var/log/kube-ovn"}}}
				app, executor, _, _ := testApplication(t, pod,
					readyPod("agent-worker", "worker", "agent", map[string]string{"app": "kubectl-ko-node-agent"}))
				client, err := app.newClient()
				require.NoError(t, err)
				client.ComponentFree = true
				cs := client.Kubernetes.(*fake.Clientset)
				cs.PrependReactor("create", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
					probe := action.(ktesting.CreateAction).GetObject().(*corev1.Pod).DeepCopy()
					require.Equal(t, "kubectl-ko-probe", probe.Labels["app"])
					require.Equal(t, pod.Spec.ServiceAccountName, probe.Spec.ServiceAccountName)
					require.True(t, probe.Spec.HostPID)
					require.Equal(t, pod.Spec.Containers[0].Image, probe.Spec.Containers[0].Image)
					logMount := slices.IndexFunc(probe.Spec.Containers[0].VolumeMounts, func(mount corev1.VolumeMount) bool { return mount.MountPath == "/var/log/kube-ovn" })
					require.GreaterOrEqual(t, logMount, 0)
					logVolume := slices.IndexFunc(probe.Spec.Volumes, func(volume corev1.Volume) bool {
						return volume.Name == probe.Spec.Containers[0].VolumeMounts[logMount].Name
					})
					require.GreaterOrEqual(t, logVolume, 0)
					require.NotNil(t, probe.Spec.Volumes[logVolume].EmptyDir, "the probe must not write the component's log directory")
					probe.Namespace = action.GetNamespace()
					probe.UID = "probe-uid"
					probe.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
					require.NoError(t, cs.Tracker().Add(probe))
					return true, probe, nil
				})
				failure := utilexec.CodeExitError{Err: errors.New("connectivity failure"), Code: 42}
				probes := 0
				executor.run = func(_ context.Context, target Target, argv []string, streams Streams) error {
					require.NotEqual(t, "pinger", target.Pod, "diagnostics must never exec in the pinger component")
					if argv[0] == "nsenter" {
						require.Equal(t, "probe", target.Container)
						require.True(t, strings.HasPrefix(target.Pod, "ko-diagnostic-"))
					} else {
						require.Equal(t, "agent-worker", target.Pod)
						require.Equal(t, "agent", target.Container)
					}
					if argv[0] == "ovs-vsctl" && slices.Contains(argv, "Interface") {
						_, err := io.WriteString(streams.Out, `{"headings":["name","external_ids","ofport"],"data":[]}`)
						return err
					}
					if argv[0] == "/kube-ovn/kubectl-ko-node-agent" {
						require.Equal(t, []string{argv[0], "netns", "pinger-uid"}, argv)
						_, err := io.WriteString(streams.Out, "/proc/321/ns/net\n")
						return err
					}
					if argv[0] != "nsenter" {
						return nil
					}
					probes++
					netns := "/proc/321/ns/net"
					if hostNetwork {
						netns = "/proc/1/ns/net"
					}
					require.Equal(t, []string{"nsenter", "--net=" + netns, "--", "env"}, argv[:4])
					for _, value := range []string{"POD_NAME=pinger", "POD_NAMESPACE=ovn-system", "POD_IP=192.0.2.2", "HOST_IP=192.0.2.1", "NODE_NAME=worker", "/kube-ovn/kube-ovn-pinger", "--exit-code=1", "--target-ip-ports=tcp-192.0.2.9-30000"} {
						require.Contains(t, argv, value)
					}
					return failure
				}
				err = app.runDiagnosticProbes(t.Context(), client, []Target{{Namespace: pod.Namespace, Pod: pod.Name, Container: "pinger", Node: "worker"}}, mode, "tcp-192.0.2.9-30000", diagnosticOptions{})
				require.ErrorIs(t, err, failure)
				require.Equal(t, 42, ExitCode(err))
				require.Equal(t, 1, probes, "a failed probe must not be replayed")
				deletions := 0
				for _, action := range cs.Actions() {
					if action.Matches("delete", "pods") {
						deletions++
						require.Equal(t, types.UID("probe-uid"), *action.(ktesting.DeleteAction).GetDeleteOptions().Preconditions.UID)
					}
				}
				require.Equal(t, 1, deletions)
				original, err := cs.CoreV1().Pods(pod.Namespace).Get(t.Context(), pod.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.Equal(t, pod.Spec, original.Spec, "the source component must remain unchanged")
			})
		}
	}
}

func TestDiagnosticProbeRejectsReplacedSourceAndCleansUp(t *testing.T) {
	pod := readyPod("pinger", "worker", "pinger", map[string]string{"app": "kube-ovn-pinger"})
	pod.UID, pod.Status.PodIP, pod.Spec.HostNetwork = "source-uid", "192.0.2.2", true
	app, executor, _, _ := testApplication(t, pod, readyPod("agent-worker", "worker", "agent", map[string]string{"app": "kubectl-ko-node-agent"}))
	client, err := app.newClient()
	require.NoError(t, err)
	client.ComponentFree = true
	cs := client.Kubernetes.(*fake.Clientset)
	cs.PrependReactor("create", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		probe := action.(ktesting.CreateAction).GetObject().(*corev1.Pod).DeepCopy()
		probe.Namespace, probe.UID = action.GetNamespace(), "probe-uid"
		probe.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		require.NoError(t, cs.Tracker().Add(probe))
		return true, probe, nil
	})
	reads := 0
	cs.PrependReactor("get", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.(ktesting.GetAction).GetName() != pod.Name {
			return false, nil, nil
		}
		reads++
		current := pod.DeepCopy()
		if reads > 1 {
			current.UID = "replacement-uid"
		}
		return true, current, nil
	})
	err = app.runDiagnosticProbes(t.Context(), client, []Target{{Namespace: pod.Namespace, Pod: pod.Name, Container: "pinger", Node: "worker"}}, "IPPorts", "tcp-192.0.2.9-30000", diagnosticOptions{})
	require.ErrorContains(t, err, "network identity changed")
	require.Empty(t, executor.calls, "a replaced source must not be probed")
	deletions := 0
	for _, action := range cs.Actions() {
		if action.Matches("delete", "pods") {
			deletions++
			require.Equal(t, types.UID("probe-uid"), *action.(ktesting.DeleteAction).GetDeleteOptions().Preconditions.UID)
		}
	}
	require.Equal(t, 1, deletions)
}

func TestDiagnosticProbePodRemovesComponentLifecycle(t *testing.T) {
	pod := readyPod("pinger", "worker", "pinger", map[string]string{"app": "kube-ovn-pinger"})
	pod.Spec.ServiceAccountName = "existing-pinger-account"
	pod.Spec.AutomountServiceAccountToken = new(true)
	pod.Spec.ReadinessGates = []corev1.PodReadinessGate{{ConditionType: "example.com/component-ready"}}
	pod.Spec.Containers[0].Lifecycle = &corev1.Lifecycle{PostStart: &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: []string{"component-hook"}}}}
	pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{RunAsNonRoot: new(true), ReadOnlyRootFilesystem: new(true), Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"NET_RAW"}}}
	pod.Spec.Volumes = []corev1.Volume{{Name: "unused-component-log", HostPath: &corev1.HostPathVolumeSource{Path: "/var/log/kube-ovn"}}}
	original := pod.DeepCopy()
	probe, err := diagnosticProbePod(pod, "independent-probe", map[string]string{"app": "kubectl-ko-probe"})
	require.NoError(t, err)
	require.Equal(t, original, pod)
	require.Equal(t, pod.Spec.ServiceAccountName, probe.Spec.ServiceAccountName)
	require.Equal(t, pod.Spec.AutomountServiceAccountToken, probe.Spec.AutomountServiceAccountToken)
	require.Nil(t, probe.Spec.ReadinessGates)
	require.Nil(t, probe.Spec.Containers[0].Lifecycle)
	require.False(t, *probe.Spec.Containers[0].SecurityContext.RunAsNonRoot)
	require.True(t, *probe.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem)
	require.Contains(t, probe.Spec.Containers[0].SecurityContext.Capabilities.Add, corev1.Capability("NET_RAW"))
	require.Contains(t, probe.Spec.Containers[0].SecurityContext.Capabilities.Add, corev1.Capability("SYS_ADMIN"))
	require.True(t, slices.ContainsFunc(probe.Spec.Volumes, func(volume corev1.Volume) bool { return volume.Name == "ko-probe-log" && volume.EmptyDir != nil }))
	require.False(t, slices.ContainsFunc(probe.Spec.Volumes, func(volume corev1.Volume) bool { return volume.Name == "unused-component-log" }))
	pod.Spec.Containers = nil
	_, err = diagnosticProbePod(pod, "independent-probe", nil)
	require.ErrorContains(t, err, "no pinger container")
}

func TestDiagnosticExternalPingIsExplicitAndPropagatesFailure(t *testing.T) {
	for _, addresses := range [][]string{nil, {"192.0.2.1", "2001:db8::1"}} {
		t.Run(strings.Join(addresses, ","), func(t *testing.T) {
			pod := readyPod("pinger", "worker", "pinger", nil)
			pod.Status.PodIPs = []corev1.PodIP{{IP: "192.0.2.2"}, {IP: "2001:db8::2"}}
			app, executor, _, _ := testApplication(t, pod)
			client, err := app.newClient()
			require.NoError(t, err)
			executor.run = func(_ context.Context, _ Target, argv []string, _ Streams) error {
				if argv[0] != "/kube-ovn/kube-ovn-pinger" {
					return nil
				}
				require.Contains(t, argv, "--external-address="+strings.Join(addresses, ","))
				require.Contains(t, argv, "--exit-code=1")
				if len(addresses) != 0 {
					return utilexec.CodeExitError{Err: errors.New("external ping failure"), Code: 1}
				}
				return nil
			}
			err = app.runDiagnosticProbes(t.Context(), client, []Target{{Namespace: pod.Namespace, Pod: pod.Name, Node: "worker"}}, "all", "tcp-192.0.2.2-30000", diagnosticOptions{externalAddresses: addresses})
			if len(addresses) == 0 {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "probe on worker")
				require.ErrorContains(t, err, "external ping failure")
			}
		})
	}
}

func TestDiagnosticExternalPingRejectsAnUnsupportedFamily(t *testing.T) {
	for _, ips := range [][]corev1.PodIP{nil, {{IP: "192.0.2.2"}}, {{IP: "invalid"}}} {
		t.Run(fmt.Sprint(ips), func(t *testing.T) {
			pod := readyPod("pinger", "worker", "pinger", nil)
			pod.Status.PodIPs = ips
			app, executor, _, _ := testApplication(t, pod)
			client, err := app.newClient()
			require.NoError(t, err)
			err = app.runDiagnosticProbes(t.Context(), client, []Target{{Namespace: pod.Namespace, Pod: pod.Name, Node: "worker"}}, "all", "", diagnosticOptions{externalAddresses: []string{"2001:db8::1"}})
			require.ErrorContains(t, err, "probe on worker")
			require.Empty(t, executor.calls, "pinger must not silently skip an explicit target")
		})
	}
}

func TestPerformanceCleansUpAnAmbiguousCommittedTransaction(t *testing.T) {
	app, executor, _, _ := testApplication(t, readyPod("nb", "node", "ovn-central", map[string]string{"ovn-nb-leader": "true"}))
	client, err := app.newClient()
	require.NoError(t, err)
	run := &resourceRun{client: client, id: "unique-run"}
	pods := &performancePods{server: &corev1.Pod{Annotations: map[string]string{annotationPrefix + "logical_switch": "subnet"}, Status: corev1.PodStatus{PodIP: "10.0.0.2"}}, service: &corev1.Service{Spec: corev1.ServiceSpec{ClusterIP: "10.96.0.2"}}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	executor.run = func(execCtx context.Context, _ Target, argv []string, _ Streams) error {
		if len(executor.calls) == 1 {
			cancel() // The transaction committed, but its response disappeared.
			return context.DeadlineExceeded
		}
		require.NoError(t, execCtx.Err(), "cleanup must survive the cancelled invocation")
		require.Equal(t, []string{"ovn-nbctl", "--if-exists", "lb-del", "ko-perf-unique-run"}, argv)
		return nil
	}
	require.ErrorIs(t, app.servicePerformance(ctx, run, pods, performanceOptions{}), context.DeadlineExceeded)
	require.Len(t, executor.calls, 2)
}

func TestServicePerformanceWaitsForChassisBeforeMeasurement(t *testing.T) {
	for _, syncFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("syncFails=%t", syncFails), func(t *testing.T) {
			app, executor, _, _ := testApplication(t, readyPod("nb", "node", "ovn-central", map[string]string{"ovn-nb-leader": "true"}))
			client, err := app.newClient()
			require.NoError(t, err)
			run := &resourceRun{client: client, id: "unique-run"}
			pods := &performancePods{
				client:  &corev1.Pod{Name: "client"},
				server:  &corev1.Pod{Annotations: map[string]string{annotationPrefix + "logical_switch": "subnet"}, Status: corev1.PodStatus{PodIP: "10.0.0.2"}},
				service: &corev1.Service{Spec: corev1.ServiceSpec{ClusterIP: "10.96.0.2"}},
			}
			failure := errors.New("measurement failed")
			if syncFails {
				failure = errors.New("flow synchronization failed")
			}
			executor.run = func(_ context.Context, _ Target, argv []string, _ Streams) error {
				switch len(executor.calls) {
				case 1:
					require.Equal(t, []string{"ovn-nbctl", "--wait=hv", "--timeout=30", "--", "lb-add", "ko-perf-unique-run", "10.96.0.2", "10.0.0.2", "--", "ls-lb-add", "subnet", "ko-perf-unique-run"}, argv)
					if syncFails {
						return failure
					}
					return nil
				case 2:
					if !syncFails {
						require.Equal(t, "qperf", argv[0])
						return failure
					}
				}
				require.Equal(t, []string{"ovn-nbctl", "--if-exists", "lb-del", "ko-perf-unique-run"}, argv)
				return nil
			}
			require.ErrorIs(t, app.servicePerformance(t.Context(), run, pods, performanceOptions{duration: 1}), failure)
			calls := 3
			if syncFails {
				calls = 2
			}
			require.Len(t, executor.calls, calls, "failed synchronization must stop measurements and still remove the owned LB")
		})
	}
}

func TestRecoveryOptionalHeadersAndProbeErrors(t *testing.T) {
	for _, code := range []int{1, 126} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			_, client, executor, _ := recoveryApplication(t)
			record := &recoveryRecord{SourceNode: "node", Directory: "/etc/ovn/recovery", Targets: []Target{{Node: "node"}}}
			executor.run = func(_ context.Context, _ Target, argv []string, _ Streams) error {
				if argv[0] == "test" {
					return utilexec.CodeExitError{Err: errors.New("test failed"), Code: code}
				}
				return nil
			}
			err := client.replaceRecoveryFiles(t.Context(), record)
			if code == 1 {
				require.NoError(t, err, "headers are optional on older images")
			} else {
				require.Error(t, err, "execution errors must not masquerade as missing headers")
				require.Len(t, executor.calls, 2)
			}
		})
	}
}

func TestDatabaseStatusRejectsInconsistentStorage(t *testing.T) {
	app, executor, out, _ := testApplication(t, readyPod("central", "node", "ovn-central", map[string]string{"app": "ovn-central"}))
	executor.run = func(_ context.Context, _ Target, _ []string, s Streams) error {
		_, err := io.WriteString(s.Out, "status: inconsistent data\n")
		return err
	}
	require.ErrorContains(t, app.Execute(t.Context(), []string{"db", "health"}), "storage is unhealthy")
	require.Equal(t, 2, strings.Count(out.String(), "inconsistent data"))
}

func TestMulticastCleansUpLostAddResponseAndPreservesExistingMembership(t *testing.T) {
	app, executor, _, _ := testApplication(t,
		&corev1.Node{Name: "a"}, &corev1.Node{Name: "b"},
		readyPod("ovs-a", "a", "openvswitch", map[string]string{"app": "ovs"}),
		readyPod("ovs-b", "b", "openvswitch", map[string]string{"app": "ovs"}),
		readyPod("cni-a", "a", "cni-server", map[string]string{"app": "kube-ovn-cni"}),
		readyPod("cni-b", "b", "cni-server", map[string]string{"app": "kube-ovn-cni"}),
	)
	client, err := app.newClient()
	require.NoError(t, err)
	pods := []*corev1.Pod{
		{Spec: corev1.PodSpec{NodeName: "a", HostNetwork: true}, Status: corev1.PodStatus{PodIP: "192.0.2.1"}},
		{Spec: corev1.PodSpec{NodeName: "b", HostNetwork: true}, Status: corev1.PodStatus{PodIP: "192.0.2.2"}},
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	membership := map[string]bool{"a": true}
	executor.run = func(execCtx context.Context, target Target, argv []string, streams Streams) error {
		require.Equal(t, "cni-server", target.Container, "host membership inspection and cleanup need CNI network capabilities")
		switch strings.Join(argv, " ") {
		case "ip -s -o addr show":
			address := "192.0.2.1"
			if target.Node == "b" {
				address = "192.0.2.2"
			}
			_, err := io.WriteString(streams.Out, "2: eth0 inet "+address+"/24\n")
			return err
		case "ip maddr show dev eth0":
			if membership[target.Node] {
				_, err := io.WriteString(streams.Out, "link 01:00:5e:00:00:64\n")
				return err
			}
		case "ip maddr add 01:00:5e:00:00:64 dev eth0":
			require.Equal(t, "b", target.Node)
			membership[target.Node] = true
			cancel() // The host changed, but the exec result was lost.
			return context.DeadlineExceeded
		case "ip maddr del 01:00:5e:00:00:64 dev eth0":
			require.NoError(t, execCtx.Err(), "cleanup must survive cancellation")
			require.Equal(t, "b", target.Node, "preexisting membership must not be removed")
			delete(membership, target.Node)
		default:
			t.Fatalf("unexpected command: %v", argv)
		}
		return nil
	}
	require.ErrorIs(t, app.multicastPerformance(ctx, client, pods[0], pods[1], performanceOptions{}), context.DeadlineExceeded)
	require.Equal(t, map[string]bool{"a": true}, membership)
}

func TestMulticastPodNamespaceUsesCNIContainer(t *testing.T) {
	app, executor, _, _ := testApplication(t,
		&corev1.Node{Name: "worker"},
		readyPod("ovs-worker", "worker", "openvswitch", map[string]string{"app": "ovs"}),
		readyPod("cni-worker", "worker", "cni-server", map[string]string{"app": "kube-ovn-cni"}),
	)
	client, err := app.newClient()
	require.NoError(t, err)
	executor.run = func(_ context.Context, target Target, argv []string, streams Streams) error {
		require.Equal(t, "openvswitch", target.Container)
		require.Contains(t, argv, "ovs-vsctl")
		_, err := io.WriteString(streams.Out, `{"headings":["name","external_ids","ofport"],"data":[["pod-port",["map",[["pod_netns","/var/run/netns/pod"]]],1]]}`)
		return err
	}
	for _, nicType := range []string{"veth-pair", "internal-port"} {
		pod := &corev1.Pod{Name: "probe", Namespace: "ovn-system", Annotations: map[string]string{annotationPrefix + "pod_nic_type": nicType}, Spec: corev1.PodSpec{NodeName: "worker"}}
		target, err := client.multicastTarget(t.Context(), pod)
		require.NoError(t, err)
		require.Equal(t, "cni-worker", target.target.Pod, "only CNI mounts the host Pod network namespaces")
		require.Equal(t, "cni-server", target.target.Container)
		require.Equal(t, "/var/run/netns/pod", target.netns)
		expected := "eth0"
		if nicType == "internal-port" {
			expected = "pod-port"
		}
		require.Equal(t, expected, target.nic)
	}
}

func TestMulticastHostNamespaceUsesPrivilegedCNIContainer(t *testing.T) {
	app, executor, _, _ := testApplication(t,
		&corev1.Node{Name: "worker"},
		readyPod("ovs-worker", "worker", "openvswitch", map[string]string{"app": "ovs"}),
		readyPod("cni-worker", "worker", "cni-server", map[string]string{"app": "kube-ovn-cni"}),
	)
	client, err := app.newClient()
	require.NoError(t, err)
	executor.run = func(_ context.Context, target Target, argv []string, streams Streams) error {
		if target.Container != "cni-server" {
			return errors.New("Helm OVS lacks NET_ADMIN: ioctl: Operation not permitted")
		}
		require.Equal(t, []string{"ip", "-s", "-o", "addr", "show"}, argv)
		_, err := io.WriteString(streams.Out, "2: eth0@if3 inet 192.0.2.1/24\n")
		return err
	}
	pod := &corev1.Pod{Spec: corev1.PodSpec{NodeName: "worker", HostNetwork: true}, Status: corev1.PodStatus{PodIP: "192.0.2.1"}}
	target, err := client.multicastTarget(t.Context(), pod)
	require.NoError(t, err)
	require.Equal(t, "cni-server", target.target.Container)
	require.Equal(t, "cni-worker", target.target.Pod)
	require.Empty(t, target.netns)
	require.Equal(t, "eth0", target.nic)
}
