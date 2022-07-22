package ovs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func mockNBGlobal() *ovnnb.NBGlobal {
	return &ovnnb.NBGlobal{
		Connections: []string{"c7744628-6399-4852-8ac0-06e4e436c146"},
		NbCfg:       100,
		Options: map[string]string{
			"mac_prefix": "11:22:33",
			"max_tunid":  "16711680",
		},
	}
}

func (suite *OvnClientTestSuite) testGetNbGlobal() {
	t := suite.T()

	ovnClient := suite.ovnClient

	t.Cleanup(func() {
		err := ovnClient.DeleteNbGlobal()
		require.NoError(t, err)

		_, err = ovnClient.GetNbGlobal()
		require.ErrorContains(t, err, "not found nb_global")
	})

	t.Run("return err when not found nb_global", func(t *testing.T) {
		_, err := ovnClient.GetNbGlobal()
		require.ErrorContains(t, err, "not found nb_global")
	})

	t.Run("no err when found nb_global", func(t *testing.T) {
		nbGlobal := mockNBGlobal()
		err := ovnClient.CreateNbGlobal(nbGlobal)
		require.NoError(t, err)

		out, err := ovnClient.GetNbGlobal()
		require.NoError(t, err)
		require.NotEmpty(t, out.UUID)
	})
}

func (suite *OvnClientTestSuite) testUpdateNbGlobal() {
	t := suite.T()

	ovnClient := suite.ovnClient
	name := "test-nb-global-update"

	t.Cleanup(func() {
		err := ovnClient.DeleteNbGlobal()
		require.NoError(t, err)

		_, err = ovnClient.GetNbGlobal()
		require.ErrorContains(t, err, "not found nb_global")
	})

	nbGlobal := mockNBGlobal()
	err := ovnClient.CreateNbGlobal(nbGlobal)
	require.NoError(t, err)

	update := &ovnnb.NBGlobal{Name: name, Options: map[string]string{
		"mac_prefix": "11:22:aa",
		"max_tunid":  "16711680",
	}}

	err = ovnClient.UpdateNbGlobal(update)
	require.NoError(t, err)

	out, err := ovnClient.GetNbGlobal()
	require.NoError(t, err)
	require.Equal(t, "11:22:aa", out.Options["mac_prefix"])
	require.Equal(t, "16711680", out.Options["max_tunid"])
}

func (suite *OvnClientTestSuite) testSetICAutoRoute() {
	t := suite.T()

	ovnClient := suite.ovnClient

	t.Cleanup(func() {
		err := ovnClient.DeleteNbGlobal()
		require.NoError(t, err)

		_, err = ovnClient.GetNbGlobal()
		require.ErrorContains(t, err, "not found nb_global")
	})

	nbGlobal := mockNBGlobal()
	err := ovnClient.CreateNbGlobal(nbGlobal)
	require.NoError(t, err)

	t.Run("enable ovn-ic auto route", func(t *testing.T) {
		err = ovnClient.SetICAutoRoute(true, []string{"1.1.1.1", "2.2.2.2"})
		require.NoError(t, err)

		out, err := ovnClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "true", out.Options["ic-route-adv"])
		require.Equal(t, "true", out.Options["ic-route-learn"])
		require.Equal(t, "1.1.1.1,2.2.2.2", out.Options["ic-route-blacklist"])
	})

	t.Run("disable ovn-ic auto route", func(t *testing.T) {
		err = ovnClient.SetICAutoRoute(false, []string{"1.1.1.1", "2.2.2.2"})
		require.NoError(t, err)

		out, err := ovnClient.GetNbGlobal()
		require.NoError(t, err)
		require.Equal(t, "false", out.Options["ic-route-adv"])
		require.Equal(t, "false", out.Options["ic-route-learn"])
		require.Empty(t, out.Options["ic-route-blacklist"])
	})

}
