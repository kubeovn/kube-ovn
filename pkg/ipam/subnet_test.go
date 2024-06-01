package ipam

import (
	"testing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/stretchr/testify/require"
)

func TestNewSubnetIPv4(t *testing.T) {
	excludeIps := []string{"10.0.0.2", "10.0.0.4", "10.0.0.100", "10.0.0.252", "10.0.0.253", "10.0.0.254"}
	subnet, err := NewSubnet("v4Subnet", "10.0.0.0/24", excludeIps)
	if err != nil {
		t.Errorf("failed to create IPv4 subnet: %v", err)
	}
	// check subnet values
	require.Equal(t, "v4Subnet", subnet.Name)
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
	subnet, err := NewSubnet("v6Subnet", "2001:db8::/64", excludeIps)
	if err != nil {
		t.Errorf("failed to create IPv6 subnet: %v", err)
	}
	// check subnet values
	require.Equal(t, "v6Subnet", subnet.Name)
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

func TestGetV4StaticAddress(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
	}
	subnet, err := NewSubnet("v4Subnet", "10.0.0.0/24", excludeIps)
	if err != nil {
		t.Errorf("failed to create v4 stack subnet: %v", err)
	}
	// 1. pod1 has v4 ip but no mac, should get specified ip and mac
	podName := "pod1"
	nicName := "pod1.default"
	var mac *string
	v4 := "10.0.0.3"
	v4IP, err := NewIP(v4)
	require.Nil(t, err)
	ip1, macStr1, err := subnet.GetStaticAddress(podName, nicName, v4IP, mac, false, true)
	require.Nil(t, err)
	require.Equal(t, v4, ip1.String())
	require.NotEqual(t, "", macStr1)
	v4IP, v6IP, m, protocol := subnet.GetPodAddress(nicName)
	require.Equal(t, v4, v4IP.String())
	require.Nil(t, v6IP)
	require.Equal(t, macStr1, m)
	require.Equal(t, kubeovnv1.ProtocolIPv4, protocol)

	// 3. pod2 has v4 ip and mac, should get specified ip and mac
	podName = "pod2"
	nicName = "pod2.default"
	v4 = "10.0.0.22"
	macIn := "00:11:22:33:44:55"
	v4IP, err = NewIP(v4)
	require.Nil(t, err)
	ip2, macOut2, err := subnet.GetStaticAddress(podName, nicName, v4IP, &macIn, false, true)
	require.Nil(t, err)
	require.Equal(t, v4, ip2.String())
	require.Equal(t, macIn, macOut2)
	v4IP, v6IP, m, protocol = subnet.GetPodAddress(nicName)
	require.Equal(t, v4, v4IP.String())
	require.Nil(t, v6IP)
	require.Equal(t, macOut2, m)
	require.Equal(t, kubeovnv1.ProtocolIPv4, protocol)

	// compare mac
	require.NotEqual(t, macStr1, macOut2)

	// 3. pod3 only has mac, should get no ip and no mac
	podName = "pod3"
	nicName = "pod3.default"
	macIn = "00:11:22:33:44:57"
	ip, macOut, err := subnet.GetStaticAddress(podName, nicName, nil, &macIn, false, true)
	require.NotNil(nil, err)
	require.Nil(t, ip)
	require.Equal(t, "", macOut)

	// 4. pod4 has the same mac with pod2 should get error
	podName = "pod4"
	nicName = "pod4.default"
	v4 = "10.0.0.23"
	macIn = "00:11:22:33:44:55"
	v4IP, err = NewIP(v4)
	require.Nil(t, err)
	ip, macOut, err = subnet.GetStaticAddress(podName, nicName, v4IP, &macIn, false, true)
	require.NotNil(nil, err)
	require.Equal(t, "", macOut)
	require.Nil(nil, ip)

	// 5. ip is assigned to pod1, should get error
	podName = "pod5"
	v4 = "10.0.0.3"
	usingPod, using := subnet.isIPAssignedToOtherPod(v4, podName)
	require.True(t, using)
	require.Equal(t, "pod1", usingPod)
}

