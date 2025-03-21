package ovs

import (
	"testing"

	"k8s.io/utils/ptr"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (suite *OvnClientTestSuite) testCreateChassisTemplateVar() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	t.Run("create ChassisTemplateVar", func(t *testing.T) {
		t.Parallel()

		node := "test-create-ctv-node"
		chassis := "test-create-ctv-chassis"
		variables := map[string]string{"k1": "v1", "k2": "v2"}

		err := nbClient.CreateChassisTemplateVar(node, chassis, variables)
		require.NoError(t, err)

		ctvList, err := nbClient.ListChassisTemplateVar()
		require.NoError(t, err)
		require.NotEmpty(t, ctvList)
		require.NotEmpty(t, ctvList[0].UUID)
		require.NotEmpty(t, ctvList[0].ExternalIDs)

		err = nbClient.CreateChassisTemplateVar(node, chassis, variables)
		require.NoError(t, err)

		ctvList, err = nbClient.ListChassisTemplateVar()
		require.NoError(t, err)
		require.NotEmpty(t, ctvList)
		require.NotEmpty(t, ctvList[0].UUID)
		require.NotEmpty(t, ctvList[0].ExternalIDs)
	})

	t.Run("create ChassisTemplateVar with empty variables", func(t *testing.T) {
		t.Parallel()

		node := "test-create-ctv-with-empty-variables-node"
		chassis := "test-create-ctv-with-empty-variables-chassis"

		err := nbClient.CreateChassisTemplateVar(node, chassis, nil)
		require.NoError(t, err)

		ctvList, err := nbClient.ListChassisTemplateVar()
		require.NoError(t, err)
		require.NotEmpty(t, ctvList)
		require.NotEmpty(t, ctvList[0].UUID)
		require.NotEmpty(t, ctvList[0].ExternalIDs)
	})

	t.Run("create ChassisTemplateVar with empty value", func(t *testing.T) {
		t.Parallel()

		node := "test-create-ctv-with-empty-value-node"
		chassis := "test-create-ctv-with-empty-value-chassis"
		variables := map[string]string{"k1": "v1", "k2": "", "k3": "v3"}

		err := nbClient.CreateChassisTemplateVar(node, chassis, variables)
		require.NoError(t, err)

		ctvList, err := nbClient.ListChassisTemplateVar()
		require.NoError(t, err)
		require.NotEmpty(t, ctvList)
		require.NotEmpty(t, ctvList[0].UUID)
		require.NotEmpty(t, ctvList[0].ExternalIDs)
	})
}

func (suite *OvnClientTestSuite) testGetChassisTemplateVar() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	t.Run("get ChassisTemplateVar", func(t *testing.T) {
		t.Parallel()

		node := "test-get-ctv-node"
		chassis := "test-get-ctv-chassis"
		variables := map[string]string{"k1": "v1", "k2": "", "k3": "v3"}

		err := nbClient.CreateChassisTemplateVar(node, chassis, variables)
		require.NoError(t, err)

		ctv, err := nbClient.GetChassisTemplateVar(chassis, true)
		require.NoError(t, err)
		require.NotNil(t, ctv)
		require.NotEmpty(t, ctv.UUID)
		require.Equal(t, chassis, ctv.Chassis)
		require.Equal(t, map[string]string{"k1": "v1", "k2": "", "k3": "v3"}, ctv.Variables)
		require.Equal(t, map[string]string{"vendor": util.CniTypeName, "node": node}, ctv.ExternalIDs)

		ctv, err = nbClient.GetChassisTemplateVar(chassis, false)
		require.NoError(t, err)
		require.NotNil(t, ctv)
		require.NotEmpty(t, ctv.UUID)
		require.Equal(t, chassis, ctv.Chassis)
		require.Equal(t, map[string]string{"k1": "v1", "k2": "", "k3": "v3"}, ctv.Variables)
		require.Equal(t, map[string]string{"vendor": util.CniTypeName, "node": node}, ctv.ExternalIDs)
	})

	t.Run("get non-existent ChassisTemplateVar", func(t *testing.T) {
		t.Parallel()

		chassis := "non-existent-chassis"
		ctv, err := nbClient.GetChassisTemplateVar(chassis, true)
		require.NoError(t, err)
		require.Nil(t, ctv)

		ctv, err = nbClient.GetChassisTemplateVar(chassis, false)
		require.Error(t, err)
		require.Nil(t, ctv)
	})
}

