package controller

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	defaultHTTPHealthCheckPath     = "/"
	defaultHTTPHealthCheckInterval = int32(5)
	defaultHTTPHealthCheckTimeout  = int32(2)
)

var httpHealthCheckClient = &http.Client{
	Transport: &http.Transport{
		Proxy:             nil,
		DisableKeepAlives: true,
	},
}

func normalizeHTTPHealthCheckPath(path string) string {
	if path == "" {
		return defaultHTTPHealthCheckPath
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

func httpHealthCheckCacheKey(namespace, svcName, ip string) string {
	return namespace + "/" + svcName + "/" + ip
}

func serviceHTTPHealthCheckEnabled(service *corev1.Service) bool {
	if service == nil || service.Annotations == nil {
		return false
	}
	port := service.Annotations[util.ServiceHTTPHealthCheckPort]
	if port == "" {
		return false
	}
	parsed, err := strconv.ParseInt(port, 10, 32)
	return err == nil && parsed > 0
}

func (c *Controller) isHTTPHealthCheckHealthy(namespace, svcName, ip string) bool {
	if c.httpHealthCheckStatus == nil {
		return false
	}
	healthy, ok := c.httpHealthCheckStatus.Load(httpHealthCheckCacheKey(namespace, svcName, ip))
	return ok && healthy
}

func (c *Controller) filterHTTPHealthCheckBackends(svc *corev1.Service, backends []string) []string {
	if !serviceHTTPHealthCheckEnabled(svc) {
		return backends
	}

	filtered := make([]string, 0, len(backends))
	for _, backend := range backends {
		host, _, err := net.SplitHostPort(backend)
		if err != nil {
			klog.Errorf("failed to parse backend %s for HTTP health check: %v", backend, err)
			continue
		}
		if c.isHTTPHealthCheckHealthy(svc.Namespace, svc.Name, host) {
			filtered = append(filtered, backend)
		} else {
			klog.V(3).Infof("skip unhealthy HTTP health-check backend %s for service %s/%s", backend, svc.Namespace, svc.Name)
		}
	}
	return filtered
}

func (c *Controller) clearHTTPHealthCheckStatus(namespace, svcName string) {
	if c.httpHealthCheckStatus == nil {
		return
	}
	prefix := namespace + "/" + svcName + "/"
	c.httpHealthCheckStatus.Range(func(key string, _ bool) bool {
		if strings.HasPrefix(key, prefix) {
			c.httpHealthCheckStatus.Delete(key)
		}
		return true
	})
}

func (c *Controller) resyncSwitchLBRuleHTTPHealthChecks() {
	if c.switchLBRuleLister == nil {
		return
	}

	slrs, err := c.switchLBRuleLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list SwitchLBRules for HTTP health checks: %v", err)
		return
	}

	activeSvcKeys := make(map[string]struct{}, len(slrs))
	for _, slr := range slrs {
		namespace := slr.Spec.Namespace
		if namespace == "" {
			namespace = corev1.NamespaceDefault
		}
		svcName := generateSvcName(slr.Name)
		if slr.Spec.HealthCheck == nil || slr.Spec.HealthCheck.Port <= 0 {
			c.clearHTTPHealthCheckStatus(namespace, svcName)
			continue
		}

		activeSvcKeys[namespace+"/"+svcName] = struct{}{}
		if err := c.checkSwitchLBRuleHTTPHealth(slr); err != nil {
			klog.Errorf("failed to run HTTP health check for SwitchLBRule %s: %v", slr.Name, err)
		}
	}

	c.pruneHTTPHealthCheckStatus(activeSvcKeys)
}

func (c *Controller) pruneHTTPHealthCheckStatus(activeSvcKeys map[string]struct{}) {
	if c.httpHealthCheckStatus == nil {
		return
	}
	c.httpHealthCheckStatus.Range(func(key string, _ bool) bool {
		parts := strings.SplitN(key, "/", 3)
		if len(parts) < 2 {
			c.httpHealthCheckStatus.Delete(key)
			return true
		}
		if _, ok := activeSvcKeys[parts[0]+"/"+parts[1]]; !ok {
			c.httpHealthCheckStatus.Delete(key)
		}
		return true
	})
}

func (c *Controller) checkSwitchLBRuleHTTPHealth(slr *kubeovnv1.SwitchLBRule) error {
	hc := slr.Spec.HealthCheck
	interval := hc.IntervalSeconds
	if interval <= 0 {
		interval = defaultHTTPHealthCheckInterval
	}
	if c.httpHealthCheckLastProbe != nil {
		if last, ok := c.httpHealthCheckLastProbe.Load(slr.Name); ok && time.Since(last) < time.Duration(interval)*time.Second {
			return nil
		}
	}

	namespace := slr.Spec.Namespace
	if namespace == "" {
		namespace = corev1.NamespaceDefault
	}
	svcName := generateSvcName(slr.Name)
	ips := c.listSwitchLBRuleBackendIPs(slr, namespace, svcName)
	path := normalizeHTTPHealthCheckPath(hc.Path)
	timeout := hc.TimeoutSeconds
	if timeout <= 0 {
		timeout = defaultHTTPHealthCheckTimeout
	}

	changed := false
	seen := make(map[string]struct{}, len(ips))
	for _, ip := range ips {
		if ip == "" {
			continue
		}
		seen[ip] = struct{}{}
		healthy := probeHTTPHealth(httpHealthCheckClient, ip, hc.Port, path, time.Duration(timeout)*time.Second)
		key := httpHealthCheckCacheKey(namespace, svcName, ip)
		prev, ok := c.httpHealthCheckStatus.Load(key)
		if !ok || prev != healthy {
			klog.Infof("SwitchLBRule %s backend %s HTTP health check changed to %t", slr.Name, ip, healthy)
			changed = true
		}
		c.httpHealthCheckStatus.Store(key, healthy)
	}

	prefix := namespace + "/" + svcName + "/"
	if c.httpHealthCheckStatus != nil {
		c.httpHealthCheckStatus.Range(func(key string, _ bool) bool {
			if !strings.HasPrefix(key, prefix) {
				return true
			}
			ip := strings.TrimPrefix(key, prefix)
			if _, ok := seen[ip]; !ok {
				c.httpHealthCheckStatus.Delete(key)
				changed = true
			}
			return true
		})
	}

	if c.httpHealthCheckLastProbe != nil {
		c.httpHealthCheckLastProbe.Store(slr.Name, time.Now())
	}

	if changed && c.addOrUpdateEndpointSliceQueue != nil {
		c.addOrUpdateEndpointSliceQueue.Add(namespace + "/" + svcName)
	}
	return nil
}

func (c *Controller) listSwitchLBRuleBackendIPs(slr *kubeovnv1.SwitchLBRule, namespace, svcName string) []string {
	ipSet := make(map[string]struct{})
	for _, endpoint := range slr.Spec.Endpoints {
		if endpoint != "" {
			ipSet[endpoint] = struct{}{}
		}
	}

	if c.endpointSlicesLister != nil {
		endpointSlices, err := c.endpointSlicesLister.EndpointSlices(namespace).List(labels.Set{discoveryv1.LabelServiceName: svcName}.AsSelector())
		if err != nil {
			klog.Errorf("failed to list EndpointSlices for service %s/%s: %v", namespace, svcName, err)
		} else {
			for _, slice := range endpointSlices {
				for _, endpoint := range slice.Endpoints {
					for _, address := range endpoint.Addresses {
						if address != "" {
							ipSet[address] = struct{}{}
						}
					}
				}
			}
		}
	}

	ips := make([]string, 0, len(ipSet))
	for ip := range ipSet {
		ips = append(ips, ip)
	}
	return ips
}

func probeHTTPHealth(client *http.Client, ip string, port int32, path string, timeout time.Duration) bool {
	if client == nil {
		client = httpHealthCheckClient
	}
	url := fmt.Sprintf("http://%s%s", util.JoinHostPort(ip, port), path)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		klog.Errorf("failed to create HTTP health check request %s: %v", url, err)
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		klog.V(3).Infof("HTTP health check %s failed: %v", url, err)
		return false
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			klog.Errorf("failed to close HTTP health check response body %s: %v", url, err)
		}
	}()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))
	return resp.StatusCode == http.StatusOK
}
