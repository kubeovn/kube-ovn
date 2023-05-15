package ipam

import (
	"fmt"
	"math/rand"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/util"
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

	Describe("[IPAM]", func() {
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
				By("create pod with static ip ")
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(im.GetSubnetV4Mask(subnetName)).To(Equal(strings.Split(ipv4CIDR, "/")[1]))
				Expect(im.Subnets[subnetName].V4Gw).To(Equal(v4Gw))

				pod1 := "pod1.ns"
				pod1Nic1 := "pod1nic1.ns"
				freeIp1 := string(im.Subnets[subnetName].V4FreeIPList[0].Start)
				ip, _, _, err := im.GetStaticAddress(pod1, pod1Nic1, freeIp1, "", subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIp1))

				ip, _, _, err = im.GetRandomAddress(pod1, pod1Nic1, "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIp1))

				By("create multiple ips on one pod ")
				pod2 := "pod2.ns"
				pod2Nic1 := "pod2Nic1.ns"
				pod2Nic2 := "pod2Nic2.ns"

				freeIp2 := string(im.Subnets[subnetName].V4FreeIPList[0].Start)
				ip, _, _, err = im.GetRandomAddress(pod2, pod2Nic1, "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIp2))

				freeIp3 := string(im.Subnets[subnetName].V4FreeIPList[0].Start)
				ip, _, _, err = im.GetRandomAddress(pod2, pod2Nic2, "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIp3))

				addresses := im.GetPodAddress(pod2)
				Expect(addresses).To(HaveLen(2))
				Expect([]string{addresses[0].Ip, addresses[1].Ip}).To(Equal([]string{freeIp2, freeIp3}))
				Expect(im.ContainAddress(freeIp2)).Should(BeTrue())
				Expect(im.ContainAddress(freeIp3)).Should(BeTrue())

				_, isIPAssigned := im.IsIPAssignedToOtherPod(freeIp2, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIp3, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				assignedPod, isIPAssigned := im.IsIPAssignedToOtherPod(freeIp1, subnetName, pod2)
				Expect(isIPAssigned).Should(BeTrue())
				Expect(assignedPod).To(Equal(pod1))

				By("get static ip conflict with ip in use ")
				pod3 := "pod3.ns"
				pod3Nic1 := "pod3Nic1.ns"
				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIp3, "", subnetName, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))

				By("release pod with multiple nics")
				im.ReleaseAddressByPod(pod2)
				Expect(im.Subnets[subnetName].V4ReleasedIPList.Contains(ipam.IP(freeIp2))).Should(BeTrue())
				Expect(im.Subnets[subnetName].V4ReleasedIPList.Contains(ipam.IP(freeIp3))).Should(BeTrue())

				By("release pod with single nic")
				im.ReleaseAddressByPod(pod1)
				Expect(im.Subnets[subnetName].V4ReleasedIPList.Contains(ipam.IP(freeIp1))).Should(BeTrue())

				By("create new pod with released ips")
				pod4 := "pod4.ns"
				pod4Nic1 := "pod4Nic1.ns"

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIp1, "", subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())

				By("create pod with no initialized subnet")
				pod5 := "pod5.ns"
				pod5Nic1 := "pod5Nic1.ns"

				_, _, _, err = im.GetRandomAddress(pod5, pod5Nic1, "", "invalid_subnet", nil, true)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))

			})

			It("change cidr", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())

				err = im.AddOrUpdateSubnet(subnetName, "10.17.0.0/16", v4Gw, []string{"10.17.0.1"})
				Expect(err).ShouldNot(HaveOccurred())
				ip, _, _, err := im.GetRandomAddress("pod5.ns", "pod5.ns", "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.17.0.2"))

				By("update invalid cidr, subnet should not change ")
				err = im.AddOrUpdateSubnet(subnetName, "1.1.256.1", v4Gw, nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
				Expect(im.Subnets[subnetName].V4CIDR.IP.String()).To(Equal("10.17.0.0"))
			})

			It("reuse released address when no unused address", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.0/30", v4Gw, nil)
				Expect(err).ShouldNot(HaveOccurred())

				ip, _, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.0.1"))

				im.ReleaseAddressByPod("pod1.ns")
				ip, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.0.2"))

				im.ReleaseAddressByPod("pod1.ns")
				ip, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.0.1"))
			})

			It("do not reuse released address after update subnet's excludedIps", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.0/30", v4Gw, nil)
				Expect(err).ShouldNot(HaveOccurred())

				ip, _, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("10.16.0.1"))

				im.ReleaseAddressByPod("pod1.ns")
				err = im.AddOrUpdateSubnet(subnetName, "10.16.0.0/30", v4Gw, []string{"10.16.0.1..10.16.0.2"})
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", "", subnetName, nil, true)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))
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
				By("create pod with static ip ")
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(im.Subnets[subnetName].V6Gw).To(Equal(v6Gw))

				pod1 := "pod1.ns"
				pod1Nic1 := "pod1nic1.ns"
				freeIp1 := string(im.Subnets[subnetName].V6FreeIPList[0].Start)
				_, ip, _, err := im.GetStaticAddress(pod1, pod1Nic1, freeIp1, "", subnetName, true)

				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIp1))

				_, ip, _, err = im.GetRandomAddress(pod1, pod1Nic1, "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIp1))

				By("create multiple ips on one pod ")
				pod2 := "pod2.ns"
				pod2Nic1 := "pod2Nic1.ns"
				pod2Nic2 := "pod2Nic2.ns"

				freeIp2 := string(im.Subnets[subnetName].V6FreeIPList[0].Start)
				_, ip, _, err = im.GetRandomAddress(pod2, pod2Nic1, "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIp2))

				freeIp3 := string(im.Subnets[subnetName].V6FreeIPList[0].Start)
				_, ip, _, err = im.GetRandomAddress(pod2, pod2Nic2, "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal(freeIp3))

				addresses := im.GetPodAddress(pod2)
				Expect(addresses).To(HaveLen(2))
				Expect([]string{addresses[0].Ip, addresses[1].Ip}).To(Equal([]string{freeIp2, freeIp3}))
				Expect(im.ContainAddress(freeIp2)).Should(BeTrue())
				Expect(im.ContainAddress(freeIp3)).Should(BeTrue())

				_, isIPAssigned := im.IsIPAssignedToOtherPod(freeIp2, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIp3, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				assignedPod, isIPAssigned := im.IsIPAssignedToOtherPod(freeIp1, subnetName, pod2)
				Expect(isIPAssigned).Should(BeTrue())
				Expect(assignedPod).To(Equal(pod1))

				By("get static ip conflict with ip in use ")
				pod3 := "pod3.ns"
				pod3Nic1 := "pod3Nic1.ns"
				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIp3, "", subnetName, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))

				By("release pod with multiple nics")
				im.ReleaseAddressByPod(pod2)
				Expect(im.Subnets[subnetName].V6ReleasedIPList.Contains(ipam.IP(freeIp2))).Should(BeTrue())
				Expect(im.Subnets[subnetName].V6ReleasedIPList.Contains(ipam.IP(freeIp3))).Should(BeTrue())

				By("release pod with single nic")
				im.ReleaseAddressByPod(pod1)
				Expect(im.Subnets[subnetName].V6ReleasedIPList.Contains(ipam.IP(freeIp1))).Should(BeTrue())

				By("create new pod with released ips")
				pod4 := "pod4.ns"
				pod4Nic1 := "pod4Nic1.ns"

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIp1, "", subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())

				By("create pod with no initialized subnet")
				pod5 := "pod5.ns"
				pod5Nic1 := "pod5Nic1.ns"

				_, _, _, err = im.GetRandomAddress(pod5, pod5Nic1, "", "invalid_subnet", nil, true)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))
			})

			It("change cidr", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())

				err = im.AddOrUpdateSubnet(subnetName, "fe00::/112", v6Gw, []string{"fe00::1"})
				Expect(err).ShouldNot(HaveOccurred())
				_, ip, _, err := im.GetRandomAddress("pod5.ns", "pod5.ns", "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fe00::2"))

				By("update invalid cidr, subnet should not change ")
				err = im.AddOrUpdateSubnet(subnetName, "fd00::g/120", v6Gw, nil)
				Expect(err).Should(MatchError(ipam.ErrInvalidCIDR))
				Expect(im.Subnets[subnetName].V6CIDR.IP.String()).To(Equal("fe00::"))
			})

			It("reuse released address when no unused address", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "fd00::/126", v6Gw, nil)
				Expect(err).ShouldNot(HaveOccurred())

				_, ip, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns")
				_, ip, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fd00::2"))

				im.ReleaseAddressByPod("pod1.ns")
				_, ip, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fd00::1"))
			})

			It("do not reuse released address after update subnet's excludedIps", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "fd00::/126", v6Gw, nil)
				Expect(err).ShouldNot(HaveOccurred())

				_, ip, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip).To(Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns")
				err = im.AddOrUpdateSubnet(subnetName, "fd00::/126", v6Gw, []string{"fd00::1..fd00::2"})
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", "", subnetName, nil, true)
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
				By("create pod with static ip ")
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, dualCIDR, dualGw, dualExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(im.Subnets[subnetName].V6Gw).To(Equal(v6Gw))
				Expect(im.Subnets[subnetName].V4Gw).To(Equal(v4Gw))

				pod1 := "pod1.ns"
				pod1Nic1 := "pod1nic1.ns"
				freeIp41 := string(im.Subnets[subnetName].V4FreeIPList[0].Start)
				freeIp61 := string(im.Subnets[subnetName].V6FreeIPList[0].Start)
				dualIp := fmt.Sprintf("%s,%s", freeIp41, freeIp61)
				ip4, ip6, _, err := im.GetStaticAddress(pod1, pod1Nic1, dualIp, "", subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip4).To(Equal(freeIp41))
				Expect(ip6).To(Equal(freeIp61))

				ip4, ip6, _, err = im.GetRandomAddress(pod1, pod1Nic1, "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip4).To(Equal(freeIp41))
				Expect(ip6).To(Equal(freeIp61))

				By("create multiple ips on one pod ")
				pod2 := "pod2.ns"
				pod2Nic1 := "pod2Nic1.ns"
				pod2Nic2 := "pod2Nic2.ns"

				freeIp42 := string(im.Subnets[subnetName].V4FreeIPList[0].Start)
				freeIp62 := string(im.Subnets[subnetName].V6FreeIPList[0].Start)
				ip4, ip6, _, err = im.GetRandomAddress(pod2, pod2Nic1, "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip4).To(Equal(freeIp42))
				Expect(ip6).To(Equal(freeIp62))

				freeIp43 := string(im.Subnets[subnetName].V4FreeIPList[0].Start)
				freeIp63 := string(im.Subnets[subnetName].V6FreeIPList[0].Start)
				ip4, ip6, _, err = im.GetRandomAddress(pod2, pod2Nic2, "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip4).To(Equal(freeIp43))
				Expect(ip6).To(Equal(freeIp63))

				addresses := im.GetPodAddress(pod2)
				Expect(addresses).To(HaveLen(4))
				Expect([]string{addresses[0].Ip, addresses[1].Ip, addresses[2].Ip, addresses[3].Ip}).
					To(Equal([]string{freeIp42, freeIp62, freeIp43, freeIp63}))
				Expect(im.ContainAddress(freeIp42)).Should(BeTrue())
				Expect(im.ContainAddress(freeIp43)).Should(BeTrue())
				Expect(im.ContainAddress(freeIp62)).Should(BeTrue())
				Expect(im.ContainAddress(freeIp63)).Should(BeTrue())

				_, isIPAssigned := im.IsIPAssignedToOtherPod(freeIp42, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIp62, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIp43, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIp63, subnetName, pod2)
				Expect(isIPAssigned).Should(BeFalse())

				assignedPod, isIPAssigned := im.IsIPAssignedToOtherPod(freeIp41, subnetName, pod2)
				Expect(isIPAssigned).Should(BeTrue())
				Expect(assignedPod).To(Equal(pod1))

				By("get static ip conflict with ip in use ")
				pod3 := "pod3.ns"
				pod3Nic1 := "pod3Nic1.ns"
				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIp43, "", subnetName, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))

				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIp63, "", subnetName, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))

				By("release pod with multiple nics")
				im.ReleaseAddressByPod(pod2)
				Expect(im.Subnets[subnetName].V4ReleasedIPList.Contains(ipam.IP(freeIp42))).Should(BeTrue())
				Expect(im.Subnets[subnetName].V4ReleasedIPList.Contains(ipam.IP(freeIp43))).Should(BeTrue())
				Expect(im.Subnets[subnetName].V6ReleasedIPList.Contains(ipam.IP(freeIp62))).Should(BeTrue())
				Expect(im.Subnets[subnetName].V6ReleasedIPList.Contains(ipam.IP(freeIp63))).Should(BeTrue())

				By("release pod with single nic")
				im.ReleaseAddressByPod(pod1)
				Expect(im.Subnets[subnetName].V4ReleasedIPList.Contains(ipam.IP(freeIp41))).Should(BeTrue())
				Expect(im.Subnets[subnetName].V6ReleasedIPList.Contains(ipam.IP(freeIp61))).Should(BeTrue())

				By("create new pod with released ips")
				pod4 := "pod4.ns"
				pod4Nic1 := "pod4Nic1.ns"

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIp41, "", subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIp61, "", subnetName, true)
				Expect(err).ShouldNot(HaveOccurred())

				By("create pod with no initialized subnet")
				pod5 := "pod5.ns"
				pod5Nic1 := "pod5Nic1.ns"

				_, _, _, err = im.GetRandomAddress(pod5, pod5Nic1, "", "invalid_subnet", nil, true)
				Expect(err).Should(MatchError(ipam.ErrNoAvailable))

			})

			It("change cidr", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, dualCIDR, dualGw, dualExcludeIPs)
				Expect(err).ShouldNot(HaveOccurred())

				err = im.AddOrUpdateSubnet(subnetName, "10.17.0.2/16,fe00::/112", dualGw, []string{"10.17.0.1", "fe00::1"})
				Expect(err).ShouldNot(HaveOccurred())
				ipv4, ipv6, _, err := im.GetRandomAddress("pod5.ns", "pod5.ns", "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.17.0.2"))
				Expect(ipv6).To(Equal("fe00::2"))
			})

			It("reuse released address when no unused address", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.2/30,fd00::/126", dualGw, nil)
				Expect(err).ShouldNot(HaveOccurred())

				ipv4, ipv6, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.16.0.1"))
				Expect(ipv6).To(Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns")
				ipv4, ipv6, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.16.0.2"))
				Expect(ipv6).To(Equal("fd00::2"))

				im.ReleaseAddressByPod("pod1.ns")
				ipv4, ipv6, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.16.0.1"))
				Expect(ipv6).To(Equal("fd00::1"))
			})

			It("do not reuse released address after update subnet's excludedIps", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.2/30,fd00::/126", dualGw, nil)
				Expect(err).ShouldNot(HaveOccurred())

				ipv4, ipv6, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", "", subnetName, nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal("10.16.0.1"))
				Expect(ipv6).To(Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns")
				err = im.AddOrUpdateSubnet(subnetName, "10.16.0.2/30,fd00::/126", dualGw, []string{"10.16.0.1..10.16.0.2", "fd00::1..fd00::2"})
				Expect(err).ShouldNot(HaveOccurred())

				_, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", "", subnetName, nil, true)
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

			ipr := ipam.IPRange{Start: "10.0.0.1", End: "10.0.0.254"}
			Expect(ipr.IPExist(ip1)).To(BeTrue())
			Expect(ipr.IPExist(ip2)).To(BeTrue())

			iprList := ipam.IPRangeList{&ipr}
			Expect(iprList.Contains(ip1)).To(BeTrue())
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

			ipr := ipam.IPRange{Start: "fd00::01", End: "fd00::ff"}
			Expect(ipr.IPExist(ip1)).To(BeTrue())
			Expect(ipr.IPExist(ip2)).To(BeTrue())

			iprList := ipam.IPRangeList{&ipr}
			Expect(iprList.Contains(ip1)).To(BeTrue())
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

				pod1 := "pod1.ns"
				pod1Nic1 := "pod1Nic1.ns"
				pod1Nic1mac := util.GenerateMac()

				_, _, err = subnet.GetStaticAddress(pod1, pod1Nic1, "10.16.0.2", pod1Nic1mac, false, true)
				Expect(err).ShouldNot(HaveOccurred())

				pod2 := "pod2.ns"
				pod2Nic1 := "pod2Nic1"
				_, _, err = subnet.GetStaticAddress(pod2, pod2Nic1, "10.16.0.3", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())

				pod2Nic2 := "pod2Nic2"
				_, _, err = subnet.GetStaticAddress(pod2, pod2Nic2, "10.16.0.20", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(subnet.V4FreeIPList).To(Equal(
					ipam.IPRangeList{
						&ipam.IPRange{Start: "10.16.0.5", End: "10.16.0.9"},
						&ipam.IPRange{Start: "10.16.0.24", End: "10.16.255.254"},
					}))

				Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.2"), pod1))
				Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.3"), pod2))
				Expect(subnet.V4IPToPod).To(HaveKeyWithValue(ipam.IP("10.16.0.20"), pod2))
				Expect(subnet.V4NicToIP).To(HaveKeyWithValue(pod1Nic1, ipam.IP("10.16.0.2")))
				Expect(subnet.V4NicToIP).To(HaveKeyWithValue(pod2Nic1, ipam.IP("10.16.0.3")))
				Expect(subnet.V4NicToIP).To(HaveKeyWithValue(pod2Nic2, ipam.IP("10.16.0.20")))
				Expect(subnet.NicToMac).To(HaveKeyWithValue(pod1Nic1, pod1Nic1mac))
				Expect(subnet.MacToPod).To(HaveKeyWithValue(pod1Nic1mac, pod1))

				_, _, err = subnet.GetStaticAddress("pod4.ns", "pod4.ns", "10.16.0.3", "", false, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))
				_, _, err = subnet.GetStaticAddress("pod5.ns", "pod5.ns", "19.16.0.3", "", false, true)
				Expect(err).Should(MatchError(ipam.ErrOutOfRange))
				_, _, err = subnet.GetStaticAddress("pod6.ns", "pod5.ns", "10.16.0.121", pod1Nic1mac, false, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))

				subnet.ReleaseAddress(pod1)
				subnet.ReleaseAddress(pod2)
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

				ip1, _, _, err := subnet.GetRandomAddress("pod1.ns", "pod1.ns", "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip1).To(Equal(ipam.IP("10.16.0.1")))
				ip1, _, _, err = subnet.GetRandomAddress("pod1.ns", "pod1.ns", "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip1).To(Equal(ipam.IP("10.16.0.1")))

				ip2, _, _, err := subnet.GetRandomAddress("pod2.ns", "pod2.ns", "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip2).To(Equal(ipam.IP("10.16.0.2")))

				_, _, _, err = subnet.GetRandomAddress("pod3.ns", "pod3.ns", "", nil, true)
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

				pod1 := "pod1.ns"
				pod1Nic1 := "pod1Nic1.ns"
				pod1Nic1mac := util.GenerateMac()

				_, _, err = subnet.GetStaticAddress(pod1, pod1Nic1, "fd00::2", pod1Nic1mac, false, true)
				Expect(err).ShouldNot(HaveOccurred())

				pod2 := "pod2.ns"
				pod2Nic1 := "pod2Nic1.ns"

				_, _, err = subnet.GetStaticAddress(pod2, pod2Nic1, "fd00::3", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())

				pod2Nic2 := "pod2Nic2.ns"
				_, _, err = subnet.GetStaticAddress(pod2, pod2Nic2, "fd00::14", "", false, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(subnet.V6FreeIPList).To(Equal(
					ipam.IPRangeList{
						&ipam.IPRange{Start: "fd00::5", End: "fd00::9"},
						&ipam.IPRange{Start: "fd00::18", End: "fd00::fffe"},
					}))

				Expect(subnet.V6IPToPod).To(HaveKeyWithValue(ipam.IP("fd00::2"), pod1))
				Expect(subnet.V6IPToPod).To(HaveKeyWithValue(ipam.IP("fd00::3"), pod2))
				Expect(subnet.V6IPToPod).To(HaveKeyWithValue(ipam.IP("fd00::14"), pod2))
				Expect(subnet.V6NicToIP).To(HaveKeyWithValue(pod1Nic1, ipam.IP("fd00::2")))
				Expect(subnet.V6NicToIP).To(HaveKeyWithValue(pod2Nic1, ipam.IP("fd00::3")))
				Expect(subnet.V6NicToIP).To(HaveKeyWithValue(pod2Nic2, ipam.IP("fd00::14")))
				Expect(subnet.NicToMac).To(HaveKeyWithValue(pod1Nic1, pod1Nic1mac))
				Expect(subnet.MacToPod).To(HaveKeyWithValue(pod1Nic1mac, pod1))

				_, _, err = subnet.GetStaticAddress("pod4.ns", "pod4.ns", "fd00::3", "", false, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))
				_, _, err = subnet.GetStaticAddress("pod5.ns", "pod5.ns", "fe00::3", "", false, true)
				Expect(err).Should(MatchError(ipam.ErrOutOfRange))
				_, _, err = subnet.GetStaticAddress("pod6.ns", "pod5.ns", "fd00::f9", pod1Nic1mac, false, true)
				Expect(err).Should(MatchError(ipam.ErrConflict))

				subnet.ReleaseAddress(pod1)
				subnet.ReleaseAddress(pod2)
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

				_, ip1, _, err := subnet.GetRandomAddress("pod1.ns", "pod1.ns", "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip1).To(Equal(ipam.IP("fd00::1")))
				_, ip1, _, err = subnet.GetRandomAddress("pod1.ns", "pod1.ns", "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip1).To(Equal(ipam.IP("fd00::1")))

				_, ip2, _, err := subnet.GetRandomAddress("pod2.ns", "pod2.ns", "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ip2).To(Equal(ipam.IP("fd00::2")))

				_, _, _, err = subnet.GetRandomAddress("pod3.ns", "pod3.ns", "", nil, true)
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

				ipv4, ipv6, _, err := subnet.GetRandomAddress("pod1.ns", "pod1.ns", "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal(ipam.IP("10.16.0.1")))
				Expect(ipv6).To(Equal(ipam.IP("fd00::1")))
				ipv4, ipv6, _, err = subnet.GetRandomAddress("pod1.ns", "pod1.ns", "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal(ipam.IP("10.16.0.1")))
				Expect(ipv6).To(Equal(ipam.IP("fd00::1")))

				ipv4, ipv6, _, err = subnet.GetRandomAddress("pod2.ns", "pod2.ns", "", nil, true)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(ipv4).To(Equal(ipam.IP("10.16.0.2")))
				Expect(ipv6).To(Equal(ipam.IP("fd00::2")))

				_, _, _, err = subnet.GetRandomAddress("pod3.ns", "pod3.ns", "", nil, true)
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
