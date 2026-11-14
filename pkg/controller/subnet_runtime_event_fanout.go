package controller

import (
	"context"
	"sync"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

// subnetRuntimeEventFanout mirrors dependency changes to the opt-in Subnet
// runtime writer. Pending names are coalesced until the runtime subscribes.
type subnetRuntimeEventFanout struct {
	mu            sync.Mutex
	nextID        uint64
	subscribers   map[uint64]func(string)
	deleteSubs    map[uint64]func(string, *kubeovnv1.Subnet)
	pending       map[string]struct{}
	deletePending map[string]*kubeovnv1.Subnet
}

func newSubnetRuntimeEventFanout() *subnetRuntimeEventFanout {
	return &subnetRuntimeEventFanout{
		subscribers:   make(map[uint64]func(string)),
		deleteSubs:    make(map[uint64]func(string, *kubeovnv1.Subnet)),
		pending:       make(map[string]struct{}),
		deletePending: make(map[string]*kubeovnv1.Subnet),
	}
}

func (f *subnetRuntimeEventFanout) publishDelete(subnet *kubeovnv1.Subnet) {
	if f == nil || subnet == nil || subnet.Name == "" {
		return
	}
	snapshot := subnet.DeepCopy()
	f.mu.Lock()
	subscribers := make([]func(string), 0, len(f.subscribers))
	for _, subscriber := range f.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	deleteSubscribers := make([]func(string, *kubeovnv1.Subnet), 0, len(f.deleteSubs))
	for _, subscriber := range f.deleteSubs {
		deleteSubscribers = append(deleteSubscribers, subscriber)
	}
	if len(subscribers) == 0 {
		f.pending[subnet.Name] = struct{}{}
	} else {
		delete(f.pending, subnet.Name)
	}
	if len(deleteSubscribers) == 0 {
		f.deletePending[subnet.Name] = snapshot
	} else {
		delete(f.deletePending, subnet.Name)
	}
	f.mu.Unlock()
	for _, subscriber := range subscribers {
		subscriber(snapshot.Name)
	}
	for _, subscriber := range deleteSubscribers {
		subscriber(snapshot.Name, snapshot.DeepCopy())
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

func (f *subnetRuntimeEventFanout) subscribeDelete(ctx context.Context, subscriber func(string, *kubeovnv1.Subnet)) func() {
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
	f.deleteSubs[id] = subscriber
	pending := make([]*kubeovnv1.Subnet, 0, len(f.deletePending))
	for name, snapshot := range f.deletePending {
		pending = append(pending, snapshot.DeepCopy())
		delete(f.deletePending, name)
	}
	f.mu.Unlock()
	for _, snapshot := range pending {
		subscriber(snapshot.Name, snapshot)
	}
	var once sync.Once
	unsubscribe := func() { once.Do(func() { f.mu.Lock(); delete(f.deleteSubs, id); f.mu.Unlock() }) }
	if ctx.Done() != nil {
		context.AfterFunc(ctx, unsubscribe)
	}
	return unsubscribe
}

func (c *Controller) SubscribeSubnetRuntimeDeleteEvents(ctx context.Context, subscriber func(string, *kubeovnv1.Subnet)) func() {
	if c == nil || c.subnetRuntimeEvents == nil {
		return func() {}
	}
	return c.subnetRuntimeEvents.subscribeDelete(ctx, subscriber)
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
