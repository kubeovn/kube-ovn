package controller

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestSyncSgLogicalPortVipLifecycle(t *testing.T) {
	for _, provider := range []bool{false, true} {
		t.Run(fmt.Sprintf("table-provider=%t", provider), func(t *testing.T) {
			client := newInMemoryOVNNbClient(t)
			ctrl := &Controller{OVNNbClient: client, sgKeyMutex: keymutex.NewHashed(0)}
			if provider {
				ctrl.OVNNbTables = client
			}
			ctrl.addOrUpdateSgQueue = newTypedRateLimitingQueue[string](t.Name(), nil)
			t.Cleanup(ctrl.addOrUpdateSgQueue.ShutDown)
			group := "vip-group"
			pgName := ovs.GetSgPortGroupName(group)
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			require.NoError(t, indexer.Add(&kubeovnv1.SecurityGroup{
				Name: group, Status: kubeovnv1.SecurityGroupStatus{PortGroup: pgName},
			}))
			ctrl.sgsLister = kubeovnlister.NewSecurityGroupLister(indexer)
			require.NoError(t, client.CreateBareLogicalSwitch("vip-switch"))
			require.NoError(t, ctrl.createPortGroup(pgName, nil))
			v4As, v6As := ovs.GetSgV4AssociatedName(group), ovs.GetSgV6AssociatedName(group)
			require.NoError(t, ctrl.createAddressSet(v4As, nil))
			require.NoError(t, ctrl.createAddressSet(v6As, nil))
			require.NoError(t, ctrl.createLogicalSwitchPort("vip-switch", "vip-port", "10.16.0.10,fd00::10",
				"00:00:00:00:00:01", "pod", "namespace", false, group, "10.16.0.99,fd00::99", false, nil, util.DefaultVpc))
			for _, step := range []struct {
				name         string
				portSecurity bool
				vips         string
				v4, v6       []string
			}{
				{name: "existing disabled port", vips: "10.16.0.99,fd00::99", v4: []string{"10.16.0.10", "10.16.0.99"}, v6: []string{"fd00::10", "fd00::99"}},
				{name: "replace VIPs", vips: "10.16.0.100,fd00::100", v4: []string{"10.16.0.10", "10.16.0.100"}, v6: []string{"fd00::10", "fd00::100"}},
				{name: "enable port security", portSecurity: true, vips: "10.16.0.100,fd00::100", v4: []string{"10.16.0.10", "10.16.0.100"}, v6: []string{"fd00::10", "fd00::100"}},
				{name: "disable port security", vips: "10.16.0.100,fd00::100", v4: []string{"10.16.0.10", "10.16.0.100"}, v6: []string{"fd00::10", "fd00::100"}},
				{name: "remove VIPs", v4: []string{"10.16.0.10"}, v6: []string{"fd00::10"}},
			} {
				t.Run(step.name, func(t *testing.T) {
					if step.name != "existing disabled port" {
						require.NoError(t, ctrl.setLogicalSwitchPortSecurity(step.portSecurity, "vip-port", "00:00:00:00:00:01", "10.16.0.10,fd00::10", step.vips))
					}
					require.NoError(t, ctrl.syncSgLogicalPort(group))
					for name, want := range map[string][]string{v4As: step.v4, v6As: step.v6} {
						as, err := client.GetAddressSet(name, false)
						require.NoError(t, err)
						require.ElementsMatch(t, want, as.Addresses)
					}
					port, err := client.GetLogicalSwitchPort("vip-port", false)
					require.NoError(t, err)
					require.Equal(t, []string{"00:00:00:00:00:01 10.16.0.10 fd00::10"}, port.Addresses)
					require.Equal(t, step.portSecurity, len(port.PortSecurity) != 0)
					pg, err := client.GetPortGroup(pgName, false)
					require.NoError(t, err)
					require.Equal(t, []string{port.UUID}, pg.Ports)
				})
			}
		})
	}
}
