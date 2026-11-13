package ipam

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWithTransactionCommitsAllocation(t *testing.T) {
	ipam := newTransactionIPAM(t)

	var allocated string
	require.NoError(t, ipam.WithTransaction(func(tx *Transaction) error {
		var err error
		allocated, _, _, err = tx.GetRandomAddress("pod", "nic", nil, "subnet", "", nil, true)
		return err
	}))
	require.Equal(t, "10.0.0.1", allocated)

	_, _, _, err := ipam.GetStaticAddress("other", "other-nic", allocated, nil, "subnet", true)
	require.ErrorIs(t, err, ErrConflict)
}

func TestWithTransactionRollsBackAllocation(t *testing.T) {
	ipam := newTransactionIPAM(t)
	failure := errors.New("persist IP resource")

	require.ErrorIs(t, ipam.WithTransaction(func(tx *Transaction) error {
		_, _, _, err := tx.GetRandomAddress("pod", "nic", nil, "subnet", "", nil, true)
		require.NoError(t, err)
		return failure
	}), failure)

	address, _, _, err := ipam.GetRandomAddress("retry", "retry-nic", nil, "subnet", "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.1", address)
}

func TestWithTransactionRollsBackStaticRebind(t *testing.T) {
	ipam := newTransactionIPAM(t)
	_, _, _, err := ipam.GetStaticAddress("pod", "nic", "10.0.0.1", nil, "subnet", true)
	require.NoError(t, err)

	failure := errors.New("persist rebind")
	require.ErrorIs(t, ipam.WithTransaction(func(tx *Transaction) error {
		_, _, _, err := tx.GetStaticAddress("pod", "nic", "10.0.0.2", nil, "subnet", true)
		require.NoError(t, err)
		return failure
	}), failure)

	_, _, _, err = ipam.GetStaticAddress("other", "other-nic", "10.0.0.1", nil, "subnet", true)
	require.ErrorIs(t, err, ErrConflict)
	_, _, _, err = ipam.GetStaticAddress("other", "other-nic", "10.0.0.2", nil, "subnet", true)
	require.NoError(t, err)
}

func TestWithTransactionRollsBackRelease(t *testing.T) {
	ipam := newTransactionIPAM(t)
	_, _, _, err := ipam.GetStaticAddress("pod", "nic", "10.0.0.1", nil, "subnet", true)
	require.NoError(t, err)

	failure := errors.New("persist release")
	require.ErrorIs(t, ipam.WithTransaction(func(tx *Transaction) error {
		require.NoError(t, tx.ReleaseAddressByNic("pod", "nic", "subnet"))
		return failure
	}), failure)
	_, _, _, err = ipam.GetStaticAddress("other", "other-nic", "10.0.0.1", nil, "subnet", true)
	require.ErrorIs(t, err, ErrConflict)

	require.NoError(t, ipam.WithTransaction(func(tx *Transaction) error {
		return tx.ReleaseAddressByNic("pod", "nic", "subnet")
	}))
	_, _, _, err = ipam.GetStaticAddress("other", "other-nic", "10.0.0.1", nil, "subnet", true)
	require.NoError(t, err)
}

func TestWithTransactionRollsBackDualStackAllocation(t *testing.T) {
	ipam := newTransactionIPAMWithCIDR(t, "10.0.0.0/29,fd00::/125")
	failure := errors.New("persist dual-stack address")

	require.ErrorIs(t, ipam.WithTransaction(func(tx *Transaction) error {
		_, _, _, err := tx.GetRandomAddress("pod", "nic", nil, "subnet", "", nil, true)
		require.NoError(t, err)
		return failure
	}), failure)

	v4, v6, _, err := ipam.GetRandomAddress("retry", "retry-nic", nil, "subnet", "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.1", v4)
	require.Equal(t, "fd00::1", v6)
}

func TestWithTransactionPanicRollsBackAndCloses(t *testing.T) {
	ipam := newTransactionIPAM(t)
	var tx *Transaction

	require.Panics(t, func() {
		_ = ipam.WithTransaction(func(current *Transaction) error {
			tx = current
			_, _, _, err := tx.GetRandomAddress("pod", "nic", nil, "subnet", "", nil, true)
			require.NoError(t, err)
			panic("persist panic")
		})
	})

	address, _, _, err := ipam.GetRandomAddress("retry", "retry-nic", nil, "subnet", "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.1", address)
	_, _, _, err = tx.GetRandomAddress("after-panic", "after-panic-nic", nil, "subnet", "", nil, true)
	require.ErrorIs(t, err, ErrTransactionClosed)
}

func TestTransactionCannotBeUsedAfterAction(t *testing.T) {
	ipam := newTransactionIPAM(t)
	var tx *Transaction
	require.NoError(t, ipam.WithTransaction(func(current *Transaction) error {
		tx = current
		return nil
	}))

	_, _, _, err := tx.GetRandomAddress("pod", "nic", nil, "subnet", "", nil, true)
	require.ErrorIs(t, err, ErrTransactionClosed)
}

