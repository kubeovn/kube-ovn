package ko

import (
	"archive/tar"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestLegacyLogCallsCollectAndPreserveValgrindFiles(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		t.Run("legacy="+strconv.FormatBool(legacy), func(t *testing.T) {
			t.Chdir(t.TempDir())
			pods := map[string]*corev1.Pod{
				"app=ovs":                   readyPod("ovs-a", "worker", "openvswitch", map[string]string{"app": "ovs"}),
				"app=ovn-central":           readyPod("central-a", "worker", "ovn-central", map[string]string{"app": "ovn-central"}),
				"app=kubectl-ko-node-agent": readyPod("agent-a", "worker", "agent", map[string]string{"app": "kubectl-ko-node-agent"}),
			}
			for _, pod := range pods {
				pod.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.Now()
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/log") {
					_, _ = io.WriteString(w, "container stdout\n")
					return
				}
				pod := pods[r.URL.Query().Get("labelSelector")]
				if pod == nil {
					t.Errorf("unexpected discovery: %s", r.URL)
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.MarshalWrite(w, &corev1.PodList{Items: []corev1.Pod{*pod}})
			}))
			defer server.Close()
			kube, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
			require.NoError(t, err)
			files := map[string][]byte{
				"/var/log/ovn":         archive(t, "ovn-controller.valgrind.log.123", tar.TypeReg, "OVN valgrind\n"),
				"/var/log/openvswitch": archive(t, "ovs-vswitchd.valgrind.log.456", tar.TypeReg, "OVS valgrind\n"),
			}
			var sources []string
			executor := &recordingExecutor{run: func(_ context.Context, target Target, argv []string, streams Streams) error {
				require.Equal(t, "agent-a", target.Pod)
				require.Equal(t, "agent", target.Container)
				require.Equal(t, []string{"tar", "-C", argv[2], "-cf", "-", "."}, argv)
				sources = append(sources, argv[2])
				_, err := streams.Out.Write(files[argv[2]])
				return err
			}}
			client := &Client{Kubernetes: kube, Executor: executor, Namespace: "ovn-system", ComponentFree: true, DiscoveryTimeout: time.Second}
			for _, component := range []string{"ovn", "ovs"} {
				app, _, _, _ := testApplication(t)
				app.newClient = func() (*Client, error) { return client, nil }
				args := []string{"log", component}
				if !legacy {
					args = []string{"logs", "--component", component}
				}
				args = append(args, "--concurrency=1", "--strict")
				require.NoError(t, app.Execute(t.Context(), args))
				ovn, err := os.ReadFile(filepath.Join("kubectl-ko-log", "worker", "ovn", "ovn-controller.valgrind.log.123"))
				require.NoError(t, err)
				require.Equal(t, "OVN valgrind\n", string(ovn))
				if component == "ovn" {
					require.NotContains(t, sources, "/var/log/openvswitch")
					continue
				}
				ovs, err := os.ReadFile(filepath.Join("kubectl-ko-log", "worker", "openvswitch", "ovs-vswitchd.valgrind.log.456"))
				require.NoError(t, err)
				require.Equal(t, "OVS valgrind\n", string(ovs))
			}
			require.Contains(t, sources, "/var/log/openvswitch")
		})
	}
}

func TestLegacyLogAcceptsAllSupportedComponents(t *testing.T) {
	for _, component := range []string{"ovn", "ovs", "all", "kube-ovn", "linux"} {
		t.Run(component, func(t *testing.T) {
			app, _, _, _ := testApplication(t)
			failure := errors.New("cluster unavailable")
			app.newClient = func() (*Client, error) { return nil, failure }
			require.ErrorIs(t, app.Execute(t.Context(), []string{"log", component}), failure)
		})
	}
}

func TestLegacyLogFailuresDoNotFabricateValgrindFiles(t *testing.T) {
	for _, strict := range []bool{false, true} {
		t.Run("strict="+strconv.FormatBool(strict), func(t *testing.T) {
			app, executor, _, stderr := testApplication(
				t,
				readyPod("ovs", "worker", "openvswitch", map[string]string{"app": "ovs"}),
				readyPod("central", "worker", "ovn-central", map[string]string{"app": "ovn-central"}),
				readyPod("agent", "worker", "agent", map[string]string{"app": "kubectl-ko-node-agent"}),
			)
			client, err := app.newClient()
			require.NoError(t, err)
			client.ComponentFree = true
			executor.run = func(_ context.Context, target Target, _ []string, _ Streams) error {
				require.Equal(t, "agent", target.Container)
				return errors.New("host archive unavailable")
			}
			directory := t.TempDir()
			err = app.Execute(t.Context(), []string{"log", "ovn", "--output-dir", directory, "--concurrency=1", "--strict=" + strconv.FormatBool(strict)})
			if strict {
				require.ErrorContains(t, err, "host archive unavailable")
			} else {
				require.NoError(t, err)
			}
			require.Contains(t, stderr.String(), "host archive unavailable")
			data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
			require.NoError(t, err)
			require.Contains(t, string(data), "host archive unavailable")
			var valgrindFiles []string
			require.NoError(t, filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
				if err == nil && strings.Contains(entry.Name(), ".valgrind.log.") {
					valgrindFiles = append(valgrindFiles, path)
				}
				return err
			}))
			require.Empty(t, valgrindFiles, "failed archives must not satisfy the CI log existence gate")
		})
	}
}

