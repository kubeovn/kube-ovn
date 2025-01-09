package util

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

func DialTCP(host string, timeout time.Duration, verbose bool) error {
	u, err := url.Parse(host)
	if err != nil {
		return fmt.Errorf("failed to parse host %q: %v", host, err)
	}

	var conn net.Conn
	address := net.JoinHostPort(u.Hostname(), u.Port())
	switch u.Scheme {
	case "tcp", "http":
		conn, err = net.DialTimeout("tcp", address, timeout)
	case "tls", "https":
		config := &tls.Config{InsecureSkipVerify: true} // #nosec G402
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", address, config)
	default:
		return fmt.Errorf("unsupported scheme %q", u.Scheme)
	}

	if err == nil {
		if verbose {
			klog.Infof("succeeded to dial host %q", host)
		}
		_ = conn.Close()
		return nil
	}

	return fmt.Errorf("timed out dialing host %q", host)
}

func DialAPIServer(host string) error {
	interval := 3 * time.Second
	timer := time.NewTimer(interval)
	for i := 0; i < 10; i++ {
		err := DialTCP(host, interval, true)
		if err == nil {
			return nil
		}
		klog.Warningf("failed to dial apiserver %q: %v", host, err)
		<-timer.C
		timer.Reset(interval)
	}

	return fmt.Errorf("timed out dialing apiserver %q", host)
}

func GetNodeInternalIP(node v1.Node) (ipv4, ipv6 string) {
	var ips []string
	for _, addr := range node.Status.Addresses {
		if addr.Type == v1.NodeInternalIP {
			ips = append(ips, addr.Address)
		}
	}

	return SplitStringIP(strings.Join(ips, ","))
}

func ServiceClusterIPs(svc v1.Service) []string {
	if len(svc.Spec.ClusterIPs) == 0 && svc.Spec.ClusterIP != v1.ClusterIPNone && svc.Spec.ClusterIP != "" {
		return []string{svc.Spec.ClusterIP}
	}

	ips := make([]string, 0, len(svc.Spec.ClusterIPs))
	for _, ip := range svc.Spec.ClusterIPs {
		if net.ParseIP(ip) == nil {
			if ip != "" && ip != v1.ClusterIPNone {
				klog.Warningf("invalid cluster IP %q for service %s/%s", ip, svc.Namespace, svc.Name)
			}
			continue
		}
		ips = append(ips, ip)
	}
	return ips
}

func LabelSelectorNotEquals(key, value string) (labels.Selector, error) {
	requirement, err := labels.NewRequirement(key, selection.NotEquals, []string{value})
	if err != nil {
		return nil, err
	}
	return labels.Everything().Add(*requirement), nil
}

func LabelSelectorNotEmpty(key string) (labels.Selector, error) {
	return LabelSelectorNotEquals(key, "")
}

func GetTruncatedUID(uid string) string {
	return uid[len(uid)-12:]
}

func SetNodeNetworkUnavailableCondition(cs kubernetes.Interface, nodeName string, status v1.ConditionStatus, reason, message string) error {
	now := metav1.NewTime(time.Now())
	patch := map[string]map[string][]v1.NodeCondition{
		"status": {
			"conditions": []v1.NodeCondition{{
				Type:               v1.NodeNetworkUnavailable,
				Status:             status,
				Reason:             reason,
				Message:            message,
				LastTransitionTime: now,
				LastHeartbeatTime:  now,
			}},
		},
	}
	data, err := json.Marshal(patch)
	if err != nil {
		klog.Errorf("failed to marshal patch data: %v", err)
		return err
	}

	if _, err = cs.CoreV1().Nodes().PatchStatus(context.Background(), nodeName, data); err != nil {
		klog.Errorf("failed to patch node %s: %v", nodeName, err)
		return err
	}

	return nil
}
