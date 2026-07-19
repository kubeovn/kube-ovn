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
	mutex     sync.RWMutex
	backends  map[gatewayNetfilterMode]gatewayNetfilterBackend
	factories map[gatewayNetfilterMode]func() (gatewayNetfilterBackend, error)
	current   gatewayNetfilterBackend
	ready     gatewayNetfilterBackend
	degraded  bool
	mode      gatewayNetfilterMode
	detector  *proxyModeDetector
	stability proxyModeStability
	coldStart bool
}

func newGatewayBackendManager(backends ...gatewayNetfilterBackend) *gatewayBackendManager {
	manager := &gatewayBackendManager{
		backends:  make(map[gatewayNetfilterMode]gatewayNetfilterBackend, len(backends)),
		factories: make(map[gatewayNetfilterMode]func() (gatewayNetfilterBackend, error)),
		mode:      gatewayNetfilterModeIPTables,
	}
	for _, backend := range backends {
		manager.backends[backend.Name()] = backend
	}
	return manager
}

func (m *gatewayBackendManager) Reconcile(ctx context.Context) error {
	desired, detectErr := m.desiredMode(ctx)
	current := m.currentBackend()
	if detectErr != nil {
		if current == nil {
			return detectErr
		}
		return errors.Join(detectErr, current.Reconcile(ctx))
	}

	target, err := m.ensureBackend(desired)
	if err != nil {
		if current != nil {
			return errors.Join(err, current.Reconcile(ctx))
		}
		return err
	}
	if current != nil && current.Name() == target.Name() {
		return current.Reconcile(ctx)
	}
	return m.switchTo(ctx, target)
}

func (m *gatewayBackendManager) ReadSubnetCounters(ctx context.Context) error {
	current := m.currentBackend()
	if current == nil {
		return nil
	}
	return current.ReadSubnetCounters(ctx)
}

func (m *gatewayBackendManager) desiredMode(ctx context.Context) (gatewayNetfilterMode, error) {
	if m.mode != gatewayNetfilterModeAuto {
		return m.mode, nil
	}
	if m.detector == nil {
		return "", errors.New("kube-proxy 模式探测器未初始化")
	}

	if m.coldStart {
		mode, err := m.detector.detectColdStart(ctx)
		if err != nil {
			return "", fmt.Errorf("冷启动探测 kube-proxy 模式: %w", err)
		}
		m.coldStart = false
		m.stability = proxyModeStability{}
		return mode, nil
	}

	mode, err := m.detector.detectHTTP(ctx)
	if err != nil {
		return "", fmt.Errorf("探测 kube-proxy 模式: %w", err)
	}
	current := m.currentBackend()
	if current != nil && current.Name() == mode {
		m.stability.observe(mode)
		return mode, nil
	}
	if m.stability.observe(mode) {
		return mode, nil
	}
	if current != nil {
		return current.Name(), nil
	}
	return mode, nil
}

func (m *gatewayBackendManager) ensureBackend(mode gatewayNetfilterMode) (gatewayNetfilterBackend, error) {
	m.mutex.RLock()
	backend := m.backends[mode]
	factory := m.factories[mode]
	m.mutex.RUnlock()
	if backend != nil {
		return backend, nil
	}
	if factory == nil {
		return nil, fmt.Errorf("网关 netfilter 后端 %s 不可用", mode)
	}

	backend, err := factory()
	if err != nil {
		return nil, fmt.Errorf("初始化 %s 后端: %w", mode, err)
	}
	m.mutex.Lock()
	if existing := m.backends[mode]; existing != nil {
		backend = existing
	} else {
		m.backends[mode] = backend
	}
	m.mutex.Unlock()
	return backend, nil
}

func (m *gatewayBackendManager) currentBackend() gatewayNetfilterBackend {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.current
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
