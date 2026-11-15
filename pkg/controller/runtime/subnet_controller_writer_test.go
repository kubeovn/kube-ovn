package controllerruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	legacycontroller "github.com/kubeovn/kube-ovn/pkg/controller"
	"github.com/kubeovn/kube-ovn/pkg/internal"
)

type recordingSubnetDataPlaneBackend struct {
	recover   int
	reconcile int
	deleting  bool
	allowed   bool
	last      *kubeovnv1.Subnet
}

type legacySubnetDataPlaneBackend struct {
	reconcile int
}

func (b *legacySubnetDataPlaneBackend) SetSubnetRuntimeWriters(
	legacycontroller.SubnetRuntimeStatusWriter,
	legacycontroller.SubnetRuntimeIPStatusWriter,
	legacycontroller.SubnetRuntimeGatewayMACWriter,
	legacycontroller.SubnetRuntimeFinalizerWriter,
	legacycontroller.SubnetRuntimeDomainWriter,
	legacycontroller.SubnetRuntimeDomainWriter,
) error {
	return nil
}

func (b *legacySubnetDataPlaneBackend) SetSubnetRuntimeVirtualPortWriter(legacycontroller.SubnetRuntimeVirtualPortWriter) error {
	return nil
}

func (b *legacySubnetDataPlaneBackend) SetSubnetRuntimeIPAMMutationWriter(legacycontroller.SubnetRuntimeIPAMMutationWriter) error {
	return nil
}

type recordingSubnetIPAMAddressBackend struct {
	existingResult  legacycontroller.SubnetRuntimeIPAMAddressResult
	existingFound   bool
	existingErr     error
	allocateResult  legacycontroller.SubnetRuntimeIPAMAddressResult
	allocateErr     error
	lastPodName     string
	lastNICName     string
	lastSubnetName  string
	lastRequestedIP string
	lastMAC         *string
	lastPoolName    string
	lastSkipped     []string
	lastConflict    bool
	releaseV4       string
	releaseV6       string
	releaseMAC      string
	releaseCalls    int
	releasePodCalls int
}

func (b *recordingSubnetIPAMAddressBackend) ExistingSubnetRuntimeIPAMAddress(context.Context, string, string, string) (legacycontroller.SubnetRuntimeIPAMAddressResult, bool, error) {
	return b.existingResult, b.existingFound, b.existingErr
}

func (b *recordingSubnetIPAMAddressBackend) ListSubnetRuntimeIPAMAddresses(context.Context, string) ([]legacycontroller.SubnetRuntimeIPAMAddressAllocation, error) {
	return nil, nil
}

func (b *recordingSubnetIPAMAddressBackend) BindSubnetRuntimeIPAMIdentity(context.Context, string, string) error {
	return nil
}

func (b *recordingSubnetIPAMAddressBackend) AllocateSubnetRuntimeIPAMRandom(_ context.Context, podName, nicName, subnetName, poolName string, skipped []string, checkConflict bool) (legacycontroller.SubnetRuntimeIPAMAddressResult, error) {
	b.lastPodName = podName
	b.lastNICName = nicName
	b.lastSubnetName = subnetName
	b.lastPoolName = poolName
	b.lastSkipped = append([]string(nil), skipped...)
	b.lastConflict = checkConflict
	return b.allocateResult, b.allocateErr
}

func (b *recordingSubnetIPAMAddressBackend) AllocateSubnetRuntimeIPAMStatic(_ context.Context, podName, nicName, requestedIP string, mac *string, subnetName string, checkConflict bool) (legacycontroller.SubnetRuntimeIPAMAddressResult, error) {
	b.lastPodName = podName
	b.lastNICName = nicName
	b.lastSubnetName = subnetName
	b.lastRequestedIP = requestedIP
	b.lastMAC = mac
	b.lastConflict = checkConflict
	return b.allocateResult, b.allocateErr
}

func (b *recordingSubnetIPAMAddressBackend) ReleaseSubnetRuntimeIPAMByNicIfMatches(_ context.Context, podName, nicName, subnetName, expectedV4, expectedV6, expectedMAC string) error {
	b.lastPodName = podName
	b.lastNICName = nicName
	b.lastSubnetName = subnetName
	b.releaseV4 = expectedV4
	b.releaseV6 = expectedV6
	b.releaseMAC = expectedMAC
	b.releaseCalls++
	return nil
}

func (b *recordingSubnetIPAMAddressBackend) ReleaseSubnetRuntimeIPAMByPod(_ context.Context, podName, subnetName string) error {
	b.lastPodName = podName
	b.lastSubnetName = subnetName
	b.releasePodCalls++
	return nil
}

type installingSubnetDataPlaneBackend struct {
	status           legacycontroller.SubnetRuntimeStatusWriter
	ipStatus         legacycontroller.SubnetRuntimeIPStatusWriter
	gatewayMAC       legacycontroller.SubnetRuntimeGatewayMACWriter
	finalizer        legacycontroller.SubnetRuntimeFinalizerWriter
	ovn              legacycontroller.SubnetRuntimeDomainWriter
	ovnMutation      legacycontroller.SubnetRuntimeOVNMutationWriter
	ipam             legacycontroller.SubnetRuntimeDomainWriter
	ipamMutation     legacycontroller.SubnetRuntimeIPAMMutationWriter
	ipamAddress      legacycontroller.SubnetRuntimeIPAMAddressWriter
	addressBackend   recordingSubnetIPAMAddressBackend
	virtualPort      legacycontroller.SubnetRuntimeVirtualPortWriter
	virtualPortCalls int
}

func (b *installingSubnetDataPlaneBackend) RecoverSubnetRuntime(context.Context, *kubeovnv1.Subnet) error {
	return nil
}

func (b *installingSubnetDataPlaneBackend) ReconcileSubnetRuntime(context.Context, *kubeovnv1.Subnet) error {
	return nil
}

func (b *installingSubnetDataPlaneBackend) SyncSubnetVirtualPort(context.Context, *kubeovnv1.Subnet) error {
	b.virtualPortCalls++
	return nil
}

func (b *installingSubnetDataPlaneBackend) SetSubnetRuntimeWriters(
	status legacycontroller.SubnetRuntimeStatusWriter,
	ipStatus legacycontroller.SubnetRuntimeIPStatusWriter,
	gatewayMAC legacycontroller.SubnetRuntimeGatewayMACWriter,
	finalizer legacycontroller.SubnetRuntimeFinalizerWriter,
	ovn legacycontroller.SubnetRuntimeDomainWriter,
	ipam legacycontroller.SubnetRuntimeDomainWriter,
) error {
	b.status = status
	b.ipStatus = ipStatus
	b.gatewayMAC = gatewayMAC
	b.finalizer = finalizer
	b.ovn = ovn
	b.ipam = ipam
	return nil
}

func (b *installingSubnetDataPlaneBackend) SetSubnetRuntimeVirtualPortWriter(writer legacycontroller.SubnetRuntimeVirtualPortWriter) error {
	b.virtualPort = writer
	return nil
}

