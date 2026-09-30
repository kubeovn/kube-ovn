package controller

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

func TestControllerProviderSubnetDHCP(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, cidr, gateway, v4Options, v6Options string
	}{
		{"IPv4", kubeovnv1.ProtocolIPv4, "10.244.0.0/16", "10.244.0.1", "dns_server=192.0.2.53", ""},
		{"IPv6", kubeovnv1.ProtocolIPv6, "fc00::af4:0/112", "fc00::af4:1", "", "dns_server=2001:db8::53"},
		{"Dual", kubeovnv1.ProtocolDual, "10.244.0.0/16,fc00::af4:0/112", "10.244.0.1,fc00::af4:1", "dns_server=192.0.2.53", "dns_server=2001:db8::53"},
		{"Dual-v4-only", kubeovnv1.ProtocolDual, "10.244.0.0/16,fc00::af4:0/112", "10.244.0.1,fc00::af4:1", "dns_server=192.0.2.53", ""},
		{"Dual-v6-only", kubeovnv1.ProtocolDual, "10.244.0.0/16,fc00::af4:0/112", "10.244.0.1,fc00::af4:1", "", "dns_server=2001:db8::53"},
	} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/initially-enabled-%t", tc.name, enabled), func(t *testing.T) {
				nbClient := newInMemoryOVNNbClient(t)
				controller := &Controller{OVNNbTables: nbClient}
				subnet := &kubeovnv1.Subnet{
					Name: "dhcp-subnet",
					Spec: kubeovnv1.SubnetSpec{
						EnableDHCP: enabled, Protocol: tc.protocol, CIDRBlock: tc.cidr, Gateway: tc.gateway,
					},
				}
				require.NoError(t, nbClient.CreateBareLogicalSwitch(subnet.Name))
				subnetDHCP, err := controller.updateSubnetDHCPOptionsTable(subnet, 1500)
				require.NoError(t, err)
				const portName = "dhcp-port"
				require.NoError(t, nbClient.CreateBareLogicalSwitchPort(subnet.Name, portName, "", ""))
				_, hasPerPort, err := controller.updatePortDHCPOptionsTable(
					subnet.Name, portName, subnetDHCP, tc.cidr, tc.gateway, tc.v4Options, tc.v6Options, 1500,
				)
				require.NoError(t, err)
				require.True(t, hasPerPort)
				portBefore, err := nbClient.GetLogicalSwitchPort(portName, false)
				require.NoError(t, err)
				rowsBefore, err := nbClient.ListDHCPOptions(true, map[string]string{ovs.PortKey: portName})
				require.NoError(t, err)
				require.NotEmpty(t, rowsBefore)
				subnet.Spec.EnableDHCP = false
				for range 2 {
					uuids, err := controller.updateSubnetDHCPOptionsTable(subnet, 1500)
					require.NoError(t, err)
					require.Equal(t, &ovs.DHCPOptionsUUIDs{}, uuids)
					rows, err := nbClient.ListDHCPOptions(true, map[string]string{ovs.LogicalSwitchKey: subnet.Name})
					require.NoError(t, err)
					require.ElementsMatch(t, rowsBefore, rows, "per-port options and server identity must survive subnet reconciliation")
					port, err := nbClient.GetLogicalSwitchPort(portName, false)
					require.NoError(t, err)
					if tc.v4Options != "" {
						require.NotNil(t, port.Dhcpv4Options)
						require.Equal(t, portBefore.Dhcpv4Options, port.Dhcpv4Options)
					}
					if tc.v6Options != "" {
						require.NotNil(t, port.Dhcpv6Options)
						require.Equal(t, portBefore.Dhcpv6Options, port.Dhcpv6Options)
					}
				}
				require.NoError(t, controller.deleteLogicalSwitchPort(portName))
				rows, err := nbClient.ListDHCPOptions(true, map[string]string{ovs.LogicalSwitchKey: subnet.Name})
				require.NoError(t, err)
				require.Empty(t, rows, "port deletion must still remove its DHCP rows")
			})
		}
	}
}
