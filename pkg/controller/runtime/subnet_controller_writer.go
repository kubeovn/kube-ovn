package controllerruntime

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	legacycontroller "github.com/kubeovn/kube-ovn/pkg/controller"
)

var (
	ErrSubnetRuntimeReaderMissing        = errors.New("subnet runtime reader is not configured")
	ErrSubnetRuntimeAllocationNotAllowed = legacycontroller.ErrSubnetRuntimeAllocationNotAllowed
)

// SubnetDataPlaneBackend is the explicit seam for the initialized legacy
// Controller. In subnet-writer mode it is the only implementation that may
// touch IPAM and OVN; the legacy Subnet workers are disabled by the mode gate.
type SubnetDataPlaneBackend interface {
	RecoverSubnetRuntime(context.Context, *kubeovnv1.Subnet) error
	ReconcileSubnetRuntime(context.Context, *kubeovnv1.Subnet) error
}

// SubnetGatedDataPlaneBackend is implemented by the real runtime backend. The
// optional seam keeps the pre-existing backend interface source-compatible
// while allowing the runtime writer to pass its dependency readiness grant.
type SubnetGatedDataPlaneBackend interface {
	ReconcileSubnetRuntimeWithGate(context.Context, *kubeovnv1.Subnet, bool) error
}

// SubnetVirtualPortBackend is the legacy OVN operation behind the runtime
// virtual-port queue admission. The runtime adapter supplies a fresh API
// object before invoking it.
type SubnetVirtualPortBackend interface {
	SyncSubnetVirtualPort(context.Context, *kubeovnv1.Subnet) error
}

// SubnetControllerWriterAdapter wires the real Controller domain path into
// the runtime reconciler. The facade carries the immutable API snapshot into
// the existing status, finalizer, IPAM, and OVN sequence.
type SubnetControllerWriterAdapter struct {
	Backend     SubnetDataPlaneBackend
	Client      client.Client
	Reader      client.Reader
	Gate        SubnetWriterGate
	ipamAdapter *subnetIPAMAdapter
}

type subnetRuntimeWriterInstaller interface {
	SetSubnetRuntimeWriters(
		legacycontroller.SubnetRuntimeStatusWriter,
		legacycontroller.SubnetRuntimeIPStatusWriter,
		legacycontroller.SubnetRuntimeGatewayMACWriter,
		legacycontroller.SubnetRuntimeFinalizerWriter,
		legacycontroller.SubnetRuntimeDomainWriter,
		legacycontroller.SubnetRuntimeDomainWriter,
	) error
}

type subnetRuntimeVirtualPortWriterInstaller interface {
	SetSubnetRuntimeVirtualPortWriter(legacycontroller.SubnetRuntimeVirtualPortWriter) error
}

type subnetRuntimeIPAMMutationWriterInstaller interface {
	SetSubnetRuntimeIPAMMutationWriter(legacycontroller.SubnetRuntimeIPAMMutationWriter) error
}

type subnetRuntimeIPAMAddressWriterInstaller interface {
	SetSubnetRuntimeIPAMAddressWriter(legacycontroller.SubnetRuntimeIPAMAddressWriter) error
}

