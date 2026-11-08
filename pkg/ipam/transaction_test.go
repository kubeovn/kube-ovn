package ipam

import (
	"errors"
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
