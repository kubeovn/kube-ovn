package cni

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestTXChecksumFeatureSelection(t *testing.T) {
	features := map[string]netlink.NetDevFeature{
		"tx-checksum-ipv4":       {Hardware: true, Active: true},
		"tx-checksum-ip-generic": {Hardware: true, Wanted: true},
		"tx-checksum-ipv6":       {Hardware: true, NoChange: true},
		"tx-checksum-sctp":       {Active: true},
		"rx-checksum":            {Hardware: true, Active: true},
	}
	require.Equal(t, map[string]bool{"tx-checksum-ipv4": false, "tx-checksum-ip-generic": false}, txChecksumChanges(features))
}

func TestOffloadProbeWithoutHostTools(t *testing.T) {
	t.Setenv("PATH", "/missing-host-tools")
	// Exercise the real generic-netlink read against an existing device without
	// depending on the ethtool executable.
	link, err := netlink.LinkByName("lo")
	require.NoError(t, err)
	features, err := netlink.NetDevFeaturesGet(link.Attrs().Index)
	if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.EOPNOTSUPP) {
		t.Skipf("ethtool generic-netlink features are unavailable: %v", err)
	}
	require.NoError(t, err)
	require.NotEmpty(t, features)
	if len(txChecksumChanges(features)) != 0 {
		t.Skip("loopback exposes mutable TX checksum features")
	}
	require.NoError(t, TurnOffNicTxChecksum("lo"))
}
