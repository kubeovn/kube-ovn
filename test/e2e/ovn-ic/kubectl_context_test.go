package ovn_ic

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
)

func TestKubectlKoCommandKeepsConnectionFlagsAfterPlugin(t *testing.T) {
	t.Setenv("E2E_BRANCH", "master")
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

	command, cleanup, err := kubectlKoCommand("kind-kube-ovn1", "ko", "exec", "ic-nbctl", "--", "show")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	require.Equal(t, []string{
		"ko", "--kubeconfig=/tmp/test-kubeconfig", "--context=kind-kube-ovn1",
		"exec", "ic-nbctl", "--", "show",
	}, command.Args[1:])
}

func TestKubectlKoCommandUsesContextForLegacyPlugin(t *testing.T) {
	t.Setenv("E2E_BRANCH", "release-1.16")
	t.Setenv("KUBECONFIG", "/tmp/shared-kubeconfig")
	previousConfig := k8sframework.TestContext.KubeConfig
	previousPath := k8sframework.TestContext.KubectlPath
	previousHost := k8sframework.TestContext.Host
	t.Cleanup(func() {
		k8sframework.TestContext.KubeConfig = previousConfig
		k8sframework.TestContext.KubectlPath = previousPath
		k8sframework.TestContext.Host = previousHost
	})
	k8sframework.TestContext.KubeConfig = t.TempDir() + "/test-kubeconfig"
	k8sframework.TestContext.KubectlPath = "kubectl"
	k8sframework.TestContext.Host = ""
	config := clientcmdapi.NewConfig()
	config.Clusters["cluster"] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:1"}
	config.Contexts["kind-kube-ovn"] = &clientcmdapi.Context{Cluster: "cluster"}
	config.Contexts["kind-kube-ovn1"] = &clientcmdapi.Context{Cluster: "cluster"}
	config.CurrentContext = "kind-kube-ovn"
	require.NoError(t, clientcmd.WriteToFile(*config, k8sframework.TestContext.KubeConfig))

	command, cleanup, err := kubectlKoCommand("kind-kube-ovn1", "ko", "nbctl", "show")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	require.Equal(t, []string{
		"ko", "nbctl", "show",
	}, command.Args[1:])
	var temporaryConfig string
	kubeconfigEntries := 0
	for _, value := range command.Env {
		if strings.HasPrefix(value, "KUBECONFIG=") {
			kubeconfigEntries++
			temporaryConfig = strings.TrimPrefix(value, "KUBECONFIG=")
		}
	}
	require.Equal(t, 1, kubeconfigEntries)
	require.NotEmpty(t, temporaryConfig)
	legacyConfig, err := clientcmd.LoadFromFile(temporaryConfig)
	require.NoError(t, err)
	require.Equal(t, "kind-kube-ovn1", legacyConfig.CurrentContext)
	sourceConfig, err := clientcmd.LoadFromFile(k8sframework.TestContext.KubeConfig)
	require.NoError(t, err)
	require.Equal(t, "kind-kube-ovn", sourceConfig.CurrentContext)
	_, err = os.Stat(temporaryConfig)
	require.NoError(t, err)
}

func TestKubectlKoCommandUsesDefaultKubeconfigForLegacyPlugin(t *testing.T) {
	t.Setenv("E2E_BRANCH", "release-1.16")
	configPath := t.TempDir() + "/config"
	t.Setenv("KUBECONFIG", configPath)
	previousConfig := k8sframework.TestContext.KubeConfig
	previousPath := k8sframework.TestContext.KubectlPath
	t.Cleanup(func() {
		k8sframework.TestContext.KubeConfig = previousConfig
		k8sframework.TestContext.KubectlPath = previousPath
	})
	k8sframework.TestContext.KubeConfig = ""
	k8sframework.TestContext.KubectlPath = "kubectl"
	config := clientcmdapi.NewConfig()
	config.Clusters["cluster"] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:1"}
	config.Contexts["kind-kube-ovn"] = &clientcmdapi.Context{Cluster: "cluster"}
	config.Contexts["kind-kube-ovn1"] = &clientcmdapi.Context{Cluster: "cluster"}
	config.CurrentContext = "kind-kube-ovn"
	require.NoError(t, clientcmd.WriteToFile(*config, configPath))

	command, cleanup, err := kubectlKoCommand("kind-kube-ovn1", "ko", "nbctl", "show")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	require.Equal(t, []string{"ko", "nbctl", "show"}, command.Args[1:])
	for _, value := range command.Env {
		if path, ok := strings.CutPrefix(value, "KUBECONFIG="); ok {
			require.NotEqual(t, configPath, path)
			selectedConfig, err := clientcmd.LoadFromFile(path)
			require.NoError(t, err)
			require.Equal(t, "kind-kube-ovn1", selectedConfig.CurrentContext)
			return
		}
	}
	t.Fatal("legacy command has no KUBECONFIG")
}
