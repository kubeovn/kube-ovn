package framework

import (
	"fmt"
	"math/rand/v2"
	"net"
	"sort"
	"strings"
	"sync"

	"k8s.io/utils/ptr"
	"k8s.io/utils/set"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	KubeOvnNamespace = "kube-system"
	DaemonSetOvsOvn  = "ovs-ovn"
)

// RandomSuffix provides a random sequence to append to resources.
func RandomSuffix() string {
	return fmt.Sprintf("%d%04d%04d", ginkgo.GinkgoParallelProcess(), rand.IntN(10000), rand.IntN(10000))
}

var (
	randCIDRSeedLock4 sync.Mutex
	randCIDRSeedLock6 sync.Mutex

	usedRandCIDRSeeds4 = set.New[byte]()
	usedRandCIDRSeeds6 = set.New[uint16]()
)

func randomCIDR4(seed *byte) string {
	ginkgo.GinkgoHelper()

	if seed == nil {
		randCIDRSeedLock4.Lock()
		defer randCIDRSeedLock4.Unlock()
		for usedRandCIDRSeeds4.Len() < 0xff+1 {
			seed = ptr.To(byte(rand.IntN(0xff + 1)))
			if !usedRandCIDRSeeds4.Has(*seed) {
				usedRandCIDRSeeds4.Insert(*seed)
				break
			}
		}
		ExpectNotNil(seed, "failed to generate random ipv4 CIDR seed")
	}

	cidr := net.IPNet{
		IP:   net.ParseIP("10.0.0.0").To4(),
		Mask: net.CIDRMask(24, 32),
	}
	cidr.IP[1] = 0xf0 | byte(ginkgo.GinkgoParallelProcess())
	cidr.IP[2] = *seed
	return cidr.String()
}

func randomCIDR6(seed *uint16) string {
	ginkgo.GinkgoHelper()

	if seed == nil {
		randCIDRSeedLock6.Lock()
		defer randCIDRSeedLock6.Unlock()
		for usedRandCIDRSeeds6.Len() < 0xffff+1 {
			seed = ptr.To(uint16(rand.IntN(0xffff + 1)))
			if !usedRandCIDRSeeds6.Has(*seed) {
				usedRandCIDRSeeds6.Insert(*seed)
				break
			}
		}
		ExpectNotNil(seed, "failed to generate random ipv4 CIDR seed")
	}

	cidr := net.IPNet{
		IP:   net.ParseIP("fc00:10:ff::").To16(),
		Mask: net.CIDRMask(96, 128),
	}
	cidr.IP[9] = byte(ginkgo.GinkgoParallelProcess())
	cidr.IP[10] = byte((*seed) >> 8)
	cidr.IP[11] = byte((*seed) & 0xff)
	return cidr.String()
}

func RandomCIDR(family string) string {
	ginkgo.GinkgoHelper()

	switch family {
	case IPv4:
		return randomCIDR4(nil)
	case IPv6:
		return randomCIDR6(nil)
	case Dual:
		return randomCIDR4(nil) + "," + randomCIDR6(nil)
	default:
		Failf("invalid ip family: %q", family)
		return ""
	}
}

func sortIPs(ips []string) {
	ginkgo.GinkgoHelper()

	sort.Slice(ips, func(i, j int) bool {
		x, err := ipam.NewIP(ips[i])
		ExpectNoError(err)
		y, err := ipam.NewIP(ips[j])
		ExpectNoError(err)
		return x.LessThan(y)
	})
}

// ipv4/ipv6 only
func RandomExcludeIPs(cidr string, count int) []string {
	ginkgo.GinkgoHelper()

	if cidr == "" || count == 0 {
		return nil
	}

	ExpectNotContainSubstring(cidr, ",")
	ExpectNotContainSubstring(cidr, ";")

	rangeCount := rand.IntN(count + 1)
	ips := randomSortedIPs(cidr, rangeCount*2+count-rangeCount, true)

	var idx int
	rangeLeft := rangeCount
	ret := make([]string, 0, count)
	for i := range count {
		if rangeLeft != 0 && rand.IntN(count-i) < rangeLeft {
			ret = append(ret, fmt.Sprintf("%s..%s", ips[idx], ips[idx+1]))
			idx++
			rangeLeft--
		} else {
			ret = append(ret, ips[idx])
		}
		idx++
	}

	return ret
}

