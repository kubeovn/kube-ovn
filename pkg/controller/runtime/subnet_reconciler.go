package controllerruntime

import (
	"context"
	"errors"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

var (
	ErrSubnetWriterAdapterMissing  = errors.New("subnet writer adapter is not configured")
	ErrSubnetWriterRecoveryMissing = errors.New("subnet writer recovery gate is not configured")
)

// SubnetWriterAdapter owns the domain mutations for one Subnet. The
// reconciler only supplies an immutable snapshot and cached dependency
// observation; it never becomes a second legacy Subnet writer by itself.
type SubnetWriterAdapter interface {
	ReconcileSubnet(context.Context, SubnetWriteSnapshot, SubnetShadowObservation) error
}

// SubnetReconciler is the explicit wiring point for the future Subnet writer.
// It is inert until both RecoveryGate and Adapter are supplied, so a manager
// cannot accidentally start a partial writer during the migration.
type SubnetReconciler struct {
	Reader       client.Reader
	RecoveryGate func(context.Context) error
	Adapter      SubnetWriterAdapter
}

func (r *SubnetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.RecoveryGate == nil {
		return ErrSubnetWriterRecoveryMissing
	}
	if r.Adapter == nil {
		return ErrSubnetWriterAdapterMissing
	}
	if r.Reader == nil {
		r.Reader = mgr.GetAPIReader()
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("kube-ovn-subnet").
		For(&kubeovnv1.Subnet{}).
		Complete(r)
}

func (r *SubnetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if r.RecoveryGate == nil {
		return ctrl.Result{}, ErrSubnetWriterRecoveryMissing
	}
	if r.Adapter == nil {
		return ctrl.Result{}, ErrSubnetWriterAdapterMissing
	}
	if r.Reader == nil {
		return ctrl.Result{}, errors.New("subnet writer API reader is not configured")
	}
	if err := r.RecoveryGate(ctx); err != nil {
		return ctrl.Result{}, err
	}
	var subnet kubeovnv1.Subnet
	if err := r.Reader.Get(ctx, types.NamespacedName{Name: req.Name}, &subnet); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	observation, err := ObserveSubnet(ctx, r.Reader, req.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	snapshot, err := NewSubnetWriteSnapshot(&subnet, observation.AllocationAllowed)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, r.Adapter.ReconcileSubnet(ctx, snapshot, observation)
}
