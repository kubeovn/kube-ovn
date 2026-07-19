package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type gatewayNetfilterBackend interface {
	Name() gatewayNetfilterMode
	Reconcile(context.Context) error
	Cleanup(context.Context) error
	ReadSubnetCounters(context.Context) error
}

type gatewayBackendManager struct {
	mutex    sync.RWMutex
	backends map[gatewayNetfilterMode]gatewayNetfilterBackend
	current  gatewayNetfilterBackend
	ready    gatewayNetfilterBackend
	degraded bool
}

func newGatewayBackendManager(backends ...gatewayNetfilterBackend) *gatewayBackendManager {
	manager := &gatewayBackendManager{
		backends: make(map[gatewayNetfilterMode]gatewayNetfilterBackend, len(backends)),
	}
	for _, backend := range backends {
		manager.backends[backend.Name()] = backend
	}
	return manager
}

func (m *gatewayBackendManager) Reconcile(ctx context.Context) error {
	m.mutex.RLock()
	current := m.current
	m.mutex.RUnlock()
	if current == nil {
		return errors.New("网关 netfilter 后端尚未选择")
	}
	return current.Reconcile(ctx)
}

func (m *gatewayBackendManager) ReadSubnetCounters(ctx context.Context) error {
	m.mutex.RLock()
	current := m.current
	m.mutex.RUnlock()
	if current == nil {
		return nil
	}
	return current.ReadSubnetCounters(ctx)
}

func (m *gatewayBackendManager) switchTo(ctx context.Context, target gatewayNetfilterBackend) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if err := target.Reconcile(ctx); err != nil {
		return fmt.Errorf("准备 %s 后端: %w", target.Name(), err)
	}

	old := m.current
	m.ready = target
	if old != nil && old.Name() != target.Name() {
		if err := old.Cleanup(ctx); err != nil {
			m.degraded = true
			return fmt.Errorf("清理 %s 后端: %w", old.Name(), err)
		}
	}

	m.current = target
	m.ready = nil
	m.degraded = false
	return nil
}

type iptablesGatewayBackend struct {
	controller *Controller
}

func (*iptablesGatewayBackend) Name() gatewayNetfilterMode {
	return gatewayNetfilterModeIPTables
}

func (b *iptablesGatewayBackend) Reconcile(context.Context) error {
	if err := b.controller.setIPSet(); err != nil {
		return err
	}
	if err := b.controller.setIptables(); err != nil {
		return err
	}
	b.controller.gcIPSet()
	return nil
}

func (b *iptablesGatewayBackend) Cleanup(context.Context) error {
	return b.controller.cleanupKubeOVNIptablesAndIPSets()
}

func (b *iptablesGatewayBackend) ReadSubnetCounters(context.Context) error {
	b.controller.setOvnSubnetGatewayMetric()
	return nil
}
