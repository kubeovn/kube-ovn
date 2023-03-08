package ovs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func createLogicalSwitchPort(c *ovnClient, lsp *ovnnb.LogicalSwitchPort) error {
	if lsp == nil {
		return fmt.Errorf("logical_switch_port is nil")
	}

	op, err := c.Create(lsp)
	if err != nil {
		return fmt.Errorf("generate operations for creating logical switch port %s: %v", lsp.Name, err)
	}

	return c.Transact("lsp-create", op)
}

func (suite *OvnClientTestSuite) testCreateLogicalSwitchPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-create-port-ls"
	ips := "10.244.0.37,fc00::af4:25"
	mac := "00:00:00:AB:B4:65"
	vips := "10.244.0.110,10.244.0.112"
	podName := "test-vm-pod"
	podNamespace := "test-ns"
	dhcpOptions := &DHCPOptionsUUIDs{
		DHCPv4OptionsUUID: "73459f83-6189-4c57-837c-4102fa293332",
		DHCPv6OptionsUUID: "d0201b01-1ef4-4eaf-9d96-8fe845e76c93",
	}

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("create logical switch port", func(t *testing.T) {
		lspName := "test-create-port-lsp"
		sgs := "sg,sg1"
		vpcName := "test-vpc"

		err = ovnClient.CreateLogicalSwitchPort(lsName, lspName, ips, mac, podName, podNamespace, true, sgs, vips, true, dhcpOptions, vpcName)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25"}, lsp.Addresses)
		require.ElementsMatch(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25 10.244.0.110 10.244.0.112"}, lsp.PortSecurity)
		require.Equal(t, map[string]string{
			sgsKey:              strings.ReplaceAll(sgs, ",", "/"),
			"associated_sg_sg":  "true",
			"associated_sg_sg1": "true",
			"associated_sg_" + util.DefaultSecurityGroupName: "false",
			"vips":        vips,
			"attach-vips": "true",
			"pod":         fmt.Sprintf("%s/%s", podNamespace, podName),
			"ls":          lsName,
			"vendor":      util.CniTypeName,
		}, lsp.ExternalIDs)
		require.Equal(t, dhcpOptions.DHCPv4OptionsUUID, *lsp.Dhcpv4Options)
		require.Equal(t, dhcpOptions.DHCPv6OptionsUUID, *lsp.Dhcpv6Options)
	})

	t.Run("create logical switch port without vips", func(t *testing.T) {
		lspName := "test-create-port-lsp-no-vip"
		sgs := "sg,sg1"
		vpcName := "test-vpc"

		err = ovnClient.CreateLogicalSwitchPort(lsName, lspName, ips, mac, podName, podNamespace, true, sgs, "", true, dhcpOptions, vpcName)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25"}, lsp.Addresses)
		require.ElementsMatch(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25"}, lsp.PortSecurity)
		require.Equal(t, map[string]string{
			sgsKey:              strings.ReplaceAll(sgs, ",", "/"),
			"associated_sg_sg":  "true",
			"associated_sg_sg1": "true",
			"associated_sg_" + util.DefaultSecurityGroupName: "false",
			"pod":    fmt.Sprintf("%s/%s", podNamespace, podName),
			"ls":     lsName,
			"vendor": util.CniTypeName,
		}, lsp.ExternalIDs)
		require.Equal(t, dhcpOptions.DHCPv4OptionsUUID, *lsp.Dhcpv4Options)
		require.Equal(t, dhcpOptions.DHCPv6OptionsUUID, *lsp.Dhcpv6Options)
	})

	t.Run("create logical switch port with default-securitygroup", func(t *testing.T) {
		lspName := "test-create-port-lsp-default-securitygroup"
		sgs := "sg,sg1,default-securitygroup"
		vpcName := "test-vpc"

		err = ovnClient.CreateLogicalSwitchPort(lsName, lspName, ips, mac, podName, podNamespace, true, sgs, vips, true, dhcpOptions, vpcName)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25"}, lsp.Addresses)
		require.ElementsMatch(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25 10.244.0.110 10.244.0.112"}, lsp.PortSecurity)
		require.Equal(t, map[string]string{
			sgsKey:              strings.ReplaceAll(sgs, ",", "/"),
			"associated_sg_sg":  "true",
			"associated_sg_sg1": "true",
			"associated_sg_" + util.DefaultSecurityGroupName: "true",
			"pod":         fmt.Sprintf("%s/%s", podNamespace, podName),
			"ls":          lsName,
			"vendor":      util.CniTypeName,
			"vips":        vips,
			"attach-vips": "true",
		}, lsp.ExternalIDs)
		require.Equal(t, dhcpOptions.DHCPv4OptionsUUID, *lsp.Dhcpv4Options)
		require.Equal(t, dhcpOptions.DHCPv6OptionsUUID, *lsp.Dhcpv6Options)
	})

	t.Run("create logical switch port with default vpc", func(t *testing.T) {
		lspName := "test-create-port-lsp-default-vpc"
		sgs := "sg,sg1"
		vpcName := "ovn-cluster"

		err = ovnClient.CreateLogicalSwitchPort(lsName, lspName, ips, mac, podName, podNamespace, true, sgs, vips, true, dhcpOptions, vpcName)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25"}, lsp.Addresses)
		require.ElementsMatch(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25 10.244.0.110 10.244.0.112"}, lsp.PortSecurity)
		require.Equal(t, map[string]string{
			sgsKey:              strings.ReplaceAll(sgs, ",", "/"),
			"associated_sg_sg":  "true",
			"associated_sg_sg1": "true",
			"pod":               fmt.Sprintf("%s/%s", podNamespace, podName),
			"ls":                lsName,
			"vendor":            util.CniTypeName,
			"vips":              vips,
			"attach-vips":       "true",
		}, lsp.ExternalIDs)
		require.Equal(t, dhcpOptions.DHCPv4OptionsUUID, *lsp.Dhcpv4Options)
		require.Equal(t, dhcpOptions.DHCPv6OptionsUUID, *lsp.Dhcpv6Options)
	})

	t.Run("create logical switch port with portSecurity=false", func(t *testing.T) {
		lspName := "test-create-port-lsp-no-portSecurity"
		sgs := "sg,sg1"
		vpcName := "test-vpc"

		err = ovnClient.CreateLogicalSwitchPort(lsName, lspName, ips, mac, podName, podNamespace, false, sgs, vips, true, dhcpOptions, vpcName)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25"}, lsp.Addresses)
		require.Equal(t, map[string]string{
			"associated_sg_" + util.DefaultSecurityGroupName: "false",
			"pod":         fmt.Sprintf("%s/%s", podNamespace, podName),
			"ls":          lsName,
			"vendor":      util.CniTypeName,
			"vips":        vips,
			"attach-vips": "true",
		}, lsp.ExternalIDs)
		require.Equal(t, dhcpOptions.DHCPv4OptionsUUID, *lsp.Dhcpv4Options)
		require.Equal(t, dhcpOptions.DHCPv6OptionsUUID, *lsp.Dhcpv6Options)
	})

	t.Run("create logical switch port without dhcp options", func(t *testing.T) {
		lspName := "test-create-port-lsp-no-dhcp-options"
		sgs := "sg,sg1"
		vpcName := "test-vpc"

		err = ovnClient.CreateLogicalSwitchPort(lsName, lspName, ips, mac, podName, podNamespace, true, sgs, vips, true, nil, vpcName)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25"}, lsp.Addresses)
		require.ElementsMatch(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25 10.244.0.110 10.244.0.112"}, lsp.PortSecurity)
		require.Equal(t, map[string]string{
			sgsKey:              strings.ReplaceAll(sgs, ",", "/"),
			"associated_sg_sg":  "true",
			"associated_sg_sg1": "true",
			"associated_sg_" + util.DefaultSecurityGroupName: "false",
			"vips":        vips,
			"attach-vips": "true",
			"pod":         fmt.Sprintf("%s/%s", podNamespace, podName),
			"ls":          lsName,
			"vendor":      util.CniTypeName,
		}, lsp.ExternalIDs)
		require.Empty(t, lsp.Dhcpv4Options)
		require.Empty(t, lsp.Dhcpv6Options)
	})
}

func (suite *OvnClientTestSuite) testCreateLocalnetLogicalSwitchPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lspName := "test-create-localnet-port-lsp"
	lsName := "test-create-localnet-port-port-ls"
	provider := "external"

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("create localnet logical switch port with vlan id", func(t *testing.T) {
		err = ovnClient.CreateLocalnetLogicalSwitchPort(lsName, lspName, provider, 200)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Equal(t, lspName, lsp.Name)
		require.Equal(t, "localnet", lsp.Type)
		require.ElementsMatch(t, []string{"unknown"}, lsp.Addresses)
		require.Equal(t, map[string]string{
			"network_name": provider,
		}, lsp.Options)

		require.Equal(t, 200, *lsp.Tag)
	})

	t.Run("create localnet logical switch port without vlan id", func(t *testing.T) {
		lspName := "test-create-localnet-port-lsp-no-vlan-id"
		err = ovnClient.CreateLocalnetLogicalSwitchPort(lsName, lspName, provider, 0)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Equal(t, lspName, lsp.Name)
		require.Equal(t, "localnet", lsp.Type)
		require.ElementsMatch(t, []string{"unknown"}, lsp.Addresses)
		require.Equal(t, map[string]string{
			"network_name": provider,
		}, lsp.Options)
		require.Empty(t, lsp.Tag)
	})

	t.Run("should no err when create logical switch port repeatedly", func(t *testing.T) {
		err = ovnClient.CreateLocalnetLogicalSwitchPort(lsName, lspName, "external", 0)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testCreateVirtualLogicalSwitchPorts() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-create-virtual-port-ls"
	vips := []string{"192.168.33.10", "192.168.33.12"}

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)
	t.Run("create virtual logical switch port", func(t *testing.T) {
		err = ovnClient.CreateVirtualLogicalSwitchPorts(lsName, vips...)
		require.NoError(t, err)
		for _, ip := range vips {
			lspName := fmt.Sprintf("%s-vip-%s", lsName, ip)

			lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.Equal(t, lspName, lsp.Name)
			require.Equal(t, "virtual", lsp.Type)
			require.Equal(t, map[string]string{
				"virtual-ip": ip,
			}, lsp.Options)
		}
	})

	t.Run("should no err when create logical switch port repeatedly", func(t *testing.T) {
		err = ovnClient.CreateVirtualLogicalSwitchPorts(lsName, vips...)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testCreateBareLogicalSwitchPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-create-bare-port-ls"
	lspName := "test-create-bare-port-lsp"

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = ovnClient.CreateBareLogicalSwitchPort(lsName, lspName, "100.64.0.4,fd00:100:64::4", "00:00:00:C9:4E:EE")
	require.NoError(t, err)

	lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"00:00:00:C9:4E:EE 100.64.0.4 fd00:100:64::4"}, lsp.Addresses)

	ls, err := ovnClient.GetLogicalSwitch(lsName, false)
	require.NoError(t, err)

	require.Contains(t, ls.Ports, lsp.UUID)
}

func (suite *OvnClientTestSuite) testSetLogicalSwitchPortVirtualParents() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-update-port-virt-parents-ls"
	ips := []string{"192.168.211.31", "192.168.211.32"}

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = ovnClient.CreateVirtualLogicalSwitchPorts(lsName, ips...)
	require.NoError(t, err)

	t.Run("set virtual-parents option", func(t *testing.T) {
		err = ovnClient.SetLogicalSwitchPortVirtualParents(lsName, "virt-parents-ls-1,virt-parents-ls-2", ips...)
		require.NoError(t, err)
		for _, ip := range ips {
			lspName := fmt.Sprintf("%s-vip-%s", lsName, ip)
			lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.Equal(t, "virt-parents-ls-1,virt-parents-ls-2", lsp.Options["virtual-parents"])
		}
	})

	t.Run("clear virtual-parents option", func(t *testing.T) {
		err = ovnClient.SetLogicalSwitchPortVirtualParents(lsName, "", ips...)
		require.NoError(t, err)
		for _, ip := range ips {
			lspName := fmt.Sprintf("%s-vip-%s", lsName, ip)
			lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.Empty(t, lsp.Options["virtual-parents"])
		}
	})
}

func (suite *OvnClientTestSuite) testSetLogicalSwitchPortSecurity() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-update-port-security-ls"
	lspName := "test-update-port-security-lsp"

	lsp := &ovnnb.LogicalSwitchPort{
		UUID: ovsclient.NamedUUID(),
		Name: lspName,
		ExternalIDs: map[string]string{
			"vendor":         util.CniTypeName,
			logicalSwitchKey: lsName,
		},
	}

	err := createLogicalSwitchPort(ovnClient, lsp)
	require.NoError(t, err)

	t.Run("update port_security and external_ids", func(t *testing.T) {
		err = ovnClient.SetLogicalSwitchPortSecurity(true, lspName, "00:00:00:AB:B4:65", "10.244.0.37,fc00::af4:25", "10.244.100.10,10.244.100.11")
		require.NoError(t, err)

		lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25 10.244.100.10 10.244.100.11"}, lsp.PortSecurity)
		require.Equal(t, map[string]string{
			"vendor":         util.CniTypeName,
			logicalSwitchKey: lsName,
			"vips":           "10.244.100.10,10.244.100.11",
			"attach-vips":    "true",
		}, lsp.ExternalIDs)
	})

	t.Run("clear port_security and external_ids", func(t *testing.T) {
		err = ovnClient.SetLogicalSwitchPortSecurity(false, lspName, "00:00:00:AB:B4:65", "10.244.0.37,fc00::af4:25", "")
		require.NoError(t, err)

		lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Empty(t, lsp.PortSecurity)
		require.Equal(t, map[string]string{
			"vendor":         util.CniTypeName,
			logicalSwitchKey: lsName,
		}, lsp.ExternalIDs)
	})

	t.Run("update port_security and external_ids when lsp.ExternalIDs is nil and vips is not nil", func(t *testing.T) {
		lspName := "test-update-port-security-lsp-nil-eid"

		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.NamedUUID(),
			Name: lspName,
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		err = ovnClient.SetLogicalSwitchPortSecurity(true, lspName, "00:00:00:AB:B4:65", "10.244.0.37,fc00::af4:25", "10.244.100.10,10.244.100.11")
		require.NoError(t, err)

		lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25 10.244.100.10 10.244.100.11"}, lsp.PortSecurity)
		require.Equal(t, map[string]string{
			"vips":        "10.244.100.10,10.244.100.11",
			"attach-vips": "true",
		}, lsp.ExternalIDs)
	})
}

func (suite *OvnClientTestSuite) testSetSetLogicalSwitchPortExternalIds() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lspName := "test-set-port-ext-id-lsp"

	lsp := &ovnnb.LogicalSwitchPort{
		UUID: ovsclient.NamedUUID(),
		Name: lspName,
		ExternalIDs: map[string]string{
			"vendor": util.CniTypeName,
		},
	}

	err := createLogicalSwitchPort(ovnClient, lsp)
	require.NoError(t, err)

	err = ovnClient.SetLogicalSwitchPortExternalIds(lspName, map[string]string{"k1": "v1"})
	require.NoError(t, err)

	lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"k1":     "v1",
		"vendor": util.CniTypeName,
	}, lsp.ExternalIDs)

	err = ovnClient.SetLogicalSwitchPortExternalIds(lspName, map[string]string{"k1": "v2"})
	require.NoError(t, err)

	lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"k1":     "v2",
		"vendor": util.CniTypeName,
	}, lsp.ExternalIDs)
}