func (suite *OvnClientTestSuite) testGetChassisTemplateVarByNodeName() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	t.Run("get ChassisTemplateVar by node name", func(t *testing.T) {
		t.Parallel()

		node := "test-get-ctv-by-node-name-node"
		chassis := "test-get-ctv-by-node-name-chassis"
		variables := map[string]string{"k1": "v1", "k2": "v2", "k3": ""}

		err := nbClient.CreateChassisTemplateVar(node, chassis, variables)
		require.NoError(t, err)

		ctv, err := nbClient.GetChassisTemplateVarByNodeName(node, true)
		require.NoError(t, err)
		require.NotNil(t, ctv)
		require.NotEmpty(t, ctv.UUID)
		require.Equal(t, chassis, ctv.Chassis)
		require.Equal(t, map[string]string{"k1": "v1", "k2": "v2", "k3": ""}, ctv.Variables)
		require.Equal(t, map[string]string{"vendor": util.CniTypeName, "node": node}, ctv.ExternalIDs)

		ctv2, err := nbClient.GetChassisTemplateVarByNodeName(node, false)
		require.NoError(t, err)
		require.NotNil(t, ctv2)
		require.Equal(t, ctv, ctv2)
	})

	t.Run("get non-existent ChassisTemplateVar by node name", func(t *testing.T) {
		t.Parallel()

		node := "non-existent-node"
		ctv, err := nbClient.GetChassisTemplateVar(node, true)
		require.NoError(t, err)
		require.Nil(t, ctv)

		ctv, err = nbClient.GetChassisTemplateVar(node, false)
		require.Error(t, err)
		require.Nil(t, ctv)
	})
}

func (suite *OvnClientTestSuite) testUpdateChassisTemplateVar() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	t.Run("update ChassisTemplateVar", func(t *testing.T) {
		t.Parallel()

		node := "test-update-ctv-node"
		chassis := "test-update-ctv-chassis"
		variables := map[string]string{"k1": "v1", "k2": "v2", "k3": "", "k4": ""}

		err := nbClient.CreateChassisTemplateVar(node, chassis, variables)
		require.NoError(t, err)

		ctv, err := nbClient.GetChassisTemplateVarByNodeName(node, true)
		require.NoError(t, err)
		require.NotNil(t, ctv)
		require.NotEmpty(t, ctv.UUID)
		require.Equal(t, chassis, ctv.Chassis)
		require.Equal(t, map[string]string{"k1": "v1", "k2": "v2", "k3": "", "k4": ""}, ctv.Variables)
		require.Equal(t, map[string]string{"vendor": util.CniTypeName, "node": node}, ctv.ExternalIDs)

		updatedVars := map[string]*string{"k1": ptr.To("v11"), "k2": nil, "k3": ptr.To("v3"), "k4": nil, "k5": ptr.To("v5"), "k6": nil}
		err = nbClient.UpdateChassisTemplateVar(node, chassis, updatedVars)
		require.NoError(t, err)

		ctv2, err := nbClient.GetChassisTemplateVarByNodeName(node, true)
		require.NoError(t, err)
		require.NotNil(t, ctv2)
		require.Equal(t, ctv.UUID, ctv2.UUID)
		require.Equal(t, chassis, ctv.Chassis)
		require.Equal(t, map[string]string{"k1": "v11", "k3": "v3", "k5": "v5"}, ctv2.Variables)
		require.Equal(t, ctv.ExternalIDs, ctv2.ExternalIDs)
	})

	t.Run("update non-existent ChassisTemplateVar", func(t *testing.T) {
		t.Parallel()

		node := "test-update-non-existent-ctv-node"
		chassis := "test-update-non-existent-ctv-chassis"
		variables := map[string]*string{"k1": ptr.To("v1"), "k2": ptr.To(""), "k3": nil}

		err := nbClient.UpdateChassisTemplateVar(node, chassis, variables)
		require.NoError(t, err)

		ctv, err := nbClient.GetChassisTemplateVarByNodeName(node, true)
		require.NoError(t, err)
		require.NotNil(t, ctv)
		require.NotEmpty(t, ctv.UUID)
		require.Equal(t, chassis, ctv.Chassis)
		require.Equal(t, map[string]string{"k1": "v1", "k2": ""}, ctv.Variables)
		require.Equal(t, map[string]string{"vendor": util.CniTypeName, "node": node}, ctv.ExternalIDs)
	})
}

