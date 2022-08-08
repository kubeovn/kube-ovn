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

func createLogicalSwitchPort(c *OvnClient, lsp *ovnnb.LogicalSwitchPort) error {
	if lsp == nil {
		return fmt.Errorf("logical_switch_port is nil")
	}

	op, err := c.Create(lsp)
	if err != nil {
		return fmt.Errorf("generate operations for creating logical switch port %s: %v", lsp.Name, err)
	}

	return c.Transact("lrp-create", op)
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

		err = ovnClient.CreateLogicalSwitchPort(lsName, lspName, ips, mac, podName, podNamespace, true, sgs, vips, true, true, dhcpOptions, vpcName)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Equal(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25"}, lsp.Addresses)
		require.Equal(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25 10.244.0.110 10.244.0.112"}, lsp.PortSecurity)
		require.Equal(t, map[string]string{
			"security_groups":   strings.ReplaceAll(sgs, ",", "/"),
			"associated_sg_sg":  "true",
			"associated_sg_sg1": "true",
			"associated_sg_" + util.DefaultSecurityGroupName: "false",
			"vips":        strings.ReplaceAll(vips, ",", "/"),
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

		err = ovnClient.CreateLogicalSwitchPort(lsName, lspName, ips, mac, podName, podNamespace, true, sgs, "", true, true, dhcpOptions, vpcName)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Equal(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25"}, lsp.Addresses)
		require.Equal(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25"}, lsp.PortSecurity)
		require.Equal(t, map[string]string{
			"security_groups":   strings.ReplaceAll(sgs, ",", "/"),
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

		err = ovnClient.CreateLogicalSwitchPort(lsName, lspName, ips, mac, podName, podNamespace, true, sgs, vips, true, true, dhcpOptions, vpcName)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Equal(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25"}, lsp.Addresses)
		require.Equal(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25 10.244.0.110 10.244.0.112"}, lsp.PortSecurity)
		require.Equal(t, map[string]string{
			"security_groups":   strings.ReplaceAll(sgs, ",", "/"),
			"associated_sg_sg":  "true",
			"associated_sg_sg1": "true",
			"associated_sg_" + util.DefaultSecurityGroupName: "true",
			"pod":         fmt.Sprintf("%s/%s", podNamespace, podName),
			"ls":          lsName,
			"vendor":      util.CniTypeName,
			"vips":        strings.ReplaceAll(vips, ",", "/"),
			"attach-vips": "true",
		}, lsp.ExternalIDs)
		require.Equal(t, dhcpOptions.DHCPv4OptionsUUID, *lsp.Dhcpv4Options)
		require.Equal(t, dhcpOptions.DHCPv6OptionsUUID, *lsp.Dhcpv6Options)
	})

	t.Run("create logical switch port with default vpc", func(t *testing.T) {
		lspName := "test-create-port-lsp-default-vpc"
		sgs := "sg,sg1"
		vpcName := "ovn-cluster"

		err = ovnClient.CreateLogicalSwitchPort(lsName, lspName, ips, mac, podName, podNamespace, true, sgs, vips, true, true, dhcpOptions, vpcName)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Equal(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25"}, lsp.Addresses)
		require.Equal(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25 10.244.0.110 10.244.0.112"}, lsp.PortSecurity)
		require.Equal(t, map[string]string{
			"security_groups":   strings.ReplaceAll(sgs, ",", "/"),
			"associated_sg_sg":  "true",
			"associated_sg_sg1": "true",
			"pod":               fmt.Sprintf("%s/%s", podNamespace, podName),
			"ls":                lsName,
			"vendor":            util.CniTypeName,
			"vips":              strings.ReplaceAll(vips, ",", "/"),
			"attach-vips":       "true",
		}, lsp.ExternalIDs)
		require.Equal(t, dhcpOptions.DHCPv4OptionsUUID, *lsp.Dhcpv4Options)
		require.Equal(t, dhcpOptions.DHCPv6OptionsUUID, *lsp.Dhcpv6Options)
	})

	t.Run("create logical switch port with portSecurity=false", func(t *testing.T) {
		lspName := "test-create-port-lsp-no-portSecurity"
		sgs := "sg,sg1"
		vpcName := "test-vpc"

		err = ovnClient.CreateLogicalSwitchPort(lsName, lspName, ips, mac, podName, podNamespace, false, sgs, vips, true, true, dhcpOptions, vpcName)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Equal(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25"}, lsp.Addresses)
		require.Equal(t, map[string]string{
			"associated_sg_" + util.DefaultSecurityGroupName: "false",
			"pod":         fmt.Sprintf("%s/%s", podNamespace, podName),
			"ls":          lsName,
			"vendor":      util.CniTypeName,
			"vips":        strings.ReplaceAll(vips, ",", "/"),
			"attach-vips": "true",
		}, lsp.ExternalIDs)
		require.Equal(t, dhcpOptions.DHCPv4OptionsUUID, *lsp.Dhcpv4Options)
		require.Equal(t, dhcpOptions.DHCPv6OptionsUUID, *lsp.Dhcpv6Options)
	})

	t.Run("create logical switch port without dhcp options", func(t *testing.T) {
		lspName := "test-create-port-lsp-no-dhcp-options"
		sgs := "sg,sg1"
		vpcName := "test-vpc"

		err = ovnClient.CreateLogicalSwitchPort(lsName, lspName, ips, mac, podName, podNamespace, true, sgs, vips, true, true, nil, vpcName)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Equal(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25"}, lsp.Addresses)
		require.Equal(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25 10.244.0.110 10.244.0.112"}, lsp.PortSecurity)
		require.Equal(t, map[string]string{
			"security_groups":   strings.ReplaceAll(sgs, ",", "/"),
			"associated_sg_sg":  "true",
			"associated_sg_sg1": "true",
			"associated_sg_" + util.DefaultSecurityGroupName: "false",
			"vips":        strings.ReplaceAll(vips, ",", "/"),
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
		require.Equal(t, []string{"unknown"}, lsp.Addresses)
		require.Equal(t, map[string]string{
			"network_name": provider,
		}, lsp.Options)

		require.Equal(t, 200, *lsp.Tag)
	})

	t.Run("create localnet logical switch port without vlan id", func(t *testing.T) {
		lspName := "test-create-localnet-port-lsp-no-vlanid"
		err = ovnClient.CreateLocalnetLogicalSwitchPort(lsName, lspName, provider, 0)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Equal(t, lspName, lsp.Name)
		require.Equal(t, "localnet", lsp.Type)
		require.Equal(t, []string{"unknown"}, lsp.Addresses)
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

	err = ovnClient.CreateBareLogicalSwitchPort(lsName, lspName)
	require.NoError(t, err)

	lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
	require.NoError(t, err)

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
		UUID: ovsclient.UUID(),
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
		require.Equal(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25 10.244.100.10 10.244.100.11"}, lsp.PortSecurity)
		require.Equal(t, map[string]string{
			"vendor":         util.CniTypeName,
			logicalSwitchKey: lsName,
			"vips":           "10.244.100.10/10.244.100.11",
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
			UUID: ovsclient.UUID(),
			Name: lspName,
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		err = ovnClient.SetLogicalSwitchPortSecurity(true, lspName, "00:00:00:AB:B4:65", "10.244.0.37,fc00::af4:25", "10.244.100.10,10.244.100.11")
		require.NoError(t, err)

		lsp, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Equal(t, []string{"00:00:00:AB:B4:65 10.244.0.37 fc00::af4:25 10.244.100.10 10.244.100.11"}, lsp.PortSecurity)
		require.Equal(t, map[string]string{
			"vips":        "10.244.100.10/10.244.100.11",
			"attach-vips": "true",
		}, lsp.ExternalIDs)
	})
}

func (suite *OvnClientTestSuite) testEnablePortLayer2forward() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-enble-port-l2-ls"
	lspName := "test-enable-port-l2-lsp"

	lsp := &ovnnb.LogicalSwitchPort{
		UUID: ovsclient.UUID(),
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
	require.Equal(t, []string{"unknown"}, lsp.Addresses)
}

func (suite *OvnClientTestSuite) testSetLogicalSwitchPortVlanTag() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-set-port-vlan-tag-ls"
	lspName := "test-set-port-vlan-tag-lsp"

	lsp := &ovnnb.LogicalSwitchPort{
		UUID: ovsclient.UUID(),
		Name: lspName,
		ExternalIDs: map[string]string{
			"vendor":         util.CniTypeName,
			logicalSwitchKey: lsName,
		},
	}

	err := createLogicalSwitchPort(ovnClient, lsp)
	require.NoError(t, err)

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

	t.Run("invalid vlan id", func(t *testing.T) {
		err = ovnClient.SetLogicalSwitchPortVlanTag(lspName, 0)
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
		UUID:        ovsclient.UUID(),
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
		require.Equal(t, []string{"00:0c:29:e4:16:cc 192.168.231.110"}, lsp.Addresses)
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

	t.Run("result should exclude lsp when vendor is not kube-ovn", func(t *testing.T) {
		lspName := "test-list-lsp-other-vendor"

		lsp := &ovnnb.LogicalSwitchPort{
			UUID:        ovsclient.UUID(),
			Name:        lspName,
			ExternalIDs: map[string]string{"vendor": "other-vendor"},
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		out, err := ovnClient.ListLogicalSwitchPorts(true, nil)
		require.NoError(t, err)

		found := false
		for _, l := range out {
			if l.Name == lspName {
				found = true
				break
			}
		}
		require.False(t, found)

	})

	t.Run("result should exclude lsp when externalIDs's length is not equal", func(t *testing.T) {
		lspName := "test-list-lsp-mismatch-length"

		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.UUID(),
			Name: lspName,
			ExternalIDs: map[string]string{
				"vendor":          util.CniTypeName,
				"security_groups": "sg",
			},
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		out, err := ovnClient.ListLogicalSwitchPorts(true, map[string]string{"security_groups": "sg", "key": "value", "key1": "value"})
		require.NoError(t, err)
		require.Empty(t, out)
	})

	t.Run("result should include lsp when key exists in lsp column: external_ids", func(t *testing.T) {
		lspName := "test-list-lsp-exist"

		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.UUID(),
			Name: lspName,
			ExternalIDs: map[string]string{
				"vendor":          util.CniTypeName,
				"security_groups": "sg",
			},
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		out, err := ovnClient.ListLogicalSwitchPorts(true, map[string]string{"security_groups": "sg"})
		require.NoError(t, err)
		require.NotEmpty(t, out)
	})

	t.Run("result should include all lsp when externalIDs is empty", func(t *testing.T) {
		prefix := "test-list-lsp-all"

		for i := 0; i < 4; i++ {
			lspName := fmt.Sprintf("%s-%d", prefix, i)
			lsp := &ovnnb.LogicalSwitchPort{
				UUID: ovsclient.UUID(),
				Name: lspName,
				ExternalIDs: map[string]string{
					"vendor":          util.CniTypeName,
					"security_groups": "sg",
				},
			}

			err := createLogicalSwitchPort(ovnClient, lsp)
			require.NoError(t, err)
		}

		out, err := ovnClient.ListLogicalSwitchPorts(true, nil)
		require.NoError(t, err)
		count := 0
		for _, v := range out {
			if strings.Contains(v.Name, prefix) {
				count++
			}
		}
		require.Equal(t, count, 4)

		out, err = ovnClient.ListLogicalSwitchPorts(true, map[string]string{})
		require.NoError(t, err)
		count = 0
		for _, v := range out {
			if strings.Contains(v.Name, prefix) {
				count++
			}
		}
		require.Equal(t, count, 4)
	})

	t.Run("result should include lsp which externalIDs[key] is ''", func(t *testing.T) {
		lspName := "test-list-lsp-no-val"

		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.UUID(),
			Name: lspName,
			ExternalIDs: map[string]string{
				"vendor":               util.CniTypeName,
				"security_groups_test": "sg",
				"key":                  "val",
			},
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		out, err := ovnClient.ListLogicalSwitchPorts(true, map[string]string{"security_groups_test": "", "key": ""})
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Equal(t, lspName, out[0].Name)

		out, err = ovnClient.ListLogicalSwitchPorts(true, map[string]string{"security_groups_test": ""})
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Equal(t, lspName, out[0].Name)

		out, err = ovnClient.ListLogicalSwitchPorts(true, map[string]string{"security_groups_test": "", "key": "", "key1": ""})
		require.NoError(t, err)
		require.Empty(t, out)
	})
}

func (suite *OvnClientTestSuite) testListRemoteTypeLogicalSwitchPorts() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient

	t.Run("should include lsp which type is remote", func(t *testing.T) {
		lspName := "test-list-lsp-remote"

		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.UUID(),
			Name: lspName,
			ExternalIDs: map[string]string{
				"vendor":               util.CniTypeName,
				"security_groups_test": "sg",
				"key":                  "val",
			},
			Type: "remote",
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		out, err := ovnClient.ListRemoteTypeLogicalSwitchPorts()
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

	err = ovnClient.CreateBareLogicalSwitchPort(lsName, lspName)
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

	t.Run("merget ExternalIDs when exist ExternalIDs", func(t *testing.T) {
		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.UUID(),
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
		lspName := "test-create-op-lsp-none-exid"

		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.UUID(),
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

	err = ovnClient.CreateBareLogicalSwitchPort(lsName, lspName)
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
