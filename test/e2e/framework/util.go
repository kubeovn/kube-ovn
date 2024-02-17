package framework

import (
	"fmt"
	"math/rand/v2"
	"net"
	"sort"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/scylladb/go-set/strset"

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

func RandomCIDR(family string) string {
	fnIPv4 := func() string {
		cidr := net.IPNet{
			IP:   net.ParseIP("10.0.0.0").To4(),
			Mask: net.CIDRMask(24, 32),
		}
		cidr.IP[1] = 0xf0 | byte(ginkgo.GinkgoParallelProcess())
		cidr.IP[2] = byte(rand.IntN(0xff + 1))
		return cidr.String()
	}

	fnIPv6 := func() string {
		cidr := net.IPNet{
			IP:   net.ParseIP("fc00:10:ff::").To16(),
			Mask: net.CIDRMask(96, 128),
		}
		cidr.IP[9] = byte(ginkgo.GinkgoParallelProcess())
		cidr.IP[10] = byte(rand.IntN(0xff + 1))
		cidr.IP[11] = byte(rand.IntN(0xff + 1))
		return cidr.String()
	}

	switch family {
	case IPv4:
		return fnIPv4()
	case IPv6:
		return fnIPv6()
	case Dual:
		return fnIPv4() + "," + fnIPv6()
	default:
		Failf("invalid ip family: %q", family)
		return ""
	}
}

func sortIPs(ips []string) {
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
	for i := 0; i < count; i++ {
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
	if cidr == "" {
		return nil
	}

	_, ipNet, err := net.ParseCIDR(cidr)
	ExpectNoError(err)

	set := strset.NewWithSize(count)
	r := ipam.NewIPRangeFromCIDR(*ipNet)
	r = ipam.NewIPRange(r.Start().Add(2), r.End().Sub(1))
	for set.Size() != count {
		set.Add(r.Random().String())
	}

	ips := set.List()
	if sort {
		sortIPs(ips)
	}

	return ips
}

func RandomIPs(cidr, sep string, count int) string {
	cidrV4, cidrV6 := util.SplitStringIP(cidr)
	ipsV4 := randomSortedIPs(cidrV4, count, false)
	ipsV6 := randomSortedIPs(cidrV6, count, false)

	dual := make([]string, 0, count)
	for i := 0; i < count; i++ {
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
	if cidr == "" || count == 0 {
		return nil
	}

	_, ipNet, err := net.ParseCIDR(cidr)
	ExpectNoError(err)

	r := ipam.NewIPRangeFromCIDR(*ipNet)
	r = ipam.NewIPRange(r.Start().Add(2), r.End().Sub(1))

	ones, bits := ipNet.Mask.Size()
	rl := ipam.NewEmptyIPRangeList()
	set := strset.NewWithSize(count)
	for set.Size() != count/4 {
		prefix := (ones+bits)/2 + rand.IntN((bits-ones)/2+1)
		_, ipNet, err = net.ParseCIDR(fmt.Sprintf("%s/%d", r.Random(), prefix))
		ExpectNoError(err)

		v := ipam.NewIPRangeFromCIDR(*ipNet)
		start, end := v.Start(), v.End()
		if !r.Contains(start) || !r.Contains(end) || rl.Contains(start) || rl.Contains(end) {
			continue
		}

		rl = rl.MergeRange(ipam.NewIPRange(start, end))
		set.Add(ipNet.String())
	}

	count -= set.Size()
	m := count / 3 // <IP1>..<IP2>
	n := count - m // <IP>
	ips := make([]ipam.IP, 0, m*2+n)
	ipSet := strset.NewWithSize(cap(ips))
	for len(ips) != cap(ips) {
		ip := r.Random()
		if rl.Contains(ip) {
			continue
		}

		s := ip.String()
		if ipSet.Has(s) {
			continue
		}
		ips = append(ips, ip)
		ipSet.Add(s)
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
				set.Add(fmt.Sprintf("%s..%s", ips[x].String(), ips[y].String()))
				i, k = i+1, k+2
				continue
			}
		}

		if j != n {
			set.Add(ips[k%len(ips)].String())
			j, k = j+1, k+1
		}
	}

	return set.List()
}

func RandomIPPool(cidr string, count int) []string {
	cidrV4, cidrV6 := util.SplitStringIP(cidr)
	ipsV4, ipsV6 := randomPool(cidrV4, count), randomPool(cidrV6, count)
	set := strset.NewWithSize(len(cidrV4) + len(cidrV6))
	set.Add(ipsV4...)
	set.Add(ipsV6...)
	return set.List()
}

func PrevIP(ip string) string {
	v, err := ipam.NewIP(ip)
	ExpectNoError(err)
	return v.Sub(1).String()
}

func NextIP(ip string) string {
	v, err := ipam.NewIP(ip)
	ExpectNoError(err)
	return v.Add(1).String()
}