// InstallRuntimeWriters connects the explicit API writers to the legacy domain
// sequence. The runtime mode owns this installation before manager start; a
// legacy backend that cannot expose the hooks fails closed instead of silently
// falling back to direct status or finalizer patches.
func (a *SubnetControllerWriterAdapter) InstallRuntimeWriters() error {
	if a == nil || a.Backend == nil {
		return errors.New("subnet controller writer backend is not configured")
	}
	if a.Client == nil || a.Reader == nil {
		return ErrSubnetRuntimeReaderMissing
	}
	installer, ok := a.Backend.(subnetRuntimeWriterInstaller)
	if !ok {
		return errors.New("subnet controller writer backend does not expose runtime writers")
	}
	virtualPortInstaller, ok := a.Backend.(subnetRuntimeVirtualPortWriterInstaller)
	if !ok {
		return errors.New("subnet controller writer backend does not expose virtual-port writer")
	}
	virtualPortBackend, ok := a.Backend.(SubnetVirtualPortBackend)
	if !ok {
		return errors.New("subnet controller writer backend does not expose virtual-port backend")
	}
	ipamMutationInstaller, ok := a.Backend.(subnetRuntimeIPAMMutationWriterInstaller)
	if !ok {
		return errors.New("subnet controller writer backend does not expose IPAM mutation writer")
	}
	statusWriter := func(ctx context.Context, subnet *kubeovnv1.Subnet) error {
		return (&subnetStatusWriter{ctx: ctx, client: a.Client, reader: a.Reader, gate: a.Gate}).PatchStatus(subnet)
	}
	gatewayMACWriter := func(ctx context.Context, subnet *kubeovnv1.Subnet, gatewayMAC string) error {
		if subnet == nil {
			return errors.New("subnet gateway MAC writer requires a Subnet")
		}
		current, err := getSubnet(ctx, a.Reader, subnet.Name, subnetStatusWriteTimeout)
		if err != nil {
			return err
		}
		if err := (&subnetStatusWriter{ctx: ctx, client: a.Client, reader: a.Reader, gate: a.Gate}).PatchGatewayMAC(current, gatewayMAC); err != nil {
			return err
		}
		subnet.ObjectMeta = *current.ObjectMeta.DeepCopy()
		subnet.Status = *current.Status.DeepCopy()
		return nil
	}
	ipStatusWriter := func(ctx context.Context, subnet *kubeovnv1.Subnet) error {
		if subnet == nil {
			return errors.New("subnet IP status writer requires a Subnet")
		}
		current, err := getSubnet(ctx, a.Reader, subnet.Name, subnetStatusWriteTimeout)
		if err != nil {
			return err
		}
		desired := current.DeepCopy()
		desired.Status.V4AvailableIPs = subnet.Status.V4AvailableIPs.Clone()
		desired.Status.V4AvailableIPRange = subnet.Status.V4AvailableIPRange
		desired.Status.V4UsingIPs = subnet.Status.V4UsingIPs.Clone()
		desired.Status.V4UsingIPRange = subnet.Status.V4UsingIPRange
		desired.Status.V6AvailableIPs = subnet.Status.V6AvailableIPs.Clone()
		desired.Status.V6AvailableIPRange = subnet.Status.V6AvailableIPRange
		desired.Status.V6UsingIPs = subnet.Status.V6UsingIPs.Clone()
		desired.Status.V6UsingIPRange = subnet.Status.V6UsingIPRange
		if err := (&subnetStatusWriter{ctx: ctx, client: a.Client, reader: a.Reader, gate: a.Gate}).PatchStatus(desired); err != nil {
			return err
		}
		subnet.ObjectMeta = *desired.ObjectMeta.DeepCopy()
		return nil
	}
	finalizerWriter := func(ctx context.Context, subnet *kubeovnv1.Subnet, remove bool) (*kubeovnv1.Subnet, error) {
		writer := &subnetFinalizerWriter{ctx: ctx, client: a.Client, reader: a.Reader, gate: a.Gate}
		updated := subnet.DeepCopy()
		var err error
		if remove {
			err = writer.RemoveFinalizer(updated)
		} else {
			err = writer.AddFinalizer(updated)
		}
		return updated, err
	}
	domainWriter := func(ctx context.Context, subnet *kubeovnv1.Subnet, operation legacycontroller.SubnetRuntimeDomainOperation, apply func(context.Context) error) error {
		if apply == nil {
			return errors.New("subnet runtime domain operation is not configured")
		}
		allocationAllowed := subnet.DeletionTimestamp.IsZero()
		snapshot, err := NewSubnetWriteSnapshot(subnet, allocationAllowed)
		if err != nil {
			return err
		}
		writer := &subnetSideEffectWriter{ctx: ctx, reader: a.Reader, gate: a.Gate}
		sideEffectOperation := SubnetWriteOperation(operation)
		return writer.Execute(snapshot, sideEffectOperation, SubnetSideEffectPlan{
			Apply: func(ctx context.Context) (SubnetSideEffectReceipt, error) {
				return nil, apply(ctx)
			},
		})
	}
	virtualPortWriter := func(ctx context.Context, name string) error {
		if err := a.Gate.WaitForRecovery(ctx); err != nil {
			return err
		}
		current, err := getSubnet(ctx, a.Reader, name, subnetStatusWriteTimeout)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		if !current.DeletionTimestamp.IsZero() {
			return nil
		}
		snapshot, err := NewSubnetWriteSnapshot(current, false)
		if err != nil {
			return err
		}
		if err := a.Gate.Validate(ctx, snapshot, current, SubnetOVNVirtualPortOperation); err != nil {
			return err
		}
		// Re-read immediately before the backend call so a delayed queue key
		// cannot use an object replaced after the first admission check.
		latest, err := getSubnet(ctx, a.Reader, name, subnetStatusWriteTimeout)
		if err != nil {
			return err
		}
		if err := snapshot.Validate(latest, SubnetOVNVirtualPortOperation); err != nil {
			return err
		}
		return virtualPortBackend.SyncSubnetVirtualPort(ctx, latest)
	}
	ipamMutationWriter := func(ctx context.Context, subnet *kubeovnv1.Subnet, operation legacycontroller.SubnetRuntimeIPAMMutationOperation, apply func(context.Context) error) error {
		if subnet == nil {
			return errors.New("subnet IPAM mutation writer requires a Subnet")
		}
		if apply == nil {
			return errors.New("subnet IPAM mutation operation is not configured")
		}
		allocationAllowed := subnet.DeletionTimestamp.IsZero()
		snapshot, err := NewSubnetWriteSnapshot(subnet, allocationAllowed)
		if err != nil {
			return err
		}
		var sideEffectOperation SubnetWriteOperation
		switch operation {
		case legacycontroller.SubnetRuntimeIPAMRegisterOperation:
			sideEffectOperation = SubnetIPAMRegisterOperation
		case legacycontroller.SubnetRuntimeIPAMDeleteOperation:
			sideEffectOperation = SubnetIPAMDeleteOperation
		case legacycontroller.SubnetRuntimeIPAMAllocateAddressOperation:
			sideEffectOperation = SubnetIPAMAddressAllocateOperation
		case legacycontroller.SubnetRuntimeIPAMReleaseAddressOperation:
			sideEffectOperation = SubnetIPAMAddressReleaseOperation
		default:
			return fmt.Errorf("unsupported Subnet IPAM mutation operation %q", operation)
		}
		writer := &subnetSideEffectWriter{ctx: ctx, reader: a.Reader, gate: a.Gate}
		return writer.Execute(snapshot, sideEffectOperation, SubnetSideEffectPlan{
			Apply: func(ctx context.Context) (SubnetSideEffectReceipt, error) {
				return nil, apply(ctx)
			},
		})
	}
	if err := installer.SetSubnetRuntimeWriters(statusWriter, ipStatusWriter, gatewayMACWriter, finalizerWriter, domainWriter, domainWriter); err != nil {
		return err
	}
	if err := ipamMutationInstaller.SetSubnetRuntimeIPAMMutationWriter(ipamMutationWriter); err != nil {
		return err
	}
	addressInstaller, ok := a.Backend.(subnetRuntimeIPAMAddressWriterInstaller)
	if !ok {
		return errors.New("subnet controller writer backend does not expose typed IPAM address writer")
	}
	backendProvider, ok := a.Backend.(legacycontroller.SubnetRuntimeIPAMAddressBackendProvider)
	if !ok {
		return errors.New("subnet controller writer backend does not expose typed IPAM address backend")
	}
	addressBackend := backendProvider.SubnetRuntimeIPAMAddressBackend()
	if addressBackend == nil {
		return errors.New("subnet controller writer typed IPAM address backend is not configured")
	}
	if a.ipamAdapter != nil {
		if err := addressInstaller.SetSubnetRuntimeIPAMAddressWriter(a.typedIPAMAddressWriter(a.ipamAdapter)); err != nil {
			return err
		}
		return virtualPortInstaller.SetSubnetRuntimeVirtualPortWriter(virtualPortWriter)
	}
	addressWriter := func(ctx context.Context, subnet *kubeovnv1.Subnet, request legacycontroller.SubnetRuntimeIPAMAddressRequest) (legacycontroller.SubnetRuntimeIPAMAddressResult, error) {
		if subnet == nil {
			return legacycontroller.SubnetRuntimeIPAMAddressResult{}, errors.New("subnet IPAM address writer requires a Subnet")
		}
		snapshot, err := NewSubnetWriteSnapshot(subnet, subnet.DeletionTimestamp.IsZero())
		if err != nil {
			return legacycontroller.SubnetRuntimeIPAMAddressResult{}, err
		}
		if request.SubnetName != "" && request.SubnetName != snapshot.Name {
			return legacycontroller.SubnetRuntimeIPAMAddressResult{}, fmt.Errorf("subnet IPAM address request targets Subnet %q while snapshot is %q", request.SubnetName, snapshot.Name)
		}
		request.SubnetName = snapshot.Name
		operation := SubnetWriteOperation(request.Operation)
		writer := &subnetSideEffectWriter{ctx: ctx, reader: a.Reader, gate: a.Gate}
		var result legacycontroller.SubnetRuntimeIPAMAddressResult
		_, err = writer.executePlan(snapshot, operation, SubnetSideEffectPlan{
			Apply: func(ctx context.Context) (SubnetSideEffectReceipt, error) {
				switch request.Operation {
				case legacycontroller.SubnetRuntimeIPAMAllocateAddressOperation:
					if existing, found, err := addressBackend.ExistingSubnetRuntimeIPAMAddress(ctx, request.PodName, request.NICName, request.SubnetName); err != nil {
						return nil, err
					} else if found {
						result = existing
						return nil, nil
					}
					if request.RequestedIP != "" {
						result, err = addressBackend.AllocateSubnetRuntimeIPAMStatic(ctx, request.PodName, request.NICName, request.RequestedIP, request.MAC, request.SubnetName, request.CheckConflict)
					} else {
						result, err = addressBackend.AllocateSubnetRuntimeIPAMRandom(ctx, request.PodName, request.NICName, request.SubnetName, request.PoolName, request.SkippedAddrs, request.CheckConflict)
					}
					return nil, err
				case legacycontroller.SubnetRuntimeIPAMReleaseAddressOperation:
					if request.NICName == "" {
						return nil, addressBackend.ReleaseSubnetRuntimeIPAMByPod(ctx, request.PodName, request.SubnetName)
					}
					nicName := request.NICName
					expectedV4, expectedV6, expectedMAC := request.ExpectedV4, request.ExpectedV6, request.ExpectedMAC
					if expectedV4 == "" && expectedV6 == "" && expectedMAC == "" {
						existing, found, readErr := addressBackend.ExistingSubnetRuntimeIPAMAddress(ctx, request.PodName, nicName, request.SubnetName)
						if readErr != nil || !found {
							return nil, readErr
						}
						expectedV4, expectedV6, expectedMAC = existing.V4, existing.V6, existing.MAC
					}
					err = addressBackend.ReleaseSubnetRuntimeIPAMByNicIfMatches(ctx, request.PodName, nicName, request.SubnetName, expectedV4, expectedV6, expectedMAC)
					return nil, err
				default:
					return nil, fmt.Errorf("unsupported Subnet IPAM address operation %q", request.Operation)
				}
			},
		})
		return result, err
	}
	if err := addressInstaller.SetSubnetRuntimeIPAMAddressWriter(addressWriter); err != nil {
		return err
	}
	return virtualPortInstaller.SetSubnetRuntimeVirtualPortWriter(virtualPortWriter)
}

