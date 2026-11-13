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
	_, _, _, err = allocation.GetRandomAddress("pod", "nic", nil, snapshot.Name, "", nil, true)
	require.NoError(t, err)
	require.Error(t, receipt.Compensate(context.Background()), "receipt must not release an address replaced outside its owner")
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
}

func TestSubnetIPAMAdapterUsesBackendSeam(t *testing.T) {
	writer, _, _, snapshot := newSubnetSideEffectWriterAPI(t, false)
	backend := &fakeSubnetIPAMBackend{}
	adapter, err := newSubnetIPAMAdapterWithBackend(writer, backend)
	require.NoError(t, err)

	v4, _, _, receipt, err := adapter.AllocateRandom(snapshot, "pod", "nic", "pool")
	require.NoError(t, err)
	require.Equal(t, "192.0.2.1", v4)
	require.NotNil(t, receipt)
	require.Equal(t, subnetIPAMKey{subnet: snapshot.Name, pod: "pod", nic: "nic"}, backend.key)
	require.Equal(t, "pool", backend.poolName)
	require.NotNil(t, backend.ctx)
	require.NoError(t, receipt.Compensate(context.Background()))
	require.Equal(t, 1, backend.releaseCalls)
	require.False(t, backend.allocated)
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
	allocateErr  error
	allocated    bool
	ctx          context.Context
	key          subnetIPAMKey
	poolName     string
	releaseCalls int
}

func (b *fakeSubnetIPAMBackend) ExistingAllocation(subnetName, podName, nicName string) (string, string, string, bool) {
	b.key = subnetIPAMKey{subnet: subnetName, pod: podName, nic: nicName}
	if !b.allocated {
		return "", "", "", false
	}
	return "192.0.2.1", "", "00:00:5e:00:53:01", true
}

func (b *fakeSubnetIPAMBackend) AllocateRandom(ctx context.Context, podName, nicName, subnetName, poolName string) (string, string, string, error) {
	b.ctx = ctx
	b.key = subnetIPAMKey{subnet: subnetName, pod: podName, nic: nicName}
	b.poolName = poolName
	if b.allocateErr != nil {
		return "", "", "", b.allocateErr
	}
	b.allocated = true
	return "192.0.2.1", "", "00:00:5e:00:53:01", nil
}

func (b *fakeSubnetIPAMBackend) ReleaseByNic(context.Context, string, string, string) error {
	b.releaseCalls++
	b.allocated = false
	return nil
}
