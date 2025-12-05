package ovs

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
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

	nbClient := suite.ovnNBClient
	asName := "test_create_as"

	t.Run("create address set", func(t *testing.T) {
		err := nbClient.CreateAddressSet(asName, map[string]string{
			sgKey: "test-sg",
		})
		require.NoError(t, err)

		as, err := nbClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.NotEmpty(t, as.UUID)
		require.Equal(t, asName, as.Name)
		// vendor is automatically added by CreateAddressSet
		require.Equal(t, map[string]string{
			sgKey:    "test-sg",
			"vendor": util.CniTypeName,
		}, as.ExternalIDs)
	})

	t.Run("error occur because of invalid address set name", func(t *testing.T) {
		err := nbClient.CreateAddressSet("test-create-as", map[string]string{
			sgKey: "test-sg",
		})
		require.Error(t, err)
	})

	t.Run("create address set that already exists", func(t *testing.T) {
		asName := "existing_address_set"
		err := nbClient.CreateAddressSet(asName, nil)
		require.NoError(t, err)

		// Attempt to create the same address set again
		err = nbClient.CreateAddressSet(asName, nil)
		require.NoError(t, err)

		// Verify that only one address set exists
		ass, err := nbClient.ListAddressSets(nil)
		require.NoError(t, err)
		count := 0
		for _, as := range ass {
			if as.Name == asName {
				count++
			}
		}
		require.Equal(t, 1, count)
	})
}

