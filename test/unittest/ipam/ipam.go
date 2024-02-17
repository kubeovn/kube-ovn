package ipam

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/ipam"
)

var _ = ginkgo.Describe("[IPAM]", func() {
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

	ginkgo.Context("[Subnet]", func() {
		ginkgo.Context("[IPv4]", func() {
			ginkgo.It("invalid subnet", func() {
				im := ipam.NewIPAM()

				ginkgo.By("invalid mask len > 32")
				maskV4Length := rand.Int() + 32
				err := im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("1.1.1.1/%d", maskV4Length), v4Gw, nil)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrInvalidCIDR))

				ginkgo.By("invalid ip range")
				invalidV4Ip := fmt.Sprintf("1.1.%d.1/24", rand.Int()+256)
				err = im.AddOrUpdateSubnet(subnetName, invalidV4Ip, v4Gw, nil)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrInvalidCIDR))
			})

			ginkgo.It("normal subnet", func() {
				ginkgo.By("create pod with static ip")
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(im.GetSubnetV4Mask(subnetName)).To(gomega.Equal(strings.Split(ipv4CIDR, "/")[1]))
				gomega.Expect(im.Subnets[subnetName].V4Gw).To(gomega.Equal(v4Gw))

				pod1 := "pod1.ns"
				pod1Nic1 := "pod1nic1.ns"
				freeIP1 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				ip, _, _, err := im.GetStaticAddress(pod1, pod1Nic1, freeIP1, nil, subnetName, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal(freeIP1))

				ip, _, _, err = im.GetRandomAddress(pod1, pod1Nic1, nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal(freeIP1))

				ginkgo.By("create multiple ips on one pod")
				pod2 := "pod2.ns"
				pod2Nic1 := "pod2Nic1.ns"
				pod2Nic2 := "pod2Nic2.ns"

				freeIP2 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				ip, _, _, err = im.GetRandomAddress(pod2, pod2Nic1, nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal(freeIP2))

				freeIP3 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				ip, _, _, err = im.GetRandomAddress(pod2, pod2Nic2, nil, subnetName, "", nil, true)

				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal(freeIP3))

				addresses := im.GetPodAddress(pod2)
				gomega.Expect(addresses).To(gomega.HaveLen(2))
				gomega.Expect([]string{addresses[0].IP, addresses[1].IP}).To(gomega.Equal([]string{freeIP2, freeIP3}))
				gomega.Expect(im.ContainAddress(freeIP2)).Should(gomega.BeTrue())
				gomega.Expect(im.ContainAddress(freeIP3)).Should(gomega.BeTrue())

				_, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP2, subnetName, pod2)
				gomega.Expect(isIPAssigned).Should(gomega.BeFalse())

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIP3, subnetName, pod2)
				gomega.Expect(isIPAssigned).Should(gomega.BeFalse())

				assignedPod, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP1, subnetName, pod2)
				gomega.Expect(isIPAssigned).Should(gomega.BeTrue())
				gomega.Expect(assignedPod).To(gomega.Equal(pod1))

				ginkgo.By("get static ip conflict with ip in use")
				pod3 := "pod3.ns"
				pod3Nic1 := "pod3Nic1.ns"
				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIP3, nil, subnetName, true)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrConflict))

				ginkgo.By("release pod with multiple nics")
				im.ReleaseAddressByPod(pod2, "")
				ip2, err := ipam.NewIP(freeIP2)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				ip3, err := ipam.NewIP(freeIP3)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip2)).Should(gomega.BeTrue())
				gomega.Expect(im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip3)).Should(gomega.BeTrue())

				ginkgo.By("release pod with single nic")
				im.ReleaseAddressByPod(pod1, "")
				ip1, err := ipam.NewIP(freeIP1)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip1)).To(gomega.BeTrue())

				ginkgo.By("create new pod with released ips")
				pod4 := "pod4.ns"
				pod4Nic1 := "pod4Nic1.ns"

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIP1, nil, subnetName, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				ginkgo.By("create pod with no initialized subnet")
				pod5 := "pod5.ns"
				pod5Nic1 := "pod5Nic1.ns"

				_, _, _, err = im.GetRandomAddress(pod5, pod5Nic1, nil, "invalid_subnet", "", nil, true)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrNoAvailable))
			})

			ginkgo.It("change cidr", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				err = im.AddOrUpdateSubnet(subnetName, "10.17.0.0/16", v4Gw, []string{"10.17.0.1"})
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				ip, _, _, err := im.GetRandomAddress("pod5.ns", "pod5.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal("10.17.0.2"))

				ginkgo.By("update invalid cidr, subnet should not change")
				err = im.AddOrUpdateSubnet(subnetName, "1.1.256.1", v4Gw, nil)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrInvalidCIDR))
				gomega.Expect(im.Subnets[subnetName].V4CIDR.IP.String()).To(gomega.Equal("10.17.0.0"))
			})

			ginkgo.It("reuse released address when no unused address", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.0/30", v4Gw, nil)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				ip, _, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal("10.16.0.1"))

				im.ReleaseAddressByPod("pod1.ns", "")
				ip, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal("10.16.0.2"))

				im.ReleaseAddressByPod("pod1.ns", "")
				ip, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal("10.16.0.1"))
			})

			ginkgo.It("do not reuse released address after update subnet's excludedIps", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.0/30", v4Gw, nil)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				ip, _, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal("10.16.0.1"))

				im.ReleaseAddressByPod("pod1.ns", "")
				err = im.AddOrUpdateSubnet(subnetName, "10.16.0.0/30", v4Gw, []string{"10.16.0.1..10.16.0.2"})
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				_, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrNoAvailable))
			})

			ginkgo.It("do not count excludedIps as subnet's v4availableIPs and v4usingIPs", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.10.0/28", "10.16.10.1", []string{"10.16.10.1", "10.16.10.10"})
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				ip, _, _, err := im.GetStaticAddress("pod1.ns", "pod1.ns", "10.16.10.10", nil, subnetName, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal("10.16.10.10"))

				v4UsingIPStr, _, v4AvailableIPStr, _ := im.GetSubnetIPRangeString(subnetName, []string{"10.16.10.10"})
				gomega.Expect(v4UsingIPStr).To(gomega.Equal(""))
				gomega.Expect(v4AvailableIPStr).To(gomega.Equal("10.16.10.2-10.16.10.9,10.16.10.11-10.16.10.14"))

				err = im.AddOrUpdateSubnet(subnetName, "10.16.10.0/28", "10.16.10.1", []string{"10.16.10.1"})
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				v4UsingIPStr, _, v4AvailableIPStr, _ = im.GetSubnetIPRangeString(subnetName, nil)
				gomega.Expect(v4UsingIPStr).To(gomega.Equal("10.16.10.10"))
				gomega.Expect(v4AvailableIPStr).To(gomega.Equal("10.16.10.2-10.16.10.9,10.16.10.11-10.16.10.14"))
			})
		})

		ginkgo.Context("[IPv6]", func() {
			ginkgo.It("invalid subnet", func() {
				im := ipam.NewIPAM()

				maskV6Length := rand.Int() + 128
				ginkgo.By("invalid mask len > 128")
				err := im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("fd00::/%d", maskV6Length), v6Gw, nil)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrInvalidCIDR))

				ginkgo.By("invalid ip range")
				err = im.AddOrUpdateSubnet(subnetName, "fd00::g/120", v6Gw, nil)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrInvalidCIDR))
			})

			ginkgo.It("normal subnet", func() {
				ginkgo.By("create pod with static ip")
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(im.Subnets[subnetName].V6Gw).To(gomega.Equal(v6Gw))

				pod1 := "pod1.ns"
				pod1Nic1 := "pod1nic1.ns"
				freeIP1 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				_, ip, _, err := im.GetStaticAddress(pod1, pod1Nic1, freeIP1, nil, subnetName, true)

				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal(freeIP1))

				_, ip, _, err = im.GetRandomAddress(pod1, pod1Nic1, nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal(freeIP1))

				ginkgo.By("create multiple ips on one pod")
				pod2 := "pod2.ns"
				pod2Nic1 := "pod2Nic1.ns"
				pod2Nic2 := "pod2Nic2.ns"

				freeIP2 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				_, ip, _, err = im.GetRandomAddress(pod2, pod2Nic1, nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal(freeIP2))

				freeIP3 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				_, ip, _, err = im.GetRandomAddress(pod2, pod2Nic2, nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal(freeIP3))

				addresses := im.GetPodAddress(pod2)
				gomega.Expect(addresses).To(gomega.HaveLen(2))
				gomega.Expect([]string{addresses[0].IP, addresses[1].IP}).To(gomega.Equal([]string{freeIP2, freeIP3}))
				gomega.Expect(im.ContainAddress(freeIP2)).Should(gomega.BeTrue())
				gomega.Expect(im.ContainAddress(freeIP3)).Should(gomega.BeTrue())

				_, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP2, subnetName, pod2)
				gomega.Expect(isIPAssigned).Should(gomega.BeFalse())

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIP3, subnetName, pod2)
				gomega.Expect(isIPAssigned).Should(gomega.BeFalse())

				assignedPod, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP1, subnetName, pod2)
				gomega.Expect(isIPAssigned).Should(gomega.BeTrue())
				gomega.Expect(assignedPod).To(gomega.Equal(pod1))

				ginkgo.By("get static ip conflict with ip in use")
				pod3 := "pod3.ns"
				pod3Nic1 := "pod3Nic1.ns"
				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIP3, nil, subnetName, true)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrConflict))

				ginkgo.By("release pod with multiple nics")
				im.ReleaseAddressByPod(pod2, "")
				ip2, err := ipam.NewIP(freeIP2)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				ip3, err := ipam.NewIP(freeIP3)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip2)).Should(gomega.BeTrue())
				gomega.Expect(im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip3)).Should(gomega.BeTrue())

				ginkgo.By("release pod with single nic")
				im.ReleaseAddressByPod(pod1, "")
				ip1, err := ipam.NewIP(freeIP1)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip1)).Should(gomega.BeTrue())

				ginkgo.By("create new pod with released ips")
				pod4 := "pod4.ns"
				pod4Nic1 := "pod4Nic1.ns"

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIP1, nil, subnetName, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				ginkgo.By("create pod with no initialized subnet")
				pod5 := "pod5.ns"
				pod5Nic1 := "pod5Nic1.ns"

				_, _, _, err = im.GetRandomAddress(pod5, pod5Nic1, nil, "invalid_subnet", "", nil, true)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrNoAvailable))
			})

			ginkgo.It("change cidr", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				err = im.AddOrUpdateSubnet(subnetName, "fe00::/112", v6Gw, []string{"fe00::1"})
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				_, ip, _, err := im.GetRandomAddress("pod5.ns", "pod5.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal("fe00::2"))

				ginkgo.By("update invalid cidr, subnet should not change")
				err = im.AddOrUpdateSubnet(subnetName, "fd00::g/120", v6Gw, nil)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrInvalidCIDR))
				gomega.Expect(im.Subnets[subnetName].V6CIDR.IP.String()).To(gomega.Equal("fe00::"))
			})

			ginkgo.It("reuse released address when no unused address", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "fd00::/126", v6Gw, nil)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				_, ip, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns", "")
				_, ip, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal("fd00::2"))

				im.ReleaseAddressByPod("pod1.ns", "")
				_, ip, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal("fd00::1"))
			})

			ginkgo.It("do not reuse released address after update subnet's excludedIps", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "fd00::/126", v6Gw, nil)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				_, ip, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip).To(gomega.Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns", "")
				err = im.AddOrUpdateSubnet(subnetName, "fd00::/126", v6Gw, []string{"fd00::1..fd00::2"})
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				_, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrNoAvailable))
			})
		})

		ginkgo.Context("[DualStack]", func() {
			ginkgo.It("invalid subnet", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("1.1.1.1/64,%s", ipv6CIDR), dualGw, nil)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrInvalidCIDR))
				err = im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("1.1.256.1/24,%s", ipv6CIDR), dualGw, nil)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrInvalidCIDR))
				err = im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("%s,fd00::/130", ipv4CIDR), dualGw, nil)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrInvalidCIDR))
				err = im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("%s,fd00::g/120", ipv4CIDR), dualGw, nil)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrInvalidCIDR))
			})

			ginkgo.It("normal subnet", func() {
				ginkgo.By("create pod with static ip")
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, dualCIDR, dualGw, dualExcludeIPs)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(im.Subnets[subnetName].V6Gw).To(gomega.Equal(v6Gw))
				gomega.Expect(im.Subnets[subnetName].V4Gw).To(gomega.Equal(v4Gw))

				pod1 := "pod1.ns"
				pod1Nic1 := "pod1nic1.ns"
				freeIP41 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				freeIP61 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				dualIP := fmt.Sprintf("%s,%s", freeIP41, freeIP61)
				ip4, ip6, _, err := im.GetStaticAddress(pod1, pod1Nic1, dualIP, nil, subnetName, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip4).To(gomega.Equal(freeIP41))
				gomega.Expect(ip6).To(gomega.Equal(freeIP61))

				ip4, ip6, _, err = im.GetRandomAddress(pod1, pod1Nic1, nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip4).To(gomega.Equal(freeIP41))
				gomega.Expect(ip6).To(gomega.Equal(freeIP61))

				ginkgo.By("create multiple ips on one pod")
				pod2 := "pod2.ns"
				pod2Nic1 := "pod2Nic1.ns"
				pod2Nic2 := "pod2Nic2.ns"

				freeIP42 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				freeIP62 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				ip4, ip6, _, err = im.GetRandomAddress(pod2, pod2Nic1, nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip4).To(gomega.Equal(freeIP42))
				gomega.Expect(ip6).To(gomega.Equal(freeIP62))

				freeIP43 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				freeIP63 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				ip4, ip6, _, err = im.GetRandomAddress(pod2, pod2Nic2, nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ip4).To(gomega.Equal(freeIP43))
				gomega.Expect(ip6).To(gomega.Equal(freeIP63))

				addresses := im.GetPodAddress(pod2)
				gomega.Expect(addresses).To(gomega.HaveLen(4))
				gomega.Expect([]string{addresses[0].IP, addresses[1].IP, addresses[2].IP, addresses[3].IP}).
					To(gomega.Equal([]string{freeIP42, freeIP62, freeIP43, freeIP63}))
				gomega.Expect(im.ContainAddress(freeIP42)).Should(gomega.BeTrue())
				gomega.Expect(im.ContainAddress(freeIP43)).Should(gomega.BeTrue())
				gomega.Expect(im.ContainAddress(freeIP62)).Should(gomega.BeTrue())
				gomega.Expect(im.ContainAddress(freeIP63)).Should(gomega.BeTrue())

				_, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP42, subnetName, pod2)
				gomega.Expect(isIPAssigned).Should(gomega.BeFalse())

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIP62, subnetName, pod2)
				gomega.Expect(isIPAssigned).Should(gomega.BeFalse())

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIP43, subnetName, pod2)
				gomega.Expect(isIPAssigned).Should(gomega.BeFalse())

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIP63, subnetName, pod2)
				gomega.Expect(isIPAssigned).Should(gomega.BeFalse())

				assignedPod, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP41, subnetName, pod2)
				gomega.Expect(isIPAssigned).Should(gomega.BeTrue())
				gomega.Expect(assignedPod).To(gomega.Equal(pod1))

				ginkgo.By("get static ip conflict with ip in use")
				pod3 := "pod3.ns"
				pod3Nic1 := "pod3Nic1.ns"
				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIP43, nil, subnetName, true)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrConflict))

				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIP63, nil, subnetName, true)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrConflict))

				ginkgo.By("release pod with multiple nics")
				im.ReleaseAddressByPod(pod2, "")
				ip42, err := ipam.NewIP(freeIP42)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				ip43, err := ipam.NewIP(freeIP43)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				ip62, err := ipam.NewIP(freeIP62)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				ip63, err := ipam.NewIP(freeIP63)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip42)).Should(gomega.BeTrue())
				gomega.Expect(im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip43)).Should(gomega.BeTrue())
				gomega.Expect(im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip62)).Should(gomega.BeTrue())
				gomega.Expect(im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip63)).Should(gomega.BeTrue())

				ginkgo.By("release pod with single nic")
				im.ReleaseAddressByPod(pod1, "")
				ip41, err := ipam.NewIP(freeIP41)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				ip61, err := ipam.NewIP(freeIP61)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip41)).Should(gomega.BeTrue())
				gomega.Expect(im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip61)).Should(gomega.BeTrue())

				ginkgo.By("create new pod with released ips")
				pod4 := "pod4.ns"
				pod4Nic1 := "pod4Nic1.ns"

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIP41, nil, subnetName, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIP61, nil, subnetName, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				ginkgo.By("create pod with no initialized subnet")
				pod5 := "pod5.ns"
				pod5Nic1 := "pod5Nic1.ns"

				_, _, _, err = im.GetRandomAddress(pod5, pod5Nic1, nil, "invalid_subnet", "", nil, true)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrNoAvailable))
			})

			ginkgo.It("change cidr", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, dualCIDR, dualGw, dualExcludeIPs)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				err = im.AddOrUpdateSubnet(subnetName, "10.17.0.2/16,fe00::/112", dualGw, []string{"10.17.0.1", "fe00::1"})
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				ipv4, ipv6, _, err := im.GetRandomAddress("pod5.ns", "pod5.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ipv4).To(gomega.Equal("10.17.0.2"))
				gomega.Expect(ipv6).To(gomega.Equal("fe00::2"))
			})

			ginkgo.It("reuse released address when no unused address", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.2/30,fd00::/126", dualGw, nil)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				ipv4, ipv6, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ipv4).To(gomega.Equal("10.16.0.1"))
				gomega.Expect(ipv6).To(gomega.Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns", "")
				ipv4, ipv6, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ipv4).To(gomega.Equal("10.16.0.2"))
				gomega.Expect(ipv6).To(gomega.Equal("fd00::2"))

				im.ReleaseAddressByPod("pod1.ns", "")
				ipv4, ipv6, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ipv4).To(gomega.Equal("10.16.0.1"))
				gomega.Expect(ipv6).To(gomega.Equal("fd00::1"))
			})

			ginkgo.It("do not reuse released address after update subnet's excludedIps", func() {
				im := ipam.NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "10.16.0.2/30,fd00::/126", dualGw, nil)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				ipv4, ipv6, _, err := im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				gomega.Expect(ipv4).To(gomega.Equal("10.16.0.1"))
				gomega.Expect(ipv6).To(gomega.Equal("fd00::1"))

				im.ReleaseAddressByPod("pod1.ns", "")
				err = im.AddOrUpdateSubnet(subnetName, "10.16.0.2/30,fd00::/126", dualGw, []string{"10.16.0.1..10.16.0.2", "fd00::1..fd00::2"})
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

				_, _, _, err = im.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
				gomega.Expect(err).Should(gomega.MatchError(ipam.ErrNoAvailable))
			})
		})
	})
})
