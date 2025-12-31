package util

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	nadutils "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/utils"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/scheme"
)

// ObjectMatchesLabelSelector checks if the given object matches the provided label selector.
// It returns true if the object has labels that match the selector, otherwise false.
// If the selector is invalid, it logs an error and returns false.
// if the selector is nil, it returns false.
func ObjectMatchesLabelSelector(obj metav1.Object, selector *metav1.LabelSelector) bool {
	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		klog.Errorf("failed to convert label selector %v: %v", selector, err)
		return false
	}
	return labelSelector.Matches(labels.Set(obj.GetLabels()))
}

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
	for range retry {
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

func PodAttachmentIPs(pod *v1.Pod, networkName string) ([]string, error) {
	if pod == nil {
		return nil, errors.New("programmatic error: pod is nil")
	}

	statuses, err := nadutils.GetNetworkStatus(pod)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to get network status for pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}

	for _, status := range statuses {
		if status.Name == networkName {
			return status.IPs, nil
		}
	}

	return nil, fmt.Errorf("pod %s/%s has no network status for network %s", pod.Namespace, pod.Name, networkName)
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

func SetOwnerReference(owner, object metav1.Object) error {
	return controllerutil.SetOwnerReference(owner, object, scheme.Scheme)
}

func DeploymentIsReady(deployment *appsv1.Deployment) bool {
	if deployment.Generation > deployment.Status.ObservedGeneration {
		return false
	}

	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentProgressing {
			// deployment exceeded its progress deadline
			if condition.Reason == "ProgressDeadlineExceeded" {
				return false
			}
			break
		}
	}
	if (deployment.Spec.Replicas != nil && deployment.Status.UpdatedReplicas < *deployment.Spec.Replicas) ||
		deployment.Status.Replicas > deployment.Status.UpdatedReplicas ||
		deployment.Status.AvailableReplicas < deployment.Status.UpdatedReplicas {
		return false
	}
	return true
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

// InjectedServiceVariables returns the environment variable values for the given service name.
// For a service named "my-service", it returns values of "MY_SERVICE_SERVICE_HOST" and "MY_SERVICE_SERVICE_PORT".
func InjectedServiceVariables(service string) (string, string) {
	prefix := strings.ToUpper(strings.ReplaceAll(service, "-", "_"))
	return os.Getenv(prefix + "_SERVICE_HOST"), os.Getenv(prefix + "_SERVICE_PORT")
}

// ObjectKind returns the kind name of the given k8s object type T.
// If T is a pointer type, it returns the kind name of the underlying type.
// For example, if T is v1.Pod, it returns "Pod".
func ObjectKind[T metav1.Object]() string {
	typ := reflect.TypeFor[T]()
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	return typ.Name()
}

// TrimManagedFields removes the managed fields from the given k8s object.
// It returns the modified object and any error encountered during the process.
func TrimManagedFields(obj any) (any, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	if accessor.GetManagedFields() != nil {
		accessor.SetManagedFields(nil)
	}
	return obj, nil
}
