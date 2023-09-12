package ipam

import (
	"fmt"
	"net"
	"sort"
	"strings"

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
	for i := 0; i < len(ips)/2; i++ {
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
				return nil, err
			}
			end, err := NewIP(ips[1])
			if err != nil {
				return nil, err
			}
			if start.GreaterThan(end) {
				return nil, fmt.Errorf("invalid ip range %q: %s is greater than %s", s, start, end)
			}
			r = NewIPRange(start, end)
		case strings.ContainsRune(s, '/'):
			_, cidr, err := net.ParseCIDR(s)
			if err != nil {
				return nil, err
			}
			r = NewIPRangeFromCIDR(*cidr)
		default:
			start, err := NewIP(s)
			if err != nil {
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
	copy(ret.ranges, r.ranges)
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
			r.ranges = append(r.ranges[:n], r.ranges[n+1:]...)
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
		r.ranges = append(r.ranges[:n], r.ranges[n+1:]...)
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

	for i := 0; i < r.Len(); i++ {
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
			ret.ranges = append(ret.ranges[:i+1], ret.ranges[i+2:]...)
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
	for i := 0; i < r.Len(); i++ {
		s = append(s, r.At(i).String())
	}
	return strings.Join(s, ",")
}
