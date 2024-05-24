package ipam

import (
	"testing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/stretchr/testify/require"
)

func TestNewSubnetIPv4(t *testing.T) {
	excludeIps := []string{"10.0.0.2", "10.0.0.4", "10.0.0.100", "10.0.0.252", "10.0.0.253", "10.0.0.254"}
	subnet, err := NewSubnet("ipv4Subnet", "10.0.0.0/24", excludeIps)
	if err != nil {
		t.Errorf("failed to create IPv4 subnet: %v", err)
	}
	// check subnet values
	require.Equal(t, "ipv4Subnet", subnet.Name)
	require.Equal(t, kubeovnv1.ProtocolIPv4, subnet.Protocol)
	require.Equal(t, "10.0.0.0", subnet.V4CIDR.IP.String())
	require.Equal(t, "ffffff00", subnet.V4CIDR.Mask.String())
	require.Equal(t, "", subnet.V4Gw)
	require.Equal(t, "", subnet.V6Gw)
	require.Equal(t, 4, subnet.V4Free.Len())
	require.Equal(t, "10.0.0.1", subnet.V4Free.At(0).start.String())
	require.Equal(t, "10.0.0.1", subnet.V4Free.At(0).end.String())
	require.Equal(t, "10.0.0.3", subnet.V4Free.At(1).start.String())
	require.Equal(t, "10.0.0.3", subnet.V4Free.At(1).end.String())
	require.Equal(t, "10.0.0.5", subnet.V4Free.At(2).start.String())
	require.Equal(t, "10.0.0.99", subnet.V4Free.At(2).end.String())
	require.Equal(t, "10.0.0.101", subnet.V4Free.At(3).start.String())
	require.Equal(t, "10.0.0.251", subnet.V4Free.At(3).end.String())

	require.Equal(t, 4, subnet.V4Reserved.Len())
	require.Equal(t, "10.0.0.2", subnet.V4Reserved.At(0).start.String())
	require.Equal(t, "10.0.0.2", subnet.V4Reserved.At(0).end.String())
	require.Equal(t, "10.0.0.4", subnet.V4Reserved.At(1).start.String())
	require.Equal(t, "10.0.0.4", subnet.V4Reserved.At(1).end.String())
	require.Equal(t, "10.0.0.100", subnet.V4Reserved.At(2).start.String())
	require.Equal(t, "10.0.0.100", subnet.V4Reserved.At(2).end.String())
	require.Equal(t, "10.0.0.252", subnet.V4Reserved.At(3).start.String())
	require.Equal(t, "10.0.0.254", subnet.V4Reserved.At(3).end.String())

	// check V4Available
	require.Equal(t, 4, subnet.V4Available.Len())
	require.Equal(t, "10.0.0.1", subnet.V4Available.At(0).start.String())
	require.Equal(t, "10.0.0.1", subnet.V4Available.At(0).end.String())
	require.Equal(t, "10.0.0.3", subnet.V4Available.At(1).start.String())
	require.Equal(t, "10.0.0.3", subnet.V4Available.At(1).end.String())
	require.Equal(t, "10.0.0.5", subnet.V4Available.At(2).start.String())
	require.Equal(t, "10.0.0.99", subnet.V4Available.At(2).end.String())
	require.Equal(t, "10.0.0.101", subnet.V4Available.At(3).start.String())
	require.Equal(t, "10.0.0.251", subnet.V4Available.At(3).end.String())

	// check V4Using
	require.Equal(t, 0, subnet.V4Using.Len())

	// check V4NicToIP
	require.Equal(t, 0, len(subnet.V4NicToIP))

	// check V4IPToPod
	require.Equal(t, 0, len(subnet.V4IPToPod))

	// check subnet has no V6CIDR
	require.Nil(t, subnet.V6CIDR)

	// make sure subnet v6 fields length is 0
	// TODO:// v6 fields should be nil is better than empty list
	require.Equal(t, 0, subnet.V6Free.Len())
	require.Equal(t, 0, subnet.V6Reserved.Len())
	require.Equal(t, 0, subnet.V6Available.Len())
	require.Equal(t, 0, subnet.V6Using.Len())
	require.Equal(t, 0, len(subnet.V6NicToIP))
	require.Equal(t, 0, len(subnet.V6IPToPod))

	require.Equal(t, 0, len(subnet.PodToNicList))
	require.Equal(t, "", subnet.V4Gw)
	require.Equal(t, "", subnet.V6Gw)

	pool, ok := subnet.IPPools[""]
	// check pool V4IPs
	require.True(t, ok)
	require.Equal(t, 1, pool.V4IPs.Len())
	require.Equal(t, "10.0.0.1", pool.V4IPs.At(0).start.String())
	require.Equal(t, "10.0.0.254", pool.V4IPs.At(0).end.String())

	// check pool V4Free
	require.Equal(t, 4, pool.V4Free.Len())
	require.Equal(t, "10.0.0.1", pool.V4Free.At(0).start.String())
	require.Equal(t, "10.0.0.1", pool.V4Free.At(0).end.String())
	require.Equal(t, "10.0.0.3", pool.V4Free.At(1).start.String())
	require.Equal(t, "10.0.0.3", pool.V4Free.At(1).end.String())
	require.Equal(t, "10.0.0.5", pool.V4Free.At(2).start.String())
	require.Equal(t, "10.0.0.99", pool.V4Free.At(2).end.String())
	require.Equal(t, "10.0.0.101", pool.V4Free.At(3).start.String())
	require.Equal(t, "10.0.0.251", pool.V4Free.At(3).end.String())

	// check pool V4Available
	require.Equal(t, 4, pool.V4Available.Len())
	require.Equal(t, "10.0.0.1", pool.V4Available.At(0).start.String())
	require.Equal(t, "10.0.0.1", pool.V4Available.At(0).end.String())
	require.Equal(t, "10.0.0.3", pool.V4Available.At(1).start.String())
	require.Equal(t, "10.0.0.3", pool.V4Available.At(1).end.String())
	require.Equal(t, "10.0.0.5", pool.V4Available.At(2).start.String())
	require.Equal(t, "10.0.0.99", pool.V4Available.At(2).end.String())
	require.Equal(t, "10.0.0.101", pool.V4Available.At(3).start.String())
	require.Equal(t, "10.0.0.251", pool.V4Available.At(3).end.String())

	// check pool V4Reserved
	require.Equal(t, 4, pool.V4Reserved.Len())
	require.Equal(t, "10.0.0.2", pool.V4Reserved.At(0).start.String())
	require.Equal(t, "10.0.0.2", pool.V4Reserved.At(0).end.String())
	require.Equal(t, "10.0.0.4", pool.V4Reserved.At(1).start.String())
	require.Equal(t, "10.0.0.4", pool.V4Reserved.At(1).end.String())
	require.Equal(t, "10.0.0.100", pool.V4Reserved.At(2).start.String())
	require.Equal(t, "10.0.0.100", pool.V4Reserved.At(2).end.String())
	require.Equal(t, "10.0.0.252", pool.V4Reserved.At(3).start.String())
	require.Equal(t, "10.0.0.254", pool.V4Reserved.At(3).end.String())

	// check pool V4Released
	require.Equal(t, 0, pool.V4Released.Len())

	// check pool V4Using
	require.Equal(t, 0, pool.V4Using.Len())

	// make sure pool v6 fields length is 0
	// TODO:// v6 fields should be nil is better than empty list
	require.Equal(t, 0, pool.V6IPs.Len())
	require.Equal(t, 0, pool.V6Free.Len())
	require.Equal(t, 0, pool.V6Available.Len())
	require.Equal(t, 0, pool.V6Reserved.Len())
	require.Equal(t, 0, pool.V6Released.Len())
	require.Equal(t, 0, pool.V6Using.Len())
}