func (suite *OvnClientTestSuite) testSetLogicalSwitchPortSecurityGroup() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lspNamePrefix := "test-set-sg-lsp"

	addOpExpect := func(lsp *ovnnb.LogicalSwitchPort, sgs []string) {
		for _, sg := range sgs {
			require.Equalf(t, "true", lsp.ExternalIDs[associatedSgKeyPrefix+sg], "%s should exist", sg)
		}

		sgList := strings.Split(lsp.ExternalIDs[sgsKey], "/")
		require.ElementsMatch(t, sgs, sgList)
	}

	removeOpExpect := func(lsp *ovnnb.LogicalSwitchPort, sgs []string) {
		for _, sg := range sgs {
			require.Equalf(t, "false", lsp.ExternalIDs[associatedSgKeyPrefix+sg], "%s should't exist", sg)
		}

		sgList := strings.Split(lsp.ExternalIDs[sgsKey], "/")
		require.NotSubset(t, sgList, sgs)
	}

	t.Run("add operation", func(t *testing.T) {
		t.Parallel()

		lspNamePrefix := lspNamePrefix + "add"
		op := "add"

		t.Run("new sgs is completely different old sgs", func(t *testing.T) {
			t.Parallel()

			lspName := lspNamePrefix + "-complete"
			lsp := &ovnnb.LogicalSwitchPort{
				Name: lspName,
				ExternalIDs: map[string]string{
					associatedSgKeyPrefix + "sg1": "true",
					sgsKey:                        "sg1",
				},
			}

			err := createLogicalSwitchPort(ovnClient, lsp)
			require.NoError(t, err)

			diffSgs, err := ovnClient.SetLogicalSwitchPortSecurityGroup(lsp, op, "sg2", "sg3")
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"sg2", "sg3"}, diffSgs)

			lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			addOpExpect(lsp, []string{"sg2", "sg1", "sg3"})
		})

		t.Run("old sg is subset of new sgs", func(t *testing.T) {
			t.Parallel()

			lspName := lspNamePrefix + "-old-subset"
			lsp := &ovnnb.LogicalSwitchPort{
				Name: lspName,
				ExternalIDs: map[string]string{
					associatedSgKeyPrefix + "sg1": "true",
					associatedSgKeyPrefix + "sg2": "true",
					sgsKey:                        "sg1/sg2",
				},
			}

			err := createLogicalSwitchPort(ovnClient, lsp)
			require.NoError(t, err)

			diffSgs, err := ovnClient.SetLogicalSwitchPortSecurityGroup(lsp, op, "sg2", "sg3", "sg4", "sg1")
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"sg4", "sg3"}, diffSgs)

			lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			addOpExpect(lsp, []string{"sg2", "sg1", "sg3", "sg4"})
		})

		t.Run("new sg is subset of old sgs", func(t *testing.T) {
			t.Parallel()

			lspName := lspNamePrefix + "-new-subset"
			lsp := &ovnnb.LogicalSwitchPort{
				Name: lspName,
				ExternalIDs: map[string]string{
					associatedSgKeyPrefix + "sg1": "true",
					associatedSgKeyPrefix + "sg2": "true",
					associatedSgKeyPrefix + "sg3": "true",
					sgsKey:                        "sg1/sg2/sg3",
				},
			}

			err := createLogicalSwitchPort(ovnClient, lsp)
			require.NoError(t, err)

			diffSgs, err := ovnClient.SetLogicalSwitchPortSecurityGroup(lsp, op, "sg2", "sg1")
			require.NoError(t, err)
			require.Empty(t, diffSgs)

			lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			addOpExpect(lsp, []string{"sg2", "sg1", "sg3"})
		})

		t.Run("new sgs is partially different old sgs", func(t *testing.T) {
			t.Parallel()

			lspName := lspNamePrefix + "-partial"
			lsp := &ovnnb.LogicalSwitchPort{
				Name: lspName,
				ExternalIDs: map[string]string{
					associatedSgKeyPrefix + "sg1": "true",
					associatedSgKeyPrefix + "sg2": "true",
					associatedSgKeyPrefix + "sg3": "true",
					sgsKey:                        "sg1/sg2/sg3",
				},
			}

			err := createLogicalSwitchPort(ovnClient, lsp)
			require.NoError(t, err)

			diffSgs, err := ovnClient.SetLogicalSwitchPortSecurityGroup(lsp, op, "sg2", "sg3", "sg4")
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"sg4"}, diffSgs)

			lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			addOpExpect(lsp, []string{"sg2", "sg1", "sg3", "sg4"})
		})

		t.Run("new sgs is empty", func(t *testing.T) {
			t.Parallel()

			lspName := lspNamePrefix + "-new-empty"
			lsp := &ovnnb.LogicalSwitchPort{
				Name: lspName,
				ExternalIDs: map[string]string{
					associatedSgKeyPrefix + "sg1": "true",
					associatedSgKeyPrefix + "sg2": "true",
					associatedSgKeyPrefix + "sg3": "true",
					sgsKey:                        "sg1/sg2/sg3",
				},
			}

			err := createLogicalSwitchPort(ovnClient, lsp)
			require.NoError(t, err)

			diffSgs, err := ovnClient.SetLogicalSwitchPortSecurityGroup(lsp, op)
			require.NoError(t, err)
			require.Empty(t, diffSgs)

			lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			addOpExpect(lsp, []string{"sg2", "sg1", "sg3"})
		})

		t.Run("old sgs is empty", func(t *testing.T) {
			t.Parallel()

			lspName := lspNamePrefix + "-old-empty"
			lsp := &ovnnb.LogicalSwitchPort{
				Name:        lspName,
				ExternalIDs: map[string]string{},
			}

			err := createLogicalSwitchPort(ovnClient, lsp)
			require.NoError(t, err)

			diffSgs, err := ovnClient.SetLogicalSwitchPortSecurityGroup(lsp, op, "sg2", "sg1")
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"sg1", "sg2"}, diffSgs)

			lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			addOpExpect(lsp, []string{"sg2", "sg1"})
		})
	})

	t.Run("remove operation", func(t *testing.T) {
		t.Parallel()

		lspNamePrefix := lspNamePrefix + "remove"
		op := "remove"

		t.Run("new sgs is completely different old sgs", func(t *testing.T) {
			t.Parallel()

			lspName := lspNamePrefix + "-complete"
			lsp := &ovnnb.LogicalSwitchPort{
				Name: lspName,
				ExternalIDs: map[string]string{
					associatedSgKeyPrefix + "sg1": "true",
					sgsKey:                        "sg1",
				},
			}

			err := createLogicalSwitchPort(ovnClient, lsp)
			require.NoError(t, err)

			diffSgs, err := ovnClient.SetLogicalSwitchPortSecurityGroup(lsp, op, "sg2", "sg3")
			require.NoError(t, err)
			require.Empty(t, diffSgs)

			lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			addOpExpect(lsp, []string{"sg1"})
		})

		t.Run("old sg is subset of new sgs", func(t *testing.T) {
			t.Parallel()

			lspName := lspNamePrefix + "-old-subset"
			lsp := &ovnnb.LogicalSwitchPort{
				Name: lspName,
				ExternalIDs: map[string]string{
					associatedSgKeyPrefix + "sg1": "true",
					associatedSgKeyPrefix + "sg2": "true",
					sgsKey:                        "sg1/sg2",
				},
			}

			err := createLogicalSwitchPort(ovnClient, lsp)
			require.NoError(t, err)

			diffSgs, err := ovnClient.SetLogicalSwitchPortSecurityGroup(lsp, op, "sg2", "sg3", "sg4", "sg1")
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"sg1", "sg2"}, diffSgs)

			lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			removeOpExpect(lsp, []string{"sg2", "sg1"})
		})

		t.Run("new sg is subset of old sgs", func(t *testing.T) {
			t.Parallel()

			lspName := lspNamePrefix + "-new-subset"
			lsp := &ovnnb.LogicalSwitchPort{
				Name: lspName,
				ExternalIDs: map[string]string{
					associatedSgKeyPrefix + "sg1": "true",
					associatedSgKeyPrefix + "sg2": "true",
					associatedSgKeyPrefix + "sg3": "true",
					sgsKey:                        "sg1/sg2/sg3",
				},
			}

			err := createLogicalSwitchPort(ovnClient, lsp)
			require.NoError(t, err)

			diffSgs, err := ovnClient.SetLogicalSwitchPortSecurityGroup(lsp, op, "sg2", "sg1")
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"sg2", "sg1"}, diffSgs)

			lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			addOpExpect(lsp, []string{"sg3"})
			removeOpExpect(lsp, []string{"sg2", "sg1"})
		})

		t.Run("new sgs is partially different old sgs", func(t *testing.T) {
			t.Parallel()

			lspName := lspNamePrefix + "-partial"
			lsp := &ovnnb.LogicalSwitchPort{
				Name: lspName,
				ExternalIDs: map[string]string{
					associatedSgKeyPrefix + "sg1": "true",
					associatedSgKeyPrefix + "sg2": "true",
					associatedSgKeyPrefix + "sg3": "true",
					sgsKey:                        "sg1/sg2/sg3",
				},
			}

			err := createLogicalSwitchPort(ovnClient, lsp)
			require.NoError(t, err)

			diffSgs, err := ovnClient.SetLogicalSwitchPortSecurityGroup(lsp, op, "sg2", "sg3", "sg4")
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"sg3", "sg2"}, diffSgs)

			lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			addOpExpect(lsp, []string{"sg1"})
			removeOpExpect(lsp, []string{"sg2", "sg3"})
		})

		t.Run("old sgs is empty", func(t *testing.T) {
			t.Parallel()

			lspName := lspNamePrefix + "-old-empty"
			lsp := &ovnnb.LogicalSwitchPort{
				Name:        lspName,
				ExternalIDs: map[string]string{},
			}

			err := createLogicalSwitchPort(ovnClient, lsp)
			require.NoError(t, err)

			diffSgs, err := ovnClient.SetLogicalSwitchPortSecurityGroup(lsp, op, "sg2", "sg1")
			require.NoError(t, err)
			require.Empty(t, diffSgs)
		})
	})
}

