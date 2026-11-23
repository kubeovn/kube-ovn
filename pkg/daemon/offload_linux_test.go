package daemon

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
)

func TestShouldDisableUFO(t *testing.T) {
	tests := []struct {
		name     string
		features map[string]netlink.NetDevFeature
		want     bool
	}{
		{name: "active", features: map[string]netlink.NetDevFeature{"tx-udp-fragmentation": {Active: true}}, want: true},
		{name: "inactive", features: map[string]netlink.NetDevFeature{"tx-udp-fragmentation": {Active: false}}},
		{name: "missing", features: map[string]netlink.NetDevFeature{"tx-checksum-ipv4": {Active: true}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldDisableUFO(tt.features))
		})
	}
}

func TestTurnOffNicTxChecksumUsesNetlink(t *testing.T) {
	originalGet, originalSet := netDevFeaturesGet, netDevFeaturesSet
	t.Cleanup(func() {
		netDevFeaturesGet, netDevFeaturesSet = originalGet, originalSet
	})

	var gotIndex int
	var gotChanges map[string]bool
	netDevFeaturesGet = func(index int) (map[string]netlink.NetDevFeature, error) {
		gotIndex = index
		return map[string]netlink.NetDevFeature{
			"tx-checksum-ipv4": {Hardware: true},
			"tx-checksum-ipv6": {Hardware: true, NoChange: true},
		}, nil
	}
	netDevFeaturesSet = func(index int, changes map[string]bool) error {
		gotChanges = changes
		require.Equal(t, gotIndex, index)
		return nil
	}
	require.NoError(t, TurnOffNicTxChecksum("lo"))
	require.NotZero(t, gotIndex)
	require.Equal(t, map[string]bool{"tx-checksum-ipv4": false}, gotChanges)
}

func TestTurnOffNicTxChecksumReturnsNetlinkErrors(t *testing.T) {
	originalGet, originalSet := netDevFeaturesGet, netDevFeaturesSet
	t.Cleanup(func() {
		netDevFeaturesGet, netDevFeaturesSet = originalGet, originalSet
	})

	queryErr := errors.New("query failed")
	netDevFeaturesGet = func(int) (map[string]netlink.NetDevFeature, error) { return nil, queryErr }
	err := TurnOffNicTxChecksum("lo")
	require.ErrorIs(t, err, queryErr)

	netDevFeaturesGet = func(int) (map[string]netlink.NetDevFeature, error) {
		return map[string]netlink.NetDevFeature{"tx-checksum-ipv4": {Hardware: true}}, nil
	}
	setErr := errors.New("set failed")
	netDevFeaturesSet = func(int, map[string]bool) error { return setErr }
	err = TurnOffNicTxChecksum("lo")
	require.ErrorIs(t, err, setErr)
}
