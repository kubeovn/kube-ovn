package controllerruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/internal"
)

func TestSubnetStatusObserverComparesOnlyIPFields(t *testing.T) {
	subnet := testSubnet("")
	subnet.Status.V6AvailableIPs.SetString("18446744073709551614", 10)
	api := newRuntimeClient(t, subnet)
	var before kubeovnv1.Subnet
	require.NoError(t, api.Get(t.Context(), clientKey(subnet.Name), &before))
	projected := subnet.Status.DeepCopy()
	projected.Conditions = []kubeovnv1.Condition{{Type: "Foreign"}}
	projected.GatewayMAC = "00:00:5e:00:53:01"
	projected.U2OInterconnectionIP = "192.0.2.2"
	projected.V4UsingIPs = internal.NewBigInt(0)
	reconciler := &SubnetStatusReconciler{Reader: api, Projector: observationProjector(projected)}

	outcome, err := reconciler.observeStatus(t.Context(), SubnetStatusRequest{Name: subnet.Name})
	require.NoError(t, err)
	require.Equal(t, "match", outcome)
	projected.V6AvailableIPs = projected.V6AvailableIPs.AddInt(1)
	outcome, err = reconciler.observeStatus(t.Context(), SubnetStatusRequest{Name: subnet.Name})
	require.NoError(t, err)
	require.Equal(t, "drift", outcome, "IPv6 counters differing by one must not round to a match")
	var after kubeovnv1.Subnet
	require.NoError(t, api.Get(t.Context(), clientKey(subnet.Name), &after))
	require.Equal(t, before, after, "neither matching nor drifting observations may write")
}

func TestSubnetStatusObserverDetectsEachIPField(t *testing.T) {
	for _, field := range []string{"v4available", "v4using", "v6available", "v6using", "v4availableRange", "v4usingRange", "v6availableRange", "v6usingRange"} {
		t.Run(field, func(t *testing.T) {
			subnet := testSubnet("")
			status := subnet.Status.DeepCopy()
			switch field {
			case "v4available":
				status.V4AvailableIPs = internal.NewBigInt(1)
			case "v4using":
				status.V4UsingIPs = internal.NewBigInt(1)
			case "v6available":
				status.V6AvailableIPs = internal.NewBigInt(1)
			case "v6using":
				status.V6UsingIPs = internal.NewBigInt(1)
			case "v4availableRange":
				status.V4AvailableIPRange = "192.0.2.1-192.0.2.2"
			case "v4usingRange":
				status.V4UsingIPRange = "192.0.2.1"
			case "v6availableRange":
				status.V6AvailableIPRange = "2001:db8::1-2001:db8::2"
			case "v6usingRange":
				status.V6UsingIPRange = "2001:db8::1"
			}
			reconciler := &SubnetStatusReconciler{Reader: newRuntimeClient(t, subnet), Projector: observationProjector(status)}
			outcome, err := reconciler.observeStatus(t.Context(), SubnetStatusRequest{Name: subnet.Name})
			require.NoError(t, err)
			require.Equal(t, "drift", outcome)
		})
	}
}

func TestSubnetStatusObserverRejectsChangedSnapshot(t *testing.T) {
	changes := []struct {
		name   string
		change func(*kubeovnv1.Subnet)
	}{
		{name: "uid", change: func(s *kubeovnv1.Subnet) { s.UID = "replacement" }},
		{name: "generation", change: func(s *kubeovnv1.Subnet) { s.Generation++ }},
		{name: "resource version", change: func(s *kubeovnv1.Subnet) { s.ResourceVersion = "changed" }},
		{name: "spec", change: func(s *kubeovnv1.Subnet) { s.Spec.ExcludeIps = []string{"192.0.2.1"} }},
	}
	for _, change := range changes {
		t.Run(change.name, func(t *testing.T) {
			subnet := testSubnet("")
			reader := &subnetStatusObservationReader{Reader: newRuntimeClient(t, subnet), change: change.change}
			reconciler := &SubnetStatusReconciler{Reader: reader, Projector: observationProjector(&subnet.Status)}
			before := testutil.ToFloat64(subnetStatusObservations.WithLabelValues("drift"))
			_, err := reconciler.Reconcile(t.Context(), SubnetStatusRequest{Name: subnet.Name})
			require.True(t, apierrors.IsConflict(err))
			require.Equal(t, before, testutil.ToFloat64(subnetStatusObservations.WithLabelValues("drift")))
		})
	}
}

