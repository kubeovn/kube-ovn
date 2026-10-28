package kubevirt

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	e2ekubectl "k8s.io/kubernetes/test/e2e/framework/kubectl"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func expectVMCaptureInterfaces(namespace, vm string, pod *corev1.Pod) {
	ginkgo.GinkgoHelper()
	testConfig := e2ekubectl.NewTestKubeconfig(k8sframework.TestContext.CertDir,
		k8sframework.TestContext.Host, k8sframework.TestContext.KubeConfig,
		k8sframework.TestContext.KubeContext, k8sframework.TestContext.KubectlPath, "")
	for _, target := range []struct{ kind, name string }{
		{"vm", namespace + "/" + vm}, {"vmi", vm}, {"pod", namespace + "/" + pod.Name},
	} {
		ginkgo.By("Listing capture interfaces for " + target.kind + " " + target.name)
		command := testConfig.KubectlCmd()
		args := append([]string{"ko"}, command.Args[1:]...)
		args = append(args, "--namespace", namespace, "--timeout", "1m", "capture", "--"+target.kind, target.name, "--", "-D")
		ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
		cmd := exec.CommandContext(ctx, command.Path, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		cancel()
		framework.ExpectNoError(err, "capture interface lookup failed: %s", stderr.String())
		framework.ExpectContainSubstring(string(output), "eth0")
		if target.kind != "pod" {
			framework.ExpectContainSubstring(stderr.String(), fmt.Sprintf("through Pod %s on node %s", pod.Name, pod.Spec.NodeName))
			framework.ExpectNotContainSubstring(string(output), "Capturing VMI")
		}
	}
}
