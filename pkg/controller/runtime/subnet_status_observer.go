package controllerruntime

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

const subnetStatusObservationTimeout = 5 * time.Second

var (
	subnetStatusObservations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kube_ovn_subnet_status_observations_total",
			Help: "Number of read-only Subnet IP status observations by outcome.",
		}, []string{"result"},
	)
	subnetStatusObservationDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "kube_ovn_subnet_status_observation_duration_seconds",
			Help: "Duration of read-only Subnet IP status observations in seconds.",
		},
	)
)

func init() {
	registerSubnetShadowMetric(subnetStatusObservations)
	registerSubnetShadowMetric(subnetStatusObservationDuration)
}

// observeStatus compares only after an uncached read confirms that the
// calculator's snapshot still identifies the same active Subnet revision.
// These observations do not provide an atomic snapshot of informer/IPAM inputs.
func (r *SubnetStatusReconciler) observeStatus(ctx context.Context, request SubnetStatusRequest) (string, error) {
	subnet, err := r.readStatusSubnet(ctx, request)
	if apierrors.IsNotFound(err) {
		return "not_found", nil
	}
	if err != nil {
		return "", err
	}
	if !subnet.DeletionTimestamp.IsZero() {
		return "terminating", nil
	}
	projection, err := r.projectStatusForSubnet(ctx, subnet)
	if err != nil {
		return "", err
	}
	current, err := getSubnet(ctx, r.Reader, request.Name, subnetStatusObservationTimeout)
	if apierrors.IsNotFound(err) {
		return "not_found", nil
	}
	if err != nil {
		return "", fmt.Errorf("confirm subnet %s status observation: %w", request.Name, err)
	}
	if !current.DeletionTimestamp.IsZero() {
		return "terminating", nil
	}
	if err := projection.Snapshot.validateCurrent(current); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if sameSubnetIPStatus(&current.Status, &projection.Status) {
		return "match", nil
	}
	return "drift", nil
}

// sameSubnetIPStatus excludes conditions, gateway MAC and other fields owned
// outside the IP calculator. BigInt comparison preserves IPv6 counter precision.
func sameSubnetIPStatus(current, projected *kubeovnv1.SubnetStatus) bool {
	return current.V4AvailableIPs.Equal(projected.V4AvailableIPs) &&
		current.V4UsingIPs.Equal(projected.V4UsingIPs) &&
		current.V6AvailableIPs.Equal(projected.V6AvailableIPs) &&
		current.V6UsingIPs.Equal(projected.V6UsingIPs) &&
		current.V4AvailableIPRange == projected.V4AvailableIPRange &&
		current.V4UsingIPRange == projected.V4UsingIPRange &&
		current.V6AvailableIPRange == projected.V6AvailableIPRange &&
		current.V6UsingIPRange == projected.V6UsingIPRange
}
