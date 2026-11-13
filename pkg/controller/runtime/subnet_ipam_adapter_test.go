package controllerruntime

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ipam"
)

func newSubnetIPAMAdapterTest(t *testing.T) (*subnetIPAMAdapter, SubnetWriteSnapshot) {
	t.Helper()
	writer, _, live, snapshot := newSubnetSideEffectWriterAPI(t, false)
	allocation := ipam.NewIPAM()
	require.NoError(t, allocation.AddOrUpdateSubnet(live.Name, "10.0.0.0/29", "10.0.0.7", nil))
	adapter, err := newSubnetIPAMAdapter(writer, allocation)
	require.NoError(t, err)
	return adapter, snapshot
}

func TestSubnetIPAMAdapterAllocatesTransactionAndCompensatesByReceipt(t *testing.T) {
	adapter, snapshot := newSubnetIPAMAdapterTest(t)
	v4, v6, mac, receipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "")
	require.NoError(t, err)
	require.Equal(t, "10.0.0.1", v4)
	require.Empty(t, v6)
	require.NotEmpty(t, mac)
	require.NotNil(t, receipt)

	_, _, _, noOpReceipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "")
	require.NoError(t, err)
	require.Nil(t, noOpReceipt, "existing allocation must not create a second receipt")

	require.NoError(t, receipt.Compensate(context.Background()))
	_, _, _, replacementReceipt, err := adapter.AllocateRandom(snapshot, "other-pod", "other-nic", "")
	require.NoError(t, err)
	require.NotNil(t, replacementReceipt)
	require.Error(t, receipt.Compensate(context.Background()), "receipt replay must be stale")
}

func TestSubnetIPAMAdapterExistingAllocationIsScopedToNIC(t *testing.T) {
	adapter, snapshot := newSubnetIPAMAdapterTest(t)
	firstV4, _, _, firstReceipt, err := adapter.AllocateRandom(snapshot, "pod", "first-nic", "")
	require.NoError(t, err)
	require.NotNil(t, firstReceipt)

	secondV4, _, _, secondReceipt, err := adapter.AllocateRandom(snapshot, "pod", "second-nic", "")
	require.NoError(t, err)
	require.NotNil(t, secondReceipt, "an allocation on another NIC must not be treated as a no-op")
	require.NotEqual(t, firstV4, secondV4)
}

func TestSubnetIPAMAdapterMacOnlyAllocationIsNoOp(t *testing.T) {
	adapter, snapshot := newSubnetIPAMAdapterTest(t)
	macOnlyIPAM := ipam.NewIPAM()
	require.NoError(t, macOnlyIPAM.AddOrUpdateSubnet(snapshot.Name, "", "", nil))
	adapter.backend = &transactionalSubnetIPAMBackend{ipam: macOnlyIPAM}

	_, v6, mac, receipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "")
	require.NoError(t, err)
	require.Empty(t, v6)
	require.NotEmpty(t, mac)
	require.NotNil(t, receipt)

	_, _, secondMAC, secondReceipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "")
	require.NoError(t, err)
	require.Equal(t, mac, secondMAC)
	require.Nil(t, secondReceipt, "existing MAC-only allocation must not create a second receipt")
}

func TestSubnetIPAMAdapterConcurrentSameKeyHasOneOwner(t *testing.T) {
	adapter, snapshot := newSubnetIPAMAdapterTest(t)
	type result struct {
		v4      string
		receipt SubnetSideEffectReceipt
		err     error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			v4, _, _, receipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "")
			results <- result{v4: v4, receipt: receipt, err: err}
		})
	}
	wg.Wait()
	close(results)
	var receipts int
	var address string
	for current := range results {
		require.NoError(t, current.err)
		if current.receipt != nil {
			receipts++
		}
		if address == "" {
			address = current.v4
		} else {
			require.Equal(t, address, current.v4)
		}
	}
	require.Equal(t, 1, receipts)
}

