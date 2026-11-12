package controllerruntime

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func newSubnetSideEffectWriterAPI(t *testing.T, terminating bool) (*subnetSideEffectWriter, client.WithWatch, *kubeovnv1.Subnet, SubnetWriteSnapshot) {
	t.Helper()
	scheme, err := NewScheme()
	require.NoError(t, err)
	subnet := testSubnet("")
	subnet.UID = "uid-a"
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	if terminating {
		subnet.Finalizers = []string{"example.com/hold"}
		subnet.DeletionTimestamp = new(metav1.Now())
	}
	api := fake.NewClientBuilder().WithScheme(scheme).WithObjects(subnet).Build()
	live := &kubeovnv1.Subnet{}
	require.NoError(t, api.Get(t.Context(), clientKey(subnet.Name), live))
	snapshot, err := NewSubnetWriteSnapshot(live, !terminating)
	require.NoError(t, err)
	w := &subnetSideEffectWriter{
		ctx:    t.Context(),
		reader: api,
		gate:   SubnetWriterGate{Recovery: func(context.Context) error { return nil }},
	}
	return w, api, live, snapshot
}

func newSubnetSideEffectWriter(t *testing.T) (*subnetSideEffectWriter, *kubeovnv1.Subnet, SubnetWriteSnapshot) {
	t.Helper()
	w, _, subnet, snapshot := newSubnetSideEffectWriterAPI(t, false)
	return w, subnet, snapshot
}

func TestSubnetSideEffectWriterRunsEachOperationAfterGate(t *testing.T) {
	for _, operation := range []SubnetWriteOperation{SubnetOVNAllocateOperation, SubnetIPAMAllocateOperation, SubnetOVNCleanupOperation, SubnetIPAMReleaseOperation} {
		t.Run(string(operation), func(t *testing.T) {
			terminating := operation == SubnetOVNCleanupOperation || operation == SubnetIPAMReleaseOperation
			w, _, _, snapshot := newSubnetSideEffectWriterAPI(t, terminating)
			calls := 0
			action := func(context.Context) error { calls++; return nil }
			var err error
			switch operation {
			case SubnetOVNAllocateOperation:
				err = w.AllocateOVN(snapshot, action)
			case SubnetIPAMAllocateOperation:
				err = w.AllocateIPAM(snapshot, action)
			case SubnetOVNCleanupOperation:
				err = w.CleanupOVN(snapshot, action)
			case SubnetIPAMReleaseOperation:
				err = w.ReleaseIPAM(snapshot, action)
			}
			require.NoError(t, err)
			require.Equal(t, 1, calls)
		})
	}
}

func TestSubnetSideEffectWriterRejectsInvalidOperationsBeforeCallback(t *testing.T) {
	w, _, snapshot := newSubnetSideEffectWriter(t)
	tests := []struct {
		name string
		run  func(SubnetSideEffectAction) error
	}{
		{name: "OVN allocation denied", run: func(action SubnetSideEffectAction) error {
			snapshot.AllocationAllowed = false
			return w.AllocateOVN(snapshot, action)
		}},
		{name: "IPAM allocation denied", run: func(action SubnetSideEffectAction) error {
			snapshot.AllocationAllowed = false
			return w.AllocateIPAM(snapshot, action)
		}},
		{name: "cleanup active", run: func(action SubnetSideEffectAction) error { return w.CleanupOVN(snapshot, action) }},
		{name: "release active", run: func(action SubnetSideEffectAction) error { return w.ReleaseIPAM(snapshot, action) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			err := test.run(func(context.Context) error {
				calls++
				return nil
			})
			require.Error(t, err)
			require.Zero(t, calls)
		})
	}
}

func TestSubnetSideEffectWriterRejectsRecoveryFailureAndCancellation(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = "uid-a"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	calls := 0
	w := &subnetSideEffectWriter{ctx: t.Context()}
	require.EqualError(t, w.AllocateIPAM(snapshot, func(context.Context) error { calls++; return nil }), "subnet writer recovery gate is not configured")
	require.Zero(t, calls)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	w.ctx = ctx
	require.ErrorIs(t, w.AllocateIPAM(snapshot, func(context.Context) error { calls++; return nil }), context.Canceled)
	require.Zero(t, calls)
}

