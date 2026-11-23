package controllerruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func newSubnetStatusWriterTest(t *testing.T, funcs interceptor.Funcs) (*subnetStatusWriter, client.WithWatch, *kubeovnv1.Subnet) {
	t.Helper()
	scheme, err := NewScheme()
	require.NoError(t, err)
	subnet := testSubnet("")
	subnet.UID = "original"
	subnet.Generation = 1
	api := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&kubeovnv1.Subnet{}).WithObjects(subnet).Build()
	w := &subnetStatusWriter{
		ctx:    t.Context(),
		client: interceptor.NewClient(api, funcs),
		reader: api,
		gate:   SubnetWriterGate{Recovery: func(context.Context) error { return nil }},
	}
	current, err := w.get(subnet.Name)
	require.NoError(t, err)
	return w, api, current
}

func TestSubnetStatusWriterPreservesForeignConditions(t *testing.T) {
	patches := 0
	w, api, subnet := newSubnetStatusWriterTest(t, interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, c client.Client, resource string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			patches++
			return c.SubResource(resource).Patch(ctx, obj, patch, opts...)
		},
	})
	subnet.Status.Conditions = []kubeovnv1.Condition{{Type: "Foreign", Status: corev1.ConditionTrue}}
	require.NoError(t, api.Status().Update(t.Context(), subnet))
	subnet, err := w.get(subnet.Name)
	require.NoError(t, err)
	subnet.Status.Ready("Prepared", "")
	subnet.Status.GatewayMAC = "00:11:22:33:44:55"
	require.NoError(t, w.PatchStatus(subnet))
	require.Equal(t, 1, patches)
	got, err := w.get(subnet.Name)
	require.NoError(t, err)
	require.Equal(t, "Foreign", string(got.Status.Conditions[0].Type))
	require.True(t, got.Status.IsReady())
	require.Equal(t, "00:11:22:33:44:55", got.Status.GatewayMAC)
}

func TestSubnetStatusWriterNoopDoesNotPatch(t *testing.T) {
	patches := 0
	w, _, subnet := newSubnetStatusWriterTest(t, interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, c client.Client, resource string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			patches++
			return c.SubResource(resource).Patch(ctx, obj, patch, opts...)
		},
	})
	require.NoError(t, w.PatchStatus(subnet))
	require.NoError(t, w.PatchGatewayMAC(subnet, ""))
	require.Zero(t, patches)
}

func TestSubnetStatusWriterRejectsNilSubnet(t *testing.T) {
	w, _, _ := newSubnetStatusWriterTest(t, interceptor.Funcs{})
	require.EqualError(t, w.PatchStatus(nil), "subnet status writer requires a Subnet")
	require.EqualError(t, w.PatchGatewayMAC(nil, "00:11:22:33:44:55"), "subnet status writer requires a Subnet")
}

func TestSubnetStatusWriterRejectsConflictsAndTermination(t *testing.T) {
	for _, change := range []string{"resource version", "replacement UID", "spec", "terminating"} {
		t.Run(change, func(t *testing.T) {
			w, api, subnet := newSubnetStatusWriterTest(t, interceptor.Funcs{})
			live := subnet.DeepCopy()
			switch change {
			case "resource version":
				live.Status.GatewayMAC = "concurrent"
				require.NoError(t, api.Status().Update(t.Context(), live))
			case "replacement UID":
				live.UID = "replacement"
				require.NoError(t, api.Update(t.Context(), live))
			case "spec":
				live.Spec.Vlan = "changed"
				require.NoError(t, api.Update(t.Context(), live))
			case "terminating":
				require.NoError(t, api.Delete(t.Context(), live))
			}
			subnet.Status.Ready("Prepared", "")
			err := w.PatchStatus(subnet)
			if change == "terminating" {
				require.Error(t, err)
			} else {
				require.True(t, apierrors.IsConflict(err), err)
			}
		})
	}
}

func TestSubnetStatusWriterConfirmsCommittedWriteAfterTimeout(t *testing.T) {
	writeErr := errors.New("response lost")
	patches := 0
	w, _, subnet := newSubnetStatusWriterTest(t, interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, c client.Client, resource string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			patches++
			if patches == 1 {
				require.NoError(t, c.SubResource(resource).Patch(ctx, obj, patch, opts...))
				return writeErr
			}
			return c.SubResource(resource).Patch(ctx, obj, patch, opts...)
		},
	})
	subnet.Status.Ready("Prepared", "")
	require.NoError(t, w.PatchStatus(subnet))
	require.Equal(t, 1, patches)
	require.True(t, subnet.Status.IsReady())
}

func TestSubnetStatusWriterRejectsReplacementAfterTimeout(t *testing.T) {
	writeErr := errors.New("response lost")
	w, _, subnet := newSubnetStatusWriterTest(t, interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, c client.Client, resource string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			require.NoError(t, c.SubResource(resource).Patch(ctx, obj, patch, opts...))
			live := &kubeovnv1.Subnet{}
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), live))
			live.UID = "replacement"
			require.NoError(t, c.Update(ctx, live))
			return writeErr
		},
	})
	subnet.Status.Ready("Prepared", "")
	err := w.PatchStatus(subnet)
	require.ErrorIs(t, err, writeErr)
	got, err := w.get(subnet.Name)
	require.NoError(t, err)
	require.Equal(t, "replacement", string(got.UID))
}

func TestSubnetStatusWriterPatchesGatewayMACWithSnapshot(t *testing.T) {
	w, api, subnet := newSubnetStatusWriterTest(t, interceptor.Funcs{})
	require.NoError(t, w.PatchGatewayMAC(subnet, "00:11:22:33:44:55"))
	require.Equal(t, "00:11:22:33:44:55", subnet.Status.GatewayMAC)
	got, err := w.get(subnet.Name)
	require.NoError(t, err)
	require.Equal(t, subnet.ResourceVersion, got.ResourceVersion)

	got.Finalizers = []string{"example.com/hold"}
	require.NoError(t, api.Update(t.Context(), got))
	require.NoError(t, api.Delete(t.Context(), got))
	require.Error(t, w.PatchGatewayMAC(subnet, "00:aa:bb:cc:dd:ee"))
}