func (suite *OvnClientTestSuite) testSetLogicalSwitchPortsSecurityGroup() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lspNamePrefix := "test-set-sgs-lsp"

	for i := 0; i < 3; i++ {
		lspName := fmt.Sprintf("%s-%d", lspNamePrefix, i)
		lsp := &ovnnb.LogicalSwitchPort{
			Name: lspName,
			ExternalIDs: map[string]string{
				"vendor":                      util.CniTypeName,
				associatedSgKeyPrefix + "sg1": "false",
				associatedSgKeyPrefix + "sg2": "false",
			},
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)
	}

	t.Run("add sg to lsp", func(t *testing.T) {
		err := ovnClient.SetLogicalSwitchPortsSecurityGroup("sg2", "add")
		require.NoError(t, err)

		for i := 0; i < 3; i++ {
			lspName := fmt.Sprintf("%s-%d", lspNamePrefix, i)
			lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.Equal(t, "false", lsp.ExternalIDs[associatedSgKeyPrefix+"sg1"])
			require.Equal(t, "true", lsp.ExternalIDs[associatedSgKeyPrefix+"sg2"])

			sgList := strings.Split(lsp.ExternalIDs[sgsKey], "/")
			require.ElementsMatch(t, []string{"sg2"}, sgList)
		}
	})

	t.Run("remove sg from lsp", func(t *testing.T) {
		err := ovnClient.SetLogicalSwitchPortsSecurityGroup("sg2", "remove")
		require.NoError(t, err)

		for i := 0; i < 3; i++ {
			lspName := fmt.Sprintf("%s-%d", lspNamePrefix, i)
			lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.Equal(t, "false", lsp.ExternalIDs[associatedSgKeyPrefix+"sg1"])
			require.Equal(t, "false", lsp.ExternalIDs[associatedSgKeyPrefix+"sg2"])

			require.Empty(t, lsp.ExternalIDs[sgsKey])
		}
	})

	t.Run("invalid op", func(t *testing.T) {
		err := ovnClient.SetLogicalSwitchPortsSecurityGroup("sg2", "del")
		require.ErrorContains(t, err, "op must be 'add' or 'remove'")
	})
}

