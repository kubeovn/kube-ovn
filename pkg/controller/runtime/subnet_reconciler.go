package controllerruntime

import (
	"context"
	"errors"
	"slices"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

var (
	ErrSubnetWriterAdapterMissing  = errors.New("subnet writer adapter is not configured")
	ErrSubnetWriterManagerMissing  = errors.New("subnet writer manager is not configured")
	ErrSubnetWriterRecoveryMissing = errors.New("subnet writer recovery gate is not configured")
)

// SubnetWriterAdapter owns the domain mutations for one Subnet. The
// reconciler only supplies an immutable snapshot and cached dependency
// observation; it never becomes a second legacy Subnet writer by itself.
type SubnetWriterAdapter interface {
	RecoverSubnet(context.Context, SubnetWriteSnapshot) error
	ReconcileSubnet(context.Context, SubnetWriteSnapshot, SubnetShadowObservation) error
}

// LegacySubnetRuntimeEventSource adapts dependency changes that are still
// observed by the legacy controller into runtime reconcile requests.
type LegacySubnetRuntimeEventSource interface {
	SubscribeSubnetRuntimeEvents(context.Context, func(string)) func()
}

// SubnetReconciler is the explicit wiring point for the future Subnet writer.
// It is inert until both RecoveryGate and Adapter are supplied, so a manager
// cannot accidentally start a partial writer during the migration.
type SubnetReconciler struct {
	Reader       client.Reader
	RecoveryGate func(context.Context) error
	Adapter      SubnetWriterAdapter
	Lease        SubnetWriterLease
	Events       LegacySubnetRuntimeEventSource
	// Fence is checked before recovery and before the adapter can perform any
	// status, finalizer, OVN, or IPAM side effect.
	Fence      SubnetWriterFence
	recoveryMu sync.Mutex
	recovered  map[subnetRecoveryKey]struct{}
}

type subnetRecoveryKey struct {
	name string
	uid  types.UID
}

func subnetSpecOrLifecycleChanged(e event.UpdateEvent) bool {
	oldSubnet, oldOK := e.ObjectOld.(*kubeovnv1.Subnet)
	newSubnet, newOK := e.ObjectNew.(*kubeovnv1.Subnet)
	if !oldOK || !newOK {
		return false
	}
	return oldSubnet.Generation != newSubnet.Generation ||
		oldSubnet.DeletionTimestamp.IsZero() != newSubnet.DeletionTimestamp.IsZero() ||
		!slices.Equal(oldSubnet.Finalizers, newSubnet.Finalizers)
}

// SetupSubnetReconciler is the explicit manager wiring entry point. Callers
// must provide both the recovery gate and the domain adapter; the default
// manager path deliberately does not call this function during migration.
func SetupSubnetReconciler(mgr ctrl.Manager, reader client.Reader, recoveryGate func(context.Context) error, adapter SubnetWriterAdapter, lease SubnetWriterLease) error {
	return (&SubnetReconciler{Reader: reader, RecoveryGate: recoveryGate, Adapter: adapter, Lease: lease}).SetupWithManager(mgr)
}

func (r *SubnetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.RecoveryGate == nil {
		return ErrSubnetWriterRecoveryMissing
	}
	if r.Adapter == nil {
		return ErrSubnetWriterAdapterMissing
	}
	if mgr == nil {
		return ErrSubnetWriterManagerMissing
	}
	return r.Lease.WithActive(context.Background(), func(context.Context) error {
		if r.Reader == nil {
			r.Reader = mgr.GetAPIReader()
		}
		updates := predicate.Funcs{
			CreateFunc:  func(event.CreateEvent) bool { return true },
			UpdateFunc:  subnetSpecOrLifecycleChanged,
			DeleteFunc:  func(event.DeleteEvent) bool { return true },
			GenericFunc: func(event.GenericEvent) bool { return true },
		}
		builder := ctrl.NewControllerManagedBy(mgr).
			Named("kube-ovn-subnet").
			For(&kubeovnv1.Subnet{}, builder.WithPredicates(updates))
		if r.Events != nil {
			builder.WatchesRawSource(source.TypedFunc[ctrl.Request](func(ctx context.Context, q workqueue.TypedRateLimitingInterface[ctrl.Request]) error {
				unsubscribe := r.Events.SubscribeSubnetRuntimeEvents(ctx, func(name string) {
					if name != "" {
						q.Add(ctrl.Request{Name: name})
					}
				})
				context.AfterFunc(ctx, unsubscribe)
				return nil
			}))
		}
		return builder.Complete(r)
	})
}

func (r *SubnetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if r.RecoveryGate == nil {
		return ctrl.Result{}, ErrSubnetWriterRecoveryMissing
	}
	if r.Adapter == nil {
		return ctrl.Result{}, ErrSubnetWriterAdapterMissing
	}
	var result ctrl.Result
	err := r.Lease.WithActive(ctx, func(ctx context.Context) error {
		if r.Fence != nil {
			if err := r.Fence.Check(ctx); err != nil {
				return err
			}
		}
		if r.Reader == nil {
			return errors.New("subnet writer API reader is not configured")
		}
		if err := r.RecoveryGate(ctx); err != nil {
			return err
		}
		var subnet kubeovnv1.Subnet
		if err := r.Reader.Get(ctx, types.NamespacedName{Name: req.Name}, &subnet); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		observation, err := observeSubnet(ctx, r.Reader, &subnet)
		if err != nil {
			return err
		}
		snapshot, err := NewSubnetWriteSnapshot(&subnet, observation.AllocationAllowed)
		if err != nil {
			return err
		}
		if err := r.recoverSubnet(ctx, snapshot); err != nil {
			return err
		}
		return r.Adapter.ReconcileSubnet(ctx, snapshot, observation)
	})
	return result, err
}

func (r *SubnetReconciler) recoverSubnet(ctx context.Context, snapshot SubnetWriteSnapshot) error {
	key := subnetRecoveryKey{name: snapshot.Name, uid: snapshot.UID}
	r.recoveryMu.Lock()
	defer r.recoveryMu.Unlock()
	if r.recovered == nil {
		r.recovered = make(map[subnetRecoveryKey]struct{})
	}
	if _, ok := r.recovered[key]; ok {
		return nil
	}
	if err := r.Adapter.RecoverSubnet(ctx, snapshot); err != nil {
		return err
	}
	r.recovered[key] = struct{}{}
	return nil
}
