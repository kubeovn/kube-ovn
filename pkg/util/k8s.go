package util

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/klog/v2"
)

func DialAPIServer(host string) error {
	u, err := url.Parse(host)
	if err != nil {
		return fmt.Errorf("failed to parse host %q: %v", host, err)
	}

	address := net.JoinHostPort(u.Hostname(), u.Port())
	timer := time.NewTimer(3 * time.Second)
	for i := 0; i < 10; i++ {
		conn, err := net.DialTimeout("tcp", address, 3*time.Second)
		if err == nil {
			klog.Infof("succeeded to dial apiserver %q", address)
			_ = conn.Close()
			return nil
		}
		klog.Warningf("failed to dial apiserver %q: %v", address, err)
		<-timer.C
		timer.Reset(3 * time.Second)
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
	ips := svc.Spec.ClusterIPs
	if len(ips) == 0 && svc.Spec.ClusterIP != v1.ClusterIPNone && svc.Spec.ClusterIP != "" {
		ips = []string{svc.Spec.ClusterIP}
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
