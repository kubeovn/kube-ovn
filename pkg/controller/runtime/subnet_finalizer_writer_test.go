package controllerruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newSubnetFinalizerWriterTest(t *testing.T, funcs interceptor.Funcs) (*subnetFinalizerWriter, client.WithWatch, *kubeovnv1.Subnet) {
	t.Helper()
	scheme, err := NewScheme()
	require.NoError(t, err)
	subnet := testSubnet("")
	subnet.UID = "original"
	subnet.Generation = 1
	api := fake.NewClientBuilder().WithScheme(scheme).WithObjects(subnet).Build()
	w := &subnetFinalizerWriter{
		ctx:    t.Context(),
		client: interceptor.NewClient(api, funcs),
		reader: api,
		gate:   SubnetWriterGate{Recovery: func(context.Context) error { return nil }},
	}
	current, err := w.get(subnet.Name)
	require.NoError(t, err)
	return w, api, current
}

func TestSubnetFinalizerWriterMigratesDeprecatedAndPreservesForeign(t *testing.T) {
	patches := 0
	w, api, subnet := newSubnetFinalizerWriterTest(t, interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			patches++
			return c.Patch(ctx, obj, patch, opts...)
		},
	})
	subnet.Finalizers = []string{"example.com/foreign", util.DeprecatedFinalizerName}
	require.NoError(t, api.Update(t.Context(), subnet))
	subnet, err := w.get(subnet.Name)
	require.NoError(t, err)
	require.NoError(t, w.AddFinalizer(subnet))
	require.Equal(t, []string{"example.com/foreign", util.KubeOVNControllerFinalizer}, subnet.Finalizers)
	require.Equal(t, 1, patches)
	require.NoError(t, w.AddFinalizer(subnet))
	require.Equal(t, 1, patches, "matching finalizer state must not patch")
}

func TestSubnetFinalizerWriterRemovesOwnedFinalizersOnTermination(t *testing.T) {
	w, api, subnet := newSubnetFinalizerWriterTest(t, interceptor.Funcs{})
	subnet.Finalizers = []string{"example.com/foreign", util.DeprecatedFinalizerName, util.KubeOVNControllerFinalizer}
	require.NoError(t, api.Update(t.Context(), subnet))
	require.NoError(t, api.Delete(t.Context(), subnet))
	terminating, err := w.get(subnet.Name)
	require.NoError(t, err)
	require.NoError(t, w.RemoveFinalizer(terminating))
	require.Equal(t, []string{"example.com/foreign"}, terminating.Finalizers)
	got, err := w.get(subnet.Name)
	require.NoError(t, err)
	require.Equal(t, []string{"example.com/foreign"}, got.Finalizers)
}

func TestSubnetFinalizerWriterRejectsStaleAndInvalidSnapshots(t *testing.T) {
	for _, change := range []string{"resource version", "replacement UID", "spec", "deletion"} {
		t.Run(change, func(t *testing.T) {
			w, api, subnet := newSubnetFinalizerWriterTest(t, interceptor.Funcs{})
			live := subnet.DeepCopy()
			switch change {
			case "resource version":
				live.Labels = map[string]string{"changed": "true"}
				require.NoError(t, api.Update(t.Context(), live))
			case "replacement UID":
				live.UID = "replacement"
				require.NoError(t, api.Update(t.Context(), live))
			case "spec":
				live.Spec.Vlan = "changed"
				require.NoError(t, api.Update(t.Context(), live))
			case "deletion":
				live.Finalizers = []string{"example.com/hold"}
				require.NoError(t, api.Update(t.Context(), live))
				require.NoError(t, api.Delete(t.Context(), live))
			}
			err := w.AddFinalizer(subnet)
			require.True(t, apierrors.IsConflict(err), err)
		})
	}

	w, _, active := newSubnetFinalizerWriterTest(t, interceptor.Funcs{})
	require.Error(t, w.RemoveFinalizer(active), "removing from an active object must fail closed")
}

func TestSubnetFinalizerWriterConfirmsCommittedWriteAfterTimeout(t *testing.T) {
	writeErr := errors.New("response lost")
	patches := 0
	w, _, subnet := newSubnetFinalizerWriterTest(t, interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			patches++
			require.NoError(t, c.Patch(ctx, obj, patch, opts...))
			return writeErr
		},
	})
	require.NoError(t, w.AddFinalizer(subnet))
	require.Equal(t, 1, patches)
	require.Contains(t, subnet.Finalizers, util.KubeOVNControllerFinalizer)
}

func TestSubnetFinalizerWriterRejectsReplacementAfterTimeout(t *testing.T) {
	writeErr := errors.New("response lost")
	w, _, subnet := newSubnetFinalizerWriterTest(t, interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			require.NoError(t, c.Patch(ctx, obj, patch, opts...))
			live := &kubeovnv1.Subnet{}
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), live))
			live.UID = "replacement"
			require.NoError(t, c.Update(ctx, live))
			return writeErr
		},
	})
	err := w.AddFinalizer(subnet)
	require.ErrorIs(t, err, writeErr)
	got, err := w.get(subnet.Name)
	require.NoError(t, err)
	require.Equal(t, "replacement", string(got.UID))
}

func TestSubnetFinalizerWriterConvergesWhenObjectAlreadyDeleted(t *testing.T) {
	w, api, subnet := newSubnetFinalizerWriterTest(t, interceptor.Funcs{})
	require.NoError(t, api.Delete(t.Context(), subnet))
	require.NoError(t, w.RemoveFinalizer(subnet))
}

func TestSubnetFinalizerWriterRejectsNilSubnet(t *testing.T) {
	w, _, _ := newSubnetFinalizerWriterTest(t, interceptor.Funcs{})
	require.EqualError(t, w.AddFinalizer(nil), "subnet finalizer writer requires a Subnet")
	require.EqualError(t, w.RemoveFinalizer(nil), "subnet finalizer writer requires a Subnet")
}