func TestSubnetStatusObserverRejectsProjectorSnapshot(t *testing.T) {
	for _, field := range []string{"name", "uid", "spec", "resource version", "deleting"} {
		t.Run(field, func(t *testing.T) {
			subnet := testSubnet("")
			subnet.Spec.ExcludeIps = []string{"192.0.2.1"}
			reconciler := &SubnetStatusReconciler{Reader: newRuntimeClient(t, subnet)}
			reconciler.Projector = SubnetStatusProjectorFunc(func(_ context.Context, snapshot SubnetWriteSnapshot, _ SubnetShadowObservation) (SubnetStatusProjection, error) {
				switch field {
				case "name":
					snapshot.Name = "different"
				case "uid":
					snapshot.UID = "different"
				case "spec":
					snapshot.Spec.ExcludeIps[0] = "192.0.2.2"
				case "resource version":
					snapshot.ResourceVersion = "different"
				case "deleting":
					snapshot.Deleting = true
				}
				return NewSubnetStatusProjection(snapshot, &subnet.Status)
			})
			_, err := reconciler.Reconcile(t.Context(), SubnetStatusRequest{Name: subnet.Name})
			require.True(t, apierrors.IsConflict(err))
			require.Equal(t, []string{"192.0.2.1"}, subnet.Spec.ExcludeIps)
		})
	}
}

func TestSubnetStatusObserverSkipsDeletion(t *testing.T) {
	for _, when := range []string{"before calculation", "during calculation", "already deleted"} {
		t.Run(when, func(t *testing.T) {
			subnet := testSubnet("")
			reader := &subnetStatusObservationReader{Reader: newRuntimeClient(t, subnet)}
			want := "terminating"
			if when == "already deleted" {
				reader.readbackErr = apierrors.NewNotFound(schema.GroupResource{Resource: "subnets"}, subnet.Name)
				want = "not_found"
			} else {
				reader.change = func(s *kubeovnv1.Subnet) { s.DeletionTimestamp = new(metav1.Now()) }
			}
			reader.changeFirst = when == "before calculation"
			calls := 0
			reconciler := &SubnetStatusReconciler{Reader: reader, Projector: SubnetStatusProjectorFunc(func(_ context.Context, snapshot SubnetWriteSnapshot, _ SubnetShadowObservation) (SubnetStatusProjection, error) {
				calls++
				return NewSubnetStatusProjection(snapshot, &subnet.Status)
			})}
			outcome, err := reconciler.observeStatus(t.Context(), SubnetStatusRequest{Name: subnet.Name})
			require.NoError(t, err)
			require.Equal(t, want, outcome)
			if reader.changeFirst {
				require.Zero(t, calls, "terminating Subnets must not reach the calculator")
			}
		})
	}
}

