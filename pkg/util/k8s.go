package util

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

func DialApiServer(host string) error {
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
