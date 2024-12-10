package ovs

import (
	"testing"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"

	"github.com/stretchr/testify/require"
)

func newChassis(nbcfg int, hostname, name string, encaps, transportZones, vtepLogicalSwitches []string, externalIDs, otherConfig map[string]string) *ovnsb.Chassis {
	return &ovnsb.Chassis{
		UUID:                ovsclient.NamedUUID(),
		Encaps:              encaps,
		ExternalIDs:         externalIDs,
		Hostname:            hostname,
		Name:                name,
		NbCfg:               nbcfg,
		OtherConfig:         otherConfig,
		TransportZones:      transportZones,
		VtepLogicalSwitches: vtepLogicalSwitches,
	}
}

func (suite *OvnClientTestSuite) testGetChassis() {
	t := suite.T()
	t.Parallel()

	sbClient := suite.ovnSBClient

	t.Cleanup(func() {
		err := sbClient.DeleteChassis("chassis-name-1")
		require.NoError(t, err)
	})

	chassis := newChassis(0, "host-name-1", "chassis-name-1", nil, nil, nil, nil, nil)
	ops, err := sbClient.ovsDbClient.Create(chassis)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops)
	require.NoError(t, err)

	t.Run("test get chassis", func(t *testing.T) {
		chassis, err := sbClient.GetChassis("chassis-name-1", false)
		require.NoError(t, err)
		require.NotNil(t, chassis)
	})

	t.Run("test get chassis with empty chassis name", func(t *testing.T) {
		chassis, err := sbClient.GetChassis("", false)
		require.Nil(t, chassis)
		require.ErrorContains(t, err, "chassis name is empty")
	})

	t.Run("test get non-existent chassis with ignoreNotFound true", func(t *testing.T) {
		chassis, err := sbClient.GetChassis("chassis-non-existent", true)
		require.NoError(t, err)
		require.Nil(t, chassis)
	})

	t.Run("test get non-existent chassis with ignoreNotFound false", func(t *testing.T) {
		chassis, err := sbClient.GetChassis("chassis-non-existent", false)
		require.Nil(t, chassis)
		require.ErrorContains(t, err, "failed to get chassis")
	})
}