func (b *installingSubnetDataPlaneBackend) SetSubnetRuntimeOVNMutationWriter(writer legacycontroller.SubnetRuntimeOVNMutationWriter) error {
	b.ovnMutation = writer
	return nil
}

func (b *installingSubnetDataPlaneBackend) SetSubnetRuntimeIPAMMutationWriter(writer legacycontroller.SubnetRuntimeIPAMMutationWriter) error {
	b.ipamMutation = writer
	return nil
}

func (b *installingSubnetDataPlaneBackend) SetSubnetRuntimeIPAMAddressWriter(writer legacycontroller.SubnetRuntimeIPAMAddressWriter) error {
	b.ipamAddress = writer
	return nil
}

func (b *installingSubnetDataPlaneBackend) SubnetRuntimeIPAMAddressBackend() legacycontroller.SubnetRuntimeIPAMAddressBackend {
	return &b.addressBackend
}

func (b *legacySubnetDataPlaneBackend) RecoverSubnetRuntime(context.Context, *kubeovnv1.Subnet) error {
	return nil
}

func (b *legacySubnetDataPlaneBackend) ReconcileSubnetRuntime(context.Context, *kubeovnv1.Subnet) error {
	b.reconcile++
	return nil
}

func (b *legacySubnetDataPlaneBackend) SyncSubnetVirtualPort(context.Context, *kubeovnv1.Subnet) error {
	return nil
}

func (b *recordingSubnetDataPlaneBackend) RecoverSubnetRuntime(context.Context, *kubeovnv1.Subnet) error {
	b.recover++
	return nil
}

func (b *recordingSubnetDataPlaneBackend) ReconcileSubnetRuntime(ctx context.Context, subnet *kubeovnv1.Subnet) error {
	return b.ReconcileSubnetRuntimeWithGate(ctx, subnet, true)
}

func (b *recordingSubnetDataPlaneBackend) ReconcileSubnetRuntimeWithGate(_ context.Context, subnet *kubeovnv1.Subnet, allocationAllowed bool) error {
	b.reconcile++
	b.allowed = allocationAllowed
	b.last = subnet.DeepCopy()
	b.deleting = !subnet.DeletionTimestamp.IsZero()
	return nil
}

func TestSubnetControllerWriterAdapterDelegatesDomainPath(t *testing.T) {
	backend := &recordingSubnetDataPlaneBackend{}
	subnet := testSubnet("")
	adapter := &SubnetControllerWriterAdapter{Backend: backend, Reader: newRuntimeClient(t, subnet), Gate: SubnetWriterGate{Recovery: func(context.Context) error { return nil }}}
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	require.NoError(t, adapter.RecoverSubnet(t.Context(), snapshot))
	require.NoError(t, adapter.ReconcileSubnet(t.Context(), snapshot, SubnetShadowObservation{}))
	require.Equal(t, 1, backend.recover)
	require.Equal(t, 1, backend.reconcile)
	require.True(t, backend.allowed)
	require.False(t, backend.deleting)
}

func TestSubnetControllerWriterAdapterRebindsOVNMarkerDuringRecovery(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	recovered := snapshot
	recovered.ResourceVersion = "8"
	markerBackend := newMemorySubnetOVNMarkerBackend()
	key := subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation)
	previous := SubnetOVNOwnerMarker{
		Key: key,
		Owner: SubnetOVNOwner{
			Token:           4,
			SubnetUID:       snapshot.UID,
			Generation:      snapshot.Generation,
			ResourceVersion: snapshot.ResourceVersion,
			WriterEpoch:     "old-epoch",
		},
	}
	require.NoError(t, markerBackend.Acquire(t.Context(), previous))
	adapter := &SubnetControllerWriterAdapter{
		Backend:   &recordingSubnetDataPlaneBackend{},
		Gate:      SubnetWriterGate{Fence: staticSubnetWriterFence{}},
		ovnMarker: markerBackend,
	}

	require.NoError(t, adapter.RecoverSubnet(t.Context(), recovered))
	current, found, err := markerBackend.Read(t.Context(), key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, previous.Owner.Token, current.Owner.Token, "recovery must preserve the pending marker token")
	require.Equal(t, "test-epoch", current.Owner.WriterEpoch)
	require.Equal(t, snapshot.UID, current.Owner.SubnetUID)
	require.Equal(t, recovered.ResourceVersion, current.Owner.ResourceVersion)
}

func TestSubnetControllerWriterAdapterFailsClosedOnStaleOVNMarker(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	markerBackend := newMemorySubnetOVNMarkerBackend()
	stale := SubnetOVNOwnerMarker{
		Key:   subnetOVNMutationKey(snapshot, SubnetOVNCleanupOperation),
		Owner: SubnetOVNOwner{Token: 4, SubnetUID: snapshot.UID, Generation: snapshot.Generation, ResourceVersion: snapshot.ResourceVersion, WriterEpoch: "old-epoch"},
	}
	require.NoError(t, markerBackend.Acquire(t.Context(), stale))
	adapter := &SubnetControllerWriterAdapter{Backend: &recordingSubnetDataPlaneBackend{}, Gate: SubnetWriterGate{Fence: staticSubnetWriterFence{}}, ovnMarker: markerBackend}
	require.ErrorIs(t, adapter.RecoverSubnet(t.Context(), snapshot), ErrSubnetOVNMarkerChanged)
}

func TestSubnetControllerWriterAdapterInstallsExplicitRuntimeWriters(t *testing.T) {
	backend := &legacycontroller.Controller{}
	client := newRuntimeClient(t)
	adapter := &SubnetControllerWriterAdapter{
		Backend: backend,
		Client:  client,
		Reader:  client,
		Gate:    SubnetWriterGate{Recovery: func(context.Context) error { return nil }},
	}
	require.NoError(t, adapter.InstallRuntimeWriters())
	require.Error(t, adapter.InstallRuntimeWriters())
}

func TestSubnetControllerWriterAdapterFailsClosedWithoutTypedIPAMAddressWriter(t *testing.T) {
	backend := &legacySubnetDataPlaneBackend{}
	client := newRuntimeClient(t)
	adapter := &SubnetControllerWriterAdapter{
		Backend: backend,
		Client:  client,
		Reader:  client,
		Gate:    SubnetWriterGate{Recovery: func(context.Context) error { return nil }},
	}
	require.EqualError(t, adapter.InstallRuntimeWriters(), "subnet controller writer backend does not expose typed IPAM address writer")
}