func TestSubnetSideEffectWriterReturnsActionErrorAndRunsOnce(t *testing.T) {
	w, _, snapshot := newSubnetSideEffectWriter(t)
	want := errors.New("OVN transaction failed")
	calls := 0
	err := w.AllocateOVN(snapshot, func(context.Context) error {
		calls++
		return want
	})
	require.ErrorIs(t, err, want)
	require.Equal(t, 1, calls)

	require.EqualError(t, w.AllocateOVN(snapshot, nil), "subnet side effect action is not configured")
}

func TestSubnetSideEffectWriterReturnsCancellationAfterAction(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	w, _, snapshot := newSubnetSideEffectWriter(t)
	w.ctx = ctx
	calls := 0
	err := w.AllocateIPAM(snapshot, func(context.Context) error {
		calls++
		cancel()
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}

// sideEffectReceiptFunc models the adapter receipt in tests.
type sideEffectReceiptFunc func(context.Context) error

func (f sideEffectReceiptFunc) Compensate(ctx context.Context) error { return f(ctx) }

func TestSubnetSideEffectWriterCompensatesConfirmedMutation(t *testing.T) {
	for _, compensationFails := range []bool{false, true} {
		t.Run(strconv.FormatBool(compensationFails), func(t *testing.T) {
			w, _, snapshot := newSubnetSideEffectWriter(t)
			applyErr := errors.New("apply failed after confirmed mutation")
			compensateErr := errors.New("compensation failed")
			applyCalls, compensateCalls := 0, 0
			err := w.Execute(snapshot, SubnetOVNAllocateOperation, SubnetSideEffectPlan{
				Apply: func(context.Context) (SubnetSideEffectReceipt, error) {
					applyCalls++
					return sideEffectReceiptFunc(func(ctx context.Context) error {
						compensateCalls++
						deadline, ok := ctx.Deadline()
						require.True(t, ok)
						require.InDelta(t, subnetSideEffectCompensationTimeout.Seconds(), time.Until(deadline).Seconds(), 1)
						if compensationFails {
							return compensateErr
						}
						return nil
					}), applyErr
				},
			})
			require.ErrorIs(t, err, applyErr)
			if compensationFails {
				require.ErrorIs(t, err, compensateErr)
			}
			require.Equal(t, 1, applyCalls)
			require.Equal(t, 1, compensateCalls)
		})
	}
}

func TestSubnetSideEffectWriterCancellationNeverAuthorizesCompensation(t *testing.T) {
	for _, applyErr := range []error{nil, errors.New("apply failed")} {
		t.Run(fmt.Sprint(applyErr), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			w, _, snapshot := newSubnetSideEffectWriter(t)
			w.ctx = ctx
			compensateCalls := 0
			err := w.Execute(snapshot, SubnetIPAMAllocateOperation, SubnetSideEffectPlan{
				Apply: func(context.Context) (SubnetSideEffectReceipt, error) {
					cancel()
					return sideEffectReceiptFunc(func(context.Context) error { compensateCalls++; return nil }), applyErr
				},
			})
			require.ErrorIs(t, err, context.Canceled)
			if applyErr != nil {
				require.ErrorIs(t, err, applyErr)
			}
			require.Zero(t, compensateCalls)
		})
	}
}

func TestSubnetSideEffectWriterGateFailureSkipsPlan(t *testing.T) {
	w, _, snapshot := newSubnetSideEffectWriter(t)
	snapshot.AllocationAllowed = false
	calls := 0
	err := w.Execute(snapshot, SubnetIPAMAllocateOperation, SubnetSideEffectPlan{
		Apply: func(context.Context) (SubnetSideEffectReceipt, error) { calls++; return nil, nil },
	})
	require.Error(t, err)
	require.Zero(t, calls)
}

func TestSubnetSideEffectWriterRejectsUnrelatedOperations(t *testing.T) {
	for _, operation := range []SubnetWriteOperation{SubnetStatusWriteOperation, SubnetFinalizerAddOperation, SubnetFinalizerRemoveOperation, "unknown"} {
		t.Run(string(operation), func(t *testing.T) {
			w, _, snapshot := newSubnetSideEffectWriter(t)
			snapshot.AllocationAllowed = false
			calls := 0
			err := w.Execute(snapshot, operation, SubnetSideEffectPlan{
				Apply: func(context.Context) (SubnetSideEffectReceipt, error) { calls++; return nil, nil },
			})
			require.ErrorContains(t, err, "unsupported Subnet side effect operation")
			require.Zero(t, calls)
		})
	}
}

func TestSubnetSideEffectWriterReportsCompensationTimeout(t *testing.T) {
	w, _, snapshot := newSubnetSideEffectWriter(t)
	applyErr := errors.New("apply failed")
	err := w.Execute(snapshot, SubnetOVNAllocateOperation, SubnetSideEffectPlan{
		Apply: func(context.Context) (SubnetSideEffectReceipt, error) {
			return sideEffectReceiptFunc(func(ctx context.Context) error { <-ctx.Done(); return nil }), applyErr
		},
	})
	require.ErrorIs(t, err, applyErr)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestSubnetSideEffectWriterSuccessfulApplySkipsCompensation(t *testing.T) {
	w, _, snapshot := newSubnetSideEffectWriter(t)
	applyCalls, compensateCalls := 0, 0
	require.NoError(t, w.Execute(snapshot, SubnetOVNAllocateOperation, SubnetSideEffectPlan{
		Apply: func(context.Context) (SubnetSideEffectReceipt, error) {
			applyCalls++
			return sideEffectReceiptFunc(func(context.Context) error { compensateCalls++; return nil }), nil
		},
	}))
	require.Equal(t, 1, applyCalls)
	require.Zero(t, compensateCalls)
}

func TestSubnetSideEffectWriterPreservesUnconfirmedOrExistingState(t *testing.T) {
	for _, outcome := range []string{"uncommitted", "existing", "uncertain commit"} {
		t.Run(outcome, func(t *testing.T) {
			w, _, snapshot := newSubnetSideEffectWriter(t)
			backend := "existing resource"
			want := backend
			applyErr := errors.New("apply outcome requires reconciliation")
			err := w.Execute(snapshot, SubnetOVNAllocateOperation, SubnetSideEffectPlan{
				Apply: func(context.Context) (SubnetSideEffectReceipt, error) {
					if outcome == "uncertain commit" {
						backend = "possibly committed resource"
						want = backend
					}
					if outcome == "existing" {
						return nil, nil
					}
					return nil, applyErr
				},
			})
			if outcome == "existing" {
				require.NoError(t, err)
			} else {
				require.Same(t, applyErr, err)
			}
			require.Equal(t, want, backend)
		})
	}
}

func TestSubnetSideEffectWriterReceiptConditionallyRestoresOnlyOwnedMutation(t *testing.T) {
	// A real adapter must implement this comparison atomically in its backend.
	// The single-threaded fake models that compare-and-swap operation.
	type state struct {
		uid      string
		epoch    int
		revision int
		exists   bool
	}
	for _, operation := range []SubnetWriteOperation{SubnetOVNAllocateOperation, SubnetIPAMAllocateOperation, SubnetOVNCleanupOperation, SubnetIPAMReleaseOperation} {
		for _, change := range []string{"unchanged", "new revision", "new leader", "replacement"} {
			t.Run(string(operation)+"/"+change, func(t *testing.T) {
				removing := operation == SubnetOVNCleanupOperation || operation == SubnetIPAMReleaseOperation
				w, _, subnet, snapshot := newSubnetSideEffectWriterAPI(t, removing)
				before := state{uid: string(subnet.UID), epoch: 1, revision: 1, exists: removing}
				backend := before
				var receipt SubnetSideEffectReceipt
				applyErr := errors.New("failure after a confirmed mutation")
				err := w.Execute(snapshot, operation, SubnetSideEffectPlan{
					Apply: func(context.Context) (SubnetSideEffectReceipt, error) {
						backend.revision++
						backend.exists = !removing
						owned := backend
						receipt = sideEffectReceiptFunc(func(ctx context.Context) error {
							if err := ctx.Err(); err != nil {
								return err
							}
							if backend == before {
								return nil
							}
							if backend != owned {
								return subnetWriteConflict(subnet.Name, "backend mutation ownership changed")
							}
							backend = before
							return nil
						})
						switch change {
						case "new revision":
							backend.revision++
						case "new leader":
							backend.epoch++
						case "replacement":
							backend.uid = "replacement"
						}
						return receipt, applyErr
					},
				})
				require.ErrorIs(t, err, applyErr)
				if change == "unchanged" {
					require.Equal(t, before, backend)
					require.NoError(t, receipt.Compensate(t.Context()), "receipt replay must settle")
				} else {
					require.True(t, apierrors.IsConflict(err), err)
					current := backend
					require.Error(t, receipt.Compensate(t.Context()))
					require.Equal(t, current, backend, "stale compensation must preserve later state")
				}
			})
		}
	}
}

func TestSubnetSideEffectWriterRejectsStaleAPIState(t *testing.T) {
	for _, operation := range []SubnetWriteOperation{SubnetOVNAllocateOperation, SubnetIPAMAllocateOperation, SubnetOVNCleanupOperation, SubnetIPAMReleaseOperation} {
		for _, change := range []string{"UID", "generation", "spec", "resource version"} {
			t.Run(string(operation)+"/"+change, func(t *testing.T) {
				terminating := operation == SubnetOVNCleanupOperation || operation == SubnetIPAMReleaseOperation
				w, api, original, snapshot := newSubnetSideEffectWriterAPI(t, terminating)
				live := original.DeepCopy()
				switch change {
				case "UID":
					live.UID = "replacement"
				case "generation":
					live.Generation++
				case "spec":
					live.Spec.Vlan = "updated"
				case "resource version":
					live.Labels = map[string]string{"changed": "true"}
				}
				require.NoError(t, api.Update(t.Context(), live))
				require.NoError(t, snapshot.Validate(original, operation), "the caller still holds a matching stale object")
				calls := 0
				err := w.Execute(snapshot, operation, SubnetSideEffectPlan{
					Apply: func(context.Context) (SubnetSideEffectReceipt, error) {
						calls++
						return sideEffectReceiptFunc(func(context.Context) error { calls++; return nil }), errors.New("apply failure")
					},
				})
				require.True(t, apierrors.IsConflict(err), err)
				require.Zero(t, calls, "neither apply nor compensation may run")
			})
		}
	}
}

func TestSubnetSideEffectWriterRejectsDeletionSinceSnapshot(t *testing.T) {
	for _, operation := range []SubnetWriteOperation{SubnetOVNAllocateOperation, SubnetIPAMAllocateOperation} {
		t.Run(string(operation), func(t *testing.T) {
			w, api, original, _ := newSubnetSideEffectWriterAPI(t, false)
			original.Finalizers = []string{"example.com/hold"}
			require.NoError(t, api.Update(t.Context(), original))
			snapshot, err := NewSubnetWriteSnapshot(original, true)
			require.NoError(t, err)
			require.NoError(t, api.Delete(t.Context(), original))
			require.NoError(t, snapshot.Validate(original, operation))
			calls := 0
			err = w.Execute(snapshot, operation, SubnetSideEffectPlan{
				Apply: func(context.Context) (SubnetSideEffectReceipt, error) { calls++; return nil, nil },
			})
			require.True(t, apierrors.IsConflict(err), err)
			require.Zero(t, calls)
		})
	}
}

func TestSubnetSideEffectWriterReadsAfterRecoveryAndBeforeEveryAction(t *testing.T) {
	for _, mutateDuringRecovery := range []bool{false, true} {
		t.Run(strconv.FormatBool(mutateDuringRecovery), func(t *testing.T) {
			w, api, original, snapshot := newSubnetSideEffectWriterAPI(t, false)
			recovered, reads, calls := false, 0, 0
			w.gate.Recovery = func(ctx context.Context) error {
				if mutateDuringRecovery {
					live := original.DeepCopy()
					live.Spec.Vlan = "changed during recovery"
					require.NoError(t, api.Update(ctx, live))
				}
				recovered = true
				return nil
			}
			w.reader = interceptor.NewClient(api, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					require.True(t, recovered, "recovery must finish before the live read")
					reads++
					return c.Get(ctx, key, obj, opts...)
				},
			})
			action := func(context.Context) error { calls++; return nil }
			err := w.AllocateOVN(snapshot, action)
			if mutateDuringRecovery {
				require.True(t, apierrors.IsConflict(err), err)
				require.Zero(t, calls)
				require.Equal(t, 1, reads)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			original.Labels = map[string]string{"changed": "after first action"}
			require.NoError(t, api.Update(t.Context(), original))
			err = w.AllocateOVN(snapshot, action)
			require.True(t, apierrors.IsConflict(err), err)
			require.Equal(t, 2, reads, "each invocation must read independently")
			require.Equal(t, 1, calls, "a stale second invocation must not replay the action")
		})
	}
}

func TestSubnetSideEffectWriterReadFailuresSkipPlan(t *testing.T) {
	for _, failure := range []string{"missing reader", "NotFound", "API error", "canceled during read", "timeout after read", "recovery error"} {
		t.Run(failure, func(t *testing.T) {
			w, api, subnet, snapshot := newSubnetSideEffectWriterAPI(t, false)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			w.ctx = ctx
			readErr := errors.New("API unavailable")
			recoveryErr := errors.New("recovery incomplete")
			reads, callbacks := 0, 0
			w.reader = interceptor.NewClient(api, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					reads++
					deadline, ok := ctx.Deadline()
					require.True(t, ok)
					require.InDelta(t, subnetSideEffectReadTimeout.Seconds(), time.Until(deadline).Seconds(), 1)
					switch failure {
					case "API error":
						return readErr
					case "canceled during read":
						cancel()
						return ctx.Err()
					case "timeout after read":
						require.NoError(t, c.Get(ctx, key, obj, opts...))
						<-ctx.Done()
						return nil
					default:
						return c.Get(ctx, key, obj, opts...)
					}
				},
			})
			switch failure {
			case "missing reader":
				w.reader = nil
			case "NotFound":
				require.NoError(t, api.Delete(t.Context(), subnet))
			case "recovery error":
				w.gate.Recovery = func(context.Context) error { return recoveryErr }
			}
			err := w.Execute(snapshot, SubnetIPAMAllocateOperation, SubnetSideEffectPlan{
				Apply: func(context.Context) (SubnetSideEffectReceipt, error) {
					callbacks++
					return sideEffectReceiptFunc(func(context.Context) error { callbacks++; return nil }), errors.New("apply failure")
				},
			})
			switch failure {
			case "missing reader":
				require.ErrorContains(t, err, "API reader is not configured")
			case "NotFound":
				require.True(t, apierrors.IsNotFound(err), err)
			case "API error":
				require.ErrorIs(t, err, readErr)
			case "canceled during read":
				require.ErrorIs(t, err, context.Canceled)
			case "timeout after read":
				require.ErrorIs(t, err, context.DeadlineExceeded)
			case "recovery error":
				require.ErrorIs(t, err, recoveryErr)
			}
			if failure == "missing reader" || failure == "recovery error" {
				require.Zero(t, reads)
			} else {
				require.Equal(t, 1, reads)
			}
			require.Zero(t, callbacks)
		})
	}
}
