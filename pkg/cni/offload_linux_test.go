package cni

import (
	"testing"

	"github.com/safchain/ethtool"
	"github.com/stretchr/testify/require"
)

func TestTXChecksumFeatureSelection(t *testing.T) {
	features := map[string]ethtool.FeatureState{
		"tx-checksum-ipv4":       {Available: true, Active: true},
		"tx-checksum-ip-generic": {Available: true, Requested: true},
		"tx-checksum-ipv6":       {Available: true, NeverChanged: true},
		"tx-checksum-sctp":       {Active: true},
		"rx-checksum":            {Available: true, Active: true},
	}
	require.Equal(t, map[string]bool{"tx-checksum-ipv4": false, "tx-checksum-ip-generic": false}, txChecksumChanges(features))
}

func TestOffloadProbeWithoutHostTools(t *testing.T) {
	t.Setenv("PATH", "/missing-host-tools")
	handle, err := ethtool.NewEthtool()
	require.NoError(t, err)
	defer handle.Close()
	// Exercise the real ioctl read against an existing device, without privileges
	// for changing the host network or a dependency on the ethtool executable.
	features, err := handle.FeaturesWithState("lo")
	require.NoError(t, err)
	require.NotEmpty(t, features)
	if len(txChecksumChanges(features)) != 0 {
		t.Skip("loopback exposes mutable TX checksum features")
	}
	require.NoError(t, TurnOffNicTxChecksum("lo"))
}