func TestNewSubnetIPv6(t *testing.T) {
	excludeIps := []string{"2001:db8::2", "2001:db8::4", "2001:db8::100", "2001:db8::252", "2001:db8::253", "2001:db8::254"}
	subnet, err := NewSubnet("ipv6Subnet", "2001:db8::/64", excludeIps)
	if err != nil {
		t.Errorf("failed to create IPv6 subnet: %v", err)
	}
	// check subnet values
	require.Equal(t, "ipv6Subnet", subnet.Name)
	require.Equal(t, kubeovnv1.ProtocolIPv6, subnet.Protocol)
	require.Equal(t, "2001:db8::", subnet.V6CIDR.IP.String())
	require.Equal(t, "ffffffffffffffff0000000000000000", subnet.V6CIDR.Mask.String())
	require.Equal(t, "", subnet.V4Gw)
	require.Equal(t, "", subnet.V6Gw)
	require.Equal(t, 5, subnet.V6Free.Len())
	require.Equal(t, "2001:db8::1", subnet.V6Free.At(0).start.String())
	require.Equal(t, "2001:db8::1", subnet.V6Free.At(0).end.String())
	require.Equal(t, "2001:db8::3", subnet.V6Free.At(1).start.String())
	require.Equal(t, "2001:db8::3", subnet.V6Free.At(1).end.String())
	require.Equal(t, "2001:db8::5", subnet.V6Free.At(2).start.String())
	require.Equal(t, "2001:db8::ff", subnet.V6Free.At(2).end.String())
	require.Equal(t, "2001:db8::101", subnet.V6Free.At(3).start.String())
	require.Equal(t, "2001:db8::251", subnet.V6Free.At(3).end.String())
	require.Equal(t, "2001:db8::255", subnet.V6Free.At(4).start.String())
	require.Equal(t, "2001:db8::ffff:ffff:ffff:fffe", subnet.V6Free.At(4).end.String())

	require.Equal(t, 4, subnet.V6Reserved.Len())
	require.Equal(t, "2001:db8::2", subnet.V6Reserved.At(0).start.String())
	require.Equal(t, "2001:db8::2", subnet.V6Reserved.At(0).end.String())
	require.Equal(t, "2001:db8::4", subnet.V6Reserved.At(1).start.String())
	require.Equal(t, "2001:db8::4", subnet.V6Reserved.At(1).end.String())
	require.Equal(t, "2001:db8::100", subnet.V6Reserved.At(2).start.String())
	require.Equal(t, "2001:db8::100", subnet.V6Reserved.At(2).end.String())
	require.Equal(t, "2001:db8::252", subnet.V6Reserved.At(3).start.String())
	require.Equal(t, "2001:db8::254", subnet.V6Reserved.At(3).end.String())

	// check V6Available
	require.Equal(t, 5, subnet.V6Available.Len())
	require.Equal(t, "2001:db8::1", subnet.V6Available.At(0).start.String())
	require.Equal(t, "2001:db8::1", subnet.V6Available.At(0).end.String())
	require.Equal(t, "2001:db8::3", subnet.V6Available.At(1).start.String())
	require.Equal(t, "2001:db8::3", subnet.V6Available.At(1).end.String())
	require.Equal(t, "2001:db8::5", subnet.V6Available.At(2).start.String())
	require.Equal(t, "2001:db8::ff", subnet.V6Available.At(2).end.String())
	require.Equal(t, "2001:db8::101", subnet.V6Available.At(3).start.String())
	require.Equal(t, "2001:db8::251", subnet.V6Available.At(3).end.String())
	require.Equal(t, "2001:db8::255", subnet.V6Available.At(4).start.String())
	require.Equal(t, "2001:db8::ffff:ffff:ffff:fffe", subnet.V6Available.At(4).end.String())

	// check V6Using
	require.Equal(t, 0, subnet.V6Using.Len())

	// check V6NicToIP
	require.Equal(t, 0, len(subnet.V6NicToIP))

	// check V6IPToPod
	require.Equal(t, 0, len(subnet.V6IPToPod))

	// check subnet has no V4CIDR
	require.Nil(t, subnet.V4CIDR)

	// make sure subnet v4 fields length is 0
	// TODO:// v4 fields should be nil is better than empty list
	require.Equal(t, 0, subnet.V4Free.Len())
	require.Equal(t, 0, subnet.V4Reserved.Len())
	require.Equal(t, 0, subnet.V4Available.Len())
	require.Equal(t, 0, subnet.V4Using.Len())
	require.Equal(t, 0, len(subnet.V4NicToIP))
	require.Equal(t, 0, len(subnet.V4IPToPod))

	require.Equal(t, 0, len(subnet.PodToNicList))
	require.Equal(t, "", subnet.V4Gw)
	require.Equal(t, "", subnet.V6Gw)

	pool, ok := subnet.IPPools[""]
	// check pool V6IPs
	require.True(t, ok)
	require.Equal(t, 1, pool.V6IPs.Len())
	require.Equal(t, "2001:db8::1", pool.V6IPs.At(0).start.String())
	require.Equal(t, "2001:db8::ffff:ffff:ffff:fffe", pool.V6IPs.At(0).end.String())

	// check pool V6Free
	require.Equal(t, 5, pool.V6Free.Len())
	require.Equal(t, "2001:db8::1", pool.V6Free.At(0).start.String())
	require.Equal(t, "2001:db8::1", pool.V6Free.At(0).end.String())
	require.Equal(t, "2001:db8::3", pool.V6Free.At(1).start.String())
	require.Equal(t, "2001:db8::3", pool.V6Free.At(1).end.String())
	require.Equal(t, "2001:db8::5", pool.V6Free.At(2).start.String())
	require.Equal(t, "2001:db8::ff", pool.V6Free.At(2).end.String())
	require.Equal(t, "2001:db8::101", pool.V6Free.At(3).start.String())
	require.Equal(t, "2001:db8::251", pool.V6Free.At(3).end.String())
	require.Equal(t, "2001:db8::255", pool.V6Free.At(4).start.String())
	require.Equal(t, "2001:db8::ffff:ffff:ffff:fffe", pool.V6Free.At(4).end.String())

	// check pool V6Available
	require.Equal(t, 5, pool.V6Available.Len())
	require.Equal(t, "2001:db8::1", pool.V6Available.At(0).start.String())
	require.Equal(t, "2001:db8::1", pool.V6Available.At(0).end.String())
	require.Equal(t, "2001:db8::3", pool.V6Available.At(1).start.String())
	require.Equal(t, "2001:db8::3", pool.V6Available.At(1).end.String())
	require.Equal(t, "2001:db8::5", pool.V6Available.At(2).start.String())
	require.Equal(t, "2001:db8::ff", pool.V6Available.At(2).end.String())
	require.Equal(t, "2001:db8::101", pool.V6Available.At(3).start.String())
	require.Equal(t, "2001:db8::251", pool.V6Available.At(3).end.String())
	require.Equal(t, "2001:db8::255", pool.V6Available.At(4).start.String())
	require.Equal(t, "2001:db8::ffff:ffff:ffff:fffe", pool.V6Available.At(4).end.String())

	// check pool V6Reserved
	require.Equal(t, 4, pool.V6Reserved.Len())
	require.Equal(t, "2001:db8::2", pool.V6Reserved.At(0).start.String())
	require.Equal(t, "2001:db8::2", pool.V6Reserved.At(0).end.String())
	require.Equal(t, "2001:db8::4", pool.V6Reserved.At(1).start.String())
	require.Equal(t, "2001:db8::4", pool.V6Reserved.At(1).end.String())
	require.Equal(t, "2001:db8::100", pool.V6Reserved.At(2).start.String())
	require.Equal(t, "2001:db8::100", pool.V6Reserved.At(2).end.String())
	require.Equal(t, "2001:db8::252", pool.V6Reserved.At(3).start.String())
	require.Equal(t, "2001:db8::254", pool.V6Reserved.At(3).end.String())

	// check pool V6Released
	require.Equal(t, 0, pool.V6Released.Len())

	// check pool V6Using
	require.Equal(t, 0, pool.V6Using.Len())

	// make sure pool v4 fields length is 0
	// TODO:// v4 fields should be nil is better than empty list
	require.Equal(t, 0, pool.V4IPs.Len())
	require.Equal(t, 0, pool.V4Free.Len())
	require.Equal(t, 0, pool.V4Available.Len())
	require.Equal(t, 0, pool.V4Reserved.Len())
	require.Equal(t, 0, pool.V4Released.Len())
	require.Equal(t, 0, pool.V4Using.Len())
}

