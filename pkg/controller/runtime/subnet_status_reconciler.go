package controllerruntime

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimecontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/source"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

var (
	ErrSubnetStatusEventNameMissing   = errors.New("subnet status event name is not configured")
	ErrSubnetStatusEventNotStatus     = errors.New("subnet status event is not status-only")
	ErrSubnetStatusEventSourceMissing = errors.New("subnet status event source is not configured")
	ErrSubnetStatusProjectorMissing   = errors.New("subnet status projector is not configured")
	ErrSubnetStatusReaderMissing      = errors.New("subnet status API reader is not configured")
	ErrSubnetStatusManagerMissing     = errors.New("subnet status manager is not configured")
)

// SubnetStatusEvent is the explicit ingress contract for status-only events.
// Producers may report the original source kind, but the runtime queue keeps
// only the Subnet name so concurrent IP, Pod, VIP, and EIP events coalesce.
type SubnetStatusEvent struct {
	Name       string
	SourceKind string
	StatusOnly bool
}

// SubnetStatusRequest is the deduplicated runtime queue key. It intentionally
// carries no source object or status snapshot; Reconcile reads the current API
// object before invoking the pure projector.
type SubnetStatusRequest struct {
	Name string
}

// NewSubnetStatusRequest validates and normalizes one event into its queue
// key. Non-status events are rejected before they can enter the status path.
func NewSubnetStatusRequest(event SubnetStatusEvent) (SubnetStatusRequest, error) {
	if event.Name == "" {
		return SubnetStatusRequest{}, ErrSubnetStatusEventNameMissing
	}
	if ClassifySubnetDependencyEvent(SubnetDependencyEvent{SourceKind: event.SourceKind, StatusOnly: event.StatusOnly}) != SubnetStatusObservePath {
		return SubnetStatusRequest{}, fmt.Errorf("%w: source %q", ErrSubnetStatusEventNotStatus, event.SourceKind)
	}
	return SubnetStatusRequest{Name: event.Name}, nil
}

// SubnetStatusEventSource is an explicit ingress owned by a future producer.
// It is deliberately separate from the legacy status queue so migration code
// cannot steal events from the current writer.
type SubnetStatusEventSource interface {
	StartSubnetStatusEventBridge(context.Context, func(SubnetStatusEvent))
}

// LegacySubnetStatusEventSource is the dependency-free callback shape exposed
// by the legacy controller. Keeping this interface on primitive arguments
// avoids an import cycle between the legacy controller and runtime package.
type LegacySubnetStatusEventSource interface {
	SubscribeSubnetStatusEvents(context.Context, func(string, string)) func()
}

type legacySubnetStatusEventSource struct {
	source LegacySubnetStatusEventSource
}

// NewLegacySubnetStatusEventSource adapts the legacy producer fan-out to the
// runtime event vocabulary. It does not consume the legacy status queue.
func NewLegacySubnetStatusEventSource(source LegacySubnetStatusEventSource) SubnetStatusEventSource {
	if source == nil {
		return nil
	}
	return legacySubnetStatusEventSource{source: source}
}

func (s legacySubnetStatusEventSource) StartSubnetStatusEventBridge(ctx context.Context, enqueue func(SubnetStatusEvent)) {
	if s.source == nil || enqueue == nil {
		return
	}
	unsubscribe := s.source.SubscribeSubnetStatusEvents(ctx, func(name, sourceKind string) {
		enqueue(SubnetStatusEvent{Name: name, SourceKind: sourceKind})
	})
	if unsubscribe == nil || ctx == nil {
		return
	}
	go func() {
		<-ctx.Done()
		unsubscribe()
	}()
}

// SubnetStatusReconciler compares a status projection with the current Subnet
// for a normalized status event. It never writes Kubernetes status or invokes
// finalizer, OVN, or IPAM operations.
type SubnetStatusReconciler struct {
	Reader    client.Reader
	Events    SubnetStatusEventSource
	Projector SubnetStatusProjector
}