func TestSubnetIPAMAdapterReceiptRejectsReplacedAddress(t *testing.T) {
	adapter, snapshot := newSubnetIPAMAdapterTest(t)
	allocation := adapterIPAM(t, adapter)
	_, _, _, receipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "")
	require.NoError(t, err)
	require.NoError(t, allocation.WithTransaction(func(tx *ipam.Transaction) error {
		return tx.ReleaseAddressByNic("pod", "nic", snapshot.Name)
	}))
	_, _, _, err = allocation.GetRandomAddress("other-pod", "other-nic", nil, snapshot.Name, "", nil, true)
	require.NoError(t, err)
	replacementV4, replacementV6, replacementMAC, err := allocation.GetRandomAddress("pod", "nic", nil, snapshot.Name, "", nil, true)
	require.NoError(t, err)
	require.ErrorIs(t, receipt.Compensate(context.Background()), ipam.ErrAddressOwnershipChanged)
	v4, v6, mac, found := allocation.GetPodAddressByNic("pod", "nic", snapshot.Name)
	require.True(t, found, "ownership mismatch must preserve the replacement allocation")
	require.Equal(t, replacementV4, v4)
	require.Equal(t, replacementV6, v6)
	require.Equal(t, replacementMAC, mac)
}

func TestSubnetIPAMAdapterTransactionFailureDoesNotReturnReceipt(t *testing.T) {
	adapter, snapshot := newSubnetIPAMAdapterTest(t)
	v4, v6, mac, receipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "missing-pool")
	require.Error(t, err)
	require.Empty(t, v4)
	require.Empty(t, v6)
	require.Empty(t, mac)
	require.Nil(t, receipt)

	_, _, _, retryReceipt, retryErr := adapter.AllocateRandom(snapshot, "pod", "nic", "")
	require.NoError(t, retryErr)
	require.NotNil(t, retryReceipt, "failed transaction must leave the address available")
}

func TestSubnetIPAMAdapterCancellationDoesNotAllocate(t *testing.T) {
	adapter, snapshot := newSubnetIPAMAdapterTest(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	adapter.writer.ctx = ctx

	_, _, _, receipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "")
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, receipt)
	_, _, _, allocated := adapterIPAM(t, adapter).GetPodAddressByNic("pod", "nic", snapshot.Name)
	require.False(t, allocated)
}

func TestSubnetIPAMReceiptCancellationPreservesAllocation(t *testing.T) {
	adapter, snapshot := newSubnetIPAMAdapterTest(t)
	_, _, _, receipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, receipt.Compensate(ctx), context.Canceled)
	_, _, _, allocated := adapterIPAM(t, adapter).GetPodAddressByNic("pod", "nic", snapshot.Name)
	require.True(t, allocated)
	require.NoError(t, receipt.Compensate(context.Background()), "a recovered owner must be able to retry after cancellation")
	_, _, _, allocated = adapterIPAM(t, adapter).GetPodAddressByNic("pod", "nic", snapshot.Name)
	require.False(t, allocated)
}

func TestSubnetIPAMAdapterUsesBackendSeam(t *testing.T) {
	writer, _, _, snapshot := newSubnetSideEffectWriterAPI(t, false)
	backend := &fakeSubnetIPAMBackend{}
	adapter, err := newSubnetIPAMAdapterWithBackend(writer, backend)
	require.NoError(t, err)

	v4, v6, mac, receipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "pool")
	require.NoError(t, err)
	require.Equal(t, "192.0.2.1", v4)
	require.Equal(t, "2001:db8::1", v6)
	require.Equal(t, "00:00:5e:00:53:01", mac)
	require.NotNil(t, receipt)
	require.Equal(t, subnetIPAMKey{subnet: snapshot.Name, pod: "pod", nic: "nic"}, backend.key)
	require.Equal(t, "pool", backend.poolName)
	require.NotNil(t, backend.ctx)
	require.NotNil(t, backend.lookupCtx)
	require.NoError(t, receipt.Compensate(context.Background()))
	require.Equal(t, 1, backend.releaseCalls)
	require.Equal(t, subnetIPAMAddress{v4: "192.0.2.1", v6: "2001:db8::1", mac: "00:00:5e:00:53:01"}, backend.releaseAllocation.address)
	require.Equal(t, subnetIPAMKey{subnet: snapshot.Name, pod: "pod", nic: "nic"}, backend.releaseAllocation.key)
	require.NotZero(t, backend.releaseAllocation.token)
	require.Equal(t, string(snapshot.UID), backend.releaseAllocation.uid)
	require.False(t, backend.allocated)
}

