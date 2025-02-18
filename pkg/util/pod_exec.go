package util

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog/v2"
)

type ExecOptions struct {
	Command            []string
	Namespace          string
	PodName            string
	ContainerName      string
	Stdin              io.Reader
	CaptureStdout      bool
	CaptureStderr      bool
	PreserveWhitespace bool
}

func ExecuteCommandInContainer(client kubernetes.Interface, cfg *rest.Config, namespace, podName, containerName string, cmd ...string) (
	string, string, error,
) {
	return ExecuteWithOptions(client, cfg, ExecOptions{
		Command:            cmd,
		Namespace:          namespace,
		PodName:            podName,
		ContainerName:      containerName,
		Stdin:              nil,
		CaptureStdout:      true,
		CaptureStderr:      true,
		PreserveWhitespace: false,
	})
}

func ExecuteWithOptions(client kubernetes.Interface, cfg *rest.Config, options ExecOptions) (string, string, error) {
	req := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(options.PodName).
		Namespace(options.Namespace).
		SubResource("exec").
		Param("container", options.ContainerName)

	req.VersionedParams(&corev1.PodExecOptions{
		Stdin:     options.Stdin != nil,
		Stdout:    options.CaptureStdout,
		Stderr:    options.CaptureStderr,
		TTY:       false,
		Container: options.ContainerName,
		Command:   options.Command,
	}, scheme.ParameterCodec)

	var stdout, stderr bytes.Buffer
	err := execute("POST", req.URL(), cfg, options.Stdin, &stdout, &stderr, false)
	if options.PreserveWhitespace {
		return stdout.String(), stderr.String(), err
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

func execute(method string, url *url.URL, cfg *rest.Config, stdin io.Reader, stdout, stderr io.Writer,
	tty bool,
) error {
	exec, err := remotecommand.NewSPDYExecutor(cfg, method, url)
	if err != nil {
		klog.Errorf("remotecommand.NewSPDYExecutor error: %v", err)
		return err
	}
	return exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    tty,
	})
}
