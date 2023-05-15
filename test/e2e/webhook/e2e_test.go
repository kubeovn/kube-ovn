package webhook

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	"github.com/onsi/ginkgo/v2"

	// Import tests.
	_ "github.com/kubeovn/kube-ovn/test/e2e/webhook/pod"
	_ "github.com/kubeovn/kube-ovn/test/e2e/webhook/subnet"
	_ "github.com/kubeovn/kube-ovn/test/e2e/webhook/vip"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	framework.RegisterCommonFlags(flag.CommandLine)
	framework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	if framework.TestContext.KubeConfig == "" {
		framework.TestContext.KubeConfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	framework.AfterReadingAllFlags(&framework.TestContext)

	e2e.RunE2ETests(t)
}