func TestSubnetStatusObserverRetryRecalculatesAfterConflict(t *testing.T) {
	subnet := testSubnet("")
	api := newRuntimeClient(t, subnet)
	calls := 0
	reconciler := &SubnetStatusReconciler{Reader: api, Projector: SubnetStatusProjectorFunc(func(ctx context.Context, snapshot SubnetWriteSnapshot, _ SubnetShadowObservation) (SubnetStatusProjection, error) {
		calls++
		if calls == 1 {
			var current kubeovnv1.Subnet
			require.NoError(t, api.Get(ctx, clientKey(subnet.Name), &current))
			current.Spec.ExcludeIps = []string{"192.0.2.1"}
			require.NoError(t, api.Update(ctx, &current))
		} else {
			require.Equal(t, []string{"192.0.2.1"}, snapshot.Spec.ExcludeIps)
		}
		return NewSubnetStatusProjection(snapshot, &subnet.Status)
	})}
	_, err := reconciler.Reconcile(t.Context(), SubnetStatusRequest{Name: subnet.Name})
	require.True(t, apierrors.IsConflict(err))
	_, err = reconciler.Reconcile(t.Context(), SubnetStatusRequest{Name: subnet.Name})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestSubnetStatusObserverRecordsBoundedOutcomes(t *testing.T) {
	for _, outcome := range []string{"match", "drift", "conflict", "not_found", "terminating", "error"} {
		t.Run(outcome, func(t *testing.T) {
			subnet := testSubnet("")
			status := subnet.Status.DeepCopy()
			reader := &subnetStatusObservationReader{Reader: newRuntimeClient(t, subnet)}
			switch outcome {
			case "drift":
				status.V4UsingIPs = internal.NewBigInt(1)
			case "conflict":
				reader.change = func(s *kubeovnv1.Subnet) { s.ResourceVersion = "changed" }
			case "not_found":
				reader.Reader = newRuntimeClient(t)
			case "terminating":
				reader.change = func(s *kubeovnv1.Subnet) { s.DeletionTimestamp = new(metav1.Now()) }
			case "error":
				reader.readbackErr = errors.New("API unavailable")
			}
			reconciler := &SubnetStatusReconciler{Reader: reader, Projector: observationProjector(status)}
			before := testutil.ToFloat64(subnetStatusObservations.WithLabelValues(outcome))
			_, err := reconciler.Reconcile(t.Context(), SubnetStatusRequest{Name: subnet.Name})
			if outcome == "error" || outcome == "conflict" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, before+1, testutil.ToFloat64(subnetStatusObservations.WithLabelValues(outcome)))
		})
	}
}

func TestSubnetStatusObserverPropagatesReadAndCancellationErrors(t *testing.T) {
	for _, when := range []string{"initial read", "readback", "canceled calculation"} {
		t.Run(when, func(t *testing.T) {
			subnet := testSubnet("")
			want := errors.New("API read unavailable")
			reader := &subnetStatusObservationReader{Reader: newRuntimeClient(t, subnet)}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch when {
			case "initial read":
				reader.initialErr = want
			case "readback":
				reader.readbackErr = want
			case "canceled calculation":
				want = context.Canceled
			}
			reconciler := &SubnetStatusReconciler{Reader: reader, Projector: SubnetStatusProjectorFunc(func(_ context.Context, snapshot SubnetWriteSnapshot, _ SubnetShadowObservation) (SubnetStatusProjection, error) {
				if when == "canceled calculation" {
					cancel()
				}
				return NewSubnetStatusProjection(snapshot, &subnet.Status)
			})}
			before := testutil.ToFloat64(subnetStatusObservations.WithLabelValues("drift"))
			_, err := reconciler.Reconcile(ctx, SubnetStatusRequest{Name: subnet.Name})
			require.ErrorIs(t, err, want)
			require.Equal(t, before, testutil.ToFloat64(subnetStatusObservations.WithLabelValues("drift")))
		})
	}
}

type subnetStatusObservationReader struct {
	client.Reader
	change      func(*kubeovnv1.Subnet)
	changeFirst bool
	initialErr  error
	readbackErr error
	reads       int
}

func (r *subnetStatusObservationReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	r.reads++
	if r.reads == 1 && r.initialErr != nil {
		return r.initialErr
	}
	if r.reads == 2 && r.readbackErr != nil {
		return r.readbackErr
	}
	if err := r.Reader.Get(ctx, key, obj, opts...); err != nil {
		return err
	}
	if r.change != nil && (r.reads == 2 || r.changeFirst) {
		r.change(obj.(*kubeovnv1.Subnet))
	}
	return nil
}

func observationProjector(status *kubeovnv1.SubnetStatus) SubnetStatusProjector {
	return SubnetStatusProjectorFunc(func(_ context.Context, snapshot SubnetWriteSnapshot, _ SubnetShadowObservation) (SubnetStatusProjection, error) {
		return NewSubnetStatusProjection(snapshot, status)
	})
}
