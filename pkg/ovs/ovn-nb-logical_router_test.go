package ovs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (suite *OvnClientTestSuite) testGetLogicalRouter() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	name := "test-get-lr"

	err := ovnClient.CreateLogicalRouter(name)
	require.NoError(t, err)
	t.Cleanup(func() {
		err = ovnClient.DeleteLogicalRouter(name)
		require.NoError(t, err)
	})

	t.Run("should return no err when found logical router", func(t *testing.T) {
		lr, err := ovnClient.GetLogicalRouter(name, false)
		require.NoError(t, err)
		require.Equal(t, name, lr.Name)
		require.NotEmpty(t, lr.UUID)
	})

	t.Run("should return err when not found logical router", func(t *testing.T) {
		_, err := ovnClient.GetLogicalRouter("test-get-lr-non-existent", false)
		require.ErrorContains(t, err, "not found logical router")
	})

	t.Run("no err when not found logical router and ignoreNotFound is true", func(t *testing.T) {
		_, err := ovnClient.GetLogicalRouter("test-get-lr-non-existent", true)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testCreateLogicalRouter() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	name := "test-create-lr"

	t.Cleanup(func() {
		err := ovnClient.DeleteLogicalRouter(name)
		require.NoError(t, err)
	})

	err := ovnClient.CreateLogicalRouter(name)
	require.NoError(t, err)

	lr, err := ovnClient.GetLogicalRouter(name, false)
	require.NoError(t, err)
	require.Equal(t, name, lr.Name)
	require.NotEmpty(t, lr.UUID)
	require.Equal(t, util.CniTypeName, lr.ExternalIDs["vendor"])
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouter() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	name := "test-delete-lr"

	t.Run("no err when delete existent logical router", func(t *testing.T) {
		t.Parallel()
		err := ovnClient.CreateLogicalRouter(name)
		require.NoError(t, err)

		err = ovnClient.DeleteLogicalRouter(name)
		require.NoError(t, err)
	})

	t.Run("no err when delete non-existent logical router", func(t *testing.T) {
		t.Parallel()
		err := ovnClient.DeleteLogicalRouter("test-delete-lr-non-existent")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testListLogicalRouter() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	namePrefix := "test-list-lr"

	names := make([]string, 3)
	for i := 0; i < 3; i++ {
		names[i] = fmt.Sprintf("%s-%d", namePrefix, i)
		err := ovnClient.CreateLogicalRouter(names[i])
		require.NoError(t, err)
	}

	t.Cleanup(func() {
		for _, lr := range names {
			err := ovnClient.DeleteLogicalRouter(lr)
			require.NoError(t, err)
		}
	})

	t.Run("return all logical router which match vendor", func(t *testing.T) {
		t.Parallel()
		lrs, err := ovnClient.ListLogicalRouter(true)
		require.NoError(t, err)

		for _, lr := range lrs {
			if !strings.Contains(lr.Name, namePrefix) {
				continue
			}
			require.Contains(t, names, lr.Name)
		}
	})
}

func (suite *OvnClientTestSuite) testLogicalRouterAddPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-add-port-lr"
	lrpName := "test-add-port-lrp"

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	lrp := &ovnnb.LogicalRouterPort{
		Name:     lrpName,
		MAC:      "00:11:22:37:af:89",
		Networks: []string{"192.168.131.1/24"},
	}
	err = ovnClient.CreateLogicalRouterPort(lrp)
	require.NoError(t, err)

	t.Cleanup(func() {
		err = ovnClient.DeleteLogicalRouter(lrName)
		require.NoError(t, err)
		err = ovnClient.DeleteLogicalRouterPort(lrpName)
		require.NoError(t, err)
	})

	t.Run("add new port to logical router", func(t *testing.T) {
		t.Parallel()
		err := ovnClient.LogicalRouterAddPort(lrName, lrpName)
		require.NoError(t, err)

		// no err when add port repeatedly
		err = ovnClient.LogicalRouterAddPort(lrName, lrpName)
		require.NoError(t, err)

		lrp, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
	})

	t.Run("should return err when logical router does not exist", func(t *testing.T) {
		t.Parallel()
		err := ovnClient.LogicalRouterAddPort("test-add-port-lr-non-existent", lrpName)
		require.ErrorContains(t, err, "not found logical router")
	})

	t.Run("should return err when logical router port does not exist", func(t *testing.T) {
		t.Parallel()
		err := ovnClient.LogicalRouterAddPort(lrName, "test-add-port-lrp-non-existent")
		require.ErrorContains(t, err, "object not found")
	})
}
