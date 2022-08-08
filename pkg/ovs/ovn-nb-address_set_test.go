package ovs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *OvnClientTestSuite) testCreateAddressSet() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	asName := "test_create_as"

	t.Run("create address set", func(t *testing.T) {
		err := ovnClient.CreateAddressSet(asName, map[string]string{
			"sg": "test-sg",
		})
		require.NoError(t, err)

		as, err := ovnClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.NotEmpty(t, as.UUID)
		require.Equal(t, asName, as.Name)
		require.Equal(t, map[string]string{
			"sg": "test-sg",
		}, as.ExternalIDs)
	})

	t.Run("error occur because of invalid address set name", func(t *testing.T) {
		err := ovnClient.CreateAddressSet("test-create-as", map[string]string{
			"sg": "test-sg",
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
		"sg": "test-sg",
	})
	require.NoError(t, err)

	t.Run("update address set v4 addresses", func(t *testing.T) {
		err = ovnClient.AddressSetUpdateAddress(asName, addresses...)
		require.NoError(t, err)

		as, err := ovnClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.Equal(t, addresses, as.Addresses)
	})

	t.Run("update address set v6 addresses", func(t *testing.T) {
		addresses := []string{"fe80::20c:29ff:fee4:16cc", "fe80::20c:29ff:fee4:1611"}
		err = ovnClient.AddressSetUpdateAddress(asName, addresses...)
		require.NoError(t, err)

		as, err := ovnClient.GetAddressSet(asName, false)
		require.NoError(t, err)
		require.Equal(t, addresses, as.Addresses)
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

func (suite *OvnClientTestSuite) testListAddressSets() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient

	t.Run("result should exclude as when externalIDs's length is not equal", func(t *testing.T) {
		asName := "test_list_as_mismatch_length"

		err := ovnClient.CreateAddressSet(asName, map[string]string{"key": "value"})
		require.NoError(t, err)

		ass, err := ovnClient.ListAddressSets(map[string]string{"sg": "sg", "direction": "to-lport"})
		require.NoError(t, err)
		require.Empty(t, ass)
	})

	t.Run("result should include as when key exists in as column: external_ids", func(t *testing.T) {
		asName := "test_list_as_exist_key"

		err := ovnClient.CreateAddressSet(asName, map[string]string{"sg": "sg", "direction": "to-lport", "key": "value"})
		require.NoError(t, err)

		ass, err := ovnClient.ListAddressSets(map[string]string{"sg": "sg", "key": "value"})
		require.NoError(t, err)
		require.Len(t, ass, 1)
		require.Equal(t, asName, ass[0].Name)
	})

	t.Run("result should include all as when externalIDs is empty", func(t *testing.T) {
		prefix := "test_list_as_all"

		for i := 0; i < 4; i++ {
			asName := fmt.Sprintf("%s_%d", prefix, i)

			err := ovnClient.CreateAddressSet(asName, map[string]string{"sg": "sg", "direction": "to-lport", "key": "value"})
			require.NoError(t, err)
		}

		out, err := ovnClient.ListAddressSets(nil)
		require.NoError(t, err)
		count := 0
		for _, v := range out {
			if strings.Contains(v.Name, prefix) {
				count++
			}
		}
		require.Equal(t, count, 4)

		out, err = ovnClient.ListAddressSets(map[string]string{})
		require.NoError(t, err)
		count = 0
		for _, v := range out {
			if strings.Contains(v.Name, prefix) {
				count++
			}
		}
		require.Equal(t, count, 4)
	})

	t.Run("result should include as which externalIDs[key] is ''", func(t *testing.T) {
		asName := "test_list_as_no_val"

		err := ovnClient.CreateAddressSet(asName, map[string]string{"sg_test": "sg", "direction": "to-lport", "key": "value"})
		require.NoError(t, err)

		ass, err := ovnClient.ListAddressSets(map[string]string{"sg_test": "", "key": ""})
		require.NoError(t, err)
		require.Len(t, ass, 1)
		require.Equal(t, asName, ass[0].Name)

		ass, err = ovnClient.ListAddressSets(map[string]string{"sg_test": ""})
		require.NoError(t, err)
		require.Len(t, ass, 1)
		require.Equal(t, asName, ass[0].Name)

		ass, err = ovnClient.ListAddressSets(map[string]string{"sg_test": "", "key": "", "key1": ""})
		require.NoError(t, err)
		require.Empty(t, ass)
	})
}
