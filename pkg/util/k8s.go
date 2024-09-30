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
	"k8s.io/apimachinery/pkg/types"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"
)

func DialTCP(host string, timeout time.Duration, verbose bool) error {
	u, err := url.Parse(host)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to parse host %q: %w", host, err)
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

func DialAPIServer(host string, interval time.Duration, retry int) error {
	timer := time.NewTimer(interval)
	for i := 0; i < retry; i++ {
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

func PodIPs(pod v1.Pod) []string {
	if len(pod.Status.PodIPs) == 0 && pod.Status.PodIP != "" {
		return []string{pod.Status.PodIP}
	}

	ips := make([]string, len(pod.Status.PodIPs))
	for i := range pod.Status.PodIPs {
		ips[i] = pod.Status.PodIPs[i].IP
	}
	return ips
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
		klog.Errorf("failed to create label requirement: %v", err)
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

func UpdateNodeLabels(cs clientv1.NodeInterface, node string, labels map[string]any) error {
	buf, err := json.Marshal(labels)
	if err != nil {
		klog.Errorf("failed to marshal labels: %v", err)
		return err
	}
	patch := fmt.Sprintf(`{"metadata":{"labels":%s}}`, string(buf))
	return nodeMergePatch(cs, node, patch)
}

func UpdateNodeAnnotations(cs clientv1.NodeInterface, node string, annotations map[string]any) error {
	buf, err := json.Marshal(annotations)
	if err != nil {
		klog.Errorf("failed to marshal annotations: %v", err)
		return err
	}
	patch := fmt.Sprintf(`{"metadata":{"annotations":%s}}`, string(buf))
	return nodeMergePatch(cs, node, patch)
}

// we do not use GenerateMergePatchPayload/GenerateStrategicMergePatchPayload,
// because we use a `null` value to delete a label/annotation
func nodeMergePatch(cs clientv1.NodeInterface, node, patch string) error {
	_, err := cs.Patch(context.Background(), node, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		klog.Errorf("failed to patch node %s with json merge patch %q: %v", node, patch, err)
		return err
	}
	return nil
}
