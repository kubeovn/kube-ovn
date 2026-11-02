package framework

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
)

func TestDeploymentClientContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}))
	defer server.Close()
	clientSet, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	require.NoError(t, err)
	client := NewDeploymentClient(clientSet, "kube-system")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = client.GetContext(ctx, "ovn-central")
	require.ErrorContains(t, err, "canceled")
	_, err = client.GetAllPodsContext(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "ovn-central"}},
		},
	})
	require.ErrorContains(t, err, "canceled")
	err = client.SetScaleContext(ctx, "ovn-central", 3)
	require.ErrorContains(t, err, "canceled")
}

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
	previousDerivedHost := derivedHost
	t.Cleanup(func() {
		k8sframework.TestContext.KubeConfig = previousConfig
		k8sframework.TestContext.KubeContext = previousContext
		k8sframework.TestContext.Host = previousHost
		derivedHost = previousDerivedHost
	})
	k8sframework.TestContext.KubeConfig = configPath
	k8sframework.TestContext.KubeContext = "one"
	k8sframework.TestContext.Host = ""

	first := &Framework{KubeContext: "one"}
	first.useContext()
	_, err = loadConfig()
	require.NoError(t, err)
	require.Equal(t, "https://127.0.0.1:1", k8sframework.TestContext.Host)

	second := &Framework{KubeContext: "two"}
	second.useContext()
	require.Equal(t, "two", k8sframework.TestContext.KubeContext)
	clientConfig, err := loadConfig()
	require.NoError(t, err)
	require.Equal(t, "https://127.0.0.1:2", clientConfig.Host)
	after, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestUseContextPreservesExplicitHost(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config")
	config := clientcmdapi.NewConfig()
	config.Clusters["one"] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:1"}
	config.Clusters["two"] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:2"}
	config.Contexts["one"] = &clientcmdapi.Context{Cluster: "one"}
	config.Contexts["two"] = &clientcmdapi.Context{Cluster: "two"}
	config.CurrentContext = "one"
	require.NoError(t, clientcmd.WriteToFile(*config, configPath))

	previousConfig := k8sframework.TestContext.KubeConfig
	previousContext := k8sframework.TestContext.KubeContext
	previousHost := k8sframework.TestContext.Host
	previousDerivedHost := derivedHost
	t.Cleanup(func() {
		k8sframework.TestContext.KubeConfig = previousConfig
		k8sframework.TestContext.KubeContext = previousContext
		k8sframework.TestContext.Host = previousHost
		derivedHost = previousDerivedHost
	})
	k8sframework.TestContext.KubeConfig = configPath
	k8sframework.TestContext.KubeContext = "one"
	k8sframework.TestContext.Host = "https://proxy.example.test"

	f := &Framework{KubeContext: "two"}
	f.useContext()
	require.Equal(t, "https://proxy.example.test", k8sframework.TestContext.Host)
	clientConfig, err := loadConfig()
	require.NoError(t, err)
	require.Equal(t, "https://proxy.example.test", clientConfig.Host)
}

func TestUseContextPreservesExplicitHostMatchingClusterServer(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config")
	config := clientcmdapi.NewConfig()
	config.Clusters["one"] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:1"}
	config.Clusters["two"] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:2"}
	config.Contexts["one"] = &clientcmdapi.Context{Cluster: "one"}
	config.Contexts["two"] = &clientcmdapi.Context{Cluster: "two"}
	config.CurrentContext = "one"
	require.NoError(t, clientcmd.WriteToFile(*config, configPath))

	previousConfig := k8sframework.TestContext.KubeConfig
	previousContext := k8sframework.TestContext.KubeContext
	previousHost := k8sframework.TestContext.Host
	previousDerivedHost := derivedHost
	t.Cleanup(func() {
		k8sframework.TestContext.KubeConfig = previousConfig
		k8sframework.TestContext.KubeContext = previousContext
		k8sframework.TestContext.Host = previousHost
		derivedHost = previousDerivedHost
	})
	k8sframework.TestContext.KubeConfig = configPath
	k8sframework.TestContext.KubeContext = "one"
	k8sframework.TestContext.Host = "https://127.0.0.1:1"

	f := &Framework{KubeContext: "two"}
	f.useContext()
	require.Equal(t, "https://127.0.0.1:1", k8sframework.TestContext.Host)
	clientConfig, err := loadConfig()
	require.NoError(t, err)
	require.Equal(t, "https://127.0.0.1:1", clientConfig.Host)
}
