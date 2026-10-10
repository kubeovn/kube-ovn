package controller

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestDeleteDefaultSecurityGroupWithAttachedPorts(t *testing.T) {
	for _, otherGroup := range []string{"", "other-group"} {
		t.Run("remaining="+otherGroup, func(t *testing.T) {
			client := newInMemoryOVNNbClient(t)
			controller := &Controller{OVNNbTables: client}
			require.NoError(t, client.CreateBareLogicalSwitch("sg-switch"))
			require.NoError(t, client.CreateBareLogicalSwitchPort("sg-switch", "sg-port", "10.0.0.2", "00:00:00:00:00:02"))
			port, err := client.GetLogicalSwitchPort("sg-port", false)
			require.NoError(t, err)
			port.ExternalIDs = map[string]string{
				"vendor": util.CniTypeName, "preserved": "value",
				"associated_sg_" + util.DefaultSecurityGroupName: "true",
				sgsKey: util.DefaultSecurityGroupName,
			}
			if otherGroup != "" {
				port.ExternalIDs[sgsKey] += "/" + otherGroup
				port.ExternalIDs["associated_sg_"+otherGroup] = "true"
			}
			require.NoError(t, client.Table(&ovnnb.LogicalSwitchPort{}).Update(t.Context(), "seed-sg", port, port, &port.ExternalIDs))
			pgName := ovs.GetSgPortGroupName(util.DefaultSecurityGroupName)
			require.NoError(t, client.CreatePortGroup(pgName, nil))

			require.NoError(t, controller.deleteSecurityGroup(util.DefaultSecurityGroupName))

			port, err = client.GetLogicalSwitchPort("sg-port", false)
			require.NoError(t, err)
			require.Equal(t, "false", port.ExternalIDs["associated_sg_"+util.DefaultSecurityGroupName])
			require.Equal(t, "value", port.ExternalIDs["preserved"])
			require.Equal(t, otherGroup, port.ExternalIDs[sgsKey])
			if otherGroup == "" {
				require.NotContains(t, port.ExternalIDs, sgsKey)
			} else {
				require.Equal(t, "true", port.ExternalIDs["associated_sg_"+otherGroup])
			}
			pg, err := client.GetPortGroup(pgName, true)
			require.NoError(t, err)
			require.Nil(t, pg)
		})
	}
}
