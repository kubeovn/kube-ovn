package ipam

import (
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("[IPAM]", func() {
	subnetName := "test"
	cidrStr := "10.16.0.0/16"
	excludeIps := []string{
		"10.16.0.1",
		"10.16.0.10..10.16.0.20",
		"10.16.0.15..10.16.0.23",
		"10.16.0.4",
		"192.168.0.1..192.168.0.10",
	}

	Describe("[IPAM]", func() {
		It("invalid subnet", func() {
			im := ipam.NewIPAM()
			err := im.AddOrUpdateSubnet("test", "1.1.1.1/64", []string{})
			Expect(err).Should(MatchError(ipam.InvalidCIDRError))
		})

		It("normal subnet", func() {
			im := ipam.NewIPAM()
			err := im.AddOrUpdateSubnet(subnetName, cidrStr, excludeIps)
			Expect(err).ShouldNot(HaveOccurred())

			_, _, _, err = im.GetStaticAddress("pod1.ns", "10.16.0.2", "", subnetName)
			Expect(err).ShouldNot(HaveOccurred())

			ip, _, _, err := im.GetRandomAddress("pod1.ns", subnetName)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ip).To(Equal("10.16.0.2"))

			ip, _, _, err = im.GetRandomAddress("pod2.ns", subnetName)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ip).To(Equal("10.16.0.3"))

			_, _, _, err = im.GetStaticAddress("pod3.ns", "10.16.0.2", "", subnetName)
			Expect(err).Should(MatchError(ipam.ConflictError))

			im.ReleaseAddressByPod("pod1.ns")
			_, _, _, err = im.GetStaticAddress("pod3.ns", "10.16.0.2", "", subnetName)
			Expect(err).ShouldNot(HaveOccurred())

			_, _, _, err = im.GetRandomAddress("pod4.ns", "invalid_subnet")
			Expect(err).Should(MatchError(ipam.NoAvailableError))

			err = im.AddOrUpdateSubnet(subnetName, cidrStr, []string{})
			Expect(err).ShouldNot(HaveOccurred())

			ip, _, _, err = im.GetRandomAddress("pod5.ns", subnetName)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ip).To(Equal("10.16.0.1"))

			addresses := im.GetPodAddress("pod5.ns")
			Expect(addresses).To(HaveLen(1))
			Expect(addresses[0].Ip).To(Equal("10.16.0.1"))
		})

		It("change cidr", func() {
			im := ipam.NewIPAM()
			err := im.AddOrUpdateSubnet(subnetName, cidrStr, excludeIps)
			Expect(err).ShouldNot(HaveOccurred())

			err = im.AddOrUpdateSubnet(subnetName, "10.17.0.0/16", []string{"10.17.0.1"})
			Expect(err).ShouldNot(HaveOccurred())
			ip, _, _, err := im.GetRandomAddress("pod5.ns", subnetName)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ip).To(Equal("10.17.0.2"))

		})

		It("reuse released address when no unused address", func() {
			im := ipam.NewIPAM()
			err := im.AddOrUpdateSubnet("test", "10.16.0.0/30", []string{})
			Expect(err).ShouldNot(HaveOccurred())

			ip, _, _, err := im.GetRandomAddress("pod1.ns", "test")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ip).To(Equal("10.16.0.1"))

			im.ReleaseAddressByPod("pod1.ns")
			ip, _, _, err = im.GetRandomAddress("pod1.ns", "test")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ip).To(Equal("10.16.0.2"))

			im.ReleaseAddressByPod("pod1.ns")
			ip, _, _, err = im.GetRandomAddress("pod1.ns", "test")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ip).To(Equal("10.16.0.1"))

		})
	})

	Describe("[IP]", func() {
		It("IP operation", func() {
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
		})
	})

	Describe("[Subnet]", func() {
		It("init subnet", func() {
			subnet, err := ipam.NewSubnet(subnetName, cidrStr, excludeIps)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(subnet.Name).To(Equal(subnetName))
			Expect(subnet.V4ReservedIPList).To(HaveLen(len(excludeIps)))
			Expect(subnet.V4FreeIPList).To(HaveLen(3))
			Expect(subnet.V4FreeIPList).To(Equal(
				ipam.IPRangeList{
					&ipam.IPRange{Start: "10.16.0.2", End: "10.16.0.3"},
					&ipam.IPRange{Start: "10.16.0.5", End: "10.16.0.9"},
					&ipam.IPRange{Start: "10.16.0.24", End: "10.16.255.254"},
				}))
		})

		It("static allocation", func() {
			subnet, err := ipam.NewSubnet(subnetName, cidrStr, excludeIps)
			Expect(err).ShouldNot(HaveOccurred())
			_, _, err = subnet.GetStaticAddress("pod1.ns", "10.16.0.2", "", false)
			Expect(err).ShouldNot(HaveOccurred())
			_, _, err = subnet.GetStaticAddress("pod2.ns", "10.16.0.3", "", false)
			Expect(err).ShouldNot(HaveOccurred())
			_, _, err = subnet.GetStaticAddress("pod3.ns", "10.16.0.20", "", false)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(subnet.V4FreeIPList).To(Equal(
				ipam.IPRangeList{
					&ipam.IPRange{Start: "10.16.0.5", End: "10.16.0.9"},
					&ipam.IPRange{Start: "10.16.0.24", End: "10.16.255.254"},
				}))

			Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.2"), "pod1.ns"))
			Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.3"), "pod2.ns"))
			Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.20"), "pod3.ns"))
			Expect(subnet.V4PodToIP).To(HaveKeyWithValue("pod1.ns", ipam.IP("10.16.0.2")))
			Expect(subnet.V4PodToIP).To(HaveKeyWithValue("pod2.ns", ipam.IP("10.16.0.3")))
			Expect(subnet.V4PodToIP).To(HaveKeyWithValue("pod3.ns", ipam.IP("10.16.0.20")))

			_, _, err = subnet.GetStaticAddress("pod4.ns", "10.16.0.3", "", false)
			Expect(err).Should(MatchError(ipam.ConflictError))
			_, _, err = subnet.GetStaticAddress("pod5.ns", "19.16.0.3", "", false)
			Expect(err).Should(MatchError(ipam.OutOfRangeError))

			subnet.ReleaseAddress("pod1.ns")
			subnet.ReleaseAddress("pod2.ns")
			subnet.ReleaseAddress("pod3.ns")
			Expect(subnet.V4FreeIPList).To(Equal(
				ipam.IPRangeList{
					&ipam.IPRange{Start: "10.16.0.5", End: "10.16.0.9"},
					&ipam.IPRange{Start: "10.16.0.24", End: "10.16.255.254"},
				}))

			Expect(subnet.V4PodToIP).To(BeEmpty())
			Expect(subnet.V4IPToPod).To(BeEmpty())
		})

		It("random allocation", func() {
			subnet, err := ipam.NewSubnet("test", "10.16.0.0/30", []string{})
			Expect(err).ShouldNot(HaveOccurred())

			ip1, _, _, err := subnet.GetRandomAddress("pod1.ns")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ip1).To(Equal(ipam.IP("10.16.0.1")))
			ip1, _, _, err = subnet.GetRandomAddress("pod1.ns")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ip1).To(Equal(ipam.IP("10.16.0.1")))

			ip2, _, _, err := subnet.GetRandomAddress("pod2.ns")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ip2).To(Equal(ipam.IP("10.16.0.2")))

			_, _, _, err = subnet.GetRandomAddress("pod3.ns")
			Expect(err).Should(MatchError(ipam.NoAvailableError))
			Expect(subnet.V4FreeIPList).To(BeEmpty())

			Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.1"), "pod1.ns"))
			Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.2"), "pod2.ns"))
			Expect(subnet.V4PodToIP).To(HaveKeyWithValue("pod1.ns", ipam.IP("10.16.0.1")))
			Expect(subnet.V4PodToIP).To(HaveKeyWithValue("pod2.ns", ipam.IP("10.16.0.2")))

			subnet.ReleaseAddress("pod1.ns")
			subnet.ReleaseAddress("pod2.ns")
			Expect(subnet.V4FreeIPList).To(Equal(
				ipam.IPRangeList{}))
			Expect(subnet.V4ReleasedIPList).To(Equal(
				ipam.IPRangeList{
					&ipam.IPRange{Start: "10.16.0.1", End: "10.16.0.2"},
				}))
			Expect(subnet.V4IPToPod).To(BeEmpty())
			Expect(subnet.V4PodToIP).To(BeEmpty())
		})
	})
})
