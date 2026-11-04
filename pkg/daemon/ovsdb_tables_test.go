package daemon

import (
	"context"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

type bridgePortProvider struct {
	bridges []vswitch.Bridge
	ports   []vswitch.Port
}

func (p *bridgePortProvider) Table(model.Model) table.Handle {
	return bridgePortHandle{provider: p}
}

type bridgePortHandle struct {
	table.Handle
	provider *bridgePortProvider
}

func (h bridgePortHandle) Filter(_ context.Context, predicate, result any) error {
	switch rows := result.(type) {
	case *[]vswitch.Bridge:
		match := predicate.(func(*vswitch.Bridge) bool)
		for i := range h.provider.bridges {
			if match(&h.provider.bridges[i]) {
				*rows = append(*rows, h.provider.bridges[i])
			}
		}
	case *[]vswitch.Port:
		match := predicate.(func(*vswitch.Port) bool)
		for i := range h.provider.ports {
			if match(&h.provider.ports[i]) {
				*rows = append(*rows, h.provider.ports[i])
			}
		}
	}
	return nil
}

func TestListVswitchBridgePortsExcludesLocalPort(t *testing.T) {
	t.Parallel()

	provider := &bridgePortProvider{
		bridges: []vswitch.Bridge{
			{Name: "br-provider", Ports: []string{"local", "nic", "vlan", "patch"}},
			{Name: "br-other", Ports: []string{"other"}},
			{Name: "br-empty", Ports: []string{"empty-local"}},
		},
		ports: []vswitch.Port{
			{UUID: "local", Name: "br-provider"},
			{UUID: "nic", Name: "eth0"},
			{UUID: "vlan", Name: "br-provider-vlan20"},
			{UUID: "patch", Name: "patch-provider-to-br-int"},
			{UUID: "other", Name: "eth1"},
			{UUID: "empty-local", Name: "br-empty"},
		},
	}
	controller := &Controller{vswitchTables: provider}

	ports, err := controller.listVswitchBridgePorts("br-provider")
	require.NoError(t, err)
	require.ElementsMatch(t, []vswitch.Port{
		{UUID: "nic", Name: "eth0"},
		{UUID: "vlan", Name: "br-provider-vlan20"},
		{UUID: "patch", Name: "patch-provider-to-br-int"},
	}, ports)

	for _, bridge := range []string{"br-missing", "br-empty"} {
		ports, err := controller.listVswitchBridgePorts(bridge)
		require.NoError(t, err)
		require.Empty(t, ports)
	}
}