func TestSubnetControllerWriterAdapterWritersSyncResourceVersion(t *testing.T) {
	scheme, err := NewScheme()
	require.NoError(t, err)
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	api := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&kubeovnv1.Subnet{}).WithObjects(subnet).Build()
	backend := &installingSubnetDataPlaneBackend{}
	adapter := &SubnetControllerWriterAdapter{
		Backend: backend,
		Client:  api,
		Reader:  api,
		Gate:    SubnetWriterGate{Recovery: func(context.Context) error { return nil }},
	}
	require.NoError(t, adapter.InstallRuntimeWriters())
	require.NotNil(t, backend.ipStatus)
	require.NotNil(t, backend.gatewayMAC)
	require.NotNil(t, backend.ovn)
	require.NotNil(t, backend.ovnMutation)
	require.NotNil(t, backend.ipam)
	require.NotNil(t, backend.ipamMutation)
	require.NotNil(t, backend.ipamAddress)
	require.NotNil(t, backend.virtualPort)
	current := &kubeovnv1.Subnet{}
	require.NoError(t, api.Get(t.Context(), client.ObjectKey{Name: subnet.Name}, current))
	require.NoError(t, backend.virtualPort(t.Context(), current.Name))
	require.Equal(t, 1, backend.virtualPortCalls)

	require.NoError(t, api.Get(t.Context(), client.ObjectKey{Name: subnet.Name}, current))
	projected := current.DeepCopy()
	projected.Status.V4AvailableIPs = internal.NewBigInt(2)
	oldResourceVersion := projected.ResourceVersion
	require.NoError(t, backend.ipStatus(t.Context(), projected))
	require.NotEqual(t, oldResourceVersion, projected.ResourceVersion)

	oldResourceVersion = projected.ResourceVersion
	require.NoError(t, backend.gatewayMAC(t.Context(), projected, "00:11:22:33:44:55"))
	require.NotEqual(t, oldResourceVersion, projected.ResourceVersion)
	require.Equal(t, "00:11:22:33:44:55", projected.Status.GatewayMAC)
	applyCalls := 0
	require.NoError(t, backend.ovn(t.Context(), projected, legacycontroller.SubnetRuntimeOVNAllocateOperation, func(context.Context) error {
		applyCalls++
		return nil
	}))
	ovnMutationCalls := 0
	require.NoError(t, backend.ovnMutation(t.Context(), projected, legacycontroller.SubnetRuntimeOVNAllocateMutationOperation, func(context.Context) error {
		ovnMutationCalls++
		return nil
	}))
	require.Equal(t, 1, ovnMutationCalls)
	require.NoError(t, backend.ipam(t.Context(), projected, legacycontroller.SubnetRuntimeIPAMAllocateOperation, func(context.Context) error {
		applyCalls++
		return nil
	}))
	require.Equal(t, 2, applyCalls)
	mutationCalls := 0
	require.NoError(t, backend.ipamMutation(t.Context(), projected, legacycontroller.SubnetRuntimeIPAMRegisterOperation, func(context.Context) error {
		mutationCalls++
		return nil
	}))
	require.NoError(t, backend.ipamMutation(t.Context(), projected, legacycontroller.SubnetRuntimeIPAMAllocateAddressOperation, func(context.Context) error {
		mutationCalls++
		return nil
	}))
	require.NoError(t, backend.ipamMutation(t.Context(), projected, legacycontroller.SubnetRuntimeIPAMReleaseAddressOperation, func(context.Context) error {
		mutationCalls++
		return nil
	}))
	require.Equal(t, 3, mutationCalls)
}

func TestSubnetControllerWriterAdapterRetainsOVNMarkerAfterApplyError(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	api := newRuntimeClient(t, subnet)
	backend := &installingSubnetDataPlaneBackend{}
	adapter := &SubnetControllerWriterAdapter{
		Backend: backend,
		Client:  api,
		Reader:  api,
		Gate:    SubnetWriterGate{Recovery: func(context.Context) error { return nil }, Fence: staticSubnetWriterFence{}},
	}
	require.NoError(t, adapter.InstallRuntimeWriters())
	markerBackend := newMemorySubnetOVNMarkerBackend()
	adapter.ovnMarker = markerBackend
	applyErr := errors.New("OVN result is uncertain")
	require.ErrorIs(t, backend.ovnMutation(t.Context(), subnet, legacycontroller.SubnetRuntimeOVNAllocateMutationOperation, func(context.Context) error {
		return applyErr
	}), applyErr)
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	marker, found, err := markerBackend.Read(t.Context(), subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation))
	require.NoError(t, err)
	require.True(t, found, "an uncertain OVN apply must retain its durable marker")
	require.NotZero(t, marker.Owner.Token)
}

func TestSubnetControllerWriterAdapterPersistsOVNReceiptLifecycle(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	api := newRuntimeClient(t, subnet)
	backend := &installingSubnetDataPlaneBackend{}
	adapter := &SubnetControllerWriterAdapter{
		Backend:     backend,
		Client:      api,
		Reader:      api,
		Gate:        SubnetWriterGate{Recovery: func(context.Context) error { return nil }, Fence: staticSubnetWriterFence{}},
		ovnMarker:   newMemorySubnetOVNMarkerBackend(),
		ovnReceipts: newMemorySubnetOVNMutationReceiptStore(),
		ovnResolver: committedSubnetOVNResolver(),
	}
	require.NoError(t, adapter.InstallRuntimeWriters())
	require.NoError(t, backend.ovnMutation(t.Context(), subnet, legacycontroller.SubnetRuntimeOVNAllocateMutationOperation, func(context.Context) error {
		return nil
	}))
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	_, found, err := adapter.ovnMarker.Read(t.Context(), subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation))
	require.NoError(t, err)
	require.False(t, found)
	receipts, err := adapter.ovnReceipts.(subnetOVNMutationReceiptLister).List(t.Context(), subnet.Name)
	require.NoError(t, err)
	require.Empty(t, receipts)

	applyErr := errors.New("OVN result is uncertain")
	require.ErrorIs(t, backend.ovnMutation(t.Context(), subnet, legacycontroller.SubnetRuntimeOVNAllocateMutationOperation, func(context.Context) error {
		return applyErr
	}), applyErr)
	receipt, found, err := adapter.ovnReceipts.Lookup(t.Context(), subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, SubnetOVNReceiptPhaseActive, receipt.Phase)
}

func TestSubnetControllerWriterAdapterRetainsReleasingOVNReceipt(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	api := newRuntimeClient(t, subnet)
	markerBackend := &failingReleaseMarkerBackend{SubnetOVNMarkerBackend: newMemorySubnetOVNMarkerBackend(), releaseErr: errors.New("OVSDB release uncertain")}
	backend := &installingSubnetDataPlaneBackend{}
	adapter := &SubnetControllerWriterAdapter{
		Backend:     backend,
		Client:      api,
		Reader:      api,
		Gate:        SubnetWriterGate{Recovery: func(context.Context) error { return nil }, Fence: staticSubnetWriterFence{}},
		ovnMarker:   markerBackend,
		ovnReceipts: newMemorySubnetOVNMutationReceiptStore(),
		ovnResolver: committedSubnetOVNResolver(),
	}
	require.NoError(t, adapter.InstallRuntimeWriters())
	require.ErrorIs(t, backend.ovnMutation(t.Context(), subnet, legacycontroller.SubnetRuntimeOVNAllocateMutationOperation, func(context.Context) error {
		return nil
	}), markerBackend.releaseErr)
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	receipt, found, err := adapter.ovnReceipts.Lookup(t.Context(), subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, SubnetOVNReceiptPhaseReleasing, receipt.Phase)
	_, found, err = markerBackend.Read(t.Context(), receipt.Key)
	require.NoError(t, err)
	require.True(t, found)
}