func (suite *OvnClientTestSuite) testUpdateChassisTemplateVarVariables() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	t.Run("test UpdateChassisTemplateVarVariables", func(t *testing.T) {
		t.Parallel()

		nodes := []string{"test-update-ctv-variables-node1", "test-update-ctv-variables-node2", "test-update-ctv-variables-node3"}
		chassises := []string{"test-update-ctv-variables-chassis1", "test-update-ctv-variables-chassis2", "test-update-ctv-variables-chassis3"}
		variables := []map[string]string{{"k1": "v1", "k2": ""}, {"k1": "", "k2": "v2"}, {"k2": "v22"}}
		// set k1 to empty in the first node, update k1 in the second node, add k1 in the third node
		nodeValues := map[string]string{nodes[1]: "v1", nodes[2]: "v11"}
		want := []map[string]string{{"k1": "", "k2": ""}, {"k1": "v1", "k2": "v2"}, {"k1": "v11", "k2": "v22"}}

		for i := range nodes {
			err := nbClient.CreateChassisTemplateVar(nodes[i], chassises[i], variables[i])
			require.NoError(t, err)
		}

		err := nbClient.UpdateChassisTemplateVarVariables("k1", nodeValues)
		require.NoError(t, err)

		for i := range nodes {
			ctv, err := nbClient.GetChassisTemplateVarByNodeName(nodes[i], true)
			require.NoError(t, err)
			require.NotNil(t, ctv)
			require.NotEmpty(t, ctv.UUID)
			require.Equal(t, chassises[i], ctv.Chassis)
			require.Equal(t, want[i], ctv.Variables)
			require.Equal(t, map[string]string{"vendor": util.CniTypeName, "node": nodes[i]}, ctv.ExternalIDs)
		}
	})
}

func (suite *OvnClientTestSuite) testDeleteChassisTemplateVarVariables() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	t.Run("test DeleteChassisTemplateVarVariables", func(t *testing.T) {
		t.Parallel()

		nodes := []string{"test-delete-ctv-variables-node1", "test-delete-ctv-variables-node2", "test-delete-ctv-variables-node3"}
		chassises := []string{"test-delete-ctv-variables-chassis1", "test-delete-ctv-variables-chassis2", "test-delete-ctv-variables-chassis3"}
		variables := []map[string]string{{"k1": "v1", "k2": ""}, {"k1": "", "k2": "v2"}, {"k2": "v22"}}
		// set k1 to empty in the first node, update k1 in the second node, add k1 in the third node
		deletedVariables := "k1"
		want := []map[string]string{{"k2": ""}, {"k2": "v2"}, {"k2": "v22"}}

		for i := range nodes {
			err := nbClient.CreateChassisTemplateVar(nodes[i], chassises[i], variables[i])
			require.NoError(t, err)
		}

		err := nbClient.DeleteChassisTemplateVarVariables(deletedVariables)
		require.NoError(t, err)

		for i := range nodes {
			ctv, err := nbClient.GetChassisTemplateVarByNodeName(nodes[i], true)
			require.NoError(t, err)
			require.NotNil(t, ctv)
			require.NotEmpty(t, ctv.UUID)
			require.Equal(t, chassises[i], ctv.Chassis)
			require.Equal(t, want[i], ctv.Variables)
			require.Equal(t, map[string]string{"vendor": util.CniTypeName, "node": nodes[i]}, ctv.ExternalIDs)
		}
	})
}

