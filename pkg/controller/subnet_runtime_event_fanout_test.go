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

func TestSubnetRuntimeEventFanoutUnsubscribeStopsEvents(t *testing.T) {
	fanout := newSubnetRuntimeEventFanout()
	unsubscribe := fanout.subscribe(t.Context(), func(string) { t.Fatal("received event after unsubscribe") })
	unsubscribe()
	fanout.publish("subnet-a")
	select {
	case <-time.After(20 * time.Millisecond):
	}
}
