package framework

import (
	"fmt"
	"math/big"
	"math/rand"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	KubeOvnNamespace = "kube-system"
	DaemonSetOvsOvn  = "ovs-ovn"
)

// RandomSuffix provides a random sequence to append to resources.
func RandomSuffix() string {
	return fmt.Sprintf("%d%04d", ginkgo.GinkgoParallelProcess(), rand.Intn(10000))
}

func RandomCIDR(family string) string {
	fnIPv4 := func() string {
		cidr := net.IPNet{
			IP:   net.ParseIP("10.0.0.0").To4(),
			Mask: net.CIDRMask(24, 32),
		}
		cidr.IP[1] = 0xf0 | byte(ginkgo.GinkgoParallelProcess())
		cidr.IP[2] = byte(rand.Intn(0xff + 1))
		return cidr.String()
	}

	fnIPv6 := func() string {
		cidr := net.IPNet{
			IP:   net.ParseIP("fc00:10:ff::").To16(),
			Mask: net.CIDRMask(96, 128),
		}
		cidr.IP[9] = byte(ginkgo.GinkgoParallelProcess())
		cidr.IP[10] = byte(rand.Intn(0xff + 1))
		cidr.IP[11] = byte(rand.Intn(0xff + 1))
		return cidr.String()
	}

	switch family {
	case "ipv4":
		return fnIPv4()
	case "ipv6":
		return fnIPv6()
	case "dual":
		return fnIPv4() + "," + fnIPv6()
	default:
		Failf("invalid ip family: %q", family)
		return ""
	}
}

func sortIPs(ips []string) {
	sort.Slice(ips, func(i, j int) bool {
		return util.Ip2BigInt(ips[i]).Cmp(util.Ip2BigInt(ips[j])) < 0
	})
}

// ipv4/ipv6 only
func RandomExcludeIPs(cidr string, count int) []string {
	if cidr == "" || count == 0 {
		return nil
	}

	rangeCount := rand.Intn(count + 1)
	ips := strings.Split(RandomIPPool(cidr, ";", rangeCount*2+count-rangeCount), ";")
	sortIPs(ips)

	var idx int
	rangeLeft := rangeCount
	ret := make([]string, 0, count)
	for i := 0; i < count; i++ {
		if rangeLeft != 0 && rand.Intn(count-i) < rangeLeft {
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

func RandomIPPool(cidr, sep string, count int) string {
	fn := func(cidr string) []string {
		if cidr == "" {
			return nil
		}

		firstIP, _ := util.FirstIP(cidr)
		_, ipnet, _ := net.ParseCIDR(cidr)

		base := util.Ip2BigInt(firstIP)
		base.Add(base, big.NewInt(1))
		prefix, size := ipnet.Mask.Size()
		rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
		max := big.NewInt(0).Exp(big.NewInt(2), big.NewInt(int64(size-prefix)), nil)
		max.Sub(max, big.NewInt(3))

		ips := make([]string, 0, count)
		for len(ips) != count {
			n := big.NewInt(0).Rand(rnd, max)
			if ip := util.BigInt2Ip(n.Add(n, base)); !util.ContainsString(ips, ip) {
				ips = append(ips, ip)
			}
		}
		return ips
	}

	cidrV4, cidrV6 := util.SplitStringIP(cidr)
	ipsV4, ipsV6 := fn(cidrV4), fn(cidrV6)

	dual := make([]string, 0, count)
	for i := 0; i < count; i++ {
		var ips []string
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

func PrevIP(ip string) string {
	n := util.Ip2BigInt(ip)
	return util.BigInt2Ip(n.Add(n, big.NewInt(-1)))
}

func NextIP(ip string) string {
	n := util.Ip2BigInt(ip)
	return util.BigInt2Ip(n.Add(n, big.NewInt(1)))
}