func TestNewSubnetDualStack(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnet, err := NewSubnet("dualSubnet", "10.0.0.0/24,2001:db8::/64", excludeIps)
	if err != nil {
		t.Errorf("failed to create dual stack subnet: %v", err)
	}
	// check subnet values
	require.Equal(t, "dualSubnet", subnet.Name)
	require.Equal(t, kubeovnv1.ProtocolDual, subnet.Protocol)
	require.Equal(t, "10.0.0.0", subnet.V4CIDR.IP.String())
	require.Equal(t, "2001:db8::", subnet.V6CIDR.IP.String())
	require.Equal(t, "ffffff00", subnet.V4CIDR.Mask.String())
	require.Equal(t, "ffffffffffffffff0000000000000000", subnet.V6CIDR.Mask.String())
	require.Equal(t, "", subnet.V4Gw)
	require.Equal(t, "", subnet.V6Gw)
	require.Equal(t, 4, subnet.V4Free.Len())
	require.Equal(t, 5, subnet.V6Free.Len())

	require.Equal(t, "10.0.0.1", subnet.V4Free.At(0).start.String())
	require.Equal(t, "10.0.0.1", subnet.V4Free.At(0).end.String())
	require.Equal(t, "10.0.0.3", subnet.V4Free.At(1).start.String())
	require.Equal(t, "10.0.0.3", subnet.V4Free.At(1).end.String())
	require.Equal(t, "10.0.0.5", subnet.V4Free.At(2).start.String())
	require.Equal(t, "10.0.0.99", subnet.V4Free.At(2).end.String())
	require.Equal(t, "10.0.0.101", subnet.V4Free.At(3).start.String())
	require.Equal(t, "10.0.0.251", subnet.V4Free.At(3).end.String())

	require.Equal(t, "2001:db8::1", subnet.V6Free.At(0).start.String())
	require.Equal(t, "2001:db8::1", subnet.V6Free.At(0).end.String())
	require.Equal(t, "2001:db8::3", subnet.V6Free.At(1).start.String())
	require.Equal(t, "2001:db8::3", subnet.V6Free.At(1).end.String())
	require.Equal(t, "2001:db8::5", subnet.V6Free.At(2).start.String())
	require.Equal(t, "2001:db8::ff", subnet.V6Free.At(2).end.String())
	require.Equal(t, "2001:db8::101", subnet.V6Free.At(3).start.String())
	require.Equal(t, "2001:db8::251", subnet.V6Free.At(3).end.String())
	require.Equal(t, "2001:db8::255", subnet.V6Free.At(4).start.String())
	require.Equal(t, "2001:db8::ffff:ffff:ffff:fffe", subnet.V6Free.At(4).end.String())

	require.Equal(t, 4, subnet.V4Reserved.Len())
	require.Equal(t, "10.0.0.2", subnet.V4Reserved.At(0).start.String())
	require.Equal(t, "10.0.0.2", subnet.V4Reserved.At(0).end.String())
	require.Equal(t, "10.0.0.4", subnet.V4Reserved.At(1).start.String())
	require.Equal(t, "10.0.0.4", subnet.V4Reserved.At(1).end.String())
	require.Equal(t, "10.0.0.100", subnet.V4Reserved.At(2).start.String())
	require.Equal(t, "10.0.0.100", subnet.V4Reserved.At(2).end.String())
	require.Equal(t, "10.0.0.252", subnet.V4Reserved.At(3).start.String())
	require.Equal(t, "10.0.0.254", subnet.V4Reserved.At(3).end.String())

	require.Equal(t, 4, subnet.V6Reserved.Len())
	require.Equal(t, "2001:db8::2", subnet.V6Reserved.At(0).start.String())
	require.Equal(t, "2001:db8::2", subnet.V6Reserved.At(0).end.String())
	require.Equal(t, "2001:db8::4", subnet.V6Reserved.At(1).start.String())
	require.Equal(t, "2001:db8::4", subnet.V6Reserved.At(1).end.String())
	require.Equal(t, "2001:db8::100", subnet.V6Reserved.At(2).start.String())
	require.Equal(t, "2001:db8::100", subnet.V6Reserved.At(2).end.String())
	require.Equal(t, "2001:db8::252", subnet.V6Reserved.At(3).start.String())
	require.Equal(t, "2001:db8::254", subnet.V6Reserved.At(3).end.String())

	// check V4Available
	require.Equal(t, 4, subnet.V4Available.Len())
	require.Equal(t, "10.0.0.1", subnet.V4Available.At(0).start.String())
	require.Equal(t, "10.0.0.1", subnet.V4Available.At(0).end.String())
	require.Equal(t, "10.0.0.3", subnet.V4Available.At(1).start.String())
	require.Equal(t, "10.0.0.3", subnet.V4Available.At(1).end.String())
	require.Equal(t, "10.0.0.5", subnet.V4Available.At(2).start.String())
	require.Equal(t, "10.0.0.99", subnet.V4Available.At(2).end.String())
	require.Equal(t, "10.0.0.101", subnet.V4Available.At(3).start.String())
	require.Equal(t, "10.0.0.251", subnet.V4Available.At(3).end.String())

	// check V6Available
	require.Equal(t, 5, subnet.V6Available.Len())
	require.Equal(t, "2001:db8::1", subnet.V6Available.At(0).start.String())
	require.Equal(t, "2001:db8::1", subnet.V6Available.At(0).end.String())
	require.Equal(t, "2001:db8::3", subnet.V6Available.At(1).start.String())
	require.Equal(t, "2001:db8::3", subnet.V6Available.At(1).end.String())
	require.Equal(t, "2001:db8::5", subnet.V6Available.At(2).start.String())
	require.Equal(t, "2001:db8::ff", subnet.V6Available.At(2).end.String())
	require.Equal(t, "2001:db8::101", subnet.V6Available.At(3).start.String())
	require.Equal(t, "2001:db8::251", subnet.V6Available.At(3).end.String())
	require.Equal(t, "2001:db8::255", subnet.V6Available.At(4).start.String())
	require.Equal(t, "2001:db8::ffff:ffff:ffff:fffe", subnet.V6Available.At(4).end.String())

	// check V4Using
	require.Equal(t, 0, subnet.V4Using.Len())

	// check V4NicToIP
	require.Equal(t, 0, len(subnet.V4NicToIP))

	// check V4IPToPod
	require.Equal(t, 0, len(subnet.V4IPToPod))

	// check V6Using
	require.Equal(t, 0, subnet.V6Using.Len())

	// check V6NicToIP
	require.Equal(t, 0, len(subnet.V6NicToIP))

	// check V6IPToPod
	require.Equal(t, 0, len(subnet.V6IPToPod))

	require.Equal(t, 0, len(subnet.PodToNicList))
	require.Equal(t, "", subnet.V4Gw)
	require.Equal(t, "", subnet.V6Gw)

	v4Pool, ok := subnet.IPPools[""]
	// check pool V4IPs
	require.True(t, ok)
	require.Equal(t, 1, v4Pool.V4IPs.Len())
	require.Equal(t, "10.0.0.1", v4Pool.V4IPs.At(0).start.String())
	require.Equal(t, "10.0.0.254", v4Pool.V4IPs.At(0).end.String())

	// check pool V4Free
	require.Equal(t, 4, v4Pool.V4Free.Len())
	require.Equal(t, "10.0.0.1", v4Pool.V4Free.At(0).start.String())
	require.Equal(t, "10.0.0.1", v4Pool.V4Free.At(0).end.String())
	require.Equal(t, "10.0.0.3", v4Pool.V4Free.At(1).start.String())
	require.Equal(t, "10.0.0.3", v4Pool.V4Free.At(1).end.String())
	require.Equal(t, "10.0.0.5", v4Pool.V4Free.At(2).start.String())
	require.Equal(t, "10.0.0.99", v4Pool.V4Free.At(2).end.String())
	require.Equal(t, "10.0.0.101", v4Pool.V4Free.At(3).start.String())
	require.Equal(t, "10.0.0.251", v4Pool.V4Free.At(3).end.String())

	// check pool V4Available
	require.Equal(t, 4, v4Pool.V4Available.Len())
	require.Equal(t, "10.0.0.1", v4Pool.V4Available.At(0).start.String())
	require.Equal(t, "10.0.0.1", v4Pool.V4Available.At(0).end.String())
	require.Equal(t, "10.0.0.3", v4Pool.V4Available.At(1).start.String())
	require.Equal(t, "10.0.0.3", v4Pool.V4Available.At(1).end.String())
	require.Equal(t, "10.0.0.5", v4Pool.V4Available.At(2).start.String())
	require.Equal(t, "10.0.0.99", v4Pool.V4Available.At(2).end.String())
	require.Equal(t, "10.0.0.101", v4Pool.V4Available.At(3).start.String())
	require.Equal(t, "10.0.0.251", v4Pool.V4Available.At(3).end.String())

	// check pool V4Reserved
	require.Equal(t, 4, v4Pool.V4Reserved.Len())
	require.Equal(t, "10.0.0.2", v4Pool.V4Reserved.At(0).start.String())
	require.Equal(t, "10.0.0.2", v4Pool.V4Reserved.At(0).end.String())
	require.Equal(t, "10.0.0.4", v4Pool.V4Reserved.At(1).start.String())
	require.Equal(t, "10.0.0.4", v4Pool.V4Reserved.At(1).end.String())
	require.Equal(t, "10.0.0.100", v4Pool.V4Reserved.At(2).start.String())
	require.Equal(t, "10.0.0.100", v4Pool.V4Reserved.At(2).end.String())
	require.Equal(t, "10.0.0.252", v4Pool.V4Reserved.At(3).start.String())
	require.Equal(t, "10.0.0.254", v4Pool.V4Reserved.At(3).end.String())

	// check pool V4Released
	require.Equal(t, 0, v4Pool.V4Released.Len())

	// check pool V4Using
	require.Equal(t, 0, v4Pool.V4Using.Len())

	v6Pool, ok := subnet.IPPools[""]
	// check pool V6IPs
	require.True(t, ok)
	require.Equal(t, 1, v6Pool.V6IPs.Len())
	require.Equal(t, "2001:db8::1", v6Pool.V6IPs.At(0).start.String())
	require.Equal(t, "2001:db8::ffff:ffff:ffff:fffe", v6Pool.V6IPs.At(0).end.String())

	// check pool V6Free
	require.Equal(t, 5, v6Pool.V6Free.Len())
	require.Equal(t, "2001:db8::1", v6Pool.V6Free.At(0).start.String())
	require.Equal(t, "2001:db8::1", v6Pool.V6Free.At(0).end.String())
	require.Equal(t, "2001:db8::3", v6Pool.V6Free.At(1).start.String())
	require.Equal(t, "2001:db8::3", v6Pool.V6Free.At(1).end.String())
	require.Equal(t, "2001:db8::5", v6Pool.V6Free.At(2).start.String())
	require.Equal(t, "2001:db8::ff", v6Pool.V6Free.At(2).end.String())
	require.Equal(t, "2001:db8::101", v6Pool.V6Free.At(3).start.String())
	require.Equal(t, "2001:db8::251", v6Pool.V6Free.At(3).end.String())
	require.Equal(t, "2001:db8::255", v6Pool.V6Free.At(4).start.String())
	require.Equal(t, "2001:db8::ffff:ffff:ffff:fffe", v6Pool.V6Free.At(4).end.String())

	// check pool V6Available
	require.Equal(t, 5, v6Pool.V6Available.Len())
	require.Equal(t, "2001:db8::1", v6Pool.V6Available.At(0).start.String())
	require.Equal(t, "2001:db8::1", v6Pool.V6Available.At(0).end.String())
	require.Equal(t, "2001:db8::3", v6Pool.V6Available.At(1).start.String())
	require.Equal(t, "2001:db8::3", v6Pool.V6Available.At(1).end.String())
	require.Equal(t, "2001:db8::5", v6Pool.V6Available.At(2).start.String())
	require.Equal(t, "2001:db8::ff", v6Pool.V6Available.At(2).end.String())
	require.Equal(t, "2001:db8::101", v6Pool.V6Available.At(3).start.String())
	require.Equal(t, "2001:db8::251", v6Pool.V6Available.At(3).end.String())
	require.Equal(t, "2001:db8::255", v6Pool.V6Available.At(4).start.String())
	require.Equal(t, "2001:db8::ffff:ffff:ffff:fffe", v6Pool.V6Available.At(4).end.String())

	// check pool V6Reserved
	require.Equal(t, 4, v6Pool.V6Reserved.Len())
	require.Equal(t, "2001:db8::2", v6Pool.V6Reserved.At(0).start.String())
	require.Equal(t, "2001:db8::2", v6Pool.V6Reserved.At(0).end.String())
	require.Equal(t, "2001:db8::4", v6Pool.V6Reserved.At(1).start.String())
	require.Equal(t, "2001:db8::4", v6Pool.V6Reserved.At(1).end.String())
	require.Equal(t, "2001:db8::100", v6Pool.V6Reserved.At(2).start.String())
	require.Equal(t, "2001:db8::100", v6Pool.V6Reserved.At(2).end.String())
	require.Equal(t, "2001:db8::252", v6Pool.V6Reserved.At(3).start.String())
	require.Equal(t, "2001:db8::254", v6Pool.V6Reserved.At(3).end.String())

	// check pool V6Released
	require.Equal(t, 0, v6Pool.V6Released.Len())

	// check pool V6Using
	require.Equal(t, 0, v6Pool.V6Using.Len())
}

