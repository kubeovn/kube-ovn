package framework

import (
	"context"

	"github.com/kubeovn/kube-ovn/pkg/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/test/e2e/framework"

	"github.com/onsi/gomega"
)

// ExecCommandInContainer executes a command in the specified container.
func ExecCommandInContainer(f *Framework, podName, containerName string, cmd ...string) (string, string, error) {
	return util.ExecuteCommandInContainer(f.ClientSet, f.ClientConfig(), f.Namespace.Name, podName, containerName, cmd...)
}

// ExecShellInContainer executes the specified command on the pod's container.
func ExecShellInContainer(f *Framework, podName, containerName string, cmd string) (string, string, error) {
	return ExecCommandInContainer(f, podName, containerName, "/bin/sh", "-c", cmd)
}

func execCommandInPod(ctx context.Context, f *Framework, podName string, cmd ...string) (string, string, error) {
	pod, err := f.PodClient().Get(ctx, podName, metav1.GetOptions{})
	framework.ExpectNoError(err, "failed to get pod %v", podName)
	gomega.Expect(pod.Spec.Containers).NotTo(gomega.BeEmpty())
	return ExecCommandInContainer(f, podName, pod.Spec.Containers[0].Name, cmd...)
}

// ExecShellInPod executes the specified command on the pod.
func ExecShellInPod(ctx context.Context, f *Framework, podName string, cmd string) (string, string, error) {
	return execCommandInPod(ctx, f, podName, "/bin/sh", "-c", cmd)
}
