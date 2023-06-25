package ipam

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"

	"github.com/scylladb/go-set/strset"
	"github.com/scylladb/go-set/u32set"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/ipam"
)

var _ = ginkgo.Context("[group:IPAM]", func() {
	ginkgo.Context("[IPRangeList]", func() {
		newIP := func(s string) ipam.IP {
			ip, err := ipam.NewIP(s)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			return ip
		}

		ginkgo.It("Contains", func() {
			v, err := ipam.NewIPRangeList(
				newIP("10.0.0.5"), newIP("10.0.0.5"),
				newIP("10.0.0.13"), newIP("10.0.0.18"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(v.Contains(newIP("10.0.0.4"))).To(gomega.BeFalse())
			gomega.Expect(v.Contains(newIP("10.0.0.5"))).To(gomega.BeTrue())
			gomega.Expect(v.Contains(newIP("10.0.0.6"))).To(gomega.BeFalse())

			gomega.Expect(v.Contains(newIP("10.0.0.12"))).To(gomega.BeFalse())
			gomega.Expect(v.Contains(newIP("10.0.0.13"))).To(gomega.BeTrue())
			gomega.Expect(v.Contains(newIP("10.0.0.14"))).To(gomega.BeTrue())
			gomega.Expect(v.Contains(newIP("10.0.0.17"))).To(gomega.BeTrue())
			gomega.Expect(v.Contains(newIP("10.0.0.18"))).To(gomega.BeTrue())
			gomega.Expect(v.Contains(newIP("10.0.0.19"))).To(gomega.BeFalse())
		})

		ginkgo.It("Add", func() {
			v, err := ipam.NewIPRangeList(
				newIP("10.0.0.5"), newIP("10.0.0.5"),
				newIP("10.0.0.13"), newIP("10.0.0.18"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(v.Add(newIP("10.0.0.4"))).To(gomega.BeTrue())
			gomega.Expect(v.Add(newIP("10.0.0.5"))).To(gomega.BeFalse())
			gomega.Expect(v.Add(newIP("10.0.0.6"))).To(gomega.BeTrue())

			gomega.Expect(v.Add(newIP("10.0.0.12"))).To(gomega.BeTrue())
			gomega.Expect(v.Add(newIP("10.0.0.13"))).To(gomega.BeFalse())
			gomega.Expect(v.Add(newIP("10.0.0.14"))).To(gomega.BeFalse())
			gomega.Expect(v.Add(newIP("10.0.0.17"))).To(gomega.BeFalse())
			gomega.Expect(v.Add(newIP("10.0.0.18"))).To(gomega.BeFalse())
			gomega.Expect(v.Add(newIP("10.0.0.19"))).To(gomega.BeTrue())

			expected, err := ipam.NewIPRangeList(
				newIP("10.0.0.4"), newIP("10.0.0.6"),
				newIP("10.0.0.12"), newIP("10.0.0.19"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(v.Equal(expected)).To(gomega.BeTrue())
		})

		ginkgo.It("Remove", func() {
			v, err := ipam.NewIPRangeList(
				newIP("10.0.0.5"), newIP("10.0.0.5"),
				newIP("10.0.0.13"), newIP("10.0.0.18"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(v.Remove(newIP("10.0.0.4"))).To(gomega.BeFalse())
			gomega.Expect(v.Remove(newIP("10.0.0.5"))).To(gomega.BeTrue())
			gomega.Expect(v.Remove(newIP("10.0.0.6"))).To(gomega.BeFalse())

			gomega.Expect(v.Remove(newIP("10.0.0.12"))).To(gomega.BeFalse())
			gomega.Expect(v.Remove(newIP("10.0.0.13"))).To(gomega.BeTrue())
			gomega.Expect(v.Remove(newIP("10.0.0.14"))).To(gomega.BeTrue())
			gomega.Expect(v.Remove(newIP("10.0.0.17"))).To(gomega.BeTrue())
			gomega.Expect(v.Remove(newIP("10.0.0.18"))).To(gomega.BeTrue())
			gomega.Expect(v.Remove(newIP("10.0.0.19"))).To(gomega.BeFalse())

			expected, err := ipam.NewIPRangeList(
				newIP("10.0.0.15"), newIP("10.0.0.16"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(v.Equal(expected)).To(gomega.BeTrue())
		})

		ginkgo.It("Allocate", func() {
			v, err := ipam.NewIPRangeList(
				newIP("10.0.0.13"), newIP("10.0.0.16"),
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ip := v.Allocate(nil)
			gomega.Expect(ip).NotTo(gomega.BeNil())
			gomega.Expect(ip.String()).To(gomega.Equal("10.0.0.13"))

			ip = v.Allocate(nil)
			gomega.Expect(ip).NotTo(gomega.BeNil())
			gomega.Expect(ip.String()).To(gomega.Equal("10.0.0.14"))

			ip = v.Allocate([]ipam.IP{newIP("10.0.0.15"), newIP("10.0.0.16")})
			gomega.Expect(ip).To(gomega.BeNil())
		})

		ginkgo.It("Separate", func() {
			v1, err := ipam.NewIPRangeList(
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

			v2, err := ipam.NewIPRangeList(
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

			separated := v1.Separate(v2)
			gomega.Expect(separated.Equal(expected)).To(gomega.BeTrue())
		})

		ginkgo.It("Merge", func() {
			v1, err := ipam.NewIPRangeList(
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

			v2, err := ipam.NewIPRangeList(
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

			merged := v1.Merge(v2)
			gomega.Expect(merged.Equal(expected)).To(gomega.BeTrue())
		})

	})

	ginkgo.It("NewIPRangeListFrom", func() {
		n := 100 + rand.Intn(50)
		set := u32set.NewWithSize(n)
		for set.Size() != n {
			set.Add(rand.Uint32())
		}

		ints := set.List()
		sort.Slice(ints, func(i, j int) bool { return ints[i] < ints[j] })

		var ips, mergedIPs []string
		var expectedCount uint32
		for i := 0; i < len(ints); i++ {
			start := uint32ToIPv4(ints[i])
			var merged string
			if rand.Int()%2 == 0 && i+1 != len(ints) {
				end := uint32ToIPv4(ints[i+1])
				ips = append(ips, fmt.Sprintf("%s..%s", start, end))
				if i != 0 && ints[i] == ints[i-1]+1 {
					merged = fmt.Sprintf("%s..%s", strings.Split(mergedIPs[len(mergedIPs)-1], "..")[0], end)
				}
				expectedCount += ints[i+1] - ints[i] + 1
				i++
			} else {
				ips = append(ips, start)
				if i != 0 && ints[i] == ints[i-1]+1 {
					merged = fmt.Sprintf("%s..%s", strings.Split(mergedIPs[len(mergedIPs)-1], "..")[0], start)
				}
				expectedCount++
			}

			if merged != "" {
				mergedIPs[len(mergedIPs)-1] = merged
			} else {
				mergedIPs = append(mergedIPs, ips[len(ips)-1])
			}
		}

		list, err := ipam.NewIPRangeListFrom(strset.New(ips...).List()...)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(list.Len()).To(gomega.Equal(len(mergedIPs)))
		gomega.Expect(list.String()).To(gomega.Equal(strings.ReplaceAll(strings.Join(mergedIPs, ","), "..", "-")))

		count := list.Count()
		gomega.Expect(count.Int64()).To(gomega.Equal(int64(expectedCount)))

		for _, s := range mergedIPs {
			fields := strings.Split(s, "..")
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
})
