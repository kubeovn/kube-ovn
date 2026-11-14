package ipam

import (
	"fmt"
	"math/rand/v2"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/scylladb/go-set/strset"
	"github.com/scylladb/go-set/u32set"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestAdditionalIPRangeList(t *testing.T) {
	t.Run("IPRangeList", func(t *testing.T) {
		newIP := func(s string) IP {
			t.Helper()
			ip, err := NewIP(s)
			require.NoError(t, err)
			return ip
		}

		t.Run("IPv4 Contains", func(t *testing.T) {
			v4, err := NewIPRangeList(
				newIP("10.0.0.5"), newIP("10.0.0.5"),
				newIP("10.0.0.13"), newIP("10.0.0.18"),
			)
			require.NoError(t, err)

			require.False(t, v4.Contains(newIP("10.0.0.4")))
			require.True(t, v4.Contains(newIP("10.0.0.5")))
			require.False(t, v4.Contains(newIP("10.0.0.6")))

			require.False(t, v4.Contains(newIP("10.0.0.12")))
			require.True(t, v4.Contains(newIP("10.0.0.13")))
			require.True(t, v4.Contains(newIP("10.0.0.14")))
			require.True(t, v4.Contains(newIP("10.0.0.17")))
			require.True(t, v4.Contains(newIP("10.0.0.18")))
			require.False(t, v4.Contains(newIP("10.0.0.19")))
		})

		t.Run("IPv6 Contains", func(t *testing.T) {
			v6, err := NewIPRangeList(
				newIP("2001:db8::5"), newIP("2001:db8::5"),
				newIP("2001:db8::13"), newIP("2001:db8::18"),
			)
			require.NoError(t, err)

			require.False(t, v6.Contains(newIP("2001:db8::4")))
			require.True(t, v6.Contains(newIP("2001:db8::5")))
			require.False(t, v6.Contains(newIP("2001:db8::6")))

			require.False(t, v6.Contains(newIP("2001:db8::12")))
			require.True(t, v6.Contains(newIP("2001:db8::13")))
			require.True(t, v6.Contains(newIP("2001:db8::14")))
			require.True(t, v6.Contains(newIP("2001:db8::17")))
			require.True(t, v6.Contains(newIP("2001:db8::18")))
			require.False(t, v6.Contains(newIP("2001:db8::19")))
		})

		t.Run("IPv4 Add", func(t *testing.T) {
			v4, err := NewIPRangeList(
				newIP("10.0.0.5"), newIP("10.0.0.5"),
				newIP("10.0.0.13"), newIP("10.0.0.18"),
			)
			require.NoError(t, err)

			require.True(t, v4.Add(newIP("10.0.0.4")))
			require.False(t, v4.Add(newIP("10.0.0.5")))
			require.True(t, v4.Add(newIP("10.0.0.6")))

			require.True(t, v4.Add(newIP("10.0.0.12")))
			require.False(t, v4.Add(newIP("10.0.0.13")))
			require.False(t, v4.Add(newIP("10.0.0.14")))
			require.False(t, v4.Add(newIP("10.0.0.17")))
			require.False(t, v4.Add(newIP("10.0.0.18")))
			require.True(t, v4.Add(newIP("10.0.0.19")))

			expected, err := NewIPRangeList(
				newIP("10.0.0.4"), newIP("10.0.0.6"),
				newIP("10.0.0.12"), newIP("10.0.0.19"),
			)
			require.NoError(t, err)
			require.True(t, v4.Equal(expected))
		})

		t.Run("IPv6 Add", func(t *testing.T) {
			v6, err := NewIPRangeList(
				newIP("2001:db8::5"), newIP("2001:db8::5"),
				newIP("2001:db8::13"), newIP("2001:db8::18"),
			)
			require.NoError(t, err)

			require.True(t, v6.Add(newIP("2001:db8::4")))
			require.False(t, v6.Add(newIP("2001:db8::5")))
			require.True(t, v6.Add(newIP("2001:db8::6")))

			require.True(t, v6.Add(newIP("2001:db8::12")))
			require.False(t, v6.Add(newIP("2001:db8::13")))
			require.False(t, v6.Add(newIP("2001:db8::14")))
			require.False(t, v6.Add(newIP("2001:db8::17")))
			require.False(t, v6.Add(newIP("2001:db8::18")))
			require.True(t, v6.Add(newIP("2001:db8::19")))

			expected, err := NewIPRangeList(
				newIP("2001:db8::4"), newIP("2001:db8::6"),
				newIP("2001:db8::12"), newIP("2001:db8::19"),
			)
			require.NoError(t, err)
			require.True(t, v6.Equal(expected))
		})

		t.Run("IPv4 Remove", func(t *testing.T) {
			v4, err := NewIPRangeList(
				newIP("10.0.0.5"), newIP("10.0.0.5"),
				newIP("10.0.0.13"), newIP("10.0.0.18"),
			)
			require.NoError(t, err)

			require.False(t, v4.Remove(newIP("10.0.0.4")))
			require.True(t, v4.Remove(newIP("10.0.0.5")))
			require.False(t, v4.Remove(newIP("10.0.0.6")))

			require.False(t, v4.Remove(newIP("10.0.0.12")))
			require.True(t, v4.Remove(newIP("10.0.0.13")))
			require.True(t, v4.Remove(newIP("10.0.0.14")))
			require.True(t, v4.Remove(newIP("10.0.0.17")))
			require.True(t, v4.Remove(newIP("10.0.0.18")))
			require.False(t, v4.Remove(newIP("10.0.0.19")))

			expected, err := NewIPRangeList(
				newIP("10.0.0.15"), newIP("10.0.0.16"),
			)
			require.NoError(t, err)
			require.True(t, v4.Equal(expected))

			// split the range
			v4, err = NewIPRangeList(
				newIP("10.0.0.10"), newIP("10.0.0.20"),
			)
			require.NoError(t, err)
			require.True(t, v4.Remove(newIP("10.0.0.15")))
			expected, err = NewIPRangeList(
				newIP("10.0.0.10"), newIP("10.0.0.14"),
				newIP("10.0.0.16"), newIP("10.0.0.20"),
			)
			require.NoError(t, err)
			require.True(t, v4.Equal(expected))
		})

		t.Run("IPv6 Remove", func(t *testing.T) {
			v6, err := NewIPRangeList(
				newIP("2001:db8::5"), newIP("2001:db8::5"),
				newIP("2001:db8::13"), newIP("2001:db8::18"),
			)
			require.NoError(t, err)

			require.False(t, v6.Remove(newIP("2001:db8::4")))
			require.True(t, v6.Remove(newIP("2001:db8::5")))
			require.False(t, v6.Remove(newIP("2001:db8::6")))

			require.False(t, v6.Remove(newIP("2001:db8::12")))
			require.True(t, v6.Remove(newIP("2001:db8::13")))
			require.True(t, v6.Remove(newIP("2001:db8::14")))
			require.True(t, v6.Remove(newIP("2001:db8::17")))
			require.True(t, v6.Remove(newIP("2001:db8::18")))
			require.False(t, v6.Remove(newIP("2001:db8::19")))

			expected, err := NewIPRangeList(
				newIP("2001:db8::15"), newIP("2001:db8::16"),
			)
			require.NoError(t, err)
			require.True(t, v6.Equal(expected))

			// split the range
			v6, err = NewIPRangeList(
				newIP("2001:db8::10"), newIP("2001:db8::20"),
			)
			require.NoError(t, err)
			require.True(t, v6.Remove(newIP("2001:db8::15")))
			expected, err = NewIPRangeList(
				newIP("2001:db8::10"), newIP("2001:db8::14"),
				newIP("2001:db8::16"), newIP("2001:db8::20"),
			)
			require.NoError(t, err)
			require.True(t, v6.Equal(expected))
		})

		t.Run("IPv4 Allocate", func(t *testing.T) {
			v4, err := NewIPRangeList(
				newIP("10.0.0.13"), newIP("10.0.0.16"),
			)
			require.NoError(t, err)

			ip := v4.Allocate(nil)
			require.NotNil(t, ip)
			require.Equal(t, "10.0.0.13", ip.String())

			ip = v4.Allocate(nil)
			require.NotNil(t, ip)
			require.Equal(t, "10.0.0.14", ip.String())

			ip = v4.Allocate([]IP{newIP("10.0.0.15"), newIP("10.0.0.16")})
			require.Nil(t, ip)
		})

		t.Run("IPv4 Separate", func(t *testing.T) {
			v41, err := NewIPRangeList(
				newIP("10.0.0.1"), newIP("10.0.0.1"),
				newIP("10.0.0.5"), newIP("10.0.0.5"),
				newIP("10.0.0.13"), newIP("10.0.0.18"),
				newIP("10.0.0.23"), newIP("10.0.0.28"),
				newIP("10.0.0.33"), newIP("10.0.0.38"),
				newIP("10.0.0.43"), newIP("10.0.0.48"),
				newIP("10.0.0.53"), newIP("10.0.0.58"),
				newIP("10.0.0.63"), newIP("10.0.0.68"),
			)
			require.NoError(t, err)

			v42, err := NewIPRangeList(
				newIP("10.0.0.1"), newIP("10.0.0.1"),
				newIP("10.0.0.11"), newIP("10.0.0.15"),
				newIP("10.0.0.17"), newIP("10.0.0.19"),
				newIP("10.0.0.23"), newIP("10.0.0.25"),
				newIP("10.0.0.27"), newIP("10.0.0.28"),
				newIP("10.0.0.33"), newIP("10.0.0.38"),
				newIP("10.0.0.42"), newIP("10.0.0.49"),
				newIP("10.0.0.53"), newIP("10.0.0.58"),
			)
			require.NoError(t, err)

			expected, err := NewIPRangeList(
				newIP("10.0.0.5"), newIP("10.0.0.5"),
				newIP("10.0.0.16"), newIP("10.0.0.16"),
				newIP("10.0.0.26"), newIP("10.0.0.26"),
				newIP("10.0.0.63"), newIP("10.0.0.68"),
			)
			require.NoError(t, err)

			separated := v41.Separate(v42)
			require.True(t, separated.Equal(expected))
		})

		t.Run("IPv6 Separate", func(t *testing.T) {
			v61, err := NewIPRangeList(
				newIP("2001:db8::1"), newIP("2001:db8::1"),
				newIP("2001:db8::5"), newIP("2001:db8::5"),
				newIP("2001:db8::13"), newIP("2001:db8::18"),
				newIP("2001:db8::23"), newIP("2001:db8::28"),
				newIP("2001:db8::33"), newIP("2001:db8::38"),
				newIP("2001:db8::43"), newIP("2001:db8::48"),
				newIP("2001:db8::53"), newIP("2001:db8::58"),
				newIP("2001:db8::63"), newIP("2001:db8::68"),
			)
			require.NoError(t, err)

			v62, err := NewIPRangeList(
				newIP("2001:db8::1"), newIP("2001:db8::1"),
				newIP("2001:db8::11"), newIP("2001:db8::15"),
				newIP("2001:db8::17"), newIP("2001:db8::19"),
				newIP("2001:db8::23"), newIP("2001:db8::25"),
				newIP("2001:db8::27"), newIP("2001:db8::28"),
				newIP("2001:db8::33"), newIP("2001:db8::38"),
				newIP("2001:db8::42"), newIP("2001:db8::49"),
				newIP("2001:db8::53"), newIP("2001:db8::58"),
			)
			require.NoError(t, err)

			expected, err := NewIPRangeList(
				newIP("2001:db8::5"), newIP("2001:db8::5"),
				newIP("2001:db8::16"), newIP("2001:db8::16"),
				newIP("2001:db8::26"), newIP("2001:db8::26"),
				newIP("2001:db8::63"), newIP("2001:db8::68"),
			)
			require.NoError(t, err)

			separated := v61.Separate(v62)
			require.True(t, separated.Equal(expected))
		})

		t.Run("IPv4 Merge", func(t *testing.T) {
			v41, err := NewIPRangeList(
				newIP("10.0.0.1"), newIP("10.0.0.1"),
				newIP("10.0.0.3"), newIP("10.0.0.3"),
				newIP("10.0.0.5"), newIP("10.0.0.5"),
				newIP("10.0.0.13"), newIP("10.0.0.18"),
				newIP("10.0.0.23"), newIP("10.0.0.28"),
				newIP("10.0.0.33"), newIP("10.0.0.38"),
				newIP("10.0.0.43"), newIP("10.0.0.48"),
				newIP("10.0.0.53"), newIP("10.0.0.58"),
				newIP("10.0.0.63"), newIP("10.0.0.68"),
				newIP("10.0.0.73"), newIP("10.0.0.78"),
				newIP("10.0.0.83"), newIP("10.0.0.88"),
				newIP("10.0.0.93"), newIP("10.0.0.95"),
			)
			require.NoError(t, err)

			v42, err := NewIPRangeList(
				newIP("10.0.0.1"), newIP("10.0.0.1"),
				newIP("10.0.0.4"), newIP("10.0.0.4"),
				newIP("10.0.0.11"), newIP("10.0.0.15"),
				newIP("10.0.0.17"), newIP("10.0.0.19"),
				newIP("10.0.0.23"), newIP("10.0.0.25"),
				newIP("10.0.0.27"), newIP("10.0.0.28"),
				newIP("10.0.0.33"), newIP("10.0.0.38"),
				newIP("10.0.0.42"), newIP("10.0.0.49"),
				newIP("10.0.0.53"), newIP("10.0.0.58"),
				newIP("10.0.0.75"), newIP("10.0.0.85"),
				newIP("10.0.0.96"), newIP("10.0.0.98"),
			)
			require.NoError(t, err)

			expected, err := NewIPRangeList(
				newIP("10.0.0.1"), newIP("10.0.0.1"),
				newIP("10.0.0.3"), newIP("10.0.0.5"),
				newIP("10.0.0.11"), newIP("10.0.0.19"),
				newIP("10.0.0.23"), newIP("10.0.0.28"),
				newIP("10.0.0.33"), newIP("10.0.0.38"),
				newIP("10.0.0.42"), newIP("10.0.0.49"),
				newIP("10.0.0.53"), newIP("10.0.0.58"),
				newIP("10.0.0.63"), newIP("10.0.0.68"),
				newIP("10.0.0.73"), newIP("10.0.0.88"),
				newIP("10.0.0.93"), newIP("10.0.0.98"),
			)
			require.NoError(t, err)

			merged := v41.Merge(v42)
			require.True(t, merged.Equal(expected))
		})
	})

	t.Run("IPv4 NewIPRangeListFrom", func(t *testing.T) {
		n := 40 + rand.IntN(20)
		cidrList := make([]*net.IPNet, 0, n)
		cidrSet := u32set.NewWithSize(n * 2)
		for attempts := 0; len(cidrList) != cap(cidrList) && attempts < cap(cidrList)*1000; attempts++ {
			_, cidr, err := net.ParseCIDR(fmt.Sprintf("%s/%d", util.Uint32ToIPv4(rand.Uint32()), 16+rand.IntN(16)))
			require.NoError(t, err)

			var invalid bool
			for _, c := range cidrList {
				if c.Contains(cidr.IP) || cidr.Contains(c.IP) {
					invalid = true
					break
				}
			}
			if !invalid {
				cidrList = append(cidrList, cidr)
				cidrSet.Add(util.IPv4ToUint32(cidr.IP))
				bcast := make(net.IP, len(cidr.IP))
				for i := range bcast {
					bcast[i] = cidr.IP[i] | ^cidr.Mask[i]
				}
				cidrSet.Add(util.IPv4ToUint32(bcast))
			}
		}
		require.Equal(t, cap(cidrList), len(cidrList))

		n = 80 + rand.IntN(40)
		set := u32set.NewWithSize(cidrSet.Size() + n)
		for attempts := 0; set.Size() != n && attempts < n*1000; attempts++ {
			v := rand.Uint32()
			ip := net.ParseIP(util.Uint32ToIPv4(v))
			var invalid bool
			for _, cidr := range cidrList {
				if cidr.Contains(ip) {
					invalid = true
					break
				}
			}
			if !invalid {
				set.Add(v)
			}
		}
		require.Equal(t, n, set.Size())
		set.Merge(cidrSet)

		ints := set.List()
		slices.Sort(ints)

		ips := make([]string, 0, len(cidrList)+set.Size())
		mergedInts := make([]uint32, 0, set.Size()*2)
		var expectedCount uint32
		for i := 0; i < len(ints); i++ {
			if cidrSet.Has(ints[i]) {
				expectedCount += ints[i+1] - ints[i] + 1
				if i != 0 && ints[i] == ints[i-1]+1 {
					mergedInts[len(mergedInts)-1] = ints[i+1]
				} else {
					mergedInts = append(mergedInts, ints[i], ints[i+1])
				}
				i++
				continue
			}

			start := util.Uint32ToIPv4(ints[i])
			if cidrSet.Has(ints[i]) || (rand.Int()%2 == 0 && i+1 != len(ints) && !cidrSet.Has(ints[i+1])) {
				if !cidrSet.Has(ints[i]) {
					end := util.Uint32ToIPv4(ints[i+1])
					ips = append(ips, fmt.Sprintf("%s..%s", start, end))
				}
				if i != 0 && ints[i] == ints[i-1]+1 {
					mergedInts[len(mergedInts)-1] = ints[i+1]
				} else {
					mergedInts = append(mergedInts, ints[i], ints[i+1])
				}
				expectedCount += ints[i+1] - ints[i] + 1
				i++
			} else {
				if rand.Int()%8 == 0 {
					start += "/32"
				}
				ips = append(ips, start)
				if i != 0 && ints[i] == ints[i-1]+1 {
					mergedInts[len(mergedInts)-1] = ints[i]
				} else {
					mergedInts = append(mergedInts, ints[i], ints[i])
				}
				expectedCount++
			}
		}

		for _, cidr := range cidrList {
			ips = append(ips, cidr.String())
		}

		mergedIPs := make([]string, len(mergedInts)/2)
		for i := range len(mergedInts) / 2 {
			if mergedInts[i*2] == mergedInts[i*2+1] {
				mergedIPs[i] = util.Uint32ToIPv4(mergedInts[i*2])
			} else {
				mergedIPs[i] = fmt.Sprintf("%s-%s", util.Uint32ToIPv4(mergedInts[i*2]), util.Uint32ToIPv4(mergedInts[i*2+1]))
			}
		}

		list, err := NewIPRangeListFrom(strset.New(ips...).List()...)
		require.NoError(t, err)
		require.Equal(t, len(mergedIPs), list.Len())
		require.Equal(t, strings.Join(mergedIPs, ","), list.String())

		count := list.Count()
		require.Equal(t, int64(expectedCount), count.Int64())

		for _, s := range mergedIPs {
			fields := strings.Split(s, "-")
			start, err := NewIP(fields[0])
			require.NoError(t, err)
			require.True(t, list.Contains(start))

			end := start
			if len(fields) != 1 {
				end, err = NewIP(fields[1])
				require.NoError(t, err)
				require.True(t, list.Contains(end))
			}

			if start.String() != "0.0.0.0" {
				require.False(t, list.Contains(start.Sub(1)))
			}
			if end.String() != "255.255.255.255" {
				require.False(t, list.Contains(end.Add(1)))
			}

			if !start.Equal(end) {
				require.True(t, list.Contains(start.Add(1)))
				require.True(t, list.Contains(end.Sub(1)))
			}
		}
	})
	t.Run("IPv6 NewIPRangeListFrom", func(t *testing.T) {
		list, err := NewIPRangeListFrom(
			"2001:db8::/126",
			"2001:db8::4",
			"2001:db8::6..2001:db8::8",
		)
		require.NoError(t, err)
		require.Equal(t, 2, list.Len())
		count := list.Count()
		require.Equal(t, int64(8), count.Int64())

		for _, address := range []string{
			"2001:db8::",
			"2001:db8::4",
			"2001:db8::6",
			"2001:db8::8",
		} {
			ip, err := NewIP(address)
			require.NoError(t, err)
			require.True(t, list.Contains(ip))
		}
		ip, err := NewIP("2001:db8::5")
		require.NoError(t, err)
		require.False(t, list.Contains(ip))
	})
}
