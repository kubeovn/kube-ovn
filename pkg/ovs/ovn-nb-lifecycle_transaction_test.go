package ovs

import (
	"context"
	"fmt"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type transactionRecordingClient struct {
	client.Client
	transactions [][]ovsdb.Operation
	competingOps []ovsdb.Operation
	abort        bool
}

func (c *transactionRecordingClient) Transact(ctx context.Context, ops ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	c.transactions = append(c.transactions, ops)
	if len(c.competingOps) != 0 {
		competingOps := c.competingOps
		c.competingOps = nil
		results, err := c.Client.Transact(ctx, competingOps...)
		if err != nil {
			return nil, err
		}
		if _, err := ovsdb.CheckOperationResults(results, competingOps); err != nil {
			return nil, err
		}
	}
	if c.abort {
		ops = append(append([]ovsdb.Operation(nil), ops...), ovsdb.Operation{
			Op:      ovsdb.OperationWait,
			Table:   ops[0].Table,
			Where:   ops[0].Where,
			Columns: []string{"_uuid"},
			Rows:    []ovsdb.Row{},
			Until:   "==",
			Timeout: new(0),
		})
	}
	return c.Client.Transact(ctx, ops...)
}

func newTransactionRecordingNbClient(t *testing.T) (*OVNNbClient, *transactionRecordingClient) {
	t.Helper()
	dbModel, err := ovnnb.FullDatabaseModel()
	require.NoError(t, err)
	_, socket := newOVSDBServer(t, t.Name(), dbModel, ovnnb.Schema())
	nbClient, err := newOvnNbClient(t, "unix:"+socket, 10)
	require.NoError(t, err)
	t.Cleanup(nbClient.Close)
	recorder := &transactionRecordingClient{Client: nbClient.Client}
	nbClient.Client = recorder
	return nbClient, recorder
}

func TestDeleteLogicalSwitchResourcesSingleTransaction(t *testing.T) {
	nbClient, recorder := newTransactionRecordingNbClient(t)
	name := "delete-subnet-resources"
	lrName := "delete-subnet-router"
	require.NoError(t, nbClient.CreateLogicalRouter(lrName))
	require.NoError(t, nbClient.CreateLogicalSwitch(name, lrName, "10.0.0.0/24", "10.0.0.1", "", true, false))
	acl, err := nbClient.newACL(name, ovnnb.ACLDirectionToLport, "1000", "ip", ovnnb.ACLActionDrop, util.NetpolACLTier)
	require.NoError(t, err)
	require.NoError(t, nbClient.CreateAcls(name, LogicalSwitchKey, acl))
	require.NoError(t, nbClient.CreateDHCPOptions(name, "10.0.0.0/24", ""))
	require.NoError(t, nbClient.CreateDHCPOptions(name, "fd00::/64", ""))
	require.NoError(t, nbClient.createDHCPEntry(name, "subnet-port", "10.0.0.0/24", ""))
	require.NoError(t, nbClient.CreateDHCPOptions("other-subnet", "10.1.0.0/24", ""))
	acls, err := nbClient.ListAcls("", map[string]string{aclParentKey: name})
	require.NoError(t, err)
	require.Len(t, acls, 1)
	dhcpOptions, err := nbClient.ListDHCPOptions(true, map[string]string{LogicalSwitchKey: name})
	require.NoError(t, err)
	require.Len(t, dhcpOptions, 3)

	recorder.transactions = nil
	require.NoError(t, nbClient.DeleteLogicalSwitchResources(name, kubeovnv1.ProtocolDual))
	require.Len(t, recorder.transactions, 1)
	var dhcpDeletes, switchDeletes int
	for _, op := range recorder.transactions[0] {
		if op.Op == ovsdb.OperationDelete {
			switch op.Table {
			case ovnnb.DHCPOptionsTable:
				dhcpDeletes++
			case ovnnb.LogicalSwitchTable:
				switchDeletes++
			}
		}
	}
	require.Equal(t, 3, dhcpDeletes)
	require.Equal(t, 1, switchDeletes)
	ls, err := nbClient.GetLogicalSwitch(name, true)
	require.NoError(t, err)
	require.Nil(t, ls)
	acls, err = nbClient.ListAcls("", map[string]string{aclParentKey: name})
	require.NoError(t, err)
	require.Empty(t, acls)
	dhcpOptions, err = nbClient.ListDHCPOptions(true, map[string]string{LogicalSwitchKey: name})
	require.NoError(t, err)
	require.Empty(t, dhcpOptions)
	lrp, err := nbClient.GetLogicalRouterPort(lrName+"-"+name, true)
	require.NoError(t, err)
	require.Nil(t, lrp)
	lr, err := nbClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)
	require.Empty(t, lr.Ports)
	otherDHCPOptions, err := nbClient.GetDHCPOptions("other-subnet", kubeovnv1.ProtocolIPv4, false)
	require.NoError(t, err)
	require.NotNil(t, otherDHCPOptions)
}