func TestGetV6StaticAddress(t *testing.T) {
	excludeIps := []string{
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnet, err := NewSubnet("v6Subnet", "2001:db8::/64", excludeIps)
	if err != nil {
		t.Errorf("failed to create v6 stack subnet: %v", err)
	}
	// 1. pod1 has v6 ip but no mac, should get specified ip and mac
	podName := "pod1"
	nicName := "pod1.default"
	v6 := "2001:db8::3"
	v6IP, err := NewIP(v6)
	require.Nil(t, err)
	ip1, macStr1, err := subnet.GetStaticAddress(podName, nicName, v6IP, nil, false, true)
	require.Nil(t, err)
	require.Equal(t, v6, ip1.String())
	require.NotEqual(t, "", macStr1)
	v4IP, v6IP, m, protocol := subnet.GetPodAddress(nicName)
	require.Nil(t, v4IP)
	require.Equal(t, v6, v6IP.String())
	require.Equal(t, macStr1, m)
	require.Equal(t, kubeovnv1.ProtocolIPv6, protocol)

	// 2. pod2 has v6 ip and mac should get specified ip and mac
	podName = "pod2"
	nicName = "pod2.default"
	v6 = "2001:db8::4"
	macIn := "00:11:22:33:44:56"
	v6IP, err = NewIP(v6)
	require.Nil(t, err)
	ip2, macOut2, err := subnet.GetStaticAddress(podName, nicName, v6IP, &macIn, false, true)
	require.Nil(t, err)
	require.Equal(t, v6, ip2.String())
	require.Equal(t, macIn, macOut2)
	v4IP, v6IP, m, protocol = subnet.GetPodAddress(nicName)
	require.Nil(t, v4IP)
	require.Equal(t, v6, v6IP.String())
	require.Equal(t, macOut2, m)
	require.Equal(t, kubeovnv1.ProtocolIPv6, protocol)

	// compare mac
	require.NotEqual(t, macStr1, macOut2)

	// 3. pod3 only has mac should get no ip and no mac
	podName = "pod3"
	nicName = "pod3.default"
	macIn = "00:11:22:33:44:57"
	ip, macOut, err := subnet.GetStaticAddress(podName, nicName, nil, &macIn, false, true)
	require.NotNil(nil, err)
	require.Nil(t, ip)
	require.Equal(t, "", macOut)

	// 4. pod4 has the same mac with pod2 should get error
	podName = "pod4"
	nicName = "pod4.default"
	v6 = "2001:db8::5"
	macIn = "00:11:22:33:44:56"
	v6IP, err = NewIP(v6)
	require.Nil(t, err)
	ip, macOut, err = subnet.GetStaticAddress(podName, nicName, v6IP, &macIn, false, true)
	require.NotNil(nil, err)
	require.Equal(t, "", macOut)
	require.Nil(nil, ip)

	// 5. ip is assigned to pod1, should get error
	podName = "pod5"
	v6 = "2001:db8::3"
	usingPod, using := subnet.isIPAssignedToOtherPod(v6, podName)
	require.True(t, using)
	require.Equal(t, "pod1", usingPod)
}

func TestGetDualStaticAddress(t *testing.T) {
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
	// 1. pod1 has v4 and v6 ip but no mac, should get specified ip and mac
	podName := "pod1"
	nicName := "pod1.default"
	var mac *string
	v4 := "10.0.0.3"
	v6 := "2001:db8::3"
	v4IP, err := NewIP(v4)
	require.Nil(t, err)
	v4Ip1, macStr1, err := subnet.GetStaticAddress(podName, nicName, v4IP, mac, false, true)
	require.Nil(t, err)
	require.Equal(t, v4, v4Ip1.String())
	require.NotEqual(t, "", macStr1)
	v6IP, err := NewIP(v6)
	require.Nil(t, err)
	v6Ip1, macStr1, err := subnet.GetStaticAddress(podName, nicName, v6IP, mac, false, true)
	require.Nil(t, err)
	require.Equal(t, v6, v6Ip1.String())
	require.NotEqual(t, "", macStr1)
	v4Ip, v6Ip, m, protocol := subnet.GetPodAddress(nicName)
	require.Equal(t, v4, v4Ip.String())
	require.Equal(t, v6, v6Ip.String())
	require.Equal(t, macStr1, m)
	require.Equal(t, kubeovnv1.ProtocolDual, protocol)

	// 2. pod2 has v4 and v6 ip and mac should get specified mac
	podName = "pod2"
	nicName = "pod2.default"
	v4 = "10.0.0.22"
	v6 = "2001:db8::22"
	macIn := "00:11:22:33:44:55"
	v4IP, err = NewIP(v4)
	require.Nil(t, err)
	v4Ip2, macOut, err := subnet.GetStaticAddress(podName, nicName, v4IP, &macIn, false, true)
	require.Nil(t, err)
	require.Equal(t, v4, v4Ip2.String())
	require.Equal(t, macIn, macOut)
	v6IP, err = NewIP(v6)
	require.Nil(t, err)
	v6Ip2, macOut, err := subnet.GetStaticAddress(podName, nicName, v6IP, &macIn, false, true)
	require.Nil(t, err)
	require.Equal(t, v6, v6Ip2.String())
	require.Equal(t, macIn, macOut)
	v4Ip, v6Ip, m, protocol = subnet.GetPodAddress(nicName)
	require.Equal(t, v4, v4Ip.String())
	require.Equal(t, v6, v6Ip.String())
	require.Equal(t, macIn, m)
	require.Equal(t, kubeovnv1.ProtocolDual, protocol)

	// compare mac
	require.NotEqual(t, macStr1, macOut)

	// 3. pod3 only has mac should get no ip and no mac
	podName = "pod3"
	nicName = "pod3.default"
	macIn = "00:11:22:33:44:57"
	ip, macOut, err := subnet.GetStaticAddress(podName, nicName, nil, &macIn, false, true)
	require.NotNil(nil, err)
	require.Nil(t, ip)
	require.Equal(t, "", macOut)

	// 4. pod4 has the same mac with pod3 should get error
	podName = "pod4"
	nicName = "pod4.default"
	v6 = "2001:db8::66"
	macIn = "00:11:22:33:44:55"
	v6IP, err = NewIP(v6)
	require.Nil(t, err)
	ip, macOut, err = subnet.GetStaticAddress(podName, nicName, v6IP, &macIn, false, true)
	require.NotNil(nil, err)
	require.Equal(t, "", macOut)
	require.Nil(nil, ip)

	// 5. ip is assigned to pod1, should get error
	podName = "pod5"
	v4 = "10.0.0.3"
	usingPod, using := subnet.isIPAssignedToOtherPod(v4, podName)
	require.True(t, using)
	require.Equal(t, "pod1", usingPod)
}

func TestGetGetV4RandomAddress(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
	}
	subnet, err := NewSubnet("randomAddressV4Subnet1", "10.0.0.0/24", excludeIps)
	if err != nil {
		t.Errorf("failed to create dual stack subnet: %v", err)
	}
	// 1. no mac, get v4 address for pod1
	podName := "pod1"
	nicName := "pod1.default"
	v4IP1, v6IP1, mac1, err := subnet.GetRandomAddress("", podName, nicName, nil, nil, false)
	require.Nil(t, err)
	require.NotEqual(t, "", v4IP1.String())
	require.Nil(t, v6IP1)
	require.NotEqual(t, "", mac1)
	// 2. has mac, get v4 address for pod2
	podName = "pod2"
	nicName = "pod2.default"
	staticMac2 := "00:11:22:33:44:55"
	v4IP2, v6IP2, mac2, err := subnet.GetRandomAddress("", podName, nicName, &staticMac2, nil, false)
	require.Nil(t, err)
	require.NotEqual(t, "", v4IP2.String())
	require.Nil(t, v6IP2)
	require.Equal(t, staticMac2, mac2)

	// compare
	require.NotEqual(t, v4IP1.String(), v4IP2.String())
	require.NotEqual(t, mac1, mac2)
}