func (suite *OvnClientTestSuite) testAddressSetUpdateAddress() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	asName := "test_update_address_as"
	addresses := []string{"1.2.3.4", "1.2.3.6", "1.2.3.7"}

	err := nbClient.CreateAddressSet(asName, map[string]string{
		sgKey: "test-sg",
	})
	require.NoError(t, err)

	t.Run("update address set v4 addresses", func(t *testing.T) {
		err = nbClient.AddressSetUpdateAddress(asName, addresses...)
		require.NoError(t, err)

		as, err := nbClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, addresses, as.Addresses)
	})

	t.Run("update address set v6 addresses", func(t *testing.T) {
		addresses := []string{"fe80::20c:29ff:fee4:16cc", "fe80::20c:29ff:fee4:1611"}
		err = nbClient.AddressSetUpdateAddress(asName, addresses...)
		require.NoError(t, err)

		as, err := nbClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, addresses, as.Addresses)
	})

	t.Run("clear address set addresses", func(t *testing.T) {
		err = nbClient.AddressSetUpdateAddress(asName)
		require.NoError(t, err)

		as, err := nbClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.Empty(t, as.Addresses)
	})

	t.Run("update with mixed IPv4 and IPv6 addresses", func(t *testing.T) {
		addresses := []string{"192.168.1.1", "2001:db8::1", "10.0.0.1", "fe80::1"}
		err := nbClient.AddressSetUpdateAddress(asName, addresses...)
		require.NoError(t, err)

		as, err := nbClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, addresses, as.Addresses)
	})

	t.Run("update with CIDR notation", func(t *testing.T) {
		addresses := []string{"192.168.1.0/24", "2001:db8::/64"}
		err := nbClient.AddressSetUpdateAddress(asName, addresses...)
		require.NoError(t, err)

		as, err := nbClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"192.168.1.0/24", "2001:db8::/64"}, as.Addresses)
	})

	t.Run("update with duplicate addresses", func(t *testing.T) {
		addresses := []string{"192.168.1.1", "192.168.1.1", "2001:db8::1", "2001:db8::1"}
		err := nbClient.AddressSetUpdateAddress(asName, addresses...)
		require.NoError(t, err)

		as, err := nbClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"192.168.1.1", "2001:db8::1"}, as.Addresses)
	})

	t.Run("update with invalid CIDR", func(t *testing.T) {
		addresses := []string{"192.168.1.1", "invalid_cidr", "2001:db8::1"}
		err := nbClient.AddressSetUpdateAddress(asName, addresses...)
		require.NoError(t, err)

		as, err := nbClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"192.168.1.1", "invalid_cidr", "2001:db8::1"}, as.Addresses)
	})

	t.Run("update with empty address list", func(t *testing.T) {
		err := nbClient.AddressSetUpdateAddress(asName)
		require.NoError(t, err)

		as, err := nbClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.Empty(t, as.Addresses)
	})

	t.Run("update non-existent address set", func(t *testing.T) {
		nonExistentAS := "non_existent_as"
		err := nbClient.AddressSetUpdateAddress(nonExistentAS, "192.168.1.1")
		require.Error(t, err)
		require.Contains(t, err.Error(), "get address set")
	})

	t.Run("update address set with invalid address", func(t *testing.T) {
		err := nbClient.AddressSetUpdateAddress(asName, "192.168.1.1/xx")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testDeleteAddressSet() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	asName := "test_delete_as"

	t.Run("no err when delete existent address set", func(t *testing.T) {
		t.Parallel()

		err := nbClient.CreateAddressSet(asName, nil)
		require.NoError(t, err)

		err = nbClient.DeleteAddressSet(asName)
		require.NoError(t, err)

		_, err = nbClient.GetAddressSet(asName, false)
		require.ErrorContains(t, err, "object not found")
	})

	t.Run("no err when delete non-existent logical router", func(t *testing.T) {
		t.Parallel()
		err := nbClient.DeleteAddressSet("test-delete-as-non-existent")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testDeleteAddressSets() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test-del-ass-pg"
	asPrefix := "test_del_ass"
	externalIDs := map[string]string{sgKey: pgName}

	for i := range 3 {
		asName := fmt.Sprintf("%s_%d", asPrefix, i)
		err := nbClient.CreateAddressSet(asName, externalIDs)
		require.NoError(t, err)
	}

	// create a new address set with no sg name, it should't be deleted
	asName := fmt.Sprintf("%s_%d", asPrefix, 3)
	err := nbClient.CreateAddressSet(asName, nil)
	require.NoError(t, err)

	err = nbClient.DeleteAddressSets(externalIDs)
	require.NoError(t, err)

	// it should't be deleted
	_, err = nbClient.GetAddressSet(asName, false)
	require.NoError(t, err)

	// should delete
	ass, err := nbClient.ListAddressSets(externalIDs)
	require.NoError(t, err)
	require.Empty(t, ass)

	// delete address sets with empty externalIDs
	err = nbClient.DeleteAddressSets(map[string]string{})
	require.NoError(t, err)
}

func (suite *OvnClientTestSuite) testListAddressSets() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	asName := "test_list_as_exist_key"

	err := nbClient.CreateAddressSet(asName, map[string]string{sgKey: "sg", "direction": "to-lport", "key": "value"})
	require.NoError(t, err)

	ass, err := nbClient.ListAddressSets(map[string]string{sgKey: "sg", "key": "value"})
	require.NoError(t, err)
	require.Len(t, ass, 1)
	require.Equal(t, asName, ass[0].Name)
}

func (suite *OvnClientTestSuite) testAddressSetFilter() {
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

func (suite *OvnClientTestSuite) testUpdateAddressSet() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	as := newAddressSet("test_update_as", map[string]string{
		sgKey: "test-sg",
	})

	t.Run("update with nil address set", func(t *testing.T) {
		err := nbClient.UpdateAddressSet(as, nil)
		require.Error(t, err)
	})

	t.Run("update with nil address set", func(t *testing.T) {
		err := nbClient.UpdateAddressSet(nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "address_set is nil")
	})
}

func (suite *OvnClientTestSuite) testBatchDeleteAddressSetByNames() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	asName := "test_batch_delete_as"

	t.Run("no err when delete existent address set", func(t *testing.T) {
		t.Parallel()

		err := nbClient.CreateAddressSet(asName, nil)
		require.NoError(t, err)

		err = nbClient.BatchDeleteAddressSetByNames([]string{asName})
		require.NoError(t, err)

		_, err = nbClient.GetAddressSet(asName, false)
		require.ErrorContains(t, err, "object not found")
	})

	t.Run("no err when delete non-existent address set", func(t *testing.T) {
		t.Parallel()
		err := nbClient.BatchDeleteAddressSetByNames([]string{"test-batch-delete-as-non-existent"})
		require.NoError(t, err)
	})
}
