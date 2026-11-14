package controllerruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/util/workqueue"
)

func TestNewSubnetStatusRequestNormalizesStatusEvents(t *testing.T) {
	tests := []struct {
		name  string
		event SubnetStatusEvent
		want  SubnetStatusRequest
	}{
		{name: "ip", event: SubnetStatusEvent{Name: "subnet-a", SourceKind: "IP"}, want: SubnetStatusRequest{Name: "subnet-a"}},
		{name: "explicit status", event: SubnetStatusEvent{Name: "subnet-a", SourceKind: "Subnet", StatusOnly: true}, want: SubnetStatusRequest{Name: "subnet-a"}},
		{name: "pod", event: SubnetStatusEvent{Name: "subnet-b", SourceKind: "Pod"}, want: SubnetStatusRequest{Name: "subnet-b"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NewSubnetStatusRequest(test.event)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestNewSubnetStatusRequestRejectsInvalidEvents(t *testing.T) {
	_, err := NewSubnetStatusRequest(SubnetStatusEvent{})
	require.ErrorIs(t, err, ErrSubnetStatusEventNameMissing)

	_, err = NewSubnetStatusRequest(SubnetStatusEvent{Name: "subnet-a", SourceKind: "Subnet"})
	require.ErrorIs(t, err, ErrSubnetStatusEventNotStatus)

	_, err = NewSubnetStatusRequest(SubnetStatusEvent{Name: "subnet-a", SourceKind: "Node"})
	require.ErrorIs(t, err, ErrSubnetStatusEventNotStatus)
}

func TestSubnetStatusEventSourceNormalizesAndDeduplicatesNames(t *testing.T) {
	queue := workqueue.NewTypedRateLimitingQueue[SubnetStatusRequest](workqueue.DefaultTypedControllerRateLimiter[SubnetStatusRequest]())
	t.Cleanup(queue.ShutDown)
	source := &recordingSubnetStatusEventSource{events: []SubnetStatusEvent{
		{Name: "subnet-b", SourceKind: "IP"},
		{Name: "subnet-a", SourceKind: "Pod"},
		{Name: "subnet-b", SourceKind: "VIP"},
		{Name: "subnet-a", SourceKind: "Subnet", StatusOnly: false},
	}}
	reconciler := &SubnetStatusReconciler{Events: source}
	require.NoError(t, reconciler.eventSource().Start(t.Context(), queue))

	request, shutdown := queue.Get()
	require.False(t, shutdown)
	require.Equal(t, SubnetStatusRequest{Name: "subnet-b"}, request)
	queue.Done(request)
	queue.Forget(request)

	request, shutdown = queue.Get()
	require.False(t, shutdown)
	require.Equal(t, SubnetStatusRequest{Name: "subnet-a"}, request)
	queue.Done(request)
	queue.Forget(request)
	require.Len(t, source.events, 4)
}

func TestSubnetStatusEventSourceClosesBridgeOnContextCancel(t *testing.T) {
	queue := workqueue.NewTypedRateLimitingQueue[SubnetStatusRequest](workqueue.DefaultTypedControllerRateLimiter[SubnetStatusRequest]())
	t.Cleanup(queue.ShutDown)
	ctx, cancel := context.WithCancel(t.Context())
	bridge := &recordingSubnetStatusEventBridge{closed: make(chan struct{})}
	reconciler := &SubnetStatusReconciler{Events: &recordingSubnetStatusEventSource{bridge: bridge}}
	require.NoError(t, reconciler.eventSource().Start(ctx, queue))
	cancel()
	select {
	case <-bridge.closed:
	case <-time.After(time.Second):
		t.Fatal("event bridge did not close after context cancellation")
	}
}

func TestSubnetStatusReconcilerProjectsCurrentSubnet(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = "subnet-status-uid"
	reader := newRuntimeClient(t, subnet)
	var gotSnapshot SubnetWriteSnapshot
	var gotObservation SubnetShadowObservation
	projector := SubnetStatusProjectorFunc(func(_ context.Context, snapshot SubnetWriteSnapshot, observation SubnetShadowObservation) (SubnetStatusProjection, error) {
		gotSnapshot = snapshot
		gotObservation = observation
		status := subnet.Status.DeepCopy()
		status.GatewayMAC = "00:00:5e:00:53:01"
		return NewSubnetStatusProjection(snapshot, status)
	})
	reconciler := &SubnetStatusReconciler{Reader: reader, Projector: projector}

	projection, err := reconciler.ProjectStatus(t.Context(), SubnetStatusRequest{Name: subnet.Name})
	require.NoError(t, err)
	require.Equal(t, subnet.Name, gotSnapshot.Name)
	require.Equal(t, subnet.UID, gotSnapshot.UID)
	require.Equal(t, subnet.Name, gotObservation.Name)
	require.Equal(t, "00:00:5e:00:53:01", projection.Status.GatewayMAC)
	require.Equal(t, gotSnapshot, projection.Snapshot)
}

func TestSubnetStatusReconcilerTreatsDeletedSubnetAsConverged(t *testing.T) {
	called := false
	reconciler := &SubnetStatusReconciler{
		Reader: newRuntimeClient(t),
		Projector: SubnetStatusProjectorFunc(func(context.Context, SubnetWriteSnapshot, SubnetShadowObservation) (SubnetStatusProjection, error) {
			called = true
			return SubnetStatusProjection{}, nil
		}),
	}
	_, err := reconciler.Reconcile(t.Context(), SubnetStatusRequest{Name: "missing"})
	require.NoError(t, err)
	require.False(t, called)
}

func TestSubnetStatusReconcilerPropagatesProjectorError(t *testing.T) {
	want := errors.New("projection failed")
	reconciler := &SubnetStatusReconciler{
		Reader: newRuntimeClient(t, testSubnet("")),
		Projector: SubnetStatusProjectorFunc(func(context.Context, SubnetWriteSnapshot, SubnetShadowObservation) (SubnetStatusProjection, error) {
			return SubnetStatusProjection{}, want
		}),
	}
	_, err := reconciler.Reconcile(t.Context(), SubnetStatusRequest{Name: "subnet-a"})
	require.ErrorIs(t, err, want)
}

func TestSubnetStatusReconcilerFailsClosedWithoutProjectionInputs(t *testing.T) {
	reconciler := &SubnetStatusReconciler{}
	_, err := reconciler.ProjectStatus(t.Context(), SubnetStatusRequest{Name: "subnet-a"})
	require.ErrorIs(t, err, ErrSubnetStatusReaderMissing)

	reconciler.Reader = newRuntimeClient(t, testSubnet(""))
	_, err = reconciler.ProjectStatus(t.Context(), SubnetStatusRequest{Name: "subnet-a"})
	require.ErrorIs(t, err, ErrSubnetStatusProjectorMissing)
}

type recordingSubnetStatusEventSource struct {
	events []SubnetStatusEvent
	bridge *recordingSubnetStatusEventBridge
}

func (s *recordingSubnetStatusEventSource) StartSubnetStatusEventBridge(_ context.Context, enqueue func(SubnetStatusEvent)) (SubnetStatusEventBridge, error) {
	for _, event := range s.events {
		enqueue(event)
	}
	if s.bridge == nil {
		s.bridge = &recordingSubnetStatusEventBridge{}
	}
	return s.bridge, nil
}

type recordingSubnetStatusEventBridge struct {
	closed chan struct{}
}

func (b *recordingSubnetStatusEventBridge) Close() {
	if b.closed != nil {
		select {
		case <-b.closed:
		default:
			close(b.closed)
		}
	}
}

var _ SubnetStatusEventSource = (*recordingSubnetStatusEventSource)(nil)