func (a *SubnetControllerWriterAdapter) typedIPAMAddressWriter(adapter *subnetIPAMAdapter) legacycontroller.SubnetRuntimeIPAMAddressWriter {
	return func(ctx context.Context, subnet *kubeovnv1.Subnet, request legacycontroller.SubnetRuntimeIPAMAddressRequest) (legacycontroller.SubnetRuntimeIPAMAddressResult, error) {
		if subnet == nil {
			return legacycontroller.SubnetRuntimeIPAMAddressResult{}, errors.New("subnet IPAM address writer requires a Subnet")
		}
		snapshot, err := NewSubnetWriteSnapshot(subnet, subnet.DeletionTimestamp.IsZero())
		if err != nil {
			return legacycontroller.SubnetRuntimeIPAMAddressResult{}, err
		}
		if request.SubnetName != "" && request.SubnetName != snapshot.Name {
			return legacycontroller.SubnetRuntimeIPAMAddressResult{}, fmt.Errorf("subnet IPAM address request targets Subnet %q while snapshot is %q", request.SubnetName, snapshot.Name)
		}
		request.SubnetName = snapshot.Name
		switch request.Operation {
		case legacycontroller.SubnetRuntimeIPAMAllocateAddressOperation:
			var v4, v6, mac string
			if request.RequestedIP != "" {
				v4, v6, mac, _, err = adapter.AllocateStaticWithContext(ctx, snapshot, request.PodName, request.NICName, request.RequestedIP, request.MAC, request.SubnetName, request.CheckConflict)
			} else {
				v4, v6, mac, _, err = adapter.AllocateRandomWithContext(ctx, snapshot, request.PodName, request.NICName, request.PoolName)
			}
			return legacycontroller.SubnetRuntimeIPAMAddressResult{V4: v4, V6: v6, MAC: mac}, err
		case legacycontroller.SubnetRuntimeIPAMReleaseAddressOperation:
			if request.NICName == "" {
				return legacycontroller.SubnetRuntimeIPAMAddressResult{}, adapter.ReleaseByPodWithContext(ctx, snapshot, request.PodName)
			}
			return legacycontroller.SubnetRuntimeIPAMAddressResult{}, adapter.ReleaseByNicWithContext(ctx, snapshot, request.PodName, request.NICName, request.ExpectedV4, request.ExpectedV6, request.ExpectedMAC)
		default:
			return legacycontroller.SubnetRuntimeIPAMAddressResult{}, fmt.Errorf("unsupported Subnet IPAM address operation %q", request.Operation)
		}
	}
}