func (suite *OvnClientTestSuite) testDeleteChassis() {
	t := suite.T()
	t.Parallel()

	sbClient := suite.ovnSBClient

	t.Cleanup(func() {
		err := sbClient.DeleteChassis("chassis-name-2")
		require.NoError(t, err)
	})

	chassis := newChassis(0, "host-name-2", "chassis-name-2", nil, nil, nil, nil, nil)
	ops, err := sbClient.ovsDbClient.Create(chassis)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops)
	require.NoError(t, err)

	t.Run("test delete chassis", func(t *testing.T) {
		chassis, err := sbClient.GetChassis("chassis-name-2", false)
		require.NotNil(t, chassis)
		require.NoError(t, err)

		err = sbClient.DeleteChassis("chassis-name-2")
		require.NoError(t, err)

		chassis, err = sbClient.GetChassis("chassis-name-2", true)
		require.NoError(t, err)
		require.Nil(t, chassis)
	})

	t.Run("test delete chassis with empty chassis name", func(t *testing.T) {
		err := sbClient.DeleteChassis("")
		require.ErrorContains(t, err, "chassis name is empty")
	})

	t.Run("test delete non-existent chassis", func(t *testing.T) {
		err := sbClient.DeleteChassis("chassis-non-existent")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testUpdateChassis() {
	t := suite.T()
	t.Parallel()

	sbClient := suite.ovnSBClient

	t.Cleanup(func() {
		err := sbClient.DeleteChassis("chassis-name-3")
		require.NoError(t, err)
	})

	chassis := newChassis(0, "host-name-3", "chassis-name-3", nil, nil, nil, nil, nil)
	ops, err := sbClient.ovsDbClient.Create(chassis)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops)
	require.NoError(t, err)

	t.Run("test update chassis with valid fields", func(t *testing.T) {
		updatedHostname := "updated-host-name"
		chassis.Hostname = updatedHostname
		err := sbClient.UpdateChassis(chassis, &chassis.Hostname)
		require.NoError(t, err)

		updatedChassis, err := sbClient.GetChassis("chassis-name-3", false)
		require.NoError(t, err)
		require.Equal(t, updatedHostname, updatedChassis.Hostname)
	})

	t.Run("test update chassis with non-existent field", func(t *testing.T) {
		err := sbClient.UpdateChassis(chassis, "NonExistentField")
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to generate update operations for chassis")
	})
}

func (suite *OvnClientTestSuite) testListChassis() {
	t := suite.T()
	t.Parallel()

	sbClient := suite.ovnSBClient

	t.Cleanup(func() {
		err := sbClient.DeleteChassis("chassis-1")
		require.NoError(t, err)
		err = sbClient.DeleteChassis("chassis-2")
		require.NoError(t, err)
	})

	chassis1 := newChassis(0, "host-1", "chassis-1", nil, nil, nil, nil, nil)
	chassis2 := newChassis(0, "host-2", "chassis-2", nil, nil, nil, nil, nil)

	ops1, err := sbClient.ovsDbClient.Create(chassis1)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops1)
	require.NoError(t, err)

	ops2, err := sbClient.ovsDbClient.Create(chassis2)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops2)
	require.NoError(t, err)

	t.Run("test list chassis", func(t *testing.T) {
		chassisList, err := sbClient.ListChassis()
		require.NoError(t, err)
		require.NotNil(t, chassisList)

		names := make(map[string]bool)
		for _, chassis := range *chassisList {
			names[chassis.Name] = true
		}
		require.True(t, names["chassis-1"])
		require.True(t, names["chassis-2"])
	})

	t.Run("test list chassis with no entries", func(t *testing.T) {
		err := sbClient.DeleteChassis("chassis-1")
		require.NoError(t, err)
		err = sbClient.DeleteChassis("chassis-2")
		require.NoError(t, err)

		chassisList, err := sbClient.ListChassis()
		require.NoError(t, err)
		require.NotNil(t, chassisList)
		var names []string
		for _, chassis := range *chassisList {
			names = append(names, chassis.Name)
		}
		require.NotContains(t, names, "chassis-1")
		require.NotContains(t, names, "chassis-2")
	})
}

