package ipam

import (
	"math/rand/v2"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestAdditionalIP(t *testing.T) {
	t.Run("IP", func(t *testing.T) {
		t.Run("IPv4", func(t *testing.T) {
			n1 := rand.Uint32()
			if n1 == 0xffffffff {
				n1--
			}
			n2 := n1 + 1

			ip1Str := util.Uint32ToIPv4(n1)
			ip2Str := util.Uint32ToIPv4(n2)

			ip1, err := NewIP(ip1Str)
			require.NoError(t, err)
			require.Equal(t, ip1Str, ip1.String())
			ip2, err := NewIP(ip2Str)
			require.NoError(t, err)
			require.Equal(t, ip2Str, ip2.String())

			require.False(t, ip1.Equal(ip2))
			require.False(t, ip1.GreaterThan(ip2))
			require.True(t, ip1.LessThan(ip2))

			require.Equal(t, ip2, ip1.Add(1))
			require.Equal(t, ip1, ip2.Add(-1))
			require.Equal(t, ip2, ip1.Sub(-1))
			require.Equal(t, ip1, ip2.Sub(1))
		})

		t.Run("IPv6", func(t *testing.T) {
			n1 := [4]uint32{rand.Uint32(), rand.Uint32(), rand.Uint32(), rand.Uint32()}
			if n1[0] == 0xffffffff && n1[1] == 0xffffffff && n1[2] == 0xffffffff && n1[3] == 0xffffffff {
				n1[3]--
			}
			n2 := [4]uint32{n1[0], n1[1], n1[2], n1[3] + 1}

			ip1Str := util.Uint32ToIPv6(n1)
			ip2Str := util.Uint32ToIPv6(n2)

			ip1, err := NewIP(ip1Str)
			require.NoError(t, err)
			ip2, err := NewIP(ip2Str)
			require.NoError(t, err)

			ip1Str = net.ParseIP(ip1Str).String()
			ip2Str = net.ParseIP(ip2Str).String()
			require.Equal(t, ip1Str, ip1.String())
			require.Equal(t, ip2Str, ip2.String())

			ip1, err = NewIP(ip1Str)
			require.NoError(t, err)
			require.Equal(t, net.ParseIP(ip1Str).String(), ip1.String())
			ip2, err = NewIP(ip2Str)
			require.NoError(t, err)
			require.Equal(t, net.ParseIP(ip2Str).String(), ip2.String())

			require.False(t, ip1.Equal(ip2))
			require.False(t, ip1.GreaterThan(ip2))
			require.True(t, ip1.LessThan(ip2))

			require.Equal(t, ip2, ip1.Add(1))
			require.Equal(t, ip1, ip2.Add(-1))
			require.Equal(t, ip2, ip1.Sub(-1))
			require.Equal(t, ip1, ip2.Sub(1))
		})
	})
}
