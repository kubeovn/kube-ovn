package ofctl

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

var _ = Describe("[ovs-ofctl]", func() {

	flow_with_tos := "cookie=0x0, duration=33.091s, table=0, n_packets=0, n_bytes=0, ip,nw_tos=32 actions=mod_vlan_pcp:2,NORMAL"
	flow_without_tos := "cookie=0x0, duration=1857.598s, table=0, n_packets=59697, n_bytes=22576303, priority=0 actions=NORMAL"

	Describe("[dump-flows]", func() {
		Context("[parse-flows]", func() {
			It("flow with ip_tos", func() {
				By("parse flow with ip_tos 32")
				values := ovs.ParseDumpFlowsOutput(flow_with_tos)
				Expect(len(values)).To(Equal(1))
				Expect(values[0]).To(Equal("32"))
			})

			It("flow without ip_tos", func() {
				By("parse flow without ip_tos")
				values := ovs.ParseDumpFlowsOutput(flow_without_tos)
				Expect(len(values)).To(Equal(0))
			})
		})
	})
})
