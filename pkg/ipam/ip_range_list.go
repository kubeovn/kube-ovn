package ipam

import (
	"fmt"
	"math/big"
	"net"
	"slices"
	"sort"
	"strings"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/internal"
)

type IPRangeList struct {
	ranges []*IPRange
}

func NewEmptyIPRangeList() *IPRangeList {
	return &IPRangeList{}
}

func NewIPRangeList(ips ...IP) (*IPRangeList, error) {
	if len(ips)%2 != 0 {
		return nil, fmt.Errorf("length of ips must be an even number, but current is %d", len(ips))
	}

	ret := &IPRangeList{make([]*IPRange, len(ips)/2)}
	for i := range len(ips) / 2 {
		ret.ranges[i] = NewIPRange(ips[i*2], ips[i*2+1])
	}
	return ret, nil
}

func NewIPRangeListFrom(x ...string) (*IPRangeList, error) {
	ret := &IPRangeList{}
	for _, s := range x {
		var r *IPRange

		switch {
		case strings.Contains(s, ".."):
			ips := strings.Split(s, "..")
			start, err := NewIP(ips[0])
			if err != nil {
				klog.Error(err)
				return nil, err
			}
			end, err := NewIP(ips[1])
			if err != nil {
				klog.Error(err)
				return nil, err
			}
			if start.GreaterThan(end) {
				return nil, fmt.Errorf("invalid ip range %q: %s is greater than %s", s, start, end)
			}
			r = NewIPRange(start, end)
		case strings.ContainsRune(s, '/'):
			_, cidr, err := net.ParseCIDR(s)
			if err != nil {
				klog.Error(err)
				return nil, err
			}
			r = NewIPRangeFromCIDR(*cidr)
		default:
			start, err := NewIP(s)
			if err != nil {
				klog.Error(err)
				return nil, err
			}
			r = NewIPRange(start, start.Clone())
		}

		ret = ret.Merge(&IPRangeList{[]*IPRange{r}})
	}
	return ret, nil
}

func (r *IPRangeList) Clone() *IPRangeList {
	ret := &IPRangeList{make([]*IPRange, r.Len())}
	for i := range r.ranges {
		ret.ranges[i] = r.ranges[i].Clone()
	}
	return ret
}

func (r *IPRangeList) Len() int {
	return len(r.ranges)
}

func (r *IPRangeList) Count() internal.BigInt {
	var count internal.BigInt
	for _, v := range r.ranges {
		count = count.Add(v.Count())
	}
	return count
}

func (r *IPRangeList) At(i int) *IPRange {
	if i < len(r.ranges) {
		return r.ranges[i]
	}
	return nil
}

func (r *IPRangeList) Find(ip IP) (int, bool) {
	return sort.Find(len(r.ranges), func(i int) int {
		if r.At(i).Start().GreaterThan(ip) {
			return -1
		}
		if r.At(i).End().LessThan(ip) {
			return 1
		}
		return 0
	})
}

func (r *IPRangeList) Contains(ip IP) bool {
	_, found := r.Find(ip)
	return found
}

func (r *IPRangeList) Add(ip IP) bool {
	n, ok := r.Find(ip)
	if ok {
		return false
	}

	if (n-1 >= 0 && r.ranges[n-1].Add(ip)) ||
		(n < r.Len() && r.ranges[n].Add(ip)) {
		if n-1 >= 0 && n < r.Len() && r.ranges[n-1].End().Add(1).Equal(r.ranges[n].Start()) {
			r.ranges[n-1].SetEnd(r.ranges[n].End())
			r.ranges = slices.Delete(r.ranges, n, n+1)
		}
		return true
	}

	tmp := make([]*IPRange, r.Len()+1)
	copy(tmp, r.ranges[:n])
	tmp[n] = NewIPRange(ip, ip)
	copy(tmp[n+1:], r.ranges[n:])
	r.ranges = tmp

	return true
}

