package ipam

import (
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestAdditionalIPRange(t *testing.T) {
	t.Run("IPRange", func(t *testing.T) {
		t.Run("IPv4", func(t *testing.T) {
			n1, n2 := rand.Uint32(), rand.Uint32()
			if n1 > n2 {
				n1, n2 = n2, n1
			}
			n := n1 + uint32(rand.Int64N(int64(n2)-int64(n1)+1))
			startStr := util.Uint32ToIPv4(n1)
			endStr := util.Uint32ToIPv4(n2)
			ipStr := util.Uint32ToIPv4(n)

			start, err := NewIP(startStr)
			require.NoError(t, err)
			end, err := NewIP(endStr)
			require.NoError(t, err)
			ip, err := NewIP(ipStr)
			require.NoError(t, err)

			r := NewIPRange(start, end)
			if n1 == n2 {
				require.Equal(t, start.String(), r.String())
			} else {
				require.Equal(t, fmt.Sprintf("%s-%s", start.String(), end.String()), r.String())
			}

			c := r.Count()
			require.Equal(t, 0, c.Int.Cmp(big.NewInt(int64(n2)-int64(n1)+1)))

			fullStart, err := NewIP("0.0.0.0")
			require.NoError(t, err)
			fullEnd, err := NewIP("255.255.255.255")
			require.NoError(t, err)
			fullRange := NewIPRange(fullStart, fullEnd)
			fullCount := fullRange.Count()
			require.Equal(t, 0, fullCount.Int.Cmp(big.NewInt(1<<32)))
			require.Equal(t, r.String(), r.Clone().String())
			require.True(t, r.Contains(start))
			require.True(t, r.Contains(end))
			require.True(t, r.Contains(ip))
			if n1 != 0 {
				require.False(t, r.Contains(start.Sub(1)))
			}
			if n2 != 0xffffffff {
				require.False(t, r.Contains(end.Add(1)))
			}
			if n1 != n2 {
				require.True(t, r.Contains(start.Add(1)))
				require.True(t, r.Contains(end.Sub(1)))
			}

			for i := uint32(0); i < 100 && i <= n2-n1; i++ {
				require.True(t, r.Contains(r.Random()))
			}

			prefix := rand.IntN(net.IPv4len*8 + 1)
			_, cidr, err := net.ParseCIDR(fmt.Sprintf("%s/%d", startStr, prefix))
			require.NoError(t, err)
			require.NotNil(t, cidr)

			r = NewIPRangeFromCIDR(*cidr)
			require.True(t, r.Contains(start))

			start, err = NewIP(cidr.IP.String())
			require.NoError(t, err)
			require.True(t, r.Contains(start))
			if !cidr.IP.Equal(net.IPv4zero) {
				require.False(t, r.Contains(start.Sub(1)))
			}
			if prefix != net.IPv4len*8 {
				require.True(t, r.Contains(start.Add(1)))
			}

			c = r.Count()
			require.Equal(t, 0, c.Int.Cmp(big.NewInt(int64(math.Exp2(float64(net.IPv4len*8-prefix))))))
			for i := int64(0); i < 100 && i <= c.Int64(); i++ {
				require.True(t, r.Contains(r.Random()))
			}
		})

		t.Run("IPv6", func(t *testing.T) {
			// Keep the endpoints ordered as 128-bit addresses. Generating each
			// word independently can produce an address outside the range when a
			// later word is smaller than the corresponding start word.
			n1 := [4]uint32{0x20010db8, 0, 0, 1}
			n2 := [4]uint32{0x20010db8, 0, 1, 0}
			n := [4]uint32{0x20010db8, 0, 0, 2}

			startStr := util.Uint32ToIPv6(n1)
			endStr := util.Uint32ToIPv6(n2)
			ipStr := util.Uint32ToIPv6(n)

			start, err := NewIP(startStr)
			require.NoError(t, err)
			end, err := NewIP(endStr)
			require.NoError(t, err)
			ip, err := NewIP(ipStr)
			require.NoError(t, err)

			r := NewIPRange(start, end)
			if n1 == n2 {
				require.Equal(t, start.String(), r.String())
			} else {
				require.Equal(t, fmt.Sprintf("%s-%s", start.String(), end.String()), r.String())
			}

			count := r.Count()
			expectedCount := big.NewInt(0).Sub(big.NewInt(0).SetBytes(net.ParseIP(endStr).To16()), big.NewInt(0).SetBytes(net.ParseIP(startStr).To16()))
			expectedCount.Add(expectedCount, big.NewInt(1))

			require.Equal(t, 0, count.Int.Cmp(expectedCount))
			require.Equal(t, r.String(), r.Clone().String())
			require.True(t, r.Contains(start))
			require.True(t, r.Contains(end))
			require.True(t, r.Contains(ip))
			if n1 != [4]uint32{0, 0, 0, 0} {
				require.False(t, r.Contains(start.Sub(1)))
			}
			if n2 != [4]uint32{0xffffffff, 0xffffffff, 0xffffffff, 0xffffffff} {
				require.False(t, r.Contains(end.Add(1)))
			}
			if n1 != n2 {
				require.True(t, r.Contains(start.Add(1)))
				require.True(t, r.Contains(end.Sub(1)))
			}

			for range 100 {
				require.True(t, r.Contains(r.Random()))
			}

			prefix := rand.IntN(net.IPv6len*8 + 1)
			_, cidr, err := net.ParseCIDR(fmt.Sprintf("%s/%d", startStr, prefix))
			require.NoError(t, err)
			require.NotNil(t, cidr)

			r = NewIPRangeFromCIDR(*cidr)
			require.True(t, r.Contains(start))

			start, err = NewIP(cidr.IP.String())
			require.NoError(t, err)
			require.True(t, r.Contains(start))
			if !cidr.IP.Equal(net.IPv6zero) {
				require.False(t, r.Contains(start.Sub(1)))
			}
			if prefix != net.IPv6len*8 {
				require.True(t, r.Contains(start.Add(1)))
			}

			for range 100 {
				require.True(t, r.Contains(r.Random()))
			}

			count = r.Count()
			expectedCount = big.NewInt(0).Exp(big.NewInt(2), big.NewInt(int64(net.IPv6len*8-prefix)), nil)
			require.Equal(t, 0, count.Int.Cmp(expectedCount))
		})
	})
}