func TestDeleteSubnetResourcesAfterSwitchIsMissing(t *testing.T) {
	nbClient, recorder := newTransactionRecordingNbClient(t)
	lsName, lrName := "missing-switch-subnet", "missing-switch-router"
	require.NoError(t, nbClient.CreateLogicalRouter(lrName))
	require.NoError(t, nbClient.CreateLogicalSwitch(lsName, lrName, "10.0.0.0/24", "10.0.0.1", "", true, false))
	require.NoError(t, nbClient.CreateDHCPOptions(lsName, "10.0.0.0/24", ""))
	require.NoError(t, nbClient.DeleteLogicalSwitch(lsName))
	recorder.transactions = nil
	require.NoError(t, nbClient.DeleteLogicalSwitchResources(lsName, kubeovnv1.ProtocolDual, lrName+"-"+lsName))
	require.Len(t, recorder.transactions, 1)
	lrp, err := nbClient.GetLogicalRouterPort(lrName+"-"+lsName, true)
	require.NoError(t, err)
	require.Nil(t, lrp)
	options, err := nbClient.ListDHCPOptions(true, map[string]string{LogicalSwitchKey: lsName})
	require.NoError(t, err)
	require.Empty(t, options)
}

func TestUpdateSubnetDHCPOptionsSingleTransaction(t *testing.T) {
	nbClient, recorder := newTransactionRecordingNbClient(t)
	subnet := &kubeovnv1.Subnet{
		Name: "dual-stack-dhcp",
		Spec: kubeovnv1.SubnetSpec{
			EnableDHCP:    true,
			EnableIPv6RA:  true,
			IPv6RAConfigs: "address_mode=dhcpv6_stateful",
			Protocol:      kubeovnv1.ProtocolDual,
			CIDRBlock:     "10.0.0.0/24,fd00::/64",
			Gateway:       "10.0.0.1,fd00::1",
		},
	}
	lrName := "dhcp-router"
	portName := lrName + "-" + subnet.Name
	require.NoError(t, nbClient.CreateLogicalRouter(lrName))
	require.NoError(t, nbClient.CreateLogicalSwitch(subnet.Name, lrName, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, "", true, false))
	recorder.transactions = nil
	ids, err := nbClient.UpdateDHCPOptions(subnet, 1500, portName)
	require.NoError(t, err)
	require.Len(t, recorder.transactions, 1)
	require.NotEmpty(t, ids.DHCPv4OptionsUUID)
	require.NotEmpty(t, ids.DHCPv6OptionsUUID)
	v4, err := nbClient.GetDHCPOptions(subnet.Name, kubeovnv1.ProtocolIPv4, false)
	require.NoError(t, err)
	v6, err := nbClient.GetDHCPOptions(subnet.Name, kubeovnv1.ProtocolIPv6, false)
	require.NoError(t, err)
	lrp, err := nbClient.GetLogicalRouterPort(portName, false)
	require.NoError(t, err)
	require.Equal(t, "dhcpv6_stateful", lrp.Ipv6RaConfigs["address_mode"])

	subnet.Spec.DHCPv4Options = "lease_time=3600"
	subnet.Spec.DHCPv6Options = "dns_server={fd00::53}"
	subnet.Spec.IPv6RAConfigs = "address_mode=slaac"
	recorder.transactions = nil
	recorder.abort = true
	_, err = nbClient.UpdateDHCPOptions(subnet, 1400, portName)
	require.Error(t, err)
	require.Len(t, recorder.transactions, 1)
	unchangedV4, err := nbClient.GetDHCPOptions(subnet.Name, kubeovnv1.ProtocolIPv4, false)
	require.NoError(t, err)
	unchangedV6, err := nbClient.GetDHCPOptions(subnet.Name, kubeovnv1.ProtocolIPv6, false)
	require.NoError(t, err)
	require.Equal(t, v4.Options, unchangedV4.Options)
	require.Equal(t, v6.Options, unchangedV6.Options)
	unchangedLRP, err := nbClient.GetLogicalRouterPort(portName, false)
	require.NoError(t, err)
	require.Equal(t, lrp.Ipv6RaConfigs, unchangedLRP.Ipv6RaConfigs)

	recorder.abort = false
	recorder.transactions = nil
	updatedIDs, err := nbClient.UpdateDHCPOptions(subnet, 1400, portName)
	require.NoError(t, err)
	require.Len(t, recorder.transactions, 1)
	require.Equal(t, ids, updatedIDs)
	updatedV4, err := nbClient.GetDHCPOptions(subnet.Name, kubeovnv1.ProtocolIPv4, false)
	require.NoError(t, err)
	updatedV6, err := nbClient.GetDHCPOptions(subnet.Name, kubeovnv1.ProtocolIPv6, false)
	require.NoError(t, err)
	require.Equal(t, "3600", updatedV4.Options["lease_time"])
	require.Equal(t, "{fd00::53}", updatedV6.Options["dns_server"])
	require.Equal(t, v4.Options["server_mac"], updatedV4.Options["server_mac"])
	require.Equal(t, v6.Options["server_id"], updatedV6.Options["server_id"])
	updatedLRP, err := nbClient.GetLogicalRouterPort(portName, false)
	require.NoError(t, err)
	require.Equal(t, "slaac", updatedLRP.Ipv6RaConfigs["address_mode"])
}