func (r *IPRangeList) Remove(ip IP) bool {
	n, ok := r.Find(ip)
	if !ok {
		return false
	}

	v, _ := r.ranges[n].Remove(ip)
	switch len(v) {
	case 0:
		r.ranges = slices.Delete(r.ranges, n, n+1)
	case 1:
		r.ranges[n] = v[0]
	case 2:
		tmp := make([]*IPRange, r.Len()+1)
		copy(tmp, r.ranges[:n])
		copy(tmp[n:], v)
		copy(tmp[n+2:], r.ranges[n+1:])
		r.ranges = tmp
	}

	return true
}

func (r *IPRangeList) Allocate(skipped []IP) IP {
	if r.Len() == 0 {
		return nil
	}

	if len(skipped) == 0 {
		ret := r.ranges[0].Start()
		r.Remove(ret)
		return ret
	}

	tmp := NewEmptyIPRangeList()
	for _, ip := range skipped {
		tmp.Add(ip)
	}

	filtered := r.Separate(tmp)
	if filtered.Len() == 0 {
		return nil
	}

	ret := filtered.ranges[0].Start()
	r.Remove(ret)
	return ret
}

func (r *IPRangeList) Equal(x *IPRangeList) bool {
	if r.Len() != x.Len() {
		return false
	}

	for i := range r.Len() {
		if !r.At(i).Start().Equal(x.At(i).Start()) || !r.At(i).End().Equal(x.At(i).End()) {
			return false
		}
	}

	return true
}

// Separate returns a new list which contains items which are in `r` but not in `x`
func (r *IPRangeList) Separate(x *IPRangeList) *IPRangeList {
	if r.Len() == 0 {
		return NewEmptyIPRangeList()
	}
	if x.Len() == 0 {
		return r.Clone()
	}

	var i, j int
	ret := &IPRangeList{}
	for ; i < r.Len(); i++ {
		start, end := r.At(i).Start(), r.At(i).End()
		for ; j < x.Len(); j++ {
			if x.At(j).End().LessThan(start) {
				continue
			}
			if x.At(j).Start().GreaterThan(end) {
				ret.ranges = append(ret.ranges, NewIPRange(start, end))
				break
			}
			if !x.At(j).End().LessThan(end) {
				if x.At(j).Start().GreaterThan(start) {
					ret.ranges = append(ret.ranges, NewIPRange(start, x.At(j).Start().Sub(1)))
				}
				break
			}
			if start.LessThan(x.At(j).Start()) {
				ret.ranges = append(ret.ranges, NewIPRange(start, x.At(j).Start().Sub(1)))
			}
			start = x.At(j).End().Add(1)
		}
		if j == x.Len() {
			ret.ranges = append(ret.ranges, NewIPRange(start, end))
		}
	}

	return ret
}

func (r *IPRangeList) Merge(x *IPRangeList) *IPRangeList {
	s := r.Separate(x)
	ret := &IPRangeList{make([]*IPRange, 0, s.Len()+x.Len())}

	var i, j int
	for i != s.Len() || j != x.Len() {
		if i == s.Len() {
			ret.ranges = append(ret.ranges, x.ranges[j].Clone())
			j++
			continue
		}
		if j == x.Len() {
			ret.ranges = append(ret.ranges, s.ranges[i].Clone())
			i++
			continue
		}
		if s.ranges[i].Start().LessThan(x.ranges[j].Start()) {
			ret.ranges = append(ret.ranges, s.ranges[i].Clone())
			i++
		} else {
			ret.ranges = append(ret.ranges, x.ranges[j].Clone())
			j++
		}
	}

	for i := 0; i < ret.Len()-1; i++ {
		if ret.ranges[i].End().Add(1).Equal(ret.ranges[i+1].Start()) {
			ret.ranges[i].end = ret.ranges[i+1].end
			ret.ranges = slices.Delete(ret.ranges, i+1, i+2)
			i--
		}
	}

	return ret.Clone()
}

