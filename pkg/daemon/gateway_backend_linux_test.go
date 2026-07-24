package daemon

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type recordingGatewayBackend struct {
	name         gatewayNetfilterMode
	calls        *[]string
	reconcileErr error
	cleanupErr   error
}

func (b *recordingGatewayBackend) Name() gatewayNetfilterMode {
	return b.name
}

func (b *recordingGatewayBackend) Reconcile(context.Context) error {
	*b.calls = append(*b.calls, string(b.name)+":reconcile")
	return b.reconcileErr
}

func (b *recordingGatewayBackend) Cleanup(context.Context) error {
	*b.calls = append(*b.calls, string(b.name)+":cleanup")
	return b.cleanupErr
}

func (*recordingGatewayBackend) ReadSubnetCounters(context.Context) error {
	return nil
}

func TestGatewayBackendManagerSwitchOrder(t *testing.T) {
	calls := []string{}
	oldBackend := &recordingGatewayBackend{name: gatewayNetfilterModeIPTables, calls: &calls}
	newBackend := &recordingGatewayBackend{name: gatewayNetfilterModeNFTables, calls: &calls}
	manager := newGatewayBackendManager(oldBackend, newBackend)
	manager.current = oldBackend

	require.NoError(t, manager.switchTo(context.Background(), newBackend))
	require.Equal(t, []string{"nftables:reconcile", "iptables:cleanup"}, calls)
	require.Equal(t, newBackend, manager.current)
	require.False(t, manager.degraded)
}

func TestGatewayBackendManagerKeepsCurrentWhenPrepareFails(t *testing.T) {
	calls := []string{}
	oldBackend := &recordingGatewayBackend{name: gatewayNetfilterModeIPTables, calls: &calls}
	newBackend := &recordingGatewayBackend{
		name:         gatewayNetfilterModeNFTables,
		calls:        &calls,
		reconcileErr: errors.New("prepare failed"),
	}
	manager := newGatewayBackendManager(oldBackend, newBackend)
	manager.current = oldBackend

	require.Error(t, manager.switchTo(context.Background(), newBackend))
	require.Equal(t, []string{"nftables:reconcile"}, calls)
	require.Equal(t, oldBackend, manager.current)
	require.False(t, manager.degraded)
}

func TestGatewayBackendManagerRetriesCleanup(t *testing.T) {
	calls := []string{}
	oldBackend := &recordingGatewayBackend{
		name:       gatewayNetfilterModeIPTables,
		calls:      &calls,
		cleanupErr: errors.New("cleanup failed"),
	}
	newBackend := &recordingGatewayBackend{name: gatewayNetfilterModeNFTables, calls: &calls}
	manager := newGatewayBackendManager(oldBackend, newBackend)
	manager.current = oldBackend

	require.Error(t, manager.switchTo(context.Background(), newBackend))
	require.Equal(t, []string{"nftables:reconcile", "iptables:cleanup"}, calls)
	require.Equal(t, oldBackend, manager.current)
	require.Equal(t, newBackend, manager.ready)
	require.True(t, manager.degraded)

	oldBackend.cleanupErr = nil
	require.NoError(t, manager.switchTo(context.Background(), newBackend))
	require.Equal(t, []string{
		"nftables:reconcile",
		"iptables:cleanup",
		"nftables:reconcile",
		"iptables:cleanup",
	}, calls)
	require.Equal(t, newBackend, manager.current)
	require.Nil(t, manager.ready)
	require.False(t, manager.degraded)
}

func TestGatewayBackendManagerCleansAbandonedReadyBackend(t *testing.T) {
	calls := []string{}
	iptablesBackend := &recordingGatewayBackend{
		name:       gatewayNetfilterModeIPTables,
		calls:      &calls,
		cleanupErr: errors.New("cleanup failed"),
	}
	nftBackend := &recordingGatewayBackend{name: gatewayNetfilterModeNFTables, calls: &calls}
	manager := newGatewayBackendManager(iptablesBackend, nftBackend)
	manager.current = iptablesBackend

	require.Error(t, manager.switchTo(context.Background(), nftBackend))
	manager.mode = gatewayNetfilterModeIPTables
	require.NoError(t, manager.Reconcile(context.Background()))
	require.Equal(t, []string{
		"nftables:reconcile",
		"iptables:cleanup",
		"iptables:reconcile",
		"nftables:cleanup",
	}, calls)
	require.Equal(t, iptablesBackend, manager.current)
	require.Nil(t, manager.ready)
	require.False(t, manager.degraded)
}

