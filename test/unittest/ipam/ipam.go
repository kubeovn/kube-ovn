package ipam

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/ipam"
)

var _ = Describe("[IPAM]", func() {
	subnetName := "test"
	ipv4CIDR, ipv6CIDR := "10.16.0.0/16", "fd00::/112"
	ipv4ExcludeIPs := []string{
		"10.16.0.1",
		"10.16.0.10..10.16.0.20",
		"10.16.0.15..10.16.0.23",
		"10.16.0.4",
		"192.168.0.1..192.168.0.10",
	}
	ipv6ExcludeIPs := []string{
		"fd00::1",
		"fd00::a..fd00::14",
		"fd00::f..fd00::17",
		"fd00::4",
		"2001::1..2001::a",
	}

	dualCIDR := fmt.Sprintf("%s,%s", ipv4CIDR, ipv6CIDR)
	dualExcludeIPs := append(ipv4ExcludeIPs, ipv6ExcludeIPs...)

	Describe("[IPAM]", func() {
		Context("[IPv4]", func() {
			It("invalid subnet", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "1.1.1.1/64", nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
				err = im.AddOrUpdateSubnet(subnetName, "1.1.256.1/24", nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
			})

			It("normal subnet", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv4CIDR, ipv4ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetStaticAddress("pod1.ns", "pod1.ns", "10.16.0.2", "", subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())

				ip, _, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.0.2"))

				ip, _, _, err = im.GetRandomAddress("pod2.ns", "pod2.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.0.3"))

				_, _, _, err = im.GetStaticAddress("pod3.ns", "pod3.ns", "10.16.0.2", "", subnetName, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))

				im.ReleaseAddressByPod("pod1.ns")
				_, _, _, err = im.GetStaticAddress("pod3.ns", "pod3.ns", "10.16.0.2", "", subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetRandomAddress("pod4.ns", "pod4.ns", "invalid_subnet", nil)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))

				err = im.AddOrUpdateSubnet(subnetName, ipv4CIDR, nil)
				Expect(err).ShouldNot(HaveOccurred())

				ip, _, _, err = im.GetRandomAddress("pod5.ns", "pod5.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.0.1"))

				addresses := im.GetPodAddress("pod5.ns")
				Expect(addresses).To(HaveLen(1))
				Expect(addresses[0].Ip).To(Equal("10.16.0.1"))
			})

			It("change cidr", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv4CIDR, ipv4ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())

				err = im.AddOrUpdateSubnet(subnetName, "10.17.0.0/16", []string{"10.17.0.1"})
				Expect(err).ShouldNot(HaveOccurred())
				ip, _, _, err := im.GetRandomAddress("pod5.ns", "pod5.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.17.0.2"))
			})

			It("reuse released address when no unused address", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.0/30", nil)
				Expect(err).ShouldNot(HaveOccurred())

				ip, _, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.0.1"))

				im.ReleaseAddressByPod("pod1.ns")
				ip, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.0.2"))

				im.ReleaseAddressByPod("pod1.ns")
				ip, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.0.1"))
			})

			It("donot reuse released address after update subnet's excludedIps", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.0/30", nil)
				Expect(err).ShouldNot(HaveOccurred())

				ip, _, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.0.1"))

				im.ReleaseAddressByPod("pod1.ns")
				err = im.AddOrUpdateSubnet(subnetName, "10.16.0.0/30", []string{"10.16.0.1..10.16.0.2"})
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))
			})
		})

		Context("[IPv6]", func() {
			It("invalid subnet", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "fd00::/130", nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
				err = im.AddOrUpdateSubnet(subnetName, "fd00::g/120", nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
			})

			It("normal subnet", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv6CIDR, ipv6ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetStaticAddress("pod1.ns", "pod1.ns", "fd00::2", "", subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())

				_, ip, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fd00::2"))

				_, ip, _, err = im.GetRandomAddress("pod2.ns", "pod2.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fd00::3"))

				_, _, _, err = im.GetStaticAddress("pod3.ns", "pod3.ns", "fd00::2", "", subnetName, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))

				im.ReleaseAddressByPod("pod1.ns")
				_, _, _, err = im.GetStaticAddress("pod3.ns", "pod3.ns", "fd00::2", "", subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetRandomAddress("pod4.ns", "pod4.ns", "invalid_subnet", nil)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))

				err = im.AddOrUpdateSubnet(subnetName, ipv6CIDR, nil)
				Expect(err).ShouldNot(HaveOccurred())

				_, ip, _, err = im.GetRandomAddress("pod5.ns", "pod5.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fd00::1"))

				addresses := im.GetPodAddress("pod5.ns")
				Expect(addresses).To(HaveLen(1))
				Expect(addresses[0].Ip).To(Equal("fd00::1"))
			})

			It("change cidr", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv6CIDR, ipv6ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())

				err = im.AddOrUpdateSubnet(subnetName, "fe00::/112", []string{"fe00::1"})
				Expect(err).ShouldNot(HaveOccurred())
				_, ip, _, err := im.GetRandomAddress("pod5.ns", "pod5.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fe00::2"))
			})

			It("reuse released address when no unused address", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "fd00::/126", nil)
				Expect(err).ShouldNot(HaveOccurred())

				_, ip, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns")
				_, ip, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fd00::2"))

				im.ReleaseAddressByPod("pod1.ns")
				_, ip, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fd00::1"))
			})

			It("donot reuse released address after update subnet's excludedIps", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "fd00::/126", nil)
				Expect(err).ShouldNot(HaveOccurred())

				_, ip, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns")
				err = im.AddOrUpdateSubnet(subnetName, "fd00::/126", []string{"fd00::1..fd00::2"})
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))
			})
		})

		Context("[DualStack]", func() {
			It("invalid subnet", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("1.1.1.1/64,%s", ipv6CIDR), nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
				err = im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("1.1.256.1/24,%s", ipv6CIDR), nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
				err = im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("%s,fd00::/130", ipv4CIDR), nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
				err = im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("%s,fd00::g/120", ipv4CIDR), nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
			})

			It("normal subnet", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, dualCIDR, dualExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetStaticAddress("pod1.ns", "pod1.ns", "10.16.0.2,fd00::2", "", subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())

				ipv4, ipv6, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.16.0.2"))
				Expect(ipv6).To(Equal("fd00::2"))

				ipv4, ipv6, _, err = im.GetRandomAddress("pod2.ns", "pod2.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.16.0.3"))
				Expect(ipv6).To(Equal("fd00::3"))

				_, _, _, err = im.GetStaticAddress("pod3.ns", "pod3.ns", "10.16.0.2,fd00::2", "", subnetName, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))

				im.ReleaseAddressByPod("pod1.ns")
				_, _, _, err = im.GetStaticAddress("pod3.ns", "pod3.ns", "10.16.0.2,fd00::2", "", subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetRandomAddress("pod4.ns", "pod4.ns", "invalid_subnet", nil)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))

				err = im.AddOrUpdateSubnet(subnetName, dualCIDR, nil)
				Expect(err).ShouldNot(HaveOccurred())

				ipv4, ipv6, _, err = im.GetRandomAddress("pod5.ns", "pod5.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.16.0.1"))
				Expect(ipv6).To(Equal("fd00::1"))

				addresses := im.GetPodAddress("pod5.ns")
				Expect(addresses).To(HaveLen(2))
				Expect(addresses[0].Ip).To(Equal("10.16.0.1"))
				Expect(addresses[1].Ip).To(Equal("fd00::1"))
			})

			It("change cidr", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, dualCIDR, dualExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())

				err = im.AddOrUpdateSubnet(subnetName, "10.17.0.2/16,fe00::/112", []string{"10.17.0.1", "fe00::1"})
				Expect(err).ShouldNot(HaveOccurred())
				ipv4, ipv6, _, err := im.GetRandomAddress("pod5.ns", "pod5.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.17.0.2"))
				Expect(ipv6).To(Equal("fe00::2"))
			})

			It("reuse released address when no unused address", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.2/30,fd00::/126", nil)
				Expect(err).ShouldNot(HaveOccurred())

				ipv4, ipv6, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.16.0.1"))
				Expect(ipv6).To(Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns")
				ipv4, ipv6, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.16.0.2"))
				Expect(ipv6).To(Equal("fd00::2"))

				im.ReleaseAddressByPod("pod1.ns")
				ipv4, ipv6, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.16.0.1"))
				Expect(ipv6).To(Equal("fd00::1"))
			})

			It("donot reuse released address after update subnet's excludedIps", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.2/30,fd00::/126", nil)
				Expect(err).ShouldNot(HaveOccurred())

				ipv4, ipv6, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.16.0.1"))
				Expect(ipv6).To(Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns")
				err = im.AddOrUpdateSubnet(subnetName, "10.16.0.2/30,fd00::/126", []string{"10.16.0.1..10.16.0.2", "fd00::1..fd00::2"})
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", subnetName, nil)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))
			})
		})
	})

	Describe("[IP]", func() {
		It("IPv4 operation", func() {
			ip1 := ipam.IP("10.0.0.16")
			ip2 := ipam.IP("10.0.0.17")

			Expect(ip1.Equal(ip1)).To(BeTrue())
			Expect(ip1.GreaterThan(ip1)).To(BeFalse())
			Expect(ip1.LessThan(ip1)).To(BeFalse())

			Expect(ip1.Equal(ip2)).To(BeFalse())
			Expect(ip1.GreaterThan(ip1)).To(BeFalse())
			Expect(ip1.LessThan(ip2)).To(BeTrue())

			Expect(ip1.Add(1)).To(Equal(ip2))
			Expect(ip2.Add(-1)).To(Equal(ip1))
			Expect(ip1.Sub(-1)).To(Equal(ip2))
			Expect(ip2.Sub(1)).To(Equal(ip1))
		})

		It("IPv6 operation", func() {
			ip1 := ipam.IP("fd00::16")
			ip2 := ipam.IP("fd00::17")

			Expect(ip1.Equal(ip1)).To(BeTrue())
			Expect(ip1.GreaterThan(ip1)).To(BeFalse())
			Expect(ip1.LessThan(ip1)).To(BeFalse())

			Expect(ip1.Equal(ip2)).To(BeFalse())
			Expect(ip1.GreaterThan(ip1)).To(BeFalse())
			Expect(ip1.LessThan(ip2)).To(BeTrue())

			Expect(ip1.Add(1)).To(Equal(ip2))
			Expect(ip2.Add(-1)).To(Equal(ip1))
			Expect(ip1.Sub(-1)).To(Equal(ip2))
			Expect(ip2.Sub(1)).To(Equal(ip1))
		})
	})

	Describe("[Subnet]", func() {
		Context("[IPv4]", func() {
			It("init subnet", func() {
				subnet, err := ipam.NewSubnet(subnetName, ipv4CIDR, ipv4ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(subnet.Name).To(Equal(subnetName))
				Expect(subnet.V4ReservedIPList).To(HaveLen(len(ipv4ExcludeIPs) - 1))
				Expect(subnet.V4FreeIPList).To(HaveLen(3))
				Expect(subnet.V4FreeIPList).To(Equal(
					ipam.IPRangeList{
						&ipam.IPRange{Start: "10.16.0.2", End: "10.16.0.3"},
						&ipam.IPRange{Start: "10.16.0.5", End: "10.16.0.9"},
						&ipam.IPRange{Start: "10.16.0.24", End: "10.16.255.254"},
					}))
			})

			It("static allocation", func() {
				subnet, err := ipam.NewSubnet(subnetName, ipv4CIDR, ipv4ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())
				_, _, err = subnet.GetStaticAddress("pod1.ns", "pod1.ns", "10.16.0.2", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())
				_, _, err = subnet.GetStaticAddress("pod2.ns", "pod2.ns", "10.16.0.3", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())
				_, _, err = subnet.GetStaticAddress("pod3.ns", "pod3.ns", "10.16.0.20", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(subnet.V4FreeIPList).To(Equal(
					ipam.IPRangeList{
						&ipam.IPRange{Start: "10.16.0.5", End: "10.16.0.9"},
						&ipam.IPRange{Start: "10.16.0.24", End: "10.16.255.254"},
					}))

				Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.2"), "pod1.ns"))
				Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.3"), "pod2.ns"))
				Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.20"), "pod3.ns"))
				Expect(subnet.V4NicToIP).To(HaveKeyWithValue("pod1.ns", ipam.IP("10.16.0.2")))
				Expect(subnet.V4NicToIP).To(HaveKeyWithValue("pod2.ns", ipam.IP("10.16.0.3")))
				Expect(subnet.V4NicToIP).To(HaveKeyWithValue("pod3.ns", ipam.IP("10.16.0.20")))

				_, _, err = subnet.GetStaticAddress("pod4.ns", "pod4.ns", "10.16.0.3", "", false, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))
				_, _, err = subnet.GetStaticAddress("pod5.ns", "pod5.ns", "19.16.0.3", "", false, true)
				Expect(err).Should(MatchError(ipam.ErrOutOfRange))

				subnet.ReleaseAddress("pod1.ns")
				subnet.ReleaseAddress("pod2.ns")
				subnet.ReleaseAddress("pod3.ns")
				Expect(subnet.V4FreeIPList).To(Equal(
					ipam.IPRangeList{
						&ipam.IPRange{Start: "10.16.0.5", End: "10.16.0.9"},
						&ipam.IPRange{Start: "10.16.0.24", End: "10.16.255.254"},
					}))

				Expect(subnet.V4NicToIP).To(BeEmpty())
				Expect(subnet.V4IPToPod).To(BeEmpty())
			})

			It("random allocation", func() {
				subnet, err := ipam.NewSubnet(subnetName, "10.16.0.0/30", nil)
				Expect(err).ShouldNot(HaveOccurred())

				ip1, _, _, err := subnet.GetRandomAddress("pod1.ns", "pod1.ns", nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip1).To(Equal(ipam.IP("10.16.0.1")))
				ip1, _, _, err = subnet.GetRandomAddress("pod1.ns", "pod1.ns", nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip1).To(Equal(ipam.IP("10.16.0.1")))

				ip2, _, _, err := subnet.GetRandomAddress("pod2.ns", "pod2.ns", nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip2).To(Equal(ipam.IP("10.16.0.2")))

				_, _, _, err = subnet.GetRandomAddress("pod3.ns", "pod3.ns", nil)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))
				Expect(subnet.V4FreeIPList).To(BeEmpty())

				Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.1"), "pod1.ns"))
				Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.2"), "pod2.ns"))
				Expect(subnet.V4NicToIP).To(HaveKeyWithValue("pod1.ns", ipam.IP("10.16.0.1")))
				Expect(subnet.V4NicToIP).To(HaveKeyWithValue("pod2.ns", ipam.IP("10.16.0.2")))

				subnet.ReleaseAddress("pod1.ns")
				subnet.ReleaseAddress("pod2.ns")
				Expect(subnet.V4FreeIPList).To(Equal(ipam.IPRangeList{}))
				Expect(subnet.V4ReleasedIPList).To(Equal(ipam.IPRangeList{&ipam.IPRange{Start: "10.16.0.1", End: "10.16.0.2"}}))
				Expect(subnet.V4IPToPod).To(BeEmpty())
				Expect(subnet.V4NicToIP).To(BeEmpty())
			})
		})

		Context("[IPv6]", func() {
			It("init subnet", func() {
				subnet, err := ipam.NewSubnet(subnetName, ipv6CIDR, ipv6ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(subnet.Name).To(Equal(subnetName))
				Expect(subnet.V6ReservedIPList).To(HaveLen(len(ipv6ExcludeIPs) - 1))
				Expect(subnet.V6FreeIPList).To(HaveLen(3))
				Expect(subnet.V6FreeIPList).To(Equal(
					ipam.IPRangeList{
						&ipam.IPRange{Start: "fd00::2", End: "fd00::3"},
						&ipam.IPRange{Start: "fd00::5", End: "fd00::9"},
						&ipam.IPRange{Start: "fd00::18", End: "fd00::fffe"},
					}))
			})

			It("static allocation", func() {
				subnet, err := ipam.NewSubnet(subnetName, ipv6CIDR, ipv6ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())
				_, _, err = subnet.GetStaticAddress("pod1.ns", "pod1.ns", "fd00::2", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())
				_, _, err = subnet.GetStaticAddress("pod2.ns", "pod2.ns", "fd00::3", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())
				_, _, err = subnet.GetStaticAddress("pod3.ns", "pod3.ns", "fd00::14", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(subnet.V6FreeIPList).To(Equal(
					ipam.IPRangeList{
						&ipam.IPRange{Start: "fd00::5", End: "fd00::9"},
						&ipam.IPRange{Start: "fd00::18", End: "fd00::fffe"},
					}))

				Expect(subnet.V6IPToPod).To(HaveKeyWithValue(ipam.IP("fd00::2"), "pod1.ns"))
				Expect(subnet.V6IPToPod).To(HaveKeyWithValue(ipam.IP("fd00::3"), "pod2.ns"))
				Expect(subnet.V6IPToPod).To(HaveKeyWithValue(ipam.IP("fd00::14"), "pod3.ns"))
				Expect(subnet.V6NicToIP).To(HaveKeyWithValue("pod1.ns", ipam.IP("fd00::2")))
				Expect(subnet.V6NicToIP).To(HaveKeyWithValue("pod2.ns", ipam.IP("fd00::3")))
				Expect(subnet.V6NicToIP).To(HaveKeyWithValue("pod3.ns", ipam.IP("fd00::14")))

				_, _, err = subnet.GetStaticAddress("pod4.ns", "pod4.ns", "fd00::3", "", false, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))
				_, _, err = subnet.GetStaticAddress("pod5.ns", "pod5.ns", "fe00::3", "", false, true)
				Expect(err).Should(MatchError(ipam.ErrOutOfRange))

				subnet.ReleaseAddress("pod1.ns")
				subnet.ReleaseAddress("pod2.ns")
				subnet.ReleaseAddress("pod3.ns")
				Expect(subnet.V6FreeIPList).To(Equal(
					ipam.IPRangeList{
						&ipam.IPRange{Start: "fd00::5", End: "fd00::9"},
						&ipam.IPRange{Start: "fd00::18", End: "fd00::fffe"},
					}))

				Expect(subnet.V6NicToIP).To(BeEmpty())
				Expect(subnet.V6IPToPod).To(BeEmpty())
			})

			It("random allocation", func() {
				subnet, err := ipam.NewSubnet(subnetName, "fd00::/126", nil)
				Expect(err).ShouldNot(HaveOccurred())

				_, ip1, _, err := subnet.GetRandomAddress("pod1.ns", "pod1.ns", nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip1).To(Equal(ipam.IP("fd00::1")))
				_, ip1, _, err = subnet.GetRandomAddress("pod1.ns", "pod1.ns", nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip1).To(Equal(ipam.IP("fd00::1")))

				_, ip2, _, err := subnet.GetRandomAddress("pod2.ns", "pod2.ns", nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip2).To(Equal(ipam.IP("fd00::2")))

				_, _, _, err = subnet.GetRandomAddress("pod3.ns", "pod3.ns", nil)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))
				Expect(subnet.V6FreeIPList).To(BeEmpty())

				Expect(subnet.V6IPToPod).To(HaveKeyWithValue(ipam.IP("fd00::1"), "pod1.ns"))
				Expect(subnet.V6IPToPod).To(HaveKeyWithValue(ipam.IP("fd00::2"), "pod2.ns"))
				Expect(subnet.V6NicToIP).To(HaveKeyWithValue("pod1.ns", ipam.IP("fd00::1")))
				Expect(subnet.V6NicToIP).To(HaveKeyWithValue("pod2.ns", ipam.IP("fd00::2")))

				subnet.ReleaseAddress("pod1.ns")
				subnet.ReleaseAddress("pod2.ns")
				Expect(subnet.V6FreeIPList).To(Equal(ipam.IPRangeList{}))
				Expect(subnet.V6ReleasedIPList).To(Equal(ipam.IPRangeList{&ipam.IPRange{Start: "fd00::1", End: "fd00::2"}}))
				Expect(subnet.V6IPToPod).To(BeEmpty())
				Expect(subnet.V6NicToIP).To(BeEmpty())
			})
		})

		Context("[DualStack]", func() {
			It("init subnet", func() {
				subnet, err := ipam.NewSubnet(subnetName, dualCIDR, dualExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(subnet.Name).To(Equal(subnetName))
				Expect(subnet.V4ReservedIPList).To(HaveLen(len(ipv4ExcludeIPs) - 1))
				Expect(subnet.V4FreeIPList).To(HaveLen(3))
				Expect(subnet.V4FreeIPList).To(Equal(
					ipam.IPRangeList{
						&ipam.IPRange{Start: "10.16.0.2", End: "10.16.0.3"},
						&ipam.IPRange{Start: "10.16.0.5", End: "10.16.0.9"},
						&ipam.IPRange{Start: "10.16.0.24", End: "10.16.255.254"},
					}))
				Expect(subnet.V6ReservedIPList).To(HaveLen(len(ipv6ExcludeIPs) - 1))
				Expect(subnet.V6FreeIPList).To(HaveLen(3))
				Expect(subnet.V6FreeIPList).To(Equal(
					ipam.IPRangeList{
						&ipam.IPRange{Start: "fd00::2", End: "fd00::3"},
						&ipam.IPRange{Start: "fd00::5", End: "fd00::9"},
						&ipam.IPRange{Start: "fd00::18", End: "fd00::fffe"},
					}))
			})

			It("static allocation", func() {
				subnet, err := ipam.NewSubnet(subnetName, dualCIDR, dualExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())
				_, _, err = subnet.GetStaticAddress("pod1.ns", "pod1.ns", "10.16.0.2", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())
				_, _, err = subnet.GetStaticAddress("pod1.ns", "pod1.ns", "fd00::2", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())
				_, _, err = subnet.GetStaticAddress("pod2.ns", "pod2.ns", "10.16.0.3", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())
				_, _, err = subnet.GetStaticAddress("pod2.ns", "pod2.ns", "fd00::3", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())
				_, _, err = subnet.GetStaticAddress("pod3.ns", "pod3.ns", "10.16.0.20", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())
				_, _, err = subnet.GetStaticAddress("pod3.ns", "pod3.ns", "fd00::14", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())

				Expect(subnet.V4FreeIPList).To(Equal(
					ipam.IPRangeList{
						&ipam.IPRange{Start: "10.16.0.5", End: "10.16.0.9"},
						&ipam.IPRange{Start: "10.16.0.24", End: "10.16.255.254"},
					}))
				Expect(subnet.V6FreeIPList).To(Equal(
					ipam.IPRangeList{
						&ipam.IPRange{Start: "fd00::5", End: "fd00::9"},
						&ipam.IPRange{Start: "fd00::18", End: "fd00::fffe"},
					}))

				Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.2"), "pod1.ns"))
				Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.3"), "pod2.ns"))
				Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.20"), "pod3.ns"))
				Expect(subnet.V4NicToIP).To(HaveKeyWithValue("pod1.ns", ipam.IP("10.16.0.2")))
				Expect(subnet.V4NicToIP).To(HaveKeyWithValue("pod2.ns", ipam.IP("10.16.0.3")))
				Expect(subnet.V4NicToIP).To(HaveKeyWithValue("pod3.ns", ipam.IP("10.16.0.20")))
				Expect(subnet.V6IPToPod).To(HaveKeyWithValue(ipam.IP("fd00::2"), "pod1.ns"))
				Expect(subnet.V6IPToPod).To(HaveKeyWithValue(ipam.IP("fd00::3"), "pod2.ns"))
				Expect(subnet.V6IPToPod).To(HaveKeyWithValue(ipam.IP("fd00::14"), "pod3.ns"))
				Expect(subnet.V6NicToIP).To(HaveKeyWithValue("pod1.ns", ipam.IP("fd00::2")))
				Expect(subnet.V6NicToIP).To(HaveKeyWithValue("pod2.ns", ipam.IP("fd00::3")))
				Expect(subnet.V6NicToIP).To(HaveKeyWithValue("pod3.ns", ipam.IP("fd00::14")))

				_, _, err = subnet.GetStaticAddress("pod4.ns", "pod4.ns", "10.16.0.3", "", false, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))
				_, _, err = subnet.GetStaticAddress("pod4.ns", "pod4.ns", "fd00::3", "", false, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))
				_, _, err = subnet.GetStaticAddress("pod5.ns", "pod5.ns", "19.16.0.3", "", false, true)
				Expect(err).Should(MatchError(ipam.ErrOutOfRange))
				_, _, err = subnet.GetStaticAddress("pod1.ns", "pod5.ns", "fe00::3", "", false, true)
				Expect(err).Should(MatchError(ipam.ErrOutOfRange))

				subnet.ReleaseAddress("pod1.ns")
				subnet.ReleaseAddress("pod2.ns")
				subnet.ReleaseAddress("pod3.ns")
				Expect(subnet.V4FreeIPList).To(Equal(
					ipam.IPRangeList{
						&ipam.IPRange{Start: "10.16.0.5", End: "10.16.0.9"},
						&ipam.IPRange{Start: "10.16.0.24", End: "10.16.255.254"},
					}))
				Expect(subnet.V6FreeIPList).To(Equal(
					ipam.IPRangeList{
						&ipam.IPRange{Start: "fd00::5", End: "fd00::9"},
						&ipam.IPRange{Start: "fd00::18", End: "fd00::fffe"},
					}))

				Expect(subnet.V4NicToIP).To(BeEmpty())
				Expect(subnet.V4IPToPod).To(BeEmpty())
				Expect(subnet.V6NicToIP).To(BeEmpty())
				Expect(subnet.V6IPToPod).To(BeEmpty())
			})

			It("random allocation", func() {
				subnet, err := ipam.NewSubnet(subnetName, "10.16.0.0/30,fd00::/126", nil)
				Expect(err).ShouldNot(HaveOccurred())

				ipv4, ipv6, _, err := subnet.GetRandomAddress("pod1.ns", "pod1.ns", nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal(ipam.IP("10.16.0.1")))
				Expect(ipv6).To(Equal(ipam.IP("fd00::1")))
				ipv4, ipv6, _, err = subnet.GetRandomAddress("pod1.ns", "pod1.ns", nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal(ipam.IP("10.16.0.1")))
				Expect(ipv6).To(Equal(ipam.IP("fd00::1")))

				ipv4, ipv6, _, err = subnet.GetRandomAddress("pod2.ns", "pod2.ns", nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal(ipam.IP("10.16.0.2")))
				Expect(ipv6).To(Equal(ipam.IP("fd00::2")))

				_, _, _, err = subnet.GetRandomAddress("pod3.ns", "pod3.ns", nil)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))
				Expect(subnet.V4FreeIPList).To(BeEmpty())
				Expect(subnet.V6FreeIPList).To(BeEmpty())

				Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.1"), "pod1.ns"))
				Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.2"), "pod2.ns"))
				Expect(subnet.V4NicToIP).To(HaveKeyWithValue("pod1.ns", ipam.IP("10.16.0.1")))
				Expect(subnet.V4NicToIP).To(HaveKeyWithValue("pod2.ns", ipam.IP("10.16.0.2")))
				Expect(subnet.V6IPToPod).To(HaveKeyWithValue(ipam.IP("fd00::1"), "pod1.ns"))
				Expect(subnet.V6IPToPod).To(HaveKeyWithValue(ipam.IP("fd00::2"), "pod2.ns"))
				Expect(subnet.V6NicToIP).To(HaveKeyWithValue("pod1.ns", ipam.IP("fd00::1")))
				Expect(subnet.V6NicToIP).To(HaveKeyWithValue("pod2.ns", ipam.IP("fd00::2")))

				subnet.ReleaseAddress("pod1.ns")
				subnet.ReleaseAddress("pod2.ns")
				Expect(subnet.V4FreeIPList).To(Equal(ipam.IPRangeList{}))
				Expect(subnet.V4ReleasedIPList).To(Equal(ipam.IPRangeList{&ipam.IPRange{Start: "10.16.0.1", End: "10.16.0.2"}}))
				Expect(subnet.V6FreeIPList).To(Equal(ipam.IPRangeList{}))
				Expect(subnet.V6ReleasedIPList).To(Equal(ipam.IPRangeList{&ipam.IPRange{Start: "fd00::1", End: "fd00::2"}}))
				Expect(subnet.V4IPToPod).To(BeEmpty())
				Expect(subnet.V4NicToIP).To(BeEmpty())
				Expect(subnet.V6IPToPod).To(BeEmpty())
				Expect(subnet.V6NicToIP).To(BeEmpty())
			})
		})
	})
})
