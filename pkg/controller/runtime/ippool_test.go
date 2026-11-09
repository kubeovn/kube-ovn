package controllerruntime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
	runtimecontroller "sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/kubeovn/kube-ovn/pkg/controller"
)

type fakeIPPoolBackend struct {
	start     func(context.Context, func(controller.IPPoolRequest))
	reconcile func(context.Context, controller.IPPoolRequest) error
}

type fakeIPPoolEventSource struct {
	start func(context.Context, func(controller.IPPoolRequest))
}

func (s *fakeIPPoolEventSource) StartIPPoolEventBridge(ctx context.Context, enqueue func(controller.IPPoolRequest)) {
	s.start(ctx, enqueue)
}

type fakeIPPoolDomainAdapter struct {
	reconcile func(context.Context, controller.IPPoolRequest) error
}

func (a *fakeIPPoolDomainAdapter) ReconcileIPPool(ctx context.Context, request controller.IPPoolRequest) error {
	return a.reconcile(ctx, request)
}

func (b *fakeIPPoolBackend) StartIPPoolEventBridge(ctx context.Context, enqueue func(controller.IPPoolRequest)) {
	b.start(ctx, enqueue)
}

func (b *fakeIPPoolBackend) ReconcileIPPool(ctx context.Context, request controller.IPPoolRequest) error {
	return b.reconcile(ctx, request)
}

func TestIPPoolRuntimeRetriesFailedOperationAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	received := make(chan controller.IPPoolRequest, 4)
	var attempts atomic.Int32
	full := controller.IPPoolRequest{Name: "pool", Operation: controller.IPPoolReconcile}
	status := controller.IPPoolRequest{Name: "pool", Operation: controller.IPPoolStatus}
	backend := &fakeIPPoolBackend{
		start: func(_ context.Context, enqueue func(controller.IPPoolRequest)) {
			enqueue(full)
			enqueue(status)
		},
		reconcile: func(_ context.Context, request controller.IPPoolRequest) error {
			received <- request
			if request.Operation == controller.IPPoolStatus && attempts.Add(1) == 1 {
				return errors.New("transient API failure")
			}
			return nil
		},
	}
	r := &IPPoolReconciler{Events: backend, Adapter: backend}
	c, err := runtimecontroller.NewTypedUnmanaged("ippool-runtime-retry-test", runtimecontroller.TypedOptions[controller.IPPoolRequest]{
		Reconciler: r,
		Logger:     logr.Discard(),
		RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[controller.IPPoolRequest](
			time.Millisecond, 10*time.Millisecond,
		),
	})
	require.NoError(t, err)
	require.NoError(t, c.Watch(r.eventSource()))
	done := make(chan error, 1)
	go func() { done <- c.Start(ctx) }()
	operations := make(map[controller.IPPoolOperation]int)
	for range 3 {
		select {
		case request := <-received:
			require.Equal(t, "pool", request.Name)
			operations[request.Operation]++
		case <-time.After(5 * time.Second):
			t.Fatal("runtime did not retry the failed operation")
		}
	}
	require.Equal(t, 1, operations[controller.IPPoolReconcile])
	require.Equal(t, 2, operations[controller.IPPoolStatus])
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("runtime controller did not stop with the leader context")
	}
}

func TestIPPoolRuntimeUsesSeparateEventSourceAndDomainAdapter(t *testing.T) {
	request := controller.IPPoolRequest{Name: "pool", Operation: controller.IPPoolStatus}
	var received controller.IPPoolRequest
	events := &fakeIPPoolEventSource{start: func(_ context.Context, enqueue func(controller.IPPoolRequest)) {
		enqueue(request)
	}}
	adapter := &fakeIPPoolDomainAdapter{reconcile: func(_ context.Context, got controller.IPPoolRequest) error {
		received = got
		return nil
	}}
	reconciler := &IPPoolReconciler{Events: events, Adapter: adapter}

	_, err := reconciler.Reconcile(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, request, received)

	queue := workqueue.NewTypedRateLimitingQueue[controller.IPPoolRequest](workqueue.DefaultTypedControllerRateLimiter[controller.IPPoolRequest]())
	t.Cleanup(queue.ShutDown)
	require.NoError(t, reconciler.eventSource().Start(t.Context(), queue))
	queued, shutdown := queue.Get()
	require.False(t, shutdown)
	require.Equal(t, request, queued)
	queue.Done(queued)
	queue.Forget(queued)
}

func TestIPPoolRuntimeRejectsMissingSharedBackend(t *testing.T) {
	config := &controller.Configuration{
		KubeRestConfig:        &rest.Config{Host: "https://127.0.0.1"},
		ControllerRuntimeMode: controller.ControllerRuntimeModeIPPool,
	}
	require.EqualError(t, Run(t.Context(), config), "IPPool runtime mode requires an initialized controller backend")
	require.EqualError(t, RunWithIPPool(t.Context(), config, nil), "IPPool runtime mode requires an initialized controller backend")
}