func (suite *OvnClientTestSuite) testGetChassisByHost() {
	t := suite.T()
	t.Parallel()

	sbClient := suite.ovnSBClient

	t.Cleanup(func() {
		err := sbClient.DeleteChassis("chassis-6")
		require.NoError(t, err)
		err = sbClient.DeleteChassis("chassis-7")
		require.NoError(t, err)
	})

	chassis1 := newChassis(0, "host-6", "chassis-6", nil, nil, nil, nil, nil)
	chassis2 := newChassis(0, "host-7", "chassis-7", nil, nil, nil, nil, nil)

	ops1, err := sbClient.ovsDbClient.Create(chassis1)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops1)
	require.NoError(t, err)

	ops2, err := sbClient.ovsDbClient.Create(chassis2)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops2)
	require.NoError(t, err)

	t.Run("test get chassis by host with valid hostname", func(t *testing.T) {
		chassis, err := sbClient.GetChassisByHost("host-6")
		require.NoError(t, err)
		require.NotNil(t, chassis)
		require.Equal(t, "chassis-6", chassis.Name)
		require.Equal(t, "host-6", chassis.Hostname)
	})

	t.Run("test get chassis by host with non-existent hostname", func(t *testing.T) {
		chassis, err := sbClient.GetChassisByHost("non-existent-host")
		require.Error(t, err)
		require.Nil(t, chassis)
		require.Contains(t, err.Error(), "failed to get Chassis")
	})

	t.Run("test get chassis by host with empty hostname", func(t *testing.T) {
		chassis, err := sbClient.GetChassisByHost("")
		require.Error(t, err)
		require.Nil(t, chassis)
		require.Contains(t, err.Error(), "failed to get Chassis")
	})

	t.Run("test get chassis by host with multiple chassis", func(t *testing.T) {
		chassis3 := newChassis(0, "host-6", "chassis-8", nil, nil, nil, nil, nil)
		ops3, err := sbClient.ovsDbClient.Create(chassis3)
		require.NoError(t, err)
		err = sbClient.Transact("chassis-add", ops3)
		require.NoError(t, err)

		chassis, err := sbClient.GetChassisByHost("host-6")
		require.Error(t, err)
		require.Nil(t, chassis)
		require.Contains(t, err.Error(), "found more than one Chassis")

		err = sbClient.DeleteChassis("chassis-8")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testDeleteChassisByHost() {
	t := suite.T()
	t.Parallel()

	sbClient := suite.ovnSBClient

	t.Cleanup(func() {
		err := sbClient.DeleteChassis("chassis-node1-1")
		require.NoError(t, err)
		err = sbClient.DeleteChassis("chassis-node1-2")
		require.NoError(t, err)
		err = sbClient.DeleteChassis("chassis-node2")
		require.NoError(t, err)
		chassis, err := sbClient.GetChassisByHost("node3")
		require.NoError(t, err)
		ops, err := sbClient.Where(chassis).Delete()
		require.NoError(t, err)
		err = sbClient.Transact("chassis-del", ops)
		require.NoError(t, err)
	})

	chassis1 := newChassis(0, "node1", "chassis-node1-1", nil, nil, nil, nil, nil)
	chassis2 := newChassis(0, "node1", "chassis-node1-2", nil, nil, nil, nil, nil)
	chassis3 := newChassis(0, "", "chassis-node2", nil, nil, nil, map[string]string{"node": "node2"}, nil)
	chassis4 := newChassis(0, "node3", "", nil, nil, nil, map[string]string{"node": "node3"}, nil)

	ops1, err := sbClient.ovsDbClient.Create(chassis1)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops1)
	require.NoError(t, err)

	ops2, err := sbClient.ovsDbClient.Create(chassis2)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops2)
	require.NoError(t, err)

	ops3, err := sbClient.ovsDbClient.Create(chassis3)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops3)
	require.NoError(t, err)

	ops4, err := sbClient.ovsDbClient.Create(chassis4)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops4)
	require.NoError(t, err)

	t.Run("test delete chassis by non-existent host", func(t *testing.T) {
		err := sbClient.DeleteChassisByHost("non-existent-node")
		require.NoError(t, err)
	})

	t.Run("test delete chassis by empty host name", func(t *testing.T) {
		err := sbClient.DeleteChassisByHost("")
		require.NoError(t, err)
	})

	t.Run("test delete chassis by empty chassis name with ExternalIDs", func(t *testing.T) {
		err := sbClient.DeleteChassisByHost("node3")
		require.ErrorContains(t, err, "chassis name is empty")
	})
}

func (suite *OvnClientTestSuite) testUpdateChassisTag() {
	t := suite.T()
	t.Parallel()

	sbClient := suite.ovnSBClient

	t.Cleanup(func() {
		err := sbClient.DeleteChassis("chassis-update-tag")
		require.NoError(t, err)
	})

	chassis := newChassis(0, "host-update-tag", "chassis-update-tag", nil, nil, nil, nil, nil)
	ops, err := sbClient.ovsDbClient.Create(chassis)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops)
	require.NoError(t, err)

	t.Run("test update chassis tag with new node name", func(t *testing.T) {
		err := sbClient.UpdateChassisTag("chassis-update-tag", "new-node-name")
		require.NoError(t, err)

		updatedChassis, err := sbClient.GetChassis("chassis-update-tag", false)
		require.NoError(t, err)
		require.NotNil(t, updatedChassis)
		require.Equal(t, util.CniTypeName, updatedChassis.ExternalIDs["vendor"])
	})

	t.Run("test update chassis tag with existing ExternalIDs", func(t *testing.T) {
		chassis.ExternalIDs = map[string]string{"existing": "value"}
		err := sbClient.UpdateChassis(chassis, &chassis.ExternalIDs)
		require.NoError(t, err)

		err = sbClient.UpdateChassisTag("chassis-update-tag", "another-node-name")
		require.NoError(t, err)

		updatedChassis, err := sbClient.GetChassis("chassis-update-tag", false)
		require.NoError(t, err)
		require.NotNil(t, updatedChassis)
		require.Equal(t, util.CniTypeName, updatedChassis.ExternalIDs["vendor"])
		require.Equal(t, "value", updatedChassis.ExternalIDs["existing"])
	})

	t.Run("test update chassis tag with non-existent chassis", func(t *testing.T) {
		err := sbClient.UpdateChassisTag("non-existent-chassis", "node-name")
		require.Error(t, err)
		require.Contains(t, err.Error(), "fail to get chassis by name=non-existent-chassis")
	})

	t.Run("test update chassis tag with empty chassis name", func(t *testing.T) {
		err := sbClient.UpdateChassisTag("", "node-name")
		require.Error(t, err)
		require.Contains(t, err.Error(), "chassis name is empty")
	})

	t.Run("test update chassis tag with empty node name", func(t *testing.T) {
		err := sbClient.UpdateChassisTag("chassis-update-tag", "")
		require.NoError(t, err)

		updatedChassis, err := sbClient.GetChassis("chassis-update-tag", false)
		require.NoError(t, err)
		require.NotNil(t, updatedChassis)
		require.Equal(t, "", updatedChassis.ExternalIDs["node"])
		require.Equal(t, util.CniTypeName, updatedChassis.ExternalIDs["vendor"])
	})
}

