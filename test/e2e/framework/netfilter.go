package framework

import (
	"fmt"
	"strings"

	"github.com/onsi/ginkgo/v2"
)

func (f *Framework) IsGatewayNFTables() bool {
	ginkgo.GinkgoHelper()

	daemonSetClient := f.DaemonSetClientNS(KubeOvnNamespace)
	daemonSet := daemonSetClient.Get("kube-ovn-cni")
	pods, err := daemonSetClient.GetPods(daemonSet)
	ExpectNoError(err)
	ExpectNotEmpty(pods.Items)

	pod := pods.Items[0]
	metricsURL := fmt.Sprintf("http://%s:10665/metrics", pod.Status.PodIP)
	metrics, _, err := ExecShellInContainer(f, pod.Namespace, pod.Name, "cni-server", "curl -fsS "+metricsURL)
	ExpectNoError(err)
	return gatewayBackendIsNFTables(metrics)
}

func gatewayBackendIsNFTables(metrics string) bool {
	for line := range strings.Lines(metrics) {
		if strings.TrimSpace(line) == `kube_ovn_gateway_netfilter_backend{backend="nftables"} 1` {
			return true
		}
	}
	return false
}
