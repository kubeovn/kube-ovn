package util

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGwIPTablesCounters_LargeValues(t *testing.T) {
	t.Parallel()
	c := GwIPTablesCounters{
		Packets:     math.MaxUint64,
		PacketBytes: math.MaxUint64,
	}
	require.Equal(t, uint64(math.MaxUint64), c.Packets)
	require.Equal(t, uint64(math.MaxUint64), c.PacketBytes)
}

func TestGwIPTablesCounters_DiffCalculation(t *testing.T) {
	t.Parallel()
	last := GwIPTablesCounters{Packets: 100, PacketBytes: 2000}
	current := GwIPTablesCounters{Packets: 250, PacketBytes: 5000}

	diffPackets := current.Packets - last.Packets
	diffPacketBytes := current.PacketBytes - last.PacketBytes
	require.Equal(t, uint64(150), diffPackets)
	require.Equal(t, uint64(3000), diffPacketBytes)
}
