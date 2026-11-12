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

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func newSubnetSideEffectWriter(t *testing.T) (*subnetSideEffectWriter, *kubeovnv1.Subnet, SubnetWriteSnapshot) {
	t.Helper()
	subnet := testSubnet("")
	subnet.UID = "uid-a"
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	w := &subnetSideEffectWriter{
		ctx:  t.Context(),
		gate: SubnetWriterGate{Recovery: func(context.Context) error { return nil }},
	}
	return w, subnet, snapshot
}

func TestSubnetSideEffectWriterRunsEachOperationAfterGate(t *testing.T) {
	w, subnet, snapshot := newSubnetSideEffectWriter(t)
	operations := []struct {
		name string
		run  func(SubnetSideEffectAction) error
	}{
		{name: "OVN allocation", run: func(action SubnetSideEffectAction) error { return w.AllocateOVN(snapshot, subnet, action) }},
		{name: "IPAM allocation", run: func(action SubnetSideEffectAction) error { return w.AllocateIPAM(snapshot, subnet, action) }},
	}
	deletion := metav1.Now()
	terminating := subnet.DeepCopy()
	terminating.DeletionTimestamp = &deletion
	terminatingSnapshot, err := NewSubnetWriteSnapshot(terminating, false)
	require.NoError(t, err)
	operations = append(operations,
		struct {
			name string
			run  func(SubnetSideEffectAction) error
		}{name: "OVN cleanup", run: func(action SubnetSideEffectAction) error {
			return w.CleanupOVN(terminatingSnapshot, terminating, action)
		}},
		struct {
			name string
			run  func(SubnetSideEffectAction) error
		}{name: "IPAM release", run: func(action SubnetSideEffectAction) error {
			return w.ReleaseIPAM(terminatingSnapshot, terminating, action)
		}},
	)
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			calls := 0
			require.NoError(t, operation.run(func(context.Context) error {
				calls++
				return nil
			}))
			require.Equal(t, 1, calls)
		})
	}
}

