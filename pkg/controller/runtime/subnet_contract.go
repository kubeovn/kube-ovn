package controllerruntime

import (
	"context"
	"errors"
	"fmt"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// SubnetEventPath describes the legacy work that a future Subnet shadow may
// observe. It does not enqueue a legacy queue or write a Kubernetes resource.
type SubnetEventPath string

const (
	SubnetSpecReconcilePath     SubnetEventPath = "spec-reconcile"
	SubnetStatusObservePath     SubnetEventPath = "status-observe"
	SubnetDependencyObservePath SubnetEventPath = "dependency-observe"
)

// SubnetDependencyEvent is the stable event vocabulary for the Subnet
// migration. Producers remain legacy-owned until a future writer is enabled.
type SubnetDependencyEvent struct {
	SourceKind string
	StatusOnly bool
}

// ClassifySubnetDependencyEvent maps an observed dependency event to the
// future Subnet path. IP and workload changes only affect derived status;
// Node changes belong to ProviderNetwork and are observed indirectly.
func ClassifySubnetDependencyEvent(event SubnetDependencyEvent) SubnetEventPath {
	if event.SourceKind == "Node" {
		return SubnetDependencyObservePath
	}
	if event.StatusOnly || slices.Contains([]string{"IP", "Pod", "VIP", "EIP", "NAT"}, event.SourceKind) {
		return SubnetStatusObservePath
	}
	return SubnetSpecReconcilePath
}

// SubnetAllocationGate is the read-only readiness contract for a future
// Subnet writer. A false result means that no IPAM allocation or OVN mutation
// may start. The legacy validator remains the current production behavior.
func SubnetAllocationGate(vlan *kubeovnv1.Vlan, providerNetwork *kubeovnv1.ProviderNetwork) bool {
	if vlan == nil || providerNetwork == nil || vlan.Status.Conflict || !providerNetwork.Status.Ready {
		return false
	}
	return slices.Contains(providerNetwork.Status.Vlans, vlan.Name)
}

// SubnetShadowObservation contains only cached/read API state. It is an
// intentionally unwired seam: constructing an observation has no status,
// finalizer, OVN, IPAM, or queue side effects.
type SubnetShadowObservation struct {
	Name                   string
	Generation             int64
	Deleting               bool
	HasControllerFinalizer bool
	HasDeprecatedFinalizer bool
	Status                 SubnetStatusObservation
	VlanName               string
	VlanFound              bool
	VlanConflict           bool
	ProviderFound          bool
	ProviderReady          bool
	ProviderHasVlan        bool
	AllocationAllowed      bool
}

// SubnetStatusObservation is a copy of the status surfaces that a future
// status-only path may compare. Big integer counters are represented as
// strings so the observation cannot share mutable numeric state with the API
// object.
type SubnetStatusObservation struct {
	Conditions         []kubeovnv1.Condition
	V4AvailableIPs     string
	V4AvailableIPRange string
	V4UsingIPs         string
	V4UsingIPRange     string
	V6AvailableIPs     string
	V6AvailableIPRange string
	V6UsingIPs         string
	V6UsingIPRange     string
	GatewayMAC         string
}

// ObserveSubnet reads a Subnet and its VLAN dependency without mutating any
// object. Missing dependencies are represented in the observation so a future
// shadow can report readiness without turning a transient absence into a
// failed status write.
func ObserveSubnet(ctx context.Context, reader client.Reader, name string) (SubnetShadowObservation, error) {
	var subnet kubeovnv1.Subnet
	if err := reader.Get(ctx, types.NamespacedName{Name: name}, &subnet); err != nil {
		return SubnetShadowObservation{}, fmt.Errorf("get subnet %s: %w", name, err)
	}
	return observeSubnet(ctx, reader, &subnet)
}

// observeSubnet derives the dependency observation from an already-read
// Subnet. Reconciler callers use this helper to keep the write snapshot and
// observation tied to the same API read.
func observeSubnet(ctx context.Context, reader client.Reader, subnet *kubeovnv1.Subnet) (SubnetShadowObservation, error) {
	if subnet == nil {
		return SubnetShadowObservation{}, errors.New("subnet observation requires a Subnet")
	}
	observation := SubnetShadowObservation{
		Name:                   subnet.Name,
		Generation:             subnet.Generation,
		Deleting:               !subnet.DeletionTimestamp.IsZero(),
		HasControllerFinalizer: slices.Contains(subnet.Finalizers, util.KubeOVNControllerFinalizer),
		HasDeprecatedFinalizer: slices.Contains(subnet.Finalizers, util.DeprecatedFinalizerName),
		Status: SubnetStatusObservation{
			Conditions:         slices.Clone(subnet.Status.Conditions),
			V4AvailableIPs:     subnet.Status.V4AvailableIPs.String(),
			V4AvailableIPRange: subnet.Status.V4AvailableIPRange,
			V4UsingIPs:         subnet.Status.V4UsingIPs.String(),
			V4UsingIPRange:     subnet.Status.V4UsingIPRange,
			V6AvailableIPs:     subnet.Status.V6AvailableIPs.String(),
			V6AvailableIPRange: subnet.Status.V6AvailableIPRange,
			V6UsingIPs:         subnet.Status.V6UsingIPs.String(),
			V6UsingIPRange:     subnet.Status.V6UsingIPRange,
			GatewayMAC:         subnet.Status.GatewayMAC,
		},
		VlanName: subnet.Spec.Vlan,
	}
	if subnet.Spec.Vlan == "" {
		observation.AllocationAllowed = true
		return observation, nil
	}

	var vlan kubeovnv1.Vlan
	if err := reader.Get(ctx, types.NamespacedName{Name: subnet.Spec.Vlan}, &vlan); err != nil {
		if apierrors.IsNotFound(err) {
			return observation, nil
		}
		return SubnetShadowObservation{}, fmt.Errorf("get vlan %s for subnet %s: %w", subnet.Spec.Vlan, subnet.Name, err)
	}
	observation.VlanFound = true
	observation.VlanConflict = vlan.Status.Conflict

	var provider kubeovnv1.ProviderNetwork
	if err := reader.Get(ctx, types.NamespacedName{Name: vlan.Spec.Provider}, &provider); err != nil {
		if apierrors.IsNotFound(err) {
			return observation, nil
		}
		return SubnetShadowObservation{}, fmt.Errorf("get provider network %s for subnet %s: %w", vlan.Spec.Provider, subnet.Name, err)
	}
	observation.ProviderFound = true
	observation.ProviderReady = provider.Status.Ready
	observation.ProviderHasVlan = slices.Contains(provider.Status.Vlans, vlan.Name)
	observation.AllocationAllowed = SubnetAllocationGate(&vlan, &provider)
	return observation, nil
}
