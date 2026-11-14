package controllerruntime

import (
	"context"
	"errors"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

// SubnetReconciler is the explicit wiring point for the future Subnet writer.
// It is inert until both RecoveryGate and Adapter are supplied, so a manager
// cannot accidentally start a partial writer during the migration.
type SubnetReconciler struct {
	Reader       client.Reader
	RecoveryGate func(context.Context) error
	Adapter      SubnetWriterAdapter
	Lease        SubnetWriterLease
	recoveryMu   sync.Mutex
	recovered    map[subnetRecoveryKey]struct{}
}

type subnetRecoveryKey struct {
	name string
	uid  types.UID
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
		return ctrl.NewControllerManagedBy(mgr).
			Named("kube-ovn-subnet").
			For(&kubeovnv1.Subnet{}).
			Complete(r)
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
