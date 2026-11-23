package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"

	openvswitch "github.com/digitalocean/go-openvswitch/ovs"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestSyncFlowsOnlyTouchesOwnedBridges(t *testing.T) {
	t.Parallel()

	var queried []string
	client := openvswitch.New(openvswitch.Exec(func(command string, args ...string) ([]byte, error) {
		require.Equal(t, "ovs-ofctl", command)
		require.Equal(t, "dump-flows", args[0])
		queried = append(queried, args[1])
		// Stop at the dump boundary so the test never executes host OVS tools.
		return nil, errors.New("injected dump failure")
	}))
	controller := &Controller{
		ovsClient: client,
		vswitchTables: &bridgePortProvider{bridges: []vswitch.Bridge{
			{Name: "br-provider", ExternalIDs: map[string]string{"vendor": util.CniTypeName}},
			{Name: "br-int"},
			{Name: "br-third-party", ExternalIDs: map[string]string{"vendor": "other"}},
		}},
	}
	controller.syncFlows()
	require.Equal(t, []string{"br-provider"}, queried)
}

func TestUnderlayServiceFlowTracksPatchOfport(t *testing.T) {
	interfaces := flowInterfaceProvider{
		"eth1":                              {Name: "eth1", Ofport: new(3)},
		"patch-localnet.underlay-to-br-int": {Name: "patch-localnet.underlay-to-br-int", Ofport: new(-1)},
	}
	patch := interfaces["patch-localnet.underlay-to-br-int"]
	underlay := interfaces["eth1"]

	c := &Controller{
		vswitchTables: interfaces,
		flowCache:     make(map[string]map[string]LbServiceRules),
		flowChan:      make(chan struct{}, 1),
	}
	rule := LbServiceRules{
		IP: "172.19.0.100", Port: 80, Protocol: "TCP", DstMac: "00:00:00:01:00:01",
		UnderlayNic: "eth1", BridgeName: "br-provider", SubnetName: "underlay",
	}
	require.ErrorIs(t, c.AddOrUpdateUnderlaySubnetSvcLocalFlowCache(rule), errUnderlayLocalnetPatchNotReady)
	require.Empty(t, c.storeFlowCache()["br-provider"])

	patch.Ofport = new(5)
	require.NoError(t, c.AddOrUpdateUnderlaySubnetSvcLocalFlowCache(rule))

	checkOutput := func(want string) {
		t.Helper()
		flows := c.storeFlowCache()["br-provider"]
		require.Len(t, flows, 1)
		require.True(t, strings.HasSuffix(flows[0], "output:"+want), "flow = %q", flows[0])
	}
	checkOutput("5")

	patch.Ofport = new(6)
	checkOutput("6")

	patch.Ofport = new(-1)
	require.Empty(t, c.storeFlowCache()["br-provider"])

	patch.Ofport = nil
	require.Empty(t, c.storeFlowCache()["br-provider"])

	patch.Ofport = new(7)
	checkOutput("7")

	underlay.Ofport = new(-1)
	require.Empty(t, c.storeFlowCache()["br-provider"])
	underlay.Ofport = new(3)
	checkOutput("7")
}

type flowInterfaceProvider map[string]*vswitch.Interface

func (p flowInterfaceProvider) Table(model.Model) table.Handle { return flowInterfaceHandle{rows: p} }

type flowInterfaceHandle struct {
	table.Handle
	rows flowInterfaceProvider
}

func (h flowInterfaceHandle) Filter(_ context.Context, predicate, result any) error {
	rows := result.(*[]vswitch.Interface)
	for _, row := range h.rows {
		if predicate.(func(*vswitch.Interface) bool)(row) {
			*rows = append(*rows, *row)
		}
	}
	return nil
}
