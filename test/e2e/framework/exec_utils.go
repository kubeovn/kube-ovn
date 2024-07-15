package framework

import (
	"context"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

// ExecCommandInContainer executes a command in the specified container.
func ExecCommandInContainer(ctx context.Context, f *Framework, namespace, pod, container string, cmd ...string) (string, string, error) {
	return util.ExecuteCommandInContainer(ctx, f.ClientSet, f.ClientConfig(), namespace, pod, container, cmd...)
}

// ExecShellInContainer executes the specified command on the pod's container.
func ExecShellInContainer(ctx context.Context, f *Framework, namespace, pod, container, cmd string) (string, string, error) {
	return ExecCommandInContainer(ctx, f, namespace, pod, container, "/bin/sh", "-c", cmd)
}

func execCommandInPod(ctx context.Context, f *Framework, namespace, pod string, cmd ...string) (string, string, error) {
	ginkgo.GinkgoHelper()

	p := f.PodClientNS(namespace).Get(ctx, pod)
	gomega.Expect(p.Spec.Containers).NotTo(gomega.BeEmpty())
	return ExecCommandInContainer(ctx, f, namespace, pod, p.Spec.Containers[0].Name, cmd...)
}

// ExecShellInPod executes the specified command on the pod.
func ExecShellInPod(ctx context.Context, f *Framework, namespace, pod, cmd string) (string, string, error) {
	ginkgo.GinkgoHelper()
	return execCommandInPod(ctx, f, namespace, pod, "/bin/sh", "-c", cmd)
}
