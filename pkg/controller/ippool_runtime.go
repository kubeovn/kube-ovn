package controller

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type IPPoolOperation uint8

const (
	IPPoolReconcile IPPoolOperation = iota
	IPPoolStatus
	IPPoolDelete
)

// IPPoolRequest keeps statistics separate from membership reconciliation and
// retains the snapshot required for cleanup after a force-delete watch event.
// Snapshot pointer identity preserves the legacy delete queue's deduplication.
type IPPoolRequest struct {
	Name      string
	Operation IPPoolOperation
	Deleted   *kubeovnv1.IPPool
}

// StartIPPoolEventBridge transfers the existing unbounded ingress to the
// manager's queue. Informers may enqueue before recovery or while the manager
// is busy; no event is dropped and retries belong exclusively to the manager.
func (c *Controller) StartIPPoolEventBridge(ctx context.Context, enqueue func(IPPoolRequest)) {
	context.AfterFunc(ctx, func() {
		c.addOrUpdateIPPoolQueue.ShutDown()
		c.updateIPPoolStatusQueue.ShutDown()
		c.deleteIPPoolQueue.ShutDown()
	})
	go forwardIPPoolQueue(ctx, c.addOrUpdateIPPoolQueue, func(name string) {
		enqueue(IPPoolRequest{Name: name, Operation: IPPoolReconcile})
	})
	go forwardIPPoolQueue(ctx, c.updateIPPoolStatusQueue, func(name string) {
		enqueue(IPPoolRequest{Name: name, Operation: IPPoolStatus})
	})
	go forwardIPPoolQueue(ctx, c.deleteIPPoolQueue, func(pool *kubeovnv1.IPPool) {
		enqueue(IPPoolRequest{Name: pool.Name, Operation: IPPoolDelete, Deleted: pool})
	})
}

func forwardIPPoolQueue[T comparable](ctx context.Context, ingress workqueue.TypedRateLimitingInterface[T], enqueue func(T)) {
	for {
		item, shutdown := ingress.Get()
		if shutdown {
			return
		}
		if ctx.Err() == nil {
			enqueue(item)
		}
		ingress.Forget(item)
		ingress.Done(item)
		if ctx.Err() != nil {
			return
		}
	}
}

// ReconcileIPPool is the migration backend. It uses the same IPAM, OVN clients
// and per-pool lock as the legacy workers, with a live API read before dispatch.
func (c *Controller) ReconcileIPPool(ctx context.Context, request IPPoolRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.ippoolKeyMutex.LockKey(request.Name)
	defer func() { _ = c.ippoolKeyMutex.UnlockKey(request.Name) }()

	pool, err := c.config.KubeOvnClient.KubeovnV1().IPPools().Get(ctx, request.Name, metav1.GetOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return c.recordIPPoolKeyError(request.Name, "GetIPPoolFailed", err)
	}
	if request.Operation == IPPoolDelete {
		if request.Deleted == nil || request.Deleted.Name != request.Name {
			return fmt.Errorf("IPPool delete request %q is missing its snapshot", request.Name)
		}
		if k8serrors.IsNotFound(err) {
			pool = request.Deleted.DeepCopy()
			// The API object is gone: perform local/OVN cleanup without trying
			// to patch a finalizer on a missing or subsequently recreated object.
			pool.Finalizers = nil
		} else if pool.UID != request.Deleted.UID || pool.DeletionTimestamp.IsZero() {
			// A delayed old deletion must never clean a same-name replacement.
			return nil
		}
		return c.handleDeleteIPPoolObject(pool)
	}
	if k8serrors.IsNotFound(err) {
		return nil
	}
	if !pool.DeletionTimestamp.IsZero() {
		return c.handleDeleteIPPoolObject(pool)
	}
	switch request.Operation {
	case IPPoolReconcile:
		return c.handleAddOrUpdateIPPoolObject(pool)
	case IPPoolStatus:
		return c.handleUpdateIPPoolStatusObject(pool)
	default:
		return fmt.Errorf("unknown IPPool operation %d", request.Operation)
	}
}

func startControllerRuntime(ctx context.Context, c *Controller, start RuntimeStarter) <-chan struct{} {
	done := make(chan struct{})
	if start == nil {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		if err := start(ctx, c); err != nil && ctx.Err() == nil {
			util.LogFatalAndExit(err, "controller-runtime manager failed")
		}
	}()
	return done
}
