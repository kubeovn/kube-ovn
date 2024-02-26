package ipam

import (
	"fmt"
	"math/rand/v2"
	"strings"

	. "github.com/onsi/ginkgo/v2"
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
	v4Gw := "10.16.0.1"
	v6Gw := "fd00::1"
	dualGw := "10.16.0.1,fd00::1"

	dualCIDR := fmt.Sprintf("%s,%s", ipv4CIDR, ipv6CIDR)
	dualExcludeIPs := append(ipv4ExcludeIPs, ipv6ExcludeIPs...)

	// TODO test case use random ip and ipcidr, and input test data should separate from test case

	Context("[Subnet]", func() {
		Context("[IPv4]", func() {
			It("invalid subnet", func() {
				im := ipam.NewIPAM()

				By("invalid mask len > 32")
				maskV4Length := rand.Int() + 32
				err := im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("1.1.1.1/%d", maskV4Length), v4Gw, nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))

				By("invalid ip range")
				invalidV4Ip := fmt.Sprintf("1.1.%d.1/24", rand.Int()+256)
				err = im.AddOrUpdateSubnet(subnetName, invalidV4Ip, v4Gw, nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
			})

			It("normal subnet", func() {
				By("create pod with static ip")
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(im.GetSubnetV4Mask(subnetName)).To(Equal(strings.Split(ipv4CIDR, "/")[1]))
				Expect(im.Subnets[subnetName].V4Gw).To(Equal(v4Gw))

				pod1 := "pod1.ns"
				pod1Nic1 := "pod1nic1.ns"
				freeIP1 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				ip, _, _, err := im.GetStaticAddress(pod1, pod1Nic1, freeIP1, nil, subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIP1))

				ip, _, _, err = im.GetRandomAddress(pod1, pod1Nic1, nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIP1))

				By("create multiple ips on one pod")
				pod2 := "pod2.ns"
				pod2Nic1 := "pod2Nic1.ns"
				pod2Nic2 := "pod2Nic2.ns"

				freeIP2 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				ip, _, _, err = im.GetRandomAddress(pod2, pod2Nic1, nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIP2))

				freeIP3 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				ip, _, _, err = im.GetRandomAddress(pod2, pod2Nic2, nil, subnetName, "", nil, true)

				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIP3))

				addresses := im.GetPodAddress(pod2)
				Expect(addresses).To(HaveLen(2))
				Expect([]string{addresses[0].IP, addresses[1].IP}).To(Equal([]string{freeIP2, freeIP3}))
				Expect(im.ContainAddress(freeIP2)).Should(BeTrue())
				Expect(im.ContainAddress(freeIP3)).Should(BeTrue())

				_, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP2, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIP3, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				assignedPod, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP1, subnetName, pod2)
				Expect(isIPAssigned).Should(BeTrue())
				Expect(assignedPod).To(Equal(pod1))

				By("get static ip conflict with ip in use")
				pod3 := "pod3.ns"
				pod3Nic1 := "pod3Nic1.ns"
				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIP3, nil, subnetName, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))

				By("release pod with multiple nics")
				im.ReleaseAddressByPod(pod2, "")
				ip2, err := ipam.NewIP(freeIP2)
				Expect(err).ShouldNot(HaveOccurred())
				ip3, err := ipam.NewIP(freeIP3)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip2)).Should(BeTrue())
				Expect(im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip3)).Should(BeTrue())

				By("release pod with single nic")
				im.ReleaseAddressByPod(pod1, "")
				ip1, err := ipam.NewIP(freeIP1)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip1)).To(BeTrue())

				By("create new pod with released ips")
				pod4 := "pod4.ns"
				pod4Nic1 := "pod4Nic1.ns"

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIP1, nil, subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())

				By("create pod with no initialized subnet")
				pod5 := "pod5.ns"
				pod5Nic1 := "pod5Nic1.ns"

				_, _, _, err = im.GetRandomAddress(pod5, pod5Nic1, nil, "invalid_subnet", "", nil, true)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))
			})

			It("change cidr", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())

				err = im.AddOrUpdateSubnet(subnetName, "10.17.0.0/16", v4Gw, []string{"10.17.0.1"})
				Expect(err).ShouldNot(HaveOccurred())
				ip, _, _, err := im.GetRandomAddress("pod5.ns", "pod5.ns", nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.17.0.2"))

				By("update invalid cidr, subnet should not change")
				err = im.AddOrUpdateSubnet(subnetName, "1.1.256.1", v4Gw, nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
				Expect(im.Subnets[subnetName].V4CIDR.IP.String()).To(Equal("10.17.0.0"))
			})

			It("reuse released address when no unused address", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.0/30", v4Gw, nil)
				Expect(err).ShouldNot(HaveOccurred())

				ip, _, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.0.1"))

				im.ReleaseAddressByPod("pod1.ns", "")
				ip, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.0.2"))

				im.ReleaseAddressByPod("pod1.ns", "")
				ip, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.0.1"))
			})

			It("do not reuse released address after update subnet's excludedIps", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.0/30", v4Gw, nil)
				Expect(err).ShouldNot(HaveOccurred())

				ip, _, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.0.1"))

				im.ReleaseAddressByPod("pod1.ns", "")
				err = im.AddOrUpdateSubnet(subnetName, "10.16.0.0/30", v4Gw, []string{"10.16.0.1..10.16.0.2"})
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))
			})

			It("do not count excludedIps as subnet's v4availableIPs and v4usingIPs", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.10.0/28", "10.16.10.1", []string{"10.16.10.1", "10.16.10.10"})
				Expect(err).ShouldNot(HaveOccurred())

				ip, _, _, err := im.GetStaticAddress("pod1.ns", "pod1.ns", "10.16.10.10", nil, subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.10.10"))

				v4UsingIPStr, _, v4AvailableIPStr, _ := im.GetSubnetIPRangeString(subnetName, []string{"10.16.10.10"})
				Expect(v4UsingIPStr).To(Equal(""))
				Expect(v4AvailableIPStr).To(Equal("10.16.10.2-10.16.10.9,10.16.10.11-10.16.10.14"))

				err = im.AddOrUpdateSubnet(subnetName, "10.16.10.0/28", "10.16.10.1", []string{"10.16.10.1"})
				Expect(err).ShouldNot(HaveOccurred())
				v4UsingIPStr, _, v4AvailableIPStr, _ = im.GetSubnetIPRangeString(subnetName, nil)
				Expect(v4UsingIPStr).To(Equal("10.16.10.10"))
				Expect(v4AvailableIPStr).To(Equal("10.16.10.2-10.16.10.9,10.16.10.11-10.16.10.14"))
			})
		})

		Context("[IPv6]", func() {
			It("invalid subnet", func() {
				im := ipam.NewIPAM()

				maskV6Length := rand.Int() + 128
				By("invalid mask len > 128")
				err := im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("fd00::/%d", maskV6Length), v6Gw, nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))

				By("invalid ip range")
				err = im.AddOrUpdateSubnet(subnetName, "fd00::g/120", v6Gw, nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
			})

			It("normal subnet", func() {
				By("create pod with static ip")
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(im.Subnets[subnetName].V6Gw).To(Equal(v6Gw))

				pod1 := "pod1.ns"
				pod1Nic1 := "pod1nic1.ns"
				freeIP1 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				_, ip, _, err := im.GetStaticAddress(pod1, pod1Nic1, freeIP1, nil, subnetName, true)

				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIP1))

				_, ip, _, err = im.GetRandomAddress(pod1, pod1Nic1, nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIP1))

				By("create multiple ips on one pod")
				pod2 := "pod2.ns"
				pod2Nic1 := "pod2Nic1.ns"
				pod2Nic2 := "pod2Nic2.ns"

				freeIP2 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				_, ip, _, err = im.GetRandomAddress(pod2, pod2Nic1, nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIP2))

				freeIP3 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				_, ip, _, err = im.GetRandomAddress(pod2, pod2Nic2, nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIP3))

				addresses := im.GetPodAddress(pod2)
				Expect(addresses).To(HaveLen(2))
				Expect([]string{addresses[0].IP, addresses[1].IP}).To(Equal([]string{freeIP2, freeIP3}))
				Expect(im.ContainAddress(freeIP2)).Should(BeTrue())
				Expect(im.ContainAddress(freeIP3)).Should(BeTrue())

				_, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP2, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIP3, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				assignedPod, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP1, subnetName, pod2)
				Expect(isIPAssigned).Should(BeTrue())
				Expect(assignedPod).To(Equal(pod1))

				By("get static ip conflict with ip in use")
				pod3 := "pod3.ns"
				pod3Nic1 := "pod3Nic1.ns"
				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIP3, nil, subnetName, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))

				By("release pod with multiple nics")
				im.ReleaseAddressByPod(pod2, "")
				ip2, err := ipam.NewIP(freeIP2)
				Expect(err).ShouldNot(HaveOccurred())
				ip3, err := ipam.NewIP(freeIP3)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip2)).Should(BeTrue())
				Expect(im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip3)).Should(BeTrue())

				By("release pod with single nic")
				im.ReleaseAddressByPod(pod1, "")
				ip1, err := ipam.NewIP(freeIP1)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip1)).Should(BeTrue())

				By("create new pod with released ips")
				pod4 := "pod4.ns"
				pod4Nic1 := "pod4Nic1.ns"

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIP1, nil, subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())

				By("create pod with no initialized subnet")
				pod5 := "pod5.ns"
				pod5Nic1 := "pod5Nic1.ns"

				_, _, _, err = im.GetRandomAddress(pod5, pod5Nic1, nil, "invalid_subnet", "", nil, true)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))
			})

			It("change cidr", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())

				err = im.AddOrUpdateSubnet(subnetName, "fe00::/112", v6Gw, []string{"fe00::1"})
				Expect(err).ShouldNot(HaveOccurred())
				_, ip, _, err := im.GetRandomAddress("pod5.ns", "pod5.ns", nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fe00::2"))

				By("update invalid cidr, subnet should not change")
				err = im.AddOrUpdateSubnet(subnetName, "fd00::g/120", v6Gw, nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
				Expect(im.Subnets[subnetName].V6CIDR.IP.String()).To(Equal("fe00::"))
			})

			It("reuse released address when no unused address", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "fd00::/126", v6Gw, nil)
				Expect(err).ShouldNot(HaveOccurred())

				_, ip, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns", "")
				_, ip, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fd00::2"))

				im.ReleaseAddressByPod("pod1.ns", "")
				_, ip, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fd00::1"))
			})

			It("do not reuse released address after update subnet's excludedIps", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "fd00::/126", v6Gw, nil)
				Expect(err).ShouldNot(HaveOccurred())

				_, ip, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns", "")
				err = im.AddOrUpdateSubnet(subnetName, "fd00::/126", v6Gw, []string{"fd00::1..fd00::2"})
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))
			})
		})

		Context("[DualStack]", func() {
			It("invalid subnet", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("1.1.1.1/64,%s", ipv6CIDR), dualGw, nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
				err = im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("1.1.256.1/24,%s", ipv6CIDR), dualGw, nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
				err = im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("%s,fd00::/130", ipv4CIDR), dualGw, nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
				err = im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("%s,fd00::g/120", ipv4CIDR), dualGw, nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
			})

			It("normal subnet", func() {
				By("create pod with static ip")
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, dualCIDR, dualGw, dualExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(im.Subnets[subnetName].V6Gw).To(Equal(v6Gw))
				Expect(im.Subnets[subnetName].V4Gw).To(Equal(v4Gw))

				pod1 := "pod1.ns"
				pod1Nic1 := "pod1nic1.ns"
				freeIP41 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				freeIP61 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				dualIP := fmt.Sprintf("%s,%s", freeIP41, freeIP61)
				ip4, ip6, _, err := im.GetStaticAddress(pod1, pod1Nic1, dualIP, nil, subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip4).To(Equal(freeIP41))
				Expect(ip6).To(Equal(freeIP61))

				ip4, ip6, _, err = im.GetRandomAddress(pod1, pod1Nic1, nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip4).To(Equal(freeIP41))
				Expect(ip6).To(Equal(freeIP61))

				By("create multiple ips on one pod")
				pod2 := "pod2.ns"
				pod2Nic1 := "pod2Nic1.ns"
				pod2Nic2 := "pod2Nic2.ns"

				freeIP42 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				freeIP62 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				ip4, ip6, _, err = im.GetRandomAddress(pod2, pod2Nic1, nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip4).To(Equal(freeIP42))
				Expect(ip6).To(Equal(freeIP62))

				freeIP43 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				freeIP63 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				ip4, ip6, _, err = im.GetRandomAddress(pod2, pod2Nic2, nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip4).To(Equal(freeIP43))
				Expect(ip6).To(Equal(freeIP63))

				addresses := im.GetPodAddress(pod2)
				Expect(addresses).To(HaveLen(4))
				Expect([]string{addresses[0].IP, addresses[1].IP, addresses[2].IP, addresses[3].IP}).
					To(Equal([]string{freeIP42, freeIP62, freeIP43, freeIP63}))
				Expect(im.ContainAddress(freeIP42)).Should(BeTrue())
				Expect(im.ContainAddress(freeIP43)).Should(BeTrue())
				Expect(im.ContainAddress(freeIP62)).Should(BeTrue())
				Expect(im.ContainAddress(freeIP63)).Should(BeTrue())

				_, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP42, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIP62, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIP43, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIP63, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				assignedPod, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP41, subnetName, pod2)
				Expect(isIPAssigned).Should(BeTrue())
				Expect(assignedPod).To(Equal(pod1))

				By("get static ip conflict with ip in use")
				pod3 := "pod3.ns"
				pod3Nic1 := "pod3Nic1.ns"
				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIP43, nil, subnetName, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))

				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIP63, nil, subnetName, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))

				By("release pod with multiple nics")
				im.ReleaseAddressByPod(pod2, "")
				ip42, err := ipam.NewIP(freeIP42)
				Expect(err).ShouldNot(HaveOccurred())
				ip43, err := ipam.NewIP(freeIP43)
				Expect(err).ShouldNot(HaveOccurred())
				ip62, err := ipam.NewIP(freeIP62)
				Expect(err).ShouldNot(HaveOccurred())
				ip63, err := ipam.NewIP(freeIP63)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip42)).Should(BeTrue())
				Expect(im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip43)).Should(BeTrue())
				Expect(im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip62)).Should(BeTrue())
				Expect(im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip63)).Should(BeTrue())

				By("release pod with single nic")
				im.ReleaseAddressByPod(pod1, "")
				ip41, err := ipam.NewIP(freeIP41)
				Expect(err).ShouldNot(HaveOccurred())
				ip61, err := ipam.NewIP(freeIP61)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip41)).Should(BeTrue())
				Expect(im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip61)).Should(BeTrue())

				By("create new pod with released ips")
				pod4 := "pod4.ns"
				pod4Nic1 := "pod4Nic1.ns"

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIP41, nil, subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIP61, nil, subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())

				By("create pod with no initialized subnet")
				pod5 := "pod5.ns"
				pod5Nic1 := "pod5Nic1.ns"

				_, _, _, err = im.GetRandomAddress(pod5, pod5Nic1, nil, "invalid_subnet", "", nil, true)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))
			})

			It("change cidr", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, dualCIDR, dualGw, dualExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())

				err = im.AddOrUpdateSubnet(subnetName, "10.17.0.2/16,fe00::/112", dualGw, []string{"10.17.0.1", "fe00::1"})
				Expect(err).ShouldNot(HaveOccurred())
				ipv4, ipv6, _, err := im.GetRandomAddress("pod5.ns", "pod5.ns", nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.17.0.2"))
				Expect(ipv6).To(Equal("fe00::2"))
			})

			It("reuse released address when no unused address", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.2/30,fd00::/126", dualGw, nil)
				Expect(err).ShouldNot(HaveOccurred())

				ipv4, ipv6, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.16.0.1"))
				Expect(ipv6).To(Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns", "")
				ipv4, ipv6, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.16.0.2"))
				Expect(ipv6).To(Equal("fd00::2"))

				im.ReleaseAddressByPod("pod1.ns", "")
				ipv4, ipv6, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.16.0.1"))
				Expect(ipv6).To(Equal("fd00::1"))
			})

			It("do not reuse released address after update subnet's excludedIps", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.2/30,fd00::/126", dualGw, nil)
				Expect(err).ShouldNot(HaveOccurred())

				ipv4, ipv6, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.16.0.1"))
				Expect(ipv6).To(Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns", "")
				err = im.AddOrUpdateSubnet(subnetName, "10.16.0.2/30,fd00::/126", dualGw, []string{"10.16.0.1..10.16.0.2", "fd00::1..fd00::2"})
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))
			})
		})
	})
})
