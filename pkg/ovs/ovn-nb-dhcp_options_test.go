package ovs

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func mockSubnet(name string, enableDHCP bool) *kubeovnv1.Subnet {
	return &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock:  "10.244.0.0/16,fc00::af4:0/112",
			Gateway:    "10.244.0.1,fc00::0af4:01",
			Protocol:   kubeovnv1.ProtocolDual,
			EnableDHCP: enableDHCP,
		},
	}
}

func (suite *OvnClientTestSuite) testUpdateDHCPOptions() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-update-dhcp-opt-ls"
	subnet := mockSubnet(lsName, true)

	t.Run("update dhcp options", func(t *testing.T) {
		uuid, err := ovnClient.UpdateDHCPOptions(subnet)
		require.NoError(t, err)

		v4DHCPOpt, err := ovnClient.GetDHCPOptions(lsName, "IPv4", false)
		require.NoError(t, err)

		v6DHCPOpt, err := ovnClient.GetDHCPOptions(lsName, "IPv6", false)
		require.NoError(t, err)

		require.Equal(t, uuid.DHCPv4OptionsUUID, v4DHCPOpt.UUID)
		require.Equal(t, uuid.DHCPv6OptionsUUID, v6DHCPOpt.UUID)
	})

	t.Run("delete dhcp options", func(t *testing.T) {
		subnet.Spec.EnableDHCP = false

		uuid, err := ovnClient.UpdateDHCPOptions(subnet)
		require.NoError(t, err)
		require.Empty(t, uuid.DHCPv4OptionsUUID)
		require.Empty(t, uuid.DHCPv6OptionsUUID)

		_, err = ovnClient.GetDHCPOptions(lsName, "IPv4", false)
		require.ErrorContains(t, err, "not found")

		_, err = ovnClient.GetDHCPOptions(lsName, "IPv6", false)
		require.ErrorContains(t, err, "not found")
	})
}

func (suite *OvnClientTestSuite) test_updateDHCPv4Options() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-update-v4-dhcp-opt-ls"
	cidr := "192.168.30.0/24"
	gateway := "192.168.30.1"
	var serverMac string

	t.Run("create dhcp options", func(t *testing.T) {
		t.Run("without options", func(t *testing.T) {
			uuid, err := ovnClient.updateDHCPv4Options(lsName, cidr, gateway, "")
			require.NoError(t, err)

			dhcpOpt, err := ovnClient.GetDHCPOptions(lsName, "IPv4", false)
			require.NoError(t, err)

			serverMac = dhcpOpt.Options["server_mac"]

			require.Equal(t, uuid, dhcpOpt.UUID)
			require.Equal(t, cidr, dhcpOpt.Cidr)
			require.Equal(t, map[string]string{
				"lease_time": "3600",
				"router":     "192.168.30.1",
				"server_id":  "169.254.0.254",
				"server_mac": serverMac,
			}, dhcpOpt.Options)
		})

		t.Run("with options", func(t *testing.T) {
			lsName := "test-update-v4-dhcp-opt-ls-with-opt"
			options := fmt.Sprintf("lease_time=%d,router=%s,server_id=%s,server_mac=%s", 7200, gateway, "169.254.0.1", "00:00:00:11:22:33")
			uuid, err := ovnClient.updateDHCPv4Options(lsName, cidr, gateway, options)
			require.NoError(t, err)

			dhcpOpt, err := ovnClient.GetDHCPOptions(lsName, "IPv4", false)
			require.NoError(t, err)

			require.Equal(t, uuid, dhcpOpt.UUID)
			require.Equal(t, cidr, dhcpOpt.Cidr)
			require.Equal(t, map[string]string{
				"lease_time": "7200",
				"router":     "192.168.30.1",
				"server_id":  "169.254.0.1",
				"server_mac": "00:00:00:11:22:33",
			}, dhcpOpt.Options)
		})
	})

	t.Run("update dhcp options", func(t *testing.T) {
		uuid, err := ovnClient.updateDHCPv4Options(lsName, cidr, gateway, "")
		require.NoError(t, err)

		dhcpOpt, err := ovnClient.GetDHCPOptions(lsName, "IPv4", false)
		require.NoError(t, err)

		require.Equal(t, uuid, dhcpOpt.UUID)
		require.Equal(t, cidr, dhcpOpt.Cidr)
		require.Equal(t, map[string]string{
			"lease_time": "3600",
			"router":     "192.168.30.1",
			"server_id":  "169.254.0.254",
			"server_mac": serverMac,
		}, dhcpOpt.Options)
	})
}

