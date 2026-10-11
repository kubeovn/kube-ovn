package ko

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCaptureArguments(t *testing.T) {
	options, err := parseCaptureArguments([]string{"-i", "eth0", "-nn", "-c1", "-s", "128", "-w", "-"})
	require.NoError(t, err)
	require.Equal(t, packetCaptureOptions{iface: "eth0", count: 1, snaplen: 128, pcap: true}, options)
}

func TestParseCaptureArgumentsListsInterfaces(t *testing.T) {
	options, err := parseCaptureArguments([]string{"-D"})
	require.NoError(t, err)
	require.True(t, options.list)
}

func TestParseCaptureArgumentsRejectsUnsupportedFilters(t *testing.T) {
	_, err := parseCaptureArguments([]string{"-c", "1", "tcp port 80"})
	require.ErrorContains(t, err, "unsupported capture argument")

	_, err = parseCaptureArguments([]string{"-w", "/tmp/capture.pcap"})
	require.ErrorContains(t, err, "only supports -w -")
}