func TestDisableSubnetDHCPAndRASingleTransaction(t *testing.T) {
	nbClient, recorder := newTransactionRecordingNbClient(t)
	subnet := &kubeovnv1.Subnet{
		Name: "disable-dhcp-subnet",
		Spec: kubeovnv1.SubnetSpec{
			EnableDHCP: true, EnableIPv6RA: true,
			Protocol:  kubeovnv1.ProtocolDual,
			CIDRBlock: "10.0.0.0/24,fd00::/64", Gateway: "10.0.0.1,fd00::1",
		},
	}
	lrName := "disable-dhcp-router"
	portName := lrName + "-" + subnet.Name
	require.NoError(t, nbClient.CreateLogicalRouter(lrName))
	require.NoError(t, nbClient.CreateLogicalSwitch(subnet.Name, lrName, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, "", true, false))
	_, err := nbClient.UpdateDHCPOptions(subnet, 1500, portName)
	require.NoError(t, err)
	require.NoError(t, nbClient.createDHCPEntry(subnet.Name, "per-port", "10.0.0.0/24", ""))
	subnet.Spec.EnableDHCP = false
	subnet.Spec.EnableIPv6RA = false
	recorder.transactions = nil
	ids, err := nbClient.UpdateDHCPOptions(subnet, 1500, portName)
	require.NoError(t, err)
	require.Len(t, recorder.transactions, 1)
	require.Empty(t, ids.DHCPv4OptionsUUID)
	require.Empty(t, ids.DHCPv6OptionsUUID)
	options, err := nbClient.ListDHCPOptions(true, map[string]string{LogicalSwitchKey: subnet.Name})
	require.NoError(t, err)
	require.Len(t, options, 1)
	require.Equal(t, "per-port", options[0].ExternalIDs[PortKey])
	lrp, err := nbClient.GetLogicalRouterPort(portName, false)
	require.NoError(t, err)
	require.Empty(t, lrp.Ipv6Prefix)
	require.Empty(t, lrp.Ipv6RaConfigs)
}