func TestGetGetV6RandomAddress(t *testing.T) {
	excludeIps := []string{
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnet, err := NewSubnet("v6Subnet", "2001:db8::/64", excludeIps)
	if err != nil {
		t.Errorf("failed to create v6 stack subnet: %v", err)
	}
	// 1. no mac, get v6 address for pod1
	podName := "pod1"
	nicName := "pod1.default"
	v4IP1, v6IP1, mac1, err := subnet.GetRandomAddress("", podName, nicName, nil, nil, false)
	require.Nil(t, err)
	require.Nil(t, v4IP1)
	require.NotEqual(t, "", v6IP1.String())
	require.NotEqual(t, "", mac1)
	// 2. has mac, get v6 address for pod2
	podName = "pod2"
	nicName = "pod2.default"
	staticMac2 := "00:11:22:33:44:55"
	v4IP2, v6IP2, mac2, err := subnet.GetRandomAddress("", podName, nicName, &staticMac2, nil, false)
	require.Nil(t, err)
	require.Nil(t, v4IP2)
	require.NotEqual(t, "", v6IP2.String())
	require.Equal(t, staticMac2, mac2)
	// compare
	require.NotEqual(t, v6IP1.String(), v6IP2.String())
	require.NotEqual(t, mac1, mac2)
}

func TestGetRandomDualStackAddress(t *testing.T) {
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
	// 1. no mac, get v4, v6 address for pod1
	podName := "pod1"
	nicName := "pod1.default"
	poolName := ""
	skippedAddrs := []string{"10.0.0.1", "10.0.0.5", "2001:db8::1", "2001:db8::5"}
	v4IP1, v6IP1, mac1, err := subnet.GetRandomAddress(poolName, podName, nicName, nil, skippedAddrs, true)
	require.Nil(t, err)
	require.NotEqual(t, "", v4IP1.String())
	require.NotEqual(t, "", v6IP1.String())
	require.NotEqual(t, "", mac1)
	// 2. has mac, get v4, v6 address for pod1
	podName = "pod2"
	nicName = "pod2.default"
	staticMac2 := "00:11:22:33:44:55"
	v4IP2, v6IP2, mac2, err := subnet.GetRandomAddress(poolName, podName, nicName, &staticMac2, skippedAddrs, true)
	require.Nil(t, err)
	require.NotEqual(t, "", v4IP2.String())
	require.NotEqual(t, "", v6IP2.String())
	require.Equal(t, staticMac2, mac2)
	// compare
	require.NotEqual(t, v4IP1.String(), v4IP2.String())
	require.NotEqual(t, v6IP1.String(), v6IP2.String())
	require.NotEqual(t, mac1, mac2)
}

func TestReleaseAddrForV4Subnet(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
	}
	subnet, err := NewSubnet("testV4RleasedSubnet", "10.0.0.0/24", excludeIps)
	if err != nil {
		t.Errorf("failed to create IPv4 subnet: %v", err)
	}
	// 1. pod1 get random v4 address
	podName := "pod1"
	nicName := "pod1.default"
	poolName := ""
	v4, _, _, err := subnet.GetRandomAddress(poolName, podName, nicName, nil, nil, false)
	require.Nil(t, err)
	require.Equal(t, subnet.V4Using.String(), v4.String())
	require.True(t, subnet.ContainAddress(v4))
	// pod1 release random v4 address
	subnet.ReleaseAddress(podName)
	require.Equal(t, subnet.V4Using.String(), "")
	require.False(t, subnet.ContainAddress(v4))
	// 2. pod2 get random v4 address
	podName = "pod2"
	nicName = "pod2.default"
	poolName = ""
	v4, _, _, err = subnet.GetRandomAddress(poolName, podName, nicName, nil, nil, false)
	require.Nil(t, err)
	require.Equal(t, subnet.V4Using.String(), v4.String())
	require.True(t, subnet.ContainAddress(v4))
	// pod2 release random v4 address
	subnet.ReleaseAddressWithNicName(podName, nicName)
	require.Equal(t, subnet.V4Using.String(), "")
	require.False(t, subnet.ContainAddress(v4))
}

