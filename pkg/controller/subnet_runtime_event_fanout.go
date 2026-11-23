package controller

import (
	"context"
	"sync"
)

// subnetRuntimeEventFanout mirrors dependency changes to the opt-in Subnet
// runtime writer. Pending names are coalesced until the runtime subscribes.
type subnetRuntimeEventFanout struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[uint64]func(string)
	pending     map[string]struct{}
}

func newSubnetRuntimeEventFanout() *subnetRuntimeEventFanout {
	return &subnetRuntimeEventFanout{
		subscribers: make(map[uint64]func(string)),
		pending:     make(map[string]struct{}),
	}
}

func newSubnetRuntimeEventFanoutForConfig(config *Configuration) *subnetRuntimeEventFanout {
	if config == nil || config.ControllerRuntimeMode != ControllerRuntimeModeSubnetWriter {
		return nil
	}
	return newSubnetRuntimeEventFanout()
}

func (f *subnetRuntimeEventFanout) publish(name string) {
	if f == nil || name == "" {
		return
	}
	f.mu.Lock()
	if len(f.subscribers) == 0 {
		f.pending[name] = struct{}{}
		f.mu.Unlock()
		return
	}
	subscribers := make([]func(string), 0, len(f.subscribers))
	for _, subscriber := range f.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	f.mu.Unlock()
	for _, subscriber := range subscribers {
		subscriber(name)
	}
}

func (f *subnetRuntimeEventFanout) subscribe(ctx context.Context, subscriber func(string)) func() {
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
	pending := make([]string, 0, len(f.pending))
	for name := range f.pending {
		pending = append(pending, name)
	}
	clear(f.pending)
	f.mu.Unlock()
	for _, name := range pending {
		subscriber(name)
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

func (c *Controller) SubscribeSubnetRuntimeEvents(ctx context.Context, subscriber func(string)) func() {
	if c == nil || c.subnetRuntimeEvents == nil {
		return func() {}
	}
	return c.subnetRuntimeEvents.subscribe(ctx, subscriber)
}

func (c *Controller) enqueueSubnetReconcile(name string) {
	if c == nil || name == "" {
		return
	}
	if controllerRuntimeSubnetWriterEnabled(c.config) {
		if c.subnetRuntimeEvents != nil {
			c.subnetRuntimeEvents.publish(name)
		}
		return
	}
	c.addOrUpdateSubnetQueue.Add(name)
}
