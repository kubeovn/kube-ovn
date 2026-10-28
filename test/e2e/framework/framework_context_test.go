package framework

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
)

func TestUseContextDoesNotRewriteKubeconfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config")
	config := clientcmdapi.NewConfig()
	config.Clusters["one"] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:1"}
	config.Clusters["two"] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:2"}
	config.Contexts["one"] = &clientcmdapi.Context{Cluster: "one"}
	config.Contexts["two"] = &clientcmdapi.Context{Cluster: "two"}
	config.CurrentContext = "one"
	require.NoError(t, clientcmd.WriteToFile(*config, configPath))
	before, err := os.ReadFile(configPath)
	require.NoError(t, err)

	previousConfig := k8sframework.TestContext.KubeConfig
	previousContext := k8sframework.TestContext.KubeContext
	previousHost := k8sframework.TestContext.Host
	t.Cleanup(func() {
		k8sframework.TestContext.KubeConfig = previousConfig
		k8sframework.TestContext.KubeContext = previousContext
		k8sframework.TestContext.Host = previousHost
	})
	k8sframework.TestContext.KubeConfig = configPath
	k8sframework.TestContext.KubeContext = "one"
	k8sframework.TestContext.Host = ""

	f := &Framework{KubeContext: "two"}
	f.useContext()
	require.Equal(t, "two", k8sframework.TestContext.KubeContext)
	clientConfig, err := k8sframework.LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "https://127.0.0.1:2", clientConfig.Host)
	after, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
