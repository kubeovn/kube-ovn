package controllerruntime

import (
	"context"
	"encoding/json/v2"
	"errors"
	"reflect"
	"slices"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// ipPoolWriter uses uncached reads so a delayed informer cannot authorize a
// write to a changed or same-name replacement object. Conflicts return to the
// runtime queue, which recomputes selection and prepares OVN on its next pass.
type ipPoolWriter struct {
	ctx    context.Context
	client client.Client
	reader client.Reader
}

const ipPoolWriteTimeout = 5 * time.Second

func (w *ipPoolWriter) get(name string) (*kubeovnv1.IPPool, error) {
	ctx, cancel := context.WithTimeout(w.ctx, ipPoolWriteTimeout)
	defer cancel()
	pool := &kubeovnv1.IPPool{}
	if err := w.reader.Get(ctx, types.NamespacedName{Name: name}, pool); err != nil {
		return nil, err
	}
	return pool, ctx.Err()
}

func sameIPPoolSpec(expected, current *kubeovnv1.IPPool) bool {
	return current.UID == expected.UID && current.Generation == expected.Generation &&
		equality.Semantic.DeepEqual(current.Spec, expected.Spec)
}

func ipPoolWriteConflict(pool *kubeovnv1.IPPool) error {
	return k8serrors.NewConflict(schema.GroupResource{Group: kubeovnv1.SchemeGroupVersion.Group, Resource: "ippools"}, pool.Name,
		errors.New("IPPool changed since the reconciliation snapshot"))
}

func (w *ipPoolWriter) current(expected *kubeovnv1.IPPool, deleting bool) (*kubeovnv1.IPPool, error) {
	current, err := w.get(expected.Name)
	if err != nil {
		return nil, err
	}
	if !sameIPPoolSpec(expected, current) || current.ResourceVersion != expected.ResourceVersion ||
		(!deleting && !current.DeletionTimestamp.IsZero()) {
		return nil, ipPoolWriteConflict(expected)
	}
	return current, nil
}

func (w *ipPoolWriter) PatchIPs(pool *kubeovnv1.IPPool, ips []string) error {
	current, err := w.current(pool, false)
	if err != nil {
		return err
	}
	desired := current.DeepCopy()
	desired.Status.IPs = slices.Clone(ips)
	if !slices.Equal(current.Status.IPs, ips) {
		ctx, cancel := context.WithTimeout(w.ctx, ipPoolWriteTimeout)
		err = w.client.Status().Patch(ctx, desired, client.MergeFromWithOptions(current, client.MergeFromWithOptimisticLock{}))
		cancel()
		if err != nil {
			return w.confirmIPs(pool, ips, err)
		}
	}
	if err := w.ctx.Err(); err != nil {
		return err
	}
	pool.ObjectMeta = *desired.ObjectMeta.DeepCopy()
	pool.Status.IPs = slices.Clone(ips)
	return nil
}

func (w *ipPoolWriter) confirmIPs(pool *kubeovnv1.IPPool, ips []string, writeErr error) error {
	if k8serrors.IsConflict(writeErr) {
		return writeErr
	}
	// A fresh deadline is necessary when the write timed out after committing.
	current, err := w.get(pool.Name)
	if err != nil {
		return errors.Join(writeErr, err)
	}
	if !sameIPPoolSpec(pool, current) || !current.DeletionTimestamp.IsZero() || !slices.Equal(current.Status.IPs, ips) {
		return writeErr
	}
	if err := w.ctx.Err(); err != nil {
		return errors.Join(writeErr, err)
	}
	pool.ObjectMeta = *current.ObjectMeta.DeepCopy()
	pool.Status.IPs = slices.Clone(ips)
	return nil
}

func (w *ipPoolWriter) PatchStatus(pool *kubeovnv1.IPPool) error {
	return w.patchStatus(pool, true)
}

func (w *ipPoolWriter) PatchStatistics(pool *kubeovnv1.IPPool) error {
	return w.patchStatus(pool, false)
}

func (w *ipPoolWriter) patchStatus(pool *kubeovnv1.IPPool, conditions bool) error {
	current, err := w.current(pool, false)
	if err != nil {
		return err
	}
	desired := current.DeepCopy()
	copyIPPoolStatistics(&desired.Status, &pool.Status)
	if conditions {
		for _, condition := range pool.Status.Conditions {
			if condition.Type != kubeovnv1.Ready && condition.Type != kubeovnv1.Error {
				continue
			}
			index := slices.IndexFunc(desired.Status.Conditions, func(c kubeovnv1.Condition) bool { return c.Type == condition.Type })
			if index < 0 {
				desired.Status.Conditions = append(desired.Status.Conditions, condition)
			} else {
				desired.Status.Conditions[index] = condition
			}
		}
	}
	if !reflect.DeepEqual(current.Status, desired.Status) {
		// A typed merge diff cannot distinguish an absent statistic from zero.
		// Send every statistic explicitly, without claiming membership ownership.
		status := desired.Status
		status.IPs = nil
		if !conditions {
			status.Conditions = nil
		}
		data, err := json.Marshal(struct {
			Metadata map[string]string      `json:"metadata"`
			Status   kubeovnv1.IPPoolStatus `json:"status"`
		}{Metadata: map[string]string{"resourceVersion": current.ResourceVersion}, Status: status})
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(w.ctx, ipPoolWriteTimeout)
		defer cancel()
		if err := w.client.Status().Patch(ctx, desired, client.RawPatch(types.MergePatchType, data)); err != nil {
			return err
		}
	}
	if err := w.ctx.Err(); err != nil {
		return err
	}
	pool.ObjectMeta = *desired.ObjectMeta.DeepCopy()
	if conditions {
		pool.Status.Conditions = slices.Clone(desired.Status.Conditions)
	}
	return nil
}

func copyIPPoolStatistics(dst, src *kubeovnv1.IPPoolStatus) {
	dst.V4AvailableIPs = src.V4AvailableIPs.Clone()
	dst.V4UsingIPs = src.V4UsingIPs.Clone()
	dst.V6AvailableIPs = src.V6AvailableIPs.Clone()
	dst.V6UsingIPs = src.V6UsingIPs.Clone()
	dst.V4AvailableIPRange = src.V4AvailableIPRange
	dst.V4UsingIPRange = src.V4UsingIPRange
	dst.V6AvailableIPRange = src.V6AvailableIPRange
	dst.V6UsingIPRange = src.V6UsingIPRange
}

func (w *ipPoolWriter) AddFinalizer(pool *kubeovnv1.IPPool) error {
	return w.patchFinalizer(pool, false)
}

func (w *ipPoolWriter) RemoveFinalizer(pool *kubeovnv1.IPPool) error {
	return w.patchFinalizer(pool, true)
}

func (w *ipPoolWriter) patchFinalizer(pool *kubeovnv1.IPPool, remove bool) error {
	current, err := w.current(pool, remove)
	if err != nil {
		if remove && k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	desired := current.DeepCopy()
	if remove {
		controllerutil.RemoveFinalizer(desired, util.DeprecatedFinalizerName)
		controllerutil.RemoveFinalizer(desired, util.KubeOVNControllerFinalizer)
	} else {
		controllerutil.AddFinalizer(desired, util.KubeOVNControllerFinalizer)
	}
	if !slices.Equal(current.Finalizers, desired.Finalizers) {
		ctx, cancel := context.WithTimeout(w.ctx, ipPoolWriteTimeout)
		defer cancel()
		if err := w.client.Patch(ctx, desired, client.MergeFromWithOptions(current, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}
	if err := w.ctx.Err(); err != nil {
		return err
	}
	pool.ObjectMeta = *desired.ObjectMeta.DeepCopy()
	return nil
}