func (suite *OvnClientTestSuite) testEnablePortLayer2forward() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-enable-port-l2-ls"
	lspName := "test-enable-port-l2-lsp"

	lsp := &ovnnb.LogicalSwitchPort{
		UUID: ovsclient.NamedUUID(),
		Name: lspName,
		ExternalIDs: map[string]string{
			"vendor":         util.CniTypeName,
			logicalSwitchKey: lsName,
		},
	}

	err := createLogicalSwitchPort(ovnClient, lsp)
	require.NoError(t, err)

	err = ovnClient.EnablePortLayer2forward(lspName)
	require.NoError(t, err)

	lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"unknown"}, lsp.Addresses)
}

func (suite *OvnClientTestSuite) testSetLogicalSwitchPortVlanTag() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-set-port-vlan-tag-ls"
	lspName := "test-set-port-vlan-tag-lsp"

	lsp := &ovnnb.LogicalSwitchPort{
		UUID: ovsclient.NamedUUID(),
		Name: lspName,
		ExternalIDs: map[string]string{
			"vendor":         util.CniTypeName,
			logicalSwitchKey: lsName,
		},
	}

	err := createLogicalSwitchPort(ovnClient, lsp)
	require.NoError(t, err)

	t.Run("set logical switch port tag when vlan id is 0", func(t *testing.T) {
		err = ovnClient.SetLogicalSwitchPortVlanTag(lspName, 0)
		require.NoError(t, err)

		lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Nil(t, lsp.Tag)
	})

	t.Run("set logical switch port vlan id", func(t *testing.T) {
		err = ovnClient.SetLogicalSwitchPortVlanTag(lspName, 10)
		require.NoError(t, err)

		lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Equal(t, 10, *lsp.Tag)

		// no error when set the same vlan id
		err = ovnClient.SetLogicalSwitchPortVlanTag(lspName, 10)
		require.NoError(t, err)
	})

	t.Run("set logical switch port tag when vlan id is 0 again", func(t *testing.T) {
		err = ovnClient.SetLogicalSwitchPortVlanTag(lspName, 0)
		require.NoError(t, err)

		lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Nil(t, lsp.Tag)
	})

	t.Run("invalid vlan id", func(t *testing.T) {
		err = ovnClient.SetLogicalSwitchPortVlanTag(lspName, -1)
		require.ErrorContains(t, err, "invalid vlan id")

		err = ovnClient.SetLogicalSwitchPortVlanTag(lspName, 4096)
		require.ErrorContains(t, err, "invalid vlan id")
	})
}

