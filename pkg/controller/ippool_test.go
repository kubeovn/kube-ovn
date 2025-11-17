package controller

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestExpandIPPoolAddresses(t *testing.T) {
	addresses, err := util.ExpandIPPoolAddresses([]string{
		"10.0.0.1",
		"2001:db8::1",
		"192.168.1.0/24",
		"10.0.0.1", // duplicate should be removed
		" 2001:db8::1 ",
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		"10.0.0.1",
		"192.168.1.0/24",
		"2001:db8::1",
	}, addresses)
}

func TestExpandIPPoolAddressesRange(t *testing.T) {
	addresses, err := util.ExpandIPPoolAddresses([]string{"10.0.0.0..10.0.0.3"})
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.0/30"}, addresses)

	addresses, err = util.ExpandIPPoolAddresses([]string{"10.0.0.1..10.0.0.5"})
	require.NoError(t, err)
	require.Equal(t, []string{
		"10.0.0.1/32",
		"10.0.0.2/31",
		"10.0.0.4/31",
	}, addresses)

	addresses, err = util.ExpandIPPoolAddresses([]string{"2001:db8::1..2001:db8::4"})
	require.NoError(t, err)
	require.Equal(t, []string{
		"2001:db8::1/128",
		"2001:db8::2/127",
		"2001:db8::4/128",
	}, addresses)
}

func TestExpandIPPoolAddressesInvalid(t *testing.T) {
	_, err := util.ExpandIPPoolAddresses([]string{"10.0.0.1..2001:db8::1"})
	require.Error(t, err)

	_, err = util.ExpandIPPoolAddresses([]string{"foo"})
	require.Error(t, err)
}

func TestIPPoolAddressSetName(t *testing.T) {
	require.Equal(t, "foo.bar", util.IPPoolAddressSetName("foo-bar"))
	require.Equal(t, "123pool", util.IPPoolAddressSetName("123pool"))
}
