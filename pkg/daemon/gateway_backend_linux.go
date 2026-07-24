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
	mutex          sync.RWMutex
	backends       map[gatewayNetfilterMode]gatewayNetfilterBackend
	factories      map[gatewayNetfilterMode]func() (gatewayNetfilterBackend, error)
	current        gatewayNetfilterBackend
	ready          gatewayNetfilterBackend
	degraded       bool
	mode           gatewayNetfilterMode
	detector       *proxyModeDetector
	stability      proxyModeStability
	coldStart      bool
	initialCleanup bool
	warning        func(reason, message string)
}

func newGatewayBackendManager(backends ...gatewayNetfilterBackend) *gatewayBackendManager {
	manager := &gatewayBackendManager{
		backends:       make(map[gatewayNetfilterMode]gatewayNetfilterBackend, len(backends)),
		factories:      make(map[gatewayNetfilterMode]func() (gatewayNetfilterBackend, error)),
		mode:           gatewayNetfilterModeIPTables,
		initialCleanup: true,
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
		metricGatewayNetfilterDetectFailures.Inc()
		m.warn("GatewayNetfilterDetectFailed", detectErr.Error())
		if current == nil {
			return detectErr
		}
		return errors.Join(detectErr, current.Reconcile(ctx))
	}

	target, err := m.ensureBackend(desired)
	if err != nil {
		metricGatewayNetfilterSwitchFailures.Inc()
		m.warn("GatewayNetfilterSwitchFailed", err.Error())
		if current != nil {
			return errors.Join(err, current.Reconcile(ctx))
		}
		return err
	}
	if current != nil && current.Name() == target.Name() {
		if err := current.Reconcile(ctx); err != nil {
			return err
		}
		if err := m.cleanupInactiveBackend(ctx, current.Name()); err != nil {
			metricGatewayNetfilterSwitchFailures.Inc()
			m.warn("GatewayNetfilterSwitchFailed", err.Error())
			return err
		}
		setGatewayNetfilterBackendMetric(current.Name())
		return nil
	}
	if err := m.switchTo(ctx, target); err != nil {
		metricGatewayNetfilterSwitchFailures.Inc()
		m.warn("GatewayNetfilterSwitchFailed", err.Error())
		return err
	}
	setGatewayNetfilterBackendMetric(target.Name())
	return nil
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
		return "", errors.New("kube-proxy mode detector is not initialized")
	}

	if m.coldStart {
		mode, err := m.detector.detectColdStart(ctx)
		if err != nil {
			return "", fmt.Errorf("detect kube-proxy mode during cold start: %w", err)
		}
		m.coldStart = false
		m.stability = proxyModeStability{}
		return mode, nil
	}

	mode, err := m.detector.detectHTTP(ctx)
	if err != nil {
		m.stability = proxyModeStability{}
		return "", fmt.Errorf("detect kube-proxy mode: %w", err)
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
		return nil, fmt.Errorf("gateway netfilter backend %s is unavailable", mode)
	}

	backend, err := factory()
	if err != nil {
		return nil, fmt.Errorf("initialize %s backend: %w", mode, err)
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

func (m *gatewayBackendManager) warn(reason, message string) {
	if m.warning != nil {
		m.warning(reason, message)
	}
}

func (m *gatewayBackendManager) cleanupInactiveBackend(ctx context.Context, current gatewayNetfilterMode) error {
	m.mutex.RLock()
	backend := m.ready
	initialCleanup := m.initialCleanup
	m.mutex.RUnlock()
	if backend == nil || backend.Name() == current {
		if !initialCleanup {
			return nil
		}
		for _, mode := range []gatewayNetfilterMode{gatewayNetfilterModeIPTables, gatewayNetfilterModeNFTables} {
			if mode == current {
				continue
			}
			var err error
			backend, err = m.ensureBackend(mode)
			if err != nil {
				return fmt.Errorf("initialize inactive %s backend: %w", mode, err)
			}
			break
		}
	}
	if backend == nil {
		return nil
	}
	if err := backend.Cleanup(ctx); err != nil {
		m.mutex.Lock()
		m.degraded = true
		m.mutex.Unlock()
		return fmt.Errorf("clean up inactive %s backend: %w", backend.Name(), err)
	}
	m.mutex.Lock()
	if m.ready != nil && m.ready.Name() == backend.Name() {
		m.ready = nil
	}
	m.initialCleanup = false
	m.degraded = false
	m.mutex.Unlock()
	return nil
}

func setGatewayNetfilterBackendMetric(mode gatewayNetfilterMode) {
	for _, backend := range []gatewayNetfilterMode{gatewayNetfilterModeIPTables, gatewayNetfilterModeNFTables} {
		value := 0.0
		if backend == mode {
			value = 1
		}
		metricGatewayNetfilterBackend.WithLabelValues(string(backend)).Set(value)
	}
}

func (m *gatewayBackendManager) switchTo(ctx context.Context, target gatewayNetfilterBackend) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if err := target.Reconcile(ctx); err != nil {
		return fmt.Errorf("prepare %s backend: %w", target.Name(), err)
	}

	old := m.current
	m.ready = target
	if old != nil && old.Name() != target.Name() {
		if err := old.Cleanup(ctx); err != nil {
			m.degraded = true
			return fmt.Errorf("clean up %s backend: %w", old.Name(), err)
		}
	}

	m.current = target
	m.ready = nil
	m.initialCleanup = false
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
