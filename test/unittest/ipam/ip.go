package ipam

import (
	"fmt"
	"math/rand/v2"
	"net"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/ipam"
)

func uint32ToIPv4(n uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d", n>>24, n&0xff0000>>16, n&0xff00>>8, n&0xff)
}

func ipv4ToUint32(ip net.IP) uint32 {
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func uint32ToIPv6(n [4]uint32) string {
	return fmt.Sprintf(
		"%04x:%04x:%04x:%04x:%04x:%04x:%04x:%04x",
		n[0]>>16, n[0]&0xffff,
		n[1]>>16, n[1]&0xffff,
		n[2]>>16, n[2]&0xffff,
		n[3]>>16, n[3]&0xffff,
	)
}

var _ = ginkgo.Context("[group:IPAM]", func() {
	ginkgo.Context("[IP]", func() {
		ginkgo.It("IPv4", func() {
			n1 := rand.Uint32()
			if n1 == 0xffffffff {
				n1--
			}
			n2 := n1 + 1

			ip1Str := uint32ToIPv4(n1)
			ip2Str := uint32ToIPv4(n2)

			ip1, err := ipam.NewIP(ip1Str)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(ip1.String()).To(gomega.Equal(ip1Str))
			ip2, err := ipam.NewIP(ip2Str)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(ip2.String()).To(gomega.Equal(ip2Str))

			gomega.Expect(ip1.Equal(ip2)).To(gomega.BeFalse())
			gomega.Expect(ip1.GreaterThan(ip2)).To(gomega.BeFalse())
			gomega.Expect(ip1.LessThan(ip2)).To(gomega.BeTrue())

			gomega.Expect(ip1.Add(1)).To(gomega.Equal(ip2))
			gomega.Expect(ip2.Add(-1)).To(gomega.Equal(ip1))
			gomega.Expect(ip1.Sub(-1)).To(gomega.Equal(ip2))
			gomega.Expect(ip2.Sub(1)).To(gomega.Equal(ip1))
		})

		ginkgo.It("IPv6", func() {
			n1 := [4]uint32{rand.Uint32(), rand.Uint32(), rand.Uint32(), rand.Uint32()}
			if n1[0] == 0xffffffff && n1[1] == 0xffffffff && n1[2] == 0xffffffff && n1[3] == 0xffffffff {
				n1[3]--
			}
			n2 := [4]uint32{n1[0], n1[1], n1[2], n1[3] + 1}

			ip1Str := uint32ToIPv6(n1)
			ip2Str := uint32ToIPv6(n2)

			ip1, err := ipam.NewIP(ip1Str)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			ip2, err := ipam.NewIP(ip2Str)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ip1Str = net.ParseIP(ip1Str).String()
			ip2Str = net.ParseIP(ip2Str).String()
			gomega.Expect(ip1.String()).To(gomega.Equal(ip1Str))
			gomega.Expect(ip2.String()).To(gomega.Equal(ip2Str))

			ip1, err = ipam.NewIP(ip1Str)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(ip1.String()).To(gomega.Equal(net.ParseIP(ip1Str).String()))
			ip2, err = ipam.NewIP(ip2Str)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(ip2.String()).To(gomega.Equal(net.ParseIP(ip2Str).String()))

			gomega.Expect(ip1.Equal(ip2)).To(gomega.BeFalse())
			gomega.Expect(ip1.GreaterThan(ip2)).To(gomega.BeFalse())
			gomega.Expect(ip1.LessThan(ip2)).To(gomega.BeTrue())

			gomega.Expect(ip1.Add(1)).To(gomega.Equal(ip2))
			gomega.Expect(ip2.Add(-1)).To(gomega.Equal(ip1))
			gomega.Expect(ip1.Sub(-1)).To(gomega.Equal(ip2))
			gomega.Expect(ip2.Sub(1)).To(gomega.Equal(ip1))
		})
	})
})
