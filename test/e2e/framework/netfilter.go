package framework

import (
	"strings"

	"github.com/onsi/ginkgo/v2"
)

func (f *Framework) IsKubeProxyNFTables() bool {
	ginkgo.GinkgoHelper()

	daemonSetClient := f.DaemonSetClientNS(KubeOvnNamespace)
	daemonSet := daemonSetClient.Get("kube-ovn-cni")
	pods, err := daemonSetClient.GetPods(daemonSet)
	ExpectNoError(err)
	ExpectNotEmpty(pods.Items)

	mode, _, err := ExecShellInContainer(f, pods.Items[0].Namespace, pods.Items[0].Name, "cni-server", "curl -fsS http://localhost:10249/proxyMode")
	ExpectNoError(err)
	return strings.TrimSpace(mode) == "nftables"
}
