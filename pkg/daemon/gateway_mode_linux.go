package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type gatewayNetfilterMode string

const (
	gatewayNetfilterModeAuto     gatewayNetfilterMode = "auto"
	gatewayNetfilterModeIPTables gatewayNetfilterMode = "iptables"
	gatewayNetfilterModeNFTables gatewayNetfilterMode = "nftables"
)

func parseGatewayNetfilterMode(value string) (gatewayNetfilterMode, error) {
	mode := gatewayNetfilterMode(strings.ToLower(strings.TrimSpace(value)))
	switch mode {
	case gatewayNetfilterModeAuto, gatewayNetfilterModeIPTables, gatewayNetfilterModeNFTables:
		return mode, nil
	default:
		return "", fmt.Errorf("不支持的网关 netfilter 模式 %q", value)
	}
}

type proxyModeDetector struct {
	endpoint        string
	client          *http.Client
	runtimeFallback func(context.Context) (gatewayNetfilterMode, error)
}

func newProxyModeDetector(endpoint string, timeout time.Duration, fallback func(context.Context) (gatewayNetfilterMode, error)) *proxyModeDetector {
	return &proxyModeDetector{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy: nil,
				DialContext: (&net.Dialer{
					Timeout: timeout,
				}).DialContext,
			},
		},
		runtimeFallback: fallback,
	}
}

func (d *proxyModeDetector) detectHTTP(ctx context.Context) (gatewayNetfilterMode, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.endpoint, nil)
	if err != nil {
		return "", err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("kube-proxy proxyMode 返回状态码 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32))
	if err != nil {
		return "", err
	}

	switch mode := strings.TrimSpace(string(body)); mode {
	case "iptables", "ipvs":
		return gatewayNetfilterModeIPTables, nil
	case "nftables":
		return gatewayNetfilterModeNFTables, nil
	default:
		return "", fmt.Errorf("未知的 kube-proxy 模式 %q", mode)
	}
}

func (d *proxyModeDetector) detectColdStart(ctx context.Context) (gatewayNetfilterMode, error) {
	mode, httpErr := d.detectHTTP(ctx)
	if httpErr == nil {
		return mode, nil
	}
	if d.runtimeFallback == nil {
		return "", httpErr
	}

	mode, fallbackErr := d.runtimeFallback(ctx)
	if fallbackErr != nil {
		return "", errors.Join(httpErr, fallbackErr)
	}
	return mode, nil
}

type proxyModeStability struct {
	last  gatewayNetfilterMode
	count int
}

func (s *proxyModeStability) observe(mode gatewayNetfilterMode) bool {
	if s.last != mode {
		s.last = mode
		s.count = 1
		return false
	}

	s.count++
	return s.count >= 3
}