func (suite *OvnClientTestSuite) testUpdateLogicalSwitchPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lspName := "test-update-lsp"

	lsp := &ovnnb.LogicalSwitchPort{
		UUID:        ovsclient.NamedUUID(),
		Name:        lspName,
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
	}

	err := createLogicalSwitchPort(ovnClient, lsp)
	require.NoError(t, err)

	t.Run("normal update", func(t *testing.T) {
		lsp := &ovnnb.LogicalSwitchPort{
			Name:      lspName,
			Addresses: []string{"00:0c:29:e4:16:cc 192.168.231.110"},
			ExternalIDs: map[string]string{
				"liveMigration": "0",
			},
			Options: map[string]string{
				"virtual-parents": "test-virtual-parents",
			},
		}
		err = ovnClient.UpdateLogicalSwitchPort(lsp)
		require.NoError(t, err)

		lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"00:0c:29:e4:16:cc 192.168.231.110"}, lsp.Addresses)
		require.Equal(t, map[string]string{
			"liveMigration": "0",
		}, lsp.ExternalIDs)
		require.Equal(t, map[string]string{
			"virtual-parents": "test-virtual-parents",
		}, lsp.Options)
	})

	t.Run("clear addresses", func(t *testing.T) {
		lsp := &ovnnb.LogicalSwitchPort{
			Name: lspName,
		}
		err = ovnClient.UpdateLogicalSwitchPort(lsp, &lsp.Addresses, &lsp.Options)
		require.NoError(t, err)

		lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Empty(t, lsp.Addresses)
		require.Empty(t, lsp.Options)
		require.Equal(t, map[string]string{
			"liveMigration": "0",
		}, lsp.ExternalIDs)
	})
}