func (suite *OvnClientTestSuite) testGetKubeOvnChassisses() {
	t := suite.T()
	t.Parallel()

	sbClient := suite.ovnSBClient

	kubeOvnChassis1 := newChassis(0, "host-1", "kube-ovn-chassis-1", nil, nil, nil, map[string]string{"vendor": util.CniTypeName}, nil)
	kubeOvnChassis2 := newChassis(0, "host-2", "kube-ovn-chassis-2", nil, nil, nil, map[string]string{"vendor": util.CniTypeName}, nil)
	nonKubeOvnChassis := newChassis(0, "host-none", "non-kube-ovn-chassis", nil, nil, nil, map[string]string{"vendor": "other"}, nil)
	mixedChassis := newChassis(0, "host-4", "mixed-chassis", nil, nil, nil, map[string]string{"vendor": util.CniTypeName, "other": "value"}, nil)

	ops1, err := sbClient.ovsDbClient.Create(kubeOvnChassis1)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops1)
	require.NoError(t, err)

	ops2, err := sbClient.ovsDbClient.Create(kubeOvnChassis2)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops2)
	require.NoError(t, err)

	ops3, err := sbClient.ovsDbClient.Create(nonKubeOvnChassis)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops3)
	require.NoError(t, err)

	ops, err := sbClient.ovsDbClient.Create(mixedChassis)
	require.NoError(t, err)
	err = sbClient.Transact("chassis-add", ops)
	require.NoError(t, err)

	// make sure the chassis created
	chassisList, err := sbClient.GetKubeOvnChassisses()
	require.NoError(t, err)
	require.NotNil(t, *chassisList)

	names := make(map[string]bool)
	for _, chassis := range *chassisList {
		names[chassis.Name] = true
		require.Equal(t, util.CniTypeName, chassis.ExternalIDs["vendor"])
	}
	require.True(t, names["kube-ovn-chassis-1"])
	require.True(t, names["kube-ovn-chassis-2"])
	require.False(t, names["non-kube-ovn-chassis"])
	require.True(t, names["mixed-chassis"])

	t.Cleanup(func() {
		err := sbClient.DeleteChassis("kube-ovn-chassis-1")
		require.NoError(t, err)
		err = sbClient.DeleteChassis("kube-ovn-chassis-2")
		require.NoError(t, err)
		err = sbClient.DeleteChassis("non-kube-ovn-chassis")
		require.NoError(t, err)
		err = sbClient.DeleteChassis("mixed-chassis")
		require.NoError(t, err)
		chassisList, err := sbClient.GetKubeOvnChassisses()
		require.NoError(t, err)
		names := make(map[string]bool)
		for _, chassis := range *chassisList {
			names[chassis.Name] = true
		}
		require.False(t, names["kube-ovn-chassis-1"])
		require.False(t, names["kube-ovn-chassis-2"])
		require.False(t, names["non-kube-ovn-chassis"])
		require.False(t, names["mixed-chassis"])
	})
}