func TestGatewayBackendManagerCleansInactiveBackendOnStartup(t *testing.T) {
	calls := []string{}
	iptablesBackend := &recordingGatewayBackend{name: gatewayNetfilterModeIPTables, calls: &calls}
	nftBackend := &recordingGatewayBackend{name: gatewayNetfilterModeNFTables, calls: &calls}
	manager := newGatewayBackendManager(iptablesBackend, nftBackend)
	manager.current = iptablesBackend
	manager.mode = gatewayNetfilterModeIPTables

	require.NoError(t, manager.Reconcile(context.Background()))
	require.Equal(t, []string{"iptables:reconcile", "nftables:cleanup"}, calls)
}

func TestKubeOVNIPSetProtocol(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		owned    bool
	}{
		{name: "ovn40subnets", protocol: "IPv4", owned: true},
		{name: "ovn60subnets-nat-policy", protocol: "IPv6", owned: true},
		{name: "KUBE-CLUSTER-IP"},
		{name: "ovn-other-project"},
	}

	for _, tt := range tests {
		protocol, owned := kubeOVNIPSetProtocol(tt.name)
		require.Equal(t, tt.owned, owned)
		require.Equal(t, tt.protocol, protocol)
	}
}

func TestGatewayBackendManagerAutoSwitchesAfterStableDetection(t *testing.T) {
	proxyMode := "nftables"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, proxyMode)
	}))
	defer server.Close()

	calls := []string{}
	iptablesBackend := &recordingGatewayBackend{name: gatewayNetfilterModeIPTables, calls: &calls}
	nftBackend := &recordingGatewayBackend{name: gatewayNetfilterModeNFTables, calls: &calls}
	manager := newGatewayBackendManager(iptablesBackend, nftBackend)
	manager.current = iptablesBackend
	manager.mode = gatewayNetfilterModeAuto
	manager.detector = newProxyModeDetector(server.URL, time.Second, nil)
	manager.coldStart = true

	require.NoError(t, manager.Reconcile(context.Background()))
	require.Equal(t, []string{"nftables:reconcile", "iptables:cleanup"}, calls)

	calls = nil
	proxyMode = "iptables"
	require.NoError(t, manager.Reconcile(context.Background()))
	require.NoError(t, manager.Reconcile(context.Background()))
	require.Equal(t, []string{"nftables:reconcile", "nftables:reconcile"}, calls)
	require.NoError(t, manager.Reconcile(context.Background()))
	require.Equal(t, []string{
		"nftables:reconcile",
		"nftables:reconcile",
		"iptables:reconcile",
		"nftables:cleanup",
	}, calls)
}

func TestGatewayBackendManagerResetsStabilityOnDetectionFailure(t *testing.T) {
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "nftables")
	}))
	defer server.Close()

	calls := []string{}
	iptablesBackend := &recordingGatewayBackend{name: gatewayNetfilterModeIPTables, calls: &calls}
	nftBackend := &recordingGatewayBackend{name: gatewayNetfilterModeNFTables, calls: &calls}
	manager := newGatewayBackendManager(iptablesBackend, nftBackend)
	manager.current = iptablesBackend
	manager.mode = gatewayNetfilterModeAuto
	manager.detector = newProxyModeDetector(server.URL, time.Second, nil)

	for range 3 {
		fail = false
		require.NoError(t, manager.Reconcile(context.Background()))
		require.Equal(t, iptablesBackend, manager.current)
		fail = true
		require.Error(t, manager.Reconcile(context.Background()))
	}

	fail = false
	for range 3 {
		require.NoError(t, manager.Reconcile(context.Background()))
	}
	require.Equal(t, nftBackend, manager.current)
}

func TestGatewayBackendManagerKeepsCurrentOnDetectionFailure(t *testing.T) {
	calls := []string{}
	nftBackend := &recordingGatewayBackend{name: gatewayNetfilterModeNFTables, calls: &calls}
	manager := newGatewayBackendManager(nftBackend)
	manager.current = nftBackend
	manager.mode = gatewayNetfilterModeAuto
	manager.detector = newProxyModeDetector("http://127.0.0.1:1/proxyMode", 10*time.Millisecond, nil)

	require.Error(t, manager.Reconcile(context.Background()))
	require.Equal(t, []string{"nftables:reconcile"}, calls)
	require.Equal(t, nftBackend, manager.current)
}

func TestGatewayBackendManagerKeepsCurrentWhenFactoryFails(t *testing.T) {
	calls := []string{}
	iptablesBackend := &recordingGatewayBackend{name: gatewayNetfilterModeIPTables, calls: &calls}
	manager := newGatewayBackendManager(iptablesBackend)
	manager.current = iptablesBackend
	manager.mode = gatewayNetfilterModeNFTables
	manager.factories[gatewayNetfilterModeNFTables] = func() (gatewayNetfilterBackend, error) {
		return nil, errors.New("nftables unavailable")
	}

	require.Error(t, manager.Reconcile(context.Background()))
	require.Equal(t, []string{"iptables:reconcile"}, calls)
	require.Equal(t, iptablesBackend, manager.current)
}
