package daemon

import (
	"context"
	"errors"
	"testing"

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
		reconcileErr: errors.New("准备失败"),
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
		cleanupErr: errors.New("清理失败"),
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
