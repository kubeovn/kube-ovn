package ipam

import (
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActivateIPPoolIPsPersistsOnlyAcceptedSelection(t *testing.T) {
	s, err := NewSubnet("subnet", "10.0.0.0/29", nil)
	require.NoError(t, err)
	selected, _, err := s.SelectIPPoolIPs("pool", nil, 1, 0)
	require.NoError(t, err)
	ip, err := NewIP(selected[0])
	require.NoError(t, err)
	_, _, err = s.GetStaticAddress("pod", "nic", ip, nil, false, false)
	require.NoError(t, err)
	called := false
	err = s.ActivateIPPoolIPs("pool", selected, 1, 0, func() error { called = true; return nil })
	require.ErrorIs(t, err, ErrSelectionChanged)
	require.False(t, called)
	require.Nil(t, s.IPPools["pool"])
}

func TestActivateIPPoolIPsPersistenceFailure(t *testing.T) {
	s, err := NewSubnet("subnet", "10.0.0.0/29", nil)
	require.NoError(t, err)
	require.NoError(t, s.AddOrUpdateIPPool("pool", []string{"10.0.0.1"}))
	selected, _, err := s.SelectIPPoolIPs("pool", nil, 2, 0)
	require.NoError(t, err)
	expected := errors.New("write failed")
	err = s.ActivateIPPoolIPs("pool", selected, 2, 0, func() error {
		if !s.Mutex.TryLock() {
			t.Fatal("preparation must not hold allocation locks")
		}
		s.Mutex.Unlock()
		return expected
	})
	require.ErrorIs(t, err, expected)
	require.True(t, s.IPPools["pool"].allocationsBlocked)
	_, _, _, err = s.GetRandomAddress("pool", "pod", "nic", nil, nil, true)
	require.ErrorIs(t, err, ErrNoAvailable)
	free, _, err := s.SelectIPPoolIPs("other", nil, 1, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.3"}, free)
	require.NoError(t, s.ActivateIPPoolIPs("pool", selected, 2, 0, func() error { return nil }))
	require.False(t, s.IPPools["pool"].allocationsBlocked)
	_, _, _, err = s.GetRandomAddress("pool", "pod", "nic", nil, nil, true)
	require.NoError(t, err)
}

func TestUsableRangeInvalidCIDR(t *testing.T) {
	_, err := usableRange(&net.IPNet{IP: net.IPv4(10, 0, 0, 1), Mask: net.IPMask{255, 0, 255, 0}})
	require.ErrorContains(t, err, "first usable IP")
	r, err := usableRange(nil)
	require.NoError(t, err)
	require.Zero(t, r.Len())
}