func (suite *OvnClientTestSuite) testDeleteChassisTemplateVar() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	t.Run("delete ChassisTemplateVar", func(t *testing.T) {
		t.Parallel()

		node := "test-delete-ctv-node"
		chassis := "test-delete-ctv-chassis"
		variables := map[string]string{"k1": "v1", "k2": "v2", "k3": ""}

		err := nbClient.CreateChassisTemplateVar(node, chassis, variables)
		require.NoError(t, err)

		ctv, err := nbClient.GetChassisTemplateVarByNodeName(node, true)
		require.NoError(t, err)
		require.NotNil(t, ctv)
		require.NotEmpty(t, ctv.UUID)
		require.Equal(t, chassis, ctv.Chassis)
		require.Equal(t, map[string]string{"k1": "v1", "k2": "v2", "k3": ""}, ctv.Variables)
		require.Equal(t, map[string]string{"vendor": util.CniTypeName, "node": node}, ctv.ExternalIDs)

		err = nbClient.DeleteChassisTemplateVar(chassis)
		require.NoError(t, err)

		ctv, err = nbClient.GetChassisTemplateVarByNodeName(node, true)
		require.NoError(t, err)
		require.Nil(t, ctv)
	})

	t.Run("delete non-existent ChassisTemplateVar", func(t *testing.T) {
		t.Parallel()

		chassis := "non-existent-ctv-chassis"
		err := nbClient.DeleteChassisTemplateVar(chassis)
		require.NoError(t, err)

		ctv, err := nbClient.GetChassisTemplateVar(chassis, true)
		require.NoError(t, err)
		require.Nil(t, ctv)
	})
}

func (suite *OvnClientTestSuite) testDeleteChassisTemplateVarByNodeName() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	t.Run("delete ChassisTemplateVar by node name", func(t *testing.T) {
		t.Parallel()

		node := "test-delete-ctv-by-node-name-node"
		chassis := "test-delete-ctv-by-node-name-chassis"
		variables := map[string]string{"k1": "v1", "k2": "v2", "k3": ""}

		err := nbClient.CreateChassisTemplateVar(node, chassis, variables)
		require.NoError(t, err)

		ctv, err := nbClient.GetChassisTemplateVarByNodeName(node, true)
		require.NoError(t, err)
		require.NotNil(t, ctv)
		require.NotEmpty(t, ctv.UUID)
		require.Equal(t, chassis, ctv.Chassis)
		require.Equal(t, map[string]string{"k1": "v1", "k2": "v2", "k3": ""}, ctv.Variables)
		require.Equal(t, map[string]string{"vendor": util.CniTypeName, "node": node}, ctv.ExternalIDs)

		err = nbClient.DeleteChassisTemplateVarByNodeName(node)
		require.NoError(t, err)

		ctv, err = nbClient.GetChassisTemplateVarByNodeName(node, true)
		require.NoError(t, err)
		require.Nil(t, ctv)
	})

	t.Run("delete non-existent ChassisTemplateVar", func(t *testing.T) {
		t.Parallel()

		node := "non-existent-ctv-node"
		err := nbClient.DeleteChassisTemplateVarByNodeName(node)
		require.NoError(t, err)

		ctv, err := nbClient.GetChassisTemplateVarByNodeName(node, true)
		require.NoError(t, err)
		require.Nil(t, ctv)
	})
}