func TestAgentCollectionPreservesComponentStdout(t *testing.T) {
	pods := map[string]*corev1.Pod{
		"app=ovs":                   readyPod("ovs-a", "worker", "openvswitch", map[string]string{"app": "ovs"}),
		"app=ovn-central":           readyPod("central-a", "worker", "ovn-central", map[string]string{"app": "ovn-central"}),
		"app=kubectl-ko-node-agent": readyPod("agent-a", "worker", "agent", map[string]string{"app": "kubectl-ko-node-agent"}),
	}
	for _, pod := range pods {
		pod.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.Now()
	}
	var logs []string
	var logsMutex sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/log") {
			logsMutex.Lock()
			logs = append(logs, r.URL.Path)
			logsMutex.Unlock()
			_, _ = io.WriteString(w, r.URL.Path)
			return
		}
		pod := pods[r.URL.Query().Get("labelSelector")]
		if pod == nil {
			t.Errorf("unexpected discovery: %s", r.URL)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, &corev1.PodList{Items: []corev1.Pod{*pod}})
	}))
	defer server.Close()
	kubernetesClient, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	require.NoError(t, err)
	client := &Client{Kubernetes: kubernetesClient, Namespace: "ovn-system", ComponentFree: true, DiscoveryTimeout: time.Second}
	tasks, err := client.collectionTasks(t.Context(), "ovn", collectionOptions{output: t.TempDir(), maxBytes: 1024})
	require.NoError(t, err)
	require.Len(t, tasks, 4)
	for _, task := range tasks {
		if task.Name != "container stdout" {
			require.Equal(t, "agent-a", task.Target.Pod, "host files must be collected by the independent agent")
			continue
		}
		require.NotEqual(t, "agent-a", task.Target.Pod)
		require.NoError(t, task.collect(t.Context()))
		contents, err := os.ReadFile(task.Path)
		require.NoError(t, err)
		require.Contains(t, string(contents), "/"+task.Target.Pod+"/log")
		require.Contains(t, task.Path, task.Target.Pod+".stdout.log")
	}
	logsMutex.Lock()
	defer logsMutex.Unlock()
	require.Equal(t, []string{"/api/v1/namespaces/ovn-system/pods/ovs-a/log", "/api/v1/namespaces/ovn-system/pods/central-a/log"}, logs)
}