func TestSubnetIPAMAdapterRecoversDurableReceipt(t *testing.T) {
	writer, _, _, snapshot := newSubnetSideEffectWriterAPI(t, false)
	backend := &fakeSubnetIPAMBackend{allocated: true, recoverAllocations: []subnetIPAMAllocation{{
		key:     subnetIPAMKey{subnet: snapshot.Name, pod: "pod", nic: "nic"},
		token:   7,
		uid:     string(snapshot.UID),
		address: subnetIPAMAddress{v4: "192.0.2.1", v6: "2001:db8::1", mac: "00:00:5e:00:53:01"},
	}}}
	adapter, err := newSubnetIPAMAdapterWithBackend(writer, backend)
	require.NoError(t, err)

	require.NoError(t, adapter.Recover(t.Context(), snapshot))
	receipt := &subnetIPAMReceipt{adapter: adapter, allocation: backend.recoverAllocations[0]}
	require.NoError(t, receipt.Compensate(t.Context()))
	require.False(t, backend.allocated)
}

func TestSubnetIPAMAdapterRejectsStaleDurableReceipt(t *testing.T) {
	writer, _, _, snapshot := newSubnetSideEffectWriterAPI(t, false)
	backend := &fakeSubnetIPAMBackend{recoverAllocations: []subnetIPAMAllocation{{
		key:     subnetIPAMKey{subnet: snapshot.Name, pod: "pod", nic: "nic"},
		token:   7,
		uid:     "replaced-subnet",
		address: subnetIPAMAddress{v4: "192.0.2.1"},
	}}}
	adapter, err := newSubnetIPAMAdapterWithBackend(writer, backend)
	require.NoError(t, err)

	require.ErrorIs(t, adapter.Recover(t.Context(), snapshot), ErrSubnetIPAMRecoveryUnsafe)
	store := adapter.store.(*memorySubnetIPAMReceiptStore)
	require.Empty(t, store.allocations, "unsafe recovery must not publish partial ownership")
}

func TestSubnetIPAMAdapterRejectsStaleDurableToken(t *testing.T) {
	writer, _, _, snapshot := newSubnetSideEffectWriterAPI(t, false)
	current := subnetIPAMAllocation{
		key:     subnetIPAMKey{subnet: snapshot.Name, pod: "pod", nic: "nic"},
		token:   7,
		uid:     string(snapshot.UID),
		address: subnetIPAMAddress{v4: "192.0.2.1"},
	}
	backend := &fakeSubnetIPAMBackend{recoverAllocations: []subnetIPAMAllocation{{
		key:     current.key,
		token:   8,
		uid:     current.uid,
		address: current.address,
	}}}
	store := &fakeDurableSubnetIPAMReceiptStore{memorySubnetIPAMReceiptStore: newMemorySubnetIPAMReceiptStore()}
	require.NoError(t, store.Remember(t.Context(), current))
	adapter, err := newSubnetIPAMAdapterWithBackendAndStore(writer, backend, store)
	require.NoError(t, err)

	require.ErrorIs(t, adapter.Recover(t.Context(), snapshot), ErrSubnetIPAMRecoveryUnsafe)
}

