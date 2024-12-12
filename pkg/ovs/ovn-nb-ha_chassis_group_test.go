package ovs

import (
	"context"
	"slices"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/uuid"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (suite *OvnClientTestSuite) testCreateHAChassisGroup() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	name := "test-create-ha-chassis-group"
	chassises := []string{string(uuid.NewUUID()), string(uuid.NewUUID()), string(uuid.NewUUID())}

	err := nbClient.CreateHAChassisGroup(name, chassises, map[string]string{"k1": "v1"})
	require.NoError(t, err)

	group, err := nbClient.GetHAChassisGroup(name, false)
	require.NoError(t, err)
	require.NotNil(t, group)
	require.Equal(t, group.ExternalIDs, map[string]string{"vendor": util.CniTypeName, "k1": "v1"})
	require.Len(t, group.HaChassis, len(chassises))
	for _, uuid := range group.HaChassis {
		chassis := &ovnnb.HAChassis{UUID: uuid}
		err = nbClient.Get(context.Background(), chassis)
		require.NoError(t, err)
		require.Contains(t, chassises, chassis.ChassisName)
		require.Equal(t, chassis.Priority, 100-slices.Index(chassises, chassis.ChassisName))
		require.Equal(t, chassis.ExternalIDs, map[string]string{"group": name, "vendor": util.CniTypeName})
	}

	// update the ha chassis group
	chassises[1], chassises[2] = chassises[2], string(uuid.NewUUID())
	err = nbClient.CreateHAChassisGroup(name, chassises, map[string]string{"k2": "v2"})
	require.NoError(t, err)

	group, err = nbClient.GetHAChassisGroup(name, false)
	require.NoError(t, err)
	require.NotNil(t, group)
	require.Equal(t, group.ExternalIDs, map[string]string{"vendor": util.CniTypeName, "k2": "v2"})
	require.Len(t, group.HaChassis, len(chassises))
	for _, uuid := range group.HaChassis {
		chassis := &ovnnb.HAChassis{UUID: uuid}
		err = nbClient.Get(context.Background(), chassis)
		require.NoError(t, err)
		require.Contains(t, chassises, chassis.ChassisName)
		require.Equal(t, chassis.Priority, 100-slices.Index(chassises, chassis.ChassisName))
		require.Equal(t, chassis.ExternalIDs, map[string]string{"group": name, "vendor": util.CniTypeName})
	}
}

func (suite *OvnClientTestSuite) testGetHAChassisGroup() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	name := "non-existent-ha-chassis-group"

	group, err := nbClient.GetHAChassisGroup(name, true)
	require.NoError(t, err)
	require.Nil(t, group)

	group, err = nbClient.GetHAChassisGroup(name, false)
	require.Error(t, err)
	require.Nil(t, group)
}

func (suite *OvnClientTestSuite) testDeleteHAChassisGroup() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	name := "test-delete-ha-chassis-group"
	chassises := []string{string(uuid.NewUUID()), string(uuid.NewUUID()), string(uuid.NewUUID())}

	err := nbClient.CreateHAChassisGroup(name, chassises, nil)
	require.NoError(t, err)

	group, err := nbClient.GetHAChassisGroup(name, false)
	require.NoError(t, err)
	require.NotNil(t, group)

	err = nbClient.DeleteHAChassisGroup(name)
	require.NoError(t, err)

	group, err = nbClient.GetHAChassisGroup(name, true)
	require.NoError(t, err)
	require.Nil(t, group)
}
