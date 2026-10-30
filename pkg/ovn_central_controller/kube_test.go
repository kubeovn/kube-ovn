package ovn_central_controller

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func testLeaseConfig() *Config {
	return &Config{
		PodName:                "pod-a",
		PodNamespace:           "kube-system",
		BootstrapLeaseDuration: time.Second,
		BootstrapRenewDeadline: 500 * time.Millisecond,
	}
}

func shortenRetryPeriod(t *testing.T) {
	t.Helper()
	old := bootstrapRetryPeriod
	bootstrapRetryPeriod = 50 * time.Millisecond
	t.Cleanup(func() { bootstrapRetryPeriod = old })
}

func TestWithBootstrapLeaseReturnsFnResult(t *testing.T) {
	shortenRetryPeriod(t)
	cfg := testLeaseConfig()

	require.NoError(t, withBootstrapLease(t.Context(), fake.NewClientset(), cfg,
		func(context.Context) error { return nil }))

	want := errors.New("boom")
	err := withBootstrapLease(t.Context(), fake.NewClientset(), cfg,
		func(context.Context) error { return want })
	require.ErrorIs(t, err, want)
	require.NotErrorIs(t, err, errLeaseLost)
}

func TestWithBootstrapLeaseNotAcquiredWhenCtxCancelled(t *testing.T) {
	shortenRetryPeriod(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var ran atomic.Bool
	err := withBootstrapLease(ctx, fake.NewClientset(), testLeaseConfig(),
		func(context.Context) error { ran.Store(true); return nil })
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, ran.Load(), "fn must not run without the lease")
}

// A lost lease must (1) cancel the ctx handed to fn, (2) make
// withBootstrapLease wait for fn to wind down instead of returning while it
// still runs destructive steps, and (3) surface as errLeaseLost -- a
// retryable error that Run (which only treats Canceled as shutdown when its
// own ctx is cancelled) will retry.
func TestWithBootstrapLeaseLostLeaseJoinsCallback(t *testing.T) {
	shortenRetryPeriod(t)
	kc := fake.NewClientset()
	var started atomic.Bool
	kc.PrependReactor("update", "leases", func(ktesting.Action) (bool, runtime.Object, error) {
		// Renewals fail once fn is running; the initial acquire is a create.
		if started.Load() {
			return true, nil, errors.New("apiserver unavailable")
		}
		return false, nil, nil
	})

	var finished atomic.Bool
	err := withBootstrapLease(t.Context(), kc, testLeaseConfig(), func(c context.Context) error {
		started.Store(true)
		select {
		case <-c.Done():
		case <-time.After(10 * time.Second):
			return errors.New("lease ctx was never cancelled")
		}
		time.Sleep(200 * time.Millisecond) // slow wind-down
		finished.Store(true)
		return c.Err()
	})
	require.True(t, finished.Load(), "returned before fn finished")
	require.ErrorIs(t, err, errLeaseLost)
	require.ErrorIs(t, err, errRetry)
}

// The next holder must not start while the previous holder's fn is still
// winding down, even though renewal already failed. client-go's
// ReleaseOnCancel releases the lease inside renew(), before the ctx handed to
// fn is cancelled, so it would let pod-b in mid-fn; we release only after
// joining the callback.
func TestWithBootstrapLeaseLostLeaseExcludesNextHolder(t *testing.T) {
	shortenRetryPeriod(t)
	kc := fake.NewClientset()
	var failRenew atomic.Bool
	kc.PrependReactor("update", "leases", func(a ktesting.Action) (bool, runtime.Object, error) {
		lease, ok := a.(ktesting.UpdateAction).GetObject().(*coordinationv1.Lease)
		if ok && failRenew.Load() && lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity == "pod-a" {
			return true, nil, errors.New("apiserver unavailable")
		}
		return false, nil, nil
	})
	// Long enough that pod-b cannot take over by plain expiry while pod-a
	// is still winding down: only an early release could let it in.
	cfgA := testLeaseConfig()
	cfgA.BootstrapLeaseDuration = 2 * time.Second
	cfgB := *cfgA
	cfgB.PodName = "pod-b"

	var (
		aDone      atomic.Bool
		bSawADone  atomic.Bool
		bRan       atomic.Bool
		wg         sync.WaitGroup
		aStarted   = make(chan struct{})
		bErr, aErr error
	)
	wg.Go(func() {
		aErr = withBootstrapLease(t.Context(), kc, cfgA, func(c context.Context) error {
			failRenew.Store(true)
			close(aStarted)
			<-c.Done()
			time.Sleep(300 * time.Millisecond) // slow wind-down
			aDone.Store(true)
			failRenew.Store(false)
			return c.Err()
		})
	})
	<-aStarted
	wg.Go(func() {
		bErr = withBootstrapLease(t.Context(), kc, &cfgB, func(context.Context) error {
			bRan.Store(true)
			bSawADone.Store(aDone.Load())
			return nil
		})
	})
	wg.Wait()

	require.ErrorIs(t, aErr, errLeaseLost)
	require.NoError(t, bErr)
	require.True(t, bRan.Load())
	require.True(t, bSawADone.Load(), "next holder started while previous fn was still running")
}
