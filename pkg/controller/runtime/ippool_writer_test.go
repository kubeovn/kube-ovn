package controllerruntime

import (
	"context"
	"encoding/json/v2"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newIPPoolWriterTest(t *testing.T, funcs interceptor.Funcs) (*ipPoolWriter, client.WithWatch, *kubeovnv1.IPPool) {
	t.Helper()
	scheme, err := NewScheme()
	require.NoError(t, err)
	pool := &kubeovnv1.IPPool{Name: "pool", UID: "original", Generation: 1, Spec: kubeovnv1.IPPoolSpec{Subnet: "subnet", IPCount: 2}}
	api := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&kubeovnv1.IPPool{}).WithObjects(pool).Build()
	w := &ipPoolWriter{ctx: t.Context(), client: interceptor.NewClient(api, funcs), reader: api}
	current, err := w.get(pool.Name)
	require.NoError(t, err)
	return w, api, current
}

func TestIPPoolWriterSequencesWritesWithoutOwningForeignFields(t *testing.T) {
	patches := 0
	checkPatch := func(obj client.Object, patch client.Patch) {
		patches++
		data, err := patch.Data(obj)
		require.NoError(t, err)
		var payload struct {
			Metadata struct {
				ResourceVersion string `json:"resourceVersion"`
			} `json:"metadata"`
		}
		require.NoError(t, json.Unmarshal(data, &payload))
		require.Equal(t, obj.GetResourceVersion(), payload.Metadata.ResourceVersion)
		require.NotEmpty(t, payload.Metadata.ResourceVersion)
	}
	w, api, pool := newIPPoolWriterTest(t, interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			checkPatch(obj, patch)
			return c.Patch(ctx, obj, patch, opts...)
		},
		SubResourcePatch: func(ctx context.Context, c client.Client, resource string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			checkPatch(obj, patch)
			return c.SubResource(resource).Patch(ctx, obj, patch, opts...)
		},
	})
	pool.Finalizers = []string{"example.com/foreign", util.DeprecatedFinalizerName}
	require.NoError(t, api.Update(t.Context(), pool))
	pool.Status.Conditions = []kubeovnv1.Condition{{Type: "Foreign", Status: corev1.ConditionTrue}}
	require.NoError(t, api.Status().Update(t.Context(), pool))
	var err error
	pool, err = w.get(pool.Name)
	require.NoError(t, err)
	pool.Status.EnsureStandardConditions()
	require.NoError(t, w.AddFinalizer(pool))
	require.Len(t, pool.Status.Conditions, 2, "metadata writes must keep pending Ready")
	require.NoError(t, w.PatchIPs(pool, []string{"10.0.0.2", "10.0.0.3"}))
	require.Len(t, pool.Status.Conditions, 2, "membership writes must keep pending Ready")
	pool.Status.Ready("Prepared", "")
	pool.Status.V4AvailableIPs.SetInt64(2)
	require.NoError(t, w.PatchStatus(pool))
	version := pool.ResourceVersion
	require.NoError(t, w.PatchStatus(pool))
	require.NoError(t, w.PatchIPs(pool, pool.Status.IPs))
	require.Equal(t, version, pool.ResourceVersion, "matching writes must settle")
	// A statistics task carries stale conditions and membership but owns neither.
	pool.Status.Conditions = nil
	pool.Status.IPs = []string{"10.0.0.6"}
	pool.Status.V4AvailableIPs.SetInt64(0)
	require.NoError(t, w.PatchStatistics(pool))
	got, err := w.get(pool.Name)
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.2", "10.0.0.3"}, got.Status.IPs)
	require.True(t, got.Status.IsReady())
	require.Equal(t, kubeovnv1.ConditionType("Foreign"), got.Status.Conditions[0].Type)
	require.Zero(t, got.Status.V4AvailableIPs.Int64())
	require.NoError(t, w.RemoveFinalizer(pool))
	require.Equal(t, []string{"example.com/foreign"}, pool.Finalizers)
	require.Equal(t, 5, patches)
}

