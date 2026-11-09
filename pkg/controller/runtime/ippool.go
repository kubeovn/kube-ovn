package controllerruntime

import (
	"context"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	runtimecontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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
	if err := c.Watch(source.TypedKind(mgr.GetCache(), &kubeovnv1.IPPool{}, ipPoolEventHandler())); err != nil {
		return fmt.Errorf("watch IPPools: %w", err)
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

// Only membership-affecting updates run the full backend. Our own statistics,
// conditions and finalizer writes must not trigger allocation or a status loop.
func ipPoolEventHandler() handler.TypedEventHandler[*kubeovnv1.IPPool, controller.IPPoolRequest] {
	return handler.TypedFuncs[*kubeovnv1.IPPool, controller.IPPoolRequest]{
		CreateFunc: func(_ context.Context, e event.TypedCreateEvent[*kubeovnv1.IPPool], q workqueue.TypedRateLimitingInterface[controller.IPPoolRequest]) {
			enqueueIPPool(q, e.Object, false)
		},
		UpdateFunc: func(_ context.Context, e event.TypedUpdateEvent[*kubeovnv1.IPPool], q workqueue.TypedRateLimitingInterface[controller.IPPoolRequest]) {
			if e.ObjectOld == nil || e.ObjectNew == nil {
				return
			}
			if e.ObjectOld.UID != e.ObjectNew.UID {
				// A relist can replace an object without a separate delete event.
				enqueueIPPool(q, e.ObjectOld, true)
				enqueueIPPool(q, e.ObjectNew, false)
				return
			}
			if !e.ObjectNew.DeletionTimestamp.IsZero() ||
				!equality.Semantic.DeepEqual(e.ObjectOld.Spec, e.ObjectNew.Spec) ||
				!slices.Equal(e.ObjectOld.Status.IPs, e.ObjectNew.Status.IPs) {
				enqueueIPPool(q, e.ObjectNew, false)
			}
		},
		DeleteFunc: func(_ context.Context, e event.TypedDeleteEvent[*kubeovnv1.IPPool], q workqueue.TypedRateLimitingInterface[controller.IPPoolRequest]) {
			enqueueIPPool(q, e.Object, true)
		},
	}
}

func enqueueIPPool(q workqueue.TypedRateLimitingInterface[controller.IPPoolRequest], pool *kubeovnv1.IPPool, deleted bool) {
	if pool == nil {
		return
	}
	request := controller.IPPoolRequest{Name: pool.Name, Operation: controller.IPPoolReconcile}
	if deleted || !pool.DeletionTimestamp.IsZero() {
		// The cache owns the event object. Retain an independent snapshot across
		// retries, even if a same-name replacement appears before cleanup runs.
		request.Operation = controller.IPPoolDelete
		request.Deleted = pool.DeepCopy()
	}
	q.Add(request)
}
