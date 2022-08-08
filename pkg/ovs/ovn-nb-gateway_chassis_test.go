package ovs

import (
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (suite *OvnClientTestSuite) testCreateGatewayChassis() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	chassis := "c7efec70-9519-4b03-8b67-057f2a95e5c7"
	name := "test-create-gateway-chassis" + "-" + chassis

	gwChassis := &ovnnb.GatewayChassis{
		Name:        name,
		ChassisName: chassis,
		Priority:    50,
	}
	err := ovnClient.CreateGatewayChassis(gwChassis)
	require.NoError(t, err)

	gwChassis, err = ovnClient.GetGatewayChassis(name, false)
	require.NoError(t, err)
	require.NotEmpty(t, gwChassis.UUID)
	require.Equal(t, name, gwChassis.Name)
	require.Equal(t, chassis, gwChassis.ChassisName)
	require.Equal(t, 50, gwChassis.Priority)
}

func (suite *OvnClientTestSuite) testCreateGatewayChassises() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	chassises := []string{"c7efec70-9519-4b03-8b67-057f2a95e5c7", "4a0891b6-fe81-4986-a367-aad0ea7ca9f3", "dcc2eda3-b3ea-4d53-afe0-7b6eaf7917ba"}
	lrpName := "test-create-gateway-chassises"

	err := ovnClient.CreateGatewayChassises(lrpName, chassises)
	require.NoError(t, err)

	for i, chassisName := range chassises {
		gwChassisName := lrpName + "-" + chassisName
		gwChassis, err := ovnClient.GetGatewayChassis(gwChassisName, false)
		require.NoError(t, err)
		require.NotEmpty(t, gwChassis.UUID)
		require.Equal(t, gwChassisName, gwChassis.Name)
		require.Equal(t, chassisName, gwChassis.ChassisName)
		require.Equal(t, 100-i, gwChassis.Priority)
	}
}
