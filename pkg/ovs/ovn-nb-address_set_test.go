package ovs

import (
	"fmt"
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/stretchr/testify/require"
)

func newAddressSet(name string, externalIDs map[string]string) *ovnnb.AddressSet {
	return &ovnnb.AddressSet{
		Name:        name,
		ExternalIDs: externalIDs,
	}
}

func (suite *OvnClientTestSuite) testCreateAddressSet() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	asName := "test_create_as"

	t.Run("create address set", func(t *testing.T) {
		err := ovnClient.CreateAddressSet(asName, map[string]string{
			sgKey: "test-sg",
		})
		require.NoError(t, err)

		as, err := ovnClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.NotEmpty(t, as.UUID)
		require.Equal(t, asName, as.Name)
		require.Equal(t, map[string]string{
			sgKey: "test-sg",
		}, as.ExternalIDs)
	})

	t.Run("error occur because of invalid address set name", func(t *testing.T) {
		err := ovnClient.CreateAddressSet("test-create-as", map[string]string{
			sgKey: "test-sg",
		})
		require.Error(t, err)
	})
}

func (suite *OvnClientTestSuite) testAddressSetUpdateAddress() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	asName := "test_update_address_as"
	addresses := []string{"1.2.3.4", "1.2.3.6", "1.2.3.7"}

	err := ovnClient.CreateAddressSet(asName, map[string]string{
		sgKey: "test-sg",
	})
	require.NoError(t, err)

	t.Run("update address set v4 addresses", func(t *testing.T) {
		err = ovnClient.AddressSetUpdateAddress(asName, addresses...)
		require.NoError(t, err)

		as, err := ovnClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, addresses, as.Addresses)
	})

	t.Run("update address set v6 addresses", func(t *testing.T) {
		addresses := []string{"fe80::20c:29ff:fee4:16cc", "fe80::20c:29ff:fee4:1611"}
		err = ovnClient.AddressSetUpdateAddress(asName, addresses...)
		require.NoError(t, err)

		as, err := ovnClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, addresses, as.Addresses)
	})

	t.Run("clear address set addresses", func(t *testing.T) {
		err = ovnClient.AddressSetUpdateAddress(asName)
		require.NoError(t, err)

		as, err := ovnClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.Empty(t, as.Addresses)
	})
}

func (suite *OvnClientTestSuite) testDeleteAddressSet() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	asName := "test_delete_as"

	t.Run("no err when delete existent address set", func(t *testing.T) {
		t.Parallel()

		err := ovnClient.CreateAddressSet(asName, nil)
		require.NoError(t, err)

		err = ovnClient.DeleteAddressSet(asName)
		require.NoError(t, err)

		_, err = ovnClient.GetAddressSet(asName, false)
		require.ErrorContains(t, err, "object not found")
	})

	t.Run("no err when delete non-existent logical router", func(t *testing.T) {
		t.Parallel()
		err := ovnClient.DeleteAddressSet("test-delete-as-non-existent")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testDeleteAddressSets() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test-del-ass-pg"
	asPrefix := "test_del_ass"
	externalIDs := map[string]string{sgKey: pgName}

	for i := 0; i < 3; i++ {
		asName := fmt.Sprintf("%s_%d", asPrefix, i)
		err := ovnClient.CreateAddressSet(asName, externalIDs)
		require.NoError(t, err)
	}

	// create a new address set with no sg name, it should't be deleted
	asName := fmt.Sprintf("%s_%d", asPrefix, 3)
	err := ovnClient.CreateAddressSet(asName, nil)
	require.NoError(t, err)

	err = ovnClient.DeleteAddressSets(externalIDs)
	require.NoError(t, err)

	// it should't be deleted
	_, err = ovnClient.GetAddressSet(asName, false)
	require.NoError(t, err)

	// should delete
	ass, err := ovnClient.ListAddressSets(externalIDs)
	require.NoError(t, err)
	require.Empty(t, ass)
}

func (suite *OvnClientTestSuite) testListAddressSets() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient

	asName := "test_list_as_exist_key"

	err := ovnClient.CreateAddressSet(asName, map[string]string{sgKey: "sg", "direction": "to-lport", "key": "value"})
	require.NoError(t, err)

	ass, err := ovnClient.ListAddressSets(map[string]string{sgKey: "sg", "key": "value"})
	require.NoError(t, err)
	require.Len(t, ass, 1)
	require.Equal(t, asName, ass[0].Name)

}

func (suite *OvnClientTestSuite) test_addressSetFilter() {
	t := suite.T()
	t.Parallel()

	pgName := "test-filter-as-pg"
	asPrefix := "test-filter-as"

	ass := make([]*ovnnb.AddressSet, 0)

	t.Run("filter address set", func(t *testing.T) {
		// create two to-lport acl
		i := 0
		for ; i < 3; i++ {
			as := newAddressSet(fmt.Sprintf("%s-%d", asPrefix, i), map[string]string{
				sgKey: pgName,
			})
			ass = append(ass, as)
		}

		// create two as without sg name
		for ; i < 5; i++ {
			as := newAddressSet(fmt.Sprintf("%s-%d", asPrefix, i), nil)
			ass = append(ass, as)
		}

		// create two as with other sg name
		for ; i < 6; i++ {
			as := newAddressSet(fmt.Sprintf("%s-%d", asPrefix, i), map[string]string{
				sgKey: pgName + "-other",
			})
			ass = append(ass, as)
		}

		/* include all as */
		filterFunc := addressSetFilter(nil)
		count := 0
		for _, as := range ass {
			if filterFunc(as) {
				count++
			}
		}
		require.Equal(t, count, 6)

		filterFunc = addressSetFilter(map[string]string{sgKey: ""})
		count = 0
		for _, as := range ass {
			if filterFunc(as) {
				count++
			}
		}
		require.Equal(t, count, 4)

		/* include all as with sg name */
		filterFunc = addressSetFilter(map[string]string{sgKey: pgName})
		count = 0
		for _, as := range ass {
			if filterFunc(as) {
				count++
			}
		}
		require.Equal(t, count, 3)
	})

	t.Run("result should exclude as when externalIDs's length is not equal", func(t *testing.T) {
		asName := "test_filter_as_mismatch_length"
		as := newAddressSet(asName, map[string]string{
			sgKey: pgName,
		})

		filterFunc := addressSetFilter(map[string]string{sgKey: pgName, "direction": "to-lport"})
		out := filterFunc(as)
		require.False(t, out)
	})
}