func TestIPPoolWriterConflictsReturnWithoutReplayingOldState(t *testing.T) {
	for _, operation := range []string{"membership", "statistics", "conditions", "finalizer"} {
		t.Run(operation, func(t *testing.T) {
			patches := 0
			concurrentChange := func(c client.Client, obj client.Object) {
				patches++
				live := &kubeovnv1.IPPool{}
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), live))
				live.Finalizers = append(live.Finalizers, "example.com/concurrent")
				require.NoError(t, c.Update(t.Context(), live))
			}
			w, _, pool := newIPPoolWriterTest(t, interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					concurrentChange(c, obj)
					return c.Patch(ctx, obj, patch, opts...)
				},
				SubResourcePatch: func(ctx context.Context, c client.Client, resource string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					concurrentChange(c, obj)
					return c.SubResource(resource).Patch(ctx, obj, patch, opts...)
				},
			})
			pool.Status.V4AvailableIPs.SetInt64(2)
			pool.Status.Ready("Prepared", "")
			var err error
			switch operation {
			case "membership":
				err = w.PatchIPs(pool, []string{"10.0.0.2"})
			case "statistics":
				err = w.PatchStatistics(pool)
			case "conditions":
				err = w.PatchStatus(pool)
			case "finalizer":
				err = w.AddFinalizer(pool)
			}
			require.True(t, k8serrors.IsConflict(err), "%v", err)
			require.Equal(t, 1, patches, "the writer must not replay old calculations")
			got, readErr := w.get(pool.Name)
			require.NoError(t, readErr)
			require.Equal(t, []string{"example.com/concurrent"}, got.Finalizers)
			require.Empty(t, got.Status.IPs)
			require.Empty(t, got.Status.Conditions)
		})
	}
}

func TestIPPoolWriterRejectsStaleOrDeletingSnapshots(t *testing.T) {
	for _, change := range []string{"UID", "generation", "spec", "deletion", "missing"} {
		t.Run(change, func(t *testing.T) {
			w, api, pool := newIPPoolWriterTest(t, interceptor.Funcs{})
			live := pool.DeepCopy()
			live.Finalizers = []string{"example.com/hold"}
			switch change {
			case "UID":
				live.UID = "replacement"
			case "generation":
				live.Generation++
			case "spec":
				live.Spec.IPCount++
			case "deletion":
				live.DeletionTimestamp = new(metav1.Now())
			}
			if change == "missing" {
				require.NoError(t, api.Delete(t.Context(), live))
			} else {
				if change == "deletion" {
					live.DeletionTimestamp = nil
					require.NoError(t, api.Update(t.Context(), live))
					require.NoError(t, api.Delete(t.Context(), live))
				} else {
					require.NoError(t, api.Update(t.Context(), live))
				}
			}
			err := w.AddFinalizer(pool)
			require.Error(t, err, "a failed finalizer guard must abort allocation")
			require.Error(t, w.PatchIPs(pool, []string{"10.0.0.2"}))
			require.Empty(t, pool.Status.IPs)
		})
	}
}

func TestIPPoolWriterMembershipUncertainResponse(t *testing.T) {
	for _, scenario := range []string{"committed", "uncommitted", "readback unavailable", "replacement", "changed spec", "terminating", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			writeErr := errors.New("response lost")
			patches := 0
			w, api, pool := newIPPoolWriterTest(t, interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, c client.Client, resource string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					patches++
					if patches > 1 {
						return c.SubResource(resource).Patch(ctx, obj, patch, opts...)
					}
					if scenario != "uncommitted" {
						require.NoError(t, c.SubResource(resource).Patch(ctx, obj, patch, opts...))
					}
					live := &kubeovnv1.IPPool{}
					require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), live))
					switch scenario {
					case "replacement":
						live.UID = "replacement"
					case "changed spec":
						live.Spec.IPCount++
						live.Generation++
					case "terminating":
						live.Finalizers = []string{"example.com/hold"}
					case "cancelled":
						cancel()
					}
					if scenario == "replacement" || scenario == "changed spec" || scenario == "terminating" {
						require.NoError(t, c.Update(t.Context(), live))
					}
					if scenario == "terminating" {
						require.NoError(t, c.Delete(t.Context(), live))
					}
					return writeErr
				},
			})
			w.ctx = ctx
			if scenario == "readback unavailable" {
				gets := 0
				w.reader = interceptor.NewClient(api, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					gets++
					if gets > 1 {
						return errors.New("API unavailable")
					}
					return c.Get(ctx, key, obj, opts...)
				}})
			}
			err := w.PatchIPs(pool, []string{"10.0.0.2"})
			if scenario == "committed" {
				require.NoError(t, err)
				require.Equal(t, []string{"10.0.0.2"}, pool.Status.IPs)
				pool.Status.Ready("Prepared", "")
				require.NoError(t, w.PatchStatus(pool), "readback must advance RV for the next write")
			} else {
				require.ErrorIs(t, err, writeErr)
				require.Empty(t, pool.Status.IPs)
			}
		})
	}
}

