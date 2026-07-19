package nftables

import (
	"fmt"
	"strings"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:nftables]", func() {
	f := framework.NewDefaultFramework("nftables")

	framework.ConformanceIt("应在 kube-proxy nftables 模式下启用并清理旧后端", func() {
		f.SkipVersionPriorTo(1, 17, "Kube-OVN nftables 网关后端从 v1.17 起支持")

		daemonSetClient := f.DaemonSetClientNS("kube-system")
		daemonSet := daemonSetClient.Get("kube-ovn-cni")
		pods, err := daemonSetClient.GetPods(daemonSet)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(pods.Items)

		for _, pod := range pods.Items {
			mode, _, err := framework.ExecShellInContainer(f, pod.Namespace, pod.Name, "cni-server", "curl -fsS http://localhost:10249/proxyMode")
			framework.ExpectNoError(err)
			if strings.TrimSpace(mode) != "nftables" {
				ginkgo.Skip("当前集群未使用 kube-proxy nftables 模式")
			}

			ginkgo.By("核对 Kube-OVN nft table")
			if f.HasIPv4() {
				_, _, err = framework.ExecShellInContainer(f, pod.Namespace, pod.Name, "cni-server", "nft list table ip kube-ovn")
				framework.ExpectNoError(err)
			}
			if f.HasIPv6() {
				_, _, err = framework.ExecShellInContainer(f, pod.Namespace, pod.Name, "cni-server", "nft list table ip6 kube-ovn")
				framework.ExpectNoError(err)
			}

			ginkgo.By("核对 backend metric")
			metricsURL := fmt.Sprintf("http://%s:10665/metrics", pod.Status.PodIP)
			metrics, _, err := framework.ExecShellInContainer(f, pod.Namespace, pod.Name, "cni-server", "curl -fsS "+metricsURL)
			framework.ExpectNoError(err)
			framework.ExpectContainSubstring(metrics, `kube_ovn_gateway_netfilter_backend{backend="nftables"} 1`)

			ginkgo.By("核对旧 iptables 和 ipset 对象已清理")
			_, _, err = framework.ExecShellInContainer(f, pod.Namespace, pod.Name, "cni-server", "! iptables-save | grep -E 'OVN-|ovn40' && ! ip6tables-save | grep -E 'OVN-|ovn60' && ! ipset list -name | grep -E '^ovn(40|60)'")
			framework.ExpectNoError(err)
		}
	})
})
