package nftables

import (
	"strings"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:nftables]", func() {
	f := framework.NewDefaultFramework("nftables")

	framework.ConformanceIt("should enable the nftables backend and clean up the old backend in kube-proxy nftables mode", func() {
		f.SkipVersionPriorTo(1, 17, "the Kube-OVN nftables gateway backend is supported since v1.17")

		daemonSetClient := f.DaemonSetClientNS("kube-system")
		daemonSet := daemonSetClient.Get("kube-ovn-cni")
		pods, err := daemonSetClient.GetPods(daemonSet)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(pods.Items)

		for _, pod := range pods.Items {
			mode, _, err := framework.ExecShellInContainer(f, pod.Namespace, pod.Name, "cni-server", "curl -fsS http://localhost:10249/proxyMode")
			framework.ExpectNoError(err)
			if strings.TrimSpace(mode) != "nftables" {
				ginkgo.Skip("the current cluster is not using kube-proxy nftables mode")
			}

			ginkgo.By("checking the Kube-OVN nftables table")
			if f.HasIPv4() {
				_, _, err = framework.ExecShellInContainer(f, pod.Namespace, pod.Name, "cni-server", "nft list table ip kube-ovn")
				framework.ExpectNoError(err)
			}
			if f.HasIPv6() {
				_, _, err = framework.ExecShellInContainer(f, pod.Namespace, pod.Name, "cni-server", "nft list table ip6 kube-ovn")
				framework.ExpectNoError(err)
			}

			ginkgo.By("checking the backend metric")
			metricsURL := framework.GatewayMetricsURL(pod.Status.PodIP)
			metrics, _, err := framework.ExecShellInContainer(f, pod.Namespace, pod.Name, "cni-server", "curl -fsS "+metricsURL)
			framework.ExpectNoError(err)
			framework.ExpectContainSubstring(metrics, `kube_ovn_gateway_netfilter_backend{backend="nftables"} 1`)

			ginkgo.By("checking that old iptables and ipset objects are removed")
			_, _, err = framework.ExecShellInContainer(f, pod.Namespace, pod.Name, "cni-server", "! iptables-save | grep -E 'OVN-|ovn40' && ! ip6tables-save | grep -E 'OVN-|ovn60' && ! ipset list -name | grep -E '^ovn(40|60)'")
			framework.ExpectNoError(err)
		}
	})
})