func (a *SubnetControllerWriterAdapter) RecoverSubnet(ctx context.Context, snapshot SubnetWriteSnapshot) error {
	if a == nil || a.Backend == nil {
		return errors.New("subnet controller writer backend is not configured")
	}
	if a.ipamAdapter != nil {
		if err := a.ipamAdapter.Recover(ctx, snapshot); err != nil {
			return err
		}
	}
	return a.Backend.RecoverSubnetRuntime(ctx, subnetFromSnapshot(snapshot))
}

func (a *SubnetControllerWriterAdapter) ReconcileSubnet(ctx context.Context, snapshot SubnetWriteSnapshot, _ SubnetShadowObservation) error {
	if a == nil || a.Backend == nil {
		return errors.New("subnet controller writer backend is not configured")
	}
	if a.Reader == nil {
		return ErrSubnetRuntimeReaderMissing
	}
	if err := a.Gate.WaitForRecovery(ctx); err != nil {
		return err
	}
	live := &kubeovnv1.Subnet{}
	if err := a.Reader.Get(ctx, client.ObjectKey{Name: snapshot.Name}, live); err != nil {
		return err
	}
	operation := SubnetStatusWriteOperation
	if snapshot.Deleting {
		operation = SubnetFinalizerRemoveOperation
	}
	if err := snapshot.Validate(live, operation); err != nil {
		return err
	}
	allocationAllowed := snapshot.AllocationAllowed
	if !snapshot.Deleting {
		// ProviderNetwork readiness can change between the shadow observation
		// and this writer admission, so re-read dependencies after checking
		// the Subnet identity.
		freshObservation, err := observeSubnet(ctx, a.Reader, live)
		if err != nil {
			return err
		}
		allocationAllowed = allocationAllowed && freshObservation.AllocationAllowed
		// The legacy runtime backend performs status, finalizer, IPAM, and OVN
		// work as one operation. Until those phases have separate writer seams,
		// fail closed before entering it when dependency readiness denies new
		// allocation.
		if !allocationAllowed {
			return fmt.Errorf("%w: subnet %s", ErrSubnetRuntimeAllocationNotAllowed, snapshot.Name)
		}
	}
	return a.reconcileRuntime(ctx, snapshot, allocationAllowed)
}

