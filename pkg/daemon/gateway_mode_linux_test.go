package daemon

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseGatewayNetfilterMode(t *testing.T) {
	tests := []struct {
		input string
		want  gatewayNetfilterMode
		err   bool
	}{
		{input: "auto", want: gatewayNetfilterModeAuto},
		{input: " IPTABLES ", want: gatewayNetfilterModeIPTables},
		{input: "nftables", want: gatewayNetfilterModeNFTables},
		{input: "nft", err: true},
	}

	for _, tt := range tests {
		got, err := parseGatewayNetfilterMode(tt.input)
		require.Equal(t, tt.err, err != nil)
		require.Equal(t, tt.want, got)
	}
}

func TestProxyModeDetectorHTTP(t *testing.T) {
	tests := []struct {
		body string
		want gatewayNetfilterMode
	}{
		{body: "iptables\n", want: gatewayNetfilterModeIPTables},
		{body: "ipvs\n", want: gatewayNetfilterModeIPTables},
		{body: "nftables\n", want: gatewayNetfilterModeNFTables},
	}

	for _, tt := range tests {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, tt.body)
		}))

		detector := newProxyModeDetector(server.URL, time.Second, nil)
		mode, err := detector.detectHTTP(context.Background())
		server.Close()

		require.NoError(t, err)
		require.Equal(t, tt.want, mode)
		transport, ok := detector.client.Transport.(*http.Transport)
		require.True(t, ok)
		require.Nil(t, transport.Proxy)
	}
}

func TestStableProxyMode(t *testing.T) {
	stable := proxyModeStability{}
	require.False(t, stable.observe(gatewayNetfilterModeNFTables))
	require.False(t, stable.observe(gatewayNetfilterModeNFTables))
	require.True(t, stable.observe(gatewayNetfilterModeNFTables))
	require.True(t, stable.observe(gatewayNetfilterModeNFTables))
	require.False(t, stable.observe(gatewayNetfilterModeIPTables))
}

func TestDetectorColdStartFallback(t *testing.T) {
	detector := newProxyModeDetector(
		"http://127.0.0.1:1/proxyMode",
		10*time.Millisecond,
		func(context.Context) (gatewayNetfilterMode, error) {
			return gatewayNetfilterModeNFTables, nil
		},
	)

	mode, err := detector.detectColdStart(context.Background())
	require.NoError(t, err)
	require.Equal(t, gatewayNetfilterModeNFTables, mode)
}
