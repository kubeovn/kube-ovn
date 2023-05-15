package ovs

import (
	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (suite *OvnClientTestSuite) testCreateGatewayChassises() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	chassises := []string{"c7efec70-9519-4b03-8b67-057f2a95e5c7", "4a0891b6-fe81-4986-a367-aad0ea7ca9f3", "dcc2eda3-b3ea-4d53-afe0-7b6eaf7917ba"}

	lrp := &ovnnb.LogicalRouterPort{
		UUID: ovsclient.NamedUUID(),
		Name: "test-create-gateway-chassises",
		ExternalIDs: map[string]string{
			"vendor": util.CniTypeName,
		},
	}

	err := createLogicalRouterPort(ovnClient, lrp)
	require.NoError(t, err)

	err = ovnClient.CreateGatewayChassises(lrp.Name, chassises...)
	require.NoError(t, err)

	lrp, err = ovnClient.GetLogicalRouterPort(lrp.Name, false)
	require.NoError(t, err)

	for i, chassisName := range chassises {
		gwChassisName := lrp.Name + "-" + chassisName
		gwChassis, err := ovnClient.GetGatewayChassis(gwChassisName, false)
		require.NoError(t, err)
		require.Equal(t, gwChassisName, gwChassis.Name)
		require.Equal(t, chassisName, gwChassis.ChassisName)
		require.Equal(t, 100-i, gwChassis.Priority)
		require.Contains(t, lrp.GatewayChassis, gwChassis.UUID)
	}
}

func (suite *OvnClientTestSuite) testDeleteGatewayChassises() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrpName := "test-gateway-chassis-del-lrp"
	chassises := []string{"ea8368a0-28cd-4549-9da5-a7ea67262619", "b25ffb94-8b32-4c7e-b5b0-0f343bf6bdd8", "62265268-8af7-4b36-a550-ab5ad38375e3"}

	lrp := &ovnnb.LogicalRouterPort{
		UUID: ovsclient.NamedUUID(),
		Name: lrpName,
		ExternalIDs: map[string]string{
			"vendor": util.CniTypeName,
		},
	}

	err := createLogicalRouterPort(ovnClient, lrp)
	require.NoError(t, err)

	err = ovnClient.CreateGatewayChassises(lrpName, chassises...)
	require.NoError(t, err)

	err = ovnClient.DeleteGatewayChassises(lrpName, append(chassises, "73bbe5d4-2b9b-47d0-aba8-94e86941881a"))
	require.NoError(t, err)

	for _, chassisName := range chassises {
		gwChassisName := lrpName + "-" + chassisName
		_, err := ovnClient.GetGatewayChassis(gwChassisName, false)
		require.ErrorContains(t, err, "object not found")
	}
}

func (suite *OvnClientTestSuite) testDeleteGatewayChassisOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrpName := "test-gateway-chassis-del-op-lrp"
	chassis := "6c322ce8-02b7-42b3-925b-ae24020272a9"
	gwChassisName := lrpName + "-" + chassis

	lrp := &ovnnb.LogicalRouterPort{
		UUID: ovsclient.NamedUUID(),
		Name: lrpName,
		ExternalIDs: map[string]string{
			"vendor": util.CniTypeName,
		},
	}

	err := createLogicalRouterPort(ovnClient, lrp)
	require.NoError(t, err)

	err = ovnClient.CreateGatewayChassises(lrpName, chassis)
	require.NoError(t, err)

	gwChassis, err := ovnClient.GetGatewayChassis(gwChassisName, false)
	require.NoError(t, err)

	ops, err := ovnClient.DeleteGatewayChassisOp(gwChassisName)
	require.NoError(t, err)
	require.Len(t, ops, 1)

	require.Equal(t,
		ovsdb.Operation{
			Op:    "delete",
			Table: "Gateway_Chassis",
			Where: []ovsdb.Condition{
				{
					Column:   "_uuid",
					Function: "==",
					Value: ovsdb.UUID{
						GoUUID: gwChassis.UUID,
					},
				},
			},
		}, ops[0])
}