func TestVpcBFDPortSingleTransaction(t *testing.T) {
	nbClient, recorder := newTransactionRecordingNbClient(t)
	lrName, portName := "bfd-router", "bfd@bfd-router"
	require.NoError(t, nbClient.CreateLogicalRouter(lrName))
	recorder.transactions = nil
	require.NoError(t, nbClient.ReconcileBFDLogicalRouterPort(lrName, portName, []string{"169.254.0.1/32"}, []string{"chassis1", "chassis2"}, true))
	require.Len(t, recorder.transactions, 1)
	lrp, err := nbClient.GetLogicalRouterPort(portName, false)
	require.NoError(t, err)
	group, err := nbClient.GetHAChassisGroup(portName, false)
	require.NoError(t, err)
	require.Equal(t, &group.UUID, lrp.HaChassisGroup)
	require.Equal(t, "true", lrp.Options["bfd-only"])
	require.Len(t, group.HaChassis, 2)

	recorder.transactions = nil
	require.NoError(t, nbClient.ReconcileBFDLogicalRouterPort(lrName, portName, []string{"169.254.0.2/32"}, nil, true))
	require.Len(t, recorder.transactions, 1)
	updated, err := nbClient.GetLogicalRouterPort(portName, false)
	require.NoError(t, err)
	require.Equal(t, lrp.UUID, updated.UUID)
	require.Equal(t, lrp.MAC, updated.MAC)
	require.Equal(t, []string{"169.254.0.2/32"}, updated.Networks)
	group, err = nbClient.GetHAChassisGroup(portName, false)
	require.NoError(t, err)
	require.Empty(t, group.HaChassis)

	recorder.transactions = nil
	require.NoError(t, nbClient.ReconcileBFDLogicalRouterPort(lrName, portName, nil, nil, false))
	require.Len(t, recorder.transactions, 1)
	lrp, err = nbClient.GetLogicalRouterPort(portName, true)
	require.NoError(t, err)
	require.Nil(t, lrp)
	group, err = nbClient.GetHAChassisGroup(portName, true)
	require.NoError(t, err)
	require.Nil(t, group)
}

func TestLogicalRouterOptionsConcurrentCreate(t *testing.T) {
	nbClient, recorder := newTransactionRecordingNbClient(t)
	name := "concurrent-vpc-router"
	options := map[string]string{"dynamic_neigh_routers": "true"}
	var err error
	recorder.competingOps, err = nbClient.Create(&ovnnb.LogicalRouter{Name: name})
	require.NoError(t, err)
	require.ErrorContains(t, nbClient.CreateLogicalRouterWithOptions(name, options), "retry applying options")
	require.Len(t, recorder.transactions, 1)
	lr, err := nbClient.GetLogicalRouter(name, false)
	require.NoError(t, err)
	require.Empty(t, lr.Options)
	recorder.transactions = nil
	require.NoError(t, nbClient.CreateLogicalRouterWithOptions(name, options))
	require.Len(t, recorder.transactions, 1)
	lr, err = nbClient.GetLogicalRouter(name, false)
	require.NoError(t, err)
	require.Equal(t, options, lr.Options)
}