func TestSubnetControllerWriterAdapterRecoversReleasingOVNReceipt(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	markerBackend := newMemorySubnetOVNMarkerBackend()
	receiptStore := newMemorySubnetOVNMutationReceiptStore()
	key := subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation)
	marker := SubnetOVNOwnerMarker{Key: key, Owner: SubnetOVNOwner{Token: 8, SubnetUID: snapshot.UID, Generation: snapshot.Generation, ResourceVersion: snapshot.ResourceVersion, WriterEpoch: "old-epoch"}}
	require.NoError(t, markerBackend.Acquire(t.Context(), marker))
	receipt := SubnetOVNMutationReceipt{Key: key, Token: marker.Owner.Token, Generation: marker.Owner.Generation, ResourceVersion: marker.Owner.ResourceVersion, WriterEpoch: marker.Owner.WriterEpoch, Phase: SubnetOVNReceiptPhaseReleasing}
	require.NoError(t, receiptStore.Remember(t.Context(), receipt))
	adapter := &SubnetControllerWriterAdapter{Backend: &recordingSubnetDataPlaneBackend{}, Gate: SubnetWriterGate{Fence: staticSubnetWriterFence{}}, ovnMarker: markerBackend, ovnReceipts: receiptStore, ovnResolver: committedSubnetOVNResolver()}
	require.NoError(t, adapter.RecoverSubnet(t.Context(), snapshot))
	_, found, err := markerBackend.Read(t.Context(), key)
	require.NoError(t, err)
	require.False(t, found)
	_, found, err = receiptStore.Lookup(t.Context(), key)
	require.NoError(t, err)
	require.False(t, found)
}

func TestSubnetControllerWriterAdapterRecoversActiveReceiptWithoutMarker(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	markerBackend := newMemorySubnetOVNMarkerBackend()
	receiptStore := newMemorySubnetOVNMutationReceiptStore()
	key := subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation)
	receipt := SubnetOVNMutationReceipt{Key: key, Token: 8, Generation: snapshot.Generation, ResourceVersion: "6", WriterEpoch: "old-epoch", Phase: SubnetOVNReceiptPhaseActive}
	require.NoError(t, receiptStore.Remember(t.Context(), receipt))
	adapter := &SubnetControllerWriterAdapter{Backend: &recordingSubnetDataPlaneBackend{}, Gate: SubnetWriterGate{Fence: staticSubnetWriterFence{}}, ovnMarker: markerBackend, ovnReceipts: receiptStore, ovnResolver: testSubnetOVNMutationResolver{state: SubnetOVNMutationStateAbsent, force: true}}
	require.NoError(t, adapter.RecoverSubnet(t.Context(), snapshot))
	current, found, err := markerBackend.Read(t.Context(), key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, receipt.Token, current.Owner.Token)
	require.Equal(t, snapshot.ResourceVersion, current.Owner.ResourceVersion)
	require.Equal(t, "test-epoch", current.Owner.WriterEpoch)
	rebound, found, err := receiptStore.Lookup(t.Context(), key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, snapshot.ResourceVersion, rebound.ResourceVersion)
	require.Equal(t, "test-epoch", rebound.WriterEpoch)
}

func TestSubnetControllerWriterAdapterRejectsReleasingReceiptWrongOwner(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	markerBackend := newMemorySubnetOVNMarkerBackend()
	receiptStore := newMemorySubnetOVNMutationReceiptStore()
	key := subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation)
	marker := SubnetOVNOwnerMarker{Key: key, Owner: SubnetOVNOwner{Token: 8, SubnetUID: snapshot.UID, Generation: snapshot.Generation, ResourceVersion: "wrong-rv", WriterEpoch: "epoch-a"}}
	require.NoError(t, markerBackend.Acquire(t.Context(), marker))
	receipt := SubnetOVNMutationReceipt{Key: key, Token: marker.Owner.Token, Generation: snapshot.Generation, ResourceVersion: snapshot.ResourceVersion, WriterEpoch: marker.Owner.WriterEpoch, Phase: SubnetOVNReceiptPhaseReleasing}
	require.NoError(t, receiptStore.Remember(t.Context(), receipt))
	adapter := &SubnetControllerWriterAdapter{Backend: &recordingSubnetDataPlaneBackend{}, Gate: SubnetWriterGate{Fence: staticSubnetWriterFence{}}, ovnMarker: markerBackend, ovnReceipts: receiptStore, ovnResolver: committedSubnetOVNResolver()}
	require.ErrorIs(t, adapter.releaseSubnetOVNReceipts(t.Context(), snapshot), ErrSubnetOVNReceiptChanged)
	_, found, err := receiptStore.Lookup(t.Context(), key)
	require.NoError(t, err)
	require.True(t, found, "owner mismatch must retain the releasing receipt")
}

func TestSubnetControllerWriterAdapterRetainsReleasingReceiptWhenResolverUnknown(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	markerBackend := newMemorySubnetOVNMarkerBackend()
	receiptStore := newMemorySubnetOVNMutationReceiptStore()
	key := subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation)
	marker := SubnetOVNOwnerMarker{Key: key, Owner: SubnetOVNOwner{
		Token: 8, SubnetUID: snapshot.UID, Generation: snapshot.Generation,
		ResourceVersion: snapshot.ResourceVersion, WriterEpoch: "epoch-a",
	}}
	require.NoError(t, markerBackend.Acquire(t.Context(), marker))
	receipt := SubnetOVNMutationReceipt{Key: key, Token: marker.Owner.Token, Generation: snapshot.Generation, ResourceVersion: marker.Owner.ResourceVersion, WriterEpoch: marker.Owner.WriterEpoch, Phase: SubnetOVNReceiptPhaseReleasing}
	require.NoError(t, receiptStore.Remember(t.Context(), receipt))
	adapter := &SubnetControllerWriterAdapter{Backend: &recordingSubnetDataPlaneBackend{}, Gate: SubnetWriterGate{Fence: staticSubnetWriterFence{}}, ovnMarker: markerBackend, ovnReceipts: receiptStore, ovnResolver: testSubnetOVNMutationResolver{state: SubnetOVNMutationStateUnknown, force: true}}
	require.ErrorIs(t, adapter.RecoverSubnet(t.Context(), snapshot), ErrSubnetOVNResolverUnknown)
	_, found, err := markerBackend.Read(t.Context(), key)
	require.NoError(t, err)
	require.True(t, found)
	_, found, err = receiptStore.Lookup(t.Context(), key)
	require.NoError(t, err)
	require.True(t, found)
}