func (r *IPRangeList) MergeRange(x *IPRange) *IPRangeList {
	return r.Merge(&IPRangeList{ranges: []*IPRange{x}}).Clone()
}

// Intersect returns a new list which contains items which are in both `r` and `x`
func (r *IPRangeList) Intersect(x *IPRangeList) *IPRangeList {
	r1, r2 := r.Separate(x), x.Separate(r)
	return r.Merge(x).Separate(r1).Separate(r2)
}

func (r *IPRangeList) String() string {
	s := make([]string, 0, r.Len())
	for i := range r.Len() {
		s = append(s, r.At(i).String())
	}
	return strings.Join(s, ",")
}

// ToCIDRs converts the IP range list to a sorted list of CIDR strings.
// Each range is converted to the minimal set of CIDRs that cover it.
// Single IPs are converted to /32 (IPv4) or /128 (IPv6) CIDRs.
//
// IMPORTANT: This method operates on the already-merged IP ranges in the IPRangeList.
// If the list was created via NewIPRangeListFrom(), overlapping ranges will have been merged,
// resulting in a more compact CIDR representation compared to util.ExpandIPPoolAddresses().
func (r *IPRangeList) ToCIDRs() ([]string, error) {
	if r.Len() == 0 {
		return nil, nil
	}

	var result []string
	for i := range r.Len() {
		start := r.At(i).Start()
		end := r.At(i).End()

		if start.Equal(end) {
			// Single IP: convert to /32 or /128
			bits := 32
			if len(start) == net.IPv6len {
				bits = 128
			}
			result = append(result, fmt.Sprintf("%s/%d", start.String(), bits))
			continue
		}

		// Range: convert to minimal CIDR set
		startIP := net.IP(start)
		endIP := net.IP(end)

		// Inline the IPRangeToCIDRs logic to avoid circular dependency
		length := net.IPv4len
		totalBits := 32
		if startIP.To4() == nil {
			length = net.IPv6len
			totalBits = 128
		}

		startInt := new(big.Int).SetBytes(startIP)
		endInt := new(big.Int).SetBytes(endIP)
		if startInt.Cmp(endInt) > 0 {
			return nil, fmt.Errorf("range %s..%s start is greater than end", start, end)
		}

		tmp := new(big.Int)
		for startInt.Cmp(endInt) <= 0 {
			zeros := countTrailingZeros(startInt, totalBits)
			// Add boundary check for safety
			if zeros > totalBits {
				return nil, fmt.Errorf("trailing zero count %d exceeds total bits %d", zeros, totalBits)
			}

			diff := tmp.Sub(endInt, startInt)
			diff.Add(diff, big.NewInt(1))

			var maxDiff int
			if bits := diff.BitLen(); bits > 0 {
				maxDiff = bits - 1
			}

			size := min(zeros, maxDiff)
			size = min(size, totalBits)
			if size < 0 {
				return nil, fmt.Errorf("calculated negative prefix size %d", size)
			}

			prefix := totalBits - size
			networkInt := new(big.Int).Set(startInt)
			networkBytes := networkInt.Bytes()
			if len(networkBytes) < length {
				padded := make([]byte, length)
				copy(padded[length-len(networkBytes):], networkBytes)
				networkBytes = padded
			} else if len(networkBytes) > length {
				networkBytes = networkBytes[len(networkBytes)-length:]
			}
			networkIP := net.IP(networkBytes)
			if length == net.IPv4len {
				networkIP = networkIP.To4()
			}
			network := &net.IPNet{IP: networkIP, Mask: net.CIDRMask(prefix, totalBits)}
			result = append(result, network.String())

			increment := new(big.Int).Lsh(big.NewInt(1), uint(size))
			startInt.Add(startInt, increment)
		}
	}

	sort.Strings(result)
	return result, nil
}

func countTrailingZeros(value *big.Int, totalBits int) int {
	if value.Sign() == 0 {
		return totalBits
	}

	zeros := 0
	for zeros < totalBits && value.Bit(zeros) == 0 {
		zeros++
	}
	return zeros
}