func (suite *OvnClientTestSuite) test_updateDHCPv6Options() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-update-v6-dhcp-opt-ls"
	cidr := "fd00::c0a8:6e01/120"
	var serverID string

	t.Run("create dhcp options", func(t *testing.T) {
		t.Run("without options", func(t *testing.T) {
			uuid, err := ovnClient.updateDHCPv6Options(lsName, cidr, "")
			require.NoError(t, err)

			dhcpOpt, err := ovnClient.GetDHCPOptions(lsName, "IPv6", false)
			require.NoError(t, err)

			serverID = dhcpOpt.Options["server_id"]

			require.Equal(t, uuid, dhcpOpt.UUID)
			require.Equal(t, cidr, dhcpOpt.Cidr)
			require.Equal(t, map[string]string{
				"server_id": serverID,
			}, dhcpOpt.Options)
		})

		t.Run("with options", func(t *testing.T) {
			lsName := "test-update-v6-dhcp-opt-ls-with-opt"
			options := fmt.Sprintf("server_id=%s", "00:00:00:55:22:33")
			uuid, err := ovnClient.updateDHCPv6Options(lsName, cidr, options)
			require.NoError(t, err)

			dhcpOpt, err := ovnClient.GetDHCPOptions(lsName, "IPv6", false)
			require.NoError(t, err)

			require.Equal(t, uuid, dhcpOpt.UUID)
			require.Equal(t, cidr, dhcpOpt.Cidr)
			require.Equal(t, map[string]string{
				"server_id": "00:00:00:55:22:33",
			}, dhcpOpt.Options)
		})
	})

	t.Run("update dhcp options", func(t *testing.T) {
		uuid, err := ovnClient.updateDHCPv6Options(lsName, cidr, "")
		require.NoError(t, err)

		dhcpOpt, err := ovnClient.GetDHCPOptions(lsName, "IPv6", false)
		require.NoError(t, err)

		require.Equal(t, uuid, dhcpOpt.UUID)
		require.Equal(t, cidr, dhcpOpt.Cidr)
		require.Equal(t, map[string]string{
			"server_id": serverID,
		}, dhcpOpt.Options)
	})
}

func (suite *OvnClientTestSuite) testDeleteDHCPOptionsByUUIDs() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-del-dhcp-opt-uuid-ls"
	v4CidrBlock := []string{"192.168.30.0/24", "192.168.40.0/24", "192.168.50.0/24"}
	uuidList := make([]string, 0)

	// create three ipv4 dhcp options
	for _, cidr := range v4CidrBlock {
		err := ovnClient.CreateDHCPOptions(lsName, cidr, "")
		require.NoError(t, err)
	}

	out, err := ovnClient.ListDHCPOptions(true, map[string]string{logicalSwitchKey: lsName})
	require.NoError(t, err)
	require.Len(t, out, 3)
	for _, o := range out {
		uuidList = append(uuidList, o.UUID)
	}

	err = ovnClient.DeleteDHCPOptionsByUUIDs(uuidList...)
	require.NoError(t, err)

	out, err = ovnClient.ListDHCPOptions(true, map[string]string{logicalSwitchKey: lsName})
	require.NoError(t, err)
	require.Empty(t, out)
}