func TestSubnetControllerWriterAdapterDoesNotAcquireMarkerWhenResolverUnknown(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	markerBackend := newMemorySubnetOVNMarkerBackend()
	receiptStore := newMemorySubnetOVNMutationReceiptStore()
	key := subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation)
	receipt := SubnetOVNMutationReceipt{Key: key, Token: 8, Generation: snapshot.Generation, ResourceVersion: "6", WriterEpoch: "old-epoch", Phase: SubnetOVNReceiptPhaseActive}
	require.NoError(t, receiptStore.Remember(t.Context(), receipt))
	adapter := &SubnetControllerWriterAdapter{Backend: &recordingSubnetDataPlaneBackend{}, Gate: SubnetWriterGate{Fence: staticSubnetWriterFence{}}, ovnMarker: markerBackend, ovnReceipts: receiptStore, ovnResolver: testSubnetOVNMutationResolver{state: SubnetOVNMutationStateUnknown, force: true}}
	require.ErrorIs(t, adapter.RecoverSubnet(t.Context(), snapshot), ErrSubnetOVNResolverUnknown)
	_, found, err := markerBackend.Read(t.Context(), key)
	require.NoError(t, err)
	require.False(t, found)
}

func TestSubnetControllerWriterAdapterDeletesAbsentAllocateReceiptForTombstone(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetDeleteSnapshot(subnet)
	require.NoError(t, err)
	markerBackend := newMemorySubnetOVNMarkerBackend()
	receiptStore := newMemorySubnetOVNMutationReceiptStore()
	key := subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation)
	marker := SubnetOVNOwnerMarker{Key: key, Owner: SubnetOVNOwner{
		Token: 8, SubnetUID: snapshot.UID, Generation: snapshot.Generation,
		ResourceVersion: snapshot.ResourceVersion, WriterEpoch: "test-epoch",
	}}
	require.NoError(t, markerBackend.Acquire(t.Context(), marker))
	receipt := SubnetOVNMutationReceipt{Key: key, Token: marker.Owner.Token, Generation: marker.Owner.Generation, ResourceVersion: marker.Owner.ResourceVersion, WriterEpoch: marker.Owner.WriterEpoch, Phase: SubnetOVNReceiptPhaseActive}
	require.NoError(t, receiptStore.Remember(t.Context(), receipt))
	adapter := &SubnetControllerWriterAdapter{
		Backend:   &recordingSubnetDataPlaneBackend{},
		Gate:      SubnetWriterGate{Fence: staticSubnetWriterFence{}},
		ovnMarker: markerBackend, ovnReceipts: receiptStore,
		ovnResolver: testSubnetOVNMutationResolver{state: SubnetOVNMutationStateAbsent, force: true},
	}
	require.NoError(t, adapter.RecoverSubnet(t.Context(), snapshot))
	_, found, err := markerBackend.Read(t.Context(), key)
	require.NoError(t, err)
	require.False(t, found)
	_, found, err = receiptStore.Lookup(t.Context(), key)
	require.NoError(t, err)
	require.False(t, found)
}

type failingReleaseMarkerBackend struct {
	SubnetOVNMarkerBackend
	releaseErr error
}

type testSubnetOVNMutationResolver struct {
	state SubnetOVNMutationState
	err   error
	force bool
}

func (r testSubnetOVNMutationResolver) ResolveSubnetOVNMutation(_ context.Context, _ SubnetWriteSnapshot, key SubnetOVNMutationKey, _ SubnetOVNMutationReceipt) (SubnetOVNMutationState, error) {
	if r.err != nil {
		return SubnetOVNMutationStateUnknown, r.err
	}
	if !r.force {
		if key.Operation == SubnetOVNCleanupOperation {
			return SubnetOVNMutationStateAbsent, nil
		}
		return SubnetOVNMutationStateCommitted, nil
	}
	return r.state, nil
}

func committedSubnetOVNResolver() testSubnetOVNMutationResolver {
	return testSubnetOVNMutationResolver{state: SubnetOVNMutationStateCommitted}
}

func (b *failingReleaseMarkerBackend) Release(context.Context, SubnetOVNOwnerMarker) error {
	return b.releaseErr
}

func TestSubnetControllerWriterAdapterReusesOVNMarkerAfterResourceVersionAdvance(t *testing.T) {
	scheme, err := NewScheme()
	require.NoError(t, err)
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	api := fake.NewClientBuilder().WithScheme(scheme).WithObjects(subnet).Build()
	backend := &installingSubnetDataPlaneBackend{}
	adapter := &SubnetControllerWriterAdapter{
		Backend: backend,
		Client:  api,
		Reader:  api,
		Gate:    SubnetWriterGate{Recovery: func(context.Context) error { return nil }, Fence: staticSubnetWriterFence{}},
	}
	require.NoError(t, adapter.InstallRuntimeWriters())
	markerBackend := newMemorySubnetOVNMarkerBackend()
	adapter.ovnMarker = markerBackend

	applyErr := errors.New("OVN result is uncertain")
	require.ErrorIs(t, backend.ovnMutation(t.Context(), subnet, legacycontroller.SubnetRuntimeOVNAllocateMutationOperation, func(context.Context) error {
		return applyErr
	}), applyErr)

	current := &kubeovnv1.Subnet{}
	require.NoError(t, api.Get(t.Context(), client.ObjectKey{Name: subnet.Name}, current))
	current.Annotations = map[string]string{"runtime.kube-ovn.io/status-write": "advanced"}
	require.NoError(t, api.Update(t.Context(), current))
	require.NotEqual(t, subnet.ResourceVersion, current.ResourceVersion)

	applyCalls := 0
	require.NoError(t, backend.ovnMutation(t.Context(), current, legacycontroller.SubnetRuntimeOVNAllocateMutationOperation, func(context.Context) error {
		applyCalls++
		return nil
	}))
	require.Equal(t, 1, applyCalls)
	snapshot, err := NewSubnetWriteSnapshot(current, true)
	require.NoError(t, err)
	_, found, err := markerBackend.Read(t.Context(), subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation))
	require.NoError(t, err)
	require.False(t, found, "successful retry must release the retained marker")
}

