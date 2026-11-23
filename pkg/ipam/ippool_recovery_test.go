package ipam

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectIPPoolIPsFiltersHeldRangeBeforeTakingPrefix(t *testing.T) {
	for _, tt := range []struct {
		cidr                              string
		excluded, members, wantV4, wantV6 []string
		v4, v6                            int
	}{
		{cidr: "10.0.0.0/29", excluded: []string{"10.0.0.1", "10.0.0.2"}, members: []string{"10.0.0.0/29"}, wantV4: []string{"10.0.0.3"}, v4: 1},
		{cidr: "fd00::/125", excluded: []string{"fd00::1", "fd00::2"}, members: []string{"fd00::/125"}, wantV6: []string{"fd00::3"}, v6: 1},
		{cidr: "10.0.0.0/29,fd00::/125", excluded: []string{"10.0.0.1", "10.0.0.2", "fd00::1", "fd00::2"}, members: []string{"10.0.0.0/29", "fd00::/125"}, wantV4: []string{"10.0.0.3"}, wantV6: []string{"fd00::3"}, v4: 1, v6: 1},
	} {
		t.Run(tt.cidr, func(t *testing.T) {
			s, err := NewSubnet("subnet", tt.cidr, tt.excluded)
			require.NoError(t, err)
			require.NoError(t, s.AddOrUpdateIPPool("pool", tt.members))
			v4, v6, err := s.SelectIPPoolIPs("pool", nil, tt.v4, tt.v6)
			require.NoError(t, err)
			require.ElementsMatch(t, tt.wantV4, v4)
			require.ElementsMatch(t, tt.wantV6, v6)
		})
	}
}

func TestIPPoolReservationSurvivesSubnetReconcile(t *testing.T) {
	ipam := NewIPAM()
	require.NoError(t, ipam.AddOrUpdateSubnet("subnet", "10.0.0.0/29", "", nil))
	require.NoError(t, ipam.AddOrUpdateIPPool("subnet", "pool", []string{"10.0.0.1"}))
	selected, _, err := ipam.SelectIPPoolIPs("subnet", "pool", nil, 2, 0)
	require.NoError(t, err)
	failed := errors.New("prepare failed")
	require.ErrorIs(t, ipam.ActivateIPPoolIPs("subnet", "pool", selected, 2, 0, func() error { return failed }), failed)
	require.NoError(t, ipam.AddOrUpdateSubnet("subnet", "10.0.0.0/29", "", []string{"10.0.0.6"}))
	_, _, _, err = ipam.GetRandomAddress("pod", "nic", nil, "subnet", "pool", nil, true)
	require.ErrorIs(t, err, ErrNoAvailable)
	_, _, _, err = ipam.GetStaticAddress("pod", "nic", "10.0.0.2", nil, "subnet", false)
	require.ErrorIs(t, err, ErrNoAvailable)
	address, _, _, err := ipam.GetRandomAddress("default-pod", "default-nic", nil, "subnet", "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.3", address)
	v4a, _, _, _, v4range, _, _, _ := ipam.IPPoolStatistics("subnet", "pool")
	require.Zero(t, v4a.Int64())
	require.Empty(t, v4range)
	require.NoError(t, ipam.ActivateIPPoolIPs("subnet", "pool", selected, 2, 0, func() error { return nil }))
	_, _, _, err = ipam.GetRandomAddress("pod", "nic", nil, "subnet", "pool", nil, true)
	require.NoError(t, err)
}

func TestIPPoolPreparationAllowsUnrelatedAllocations(t *testing.T) {
	ipam := NewIPAM()
	require.NoError(t, ipam.AddOrUpdateSubnet("subnet", "10.0.0.0/29", "", nil))
	require.NoError(t, ipam.AddOrUpdateSubnet("other", "10.1.0.0/29", "", nil))
	selected, _, err := ipam.SelectIPPoolIPs("subnet", "pool", nil, 2, 0)
	require.NoError(t, err)
	err = ipam.ActivateIPPoolIPs("subnet", "pool", selected, 2, 0, func() error {
		_, _, _, err := ipam.GetRandomAddress("other-pod", "other-nic", nil, "other", "", nil, true)
		require.NoError(t, err)
		_, _, _, err = ipam.GetRandomAddress("pool-pod", "pool-nic", nil, "subnet", "pool", nil, true)
		require.ErrorIs(t, err, ErrNoAvailable)
		return ipam.AddOrUpdateSubnet("subnet", "10.0.0.0/29", "", []string{"10.0.0.6"})
	})
	require.NoError(t, err)
	_, _, _, err = ipam.GetRandomAddress("pool-pod", "pool-nic", nil, "subnet", "pool", nil, true)
	require.NoError(t, err)
}

func TestIPPoolPreparationRevalidatesSubnetChanges(t *testing.T) {
	ipam := NewIPAM()
	require.NoError(t, ipam.AddOrUpdateSubnet("subnet", "10.0.0.0/29", "", nil))
	selected, _, err := ipam.SelectIPPoolIPs("subnet", "pool", nil, 2, 0)
	require.NoError(t, err)
	err = ipam.ActivateIPPoolIPs("subnet", "pool", selected, 2, 0, func() error {
		return ipam.AddOrUpdateSubnet("subnet", "10.0.0.0/29", "", []string{selected[1]})
	})
	require.ErrorIs(t, err, ErrSelectionChanged)
	_, _, _, err = ipam.GetRandomAddress("pod", "nic", nil, "subnet", "pool", nil, true)
	require.ErrorIs(t, err, ErrNoAvailable)
	selected, _, err = ipam.SelectIPPoolIPs("subnet", "pool", nil, 2, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.1", "10.0.0.3"}, selected)
	require.NoError(t, ipam.ActivateIPPoolIPs("subnet", "pool", selected, 2, 0, func() error { return nil }))
}

func TestIPPoolPreparationRejectsReplacedSubnet(t *testing.T) {
	ipam := NewIPAM()
	require.NoError(t, ipam.AddOrUpdateSubnet("subnet", "10.0.0.0/29", "", nil))
	selected, _, err := ipam.SelectIPPoolIPs("subnet", "pool", nil, 2, 0)
	require.NoError(t, err)
	err = ipam.ActivateIPPoolIPs("subnet", "pool", selected, 2, 0, func() error {
		ipam.DeleteSubnet("subnet")
		return ipam.AddOrUpdateSubnet("subnet", "10.1.0.0/29", "", nil)
	})
	require.ErrorIs(t, err, ErrSelectionChanged)
	require.Nil(t, ipam.Subnets["subnet"].IPPools["pool"])
}