func TestReleaseV6SubnetAddrForV6Subnet(t *testing.T) {
	excludeIps := []string{"2001:db8::2", "2001:db8::4", "2001:db8::100", "2001:db8::252", "2001:db8::253", "2001:db8::254"}
	subnet, err := NewSubnet("testV6RleasedSubnet", "2001:db8::/64", excludeIps)
	if err != nil {
		t.Errorf("failed to create IPv6 subnet: %v", err)
	}
	// 1. pod1 get random v6 address
	podName := "pod1"
	nicName := "pod1.default"
	poolName := ""
	_, v6, _, err := subnet.GetRandomAddress(poolName, podName, nicName, nil, nil, false)
	require.Nil(t, err)
	require.Equal(t, subnet.V6Using.String(), v6.String())
	require.True(t, subnet.ContainAddress(v6))
	// pod1 release random v6 address
	subnet.ReleaseAddress(podName)
	require.Equal(t, subnet.V6Using.String(), "")
	require.False(t, subnet.ContainAddress(v6))
	// 2. pod2 get random v6 address
	podName = "pod2"
	nicName = "pod2.default"
	poolName = ""
	_, v6, _, err = subnet.GetRandomAddress(poolName, podName, nicName, nil, nil, false)
	require.Nil(t, err)
	require.Equal(t, subnet.V6Using.String(), v6.String())
	require.True(t, subnet.ContainAddress(v6))
	// pod2 release random v6 address
	subnet.ReleaseAddressWithNicName(podName, nicName)
	require.Equal(t, subnet.V6Using.String(), "")
	require.False(t, subnet.ContainAddress(v6))
}

