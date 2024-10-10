package ovs

import (
	"github.com/stretchr/testify/require"
)

func (suite *OvnClientTestSuite) testOvnIcNbCommand() {
	t := suite.T()
	t.Parallel()

	ovnLegacyClient := suite.ovnLegacyClient
	cmd := []string{"--format=csv", "--data=bare", "--no-heading", "--columns=name", "list", "Transit_Switch"}
	output, err := ovnLegacyClient.ovnIcNbCommand(cmd...)
	// ovn-ic-nbctl not found
	// TODO: ic nb db use mock db like nb and sb
	require.Error(t, err)
	require.Empty(t, output)
}

func (suite *OvnClientTestSuite) testGetTsSubnet() {
	t := suite.T()
	t.Parallel()

	ovnLegacyClient := suite.ovnLegacyClient
	subnet, err := ovnLegacyClient.GetTsSubnet("ts1")
	// ovn-ic-nbctl not found
	// TODO: ic nb db use mock db like nb and sb
	require.Error(t, err)
	require.Empty(t, subnet)
}

func (suite *OvnClientTestSuite) testGetTs() {
	t := suite.T()
	t.Parallel()

	ovnLegacyClient := suite.ovnLegacyClient
	ts, err := ovnLegacyClient.GetTs()
	// ovn-ic-nbctl not found
	require.Error(t, err)
	require.Empty(t, ts)
}