func TestGetStaticMac(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnet, err := NewSubnet("dualSubnet", "10.0.0.0/24,2001:db8::/64", excludeIps)
	if err != nil {
		t.Errorf("failed to create dual stack subnet: %v", err)
	}
	podName := "pod1"
	nicName := "eth0"
	mac := "00:11:22:33:44:55"
	err = subnet.GetStaticMac(podName, nicName, mac, true)
	require.Nil(t, err)
	require.Equal(t, podName, subnet.MacToPod[mac])
	require.Equal(t, mac, subnet.NicToMac[nicName])
	podName = "pod2"
	err = subnet.GetStaticMac(podName, nicName, mac, true)
	require.NotNil(t, err)
}

func TestGetRandomAddress(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnet, err := NewSubnet("dualSubnet", "10.0.0.0/24,2001:db8::/64", excludeIps)
	if err != nil {
		t.Errorf("failed to create dual stack subnet: %v", err)
	}
	podName := "pod1"
	nicName := "pod1.default"
	mac := "00:11:22:33:44:55"
	poolName := ""
	shouldGetV4IP := "10.0.0.3"
	shouldGetV6IP := "2001:db8::3"
	skippedAddrs := []string{"10.0.0.1", "10.0.0.5", "2001:db8::1", "2001:db8::5"}
	v4IP, v6IP, macStr, err := subnet.GetRandomAddress(poolName, podName, nicName, &mac, skippedAddrs, true)
	require.Nil(t, err)
	require.Equal(t, shouldGetV4IP, v4IP.String())
	require.Equal(t, shouldGetV6IP, v6IP.String())
	require.Equal(t, mac, macStr)
	require.Equal(t, podName, subnet.MacToPod[mac])
	require.Equal(t, mac, subnet.NicToMac[nicName])
	require.Equal(t, shouldGetV4IP, subnet.V4NicToIP[nicName].String())
	require.Equal(t, shouldGetV6IP, subnet.V6NicToIP[nicName].String())
	nicList := subnet.PodToNicList[podName]
	require.Equal(t, 1, len(nicList))
	require.Contains(t, nicList, nicName)
	subnet.popPodNic(podName, nicName)
	require.Equal(t, 0, len(subnet.PodToNicList[podName]))
}