func TestReleaseAddrForDualSubnet(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnet, err := NewSubnet("testDualRleasedSubnet", "10.0.0.0/24,2001:db8::/64", excludeIps)
	if err != nil {
		t.Errorf("failed to create dual stack subnet: %v", err)
	}
	// 1. pod1 get random v4, v6 address
	podName := "pod1"
	nicName := "pod1.default"
	poolName := ""
	v4, v6, _, err := subnet.GetRandomAddress(poolName, podName, nicName, nil, nil, true)
	require.Nil(t, err)
	require.Equal(t, subnet.V4Using.String(), v4.String())
	require.True(t, subnet.ContainAddress(v4))
	require.Equal(t, subnet.V6Using.String(), v6.String())
	require.True(t, subnet.ContainAddress(v6))
	// pod1 release random v4, v6 address
	subnet.ReleaseAddress(podName)
	require.Equal(t, subnet.V4Using.String(), "")
	require.Equal(t, subnet.V6Using.String(), "")
	require.False(t, subnet.ContainAddress(v4))
	require.False(t, subnet.ContainAddress(v6))
	// 2. pod1 get random v4, v6 address
	podName = "pod2"
	nicName = "pod2.default"
	poolName = ""
	v4, v6, _, err = subnet.GetRandomAddress(poolName, podName, nicName, nil, nil, true)
	require.Nil(t, err)
	require.Equal(t, subnet.V4Using.String(), v4.String())
	require.True(t, subnet.ContainAddress(v4))
	require.Equal(t, subnet.V6Using.String(), v6.String())
	require.True(t, subnet.ContainAddress(v6))
	// pod1 release random v4, v6 address
	subnet.ReleaseAddressWithNicName(podName, nicName)
	require.Equal(t, subnet.V4Using.String(), "")
	require.Equal(t, subnet.V6Using.String(), "")
	require.False(t, subnet.ContainAddress(v4))
	require.False(t, subnet.ContainAddress(v6))
}

// TODO: ippool