func (suite *OvnClientTestSuite) testListLogicalSwitchPorts() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient

	lsName := "test-list-lsp-ls"

	t.Run("normal lsp", func(t *testing.T) {
		t.Parallel()

		// normal lsp
		lspName := "test-list-normal-lsp"
		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.NamedUUID(),
			Name: lspName,
			ExternalIDs: map[string]string{
				logicalSwitchKey: lsName,
				"vendor":         util.CniTypeName,
			},
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		out, err := ovnClient.ListLogicalSwitchPorts(true, map[string]string{logicalSwitchKey: lsName}, func(lsp *ovnnb.LogicalSwitchPort) bool {
			return lsp.Type == ""
		})
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Equal(t, lspName, out[0].Name)
	})

	t.Run("patch lsp", func(t *testing.T) {
		t.Parallel()

		// patch lsp
		lspName := "test-list-patch-lsp"
		lrpName := "test-list-patch-lsp-lrp"
		lsp := &ovnnb.LogicalSwitchPort{
			Name: lspName,
			ExternalIDs: map[string]string{
				logicalSwitchKey: lsName,
				"vendor":         util.CniTypeName,
			},
			Type: "router",
			Options: map[string]string{
				"router-port": lrpName,
			},
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		out, err := ovnClient.ListLogicalSwitchPorts(true, map[string]string{logicalSwitchKey: lsName}, func(lsp *ovnnb.LogicalSwitchPort) bool {
			return lsp.Type == "router" && len(lsp.Options) != 0 && lsp.Options["router-port"] == lrpName
		})
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Equal(t, lspName, out[0].Name)
	})

	t.Run("remote lsp", func(t *testing.T) {
		t.Parallel()

		// remote lsp
		lspName := "test-list-remote-lsp"
		lsp := &ovnnb.LogicalSwitchPort{
			Name: lspName,
			ExternalIDs: map[string]string{
				logicalSwitchKey: lsName,
				"vendor":         util.CniTypeName,
			},
			Type: "remote",
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		out, err := ovnClient.ListLogicalSwitchPorts(true, map[string]string{logicalSwitchKey: lsName}, func(lsp *ovnnb.LogicalSwitchPort) bool {
			return lsp.Type == "remote"
		})
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Equal(t, lspName, out[0].Name)
	})

	t.Run("virtual lsp", func(t *testing.T) {
		t.Parallel()

		// virtual lsp
		lspName := "test-list-virtual-lsp"
		lsp := &ovnnb.LogicalSwitchPort{
			Name: lspName,
			ExternalIDs: map[string]string{
				logicalSwitchKey: lsName,
				"vendor":         util.CniTypeName,
			},
			Type: "virtual",
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		out, err := ovnClient.ListLogicalSwitchPorts(true, map[string]string{logicalSwitchKey: lsName}, func(lsp *ovnnb.LogicalSwitchPort) bool {
			return lsp.Type == "virtual"
		})
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Equal(t, lspName, out[0].Name)
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalSwitchPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lspName := "test-delete-port-lsp"
	lsName := "test-delete-port-ls"

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = ovnClient.CreateBareLogicalSwitchPort(lsName, lspName, "", "")
	require.NoError(t, err)

	t.Run("no err when delete existent logical switch port", func(t *testing.T) {
		ls, err := ovnClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Contains(t, ls.Ports, lsp.UUID)

		err = ovnClient.DeleteLogicalSwitchPort(lspName)
		require.NoError(t, err)

		_, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.ErrorContains(t, err, "object not found")

		ls, err = ovnClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.NotContains(t, ls.Ports, lsp.UUID)
	})

	t.Run("no err when delete non-existent logical switch port", func(t *testing.T) {
		err := ovnClient.DeleteLogicalSwitchPort("test-delete-lrp-non-existent")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testCreateLogicalSwitchPortOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lspName := "test-create-op-lsp"
	lsName := "test-create-op-ls"

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("merge ExternalIDs when exist ExternalIDs", func(t *testing.T) {
		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.NamedUUID(),
			Name: lspName,
			ExternalIDs: map[string]string{
				"pod": lspName,
			},
		}

		ops, err := ovnClient.CreateLogicalSwitchPortOp(lsp, lsName)
		require.NoError(t, err)
		require.Len(t, ops, 2)

		require.Equal(t, ovsdb.OvsMap{
			GoMap: map[interface{}]interface{}{
				logicalSwitchKey: lsName,
				"pod":            lspName,
				"vendor":         "kube-ovn",
			},
		}, ops[0].Row["external_ids"])

		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: lsp.UUID,
						},
					},
				},
			},
		}, ops[1].Mutations)
	})

	t.Run("attach ExternalIDs when does't exist ExternalIDs", func(t *testing.T) {
		lspName := "test-create-op-lsp-none-ext-id"

		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.NamedUUID(),
			Name: lspName,
		}

		ops, err := ovnClient.CreateLogicalSwitchPortOp(lsp, lsName)
		require.NoError(t, err)
		require.Len(t, ops, 2)

		require.Equal(t, ovsdb.OvsMap{
			GoMap: map[interface{}]interface{}{
				logicalSwitchKey: lsName,
				"vendor":         "kube-ovn",
			},
		}, ops[0].Row["external_ids"])

		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: lsp.UUID,
						},
					},
				},
			},
		}, ops[1].Mutations)
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalSwitchPortOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lspName := "test-del-op-lsp"
	lsName := "test-del-op-ls"

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = ovnClient.CreateBareLogicalSwitchPort(lsName, lspName, "", "")
	require.NoError(t, err)

	lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
	require.NoError(t, err)

	ops, err := ovnClient.DeleteLogicalSwitchPortOp(lspName)
	require.NoError(t, err)
	require.Len(t, ops, 2)

	require.Equal(t, []ovsdb.Mutation{
		{
			Column:  "ports",
			Mutator: ovsdb.MutateOperationDelete,
			Value: ovsdb.OvsSet{
				GoSet: []interface{}{
					ovsdb.UUID{
						GoUUID: lsp.UUID,
					},
				},
			},
		},
	}, ops[0].Mutations)

	require.Equal(t,
		ovsdb.Operation{
			Op:    "delete",
			Table: "Logical_Switch_Port",
			Where: []ovsdb.Condition{
				{
					Column:   "_uuid",
					Function: "==",
					Value: ovsdb.UUID{
						GoUUID: lsp.UUID,
					},
				},
			},
		}, ops[1])
}

