package controllerruntime

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

const subnetStatusWriteTimeout = 5 * time.Second

// subnetStatusWriter persists only status and uses uncached reads immediately
// before every write. It remains an isolated API seam; subnet-writer mode
// delegates the complete domain sequence to the Controller facade so status,
// finalizer, IPAM, and OVN mutations share one owner.
type subnetStatusWriter struct {
	ctx    context.Context
	client client.Client
	reader client.Reader
	gate   SubnetWriterGate
}

func (w *subnetStatusWriter) get(name string) (*kubeovnv1.Subnet, error) {
	return getSubnet(w.ctx, w.reader, name, subnetStatusWriteTimeout)
}

func sameSubnetSpec(expected, current *kubeovnv1.Subnet) bool {
	return expected.UID == current.UID && expected.Generation == current.Generation &&
		equality.Semantic.DeepEqual(expected.Spec, current.Spec)
}

func (w *subnetStatusWriter) current(expected *kubeovnv1.Subnet) (*kubeovnv1.Subnet, error) {
	current, err := w.get(expected.Name)
	if err != nil {
		return nil, err
	}
	snapshot, err := NewSubnetWriteSnapshot(expected, true)
	if err != nil {
		return nil, err
	}
	if err := w.gate.Validate(w.ctx, snapshot, current, SubnetStatusWriteOperation); err != nil {
		return nil, err
	}
	return current, nil
}

// PatchStatus updates all status fields owned by the Subnet writer while
// retaining condition types owned by other controllers. A resource-version
// conflict is returned to reconciliation without replaying the old status.
func (w *subnetStatusWriter) PatchStatus(subnet *kubeovnv1.Subnet) error {
	if subnet == nil {
		return errors.New("subnet status writer requires a Subnet")
	}
	current, err := w.current(subnet)
	if err != nil {
		return err
	}
	desired := current.DeepCopy()
	desired.Status = *subnet.Status.DeepCopy()
	desired.Status.Conditions = mergeSubnetConditions(current.Status.Conditions, subnet.Status.Conditions)
	if reflect.DeepEqual(current.Status, desired.Status) {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		subnet.Status = *desired.Status.DeepCopy()
		return nil
	}
	return w.patchStatus(subnet, current, desired, func(live *kubeovnv1.Subnet) bool {
		return sameSubnetStatus(&live.Status, &desired.Status)
	})
}

// PatchGatewayMAC updates the gateway MAC without requiring callers to carry
// a full, freshly calculated Subnet status. It shares the same snapshot and
// optimistic-lock checks as PatchStatus.
func (w *subnetStatusWriter) PatchGatewayMAC(subnet *kubeovnv1.Subnet, gatewayMAC string) error {
	if subnet == nil {
		return errors.New("subnet status writer requires a Subnet")
	}
	current, err := w.current(subnet)
	if err != nil {
		return err
	}
	if current.Status.GatewayMAC == gatewayMAC {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		subnet.ObjectMeta = *current.ObjectMeta.DeepCopy()
		subnet.Status.GatewayMAC = gatewayMAC
		return nil
	}
	desired := current.DeepCopy()
	desired.Status.GatewayMAC = gatewayMAC
	return w.patchStatus(subnet, current, desired, func(live *kubeovnv1.Subnet) bool {
		return live.Status.GatewayMAC == gatewayMAC
	})
}

func (w *subnetStatusWriter) patchStatus(subnet, current, desired *kubeovnv1.Subnet, matches func(*kubeovnv1.Subnet) bool) error {
	ctx, cancel := context.WithTimeout(w.ctx, subnetStatusWriteTimeout)
	err := w.client.Status().Patch(ctx, desired, client.MergeFromWithOptions(current, client.MergeFromWithOptimisticLock{}))
	cancel()
	if err != nil {
		return w.confirmStatus(subnet, matches, err)
	}
	if err := w.ctx.Err(); err != nil {
		return err
	}
	subnet.ObjectMeta = *desired.ObjectMeta.DeepCopy()
	subnet.Status = *desired.Status.DeepCopy()
	return nil
}

func (w *subnetStatusWriter) confirmStatus(subnet *kubeovnv1.Subnet, matches func(*kubeovnv1.Subnet) bool, writeErr error) error {
	if apierrors.IsConflict(writeErr) {
		return writeErr
	}
	current, err := confirmSubnetWrite(w.ctx, w.reader, w.gate, subnet, subnetStatusWriteTimeout, writeErr, func(current *kubeovnv1.Subnet) bool {
		return current.DeletionTimestamp.IsZero() && matches(current)
	})
	if err != nil {
		return err
	}
	if err := syncSubnetMetadata(w.ctx, subnet, current); err != nil {
		return err
	}
	subnet.Status = *current.Status.DeepCopy()
	return nil
}

func mergeSubnetConditions(current, desired []kubeovnv1.Condition) []kubeovnv1.Condition {
	merged := slices.Clone(current)
	for _, condition := range desired {
		if condition.Type != kubeovnv1.Ready && condition.Type != kubeovnv1.Validated && condition.Type != kubeovnv1.Error {
			continue
		}
		index := slices.IndexFunc(merged, func(existing kubeovnv1.Condition) bool { return existing.Type == condition.Type })
		if index < 0 {
			merged = append(merged, condition)
		} else {
			merged[index] = condition
		}
	}
	return merged
}

func sameSubnetStatus(current, desired *kubeovnv1.SubnetStatus) bool {
	currentCopy := current.DeepCopy()
	desiredCopy := desired.DeepCopy()
	currentCopy.Conditions = ownedSubnetConditions(currentCopy.Conditions)
	desiredCopy.Conditions = ownedSubnetConditions(desiredCopy.Conditions)
	return reflect.DeepEqual(currentCopy, desiredCopy)
}

func ownedSubnetConditions(conditions []kubeovnv1.Condition) []kubeovnv1.Condition {
	owned := make([]kubeovnv1.Condition, 0, len(conditions))
	for _, condition := range conditions {
		if condition.Type == kubeovnv1.Ready || condition.Type == kubeovnv1.Validated || condition.Type == kubeovnv1.Error {
			owned = append(owned, condition)
		}
	}
	return owned
}
