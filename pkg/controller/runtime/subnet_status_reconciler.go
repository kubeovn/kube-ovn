package controllerruntime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
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
	ErrSubnetStatusEventNameMissing     = errors.New("subnet status event name is not configured")
	ErrSubnetStatusEventNotStatus       = errors.New("subnet status event is not status-only")
	ErrSubnetStatusEventSourceMissing   = errors.New("subnet status event source is not configured")
	ErrSubnetStatusProjectorMissing     = errors.New("subnet status projector is not configured")
	ErrSubnetStatusReaderMissing        = errors.New("subnet status API reader is not configured")
	ErrSubnetStatusManagerMissing       = errors.New("subnet status manager is not configured")
	ErrSubnetStatusBridgeContextMissing = errors.New("subnet status event bridge context is not configured")
	ErrSubnetStatusBridgeEnqueueMissing = errors.New("subnet status event bridge enqueue function is not configured")
	ErrSubnetStatusBridgeActive         = errors.New("subnet status event bridge is already active")
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
	StartSubnetStatusEventBridge(context.Context, func(SubnetStatusEvent)) (SubnetStatusEventBridge, error)
}

// SubnetStatusEventBridge owns one subscription lifecycle. Closing it must be
// safe to repeat so controller-runtime restarts cannot leak a legacy watcher.
type SubnetStatusEventBridge interface {
	Close()
}

// LegacySubnetStatusEventSource is the dependency-free callback shape exposed
// by the legacy controller. Keeping this interface on primitive arguments
// avoids an import cycle between the legacy controller and runtime package.
type LegacySubnetStatusEventSource interface {
	SubscribeSubnetStatusEvents(context.Context, func(string, string)) func()
}

type legacySubnetStatusEventSource struct {
	source  LegacySubnetStatusEventSource
	mu      sync.Mutex
	cond    *sync.Cond
	bridge  *legacySubnetStatusEventBridge
	closing bool
}

type legacySubnetStatusEventBridge struct {
	closeOnce sync.Once
	mu        sync.RWMutex
	enqueue   func(SubnetStatusEvent)
	ctx       context.Context
	close     func()
	closed    bool
}

func (b *legacySubnetStatusEventBridge) Close() {
	if b == nil || b.close == nil {
		return
	}
	b.closeOnce.Do(b.close)
}

func (b *legacySubnetStatusEventBridge) emit(event SubnetStatusEvent) {
	b.mu.RLock()
	enqueue := b.enqueue
	b.mu.RUnlock()
	if enqueue != nil {
		enqueue(event)
	}
}

// NewLegacySubnetStatusEventSource adapts the legacy producer fan-out to the
// runtime event vocabulary. It does not consume the legacy status queue.
func NewLegacySubnetStatusEventSource(source LegacySubnetStatusEventSource) SubnetStatusEventSource {
	if source == nil {
		return nil
	}
	adapter := &legacySubnetStatusEventSource{source: source}
	adapter.cond = sync.NewCond(&adapter.mu)
	return adapter
}

func (s *legacySubnetStatusEventSource) StartSubnetStatusEventBridge(ctx context.Context, enqueue func(SubnetStatusEvent)) (SubnetStatusEventBridge, error) {
	if s.source == nil {
		return nil, ErrSubnetStatusEventSourceMissing
	}
	if enqueue == nil {
		return nil, ErrSubnetStatusBridgeEnqueueMissing
	}
	if ctx == nil {
		return nil, ErrSubnetStatusBridgeContextMissing
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.closing {
		s.cond.Wait()
	}
	if s.bridge != nil {
		if !sameSubnetStatusEventContext(s.bridge.ctx, ctx) {
			return nil, ErrSubnetStatusBridgeActive
		}
		return s.bridge, nil
	}
	bridge := &legacySubnetStatusEventBridge{ctx: ctx, enqueue: enqueue}
	unsubscribe := s.source.SubscribeSubnetStatusEvents(ctx, func(name, sourceKind string) {
		bridge.emit(SubnetStatusEvent{Name: name, SourceKind: sourceKind})
	})
	if unsubscribe == nil {
		return nil, ErrSubnetStatusEventSourceMissing
	}
	bridge.close = func() {
		s.mu.Lock()
		if s.bridge != bridge {
			s.mu.Unlock()
			return
		}
		s.closing = true
		s.mu.Unlock()

		bridge.mu.Lock()
		bridge.closed = true
		bridge.enqueue = nil
		bridge.mu.Unlock()
		unsubscribe()

		s.mu.Lock()
		defer s.mu.Unlock()
		if s.bridge == bridge {
			s.bridge = nil
		}
		s.closing = false
		s.cond.Broadcast()
	}
	s.bridge = bridge
	context.AfterFunc(ctx, bridge.Close)
	return bridge, nil
}

func sameSubnetStatusEventContext(left, right context.Context) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	leftValue, rightValue := reflect.ValueOf(left), reflect.ValueOf(right)
	if leftValue.Type() != rightValue.Type() || !leftValue.Type().Comparable() {
		return false
	}
	return leftValue.Interface() == rightValue.Interface()
}

// SubnetStatusReconciler compares a status projection with the current Subnet
// for a normalized status event. Observer mode never writes Kubernetes status
// or invokes finalizer, OVN, or IPAM operations; an optional Writer is used
// only by an explicitly gated writer mode.
type SubnetStatusReconciler struct {
	Reader    client.Reader
	Events    SubnetStatusEventSource
	Projector SubnetStatusProjector
	Writer    SubnetStatusWriter
}

// SubnetStatusWriter is optional in the observer mode and required only by
// the opt-in subnet-writer mode. It keeps status persistence behind the same
// conflict and durable-fence checks as direct Subnet reconciles.
type SubnetStatusWriter interface {
	PatchStatus(*kubeovnv1.Subnet) error
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
		bridge, err := r.Events.StartSubnetStatusEventBridge(ctx, func(event SubnetStatusEvent) {
			request, err := NewSubnetStatusRequest(event)
			if err == nil {
				q.Add(request)
			}
		})
		if err != nil {
			return err
		}
		if bridge == nil {
			return ErrSubnetStatusEventSourceMissing
		}
		context.AfterFunc(ctx, bridge.Close)
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
