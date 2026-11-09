package controller

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
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

// beginIPPoolRuntimeRecovery closes the reconcile gate until InitIPAM and the
// startup prerequisite syncs have rebuilt the shared allocation state.
func (c *Controller) beginIPPoolRuntimeRecovery() {
	if c.ipPoolRecoveryReady == nil {
		c.ipPoolRecoveryReady = make(chan struct{})
	}
}

// finishIPPoolRuntimeRecovery transfers the recovered IPAM state to the
// native runtime queue. It is intentionally idempotent for startup error paths
// and tests that exercise the handoff directly.
func (c *Controller) finishIPPoolRuntimeRecovery() {
	if c.ipPoolRecoveryReady == nil {
		return
	}
	c.ipPoolRecoveryOnce.Do(func() { close(c.ipPoolRecoveryReady) })
}

func (c *Controller) waitForIPPoolRuntimeRecovery(ctx context.Context) error {
	if c.ipPoolRecoveryReady == nil {
		return nil
	}
	select {
	case <-c.ipPoolRecoveryReady:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WaitForIPPoolRuntimeRecovery exposes the startup barrier to reconcilers that
// share the runtime manager with IPPool. A nil gate keeps standalone tests and
// other controller-runtime modes unchanged.
func (c *Controller) WaitForIPPoolRuntimeRecovery(ctx context.Context) error {
	return c.waitForIPPoolRuntimeRecovery(ctx)
}

func (c *Controller) ipPoolEventHandler() cache.ResourceEventHandlerFuncs {
	// Retain deletion snapshots through recovery and native cache startup. An
	// object restored into IPAM can disappear before the manager's initial list.
	h := cache.ResourceEventHandlerFuncs{DeleteFunc: c.enqueueDeleteIPPool}
	if !controllerRuntimeIPPoolEnabled(c.config) {
		h.AddFunc = c.enqueueAddIPPool
		h.UpdateFunc = c.enqueueUpdateIPPool
	} else {
		h.UpdateFunc = func(oldObj, newObj any) {
			oldPool := oldObj.(*kubeovnv1.IPPool)
			if oldPool.UID != newObj.(*kubeovnv1.IPPool).UID {
				// Relisting can report a replacement as an update.
				c.enqueueDeleteIPPool(oldPool)
			}
		}
	}
	return h
}

// StartIPPoolEventBridge transfers explicit Subnet membership/statistics
// notifications and legacy deletion snapshots to the manager queue. The delete
// backstop covers objects lost between IPAM recovery and the native initial list.
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
	if err := c.waitForIPPoolRuntimeRecovery(ctx); err != nil {
		return err
	}
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
		switch {
		case k8serrors.IsNotFound(err):
			pool = request.Deleted.DeepCopy()
			// The API object is gone: perform local/OVN cleanup without trying
			// to patch a finalizer on a missing or subsequently recreated object.
			pool.Finalizers = nil
		case pool.UID != request.Deleted.UID:
			return c.reconcileReplacedIPPool(ctx, pool, request.Deleted)
		case pool.DeletionTimestamp.IsZero():
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

// The caller holds the shared name lock. Only the old subnet's IPAM entry is
// orphaned; the globally named address set must first follow the live object.
func (c *Controller) reconcileReplacedIPPool(ctx context.Context, pool, deleted *kubeovnv1.IPPool) error {
	if pool.Spec.Subnet == deleted.Spec.Subnet {
		// The same IPAM entry already belongs to the replacement.
		return nil
	}
	if !pool.DeletionTimestamp.IsZero() {
		if err := c.handleDeleteIPPoolObject(pool); err != nil {
			return err
		}
	} else {
		if err := c.handleAddOrUpdateIPPoolObject(pool); err != nil {
			return err
		}
		// Count-pool reconciliation rereads the API and can return nil when the
		// object disappears or starts terminating, without preparing OVN. Keep
		// the old reservation until a retry can safely prepare or remove it.
		latest, err := c.config.KubeOvnClient.KubeovnV1().IPPools().Get(ctx, pool.Name, metav1.GetOptions{})
		if err != nil {
			return c.recordIPPoolKeyError(pool.Name, "GetIPPoolFailed", err)
		}
		if latest.UID != pool.UID || latest.Generation != pool.Generation || !latest.DeletionTimestamp.IsZero() {
			return fmt.Errorf("IPPool %q changed during replacement reconciliation", pool.Name)
		}
	}
	// Never delete the replacement's address set, finalizers or IPAM entry.
	c.ipam.RemoveIPPool(deleted.Spec.Subnet, deleted.Name)
	return nil
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
