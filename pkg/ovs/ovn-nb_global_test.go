package ovs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func mockNBGlobal() *ovnnb.NBGlobal {
	return &ovnnb.NBGlobal{
		NbCfg: 100,
		Options: map[string]string{
			"mac_prefix": "11:22:33",
			"max_tunid":  "16711680",
		},
	}
}

func (suite *OvnClientTestSuite) testGetNbGlobal() {
	t := suite.T()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient

	t.Cleanup(func() {
		err := failedNbClient.DeleteNbGlobal()
		require.Error(t, err)

		err = nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		_, err = nbClient.GetNbGlobal()
		require.ErrorContains(t, err, "not found nb_global")
	})

	t.Run("return err when not found nb_global", func(t *testing.T) {
		_, err := nbClient.GetNbGlobal()
		require.ErrorContains(t, err, "not found nb_global")
	})

	t.Run("no err when found nb_global", func(t *testing.T) {
		nbGlobal := mockNBGlobal()
		err := nbClient.CreateNbGlobal(nbGlobal)
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.NotEmpty(t, out.UUID)
	})

	t.Run("failed client create nb_global", func(t *testing.T) {
		err := failedNbClient.CreateNbGlobal(nil)
		require.Error(t, err)
	})
}

func (suite *OvnClientTestSuite) testUpdateNbGlobal() {
	t := suite.T()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient

	t.Cleanup(func() {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)
	})

	nbGlobal := mockNBGlobal()
	err := nbClient.CreateNbGlobal(nbGlobal)
	require.NoError(t, err)

	nbGlobal, err = nbClient.GetNbGlobal()
	require.NoError(t, err)

	t.Run("normal update", func(t *testing.T) {
		nbGlobal.Options = map[string]string{
			"mac_prefix": "11:22:aa",
			"max_tunid":  "16711680",
		}

		err = nbClient.UpdateNbGlobal(nbGlobal)
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "11:22:aa", out.Options["mac_prefix"])
		require.Equal(t, "16711680", out.Options["max_tunid"])
	})

	t.Run("failed client update", func(t *testing.T) {
		err = failedNbClient.UpdateNbGlobal(nil)
		require.Error(t, err)
	})

	t.Run("create options", func(t *testing.T) {
		nbGlobal := &ovnnb.NBGlobal{
			UUID: nbGlobal.UUID,
		}

		err = nbClient.UpdateNbGlobal(nbGlobal, &nbGlobal.Name, &nbGlobal.Options)
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Empty(t, out.Name)
		require.Empty(t, out.Options)
	})
}

