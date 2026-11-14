package controller

import (
	"context"
	"sync"
	"time"
)

type subnetStatusEvent struct {
	name       string
	sourceKind string
}

// subnetStatusEventFanout mirrors legacy status notifications to an opt-in
// runtime consumer without changing ownership of the legacy status queue.
// Pending events are coalesced by Subnet name until the runtime subscribes.
type subnetStatusEventFanout struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[uint64]func(string, string)
	pending     map[string]subnetStatusEvent
}

func newSubnetStatusEventFanout() *subnetStatusEventFanout {
	return &subnetStatusEventFanout{
		subscribers: make(map[uint64]func(string, string)),
		pending:     make(map[string]subnetStatusEvent),
	}
}

func newSubnetStatusEventFanoutForConfig(config *Configuration) *subnetStatusEventFanout {
	if config == nil || (config.ControllerRuntimeMode != ControllerRuntimeModeSubnetStatus &&
		config.ControllerRuntimeMode != ControllerRuntimeModeSubnetWriter) {
		return nil
	}
	return newSubnetStatusEventFanout()
}

func (f *subnetStatusEventFanout) publish(name, sourceKind string) {
	if f == nil || name == "" {
		return
	}
	f.mu.Lock()
	if len(f.subscribers) == 0 {
		f.pending[name] = subnetStatusEvent{name: name, sourceKind: sourceKind}
		f.mu.Unlock()
		return
	}
	subscribers := make([]func(string, string), 0, len(f.subscribers))
	for _, subscriber := range f.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	f.mu.Unlock()
	for _, subscriber := range subscribers {
		subscriber(name, sourceKind)
	}
}

func (f *subnetStatusEventFanout) subscribe(ctx context.Context, subscriber func(string, string)) func() {
	if f == nil || subscriber == nil {
		return func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return func() {}
	}
	f.mu.Lock()
	f.nextID++
	id := f.nextID
	f.subscribers[id] = subscriber
	pending := make([]subnetStatusEvent, 0, len(f.pending))
	for _, event := range f.pending {
		pending = append(pending, event)
	}
	clear(f.pending)
	f.mu.Unlock()

	for _, event := range pending {
		subscriber(event.name, event.sourceKind)
	}

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			f.mu.Lock()
			delete(f.subscribers, id)
			f.mu.Unlock()
		})
	}
	if ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			unsubscribe()
		}()
	}
	return unsubscribe
}

func (c *Controller) SubscribeSubnetStatusEvents(ctx context.Context, subscriber func(string, string)) func() {
	if c == nil || c.subnetStatusEvents == nil {
		return func() {}
	}
	return c.subnetStatusEvents.subscribe(ctx, subscriber)
}

func (c *Controller) enqueueSubnetStatus(name, sourceKind string) {
	if !controllerRuntimeSubnetWriterEnabled(c.config) {
		c.updateSubnetStatusQueue.Add(name)
	}
	c.subnetStatusEvents.publish(name, sourceKind)
}

func (c *Controller) enqueueSubnetStatusAfter(name, sourceKind string, delay time.Duration) {
	if !controllerRuntimeSubnetWriterEnabled(c.config) {
		c.updateSubnetStatusQueue.AddAfter(name, delay)
	}
	if c.subnetStatusEvents == nil || name == "" {
		return
	}
	time.AfterFunc(delay, func() {
		c.subnetStatusEvents.publish(name, sourceKind)
	})
}