func (a *SubnetControllerWriterAdapter) ReconcileDeletedSubnet(ctx context.Context, snapshot SubnetWriteSnapshot) error {
	if a == nil || a.Backend == nil {
		return errors.New("subnet controller writer backend is not configured")
	}
	if !snapshot.Deleting {
		return errors.New("subnet delete snapshot is not terminating")
	}
	if a.Reader != nil {
		live := &kubeovnv1.Subnet{}
		if err := a.Reader.Get(ctx, client.ObjectKey{Name: snapshot.Name}, live); err == nil {
			if live.UID != snapshot.UID {
				return apierrors.NewConflict(schema.GroupResource{Group: kubeovnv1.SchemeGroupVersion.Group, Resource: "subnets"}, snapshot.Name, errors.New("replacement subnet has a different UID"))
			}
			return errors.New("deleting subnet snapshot still has a live API object")
		} else if !apierrors.IsNotFound(err) {
			return err
		}
	}
	if err := a.Gate.WaitForRecovery(ctx); err != nil {
		return err
	}
	return a.reconcileRuntime(ctx, snapshot, snapshot.AllocationAllowed)
}

func (a *SubnetControllerWriterAdapter) reconcileRuntime(ctx context.Context, snapshot SubnetWriteSnapshot, allocationAllowed bool) error {
	subnet := subnetFromSnapshot(snapshot)
	if gated, ok := a.Backend.(SubnetGatedDataPlaneBackend); ok {
		return gated.ReconcileSubnetRuntimeWithGate(ctx, subnet, allocationAllowed)
	}
	// The adapter has already performed active dependency admission. Keep the
	// old backend interface as a compatibility fallback for external
	// implementations that predate the gated seam.
	return a.Backend.ReconcileSubnetRuntime(ctx, subnet)
}

func subnetFromSnapshot(snapshot SubnetWriteSnapshot) *kubeovnv1.Subnet {
	subnet := &kubeovnv1.Subnet{
		Name:            snapshot.Name,
		UID:             snapshot.UID,
		Generation:      snapshot.Generation,
		ResourceVersion: snapshot.ResourceVersion,
		Finalizers:      append([]string(nil), snapshot.Finalizers...),
		Spec:            *snapshot.Spec.DeepCopy(),
		Status:          *snapshot.Status.DeepCopy(),
	}
	if snapshot.Deleting {
		subnet.DeletionTimestamp = new(metav1.Time)
		subnet.DeletionTimestamp.Time = metav1.Now().Time
	}
	return subnet
}

var _ SubnetWriterAdapter = (*SubnetControllerWriterAdapter)(nil)