func (suite *OvnClientTestSuite) testSetAzName() {
	t := suite.T()

	nbClient := suite.ovnNBClient

	t.Cleanup(func() {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		_, err = nbClient.GetNbGlobal()
		require.ErrorContains(t, err, "not found nb_global")
	})

	nbGlobal := mockNBGlobal()
	err := nbClient.CreateNbGlobal(nbGlobal)
	require.NoError(t, err)

	t.Run("set az name", func(t *testing.T) {
		err = nbClient.SetAzName("test-az")
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "test-az", out.Name)
	})

	t.Run("clear az name", func(t *testing.T) {
		err = nbClient.SetAzName("")
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Empty(t, out.Name)
	})

	t.Run("set az name when it's different", func(t *testing.T) {
		err = nbClient.SetAzName("new-az")
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "new-az", out.Name)
	})

	t.Run("set az name when it's the same", func(t *testing.T) {
		err = nbClient.SetAzName("new-az")
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "new-az", out.Name)
	})

	t.Run("set az name when GetNbGlobal fails", func(t *testing.T) {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		err = nbClient.SetAzName("test-az")
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to get nb global")
		err = nbClient.CreateNbGlobal(nbGlobal)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testSetICAutoRoute() {
	t := suite.T()

	nbClient := suite.ovnNBClient

	t.Cleanup(func() {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		_, err = nbClient.GetNbGlobal()
		require.ErrorContains(t, err, "not found nb_global")
	})

	nbGlobal := mockNBGlobal()
	err := nbClient.CreateNbGlobal(nbGlobal)
	require.NoError(t, err)

	t.Run("enable ovn-ic auto route", func(t *testing.T) {
		err = nbClient.SetICAutoRoute(true, []string{"1.1.1.1", "2.2.2.2"})
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "true", out.Options["ic-route-adv"])
		require.Equal(t, "true", out.Options["ic-route-learn"])
		require.Equal(t, "1.1.1.1,2.2.2.2", out.Options["ic-route-blacklist"])
	})

	t.Run("disable ovn-ic auto route", func(t *testing.T) {
		err = nbClient.SetICAutoRoute(false, []string{"1.1.1.1", "2.2.2.2"})
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.NotContains(t, out.Options, "ic-route-adv")
		require.NotContains(t, out.Options, "ic-route-learn")
		require.NotContains(t, out.Options, "ic-route-blacklist")
	})

	t.Run("enable ovn-ic auto route with empty blacklist", func(t *testing.T) {
		err = nbClient.SetICAutoRoute(true, []string{})
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "true", out.Options["ic-route-adv"])
		require.Equal(t, "true", out.Options["ic-route-learn"])
		require.Equal(t, "", out.Options["ic-route-blacklist"])
	})

	t.Run("enable ovn-ic auto route with multiple blacklist entries", func(t *testing.T) {
		err = nbClient.SetICAutoRoute(true, []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"})
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "true", out.Options["ic-route-adv"])
		require.Equal(t, "true", out.Options["ic-route-learn"])
		require.Equal(t, "1.1.1.1,2.2.2.2,3.3.3.3", out.Options["ic-route-blacklist"])
	})

	t.Run("enable ovn-ic auto route when already enabled", func(t *testing.T) {
		err = nbClient.SetICAutoRoute(true, []string{"1.1.1.1"})
		require.NoError(t, err)

		err = nbClient.SetICAutoRoute(true, []string{"1.1.1.1"})
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "true", out.Options["ic-route-adv"])
		require.Equal(t, "true", out.Options["ic-route-learn"])
		require.Equal(t, "1.1.1.1", out.Options["ic-route-blacklist"])
	})

	t.Run("disable ovn-ic auto route when already disabled", func(t *testing.T) {
		err = nbClient.SetICAutoRoute(false, []string{})
		require.NoError(t, err)

		err = nbClient.SetICAutoRoute(false, []string{})
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.NotContains(t, out.Options, "ic-route-adv")
		require.NotContains(t, out.Options, "ic-route-learn")
		require.NotContains(t, out.Options, "ic-route-blacklist")
	})

	t.Run("set ovn-ic auto route when GetNbGlobal fails", func(t *testing.T) {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		err = nbClient.SetICAutoRoute(true, []string{"1.1.1.1"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to get nb global")

		err = nbClient.CreateNbGlobal(nbGlobal)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testSetUseCtInvMatch() {
	t := suite.T()

	nbClient := suite.ovnNBClient

	t.Cleanup(func() {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		_, err = nbClient.GetNbGlobal()
		require.ErrorContains(t, err, "not found nb_global")
	})

	nbGlobal := mockNBGlobal()
	err := nbClient.CreateNbGlobal(nbGlobal)
	require.NoError(t, err)

	err = nbClient.SetUseCtInvMatch()
	require.NoError(t, err)

	out, err := nbClient.GetNbGlobal()
	require.NoError(t, err)
	require.Equal(t, "false", out.Options["use_ct_inv_match"])
}

func (suite *OvnClientTestSuite) testSetLBCIDR() {
	t := suite.T()

	nbClient := suite.ovnNBClient
	serviceCIDR := "10.96.0.0/12"

	t.Cleanup(func() {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		_, err = nbClient.GetNbGlobal()
		require.ErrorContains(t, err, "not found nb_global")
	})

	nbGlobal := mockNBGlobal()
	err := nbClient.CreateNbGlobal(nbGlobal)
	require.NoError(t, err)

	err = nbClient.SetLBCIDR(serviceCIDR)
	require.NoError(t, err)

	out, err := nbClient.GetNbGlobal()
	require.NoError(t, err)
	require.Equal(t, serviceCIDR, out.Options["svc_ipv4_cidr"])
}

func (suite *OvnClientTestSuite) testSetOVNIPSec() {
	t := suite.T()

	nbClient := suite.ovnNBClient

	t.Cleanup(func() {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		_, err = nbClient.GetNbGlobal()
		require.ErrorContains(t, err, "not found nb_global")
	})

	nbGlobal := mockNBGlobal()
	err := nbClient.CreateNbGlobal(nbGlobal)
	require.NoError(t, err)

	t.Run("enable OVN IPSec", func(t *testing.T) {
		err = nbClient.SetOVNIPSec(true)
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.True(t, out.Ipsec)
	})

	t.Run("disable OVN IPSec", func(t *testing.T) {
		err = nbClient.SetOVNIPSec(false)
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.False(t, out.Ipsec)
	})

	t.Run("set OVN IPSec when it's already set", func(t *testing.T) {
		err = nbClient.SetOVNIPSec(true)
		require.NoError(t, err)

		err = nbClient.SetOVNIPSec(true)
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.True(t, out.Ipsec)
	})

	t.Run("set OVN IPSec when GetNbGlobal fails", func(t *testing.T) {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		err = nbClient.SetOVNIPSec(true)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to get nb global")
		err = nbClient.CreateNbGlobal(nbGlobal)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testSetNbGlobalOptions() {
	t := suite.T()

	nbClient := suite.ovnNBClient

	t.Cleanup(func() {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		_, err = nbClient.GetNbGlobal()
		require.ErrorContains(t, err, "not found nb_global")
	})

	nbGlobal := mockNBGlobal()
	err := nbClient.CreateNbGlobal(nbGlobal)
	require.NoError(t, err)

	t.Run("set new option", func(t *testing.T) {
		err := nbClient.SetNbGlobalOptions("new_option", "new_value")
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "new_value", out.Options["new_option"])
	})

	t.Run("update existing option", func(t *testing.T) {
		err := nbClient.SetNbGlobalOptions("mac_prefix", "aa:bb:cc")
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "aa:bb:cc", out.Options["mac_prefix"])
	})

	t.Run("set option with non-string value", func(t *testing.T) {
		err := nbClient.SetNbGlobalOptions("numeric_option", 42)
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "42", out.Options["numeric_option"])
	})

	t.Run("set option when options map is nil", func(t *testing.T) {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		nbGlobal.Options = nil

		err = nbClient.CreateNbGlobal(nbGlobal)
		require.NoError(t, err)

		err = nbClient.SetNbGlobalOptions("new_option", "new_value")
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "new_value", out.Options["new_option"])
	})

	t.Run("set option with same value (no update)", func(t *testing.T) {
		err := nbClient.SetNbGlobalOptions("existing_option", "existing_value")
		require.NoError(t, err)

		err = nbClient.SetNbGlobalOptions("existing_option", "existing_value")
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "existing_value", out.Options["existing_option"])
	})

	t.Run("set option when GetNbGlobal fails", func(t *testing.T) {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		err = nbClient.SetNbGlobalOptions("test_option", "test_value")
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to get nb global")

		err = nbClient.CreateNbGlobal(nbGlobal)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testSetLsDnatModDlDst() {
	t := suite.T()

	nbClient := suite.ovnNBClient

	t.Cleanup(func() {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		_, err = nbClient.GetNbGlobal()
		require.ErrorContains(t, err, "not found nb_global")
	})

	nbGlobal := mockNBGlobal()
	err := nbClient.CreateNbGlobal(nbGlobal)
	require.NoError(t, err)

	t.Run("enable ls_dnat_mod_dl_dst", func(t *testing.T) {
		err := nbClient.SetLsDnatModDlDst(true)
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "true", out.Options["ls_dnat_mod_dl_dst"])
	})

	t.Run("disable ls_dnat_mod_dl_dst", func(t *testing.T) {
		err := nbClient.SetLsDnatModDlDst(false)
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "false", out.Options["ls_dnat_mod_dl_dst"])
	})
}

func (suite *OvnClientTestSuite) testSetLsCtSkipDstLportIPs() {
	t := suite.T()

	nbClient := suite.ovnNBClient

	t.Cleanup(func() {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		_, err = nbClient.GetNbGlobal()
		require.ErrorContains(t, err, "not found nb_global")
	})

	nbGlobal := mockNBGlobal()
	err := nbClient.CreateNbGlobal(nbGlobal)
	require.NoError(t, err)

	t.Run("enable ls_ct_skip_dst_lport_ips", func(t *testing.T) {
		err := nbClient.SetLsCtSkipDstLportIPs(true)
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "true", out.Options["ls_ct_skip_dst_lport_ips"])
	})

	t.Run("disable ls_ct_skip_dst_lport_ips", func(t *testing.T) {
		err := nbClient.SetLsCtSkipDstLportIPs(false)
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "false", out.Options["ls_ct_skip_dst_lport_ips"])
	})
}

func (suite *OvnClientTestSuite) testSetNodeLocalDNSIP() {
	t := suite.T()

	nbClient := suite.ovnNBClient

	t.Cleanup(func() {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		_, err = nbClient.GetNbGlobal()
		require.ErrorContains(t, err, "not found nb_global")
	})

	nbGlobal := mockNBGlobal()
	err := nbClient.CreateNbGlobal(nbGlobal)
	require.NoError(t, err)

	t.Run("set node local DNS IP", func(t *testing.T) {
		err := nbClient.SetNodeLocalDNSIP("192.168.0.10")
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "192.168.0.10", out.Options["node_local_dns_ip"])
	})

	t.Run("update existing node local DNS IP", func(t *testing.T) {
		err := nbClient.SetNodeLocalDNSIP("192.168.0.20")
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "192.168.0.20", out.Options["node_local_dns_ip"])
	})

	t.Run("remove node local DNS IP", func(t *testing.T) {
		err := nbClient.SetNodeLocalDNSIP("")
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		_, exists := out.Options["node_local_dns_ip"]
		require.False(t, exists)
	})

	t.Run("set node local DNS IP when options is nil", func(t *testing.T) {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		nbGlobal.Options = nil
		err = nbClient.CreateNbGlobal(nbGlobal)
		require.NoError(t, err)

		err = nbClient.SetNodeLocalDNSIP("192.168.0.30")
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "192.168.0.30", out.Options["node_local_dns_ip"])
	})

	t.Run("remove non-existent node local DNS IP", func(t *testing.T) {
		err := nbClient.SetNodeLocalDNSIP("")
		require.NoError(t, err)

		out, err := nbClient.GetNbGlobal()
		require.NoError(t, err)
		_, exists := out.Options["node_local_dns_ip"]
		require.False(t, exists)
	})

	t.Run("set node local DNS IP when GetNbGlobal fails", func(t *testing.T) {
		err := nbClient.DeleteNbGlobal()
		require.NoError(t, err)

		err = nbClient.SetNodeLocalDNSIP("")
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to get nb global")

		err = nbClient.CreateNbGlobal(nbGlobal)
		require.NoError(t, err)
	})
}