func (suite *OvnClientTestSuite) testDeleteDHCPOptions() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-del-dhcp-opt-ls"
	v4CidrBlock := []string{"192.168.30.0/24", "192.168.40.0/24", "192.168.50.0/24"}
	v6CidrBlock := []string{"fd00::c0a8:6401/120", "fd00::c0a8:6e01/120"}

	prepare := func() {
		// create three ipv4 dhcp options
		for _, cidr := range v4CidrBlock {
			err := ovnClient.CreateDHCPOptions(lsName, cidr, "")
			require.NoError(t, err)
		}

		// create two ipv6 dhcp options
		for _, cidr := range v6CidrBlock {
			err := ovnClient.CreateDHCPOptions(lsName, cidr, "")
			require.NoError(t, err)
		}
	}

	t.Run("delete single protocol dhcp options", func(t *testing.T) {
		prepare()

		/* delete ipv4 dhcp options */
		err := ovnClient.DeleteDHCPOptions(lsName, "IPv4")
		require.NoError(t, err)

		out, err := ovnClient.ListDHCPOptions(true, map[string]string{logicalSwitchKey: lsName, "protocol": "IPv4"})
		require.NoError(t, err)
		require.Empty(t, out)

		out, err = ovnClient.ListDHCPOptions(true, map[string]string{logicalSwitchKey: lsName, "protocol": "IPv6"})
		require.NoError(t, err)
		require.Len(t, out, 2)

		/* delete ipv6 dhcp options */
		err = ovnClient.DeleteDHCPOptions(lsName, "IPv6")
		require.NoError(t, err)

		out, err = ovnClient.ListDHCPOptions(true, map[string]string{logicalSwitchKey: lsName, "protocol": "IPv6"})
		require.NoError(t, err)
		require.Empty(t, out)
	})

	t.Run("delete all protocol dhcp options", func(t *testing.T) {
		prepare()

		err := ovnClient.DeleteDHCPOptions(lsName, kubeovnv1.ProtocolDual)
		require.NoError(t, err)

		out, err := ovnClient.ListDHCPOptions(true, map[string]string{logicalSwitchKey: lsName, "protocol": "IPv6"})
		require.NoError(t, err)
		require.Empty(t, out)
	})
}

func (suite *OvnClientTestSuite) testGetDHCPOptions() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-get-dhcp-opt-ls"

	t.Run("ipv4 dhcp options", func(t *testing.T) {
		cidr := "192.168.30.0/24"
		protocol := kubeovnv1.ProtocolIPv4
		err := ovnClient.CreateDHCPOptions(lsName, cidr, "")
		require.NoError(t, err)

		t.Run("found dhcp options", func(t *testing.T) {
			_, err := ovnClient.GetDHCPOptions(lsName, protocol, false)
			require.NoError(t, err)
		})

		t.Run("protocol is different", func(t *testing.T) {
			_, err := ovnClient.GetDHCPOptions(lsName, kubeovnv1.ProtocolIPv6, false)
			require.ErrorContains(t, err, "not found")
		})

		t.Run("logical switch name is different", func(t *testing.T) {
			_, err := ovnClient.GetDHCPOptions(lsName+"x", protocol, false)
			require.ErrorContains(t, err, "not found")
		})

		t.Run("not found and ignoreNotFound=true", func(t *testing.T) {
			_, err := ovnClient.GetDHCPOptions(lsName+"x", protocol, true)
			require.NoError(t, err)
		})
	})

	t.Run("ipv6 dhcp options", func(t *testing.T) {
		cidr := "fd00::c0a8:6901/120"
		protocol := kubeovnv1.ProtocolIPv6
		err := ovnClient.CreateDHCPOptions(lsName, cidr, "")
		require.NoError(t, err)

		t.Run("found dhcp options", func(t *testing.T) {
			_, err := ovnClient.GetDHCPOptions(lsName, protocol, false)
			require.NoError(t, err)
		})
	})

	t.Run("invalid protocol", func(t *testing.T) {
		protocol := kubeovnv1.ProtocolDual
		_, err := ovnClient.GetDHCPOptions(lsName, protocol, false)
		require.ErrorContains(t, err, "protocol must be IPv4 or IPv6")

		protocol = ""
		_, err = ovnClient.GetDHCPOptions(lsName, protocol, false)
		require.ErrorContains(t, err, "protocol must be IPv4 or IPv6")
	})
}

func (suite *OvnClientTestSuite) testListDHCPOptions() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-list-dhcp-opt-ls"
	v4CidrBlock := []string{"192.168.30.0/24", "192.168.40.0/24", "192.168.50.0/24"}

	// create three ipv4 dhcp options
	for _, cidr := range v4CidrBlock {
		err := ovnClient.CreateDHCPOptions(lsName, cidr, "")
		require.NoError(t, err)
	}

	/* list all direction acl */
	out, err := ovnClient.ListDHCPOptions(true, map[string]string{logicalSwitchKey: lsName})
	require.NoError(t, err)
	require.Len(t, out, 3)
}

