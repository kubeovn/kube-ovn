package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSubnetStatusEventFanoutReplaysCoalescedPendingEvents(t *testing.T) {
	fanout := newSubnetStatusEventFanout()
	fanout.publish("subnet-a", "IP")
	fanout.publish("subnet-a", "Pod")
	fanout.publish("subnet-b", "VIP")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events := make(chan subnetStatusEvent, 2)
	unsubscribe := fanout.subscribe(ctx, func(name, sourceKind string) {
		events <- subnetStatusEvent{name: name, sourceKind: sourceKind}
	})
	defer unsubscribe()

	got := []subnetStatusEvent{<-events, <-events}
	require.ElementsMatch(t, []subnetStatusEvent{
		{name: "subnet-a", sourceKind: "Pod"},
		{name: "subnet-b", sourceKind: "VIP"},
	}, got)
}

func TestSubnetStatusEventFanoutNilContextRequiresExplicitUnsubscribe(t *testing.T) {
	fanout := newSubnetStatusEventFanout()
	events := make(chan subnetStatusEvent, 1)
	var nilContext context.Context
	unsubscribe := fanout.subscribe(nilContext, func(name, sourceKind string) {
		events <- subnetStatusEvent{name: name, sourceKind: sourceKind}
	})

	fanout.publish("subnet-a", "IP")
	require.Equal(t, subnetStatusEvent{name: "subnet-a", sourceKind: "IP"}, <-events)
	unsubscribe()
	fanout.publish("subnet-b", "IP")
	select {
	case event := <-events:
		t.Fatalf("received event after explicit unsubscribe: %+v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestEnqueueSubnetStatusPreservesLegacyQueueAndFansOut(t *testing.T) {
	c := &Controller{
		updateSubnetStatusQueue: newTypedRateLimitingQueue[string]("UpdateSubnetStatus", nil),
		subnetStatusEvents:      newSubnetStatusEventFanout(),
	}
	t.Cleanup(c.updateSubnetStatusQueue.ShutDown)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events := make(chan subnetStatusEvent, 1)
	unsubscribe := c.SubscribeSubnetStatusEvents(ctx, func(name, sourceKind string) {
		events <- subnetStatusEvent{name: name, sourceKind: sourceKind}
	})
	defer unsubscribe()

	c.enqueueSubnetStatus("subnet-a", "EIP")
	item, shutdown := c.updateSubnetStatusQueue.Get()
	require.False(t, shutdown)
	require.Equal(t, "subnet-a", item)
	c.updateSubnetStatusQueue.Done(item)
	c.updateSubnetStatusQueue.Forget(item)
	require.Equal(t, subnetStatusEvent{name: "subnet-a", sourceKind: "EIP"}, <-events)
}

func TestEnqueueSubnetStatusAfterDelaysFanout(t *testing.T) {
	c := &Controller{
		updateSubnetStatusQueue: newTypedRateLimitingQueue[string]("UpdateSubnetStatus", nil),
		subnetStatusEvents:      newSubnetStatusEventFanout(),
	}
	t.Cleanup(c.updateSubnetStatusQueue.ShutDown)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events := make(chan subnetStatusEvent, 1)
	unsubscribe := c.SubscribeSubnetStatusEvents(ctx, func(name, sourceKind string) {
		events <- subnetStatusEvent{name: name, sourceKind: sourceKind}
	})
	defer unsubscribe()

	c.enqueueSubnetStatusAfter("subnet-a", "VIP", 10*time.Millisecond)
	select {
	case event := <-events:
		require.Equal(t, subnetStatusEvent{name: "subnet-a", sourceKind: "VIP"}, event)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delayed status fan-out")
	}
}

func TestSubnetWriterStatusEventsSkipLegacyQueue(t *testing.T) {
	c := &Controller{
		config:                  &Configuration{ControllerRuntimeMode: ControllerRuntimeModeSubnetWriter},
		updateSubnetStatusQueue: newTypedRateLimitingQueue[string]("UpdateSubnetStatus", nil),
		subnetRuntimeEvents:     newSubnetRuntimeEventFanout(),
	}
	t.Cleanup(c.updateSubnetStatusQueue.ShutDown)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events := make(chan string, 1)
	unsubscribe := c.SubscribeSubnetRuntimeEvents(ctx, func(name string) {
		events <- name
	})
	defer unsubscribe()

	c.enqueueSubnetStatus("subnet-a", "IP")
	select {
	case event := <-events:
		require.Equal(t, "subnet-a", event)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for writer runtime fan-out")
	}
	require.Equal(t, 0, c.updateSubnetStatusQueue.Len())
}

func TestSubnetStatusEventFanoutIsDisabledForSubnetWriter(t *testing.T) {
	config := &Configuration{ControllerRuntimeMode: ControllerRuntimeModeSubnetWriter}
	require.Nil(t, newSubnetStatusEventFanoutForConfig(config))
}