func TestLogicalSwitchRepairsMissingPatchPort(t *testing.T) {
	nbClient, recorder := newTransactionRecordingNbClient(t)
	lrName := "repair-router"
	require.NoError(t, nbClient.CreateLogicalRouter(lrName))
	for _, randomAllocateGW := range []bool{false, true} {
		for _, deleteSwitchPort := range []bool{false, true} {
			name := fmt.Sprintf("random=%t/deleteSwitchPort=%t", randomAllocateGW, deleteSwitchPort)
			t.Run(name, func(t *testing.T) {
				lsName := fmt.Sprintf("repair-subnet-%t-%t", randomAllocateGW, deleteSwitchPort)
				lspName, lrpName := lsName+"-"+lrName, lrName+"-"+lsName
				require.NoError(t, nbClient.CreateLogicalSwitch(lsName, lrName, "10.0.0.0/24", "10.0.0.1", "", true, randomAllocateGW))
				if deleteSwitchPort {
					require.NoError(t, nbClient.DeleteLogicalSwitchPort(lspName))
				} else {
					require.NoError(t, nbClient.DeleteLogicalRouterPort(lrpName))
				}
				recorder.transactions = nil
				require.NoError(t, nbClient.CreateLogicalSwitch(lsName, lrName, "10.0.0.0/24", "10.0.0.2", "", true, randomAllocateGW))
				require.Len(t, recorder.transactions, 1)
				ls, err := nbClient.GetLogicalSwitch(lsName, false)
				require.NoError(t, err)
				lsp, err := nbClient.GetLogicalSwitchPort(lspName, false)
				require.NoError(t, err)
				require.Contains(t, ls.Ports, lsp.UUID)
				lr, err := nbClient.GetLogicalRouter(lrName, false)
				require.NoError(t, err)
				lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
				require.NoError(t, err)
				require.Contains(t, lr.Ports, lrp.UUID)
				if randomAllocateGW && deleteSwitchPort {
					require.Equal(t, []string{"10.0.0.1/24"}, lrp.Networks)
				} else {
					require.Equal(t, []string{"10.0.0.2/24"}, lrp.Networks)
				}
			})
		}
	}
}

func TestLogicalSwitchPatchPortConcurrentCreate(t *testing.T) {
	nbClient, recorder := newTransactionRecordingNbClient(t)
	lrName := "concurrent-router"
	for _, randomAllocateGW := range []bool{false, true} {
		lsName := fmt.Sprintf("concurrent-subnet-%t", randomAllocateGW)
		require.NoError(t, nbClient.CreateLogicalRouter(lrName))
		var err error
		recorder.competingOps, err = nbClient.Create(&ovnnb.LogicalSwitch{Name: lsName})
		require.NoError(t, err)
		recorder.transactions = nil
		require.ErrorContains(t, nbClient.CreateLogicalSwitch(lsName, lrName, "10.0.0.0/24", "10.0.0.1", "", true, randomAllocateGW), "retry reconciling patch ports")
		require.Len(t, recorder.transactions, 1)
		recorder.transactions = nil
		require.NoError(t, nbClient.CreateLogicalSwitch(lsName, lrName, "10.0.0.0/24", "10.0.0.1", "", true, randomAllocateGW))
		require.Len(t, recorder.transactions, 1)
		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		lsp, err := nbClient.GetLogicalSwitchPort(lsName+"-"+lrName, false)
		require.NoError(t, err)
		require.Contains(t, ls.Ports, lsp.UUID)
		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		lrp, err := nbClient.GetLogicalRouterPort(lrName+"-"+lsName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
		require.Equal(t, []string{"10.0.0.1/24"}, lrp.Networks)

		recorder.transactions = nil
		require.NoError(t, nbClient.CreateLogicalSwitch(lsName, lrName, "10.0.0.0/24", "10.0.0.2", "", true, randomAllocateGW))
		lrp, err = nbClient.GetLogicalRouterPort(lrName+"-"+lsName, false)
		require.NoError(t, err)
		if randomAllocateGW {
			require.Empty(t, recorder.transactions)
			require.Equal(t, []string{"10.0.0.1/24"}, lrp.Networks)
		} else {
			require.Len(t, recorder.transactions, 1)
			require.Equal(t, []string{"10.0.0.2/24"}, lrp.Networks)
		}
	}
}
