package ipam

import (
	"fmt"
	"math/rand/v2"
	"net"
	"slices"
	"strings"

	"github.com/scylladb/go-set/strset"
	"github.com/scylladb/go-set/u32set"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var _ = ginkgo.Context("[group:IPAM]", func() {
	ginkgo.Context("[IPRangeList]", func() {
		newIP := func(s string) ipam.IP {
			ginkgo.GinkgoHelper()
			ip, err := ipam.NewIP(s)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			return ip
		}

		ginkgo.It("IPv4 Contains", func() {
			v4, err := ipam.NewIPRangeList(
				newIP("10.0.0.5"), newIP("10.0.0.5"),
				newIP("10.0.0.13"), newIP("10.0.0.18"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(v4.Contains(newIP("10.0.0.4"))).To(gomega.BeFalse())
			gomega.Expect(v4.Contains(newIP("10.0.0.5"))).To(gomega.BeTrue())
			gomega.Expect(v4.Contains(newIP("10.0.0.6"))).To(gomega.BeFalse())

			gomega.Expect(v4.Contains(newIP("10.0.0.12"))).To(gomega.BeFalse())
			gomega.Expect(v4.Contains(newIP("10.0.0.13"))).To(gomega.BeTrue())
			gomega.Expect(v4.Contains(newIP("10.0.0.14"))).To(gomega.BeTrue())
			gomega.Expect(v4.Contains(newIP("10.0.0.17"))).To(gomega.BeTrue())
			gomega.Expect(v4.Contains(newIP("10.0.0.18"))).To(gomega.BeTrue())
			gomega.Expect(v4.Contains(newIP("10.0.0.19"))).To(gomega.BeFalse())
		})

		ginkgo.It("IPv6 Contains", func() {
			v6, err := ipam.NewIPRangeList(
				newIP("2001:db8::5"), newIP("2001:db8::5"),
				newIP("2001:db8::13"), newIP("2001:db8::18"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(v6.Contains(newIP("2001:db8::4"))).To(gomega.BeFalse())
			gomega.Expect(v6.Contains(newIP("2001:db8::5"))).To(gomega.BeTrue())
			gomega.Expect(v6.Contains(newIP("2001:db8::6"))).To(gomega.BeFalse())

			gomega.Expect(v6.Contains(newIP("2001:db8::12"))).To(gomega.BeFalse())
			gomega.Expect(v6.Contains(newIP("2001:db8::13"))).To(gomega.BeTrue())
			gomega.Expect(v6.Contains(newIP("2001:db8::14"))).To(gomega.BeTrue())
			gomega.Expect(v6.Contains(newIP("2001:db8::17"))).To(gomega.BeTrue())
			gomega.Expect(v6.Contains(newIP("2001:db8::18"))).To(gomega.BeTrue())
			gomega.Expect(v6.Contains(newIP("2001:db8::19"))).To(gomega.BeFalse())
		})

		ginkgo.It("IPv4 Add", func() {
			v4, err := ipam.NewIPRangeList(
				newIP("10.0.0.5"), newIP("10.0.0.5"),
				newIP("10.0.0.13"), newIP("10.0.0.18"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(v4.Add(newIP("10.0.0.4"))).To(gomega.BeTrue())
			gomega.Expect(v4.Add(newIP("10.0.0.5"))).To(gomega.BeFalse())
			gomega.Expect(v4.Add(newIP("10.0.0.6"))).To(gomega.BeTrue())

			gomega.Expect(v4.Add(newIP("10.0.0.12"))).To(gomega.BeTrue())
			gomega.Expect(v4.Add(newIP("10.0.0.13"))).To(gomega.BeFalse())
			gomega.Expect(v4.Add(newIP("10.0.0.14"))).To(gomega.BeFalse())
			gomega.Expect(v4.Add(newIP("10.0.0.17"))).To(gomega.BeFalse())
			gomega.Expect(v4.Add(newIP("10.0.0.18"))).To(gomega.BeFalse())
			gomega.Expect(v4.Add(newIP("10.0.0.19"))).To(gomega.BeTrue())

			expected, err := ipam.NewIPRangeList(
				newIP("10.0.0.4"), newIP("10.0.0.6"),
				newIP("10.0.0.12"), newIP("10.0.0.19"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(v4.Equal(expected)).To(gomega.BeTrue())
		})

		ginkgo.It("IPv6 Add", func() {
			v6, err := ipam.NewIPRangeList(
				newIP("2001:db8::5"), newIP("2001:db8::5"),
				newIP("2001:db8::13"), newIP("2001:db8::18"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(v6.Add(newIP("2001:db8::4"))).To(gomega.BeTrue())
			gomega.Expect(v6.Add(newIP("2001:db8::5"))).To(gomega.BeFalse())
			gomega.Expect(v6.Add(newIP("2001:db8::6"))).To(gomega.BeTrue())

			gomega.Expect(v6.Add(newIP("2001:db8::12"))).To(gomega.BeTrue())
			gomega.Expect(v6.Add(newIP("2001:db8::13"))).To(gomega.BeFalse())
			gomega.Expect(v6.Add(newIP("2001:db8::14"))).To(gomega.BeFalse())
			gomega.Expect(v6.Add(newIP("2001:db8::17"))).To(gomega.BeFalse())
			gomega.Expect(v6.Add(newIP("2001:db8::18"))).To(gomega.BeFalse())
			gomega.Expect(v6.Add(newIP("2001:db8::19"))).To(gomega.BeTrue())

			expected, err := ipam.NewIPRangeList(
				newIP("2001:db8::4"), newIP("2001:db8::6"),
				newIP("2001:db8::12"), newIP("2001:db8::19"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(v6.Equal(expected)).To(gomega.BeTrue())
		})

		ginkgo.It("IPv4 Remove", func() {
			v4, err := ipam.NewIPRangeList(
				newIP("10.0.0.5"), newIP("10.0.0.5"),
				newIP("10.0.0.13"), newIP("10.0.0.18"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(v4.Remove(newIP("10.0.0.4"))).To(gomega.BeFalse())
			gomega.Expect(v4.Remove(newIP("10.0.0.5"))).To(gomega.BeTrue())
			gomega.Expect(v4.Remove(newIP("10.0.0.6"))).To(gomega.BeFalse())

			gomega.Expect(v4.Remove(newIP("10.0.0.12"))).To(gomega.BeFalse())
			gomega.Expect(v4.Remove(newIP("10.0.0.13"))).To(gomega.BeTrue())
			gomega.Expect(v4.Remove(newIP("10.0.0.14"))).To(gomega.BeTrue())
			gomega.Expect(v4.Remove(newIP("10.0.0.17"))).To(gomega.BeTrue())
			gomega.Expect(v4.Remove(newIP("10.0.0.18"))).To(gomega.BeTrue())
			gomega.Expect(v4.Remove(newIP("10.0.0.19"))).To(gomega.BeFalse())

			expected, err := ipam.NewIPRangeList(
				newIP("10.0.0.15"), newIP("10.0.0.16"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(v4.Equal(expected)).To(gomega.BeTrue())

			// split the range
			v4, err = ipam.NewIPRangeList(
				newIP("10.0.0.10"), newIP("10.0.0.20"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(v4.Remove(newIP("10.0.0.15"))).To(gomega.BeTrue())
			expected, err = ipam.NewIPRangeList(
				newIP("10.0.0.10"), newIP("10.0.0.14"),
				newIP("10.0.0.16"), newIP("10.0.0.20"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(v4.Equal(expected)).To(gomega.BeTrue())
		})

		ginkgo.It("IPv6 Remove", func() {
			v6, err := ipam.NewIPRangeList(
				newIP("2001:db8::5"), newIP("2001:db8::5"),
				newIP("2001:db8::13"), newIP("2001:db8::18"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(v6.Remove(newIP("2001:db8::4"))).To(gomega.BeFalse())
			gomega.Expect(v6.Remove(newIP("2001:db8::5"))).To(gomega.BeTrue())
			gomega.Expect(v6.Remove(newIP("2001:db8::6"))).To(gomega.BeFalse())

			gomega.Expect(v6.Remove(newIP("2001:db8::12"))).To(gomega.BeFalse())
			gomega.Expect(v6.Remove(newIP("2001:db8::13"))).To(gomega.BeTrue())
			gomega.Expect(v6.Remove(newIP("2001:db8::14"))).To(gomega.BeTrue())
			gomega.Expect(v6.Remove(newIP("2001:db8::17"))).To(gomega.BeTrue())
			gomega.Expect(v6.Remove(newIP("2001:db8::18"))).To(gomega.BeTrue())
			gomega.Expect(v6.Remove(newIP("2001:db8::19"))).To(gomega.BeFalse())

			expected, err := ipam.NewIPRangeList(
				newIP("2001:db8::15"), newIP("2001:db8::16"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(v6.Equal(expected)).To(gomega.BeTrue())

			// split the range
			v6, err = ipam.NewIPRangeList(
				newIP("2001:db8::10"), newIP("2001:db8::20"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(v6.Remove(newIP("2001:db8::15"))).To(gomega.BeTrue())
			expected, err = ipam.NewIPRangeList(
				newIP("2001:db8::10"), newIP("2001:db8::14"),
				newIP("2001:db8::16"), newIP("2001:db8::20"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(v6.Equal(expected)).To(gomega.BeTrue())
		})

		ginkgo.It("IPv4 Allocate", func() {
			v4, err := ipam.NewIPRangeList(
				newIP("10.0.0.13"), newIP("10.0.0.16"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ip := v4.Allocate(nil)
			gomega.Expect(ip).NotTo(gomega.BeNil())
			gomega.Expect(ip.String()).To(gomega.Equal("10.0.0.13"))

			ip = v4.Allocate(nil)
			gomega.Expect(ip).NotTo(gomega.BeNil())
			gomega.Expect(ip.String()).To(gomega.Equal("10.0.0.14"))

			ip = v4.Allocate([]ipam.IP{newIP("10.0.0.15"), newIP("10.0.0.16")})
			gomega.Expect(ip).To(gomega.BeNil())
		})

		ginkgo.It("IPv4 Separate", func() {
			v41, err := ipam.NewIPRangeList(
				newIP("10.0.0.1"), newIP("10.0.0.1"),
				newIP("10.0.0.5"), newIP("10.0.0.5"),
				newIP("10.0.0.13"), newIP("10.0.0.18"),
				newIP("10.0.0.23"), newIP("10.0.0.28"),
				newIP("10.0.0.33"), newIP("10.0.0.38"),
				newIP("10.0.0.43"), newIP("10.0.0.48"),
				newIP("10.0.0.53"), newIP("10.0.0.58"),
				newIP("10.0.0.63"), newIP("10.0.0.68"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			v42, err := ipam.NewIPRangeList(
				newIP("10.0.0.1"), newIP("10.0.0.1"),
				newIP("10.0.0.11"), newIP("10.0.0.15"),
				newIP("10.0.0.17"), newIP("10.0.0.19"),
				newIP("10.0.0.23"), newIP("10.0.0.25"),
				newIP("10.0.0.27"), newIP("10.0.0.28"),
				newIP("10.0.0.33"), newIP("10.0.0.38"),
				newIP("10.0.0.42"), newIP("10.0.0.49"),
				newIP("10.0.0.53"), newIP("10.0.0.58"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			expected, err := ipam.NewIPRangeList(
				newIP("10.0.0.5"), newIP("10.0.0.5"),
				newIP("10.0.0.16"), newIP("10.0.0.16"),
				newIP("10.0.0.26"), newIP("10.0.0.26"),
				newIP("10.0.0.63"), newIP("10.0.0.68"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			separated := v41.Separate(v42)
			gomega.Expect(separated.Equal(expected)).To(gomega.BeTrue())
		})

		ginkgo.It("IPv6 Separate", func() {
			v61, err := ipam.NewIPRangeList(
				newIP("2001:db8::1"), newIP("2001:db8::1"),
				newIP("2001:db8::5"), newIP("2001:db8::5"),
				newIP("2001:db8::13"), newIP("2001:db8::18"),
				newIP("2001:db8::23"), newIP("2001:db8::28"),
				newIP("2001:db8::33"), newIP("2001:db8::38"),
				newIP("2001:db8::43"), newIP("2001:db8::48"),
				newIP("2001:db8::53"), newIP("2001:db8::58"),
				newIP("2001:db8::63"), newIP("2001:db8::68"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			v62, err := ipam.NewIPRangeList(
				newIP("2001:db8::1"), newIP("2001:db8::1"),
				newIP("2001:db8::11"), newIP("2001:db8::15"),
				newIP("2001:db8::17"), newIP("2001:db8::19"),
				newIP("2001:db8::23"), newIP("2001:db8::25"),
				newIP("2001:db8::27"), newIP("2001:db8::28"),
				newIP("2001:db8::33"), newIP("2001:db8::38"),
				newIP("2001:db8::42"), newIP("2001:db8::49"),
				newIP("2001:db8::53"), newIP("2001:db8::58"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			expected, err := ipam.NewIPRangeList(
				newIP("2001:db8::5"), newIP("2001:db8::5"),
				newIP("2001:db8::16"), newIP("2001:db8::16"),
				newIP("2001:db8::26"), newIP("2001:db8::26"),
				newIP("2001:db8::63"), newIP("2001:db8::68"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			separated := v61.Separate(v62)
			gomega.Expect(separated.Equal(expected)).To(gomega.BeTrue())
		})

		ginkgo.It("IPv4 Merge", func() {
			v41, err := ipam.NewIPRangeList(
				newIP("10.0.0.1"), newIP("10.0.0.1"),
				newIP("10.0.0.3"), newIP("10.0.0.3"),
				newIP("10.0.0.5"), newIP("10.0.0.5"),
				newIP("10.0.0.13"), newIP("10.0.0.18"),
				newIP("10.0.0.23"), newIP("10.0.0.28"),
				newIP("10.0.0.33"), newIP("10.0.0.38"),
				newIP("10.0.0.43"), newIP("10.0.0.48"),
				newIP("10.0.0.53"), newIP("10.0.0.58"),
				newIP("10.0.0.63"), newIP("10.0.0.68"),
				newIP("10.0.0.73"), newIP("10.0.0.78"),
				newIP("10.0.0.83"), newIP("10.0.0.88"),
				newIP("10.0.0.93"), newIP("10.0.0.95"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			v42, err := ipam.NewIPRangeList(
				newIP("10.0.0.1"), newIP("10.0.0.1"),
				newIP("10.0.0.4"), newIP("10.0.0.4"),
				newIP("10.0.0.11"), newIP("10.0.0.15"),
				newIP("10.0.0.17"), newIP("10.0.0.19"),
				newIP("10.0.0.23"), newIP("10.0.0.25"),
				newIP("10.0.0.27"), newIP("10.0.0.28"),
				newIP("10.0.0.33"), newIP("10.0.0.38"),
				newIP("10.0.0.42"), newIP("10.0.0.49"),
				newIP("10.0.0.53"), newIP("10.0.0.58"),
				newIP("10.0.0.75"), newIP("10.0.0.85"),
				newIP("10.0.0.96"), newIP("10.0.0.98"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			expected, err := ipam.NewIPRangeList(
				newIP("10.0.0.1"), newIP("10.0.0.1"),
				newIP("10.0.0.3"), newIP("10.0.0.5"),
				newIP("10.0.0.11"), newIP("10.0.0.19"),
				newIP("10.0.0.23"), newIP("10.0.0.28"),
				newIP("10.0.0.33"), newIP("10.0.0.38"),
				newIP("10.0.0.42"), newIP("10.0.0.49"),
				newIP("10.0.0.53"), newIP("10.0.0.58"),
				newIP("10.0.0.63"), newIP("10.0.0.68"),
				newIP("10.0.0.73"), newIP("10.0.0.88"),
				newIP("10.0.0.93"), newIP("10.0.0.98"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			merged := v41.Merge(v42)
			gomega.Expect(merged.Equal(expected)).To(gomega.BeTrue())
		})
	})

	ginkgo.It("IPv4 NewIPRangeListFrom", func() {
		n := 40 + rand.IntN(20)
		cidrList := make([]*net.IPNet, 0, n)
		cidrSet := u32set.NewWithSize(n * 2)
		for len(cidrList) != cap(cidrList) {
			_, cidr, err := net.ParseCIDR(fmt.Sprintf("%s/%d", util.Uint32ToIPv4(rand.Uint32()), 16+rand.IntN(16)))
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			var invalid bool
			for _, c := range cidrList {
				if c.Contains(cidr.IP) || cidr.Contains(c.IP) {
					invalid = true
					break
				}
			}
			if !invalid {
				cidrList = append(cidrList, cidr)
				cidrSet.Add(util.IPv4ToUint32(cidr.IP))
				bcast := make(net.IP, len(cidr.IP))
				for i := range bcast {
					bcast[i] = cidr.IP[i] | ^cidr.Mask[i]
				}
				cidrSet.Add(util.IPv4ToUint32(bcast))
			}
		}

		n = 80 + rand.IntN(40)
		set := u32set.NewWithSize(cidrSet.Size() + n)
		for set.Size() != n {
			v := rand.Uint32()
			ip := net.ParseIP(util.Uint32ToIPv4(v))
			var invalid bool
			for _, cidr := range cidrList {
				if cidr.Contains(ip) {
					invalid = true
					break
				}
			}
			if !invalid {
				set.Add(v)
			}
		}
		set.Merge(cidrSet)

		ints := set.List()
		slices.Sort(ints)

		ips := make([]string, 0, len(cidrList)+set.Size())
		mergedInts := make([]uint32, 0, set.Size()*2)
		var expectedCount uint32
		for i := 0; i < len(ints); i++ {
			if cidrSet.Has(ints[i]) {
				expectedCount += ints[i+1] - ints[i] + 1
				if i != 0 && ints[i] == ints[i-1]+1 {
					mergedInts[len(mergedInts)-1] = ints[i+1]
				} else {
					mergedInts = append(mergedInts, ints[i], ints[i+1])
				}
				i++
				continue
			}

			start := util.Uint32ToIPv4(ints[i])
			if cidrSet.Has(ints[i]) || (rand.Int()%2 == 0 && i+1 != len(ints) && !cidrSet.Has(ints[i+1])) {
				if !cidrSet.Has(ints[i]) {
					end := util.Uint32ToIPv4(ints[i+1])
					ips = append(ips, fmt.Sprintf("%s..%s", start, end))
				}
				if i != 0 && ints[i] == ints[i-1]+1 {
					mergedInts[len(mergedInts)-1] = ints[i+1]
				} else {
					mergedInts = append(mergedInts, ints[i], ints[i+1])
				}
				expectedCount += ints[i+1] - ints[i] + 1
				i++
			} else {
				if rand.Int()%8 == 0 {
					start += "/32"
				}
				ips = append(ips, start)
				if i != 0 && ints[i] == ints[i-1]+1 {
					mergedInts[len(mergedInts)-1] = ints[i]
				} else {
					mergedInts = append(mergedInts, ints[i], ints[i])
				}
				expectedCount++
			}
		}

		for _, cidr := range cidrList {
			ips = append(ips, cidr.String())
		}

		mergedIPs := make([]string, len(mergedInts)/2)
		for i := range len(mergedInts) / 2 {
			if mergedInts[i*2] == mergedInts[i*2+1] {
				mergedIPs[i] = util.Uint32ToIPv4(mergedInts[i*2])
			} else {
				mergedIPs[i] = fmt.Sprintf("%s-%s", util.Uint32ToIPv4(mergedInts[i*2]), util.Uint32ToIPv4(mergedInts[i*2+1]))
			}
		}

		list, err := ipam.NewIPRangeListFrom(strset.New(ips...).List()...)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(list.Len()).To(gomega.Equal(len(mergedIPs)))
		gomega.Expect(list.String()).To(gomega.Equal(strings.Join(mergedIPs, ",")))

		count := list.Count()
		gomega.Expect(count.Int64()).To(gomega.Equal(int64(expectedCount)))

		for _, s := range mergedIPs {
			fields := strings.Split(s, "-")
			start, err := ipam.NewIP(fields[0])
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(list.Contains(start)).To(gomega.BeTrue())

			end := start
			if len(fields) != 1 {
				end, err = ipam.NewIP(fields[1])
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(list.Contains(end)).To(gomega.BeTrue())
			}

			if start.String() != "0.0.0.0" {
				gomega.Expect(list.Contains(start.Sub(1))).To(gomega.BeFalse())
			}
			if end.String() != "255.255.255.255" {
				gomega.Expect(list.Contains(end.Add(1))).To(gomega.BeFalse())
			}

			if !start.Equal(end) {
				gomega.Expect(list.Contains(start.Add(1))).To(gomega.BeTrue())
				gomega.Expect(list.Contains(end.Sub(1))).To(gomega.BeTrue())
			}
		}
	})
	ginkgo.It("IPv6 NewIPRangeListFrom", func() {
		// TODO
	})
})
