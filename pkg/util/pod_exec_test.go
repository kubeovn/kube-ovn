package util

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestExecuteCommandInContainer(t *testing.T) {
	cfg := &rest.Config{}
	kubeClient, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	namespace := "default"
	podName := "pod"
	containerName := "container"
	cmd := "ls"
	stdout, stderr, err := ExecuteCommandInContainer(kubeClient, cfg, namespace, podName, containerName, cmd)
	require.Error(t, err)
	require.Empty(t, stdout)
	require.Empty(t, stderr)
}

func TestExecuteWithOptions(t *testing.T) {
	cfg := &rest.Config{}
	kubeClient, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	namespace := "default"
	podName := "pod"
	containerName := "container"
	cmd := []string{"ls"}
	execOptions := ExecOptions{
		Command:            cmd,
		Namespace:          namespace,
		PodName:            podName,
		ContainerName:      containerName,
		Stdin:              nil,
		CaptureStdout:      true,
		CaptureStderr:      true,
		PreserveWhitespace: true,
	}
	stdout, stderr, err := ExecuteWithOptions(kubeClient, cfg, execOptions)
	require.Error(t, err)
	require.Empty(t, stdout)
	require.Empty(t, stderr)
}

func TestExecute(t *testing.T) {
	cfg := &rest.Config{}
	client, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	namespace := "default"
	podName := "pod"
	containerName := "container"
	options := ExecOptions{
		Namespace:     namespace,
		PodName:       podName,
		ContainerName: containerName,
		Stdin:         nil,
	}
	req := client.CoreV1().RESTClient().Post().
		Resource("xxxx").
		Name(options.PodName).
		Namespace(options.Namespace).
		SubResource("exec").
		Param("container", options.ContainerName)

	var stdout, stderr bytes.Buffer
	cfg.ExecProvider = &clientcmdapi.ExecConfig{APIVersion: "client.authentication.k8s.io/v1beta1"}
	cfg.AuthProvider = &clientcmdapi.AuthProviderConfig{Name: "exec"}
	err = execute(context.Background(), "xxxx", req.URL(), cfg, options.Stdin, &stdout, &stderr, false)
	require.Error(t, err)
}

func TestExecuteWithOptionsRespectsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}))
	defer server.Close()
	cfg := &rest.Config{Host: server.URL}
	client, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err = ExecuteWithOptions(client, cfg, ExecOptions{
		Context:       ctx,
		Namespace:     "default",
		PodName:       "pod",
		ContainerName: "container",
		Command:       []string{"echo", "ok"},
		CaptureStdout: true,
		CaptureStderr: true,
	})
	require.ErrorContains(t, err, "canceled")
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}