func TestSubnetControllerWriterAdapterDeleteUsesMarkerOnlyRecovery(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetDeleteSnapshot(subnet)
	require.NoError(t, err)
	markerBackend := newMemorySubnetOVNMarkerBackend()
	key := subnetOVNMutationKey(snapshot, SubnetOVNCleanupOperation)
	allocateKey := subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation)
	oldSnapshot := snapshot
	oldSnapshot.Generation = 2
	oldSnapshot.ResourceVersion = "6"
	oldAllocateKey := subnetOVNMutationKey(oldSnapshot, SubnetOVNAllocateOperation)
	pending := SubnetOVNOwnerMarker{
		Key: key,
		Owner: SubnetOVNOwner{
			Token:           9,
			SubnetUID:       snapshot.UID,
			Generation:      snapshot.Generation,
			ResourceVersion: snapshot.ResourceVersion,
			WriterEpoch:     "old-epoch",
		},
	}
	require.NoError(t, markerBackend.Acquire(t.Context(), pending))
	allocatePending := pending
	allocatePending.Key = allocateKey
	require.NoError(t, markerBackend.Acquire(t.Context(), allocatePending))
	oldAllocatePending := allocatePending
	oldAllocatePending.Key = oldAllocateKey
	oldAllocatePending.Owner.Generation = oldSnapshot.Generation
	oldAllocatePending.Owner.ResourceVersion = oldSnapshot.ResourceVersion
	require.NoError(t, markerBackend.Acquire(t.Context(), oldAllocatePending))
	backend := &recordingSubnetDataPlaneBackend{}
	api := newRuntimeClient(t)
	adapter := &SubnetControllerWriterAdapter{
		Backend:   backend,
		Reader:    api,
		Gate:      SubnetWriterGate{Recovery: func(context.Context) error { return nil }, Fence: staticSubnetWriterFence{}},
		ovnMarker: markerBackend,
	}

	require.NoError(t, adapter.ReconcileDeletedSubnet(t.Context(), snapshot))
	require.Zero(t, backend.recover, "delete cleanup must not repeat full backend recovery")
	require.Equal(t, 1, backend.reconcile)
	_, found, err := markerBackend.Read(t.Context(), allocateKey)
	require.NoError(t, err)
	require.False(t, found, "successful delete cleanup must resolve an older allocation marker")
	_, found, err = markerBackend.Read(t.Context(), oldAllocateKey)
	require.NoError(t, err)
	require.False(t, found, "successful delete cleanup must resolve an older-generation marker")
	_, found, err = markerBackend.Read(t.Context(), key)
	require.NoError(t, err)
	require.False(t, found, "successful delete cleanup must resolve the cleanup marker")
}

func TestSubnetControllerWriterAdapterDeleteRebindsSameGenerationAllocateReceipt(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetDeleteSnapshot(subnet)
	require.NoError(t, err)
	markerBackend := newMemorySubnetOVNMarkerBackend()
	receiptStore := newMemorySubnetOVNMutationReceiptStore()
	key := subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation)
	marker := SubnetOVNOwnerMarker{Key: key, Owner: SubnetOVNOwner{Token: 8, SubnetUID: snapshot.UID, Generation: snapshot.Generation, ResourceVersion: "6", WriterEpoch: "old-epoch"}}
	require.NoError(t, markerBackend.Acquire(t.Context(), marker))
	receipt := SubnetOVNMutationReceipt{Key: key, Token: marker.Owner.Token, Generation: snapshot.Generation, ResourceVersion: marker.Owner.ResourceVersion, WriterEpoch: marker.Owner.WriterEpoch, Phase: SubnetOVNReceiptPhaseActive}
	require.NoError(t, receiptStore.Remember(t.Context(), receipt))
	adapter := &SubnetControllerWriterAdapter{Backend: &recordingSubnetDataPlaneBackend{}, Reader: newRuntimeClient(t), Gate: SubnetWriterGate{Recovery: func(context.Context) error { return nil }, Fence: staticSubnetWriterFence{}}, ovnMarker: markerBackend, ovnReceipts: receiptStore, ovnResolver: committedSubnetOVNResolver()}
	require.NoError(t, adapter.ReconcileDeletedSubnet(t.Context(), snapshot))
	_, found, err := receiptStore.Lookup(t.Context(), key)
	require.NoError(t, err)
	require.False(t, found)
}

func TestSubnetControllerWriterAdapterDeleteCleansOldReceiptWithoutMarker(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetDeleteSnapshot(subnet)
	require.NoError(t, err)
	old := snapshot
	old.Generation = 2
	old.ResourceVersion = "6"
	key := subnetOVNMutationKey(old, SubnetOVNAllocateOperation)
	receiptStore := newMemorySubnetOVNMutationReceiptStore()
	receipt := SubnetOVNMutationReceipt{Key: key, Token: 8, Generation: old.Generation, ResourceVersion: old.ResourceVersion, WriterEpoch: "old-epoch", Phase: SubnetOVNReceiptPhaseActive}
	require.NoError(t, receiptStore.Remember(t.Context(), receipt))
	adapter := &SubnetControllerWriterAdapter{Backend: &recordingSubnetDataPlaneBackend{}, Reader: newRuntimeClient(t), Gate: SubnetWriterGate{Recovery: func(context.Context) error { return nil }, Fence: staticSubnetWriterFence{}}, ovnMarker: newMemorySubnetOVNMarkerBackend(), ovnReceipts: receiptStore, ovnResolver: committedSubnetOVNResolver()}
	require.NoError(t, adapter.ReconcileDeletedSubnet(t.Context(), snapshot))
	_, found, err := receiptStore.Lookup(t.Context(), key)
	require.NoError(t, err)
	require.False(t, found)
}

func TestSubnetControllerWriterAdapterTypedIPAMAddressWriterPreservesRequest(t *testing.T) {
	scheme, err := NewScheme()
	require.NoError(t, err)
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	api := fake.NewClientBuilder().WithScheme(scheme).WithObjects(subnet).Build()
	backend := &installingSubnetDataPlaneBackend{}
	backend.addressBackend.allocateResult = legacycontroller.SubnetRuntimeIPAMAddressResult{V4: "10.0.0.8", V6: "fd00::8", MAC: "00:11:22:33:44:55"}
	adapter := &SubnetControllerWriterAdapter{
		Backend: backend,
		Client:  api,
		Reader:  api,
		Gate:    SubnetWriterGate{Recovery: func(context.Context) error { return nil }},
	}
	require.NoError(t, adapter.InstallRuntimeWriters())

	current := &kubeovnv1.Subnet{}
	require.NoError(t, api.Get(t.Context(), client.ObjectKey{Name: subnet.Name}, current))
	_, err = backend.ipamAddress(t.Context(), current, legacycontroller.SubnetRuntimeIPAMAddressRequest{
		Operation:  legacycontroller.SubnetRuntimeIPAMAllocateAddressOperation,
		SubnetName: "other-subnet",
	})
	require.ErrorContains(t, err, "targets Subnet")
	result, err := backend.ipamAddress(t.Context(), current, legacycontroller.SubnetRuntimeIPAMAddressRequest{
		Operation:     legacycontroller.SubnetRuntimeIPAMAllocateAddressOperation,
		SubnetName:    current.Name,
		PodName:       "pod-a",
		NICName:       "nic-a",
		PoolName:      "pool-a",
		SkippedAddrs:  []string{"10.0.0.2"},
		CheckConflict: true,
	})
	require.NoError(t, err)
	require.Equal(t, backend.addressBackend.allocateResult, result)
	require.Equal(t, "pod-a", backend.addressBackend.lastPodName)
	require.Equal(t, "nic-a", backend.addressBackend.lastNICName)
	require.Equal(t, "pool-a", backend.addressBackend.lastPoolName)
	require.Equal(t, []string{"10.0.0.2"}, backend.addressBackend.lastSkipped)
	require.Empty(t, backend.addressBackend.lastRequestedIP)
	require.Nil(t, backend.addressBackend.lastMAC)
	require.True(t, backend.addressBackend.lastConflict)

	mac := "00:11:22:33:44:66"
	_, err = backend.ipamAddress(t.Context(), current, legacycontroller.SubnetRuntimeIPAMAddressRequest{
		Operation:     legacycontroller.SubnetRuntimeIPAMAllocateAddressOperation,
		SubnetName:    current.Name,
		PodName:       "pod-a",
		NICName:       "nic-a",
		RequestedIP:   "10.0.0.8",
		MAC:           &mac,
		CheckConflict: true,
	})
	require.NoError(t, err)
	require.Equal(t, "10.0.0.8", backend.addressBackend.lastRequestedIP)
	require.Same(t, &mac, backend.addressBackend.lastMAC)

	backend.addressBackend.existingResult = result
	backend.addressBackend.existingFound = true
	_, err = backend.ipamAddress(t.Context(), current, legacycontroller.SubnetRuntimeIPAMAddressRequest{
		Operation:  legacycontroller.SubnetRuntimeIPAMReleaseAddressOperation,
		SubnetName: current.Name,
		PodName:    "pod-a",
		NICName:    "nic-a",
	})
	require.NoError(t, err)
	require.Equal(t, 1, backend.addressBackend.releaseCalls)
	require.Equal(t, result.V4, backend.addressBackend.releaseV4)
	require.Equal(t, result.V6, backend.addressBackend.releaseV6)
	require.Equal(t, result.MAC, backend.addressBackend.releaseMAC)

	_, err = backend.ipamAddress(t.Context(), current, legacycontroller.SubnetRuntimeIPAMAddressRequest{
		Operation:  legacycontroller.SubnetRuntimeIPAMReleaseAddressOperation,
		SubnetName: current.Name,
		PodName:    "pod-a",
	})
	require.NoError(t, err)
	require.Equal(t, 1, backend.addressBackend.releasePodCalls)
	require.Equal(t, "pod-a", backend.addressBackend.lastPodName)
}

