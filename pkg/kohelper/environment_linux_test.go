//go:build linux

package kohelper

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type environmentFailingWriter struct{}

func (environmentFailingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestRunEnvironmentReturnsOutputError(t *testing.T) {
	err := RunEnvironment(t.Context(), environmentFailingWriter{}, environmentFailingWriter{})
	require.ErrorContains(t, err, "write failed")
}

func TestReportChecksumCounters(t *testing.T) {
	for _, test := range []struct {
		name, input, output string
	}{
		{"zero", "Tcp: InCsumErrors\nTcp: 0\nUdp: InCsumErrors\nUdp: 0\n", ""},
		{"udp", "Tcp: InCsumErrors\nTcp: 0\nUdp: InDatagrams InCsumErrors\nUdp: 10 23\n", "Udp: InCsumErrors=23"},
		{"tcp", "Tcp: InCsumErrors\nTcp: 17\nUdp: InCsumErrors\nUdp: 0\n", "Tcp: InCsumErrors=17"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			require.NoError(t, reportChecksumCounters(strings.NewReader(test.input), &output))
			if test.output == "" {
				require.Empty(t, output.String())
			} else {
				require.Contains(t, output.String(), test.output)
			}
		})
	}
	for _, input := range []string{"Udp: InCsumErrors\n", "Udp: InCsumErrors\nUdp: invalid\n", "Udp: InCsumErrors\nTcp: 12\n"} {
		require.Error(t, reportChecksumCounters(strings.NewReader(input), io.Discard))
	}
}

func TestProbeUDPPreservesInvalidAddressError(t *testing.T) {
	state, err := probeUDP(t.Context(), "invalid-address")
	require.Equal(t, "unknown", state)
	require.ErrorContains(t, err, "dial UDP probe")
}