func TestIPPoolWriterActivationConflictRetainsReservationAndReselects(t *testing.T) {
	for _, change := range []string{"shrink", "deletion"} {
		t.Run(change, func(t *testing.T) {
			w, api, pool := newIPPoolWriterTest(t, interceptor.Funcs{})
			require.NoError(t, w.AddFinalizer(pool))
			allocation := ipam.NewIPAM()
			require.NoError(t, allocation.AddOrUpdateSubnet(pool.Spec.Subnet, "10.0.0.0/29", "", nil))
			v4, _, err := allocation.SelectIPPoolIPs(pool.Spec.Subnet, pool.Name, nil, 2, 0)
			require.NoError(t, err)
			err = allocation.ActivateIPPoolIPs(pool.Spec.Subnet, pool.Name, v4, 2, 0, func() error {
				// Simulate a spec/delete event arriving during OVN preparation.
				live, readErr := w.get(pool.Name)
				require.NoError(t, readErr)
				if change == "shrink" {
					live.Spec.IPCount = 1
					live.Generation++
					require.NoError(t, api.Update(t.Context(), live))
				} else {
					require.NoError(t, api.Delete(t.Context(), live))
				}
				return w.PatchIPs(pool, v4)
			})
			require.True(t, k8serrors.IsConflict(err), "%v", err)
			_, _, _, err = allocation.GetRandomAddress("pod", "nic", nil, pool.Spec.Subnet, pool.Name, nil, true)
			require.ErrorIs(t, err, ipam.ErrNoAvailable)
			fresh, err := w.get(pool.Name)
			require.NoError(t, err)
			require.Empty(t, fresh.Status.IPs)
			if change == "shrink" {
				v4, _, err = allocation.SelectIPPoolIPs(fresh.Spec.Subnet, fresh.Name, nil, 1, 0)
				require.NoError(t, err)
				require.NoError(t, allocation.ActivateIPPoolIPs(fresh.Spec.Subnet, fresh.Name, v4, 1, 0, func() error { return w.PatchIPs(fresh, v4) }))
				v4IP, _, _, allocErr := allocation.GetRandomAddress("pod", "nic", nil, fresh.Spec.Subnet, fresh.Name, nil, true)
				require.NoError(t, allocErr)
				require.Contains(t, fresh.Status.IPs, v4IP)
			}
		})
	}
}

func TestIPPoolWriterFinalizerRemovalProtectsReplacement(t *testing.T) {
	w, api, pool := newIPPoolWriterTest(t, interceptor.Funcs{})
	require.NoError(t, w.AddFinalizer(pool))
	replacement := pool.DeepCopy()
	replacement.UID = "replacement"
	replacement.Finalizers = append(replacement.Finalizers, "example.com/foreign")
	require.NoError(t, api.Update(t.Context(), replacement))
	require.True(t, k8serrors.IsConflict(w.RemoveFinalizer(pool)))
	got, err := w.get(pool.Name)
	require.NoError(t, err)
	require.Equal(t, replacement.Finalizers, got.Finalizers)
	require.NoError(t, api.Delete(t.Context(), got))
	terminating, err := w.get(pool.Name)
	require.NoError(t, err)
	require.NoError(t, w.RemoveFinalizer(terminating))
	require.Equal(t, []string{"example.com/foreign"}, terminating.Finalizers)
}