func TestWithTransactionSerializesConcurrentAllocation(t *testing.T) {
	ipam := newTransactionIPAM(t)
	started := make(chan struct{})
	release := make(chan struct{})
	transactionDone := make(chan error, 1)
	go func() {
		transactionDone <- ipam.WithTransaction(func(tx *Transaction) error {
			close(started)
			<-release
			_, _, _, err := tx.GetRandomAddress("transaction-pod", "transaction-nic", nil, "subnet", "", nil, true)
			return err
		})
	}()
	<-started

	allocationDone := make(chan error, 1)
	var allocated string
	go func() {
		var err error
		allocated, _, _, err = ipam.GetRandomAddress("concurrent-pod", "concurrent-nic", nil, "subnet", "", nil, true)
		allocationDone <- err
	}()
	select {
	case err := <-allocationDone:
		t.Fatalf("concurrent allocation completed before transaction: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-transactionDone)
	require.NoError(t, <-allocationDone)
	require.Equal(t, "10.0.0.2", allocated)
}

func TestWithTransactionRejectsNilAction(t *testing.T) {
	ipam := NewIPAM()
	require.ErrorIs(t, ipam.WithTransaction(nil), ErrTransactionNilAction)
}

func TestWithTransactionContextRejectsCancellationBeforeAction(t *testing.T) {
	ipam := newTransactionIPAM(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	require.ErrorIs(t, ipam.WithTransactionContext(ctx, func(*Transaction) error {
		called = true
		return nil
	}), context.Canceled)
	require.False(t, called)

	address, _, _, err := ipam.GetRandomAddress("retry", "retry-nic", nil, "subnet", "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.1", address)
}

func TestWithTransactionContextChecksCancellationAfterLockAcquisition(t *testing.T) {
	ipam := newTransactionIPAM(t)

	ctx := &firstErrContext{
		Context:      context.Background(),
		firstErr:     make(chan struct{}),
		secondErr:    make(chan struct{}),
		releaseCheck: make(chan struct{}),
	}
	called := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- ipam.WithTransactionContext(ctx, func(*Transaction) error {
			close(called)
			return nil
		})
	}()
	<-ctx.firstErr
	select {
	case <-ctx.secondErr:
	case <-time.After(time.Second):
		t.Fatal("transaction did not perform the lock-acquired context check")
	}
	close(ctx.releaseCheck)
	require.ErrorIs(t, <-done, context.Canceled)
	select {
	case <-called:
		t.Fatal("transaction action ran after cancellation while waiting for the IPAM lock")
	default:
	}
}

type firstErrContext struct {
	context.Context
	firstErr     chan struct{}
	secondErr    chan struct{}
	releaseCheck chan struct{}
	calls        atomic.Uint32
}

func (c *firstErrContext) Err() error {
	switch c.calls.Add(1) {
	case 1:
		close(c.firstErr)
		return nil
	case 2:
		close(c.secondErr)
		<-c.releaseCheck
		return context.Canceled
	default:
		return context.Canceled
	}
}

func TestWithTransactionContextRollsBackActionCancellation(t *testing.T) {
	ipam := newTransactionIPAM(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	require.ErrorIs(t, ipam.WithTransactionContext(ctx, func(tx *Transaction) error {
		_, _, _, err := tx.GetRandomAddress("pod", "nic", nil, "subnet", "", nil, true)
		require.NoError(t, err)
		cancel()
		return ctx.Err()
	}), context.Canceled)

	address, _, _, err := ipam.GetRandomAddress("retry", "retry-nic", nil, "subnet", "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.1", address)
}

func TestWithTransactionContextReportsCancellationAfterCommit(t *testing.T) {
	ipam := newTransactionIPAM(t)
	ctx, cancel := context.WithCancel(t.Context())

	require.ErrorIs(t, ipam.WithTransactionContext(ctx, func(tx *Transaction) error {
		_, _, _, err := tx.GetRandomAddress("pod", "nic", nil, "subnet", "", nil, true)
		require.NoError(t, err)
		cancel()
		return nil
	}), context.Canceled)

	address, _, _, err := ipam.GetRandomAddress("retry", "retry-nic", nil, "subnet", "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.2", address, "cancellation after the action reports an uncertain committed result")
}

func TestTransactionGetPodAddressByNicReadsSnapshot(t *testing.T) {
	ipam := newTransactionIPAM(t)
	_, _, _, err := ipam.GetRandomAddress("pod", "nic", nil, "subnet", "", nil, true)
	require.NoError(t, err)

	require.NoError(t, ipam.WithTransaction(func(tx *Transaction) error {
		v4, v6, mac, found, err := tx.GetPodAddressByNic("pod", "nic", "subnet")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, "10.0.0.1", v4)
		require.Empty(t, v6)
		require.NotEmpty(t, mac)
		return nil
	}))
}

func newTransactionIPAM(t *testing.T) *IPAM {
	t.Helper()
	return newTransactionIPAMWithCIDR(t, "10.0.0.0/29")
}

func newTransactionIPAMWithCIDR(t *testing.T, cidr string) *IPAM {
	t.Helper()
	ipam := NewIPAM()
	require.NoError(t, ipam.AddOrUpdateSubnet("subnet", cidr, gatewayForCIDR(cidr), nil))
	return ipam
}

func gatewayForCIDR(cidr string) string {
	if cidr == "10.0.0.0/29,fd00::/125" {
		return "10.0.0.7,fd00::7"
	}
	return "10.0.0.7"
}