func TestSubnetControllerWriterAdapterUsesDurableIPAMAdapter(t *testing.T) {
	scheme, err := NewScheme()
	require.NoError(t, err)
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	api := fake.NewClientBuilder().WithScheme(scheme).WithObjects(subnet).Build()
	backend := &installingSubnetDataPlaneBackend{}
	backend.addressBackend.allocateResult = legacycontroller.SubnetRuntimeIPAMAddressResult{V4: "10.0.0.9", MAC: "00:11:22:33:44:77"}
	store := newMemorySubnetIPAMReceiptStore()
	runtimeBackend, err := newSubnetRuntimeIPAMBackend(&backend.addressBackend, store)
	require.NoError(t, err)
	ipamAdapter, err := newSubnetIPAMAdapterWithBackendAndStore(&subnetSideEffectWriter{
		ctx:    t.Context(),
		reader: api,
		gate:   SubnetWriterGate{Recovery: func(context.Context) error { return nil }, Fence: staticSubnetWriterFence{}},
	}, runtimeBackend, store)
	require.NoError(t, err)
	adapter := &SubnetControllerWriterAdapter{
		Backend:     backend,
		Client:      api,
		Reader:      api,
		Gate:        SubnetWriterGate{Recovery: func(context.Context) error { return nil }, Fence: staticSubnetWriterFence{}},
		ipamAdapter: ipamAdapter,
	}
	require.NoError(t, adapter.InstallRuntimeWriters())

	current := &kubeovnv1.Subnet{}
	require.NoError(t, api.Get(t.Context(), client.ObjectKey{Name: subnet.Name}, current))
	result, err := backend.ipamAddress(t.Context(), current, legacycontroller.SubnetRuntimeIPAMAddressRequest{
		Operation:  legacycontroller.SubnetRuntimeIPAMAllocateAddressOperation,
		SubnetName: current.Name,
		PodName:    "pod-a",
		NICName:    "nic-a",
	})
	require.NoError(t, err)
	require.Equal(t, backend.addressBackend.allocateResult, result)
	key := subnetIPAMKey{subnet: current.Name, pod: "pod-a", nic: "nic-a"}
	allocation, found, err := store.Lookup(t.Context(), key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, subnetIPAMAddress{v4: result.V4, v6: result.V6, mac: result.MAC}, allocation.address)

	backend.addressBackend.existingResult = result
	backend.addressBackend.existingFound = true
	snapshot, err := NewSubnetWriteSnapshot(current, true)
	require.NoError(t, err)
	require.NoError(t, ipamAdapter.Recover(t.Context(), snapshot))

	_, err = backend.ipamAddress(t.Context(), current, legacycontroller.SubnetRuntimeIPAMAddressRequest{
		Operation:  legacycontroller.SubnetRuntimeIPAMReleaseAddressOperation,
		SubnetName: current.Name,
		PodName:    "pod-a",
		NICName:    "nic-a",
	})
	require.NoError(t, err)
	_, found, err = store.Lookup(t.Context(), key)
	require.NoError(t, err)
	require.False(t, found)

	backend.addressBackend.existingFound = false
	backend.addressBackend.allocateResult = legacycontroller.SubnetRuntimeIPAMAddressResult{V4: "10.0.0.10", MAC: "00:11:22:33:44:88"}
	staticResult, err := backend.ipamAddress(t.Context(), current, legacycontroller.SubnetRuntimeIPAMAddressRequest{
		Operation:   legacycontroller.SubnetRuntimeIPAMAllocateAddressOperation,
		SubnetName:  current.Name,
		PodName:     "pod-b",
		NICName:     "nic-b",
		RequestedIP: "10.0.0.10",
	})
	require.NoError(t, err)
	require.Equal(t, backend.addressBackend.allocateResult, staticResult)

	_, err = backend.ipamAddress(t.Context(), current, legacycontroller.SubnetRuntimeIPAMAddressRequest{
		Operation:  legacycontroller.SubnetRuntimeIPAMReleaseAddressOperation,
		SubnetName: current.Name,
		PodName:    "pod-b",
	})
	require.NoError(t, err)
	_, found, err = store.Lookup(t.Context(), subnetIPAMKey{subnet: current.Name, pod: "pod-b", nic: "nic-b"})
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, 2, backend.addressBackend.releaseCalls, "durable release should fence each owned tuple")
	require.Zero(t, backend.addressBackend.releasePodCalls, "durable release must not use the broad Pod fallback")
}