func (suite *OvnClientTestSuite) testlogicalSwitchPortFilter() {
	t := suite.T()
	t.Parallel()

	lsName := "test-filter-lsp-lr"
	prefix := "test-filter-lsp"
	lsps := make([]*ovnnb.LogicalSwitchPort, 0)
	var patchPort string

	i := 0
	// create three normal lsp
	for ; i < 3; i++ {
		lspName := fmt.Sprintf("%s-%d", prefix, i)
		lsp := &ovnnb.LogicalSwitchPort{
			Name: lspName,
			ExternalIDs: map[string]string{
				logicalSwitchKey: lsName,
				"vendor":         util.CniTypeName,
			},
		}

		lsps = append(lsps, lsp)
	}

	// create one patch lsp
	for ; i < 4; i++ {
		lspName := fmt.Sprintf("%s-%d", prefix, i)
		patchPort = fmt.Sprintf("%s-lrp", lspName)
		lsp := &ovnnb.LogicalSwitchPort{
			Name: lspName,
			ExternalIDs: map[string]string{
				logicalSwitchKey: lsName,
				"vendor":         util.CniTypeName,
			},
			Type: "router",
			Options: map[string]string{
				"router-port": patchPort,
			},
		}

		lsps = append(lsps, lsp)
	}

	// create one remote lsp
	for ; i < 5; i++ {
		lspName := fmt.Sprintf("%s-%d", prefix, i)
		lsp := &ovnnb.LogicalSwitchPort{
			Name: lspName,
			ExternalIDs: map[string]string{
				logicalSwitchKey: lsName,
				"vendor":         util.CniTypeName,
			},
			Type: "remote",
		}

		lsps = append(lsps, lsp)
	}

	// create one virtual lsp
	for ; i < 6; i++ {
		lspName := fmt.Sprintf("%s-%d", prefix, i)
		lsp := &ovnnb.LogicalSwitchPort{
			Name: lspName,
			ExternalIDs: map[string]string{
				logicalSwitchKey: lsName,
				"vendor":         util.CniTypeName,
			},
			Type: "virtual",
		}

		lsps = append(lsps, lsp)
	}

	// create two normal lsp with different logical switch name and vendor
	for ; i < 8; i++ {
		lspName := fmt.Sprintf("%s-%d", prefix, i)
		lsp := &ovnnb.LogicalSwitchPort{
			Name: lspName,
			ExternalIDs: map[string]string{
				logicalSwitchKey: lsName + "-test",
				"vendor":         util.CniTypeName + "-test",
			},
		}

		lsps = append(lsps, lsp)
	}

	// create one normal lsp with different logical switch name and no vendor
	for ; i < 9; i++ {
		lspName := fmt.Sprintf("%s-%d", prefix, i)
		lsp := &ovnnb.LogicalSwitchPort{
			Name: lspName,
			ExternalIDs: map[string]string{
				logicalSwitchKey: lsName + "-test",
			},
		}

		lsps = append(lsps, lsp)
	}

	t.Run("include all lsp", func(t *testing.T) {
		filterFunc := logicalSwitchPortFilter(false, nil, nil)
		count := 0
		for _, lsp := range lsps {
			if filterFunc(lsp) {
				count++
			}
		}
		require.Equal(t, count, 9)
	})

	t.Run("include all lsp which vendor is kube-ovn", func(t *testing.T) {
		filterFunc := logicalSwitchPortFilter(true, nil, nil)
		count := 0
		for _, lsp := range lsps {
			if filterFunc(lsp) {
				count++
			}
		}
		require.Equal(t, count, 6)
	})

	t.Run("include all lsp with external ids", func(t *testing.T) {
		filterFunc := logicalSwitchPortFilter(true, map[string]string{logicalSwitchKey: lsName}, nil)
		count := 0
		for _, lsp := range lsps {
			if filterFunc(lsp) {
				count++
			}
		}
		require.Equal(t, count, 6)
	})

	t.Run("list normal type lsp", func(t *testing.T) {
		filterFunc := logicalSwitchPortFilter(true, map[string]string{logicalSwitchKey: lsName}, func(lsp *ovnnb.LogicalSwitchPort) bool {
			return lsp.Type == ""
		})
		count := 0
		for _, lsp := range lsps {
			if filterFunc(lsp) {
				count++
			}
		}
		require.Equal(t, count, 3)
	})

	t.Run("list remote type lsp", func(t *testing.T) {
		filterFunc := logicalSwitchPortFilter(true, map[string]string{logicalSwitchKey: lsName}, func(lsp *ovnnb.LogicalSwitchPort) bool {
			return lsp.Type == "remote"
		})
		count := 0
		for _, lsp := range lsps {
			if filterFunc(lsp) {
				count++
			}
		}
		require.Equal(t, count, 1)
	})

	t.Run("list virtual type lsp", func(t *testing.T) {
		filterFunc := logicalSwitchPortFilter(true, map[string]string{logicalSwitchKey: lsName}, func(lsp *ovnnb.LogicalSwitchPort) bool {
			return lsp.Type == "virtual"
		})
		count := 0
		for _, lsp := range lsps {
			if filterFunc(lsp) {
				count++
			}
		}
		require.Equal(t, count, 1)
	})

	t.Run("list patch type lsp", func(t *testing.T) {
		filterFunc := logicalSwitchPortFilter(true, map[string]string{logicalSwitchKey: lsName}, func(lsp *ovnnb.LogicalSwitchPort) bool {
			return lsp.Type == "router" && len(lsp.Options) != 0 && lsp.Options["router-port"] == patchPort
		})

		count := 0
		for _, lsp := range lsps {
			if filterFunc(lsp) {
				count++
			}
		}
		require.Equal(t, count, 1)
	})

	t.Run("externalIDs's length is not equal", func(t *testing.T) {
		t.Parallel()

		filterFunc := logicalSwitchPortFilter(true, map[string]string{
			logicalSwitchKey: lsName,
			"key":            "value",
		}, nil)

		count := 0
		for _, lsp := range lsps {
			if filterFunc(lsp) {
				count++
			}
		}
		require.Empty(t, count)
	})

	t.Run("list lsp without vendor", func(t *testing.T) {
		filterFunc := logicalSwitchPortFilter(false, nil, func(lsp *ovnnb.LogicalSwitchPort) bool {
			return len(lsp.ExternalIDs) == 0 || len(lsp.ExternalIDs["vendor"]) == 0
		})

		count := 0
		for _, lsp := range lsps {
			if filterFunc(lsp) {
				count++
			}
		}
		require.Equal(t, count, 1)
	})

	t.Run("list lsp which vendor is not kube-ovn", func(t *testing.T) {
		filterFunc := logicalSwitchPortFilter(false, nil, func(lsp *ovnnb.LogicalSwitchPort) bool {
			return len(lsp.ExternalIDs) == 0 || lsp.ExternalIDs["vendor"] != util.CniTypeName
		})

		count := 0
		for _, lsp := range lsps {
			if filterFunc(lsp) {
				count++
			}
		}
		require.Equal(t, count, 3)
	})
}

func (suite *OvnClientTestSuite) testgetLogicalSwitchPortSgs() {
	t := suite.T()
	t.Parallel()

	t.Run("has associated security group", func(t *testing.T) {
		t.Parallel()
		lsp := &ovnnb.LogicalSwitchPort{
			ExternalIDs: map[string]string{
				"vendor":            util.CniTypeName,
				"associated_sg_sg1": "true",
				"associated_sg_sg2": "true",
			},
		}

		sgs := getLogicalSwitchPortSgs(lsp)
		require.Equal(t, map[string]struct{}{
			"sg1": {},
			"sg2": {},
		}, sgs)
	})

	t.Run("has no associated security group", func(t *testing.T) {
		t.Parallel()
		lsp := &ovnnb.LogicalSwitchPort{
			ExternalIDs: map[string]string{
				"vendor": util.CniTypeName,
			},
		}

		sgs := getLogicalSwitchPortSgs(lsp)
		require.Empty(t, sgs)
	})

	t.Run("has no external ids", func(t *testing.T) {
		t.Parallel()
		lsp := &ovnnb.LogicalSwitchPort{}

		sgs := getLogicalSwitchPortSgs(lsp)
		require.Empty(t, sgs)
	})
}