func TestSubnetSideEffectWriterRejectsInvalidOperationsBeforeCallback(t *testing.T) {
	w, subnet, snapshot := newSubnetSideEffectWriter(t)
	tests := []struct {
		name string
		run  func(SubnetSideEffectAction) error
	}{
		{name: "OVN allocation denied", run: func(action SubnetSideEffectAction) error {
			snapshot.AllocationAllowed = false
			return w.AllocateOVN(snapshot, subnet, action)
		}},
		{name: "IPAM allocation denied", run: func(action SubnetSideEffectAction) error {
			snapshot.AllocationAllowed = false
			return w.AllocateIPAM(snapshot, subnet, action)
		}},
		{name: "cleanup active", run: func(action SubnetSideEffectAction) error { return w.CleanupOVN(snapshot, subnet, action) }},
		{name: "release active", run: func(action SubnetSideEffectAction) error { return w.ReleaseIPAM(snapshot, subnet, action) }},
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

	changed := subnet.DeepCopy()
	changed.ResourceVersion = "8"
	calls := 0
	err := w.AllocateOVN(snapshot, changed, func(context.Context) error { calls++; return nil })
	require.True(t, apierrors.IsConflict(err), err)
	require.Zero(t, calls)
}

func TestSubnetSideEffectWriterRejectsRecoveryFailureAndCancellation(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = "uid-a"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	calls := 0
	w := &subnetSideEffectWriter{ctx: t.Context()}
	require.EqualError(t, w.AllocateIPAM(snapshot, subnet, func(context.Context) error { calls++; return nil }), "subnet writer recovery gate is not configured")
	require.Zero(t, calls)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	w.ctx = ctx
	require.ErrorIs(t, w.AllocateIPAM(snapshot, subnet, func(context.Context) error { calls++; return nil }), context.Canceled)
	require.Zero(t, calls)
}

func TestSubnetSideEffectWriterReturnsActionErrorAndRunsOnce(t *testing.T) {
	w, subnet, snapshot := newSubnetSideEffectWriter(t)
	want := errors.New("OVN transaction failed")
	calls := 0
	err := w.AllocateOVN(snapshot, subnet, func(context.Context) error {
		calls++
		return want
	})
	require.ErrorIs(t, err, want)
	require.Equal(t, 1, calls)

	require.EqualError(t, w.AllocateOVN(snapshot, subnet, nil), "subnet side effect action is not configured")
}

func TestSubnetSideEffectWriterReturnsCancellationAfterAction(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	w, subnet, snapshot := newSubnetSideEffectWriter(t)
	w.ctx = ctx
	calls := 0
	err := w.AllocateIPAM(snapshot, subnet, func(context.Context) error {
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
			w, subnet, snapshot := newSubnetSideEffectWriter(t)
			applyErr := errors.New("apply failed after confirmed mutation")
			compensateErr := errors.New("compensation failed")
			applyCalls, compensateCalls := 0, 0
			err := w.Execute(snapshot, subnet, SubnetOVNAllocateOperation, SubnetSideEffectPlan{
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
			w, subnet, snapshot := newSubnetSideEffectWriter(t)
			w.ctx = ctx
			compensateCalls := 0
			err := w.Execute(snapshot, subnet, SubnetIPAMAllocateOperation, SubnetSideEffectPlan{
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
	w, subnet, snapshot := newSubnetSideEffectWriter(t)
	snapshot.AllocationAllowed = false
	calls := 0
	err := w.Execute(snapshot, subnet, SubnetIPAMAllocateOperation, SubnetSideEffectPlan{
		Apply: func(context.Context) (SubnetSideEffectReceipt, error) { calls++; return nil, nil },
	})
	require.Error(t, err)
	require.Zero(t, calls)
}

func TestSubnetSideEffectWriterRejectsUnrelatedOperations(t *testing.T) {
	for _, operation := range []SubnetWriteOperation{SubnetStatusWriteOperation, SubnetFinalizerAddOperation, SubnetFinalizerRemoveOperation, "unknown"} {
		t.Run(string(operation), func(t *testing.T) {
			w, subnet, snapshot := newSubnetSideEffectWriter(t)
			snapshot.AllocationAllowed = false
			calls := 0
			err := w.Execute(snapshot, subnet, operation, SubnetSideEffectPlan{
				Apply: func(context.Context) (SubnetSideEffectReceipt, error) { calls++; return nil, nil },
			})
			require.ErrorContains(t, err, "unsupported Subnet side effect operation")
			require.Zero(t, calls)
		})
	}
}

func TestSubnetSideEffectWriterReportsCompensationTimeout(t *testing.T) {
	w, subnet, snapshot := newSubnetSideEffectWriter(t)
	applyErr := errors.New("apply failed")
	err := w.Execute(snapshot, subnet, SubnetOVNAllocateOperation, SubnetSideEffectPlan{
		Apply: func(context.Context) (SubnetSideEffectReceipt, error) {
			return sideEffectReceiptFunc(func(ctx context.Context) error { <-ctx.Done(); return nil }), applyErr
		},
	})
	require.ErrorIs(t, err, applyErr)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestSubnetSideEffectWriterSuccessfulApplySkipsCompensation(t *testing.T) {
	w, subnet, snapshot := newSubnetSideEffectWriter(t)
	applyCalls, compensateCalls := 0, 0
	require.NoError(t, w.Execute(snapshot, subnet, SubnetOVNAllocateOperation, SubnetSideEffectPlan{
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
			w, subnet, snapshot := newSubnetSideEffectWriter(t)
			backend := "existing resource"
			want := backend
			applyErr := errors.New("apply outcome requires reconciliation")
			err := w.Execute(snapshot, subnet, SubnetOVNAllocateOperation, SubnetSideEffectPlan{
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
				w, subnet, snapshot := newSubnetSideEffectWriter(t)
				removing := operation == SubnetOVNCleanupOperation || operation == SubnetIPAMReleaseOperation
				if removing {
					subnet.DeletionTimestamp = new(metav1.Now())
					var err error
					snapshot, err = NewSubnetWriteSnapshot(subnet, false)
					require.NoError(t, err)
				}
				before := state{uid: string(subnet.UID), epoch: 1, revision: 1, exists: removing}
				backend := before
				var receipt SubnetSideEffectReceipt
				applyErr := errors.New("failure after a confirmed mutation")
				err := w.Execute(snapshot, subnet, operation, SubnetSideEffectPlan{
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
