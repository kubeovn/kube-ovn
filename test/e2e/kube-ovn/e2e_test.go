package kube_ovn

import (
	"flag"
	"testing"

	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	"github.com/onsi/ginkgo/v2"

	// Import tests.
	_ "github.com/kubeovn/kube-ovn/test/e2e/kube-ovn/ipam"
	_ "github.com/kubeovn/kube-ovn/test/e2e/kube-ovn/kubectl-ko"
	_ "github.com/kubeovn/kube-ovn/test/e2e/kube-ovn/node"
	_ "github.com/kubeovn/kube-ovn/test/e2e/kube-ovn/qos"
	_ "github.com/kubeovn/kube-ovn/test/e2e/kube-ovn/subnet"
	_ "github.com/kubeovn/kube-ovn/test/e2e/kube-ovn/underlay"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	framework.RegisterCommonFlags(flag.CommandLine)
	framework.RegisterClusterFlags(flag.CommandLine)

	// Parse all the flags
	flag.Parse()
	framework.AfterReadingAllFlags(&framework.TestContext)
}

func TestE2E(t *testing.T) {
	e2e.RunE2ETests(t)
}
