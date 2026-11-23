package controllerruntime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLegacySubnetStatusEventSourceOwnsOneRestartableBridge(t *testing.T) {
	legacy := &lifecycleLegacyEventSource{}
	source := NewLegacySubnetStatusEventSource(legacy)
	bridge, err := source.StartSubnetStatusEventBridge(t.Context(), func(SubnetStatusEvent) {})
	require.NoError(t, err)
	require.NotNil(t, bridge)
	same, err := source.StartSubnetStatusEventBridge(t.Context(), func(SubnetStatusEvent) {})
	require.NoError(t, err)
	require.Same(t, bridge, same)
	require.Equal(t, 1, legacy.subscriptions())

	bridge.Close()
	bridge.Close()
	require.Equal(t, 1, legacy.unsubscriptions())

	restarted, err := source.StartSubnetStatusEventBridge(t.Context(), func(SubnetStatusEvent) {})
	require.NoError(t, err)
	require.NotSame(t, bridge, restarted)
	require.Equal(t, 2, legacy.subscriptions())
	restarted.Close()
}

func TestLegacySubnetStatusEventSourceRejectsDifferentActiveContext(t *testing.T) {
	legacy := &lifecycleLegacyEventSource{}
	source := NewLegacySubnetStatusEventSource(legacy)
	var first, second []SubnetStatusEvent
	ctx := t.Context()
	bridge, err := source.StartSubnetStatusEventBridge(ctx, func(event SubnetStatusEvent) {
		first = append(first, event)
	})
	require.NoError(t, err)
	otherContext, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err = source.StartSubnetStatusEventBridge(otherContext, func(event SubnetStatusEvent) {
		second = append(second, event)
	})
	require.ErrorIs(t, err, ErrSubnetStatusBridgeActive)
	legacy.emit("subnet-a", "IP")
	require.Equal(t, []SubnetStatusEvent{{Name: "subnet-a", SourceKind: "IP"}}, first)
	require.Empty(t, second)
	bridge.Close()
}

func TestLegacySubnetStatusEventSourceRejectsCanceledBridgeContext(t *testing.T) {
	legacy := &lifecycleLegacyEventSource{}
	source := NewLegacySubnetStatusEventSource(legacy)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := source.StartSubnetStatusEventBridge(ctx, func(SubnetStatusEvent) {})
	require.ErrorIs(t, err, context.Canceled)
}

func TestLegacySubnetStatusEventSourceRequiresEnqueue(t *testing.T) {
	source := NewLegacySubnetStatusEventSource(&lifecycleLegacyEventSource{})
	_, err := source.StartSubnetStatusEventBridge(t.Context(), nil)
	require.ErrorIs(t, err, ErrSubnetStatusBridgeEnqueueMissing)
}

func TestLegacySubnetStatusEventSourceClosesOnContextCancel(t *testing.T) {
	legacy := &lifecycleLegacyEventSource{unsubscribed: make(chan struct{})}
	source := NewLegacySubnetStatusEventSource(legacy)
	ctx, cancel := context.WithCancel(t.Context())
	_, err := source.StartSubnetStatusEventBridge(ctx, func(SubnetStatusEvent) {})
	require.NoError(t, err)
	cancel()
	select {
	case <-legacy.unsubscribed:
	case <-time.After(time.Second):
		t.Fatal("event bridge did not close")
	}
}

func TestLegacySubnetStatusEventSourceWaitsForUnsubscribeBeforeRestart(t *testing.T) {
	legacy := &lifecycleLegacyEventSource{
		unsubscribeStarted:  make(chan struct{}),
		unsubscribeContinue: make(chan struct{}),
	}
	source := NewLegacySubnetStatusEventSource(legacy)
	bridge, err := source.StartSubnetStatusEventBridge(t.Context(), func(SubnetStatusEvent) {})
	require.NoError(t, err)

	closed := make(chan struct{})
	go func() {
		bridge.Close()
		close(closed)
	}()
	<-legacy.unsubscribeStarted

	started := make(chan struct{})
	go func() {
		_, _ = source.StartSubnetStatusEventBridge(t.Context(), func(SubnetStatusEvent) {})
		close(started)
	}()
	select {
	case <-started:
		t.Fatal("bridge restarted before unsubscribe completed")
	case <-time.After(25 * time.Millisecond):
	}

	close(legacy.unsubscribeContinue)
	<-closed
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("bridge did not restart after unsubscribe completed")
	}
}

type lifecycleLegacyEventSource struct {
	mu                   sync.Mutex
	subscribeN           int
	unsubscribeN         int
	unsubscribed         chan struct{}
	unsubscribeStarted   chan struct{}
	unsubscribeContinue  chan struct{}
	unsubscribeStartOnce sync.Once
	callback             func(string, string)
}

func (s *lifecycleLegacyEventSource) SubscribeSubnetStatusEvents(_ context.Context, callback func(string, string)) func() {
	s.mu.Lock()
	s.subscribeN++
	s.callback = callback
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			if s.unsubscribeStarted != nil {
				s.unsubscribeStartOnce.Do(func() { close(s.unsubscribeStarted) })
			}
			if s.unsubscribeContinue != nil {
				<-s.unsubscribeContinue
			}
			s.mu.Lock()
			s.unsubscribeN++
			s.mu.Unlock()
			if s.unsubscribed != nil {
				close(s.unsubscribed)
			}
		})
	}
}

func (s *lifecycleLegacyEventSource) emit(name, sourceKind string) {
	s.mu.Lock()
	callback := s.callback
	s.mu.Unlock()
	callback(name, sourceKind)
}

func (s *lifecycleLegacyEventSource) subscriptions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.subscribeN
}

func (s *lifecycleLegacyEventSource) unsubscriptions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unsubscribeN
}

var _ LegacySubnetStatusEventSource = (*lifecycleLegacyEventSource)(nil)