func TestSubnetIPAMAdapterRejectsInvalidDurableReceiptIdentity(t *testing.T) {
	writer, _, _, snapshot := newSubnetSideEffectWriterAPI(t, false)
	for name, allocation := range map[string]subnetIPAMAllocation{
		"missing pod":     {key: subnetIPAMKey{subnet: snapshot.Name, nic: "nic"}, uid: string(snapshot.UID), token: 1, address: subnetIPAMAddress{v4: "192.0.2.1"}},
		"missing nic":     {key: subnetIPAMKey{subnet: snapshot.Name, pod: "pod"}, uid: string(snapshot.UID), token: 1, address: subnetIPAMAddress{v4: "192.0.2.1"}},
		"missing token":   {key: subnetIPAMKey{subnet: snapshot.Name, pod: "pod", nic: "nic"}, uid: string(snapshot.UID), address: subnetIPAMAddress{v4: "192.0.2.1"}},
		"missing address": {key: subnetIPAMKey{subnet: snapshot.Name, pod: "pod", nic: "nic"}, uid: string(snapshot.UID), token: 1},
	} {
		t.Run(name, func(t *testing.T) {
			backend := &fakeSubnetIPAMBackend{recoverAllocations: []subnetIPAMAllocation{allocation}}
			adapter, err := newSubnetIPAMAdapterWithBackend(writer, backend)
			require.NoError(t, err)
			require.ErrorIs(t, adapter.Recover(t.Context(), snapshot), ErrSubnetIPAMRecoveryUnsafe)
			store := adapter.store.(*memorySubnetIPAMReceiptStore)
			require.Empty(t, store.allocations)
		})
	}
}

func TestSubnetIPAMAdapterReceiptStoreSurvivesAdapterRecreation(t *testing.T) {
	writer, _, _, snapshot := newSubnetSideEffectWriterAPI(t, false)
	allocation := subnetIPAMAllocation{
		key:     subnetIPAMKey{subnet: snapshot.Name, pod: "pod", nic: "nic"},
		token:   7,
		uid:     string(snapshot.UID),
		address: subnetIPAMAddress{v4: "192.0.2.1", v6: "2001:db8::1", mac: "00:00:5e:00:53:01"},
	}
	backend := &fakeSubnetIPAMBackend{allocated: true, recoverAllocations: []subnetIPAMAllocation{allocation}}
	store := &fakeDurableSubnetIPAMReceiptStore{memorySubnetIPAMReceiptStore: newMemorySubnetIPAMReceiptStore()}
	adapter, err := newSubnetIPAMAdapterWithBackendAndStore(writer, backend, store)
	require.NoError(t, err)
	require.NoError(t, adapter.Recover(t.Context(), snapshot))

	restarted, err := newSubnetIPAMAdapterWithBackendAndStore(writer, backend, store)
	require.NoError(t, err)
	require.NoError(t, restarted.Recover(t.Context(), snapshot))
	receipt := &subnetIPAMReceipt{adapter: restarted, allocation: allocation}
	require.NoError(t, receipt.Compensate(t.Context()))
	require.False(t, backend.allocated)
}

func TestSubnetIPAMAdapterStoreFailureReleasesAllocation(t *testing.T) {
	writer, _, _, snapshot := newSubnetSideEffectWriterAPI(t, false)
	backend := &fakeSubnetIPAMBackend{}
	store := &fakeDurableSubnetIPAMReceiptStore{
		memorySubnetIPAMReceiptStore: newMemorySubnetIPAMReceiptStore(),
		rememberErr:                  errors.New("receipt store unavailable"),
	}
	adapter, err := newSubnetIPAMAdapterWithBackendAndStore(writer, backend, store)
	require.NoError(t, err)

	_, _, _, receipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "")
	require.ErrorIs(t, err, store.rememberErr)
	require.Nil(t, receipt)
	require.False(t, backend.allocated)
}

