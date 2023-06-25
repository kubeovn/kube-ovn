package ipam

import (
	"fmt"
	"math/big"
	"math/rand"
	"net"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/ipam"
)

var _ = ginkgo.Context("[group:IPAM]", func() {
	ginkgo.Context("[IPRange]", func() {
		ginkgo.It("IPv4", func() {
			n1, n2 := rand.Uint32(), rand.Uint32()
			if n1 > n2 {
				n1, n2 = n2, n1
			}
			n := n1 + uint32(rand.Int63n(int64(n2-n1+1)))
			startStr := uint32ToIPv4(n1)
			endStr := uint32ToIPv4(n2)
			ipStr := uint32ToIPv4(n)

			start, err := ipam.NewIP(startStr)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			end, err := ipam.NewIP(endStr)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			ip, err := ipam.NewIP(ipStr)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			r := ipam.NewIPRange(start, end)
			if n1 == n2 {
				gomega.Expect(r.String()).To(gomega.Equal(start.String()))
			} else {
				gomega.Expect(r.String()).To(gomega.Equal(fmt.Sprintf("%s-%s", start.String(), end.String())))
			}

			c := r.Count()
			gomega.Expect(c.Int.Cmp(big.NewInt(int64(n2 - n1 + 1)))).To(gomega.Equal(0))
			gomega.Expect(r.Clone().String()).To(gomega.Equal(r.String()))
			gomega.Expect(r.Contains(start)).To(gomega.BeTrue())
			gomega.Expect(r.Contains(end)).To(gomega.BeTrue())
			gomega.Expect(r.Contains(ip)).To(gomega.BeTrue())
			if n1 != 0 {
				gomega.Expect(r.Contains(start.Sub(1))).To(gomega.BeFalse())
			}
			if n2 != 0xffffffff {
				gomega.Expect(r.Contains(end.Add(1))).To(gomega.BeFalse())
			}
			if n1 != n2 {
				gomega.Expect(r.Contains(start.Add(1))).To(gomega.BeTrue())
				gomega.Expect(r.Contains(end.Sub(1))).To(gomega.BeTrue())
			}
		})

		ginkgo.It("IPv6", func() {
			n1 := [4]uint32{rand.Uint32(), rand.Uint32(), rand.Uint32(), rand.Uint32()}
			n2 := [4]uint32{rand.Uint32(), rand.Uint32(), rand.Uint32(), rand.Uint32()}
			for i := 0; i < 4; i++ {
				if n1[i] < n2[i] {
					break
				}
				if n1[i] > n2[i] {
					n1, n2 = n2, n1
					break
				}
			}

			var n [4]uint32
			for i := 0; i < 4; i++ {
				n[i] = n1[i] + uint32(rand.Int63n(int64(n2[i]-n1[i]+1)))
			}

			startStr := uint32ToIPv6(n1)
			endStr := uint32ToIPv6(n2)
			ipStr := uint32ToIPv6(n)

			start, err := ipam.NewIP(startStr)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			end, err := ipam.NewIP(endStr)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			ip, err := ipam.NewIP(ipStr)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			r := ipam.NewIPRange(start, end)
			if n1 == n2 {
				gomega.Expect(r.String()).To(gomega.Equal(start.String()))
			} else {
				gomega.Expect(r.String()).To(gomega.Equal(fmt.Sprintf("%s-%s", start.String(), end.String())))
			}

			count := r.Count()
			expectedCount := big.NewInt(0).Sub(big.NewInt(0).SetBytes(net.ParseIP(endStr).To16()), big.NewInt(0).SetBytes(net.ParseIP(startStr).To16()))
			expectedCount.Add(expectedCount, big.NewInt(1))

			gomega.Expect(count.Int.Cmp(expectedCount)).To(gomega.Equal(0))
			gomega.Expect(r.Clone().String()).To(gomega.Equal(r.String()))
			gomega.Expect(r.Contains(start)).To(gomega.BeTrue())
			gomega.Expect(r.Contains(end)).To(gomega.BeTrue())
			gomega.Expect(r.Contains(ip)).To(gomega.BeTrue())
			if n1 != [4]uint32{0, 0, 0, 0} {
				gomega.Expect(r.Contains(start.Sub(1))).To(gomega.BeFalse())
			}
			if n2 != [4]uint32{0xffffffff, 0xffffffff, 0xffffffff, 0xffffffff} {
				gomega.Expect(r.Contains(end.Add(1))).To(gomega.BeFalse())
			}
			if n1 != n2 {
				gomega.Expect(r.Contains(start.Add(1))).To(gomega.BeTrue())
				gomega.Expect(r.Contains(end.Sub(1))).To(gomega.BeTrue())
			}
		})
	})
})
