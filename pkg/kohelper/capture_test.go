package kohelper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCaptureArgs(t *testing.T) {
	options, err := parseCaptureArgs([]string{"--netns", "/var/run/netns/pod", "--interface", "eth0", "--count", "2", "--snaplen", "512", "--pcap"})
	require.NoError(t, err)
	require.Equal(t, captureOptions{netns: "/var/run/netns/pod", iface: "eth0", count: 2, snaplen: 512, pcap: true}, options)
}

func TestParseCaptureArgsRequiresStructuredOptions(t *testing.T) {
	_, err := parseCaptureArgs([]string{"--netns", "/var/run/netns/pod", "tcp", "port", "80"})
	require.ErrorContains(t, err, "unsupported capture option")
}
