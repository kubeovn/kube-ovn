package ovn_central_controller

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
