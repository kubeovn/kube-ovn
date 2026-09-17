package client

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMonitorWithTimeoutBreaksReconnectDeadlock(t *testing.T) {
	runMonitor := func(ctx context.Context, rpcMutex *sync.RWMutex, disconnectDone chan struct{}, release <-chan struct{}) error {
		rpcMutex.RLock()
		defer rpcMutex.RUnlock()

		go func() {
			rpcMutex.Lock()
			close(disconnectDone)
			rpcMutex.Unlock()
		}()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}

	legacyDone := make(chan error, 1)
	legacyRelease := make(chan struct{})
	legacyDisconnectDone := make(chan struct{})
	var legacyMutex sync.RWMutex
	go func() {
		legacyDone <- runMonitor(context.Background(), &legacyMutex, legacyDisconnectDone, legacyRelease)
	}()
	require.Never(t, func() bool {
		select {
		case <-legacyDone:
			return true
		default:
			return false
		}
	}, 20*time.Millisecond, time.Millisecond)
	close(legacyRelease)
	require.NoError(t, <-legacyDone)
	require.Eventually(t, func() bool {
		select {
		case <-legacyDisconnectDone:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	disconnectDone := make(chan struct{})
	var rpcMutex sync.RWMutex
	err := monitorWithTimeout(func(ctx context.Context) error {
		return runMonitor(ctx, &rpcMutex, disconnectDone, make(chan struct{}))
	}, 10*time.Millisecond)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Eventually(t, func() bool {
		select {
		case <-disconnectDone:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}
