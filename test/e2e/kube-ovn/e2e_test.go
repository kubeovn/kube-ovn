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
	_ "github.com/kubeovn/kube-ovn/test/e2e/kube-ovn/network-policy"
	_ "github.com/kubeovn/kube-ovn/test/e2e/kube-ovn/node"
	_ "github.com/kubeovn/kube-ovn/test/e2e/kube-ovn/pod"
	_ "github.com/kubeovn/kube-ovn/test/e2e/kube-ovn/qos"
	_ "github.com/kubeovn/kube-ovn/test/e2e/kube-ovn/service"
	_ "github.com/kubeovn/kube-ovn/test/e2e/kube-ovn/subnet"
	_ "github.com/kubeovn/kube-ovn/test/e2e/kube-ovn/switch_lb_rule"
	_ "github.com/kubeovn/kube-ovn/test/e2e/kube-ovn/underlay"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	framework.RegisterCommonFlags(flag.CommandLine)
	framework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	framework.AfterReadingAllFlags(&framework.TestContext)
	e2e.RunE2ETests(t)
}
