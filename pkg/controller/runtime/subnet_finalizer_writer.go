package controllerruntime

import (
	"context"
	"errors"
	"slices"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const subnetFinalizerWriteTimeout = 5 * time.Second

// subnetFinalizerWriter persists only Subnet finalizers. It is deliberately
// unwired while the legacy Subnet controller remains the production writer.
type subnetFinalizerWriter struct {
	ctx    context.Context
	client client.Client
	reader client.Reader
	gate   SubnetWriterGate
}

func (w *subnetFinalizerWriter) get(name string) (*kubeovnv1.Subnet, error) {
	return getSubnet(w.ctx, w.reader, name, subnetFinalizerWriteTimeout)
}

func (w *subnetFinalizerWriter) current(expected *kubeovnv1.Subnet, remove bool) (*kubeovnv1.Subnet, error) {
	current, err := w.get(expected.Name)
	if err != nil {
		return nil, err
	}
	operation := SubnetFinalizerAddOperation
	if remove {
		operation = SubnetFinalizerRemoveOperation
	}
	snapshot, err := NewSubnetWriteSnapshot(expected, true)
	if err != nil {
		return nil, err
	}
	if err := w.gate.Validate(w.ctx, snapshot, current, operation); err != nil {
		return nil, err
	}
	return current, nil
}

func (w *subnetFinalizerWriter) AddFinalizer(subnet *kubeovnv1.Subnet) error {
	return w.patchFinalizer(subnet, false)
}

func (w *subnetFinalizerWriter) RemoveFinalizer(subnet *kubeovnv1.Subnet) error {
	return w.patchFinalizer(subnet, true)
}

func (w *subnetFinalizerWriter) patchFinalizer(subnet *kubeovnv1.Subnet, remove bool) error {
	if subnet == nil {
		return errors.New("subnet finalizer writer requires a Subnet")
	}
	current, err := w.current(subnet, remove)
	if err != nil {
		if remove && apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	desired := current.DeepCopy()
	if remove {
		controllerutil.RemoveFinalizer(desired, util.DeprecatedFinalizerName)
		controllerutil.RemoveFinalizer(desired, util.KubeOVNControllerFinalizer)
	} else {
		controllerutil.RemoveFinalizer(desired, util.DeprecatedFinalizerName)
		controllerutil.AddFinalizer(desired, util.KubeOVNControllerFinalizer)
	}
	if slices.Equal(current.Finalizers, desired.Finalizers) {
		return w.sync(subnet, desired)
	}
	ctx, cancel := context.WithTimeout(w.ctx, subnetFinalizerWriteTimeout)
	err = w.client.Patch(ctx, desired, client.MergeFromWithOptions(current, client.MergeFromWithOptimisticLock{}))
	cancel()
	if err != nil {
		return w.confirm(subnet, desired.Finalizers, remove, err)
	}
	return w.sync(subnet, desired)
}

func (w *subnetFinalizerWriter) sync(subnet, desired *kubeovnv1.Subnet) error {
	return syncSubnetMetadata(w.ctx, subnet, desired)
}

func (w *subnetFinalizerWriter) confirm(subnet *kubeovnv1.Subnet, finalizers []string, remove bool, writeErr error) error {
	if apierrors.IsConflict(writeErr) {
		return writeErr
	}
	current, err := confirmSubnetWrite(w.ctx, w.reader, w.gate, subnet, subnetFinalizerWriteTimeout, writeErr, func(current *kubeovnv1.Subnet) bool {
		return (remove == !current.DeletionTimestamp.IsZero()) && slices.Equal(current.Finalizers, finalizers)
	})
	if err != nil {
		return err
	}
	return w.sync(subnet, current)
}
