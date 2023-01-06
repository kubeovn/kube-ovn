package ofctl

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

var _ = Describe("[ovs-ofctl]", func() {

	flow_with_mark := "cookie=0x0, duration=33.091s, table=0, n_packets=0, n_bytes=0, pkt_mark=0x2 actions=mod_vlan_pcp:2,NORMAL"
	flow_without_mark := "cookie=0x0, duration=1857.598s, table=0, n_packets=59697, n_bytes=22576303, priority=0 actions=NORMAL"

	Describe("[dump-flows]", func() {
		Context("[parse-flows]", func() {
			It("flow with pkt_mark", func() {
				By("parse flow with pkt_mark 0x2")
				marks := ovs.ParseDumpFlowsOutput(flow_with_mark)
				Expect(len(marks)).To(Equal(1))
				Expect(marks[0]).To(Equal("0x2"))
			})

			It("flow without pkt_mark", func() {
				By("parse flow without pkt_mark")
				marks := ovs.ParseDumpFlowsOutput(flow_without_mark)
				Expect(len(marks)).To(Equal(0))
			})
		})
	})
})