func TestSubnetControllerWriterAdapterVirtualPortRejectsReplacement(t *testing.T) {
	scheme, err := NewScheme()
	require.NoError(t, err)
	original := testSubnet("")
	original.UID = types.UID("subnet-old")
	api := fake.NewClientBuilder().WithScheme(scheme).WithObjects(original).Build()
	replacement := original.DeepCopy()
	replacement.UID = types.UID("subnet-new")
	replacement.ResourceVersion = ""
	replacement.Spec.CIDRBlock = "10.0.1.0/24"
	recoveryCalls := 0
	backend := &installingSubnetDataPlaneBackend{}
	adapter := &SubnetControllerWriterAdapter{
		Backend: backend,
		Client:  api,
		Reader:  api,
		Gate: SubnetWriterGate{Recovery: func(context.Context) error {
			recoveryCalls++
			if recoveryCalls == 2 {
				require.NoError(t, api.Delete(t.Context(), original))
				require.NoError(t, api.Create(t.Context(), replacement))
			}
			return nil
		}},
	}
	require.NoError(t, adapter.InstallRuntimeWriters())

	err = backend.virtualPort(t.Context(), original.Name)
	require.True(t, apierrors.IsConflict(err), err)
	require.Zero(t, backend.virtualPortCalls, "replacement must be rejected before OVN mutation")
}

func TestSubnetControllerWriterAdapterPreservesTermination(t *testing.T) {
	backend := &recordingSubnetDataPlaneBackend{}
	subnet := testSubnet("")
	subnet.Finalizers = []string{"kube-ovn.io/subnet"}
	now := metav1.Now()
	subnet.DeletionTimestamp = &now
	adapter := &SubnetControllerWriterAdapter{Backend: backend, Reader: newRuntimeClient(t, subnet), Gate: SubnetWriterGate{Recovery: func(context.Context) error { return nil }}}
	snapshot, err := NewSubnetWriteSnapshot(subnet, false)
	require.NoError(t, err)
	require.NoError(t, adapter.ReconcileSubnet(t.Context(), snapshot, SubnetShadowObservation{}))
	require.True(t, backend.deleting)
	require.False(t, backend.allowed)
}

func TestSubnetControllerWriterAdapterPreservesStatusAndFinalizers(t *testing.T) {
	backend := &recordingSubnetDataPlaneBackend{}
	subnet := testSubnet("")
	subnet.Finalizers = []string{"kube-ovn.io/subnet"}
	subnet.Status.GatewayMAC = "00:11:22:33:44:55"
	adapter := &SubnetControllerWriterAdapter{Backend: backend, Reader: newRuntimeClient(t, subnet), Gate: SubnetWriterGate{Recovery: func(context.Context) error { return nil }}}
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	require.NoError(t, adapter.ReconcileSubnet(t.Context(), snapshot, SubnetShadowObservation{}))
	require.Equal(t, subnet.Finalizers, backend.last.Finalizers)
	require.Equal(t, subnet.Status.GatewayMAC, backend.last.Status.GatewayMAC)
}

func TestSubnetControllerWriterAdapterRejectsAllocationWhenGateIsClosed(t *testing.T) {
	backend := &recordingSubnetDataPlaneBackend{}
	subnet := testSubnet("vlan-a")
	vlan := testVlan("vlan-a", "provider-a")
	provider := testProvider("provider-a", false, "vlan-a")
	adapter := &SubnetControllerWriterAdapter{
		Backend: backend,
		Reader:  newRuntimeClient(t, subnet, vlan, provider),
		Gate:    SubnetWriterGate{Recovery: func(context.Context) error { return nil }},
	}
	snapshot, err := NewSubnetWriteSnapshot(subnet, false)
	require.NoError(t, err)

	err = adapter.ReconcileSubnet(t.Context(), snapshot, SubnetShadowObservation{})
	require.ErrorIs(t, err, ErrSubnetRuntimeAllocationNotAllowed)
	require.Zero(t, backend.reconcile)
}

func TestSubnetControllerWriterAdapterRechecksDependencyGate(t *testing.T) {
	backend := &recordingSubnetDataPlaneBackend{}
	subnet := testSubnet("vlan-a")
	vlan := testVlan("vlan-a", "provider-a")
	provider := testProvider("provider-a", false, "vlan-a")
	adapter := &SubnetControllerWriterAdapter{
		Backend: backend,
		Reader:  newRuntimeClient(t, subnet, vlan, provider),
		Gate:    SubnetWriterGate{Recovery: func(context.Context) error { return nil }},
	}
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)

	err = adapter.ReconcileSubnet(t.Context(), snapshot, SubnetShadowObservation{})
	require.ErrorIs(t, err, ErrSubnetRuntimeAllocationNotAllowed)
	require.Zero(t, backend.reconcile)
}

func TestSubnetControllerWriterAdapterLegacyBackendStillFailsClosed(t *testing.T) {
	backend := &legacySubnetDataPlaneBackend{}
	subnet := testSubnet("vlan-a")
	adapter := &SubnetControllerWriterAdapter{
		Backend: backend,
		Reader:  newRuntimeClient(t, subnet),
		Gate:    SubnetWriterGate{Recovery: func(context.Context) error { return nil }},
	}
	snapshot, err := NewSubnetWriteSnapshot(subnet, false)
	require.NoError(t, err)

	err = adapter.ReconcileSubnet(t.Context(), snapshot, SubnetShadowObservation{})
	require.ErrorIs(t, err, ErrSubnetRuntimeAllocationNotAllowed)
	require.Zero(t, backend.reconcile)
}

func TestSubnetControllerWriterAdapterRejectsStaleSnapshot(t *testing.T) {
	backend := &recordingSubnetDataPlaneBackend{}
	live := testSubnet("vlan-new")
	live.UID = types.UID("uid-new")
	live.Generation = 2
	live.ResourceVersion = "8"
	adapter := &SubnetControllerWriterAdapter{
		Backend: backend,
		Reader:  newRuntimeClient(t, live),
		Gate:    SubnetWriterGate{Recovery: func(context.Context) error { return nil }},
	}
	stale := live.DeepCopy()
	stale.UID = types.UID("uid-old")
	stale.Generation = 1
	stale.ResourceVersion = "7"
	stale.Spec.Vlan = "vlan-old"
	snapshot, err := NewSubnetWriteSnapshot(stale, true)
	require.NoError(t, err)

	err = adapter.ReconcileSubnet(t.Context(), snapshot, SubnetShadowObservation{})
	require.True(t, apierrors.IsConflict(err), err)
	require.Zero(t, backend.reconcile)
}

func TestSubnetControllerWriterAdapterFailsClosed(t *testing.T) {
	adapter := &SubnetControllerWriterAdapter{}
	snapshot, err := NewSubnetWriteSnapshot(testSubnet(""), true)
	require.NoError(t, err)
	require.EqualError(t, adapter.ReconcileSubnet(t.Context(), snapshot, SubnetShadowObservation{}), "subnet controller writer backend is not configured")
	adapter.Backend = &recordingSubnetDataPlaneBackend{}
	adapter.Reader = newRuntimeClient(t, testSubnet(""))
	adapter.Gate = SubnetWriterGate{Recovery: func(context.Context) error { return errors.New("recovery pending") }}
	require.ErrorContains(t, adapter.ReconcileSubnet(t.Context(), snapshot, SubnetShadowObservation{}), "recovery pending")
}