func TestGetStaticAddress(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnet, err := NewSubnet("dualSubnet1", "10.0.0.0/24,2001:db8::/64", excludeIps)
	if err != nil {
		t.Errorf("failed to create dual stack subnet: %v", err)
	}
	// 1. po1 has v4 ip but no mac
	podName := "pod1"
	nicName := "pod1.default"
	var mac *string
	v4 := "10.0.0.3"
	v4IP, err := NewIP(v4)
	require.Nil(t, err)
	ip, macStr, err := subnet.GetStaticAddress(podName, nicName, v4IP, mac, false, true)
	require.Nil(t, err)
	require.Equal(t, v4, ip.String())
	require.NotEqual(t, "", macStr)

	// 2. pod has v6 ip but no mac
	podName = "pod2"
	nicName = "pod2.default"
	v6 := "2001:db8::3"
	v6IP, err := NewIP(v6)
	require.Nil(t, err)
	ip, macStr, err = subnet.GetStaticAddress(podName, nicName, v6IP, mac, false, true)
	require.Nil(t, err)
	require.Equal(t, v6, ip.String())
	require.NotEqual(t, "", macStr)

	// 3. pod has v4 ip and mac
	podName = "pod3"
	nicName = "pod3.default"
	v4 = "10.0.0.33"
	macIn := "00:11:22:33:44:55"
	v4IP, err = NewIP(v4)
	require.Nil(t, err)
	ip, macOut, err := subnet.GetStaticAddress(podName, nicName, v4IP, &macIn, false, true)
	require.Nil(t, err)
	require.Equal(t, v4, ip.String())
	require.Equal(t, macIn, macOut)

	// 4. pod has v6 ip and mac
	podName = "pod4"
	nicName = "pod4.default"
	v6 = "2001:db8::33"
	macIn = "00:11:22:33:44:56"
	v6IP, err = NewIP(v6)
	require.Nil(t, err)
	ip, macOut, err = subnet.GetStaticAddress(podName, nicName, v6IP, &macIn, false, true)
	require.Nil(t, err)
	require.Equal(t, v6, ip.String())
	require.Equal(t, macIn, macOut)

	// 5. only has mac
	podName = "pod5"
	nicName = "pod5.default"
	macIn = "00:11:22:33:44:57"
	ip, macOut, err = subnet.GetStaticAddress(podName, nicName, nil, &macIn, false, true)
	require.NotNil(nil, err)
	require.Nil(t, ip)
	require.Equal(t, "", macOut)
}

