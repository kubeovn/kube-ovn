package controllerruntime

import (
	"context"
	"fmt"

	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	runtimecontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/kubeovn/kube-ovn/pkg/controller"
)

type ipPoolBackend interface {
	StartIPPoolEventBridge(context.Context, func(controller.IPPoolRequest))
	ReconcileIPPool(context.Context, controller.IPPoolRequest) error
}

// IPPoolReconciler migrates scheduling and retries while keeping the initialized
// legacy controller as the only owner of allocation and data-plane operations.
type IPPoolReconciler struct {
	Backend ipPoolBackend
}

func (r *IPPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c, err := runtimecontroller.NewTyped("kube-ovn-ippool", mgr, runtimecontroller.TypedOptions[controller.IPPoolRequest]{
		Reconciler: r,
		// Operations on the same pool share a key mutex in the backend. Start
		// with one worker while the legacy business handlers are being reused.
		MaxConcurrentReconciles: 1,
		RateLimiter:             workqueue.DefaultTypedControllerRateLimiter[controller.IPPoolRequest](),
	})
	if err != nil {
		return fmt.Errorf("create IPPool controller: %w", err)
	}
	return c.Watch(r.eventSource())
}

func (r *IPPoolReconciler) eventSource() source.TypedSource[controller.IPPoolRequest] {
	return source.TypedFunc[controller.IPPoolRequest](func(ctx context.Context, q workqueue.TypedRateLimitingInterface[controller.IPPoolRequest]) error {
		r.Backend.StartIPPoolEventBridge(ctx, q.Add)
		return nil
	})
}

func (r *IPPoolReconciler) Reconcile(ctx context.Context, request controller.IPPoolRequest) (ctrl.Result, error) {
	return ctrl.Result{}, r.Backend.ReconcileIPPool(ctx, request)
}