func TestLogsWritesManifestAndRetainsPartialFailures(t *testing.T) {
	for _, strict := range []bool{false, true} {
		t.Run(strconv.FormatBool(strict), func(t *testing.T) {
			pod := readyPod("cni", "worker", "cni-server", map[string]string{"app": "kube-ovn-cni"})
			app, executor, _, _ := testApplication(t, pod)
			executor.run = func(_ context.Context, _ Target, argv []string, streams Streams) error {
				if argv[0] == "dmesg" {
					return errors.New("dmesg denied")
				}
				_, err := io.WriteString(streams.Out, "node diagnostics\n")
				return err
			}
			dir := t.TempDir()
			err := app.Execute(t.Context(), []string{"logs", "--component", "linux", "--output-dir", dir, "--concurrency", "1", "--strict=" + strconv.FormatBool(strict)})
			if strict {
				require.ErrorContains(t, err, "dmesg denied")
			} else {
				require.NoError(t, err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
			require.NoError(t, err)
			require.Contains(t, string(data), `"durationNanoseconds":`)
			var manifest struct {
				SchemaVersion string           `json:"schemaVersion"`
				Items         []collectionTask `json:"items"`
			}
			require.NoError(t, json.Unmarshal(data, &manifest))
			require.Equal(t, "v1", manifest.SchemaVersion)
			require.NotEmpty(t, manifest.Items)
			failures := 0
			for _, item := range manifest.Items {
				// Fast failures can finish within one clock tick on Windows.
				require.GreaterOrEqual(t, item.Duration, int64(0))
				require.FileExists(t, item.Path)
				if item.Error != "" {
					failures++
					require.Equal(t, "dmesg", item.Name)
					require.Contains(t, item.Error, "dmesg denied")
				}
			}
			require.Equal(t, 1, failures)
			data, err = os.ReadFile(filepath.Join(dir, "worker", "linux", "addr.log"))
			require.NoError(t, err)
			require.Contains(t, string(data), "node diagnostics")
		})
	}
}

func TestCollectDirectoryDrainsTarRecordPadding(t *testing.T) {
	app, executor, _, _ := testApplication(t)
	client, err := app.newClient()
	require.NoError(t, err)
	data := archive(t, "daemon.log", tar.TypeReg, "remote log\n")
	executor.run = func(_ context.Context, _ Target, _ []string, streams Streams) error {
		if _, err := streams.Out.Write(data); err != nil {
			return err
		}
		_, err := streams.Out.Write(make([]byte, 8192))
		return err
	}
	dir := t.TempDir()
	require.NoError(t, client.collectDirectory(t.Context(), Target{}, "/var/log/ovn", dir, 1<<20))
	contents, err := os.ReadFile(filepath.Join(dir, "daemon.log"))
	require.NoError(t, err)
	require.Equal(t, "remote log\n", string(contents))
}

func TestIPsecCollectionExecutesOnlyIndependentAgent(t *testing.T) {
	pod := readyPod("cni", "worker", "cni-server", map[string]string{"app": "kube-ovn-cni"})
	agent := readyPod("agent", "worker", "agent", map[string]string{"app": "kubectl-ko-node-agent"})
	app, executor, _, _ := testApplication(t, pod, agent)
	client, err := app.newClient()
	require.NoError(t, err)
	client.ComponentFree = true
	executor.run = func(_ context.Context, target Target, argv []string, streams Streams) error {
		require.Equal(t, "agent", target.Pod)
		require.Equal(t, []string{"/kube-ovn/kubectl-ko-node-agent", "ipsec", string(pod.UID)}, argv)
		_, err := io.WriteString(streams.Out, "actual Pod charon status\n")
		return err
	}
	task := client.ipsecTask(Target{Namespace: agent.Namespace, Pod: agent.Name, Container: "agent", Node: "worker"}, Target{Namespace: pod.Namespace, Pod: pod.Name}, t.TempDir(), 1024)
	require.NoError(t, task.collect(t.Context()))
	data, err := os.ReadFile(task.Path)
	require.NoError(t, err)
	require.Contains(t, string(data), "actual Pod charon status")
	pod.DeletionTimestamp = new(metav1.Now())
	_, err = client.Kubernetes.CoreV1().Pods(pod.Namespace).Update(t.Context(), pod, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.ErrorContains(t, task.collect(t.Context()), "terminating")
	require.Len(t, executor.calls, 1, "an invalid source must not issue another agent request")
}

func TestLinuxCollectionWithoutCNIPreservesNodeStateAndSourceFailure(t *testing.T) {
	app, executor, _, _ := testApplication(
		t,
		&corev1.Node{Name: "worker", Labels: map[string]string{corev1.LabelOSStable: "linux"}},
		&corev1.Node{Name: "missing", Labels: map[string]string{corev1.LabelOSStable: "linux"}},
		&corev1.Node{Name: "windows", Labels: map[string]string{corev1.LabelOSStable: "windows"}},
		readyPod("agent", "worker", "agent", map[string]string{"app": "kubectl-ko-node-agent"}),
	)
	client, err := app.newClient()
	require.NoError(t, err)
	client.ComponentFree = true
	executor.run = func(_ context.Context, target Target, argv []string, streams Streams) error {
		require.Equal(t, "agent", target.Container)
		require.Equal(t, "worker", target.Node)
		require.NotContains(t, argv, "ipsec", "a missing source must never query another daemon")
		_, err := io.WriteString(streams.Out, "node state\n")
		return err
	}
	dir := t.TempDir()
	err = app.Execute(t.Context(), []string{"logs", "--component", "linux", "--output-dir", dir, "--concurrency", "1", "--strict"})
	require.ErrorContains(t, err, `node "missing"`)
	require.ErrorContains(t, err, "IPsec source")
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var manifest struct {
		Items          []collectionTask `json:"items"`
		DiscoveryError string           `json:"discoveryError"`
	}
	require.NoError(t, json.Unmarshal(data, &manifest))
	require.ErrorContains(t, errors.New(manifest.DiscoveryError), `node "missing"`)
	require.NotEmpty(t, manifest.Items)
	for _, item := range manifest.Items {
		require.Equal(t, "worker", item.Target.Node)
		if item.Name == "ipsec" {
			require.Contains(t, item.Error, "IPsec source")
		} else {
			require.Empty(t, item.Error)
		}
	}
	data, err = os.ReadFile(filepath.Join(dir, "worker", "linux", "addr.log"))
	require.NoError(t, err)
	require.Contains(t, string(data), `"ip" "-s" "addr" "show"`)
	require.Contains(t, string(data), "node state")
}
