package ipam

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

type IP string

type IPRange struct {
	Start IP
	End   IP
}

func (a IP) Equal(b IP) bool {
	return a == b
}

func (a IP) LessThan(b IP) bool {
	return util.Ip2BigInt(string(a)).Cmp(util.Ip2BigInt(string(b))) == -1
}

func (a IP) GreaterThan(b IP) bool {
	return util.Ip2BigInt(string(a)).Cmp(util.Ip2BigInt(string(b))) == 1
}

func (a IP) Add(num int64) IP {
	return IP(util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(string(a)), big.NewInt(num))))
}

func (a IP) Sub(num int64) IP {
	return IP(util.BigInt2Ip(big.NewInt(0).Sub(util.Ip2BigInt(string(a)), big.NewInt(num))))
}

func (ipr IPRange) IPExist(ip IP) bool {
	return (ipr.Start.LessThan(ip) || ipr.Start.Equal(ip)) &&
		(ipr.End.GreaterThan(ip) || ipr.End.Equal(ip))
}

type IPRangeList []*IPRange

func (iprl IPRangeList) Contains(ip IP) bool {
	for _, ipr := range iprl {
		if ipr.IPExist(ip) {
			return true
		}
	}
	return false
}

func (iprl IPRangeList) Equal(iprl2 IPRangeList) bool {
	if len(iprl) != len(iprl2) {
		return false
	}

	for i, range1 := range iprl {
		range2 := iprl2[i]
		if !range1.Start.Equal(range2.Start) || !range1.End.Equal(range2.End) {
			return false
		}
	}

	return true
}

func (iprl IPRangeList) IpRangetoString() string {
	var ipRangeString []string
	for _, ipr := range iprl {
		if ipr.Start.Equal(ipr.End) {
			ipRangeString = append(ipRangeString, string(ipr.Start))
		} else {
			ipRangeString = append(ipRangeString, fmt.Sprintf("%s-%s", ipr.Start, ipr.End))
		}
	}
	return strings.Join(ipRangeString, ",")
}

func splitIPRangeList(iprl IPRangeList, ip IP) (bool, IPRangeList) {
	newIPRangeList := []*IPRange{}
	split := false
	for _, ipr := range iprl {
		if split {
			newIPRangeList = append(newIPRangeList, ipr)
			continue
		}

		if ipr.Start.Equal(ipr.End) && ipr.Start.Equal(ip) {
			split = true
			continue
		}

		if ipr.Start.Equal(ip) {
			newIPRangeList = append(newIPRangeList, &IPRange{Start: ip.Add(1), End: ipr.End})
			split = true
			continue
		}

		if ipr.End.Equal(ip) {
			newIPRangeList = append(newIPRangeList, &IPRange{Start: ipr.Start, End: ip.Add(-1)})
			split = true
			continue
		}

		if ipr.IPExist(ip) {
			newIpr1 := IPRange{Start: ipr.Start, End: ip.Add(-1)}
			newIpr2 := IPRange{Start: ip.Add(1), End: ipr.End}
			newIPRangeList = append(newIPRangeList, &newIpr1, &newIpr2)
			split = true
			continue
		}

		newIPRangeList = append(newIPRangeList, ipr)
	}
	return split, newIPRangeList
}

func mergeIPRangeList(iprl IPRangeList, ip IP) (bool, IPRangeList) {
	insertIPRangeList := []*IPRange{}
	inserted := false
	if iprl.Contains(ip) {
		return false, nil
	}

	for _, ipr := range iprl {
		if inserted || ipr.Start.LessThan(ip) {
			insertIPRangeList = append(insertIPRangeList, ipr)
			continue
		}

		if ipr.Start.GreaterThan(ip) {
			insertIPRangeList = append(insertIPRangeList, &IPRange{Start: ip, End: ip}, ipr)
			inserted = true
			continue
		}
	}
	if !inserted {
		newIpr := IPRange{Start: ip, End: ip}
		insertIPRangeList = append(insertIPRangeList, &newIpr)
	}

	mergedIPRangeList := []*IPRange{}
	for _, ipr := range insertIPRangeList {
		if len(mergedIPRangeList) == 0 {
			mergedIPRangeList = append(mergedIPRangeList, ipr)
			continue
		}

		if mergedIPRangeList[len(mergedIPRangeList)-1].End.Add(1).Equal(ipr.Start) {
			mergedIPRangeList[len(mergedIPRangeList)-1].End = ipr.End
		} else {
			mergedIPRangeList = append(mergedIPRangeList, ipr)
		}
	}

	return true, mergedIPRangeList
}

func convertExcludeIps(excludeIps []string) IPRangeList {
	newIPRangeList := make([]*IPRange, 0, len(excludeIps))
	for _, ex := range excludeIps {
		ips := strings.Split(ex, "..")
		if len(ips) == 1 {
			ipr := IPRange{Start: IP(ips[0]), End: IP(ips[0])}
			newIPRangeList = append(newIPRangeList, &ipr)
		} else {
			ipr := IPRange{Start: IP(ips[0]), End: IP(ips[1])}
			newIPRangeList = append(newIPRangeList, &ipr)
		}
	}
	return newIPRangeList
}

func splitRange(a, b *IPRange) IPRangeList {
	if b.End.LessThan(a.Start) || b.Start.GreaterThan(a.End) {
		return IPRangeList{a}
	}

	if (a.Start.Equal(b.Start) || a.Start.GreaterThan(b.Start)) &&
		(a.End.Equal(b.End) || a.End.LessThan(b.End)) {
		return nil
	}

	if (a.Start.Equal(b.Start) || a.Start.GreaterThan(b.Start)) &&
		a.End.GreaterThan(b.End) {
		ipr := IPRange{Start: b.End.Add(1), End: a.End}
		return IPRangeList{&ipr}
	}

	if (a.End.Equal(b.End) || a.End.LessThan(b.End)) &&
		a.Start.LessThan(b.Start) {
		ipr := IPRange{Start: a.Start, End: b.Start.Add(-1)}
		return IPRangeList{&ipr}
	}

	ipr1 := IPRange{Start: a.Start, End: b.Start.Add(-1)}
	ipr2 := IPRange{Start: b.End.Add(1), End: a.End}
	return IPRangeList{&ipr1, &ipr2}
}