// ipv4/ipv6 only
func randomSortedIPs(cidr string, count int, sort bool) []string {
	ginkgo.GinkgoHelper()

	if cidr == "" {
		return nil
	}

	_, ipNet, err := net.ParseCIDR(cidr)
	ExpectNoError(err)

	s := set.New[string]()
	r := ipam.NewIPRangeFromCIDR(*ipNet)
	r = ipam.NewIPRange(r.Start().Add(2), r.End().Sub(1))
	for s.Len() != count {
		s.Insert(r.Random().String())
	}

	ips := s.UnsortedList()
	if sort {
		sortIPs(ips)
	}

	return ips
}

func RandomIPs(cidr, sep string, count int) string {
	ginkgo.GinkgoHelper()

	cidrV4, cidrV6 := util.SplitStringIP(cidr)
	ipsV4 := randomSortedIPs(cidrV4, count, false)
	ipsV6 := randomSortedIPs(cidrV6, count, false)

	dual := make([]string, 0, count)
	for i := range count {
		ips := make([]string, 0, 2)
		if i < len(ipsV4) {
			ips = append(ips, ipsV4[i])
		}
		if i < len(ipsV6) {
			ips = append(ips, ipsV6[i])
		}
		dual = append(dual, strings.Join(ips, ","))
	}

	return strings.Join(dual, sep)
}

// ipv4/ipv6 only
func randomPool(cidr string, count int) []string {
	ginkgo.GinkgoHelper()

	if cidr == "" || count == 0 {
		return nil
	}

	_, ipNet, err := net.ParseCIDR(cidr)
	ExpectNoError(err)

	r := ipam.NewIPRangeFromCIDR(*ipNet)
	r = ipam.NewIPRange(r.Start().Add(2), r.End().Sub(1))

	ones, bits := ipNet.Mask.Size()
	rl := ipam.NewEmptyIPRangeList()
	s := set.New[string]()
	for s.Len() != count/4 {
		prefix := (ones+bits)/2 + rand.IntN((bits-ones)/2+1)
		_, ipNet, err = net.ParseCIDR(fmt.Sprintf("%s/%d", r.Random(), prefix))
		ExpectNoError(err)

		v := ipam.NewIPRangeFromCIDR(*ipNet)
		start, end := v.Start(), v.End()
		if !r.Contains(start) || !r.Contains(end) || rl.Contains(start) || rl.Contains(end) {
			continue
		}

		rl = rl.MergeRange(ipam.NewIPRange(start, end))
		s.Insert(ipNet.String())
	}

	count -= s.Len()
	m := count / 3 // <IP1>..<IP2>
	n := count - m // <IP>
	ips := make([]ipam.IP, 0, m*2+n)
	ipSet := set.New[string]()
	for len(ips) != cap(ips) {
		ip := r.Random()
		if rl.Contains(ip) {
			continue
		}

		ipStr := ip.String()
		if ipSet.Has(ipStr) {
			continue
		}
		ips = append(ips, ip)
		ipSet.Insert(ipStr)
	}
	sort.Slice(ips, func(i, j int) bool { return ips[i].LessThan(ips[j]) })

	var i, j int
	k := rand.IntN(len(ips))
	for i != m || j != n {
		if i != m {
			x, y := k%len(ips), (k+1)%len(ips)
			n1, _ := rl.Find(ips[x])
			n2, _ := rl.Find(ips[y])
			if n1 == n2 && ips[x].LessThan(ips[y]) {
				s.Insert(fmt.Sprintf("%s..%s", ips[x].String(), ips[y].String()))
				i, k = i+1, k+2
				continue
			}
		}

		if j != n {
			s.Insert(ips[k%len(ips)].String())
			j, k = j+1, k+1
		}
	}

	return s.UnsortedList()
}

// RandomIPPool generates random IP addresses from the given CIDR.
// WARNING: If cidr contains both IPv4 and IPv6 (dual-stack), this function
// will return a mix of both IP families. For use cases requiring single IP family
// (e.g., OVN address sets), split the CIDR first using util.SplitStringIP().
func RandomIPPool(cidr string, count int) []string {
	ginkgo.GinkgoHelper()

	cidrV4, cidrV6 := util.SplitStringIP(cidr)
	ipsV4, ipsV6 := randomPool(cidrV4, count), randomPool(cidrV6, count)
	s := set.New[string]()
	s.Insert(ipsV4...)
	s.Insert(ipsV6...)
	return s.UnsortedList()
}

func PrevIP(ip string) string {
	ginkgo.GinkgoHelper()

	v, err := ipam.NewIP(ip)
	ExpectNoError(err)
	return v.Sub(1).String()
}

func NextIP(ip string) string {
	ginkgo.GinkgoHelper()

	v, err := ipam.NewIP(ip)
	ExpectNoError(err)
	return v.Add(1).String()
}
