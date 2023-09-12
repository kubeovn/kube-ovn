package ipam

import (
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
		// bs----be----as----ae or as----ae----bs----be
		// a is aready splited with b

		return IPRangeList{a}
	}

	if (a.Start.Equal(b.Start) || a.Start.GreaterThan(b.Start)) &&
		(a.End.Equal(b.End) || a.End.LessThan(b.End)) {
		// as(bs)---- or bs----as----
		// ae(be) or ae----be
		// a is the same as b
		// a in b

		return nil
	}

	if (a.Start.Equal(b.Start) || a.Start.GreaterThan(b.Start)) &&
		a.End.GreaterThan(b.End) {
		// as(bs)---- or bs----as----
		// be----ae
		// as(bs)----be----ae
		// bs----as----be----ae
		// get be----ae

		ipr := IPRange{Start: b.End.Add(1), End: a.End}
		return IPRangeList{&ipr}
	}

	if (a.End.Equal(b.End) || a.End.LessThan(b.End)) &&
		a.Start.LessThan(b.Start) {
		// -----ae(be) or ----ae----be
		// -----as----bs
		// -----as----bs-----ae(be)
		// -----as----bs-----ae----be
		// get as----bs

		ipr := IPRange{Start: a.Start, End: b.Start.Add(-1)}
		return IPRangeList{&ipr}
	}

	ipr1 := IPRange{Start: a.Start, End: b.Start.Add(-1)}
	ipr2 := IPRange{Start: b.End.Add(1), End: a.End}
	results := IPRangeList{}
	if !ipr1.Start.GreaterThan(ipr1.End) {
		// start <= end
		results = append(results, &ipr1)
	}
	if !ipr2.Start.GreaterThan(ipr2.End) {
		// start <= end
		results = append(results, &ipr2)
	}
	return results
}
