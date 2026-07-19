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

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
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
	endpoint         string
	client           *http.Client
	coldStartTimeout time.Duration
	runtimeFallback  func(context.Context) (gatewayNetfilterMode, error)
}

func newProxyModeDetector(endpoint string, timeout time.Duration, fallback func(context.Context) (gatewayNetfilterMode, error)) *proxyModeDetector {
	requestTimeout := min(timeout, 2*time.Second)
	if requestTimeout <= 0 {
		requestTimeout = 2 * time.Second
	}
	return &proxyModeDetector{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				Proxy: nil,
				DialContext: (&net.Dialer{
					Timeout: requestTimeout,
				}).DialContext,
			},
		},
		coldStartTimeout: timeout,
		runtimeFallback:  fallback,
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

	return gatewayNetfilterModeForProxyMode(strings.TrimSpace(string(body)))
}

func gatewayNetfilterModeForProxyMode(mode string) (gatewayNetfilterMode, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "":
		return gatewayNetfilterModeIPTables, nil
	case "iptables", "ipvs":
		return gatewayNetfilterModeIPTables, nil
	case "nftables":
		return gatewayNetfilterModeNFTables, nil
	default:
		return "", fmt.Errorf("未知的 kube-proxy 模式 %q", mode)
	}
}

func (c *Controller) detectKubeProxyModeFromConfig(ctx context.Context) (gatewayNetfilterMode, error) {
	configMap, err := c.config.KubeClient.CoreV1().ConfigMaps("kube-system").Get(ctx, "kube-proxy", metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return gatewayNetfilterModeIPTables, nil
		}
		return "", fmt.Errorf("读取 kube-proxy ConfigMap: %w", err)
	}

	raw := configMap.Data["config.conf"]
	if raw == "" {
		return "", errors.New("kube-proxy ConfigMap 缺少 config.conf")
	}
	config := struct {
		Mode string `json:"mode"`
	}{}
	if err := yaml.Unmarshal([]byte(raw), &config); err != nil {
		return "", fmt.Errorf("解析 kube-proxy config.conf: %w", err)
	}
	return gatewayNetfilterModeForProxyMode(config.Mode)
}

func (d *proxyModeDetector) detectColdStart(ctx context.Context) (gatewayNetfilterMode, error) {
	waitCtx := ctx
	cancel := func() {}
	if d.coldStartTimeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, d.coldStartTimeout)
	}
	defer cancel()

	var httpErr error
	for {
		mode, err := d.detectHTTP(waitCtx)
		if err == nil {
			return mode, nil
		}
		httpErr = err
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return "", errors.Join(httpErr, ctx.Err())
			}
			goto fallback
		case <-timer.C:
		}
	}

fallback:
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