// SetupWithManager registers the status-only queue and explicit event source.
// Only the opt-in subnet-status mode calls this during migration.
func (r *SubnetStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if mgr == nil {
		return ErrSubnetStatusManagerMissing
	}
	if r.Events == nil {
		return ErrSubnetStatusEventSourceMissing
	}
	if r.Projector == nil {
		return ErrSubnetStatusProjectorMissing
	}
	if r.Reader == nil {
		r.Reader = mgr.GetAPIReader()
	}
	c, err := runtimecontroller.NewTyped("kube-ovn-subnet-status", mgr, runtimecontroller.TypedOptions[SubnetStatusRequest]{
		Reconciler:              r,
		MaxConcurrentReconciles: 1,
		RateLimiter:             workqueue.DefaultTypedControllerRateLimiter[SubnetStatusRequest](),
	})
	if err != nil {
		return fmt.Errorf("create Subnet status controller: %w", err)
	}
	return c.Watch(r.eventSource())
}

func (r *SubnetStatusReconciler) eventSource() source.TypedSource[SubnetStatusRequest] {
	return source.TypedFunc[SubnetStatusRequest](func(ctx context.Context, q workqueue.TypedRateLimitingInterface[SubnetStatusRequest]) error {
		r.Events.StartSubnetStatusEventBridge(ctx, func(event SubnetStatusEvent) {
			request, err := NewSubnetStatusRequest(event)
			if err == nil {
				q.Add(request)
			}
		})
		return nil
	})
}

// Reconcile consumes one normalized queue key. A deleted Subnet is already
// converged for this read-only path and therefore does not retry.
func (r *SubnetStatusReconciler) Reconcile(ctx context.Context, request SubnetStatusRequest) (ctrl.Result, error) {
	started := time.Now()
	outcome, err := r.observeStatus(ctx, request)
	if err != nil {
		outcome = "error"
		if apierrors.IsConflict(err) {
			outcome = "conflict"
		}
	}
	subnetStatusObservations.WithLabelValues(outcome).Inc()
	subnetStatusObservationDuration.Observe(time.Since(started).Seconds())
	return ctrl.Result{}, err
}

// ProjectStatus reads one current Subnet and invokes the injected projector.
// The returned projection is intentionally handed to the caller; this seam
// does not own the subsequent API write.
func (r *SubnetStatusReconciler) ProjectStatus(ctx context.Context, request SubnetStatusRequest) (SubnetStatusProjection, error) {
	subnet, err := r.readStatusSubnet(ctx, request)
	if err != nil {
		return SubnetStatusProjection{}, err
	}
	return r.projectStatusForSubnet(ctx, subnet)
}

func (r *SubnetStatusReconciler) readStatusSubnet(ctx context.Context, request SubnetStatusRequest) (*kubeovnv1.Subnet, error) {
	if request.Name == "" {
		return nil, ErrSubnetStatusEventNameMissing
	}
	if r.Reader == nil {
		return nil, ErrSubnetStatusReaderMissing
	}
	if r.Projector == nil {
		return nil, ErrSubnetStatusProjectorMissing
	}
	subnet, err := getSubnet(ctx, r.Reader, request.Name, subnetStatusObservationTimeout)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, err
		}
		return nil, fmt.Errorf("get subnet %s: %w", request.Name, err)
	}
	return subnet, nil
}

func (r *SubnetStatusReconciler) projectStatusForSubnet(ctx context.Context, subnet *kubeovnv1.Subnet) (SubnetStatusProjection, error) {
	observation, err := observeSubnet(ctx, r.Reader, subnet)
	if err != nil {
		return SubnetStatusProjection{}, err
	}
	snapshot, err := NewSubnetWriteSnapshot(subnet, observation.AllocationAllowed)
	if err != nil {
		return SubnetStatusProjection{}, err
	}
	projection, err := r.Projector.ProjectSubnetStatus(ctx, snapshot, observation)
	if err != nil {
		return SubnetStatusProjection{}, err
	}
	if err := projection.Snapshot.validateCurrent(subnet); err != nil {
		return SubnetStatusProjection{}, err
	}
	return projection, nil
}