func TestSubnetIPAMAdapterRecoveryStoreFailureRollsBackNewRecords(t *testing.T) {
	writer, _, _, snapshot := newSubnetSideEffectWriterAPI(t, false)
	allocations := []subnetIPAMAllocation{
		{key: subnetIPAMKey{subnet: snapshot.Name, pod: "pod-a", nic: "nic"}, token: 1, uid: string(snapshot.UID), address: subnetIPAMAddress{v4: "192.0.2.1"}},
		{key: subnetIPAMKey{subnet: snapshot.Name, pod: "pod-b", nic: "nic"}, token: 2, uid: string(snapshot.UID), address: subnetIPAMAddress{v4: "192.0.2.2"}},
	}
	backend := &fakeSubnetIPAMBackend{recoverAllocations: allocations}
	store := &fakeDurableSubnetIPAMReceiptStore{memorySubnetIPAMReceiptStore: newMemorySubnetIPAMReceiptStore(), rememberAfter: 2, rememberErr: errors.New("receipt store unavailable")}
	adapter, err := newSubnetIPAMAdapterWithBackendAndStore(writer, backend, store)
	require.NoError(t, err)

	require.ErrorIs(t, adapter.Recover(t.Context(), snapshot), store.rememberErr)
	_, found, err := store.Lookup(t.Context(), allocations[0].key)
	require.NoError(t, err)
	require.False(t, found)
}

// fakeDurableSubnetIPAMReceiptStore keeps state outside an adapter instance to
// model the persistence boundary used by a production API-backed store.
type fakeDurableSubnetIPAMReceiptStore struct {
	*memorySubnetIPAMReceiptStore
	rememberErr   error
	rememberAfter int
	rememberCalls int
}

func (s *fakeDurableSubnetIPAMReceiptStore) Remember(ctx context.Context, allocation subnetIPAMAllocation) error {
	s.rememberCalls++
	if s.rememberErr != nil && (s.rememberAfter == 0 || s.rememberCalls >= s.rememberAfter) {
		return s.rememberErr
	}
	return s.memorySubnetIPAMReceiptStore.Remember(ctx, allocation)
}

func TestSubnetIPAMAdapterFailsClosedWithoutDurableRecovery(t *testing.T) {
	adapter, snapshot := newSubnetIPAMAdapterTest(t)
	require.ErrorIs(t, adapter.Recover(t.Context(), snapshot), ErrSubnetIPAMRecoveryUnavailable)
}

func TestSubnetIPAMAdapterBackendErrorDoesNotReturnReceipt(t *testing.T) {
	writer, _, _, snapshot := newSubnetSideEffectWriterAPI(t, false)
	backend := &fakeSubnetIPAMBackend{allocateErr: errors.New("backend allocation failed")}
	adapter, err := newSubnetIPAMAdapterWithBackend(writer, backend)
	require.NoError(t, err)

	_, _, _, receipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "")
	require.EqualError(t, err, "backend allocation failed")
	require.Nil(t, receipt)
	require.False(t, backend.allocated)
}

func TestSubnetIPAMAdapterLookupErrorDoesNotAllocate(t *testing.T) {
	writer, _, _, snapshot := newSubnetSideEffectWriterAPI(t, false)
	backend := &fakeSubnetIPAMBackend{lookupErr: errors.New("backend lookup failed")}
	adapter, err := newSubnetIPAMAdapterWithBackend(writer, backend)
	require.NoError(t, err)

	_, _, _, receipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "")
	require.EqualError(t, err, "backend lookup failed")
	require.Nil(t, receipt)
	require.False(t, backend.allocated)
}

func TestSubnetIPAMReceiptReleaseErrorPreservesOwnerForRetry(t *testing.T) {
	writer, _, _, snapshot := newSubnetSideEffectWriterAPI(t, false)
	backend := &fakeSubnetIPAMBackend{releaseErr: errors.New("backend release failed")}
	adapter, err := newSubnetIPAMAdapterWithBackend(writer, backend)
	require.NoError(t, err)

	_, _, _, receipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "")
	require.NoError(t, err)
	require.ErrorIs(t, receipt.Compensate(context.Background()), backend.releaseErr)
	require.True(t, backend.allocated)

	backend.releaseErr = nil
	require.NoError(t, receipt.Compensate(context.Background()), "a recovered owner must be able to retry release")
	require.False(t, backend.allocated)
}