func (suite *OvnClientTestSuite) test_dhcpOptionsFilter() {
	t := suite.T()
	t.Parallel()

	lsName := "test-filter-dhcp-opt-ls"
	v4CidrBlock := []string{"192.168.30.0/24", "192.168.40.0/24", "192.168.50.0/24"}
	v6CidrBlock := []string{"fd00::c0a8:6401/120", "fd00::c0a8:6e01/120"}
	dhcpOpts := make([]*ovnnb.DHCPOptions, 0)

	t.Run("filter dhcp options", func(t *testing.T) {
		t.Parallel()

		// create three ipv4 dhcp options
		for _, cidr := range v4CidrBlock {
			dhcpOpt, err := newDHCPOptions(lsName, cidr, "")
			require.NoError(t, err)
			dhcpOpts = append(dhcpOpts, dhcpOpt)
		}

		// create two ipv6 dhcp options
		for _, cidr := range v6CidrBlock {
			dhcpOpt, err := newDHCPOptions(lsName, cidr, "")
			require.NoError(t, err)
			dhcpOpts = append(dhcpOpts, dhcpOpt)
		}

		// create three ipv4 dhcp options with other logical switch name
		for _, cidr := range v4CidrBlock {
			dhcpOpt, err := newDHCPOptions(lsName, cidr, "")
			dhcpOpt.ExternalIDs[logicalSwitchKey] = lsName + "-test"
			require.NoError(t, err)
			dhcpOpts = append(dhcpOpts, dhcpOpt)
		}

		// create three ipv4 dhcp options with other vendor
		for _, cidr := range v4CidrBlock {
			dhcpOpt, err := newDHCPOptions(lsName, cidr, "")
			dhcpOpt.ExternalIDs["vendor"] = util.CniTypeName + "-test"
			require.NoError(t, err)
			dhcpOpts = append(dhcpOpts, dhcpOpt)
		}

		/* include all dhcp options */
		filterFunc := dhcpOptionsFilter(false, nil)
		count := 0
		for _, dhcpOpt := range dhcpOpts {
			if filterFunc(dhcpOpt) {
				count++
			}
		}
		require.Equal(t, count, 11)

		/* include same vendor dhcp options */
		filterFunc = dhcpOptionsFilter(true, nil)
		count = 0
		for _, dhcpOpt := range dhcpOpts {
			if filterFunc(dhcpOpt) {
				count++
			}
		}
		require.Equal(t, count, 8)

		/* include same ls dhcp options */
		filterFunc = dhcpOptionsFilter(true, map[string]string{logicalSwitchKey: lsName})
		count = 0
		for _, dhcpOpt := range dhcpOpts {
			if filterFunc(dhcpOpt) {
				count++
			}
		}
		require.Equal(t, count, 5)

		/* include same protocol dhcp options */
		filterFunc = dhcpOptionsFilter(true, map[string]string{logicalSwitchKey: lsName, "protocol": "IPv4"})
		count = 0
		for _, dhcpOpt := range dhcpOpts {
			if filterFunc(dhcpOpt) {
				count++
			}
		}
		require.Equal(t, count, 3)

		/* include all protocol dhcp options */
		filterFunc = dhcpOptionsFilter(true, map[string]string{logicalSwitchKey: lsName, "protocol": ""})
		count = 0
		for _, dhcpOpt := range dhcpOpts {
			if filterFunc(dhcpOpt) {
				count++
			}
		}
		require.Equal(t, count, 5)
	})

	t.Run("result should exclude dhcp options when externalIDs's length is not equal", func(t *testing.T) {
		t.Parallel()

		dhcpOpt, err := newDHCPOptions(lsName, "192.168.30.0/24", "")
		require.NoError(t, err)

		filterFunc := dhcpOptionsFilter(true, map[string]string{
			logicalSwitchKey: lsName,
			"key":            "value",
		})

		require.False(t, filterFunc(dhcpOpt))
	})
}