// func TestReleaseAddress(t *testing.T) {
// 	subnet := &Subnet{}
// 	podName := "pod1"
// 	subnet.ReleaseAddress(podName)
// }

// func TestContainAddress(t *testing.T) {
// 	subnet := &Subnet{}
// 	address := net.ParseIP("10.0.0.1")
// 	if !subnet.ContainAddress(address) {
// 		t.Errorf("Address not found in subnet")
// 	}
// }

// func TestGetPodAddress(t *testing.T) {
// 	subnet := &Subnet{}
// 	nicName := "nic1"
// 	_, _, _, _ = subnet.GetPodAddress("pod1", nicName)
// }

// func TestIsIPAssignedToOtherPod(t *testing.T) {
// 	subnet := &Subnet{}
// 	ip := "10.0.0.1"
// 	podName := "pod1"
// 	_, assigned := subnet.isIPAssignedToOtherPod(ip, podName)
// 	if assigned {
// 		t.Errorf("IP assigned to other pod")
// 	}
// }

// func TestAddOrUpdateIPPool(t *testing.T) {
// 	subnet := &Subnet{}
// 	poolName := "pool1"
// 	ips := []string{"10.0.0.1", "10.0.0.2"}
// 	err := subnet.AddOrUpdateIPPool(poolName, ips)
// 	if err != nil {
// 		t.Errorf("failed to add or update IP pool: %v", err)
// 	}
// }

// func TestRemoveIPPool(t *testing.T) {
// 	subnet := &Subnet{}
// 	poolName := "pool1"
// 	subnet.RemoveIPPool(poolName)
// }

// func TestIPPoolStatistics(t *testing.T) {
// 	subnet := &Subnet{}
// 	_, _, _, _, _, _, _, _ = subnet.IPPoolStatistics("pool1")
// }