func TestSubnetIPAMReceiptReleaseCancellationPreservesOwnerForRetry(t *testing.T) {
	writer, _, _, snapshot := newSubnetSideEffectWriterAPI(t, false)
	backend := &fakeSubnetIPAMBackend{releaseStarted: make(chan struct{})}
	adapter, err := newSubnetIPAMAdapterWithBackend(writer, backend)
	require.NoError(t, err)

	_, _, _, receipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- receipt.Compensate(ctx) }()
	<-backend.releaseStarted
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.True(t, backend.allocated, "canceled release must preserve ownership for recovery")

	backend.releaseStarted = nil
	require.NoError(t, receipt.Compensate(context.Background()))
	require.False(t, backend.allocated)
}

func TestSubnetIPAMAdapterRejectsInvalidDependencies(t *testing.T) {
	writer, _, _, _ := newSubnetSideEffectWriterAPI(t, false)
	_, err := newSubnetIPAMAdapter(nil, ipam.NewIPAM())
	require.Error(t, err)
	_, err = newSubnetIPAMAdapter(writer, nil)
	require.Error(t, err)
	_, err = newSubnetIPAMAdapterWithBackend(writer, nil)
	require.Error(t, err)
}

func adapterIPAM(t *testing.T, adapter *subnetIPAMAdapter) *ipam.IPAM {
	t.Helper()
	backend, ok := adapter.backend.(*transactionalSubnetIPAMBackend)
	require.True(t, ok)
	return backend.ipam
}

type fakeSubnetIPAMBackend struct {
	allocateErr        error
	allocated          bool
	ctx                context.Context
	key                subnetIPAMKey
	lookupCtx          context.Context
	lookupErr          error
	poolName           string
	releaseCalls       int
	releaseErr         error
	releaseAllocation  subnetIPAMAllocation
	releaseStarted     chan struct{}
	recoverAllocations []subnetIPAMAllocation
}

func (b *fakeSubnetIPAMBackend) ExistingAllocation(ctx context.Context, subnetName, podName, nicName string) (subnetIPAMAddress, bool, error) {
	b.lookupCtx = ctx
	b.key = subnetIPAMKey{subnet: subnetName, pod: podName, nic: nicName}
	if b.lookupErr != nil {
		return subnetIPAMAddress{}, false, b.lookupErr
	}
	if !b.allocated {
		return subnetIPAMAddress{}, false, nil
	}
	return subnetIPAMAddress{v4: "192.0.2.1", v6: "2001:db8::1", mac: "00:00:5e:00:53:01"}, true, nil
}

func (b *fakeSubnetIPAMBackend) AllocateRandom(ctx context.Context, podName, nicName, subnetName, poolName string) (subnetIPAMAddress, error) {
	b.ctx = ctx
	b.key = subnetIPAMKey{subnet: subnetName, pod: podName, nic: nicName}
	b.poolName = poolName
	if b.allocateErr != nil {
		return subnetIPAMAddress{}, b.allocateErr
	}
	b.allocated = true
	return subnetIPAMAddress{v4: "192.0.2.1", v6: "2001:db8::1", mac: "00:00:5e:00:53:01"}, nil
}

func (b *fakeSubnetIPAMBackend) Recover(context.Context, SubnetWriteSnapshot) ([]subnetIPAMAllocation, error) {
	return b.recoverAllocations, nil
}

func (b *fakeSubnetIPAMBackend) ReleaseIfMatches(ctx context.Context, allocation subnetIPAMAllocation) error {
	b.releaseCalls++
	b.releaseAllocation = allocation
	if b.releaseStarted != nil {
		close(b.releaseStarted)
		<-ctx.Done()
		return ctx.Err()
	}
	if b.releaseErr != nil {
		return b.releaseErr
	}
	if !b.allocated || allocation.address != (subnetIPAMAddress{v4: "192.0.2.1", v6: "2001:db8::1", mac: "00:00:5e:00:53:01"}) {
		return ipam.ErrAddressOwnershipChanged
	}
	b.allocated = false
	return nil
}
