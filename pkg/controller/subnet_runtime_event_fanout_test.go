package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSubnetRuntimeEventFanoutReplaysPendingNames(t *testing.T) {
	fanout := newSubnetRuntimeEventFanout()
	fanout.publish("subnet-a")
	fanout.publish("subnet-a")

	events := make(chan string, 1)
	unsubscribe := fanout.subscribe(t.Context(), func(name string) { events <- name })
	defer unsubscribe()
	require.Equal(t, "subnet-a", <-events)
}

func TestEnqueueSubnetReconcileRoutesWriterEvents(t *testing.T) {
	c := &Controller{
		config:                 &Configuration{ControllerRuntimeMode: ControllerRuntimeModeSubnetWriter},
		addOrUpdateSubnetQueue: newTypedRateLimitingQueue[string]("AddSubnet", nil),
		subnetRuntimeEvents:    newSubnetRuntimeEventFanout(),
	}
	t.Cleanup(c.addOrUpdateSubnetQueue.ShutDown)
	events := make(chan string, 1)
	unsubscribe := c.SubscribeSubnetRuntimeEvents(context.Background(), func(name string) { events <- name })
	defer unsubscribe()
	c.enqueueSubnetReconcile("subnet-a")
	require.Equal(t, "subnet-a", <-events)
	require.Equal(t, 0, c.addOrUpdateSubnetQueue.Len())
}

func TestEnqueueSubnetStatusAfterRoutesWriterEvents(t *testing.T) {
	c := &Controller{
		config:              &Configuration{ControllerRuntimeMode: ControllerRuntimeModeSubnetWriter},
		subnetRuntimeEvents: newSubnetRuntimeEventFanout(),
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events := make(chan string, 1)
	unsubscribe := c.SubscribeSubnetRuntimeEvents(ctx, func(name string) {
		events <- name
	})
	defer unsubscribe()

	c.enqueueSubnetStatusAfter("subnet-a", "VIP", 10*time.Millisecond)
	select {
	case event := <-events:
		require.Equal(t, "subnet-a", event)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delayed writer runtime fan-out")
	}
}

func TestSubnetRuntimeEventFanoutUnsubscribeStopsEvents(t *testing.T) {
	fanout := newSubnetRuntimeEventFanout()
	events := make(chan struct{}, 1)
	unsubscribe := fanout.subscribe(t.Context(), func(string) { events <- struct{}{} })
	unsubscribe()
	fanout.publish("subnet-a")
	select {
	case <-events:
		t.Fatal("received event after unsubscribe")
	default:
	}
}
