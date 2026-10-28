package ovn_ic

import (
	"testing"

	"github.com/stretchr/testify/require"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
)

func TestKubectlKoCommandKeepsConnectionFlagsAfterPlugin(t *testing.T) {
	previousConfig := k8sframework.TestContext.KubeConfig
	previousPath := k8sframework.TestContext.KubectlPath
	previousHost := k8sframework.TestContext.Host
	t.Cleanup(func() {
		k8sframework.TestContext.KubeConfig = previousConfig
		k8sframework.TestContext.KubectlPath = previousPath
		k8sframework.TestContext.Host = previousHost
	})
	k8sframework.TestContext.KubeConfig = "/tmp/test-kubeconfig"
	k8sframework.TestContext.KubectlPath = "kubectl"
	k8sframework.TestContext.Host = ""

	command := kubectlKoCommand("kind-kube-ovn1", "exec", "ic-nbctl", "--", "show")
	require.Equal(t, []string{
		"ko", "--kubeconfig=/tmp/test-kubeconfig", "--context=kind-kube-ovn1",
		"exec", "ic-nbctl", "--", "show",
	}, command.Args[1:])
}
